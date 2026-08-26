package riff_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"testing"

	"yellowjacket/backend/riff"
)

// chunk is one sub-chunk to put in a test container.
type chunk struct {
	id   string
	data []byte
}

// buildRIFF assembles a container from magic, form type and chunks,
// padding odd-length chunks the way a writer must.
func buildRIFF(magic, form string, chunks []chunk) []byte {
	var body bytes.Buffer

	body.WriteString(form)

	for _, c := range chunks {
		body.WriteString(c.id)
		_ = binary.Write(&body, binary.LittleEndian, uint32(len(c.data)))
		body.Write(c.data)

		if len(c.data)%2 != 0 {
			body.WriteByte(0)
		}
	}

	var out bytes.Buffer

	out.WriteString(magic)
	_ = binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())

	return out.Bytes()
}

func TestID3Chunk_FindsTheTagPastTheAudio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		chunks []chunk
		want   string
	}{
		{
			name: "after an odd-length chunk",
			chunks: []chunk{
				{id: "fmt ", data: make([]byte, 16)},
				{id: "LIST", data: []byte("INFOodd")},
				{id: "data", data: make([]byte, 200)},
				{id: "id3 ", data: []byte("ID3vTAG")},
			},
			want: "ID3vTAG",
		},
		{
			// The chunk ID is written both ways in the wild, and the
			// writer accepts either, so the reader must too.
			name: "uppercase ID3",
			chunks: []chunk{
				{id: "data", data: make([]byte, 8)},
				{id: "ID3 ", data: []byte("upper")},
			},
			want: "upper",
		},
		{
			name: "first chunk",
			chunks: []chunk{
				{id: "id3 ", data: []byte("first")},
				{id: "data", data: make([]byte, 8)},
			},
			want: "first",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := bytes.NewReader(buildRIFF("RIFF", "WAVE", tc.chunks))

			got, err := riff.ID3Chunk(r)
			if err != nil {
				t.Fatalf("ID3Chunk: %v", err)
			}

			if string(got) != tc.want {
				t.Errorf("chunk data: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestID3Chunk_RejectsWhatItCannotRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		bytes []byte
		want  error
	}{
		{
			name:  "no ID3 chunk",
			bytes: buildRIFF("RIFF", "WAVE", []chunk{{id: "data", data: []byte{1, 2}}}),
			want:  riff.ErrNoID3Chunk,
		},
		{
			name:  "no chunks at all",
			bytes: buildRIFF("RIFF", "WAVE", nil),
			want:  riff.ErrNoID3Chunk,
		},
		{
			name:  "not RIFF",
			bytes: []byte("ID3\x03\x00\x00\x00\x00\x00\x00\x00\x00"),
			want:  riff.ErrNotRIFF,
		},
		{
			name:  "not WAVE",
			bytes: buildRIFF("RIFF", "AVI ", []chunk{{id: "id3 ", data: []byte("x")}}),
			want:  riff.ErrNotWAVE,
		},
		{
			name:  "RF64",
			bytes: buildRIFF("RF64", "WAVE", []chunk{{id: "id3 ", data: []byte("x")}}),
			want:  riff.ErrRF64NotSupported,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := riff.ID3Chunk(bytes.NewReader(tc.bytes))
			if !errors.Is(err, tc.want) {
				t.Errorf("ID3Chunk error: got %v, want %v", err, tc.want)
			}
		})
	}
}

// A file cut short mid-chunk is a file with no tag, not a reason to
// allocate the size it claims: the declared size is four bytes any
// truncation can leave saying 4 GB.
func TestID3Chunk_ToleratesATruncatedFile(t *testing.T) {
	t.Parallel()

	full := buildRIFF("RIFF", "WAVE", []chunk{
		{id: "data", data: make([]byte, 64)},
		{id: "id3 ", data: []byte("tag")},
	})

	t.Run("cut inside the audio", func(t *testing.T) {
		t.Parallel()

		_, err := riff.ID3Chunk(bytes.NewReader(full[:32]))
		if !errors.Is(err, riff.ErrNoID3Chunk) {
			t.Errorf("ID3Chunk error: got %v, want %v", err, riff.ErrNoID3Chunk)
		}
	})

	t.Run("cut inside the tag", func(t *testing.T) {
		t.Parallel()

		if _, err := riff.ID3Chunk(bytes.NewReader(full[:len(full)-2])); err == nil {
			t.Error("ID3Chunk: got nil error for a truncated tag chunk")
		}
	})
}

// Parse is the writer's half and reads every chunk into memory, which
// is what preserving them needs.
func TestParse_ReadsEveryChunkInOrder(t *testing.T) {
	t.Parallel()

	raw := buildRIFF("RIFF", "WAVE", []chunk{
		{id: "fmt ", data: make([]byte, 16)},
		{id: "LIST", data: []byte("INFOodd")},
		{id: "id3 ", data: []byte("tag")},
	})

	chunks, err := riff.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []string{"fmt ", "LIST", "id3 "}
	if len(chunks) != len(want) {
		t.Fatalf("chunk count: got %d, want %d", len(chunks), len(want))
	}

	for i, id := range want {
		if got := string(chunks[i].ID[:]); got != id {
			t.Errorf("chunk %d: got %q, want %q", i, got, id)
		}
	}

	if !riff.IsID3(chunks[2].ID) || string(chunks[2].Data) != "tag" {
		t.Errorf("id3 chunk: got %q", chunks[2].Data)
	}

	// The padding byte after an odd chunk is not part of its data.
	if string(chunks[1].Data) != "INFOodd" {
		t.Errorf("odd chunk data: got %q, want %q", chunks[1].Data, "INFOodd")
	}
}

// A chunk size is four bytes read off the file, so a truncated or
// malformed WAV is free to declare a chunk larger than the whole of
// itself.  Parse must grow with what arrives rather than with what was
// claimed.
//
// This measures the allocation instead of the error because the error
// is the same either way: a build sizing its buffer from the header
// reports the truncation correctly, having asked the allocator for a
// gigabyte on the way.  Deliberately not parallel — TotalAlloc is
// process-wide, and a test paused beside another one is measuring it
// too.
func TestParse_DoesNotAllocateWhatAChunkClaims(t *testing.T) {
	// Large enough that a header-sized buffer is unmistakable, in a
	// container of a few dozen bytes.
	const declared = 1 << 30

	var raw bytes.Buffer

	raw.WriteString("RIFF")
	_ = binary.Write(&raw, binary.LittleEndian, uint32(declared+12))
	raw.WriteString("WAVE")
	raw.WriteString("data")
	_ = binary.Write(&raw, binary.LittleEndian, uint32(declared))
	raw.WriteString("and then the file ends")

	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	if _, err := riff.Parse(bytes.NewReader(raw.Bytes())); err == nil {
		t.Fatal("Parse: got nil error for a chunk larger than the file holding it")
	}

	runtime.ReadMemStats(&after)

	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Errorf("Parse allocated %d bytes reading a %d-byte file whose chunk header claimed %d",
			grew, raw.Len(), declared)
	}
}

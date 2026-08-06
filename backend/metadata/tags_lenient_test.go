package metadata

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// errTestParseFailure stands in for the strict parser's error when
// exercising the fallback directly.
var errTestParseFailure = errors.New("original parse failure")

// synchsafe encodes n as a 4-byte ID3v2.4 synchsafe integer.
func synchsafe(n int) []byte {
	return []byte{
		byte(n>>21) & 0x7f,
		byte(n>>14) & 0x7f,
		byte(n>>7) & 0x7f,
		byte(n) & 0x7f,
	}
}

// id3Frame builds a single ID3v2.4 frame from a raw body.
func id3Frame(id string, body []byte) []byte {
	var buf bytes.Buffer

	buf.WriteString(id)
	buf.Write(synchsafe(len(body)))
	buf.Write([]byte{0, 0}) // Flags.
	buf.Write(body)

	return buf.Bytes()
}

// utf8TextBody builds a UTF-8 text frame body (encoding byte $03).
func utf8TextBody(s string) []byte {
	return append([]byte{3}, []byte(s)...)
}

// malformedUTF16TXXX reproduces the real-world frame that motivated the
// lenient fallback: a UTF-16BE TXXX frame carrying two stray NUL bytes,
// one after the description terminator and one at the very end.  Both
// the description and the value end up an odd number of bytes, which
// dhowden/tag rejects outright.
func malformedUTF16TXXX(description, value string) []byte {
	var buf bytes.Buffer

	buf.WriteByte(1) // Encoding: UTF-16 with BOM.

	writeUTF16BE := func(s string) {
		buf.Write([]byte{0xfe, 0xff}) // Big-endian BOM.

		for _, r := range s {
			_ = binary.Write(&buf, binary.BigEndian, uint16(r))
		}
	}

	writeUTF16BE(description)
	buf.Write([]byte{0, 0}) // Terminator.
	buf.WriteByte(0)        // Stray NUL.
	writeUTF16BE(value)
	buf.WriteByte(0) // Stray NUL.

	return buf.Bytes()
}

// buildID3v24 assembles a tag from frames and appends a stub of MPEG
// audio so the result looks like a real file to a parser.
func buildID3v24(frames ...[]byte) []byte {
	body := bytes.Join(frames, nil)

	var buf bytes.Buffer

	buf.WriteString("ID3")
	buf.Write([]byte{4, 0}) // Version 2.4.0.
	buf.WriteByte(0)        // Flags.
	buf.Write(synchsafe(len(body)))
	buf.Write(body)
	buf.Write([]byte{0xff, 0xfb, 0x90, 0x00}) // MPEG frame header stub.

	return buf.Bytes()
}

func TestExtractTagsFromReaderRecoversMalformedFrame(t *testing.T) {
	data := buildID3v24(
		id3Frame("TIT2", utf8TextBody("Evolution's a Lie")),
		id3Frame("TPE1", utf8TextBody("Ariel Pink")),
		id3Frame("TALB", utf8TextBody("Sit n' Spin")),
		id3Frame("TRCK", utf8TextBody("1/17")),
		id3Frame("TXXX", malformedUTF16TXXX("LABEL", "Mexican Summer")),
	)

	meta, err := ExtractTagsFromReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ExtractTagsFromReader returned a hard error: %v", err)
	}

	if !errors.Is(meta.TagReadWarning, ErrTagsRecovered) {
		t.Errorf(
			"TagReadWarning = %v, want it to wrap ErrTagsRecovered",
			meta.TagReadWarning,
		)
	}

	if meta.Title != "Evolution's a Lie" {
		t.Errorf("Title = %q, want %q", meta.Title, "Evolution's a Lie")
	}

	if meta.Artist != "Ariel Pink" {
		t.Errorf("Artist = %q, want %q", meta.Artist, "Ariel Pink")
	}

	if meta.Album != "Sit n' Spin" {
		t.Errorf("Album = %q, want %q", meta.Album, "Sit n' Spin")
	}

	if meta.TrackNumber != 1 || meta.TotalTracks != 17 {
		t.Errorf(
			"track = %d/%d, want 1/17",
			meta.TrackNumber, meta.TotalTracks,
		)
	}
}

// TestExtractTagsFromReaderCleanTagHasNoWarning guards against the
// fallback firing on tags the strict parser handles.
func TestExtractTagsFromReaderCleanTagHasNoWarning(t *testing.T) {
	data := buildID3v24(
		id3Frame("TIT2", utf8TextBody("Clean Title")),
		id3Frame("TXXX", utf8TextBody("LABEL\x00Mexican Summer")),
	)

	meta, err := ExtractTagsFromReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ExtractTagsFromReader: %v", err)
	}

	if meta.TagReadWarning != nil {
		t.Errorf("TagReadWarning = %v, want nil", meta.TagReadWarning)
	}

	if meta.Title != "Clean Title" {
		t.Errorf("Title = %q, want %q", meta.Title, "Clean Title")
	}
}

// TestRecoverTagsUnsalvageable covers the case where the fallback finds
// no ID3v2 frames either: metadata comes back empty but usable, with a
// warning that tells the caller to fall back to the filename.
func TestRecoverTagsUnsalvageable(t *testing.T) {
	r := strings.NewReader("not an audio file at all")

	meta := recoverTags(r, errTestParseFailure)

	if !errors.Is(meta.TagReadWarning, ErrTagsUnreadable) {
		t.Errorf(
			"TagReadWarning = %v, want it to wrap ErrTagsUnreadable",
			meta.TagReadWarning,
		)
	}

	if !errors.Is(meta.TagReadWarning, errTestParseFailure) {
		t.Errorf("TagReadWarning = %v, want it to wrap the cause", meta.TagReadWarning)
	}

	if meta.Title != "" || meta.Artist != "" {
		t.Errorf("expected empty metadata, got %+v", meta)
	}
}

func TestParsePosition(t *testing.T) {
	tests := []struct {
		raw          string
		want, wantOf int
	}{
		{"", 0, 0},
		{"3", 3, 0},
		{"3/17", 3, 17},
		{" 3 / 17 ", 3, 17},
		{"03/17", 3, 17},
		{"A/B", 0, 0},
		{"1/", 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, gotOf := parsePosition(tt.raw)
			if got != tt.want || gotOf != tt.wantOf {
				t.Errorf(
					"parsePosition(%q) = %d/%d, want %d/%d",
					tt.raw, got, gotOf, tt.want, tt.wantOf,
				)
			}
		})
	}
}

func TestParseLeadingInt(t *testing.T) {
	tests := map[string]int{
		"":           0,
		"2021":       2021,
		"2021-06-11": 2021,
		"1995\t":     1995,
		"none":       0,
	}

	for raw, want := range tests {
		if got := parseLeadingInt(raw); got != want {
			t.Errorf("parseLeadingInt(%q) = %d, want %d", raw, got, want)
		}
	}
}

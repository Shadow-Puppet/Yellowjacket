package tagwriter

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	id3v2 "github.com/bogem/id3v2/v2"
	"github.com/dhowden/tag"

	"yellowjacket/backend/metadata"
)

// createTestWAV builds a minimal valid WAV file with an optional
// ID3v2 tag populated from fields.  It returns the path to the
// created file inside dir.
func createTestWAV(
	t *testing.T,
	dir string,
	name string,
	fields TagChanges,
) string {
	t.Helper()

	var buf bytes.Buffer

	// --- fmt chunk (24 bytes total: 8 header + 16 data) ---
	fmtData := makePCMFmtData()

	// --- data chunk: 200 bytes of silence ---
	const silenceLen = 200 //nolint:mnd
	silence := make([]byte, silenceLen)

	// --- optional id3 chunk ---
	var id3Bytes []byte

	if len(fields) > 0 {
		id3Tag := id3v2.NewEmptyTag()
		id3Tag.SetDefaultEncoding(id3v2.EncodingUTF8)
		applyTextChanges(id3Tag, fields)
		applyCoverArtChanges(id3Tag, fields)

		var id3Buf bytes.Buffer
		if _, err := id3Tag.WriteTo(&id3Buf); err != nil {
			t.Fatalf("write id3v2 tag: %v", err)
		}

		id3Bytes = id3Buf.Bytes()
	}

	// Calculate RIFF payload size:
	//   4 (WAVE)
	// + 8 + 16 (fmt chunk)
	// + 8 + silenceLen (data chunk)
	// + optional id3 chunk: 8 + len(id3Bytes) + padding
	riffPayload := uint32(4 + 8 + 16 + 8 + silenceLen) //nolint:mnd

	if len(id3Bytes) > 0 {
		riffPayload += 8 + uint32(len(id3Bytes)) //nolint:mnd

		if len(id3Bytes)%2 != 0 {
			riffPayload++
		}
	}

	// RIFF header.
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, riffPayload)
	buf.WriteString("WAVE")

	// fmt chunk.
	buf.WriteString("fmt ")
	_ = binary.Write(
		&buf, binary.LittleEndian, uint32(len(fmtData)),
	)
	buf.Write(fmtData)

	// data chunk.
	buf.WriteString("data")
	_ = binary.Write(
		&buf, binary.LittleEndian, uint32(silenceLen),
	)
	buf.Write(silence)

	// id3 chunk (if any).
	if len(id3Bytes) > 0 {
		buf.WriteString("id3 ")
		_ = binary.Write(
			&buf, binary.LittleEndian, uint32(len(id3Bytes)),
		)
		buf.Write(id3Bytes)

		if len(id3Bytes)%2 != 0 {
			buf.WriteByte(0)
		}
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write test WAV: %v", err)
	}

	return path
}

// makePCMFmtData returns 16 bytes of PCM format data:
// AudioFormat=1, Channels=1, SampleRate=44100,
// ByteRate=88200, BlockAlign=2, BitsPerSample=16.
func makePCMFmtData() []byte {
	var buf bytes.Buffer

	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))     // PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))     // mono
	_ = binary.Write(&buf, binary.LittleEndian, uint32(44100)) // sample rate
	_ = binary.Write(&buf, binary.LittleEndian, uint32(88200)) // byte rate
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))     // block align
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))    // bits per sample

	return buf.Bytes()
}

// readWavID3Tags extracts ID3v2 metadata from a WAV file by parsing
// the RIFF structure and reading the id3 chunk with dhowden/tag.
// This is necessary because dhowden/tag's ReadFrom does not support
// WAV files directly.
func readWavID3Tags(
	t *testing.T,
	path string,
) *metadata.TrackMetadata {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open WAV for read-back: %v", err)
	}

	defer func() { _ = f.Close() }()

	chunks, err := parseRIFF(f)
	if err != nil {
		t.Fatalf("parseRIFF: %v", err)
	}

	// Find the id3 chunk.
	var id3Data []byte

	for _, c := range chunks {
		if isID3ChunkID(c.id) {
			id3Data = c.data

			break
		}
	}

	if id3Data == nil {
		t.Fatal("no id3 chunk found in WAV file")
	}

	// dhowden/tag ReadID3v2Tags expects an io.ReadSeeker starting
	// with the "ID3" magic bytes.
	m, err := tag.ReadID3v2Tags(bytes.NewReader(id3Data))
	if err != nil {
		t.Fatalf("ReadID3v2Tags: %v", err)
	}

	trackNum, _ := m.Track()
	discNum, _ := m.Disc()

	meta := &metadata.TrackMetadata{
		Title:       m.Title(),
		Artist:      m.Artist(),
		Album:       m.Album(),
		AlbumArtist: m.AlbumArtist(),
		Composer:    m.Composer(),
		Genre:       m.Genre(),
		Year:        m.Year(),
		TrackNumber: trackNum,
		DiscNumber:  discNum,
	}

	if pic := m.Picture(); pic != nil {
		meta.Picture = &metadata.PictureData{
			Data:     pic.Data,
			MIMEType: pic.MIMEType,
			Ext:      pic.Ext,
		}
	}

	return meta
}

// createTestWAVWithExtraChunks creates a WAV file containing
// additional non-standard chunks (LIST INFO, bext) to verify that
// writeWavTags preserves chunks it does not understand.
func createTestWAVWithExtraChunks(
	t *testing.T,
	dir string,
	name string,
) string {
	t.Helper()

	var buf bytes.Buffer

	fmtData := makePCMFmtData()

	// LIST INFO chunk: LIST + size + "INFO" + INAM sub-chunk.
	var listBuf bytes.Buffer

	listBuf.WriteString("INFO")

	// INAM sub-chunk.
	inamValue := []byte("Test Track Name")
	listBuf.WriteString("INAM")
	_ = binary.Write(
		&listBuf, binary.LittleEndian, uint32(len(inamValue)),
	)
	listBuf.Write(inamValue)

	// Pad INAM if odd length.
	if len(inamValue)%2 != 0 {
		listBuf.WriteByte(0)
	}

	listData := listBuf.Bytes()

	// Fake bext chunk: 8 bytes of test data.
	bextData := []byte{0xBE, 0xEF, 0xCA, 0xFE, 0xDE, 0xAD, 0x01, 0x02}

	// 200 bytes of silence for data chunk.
	const silenceLen = 200 //nolint:mnd
	silence := make([]byte, silenceLen)

	// Calculate RIFF payload.
	riffPayload := uint32(4) // WAVE

	// fmt: 8 + 16.
	riffPayload += 8 + uint32(len(fmtData)) //nolint:mnd

	// LIST: 8 + len(listData).
	riffPayload += 8 + uint32(len(listData)) //nolint:mnd

	// bext: 8 + len(bextData).
	riffPayload += 8 + uint32(len(bextData)) //nolint:mnd

	// data: 8 + silenceLen.
	riffPayload += 8 + silenceLen //nolint:mnd

	// RIFF header.
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, riffPayload)
	buf.WriteString("WAVE")

	// fmt chunk.
	buf.WriteString("fmt ")
	_ = binary.Write(
		&buf, binary.LittleEndian, uint32(len(fmtData)),
	)
	buf.Write(fmtData)

	// LIST chunk.
	buf.WriteString("LIST")
	_ = binary.Write(
		&buf, binary.LittleEndian, uint32(len(listData)),
	)
	buf.Write(listData)

	// bext chunk.
	buf.WriteString("bext")
	_ = binary.Write(
		&buf, binary.LittleEndian, uint32(len(bextData)),
	)
	buf.Write(bextData)

	// data chunk.
	buf.WriteString("data")
	_ = binary.Write(
		&buf, binary.LittleEndian, uint32(silenceLen),
	)
	buf.Write(silence)

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write test WAV with extra chunks: %v", err)
	}

	return path
}

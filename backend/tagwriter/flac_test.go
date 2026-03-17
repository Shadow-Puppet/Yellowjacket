package tagwriter

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	flac "github.com/go-flac/go-flac/v2"

	"yellowjacket/backend/metadata"
)

// Test-only sentinel errors for the Vorbis Comment parser.
var (
	errVorbisCommentTooShort  = errors.New("vorbis comment too short")
	errVorbisCommentTruncated = errors.New("vorbis comment truncated")
)

// makeMinimalFLAC creates a minimal valid FLAC file at path.
// The file contains: fLaC header, StreamInfo metadata block, and a
// minimal valid FLAC frame (sync code + padding).
func makeMinimalFLAC(t *testing.T, path string) {
	t.Helper()

	var buf bytes.Buffer

	// fLaC magic.
	buf.WriteString("fLaC")

	// StreamInfo metadata block (type=0, last=true → 0x80).
	// StreamInfo data is exactly 34 bytes.
	streamInfo := makeStreamInfoData()
	// Block header: type byte (0x80 = last + StreamInfo), 3-byte length.
	buf.WriteByte(0x80) // last-metadata-block flag | StreamInfo type
	// 3-byte big-endian length of StreamInfo data.
	siLen := uint32(len(streamInfo))
	buf.WriteByte(byte(siLen >> 16))
	buf.WriteByte(byte(siLen >> 8))
	buf.WriteByte(byte(siLen))
	buf.Write(streamInfo)

	// Minimal FLAC frame — sync code 0xFFF8 + enough padding bytes to
	// form a parseable (though silent) frame. The go-flac library only
	// checks for the sync code (0xFF, byte>>2 == 0x3E) when parsing frames.
	buf.Write([]byte{0xFF, 0xF8}) // sync code
	// Add some zero padding as a pseudo-frame body.
	buf.Write(make([]byte, 16))

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write minimal FLAC: %v", err)
	}
}

// makeStreamInfoData creates a 34-byte StreamInfo metadata block body:
// - min/max block size: 4096
// - min/max frame size: 0
// - sample rate: 44100, channels: 1 (mono), bits per sample: 16
// - total samples: 0
// - MD5: all zeros.
func makeStreamInfoData() []byte {
	var buf bytes.Buffer

	_ = binary.Write(&buf, binary.BigEndian, uint16(4096)) // min block size
	_ = binary.Write(&buf, binary.BigEndian, uint16(4096)) // max block size

	// min frame size (3 bytes, big-endian).
	buf.Write([]byte{0, 0, 0})
	// max frame size (3 bytes, big-endian).
	buf.Write([]byte{0, 0, 0})

	// Next 8 bytes pack: sample rate (20 bits), channels-1 (3 bits),
	// bits-per-sample-1 (5 bits), total samples (36 bits).
	//
	// 44100 Hz, 1 channel, 16 bps, 0 total samples.
	buf.Write([]byte{
		0xAC, 0x44, 0x00, 0xF0,
		0x00, 0x00, 0x00, 0x00,
	})

	// MD5 signature (16 bytes of zeros).
	buf.Write(make([]byte, 16))

	return buf.Bytes()
}

func TestWriteFlacTags_TextFields(t *testing.T) {
	tmpDir := t.TempDir()
	flacPath := filepath.Join(tmpDir, "test.flac")
	makeMinimalFLAC(t, flacPath)

	changes := TagChanges{
		FieldTitle:       "Test Title",
		FieldArtist:      "Test Artist",
		FieldAlbum:       "Test Album",
		FieldAlbumArtist: "Test Album Artist",
		FieldGenre:       "Rock",
		FieldYear:        2024,
		FieldTrackNumber: 3,
		FieldDiscNumber:  1,
		FieldComposer:    "Test Composer",
	}

	if err := writeFlacTags(testLogger(), flacPath, changes); err != nil {
		t.Fatalf("writeFlacTags: %v", err)
	}

	// Read back with metadata.ExtractTags to verify round-trip.
	tags, err := metadata.ExtractTags(flacPath)
	if err != nil {
		t.Fatalf("ExtractTags: %v", err)
	}

	assertEqual(t, "Title", "Test Title", tags.Title)
	assertEqual(t, "Artist", "Test Artist", tags.Artist)
	assertEqual(t, "Album", "Test Album", tags.Album)
	assertEqual(t, "AlbumArtist", "Test Album Artist", tags.AlbumArtist)
	assertEqual(t, "Genre", "Rock", tags.Genre)
	assertEqual(t, "Composer", "Test Composer", tags.Composer)
	assertEqual(t, "Year", 2024, tags.Year)
	assertEqual(t, "TrackNumber", 3, tags.TrackNumber)
	assertEqual(t, "DiscNumber", 1, tags.DiscNumber)
}

func TestWriteFlacTags_CoverArt(t *testing.T) {
	tmpDir := t.TempDir()
	flacPath := filepath.Join(tmpDir, "test.flac")
	makeMinimalFLAC(t, flacPath)

	jpegData := tinyJPEG(t)

	changes := TagChanges{
		FieldCoverArt: jpegData,
	}

	if err := writeFlacTags(testLogger(), flacPath, changes); err != nil {
		t.Fatalf("writeFlacTags: %v", err)
	}

	tags, err := metadata.ExtractTags(flacPath)
	if err != nil {
		t.Fatalf("ExtractTags: %v", err)
	}

	if tags.Picture == nil {
		t.Fatal("expected picture data, got nil")
	}

	if !bytes.Equal(tags.Picture.Data, jpegData) {
		t.Errorf("picture data mismatch: got %d bytes, want %d bytes",
			len(tags.Picture.Data), len(jpegData))
	}

	assertEqual(t, "MIME type", "image/jpeg", tags.Picture.MIMEType)
}

func TestWriteFlacTags_ClearCoverArt(t *testing.T) {
	tmpDir := t.TempDir()
	flacPath := filepath.Join(tmpDir, "test.flac")
	makeMinimalFLAC(t, flacPath)

	// First add cover art.
	jpegData := tinyJPEG(t)

	err := writeFlacTags(testLogger(), flacPath, TagChanges{FieldCoverArt: jpegData})
	if err != nil {
		t.Fatalf("add cover art: %v", err)
	}

	// Verify art was added.
	tags, err := metadata.ExtractTags(flacPath)
	if err != nil {
		t.Fatalf("ExtractTags after add: %v", err)
	}

	if tags.Picture == nil {
		t.Fatal("expected picture after add, got nil")
	}

	// Now clear cover art (nil value).
	if err := writeFlacTags(testLogger(), flacPath, TagChanges{FieldCoverArt: nil}); err != nil {
		t.Fatalf("clear cover art: %v", err)
	}

	tags, err = metadata.ExtractTags(flacPath)
	if err != nil {
		t.Fatalf("ExtractTags after clear: %v", err)
	}

	if tags.Picture != nil {
		t.Errorf("expected no picture after clear, got %d bytes", len(tags.Picture.Data))
	}
}

func TestWriteFlacTags_PartialUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	flacPath := filepath.Join(tmpDir, "test.flac")
	makeMinimalFLAC(t, flacPath)

	// Write all fields first.
	allFields := TagChanges{
		FieldTitle:       "Original Title",
		FieldArtist:      "Original Artist",
		FieldAlbum:       "Original Album",
		FieldGenre:       "Jazz",
		FieldYear:        2020,
		FieldTrackNumber: 5,
		FieldDiscNumber:  2,
		FieldComposer:    "Original Composer",
	}

	if err := writeFlacTags(testLogger(), flacPath, allFields); err != nil {
		t.Fatalf("write all fields: %v", err)
	}

	// Update only title and genre.
	partial := TagChanges{
		FieldTitle: "Updated Title",
		FieldGenre: "Blues",
	}

	if err := writeFlacTags(testLogger(), flacPath, partial); err != nil {
		t.Fatalf("partial update: %v", err)
	}

	tags, err := metadata.ExtractTags(flacPath)
	if err != nil {
		t.Fatalf("ExtractTags: %v", err)
	}

	// Changed fields should have new values.
	assertEqual(t, "Title", "Updated Title", tags.Title)
	assertEqual(t, "Genre", "Blues", tags.Genre)

	// Unchanged fields should be preserved.
	assertEqual(t, "Artist", "Original Artist", tags.Artist)
	assertEqual(t, "Album", "Original Album", tags.Album)
	assertEqual(t, "Year", 2020, tags.Year)
	assertEqual(t, "TrackNumber", 5, tags.TrackNumber)
	assertEqual(t, "DiscNumber", 2, tags.DiscNumber)
	assertEqual(t, "Composer", "Original Composer", tags.Composer)
}

func TestWriteFlacTags_PreservesStreamInfo(t *testing.T) {
	tmpDir := t.TempDir()
	flacPath := filepath.Join(tmpDir, "test.flac")
	makeMinimalFLAC(t, flacPath)

	// Parse before writing to capture original StreamInfo.
	fBefore, err := flac.ParseFile(flacPath)
	if err != nil {
		t.Fatalf("parse before: %v", err)
	}

	siBefore, err := fBefore.GetStreamInfo()
	if err != nil {
		t.Fatalf("get stream info before: %v", err)
	}

	_ = fBefore.Close()

	// Write tags.
	changes := TagChanges{
		FieldTitle:  "Some Title",
		FieldArtist: "Some Artist",
	}

	if err := writeFlacTags(testLogger(), flacPath, changes); err != nil {
		t.Fatalf("writeFlacTags: %v", err)
	}

	// Parse after writing and verify StreamInfo is intact.
	fAfter, err := flac.ParseFile(flacPath)
	if err != nil {
		t.Fatalf("parse after: %v", err)
	}

	defer func() { _ = fAfter.Close() }()

	siAfter, err := fAfter.GetStreamInfo()
	if err != nil {
		t.Fatalf("get stream info after: %v", err)
	}

	assertEqual(t, "SampleRate", siBefore.SampleRate, siAfter.SampleRate)
	assertEqual(t, "ChannelCount", siBefore.ChannelCount, siAfter.ChannelCount)
	assertEqual(t, "BitDepth", siBefore.BitDepth, siAfter.BitDepth)
	assertEqual(t, "BlockSizeMin", siBefore.BlockSizeMin, siAfter.BlockSizeMin)

	// Verify the file has StreamInfo as the first metadata block.
	if fAfter.Meta[0].Type != flac.StreamInfo {
		t.Errorf("first metadata block type: got %d, want %d (StreamInfo)",
			fAfter.Meta[0].Type, flac.StreamInfo)
	}
}

func TestWriteFlacTags_ReplaceComment(t *testing.T) {
	tmpDir := t.TempDir()
	flacPath := filepath.Join(tmpDir, "test.flac")
	makeMinimalFLAC(t, flacPath)

	// Write title once.
	if err := writeFlacTags(testLogger(), flacPath, TagChanges{FieldTitle: "First"}); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Write title again.
	if err := writeFlacTags(testLogger(), flacPath, TagChanges{FieldTitle: "Second"}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	// Parse the FLAC and check no duplicate TITLE entries.
	f, err := flac.ParseFile(flacPath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	defer func() { _ = f.Close() }()

	for _, meta := range f.Meta {
		if meta.Type != flac.VorbisComment {
			continue
		}

		cmt, parseErr := parseVorbisComment(*meta)
		if parseErr != nil {
			t.Fatalf("parse vorbis comments: %v", parseErr)
		}

		titles := cmt.get("TITLE")
		if len(titles) != 1 {
			t.Errorf("expected 1 TITLE entry, got %d: %v", len(titles), titles)
		}

		if len(titles) > 0 && titles[0] != "Second" {
			t.Errorf("TITLE value: got %q, want %q", titles[0], "Second")
		}

		break
	}

	// Also verify via metadata.ExtractTags round-trip.
	tags, err := metadata.ExtractTags(flacPath)
	if err != nil {
		t.Fatalf("ExtractTags: %v", err)
	}

	assertEqual(t, "Title", "Second", tags.Title)
}

func TestWriteFlacTags_AtomicSafety(t *testing.T) {
	tmpDir := t.TempDir()

	// Verify that writing to a corrupt (non-FLAC) file fails and
	// leaves the file untouched.
	corruptPath := filepath.Join(tmpDir, "corrupt.flac")
	if err := os.WriteFile(corruptPath, []byte("not a flac file"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	err := writeFlacTags(testLogger(), corruptPath, TagChanges{FieldTitle: "Fail"})
	if err == nil {
		t.Fatal("expected error writing to corrupt file, got nil")
	}

	// The corrupt file should remain untouched.
	content, readErr := os.ReadFile(corruptPath)
	if readErr != nil {
		t.Fatalf("read corrupt file: %v", readErr)
	}

	if string(content) != "not a flac file" {
		t.Errorf("corrupt file was modified: got %q", string(content))
	}

	// Verify that a valid FLAC file stays intact when the write fails
	// due to invalid cover art data.
	safePath := filepath.Join(tmpDir, "safe.flac")
	makeMinimalFLAC(t, safePath)

	safeBefore, readErr := os.ReadFile(safePath)
	if readErr != nil {
		t.Fatalf("read safe before: %v", readErr)
	}

	// Provide random bytes that aren't valid JPEG or PNG — this will
	// cause flacpicture.NewFromImageData to fail, which happens before
	// AtomicWrite so the original file stays intact.
	randomData := make([]byte, 32)
	_, _ = rand.Read(randomData)

	err = writeFlacTags(testLogger(), safePath, TagChanges{FieldCoverArt: randomData})
	if err == nil {
		t.Fatal("expected error with invalid image data, got nil")
	}

	safeAfter, readErr := os.ReadFile(safePath)
	if readErr != nil {
		t.Fatalf("read safe after: %v", readErr)
	}

	if !bytes.Equal(safeBefore, safeAfter) {
		t.Error("safe file was modified despite failed write")
	}
}

// parseVorbisComment is a test helper to parse Vorbis Comments from a raw
// metadata block using a minimal inline parser.
func parseVorbisComment(meta flac.MetaDataBlock) (*vorbisCommentHelper, error) {
	result := &vorbisCommentHelper{comments: map[string][]string{}}
	data := meta.Data

	if len(data) < 4 { //nolint:mnd
		return nil, errVorbisCommentTooShort
	}

	// Vendor string length (little-endian 32-bit).
	vendorLen := int(binary.LittleEndian.Uint32(data[0:4]))
	offset := 4 + vendorLen

	if offset+4 > len(data) {
		return nil, fmt.Errorf("%w: after vendor string", errVorbisCommentTruncated)
	}

	// Comment count.
	count := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
	offset += 4

	for range count {
		if offset+4 > len(data) {
			return nil, fmt.Errorf("%w: comment header", errVorbisCommentTruncated)
		}

		cmtLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4

		if offset+cmtLen > len(data) {
			return nil, fmt.Errorf("%w: comment entry", errVorbisCommentTruncated)
		}

		entry := string(data[offset : offset+cmtLen])
		offset += cmtLen

		key, val, found := strings.Cut(entry, "=")
		if !found {
			continue
		}

		result.comments[key] = append(result.comments[key], val)
	}

	return result, nil
}

type vorbisCommentHelper struct {
	comments map[string][]string
}

func (v *vorbisCommentHelper) get(key string) []string {
	return v.comments[key]
}

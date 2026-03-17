package tagwriter

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	id3v2 "github.com/bogem/id3v2/v2"

	"yellowjacket/backend/metadata"
)

// createTestMP3 creates a minimal MP3 file with an ID3v2 tag followed by
// a single silent MP3 frame. The tag is populated with the supplied fields
// so tests can verify round-trip behaviour.
func createTestMP3(t *testing.T, dir string, name string, fields TagChanges) string {
	t.Helper()

	path := filepath.Join(dir, name)

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test mp3: %v", err)
	}

	tag := id3v2.NewEmptyTag()
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)

	// Apply seed fields using the same logic as the writer.
	applyTextChanges(tag, fields)
	applyCoverArtChanges(tag, fields)

	if _, wErr := tag.WriteTo(f); wErr != nil {
		_ = f.Close()

		t.Fatalf("write id3v2 tag: %v", wErr)
	}

	// Write a minimal valid MPEG audio frame (Layer III, 128 kbps, 44100 Hz,
	// mono, no padding). The frame header is 4 bytes; the frame body is
	// 417 - 4 = 413 zero bytes (silence). A real decoder would produce
	// silence for these bytes.
	frameHeader := []byte{0xFF, 0xFB, 0x90, 0x00}

	if _, wErr := f.Write(frameHeader); wErr != nil {
		_ = f.Close()

		t.Fatalf("write mp3 frame header: %v", wErr)
	}

	silence := make([]byte, 413)

	if _, wErr := f.Write(silence); wErr != nil {
		_ = f.Close()

		t.Fatalf("write mp3 frame body: %v", wErr)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close test mp3: %v", err)
	}

	return path
}

func TestWriteMp3Tags_TextFields(t *testing.T) {
	dir := t.TempDir()

	// Create a bare fixture with no initial metadata.
	path := createTestMP3(t, dir, "text.mp3", nil)

	// Write all text fields.
	changes := TagChanges{
		FieldTitle:       "Test Title",
		FieldArtist:      "Test Artist",
		FieldAlbum:       "Test Album",
		FieldGenre:       "Rock",
		FieldYear:        2024,
		FieldTrackNumber: 3,
		FieldDiscNumber:  1,
		FieldComposer:    "Test Composer",
	}

	if err := writeMp3Tags(testLogger(), path, changes); err != nil {
		t.Fatalf("writeMp3Tags: %v", err)
	}

	// Read back with the existing metadata package.
	meta, err := metadata.ExtractTags(path)
	if err != nil {
		t.Fatalf("ExtractTags: %v", err)
	}

	assertStrField(t, "Title", meta.Title, "Test Title")
	assertStrField(t, "Artist", meta.Artist, "Test Artist")
	assertStrField(t, "Album", meta.Album, "Test Album")
	assertStrField(t, "Genre", meta.Genre, "Rock")
	assertIntField(t, "Year", meta.Year, 2024)
	assertIntField(t, "TrackNumber", meta.TrackNumber, 3)
	assertIntField(t, "DiscNumber", meta.DiscNumber, 1)
	assertStrField(t, "Composer", meta.Composer, "Test Composer")
}

func TestWriteMp3Tags_CoverArt(t *testing.T) {
	dir := t.TempDir()
	path := createTestMP3(t, dir, "cover.mp3", nil)

	art := makeMinimalJPEG(t)
	changes := TagChanges{
		FieldCoverArt: art,
	}

	if err := writeMp3Tags(testLogger(), path, changes); err != nil {
		t.Fatalf("writeMp3Tags: %v", err)
	}

	meta, err := metadata.ExtractTags(path)
	if err != nil {
		t.Fatalf("ExtractTags: %v", err)
	}

	if meta.Picture == nil {
		t.Fatal("expected picture data, got nil")
	}

	if !bytes.Equal(meta.Picture.Data, art) {
		t.Errorf("picture data mismatch: got %d bytes, want %d", len(meta.Picture.Data), len(art))
	}

	if meta.Picture.MIMEType != "image/jpeg" {
		t.Errorf("MIME type: got %q, want %q", meta.Picture.MIMEType, "image/jpeg")
	}
}

func TestWriteMp3Tags_ClearCoverArt(t *testing.T) {
	dir := t.TempDir()

	// Start with cover art embedded.
	art := makeMinimalJPEG(t)
	path := createTestMP3(t, dir, "clear.mp3", TagChanges{
		FieldCoverArt: art,
	})

	// Verify art is present before clearing.
	meta, err := metadata.ExtractTags(path)
	if err != nil {
		t.Fatalf("ExtractTags before clear: %v", err)
	}

	if meta.Picture == nil {
		t.Fatal("expected picture before clear, got nil")
	}

	// Clear by setting cover_art to nil.
	if err := writeMp3Tags(testLogger(), path, TagChanges{
		FieldCoverArt: nil,
	}); err != nil {
		t.Fatalf("writeMp3Tags (clear): %v", err)
	}

	meta, err = metadata.ExtractTags(path)
	if err != nil {
		t.Fatalf("ExtractTags after clear: %v", err)
	}

	if meta.Picture != nil {
		t.Errorf("expected no picture after clear, got %d bytes", len(meta.Picture.Data))
	}
}

func TestWriteMp3Tags_PartialUpdate(t *testing.T) {
	dir := t.TempDir()

	// Create fixture with all fields populated.
	initial := TagChanges{
		FieldTitle:       "Original Title",
		FieldArtist:      "Original Artist",
		FieldAlbum:       "Original Album",
		FieldGenre:       "Jazz",
		FieldYear:        2020,
		FieldTrackNumber: 5,
		FieldDiscNumber:  2,
		FieldComposer:    "Original Composer",
	}
	path := createTestMP3(t, dir, "partial.mp3", initial)

	// Update only title and artist.
	if err := writeMp3Tags(testLogger(), path, TagChanges{
		FieldTitle:  "New Title",
		FieldArtist: "New Artist",
	}); err != nil {
		t.Fatalf("writeMp3Tags: %v", err)
	}

	meta, err := metadata.ExtractTags(path)
	if err != nil {
		t.Fatalf("ExtractTags: %v", err)
	}

	// Changed fields.
	assertStrField(t, "Title", meta.Title, "New Title")
	assertStrField(t, "Artist", meta.Artist, "New Artist")

	// Unchanged fields.
	assertStrField(t, "Album", meta.Album, "Original Album")
	assertStrField(t, "Genre", meta.Genre, "Jazz")
	assertIntField(t, "Year", meta.Year, 2020)
	assertIntField(t, "TrackNumber", meta.TrackNumber, 5)
	assertIntField(t, "DiscNumber", meta.DiscNumber, 2)
	assertStrField(t, "Composer", meta.Composer, "Original Composer")
}

func TestWriteMp3Tags_AtomicSafety(t *testing.T) {
	dir := t.TempDir()
	path := createTestMP3(t, dir, "atomic.mp3", TagChanges{
		FieldTitle: "Before",
	})

	// Read the original file content so we can verify it is unchanged
	// after a failed write.
	originalData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}

	// Attempt to write to a non-existent directory to force AtomicWrite
	// to fail (the temp file creation will fail).
	badPath := filepath.Join(dir, "nonexistent", "subdir", "file.mp3")
	writeErr := writeMp3Tags(testLogger(), badPath, TagChanges{
		FieldTitle: "After",
	})

	if writeErr == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}

	// Verify the original file is completely unchanged.
	afterData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after failed write: %v", err)
	}

	if !bytes.Equal(originalData, afterData) {
		t.Error("original file was modified despite write failure")
	}
}

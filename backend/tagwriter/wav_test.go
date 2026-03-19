package tagwriter

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	id3v2 "github.com/bogem/id3v2/v2"

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

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestWriteWavTags_TextFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := createTestWAV(t, dir, "text.wav", nil)

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

	if err := writeWavTags(testLogger(), path, changes); err != nil {
		t.Fatalf("writeWavTags: %v", err)
	}

	meta := readWavID3Tags(t, path)

	assertStrField(t, "Title", meta.Title, "Test Title")
	assertStrField(t, "Artist", meta.Artist, "Test Artist")
	assertStrField(t, "Album", meta.Album, "Test Album")
	assertStrField(t, "AlbumArtist", meta.AlbumArtist, "Test Album Artist")
	assertStrField(t, "Genre", meta.Genre, "Rock")
	assertIntField(t, "Year", meta.Year, 2024)
	assertIntField(t, "TrackNumber", meta.TrackNumber, 3)
	assertIntField(t, "DiscNumber", meta.DiscNumber, 1)
	assertStrField(t, "Composer", meta.Composer, "Test Composer")
}

func TestWriteWavTags_CoverArt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := createTestWAV(t, dir, "cover.wav", nil)

	art := tinyJPEG(t)
	changes := TagChanges{
		FieldCoverArt: art,
	}

	if err := writeWavTags(testLogger(), path, changes); err != nil {
		t.Fatalf("writeWavTags: %v", err)
	}

	meta := readWavID3Tags(t, path)

	if meta.Picture == nil {
		t.Fatal("expected picture data, got nil")
	}

	if !bytes.Equal(meta.Picture.Data, art) {
		t.Errorf("picture data mismatch: got %d bytes, want %d",
			len(meta.Picture.Data), len(art))
	}

	assertStrField(t, "MIME", meta.Picture.MIMEType, "image/jpeg")
}

func TestWriteWavTags_ClearCoverArt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	art := tinyJPEG(t)

	// Create WAV with cover art embedded.
	path := createTestWAV(t, dir, "clear.wav", TagChanges{
		FieldCoverArt: art,
	})

	// Verify art is present before clearing.
	meta := readWavID3Tags(t, path)
	if meta.Picture == nil {
		t.Fatal("expected picture before clear, got nil")
	}

	// Clear cover art by setting to nil.
	err := writeWavTags(testLogger(), path, TagChanges{
		FieldCoverArt: nil,
	})
	if err != nil {
		t.Fatalf("writeWavTags (clear): %v", err)
	}

	meta = readWavID3Tags(t, path)
	if meta.Picture != nil {
		t.Errorf("expected no picture after clear, got %d bytes",
			len(meta.Picture.Data))
	}
}

func TestWriteWavTags_PartialUpdate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	initial := TagChanges{
		FieldTitle:       "Original Title",
		FieldArtist:      "Original Artist",
		FieldAlbum:       "Original Album",
		FieldAlbumArtist: "Original AA",
		FieldGenre:       "Jazz",
		FieldYear:        2020,
		FieldTrackNumber: 5,
		FieldDiscNumber:  2,
		FieldComposer:    "Original Composer",
	}

	path := createTestWAV(t, dir, "partial.wav", initial)

	// Update only title and artist.
	err := writeWavTags(testLogger(), path, TagChanges{
		FieldTitle:  "New Title",
		FieldArtist: "New Artist",
	})
	if err != nil {
		t.Fatalf("writeWavTags: %v", err)
	}

	meta := readWavID3Tags(t, path)

	// Changed fields.
	assertStrField(t, "Title", meta.Title, "New Title")
	assertStrField(t, "Artist", meta.Artist, "New Artist")

	// Unchanged fields.
	assertStrField(t, "Album", meta.Album, "Original Album")
	assertStrField(t, "AlbumArtist", meta.AlbumArtist, "Original AA")
	assertStrField(t, "Genre", meta.Genre, "Jazz")
	assertIntField(t, "Year", meta.Year, 2020)
	assertIntField(t, "TrackNumber", meta.TrackNumber, 5)
	assertIntField(t, "DiscNumber", meta.DiscNumber, 2)
	assertStrField(t, "Composer", meta.Composer, "Original Composer")
}

func TestWriteWavTags_ChunkPreservation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := createTestWAVWithExtraChunks(t, dir, "chunks.wav")

	// Parse original RIFF to capture chunk data before writing.
	origFile, err := os.Open(path)
	if err != nil {
		t.Fatalf("open original: %v", err)
	}

	origChunks, err := parseRIFF(origFile)
	_ = origFile.Close()

	if err != nil {
		t.Fatalf("parse original RIFF: %v", err)
	}

	// Record original chunk data by ID string.
	origData := map[string][]byte{}
	for _, c := range origChunks {
		origData[string(c.id[:])] = c.data
	}

	// Write a tag to trigger RIFF rewrite.
	err = writeWavTags(testLogger(), path, TagChanges{
		FieldTitle: "Chunk Test",
	})
	if err != nil {
		t.Fatalf("writeWavTags: %v", err)
	}

	// Re-parse and verify all non-ID3 chunks are preserved.
	newFile, err := os.Open(path)
	if err != nil {
		t.Fatalf("open after write: %v", err)
	}

	newChunks, err := parseRIFF(newFile)
	_ = newFile.Close()

	if err != nil {
		t.Fatalf("parse new RIFF: %v", err)
	}

	// Count non-id3 chunks in both.
	origNonID3 := 0
	for _, c := range origChunks {
		if !isID3ChunkID(c.id) {
			origNonID3++
		}
	}

	newNonID3 := 0
	for _, c := range newChunks {
		if !isID3ChunkID(c.id) {
			newNonID3++
		}
	}

	if origNonID3 != newNonID3 {
		t.Errorf("non-ID3 chunk count: got %d, want %d",
			newNonID3, origNonID3)
	}

	// Verify fmt chunk is byte-identical.
	checkChunkPreserved(t, newChunks, "fmt ", origData["fmt "])

	// Verify data chunk is byte-identical (WAV-03: audio preserved).
	checkChunkPreserved(t, newChunks, "data", origData["data"])

	// Verify LIST chunk is preserved.
	checkChunkPreserved(t, newChunks, "LIST", origData["LIST"])

	// Verify bext chunk is preserved.
	checkChunkPreserved(t, newChunks, "bext", origData["bext"])

	// Verify id3 chunk now exists with the written title.
	meta := readWavID3Tags(t, path)
	assertStrField(t, "Title", meta.Title, "Chunk Test")
}

// checkChunkPreserved asserts that a chunk with the given ID exists
// in chunks and its data matches want byte-for-byte.
func checkChunkPreserved(
	t *testing.T,
	chunks []riffChunk,
	idStr string,
	want []byte,
) {
	t.Helper()

	for _, c := range chunks {
		if string(c.id[:]) == idStr {
			if !bytes.Equal(c.data, want) {
				t.Errorf("chunk %q data changed: got %d bytes, want %d",
					idStr, len(c.data), len(want))
			}

			return
		}
	}

	t.Errorf("chunk %q not found after write", idStr)
}

func TestWriteWavTags_AtomicSafety(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := createTestWAV(t, dir, "atomic.wav", TagChanges{
		FieldTitle: "Before",
	})

	// Read original file content.
	originalData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}

	// Attempt to write to a non-existent directory to force
	// AtomicWrite to fail on temp file creation.
	badPath := filepath.Join(
		dir, "nonexistent", "subdir", "file.wav",
	)

	writeErr := writeWavTags(testLogger(), badPath, TagChanges{
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

func TestWriteWavTags_RejectsRF64(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Build a minimal RF64 file header.
	var buf bytes.Buffer

	buf.WriteString("RF64")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0xFFFFFFFF))
	buf.WriteString("WAVE")

	// Minimal ds64 chunk (required for RF64 but we just need
	// enough bytes for parseRIFF to hit the RF64 rejection).
	buf.WriteString("ds64")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(28)) //nolint:mnd
	buf.Write(make([]byte, 28))                             //nolint:mnd

	path := filepath.Join(dir, "rf64.wav")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write RF64 file: %v", err)
	}

	err := writeWavTags(testLogger(), path, TagChanges{
		FieldTitle: "Should Fail",
	})

	if err == nil {
		t.Fatal("expected error for RF64 file, got nil")
	}

	if !strings.Contains(err.Error(), "RF64") {
		t.Errorf("error should mention RF64, got: %v", err)
	}
}

// readWavID3Tags extracts ID3v2 metadata from a WAV file by parsing
// the RIFF structure and reading the id3 chunk with bogem/id3v2.
// dhowden/tag's ReadFrom does not support WAV files, and its
// ReadID3v2Tags fails on empty tags (after clearing all frames).
// Using bogem/id3v2.ParseReader handles all cases correctly.
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

	parsed, err := id3v2.ParseReader(
		bytes.NewReader(id3Data),
		id3v2.Options{Parse: true},
	)
	if err != nil {
		t.Fatalf("ParseReader: %v", err)
	}

	meta := &metadata.TrackMetadata{
		Title:  parsed.Title(),
		Artist: parsed.Artist(),
		Album:  parsed.Album(),
		Genre:  parsed.Genre(),
		Year:   atoiSafe(parsed.Year()),
	}

	// Album artist (TPE2).
	if frames := parsed.GetFrames(
		parsed.CommonID("Band/Orchestra/Accompaniment"),
	); len(frames) > 0 {
		if tf, ok := frames[0].(id3v2.TextFrame); ok {
			meta.AlbumArtist = tf.Text
		}
	}

	// Composer (TCOM).
	if frames := parsed.GetFrames("TCOM"); len(frames) > 0 {
		if tf, ok := frames[0].(id3v2.TextFrame); ok {
			meta.Composer = tf.Text
		}
	}

	// Track number (TRCK).
	trckID := parsed.CommonID("Track number/Position in set")
	if frames := parsed.GetFrames(trckID); len(frames) > 0 {
		if tf, ok := frames[0].(id3v2.TextFrame); ok {
			meta.TrackNumber = atoiSafe(tf.Text)
		}
	}

	// Disc number (TPOS).
	tposID := parsed.CommonID("Part of a set")
	if frames := parsed.GetFrames(tposID); len(frames) > 0 {
		if tf, ok := frames[0].(id3v2.TextFrame); ok {
			meta.DiscNumber = atoiSafe(tf.Text)
		}
	}

	// Cover art (APIC).
	apicID := parsed.CommonID("Attached picture")
	if frames := parsed.GetFrames(apicID); len(frames) > 0 {
		if pf, ok := frames[0].(id3v2.PictureFrame); ok {
			meta.Picture = &metadata.PictureData{
				Data:     pf.Picture,
				MIMEType: pf.MimeType,
			}
		}
	}

	return meta
}

// atoiSafe converts a string to int, returning 0 on failure.
func atoiSafe(s string) int {
	n, _ := strconv.Atoi(s)

	return n
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

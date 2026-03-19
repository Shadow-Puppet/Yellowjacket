package tagwriter

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"yellowjacket/backend/metadata"
)

// createTestOGG writes a minimal valid OGG Vorbis file to path.
// The file contains:
//   - Page 0 (bos): Vorbis identification header packet.
//   - Page 1: Vorbis comment header packet (empty) + setup header packet (synthetic).
//   - Page 2 (eos): Synthetic audio data page.
//
// The file is valid enough for writeOggTags and metadata.ExtractTags.
func createTestOGG(t *testing.T, path string) {
	t.Helper()

	const serialNo uint32 = 0x12345678

	// --- Page 0: Identification header (bos) ---
	identPacket := buildVorbisIdentPacket()
	identSegs := splitPacketIntoSegments(identPacket)
	identPage := oggPage{
		headerType:   0x02, // bos
		granulePos:   0,
		serialNo:     serialNo,
		seqNo:        0,
		segmentTable: identSegs,
		data:         identPacket,
	}

	// --- Page 1: Comment + Setup headers ---
	commentPacket := buildEmptyVorbisCommentPacket()
	setupPacket := buildSyntheticSetupPacket()

	headerPages := buildHeaderPages(commentPacket, setupPacket, serialNo)

	// Assign sequence numbers starting from 1.
	for i := range headerPages {
		headerPages[i].seqNo = uint32(i + 1)
	}

	// --- Page 2+N: Audio page (eos) ---
	audioData := make([]byte, 64) // Synthetic audio (never decoded).
	for i := range audioData {
		audioData[i] = byte(i)
	}

	audioSegs := splitPacketIntoSegments(audioData)
	audioPage := oggPage{
		headerType:   0x04, // eos
		granulePos:   4096,
		serialNo:     serialNo,
		seqNo:        uint32(len(headerPages) + 1),
		segmentTable: audioSegs,
		data:         audioData,
	}

	// Write all pages.
	var buf bytes.Buffer

	if err := writeOggPage(&buf, identPage); err != nil {
		t.Fatalf("write ident page: %v", err)
	}

	for _, hp := range headerPages {
		if err := writeOggPage(&buf, hp); err != nil {
			t.Fatalf("write header page: %v", err)
		}
	}

	if err := writeOggPage(&buf, audioPage); err != nil {
		t.Fatalf("write audio page: %v", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write test OGG: %v", err)
	}
}

// buildVorbisIdentPacket builds a minimal Vorbis identification header packet.
// Layout: type(1) + "vorbis"(6) + version(4) + channels(1) + sample_rate(4)
//
//   - bitrate_max(4) + bitrate_nom(4) + bitrate_min(4) + blocksize(1) + framing(1)
//
// = 30 bytes total.
func buildVorbisIdentPacket() []byte {
	buf := make([]byte, 30)

	buf[0] = 0x01 // packet type: identification
	copy(buf[1:7], "vorbis")

	// Version 0.
	binary.LittleEndian.PutUint32(buf[7:11], 0)

	// 1 channel.
	buf[11] = 1

	// Sample rate 44100.
	binary.LittleEndian.PutUint32(buf[12:16], 44100)

	// Bitrate max/nominal/min = 0 (unset).
	binary.LittleEndian.PutUint32(buf[16:20], 0)
	binary.LittleEndian.PutUint32(buf[20:24], 0)
	binary.LittleEndian.PutUint32(buf[24:28], 0)

	// Blocksize: upper nibble = blocksize1 (log2), lower = blocksize0 (log2).
	// Using 8 and 6 → 0x86 (256 and 64 samples).
	buf[28] = 0x86

	// Framing bit.
	buf[29] = 0x01

	return buf
}

// buildEmptyVorbisCommentPacket builds a Vorbis Comment packet with
// an empty vendor string and zero comments.
func buildEmptyVorbisCommentPacket() []byte {
	vendor := []byte("YellowJacket test")
	vc := &oggVorbisComment{
		vendor:  vendor,
		entries: nil,
	}

	return serializeVorbisCommentPacket(vc)
}

// buildSyntheticSetupPacket builds a synthetic Vorbis setup header packet.
// It has the correct 7-byte magic (\x05vorbis) followed by synthetic data.
// This data is never decoded by the audio codec — only the header magic
// is validated during tag writing.
func buildSyntheticSetupPacket() []byte {
	// Vorbis setup header magic + padding.
	const setupSize = 64

	buf := make([]byte, setupSize)
	buf[0] = 0x05 // packet type: setup
	copy(buf[1:7], "vorbis")

	// Fill the rest with deterministic data.
	for i := 7; i < setupSize; i++ {
		buf[i] = byte(i * 3)
	}

	return buf
}

// ---------------------------------------------------------------------------
// CRC32 and fixture validation tests
// ---------------------------------------------------------------------------

func TestOggCRC_KnownVectors(t *testing.T) {
	t.Parallel()

	// Vector 1: Empty input → CRC = 0.
	got := oggCRC(nil)
	if got != 0 {
		t.Errorf("empty input: got 0x%08x, want 0x00000000", got)
	}

	// Vector 2: "OggS" → known value computed by libogg reference.
	gotOggS := oggCRC([]byte("OggS"))
	if gotOggS != 0x5fb0a94f {
		t.Errorf("OggS: got 0x%08x, want 0x5fb0a94f", gotOggS)
	}

	// Vector 3: Bytes {1..8} → known value.
	gotSeq := oggCRC([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	if gotSeq != 0x7d0f3681 {
		t.Errorf("{1..8}: got 0x%08x, want 0x7d0f3681", gotSeq)
	}

	// Vector 4: Self-consistency — compute CRC of test fixture pages.
	dir := t.TempDir()
	path := filepath.Join(dir, "crc_test.ogg")
	createTestOGG(t, path)

	pages, err := parseOggPages(testLogger(), path)
	if err != nil {
		t.Fatalf("parseOggPages: %v", err)
	}

	for i, page := range pages {
		// Serialize page, zero CRC field, recompute.
		raw := pageBytes(page)
		if len(raw) < 26 {
			t.Fatalf("page %d: too short (%d bytes)", i, len(raw))
		}

		// Extract stored CRC.
		storedCRC := binary.LittleEndian.Uint32(raw[22:26])

		// Zero CRC field and recompute.
		raw[22] = 0
		raw[23] = 0
		raw[24] = 0
		raw[25] = 0

		computedCRC := oggCRC(raw)
		if computedCRC != storedCRC {
			t.Errorf("page %d: CRC mismatch: stored=0x%08x computed=0x%08x",
				i, storedCRC, computedCRC)
		}
	}
}

func TestCreateTestOGG_Valid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "valid.ogg")
	createTestOGG(t, path)

	pages, err := parseOggPages(testLogger(), path)
	if err != nil {
		t.Fatalf("parseOggPages: %v", err)
	}

	if len(pages) < 3 {
		t.Fatalf("expected at least 3 pages, got %d", len(pages))
	}

	// All pages should have the same serial number.
	serialNo := pages[0].serialNo
	for i, p := range pages {
		if p.serialNo != serialNo {
			t.Errorf("page %d: serial 0x%08x, want 0x%08x", i, p.serialNo, serialNo)
		}
	}

	// First page should be bos.
	if pages[0].headerType&0x02 == 0 {
		t.Error("first page is not bos")
	}

	// First page should have Vorbis identification header.
	data := pages[0].data
	if len(data) < 7 || data[0] != 0x01 || string(data[1:7]) != "vorbis" {
		t.Error("first page does not contain Vorbis identification header")
	}

	// Verify metadata.ExtractTags can read the fixture (even with no tags).
	_, err = metadata.ExtractTags(path)
	if err != nil {
		t.Fatalf("metadata.ExtractTags on empty fixture: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Round-trip tests for OGG requirements (OGG-01 through OGG-06)
// ---------------------------------------------------------------------------

func TestWriteOggTags_TextFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "text.ogg")
	createTestOGG(t, path)

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

	if err := writeOggTags(testLogger(), path, changes); err != nil {
		t.Fatalf("writeOggTags: %v", err)
	}

	// Read back with metadata.ExtractTags (dhowden/tag).
	tags, err := metadata.ExtractTags(path)
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

func TestWriteOggTags_CoverArt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "cover.ogg")
	createTestOGG(t, path)

	jpegData := tinyJPEG(t)

	changes := TagChanges{
		FieldCoverArt: jpegData,
	}

	if err := writeOggTags(testLogger(), path, changes); err != nil {
		t.Fatalf("writeOggTags: %v", err)
	}

	tags, err := metadata.ExtractTags(path)
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

func TestWriteOggTags_ClearCoverArt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "clear_art.ogg")
	createTestOGG(t, path)

	// First add cover art.
	jpegData := tinyJPEG(t)

	err := writeOggTags(testLogger(), path, TagChanges{FieldCoverArt: jpegData})
	if err != nil {
		t.Fatalf("add cover art: %v", err)
	}

	// Verify art was added.
	tags, err := metadata.ExtractTags(path)
	if err != nil {
		t.Fatalf("ExtractTags after add: %v", err)
	}

	if tags.Picture == nil {
		t.Fatal("expected picture after add, got nil")
	}

	// Now clear cover art (nil value).
	if err := writeOggTags(testLogger(), path, TagChanges{FieldCoverArt: nil}); err != nil {
		t.Fatalf("clear cover art: %v", err)
	}

	tags, err = metadata.ExtractTags(path)
	if err != nil {
		t.Fatalf("ExtractTags after clear: %v", err)
	}

	if tags.Picture != nil {
		t.Errorf("expected no picture after clear, got %d bytes", len(tags.Picture.Data))
	}
}

func TestWriteOggTags_PartialUpdate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "partial.ogg")
	createTestOGG(t, path)

	// Write all fields first.
	allFields := TagChanges{
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

	if err := writeOggTags(testLogger(), path, allFields); err != nil {
		t.Fatalf("write all fields: %v", err)
	}

	// Update only title and genre.
	partial := TagChanges{
		FieldTitle: "Updated Title",
		FieldGenre: "Blues",
	}

	if err := writeOggTags(testLogger(), path, partial); err != nil {
		t.Fatalf("partial update: %v", err)
	}

	tags, err := metadata.ExtractTags(path)
	if err != nil {
		t.Fatalf("ExtractTags: %v", err)
	}

	// Changed fields should have new values.
	assertEqual(t, "Title", "Updated Title", tags.Title)
	assertEqual(t, "Genre", "Blues", tags.Genre)

	// Unchanged fields should be preserved.
	assertEqual(t, "Artist", "Original Artist", tags.Artist)
	assertEqual(t, "Album", "Original Album", tags.Album)
	assertEqual(t, "AlbumArtist", "Original AA", tags.AlbumArtist)
	assertEqual(t, "Year", 2020, tags.Year)
	assertEqual(t, "TrackNumber", 5, tags.TrackNumber)
	assertEqual(t, "DiscNumber", 2, tags.DiscNumber)
	assertEqual(t, "Composer", "Original Composer", tags.Composer)
}

func TestWriteOggTags_AudioPreservation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "audio.ogg")
	createTestOGG(t, path)

	// Capture original audio page data.
	pagesBefore, err := parseOggPages(testLogger(), path)
	if err != nil {
		t.Fatalf("parseOggPages before: %v", err)
	}

	audioStartBefore := findAudioPageStart(pagesBefore)

	var originalAudioData []byte
	for _, p := range pagesBefore[audioStartBefore:] {
		originalAudioData = append(originalAudioData, p.data...)
	}

	if len(originalAudioData) == 0 {
		t.Fatal("no audio data found in fixture")
	}

	// Write tags.
	changes := TagChanges{
		FieldTitle:  "Audio Test",
		FieldArtist: "Audio Artist",
	}

	if err := writeOggTags(testLogger(), path, changes); err != nil {
		t.Fatalf("writeOggTags: %v", err)
	}

	// Parse again and compare audio data.
	pagesAfter, err := parseOggPages(testLogger(), path)
	if err != nil {
		t.Fatalf("parseOggPages after: %v", err)
	}

	audioStartAfter := findAudioPageStart(pagesAfter)

	var newAudioData []byte
	for _, p := range pagesAfter[audioStartAfter:] {
		newAudioData = append(newAudioData, p.data...)
	}

	if !bytes.Equal(originalAudioData, newAudioData) {
		t.Errorf("audio data changed after tag write: before=%d bytes, after=%d bytes",
			len(originalAudioData), len(newAudioData))
	}
}

func TestWriteOggTags_AtomicSafety(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Test 1: Corrupt (non-OGG) file — should fail and leave file untouched.
	corruptPath := filepath.Join(dir, "corrupt.ogg")
	corruptContent := []byte("not an ogg file at all")

	if err := os.WriteFile(corruptPath, corruptContent, 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	err := writeOggTags(testLogger(), corruptPath, TagChanges{FieldTitle: "Fail"})
	if err == nil {
		t.Fatal("expected error writing to corrupt file, got nil")
	}

	// Verify corrupt file is untouched.
	content, readErr := os.ReadFile(corruptPath)
	if readErr != nil {
		t.Fatalf("read corrupt: %v", readErr)
	}

	if !bytes.Equal(content, corruptContent) {
		t.Error("corrupt file was modified despite write failure")
	}

	// Test 2: Valid OGG → write to non-existent directory path should fail.
	validPath := filepath.Join(dir, "valid.ogg")
	createTestOGG(t, validPath)

	originalData, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatalf("read valid before: %v", err)
	}

	badPath := filepath.Join(dir, "nonexistent", "subdir", "file.ogg")

	writeErr := writeOggTags(testLogger(), badPath, TagChanges{FieldTitle: "Fail"})
	if writeErr == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}

	// Verify the original valid file is unchanged.
	afterData, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatalf("read valid after: %v", err)
	}

	if !bytes.Equal(originalData, afterData) {
		t.Error("valid file was modified despite failure elsewhere")
	}
}

func TestWriteOggTags_RejectNonVorbis(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "theora.ogg")

	// Build a valid OGG page structure with a Theora identification header
	// instead of Vorbis.
	const serialNo uint32 = 0xDEADBEEF

	theoraIdent := make([]byte, 30)
	theoraIdent[0] = 0x80 // Theora ID header type
	copy(theoraIdent[1:7], "theora")

	identSegs := splitPacketIntoSegments(theoraIdent)
	identPage := oggPage{
		headerType:   0x02, // bos
		granulePos:   0,
		serialNo:     serialNo,
		seqNo:        0,
		segmentTable: identSegs,
		data:         theoraIdent,
	}

	var buf bytes.Buffer

	if err := writeOggPage(&buf, identPage); err != nil {
		t.Fatalf("write theora page: %v", err)
	}

	// Add an EOS page.
	eosPage := oggPage{
		headerType:   0x04,
		granulePos:   0,
		serialNo:     serialNo,
		seqNo:        1,
		segmentTable: []byte{0},
		data:         nil,
	}

	if err := writeOggPage(&buf, eosPage); err != nil {
		t.Fatalf("write eos page: %v", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write theora OGG: %v", err)
	}

	err := writeOggTags(testLogger(), path, TagChanges{FieldTitle: "Should Fail"})
	if err == nil {
		t.Fatal("expected error for non-Vorbis OGG, got nil")
	}

	if !errorContains(err, "not an OGG Vorbis") {
		t.Errorf("error should mention non-Vorbis, got: %v", err)
	}
}

func TestWriteOggTags_RejectMultiStream(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "multi.ogg")

	// Build an OGG file with pages from two different serial numbers.
	// First page is a valid Vorbis bos page.
	identPacket := buildVorbisIdentPacket()
	identSegs := splitPacketIntoSegments(identPacket)

	const serial1 uint32 = 0x11111111
	const serial2 uint32 = 0x22222222

	page1 := oggPage{
		headerType:   0x02, // bos
		granulePos:   0,
		serialNo:     serial1,
		seqNo:        0,
		segmentTable: identSegs,
		data:         identPacket,
	}

	// Second bos page with a different serial number.
	page2 := oggPage{
		headerType:   0x02, // bos
		granulePos:   0,
		serialNo:     serial2,
		seqNo:        0,
		segmentTable: identSegs,
		data:         identPacket,
	}

	var buf bytes.Buffer

	if err := writeOggPage(&buf, page1); err != nil {
		t.Fatalf("write page1: %v", err)
	}

	if err := writeOggPage(&buf, page2); err != nil {
		t.Fatalf("write page2: %v", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write multi-stream OGG: %v", err)
	}

	err := writeOggTags(testLogger(), path, TagChanges{FieldTitle: "Should Fail"})
	if err == nil {
		t.Fatal("expected error for multi-stream OGG, got nil")
	}

	if !errorContains(err, "multiple streams") {
		t.Errorf("error should mention multiple streams, got: %v", err)
	}
}

// errorContains checks if an error's message contains the given substring.
func errorContains(err error, substr string) bool {
	if err == nil {
		return false
	}

	return bytes.Contains(
		[]byte(err.Error()),
		[]byte(substr),
	)
}

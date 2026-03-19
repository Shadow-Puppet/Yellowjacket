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

package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

// testFlacFiles returns the paths to all .flac files in the
// test_data directory.  It skips the test if none are found.
func testFlacFiles(t *testing.T) []string {
	t.Helper()

	root := filepath.Join("..", "..", "test_data")

	var files []string

	err := filepath.Walk(root, func(
		path string, info os.FileInfo, err error,
	) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && filepath.Ext(path) == ".flac" {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking test_data: %v", err)
	}

	if len(files) == 0 {
		t.Skip("no .flac test fixtures found in test_data/")
	}

	return files
}

// TestGetFlacDuration_BasicParsing verifies that getFlacDuration
// returns a positive duration for every FLAC test fixture.
func TestGetFlacDuration_BasicParsing(t *testing.T) {
	for _, path := range testFlacFiles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}

			defer func() { _ = f.Close() }()

			ms, err := getFlacDuration(f)
			if err != nil {
				t.Fatalf("getFlacDuration: %v", err)
			}

			if ms <= 0 {
				t.Errorf(
					"expected positive duration, got %d",
					ms,
				)
			}

			t.Logf("duration: %dms", ms)
		})
	}
}

// TestGetFlacDuration_MatchesBeepDecode verifies that the fast
// header-only parser produces a duration within 1 second of the full
// decode via beep, for every FLAC test fixture.
func TestGetFlacDuration_MatchesBeepDecode(t *testing.T) {
	for _, path := range testFlacFiles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			refMS, err := GetTrackLengthMillis(path)
			if err != nil {
				t.Fatalf("beep decode failed: %v", err)
			}

			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}

			defer func() { _ = f.Close() }()

			fastMS, err := getFlacDuration(f)
			if err != nil {
				t.Fatalf("getFlacDuration: %v", err)
			}

			diffMS := refMS - fastMS
			if diffMS < 0 {
				diffMS = -diffMS
			}

			const toleranceMS = 1000

			t.Logf(
				"beep=%dms  fast=%dms  diff=%dms",
				refMS, fastMS, diffMS,
			)

			if diffMS > toleranceMS {
				t.Errorf(
					"duration mismatch: beep=%dms "+
						"fast=%dms (diff %dms "+
						"exceeds %dms tolerance)",
					refMS, fastMS, diffMS, toleranceMS,
				)
			}
		})
	}
}

// TestGetFlacDuration_WithPrependedID3v2 creates a temporary FLAC
// file with a synthetic ID3v2 tag prepended and verifies that
// getFlacDuration correctly skips it and parses the duration.
func TestGetFlacDuration_WithPrependedID3v2(t *testing.T) {
	files := testFlacFiles(t)

	// Use the first test fixture as our source.
	src := files[0]

	srcData, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}

	// Build a minimal ID3v2.3 header with 256 bytes of padding.
	//nolint:mnd // synthetic tag construction.
	paddingSize := 256

	id3Header := buildID3v2Header(paddingSize)

	// Write: ID3v2 header + padding + original FLAC data.
	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "test_id3v2.flac")

	out := make([]byte, 0, len(id3Header)+paddingSize+len(srcData))
	out = append(out, id3Header...)
	out = append(out, make([]byte, paddingSize)...)
	out = append(out, srcData...)

	if err := os.WriteFile(tmpPath, out, 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	// Get reference duration from original file.
	origF, err := os.Open(src)
	if err != nil {
		t.Fatalf("open original: %v", err)
	}

	defer func() { _ = origF.Close() }()

	origMS, err := getFlacDuration(origF)
	if err != nil {
		t.Fatalf("getFlacDuration on original: %v", err)
	}

	// Parse the ID3v2-wrapped file.
	tmpF, err := os.Open(tmpPath)
	if err != nil {
		t.Fatalf("open temp: %v", err)
	}

	defer func() { _ = tmpF.Close() }()

	wrappedMS, err := getFlacDuration(tmpF)
	if err != nil {
		t.Fatalf(
			"getFlacDuration on ID3v2-wrapped file: %v", err,
		)
	}

	if origMS != wrappedMS {
		t.Errorf(
			"duration mismatch: original=%dms wrapped=%dms",
			origMS, wrappedMS,
		)
	}

	t.Logf(
		"original=%dms  wrapped=%dms", origMS, wrappedMS,
	)
}

// TestParseFlacStreamInfo verifies the bit-level parsing of sample
// rate and total samples from a known StreamInfo block.
func TestParseFlacStreamInfo(t *testing.T) {
	// Construct a 34-byte StreamInfo with known values.
	// Layout of bytes 10-17 (64 bits, big-endian):
	//   bits  0-19: sample rate     (20 bits)
	//   bits 20-22: channels - 1    (3 bits)
	//   bits 23-27: bps - 1         (5 bits)
	//   bits 28-63: total samples   (36 bits)
	//
	// Test values:
	//   sample rate  = 44100  (0x0AC44)
	//   channels     = 2      (stored as 1, 0b001)
	//   bps          = 16     (stored as 15, 0b01111)
	//   total samples = 11614366 (0x00B1389E)
	//
	// Packed: 0x0AC442F000B1389E
	//   byte 10 = 0x0A   byte 14 = 0x00
	//   byte 11 = 0xC4   byte 15 = 0xB1
	//   byte 12 = 0x42   byte 16 = 0x38
	//   byte 13 = 0xF0   byte 17 = 0x9E
	//
	//nolint:mnd // byte values from manual FLAC spec packing.
	var si [streamInfoLength]byte

	si[10] = 0x0A
	si[11] = 0xC4
	si[12] = 0x42
	si[13] = 0xF0
	si[14] = 0x00
	si[15] = 0xB1
	si[16] = 0x38
	si[17] = 0x9E

	sr, total := parseFlacStreamInfo(si)

	//nolint:mnd // expected test values.
	const (
		wantSR    = 44100
		wantTotal = 11614366
	)

	if sr != wantSR {
		t.Errorf("sample rate: got %d, want %d", sr, wantSR)
	}

	if total != wantTotal {
		t.Errorf(
			"total samples: got %d, want %d",
			total, wantTotal,
		)
	}
}

// buildID3v2Header creates a minimal 10-byte ID3v2.3 header with
// the given payload size encoded as a syncsafe integer.
//
//nolint:mnd // byte offsets from the ID3v2 spec.
func buildID3v2Header(payloadSize int) []byte {
	header := []byte{
		'I', 'D', '3', // signature
		3, 0, // version 2.3.0
		0,          // flags
		0, 0, 0, 0, // size (syncsafe, filled below)
	}

	header[6] = byte((payloadSize >> 21) & 0x7F)
	header[7] = byte((payloadSize >> 14) & 0x7F)
	header[8] = byte((payloadSize >> 7) & 0x7F)
	header[9] = byte(payloadSize & 0x7F)

	return header
}

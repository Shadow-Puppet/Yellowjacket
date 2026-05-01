package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

// testMP3Files returns the paths to all .mp3 files in the curated
// fixture library (`test_data/music_library_test/`).  Scoped
// narrowly so that ad-hoc scramble / autotag fixtures placed
// elsewhere under `test_data/` (e.g. `test_data/mb-tag/`) don't
// get pulled into the assertion and fail on non-curated codecs.
// Skips the test when the directory isn't present.
func testMP3Files(t *testing.T) []string {
	t.Helper()

	root := filepath.Join("..", "..", "test_data", "music_library_test")

	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Skip("test_data/music_library_test not present, skipping")
	}

	var files []string

	err := filepath.Walk(root, func(
		path string, info os.FileInfo, err error,
	) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && filepath.Ext(path) == ".mp3" {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking test_data: %v", err)
	}

	if len(files) == 0 {
		t.Skip("no .mp3 test fixtures found in test_data/")
	}

	return files
}

// TestGetMP3Duration_MatchesBeepDecode verifies that the fast
// header-only parser produces a duration within 1 second of the
// full decode via beep, for every test MP3 file.
func TestGetMP3Duration_MatchesBeepDecode(t *testing.T) {
	for _, path := range testMP3Files(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			// Reference value: full beep decode.
			refMS, err := GetTrackLengthMillis(path)
			if err != nil {
				t.Fatalf(
					"beep decode failed: %v", err,
				)
			}

			// Fast path.
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}

			defer func() { _ = f.Close() }()

			fastMS, _, err := getMP3Duration(f)
			if err != nil {
				t.Fatalf(
					"getMP3Duration failed: %v", err,
				)
			}

			diffMS := refMS - fastMS
			if diffMS < 0 {
				diffMS = -diffMS
			}

			// Allow up to 1 second of difference to account
			// for rounding and the slight inaccuracy of the
			// CBR fallback for VBR-without-Xing files.
			const toleranceMS = 1000

			t.Logf(
				"beep=%dms  fast=%dms  diff=%dms",
				refMS, fastMS, diffMS,
			)

			if diffMS > toleranceMS {
				t.Errorf(
					"duration mismatch: beep=%dms fast=%dms "+
						"(diff %dms exceeds %dms tolerance)",
					refMS, fastMS, diffMS, toleranceMS,
				)
			}
		})
	}
}

// TestGetMP3Duration_BasicParsing exercises the parser on a single
// file and verifies a positive duration is returned.
func TestGetMP3Duration_BasicParsing(t *testing.T) {
	files := testMP3Files(t)

	f, err := os.Open(files[0])
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = f.Close() }()

	ms, _, err := getMP3Duration(f)
	if err != nil {
		t.Fatalf("getMP3Duration: %v", err)
	}

	if ms <= 0 {
		t.Errorf("expected positive duration, got %d", ms)
	}
}

// TestGetMP3Duration_WithMultipleID3v2 creates a temporary MP3 file
// with two consecutive ID3v2 tags prepended and verifies that
// getMP3Duration correctly skips both and finds the audio.
func TestGetMP3Duration_WithMultipleID3v2(t *testing.T) {
	files := testMP3Files(t)
	src := files[0]

	srcData, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}

	// Get reference duration from the original file.
	origF, err := os.Open(src)
	if err != nil {
		t.Fatalf("open original: %v", err)
	}

	defer func() { _ = origF.Close() }()

	origMS, _, err := getMP3Duration(origF)
	if err != nil {
		t.Fatalf("getMP3Duration on original: %v", err)
	}

	// Build a file with two ID3v2 tags: 1 KB + 2 KB of padding.
	//nolint:mnd // synthetic tag construction.
	tag1Size := 1024
	tag2Size := 2048

	tag1 := buildID3v2Header(tag1Size)
	tag2 := buildID3v2Header(tag2Size)

	out := make(
		[]byte,
		0,
		len(tag1)+tag1Size+len(tag2)+tag2Size+len(srcData),
	)
	out = append(out, tag1...)
	out = append(out, make([]byte, tag1Size)...)
	out = append(out, tag2...)
	out = append(out, make([]byte, tag2Size)...)
	out = append(out, srcData...)

	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "multi_id3v2.mp3")

	if err := os.WriteFile(
		tmpPath, out, 0o644,
	); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	tmpF, err := os.Open(tmpPath)
	if err != nil {
		t.Fatalf("open temp: %v", err)
	}

	defer func() { _ = tmpF.Close() }()

	wrappedMS, _, err := getMP3Duration(tmpF)
	if err != nil {
		t.Fatalf(
			"getMP3Duration on multi-ID3v2 file: %v", err,
		)
	}

	diffMS := origMS - wrappedMS
	if diffMS < 0 {
		diffMS = -diffMS
	}

	// The CBR calculation uses file size, so the prepended tags
	// will cause a slight overestimate.  Allow generous tolerance.
	const toleranceMS = 5000

	t.Logf(
		"original=%dms  wrapped=%dms  diff=%dms",
		origMS, wrappedMS, diffMS,
	)

	if diffMS > toleranceMS {
		t.Errorf(
			"duration mismatch: original=%dms "+
				"wrapped=%dms (diff %dms "+
				"exceeds %dms tolerance)",
			origMS, wrappedMS, diffMS, toleranceMS,
		)
	}
}

// TestSkipAdditionalID3v2 verifies that skipAdditionalID3v2 handles
// files with no additional tags, one additional tag, and multiple
// additional tags.
func TestSkipAdditionalID3v2(t *testing.T) {
	// Build a file: [ID3v2(100)] [ID3v2(200)] [ID3v2(50)] [data]
	//nolint:mnd // synthetic tag sizes for test.
	sizes := []int{100, 200, 50}

	var buf []byte

	for _, sz := range sizes {
		buf = append(buf, buildID3v2Header(sz)...)
		buf = append(buf, make([]byte, sz)...)
	}

	buf = append(buf, []byte("audio data here")...)

	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "multi_id3.bin")

	if err := os.WriteFile(
		tmpPath, buf, 0o644,
	); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = f.Close() }()

	// skipID3v2 handles the first tag.
	firstEnd, err := skipID3v2(f)
	if err != nil {
		t.Fatalf("skipID3v2: %v", err)
	}

	//nolint:mnd // expected offset after first tag.
	expectedFirst := int64(10 + 100)
	if firstEnd != expectedFirst {
		t.Fatalf(
			"first tag end: got %d, want %d",
			firstEnd, expectedFirst,
		)
	}

	// skipAdditionalID3v2 handles the remaining tags.
	finalOffset, err := skipAdditionalID3v2(f, firstEnd)
	if err != nil {
		t.Fatalf("skipAdditionalID3v2: %v", err)
	}

	// Expected: 10+100 + 10+200 + 10+50 = 380
	//nolint:mnd // expected offset after all tags.
	expectedAll := int64(10 + 100 + 10 + 200 + 10 + 50)
	if finalOffset != expectedAll {
		t.Errorf(
			"final offset: got %d, want %d",
			finalOffset, expectedAll,
		)
	}
}

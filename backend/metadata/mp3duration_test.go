package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

// testMP3Files returns the paths to all .mp3 files in the test_data
// directory.  It skips the test if none are found.
func testMP3Files(t *testing.T) []string {
	t.Helper()

	root := filepath.Join("..", "..", "test_data")

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

			fastMS, err := getMP3Duration(f)
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

	ms, err := getMP3Duration(f)
	if err != nil {
		t.Fatalf("getMP3Duration: %v", err)
	}

	if ms <= 0 {
		t.Errorf("expected positive duration, got %d", ms)
	}
}

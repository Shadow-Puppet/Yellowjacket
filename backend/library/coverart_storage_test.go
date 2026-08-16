package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yellowjacket/backend/coverart"
	"yellowjacket/backend/database/sql/sqlcgen"
)

// TestScan_StoresOnlyCoverTiers pins the size decision: a scan writes
// the three rendered tiers and nothing else.
//
// The full-resolution image used to be written beside them, and on a
// real 2,057-album library that was 1,134 MB of a 1.4 GB covers
// directory against 110 MB for all three tiers together - with nothing
// rendering it, since the grid caps at 350 px and the largest tier is
// 400. The bytes are still in the audio file if a bigger one is ever
// wanted, which is where these came from.
func TestScan_StoresOnlyCoverTiers(t *testing.T) {
	// Not parallel: YJ_HOME is process-wide, and this test needs the
	// covers directory to itself.
	t.Setenv("YJ_HOME", t.TempDir())

	lib, db := setupTestLibrary(t)

	root, err := filepath.Abs("../../test_data/music_library_test")
	if err != nil {
		t.Fatalf("resolve fixture path: %v", err)
	}

	library, err := db.Queries.CreateLibrary(lib.ctx, sqlcgen.CreateLibraryParams{
		Name: "Fixtures",
		Path: root,
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	lib.scanInternal(library.ID, library.Name, library.Path)

	coversDir, err := coverart.CoversDir()
	if err != nil {
		t.Fatalf("covers dir: %v", err)
	}

	entries, err := os.ReadDir(coversDir)
	if err != nil {
		t.Fatalf("read covers dir: %v", err)
	}

	if len(entries) == 0 {
		t.Skip("fixture library produced no cover art; run make testdata")
	}

	perTier := map[string]int{}

	for _, entry := range entries {
		name := entry.Name()
		base := coverart.BaseName(name)

		if base+filepath.Ext(name) == name {
			t.Errorf("full-size cover written: %s", name)

			continue
		}

		perTier[strings.TrimSuffix(strings.TrimPrefix(name, base), ".jpg")]++
	}

	for _, suffix := range coverart.Suffixes {
		if perTier[suffix] == 0 {
			t.Errorf("no %s tier written", suffix)
		}
	}

	// And what the database points at is a file that exists.
	var stored string
	if err := db.QueryRowWriter(
		"SELECT file_path FROM cover_art LIMIT 1",
	).Scan(&stored); err != nil {
		t.Fatalf("read cover_art path: %v", err)
	}

	if _, err := os.Stat(stored); err != nil {
		t.Errorf("cover_art.file_path names a file that is not there: %v", err)
	}
}

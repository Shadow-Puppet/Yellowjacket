package library

import (
	"path/filepath"
	"testing"

	"yellowjacket/backend/database/sql/sqlcgen"
)

// TestScan_FixtureLibraryLeavesNothingBehind runs a real scan over the
// generated fixture library and asserts the invariant the file-shaped
// schema exists for: every row is a file's, and nothing outlives one.
//
// The old schema could not state this. A scan wrote a recording, an
// artist credit, a credit-artist link and a release-group link per
// file, all of which survived the file's deletion, and a real library
// accumulated 812 recordings, 216 release groups and 260 artists with
// nothing behind them - which is what made "do I own this" unanswerable.
func TestScan_FixtureLibraryLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	lib, db := setupTestLibrary(t)

	root, err := filepath.Abs("../../test_data/music_library_test")
	if err != nil {
		t.Fatalf("resolve fixture path: %v", err)
	}

	if _, err := filepath.Glob(filepath.Join(root, "*")); err != nil {
		t.Skipf("fixture library not generated (make testdata): %v", err)
	}

	library, err := db.Queries.CreateLibrary(lib.ctx, sqlcgen.CreateLibraryParams{
		Name: "Fixtures",
		Path: root,
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	if metrics := lib.scanInternal(library.ID, library.Name, library.Path); metrics == nil {
		t.Fatal("scanInternal returned nil metrics")
	}

	count := func(query string) int64 {
		t.Helper()

		var n int64
		if err := db.QueryRowWriter(query).Scan(&n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}

		return n
	}

	files := count("SELECT COUNT(*) FROM audio_files")
	if files == 0 {
		t.Skip("fixture library is empty; run make testdata")
	}

	tracks, err := lib.GetTracks(0)
	if err != nil {
		t.Fatalf("GetTracks: %v", err)
	}

	// One track per file: the projection cannot multiply rows, because
	// there is no join table left to multiply them.
	if int64(len(tracks)) != files {
		t.Errorf("GetTracks returned %d rows for %d files", len(tracks), files)
	}

	// Nothing shared outlives what refers to it.
	for _, c := range []struct {
		what  string
		query string
	}{
		{"albums with no file", `SELECT COUNT(*) FROM albums al
			WHERE NOT EXISTS (SELECT 1 FROM audio_files af WHERE af.album_id = al.id)`},
		{"artists nothing refers to", `SELECT COUNT(*) FROM artists a
			WHERE NOT EXISTS (SELECT 1 FROM audio_files af WHERE af.artist_id = a.id)
			  AND NOT EXISTS (SELECT 1 FROM albums al WHERE al.artist_id = a.id)`},
		{"genre links with no file", `SELECT COUNT(*) FROM file_genres fg
			WHERE NOT EXISTS (SELECT 1 FROM audio_files af WHERE af.id = fg.audio_file_id)`},
	} {
		if n := count(c.query); n != 0 {
			t.Errorf("%s = %d, want 0", c.what, n)
		}
	}

	// And the scan actually filed things: albums, artists and genres
	// all resolved, with the tags on the files that named them.
	if albums, err := lib.GetAlbums(0); err != nil || len(albums) == 0 {
		t.Errorf("GetAlbums = %d albums, err %v; want some", len(albums), err)
	}

	if artists, err := lib.GetArtists(0); err != nil || len(artists) == 0 {
		t.Errorf("GetArtists = %d artists, err %v; want some", len(artists), err)
	}

	if genres, err := lib.GetGenres(0); err != nil || len(genres) == 0 {
		t.Errorf("GetGenres = %d genres, err %v; want some", len(genres), err)
	}
}

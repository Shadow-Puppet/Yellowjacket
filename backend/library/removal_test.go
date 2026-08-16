package library

import (
	"os"
	"path/filepath"
	"testing"

	"yellowjacket/backend/coverart"
	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
)

// seedRemovableLibrary builds a library with one track, its recording
// chain, a cover art row, and a tagging queue entry — the shape a real
// scan leaves behind.
func seedRemovableLibrary(
	t *testing.T,
	lib *Library,
	coverPath string,
) sqlcgen.Library {
	t.Helper()

	ctx := lib.ctx
	q := lib.db.Queries

	library, err := q.CreateLibrary(ctx, sqlcgen.CreateLibraryParams{
		Name: "Test Library",
		Path: "/music",
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	if _, err := lib.db.ExecContext(
		`INSERT INTO cover_art (is_embedded, file_path, mime_type)
		 VALUES (0, ?, 'image/jpeg')`, coverPath,
	); err != nil {
		t.Fatalf("insert cover art: %v", err)
	}

	database.InsertTestTrack(t, lib.db, database.TestTrack{
		FilePath:  "/music/song.mp3",
		Title:     "Test Song",
		Artist:    "Test Artist",
		Album:     "Test Album",
		LengthMs:  180000,
		LibraryID: library.ID,
	})

	// Every scanned library gets tagging_items rows, one per album
	// folder.  These FK-reference libraries.
	if _, err := lib.db.ExecContext(
		`INSERT INTO tagging_items (group_key, library_id, album_name)
		 VALUES ('grp1', ?, 'Test Album')`, library.ID,
	); err != nil {
		t.Fatalf("insert tagging_items: %v", err)
	}

	if _, err := lib.db.ExecContext(
		`INSERT INTO tagging_candidates (group_key, candidates)
		 VALUES ('grp1', '[]')`,
	); err != nil {
		t.Fatalf("insert tagging_candidates: %v", err)
	}

	return library
}

func countRows(
	t *testing.T,
	lib *Library,
	table string,
	args ...any,
) int64 {
	t.Helper()

	query := "SELECT COUNT(*) FROM " + table

	rows, err := lib.db.QueryContext(query, args...)
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}

	defer func() { _ = rows.Close() }()

	var n int64

	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan count %s: %v", table, err)
		}
	}

	return n
}

// A library with tagging_items must still be removable.  tagging_items
// FK-references libraries with no ON DELETE clause, so leaving those
// rows behind fails the DELETE and rolls back the entire removal.
func TestRemoveLibrary_WithTaggingItems(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	library := seedRemovableLibrary(t, lib, "/nonexistent/cover.jpg")

	summary, err := lib.RemoveLibrary(library.ID)
	if err != nil {
		t.Fatalf("RemoveLibrary: %v", err)
	}

	if summary.TracksDeleted != 1 {
		t.Errorf("TracksDeleted = %d, want 1", summary.TracksDeleted)
	}

	// The test DB keeps a sentinel library at id=0 so audio_files rows
	// using the default library_id satisfy their FK, so scope this one
	// to the library actually removed.
	if n := countRows(
		t, lib, "libraries WHERE id = ?", library.ID,
	); n != 0 {
		t.Errorf("library row still present after removal")
	}

	for _, table := range []string{
		"audio_files",
		"albums",
		"artists",
		"file_genres",
		"tagging_items",
		"tagging_candidates",
		"cover_art",
	} {
		if n := countRows(t, lib, table); n != 0 {
			t.Errorf("%s has %d rows after removal, want 0", table, n)
		}
	}
}

// Removing a library must delete the cover art original *and* its
// derived size variants, which are not recorded in the database.
func TestRemoveLibrary_DeletesCoverArtVariants(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	dir := t.TempDir()

	var paths []string
	for _, tier := range thumbnailTiers {
		paths = append(paths, filepath.Join(
			dir, coverart.SizedFilename("abc123.jpg", tier.Suffix),
		))
	}

	// The largest tier is what cover_art.file_path names.
	cover := filepath.Join(dir, coverart.SizedFilename("abc123.jpg", "_lg"))

	for _, p := range paths {
		if err := os.WriteFile(p, []byte("img"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	library := seedRemovableLibrary(t, lib, cover)

	if _, err := lib.RemoveLibrary(library.ID); err != nil {
		t.Fatalf("RemoveLibrary: %v", err)
	}

	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("cover art file still present: %s", filepath.Base(p))
		}
	}
}

// CoverArtFileSet must cover every generated tier, from any of them:
// cover_art.file_path names the largest, and the others are derived.
func TestCoverArtFileSet(t *testing.T) {
	t.Parallel()

	got := CoverArtFileSet("/covers/abc123_lg.jpg")

	want := []string{
		"/covers/abc123_sm.jpg",
		"/covers/abc123_md.jpg",
		"/covers/abc123_lg.jpg",
	}

	if len(got) != len(want) {
		t.Fatalf("got %d paths, want %d: %v", len(got), len(want), got)
	}

	for i, w := range want {
		if got[i] != w {
			t.Errorf("path %d = %q, want %q", i, got[i], w)
		}
	}
}

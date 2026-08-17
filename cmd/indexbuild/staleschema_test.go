//go:build indexbuild

package main

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"yellowjacket/backend/database"
	"yellowjacket/backend/system"
)

// TestRetireLibraryTables is the CI failure written down.
//
// The index job's persistent volume held a database from before plan
// 013 reshaped audio_files, so every run died applying an index to a
// column the old table does not have.  The catalog in the same file is
// a day of downloading to rebuild and has to survive the repair.
func TestRetireLibraryTables(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	t.Setenv("YJ_HOME", t.TempDir())

	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		t.Fatalf("resolve data dir: %v", err)
	}

	dbPath := filepath.Join(dataDir, "yj.db")

	// Build the real thing, then put it back into the shape the volume
	// was actually in: a pre-013 audio_files, and a table the schema
	// stopped describing at all.
	if _, err := database.NewDB(logger); err != nil {
		t.Fatalf("first open: %v", err)
	}

	exec(t, dbPath, `
		INSERT INTO explore_index (entity_type, mbid, title, artist_name, artist_mbid)
		VALUES (1, randomblob(16), 'A Catalog Row', 'An Artist', randomblob(16));

		DROP TABLE audio_files;
		CREATE TABLE audio_files (
			id           INTEGER PRIMARY KEY,
			file_path    TEXT NOT NULL UNIQUE,
			recording_id INTEGER
		);
		CREATE TABLE recordings (id INTEGER PRIMARY KEY, title TEXT);
	`)

	// This used to assert the symptom -- that the schema cannot be
	// applied over a table whose shape has moved on -- because at the
	// time nothing repaired it and only this job did.  The app-side
	// repair (backend/database/staleshape.go) now retires a stale
	// non-authored table before applySchema meets it, so opening
	// succeeds and the symptom no longer reproduces from here.
	//
	// That does not make retireLibraryTables redundant, and the rest of
	// this test is why: the app-side repair only removes what is *stale*,
	// while this database wants its library half gone entirely, healthy
	// or not, because nothing here scans, plays or authors.
	if _, err := database.NewDB(logger); err != nil {
		t.Fatalf("the app-side repair should have opened this: %v", err)
	}

	if err := retireLibraryTables(context.Background(), logger); err != nil {
		t.Fatalf("retireLibraryTables: %v", err)
	}

	for _, table := range []string{"audio_files", "recordings"} {
		if count(t, dbPath, sqliteMasterQuery(table)) != 0 {
			t.Errorf("%s survived; the schema cannot be applied over it", table)
		}
	}

	if _, err := database.NewDB(logger); err != nil {
		t.Fatalf("open after retiring stale tables: %v", err)
	}

	// The catalog is why the volume exists.
	if got := count(t, dbPath, "SELECT COUNT(*) FROM explore_index"); got != 1 {
		t.Errorf("explore_index rows = %d, want 1 (the catalog must survive)", got)
	}
}

func sqliteMasterQuery(name string) string {
	return `SELECT COUNT(*) FROM sqlite_master WHERE name = '` + name + `'`
}

func exec(t *testing.T, dbPath, statements string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	if _, err := db.Exec(statements); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func count(t *testing.T, dbPath, query string) int {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	defer func() { _ = db.Close() }()

	var n int

	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return n
}

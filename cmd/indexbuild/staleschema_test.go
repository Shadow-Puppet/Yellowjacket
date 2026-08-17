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
	"yellowjacket/backend/datamap"
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

// TestTheCatalogSurvivesAStaleShape is the accident written down.
//
// The app repairs a stale Cache table by dropping it: its catalog is
// downloaded, so a wrong shape costs about a minute of re-fetching and
// keeping it costs every Explore read. Applied here that rule is
// catastrophic — this database is what the artifact is *cut from*, so
// there is nothing to re-fetch and the only way back is the ~205 GB
// dump stream the /cache volume exists to avoid.
//
// It shipped without that distinction and dropped the real CI catalog
// on the first run:
//
//	retiring a table ... table=explore_index
//	  reason="column entity_type is TEXT, schema declares INTEGER"
//	index maintenance mode=build reason="no completed import yet"
//
// The mismatch was real: that database is deliberately kept in the
// older encoding, which `fix(indexexport): read an index older than the
// binary` exists to tolerate. So the shape will not match, every run,
// by design — and the catalog must survive it anyway.
func TestTheCatalogSurvivesAStaleShape(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	t.Setenv("YJ_HOME", t.TempDir())

	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		t.Fatalf("resolve data dir: %v", err)
	}

	dbPath := filepath.Join(dataDir, "yj.db")

	if _, err := database.NewDB(logger); err != nil {
		t.Fatalf("first open: %v", err)
	}

	// The shape the real index database is in: every current column,
	// but the ids and the entity type still text.  That is what the
	// exporter's backward-compatibility fix tolerates, and it is what
	// the repair saw and called stale.
	exec(t, dbPath, `
		DROP TABLE explore_index;
		CREATE TABLE explore_index (
			id                     INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type            TEXT NOT NULL,
			mbid                   TEXT NOT NULL,
			title                  TEXT NOT NULL,
			artist_name            TEXT NOT NULL,
			artist_mbid            TEXT NOT NULL,
			aliases                TEXT NOT NULL DEFAULT '',
			popularity             INTEGER NOT NULL DEFAULT 0,
			listener_count         INTEGER NOT NULL DEFAULT 0,
			duration               INTEGER NOT NULL DEFAULT 0,
			caa_release_mbid       TEXT NOT NULL DEFAULT '',
			release_name           TEXT NOT NULL DEFAULT '',
			primary_type           TEXT NOT NULL DEFAULT '',
			secondary_types        TEXT NOT NULL DEFAULT '',
			release_date           TEXT NOT NULL DEFAULT '',
			total_tracks           INTEGER NOT NULL DEFAULT 0,
			artist_type            TEXT NOT NULL DEFAULT '',
			country                TEXT NOT NULL DEFAULT '',
			disambiguation         TEXT NOT NULL DEFAULT '',
			sort_name              TEXT NOT NULL DEFAULT '',
			in_library             INTEGER NOT NULL DEFAULT 0,
			is_similar             INTEGER NOT NULL DEFAULT 0,
			local_artist_id        INTEGER,
			local_release_group_id INTEGER,
			local_recording_id     INTEGER,
			discog_fetched         INTEGER NOT NULL DEFAULT 0,
			UNIQUE(mbid)
		);
		INSERT INTO explore_index
			(entity_type, mbid, title, artist_name, artist_mbid)
		VALUES ('artist', 'a-b-c', 'A Catalog Row', 'An Artist', 'd-e-f');
	`)

	if _, err := database.NewDB(logger); err != nil {
		t.Fatalf("open with a stale catalog shape: %v", err)
	}

	if got := count(t, dbPath, "SELECT COUNT(*) FROM explore_index"); got != 1 {
		t.Fatalf(
			"explore_index rows = %d, want 1 — the catalog was retired, "+
				"which costs this database a ~205GB rebuild",
			got,
		)
	}
}

// TestNoCacheTableIsRetiredHere is the general form of the accident
// above, and it exists because the specific one is not the risk.
//
// `TestTheCatalogSurvivesAStaleShape` pins one table in one wrong shape,
// which is the failure that happened. What cost the ~205 GB was not that
// shape: it was a destructive repair added to `database.NewDB` -- the
// one chokepoint every binary in this project shares -- without asking
// which binary it was running in. The next such repair will have a
// different name and a different reason, and this database still cannot
// afford it.
//
// So the assertion is about the *outcome* rather than the mechanism: put
// every Cache table in a shape the schema has certainly moved past, open
// the database the way cmd/indexbuild does, and require that all of them
// are still there afterwards. Any future repair that drops one fails
// here regardless of how it decides to.
//
// Two things about it are deliberate.
//
// The table list comes from `datamap.ByKind(Cache)` rather than being
// written out, so a Cache table added next year is covered by this test
// on the day it is added -- the same reason `TestCatalogCoversSchema`
// reads the schema instead of a list.
//
// And `NewDB` returning an error is *accepted*, because that is the
// trade the fix documents: with Cache tables no longer rebuilt here, a
// shape the schema moved past now fails this job loudly instead of
// silently costing it a day of downloading. Loud is fine. Gone is not.
func TestNoCacheTableIsRetiredHere(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	t.Setenv("YJ_HOME", t.TempDir())

	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		t.Fatalf("resolve data dir: %v", err)
	}

	dbPath := filepath.Join(dataDir, "yj.db")

	if _, err := database.NewDB(logger); err != nil {
		t.Fatalf("first open: %v", err)
	}

	// An FTS table is four shadow tables and cannot be given a "wrong
	// shape" meaningfully; the repair skips them for the same reason and
	// retires them with their parent, which the parents below cover.
	var cache []string

	for _, table := range datamap.ByKind(datamap.Cache) {
		if table.FTS {
			continue
		}

		cache = append(cache, table.Name)
	}

	if len(cache) == 0 {
		t.Fatal("no Cache tables to check: the datamap or this test is wrong")
	}

	for _, name := range cache {
		// A shape nothing in the current schema describes. What matters
		// is only that it disagrees; the real mismatch was one column's
		// type.
		exec(t, dbPath, `
			DROP TABLE IF EXISTS `+name+`;
			CREATE TABLE `+name+` (id INTEGER PRIMARY KEY, moved_past TEXT);
			INSERT INTO `+name+` (moved_past) VALUES ('irreplaceable');
		`)
	}

	// The error is not the assertion: see the note above.
	_, _ = database.NewDB(logger)

	for _, name := range cache {
		rows := count(t, dbPath,
			`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = '`+name+`'`)
		if rows == 0 {
			t.Errorf("%s was retired: in this database a Cache table is derived, "+
				"not downloaded, and dropping one costs the ~205 GB dump stream", name)

			continue
		}

		// Present but emptied is the same loss wearing a different
		// shape: SQLite does an implicit DELETE before a DROP, and a
		// repair that recreated the table would look identical here.
		if n := count(t, dbPath, `SELECT count(*) FROM `+name); n == 0 {
			t.Errorf("%s survived but was emptied", name)
		}
	}
}

package database

import (
	"context"
	"database/sql"
	"log/slog"
	"path"
	"testing"

	_ "modernc.org/sqlite"
)

// testLogger discards the repair's warnings; the tests assert on the
// database, not on the log.
func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// openRaw opens a scratch database file with no schema applied, so a
// test can build an *old* shape and then let NewDB's repair meet it.
func openRaw(t *testing.T, dir string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", path.Join(dir, "yj.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

// TestRetiresIndexMissingAColumn is plan 014's bug, symptom first: an
// explore_index created before `total_tracks` existed, met by the
// projection every explore read uses.  Before the repair this failed
// with "no such column: total_tracks" on every install that already had
// a catalog, while a fresh one was perfectly healthy.
func TestRetiresIndexMissingAColumn(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openRaw(t, dir)

	// The pre-014 shape: the columns the projection needs, minus the
	// one the plan added.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE explore_index (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type INTEGER NOT NULL,
			mbid BLOB NOT NULL,
			title TEXT NOT NULL,
			artist_name TEXT NOT NULL,
			artist_mbid BLOB NOT NULL
		);
		INSERT INTO explore_index (entity_type, mbid, title, artist_name, artist_mbid)
			VALUES (1, x'00112233445566778899aabbccddeeff', 'x', 'y', x'');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := retireStaleTables(ctx, db, testLogger()); err != nil {
		t.Fatalf("retire: %v", err)
	}

	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("applySchema: %v", err)
	}

	// The column the projection needs is there now.
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('explore_index')
		 WHERE name = 'total_tracks'`,
	).Scan(&n); err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if n != 1 {
		t.Fatalf("explore_index still has no total_tracks column")
	}

	// And the catalog really was retired rather than patched, so the
	// artifact is fetched again instead of half a catalog being served.
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM explore_index",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}

	if n != 0 {
		t.Fatalf("stale rows survived the retire: %d", n)
	}
}

// TestRetiresIndexWithTextMBIDs is the half an ALTER could not have
// repaired: plan 013 changed mbid from TEXT to BLOB, and SQLite does not
// coerce between them, so a query against 16 raw bytes returns no rows
// rather than an error.
func TestRetiresIndexWithTextMBIDs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openRaw(t, dir)

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE explore_index (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT NOT NULL,
			mbid TEXT NOT NULL,
			title TEXT NOT NULL,
			artist_name TEXT NOT NULL,
			artist_mbid TEXT NOT NULL,
			total_tracks INTEGER NOT NULL DEFAULT 0
		);
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := retireStaleTables(ctx, db, testLogger()); err != nil {
		t.Fatalf("retire: %v", err)
	}

	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("applySchema: %v", err)
	}

	var typ string
	if err := db.QueryRowContext(ctx,
		`SELECT type FROM pragma_table_info('explore_index') WHERE name = 'mbid'`,
	).Scan(&typ); err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if typ != "BLOB" {
		t.Fatalf("mbid is still %s, want BLOB", typ)
	}
}

// TestRetiringTheIndexTakesItsMetaWithIt guards the thing that makes the
// repair actually repair: the marker saying the import finished is what
// stops the artifact being fetched again, so a catalog dropped without
// it would stay empty forever.
func TestRetiringTheIndexTakesItsMetaWithIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openRaw(t, dir)

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE explore_index (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type INTEGER NOT NULL,
			mbid BLOB NOT NULL
		);
		CREATE TABLE explore_index_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO explore_index_meta VALUES ('dump_import_done', '1');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := retireStaleTables(ctx, db, testLogger()); err != nil {
		t.Fatalf("retire: %v", err)
	}

	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("applySchema: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM explore_index_meta WHERE key = 'dump_import_done'",
	).Scan(&n); err != nil {
		t.Fatalf("meta: %v", err)
	}

	if n != 0 {
		t.Fatalf("the import-done marker survived a retired catalog")
	}
}

// TestHealthyDatabaseIsUntouched is the other half, and the one that
// would make this dangerous if it failed: a current schema must survive
// a launch with its catalog intact.  A repair that retires a healthy
// catalog costs every user an artifact download on every start.
func TestHealthyDatabaseIsUntouched(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openRaw(t, dir)

	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("applySchema: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO explore_index (entity_type, mbid, title, artist_name, artist_mbid)
			VALUES (1, x'00112233445566778899aabbccddeeff', 'x', 'y', x'')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := retireStaleTables(ctx, db, testLogger()); err != nil {
		t.Fatalf("retire: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM explore_index",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}

	if n != 1 {
		t.Fatalf("a healthy catalog was retired: %d rows left", n)
	}
}

// TestAuthoredTablesAreNeverRetired states the boundary in a test rather
// than only in a comment: this mechanism deletes data, and the only
// thing standing between it and a user's playlists is the Kind filter.
func TestAuthoredTablesAreNeverRetired(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openRaw(t, dir)

	// A playlists table missing most of its current columns.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE playlists (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
		INSERT INTO playlists (name) VALUES ('irreplaceable');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := retireStaleTables(ctx, db, testLogger()); err != nil {
		t.Fatalf("retire: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM playlists",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}

	if n != 1 {
		t.Fatalf("an authored table was retired; rows left: %d", n)
	}
}

// TestRetiresTablesTheSchemaNoLongerDescribes covers what plan 013 left
// behind on every database that predates it: seven tables the schema
// stopped describing, plus the schema_migrations table that squashing
// the chain retired.  They are not stale in shape — they are simply not
// ours any more.
func TestRetiresTablesTheSchemaNoLongerDescribes(t *testing.T) {
	ctx := context.Background()
	db := openRaw(t, t.TempDir())

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE recordings (id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE artist_credit (id INTEGER PRIMARY KEY, text TEXT);
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY);
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := retireStaleTables(ctx, db, testLogger()); err != nil {
		t.Fatalf("retire: %v", err)
	}

	for _, table := range []string{"recordings", "artist_credit", "schema_migrations"} {
		var n int

		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?",
			table,
		).Scan(&n); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}

		if n != 0 {
			t.Errorf("%s survived; the schema no longer describes it", table)
		}
	}
}

// TestFTSShadowTablesAreNotObsolete is the sweep's sharp edge: an FTS5
// virtual table is backed by four shadow tables that appear in
// sqlite_master under their own names and are in no schema file.
// Dropping one destroys the index it belongs to.
func TestFTSShadowTablesAreNotObsolete(t *testing.T) {
	ctx := context.Background()
	db := openRaw(t, t.TempDir())

	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("applySchema: %v", err)
	}

	obsolete, err := obsoleteTables(ctx, db)
	if err != nil {
		t.Fatalf("obsoleteTables: %v", err)
	}

	if len(obsolete) != 0 {
		t.Fatalf("a freshly created schema reported obsolete tables: %v", obsolete)
	}
}

// TestRetiringOwnedTablesDoesNotDangle pins the one behaviour that is
// silently wrong rather than loudly broken.
//
// Retiring audio_files leaves playlist entries behind.  With
// foreign_keys ON — which applyPRAGMAs has done before NewDB gets here —
// SET NULL fires and they point at nothing.  With it OFF they keep ids
// that the rescan reissues from 1, so every playlist quietly fills with
// different songs.  Nothing about the schema makes that ordering
// obvious, so it is asserted rather than assumed.
func TestRetiringOwnedTablesDoesNotDangle(t *testing.T) {
	ctx := context.Background()
	db := openRaw(t, t.TempDir())

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma: %v", err)
	}

	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("applySchema: %v", err)
	}

	// Break audio_files' shape so it is retired, keeping a playlist
	// entry that references it.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO playlists (id, name) VALUES (1, 'keepme');
		INSERT INTO libraries (id, name, path) VALUES (0, 'test', '/music');
		INSERT INTO audio_files (id, file_path, file_type_id, length_milliseconds)
			VALUES (7, '/music/a.flac', 1, 1000);
		INSERT INTO playlist_tracks (playlist_id, audio_file_id, position)
			VALUES (1, 7, 0);
		DROP VIEW IF EXISTS track_metadata;
		ALTER TABLE audio_files DROP COLUMN artist_credit;
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := retireStaleTables(ctx, db, testLogger()); err != nil {
		t.Fatalf("retire: %v", err)
	}

	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("applySchema: %v", err)
	}

	var dangling int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM playlist_tracks WHERE audio_file_id IS NOT NULL",
	).Scan(&dangling); err != nil {
		t.Fatalf("count: %v", err)
	}

	if dangling != 0 {
		t.Fatalf(
			"%d playlist entries still point at retired audio_files ids; "+
				"a rescan will reissue those ids to different tracks",
			dangling,
		)
	}

	// The playlist itself is authored and must be untouched.
	var playlists int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM playlists",
	).Scan(&playlists); err != nil {
		t.Fatalf("playlists: %v", err)
	}

	if playlists != 1 {
		t.Fatalf("authored playlist lost: %d", playlists)
	}
}

// TestRetiringInterlinkedLegacyTables is the bug the unit tests missed
// and a real database found.
//
// The tables plan 013 retired reference each other -- pre-013
// audio_files has a foreign key into recordings -- so with foreign keys
// ON, dropping them one at a time fails with "FOREIGN KEY constraint
// failed" on whichever goes first, and map iteration order decides
// which that is.  Every other test in this file ran with foreign keys
// off and passed happily; the app enables them in applyPRAGMAs before
// the repair runs, so only the real launch path showed it.
func TestRetiringInterlinkedLegacyTables(t *testing.T) {
	ctx := context.Background()
	db := openRaw(t, t.TempDir())

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma: %v", err)
	}

	// The pre-013 shape, with the reference that makes ordering matter.
	// release_group_recordings sorts *after* recordings and references
	// it, so the deterministic order retires the parent while the child
	// still holds rows pointing at it -- which is the case that fails
	// without the deferral, rather than one that fails on some runs.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE recordings (id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE artist_credit (id INTEGER PRIMARY KEY, text TEXT);
		CREATE TABLE release_group_recordings (
			id           INTEGER PRIMARY KEY,
			recording_id INTEGER NOT NULL,
			FOREIGN KEY(recording_id) REFERENCES recordings(id)
		);
		CREATE TABLE audio_files (
			id           INTEGER PRIMARY KEY,
			file_path    TEXT NOT NULL UNIQUE,
			recording_id INTEGER,
			FOREIGN KEY(recording_id) REFERENCES recordings(id)
		);
		INSERT INTO recordings (id, name) VALUES (1, 'x');
		INSERT INTO release_group_recordings (id, recording_id) VALUES (1, 1);
		INSERT INTO audio_files (id, file_path, recording_id)
			VALUES (1, '/music/a.flac', 1);
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := retireStaleTables(ctx, db, testLogger()); err != nil {
		t.Fatalf("retire: %v", err)
	}

	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("applySchema: %v", err)
	}

	for _, table := range []string{"recordings", "artist_credit"} {
		var n int

		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?",
			table,
		).Scan(&n); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}

		if n != 0 {
			t.Errorf("%s survived the retire", table)
		}
	}

	// And the rebuilt audio_files is the current shape, which is the
	// whole reason the old one had to go.
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('audio_files')
		 WHERE name = 'artist_credit'`,
	).Scan(&n); err != nil {
		t.Fatalf("inspect audio_files: %v", err)
	}

	if n != 1 {
		t.Fatal("audio_files was not rebuilt in the current shape")
	}
}

// TestParseCreateTablesReadsTheRealSchema keeps the parser honest
// against the files it actually runs on: a parser that silently found
// no columns would report every table healthy and repair nothing.
func TestParseCreateTablesReadsTheRealSchema(t *testing.T) {
	declared, err := declaredTables()
	if err != nil {
		t.Fatalf("declaredTables: %v", err)
	}

	cols, ok := declared["explore_index"]
	if !ok {
		t.Fatal("explore_index was not parsed out of the schema files")
	}

	want := map[string]string{
		"mbid":         "BLOB",
		"total_tracks": "INTEGER",
		"artist_name":  "TEXT",
	}

	got := make(map[string]string, len(cols))
	for _, c := range cols {
		got[c.name] = c.typ
	}

	for name, typ := range want {
		if got[name] != typ {
			t.Errorf("explore_index.%s parsed as %q, want %q", name, got[name], typ)
		}
	}

	// A table constraint must not be mistaken for a column.
	for _, c := range cols {
		switch c.name {
		case "PRIMARY", "FOREIGN", "UNIQUE", "CHECK", "CONSTRAINT":
			t.Errorf("parsed table constraint %q as a column", c.name)
		}
	}
}

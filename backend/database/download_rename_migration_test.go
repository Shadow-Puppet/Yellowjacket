package database

import (
	"database/sql"
	"errors"
	"testing"
)

// oldDownloadRequestsDDL, oldDownloadWantsDDL and oldDownloadItemsDDL
// are frozen snapshots of the download subsystem's tables exactly as
// they read before the Want/Request rename (see
// download_rename_migration.go) — i.e. what a real user's existing
// database looks like today, before upgrading to a build that includes
// this migration.
const oldDownloadRequestsDDL = `
CREATE TABLE IF NOT EXISTS download_requests (
    id                 TEXT PRIMARY KEY,
    library_id         INTEGER NOT NULL,
    source             TEXT    NOT NULL DEFAULT 'manual',
    want_id            INTEGER REFERENCES download_wants(id) ON DELETE SET NULL,
    release_mbid       TEXT,
    release_group_mbid TEXT,
    recording_mbid     TEXT,
    artist             TEXT    NOT NULL DEFAULT '',
    album              TEXT    NOT NULL DEFAULT '',
    query              TEXT    NOT NULL DEFAULT '',
    expected           TEXT    NOT NULL DEFAULT '[]',
    state              TEXT    NOT NULL DEFAULT 'searching'
        CHECK(state IN ('searching', 'found', 'queued', 'grabbing',
                        'verifying', 'tagging', 'importing',
                        'complete', 'cancelled', 'failed')),
    error              TEXT    NOT NULL DEFAULT '',
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_download_requests_created
    ON download_requests(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_download_requests_state
    ON download_requests(state);
`

const oldDownloadWantsDDL = `
CREATE TABLE IF NOT EXISTS download_wants (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    mbid       TEXT    NOT NULL,
    entity     TEXT    NOT NULL
        CHECK(entity IN ('artist', 'release-group', 'release', 'recording')),
    library_id INTEGER NOT NULL,
    artist     TEXT    NOT NULL DEFAULT '',
    title      TEXT    NOT NULL DEFAULT '',
    scope      TEXT    NOT NULL DEFAULT 'future'
        CHECK(scope IN ('future', 'all')),
    secondary  INTEGER NOT NULL DEFAULT 0,
    state      TEXT    NOT NULL DEFAULT 'wanted'
        CHECK(state IN ('wanted', 'satisfied', 'paused')),
    parent_id  INTEGER,
    attempts     INTEGER  NOT NULL DEFAULT 0,
    last_error   TEXT     NOT NULL DEFAULT '',
    last_tried_at DATETIME,
    next_try_at  DATETIME,
    external_ids TEXT    NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(mbid, library_id),
    FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE,
    FOREIGN KEY(parent_id) REFERENCES download_wants(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_download_wants_due
    ON download_wants(next_try_at)
    WHERE state = 'wanted';

CREATE INDEX IF NOT EXISTS idx_download_wants_entity
    ON download_wants(entity, state);

CREATE INDEX IF NOT EXISTS idx_download_wants_parent
    ON download_wants(parent_id);
`

const oldDownloadItemsDDL = `
CREATE TABLE IF NOT EXISTS download_items (
    id           TEXT PRIMARY KEY,
    request_id   TEXT    NOT NULL,
    provider_id  INTEGER NOT NULL,
    transport_id INTEGER,
    external_id  TEXT    NOT NULL DEFAULT '',
    candidate    TEXT    NOT NULL DEFAULT '{}',
    state        TEXT    NOT NULL DEFAULT 'queued'
        CHECK(state IN ('searching', 'found', 'queued', 'grabbing',
                        'verifying', 'tagging', 'importing',
                        'complete', 'cancelled', 'failed')),
    staging_dir  TEXT    NOT NULL DEFAULT '',
    bytes_done   INTEGER NOT NULL DEFAULT 0,
    bytes_total  INTEGER NOT NULL DEFAULT 0,
    imported_paths TEXT  NOT NULL DEFAULT '[]',
    error        TEXT    NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(request_id) REFERENCES download_requests(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_download_items_live
    ON download_items(state)
    WHERE state NOT IN ('complete', 'cancelled', 'failed');

CREATE INDEX IF NOT EXISTS idx_download_items_request
    ON download_items(request_id);

CREATE INDEX IF NOT EXISTS idx_download_items_state
    ON download_items(state);
`

// seedOldDownloadSchema builds the pre-rename download tables and
// inserts one row of real data into each, standing in for a real
// user's database at the moment it upgrades.
func seedOldDownloadSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, ddl := range []string{
		oldDownloadWantsDDL, oldDownloadRequestsDDL, oldDownloadItemsDDL,
	} {
		if _, err := db.ExecContext(t.Context(), ddl); err != nil {
			t.Fatalf("create old download schema: %v", err)
		}
	}

	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO libraries (id, name, path) VALUES (1, 'Test', '/music')`,
	); err != nil {
		t.Fatalf("seed library: %v", err)
	}

	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO download_wants
		    (id, mbid, entity, library_id, artist, title, state)
		 VALUES (1, 'artist-mbid', 'artist', 1, 'Radiohead', 'Radiohead', 'wanted')`,
	); err != nil {
		t.Fatalf("seed download_wants: %v", err)
	}

	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO download_requests
		    (id, library_id, source, want_id, release_group_mbid, artist, album, state)
		 VALUES ('dl-1', 1, 'wanted', 1, 'rg-mbid', 'Radiohead', 'OK Computer', 'complete')`,
	); err != nil {
		t.Fatalf("seed download_requests: %v", err)
	}

	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO download_items
		    (id, request_id, provider_id, state)
		 VALUES ('item-1', 'dl-1', 1, 'complete')`,
	); err != nil {
		t.Fatalf("seed download_items: %v", err)
	}
}

// TestDownloadRename_FreshInstallUntouched confirms applySchema on a
// brand-new database produces the target shape directly and that
// migrateDownloadRename's gate (checking for the old download_wants
// table) is a no-op there — the destructive path this test guards
// against is exactly the one described in download_rename_migration.go:
// blindly replaying the rename against a fresh database's already-
// correct, empty download_requests/download_downloads tables.
func TestDownloadRename_FreshInstallUntouched(t *testing.T) {
	t.Parallel()

	db := openMemDB(t)

	if err := applySchema(t.Context(), db); err != nil {
		t.Fatalf("apply schema (fresh): %v", err)
	}

	for _, table := range []string{"download_downloads", "download_requests", "download_items"} {
		var name string

		err := db.QueryRowContext(
			t.Context(),
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
			table,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist on a fresh install: %v", table, err)
		}
	}

	var stray string

	err := db.QueryRowContext(
		t.Context(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'download_wants'`,
	).Scan(&stray)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("old download_wants table should not exist on a fresh install, err=%v", err)
	}

	// Both auto-download guardrail indexes sql/schemas deliberately
	// omits (see ensureDownloadIndexes) must still exist.
	for _, idx := range []string{
		"idx_download_requests_due",
		"idx_download_requests_entity",
		"idx_download_requests_parent",
		"idx_download_items_download",
	} {
		var name string

		err := db.QueryRowContext(
			t.Context(),
			`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`,
			idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected index %q to exist on a fresh install: %v", idx, err)
		}
	}
}

// TestDownloadRename_UpgradesExistingDatabase is the regression test
// for the rename itself: an old-shaped database (download_wants +
// old-style download_requests, both with real rows) must end up with
// the same table names, column names, and data a fresh install would
// have — nothing dropped, nothing silently emptied.
func TestDownloadRename_UpgradesExistingDatabase(t *testing.T) {
	t.Parallel()

	fresh := openMemDB(t)
	if err := applySchema(t.Context(), fresh); err != nil {
		t.Fatalf("apply schema (fresh): %v", err)
	}

	upgraded := openMemDB(t)

	librariesDDL, err := schemas.ReadFile("sql/schemas/libraries.sql")
	if err != nil {
		t.Fatalf("read libraries schema: %v", err)
	}

	if _, err := upgraded.ExecContext(t.Context(), string(librariesDDL)); err != nil {
		t.Fatalf("create libraries table: %v", err)
	}

	seedOldDownloadSchema(t, upgraded)

	if err := applySchema(t.Context(), upgraded); err != nil {
		t.Fatalf("apply schema (upgrade path): %v", err)
	}

	// Column order must match a fresh install's, for the same reason
	// TestMigrations_ColumnOrderMatchesFreshInstall checks tagging_items:
	// sqlc's `SELECT *` binds positionally.
	for _, table := range []string{"download_downloads", "download_requests", "download_items"} {
		freshCols := tableColumns(t, fresh, table)
		upgradedCols := tableColumns(t, upgraded, table)

		if len(freshCols) != len(upgradedCols) {
			t.Fatalf(
				"%s: column count mismatch: fresh has %d (%v), upgraded has %d (%v)",
				table, len(freshCols), freshCols, len(upgradedCols), upgradedCols,
			)
		}

		for i := range freshCols {
			if freshCols[i] != upgradedCols[i] {
				t.Errorf(
					"%s: column order mismatch at %d: fresh %q, upgraded %q\nfresh:    %v\nupgraded: %v",
					table,
					i,
					freshCols[i],
					upgradedCols[i],
					freshCols,
					upgradedCols,
				)
			}
		}
	}

	// The seeded rows survived the rename under their new names.
	var (
		requestMBID   string
		requestEntity string
	)

	err = upgraded.QueryRowContext(
		t.Context(), `SELECT mbid, entity FROM download_requests WHERE id = 1`,
	).Scan(&requestMBID, &requestEntity)
	if err != nil {
		t.Fatalf("seeded request row missing after rename: %v", err)
	}

	if requestMBID != "artist-mbid" || requestEntity != "artist" {
		t.Errorf("request row corrupted: mbid=%q entity=%q", requestMBID, requestEntity)
	}

	var (
		downloadRequestID sql.NullInt64
		downloadAlbum     string
	)

	err = upgraded.QueryRowContext(
		t.Context(),
		`SELECT request_id, album FROM download_downloads WHERE id = 'dl-1'`,
	).Scan(&downloadRequestID, &downloadAlbum)
	if err != nil {
		t.Fatalf("seeded download row missing after rename: %v", err)
	}

	if !downloadRequestID.Valid || downloadRequestID.Int64 != 1 {
		t.Errorf("download.request_id = %v, want 1 (renamed from want_id)", downloadRequestID)
	}

	if downloadAlbum != "OK Computer" {
		t.Errorf("download.album = %q, want OK Computer", downloadAlbum)
	}

	var itemDownloadID string

	err = upgraded.QueryRowContext(
		t.Context(),
		`SELECT download_id FROM download_items WHERE id = 'item-1'`,
	).Scan(&itemDownloadID)
	if err != nil {
		t.Fatalf("seeded item row missing after rename: %v", err)
	}

	if itemDownloadID != "dl-1" {
		t.Errorf("item.download_id = %q, want dl-1 (renamed from request_id)", itemDownloadID)
	}

	// The old table is gone, not just emptied.
	var stray string

	err = upgraded.QueryRowContext(
		t.Context(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'download_wants'`,
	).Scan(&stray)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("old download_wants table should be gone after migration, err=%v", err)
	}

	// Running the whole thing again (as a second app startup would) is
	// a no-op: the gate sees no download_wants table and does nothing
	// further, so this must not error or duplicate anything.
	if err := applySchema(t.Context(), upgraded); err != nil {
		t.Fatalf("apply schema a second time: %v", err)
	}

	var count int

	if err := upgraded.QueryRowContext(
		t.Context(), `SELECT COUNT(*) FROM download_requests`,
	).Scan(&count); err != nil {
		t.Fatalf("count download_requests: %v", err)
	}

	if count != 1 {
		t.Errorf("download_requests has %d rows after a second migration pass, want 1", count)
	}
}

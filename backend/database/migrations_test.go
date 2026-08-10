package database

import (
	"database/sql"
	"testing"
)

// oldTaggingItemsDDL is a frozen snapshot of tagging_items exactly as
// it read before sql/migrations/0001_tagging_items_synthetic.sql —
// i.e. what a real user's existing database looks like today, before
// upgrading to a build that includes that migration.
const oldTaggingItemsDDL = `
CREATE TABLE IF NOT EXISTS tagging_items (
  group_key               TEXT PRIMARY KEY,
  library_id              INTEGER NOT NULL,
  track_count             INTEGER NOT NULL DEFAULT 0,
  album_name              TEXT NOT NULL DEFAULT '',
  album_artist            TEXT NOT NULL DEFAULT '',
  disc_number              INTEGER NOT NULL DEFAULT 0,
  best_match_release_mbid TEXT,
  score                   REAL,
  last_checked_at         DATETIME,
  status                  TEXT NOT NULL DEFAULT 'pending'
    CHECK(status IN ('pending', 'matched', 'confirmed', 'skipped')),
  cleared_at              DATETIME,
  created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(library_id) REFERENCES libraries(id)
);

CREATE INDEX IF NOT EXISTS idx_tagging_items_library_status
    ON tagging_items(library_id, status);

CREATE INDEX IF NOT EXISTS idx_tagging_items_status_pending
    ON tagging_items(library_id) WHERE status = 'pending';
`

// tableColumns returns the column names of a table in on-disk
// (positional) order, via PRAGMA table_info — the order sqlc's
// generated `SELECT *` scans bind to positionally.
func tableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "PRAGMA table_info("+table+")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}

	defer func() { _ = rows.Close() }()

	var cols []string

	for rows.Next() {
		var (
			cid        int
			name       string
			ctype      string
			notnull    int
			dfltValue  sql.NullString
			primaryKey int
		)

		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &primaryKey); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}

		cols = append(cols, name)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info: %v", err)
	}

	return cols
}

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}

	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if err := applyPRAGMAs(t.Context(), db); err != nil {
		t.Fatalf("apply pragmas: %v", err)
	}

	return db
}

// TestMigrations_ColumnOrderMatchesFreshInstall is the regression
// test for the exact failure mode that got the old 48-step migration
// chain torn out (see .planning/NOTES.md, "No migration chain"):
// sql/schemas drifting from what migrations actually produce, so
// sqlc-generated code silently reads the wrong thing.
//
// A fresh install takes tagging_items straight from sql/schemas
// (CREATE TABLE, columns in file order). An existing database takes
// it from sql/schemas (the base shape, unchanged since the table
// already existed) plus sql/migrations/0001 (`ALTER TABLE ADD
// COLUMN`, which SQLite always appends at the END of the column
// list, regardless of where the column sits in the CREATE TABLE
// statement). If sql/schemas ever declares a migrated column
// somewhere other than last, the two paths produce tables with the
// SAME columns in a DIFFERENT order — invisible until a `SELECT *`
// (e.g. GetTaggingItem) silently binds a value to the wrong field.
func TestMigrations_ColumnOrderMatchesFreshInstall(t *testing.T) {
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

	if _, err := upgraded.ExecContext(t.Context(), oldTaggingItemsDDL); err != nil {
		t.Fatalf("create pre-migration tagging_items: %v", err)
	}

	// sql/schemas no-ops on the pre-existing tagging_items (IF NOT
	// EXISTS), then sql/migrations/0001's ALTER TABLE statements
	// actually add the missing columns for real this time.
	if err := applySchema(t.Context(), upgraded); err != nil {
		t.Fatalf("apply schema (upgrade path): %v", err)
	}

	freshCols := tableColumns(t, fresh, "tagging_items")
	upgradedCols := tableColumns(t, upgraded, "tagging_items")

	if len(freshCols) != len(upgradedCols) {
		t.Fatalf(
			"column count mismatch: fresh install has %d (%v), upgraded has %d (%v)",
			len(freshCols), freshCols, len(upgradedCols), upgradedCols,
		)
	}

	for i := range freshCols {
		if freshCols[i] != upgradedCols[i] {
			t.Errorf(
				"column order mismatch at position %d: fresh install has %q, upgraded has %q\nfresh:    %v\nupgraded: %v",
				i,
				freshCols[i],
				upgradedCols[i],
				freshCols,
				upgradedCols,
			)
		}
	}
}

// TestMigrations_FreshDatabaseStillRecordsAndGetsIndex confirms a
// brand-new database runs migration 0001 (tolerating "duplicate
// column name" from its ALTER TABLE statements, since sql/schemas
// already declared those columns), records it applied, AND still
// gets the trailing CREATE INDEX statement sql/schemas deliberately
// omits for migrated columns.
func TestMigrations_FreshDatabaseStillRecordsAndGetsIndex(t *testing.T) {
	t.Parallel()

	fresh := openMemDB(t)
	if err := applySchema(t.Context(), fresh); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	var version int

	err := fresh.QueryRowContext(
		t.Context(), "SELECT version FROM schema_migrations WHERE version = 1",
	).Scan(&version)
	if err != nil {
		t.Fatalf("expected migration 1 to be recorded as applied on a fresh db: %v", err)
	}

	var indexName string

	err = fresh.QueryRowContext(
		t.Context(),
		"SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_tagging_items_parent_group_key'",
	).Scan(&indexName)
	if err != nil {
		t.Fatalf("expected idx_tagging_items_parent_group_key to exist on a fresh db: %v", err)
	}
}

package database

import (
	"strings"
	"testing"
)

// seedExploreRow inserts one explore_index row.
func seedExploreRow(t *testing.T, db *DB, mbid, title, artist string) {
	t.Helper()

	if _, err := db.ExecContext(`
		INSERT INTO explore_index (entity_type, mbid, title, artist_name, artist_mbid)
		VALUES ('recording', ?, ?, ?, '')
	`, mbid, title, artist); err != nil {
		t.Fatalf("seed %s: %v", mbid, err)
	}
}

// ftsMatches returns how many FTS rows match a query.
func ftsMatches(t *testing.T, db *DB, query string) int {
	t.Helper()

	rows, err := db.QueryContext(
		"SELECT COUNT(*) FROM explore_index_fts WHERE explore_index_fts MATCH ?", query,
	)
	if err != nil {
		t.Fatalf("fts query %q: %v", query, err)
	}

	defer func() { _ = rows.Close() }()

	n := 0

	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan fts count: %v", err)
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("fts rows: %v", err)
	}

	return n
}

// Rows written while FTS sync is suspended are invisible to search
// until the window closes — and fully searchable afterwards.  This is
// the contract the dump import's bulk-load path depends on.
func TestExploreFTSSuspendResumeIndexesBulkRows(t *testing.T) {
	db := NewTestDB(t)

	seedExploreRow(t, db, "mbid-before", "Before Suspend", "Artist One")

	if got := ftsMatches(t, db, "Before"); got != 1 {
		t.Fatalf("matches for pre-suspend row = %d, want 1", got)
	}

	if err := db.SuspendExploreIndexFTS(); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	seedExploreRow(t, db, "mbid-during", "During Suspend", "Artist Two")

	if got := ftsMatches(t, db, "During"); got != 0 {
		t.Errorf("matches while suspended = %d, want 0 (triggers should be off)", got)
	}

	if err := db.ResumeExploreIndexFTS(); err != nil {
		t.Fatalf("resume: %v", err)
	}

	if got := ftsMatches(t, db, "During"); got != 1 {
		t.Errorf("matches for bulk-loaded row after resume = %d, want 1", got)
	}

	if got := ftsMatches(t, db, "Before"); got != 1 {
		t.Errorf("matches for pre-suspend row after resume = %d, want 1", got)
	}
}

// The import wipes explore_index before reassembling it.  With the
// triggers suspended that DELETE writes no FTS delete-markers, so the
// rebuild must be what clears the old rows out of search.
func TestExploreFTSResumeDropsDeletedRows(t *testing.T) {
	db := NewTestDB(t)

	seedExploreRow(t, db, "mbid-stale", "Stale Recording", "Old Artist")

	if err := db.SuspendExploreIndexFTS(); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	if _, err := db.ExecContext("DELETE FROM explore_index"); err != nil {
		t.Fatalf("wipe: %v", err)
	}

	seedExploreRow(t, db, "mbid-fresh", "Fresh Recording", "New Artist")

	if err := db.ResumeExploreIndexFTS(); err != nil {
		t.Fatalf("resume: %v", err)
	}

	if got := ftsMatches(t, db, "Stale"); got != 0 {
		t.Errorf("matches for wiped row = %d, want 0", got)
	}

	if got := ftsMatches(t, db, "Fresh"); got != 1 {
		t.Errorf("matches for reassembled row = %d, want 1", got)
	}
}

// resumeFTS runs from a defer as well as at its natural point in the
// pipeline, so a second call must be harmless.
func TestExploreFTSResumeIsIdempotent(t *testing.T) {
	db := NewTestDB(t)

	if err := db.SuspendExploreIndexFTS(); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	seedExploreRow(t, db, "mbid-a", "Repeatable Resume", "Artist")

	if err := db.ResumeExploreIndexFTS(); err != nil {
		t.Fatalf("first resume: %v", err)
	}

	if err := db.ResumeExploreIndexFTS(); err != nil {
		t.Fatalf("second resume: %v", err)
	}

	if got := ftsMatches(t, db, "Repeatable"); got != 1 {
		t.Errorf("matches after repeated resume = %d, want 1", got)
	}

	// Triggers must still be live for ordinary writes after the window.
	seedExploreRow(t, db, "mbid-b", "Postwindow Row", "Artist")

	if got := ftsMatches(t, db, "Postwindow"); got != 1 {
		t.Errorf("matches for row written after resume = %d, want 1", got)
	}
}

// Suspending twice must not fail — the triggers are simply already gone.
func TestExploreFTSSuspendIsIdempotent(t *testing.T) {
	db := NewTestDB(t)

	if err := db.SuspendExploreIndexFTS(); err != nil {
		t.Fatalf("first suspend: %v", err)
	}

	if err := db.SuspendExploreIndexFTS(); err != nil {
		t.Fatalf("second suspend: %v", err)
	}

	if err := db.ResumeExploreIndexFTS(); err != nil {
		t.Fatalf("resume: %v", err)
	}
}

// ftsSegmentCount reports how much the FTS index itself has been
// written to.  Every delete + insert the update trigger performs
// appends to the shadow content table, so this is the observable that
// tells "the trigger re-indexed the row" from "the trigger declined
// to".  Search results cannot: a no-op re-index leaves the same
// matches behind.
func ftsSegmentCount(t *testing.T, db *DB) int {
	t.Helper()

	rows, err := db.QueryContext("SELECT COUNT(*) FROM explore_index_fts_data")
	if err != nil {
		t.Fatalf("fts data count: %v", err)
	}

	defer func() { _ = rows.Close() }()

	n := 0

	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan fts data count: %v", err)
		}
	}

	return n
}

// The common write in this schema is an upsert whose merge rules keep
// every existing value — the discography backfill re-browsing a known
// artist, the incremental dump refreshing popularity.  Re-indexing
// those cost an FTS5 delete against a multi-million row index while
// holding the single writer connection, which is what starved the
// playback path.  An update that leaves title, artist_name and aliases
// alone must not touch the FTS index at all.
func TestExploreFTSUpdateSkipsUnchangedText(t *testing.T) {
	db := NewTestDB(t)

	seedExploreRow(t, db, "mbid-1", "Unchanged Title", "Steady Artist")

	before := ftsSegmentCount(t, db)

	// A popularity refresh: an FTS column is not named at all.
	if _, err := db.ExecContext(
		"UPDATE explore_index SET popularity = 42 WHERE mbid = 'mbid-1'",
	); err != nil {
		t.Fatalf("popularity update: %v", err)
	}

	// An upsert-shaped write that re-states the text identically, which
	// is what the merge rules produce for a row that has not changed.
	if _, err := db.ExecContext(`
		UPDATE explore_index
		SET title = 'Unchanged Title', artist_name = 'Steady Artist', popularity = 43
		WHERE mbid = 'mbid-1'
	`); err != nil {
		t.Fatalf("no-op text update: %v", err)
	}

	if got := ftsSegmentCount(t, db); got != before {
		t.Errorf(
			"FTS index written by an update that changed no text: %d rows, want %d",
			got, before,
		)
	}

	if got := ftsMatches(t, db, "Unchanged"); got != 1 {
		t.Errorf("matches after unchanged updates = %d, want 1", got)
	}
}

// The other half of the same guard: a real rename still re-indexes,
// old term gone and new term found.
func TestExploreFTSUpdateReindexesChangedText(t *testing.T) {
	db := NewTestDB(t)

	seedExploreRow(t, db, "mbid-2", "Original Title", "Some Artist")

	if _, err := db.ExecContext(
		"UPDATE explore_index SET title = 'Corrected Title' WHERE mbid = 'mbid-2'",
	); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if got := ftsMatches(t, db, "Original"); got != 0 {
		t.Errorf("matches for the old title = %d, want 0", got)
	}

	if got := ftsMatches(t, db, "Corrected"); got != 1 {
		t.Errorf("matches for the new title = %d, want 1", got)
	}

	// The same for the other two indexed columns.
	if _, err := db.ExecContext(
		"UPDATE explore_index SET artist_name = 'Renamed Artist', aliases = 'AKA Thing' WHERE mbid = 'mbid-2'",
	); err != nil {
		t.Fatalf("artist rename: %v", err)
	}

	if got := ftsMatches(t, db, "Renamed"); got != 1 {
		t.Errorf("matches for the new artist = %d, want 1", got)
	}

	if got := ftsMatches(t, db, "AKA"); got != 1 {
		t.Errorf("matches for the new alias = %d, want 1", got)
	}
}

// An existing install already carries the previous, unguarded trigger,
// and a create that tolerated "already exists" would leave it there
// forever — so the definition has to be replaced on open, not merely
// offered.
func TestExploreFTSTriggersAreReplacedOnOpen(t *testing.T) {
	db := NewTestDB(t)

	if err := db.SuspendExploreIndexFTS(); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	// The shape that shipped before: fires on every UPDATE.
	if _, err := db.ExecContext(`
		CREATE TRIGGER explore_index_au AFTER UPDATE ON explore_index BEGIN
			INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name, aliases)
			VALUES ('delete', old.id, old.title, old.artist_name, old.aliases);
			INSERT INTO explore_index_fts(rowid, title, artist_name, aliases)
			VALUES (new.id, new.title, new.artist_name, new.aliases);
		END
	`); err != nil {
		t.Fatalf("install old trigger: %v", err)
	}

	if err := createExploreIndexFTSTriggers(db.Ctx, db.db); err != nil {
		t.Fatalf("recreate triggers: %v", err)
	}

	rows, err := db.QueryContext(
		"SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = 'explore_index_au'",
	)
	if err != nil {
		t.Fatalf("read trigger sql: %v", err)
	}

	defer func() { _ = rows.Close() }()

	definition := ""

	if rows.Next() {
		if err := rows.Scan(&definition); err != nil {
			t.Fatalf("scan trigger sql: %v", err)
		}
	}

	if !strings.Contains(definition, "UPDATE OF") ||
		!strings.Contains(definition, "WHEN") {
		t.Errorf("explore_index_au was not replaced; definition is:\n%s", definition)
	}
}

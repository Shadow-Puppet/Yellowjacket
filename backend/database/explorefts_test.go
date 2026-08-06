package database

import (
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

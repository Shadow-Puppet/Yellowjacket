package explore

import (
	"context"
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/explore/eval"
)

// seedIndexRow inserts one explore_index row.  The FTS triggers keep
// explore_index_fts in sync automatically.
func seedIndexRow(
	t *testing.T,
	db *database.DB,
	entityType, mbid, title, artist string,
	popularity int,
) {
	t.Helper()

	_, err := db.ExecContext(`
		INSERT INTO explore_index
			(entity_type, mbid, title, artist_name, artist_mbid, popularity, listener_count)
		VALUES (?, ?, ?, ?, x'', ?, ?)
	`, dbEntityType(entityType), dbMBID(testMBID(mbid)), title, artist, popularity, popularity/10)
	if err != nil {
		t.Fatalf("seed %s/%s: %v", entityType, mbid, err)
	}
}

// newTestIndex builds a SearchIndex over a seeded in-memory DB.  lb and
// artistImg are nil because Search touches neither.
func newTestIndex(t *testing.T, db *database.DB) *SearchIndex {
	t.Helper()

	idx := NewSearchIndex(db, nil, nil, slog.Default())
	idx.MarkReadyIfPopulated()

	return idx
}

// TestEvalHarnessIndexRanking is the end-to-end wiring of the eval
// harness against the real FTS index Search.  It seeds a controlled
// corpus where the correct answer is known, then asserts the harness
// reports a perfect score — proving both the index ranking and the
// harness plumbing on a case we fully control.
func TestEvalHarnessIndexRanking(t *testing.T) {
	db := database.NewTestDB(t)

	// Popular exact-match artist should beat a more obscure namesake
	// and an unrelated album.
	seedIndexRow(t, db, "artist", "rh", "Radiohead", "Radiohead", 5_000_000)
	seedIndexRow(t, db, "artist", "radio-obscure", "Radio Birdman", "Radio Birdman", 40_000)
	seedIndexRow(t, db, "release_group", "okc", "OK Computer", "Radiohead", 2_000_000)
	seedIndexRow(t, db, "artist", "beatles", "The Beatles", "The Beatles", 9_000_000)
	seedIndexRow(t, db, "artist", "teenagers", "The Teenagers", "The Teenagers", 60_000)

	idx := newTestIndex(t, db)

	ranker := eval.RankerFunc(func(query string, limit int) []eval.Result {
		hits := idx.Search(context.Background(), query, limit)
		out := make([]eval.Result, 0, len(hits))

		for _, h := range hits {
			out = append(out, eval.Result{EntityType: h.EntityType, MBID: h.MBID})
		}

		return out
	})

	fixtures := []eval.Fixture{
		{
			Query:  "radiohead",
			Note:   "popular exact artist match",
			Expect: []eval.Expected{{Type: "artist", MBID: testMBID("rh")}},
		},
		{
			Query:  "the teenagers",
			Note:   "low-popularity exact match must beat high-popularity article match",
			Expect: []eval.Expected{{Type: "artist", MBID: testMBID("teenagers")}},
		},
	}

	report := eval.Evaluate(ranker, fixtures, 5)
	t.Log("\n" + report.Format())

	if report.HitRate < 1.0 {
		t.Errorf("expected every query to surface its result, got hit rate %.3f", report.HitRate)
	}

	if report.Top1Rate < 1.0 {
		t.Errorf("expected every result at rank 1, got top-1 rate %.3f:\n%s",
			report.Top1Rate, report.Format())
	}
}

// TestExploreFTSDiacriticFolding proves migration 37: an unaccented
// query must find an accented title (and vice versa) now that
// explore_index_fts folds diacritics.
func TestExploreFTSDiacriticFolding(t *testing.T) {
	db := database.NewTestDB(t)

	seedIndexRow(t, db, "artist", "bey", "Beyoncé", "Beyoncé", 8_000_000)
	seedIndexRow(t, db, "artist", "bjork", "Björk", "Björk", 3_000_000)

	idx := newTestIndex(t, db)

	cases := []struct {
		query    string
		wantMBID string
	}{
		{"beyonce", "bey"}, // unaccented query → accented title
		{"beyoncé", "bey"}, // accented query still works
		{"bjork", "bjork"}, // ö → o folding
		{"björk", "bjork"}, // accented query still works
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			hits := idx.Search(context.Background(), tc.query, 5)
			if len(hits) == 0 {
				t.Fatalf("query %q returned no hits", tc.query)
			}

			if hits[0].MBID != testMBID(tc.wantMBID) {
				t.Errorf(
					"query %q: top hit = %q, want %q",
					tc.query,
					hits[0].MBID,
					testMBID(tc.wantMBID),
				)
			}
		})
	}
}

// TestEvalFixtureFileParses guards the checked-in fixture file so a
// malformed edit fails fast rather than silently skipping queries.
func TestEvalFixtureFileParses(t *testing.T) {
	fixtures, err := eval.LoadFixtures("eval/testdata/eval_queries.json")
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}

	for i, fx := range fixtures {
		if fx.Query == "" {
			t.Errorf("fixture %d has empty query", i)
		}

		if len(fx.Expect) == 0 {
			t.Errorf("fixture %q has no expectations", fx.Query)
		}
	}
}

// seedIndexResult writes one explore_index row through the same
// upsertBatch every other writer uses.
//
// The three seeders that used to bind upsertIndexSQL's parameters by
// hand said, in a comment, that this was on purpose: a schema change
// should break the tests where it breaks the app.  It did not - it
// broke them at "missing argument with index 25", one file at a time,
// for a column none of them cares about.  Going through the one writer
// keeps the property they wanted (a field written to the wrong column
// still fails here) without three copies of a 24-argument list.
func seedIndexResult(t *testing.T, db *database.DB, r SearchIndexResult) {
	t.Helper()

	NewSearchIndex(db, nil, nil, slog.Default()).
		upsertBatch([]SearchIndexResult{r})

	// upsertBatch logs and swallows, which is right for a background
	// merge and useless for a fixture, so the row is checked for.
	var n int

	if err := db.QueryRowWriter(
		"SELECT COUNT(*) FROM explore_index WHERE mbid = ?", dbMBID(r.MBID),
	).Scan(&n); err != nil || n == 0 {
		t.Fatalf("seed explore_index row %q: not written (%v)", r.MBID, err)
	}
}

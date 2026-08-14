package explore

import (
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
)

// seedIndexArtist inserts an explore_index artist row, optionally
// already marked as having had its ListenBrainz discography fetched.
func seedIndexArtist(t *testing.T, db *database.DB, mbid string, discogFetched int) {
	t.Helper()

	if _, err := db.ExecContext(
		upsertIndexSQL,
		"artist", mbid, "Seeded Artist", "Seeded Artist", mbid, "",
		0, 0,
		0, "", "",
		"", "", "",
		"", "", "", "",
		1, 0,
		0, 0, 0,
		discogFetched,
	); err != nil {
		t.Fatalf("seed explore_index row for %q: %v", mbid, err)
	}
}

// TestArtistEnrichmentMarksAreIndependent is the reason these are two
// columns rather than one boolean: each fetch fails on its own, and one
// failing must not claim the other as done.
func TestArtistEnrichmentMarksAreIndependent(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, slog.Default())

	const mbid = "11111111-1111-1111-1111-111111111111"

	if mark := si.enrichmentFor(mbid); mark.Browsed || mark.Similar {
		t.Fatalf("unmarked artist reported as enriched: %+v", mark)
	}

	si.markArtistBrowsed(mbid)

	mark := si.enrichmentFor(mbid)
	if !mark.Browsed {
		t.Error("browsed_at was not recorded")
	}

	if mark.Similar {
		t.Error("marking browsed also marked similar")
	}

	// The second mark upserts onto the same row rather than replacing it.
	si.markArtistSimilar(mbid)

	mark = si.enrichmentFor(mbid)
	if !mark.Browsed || !mark.Similar {
		t.Errorf("marking similar lost the browsed mark: %+v", mark)
	}

	if !si.artistBrowsed(mbid) {
		t.Error("artistBrowsed disagreed with enrichmentFor")
	}
}

// TestUnenrichedIncludesBrowsedGap is the query change 011 turns on: an
// artist the catalog artifact already covers (discog_fetched = 1) has
// still never been browsed, and the artifact's per-artist coverage is
// graded — so "the artifact knows them" is not "we have their
// discography", and the backfill must still pick them up.
func TestUnenrichedIncludesBrowsedGap(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, slog.Default())

	const mbid = "22222222-2222-2222-2222-222222222222"

	if _, err := db.ExecContext(
		"INSERT INTO artists (name, mbid) VALUES (?, ?)", "Owned Artist", mbid,
	); err != nil {
		t.Fatalf("seed library artist: %v", err)
	}

	seedIndexArtist(t, db, mbid, 1)

	mbids := si.unenrichedLibraryArtistMBIDs(10)
	if !containsMBID(mbids, mbid) {
		t.Fatalf("artist with discog_fetched=1 but no browse was skipped: %v", mbids)
	}

	// Both marks set: nothing left to do, so it drops out entirely.
	si.markArtistBrowsed(mbid)
	si.markArtistSimilar(mbid)

	if mbids := si.unenrichedLibraryArtistMBIDs(10); containsMBID(mbids, mbid) {
		t.Errorf("fully enriched artist was returned again: %v", mbids)
	}
}

// TestUnenrichedIgnoresSimilarMark pins the other half of that query:
// the backfill no longer fetches similar artists (the artist page does,
// on view), so an artist it has finished with must drop out even though
// similar_at is still NULL.  Testing a mark nothing in the pass sets
// makes every owned artist a candidate on every run, forever — which is
// a backfill that reports progress and never converges.
func TestUnenrichedIgnoresSimilarMark(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, slog.Default())

	const mbid = "33333333-3333-3333-3333-333333333333"

	if _, err := db.ExecContext(
		"INSERT INTO artists (name, mbid) VALUES (?, ?)", "Owned Artist", mbid,
	); err != nil {
		t.Fatalf("seed library artist: %v", err)
	}

	seedIndexArtist(t, db, mbid, 1)
	si.markArtistBrowsed(mbid)

	if mbids := si.unenrichedLibraryArtistMBIDs(10); containsMBID(mbids, mbid) {
		t.Errorf("artist with no similar_at was returned again: %v", mbids)
	}
}

func containsMBID(mbids []string, want string) bool {
	for _, m := range mbids {
		if m == want {
			return true
		}
	}

	return false
}

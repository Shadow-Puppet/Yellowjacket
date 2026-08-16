package explore

import (
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
)

// TestPruneStaleLocalCrossReferences verifies that an explore_index row
// whose local_*_id points at a library row that no longer exists gets its
// in_library flag and local_*_id cleared, while a row still backed by a
// real library entity is left untouched. This is the fix for stale
// "in_library" bookkeeping surviving a rescan that removed the file it
// was tied to (upsertIndexConflictSQL only ever adds these references,
// never clears them).
func TestPruneStaleLocalCrossReferences(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, slog.Default())

	// A library artist that still exists - which now means one with a
	// file behind it.  An artist row on its own is not ownership; that
	// was the whole bug.
	database.InsertTestTrack(t, db, database.TestTrack{
		FilePath: "/music/still-owned.mp3",
		Artist:   "Still Owned",
	})

	artist, err := db.Queries.GetArtistByName(t.Context(), "Still Owned")
	if err != nil {
		t.Fatalf("read seeded artist: %v", err)
	}

	// Two explore_index artist rows: one pointing at the still-existing
	// artist, one pointing at a local ID that has since been deleted
	// (simulating a rescan that removed the owning artist).
	seedArtist := func(mbid, title string, localID int64) {
		t.Helper()

		seedIndexResult(t, db, SearchIndexResult{
			EntityType:    EntityArtist,
			MBID:          testMBID(mbid),
			Title:         title,
			ArtistName:    title,
			ArtistMBID:    testMBID(mbid),
			InLibrary:     true,
			LocalArtistID: localID,
		})
	}

	seedArtist("still-owned-mbid", "Still Owned", artist.ID)
	seedArtist("removed-mbid", "Removed Artist", 999999)

	si.pruneStaleLocalCrossReferences()

	stillOwned := si.LookupArtistByMBID(testMBID("still-owned-mbid"))
	if stillOwned == nil {
		t.Fatal("expected still-owned artist row to survive pruning")
	}

	if !stillOwned.InLibrary || stillOwned.LocalArtistID != artist.ID {
		t.Errorf(
			"still-owned artist: InLibrary=%v LocalArtistID=%d, want InLibrary=true LocalArtistID=%d",
			stillOwned.InLibrary,
			stillOwned.LocalArtistID,
			artist.ID,
		)
	}

	removed := si.LookupArtistByMBID(testMBID("removed-mbid"))
	if removed == nil {
		t.Fatal("expected removed-artist row to still exist (only cross-references cleared)")
	}

	if removed.InLibrary || removed.LocalArtistID != 0 {
		t.Errorf(
			"removed artist: InLibrary=%v LocalArtistID=%d, want InLibrary=false LocalArtistID=0",
			removed.InLibrary, removed.LocalArtistID,
		)
	}
}

// TestUnenrichedLibraryArtistMBIDs_OrdersByOwnedTrackCount verifies the
// backfill queue prioritizes artists by how many tracks the user actually
// owns, not by how many duplicate-mbid artist rows happen to exist (the
// previous "ORDER BY COUNT(*)" grouped on a.mbid, which is nearly always 1
// per artist and so wasn't really ordering by anything meaningful).
func TestUnenrichedLibraryArtistMBIDs_OrdersByOwnedTrackCount(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, slog.Default())

	seedArtistWithTracks := func(name, mbid string, trackCount int) {
		t.Helper()

		for i := range trackCount {
			database.InsertTestTrack(t, db, database.TestTrack{
				FilePath:   name + "/" + string(rune('a'+i)) + ".mp3",
				Title:      name,
				Artist:     name,
				ArtistMBID: mbid,
				LengthMs:   180000,
			})
		}
	}

	seedArtistWithTracks("Few Tracks", "few-mbid", 1)
	seedArtistWithTracks("Many Tracks", "many-mbid", 5)

	mbids := si.unenrichedLibraryArtistMBIDs(10)
	if len(mbids) != 2 {
		t.Fatalf("unenrichedLibraryArtistMBIDs() = %v, want 2 entries", mbids)
	}

	if mbids[0] != "many-mbid" {
		t.Errorf(
			"unenrichedLibraryArtistMBIDs()[0] = %q, want %q (most owned tracks first)",
			mbids[0],
			"many-mbid",
		)
	}
}

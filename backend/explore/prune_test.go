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

// TestPruneClearsInLibraryWithNoLocalID covers the fixed point: a row
// carrying in_library with a NULL local_*_id.  The upsert's conflict
// clause is `in_library = MAX(in_library, excluded.in_library)`, so it
// can only ever raise the flag, and this pass used to be gated on the id
// being present — which meant nothing in the app could clear such a row,
// ever.  It is asserted for all three entity types because the gate was
// written once and used three times, so a fix applied to one is a fix
// that looks complete.
//
// The rows are seeded with raw SQL rather than through seedIndexResult
// deliberately: upsertBatch writes a zero LocalArtistID as literal 0,
// not NULL, and 0 satisfies `IS NOT NULL` — so the old gate already
// caught that shape and a fixture built through the upsert cannot
// reproduce this at all.  NULL is what the artifact importer and any
// older writer leave behind, the column being nullable with no default.
func TestPruneClearsInLibraryWithNoLocalID(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, slog.Default())

	// A genuinely owned artist, to prove the wider gate does not simply
	// clear everything it now looks at.
	database.InsertTestTrack(t, db, database.TestTrack{
		FilePath: "/music/owned.mp3",
		Artist:   "Owned",
	})

	artist, err := db.Queries.GetArtistByName(t.Context(), "Owned")
	if err != nil {
		t.Fatalf("read seeded artist: %v", err)
	}

	seedIndexResult(t, db, SearchIndexResult{
		EntityType:    EntityArtist,
		MBID:          testMBID("owned"),
		Title:         "Owned",
		ArtistName:    "Owned",
		ArtistMBID:    testMBID("owned"),
		InLibrary:     true,
		LocalArtistID: artist.ID,
	})

	orphans := []struct {
		name       string
		entityType string
		mbid       string
	}{
		{"artist", EntityArtist, "orphan-artist"},
		{"release group", EntityReleaseGroup, "orphan-release-group"},
		{"recording", EntityRecording, "orphan-recording"},
	}

	for _, o := range orphans {
		if _, err := db.ExecContext(
			`INSERT INTO explore_index
			     (entity_type, mbid, title, artist_name, artist_mbid,
			      in_library,
			      local_artist_id, local_release_group_id, local_recording_id)
			 VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)`,
			dbEntityType(o.entityType), dbMBID(testMBID(o.mbid)), o.name, o.name,
			dbMBID(testMBID(o.mbid)),
			nil, nil, nil,
		); err != nil {
			t.Fatalf("seed %s orphan: %v", o.name, err)
		}
	}

	si.pruneStaleLocalCrossReferences()

	inLibrary := func(t *testing.T, mbid string) int {
		t.Helper()

		var flag int
		if err := db.QueryRowWriter(
			"SELECT in_library FROM explore_index WHERE mbid = ?", dbMBID(mbid),
		).Scan(&flag); err != nil {
			t.Fatalf("read in_library for %q: %v", mbid, err)
		}

		return flag
	}

	for _, o := range orphans {
		if got := inLibrary(t, testMBID(o.mbid)); got != 0 {
			t.Errorf("%s with a NULL local id: in_library = %d, want 0", o.name, got)
		}
	}

	if got := inLibrary(t, testMBID("owned")); got != 1 {
		t.Errorf("owned artist: in_library = %d, want 1 (it still has a file)", got)
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

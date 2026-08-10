package explore

import (
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
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

	// A library artist that still exists.
	artist, err := db.Queries.UpsertArtist(t.Context(), "Still Owned")
	if err != nil {
		t.Fatalf("upsert artist: %v", err)
	}

	// Two explore_index artist rows: one pointing at the still-existing
	// artist, one pointing at a local ID that has since been deleted
	// (simulating a rescan that removed the owning artist).
	seedArtist := func(mbid, title string, localID int64) {
		t.Helper()

		if _, err := db.ExecContext(
			upsertIndexSQL,
			"artist", mbid, title, title, mbid, "",
			0, 0,
			0, "", "",
			"", "", "",
			"", "", "", "",
			1, 0,
			localID, 0, 0,
			0,
		); err != nil {
			t.Fatalf("seed explore_index row for %q: %v", mbid, err)
		}
	}

	seedArtist("still-owned-mbid", "Still Owned", artist.ID)
	seedArtist("removed-mbid", "Removed Artist", 999999)

	si.pruneStaleLocalCrossReferences()

	stillOwned := si.LookupArtistByMBID("still-owned-mbid")
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

	removed := si.LookupArtistByMBID("removed-mbid")
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
	q := db.Queries
	ctx := t.Context()

	seedArtistWithTracks := func(name, mbid string, trackCount int) {
		t.Helper()

		artist, err := q.UpsertArtist(ctx, name)
		if err != nil {
			t.Fatalf("upsert artist %q: %v", name, err)
		}

		_, err = db.ExecContext("UPDATE artists SET mbid = ? WHERE id = ?", mbid, artist.ID)
		if err != nil {
			t.Fatalf("set mbid for %q: %v", name, err)
		}

		ac, err := q.UpsertArtistCredit(ctx, name)
		if err != nil {
			t.Fatalf("upsert artist credit %q: %v", name, err)
		}

		if _, err := q.CreateArtistCreditArtist(ctx, sqlcgen.CreateArtistCreditArtistParams{
			ArtistID: artist.ID,
			CreditID: ac.ID,
		}); err != nil {
			t.Fatalf("link artist credit artist %q: %v", name, err)
		}

		for i := range trackCount {
			rec, err := q.CreateRecordingFull(ctx, sqlcgen.CreateRecordingFullParams{
				Name:           name,
				ArtistCreditID: ac.ID,
			})
			if err != nil {
				t.Fatalf("create recording for %q: %v", name, err)
			}

			if _, err := q.CreateAudioFile(ctx, sqlcgen.CreateAudioFileParams{
				FilePath:           name + "/" + string(rune('a'+i)) + ".mp3",
				LengthMilliseconds: 180000,
				RecordingID:        rec.ID,
				Basename:           string(rune('a'+i)) + ".mp3",
			}); err != nil {
				t.Fatalf("create audio file for %q: %v", name, err)
			}
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

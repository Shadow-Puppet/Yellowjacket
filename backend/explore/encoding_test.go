package explore

import (
	"testing"

	"yellowjacket/backend/database"
)

// TestStoredEncodingRoundTrips walks every read path in the package
// against a row written by the real upsert.
//
// It exists because of how this storage change fails when it fails.
// MBIDs are stored as 16 raw bytes and entity types as codes - which
// took the catalog and its indexes from 780 MB to 405 MB, measured on a
// real 2,052,200-row catalog - and SQLite does not coerce between TEXT
// and BLOB. A query that still compares against a 36-character string
// therefore returns *no rows* rather than an error, and a scan into a
// plain string yields sixteen bytes of mojibake. Neither shows up as a
// failure anywhere except in a result that is quietly empty.
//
// So this is not a unit test of the encoding (that is TestMBIDRoundTrip)
// but a sweep: every query that reads the catalog, asserted to return
// something and to hand back canonical dashed ids.
func TestStoredEncodingRoundTrips(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	const (
		artist = "c0b2500e-0cef-4130-9b13-1b9d9a2f2c07"
		album  = "11111111-2222-3333-4444-555555555555"
	)

	si.upsertBatch([]SearchIndexResult{
		{
			EntityType: EntityArtist,
			MBID:       artist,
			Title:      "Radiohead",
			ArtistName: "Radiohead",
			ArtistMBID: artist,
			Popularity: 90000,
		},
		{
			EntityType:     EntityReleaseGroup,
			MBID:           album,
			Title:          "Kid A",
			ArtistName:     "Radiohead",
			ArtistMBID:     artist,
			Popularity:     80000,
			CAAReleaseMBID: album,
		},
	})

	// The stored form is the compact one, not the strings above.
	var typeMBID, typeEntity string
	if err := db.QueryRowWriter(
		"SELECT typeof(mbid), typeof(entity_type) FROM explore_index LIMIT 1",
	).Scan(&typeMBID, &typeEntity); err != nil {
		t.Fatalf("typeof: %v", err)
	}

	if typeMBID != "blob" || typeEntity != "integer" {
		t.Errorf("stored as mbid=%s entity_type=%s, want blob/integer", typeMBID, typeEntity)
	}

	si.MarkReadyIfPopulated()

	// Read paths.
	if got := si.LookupArtistByMBID(artist); got == nil {
		t.Error("LookupArtistByMBID found nothing")
	} else if got.MBID != artist || got.ArtistMBID != artist || got.EntityType != EntityArtist {
		t.Errorf("lookup artist = %+v, want dashed ids and the artist type", got)
	}

	if got := si.LookupReleaseGroupByMBID(album); got == nil {
		t.Error("LookupReleaseGroupByMBID found nothing")
	} else if got.MBID != album || got.EntityType != EntityReleaseGroup {
		t.Errorf("lookup album = %+v, want the dashed id and the release-group type", got)
	}

	if rgs := si.TopReleaseGroupsByArtist(artist, 5); len(rgs) == 0 {
		t.Error("TopReleaseGroupsByArtist found nothing")
	} else if rgs[0].MBID != album || rgs[0].ArtistMBID != artist {
		t.Errorf("top release groups[0] = %+v, want %s by %s", rgs[0], album, artist)
	}

	// The exact-match tier reads the two partial LOWER() indexes, whose
	// predicate has to agree with its WHERE clause or the seek silently
	// becomes a scan.
	if m := si.ExactMatches("radiohead", 3); len(m) == 0 {
		t.Error("ExactMatches found nothing")
	} else if m[0].MBID != artist || m[0].EntityType != EntityArtist {
		t.Errorf("exact match[0] = %+v, want the artist", m[0])
	}

	if hits := si.Search(t.Context(), "radiohead", 5); len(hits) == 0 {
		t.Error("Search found nothing")
	} else if hits[0].MBID != artist || hits[0].EntityType != EntityArtist {
		t.Errorf("search hit[0] = %+v, want the artist", hits[0])
	}

	if b := si.GetPopularityBatch([]string{artist, album}); b == nil || len(b.Popularity) != 2 {
		t.Errorf("GetPopularityBatch = %+v, want two entries", b)
	}

	if m := si.ReleaseGroupMBIDsForCAAReleaseMBIDs([]string{album}); len(m) != 1 {
		t.Errorf("CAA lookup = %v, want one entry", m)
	}
}

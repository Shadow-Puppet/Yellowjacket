package autotagservice

import (
	"encoding/json"
	"testing"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/database"
)

// seedAlbumGroup writes one album's files, its tagging item and the
// durable candidate blob the prefetch would have left behind.
//
// The candidate list is what a real one looks like in the two ways
// that decide the tier: a per-track alignment for every local track,
// and a runner-up far enough away not to count as ambiguity.
func seedAlbumGroup(
	t *testing.T,
	db *database.DB,
	groupKey string,
	tracks int,
	status string,
	score float64,
) int64 {
	t.Helper()

	for i := 1; i <= tracks; i++ {
		database.InsertTestTrack(t, db, database.TestTrack{
			FilePath:    filePathFor(groupKey, i),
			Title:       titleFor(i),
			Artist:      "Tideline",
			Album:       "Glass Harbour",
			AlbumArtist: "Tideline",
			TrackNumber: int64(i),
			LengthMs:    200000,
			LibraryID:   0,
			GroupKey:    groupKey,
		})
	}

	if _, err := db.ExecContext(`
		INSERT INTO tagging_items
		    (group_key, library_id, track_count, album_name, album_artist,
		     disc_number, status, score, best_match_release_mbid)
		VALUES (?, 0, ?, 'Glass Harbour', 'Tideline', 0, ?, ?, 'rel-1')
	`, groupKey, tracks, status, score); err != nil {
		t.Fatalf("insert tagging item: %v", err)
	}

	var albumID int64
	if err := db.QueryRowWriter(
		`SELECT album_id FROM audio_files WHERE group_key = ? LIMIT 1`, groupKey,
	).Scan(&albumID); err != nil {
		t.Fatalf("read album id: %v", err)
	}

	return albumID
}

func filePathFor(groupKey string, n int) string {
	return "/music/" + groupKey + "/0" + string(rune('0'+n)) + ".mp3"
}

func titleFor(n int) string {
	return "Track " + string(rune('0'+n))
}

// storeCandidates writes the durable blob GetCandidates would have
// cached, with `top` as the winning score.
func storeCandidates(
	t *testing.T, db *database.DB, groupKey string, tracks int, top float64,
) {
	t.Helper()

	aligns := make([]autotag.TrackAlignment, 0, tracks)
	for i := range tracks {
		aligns = append(aligns, autotag.TrackAlignment{
			Status:     autotag.AlignmentMatched,
			LocalIndex: i,
		})
	}

	cands := []autotag.Candidate{
		{
			ReleaseMBID:      "rel-1",
			ReleaseGroupMBID: "rg-1",
			Title:            "Glass Harbour",
			ArtistCredit:     "Tideline",
			TrackCount:       tracks,
			Alignments:       aligns,
			Score:            top,
		},
		{
			ReleaseMBID:      "rel-2",
			ReleaseGroupMBID: "rg-2",
			Title:            "Something Else",
			ArtistCredit:     "Another Band",
			TrackCount:       tracks,
			Score:            0.40,
		},
	}

	blob, err := json.Marshal(cands)
	if err != nil {
		t.Fatalf("marshal candidates: %v", err)
	}

	if _, err := db.ExecContext(
		`INSERT INTO tagging_candidates (group_key, candidates) VALUES (?, ?)`,
		groupKey, string(blob),
	); err != nil {
		t.Fatalf("insert candidates: %v", err)
	}
}

// A confident match is what the album page exists to surface.
func TestMatchForAlbumSurfacesAConfidentMatch(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	albumID := seedAlbumGroup(t, db, "grp-1", 8, "pending", 0.95)
	storeCandidates(t, db, "grp-1", 8, 0.95)

	got, err := svc.MatchForAlbum(albumID)
	if err != nil {
		t.Fatalf("MatchForAlbum: %v", err)
	}

	if got == nil {
		t.Fatal("no match returned for a strong candidate")
	}

	if got.Recommendation != string(autotag.RecommendationStrong) {
		t.Errorf("recommendation = %q, want strong", got.Recommendation)
	}

	// The release named is the release Apply would write — the page
	// must not offer one album and tag another.
	if got.ReleaseMBID != "rel-1" || got.Title != "Glass Harbour" {
		t.Errorf("named %q/%q, want rel-1/Glass Harbour", got.ReleaseMBID, got.Title)
	}

	if got.GroupCount != 1 {
		t.Errorf("groupCount = %d, want 1", got.GroupCount)
	}
}

// The tier is computed from the candidates, not read off the raw
// score — a high number the scorer would have capped must not reach
// the page as confidence it withheld.
func TestMatchForAlbumDoesNotTrustTheStoredScore(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	// Two tracks: below the evidence floor, so `Recommend` caps this
	// at medium however well it scores.
	albumID := seedAlbumGroup(t, db, "grp-2", 2, "pending", 0.99)
	storeCandidates(t, db, "grp-2", 2, 0.99)

	got, err := svc.MatchForAlbum(albumID)
	if err != nil {
		t.Fatalf("MatchForAlbum: %v", err)
	}

	if got != nil {
		t.Errorf("surfaced %+v for a two-track folder, want nothing", got)
	}
}

// A weak match is not worth interrupting for.
func TestMatchForAlbumStaysQuietBelowTheTier(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	albumID := seedAlbumGroup(t, db, "grp-3", 8, "pending", 0.60)
	storeCandidates(t, db, "grp-3", 8, 0.60)

	got, err := svc.MatchForAlbum(albumID)
	if err != nil {
		t.Fatalf("MatchForAlbum: %v", err)
	}

	if got != nil {
		t.Errorf("surfaced %+v for a 0.60 match, want nothing", got)
	}
}

// An album the user has already answered for is not re-offered.
//
// `confirmed` covers both a finished apply and an explicit "leave as
// is", and arguing with the second would be actively wrong.
func TestMatchForAlbumRespectsAnAnswerAlreadyGiven(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"confirmed", "skipped", "matched"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()

			db := database.NewTestDB(t)
			svc := newTestService(t, db)

			albumID := seedAlbumGroup(t, db, "grp-"+status, 8, status, 0.95)
			storeCandidates(t, db, "grp-"+status, 8, 0.95)

			got, err := svc.MatchForAlbum(albumID)
			if err != nil {
				t.Fatalf("MatchForAlbum: %v", err)
			}

			if got != nil {
				t.Errorf("surfaced %+v for a %s group, want nothing", got, status)
			}
		})
	}
}

// A folder nobody has scored yet says nothing, rather than scoring it
// now: the MusicBrainz limiter is shared with every page the user can
// open, and this runs on page load.
func TestMatchForAlbumMakesNoNetworkCallForAnUnscoredFolder(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	// No storeCandidates: the prefetch has not reached this folder.
	albumID := seedAlbumGroup(t, db, "grp-4", 8, "pending", 0.95)

	got, err := svc.MatchForAlbum(albumID)
	if err != nil {
		t.Fatalf("MatchForAlbum: %v", err)
	}

	if got != nil {
		t.Errorf("surfaced %+v with no cached candidates, want nothing", got)
	}
}

// A multi-disc album is several groups, and the count is what stops
// the page offering one button that would retag one disc of two.
func TestMatchForAlbumCountsEveryGroupOfTheAlbum(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	albumID := seedAlbumGroup(t, db, "grp-d1", 8, "pending", 0.95)
	storeCandidates(t, db, "grp-d1", 8, 0.95)

	// Disc two: same album row, its own folder and tagging group.
	for i := 1; i <= 6; i++ {
		database.InsertTestTrack(t, db, database.TestTrack{
			FilePath:    filePathFor("grp-d2", i),
			Title:       titleFor(i),
			Artist:      "Tideline",
			Album:       "Glass Harbour",
			AlbumArtist: "Tideline",
			TrackNumber: int64(i),
			DiscNumber:  2,
			LengthMs:    200000,
			LibraryID:   0,
			GroupKey:    "grp-d2",
		})
	}

	if _, err := db.ExecContext(`
		INSERT INTO tagging_items
		    (group_key, library_id, track_count, album_name, album_artist,
		     disc_number, status, score)
		VALUES ('grp-d2', 0, 6, 'Glass Harbour', 'Tideline', 2, 'pending', 0.93)
	`); err != nil {
		t.Fatalf("insert disc two: %v", err)
	}

	storeCandidates(t, db, "grp-d2", 6, 0.93)

	got, err := svc.MatchForAlbum(albumID)
	if err != nil {
		t.Fatalf("MatchForAlbum: %v", err)
	}

	if got == nil {
		t.Fatal("no match returned")
	}

	if got.GroupCount != 2 {
		t.Errorf("groupCount = %d, want 2", got.GroupCount)
	}

	// Best-first: the 0.95 disc is the one described.
	if got.GroupKey != "grp-d1" {
		t.Errorf("described %q, want the higher-scoring grp-d1", got.GroupKey)
	}
}

// An album with no local files at all — a pure catalog page — is not
// a question this can answer.
func TestMatchForAlbumSaysNothingWithoutAnAlbum(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	for _, id := range []int64{0, -1, 4242} {
		got, err := svc.MatchForAlbum(id)
		if err != nil {
			t.Fatalf("MatchForAlbum(%d): %v", id, err)
		}

		if got != nil {
			t.Errorf("MatchForAlbum(%d) = %+v, want nil", id, got)
		}
	}
}

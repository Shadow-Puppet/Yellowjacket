package autotag_test

import (
	"context"
	"log/slog"
	"testing"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/database"
)

// seedAlbum drops a minimal release_group + recordings + audio_files
// chain into the test DB, returning the group_key derived by the
// scan pipeline.
type seededAlbum struct {
	groupKey    string
	albumName   string
	libraryID   int64
	releaseMBID string // MBID set on release_groups row (empty → none)
	tracks      []seededTrack
}

type seededTrack struct {
	filePath      string
	title         string
	trackNumber   int
	lengthMillis  int64
	recordingMBID string // set a recording MBID to mark this track as "already tagged"
}

func seed(t *testing.T, db *database.DB, album seededAlbum) {
	t.Helper()

	for _, tr := range album.tracks {
		database.InsertTestTrack(t, db, database.TestTrack{
			FilePath:      tr.filePath,
			Title:         tr.title,
			Artist:        "Test Artist",
			Album:         album.albumName,
			AlbumMBID:     album.releaseMBID,
			RecordingMBID: tr.recordingMBID,
			TrackNumber:   int64(tr.trackNumber),
			LengthMs:      tr.lengthMillis,
			LibraryID:     album.libraryID,
			GroupKey:      album.groupKey,
		})
	}

	if _, err := db.ExecContext(`
		INSERT INTO tagging_items (group_key, library_id, track_count, album_name, album_artist, disc_number, status)
		VALUES (?, ?, ?, ?, ?, 0, 'pending')
	`, album.groupKey, album.libraryID, len(album.tracks), album.albumName, "Test Artist"); err != nil {
		t.Fatalf("insert tagging item: %v", err)
	}
}

func TestScorer_LocalHitSurfacesFirst(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	// Seed a local release_group WITH an MBID — this is the
	// zero-cost candidate.  Canonical tracks carry recording MBIDs
	// so the local resolver treats them as tagged candidates.
	seed(t, db, seededAlbum{
		groupKey:    "g-canonical",
		albumName:   "Good Album",
		libraryID:   0,
		releaseMBID: "rg-abcd",
		tracks: []seededTrack{
			{
				filePath: "/lib/a.mp3", title: "Song A",
				trackNumber: 1, lengthMillis: 200000,
				recordingMBID: "rec-a",
			},
			{
				filePath: "/lib/b.mp3", title: "Song B",
				trackNumber: 2, lengthMillis: 180000,
				recordingMBID: "rec-b",
			},
		},
	})

	// Seed a pending group whose album name matches the canonical.
	// Local resolver finds canonical as a candidate; MB would also
	// run in parallel (we no longer short-circuit it).  The fake
	// returns no MB hits here so local is the only ranked option.
	seed(t, db, seededAlbum{
		groupKey:  "g-pending",
		albumName: "Good Album",
		libraryID: 0,
		tracks: []seededTrack{
			{filePath: "/other/a.mp3", title: "Song A", trackNumber: 1, lengthMillis: 200000},
			{filePath: "/other/b.mp3", title: "Song B", trackNumber: 2, lengthMillis: 180000},
		},
	})

	fakeMB := &countingMBClient{onSearch: func() {}}

	scorer := autotag.NewScorer(db.Queries, fakeMB, slog.New(slog.DiscardHandler))

	result, err := scorer.ScoreGroup(context.Background(), "g-pending")
	if err != nil {
		t.Fatalf("ScoreGroup: %v", err)
	}

	if len(result.Candidates) == 0 {
		t.Fatal("no candidates")
	}

	if result.Candidates[0].ReleaseGroupMBID != "rg-abcd" {
		t.Errorf("top candidate mbid = %q, want 'rg-abcd'", result.Candidates[0].ReleaseGroupMBID)
	}
}

// TestScorer_LocalFirstSkipsMB asserts the background (prefetch)
// scoring path makes zero MusicBrainz calls when a local candidate is
// already a strong match — the whole point of ScoreGroupLocalFirst.
func TestScorer_LocalFirstSkipsMB(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	// A canonical local album (MBIDs present) that the pending group
	// matches track-for-track — the local candidate scores >= 0.90.
	seed(t, db, seededAlbum{
		groupKey:    "g-canonical",
		albumName:   "Good Album",
		releaseMBID: "rg-abcd",
		tracks: []seededTrack{
			{
				filePath: "/lib/a.mp3", title: "Song A",
				trackNumber: 1, lengthMillis: 200000, recordingMBID: "rec-a",
			},
			{
				filePath: "/lib/b.mp3", title: "Song B",
				trackNumber: 2, lengthMillis: 180000, recordingMBID: "rec-b",
			},
		},
	})
	seed(t, db, seededAlbum{
		groupKey:  "g-pending",
		albumName: "Good Album",
		tracks: []seededTrack{
			{filePath: "/other/a.mp3", title: "Song A", trackNumber: 1, lengthMillis: 200000},
			{filePath: "/other/b.mp3", title: "Song B", trackNumber: 2, lengthMillis: 180000},
		},
	})

	mbCalls := 0
	fakeMB := &countingMBClient{onSearch: func() { mbCalls++ }}

	scorer := autotag.NewScorer(db.Queries, fakeMB, slog.New(slog.DiscardHandler))

	result, err := scorer.ScoreGroupLocalFirst(context.Background(), "g-pending")
	if err != nil {
		t.Fatalf("ScoreGroupLocalFirst: %v", err)
	}

	if mbCalls != 0 {
		t.Errorf("made %d MB calls, want 0 (strong local match should skip MB)", mbCalls)
	}

	if len(result.Candidates) == 0 || result.Candidates[0].ReleaseGroupMBID != "rg-abcd" {
		t.Errorf("top candidate = %+v, want local rg-abcd", result.Candidates)
	}
}

// TestScorer_IDFirstSkipsSearch asserts that a group whose tracks
// already carry recording MBIDs resolves through the ID-first path
// — the release the recordings vote for is looked up directly and
// the fuzzy search cascade never runs.
func TestScorer_IDFirstSkipsSearch(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	seed(t, db, seededAlbum{
		groupKey:  "g-tagged",
		albumName: "Good Album",
		tracks: []seededTrack{
			{
				filePath: "/lib/a.mp3", title: "Song A",
				trackNumber: 1, lengthMillis: 200000, recordingMBID: "rec-a",
			},
			{
				filePath: "/lib/b.mp3", title: "Song B",
				trackNumber: 2, lengthMillis: 180000, recordingMBID: "rec-b",
			},
		},
	})

	fake := &idFakeClient{
		recRels: map[string][]autotag.MBReleaseRef{
			"rec-a": {{MBID: "rel-full", Status: "Official", Date: "1999"}},
			"rec-b": {{MBID: "rel-full", Status: "Official", Date: "1999"}},
		},
		releases: map[string]autotag.MBRelease{
			"rel-full": {
				MBID: "rel-full", Title: "Good Album",
				ArtistCredit: "Test Artist", Status: "Official", Country: "US",
				Tracks: []autotag.CandidateTrack{
					{Position: 1, Title: "Song A", LengthMillis: 200000, MBID: "rec-a"},
					{Position: 2, Title: "Song B", LengthMillis: 180000, MBID: "rec-b"},
				},
			},
		},
	}

	scorer := autotag.NewScorer(db.Queries, fake, slog.New(slog.DiscardHandler))

	result, err := scorer.ScoreGroup(context.Background(), "g-tagged")
	if err != nil {
		t.Fatalf("ScoreGroup: %v", err)
	}

	if fake.searches != 0 {
		t.Errorf("made %d search calls, want 0 (ID-first should skip the cascade)", fake.searches)
	}

	if len(result.Candidates) == 0 {
		t.Fatal("no candidates")
	}

	top := result.Candidates[0]
	if top.ReleaseMBID != "rel-full" || top.Provenance != "id" {
		t.Errorf(
			"top = %q via %q (%.3f), want 'rel-full' via 'id'",
			top.ReleaseMBID, top.Provenance, top.Score,
		)
	}
}

// idFakeClient serves canned recording→release lookups and counts
// search calls so the ID-first test can assert the cascade stayed
// cold.
type idFakeClient struct {
	searches int
	recRels  map[string][]autotag.MBReleaseRef
	releases map[string]autotag.MBRelease
}

func (c *idFakeClient) SearchReleaseGroups(
	_ context.Context, _ string, _ int,
) ([]autotag.MBReleaseGroupHit, int, error) {
	c.searches++

	return nil, 0, nil
}

func (c *idFakeClient) SearchRecordings(
	_ context.Context, _ string, _ int,
) ([]autotag.MBRecordingHit, int, error) {
	c.searches++

	return nil, 0, nil
}

func (c *idFakeClient) LookupRecordingReleases(
	_ context.Context, mbid string,
) ([]autotag.MBReleaseRef, error) {
	return c.recRels[mbid], nil
}

func (c *idFakeClient) BrowseReleases(
	_ context.Context, _ string,
) ([]autotag.MBRelease, error) {
	return nil, nil
}

func (c *idFakeClient) LookupRelease(
	_ context.Context, mbid string,
) (autotag.MBRelease, error) {
	rel, ok := c.releases[mbid]
	if !ok {
		return autotag.MBRelease{}, autotag.ErrGroupNotFound
	}

	return rel, nil
}

func (c *idFakeClient) LookupReleaseGroup(
	_ context.Context, _ string,
) (autotag.MBReleaseGroupHit, error) {
	return autotag.MBReleaseGroupHit{}, nil
}

func (c *idFakeClient) SearchReleaseGroupsLocal(
	_ context.Context, _ string, _ int,
) ([]autotag.MBReleaseGroupHit, bool) {
	return nil, false
}

func TestScorer_PersistScoreWritesTopMatch(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	seed(t, db, seededAlbum{
		groupKey:    "g-canonical",
		albumName:   "Good Album",
		releaseMBID: "rg-abcd",
		tracks: []seededTrack{
			{
				filePath: "/lib/a.mp3", title: "Song A",
				trackNumber: 1, lengthMillis: 200000,
				recordingMBID: "rec-a",
			},
		},
	})
	seed(t, db, seededAlbum{
		groupKey:  "g-pending",
		albumName: "Good Album",
		tracks: []seededTrack{
			{filePath: "/other/a.mp3", title: "Song A", trackNumber: 1, lengthMillis: 200000},
		},
	})

	scorer := autotag.NewScorer(
		db.Queries, nil, slog.New(slog.DiscardHandler),
	)

	result, err := scorer.ScoreGroup(context.Background(), "g-pending")
	if err != nil {
		t.Fatalf("ScoreGroup: %v", err)
	}

	if err := scorer.PersistScore(context.Background(), result); err != nil {
		t.Fatalf("PersistScore: %v", err)
	}

	got, err := db.Queries.GetTaggingItem(context.Background(), "g-pending")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	// PersistScore records the pill score + best match but must leave
	// the review status untouched so the folder stays in the queue.
	if got.Status != "pending" {
		t.Errorf("status = %q, want 'pending' (PersistScore must not flip status)", got.Status)
	}

	if !got.BestMatchReleaseMbid.Valid || got.BestMatchReleaseMbid.String != "rg-abcd" {
		t.Errorf("best_match_release_mbid = %+v, want rg-abcd", got.BestMatchReleaseMbid)
	}

	if !got.Score.Valid || got.Score.Float64 < 0.9 { //nolint:mnd
		t.Errorf("score = %+v, want >= 0.9", got.Score)
	}
}

func TestScorer_GroupNotFound(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	scorer := autotag.NewScorer(db.Queries, nil, slog.New(slog.DiscardHandler))

	_, err := scorer.ScoreGroup(context.Background(), "nonexistent")
	if err == nil || err.Error() != autotag.ErrGroupNotFound.Error() {
		t.Fatalf("err = %v, want ErrGroupNotFound", err)
	}
}

// countingMBClient is a fake that increments a counter whenever
// the scorer would have made an MB call.  Used to assert the
// zero-network-call path stays zero.
type countingMBClient struct {
	onSearch func()
}

func (c *countingMBClient) SearchReleaseGroups(
	_ context.Context, _ string, _ int,
) ([]autotag.MBReleaseGroupHit, int, error) {
	c.onSearch()

	return nil, 0, nil
}

func (c *countingMBClient) BrowseReleases(
	_ context.Context, _ string,
) ([]autotag.MBRelease, error) {
	c.onSearch()

	return nil, nil
}

func (c *countingMBClient) SearchRecordings(
	_ context.Context, _ string, _ int,
) ([]autotag.MBRecordingHit, int, error) {
	c.onSearch()

	return nil, 0, nil
}

func (c *countingMBClient) LookupRecordingReleases(
	_ context.Context, _ string,
) ([]autotag.MBReleaseRef, error) {
	c.onSearch()

	return nil, nil
}

func (c *countingMBClient) LookupRelease(_ context.Context, _ string) (autotag.MBRelease, error) {
	c.onSearch()

	return autotag.MBRelease{}, nil
}

func (c *countingMBClient) LookupReleaseGroup(
	_ context.Context,
	_ string,
) (autotag.MBReleaseGroupHit, error) {
	c.onSearch()

	return autotag.MBReleaseGroupHit{}, nil
}

// SearchReleaseGroupsLocal is not a network call — it never counts
// against the zero-network-call assertions this fake exists for.
func (c *countingMBClient) SearchReleaseGroupsLocal(
	_ context.Context, _ string, _ int,
) ([]autotag.MBReleaseGroupHit, bool) {
	return nil, false
}

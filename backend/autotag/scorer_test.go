package autotag_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
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

	ctx := db.Ctx
	q := db.Queries

	ac, err := q.UpsertArtistCredit(ctx, "Test Artist")
	if err != nil {
		t.Fatalf("upsert ac: %v", err)
	}

	rg, err := q.UpsertReleaseGroup(ctx, sqlcgen.UpsertReleaseGroupParams{
		Name:                album.albumName,
		AlbumArtistCreditID: sql.NullInt64{Int64: ac.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("upsert rg: %v", err)
	}

	if album.releaseMBID != "" {
		if _, err := db.ExecContext(
			`UPDATE release_groups SET mbid = ? WHERE id = ?`,
			album.releaseMBID, rg.ID,
		); err != nil {
			t.Fatalf("set rg mbid: %v", err)
		}
	}

	for _, tr := range album.tracks {
		rec, err := q.CreateRecordingFull(ctx, sqlcgen.CreateRecordingFullParams{
			Name:           tr.title,
			ArtistCreditID: ac.ID,
			TrackNumber:    sql.NullInt64{Int64: int64(tr.trackNumber), Valid: true},
		})
		if err != nil {
			t.Fatalf("create recording: %v", err)
		}

		if tr.recordingMBID != "" {
			if _, err := db.ExecContext(
				`UPDATE recordings SET mbid = ? WHERE id = ?`,
				tr.recordingMBID, rec.ID,
			); err != nil {
				t.Fatalf("set recording mbid: %v", err)
			}
		}

		if _, err := q.CreateReleaseGroupRecording(ctx, sqlcgen.CreateReleaseGroupRecordingParams{
			ReleaseGroupID: rg.ID,
			RecordingID:    rec.ID,
			TrackNumber:    sql.NullInt64{Int64: int64(tr.trackNumber), Valid: true},
		}); err != nil {
			t.Fatalf("link rg recording: %v", err)
		}

		if _, err := q.CreateAudioFileWithGroupKey(ctx, sqlcgen.CreateAudioFileWithGroupKeyParams{
			FilePath:           tr.filePath,
			LengthMilliseconds: tr.lengthMillis,
			FileTypeID:         0,
			RecordingID:        rec.ID,
			Basename:           tr.filePath,
			LibraryID:          album.libraryID,
			GroupKey:           album.groupKey,
			TagStatus:          "untagged",
		}); err != nil {
			t.Fatalf("create audio file: %v", err)
		}
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

func TestScorer_PersistBestWritesMatched(t *testing.T) {
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

	if err := scorer.PersistBest(context.Background(), result); err != nil {
		t.Fatalf("PersistBest: %v", err)
	}

	got, err := db.Queries.GetTaggingItem(context.Background(), "g-pending")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	if got.Status != "matched" {
		t.Errorf("status = %q, want 'matched'", got.Status)
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

func (c *countingMBClient) LookupArtist(_ context.Context, _ string) (string, error) {
	c.onSearch()

	return "", nil
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

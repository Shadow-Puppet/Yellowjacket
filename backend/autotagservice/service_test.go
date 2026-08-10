package autotagservice

import (
	"database/sql"
	"errors"
	"log/slog"
	"testing"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
)

// newTestService builds a Service with just enough wired up for
// SplitMixedFolder — no MB client, no tag writer.  Constructed
// directly (bypassing NewService) since this package's tests live
// inside the package and don't need the explore/tagwriter
// dependencies that method never touches.
func newTestService(t *testing.T, db *database.DB) *Service {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)

	return &Service{
		db:     db,
		scorer: autotag.NewScorer(db.Queries, nil, logger),
		logger: logger,
		ctx:    db.Ctx,
	}
}

// seedMixedBagFolder drops one physical folder (single group_key)
// containing two 2-track clusters (different album/album-artist tags
// each) plus one leftover track with no album tag at all — the shape
// SplitMixedFolder is meant to untangle.
func seedMixedBagFolder(t *testing.T, db *database.DB, groupKey string, libraryID int64) {
	t.Helper()

	ctx := db.Ctx
	q := db.Queries

	addTrack := func(filePath, title, artist, album, albumArtist string, trackNum int) {
		ac, err := q.UpsertArtistCredit(ctx, artist)
		if err != nil {
			t.Fatalf("upsert artist credit: %v", err)
		}

		rec, err := q.CreateRecordingFull(ctx, sqlcgen.CreateRecordingFullParams{
			Name:           title,
			ArtistCreditID: ac.ID,
			TrackNumber:    sql.NullInt64{Int64: int64(trackNum), Valid: true},
		})
		if err != nil {
			t.Fatalf("create recording: %v", err)
		}

		if album != "" {
			albumArtistAC, err := q.UpsertArtistCredit(ctx, albumArtist)
			if err != nil {
				t.Fatalf("upsert album artist credit: %v", err)
			}

			rg, err := q.UpsertReleaseGroup(ctx, sqlcgen.UpsertReleaseGroupParams{
				Name:                album,
				AlbumArtistCreditID: sql.NullInt64{Int64: albumArtistAC.ID, Valid: true},
			})
			if err != nil {
				t.Fatalf("upsert release group: %v", err)
			}

			if _, err := q.CreateReleaseGroupRecording(
				ctx,
				sqlcgen.CreateReleaseGroupRecordingParams{
					ReleaseGroupID: rg.ID,
					RecordingID:    rec.ID,
					TrackNumber:    sql.NullInt64{Int64: int64(trackNum), Valid: true},
				},
			); err != nil {
				t.Fatalf("link release group recording: %v", err)
			}
		}

		if _, err := q.CreateAudioFileWithGroupKey(ctx, sqlcgen.CreateAudioFileWithGroupKeyParams{
			FilePath:           filePath,
			LengthMilliseconds: 200000,
			FileTypeID:         0,
			RecordingID:        rec.ID,
			Basename:           filePath,
			LibraryID:          libraryID,
			GroupKey:           groupKey,
			TagStatus:          "untagged",
		}); err != nil {
			t.Fatalf("create audio file: %v", err)
		}
	}

	addTrack("/junk/01.mp3", "Song A1", "Artist One", "Album One", "Artist One", 1)
	addTrack("/junk/02.mp3", "Song A2", "Artist One", "Album One", "Artist One", 2)
	addTrack("/junk/03.mp3", "Song B1", "Artist Two", "Album Two", "Artist Two", 1)
	addTrack("/junk/04.mp3", "Song B2", "Artist Two", "Album Two", "Artist Two", 2)
	addTrack("/junk/05.mp3", "Lone Song", "Artist Three", "", "", 1)

	if _, err := db.ExecContext(`
		INSERT INTO tagging_items (group_key, library_id, track_count, album_name, album_artist, disc_number, status)
		VALUES (?, ?, 5, '', '', 0, 'pending')
	`, groupKey, libraryID); err != nil {
		t.Fatalf("insert tagging item: %v", err)
	}
}

// seedCoherentAlbum drops a single-artist, single-album folder — the
// negative case for the mixed-bag triage query.
func seedCoherentAlbum(t *testing.T, db *database.DB, groupKey string, libraryID int64) {
	t.Helper()

	ctx := db.Ctx
	q := db.Queries

	ac, err := q.UpsertArtistCredit(ctx, "The Beatles")
	if err != nil {
		t.Fatalf("upsert artist credit: %v", err)
	}

	rg, err := q.UpsertReleaseGroup(ctx, sqlcgen.UpsertReleaseGroupParams{
		Name:                "Abbey Road",
		AlbumArtistCreditID: sql.NullInt64{Int64: ac.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("upsert release group: %v", err)
	}

	titles := []string{"Come Together", "Something", "Maxwell's Silver Hammer", "Oh! Darling"}
	for i, title := range titles {
		rec, err := q.CreateRecordingFull(ctx, sqlcgen.CreateRecordingFullParams{
			Name:           title,
			ArtistCreditID: ac.ID,
			TrackNumber:    sql.NullInt64{Int64: int64(i + 1), Valid: true},
		})
		if err != nil {
			t.Fatalf("create recording: %v", err)
		}

		if _, err := q.CreateReleaseGroupRecording(ctx, sqlcgen.CreateReleaseGroupRecordingParams{
			ReleaseGroupID: rg.ID,
			RecordingID:    rec.ID,
			TrackNumber:    sql.NullInt64{Int64: int64(i + 1), Valid: true},
		}); err != nil {
			t.Fatalf("link release group recording: %v", err)
		}

		if _, err := q.CreateAudioFileWithGroupKey(ctx, sqlcgen.CreateAudioFileWithGroupKeyParams{
			FilePath:           groupKey + "/" + title + ".mp3",
			LengthMilliseconds: 200000,
			FileTypeID:         0,
			RecordingID:        rec.ID,
			Basename:           title + ".mp3",
			LibraryID:          libraryID,
			GroupKey:           groupKey,
			TagStatus:          "untagged",
		}); err != nil {
			t.Fatalf("create audio file: %v", err)
		}
	}

	if _, err := db.ExecContext(`
		INSERT INTO tagging_items (group_key, library_id, track_count, album_name, album_artist, disc_number, status)
		VALUES (?, ?, 4, 'Abbey Road', 'The Beatles', 0, 'pending')
	`, groupKey, libraryID); err != nil {
		t.Fatalf("insert tagging item: %v", err)
	}
}

func TestListPendingFolders_FlagsLikelyMixedBag(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedMixedBagFolder(t, db, "g-junk", 0)
	seedCoherentAlbum(t, db, "g-abbey-road", 0)

	s := newTestService(t, db)

	items, err := s.ListPendingFolders(0)
	if err != nil {
		t.Fatalf("ListPendingFolders: %v", err)
	}

	got := make(map[string]bool, len(items))
	for _, it := range items {
		got[it.GroupKey] = it.LikelyMixedBag
	}

	if !got["g-junk"] {
		t.Error("expected g-junk (no artist/album consensus) to be flagged LikelyMixedBag")
	}

	if got["g-abbey-road"] {
		t.Error(
			"expected g-abbey-road (coherent single-artist album) to NOT be flagged LikelyMixedBag",
		)
	}
}

func TestSplitMixedFolder_CarvesOutClustersAndSingletons(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedMixedBagFolder(t, db, "g-junk", 0)

	s := newTestService(t, db)

	items, err := s.SplitMixedFolder("g-junk")
	if err != nil {
		t.Fatalf("SplitMixedFolder: %v", err)
	}

	// Every track leaves the parent: 2 clustered groups (2 tracks
	// each) + 1 singleton for the unclustered "Lone Song" track.  The
	// parent is now empty and must not survive as a 4th item.
	if len(items) != 3 { //nolint:mnd
		t.Fatalf("expected 3 resulting groups, got %d: %+v", len(items), items)
	}

	for _, it := range items {
		if it.GroupKey == "g-junk" {
			t.Fatal("expected the original group to be fully drained and removed")
		}

		if !it.Synthetic {
			t.Errorf("child group %q: Synthetic = false, want true", it.GroupKey)
		}
	}

	var (
		clustered []PendingItem
		singleton *PendingItem
	)

	for i, it := range items {
		if it.TrackCount == 1 {
			singleton = &items[i]

			continue
		}

		clustered = append(clustered, it)
	}

	if singleton == nil {
		t.Fatal("expected a singleton child for the unclustered Lone Song track")
	}

	if singleton.AlbumName != "" {
		t.Errorf(
			"singleton child album_name = %q, want empty (Lone Song had no album tag)",
			singleton.AlbumName,
		)
	}

	if len(clustered) != 2 { //nolint:mnd
		t.Fatalf("expected 2 clustered children, got %d", len(clustered))
	}

	seenAlbums := map[string]bool{}

	for _, c := range clustered {
		if c.TrackCount != 2 { //nolint:mnd
			t.Errorf("child group %q: track_count = %d, want 2", c.GroupKey, c.TrackCount)
		}

		seenAlbums[c.AlbumName] = true
	}

	if !seenAlbums["Album One"] || !seenAlbums["Album Two"] {
		t.Errorf("expected children for Album One and Album Two, got %+v", clustered)
	}

	// The physical file paths must be untouched — only group_key
	// reassignment happened, no files moved on disk.
	locals, err := s.scorer.LocalTracksForGroup(db.Ctx, clustered[0].GroupKey)
	if err != nil {
		t.Fatalf("load synthetic group tracks: %v", err)
	}

	for _, l := range locals {
		if l.FilePath == "" {
			t.Error("expected non-empty file path preserved on the synthetic group's tracks")
		}
	}
}

func TestSplitMixedFolder_NothingToClusterErrors(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	if _, err := db.Queries.CreateAudioFileWithGroupKey(
		db.Ctx,
		sqlcgen.CreateAudioFileWithGroupKeyParams{
			FilePath:    "/coherent/01.mp3",
			FileTypeID:  0,
			RecordingID: mustCreateRecording(t, db, "Track"),
			Basename:    "01.mp3",
			LibraryID:   0,
			GroupKey:    "g-coherent",
			TagStatus:   "untagged",
		},
	); err != nil {
		t.Fatalf("create audio file: %v", err)
	}

	if _, err := db.ExecContext(`
		INSERT INTO tagging_items (group_key, library_id, track_count, album_name, album_artist, disc_number, status)
		VALUES ('g-coherent', 0, 1, '', '', 0, 'pending')
	`); err != nil {
		t.Fatalf("insert tagging item: %v", err)
	}

	s := newTestService(t, db)

	if _, err := s.SplitMixedFolder("g-coherent"); !errors.Is(err, errNothingToSplit) {
		t.Fatalf("err = %v, want errNothingToSplit", err)
	}
}

func TestListPendingFolders_PrunesOrphanedEntries(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	// A real, live folder — must survive.
	if _, err := db.Queries.CreateAudioFileWithGroupKey(
		db.Ctx,
		sqlcgen.CreateAudioFileWithGroupKeyParams{
			FilePath:    "/live/01.mp3",
			FileTypeID:  0,
			RecordingID: mustCreateRecording(t, db, "Track"),
			Basename:    "01.mp3",
			LibraryID:   0,
			GroupKey:    "g-live",
			TagStatus:   "untagged",
		},
	); err != nil {
		t.Fatalf("create audio file: %v", err)
	}

	if _, err := db.ExecContext(`
		INSERT INTO tagging_items (group_key, library_id, track_count, album_name, album_artist, disc_number, status)
		VALUES ('g-live', 0, 1, '', '', 0, 'pending')
	`); err != nil {
		t.Fatalf("insert live tagging item: %v", err)
	}

	// An orphaned row: no audio_files row points at this group_key
	// any more (the file was deleted/moved and the bookkeeping that's
	// supposed to clean this up never ran) — this is exactly the
	// "old/nonexistent" entry the review UI shouldn't show.
	if _, err := db.ExecContext(`
		INSERT INTO tagging_items (group_key, library_id, track_count, album_name, album_artist, disc_number, status)
		VALUES ('g-orphan', 0, 3, 'Ghost Album', 'Ghost Artist', 0, 'pending')
	`); err != nil {
		t.Fatalf("insert orphaned tagging item: %v", err)
	}

	s := newTestService(t, db)

	items, err := s.ListPendingFolders(0)
	if err != nil {
		t.Fatalf("ListPendingFolders: %v", err)
	}

	got := make(map[string]bool, len(items))
	for _, it := range items {
		got[it.GroupKey] = true
	}

	if !got["g-live"] {
		t.Error("expected g-live (has a real audio_files row) to remain listed")
	}

	if got["g-orphan"] {
		t.Error("expected g-orphan (no matching audio_files rows) to be pruned, not listed")
	}

	if _, err := db.Queries.GetTaggingItem(db.Ctx, "g-orphan"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected g-orphan row to be deleted from tagging_items, got err=%v", err)
	}
}

func mustCreateRecording(t *testing.T, db *database.DB, title string) int64 {
	t.Helper()

	ac, err := db.Queries.UpsertArtistCredit(db.Ctx, "Artist")
	if err != nil {
		t.Fatalf("upsert artist credit: %v", err)
	}

	rec, err := db.Queries.CreateRecordingFull(db.Ctx, sqlcgen.CreateRecordingFullParams{
		Name:           title,
		ArtistCreditID: ac.ID,
	})
	if err != nil {
		t.Fatalf("create recording: %v", err)
	}

	return rec.ID
}

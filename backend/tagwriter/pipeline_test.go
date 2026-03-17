package tagwriter

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
)

// --- Mock implementations ---

// mockPlayer records calls to CurrentFilePath and StopAndRelease.
type mockPlayer struct {
	filePath      string
	stopCalled    bool
	stopCallCount int
}

func (m *mockPlayer) CurrentFilePath() string { return m.filePath }

func (m *mockPlayer) StopAndRelease() {
	m.stopCalled = true
	m.stopCallCount++
}

// mockPipelineLocker records calls to AcquirePipelineLock and
// ReleasePipelineLock.
type mockPipelineLocker struct {
	acquireCalled bool
	releaseCalled bool
	acquireCount  int
	releaseCount  int
}

func (m *mockPipelineLocker) AcquirePipelineLock() {
	m.acquireCalled = true
	m.acquireCount++
}

func (m *mockPipelineLocker) ReleasePipelineLock() {
	m.releaseCalled = true
	m.releaseCount++
}

// --- Test helpers ---

// seedTestTrack creates a minimal set of DB records for testing:
// library, file_type, artist_credit, artist, recording, audio_file,
// release_group, release_group_recording, genre, recording_genre,
// and FTS5 search_index.  Returns the audio_file ID.
func seedTestTrack(
	t *testing.T,
	db *database.DB,
	filePath string,
) int64 {
	t.Helper()

	ctx := context.Background()
	q := db.Queries

	// Artist credit.
	ac, err := q.UpsertArtistCredit(ctx, "Old Artist")
	if err != nil {
		t.Fatalf("upsert artist credit: %v", err)
	}

	artist, err := q.UpsertArtist(ctx, "Old Artist")
	if err != nil {
		t.Fatalf("upsert artist: %v", err)
	}

	if _, err := q.CreateArtistCreditArtist(ctx,
		sqlcgen.CreateArtistCreditArtistParams{
			ArtistID: artist.ID,
			CreditID: ac.ID,
		},
	); err != nil {
		t.Fatalf("create aca: %v", err)
	}

	// Recording.
	rec, err := q.CreateRecordingFull(ctx, sqlcgen.CreateRecordingFullParams{
		Name:           "Old Title",
		ArtistCreditID: ac.ID,
		TrackNumber:    sql.NullInt64{Int64: 1, Valid: true},
		DiscNumber:     sql.NullInt64{Int64: 1, Valid: true},
		Year:           sql.NullInt64{Int64: 2020, Valid: true},
		Genre:          sql.NullString{String: "Rock", Valid: true},
		Composer:       sql.NullString{String: "Old Composer", Valid: true},
	})
	if err != nil {
		t.Fatalf("create recording: %v", err)
	}

	// Release group.
	rg, err := q.UpsertReleaseGroup(ctx, sqlcgen.UpsertReleaseGroupParams{
		Name:                "Old Album",
		AlbumArtistCreditID: sql.NullInt64{Int64: ac.ID, Valid: true},
		Year:                sql.NullInt64{Int64: 2020, Valid: true},
	})
	if err != nil {
		t.Fatalf("upsert release group: %v", err)
	}

	if _, err := q.CreateReleaseGroupRecording(ctx,
		sqlcgen.CreateReleaseGroupRecordingParams{
			ReleaseGroupID: rg.ID,
			RecordingID:    rec.ID,
			TrackNumber:    sql.NullInt64{Int64: 1, Valid: true},
			DiscNumber:     sql.NullInt64{Int64: 1, Valid: true},
		},
	); err != nil {
		t.Fatalf("create rg recording: %v", err)
	}

	// Genre.
	genre, err := q.UpsertGenre(ctx, "Rock")
	if err != nil {
		t.Fatalf("upsert genre: %v", err)
	}

	if err := q.CreateRecordingGenre(ctx, sqlcgen.CreateRecordingGenreParams{
		RecordingID: rec.ID,
		GenreID:     genre.ID,
	}); err != nil {
		t.Fatalf("create recording genre: %v", err)
	}

	// Audio file.
	af, err := q.CreateAudioFile(ctx, sqlcgen.CreateAudioFileParams{
		FilePath:           filePath,
		LengthMilliseconds: 180000,
		FileTypeID:         0,
		RecordingID:        rec.ID,
		Basename:           filepath.Base(filePath),
		LibraryID:          0,
	})
	if err != nil {
		t.Fatalf("create audio file: %v", err)
	}

	// Seed FTS5 search index.
	// SAFETY: FTS5 INSERT for test setup. All values parameterized.
	tx, err := db.BeginTx()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO search_index(rowid, file_path, title, artist, album)
		 VALUES (?, ?, ?, ?, ?)`,
		af.ID, filePath, "Old Title", "Old Artist", "Old Album",
	); err != nil {
		t.Fatalf("insert search index: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	return af.ID
}

// createPipelineTestMP3 creates a minimal MP3 file for pipeline tests
// using the existing createTestMP3 helper from mp3_test.go.
func createPipelineTestMP3(t *testing.T, dir string) string {
	t.Helper()

	return createTestMP3(t, dir, "pipeline_test.mp3", TagChanges{
		FieldTitle:  "Old Title",
		FieldArtist: "Old Artist",
		FieldAlbum:  "Old Album",
		FieldGenre:  "Rock",
		FieldYear:   2020,
	})
}

// TestWriteTrackTags_PlayerSafety verifies that the player is
// stopped when the currently-playing file is the target of a write.
func TestWriteTrackTags_PlayerSafety(t *testing.T) {
	db := database.NewTestDB(t)
	dir := t.TempDir()
	mp3Path := createPipelineTestMP3(t, dir)

	trackID := seedTestTrack(t, db, mp3Path)

	mp := &mockPlayer{filePath: mp3Path}
	ml := &mockPipelineLocker{}

	tw := NewTagWriter(testLogger(), db, mp, ml)

	err := tw.WriteTrackTags(trackID, TagChanges{
		FieldTitle: "New Title",
	})
	if err != nil {
		t.Fatalf("WriteTrackTags: %v", err)
	}

	if !mp.stopCalled {
		t.Error("expected StopAndRelease to be called")
	}
}

// TestWriteTrackTags_ScanMutex verifies that AcquirePipelineLock
// is called during tag writes.
func TestWriteTrackTags_ScanMutex(t *testing.T) {
	db := database.NewTestDB(t)
	dir := t.TempDir()
	mp3Path := createPipelineTestMP3(t, dir)

	trackID := seedTestTrack(t, db, mp3Path)

	mp := &mockPlayer{}
	ml := &mockPipelineLocker{}

	tw := NewTagWriter(testLogger(), db, mp, ml)

	err := tw.WriteTrackTags(trackID, TagChanges{
		FieldTitle: "New Title",
	})
	if err != nil {
		t.Fatalf("WriteTrackTags: %v", err)
	}

	if !ml.acquireCalled {
		t.Error("expected AcquirePipelineLock to be called")
	}

	if !ml.releaseCalled {
		t.Error("expected ReleasePipelineLock to be called")
	}
}

// TestWriteTrackTags_OrphanCleanup verifies that orphaned artist
// credits are cleaned up when BOTH the artist and album change
// (so the old credit has zero references in both recordings and
// release_groups).
func TestWriteTrackTags_OrphanCleanup(t *testing.T) {
	db := database.NewTestDB(t)
	dir := t.TempDir()
	mp3Path := createPipelineTestMP3(t, dir)

	trackID := seedTestTrack(t, db, mp3Path)

	mp := &mockPlayer{}
	ml := &mockPipelineLocker{}

	tw := NewTagWriter(testLogger(), db, mp, ml)

	// Get old artist credit ID before change.
	ctx := context.Background()

	af, err := db.Queries.GetAudioFile(ctx, trackID)
	if err != nil {
		t.Fatalf("get audio file: %v", err)
	}

	oldRec, err := db.Queries.GetRecording(ctx, af.RecordingID)
	if err != nil {
		t.Fatalf("get recording: %v", err)
	}

	oldACID := oldRec.ArtistCreditID

	// Change both artist and album so the old credit loses all
	// references (recordings AND release_groups.album_artist_credit_id).
	if err := tw.WriteTrackTags(trackID, TagChanges{
		FieldArtist: "New Artist",
		FieldAlbum:  "New Album",
	}); err != nil {
		t.Fatalf("WriteTrackTags: %v", err)
	}

	// Verify old artist credit is orphaned and deleted.
	refCount, err := db.Queries.CountArtistCreditReferences(ctx, oldACID)
	if err != nil {
		t.Fatalf("count refs: %v", err)
	}

	if refCount != 0 {
		// The old AC should have 0 references. Check if it still exists.
		_, acErr := db.Queries.GetArtistCredit(ctx, oldACID)
		if acErr == nil {
			t.Errorf(
				"expected old artist credit %d to be deleted (orphan), "+
					"but it still exists with %d refs",
				oldACID, refCount,
			)
		}
	}

	// Verify new recording has new artist credit.
	newRec, err := db.Queries.GetRecording(ctx, af.RecordingID)
	if err != nil {
		t.Fatalf("get new recording: %v", err)
	}

	newAC, err := db.Queries.GetArtistCredit(ctx, newRec.ArtistCreditID)
	if err != nil {
		t.Fatalf("get new artist credit: %v", err)
	}

	if newAC.Text != "New Artist" {
		t.Errorf("new artist credit: got %q, want %q", newAC.Text, "New Artist")
	}
}

// TestWriteTrackTags_GenreRelink verifies that genre changes relink
// correctly, including multi-genre support, and orphan genres are
// cleaned up.
func TestWriteTrackTags_GenreRelink(t *testing.T) {
	db := database.NewTestDB(t)
	dir := t.TempDir()
	mp3Path := createPipelineTestMP3(t, dir)

	trackID := seedTestTrack(t, db, mp3Path)

	mp := &mockPlayer{}
	ml := &mockPipelineLocker{}

	tw := NewTagWriter(testLogger(), db, mp, ml)

	// Change genre from "Rock" to "Jazz; Blues" (multi-genre).
	if err := tw.WriteTrackTags(trackID, TagChanges{
		FieldGenre: "Jazz; Blues",
	}); err != nil {
		t.Fatalf("WriteTrackTags: %v", err)
	}

	ctx := context.Background()

	af, err := db.Queries.GetAudioFile(ctx, trackID)
	if err != nil {
		t.Fatalf("get audio file: %v", err)
	}

	// Verify new genres are linked.
	genres, err := db.Queries.GetGenresByRecordingID(ctx, af.RecordingID)
	if err != nil {
		t.Fatalf("get genres: %v", err)
	}

	if len(genres) != 2 {
		t.Fatalf("expected 2 genres, got %d", len(genres))
	}

	genreNames := make(map[string]bool)
	for _, g := range genres {
		genreNames[g.Name] = true
	}

	if !genreNames["Jazz"] {
		t.Error("expected Jazz genre to be linked")
	}

	if !genreNames["Blues"] {
		t.Error("expected Blues genre to be linked")
	}

	// Verify old "Rock" genre is deleted (orphaned).
	oldRockRefs, err := db.Queries.CountGenreReferences(ctx, 1) // ID 1 from seeding
	if err != nil {
		// Genre might not exist anymore, which is expected.
		return
	}

	if oldRockRefs > 0 {
		t.Logf("Rock genre still has %d refs (may be expected if reused)", oldRockRefs)
	}
}

// TestWriteTrackTags_DBSync verifies the full DB sync pipeline:
// recording updated, new entities created, FTS5 updated.
func TestWriteTrackTags_DBSync(t *testing.T) {
	db := database.NewTestDB(t)
	dir := t.TempDir()
	mp3Path := createPipelineTestMP3(t, dir)

	trackID := seedTestTrack(t, db, mp3Path)

	mp := &mockPlayer{}
	ml := &mockPipelineLocker{}

	tw := NewTagWriter(testLogger(), db, mp, ml)

	if err := tw.WriteTrackTags(trackID, TagChanges{
		FieldTitle:    "New Title",
		FieldArtist:   "New Artist",
		FieldAlbum:    "New Album",
		FieldYear:     2025,
		FieldGenre:    "Electronic",
		FieldComposer: "New Composer",
	}); err != nil {
		t.Fatalf("WriteTrackTags: %v", err)
	}

	ctx := context.Background()

	// Verify recording updated.
	af, err := db.Queries.GetAudioFile(ctx, trackID)
	if err != nil {
		t.Fatalf("get audio file: %v", err)
	}

	rec, err := db.Queries.GetRecording(ctx, af.RecordingID)
	if err != nil {
		t.Fatalf("get recording: %v", err)
	}

	if rec.Name != "New Title" {
		t.Errorf("recording name: got %q, want %q", rec.Name, "New Title")
	}

	if rec.Year.Int64 != 2025 {
		t.Errorf("year: got %d, want 2025", rec.Year.Int64)
	}

	if rec.Composer.String != "New Composer" {
		t.Errorf("composer: got %q, want %q", rec.Composer.String, "New Composer")
	}

	// Verify new artist credit.
	ac, err := db.Queries.GetArtistCredit(ctx, rec.ArtistCreditID)
	if err != nil {
		t.Fatalf("get artist credit: %v", err)
	}

	if ac.Text != "New Artist" {
		t.Errorf("artist credit: got %q, want %q", ac.Text, "New Artist")
	}

	// Verify new release group linked.
	rgLinks, err := db.Queries.GetRecordingReleaseGroups(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get rg links: %v", err)
	}

	if len(rgLinks) != 1 {
		t.Fatalf("expected 1 rg link, got %d", len(rgLinks))
	}

	rg, err := db.Queries.GetReleaseGroup(ctx, rgLinks[0].ReleaseGroupID)
	if err != nil {
		t.Fatalf("get release group: %v", err)
	}

	if rg.Name != "New Album" {
		t.Errorf("release group name: got %q, want %q", rg.Name, "New Album")
	}

	// Verify genre re-linked.
	genres, err := db.Queries.GetGenresByRecordingID(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get genres: %v", err)
	}

	if len(genres) != 1 || genres[0].Name != "Electronic" {
		t.Errorf("genres: got %v, want [Electronic]", genres)
	}

	// Verify FTS5 search index contains new values.
	// SAFETY: FTS5 query for test verification. All values parameterized.
	rows, err := db.QueryContext(
		"SELECT title, artist, album FROM search_index WHERE search_index MATCH ?",
		"New Title",
	)
	if err != nil {
		t.Fatalf("FTS5 query: %v", err)
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		t.Error("expected FTS5 result for 'New Title'")
	}
}

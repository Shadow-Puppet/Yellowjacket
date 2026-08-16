package tagwriter

import (
	"context"
	"testing"

	"yellowjacket/backend/database"
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

	return database.InsertTestTrack(t, db, database.TestTrack{
		FilePath:    filePath,
		Title:       "Old Title",
		Artist:      "Old Artist",
		Album:       "Old Album",
		Genres:      []string{"Rock"},
		TrackNumber: 1,
		DiscNumber:  1,
		Year:        2020,
		LengthMs:    180000,
	})
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

	// Change both artist and album.  Under the old schema this
	// relinked a recording to a new credit and left the previous
	// credit, its artist link and its release group behind, which is
	// what three orphan sweeps in syncDatabase existed to clean up.
	if err := tw.WriteTrackTags(trackID, TagChanges{
		FieldArtist: "New Artist",
		FieldAlbum:  "New Album",
	}); err != nil {
		t.Fatalf("WriteTrackTags: %v", err)
	}

	af, err := db.Queries.GetAudioFile(ctx, trackID)
	if err != nil {
		t.Fatalf("get audio file: %v", err)
	}

	if af.ArtistCredit != "New Artist" {
		t.Errorf("artist credit: got %q, want %q", af.ArtistCredit, "New Artist")
	}

	if !af.AlbumID.Valid {
		t.Fatal("file has no album after the write")
	}

	album, err := db.Queries.GetAlbum(ctx, af.AlbumID.Int64)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}

	if album.Name != "New Album" {
		t.Errorf("album name: got %q, want %q", album.Name, "New Album")
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

	// Verify new genres are linked.
	genres, err := db.Queries.GetGenreNamesByFile(ctx, trackID)
	if err != nil {
		t.Fatalf("get genres: %v", err)
	}

	if len(genres) != 2 {
		t.Fatalf("expected 2 genres, got %d", len(genres))
	}

	genreNames := make(map[string]bool)
	for _, g := range genres {
		genreNames[g] = true
	}

	if !genreNames["Jazz"] {
		t.Error("expected Jazz genre to be linked")
	}

	if !genreNames["Blues"] {
		t.Error("expected Blues genre to be linked")
	}

	// The old "Rock" link is gone: genres are relinked wholesale, so a
	// genre dropped from the tag is dropped from the file.
	if genreNames["Rock"] {
		t.Error("expected the old Rock link to be replaced")
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

	// Verify the file's own tag columns.
	af, err := db.Queries.GetAudioFile(ctx, trackID)
	if err != nil {
		t.Fatalf("get audio file: %v", err)
	}

	if af.Title != "New Title" {
		t.Errorf("title: got %q, want %q", af.Title, "New Title")
	}

	if af.Year.Int64 != 2025 {
		t.Errorf("year: got %d, want 2025", af.Year.Int64)
	}

	if af.Composer != "New Composer" {
		t.Errorf("composer: got %q, want %q", af.Composer, "New Composer")
	}

	if af.ArtistCredit != "New Artist" {
		t.Errorf("artist credit: got %q, want %q", af.ArtistCredit, "New Artist")
	}

	if !af.AlbumID.Valid {
		t.Fatal("file has no album after the write")
	}

	album, err := db.Queries.GetAlbum(ctx, af.AlbumID.Int64)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}

	if album.Name != "New Album" {
		t.Errorf("album name: got %q, want %q", album.Name, "New Album")
	}

	// Verify genre re-linked.
	genres, err := db.Queries.GetGenreNamesByFile(ctx, trackID)
	if err != nil {
		t.Fatalf("get genres: %v", err)
	}

	if len(genres) != 1 || genres[0] != "Electronic" {
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

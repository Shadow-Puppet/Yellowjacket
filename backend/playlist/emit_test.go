package playlist

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/events"
)

// stubLibraryDir satisfies LibraryDirProvider.
type stubLibraryDir struct{ dir string }

func (s stubLibraryDir) GetLibraryDirectory() string { return s.dir }

// setupRecordedService builds a playlist service on an in-memory DB
// that writes its M3U8 files to a temp directory and records the events
// it would push to the frontend.
func setupRecordedService(
	t *testing.T,
) (*Service, *database.DB, *events.Recorder) {
	t.Helper()

	db := database.NewTestDB(t)
	libDir := t.TempDir()

	svc := NewService(slog.Default(), db, stubLibraryDir{dir: libDir})
	svc.dataDirOverride = t.TempDir()

	rec := events.NewRecorder()
	svc.SetContext(events.WithSink(context.Background(), rec))

	return svc, db, rec
}

// seedPlaylistTracks inserts `count` audio_file rows and returns their
// paths.
func seedPlaylistTracks(t *testing.T, db *database.DB, count int) []string {
	t.Helper()

	_, err := db.ExecContext(
		"INSERT OR IGNORE INTO artist_credit (id, text) VALUES (1, 'Test Artist')",
	)
	if err != nil {
		t.Fatalf("insert artist_credit: %v", err)
	}

	paths := make([]string, count)

	for i := range count {
		id := i + 1
		paths[i] = fmt.Sprintf("/test/pl-track%d.mp3", id)

		if _, err := db.ExecContext(
			"INSERT OR IGNORE INTO recordings (id, name, artist_credit_id) "+
				"VALUES (?, ?, 1)",
			id, fmt.Sprintf("Track %d", id),
		); err != nil {
			t.Fatalf("insert recording %d: %v", id, err)
		}

		if _, err := db.ExecContext(
			"INSERT OR IGNORE INTO audio_files (id, file_path, "+
				"length_milliseconds, file_type_id, recording_id) "+
				"VALUES (?, ?, 180000, 0, ?)",
			id, paths[i], id,
		); err != nil {
			t.Fatalf("insert audio_file %d: %v", id, err)
		}
	}

	return paths
}

func TestEmit_CreatePlaylistAnnouncesTheNewRow(t *testing.T) {
	t.Parallel()

	svc, _, rec := setupRecordedService(t)

	created, err := svc.CreatePlaylist("Road Trip")
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}

	ev, ok := rec.Last(events.PlaylistCreated)
	if !ok {
		t.Fatalf("no PlaylistCreated; got %v", rec.Names())
	}

	summary, ok := ev.Payload().(Summary)
	if !ok {
		t.Fatalf("payload is %T, want playlist.Summary", ev.Payload())
	}

	// The frontend adds the sidebar entry straight from this payload
	// rather than re-fetching, so an empty field here is a blank row.
	if summary.ID != created.ID {
		t.Errorf("emitted ID %d, want %d", summary.ID, created.ID)
	}

	if summary.Name != "Road Trip" {
		t.Errorf("emitted name %q, want Road Trip", summary.Name)
	}

	if summary.CreatedAt == "" || summary.UpdatedAt == "" {
		t.Errorf("emitted empty timestamps: %+v", summary)
	}
}

func TestEmit_RejectedCreateIsSilent(t *testing.T) {
	t.Parallel()

	svc, _, rec := setupRecordedService(t)

	if _, err := svc.CreatePlaylist("   "); err == nil {
		t.Fatal("CreatePlaylist accepted a blank name")
	}

	if got := rec.Count(events.PlaylistCreated); got != 0 {
		t.Errorf("emitted %d PlaylistCreated for a rejected create, want 0", got)
	}
}

// TestEmit_TracksChangedFiresAfterTheWriteIsVisible is the reason this
// package is worth covering as well as queue: the frontend re-reads the
// playlist when it sees PlaylistTracksChanged, so an event emitted
// before the rows were committed would have it read the old contents.
func TestEmit_TracksChangedFiresAfterTheWriteIsVisible(t *testing.T) {
	t.Parallel()

	svc, db, rec := setupRecordedService(t)
	paths := seedPlaylistTracks(t, db, 3)

	created, err := svc.CreatePlaylist("Mix")
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}

	rec.Reset()

	if err := svc.AddTracksToPlaylist(created.ID, paths); err != nil {
		t.Fatalf("AddTracksToPlaylist: %v", err)
	}

	ev, ok := rec.Last(events.PlaylistTracksChanged)
	if !ok {
		t.Fatalf("no PlaylistTracksChanged; got %v", rec.Names())
	}

	id, ok := ev.Payload().(int64)
	if !ok {
		t.Fatalf("payload is %T, want int64", ev.Payload())
	}

	if id != created.ID {
		t.Errorf("emitted playlist ID %d, want %d", id, created.ID)
	}

	// Read back the way the frontend would on receipt of the event.
	tracks, err := svc.GetPlaylistTracks(created.ID)
	if err != nil {
		t.Fatalf("GetPlaylistTracks: %v", err)
	}

	if len(tracks) != len(paths) {
		t.Errorf(
			"a frontend reacting to the event reads %d tracks, want %d",
			len(tracks), len(paths),
		)
	}
}

func TestEmit_DeletePlaylistAnnouncesTheID(t *testing.T) {
	t.Parallel()

	svc, _, rec := setupRecordedService(t)

	created, err := svc.CreatePlaylist("Temp")
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}

	rec.Reset()

	if err := svc.DeletePlaylist(created.ID); err != nil {
		t.Fatalf("DeletePlaylist: %v", err)
	}

	ev, ok := rec.Last(events.PlaylistDeleted)
	if !ok {
		t.Fatalf("no PlaylistDeleted; got %v", rec.Names())
	}

	if id, _ := ev.Payload().(int64); id != created.ID {
		t.Errorf("emitted ID %v, want %d", ev.Payload(), created.ID)
	}
}

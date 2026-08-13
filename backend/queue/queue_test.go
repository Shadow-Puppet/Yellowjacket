package queue

import (
	"fmt"
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
)

// mockTrackLoader satisfies the TrackLoader interface for tests.
// All methods are no-ops.
type mockTrackLoader struct {
	loadedFile string
}

func (m *mockTrackLoader) LoadFile(filePath string) error {
	m.loadedFile = filePath

	return nil
}

func (m *mockTrackLoader) Play() error     { return nil }
func (m *mockTrackLoader) IsPlaying() bool { return false }
func (m *mockTrackLoader) UnloadTrack()    {}

func (m *mockTrackLoader) CurrentPositionSeconds() (int, error) {
	return 0, nil
}

// setupTestQueue creates an isolated Queue backed by an in-memory DB.
func setupTestQueue(t *testing.T) (*Queue, *database.DB) {
	t.Helper()

	db := database.NewTestDB(t)
	q := NewQueue(slog.Default(), db)
	q.SetPlayer(&mockTrackLoader{})

	return q, db
}

// seedAudioFiles inserts `count` audio_file rows (with FK chain) and
// returns the file paths as a string slice.
func seedAudioFiles(t *testing.T, db *database.DB, count int) []string {
	t.Helper()

	// Shared artist credit.
	_, err := db.ExecContext(
		"INSERT OR IGNORE INTO artist_credit (id, text) VALUES (1, 'Test Artist')",
	)
	if err != nil {
		t.Fatalf("insert artist_credit: %v", err)
	}

	paths := make([]string, count)

	for i := range count {
		recID := i + 1
		afID := i + 1
		fp := fmt.Sprintf("/test/track%d.mp3", i+1)
		paths[i] = fp

		_, err := db.ExecContext(
			"INSERT OR IGNORE INTO recordings (id, name, artist_credit_id) VALUES (?, ?, 1)",
			recID, fmt.Sprintf("Track %d", i+1),
		)
		if err != nil {
			t.Fatalf("insert recording %d: %v", recID, err)
		}

		_, err = db.ExecContext(
			"INSERT OR IGNORE INTO audio_files (id, file_path, "+
				"length_milliseconds, file_type_id, recording_id) "+
				"VALUES (?, ?, 180000, 0, ?)",
			afID, fp, recID,
		)
		if err != nil {
			t.Fatalf("insert audio_file %d: %v", afID, err)
		}
	}

	return paths
}

func TestSetQueue_PopulatesTracks(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 0, false, Source{})

	state := q.GetState()
	if got := len(state.Tracks); got != 5 {
		t.Errorf("track count: got %d, want 5", got)
	}

	if state.CurrentIndex != 0 {
		t.Errorf("currentIndex: got %d, want 0", state.CurrentIndex)
	}
}

func TestSetQueue_RecordsSource(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)
	source := Source{Type: "playlist", ID: 42, Label: "Road Trip"}

	q.SetQueue(paths, 0, false, source)

	if got := q.GetState().Source; got != source {
		t.Errorf("source: got %+v, want %+v", got, source)
	}
}

func TestSetQueue_ReplacesPriorSource(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 0, false, Source{Type: "album", ID: 1, Label: "First"})
	q.SetQueue(paths, 0, false, Source{Type: "genre", Label: "Jazz"})

	want := Source{Type: "genre", Label: "Jazz"}
	if got := q.GetState().Source; got != want {
		t.Errorf("source: got %+v, want %+v", got, want)
	}
}

func TestClear_ResetsSource(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 0, false, Source{Type: "album", ID: 1, Label: "Some Album"})
	q.Clear()

	if got := q.GetState().Source; got != (Source{}) {
		t.Errorf("source after Clear: got %+v, want zero value", got)
	}
}

func TestSetQueue_WithStartIndex(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 2, false, Source{})

	state := q.GetState()
	if state.CurrentIndex != 2 {
		t.Errorf("currentIndex: got %d, want 2", state.CurrentIndex)
	}
}

func TestSetQueue_WithShuffleStart(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	// Enable shuffle mode first.
	q.ToggleShuffle()

	q.SetQueue(paths, 0, true, Source{})

	state := q.GetState()
	if !state.ShuffleMode {
		t.Error("shuffleMode: got false, want true")
	}

	q.mu.Lock()
	soLen := len(q.shuffleOrder)
	q.mu.Unlock()

	if soLen != 5 {
		t.Errorf("shuffleOrder length: got %d, want 5", soLen)
	}
}

func TestAddTrack_AppendsToQueue(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 4)

	q.SetQueue(paths[:3], 0, false, Source{})
	q.AddTrack(paths[3])

	state := q.GetState()
	if got := len(state.Tracks); got != 4 {
		t.Errorf("track count: got %d, want 4", got)
	}

	lastTrack := state.Tracks[len(state.Tracks)-1]
	if lastTrack.FilePath != paths[3] {
		t.Errorf("last track path: got %q, want %q", lastTrack.FilePath, paths[3])
	}
}

func TestInsertTracksAt_BeforeCurrentIndex(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 7)

	q.SetQueue(paths[:5], 2, false, Source{})

	// Insert 2 tracks at index 1 (before currentIndex=2).
	q.InsertTracksAt(paths[5:7], 1)

	state := q.GetState()
	// currentIndex should shift by 2 (the number of inserted tracks).
	if state.CurrentIndex != 4 {
		t.Errorf("currentIndex after insert before: got %d, want 4", state.CurrentIndex)
	}

	if got := len(state.Tracks); got != 7 {
		t.Errorf("track count: got %d, want 7", got)
	}
}

func TestInsertTracksAt_AfterCurrentIndex(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 7)

	q.SetQueue(paths[:5], 2, false, Source{})

	// Insert 2 tracks at index 3 (after currentIndex=2).
	q.InsertTracksAt(paths[5:7], 3)

	state := q.GetState()
	// currentIndex should remain 2.
	if state.CurrentIndex != 2 {
		t.Errorf("currentIndex after insert after: got %d, want 2", state.CurrentIndex)
	}
}

func TestMoveQueueTracks_ForwardMove(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 0, false, Source{})

	// Move track at index 1 to index 3.
	q.MoveQueueTracks([]int{1}, 3)

	state := q.GetState()
	// After moving index 1 forward: the track originally at index 1
	// should now be at index 2 (adjustedIdx = 3-1 = 2).
	if state.Tracks[2].FilePath != paths[1] {
		t.Errorf("moved track: got %q at index 2, want %q", state.Tracks[2].FilePath, paths[1])
	}
}

func TestMoveQueueTracks_BackwardMove(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 0, false, Source{})

	// Move track at index 3 to index 1.
	q.MoveQueueTracks([]int{3}, 1)

	state := q.GetState()
	// Track originally at index 3 should now be at index 1.
	if state.Tracks[1].FilePath != paths[3] {
		t.Errorf("moved track: got %q at index 1, want %q", state.Tracks[1].FilePath, paths[3])
	}
}

func TestMoveQueueTracks_MoveCurrentTrack(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 2, false, Source{})

	// Move the current track (index 2) to index 4.
	q.MoveQueueTracks([]int{2}, 4)

	state := q.GetState()
	// The current track should follow to its new position.
	currentPath := state.Tracks[state.CurrentIndex].FilePath
	if currentPath != paths[2] {
		t.Errorf("current track after move: got %q, want %q", currentPath, paths[2])
	}
}

func TestRemoveTrack_RemovesCorrectTrack(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 0, false, Source{})

	q.RemoveTrack(2)

	state := q.GetState()
	if got := len(state.Tracks); got != 4 {
		t.Errorf("track count: got %d, want 4", got)
	}

	// Verify the removed track (paths[2]) is not present.
	for _, track := range state.Tracks {
		if track.FilePath == paths[2] {
			t.Errorf("removed track %q still present in queue", paths[2])
		}
	}
}

func TestRemoveTrack_RemoveCurrentTrack(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 2, false, Source{})

	q.RemoveTrack(2)

	state := q.GetState()
	if got := len(state.Tracks); got != 4 {
		t.Errorf("track count: got %d, want 4", got)
	}

	// After removing currentIndex=2, index should be clamped to valid range.
	if state.CurrentIndex < 0 || state.CurrentIndex >= len(state.Tracks) {
		t.Errorf(
			"currentIndex out of range: got %d, track count %d",
			state.CurrentIndex, len(state.Tracks),
		)
	}
}

func TestClear_EmptiesQueue(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 0, false, Source{})
	q.Clear()

	state := q.GetState()
	if got := len(state.Tracks); got != 0 {
		t.Errorf("track count after clear: got %d, want 0", got)
	}

	if state.CurrentIndex != -1 {
		t.Errorf("currentIndex after clear: got %d, want -1", state.CurrentIndex)
	}
}

func TestToggleShuffle_TogglesMode(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 0, false, Source{})

	// Toggle on.
	q.ToggleShuffle()
	state := q.GetState()

	if !state.ShuffleMode {
		t.Error("shuffleMode after first toggle: got false, want true")
	}

	q.mu.Lock()
	soLen := len(q.shuffleOrder)
	q.mu.Unlock()

	if soLen != 5 {
		t.Errorf("shuffleOrder length after toggle on: got %d, want 5", soLen)
	}

	// Toggle off.
	q.ToggleShuffle()
	state = q.GetState()

	if state.ShuffleMode {
		t.Error("shuffleMode after second toggle: got true, want false")
	}

	q.mu.Lock()
	soLen = len(q.shuffleOrder)
	q.mu.Unlock()

	if soLen != 0 {
		t.Errorf("shuffleOrder length after toggle off: got %d, want 0", soLen)
	}
}

func TestCycleRepeat_CyclesThroughModes(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	_ = seedAudioFiles(t, db, 1)

	// Default is RepeatOff.
	state := q.GetState()
	if state.RepeatMode != RepeatOff {
		t.Errorf("initial repeatMode: got %q, want %q", state.RepeatMode, RepeatOff)
	}

	// off -> all
	q.CycleRepeat()
	state = q.GetState()

	if state.RepeatMode != RepeatAll {
		t.Errorf("after first cycle: got %q, want %q", state.RepeatMode, RepeatAll)
	}

	// all -> one
	q.CycleRepeat()
	state = q.GetState()

	if state.RepeatMode != RepeatOne {
		t.Errorf("after second cycle: got %q, want %q", state.RepeatMode, RepeatOne)
	}

	// one -> off
	q.CycleRepeat()
	state = q.GetState()

	if state.RepeatMode != RepeatOff {
		t.Errorf("after third cycle: got %q, want %q", state.RepeatMode, RepeatOff)
	}
}

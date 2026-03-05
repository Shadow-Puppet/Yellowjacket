package queue

import (
	"log/slog"
	"testing"
)

func TestSaveState_RestoreState_Roundtrip(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 2, false)

	// Change modes so we test all fields.
	q.CycleRepeat() // off -> all
	q.ToggleShuffle()

	q.SaveState()

	// Create a new Queue with the same DB.
	q2 := NewQueue(slog.Default(), db)
	q2.SetPlayer(&mockTrackLoader{})
	q2.RestoreState()

	s1 := q.GetState()
	s2 := q2.GetState()

	// Tracks length.
	if len(s2.Tracks) != len(s1.Tracks) {
		t.Fatalf("tracks length: got %d, want %d", len(s2.Tracks), len(s1.Tracks))
	}

	// Each track's FilePath, Title, Artist.
	for i := range s1.Tracks {
		if s2.Tracks[i].FilePath != s1.Tracks[i].FilePath {
			t.Errorf(
				"track[%d] FilePath: got %q, want %q",
				i, s2.Tracks[i].FilePath, s1.Tracks[i].FilePath,
			)
		}

		if s2.Tracks[i].Title != s1.Tracks[i].Title {
			t.Errorf("track[%d] Title: got %q, want %q", i, s2.Tracks[i].Title, s1.Tracks[i].Title)
		}

		if s2.Tracks[i].Artist != s1.Tracks[i].Artist {
			t.Errorf(
				"track[%d] Artist: got %q, want %q",
				i, s2.Tracks[i].Artist, s1.Tracks[i].Artist,
			)
		}
	}

	// CurrentIndex.
	if s2.CurrentIndex != s1.CurrentIndex {
		t.Errorf("currentIndex: got %d, want %d", s2.CurrentIndex, s1.CurrentIndex)
	}

	// ShuffleMode.
	if s2.ShuffleMode != s1.ShuffleMode {
		t.Errorf("shuffleMode: got %v, want %v", s2.ShuffleMode, s1.ShuffleMode)
	}

	// RepeatMode.
	if s2.RepeatMode != s1.RepeatMode {
		t.Errorf("repeatMode: got %q, want %q", s2.RepeatMode, s1.RepeatMode)
	}

	// ShuffleOrder.
	q.mu.Lock()
	q2.mu.Lock()

	if len(q2.shuffleOrder) != len(q.shuffleOrder) {
		t.Errorf("shuffleOrder length: got %d, want %d", len(q2.shuffleOrder), len(q.shuffleOrder))
	} else {
		for i := range q.shuffleOrder {
			if q2.shuffleOrder[i] != q.shuffleOrder[i] {
				t.Errorf(
					"shuffleOrder[%d]: got %d, want %d",
					i, q2.shuffleOrder[i], q.shuffleOrder[i],
				)
			}
		}
	}

	q2.mu.Unlock()
	q.mu.Unlock()
}

func TestSaveState_RestoreState_EmptyQueue(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)

	// Save empty state (no SetQueue called).
	q.SaveState()

	q2 := NewQueue(slog.Default(), db)
	q2.SetPlayer(&mockTrackLoader{})
	q2.RestoreState()

	state := q2.GetState()
	if len(state.Tracks) != 0 {
		t.Errorf("tracks after restore empty: got %d, want 0", len(state.Tracks))
	}
}

func TestSaveState_RestoreState_SingleTrack(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 1)

	q.SetQueue(paths, 0, false)
	q.SaveState()

	q2 := NewQueue(slog.Default(), db)
	q2.SetPlayer(&mockTrackLoader{})
	q2.RestoreState()

	state := q2.GetState()
	if len(state.Tracks) != 1 {
		t.Fatalf("tracks: got %d, want 1", len(state.Tracks))
	}

	if state.Tracks[0].FilePath != paths[0] {
		t.Errorf("track FilePath: got %q, want %q", state.Tracks[0].FilePath, paths[0])
	}

	if state.CurrentIndex != 0 {
		t.Errorf("currentIndex: got %d, want 0", state.CurrentIndex)
	}
}

func TestSaveState_RestoreState_PreservesTrackOrder(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 10)

	q.SetQueue(paths, 0, false)
	q.SaveState()

	q2 := NewQueue(slog.Default(), db)
	q2.SetPlayer(&mockTrackLoader{})
	q2.RestoreState()

	state := q2.GetState()
	if len(state.Tracks) != 10 {
		t.Fatalf("tracks: got %d, want 10", len(state.Tracks))
	}

	for i, track := range state.Tracks {
		if track.FilePath != paths[i] {
			t.Errorf("track[%d] order: got %q, want %q", i, track.FilePath, paths[i])
		}
	}
}

func TestRestoreState_NoSavedState(t *testing.T) {
	t.Parallel()

	_, db := setupTestQueue(t)

	// RestoreState on fresh DB with no prior SaveState — should not panic.
	q2 := NewQueue(slog.Default(), db)
	q2.SetPlayer(&mockTrackLoader{})
	q2.RestoreState()

	state := q2.GetState()
	if len(state.Tracks) != 0 {
		t.Errorf("tracks after restore (no save): got %d, want 0", len(state.Tracks))
	}
}

func TestSaveState_OverwritesPreviousState(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 8)

	// First save: 5 tracks.
	q.SetQueue(paths[:5], 0, false)
	q.SaveState()

	// Second save: 3 different tracks.
	q.SetQueue(paths[5:8], 0, false)
	q.SaveState()

	q2 := NewQueue(slog.Default(), db)
	q2.SetPlayer(&mockTrackLoader{})
	q2.RestoreState()

	state := q2.GetState()
	if len(state.Tracks) != 3 {
		t.Fatalf("tracks after overwrite: got %d, want 3", len(state.Tracks))
	}

	// Verify the 3 tracks are from the second save, not the first.
	for i, track := range state.Tracks {
		if track.FilePath != paths[5+i] {
			t.Errorf("track[%d]: got %q, want %q", i, track.FilePath, paths[5+i])
		}
	}
}

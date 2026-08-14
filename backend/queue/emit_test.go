package queue

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"yellowjacket/backend/database"
	"yellowjacket/backend/events"
)

// waitFor is how long a test waits for an event emitted from a
// background goroutine (SetQueue resolves large queues in phases).
const waitFor = 5 * time.Second

// setupRecordedQueue is setupTestQueue with an event sink installed, so
// what the frontend would receive is assertable.
//
// Before events.Emit existed this was impossible: SetContext with a
// context.Background() made every emit call log.Fatalf inside Wails,
// and SetContext(nil) made the queue skip emitting entirely — so the
// payloads below have never been covered.
func setupRecordedQueue(t *testing.T) (*Queue, *database.DB, *events.Recorder) {
	t.Helper()

	db := database.NewTestDB(t)
	q := NewQueue(slog.Default(), db)
	q.SetPlayer(&mockTrackLoader{})

	rec := events.NewRecorder()
	_ = q.ServiceStartup(events.WithSink(context.Background(), rec), application.ServiceOptions{})

	return q, db, rec
}

// stateOf returns the State payload of the most recent QueueChanged.
func stateOf(t *testing.T, rec *events.Recorder) State {
	t.Helper()

	ev, ok := rec.Last(events.QueueChanged)
	if !ok {
		t.Fatalf("no QueueChanged emitted; got %v", rec.Names())
	}

	state, ok := ev.Payload().(State)
	if !ok {
		t.Fatalf("QueueChanged payload is %T, want queue.State", ev.Payload())
	}

	return state
}

// modifiedOf returns the TracksModified payload of the most recent
// QueueTracksModified.
func modifiedOf(t *testing.T, rec *events.Recorder) TracksModified {
	t.Helper()

	ev, ok := rec.Last(events.QueueTracksModified)
	if !ok {
		t.Fatalf("no QueueTracksModified emitted; got %v", rec.Names())
	}

	mod, ok := ev.Payload().(TracksModified)
	if !ok {
		t.Fatalf(
			"QueueTracksModified payload is %T, want queue.TracksModified",
			ev.Payload(),
		)
	}

	return mod
}

func TestEmit_SetQueuePushesFullState(t *testing.T) {
	t.Parallel()

	q, db, rec := setupRecordedQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 2, false, Source{})

	if _, ok := rec.Wait(events.QueueChanged, waitFor); !ok {
		t.Fatalf("no QueueChanged after SetQueue; got %v", rec.Names())
	}

	state := stateOf(t, rec)
	if len(state.Tracks) != 5 {
		t.Errorf("emitted %d tracks, want 5", len(state.Tracks))
	}

	if state.CurrentIndex != 2 {
		t.Errorf("emitted currentIndex %d, want 2", state.CurrentIndex)
	}
}

// TestEmit_ClearSendsEmptyNotNilTrackList pins a frontend contract that
// only exists in the emitted payload: the queue's own tracks field is
// nil after Clear, and the store does `state.tracks.length` on receipt.
func TestEmit_ClearSendsEmptyNotNilTrackList(t *testing.T) {
	t.Parallel()

	q, db, rec := setupRecordedQueue(t)
	paths := seedAudioFiles(t, db, 3)

	q.SetQueue(paths, 0, false, Source{})
	rec.Reset()
	q.Clear()

	state := stateOf(t, rec)
	if state.Tracks == nil {
		t.Error("emitted tracks is nil; the frontend reads .length on it")
	}

	if len(state.Tracks) != 0 {
		t.Errorf("emitted %d tracks after Clear, want 0", len(state.Tracks))
	}

	if state.CurrentIndex != -1 {
		t.Errorf("emitted currentIndex %d after Clear, want -1", state.CurrentIndex)
	}
}

func TestEmit_CycleRepeatWalksAllModes(t *testing.T) {
	t.Parallel()

	q, _, rec := setupRecordedQueue(t)

	want := []RepeatMode{RepeatAll, RepeatOne, RepeatOff}

	for i, wantMode := range want {
		q.CycleRepeat()

		modeEvents := rec.Named(events.QueueModeChanged)
		if len(modeEvents) != i+1 {
			t.Fatalf("after %d cycles: %d QueueModeChanged events, want %d",
				i+1, len(modeEvents), i+1)
		}

		mode, ok := modeEvents[i].Payload().(ModeChanged)
		if !ok {
			t.Fatalf("payload is %T, want queue.ModeChanged", modeEvents[i].Payload())
		}

		if mode.RepeatMode != wantMode {
			t.Errorf("cycle %d emitted %v, want %v", i+1, mode.RepeatMode, wantMode)
		}

		if mode.ShuffleMode {
			t.Errorf("cycle %d emitted shuffleMode true; only repeat changed", i+1)
		}
	}
}

func TestEmit_ToggleShuffleReportsBothModes(t *testing.T) {
	t.Parallel()

	q, db, rec := setupRecordedQueue(t)
	q.SetQueue(seedAudioFiles(t, db, 5), 0, false, Source{})
	rec.Reset()

	q.ToggleShuffle()

	ev, ok := rec.Last(events.QueueModeChanged)
	if !ok {
		t.Fatalf("no QueueModeChanged; got %v", rec.Names())
	}

	mode, ok := ev.Payload().(ModeChanged)
	if !ok {
		t.Fatalf("payload is %T, want queue.ModeChanged", ev.Payload())
	}

	if !mode.ShuffleMode {
		t.Error("emitted shuffleMode false after ToggleShuffle")
	}

	// The mode event carries both modes, so a frontend that renders the
	// two toggles from one event cannot desync them.
	if mode.RepeatMode != RepeatOff {
		t.Errorf("emitted repeatMode %v, want RepeatOff", mode.RepeatMode)
	}
}

// TestEmit_AddTrackSendsDeltaNotSnapshot pins the distinction the whole
// TracksModified type exists for: appending must not re-push the queue.
func TestEmit_AddTrackSendsDeltaNotSnapshot(t *testing.T) {
	t.Parallel()

	q, db, rec := setupRecordedQueue(t)
	paths := seedAudioFiles(t, db, 4)

	q.SetQueue(paths[:3], 0, false, Source{})

	if _, ok := rec.Wait(events.QueueChanged, waitFor); !ok {
		t.Fatalf("no QueueChanged after SetQueue; got %v", rec.Names())
	}

	rec.Reset()
	q.AddTrack(paths[3])

	if got := rec.Count(events.QueueChanged); got != 0 {
		t.Errorf("AddTrack emitted %d QueueChanged; want a delta only", got)
	}

	mod := modifiedOf(t, rec)
	if mod.Action != "add" {
		t.Errorf("action = %q, want \"add\"", mod.Action)
	}

	if mod.Index != 3 {
		t.Errorf("index = %d, want 3 (appended at the end)", mod.Index)
	}

	if len(mod.Tracks) != 1 || mod.Tracks[0].FilePath != paths[3] {
		t.Errorf("tracks = %v, want just %s", mod.Tracks, paths[3])
	}
}

func TestEmit_RemoveTracksReportsPositions(t *testing.T) {
	t.Parallel()

	q, db, rec := setupRecordedQueue(t)
	paths := seedAudioFiles(t, db, 5)

	q.SetQueue(paths, 0, false, Source{})

	if _, ok := rec.Wait(events.QueueChanged, waitFor); !ok {
		t.Fatalf("no QueueChanged after SetQueue; got %v", rec.Names())
	}

	rec.Reset()
	q.RemoveTracks([]int{3, 1})

	mod := modifiedOf(t, rec)
	if mod.Action != "remove" {
		t.Errorf("action = %q, want \"remove\"", mod.Action)
	}

	if len(mod.Positions) != 2 {
		t.Fatalf("positions = %v, want two entries", mod.Positions)
	}
}

// TestEmit_NextPushesIndexOnly covers auto-advance, which is what the
// player calls at the end of a track: the frontend must be able to move
// the now-playing highlight without re-rendering the queue.
func TestEmit_NextPushesIndexOnly(t *testing.T) {
	t.Parallel()

	q, db, rec := setupRecordedQueue(t)
	paths := seedAudioFiles(t, db, 3)

	q.SetQueue(paths, 0, false, Source{})

	if _, ok := rec.Wait(events.QueueChanged, waitFor); !ok {
		t.Fatalf("no QueueChanged after SetQueue; got %v", rec.Names())
	}

	rec.Reset()
	q.Next()
	q.Next()

	idxEvents := rec.Named(events.QueueIndexChanged)
	if len(idxEvents) != 2 {
		t.Fatalf("got %d QueueIndexChanged, want 2 (%v)", len(idxEvents), rec.Names())
	}

	for i, ev := range idxEvents {
		idx, ok := ev.Payload().(IndexChanged)
		if !ok {
			t.Fatalf("payload is %T, want queue.IndexChanged", ev.Payload())
		}

		if idx.CurrentIndex != i+1 {
			t.Errorf("advance %d emitted index %d, want %d", i+1, idx.CurrentIndex, i+1)
		}
	}

	if got := rec.Count(events.QueueChanged); got != 0 {
		t.Errorf("advancing emitted %d QueueChanged; want index deltas only", got)
	}
}

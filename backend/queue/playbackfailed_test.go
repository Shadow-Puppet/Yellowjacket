package queue

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"yellowjacket/backend/database"
	"yellowjacket/backend/events"
)

var errFileMissing = errors.New("no such file or directory")

// failingLoader is a TrackLoader that refuses to load a named set of
// paths — a moved file, in other words, which is the whole of
// errors.C1.
type failingLoader struct {
	mockTrackLoader
	fails    map[string]bool
	unloaded int
}

func (f *failingLoader) LoadFile(filePath string) error {
	if f.fails[filePath] {
		return errFileMissing
	}

	return f.mockTrackLoader.LoadFile(filePath)
}

func (f *failingLoader) UnloadTrack() { f.unloaded++ }

// setupFailingQueue is setupRecordedQueue with a loader that fails on
// the given paths.
func setupFailingQueue(
	t *testing.T,
) (*Queue, *database.DB, *events.Recorder, *failingLoader) {
	t.Helper()

	db := database.NewTestDB(t)
	q := NewQueue(slog.Default(), db)
	loader := &failingLoader{fails: map[string]bool{}}
	q.SetPlayer(loader)

	rec := events.NewRecorder()
	_ = q.ServiceStartup(events.WithSink(context.Background(), rec), application.ServiceOptions{})

	return q, db, rec, loader
}

// failureOf returns the payload of the most recent PlaybackFailed.
func failureOf(t *testing.T, rec *events.Recorder) PlaybackFailure {
	t.Helper()

	ev, ok := rec.Last(events.PlaybackFailed)
	if !ok {
		t.Fatalf("no PlaybackFailed emitted; got %v", rec.Names())
	}

	failure, ok := ev.Payload().(PlaybackFailure)
	if !ok {
		t.Fatalf(
			"PlaybackFailed payload is %T, want queue.PlaybackFailure",
			ev.Payload(),
		)
	}

	return failure
}

func TestPlaybackFailed_EmittedForAMissingFile(t *testing.T) {
	t.Parallel()

	q, db, rec, loader := setupFailingQueue(t)
	paths := seedAudioFiles(t, db, 3)
	loader.fails[paths[1]] = true

	q.SetQueue(paths, 0, false, Source{})
	q.PlayIndex(1)

	failure := failureOf(t, rec)
	if failure.FilePath != paths[1] {
		t.Errorf("filePath: got %q, want %q", failure.FilePath, paths[1])
	}

	if failure.Reason == "" {
		t.Error("reason is empty; the frontend has nothing to log")
	}

	if failure.Title == "" {
		t.Error("title is empty; a message cannot name the track")
	}
}

func TestPlaybackFailed_AutoAdvanceSkipsPastIt(t *testing.T) {
	t.Parallel()

	q, db, rec, loader := setupFailingQueue(t)
	paths := seedAudioFiles(t, db, 3)
	loader.fails[paths[1]] = true

	q.SetQueue(paths, 0, false, Source{})
	q.Play()

	// The first track finished: auto-advance lands on the missing
	// file and must step over it rather than stopping dead.
	q.OnPlaybackFinished(nil)

	if got := q.GetState().CurrentIndex; got != 2 {
		t.Errorf("currentIndex after skipping: got %d, want 2", got)
	}

	if _, ok := rec.Last(events.PlaybackFailed); !ok {
		t.Errorf("skipped silently; events were %v", rec.Names())
	}
}

func TestPlaybackFailed_NextSkipsPastIt(t *testing.T) {
	t.Parallel()

	q, db, _, loader := setupFailingQueue(t)
	paths := seedAudioFiles(t, db, 4)
	loader.fails[paths[1]] = true
	loader.fails[paths[2]] = true

	q.SetQueue(paths, 0, false, Source{})
	q.Next()

	if got := q.GetState().CurrentIndex; got != 3 {
		t.Errorf(
			"currentIndex after two unplayable tracks: got %d, want 3",
			got,
		)
	}
}

func TestPlaybackFailed_WholeQueueUnplayableStopsOnce(t *testing.T) {
	t.Parallel()

	q, db, rec, loader := setupFailingQueue(t)
	paths := seedAudioFiles(t, db, 3)

	for _, p := range paths {
		loader.fails[p] = true
	}

	q.SetQueue(paths, 0, false, Source{})
	q.repeatMode = RepeatAll

	rec.Reset()
	q.Next()

	// Every file is gone (a disconnected drive).  One pass, then
	// stop — not an endless wrap around a RepeatAll queue.
	if got := q.GetState().CurrentIndex; got != -1 {
		t.Errorf("currentIndex: got %d, want -1 (exhausted)", got)
	}

	if got := rec.Count(events.PlaybackFailed); got != len(paths) {
		t.Errorf(
			"PlaybackFailed count: got %d, want %d (one pass)",
			got, len(paths),
		)
	}

	if loader.unloaded != 0 {
		t.Errorf(
			"player unloaded %d times; the finished track should stay "+
				"on the now-playing bar",
			loader.unloaded,
		)
	}
}

func TestQueueExhausted_KeepsTheFinishedTrackLoaded(t *testing.T) {
	t.Parallel()

	q, db, _, loader := setupFailingQueue(t)
	paths := seedAudioFiles(t, db, 1)

	q.SetQueue(paths, 0, false, Source{})
	q.Play()
	q.OnPlaybackFinished(nil)

	if q.GetState().CurrentIndex != -1 {
		t.Errorf(
			"currentIndex: got %d, want -1",
			q.GetState().CurrentIndex,
		)
	}

	// H-18: the bar used to blank while the queue panel still listed
	// what had just played.
	if loader.unloaded != 0 {
		t.Errorf("player unloaded %d times, want 0", loader.unloaded)
	}
}

func TestQueueExhausted_UnloadsWhenTheTrackIsRemoved(t *testing.T) {
	t.Parallel()

	q, db, _, loader := setupFailingQueue(t)
	paths := seedAudioFiles(t, db, 1)

	q.SetQueue(paths, 0, false, Source{})
	q.RemoveTrack(0)

	// Nothing left to show, so the bar clears.
	if loader.unloaded == 0 {
		t.Error("player not unloaded after its track left the queue")
	}
}

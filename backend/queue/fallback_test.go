package queue

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeFallbackSource records every call and returns whatever was
// configured, optionally gated by a channel so a test can control
// exactly when resolution completes (to exercise the staleness check).
type fakeFallbackSource struct {
	mu    sync.Mutex
	calls []FallbackContext

	paths  []string
	source Source
	err    error

	// gate, if set, blocks ResolveFallback until closed.
	gate chan struct{}
}

func (f *fakeFallbackSource) ResolveFallback(
	_ context.Context,
	fctx FallbackContext,
) ([]string, Source, error) {
	if f.gate != nil {
		<-f.gate
	}

	f.mu.Lock()
	f.calls = append(f.calls, fctx)
	f.mu.Unlock()

	return f.paths, f.source, f.err
}

func (f *fakeFallbackSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.calls)
}

func (f *fakeFallbackSource) lastContext() FallbackContext {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls[len(f.calls)-1]
}

// waitUntil polls cond until it's true or fails the test after a
// short deadline, naming what it was waiting for on timeout.
func waitUntil(t *testing.T, cond func() bool, what string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for: %s", what)
}

func TestFallback_TriggersOnNaturalFinish(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	seedPaths := seedAudioFiles(t, db, 1)
	fallbackPaths := seedAudioFiles(t, db, 6)[1:] // distinct from seed

	fake := &fakeFallbackSource{
		paths:  fallbackPaths,
		source: Source{Type: "playlist", ID: 9, Label: "Favorites"},
	}
	q.SetFallbackSource(fake)

	q.SetQueue(seedPaths, 0, false, Source{Type: "album", ID: 1, Label: "Seed Album"})
	q.OnPlaybackFinished()

	waitUntil(t, func() bool { return fake.callCount() == 1 }, "fallback to be resolved")
	waitUntil(t, func() bool {
		return q.GetState().Source == fake.source
	}, "queue to adopt the fallback source")

	state := q.GetState()
	if got := len(state.Tracks); got != len(fallbackPaths) {
		t.Errorf("track count: got %d, want %d", got, len(fallbackPaths))
	}

	ctx := fake.lastContext()
	if ctx.PreviousSource.Type != "album" {
		t.Errorf("previous source type: got %q, want %q", ctx.PreviousSource.Type, "album")
	}

	if len(ctx.SeedPaths) != 1 || ctx.SeedPaths[0] != seedPaths[0] {
		t.Errorf("seed paths: got %v, want %v", ctx.SeedPaths, seedPaths)
	}
}

func TestFallback_TriggersOnNextPastEnd(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	seedPaths := seedAudioFiles(t, db, 1)
	fallbackPaths := seedAudioFiles(t, db, 6)[1:]

	fake := &fakeFallbackSource{
		paths:  fallbackPaths,
		source: Source{Type: "dynamicMix", Label: "a mix"},
	}
	q.SetFallbackSource(fake)

	q.SetQueue(seedPaths, 0, false, Source{})
	q.Next() // already at the only/last track: exhausts

	waitUntil(t, func() bool { return fake.callCount() == 1 }, "fallback to be resolved")
	waitUntil(t, func() bool {
		return len(q.GetState().Tracks) == len(fallbackPaths)
	}, "queue to adopt the fallback tracks")
}

func TestFallback_TriggersOnCurrentTrackRemoved(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	seedPaths := seedAudioFiles(t, db, 1)
	fallbackPaths := seedAudioFiles(t, db, 6)[1:]

	fake := &fakeFallbackSource{
		paths:  fallbackPaths,
		source: Source{Type: "playlist", Label: "Favorites"},
	}
	q.SetFallbackSource(fake)

	q.SetQueue(seedPaths, 0, false, Source{})
	q.RemoveTrack(0) // removes the only (currently playing) track

	waitUntil(t, func() bool { return fake.callCount() == 1 }, "fallback to be resolved")
	waitUntil(t, func() bool {
		return len(q.GetState().Tracks) == len(fallbackPaths)
	}, "queue to adopt the fallback tracks")
}

func TestFallback_EmptyResultLeavesQueueExhausted(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	seedPaths := seedAudioFiles(t, db, 1)

	fake := &fakeFallbackSource{paths: nil, source: Source{}} // "stop"
	q.SetFallbackSource(fake)

	q.SetQueue(seedPaths, 0, false, Source{})
	q.OnPlaybackFinished()

	waitUntil(t, func() bool { return fake.callCount() == 1 }, "fallback to be resolved")

	// Give a wrongly-applied fallback a moment to (not) land.
	time.Sleep(20 * time.Millisecond)

	state := q.GetState()
	if len(state.Tracks) != 1 {
		t.Errorf("track count: got %d, want 1 (queue unchanged)", len(state.Tracks))
	}

	if state.CurrentIndex != -1 {
		t.Errorf("currentIndex: got %d, want -1 (still exhausted)", state.CurrentIndex)
	}
}

func TestFallback_StaleResolutionDiscarded(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	seedPaths := seedAudioFiles(t, db, 1)
	stalePaths := seedAudioFiles(t, db, 6)[1:4]
	freshPaths := seedAudioFiles(t, db, 9)[6:9]

	gate := make(chan struct{})
	fake := &fakeFallbackSource{
		paths:  stalePaths,
		source: Source{Type: "dynamicMix", Label: "stale"},
		gate:   gate,
	}
	q.SetFallbackSource(fake)

	q.SetQueue(seedPaths, 0, false, Source{})
	q.OnPlaybackFinished() // starts resolving, blocked on gate

	time.Sleep(20 * time.Millisecond) // let the goroutine reach the gate

	// Something else claims the queue before the stale resolution lands.
	q.SetQueue(freshPaths, 0, false, Source{Type: "album", Label: "fresh"})

	close(gate) // let the stale resolution finish and try to apply

	waitUntil(t, func() bool {
		state := q.GetState()
		if state.Source.Label == "stale" {
			t.Fatal("stale fallback was applied")
		}

		return state.Source.Label == "fresh"
	}, "the fresh queue to survive the stale fallback")
}

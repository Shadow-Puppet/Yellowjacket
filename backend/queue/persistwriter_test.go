package queue

import (
	"testing"
	"time"
)

// mustFinish fails the test if fn has not finished within d.
func mustFinish(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()

	done := make(chan struct{})

	go func() {
		fn()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not finish within %s", what, d)
	}
}

// The bug this exists for: SQLite's writer is a single connection, a
// background pass can hold it for seconds, and the queue used to do its
// writes inline while holding q.mu.  So starting an album left the
// track changed, the transport at paused and the queue panel empty —
// SetQueue was still waiting on a durability write, and every other
// bound method was waiting on SetQueue.
//
// A stalled writer must cost nothing but durability.
func TestStalledWriterDoesNotBlockTheQueue(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 5)

	// Occupy the persistence goroutine the way a held write connection
	// does, and keep it occupied for the rest of the test.
	release := make(chan struct{})
	defer close(release)

	q.submitWrite(func() { <-release })

	mustFinish(t, 5*time.Second, "SetQueue", func() {
		q.SetQueue(paths, 0, false, Source{Type: "album", ID: 1, Label: "Test"})
	})

	mustFinish(t, 5*time.Second, "GetState", func() {
		if got := len(q.GetState().Tracks); got != len(paths) {
			t.Errorf("tracks = %d, want %d", got, len(paths))
		}
	})

	mustFinish(t, 5*time.Second, "Play", func() { q.Play() })

	mustFinish(t, 5*time.Second, "AddTrack", func() { q.AddTrack(paths[0]) })

	mustFinish(t, 5*time.Second, "RemoveTrack", func() { q.RemoveTrack(0) })
}

// Order is the whole reason these run on one goroutine: "clear and
// rewrite the queue" followed by "insert at 4" is not the same thing in
// the other order.
func TestWritesRunInSubmissionOrder(t *testing.T) {
	t.Parallel()

	q, _ := setupTestQueue(t)

	var order []int

	for i := range 20 {
		q.submitWrite(func() { order = append(order, i) })
	}

	q.flushWrites()

	if len(order) != 20 {
		t.Fatalf("ran %d writes, want 20", len(order))
	}

	for i, got := range order {
		if got != i {
			t.Fatalf("write %d ran at position %d", got, i)
		}
	}
}

// Making the writes asynchronous introduces one hazard the inline
// version could not have: a path that reads back what it wrote.  There
// are two — RestoreState and CompactAfterLibraryRemoval — and both must
// see the writes that are still in flight, or they rebuild the queue
// from the one before it.
func TestRestoreStateWaitsForPendingWrites(t *testing.T) {
	t.Parallel()

	q, db := setupTestQueue(t)
	paths := seedAudioFiles(t, db, 4)

	// Hold the writer so SetQueue's rows are provably still pending.
	release := make(chan struct{})

	q.submitWrite(func() { <-release })

	q.SetQueue(paths, 0, false, Source{})

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()

	q.RestoreState()

	if got := len(q.GetState().Tracks); got != len(paths) {
		t.Errorf("tracks after restore = %d, want %d", got, len(paths))
	}
}

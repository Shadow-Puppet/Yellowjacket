package jobs

import (
	"errors"
	"log/slog"
	"sync"
	"testing"
)

var errBoom = errors.New("boom")

func testRegistry() *Registry {
	return NewRegistry(slog.New(slog.DiscardHandler), nil)
}

func TestStartRegistersJob(t *testing.T) {
	t.Parallel()

	r := testRegistry()

	h := r.Start(Spec{
		ID:    "scan:1",
		Kind:  KindLibraryScan,
		Title: "Scanning Music",
		Caps:  Caps{Pausable: true, Cancellable: true},
	})

	if h == nil {
		t.Fatal("expected a handle")
	}

	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 job, got %d", len(snap))
	}

	if snap[0].State != StateRunning {
		t.Errorf("expected default state running, got %q", snap[0].State)
	}

	if snap[0].Title != "Scanning Music" {
		t.Errorf("unexpected title %q", snap[0].Title)
	}
}

func TestStartReusesHandleAndLog(t *testing.T) {
	t.Parallel()

	r := testRegistry()

	h := r.Start(Spec{ID: "scan:1", Kind: KindLibraryScan, State: StateQueued})
	h.Logf(LevelInfo, "queued")

	// A queued scan being popped off the queue re-registers the same ID.
	again := r.Start(Spec{ID: "scan:1", Kind: KindLibraryScan})
	if again != h {
		t.Fatal("expected the same handle to be reused")
	}

	if got := len(r.Logs("scan:1")); got != 1 {
		t.Errorf("expected the log to survive re-registration, got %d entries", got)
	}

	if again.State() != StateRunning {
		t.Errorf("expected state running after restart, got %q", again.State())
	}

	if len(r.Snapshot()) != 1 {
		t.Error("re-registering should not duplicate the job")
	}
}

func TestLogRingIsBounded(t *testing.T) {
	t.Parallel()

	r := testRegistry()
	h := r.Start(Spec{ID: "scan:1", Kind: KindLibraryScan})

	for range maxLogEntries + 50 {
		h.Logf(LevelWarn, "warning")
	}

	logs := r.Logs("scan:1")
	if len(logs) != maxLogEntries {
		t.Errorf("expected log capped at %d, got %d", maxLogEntries, len(logs))
	}

	// LogCount keeps counting past the ring so the UI can show that
	// entries were dropped.
	if got := h.Snapshot().LogCount; got != maxLogEntries+50 {
		t.Errorf("expected LogCount %d, got %d", maxLogEntries+50, got)
	}

	if got := h.Snapshot().WarnCount; got != maxLogEntries+50 {
		t.Errorf("expected WarnCount %d, got %d", maxLogEntries+50, got)
	}
}

func TestTerminalStatesStampEndedAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		apply func(*Handle)
		want  State
	}{
		{"complete", func(h *Handle) { h.Complete() }, StateComplete},
		{"cancelled", func(h *Handle) { h.Cancelled() }, StateCancelled},
		{"failed", func(h *Handle) { h.Fail(errBoom) }, StateError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := testRegistry()
			h := r.Start(Spec{ID: "scan:1", Kind: KindLibraryScan})
			tt.apply(h)

			snap := h.Snapshot()
			if snap.State != tt.want {
				t.Errorf("expected state %q, got %q", tt.want, snap.State)
			}

			if !snap.State.IsTerminal() {
				t.Error("expected a terminal state")
			}

			if snap.EndedAt == 0 {
				t.Error("expected EndedAt to be stamped")
			}
		})
	}
}

func TestFailRecordsErrorMessage(t *testing.T) {
	t.Parallel()

	r := testRegistry()
	h := r.Start(Spec{ID: "index:build", Kind: KindIndexBuild})
	h.Fail(errBoom)

	if got := h.Snapshot().Error; got != "boom" {
		t.Errorf("expected error %q, got %q", "boom", got)
	}

	if got := h.Snapshot().ErrorCount; got != 1 {
		t.Errorf("expected the failure to be logged, got ErrorCount %d", got)
	}
}

func TestPauseInvokesControlAndMarksPausing(t *testing.T) {
	t.Parallel()

	r := testRegistry()

	var wg sync.WaitGroup

	wg.Add(1)
	r.Start(Spec{
		ID:       "scan:1",
		Kind:     KindLibraryScan,
		Caps:     Caps{Pausable: true},
		Controls: Controls{Pause: wg.Done},
	})

	r.Pause("scan:1")
	wg.Wait()

	// The producer confirms StatePaused; the registry only promises
	// the intermediate "pausing" state.
	if got := r.Get("scan:1").State(); got != StatePausing {
		t.Errorf("expected state pausing, got %q", got)
	}
}

func TestControlsRespectCapabilities(t *testing.T) {
	t.Parallel()

	r := testRegistry()

	called := false

	r.Start(Spec{
		ID:       "scan:1",
		Kind:     KindLibraryScan,
		Caps:     Caps{Pausable: false, Cancellable: false},
		Controls: Controls{Pause: func() { called = true }, Cancel: func() { called = true }},
	})

	r.Pause("scan:1")
	r.Cancel("scan:1")

	if called {
		t.Error("controls must not fire for a job that declares no capability")
	}

	if got := r.Get("scan:1").State(); got != StateRunning {
		t.Errorf("expected state to be unchanged, got %q", got)
	}
}

func TestControlsIgnoreTerminalJobs(t *testing.T) {
	t.Parallel()

	r := testRegistry()

	called := false
	h := r.Start(Spec{
		ID:       "scan:1",
		Kind:     KindLibraryScan,
		Caps:     Caps{Pausable: true, Cancellable: true},
		Controls: Controls{Pause: func() { called = true }, Cancel: func() { called = true }},
	})
	h.Complete()

	r.Pause("scan:1")
	r.Cancel("scan:1")

	if called {
		t.Error("controls must not fire for a finished job")
	}
}

func TestControlsOnUnknownJobAreNoOps(t *testing.T) {
	t.Parallel()

	r := testRegistry()

	// Stale IDs arrive from a frontend holding an old snapshot.
	r.Pause("scan:404")
	r.Resume("scan:404")
	r.Cancel("scan:404")

	if got := len(r.Logs("scan:404")); got != 0 {
		t.Errorf("expected no logs for an unknown job, got %d", got)
	}
}

func TestClearFinishedKeepsActiveJobs(t *testing.T) {
	t.Parallel()

	r := testRegistry()

	done := r.Start(Spec{ID: "scan:1", Kind: KindLibraryScan})
	done.Complete()
	r.Start(Spec{ID: "scan:2", Kind: KindLibraryScan})

	r.ClearFinished()

	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 job to survive, got %d", len(snap))
	}

	if snap[0].ID != "scan:2" {
		t.Errorf("expected the active job to survive, kept %q", snap[0].ID)
	}
}

func TestHasActive(t *testing.T) {
	t.Parallel()

	r := testRegistry()

	if r.HasActive() {
		t.Error("an empty registry has no active jobs")
	}

	h := r.Start(Spec{ID: "scan:1", Kind: KindLibraryScan})

	if !r.HasActive() {
		t.Error("expected the running job to count as active")
	}

	h.Complete()

	if r.HasActive() {
		t.Error("a completed job is not active")
	}
}

func TestSnapshotIsADeepCopy(t *testing.T) {
	t.Parallel()

	r := testRegistry()
	h := r.Start(Spec{ID: "index:build", Kind: KindIndexBuild})
	h.SetStages([]Stage{{Name: "Catalog Import", State: "running"}})

	snap := h.Snapshot()
	snap.Stages[0].State = "mutated"

	if got := h.Snapshot().Stages[0].State; got != "running" {
		t.Errorf("mutating a snapshot leaked into the job: %q", got)
	}
}

func TestConcurrentUpdatesAreSafe(t *testing.T) {
	t.Parallel()

	r := testRegistry()
	h := r.Start(Spec{ID: "scan:1", Kind: KindLibraryScan})

	var wg sync.WaitGroup

	for i := range 8 {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			for range 100 {
				h.SetProgress(int64(n), 100)
				h.Logf(LevelWarn, "concurrent")

				_ = r.Snapshot()
			}
		}(i)
	}

	wg.Wait()

	if got := h.Snapshot().WarnCount; got != 800 {
		t.Errorf("expected 800 warnings, got %d", got)
	}
}

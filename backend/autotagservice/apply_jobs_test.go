package autotagservice

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/jobs"
)

var errApplyFailed = errors.New("write failed")

// newJobService builds the smallest Service that can register a job:
// no database, no scorer, no MB client.
func newJobService(t *testing.T) (*Service, *jobs.Registry) {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)
	reg := jobs.NewRegistry(logger, nil)
	svc := &Service{
		logger:         logger,
		ctx:            context.Background(),
		runningApplies: make(map[string]struct{}),
	}

	svc.SetJobRegistry(reg)

	return svc, reg
}

func TestApplyJob_RegistersACancellableJob(t *testing.T) {
	svc, reg := newJobService(t)

	handle, ctx, cancel := svc.startApplyJob("/music/Artist/Album", 9)
	defer cancel()

	if handle == nil {
		t.Fatal("no job handle: an apply that is not registered has no cancel and no progress")
	}

	snapshot := handle.Snapshot()

	if snapshot.Kind != jobs.KindAutotagApply {
		t.Errorf("kind = %q, want %q", snapshot.Kind, jobs.KindAutotagApply)
	}

	if snapshot.Total != 9 {
		t.Errorf("total = %d, want 9", snapshot.Total)
	}

	if !snapshot.Caps.Cancellable {
		t.Error("apply job is not cancellable, which is the point of registering it")
	}

	if snapshot.Subtitle != "Album" {
		t.Errorf("subtitle = %q, want the folder name", snapshot.Subtitle)
	}

	// The registry's Cancel control has to reach the context the apply
	// is running under, or the button is decoration.
	reg.Cancel(applyJobID("/music/Artist/Album"))

	<-ctx.Done()
}

func TestApplyJob_FinishStateMatchesTheRun(t *testing.T) {
	cancelled, cancelStop := context.WithCancel(context.Background())
	cancelStop()

	tests := []struct {
		name   string
		ctx    context.Context
		result *autotag.ApplyResult
		err    error
		want   jobs.State
	}{
		{
			name:   "every track written",
			ctx:    context.Background(),
			result: &autotag.ApplyResult{Succeeded: 4},
			want:   jobs.StateComplete,
		},
		{
			name:   "some tracks failed",
			ctx:    context.Background(),
			result: &autotag.ApplyResult{Succeeded: 3, Failed: 1},
			want:   jobs.StateComplete,
		},
		{
			name: "the apply itself failed",
			ctx:  context.Background(),
			err:  errApplyFailed,
			want: jobs.StateError,
		},
		{
			// Cancelled beats failed: Apply returns a context error on
			// the way out, and reading that as a failure would make
			// every cancel look like a bug.
			name:   "the user cancelled",
			ctx:    cancelled,
			result: &autotag.ApplyResult{Succeeded: 1},
			err:    context.Canceled,
			want:   jobs.StateCancelled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newJobService(t)
			handle, _, cancel := svc.startApplyJob("/music/"+tt.name, 4)

			defer cancel()

			svc.finishApplyJob(tt.ctx, handle, tt.result, tt.err)

			if got := handle.State(); got != tt.want {
				t.Errorf("state = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWritesInFlight_TracksTheApplySet(t *testing.T) {
	svc, _ := newJobService(t)

	if svc.WritesInFlight() {
		t.Fatal("nothing is running, so nothing should be reported in flight")
	}

	svc.tryStartApply("/music/Album")

	if !svc.WritesInFlight() {
		t.Error("an apply is running: quitting now would half-retag a folder")
	}

	svc.endApply("/music/Album")

	if svc.WritesInFlight() {
		t.Error("the apply finished and the app should stop asking about it")
	}
}

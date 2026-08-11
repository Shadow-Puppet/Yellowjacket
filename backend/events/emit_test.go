package events_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"yellowjacket/backend/events"
)

func TestEmitDropsWithoutRuntimeOrSink(t *testing.T) {
	t.Parallel()

	// The point of the wrapper: neither of these may reach
	// runtime.EventsEmit, which would log.Fatalf and take the test
	// binary down with it.
	events.Emit(context.Background(), events.QueueChanged, "payload")

	//nolint:staticcheck // a nil context is the case under test.
	events.Emit(nil, events.QueueChanged, "payload")
}

func TestDeliverReportsMissingRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{name: "nil context", ctx: nil},
		{name: "no runtime", ctx: context.Background()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := events.Deliver(tt.ctx, events.QueueChanged)
			if !errors.Is(err, events.ErrNoRuntime) {
				t.Fatalf("got %v, want ErrNoRuntime", err)
			}
		})
	}
}

func TestSinkReceivesEmittedEvents(t *testing.T) {
	t.Parallel()

	rec := events.NewRecorder()
	ctx := events.WithSink(context.Background(), rec)

	events.Emit(ctx, events.QueueChanged, "one")
	events.Emit(ctx, events.VolumeChanged, 42)

	if err := events.Deliver(ctx, events.TrackChanged); err != nil {
		t.Fatalf("Deliver with a sink installed: %v", err)
	}

	want := []string{
		events.QueueChanged,
		events.VolumeChanged,
		events.TrackChanged,
	}

	got := rec.Names()
	if len(got) != len(want) {
		t.Fatalf("recorded %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, got[i], want[i])
		}
	}

	ev, ok := rec.Last(events.VolumeChanged)
	if !ok {
		t.Fatal("VolumeChanged not recorded")
	}

	if ev.Payload() != 42 {
		t.Errorf("VolumeChanged payload = %v, want 42", ev.Payload())
	}
}

func TestRecorderPayloadOfArgumentlessEvent(t *testing.T) {
	t.Parallel()

	rec := events.NewRecorder()
	rec.Emit(events.SeekFailed)

	ev, ok := rec.Last(events.SeekFailed)
	if !ok {
		t.Fatal("SeekFailed not recorded")
	}

	if ev.Payload() != nil {
		t.Errorf("payload = %v, want nil", ev.Payload())
	}
}

func TestRecorderWaitSeesEventsAlreadyRecorded(t *testing.T) {
	t.Parallel()

	rec := events.NewRecorder()
	rec.Emit(events.LibraryScanComplete, 31)

	ev, ok := rec.Wait(events.LibraryScanComplete, time.Second)
	if !ok {
		t.Fatal("Wait missed an event recorded before the call")
	}

	if ev.Payload() != 31 {
		t.Errorf("payload = %v, want 31", ev.Payload())
	}
}

func TestRecorderWaitBlocksForBackgroundEmit(t *testing.T) {
	t.Parallel()

	rec := events.NewRecorder()
	ctx := events.WithSink(context.Background(), rec)

	go func() {
		time.Sleep(10 * time.Millisecond)
		events.Emit(ctx, events.LibraryScanProgress, 1)
		events.Emit(ctx, events.LibraryScanComplete, 2)
	}()

	if _, ok := rec.Wait(events.LibraryScanComplete, 2*time.Second); !ok {
		t.Fatal("Wait timed out on a background emit")
	}
}

func TestRecorderWaitTimesOut(t *testing.T) {
	t.Parallel()

	rec := events.NewRecorder()

	if _, ok := rec.Wait(events.QueueChanged, 20*time.Millisecond); ok {
		t.Fatal("Wait returned an event that was never emitted")
	}
}

func TestRecorderIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	rec := events.NewRecorder()
	ctx := events.WithSink(context.Background(), rec)

	const emitters, each = 8, 25

	var wg sync.WaitGroup

	wg.Add(emitters)

	for range emitters {
		go func() {
			defer wg.Done()

			for range each {
				events.Emit(ctx, events.QueueChanged, 1)
			}
		}()
	}

	wg.Wait()

	if got := rec.Count(events.QueueChanged); got != emitters*each {
		t.Errorf("recorded %d events, want %d", got, emitters*each)
	}
}

func TestRecorderReset(t *testing.T) {
	t.Parallel()

	rec := events.NewRecorder()
	rec.Emit(events.QueueChanged)
	rec.Reset()

	if got := rec.Events(); len(got) != 0 {
		t.Errorf("after Reset: %v, want empty", got)
	}
}

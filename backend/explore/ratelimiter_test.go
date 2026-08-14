package explore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRateLimiterBurst(t *testing.T) {
	rl := NewRateLimiter()
	ctx := context.Background()

	const n = 5

	start := time.Now()

	for i := range n {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("Wait %d: %v", i, err)
		}
	}

	elapsed := time.Since(start)

	// First request is immediate; 4 more at 1/sec = ≥4s total.
	if elapsed < 4*time.Second {
		t.Errorf(
			"elapsed %v, want ≥ 4s (rate limiter too fast)", elapsed,
		)
	}

	// Generous upper bound to avoid CI flakes.
	if elapsed > 7*time.Second {
		t.Errorf(
			"elapsed %v, want ≤ 7s (rate limiter too slow)", elapsed,
		)
	}
}

// TestRateLimiterBackgroundYields is the property the whole priority
// lane exists for: an interactive caller arriving while a backfill is
// running is not queued behind the rest of the backfill.
func TestRateLimiterBackgroundYields(t *testing.T) {
	t.Parallel()

	// Fast rates: this asserts ordering, not pacing.
	rl := NewRateLimiterBurst(50, 1).WithBackgroundLane(50)
	bg := WithBackgroundPriority(context.Background())

	// Hold the gate open for the length of the test by keeping one
	// interactive wait outstanding.
	rl.enterInteractive()

	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = rl.Wait(bg)
	}()

	select {
	case <-done:
		t.Fatal("background Wait proceeded while an interactive wait was outstanding")
	case <-time.After(100 * time.Millisecond):
	}

	rl.exitInteractive()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background Wait did not proceed after the interactive wait cleared")
	}
}

// TestRateLimiterBackgroundLanePaces checks the second half: background
// callers are held to their own slower rate even with the gate clear.
func TestRateLimiterBackgroundLanePaces(t *testing.T) {
	t.Parallel()

	// Interactive lane is effectively unlimited; the background lane is
	// the only thing that can slow this down.
	rl := NewRateLimiterBurst(1000, 1000).WithBackgroundLane(4)
	bg := WithBackgroundPriority(context.Background())

	start := time.Now()

	for i := range 3 {
		if err := rl.Wait(bg); err != nil {
			t.Fatalf("Wait %d: %v", i, err)
		}
	}

	// Burst of 1, then two more at 4/sec = ≥500ms.
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Errorf("elapsed %v, want ≥ 400ms (background lane not pacing)", elapsed)
	}
}

// TestRateLimiterBackgroundCancels guards the loop in waitBackground:
// a background caller blocked behind a permanently busy interactive
// lane must still honour its context.
func TestRateLimiterBackgroundCancels(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter().WithBackgroundLane(1)
	rl.enterInteractive() // never cleared

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := rl.Wait(WithBackgroundPriority(ctx))
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context.DeadlineExceeded", err)
	}
}

// TestRateLimiterInteractiveUnmarked confirms an unmarked context is
// interactive — the default has to be the safe one, since every
// existing call site is unmarked.
func TestRateLimiterInteractiveUnmarked(t *testing.T) {
	t.Parallel()

	if isBackgroundPriority(context.Background()) {
		t.Error("an unmarked context reported as background priority")
	}

	if !isBackgroundPriority(WithBackgroundPriority(context.Background())) {
		t.Error("a marked context did not report as background priority")
	}
}

func TestRateLimiterContextCancel(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter()

	// Drain the initial token so the next Wait must block.
	if err := rl.Wait(context.Background()); err != nil {
		t.Fatalf("drain token: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := rl.Wait(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

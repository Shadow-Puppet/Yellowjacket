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

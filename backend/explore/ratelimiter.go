// Package explore provides MusicBrainz and ListenBrainz API clients
// with rate-limited HTTP access and a SQLite response cache.
package explore

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter enforces a maximum request rate using a token bucket.
// MusicBrainz requires ≤1 request per second and rejects ALL
// requests (not just excess) when the rate is exceeded, so callers
// block proactively via Wait rather than retrying reactively.
//
// A limiter may carry a second, slower **background lane** (see
// WithBackgroundLane).  A caller marked by WithBackgroundPriority is
// paced by that lane *and* yields to interactive callers: while any
// interactive Wait is outstanding, background waits do not take a
// token at all.  This is what keeps a multi-thousand-request backfill
// from putting the album page the user is looking at right now behind
// hours of queued work.
//
// RateLimiter is safe for concurrent use.
type RateLimiter struct {
	limiter *rate.Limiter

	// background paces callers marked with WithBackgroundPriority.  Nil
	// means background callers are paced by limiter alone and only the
	// yield gate below applies.
	background *rate.Limiter

	mu sync.Mutex
	// interactive counts Waits currently blocked (or about to block) on
	// behalf of a user-facing request.
	interactive int
	// clear is closed when interactive falls to zero, and is nil while
	// there are none.  Background waiters select on it rather than
	// polling, so a quiet limiter costs nothing.
	clear chan struct{}
}

// NewRateLimiter returns a rate limiter that allows exactly one
// request per second with a burst size of 1. The first call to
// Wait returns immediately; subsequent calls block until the next
// token is available.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Every(time.Second), 1),
	}
}

// NewRateLimiterN returns a rate limiter that allows n requests
// per second with a burst of n.  Used for background tasks like
// index building where a higher rate is acceptable.
func NewRateLimiterN(n int) *RateLimiter {
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(n), n),
	}
}

// NewRateLimiterF returns a rate limiter that allows f requests
// per second with a burst of 1.
func NewRateLimiterF(f float64) *RateLimiter {
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(f), 1),
	}
}

// NewRateLimiterBurst returns a rate limiter that allows n requests
// per second with a burst size of b.  The burst allows short spikes
// (e.g. 3 concurrent search calls) without queueing, while still
// limiting sustained throughput.
func NewRateLimiterBurst(n, b int) *RateLimiter {
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(n), b),
	}
}

// WithBackgroundLane gives the limiter a slower sustained rate for
// callers marked by WithBackgroundPriority, and returns it so the
// call can be chained onto a constructor.
//
// The interactive lane is deliberately allowed to be faster than the
// origin's documented limit (MB's search limiter is 3/s burst 1, which
// staggers three concurrent search goroutines rather than raising the
// sustained rate).  Sustained background work has no such excuse: a
// backfill running for an hour at the interactive rate is exactly the
// traffic shape that earns an all-or-nothing 503 for every request the
// app makes, interactive ones included.
func (r *RateLimiter) WithBackgroundLane(perSecond float64) *RateLimiter {
	r.background = rate.NewLimiter(rate.Limit(perSecond), 1)

	return r
}

// Wait blocks until the rate limiter allows the caller to proceed
// or the context is cancelled. Returns ctx.Err() if the context
// expires before a token becomes available.
//
// A context marked by WithBackgroundPriority takes the background
// lane; every other caller is interactive.
func (r *RateLimiter) Wait(ctx context.Context) error {
	if isBackgroundPriority(ctx) {
		return r.waitBackground(ctx)
	}

	r.enterInteractive()
	defer r.exitInteractive()

	return r.limiter.Wait(ctx)
}

// waitBackground yields to interactive callers, paces on the background
// lane if there is one, and only then takes a token.
//
// One request of slippage is possible and is accepted rather than
// designed out: an interactive caller arriving *during* the final
// limiter.Wait below queues behind this one background request.  Making
// that impossible would mean cancelling an already-granted reservation,
// which the token bucket cannot express, in exchange for at most one
// request-time of latency.
func (r *RateLimiter) waitBackground(ctx context.Context) error {
	for {
		if err := r.awaitClear(ctx); err != nil {
			return err
		}

		if r.background != nil {
			if err := r.background.Wait(ctx); err != nil {
				return err
			}
		}

		// An interactive caller may have arrived while the background
		// lane was pacing us.  Go round again — a background task that
		// never runs while the user is active is the intent, and the
		// context is what ends the loop.
		if r.pendingInteractive() {
			continue
		}

		return r.limiter.Wait(ctx)
	}
}

// enterInteractive registers an outstanding interactive wait.
func (r *RateLimiter) enterInteractive() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.interactive == 0 {
		r.clear = make(chan struct{})
	}

	r.interactive++
}

// exitInteractive retires one, releasing background waiters when the
// last interactive caller is done.
func (r *RateLimiter) exitInteractive() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.interactive--

	if r.interactive <= 0 {
		r.interactive = 0

		if r.clear != nil {
			close(r.clear)
			r.clear = nil
		}
	}
}

// pendingInteractive reports whether any interactive wait is outstanding.
func (r *RateLimiter) pendingInteractive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.interactive > 0
}

// awaitClear blocks until no interactive wait is outstanding.
func (r *RateLimiter) awaitClear(ctx context.Context) error {
	for {
		r.mu.Lock()
		ch := r.clear
		r.mu.Unlock()

		if ch == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

// backgroundPriorityKey types the context marker below.
type backgroundPriorityKey struct{}

// WithBackgroundPriority marks a context as belonging to background
// work — a post-scan backfill rather than something a user is waiting
// on.  Every RateLimiter.Wait made under it yields to interactive
// callers and is paced by the background lane.
//
// It is a context marker rather than a parameter because the marking
// has to survive the whole call chain — a backfill calls the same
// MusicBrainzClient methods the detail pages call, and threading a
// priority argument through all of them would put the decision at
// every call site instead of at the one place that knows.
func WithBackgroundPriority(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundPriorityKey{}, true)
}

// isBackgroundPriority reports whether ctx was marked by
// WithBackgroundPriority.
func isBackgroundPriority(ctx context.Context) bool {
	v, _ := ctx.Value(backgroundPriorityKey{}).(bool)

	return v
}

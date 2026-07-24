// Package explore provides MusicBrainz and ListenBrainz API clients
// with rate-limited HTTP access and a SQLite response cache.
package explore

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter enforces a maximum request rate using a token bucket.
// MusicBrainz requires ≤1 request per second and rejects ALL
// requests (not just excess) when the rate is exceeded, so callers
// block proactively via Wait rather than retrying reactively.
//
// RateLimiter is safe for concurrent use.
type RateLimiter struct {
	limiter *rate.Limiter
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

// Wait blocks until the rate limiter allows the caller to proceed
// or the context is cancelled. Returns ctx.Err() if the context
// expires before a token becomes available.
func (r *RateLimiter) Wait(ctx context.Context) error {
	return r.limiter.Wait(ctx)
}

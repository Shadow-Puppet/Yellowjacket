package player

import (
	"errors"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
)

// BufferedStreamer wraps a beep.Streamer with a goroutine-driven
// read-ahead ring buffer. It decouples the source streamer's I/O
// timing from the speaker callback's real-time deadline, preventing
// audible glitches caused by disk stalls, GC pauses, or CPU
// scheduling delays.
//
// The read-ahead goroutine continuously fills the ring buffer from
// the source. The speaker callback drains the ring buffer without
// ever touching the source directly. If the ring buffer is
// temporarily empty (read-ahead hasn't caught up), Stream returns
// silence rather than blocking or signaling end-of-stream.
type BufferedStreamer struct {
	// srcMu serializes all access to the underlying source. The
	// read-ahead goroutine holds it while calling source.Stream;
	// callers that need to Seek the source must hold it too (via
	// LockSource/UnlockSource) so the non-thread-safe decoder is
	// never read and seeked concurrently.
	srcMu   sync.Mutex
	mu      sync.Mutex
	source  beep.Streamer
	ring    [][2]float64
	readPos int
	writPos int
	count   int
	done    bool
	err     error
	closed  chan struct{}

	// starved counts consecutive Stream calls served with silence
	// because the ring was empty, and starvedSince is when that run
	// began.  An underrun is legitimate for a moment -- that is what
	// the read-ahead exists to absorb -- but it is not legitimate
	// forever, and "forever" is indistinguishable from healthy
	// playback everywhere above this type: the chain never ends, so
	// the player stays in Playing with the button showing pause, and
	// the decoder's position never moves, so the 1 Hz report pins the
	// seek bar and suppresses its interpolation.
	starved      int
	starvedSince time.Time
}

// The silence fill is bounded by both a duration and a run of calls,
// and it needs both.
//
// Duration alone is the real measure -- the speaker paces itself, so
// wall clock is what says whether the source has actually stopped --
// but a caller draining in a tight loop (a test, a decode-to-buffer)
// makes hundreds of calls in microseconds and would trip nothing.
// A call count alone is the opposite failure: the same tight loop
// spends the whole budget before the read-ahead goroutine has been
// scheduled once, and ends a perfectly good stream at sample zero.
//
// The duration is longer than the 2 s read-ahead it is there to
// outlast, and the count is short enough that the speaker (~200 ms a
// call) reaches it well inside that.
const (
	maxStarvedDuration = 3 * time.Second
	minStarvedCalls    = 8
)

// errSourceStalled is returned by Err when the source stopped
// producing samples without ever reporting end-of-stream.
var errSourceStalled = errors.New(
	"audio source stopped producing samples",
)

// NewBufferedStreamer creates a BufferedStreamer that pre-fills
// bufferSize samples from source via a background goroutine.
// A typical bufferSize is 2× the sample rate (~2 seconds of audio).
func NewBufferedStreamer(
	source beep.Streamer,
	bufferSize int,
) *BufferedStreamer {
	bs := &BufferedStreamer{
		source: source,
		ring:   make([][2]float64, bufferSize),
		closed: make(chan struct{}),
	}

	go bs.readAhead()

	return bs
}

// finish marks the stream ended, recording err as the reason when
// there is one.  Every exit from readAhead goes through it: an exit
// that leaves done false strands Stream in its underrun branch,
// where it returns silence and ok forever.
func (bs *BufferedStreamer) finish(err error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.done = true

	if err != nil && bs.err == nil {
		bs.err = err
	}
}

// readAhead continuously reads from the source into the ring buffer
// until the source is drained, an error occurs, or Close is called.
// It always marks the stream done on the way out.
func (bs *BufferedStreamer) readAhead() {
	// Temporary buffer for reading from source outside the lock.
	// 512 samples per chunk keeps the critical section short.
	const chunkSize = 512

	tmp := make([][2]float64, chunkSize)

	// Every exit marks the stream done.  An exit that does not is what
	// stranded Stream in its underrun branch, returning silence and ok
	// for the rest of the process's life.
	var exitErr error

	defer func() { bs.finish(exitErr) }()

	for {
		// Check if closed.
		select {
		case <-bs.closed:
			return
		default:
		}

		bs.mu.Lock()

		// Stream gave up waiting for us.  Nothing downstream is
		// listening any more, so filling the ring is work for nobody.
		if bs.done {
			bs.mu.Unlock()

			return
		}

		space := len(bs.ring) - bs.count

		if space == 0 {
			// Buffer full — release lock and wait briefly.
			bs.mu.Unlock()

			select {
			case <-bs.closed:
				return
			case <-time.After(1 * time.Millisecond):
			}

			continue
		}

		// Determine how many samples to request.
		toRead := space
		if toRead > chunkSize {
			toRead = chunkSize
		}

		bs.mu.Unlock()

		// Read from source WITHOUT holding bs.mu so disk I/O does
		// not block the speaker goroutine. srcMu is held to keep
		// this read from racing a concurrent source Seek.
		bs.srcMu.Lock()
		n, ok := bs.source.Stream(tmp[:toRead])
		bs.srcMu.Unlock()

		if n > 0 {
			bs.mu.Lock()

			for i := range n {
				bs.ring[bs.writPos] = tmp[i]
				bs.writPos = (bs.writPos + 1) % len(bs.ring)
			}

			bs.count += n
			bs.mu.Unlock()
		}

		if !ok {
			// A drained source and a failed one both land here and are
			// not the same event: one is a track that ended, the other
			// is a track that broke.  Err is what tells them apart, and
			// it is why the player must ask before treating this as a
			// natural finish.
			exitErr = bs.source.Err()

			return
		}

		// If source returned 0 samples but is still ok, yield
		// briefly to avoid busy-spinning.
		if n == 0 {
			select {
			case <-bs.closed:
				return
			case <-time.After(1 * time.Millisecond):
			}
		}
	}
}

// Stream copies samples from the ring buffer into the provided
// slice. If the buffer is temporarily empty but the source is not
// yet drained, it fills the output with silence and returns
// (len(samples), true) to avoid speaker underrun.
func (bs *BufferedStreamer) Stream(
	samples [][2]float64,
) (int, bool) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.count == 0 && bs.done {
		return 0, false
	}

	if bs.count == 0 {
		// The read-ahead has not caught up.  Silence buys it time --
		// but only for a bounded stretch, because "forever" is
		// reported upward as healthy playback and there is no watchdog
		// above this to notice otherwise.
		bs.starved++

		if bs.starvedSince.IsZero() {
			bs.starvedSince = time.Now()
		}

		if bs.starved >= minStarvedCalls &&
			time.Since(bs.starvedSince) > maxStarvedDuration {
			bs.done = true

			if bs.err == nil {
				bs.err = errSourceStalled
			}

			return 0, false
		}

		for i := range samples {
			samples[i] = [2]float64{}
		}

		return len(samples), true
	}

	// Samples arrived, so whatever the stall was, it is over.
	bs.resetStarvationLocked()

	// Copy available samples from ring buffer.
	n := len(samples)
	if n > bs.count {
		n = bs.count
	}

	for i := range n {
		samples[i] = bs.ring[bs.readPos]
		bs.readPos = (bs.readPos + 1) % len(bs.ring)
	}

	bs.count -= n

	return n, true
}

// Err returns any error encountered by the source streamer.
func (bs *BufferedStreamer) Err() error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	return bs.err
}

// Flush discards all buffered samples so the next Stream call
// returns freshly-read data from the source. This must be called
// after seeking the underlying source to prevent stale pre-seek
// audio from being played back.
func (bs *BufferedStreamer) Flush() {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.readPos = 0
	bs.writPos = 0
	bs.count = 0

	// A seek empties the ring on purpose, and the refill that follows
	// is exactly the stall the budget exists to tolerate.  Charging it
	// against a budget the previous underrun already spent would end
	// the track on a seek near the end of a slow file.
	bs.resetStarvationLocked()
}

// resetStarvationLocked forgets an underrun run.  Must be called with
// bs.mu held.
func (bs *BufferedStreamer) resetStarvationLocked() {
	bs.starved = 0
	bs.starvedSince = time.Time{}
}

// LockSource blocks the read-ahead goroutine from touching the
// underlying source, giving the caller exclusive access so it can
// safely Seek the non-thread-safe decoder. Every LockSource must
// be paired with an UnlockSource.
func (bs *BufferedStreamer) LockSource() {
	bs.srcMu.Lock()
}

// UnlockSource releases the exclusive source access acquired by
// LockSource, allowing the read-ahead goroutine to resume.
func (bs *BufferedStreamer) UnlockSource() {
	bs.srcMu.Unlock()
}

// Close signals the read-ahead goroutine to stop. It is safe to
// call multiple times.
func (bs *BufferedStreamer) Close() {
	select {
	case <-bs.closed:
		// Already closed.
	default:
		close(bs.closed)
	}
}

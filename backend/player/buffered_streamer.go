package player

import (
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
	mu      sync.Mutex
	source  beep.Streamer
	ring    [][2]float64
	readPos int
	writPos int
	count   int
	done    bool
	err     error
	closed  chan struct{}
}

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

// readAhead continuously reads from the source into the ring buffer
// until the source is drained, an error occurs, or Close is called.
func (bs *BufferedStreamer) readAhead() {
	// Temporary buffer for reading from source outside the lock.
	// 512 samples per chunk keeps the critical section short.
	const chunkSize = 512

	tmp := make([][2]float64, chunkSize)

	for {
		// Check if closed.
		select {
		case <-bs.closed:
			return
		default:
		}

		bs.mu.Lock()
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

		// Read from source WITHOUT holding the lock so disk I/O
		// does not block the speaker goroutine.
		n, ok := bs.source.Stream(tmp[:toRead])

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
			bs.mu.Lock()
			bs.done = true

			if srcErr := bs.source.Err(); srcErr != nil {
				bs.err = srcErr
			}

			bs.mu.Unlock()

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
		// Buffer temporarily empty — fill with silence.
		for i := range samples {
			samples[i] = [2]float64{}
		}

		return len(samples), true
	}

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

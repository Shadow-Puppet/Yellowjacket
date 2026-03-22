package player

import (
	"runtime"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// slowStreamer wraps a beep.Streamer and introduces a delay before
// each Stream call, simulating slow disk I/O.
type slowStreamer struct {
	inner beep.Streamer
	delay time.Duration
}

func (s *slowStreamer) Stream(samples [][2]float64) (int, bool) {
	time.Sleep(s.delay)

	return s.inner.Stream(samples)
}

func (s *slowStreamer) Err() error { return s.inner.Err() }

// finiteStreamer produces exactly N samples with incrementing values
// starting at 1.0 (so sample 0 → 1.0, sample 1 → 2.0, etc.) and
// then signals end-of-stream. Values start at 1 so they are
// distinguishable from silence (zero).
func finiteStreamer(n int) beep.Streamer {
	pos := 0

	return beep.StreamerFunc(func(samples [][2]float64) (int, bool) {
		if pos >= n {
			return 0, false
		}

		filled := 0

		for i := range samples {
			if pos >= n {
				break
			}

			val := float64(pos + 1) // +1 so first sample is 1.0
			samples[i] = [2]float64{val, val}
			pos++
			filled++
		}

		return filled, true
	})
}

func TestBufferedStreamer_BasicStream(t *testing.T) {
	const total = 1000

	src := finiteStreamer(total)
	bs := NewBufferedStreamer(src, 2048)

	defer bs.Close()

	var collected [][2]float64

	buf := make([][2]float64, 256)

	for {
		n, ok := bs.Stream(buf)

		for i := range n {
			// Skip silence frames (buffer not yet filled).
			if buf[i][0] == 0 && buf[i][1] == 0 && len(collected) == 0 {
				continue
			}

			collected = append(collected, buf[i])
		}

		if !ok {
			break
		}

		// Safety valve: if we've collected enough samples plus
		// extra from potential silence padding, break.
		if len(collected) >= total {
			// Drain remaining.
			for {
				n, ok = bs.Stream(buf)
				if !ok {
					break
				}

				for i := range n {
					if buf[i][0] != 0 || buf[i][1] != 0 {
						collected = append(collected, buf[i])
					}
				}
			}

			break
		}
	}

	if len(collected) != total {
		t.Fatalf(
			"expected %d samples, got %d", total, len(collected),
		)
	}

	// Verify ordering (values start at 1.0).
	for i, s := range collected {
		expected := float64(i + 1)
		if s[0] != expected || s[1] != expected {
			t.Fatalf(
				"sample %d: expected [%f %f], got [%f %f]",
				i, expected, expected, s[0], s[1],
			)
		}
	}
}

func TestBufferedStreamer_SmallReads(t *testing.T) {
	const total = 200

	src := finiteStreamer(total)
	bs := NewBufferedStreamer(src, 512)

	defer bs.Close()

	// Give read-ahead time to fill.
	time.Sleep(50 * time.Millisecond)

	var collected [][2]float64

	buf := make([][2]float64, 1) // Read one sample at a time.

	for {
		n, ok := bs.Stream(buf)

		for i := range n {
			if buf[i][0] == 0 && buf[i][1] == 0 && len(collected) == 0 {
				continue
			}

			collected = append(collected, buf[i])
		}

		if !ok {
			break
		}

		if len(collected) >= total {
			// Drain.
			for {
				n, ok = bs.Stream(buf)
				if !ok {
					break
				}

				for i := range n {
					if buf[i][0] != 0 || buf[i][1] != 0 {
						collected = append(collected, buf[i])
					}
				}
			}

			break
		}
	}

	if len(collected) != total {
		t.Fatalf(
			"expected %d samples, got %d", total, len(collected),
		)
	}

	for i, s := range collected {
		expected := float64(i + 1)
		if s[0] != expected || s[1] != expected {
			t.Fatalf(
				"sample %d: expected [%f %f], got [%f %f]",
				i, expected, expected, s[0], s[1],
			)
		}
	}
}

func TestBufferedStreamer_SourceDrained(t *testing.T) {
	const total = 100

	src := finiteStreamer(total)
	bs := NewBufferedStreamer(src, 256)

	defer bs.Close()

	// Wait for read-ahead to completely drain the source.
	time.Sleep(50 * time.Millisecond)

	// Read all samples out.
	consumed := 0
	buf := make([][2]float64, 32)
	hitEOF := false

	for range 1000 { // Safety limit.
		n, ok := bs.Stream(buf)

		for i := range n {
			if buf[i][0] != 0 || buf[i][1] != 0 {
				consumed++
			}
		}

		if !ok {
			hitEOF = true

			break
		}
	}

	if !hitEOF {
		t.Fatal("expected stream to return ok=false after source drained")
	}

	if consumed != total {
		t.Fatalf("expected %d non-zero samples, got %d", total, consumed)
	}
}

func TestBufferedStreamer_EmptyBufferReturnsSilence(t *testing.T) {
	// Use a slow source that sleeps 50ms per call.
	src := &slowStreamer{
		inner: finiteStreamer(100),
		delay: 50 * time.Millisecond,
	}
	bs := NewBufferedStreamer(src, 1024)

	defer bs.Close()

	// Immediately call Stream before read-ahead has had time to
	// fill anything. The buffer should be empty.
	buf := make([][2]float64, 64)
	n, ok := bs.Stream(buf)

	if !ok {
		t.Fatal("expected ok=true when buffer is empty but source not drained")
	}

	if n != len(buf) {
		t.Fatalf("expected %d samples (silence), got %d", len(buf), n)
	}

	// All returned samples should be silence (zeros).
	for i := range n {
		if buf[i][0] != 0 || buf[i][1] != 0 {
			t.Fatalf(
				"sample %d should be silence, got [%f %f]",
				i, buf[i][0], buf[i][1],
			)
		}
	}
}

func TestBufferedStreamer_Flush(t *testing.T) {
	// Use a slow source so the read-ahead goroutine cannot fully
	// drain it before we flush. Each chunk sleeps 5ms, giving us
	// time to flush while data is still being produced.
	src := &slowStreamer{
		inner: finiteStreamer(5000),
		delay: 5 * time.Millisecond,
	}
	bs := NewBufferedStreamer(src, 2048)

	defer bs.Close()

	// Let read-ahead fill some data.
	time.Sleep(50 * time.Millisecond)

	// Read a few samples to confirm data is buffered.
	buf := make([][2]float64, 32)
	n, ok := bs.Stream(buf)

	if !ok || n == 0 {
		t.Fatal("expected buffered data before flush")
	}

	// Record the last sample value we saw.
	lastBefore := buf[n-1][0]

	// Verify the buffer had more data than we consumed (i.e.
	// there's stale data in the ring that Flush should discard).
	bs.mu.Lock()
	countBeforeFlush := bs.count
	bs.mu.Unlock()

	if countBeforeFlush == 0 {
		t.Fatal("expected non-empty ring buffer before flush")
	}

	// Flush discards all buffered data.
	bs.Flush()

	// Verify the ring buffer is empty after flush.
	bs.mu.Lock()
	countAfterFlush := bs.count
	bs.mu.Unlock()

	if countAfterFlush != 0 {
		t.Fatalf(
			"expected 0 samples after flush, got %d",
			countAfterFlush,
		)
	}

	// Wait for read-ahead to refill with fresh data.
	time.Sleep(100 * time.Millisecond)

	// The next non-zero sample must come from AFTER the
	// pre-flush position in the source (i.e. its value must be
	// greater than lastBefore + countBeforeFlush, since those
	// samples were discarded).
	foundNonZero := false

	for range 200 {
		n, ok = bs.Stream(buf)

		for i := range n {
			if buf[i][0] != 0 {
				// The sample must be strictly greater than what
				// was buffered before flush.
				if buf[i][0] <= lastBefore {
					t.Fatalf(
						"after flush, got sample value %f "+
							"which is <= pre-flush value %f "+
							"(stale data not discarded)",
						buf[i][0], lastBefore,
					)
				}

				foundNonZero = true

				break
			}
		}

		if foundNonZero || !ok {
			break
		}

		time.Sleep(1 * time.Millisecond)
	}

	if !foundNonZero {
		t.Fatal("expected non-zero samples after flush")
	}
}

func TestBufferedStreamer_Close(t *testing.T) {
	// Use a source that never drains.
	infinite := beep.StreamerFunc(func(samples [][2]float64) (int, bool) {
		for i := range samples {
			samples[i] = [2]float64{1.0, 1.0}
		}

		return len(samples), true
	})

	goroutinesBefore := runtime.NumGoroutine()
	bs := NewBufferedStreamer(infinite, 4096)

	// Let read-ahead goroutine start.
	time.Sleep(10 * time.Millisecond)

	bs.Close()

	// Wait for goroutine to exit.
	time.Sleep(50 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()

	// The goroutine count should not have increased. Allow ±1 for
	// runtime fluctuations.
	if goroutinesAfter > goroutinesBefore+1 {
		t.Fatalf(
			"goroutine leak: before=%d after=%d",
			goroutinesBefore, goroutinesAfter,
		)
	}

	// Calling Close again should not panic.
	bs.Close()
}

package player

import (
	"errors"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// errTestDecode stands in for a decoder blowing up mid-track.
var errTestDecode = errors.New("decode blew up")

// stalledStreamer never produces a sample and never reports
// end-of-stream: (0, true), forever. A damaged file that decodes to
// nothing looks like this, and so does any source whose producer has
// quietly stopped.
type stalledStreamer struct{}

func (stalledStreamer) Stream(_ [][2]float64) (int, bool) { return 0, true }
func (stalledStreamer) Err() error                        { return nil }

// failingStreamer produces n good samples and then fails, which is
// what a decode error mid-track looks like: the same (0, false) a
// finished track returns, distinguishable only by Err.
type failingStreamer struct {
	remaining int
	err       error
}

func (f *failingStreamer) Stream(samples [][2]float64) (int, bool) {
	if f.remaining <= 0 {
		return 0, false
	}

	n := min(len(samples), f.remaining)

	for i := range n {
		samples[i] = [2]float64{1, 1}
	}

	f.remaining -= n

	return n, true
}

func (f *failingStreamer) Err() error { return f.err }

// drainUntilEnd calls Stream until it reports end-of-stream, or gives
// up. It returns whether the stream ended.
//
// The give-up bound is wall clock rather than a call count: the stall
// budget is a duration, so a tight loop has to actually wait it out.
func drainUntilEnd(bs *BufferedStreamer, within time.Duration) bool {
	buf := make([][2]float64, 512)
	deadline := time.Now().Add(within)

	for time.Now().Before(deadline) {
		if _, ok := bs.Stream(buf); !ok {
			return true
		}

		time.Sleep(time.Millisecond)
	}

	return false
}

// A source that stops producing without ever ending is the fault this
// whole file exists for: Stream used to answer with silence and ok
// forever, so the chain never ended, the player stayed in Playing
// with the button showing pause, and the decoder's position never
// moved -- a frozen seek bar over a track that was not playing.
func TestAStalledSourceEndsTheStream(t *testing.T) {
	bs := NewBufferedStreamer(stalledStreamer{}, 2048)
	defer bs.Close()

	if !drainUntilEnd(bs, maxStarvedDuration+2*time.Second) {
		t.Fatal(
			"a stalled source never ended the stream: the player " +
				"would sit in Playing with a frozen position",
		)
	}

	if !errors.Is(bs.Err(), errSourceStalled) {
		t.Fatalf(
			"expected the stall to be reported, got %v", bs.Err(),
		)
	}
}

// Close is the other exit that used to leave `done` false, with the
// same consequence: the ring drains and every call after it is
// silence that claims to be audio.
func TestClosingEndsTheStream(t *testing.T) {
	bs := NewBufferedStreamer(finiteStreamer(1<<20), 2048)

	// Let the read-ahead fill something, so this exercises the drain
	// after Close rather than a buffer that was empty anyway.
	time.Sleep(20 * time.Millisecond)
	bs.Close()

	if !drainUntilEnd(bs, 2*time.Second) {
		t.Fatal("a closed streamer never reported end-of-stream")
	}
}

// A source that fails is not a source that finished, and only Err
// tells them apart. Before this, the player reported a mid-track
// decode failure to the queue as a natural end, so the queue
// auto-advanced in silence and counted the broken track as played.
func TestAFailedSourceReportsItsError(t *testing.T) {
	src := &failingStreamer{remaining: 4096, err: errTestDecode}

	bs := NewBufferedStreamer(src, 2048)
	defer bs.Close()

	if !drainUntilEnd(bs, 2*time.Second) {
		t.Fatal("a failing source never reported end-of-stream")
	}

	if !errors.Is(bs.Err(), errTestDecode) {
		t.Fatalf(
			"expected the source's error to survive, got %v",
			bs.Err(),
		)
	}
}

// The ordinary case has to keep working: a source that ends cleanly
// ends with no error, or every finished track would be reported as a
// failure and skipped.
func TestADrainedSourceReportsNoError(t *testing.T) {
	bs := NewBufferedStreamer(finiteStreamer(4096), 2048)
	defer bs.Close()

	if !drainUntilEnd(bs, 2*time.Second) {
		t.Fatal("a finite source never reported end-of-stream")
	}

	if bs.Err() != nil {
		t.Fatalf(
			"a track that finished normally reported %v", bs.Err(),
		)
	}
}

// A slow source is exactly what the read-ahead exists to absorb, so
// underruns must not be charged cumulatively -- otherwise a file on a
// slow disk ends itself partway through.
func TestUnderrunsDoNotAccumulateAcrossASlowSource(t *testing.T) {
	const total = 8192

	src := &slowStreamer{
		inner: finiteStreamer(total),
		delay: 2 * time.Millisecond,
	}

	bs := NewBufferedStreamer(src, 1024)
	defer bs.Close()

	buf := make([][2]float64, 256)
	got := 0

	for {
		n, ok := bs.Stream(buf)
		if !ok {
			break
		}

		for i := range n {
			if buf[i][0] != 0 {
				got++
			}
		}
	}

	if got != total {
		t.Fatalf(
			"a slow but healthy source was cut short: got %d of %d "+
				"samples",
			got, total,
		)
	}
}

// beep.Streamer is what the player wraps; keep the type honest.
var _ beep.Streamer = (*BufferedStreamer)(nil)

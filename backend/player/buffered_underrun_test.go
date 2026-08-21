package player

import (
	"sync"
	"testing"
	"time"
)

// An underrun is audible and nothing counted it (#135).
//
// The distinction these tests exist for is that `starved` and
// `underruns` disagree on purpose. `starved` is a stall detector: it is
// reset by every arriving sample, because its job is to end a track
// whose source has died and a source that is merely slow must not be
// cut short (TestUnderrunsDoNotAccumulateAcrossASlowSource, next
// door). That reset is exactly what made the audible case invisible --
// a hundred short underruns a minute never approach the give-up
// threshold, and each one is a run of zeros spliced into the waveform
// with a step discontinuity at both edges.

// TestAnEmptyRingIsCounted is the measurement itself: silence served
// for a missing sample is recorded rather than merely tolerated.
func TestAnEmptyRingIsCounted(t *testing.T) {
	t.Parallel()

	// A source that never produces is the cleanest way to make the
	// ring empty on demand; the stall budget is far longer than the
	// handful of calls below.
	bs := NewBufferedStreamer(stalledStreamer{}, 1024)
	defer bs.Close()

	if got := bs.Underruns(); got != (UnderrunStats{}) {
		t.Fatalf("a fresh streamer already reports %+v", got)
	}

	buf := make([][2]float64, 256)

	for range 3 {
		if _, ok := bs.Stream(buf); !ok {
			t.Fatal("the stall budget ran out before the test did")
		}
	}

	got := bs.Underruns()

	if got.Calls != 3 {
		t.Errorf("Calls = %d, want 3", got.Calls)
	}

	if got.Samples != int64(3*len(buf)) {
		t.Errorf("Samples = %d, want %d", got.Samples, 3*len(buf))
	}

	// Three consecutive silent calls are one episode, not three. That
	// is the number that means something audible: one interruption is
	// one pop however many callbacks it spans.
	if got.Runs != 1 {
		t.Errorf("Runs = %d, want 1 -- an unbroken run is one episode", got.Runs)
	}
}

// TestSilenceIsWhatIsCounted pins what an underrun actually does to the
// waveform, which is the reason to count it at all.
func TestSilenceIsWhatIsCounted(t *testing.T) {
	t.Parallel()

	bs := NewBufferedStreamer(stalledStreamer{}, 1024)
	defer bs.Close()

	buf := make([][2]float64, 64)
	for i := range buf {
		buf[i] = [2]float64{0.5, 0.5}
	}

	n, ok := bs.Stream(buf)
	if !ok || n != len(buf) {
		t.Fatalf("Stream = (%d, %v), want (%d, true)", n, ok, len(buf))
	}

	for i := range buf {
		if buf[i] != ([2]float64{}) {
			t.Fatalf("sample %d is %v, want silence", i, buf[i])
		}
	}

	if got := bs.Underruns().Samples; got != int64(len(buf)) {
		t.Errorf("counted %d samples of silence, wrote %d", got, len(buf))
	}
}

// TestSeparateEpisodesAreSeparateRuns is the counter's whole shape:
// audio arriving between two underruns makes them two, because that is
// two interruptions and two clicks.
func TestSeparateEpisodesAreSeparateRuns(t *testing.T) {
	t.Parallel()

	// A source that yields nothing until it is fed, so the ring can be
	// emptied, filled and emptied again on demand.
	src := &gatedStreamer{}

	bs := NewBufferedStreamer(src, 1024)
	defer bs.Close()

	buf := make([][2]float64, 128)

	starve := func() {
		t.Helper()

		for range 2 {
			if _, ok := bs.Stream(buf); !ok {
				t.Fatal("the stall budget ran out before the test did")
			}
		}
	}

	// feed lets exactly one bufferful through and drains it, so the
	// ring is empty again on return. Allowing more would mean the
	// starve() after it drained real audio instead of underrunning,
	// which is what the first version of this test did -- it reported
	// one episode and looked like the counter was wrong.
	feed := func() {
		t.Helper()

		src.allow(len(buf))

		// The read-ahead is a goroutine, so wait for real samples
		// rather than assuming they have landed.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			n, ok := bs.Stream(buf)
			if ok && n > 0 && buf[0] != ([2]float64{}) {
				return
			}

			time.Sleep(time.Millisecond)
		}

		t.Fatal("the source never delivered a sample")
	}

	starve()
	feed()
	starve()

	if got := bs.Underruns().Runs; got < 2 {
		t.Errorf(
			"Runs = %d, want at least 2 -- audio in between makes two "+
				"episodes, not one",
			got,
		)
	}
}

// TestTheStallResetDoesNotClearTheCounter is the regression this file
// is really about.
//
// resetStarvationLocked runs on every arriving sample and on every
// Flush. If it cleared the cumulative count too, the counter would
// report zero on exactly the workload it exists to measure -- a stream
// that underruns repeatedly but always recovers -- which is
// indistinguishable from healthy playback and is what the code did
// before #135.
func TestTheStallResetDoesNotClearTheCounter(t *testing.T) {
	t.Parallel()

	bs := NewBufferedStreamer(stalledStreamer{}, 1024)
	defer bs.Close()

	buf := make([][2]float64, 128)

	if _, ok := bs.Stream(buf); !ok {
		t.Fatal("the stall budget ran out before the test did")
	}

	before := bs.Underruns()
	if before.Runs == 0 {
		t.Fatal("nothing was counted, so the reset cannot be tested")
	}

	// Both of the ways a run is forgotten.
	bs.mu.Lock()
	bs.resetStarvationLocked()
	bs.mu.Unlock()

	bs.Flush()

	if got := bs.Underruns(); got != before {
		t.Errorf(
			"forgetting the stall run also discarded the count: %+v, "+
				"want %+v",
			got, before,
		)
	}
}

// TestUnderrunDeltaNeverGoesBackwards covers the one arithmetic trap in
// the reporting side.
//
// The counter belongs to the streamer and the streamer is replaced on
// every track, so a baseline carried across a track change is the
// previous track's total subtracted from a fresh zero. The load path
// resets the baseline, and this clamps as well -- a negative count in a
// log line reads as a broken instrument, which would discredit the
// measurement rather than merely mis-state it.
func TestUnderrunDeltaNeverGoesBackwards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		now  UnderrunStats
		last UnderrunStats
		want UnderrunStats
	}{
		{
			name: "ordinary progress",
			now:  UnderrunStats{Runs: 5, Calls: 40, Samples: 4000},
			last: UnderrunStats{Runs: 2, Calls: 10, Samples: 1000},
			want: UnderrunStats{Runs: 3, Calls: 30, Samples: 3000},
		},
		{
			name: "nothing happened",
			now:  UnderrunStats{Runs: 5, Calls: 40, Samples: 4000},
			last: UnderrunStats{Runs: 5, Calls: 40, Samples: 4000},
			want: UnderrunStats{},
		},
		{
			name: "a new streamer, with a stale baseline",
			now:  UnderrunStats{},
			last: UnderrunStats{Runs: 9, Calls: 90, Samples: 9000},
			want: UnderrunStats{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := underrunDelta(tt.now, tt.last); got != tt.want {
				t.Errorf("underrunDelta = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// gatedStreamer produces only what it has been allowed to, and
// otherwise stalls without ending -- so a test can decide exactly when
// the ring runs dry.
type gatedStreamer struct {
	mu        sync.Mutex
	remaining int
}

func (g *gatedStreamer) allow(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.remaining += n
}

func (g *gatedStreamer) Stream(samples [][2]float64) (int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.remaining <= 0 {
		return 0, true
	}

	n := min(len(samples), g.remaining)

	for i := range n {
		samples[i] = [2]float64{0.25, 0.25}
	}

	g.remaining -= n

	return n, true
}

func (g *gatedStreamer) Err() error { return nil }

package queue

import (
	"log/slog"
	"testing"
)

// newTestQueueDirect creates a Queue with direct field manipulation
// (no DB needed) for pure navigation logic tests.
func newTestQueueDirect(tracks int, currentIndex int) *Queue {
	q := &Queue{
		logger:     slog.Default(),
		repeatMode: RepeatOff,
	}

	q.tracks = make([]Track, tracks)
	for i := 0; i < tracks; i++ {
		q.tracks[i] = Track{FilePath: "/test/track.mp3", Position: int64(i)}
	}

	q.currentIndex = currentIndex

	return q
}

func TestNextIndex_NormalMode_AdvancesToNextTrack(t *testing.T) {
	t.Parallel()

	q := newTestQueueDirect(5, 2)

	got := q.nextIndex()
	if got != 3 {
		t.Errorf("nextIndex: got %d, want 3", got)
	}
}

func TestNextIndex_NormalMode_EndOfQueue_RepeatOff(t *testing.T) {
	t.Parallel()

	q := newTestQueueDirect(5, 4)

	got := q.nextIndex()
	if got != -1 {
		t.Errorf("nextIndex at end (repeatOff): got %d, want -1", got)
	}
}

func TestNextIndex_NormalMode_EndOfQueue_RepeatAll(t *testing.T) {
	t.Parallel()

	q := newTestQueueDirect(5, 4)
	q.repeatMode = RepeatAll

	got := q.nextIndex()
	if got != 0 {
		t.Errorf("nextIndex at end (repeatAll): got %d, want 0", got)
	}
}

func TestNextIndex_RepeatOne(t *testing.T) {
	t.Parallel()

	// Note: RepeatOne is handled in the Next() method, not nextIndex().
	// nextIndex() with RepeatOne still advances normally — the repeat-one
	// logic replays the current track before calling nextIndex().
	// This test verifies nextIndex advances in the RepeatOne case.
	q := newTestQueueDirect(5, 2)
	q.repeatMode = RepeatOne

	got := q.nextIndex()
	// nextIndex itself doesn't handle RepeatOne — it just advances.
	if got != 3 {
		t.Errorf("nextIndex (repeatOne): got %d, want 3", got)
	}
}

func TestPreviousIndex_NormalMode_GoesBack(t *testing.T) {
	t.Parallel()

	q := newTestQueueDirect(5, 3)

	got := q.previousIndex()
	if got != 2 {
		t.Errorf("previousIndex: got %d, want 2", got)
	}
}

func TestPreviousIndex_AtStart_RepeatOff(t *testing.T) {
	t.Parallel()

	q := newTestQueueDirect(5, 0)

	got := q.previousIndex()
	if got != -1 {
		t.Errorf("previousIndex at start (repeatOff): got %d, want -1", got)
	}
}

func TestPreviousIndex_AtStart_RepeatAll(t *testing.T) {
	t.Parallel()

	q := newTestQueueDirect(5, 0)
	q.repeatMode = RepeatAll

	got := q.previousIndex()
	if got != 4 {
		t.Errorf("previousIndex at start (repeatAll): got %d, want 4", got)
	}
}

func TestGenerateShuffleOrder_Properties(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		trackCount int
		currentIdx int
	}{
		{"single track", 1, 0},
		{"five tracks", 5, 2},
		{"twenty tracks", 20, 10},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			q := newTestQueueDirect(tc.trackCount, tc.currentIdx)
			q.generateShuffleOrder()

			// Property 1: length matches track count.
			if got := len(q.shuffleOrder); got != tc.trackCount {
				t.Errorf("shuffleOrder length: got %d, want %d", got, tc.trackCount)
			}

			// Property 2: current track is at shuffleOrder[0].
			if q.shuffleOrder[0] != tc.currentIdx {
				t.Errorf("shuffleOrder[0]: got %d, want %d (currentIndex)", q.shuffleOrder[0], tc.currentIdx)
			}

			// Property 3: all indices present (no duplicates, no missing).
			seen := make(map[int]bool, tc.trackCount)
			for _, idx := range q.shuffleOrder {
				if idx < 0 || idx >= tc.trackCount {
					t.Errorf("shuffleOrder contains out-of-range index: %d", idx)
				}

				if seen[idx] {
					t.Errorf("shuffleOrder contains duplicate index: %d", idx)
				}

				seen[idx] = true
			}

			if len(seen) != tc.trackCount {
				t.Errorf("unique indices in shuffleOrder: got %d, want %d", len(seen), tc.trackCount)
			}
		})
	}
}

func TestNextIndex_ShuffleMode(t *testing.T) {
	t.Parallel()

	q := newTestQueueDirect(5, 2)
	q.shuffleMode = true
	// Set a known shuffle order: [2, 4, 0, 3, 1]
	// Current index is 2, which is at shuffleOrder[0].
	q.shuffleOrder = []int{2, 4, 0, 3, 1}

	// Next in shuffle order should be shuffleOrder[1] = 4.
	got := q.nextIndex()
	if got != 4 {
		t.Errorf("nextIndex (shuffle): got %d, want 4", got)
	}

	// Advance to index 4 and get next.
	q.currentIndex = 4
	got = q.nextIndex()
	if got != 0 {
		t.Errorf("nextIndex (shuffle, pos 2): got %d, want 0", got)
	}

	// At the end of shuffle order with RepeatOff.
	q.currentIndex = 1 // last in shuffleOrder
	got = q.nextIndex()
	if got != -1 {
		t.Errorf("nextIndex (shuffle, end, repeatOff): got %d, want -1", got)
	}

	// At the end of shuffle order with RepeatAll.
	q.repeatMode = RepeatAll
	got = q.nextIndex()
	if got != 2 {
		t.Errorf("nextIndex (shuffle, end, repeatAll): got %d, want 2 (wraps to shuffleOrder[0])", got)
	}
}

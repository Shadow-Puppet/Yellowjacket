package queue

import (
	"errors"
	"testing"

	"yellowjacket/backend/events"
)

// errTestDecode stands in for a decoder blowing up mid-track.
var errTestDecode = errors.New("decode blew up")

// currentIndex == -1 against a non-empty queue is a state this
// package produces on purpose: onQueueExhausted(false) sets it and
// deliberately leaves the finished track loaded in the player, so it
// stays on the now-playing bar. Pressing play from there and letting
// it finish re-enters OnPlaybackFinished with exactly that pair --
// which used to index q.tracks[-1] and panic, on a goroutine
// dispatched from the audio callback with no caller to recover it.
func TestFinishedWithNoCurrentTrackDoesNotPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		index int
	}{
		{"exhausted queue leaves -1", -1},
		{"index past the end", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q, _, _ := setupRecordedQueue(t)
			q.tracks = []Track{
				{FilePath: "/a.mp3"},
				{FilePath: "/b.mp3"},
			}
			q.currentIndex = tt.index

			// The assertion is that this returns at all.
			q.OnPlaybackFinished(nil)

			if q.currentIndex != tt.index {
				t.Errorf(
					"an out-of-range index was acted on: %d became %d",
					tt.index, q.currentIndex,
				)
			}
		})
	}
}

// A track that broke mid-playback is not a track that was listened
// to. The player cannot say so itself -- the metadata is here -- so
// it hands the reason over and this is where it becomes a
// PlaybackFailed rather than a silent auto-advance.
func TestAFailedTrackIsReportedAndNotCountedAsAPlay(t *testing.T) {
	t.Parallel()

	q, _, rec := setupRecordedQueue(t)
	q.tracks = []Track{
		{FilePath: "/a.mp3", Title: "A", AudioFileID: 1},
		{FilePath: "/b.mp3", Title: "B", AudioFileID: 2},
	}
	q.currentIndex = 0

	q.OnPlaybackFinished(errTestDecode)

	if _, ok := rec.Last(events.PlaybackFailed); !ok {
		t.Errorf(
			"a track that failed mid-playback told the user nothing; "+
				"got %v",
			rec.Names(),
		)
	}
}

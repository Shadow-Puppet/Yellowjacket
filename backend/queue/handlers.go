package queue

// OnPlaybackFinished is called when a track stops streaming. This
// drives the auto-advance behavior and records the play.
//
// srcErr says why the track stopped: nil for one that reached its
// end, non-nil for one that broke partway through.  The player cannot
// tell the user which, because the metadata lives here -- so a failure
// is reported as PlaybackFailed and *not* recorded as a play, while
// the advance happens either way.  Before this, a file that failed
// mid-track advanced in silence and was counted as listened to.
//
//wails:ignore // internal wiring, not part of the app's IPC surface.
func (q *Queue) OnPlaybackFinished(srcErr error) {
	q.mu.Lock()

	// currentIndex is -1 whenever the queue has been exhausted, and
	// onQueueExhausted deliberately leaves the finished track loaded
	// in the player -- so a natural finish can re-enter here against a
	// queue that is not empty and an index that is not valid.  Every
	// other path in this package bounds-checks before indexing; this
	// one panicked, on a goroutine with no caller to recover it.
	if q.currentIndex < 0 || q.currentIndex >= len(q.tracks) {
		q.mu.Unlock()

		return
	}

	// Capture the track that just finished before advancing.
	finished := q.tracks[q.currentIndex]
	finishedID := finished.AudioFileID

	if srcErr != nil {
		q.emitPlaybackFailed(finished, srcErr)
	}

	// A track that broke was not listened to.
	recordFinished := func() {
		if srcErr == nil {
			q.recordPlay(finishedID)
		}
	}

	// Repeat One: replay the current track.
	if q.repeatMode == RepeatOne {
		if q.playCurrentTrack() {
			q.emitIndexChanged()
		}

		q.mu.Unlock()
		recordFinished()

		return
	}

	nextIdx := q.nextIndex()
	if nextIdx == -1 {
		// Queue exhausted — this is the extension point for a future fallback playlist.
		q.onQueueExhausted(false)
		q.mu.Unlock()
		recordFinished()

		return
	}

	q.currentIndex = nextIdx

	// Skip over tracks that cannot be played instead of reverting.
	// Reverting stopped playback dead on the first moved file and left
	// Next unable to get past it, since Next hit the same track.
	if !q.playCurrentOrSkip(true, q.nextIndex) {
		q.onQueueExhausted(false)
		q.mu.Unlock()
		recordFinished()

		return
	}

	q.emitIndexChanged()
	q.mu.Unlock()
	recordFinished()
}

package queue

// OnPlaybackFinished is called when a track finishes playing naturally.
// This drives the auto-advance behavior and records the play.
func (q *Queue) OnPlaybackFinished() {
	q.mu.Lock()

	if len(q.tracks) == 0 {
		q.mu.Unlock()

		return
	}

	// Capture the track that just finished before advancing.
	finishedID := q.tracks[q.currentIndex].AudioFileID

	// Repeat One: replay the current track.
	if q.repeatMode == RepeatOne {
		if q.playCurrentTrack() {
			q.emitIndexChanged()
		}

		q.mu.Unlock()
		q.recordPlay(finishedID)

		return
	}

	nextIdx := q.nextIndex()
	if nextIdx == -1 {
		// Queue exhausted — this is the extension point for a future fallback playlist.
		q.onQueueExhausted(false)
		q.mu.Unlock()
		q.recordPlay(finishedID)

		return
	}

	q.currentIndex = nextIdx

	// Skip over tracks that cannot be played instead of reverting.
	// Reverting stopped playback dead on the first moved file and left
	// Next unable to get past it, since Next hit the same track.
	if !q.playCurrentOrSkip(true, q.nextIndex) {
		q.onQueueExhausted(false)
		q.mu.Unlock()
		q.recordPlay(finishedID)

		return
	}

	q.emitIndexChanged()
	q.mu.Unlock()
	q.recordPlay(finishedID)
}

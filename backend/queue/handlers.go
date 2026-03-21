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
		q.onQueueExhausted()
		q.mu.Unlock()
		q.recordPlay(finishedID)

		return
	}

	prevIndex := q.currentIndex
	q.currentIndex = nextIdx

	if !q.playCurrentTrack() {
		q.currentIndex = prevIndex
		q.mu.Unlock()
		q.recordPlay(finishedID)

		return
	}

	q.emitIndexChanged()
	q.mu.Unlock()
	q.recordPlay(finishedID)
}

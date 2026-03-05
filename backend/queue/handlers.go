package queue

// OnPlaybackFinished is called when a track finishes playing naturally.
// This drives the auto-advance behavior.
func (q *Queue) OnPlaybackFinished() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 {
		return
	}

	// Repeat One: replay the current track.
	if q.repeatMode == RepeatOne {
		if q.playCurrentTrack() {
			q.emitIndexChanged()
		}

		return
	}

	nextIdx := q.nextIndex()
	if nextIdx == -1 {
		// Queue exhausted — this is the extension point for a future fallback playlist.
		q.onQueueExhausted()

		return
	}

	prevIndex := q.currentIndex
	q.currentIndex = nextIdx

	if !q.playCurrentTrack() {
		q.currentIndex = prevIndex

		return
	}

	q.emitIndexChanged()
}

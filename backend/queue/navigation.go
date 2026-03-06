package queue

import "math/rand/v2"

// nextIndex returns the next track index respecting shuffle and repeat modes.
// Returns -1 if there is no next track (queue exhausted).
func (q *Queue) nextIndex() int {
	if len(q.tracks) == 0 {
		return -1
	}

	if q.shuffleMode && len(q.shuffleOrder) > 0 {
		return q.nextShuffledIndex()
	}

	next := q.currentIndex + 1
	if next >= len(q.tracks) {
		if q.repeatMode == RepeatAll {
			return 0
		}

		return -1
	}

	return next
}

// previousIndex returns the previous track index respecting shuffle and repeat.
// Returns -1 if there is no previous track.
func (q *Queue) previousIndex() int {
	if len(q.tracks) == 0 {
		return -1
	}

	if q.shuffleMode && len(q.shuffleOrder) > 0 {
		return q.previousShuffledIndex()
	}

	prev := q.currentIndex - 1
	if prev < 0 {
		if q.repeatMode == RepeatAll {
			return len(q.tracks) - 1
		}

		return -1
	}

	return prev
}

// nextShuffledIndex finds the next index in the shuffle order.
func (q *Queue) nextShuffledIndex() int {
	shufflePos := q.currentShufflePosition()
	if shufflePos == -1 {
		// Current track not found in shuffle order — shouldn't happen.
		return -1
	}

	nextShufflePos := shufflePos + 1
	if nextShufflePos >= len(q.shuffleOrder) {
		if q.repeatMode == RepeatAll {
			return q.shuffleOrder[0]
		}

		return -1
	}

	return q.shuffleOrder[nextShufflePos]
}

// previousShuffledIndex finds the previous index in the shuffle order.
func (q *Queue) previousShuffledIndex() int {
	shufflePos := q.currentShufflePosition()
	if shufflePos == -1 {
		return -1
	}

	prevShufflePos := shufflePos - 1
	if prevShufflePos < 0 {
		if q.repeatMode == RepeatAll {
			return q.shuffleOrder[len(q.shuffleOrder)-1]
		}

		return -1
	}

	return q.shuffleOrder[prevShufflePos]
}

// currentShufflePosition finds where the current track index is in the shuffle order.
func (q *Queue) currentShufflePosition() int {
	for i, idx := range q.shuffleOrder {
		if idx == q.currentIndex {
			return i
		}
	}

	return -1
}

// generateShuffleOrder creates a Fisher-Yates shuffled index order,
// placing the current track at position 0 so it doesn't replay immediately.
func (q *Queue) generateShuffleOrder() {
	n := len(q.tracks)
	if n == 0 {
		q.shuffleOrder = nil

		return
	}

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}

	// Fisher-Yates shuffle.
	for i := n - 1; i > 0; i-- {
		j := rand.IntN(i + 1)
		order[i], order[j] = order[j], order[i]
	}

	// Move the current track to position 0 so it doesn't replay immediately.
	for i, idx := range order {
		if idx == q.currentIndex {
			order[0], order[i] = order[i], order[0]

			break
		}
	}

	q.shuffleOrder = order
}

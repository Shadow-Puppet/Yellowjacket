package queue

import (
	"yellowjacket/backend/events"
)

// emitQueueChanged emits the full queue state to the frontend.
func (q *Queue) emitQueueChanged() {
	state := State{
		Tracks:           q.tracks,
		CurrentIndex:     q.currentIndex,
		ShuffleMode:      q.shuffleMode,
		RepeatMode:       q.repeatMode,
		SourcePlaylistID: q.sourcePlaylistID,
	}

	// Ensure tracks is never nil in JSON.
	if state.Tracks == nil {
		state.Tracks = []Track{}
	}

	events.Emit(q.ctx, events.QueueChanged, state)
}

// emitIndexChanged emits only the current index to the frontend.
func (q *Queue) emitIndexChanged() {
	events.Emit(
		q.ctx,
		events.QueueIndexChanged,
		IndexChanged{CurrentIndex: q.currentIndex},
	)
}

// emitModeChanged emits only the shuffle/repeat mode to the frontend.
func (q *Queue) emitModeChanged() {
	events.Emit(
		q.ctx,
		events.QueueModeChanged,
		ModeChanged{
			ShuffleMode: q.shuffleMode,
			RepeatMode:  q.repeatMode,
		},
	)
}

// emitTracksModified emits a delta update for track list changes.
func (q *Queue) emitTracksModified(
	action string,
	tracks []Track,
	index int,
	positions []int,
) {
	events.Emit(
		q.ctx,
		events.QueueTracksModified,
		TracksModified{
			Action:       action,
			Tracks:       tracks,
			Index:        index,
			Positions:    positions,
			CurrentIndex: q.currentIndex,
		},
	)
}

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

// emitPlaybackFailed tells the frontend that a track could not be
// played.  Before this existed the failure was logged, the index was
// reverted and nothing reached the user: a moved file was a button
// that did nothing, twice, forever (errors.C1).
func (q *Queue) emitPlaybackFailed(track Track, reason error) {
	msg := ""
	if reason != nil {
		msg = reason.Error()
	}

	events.Emit(
		q.ctx,
		events.PlaybackFailed,
		PlaybackFailure{
			FilePath: track.FilePath,
			Title:    track.Title,
			Artist:   track.Artist,
			Reason:   msg,
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

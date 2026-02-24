package queue

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/events"
)

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
		q.playCurrentTrack()
		q.emitIndexChanged()

		return
	}

	nextIdx := q.nextIndex()
	if nextIdx == -1 {
		// Queue exhausted — this is the extension point for a future fallback playlist.
		q.onQueueExhausted()

		return
	}

	q.currentIndex = nextIdx
	q.playCurrentTrack()
	q.emitIndexChanged()
}

// registerEventHandlers sets up Wails event listeners for queue commands.
func (q *Queue) registerEventHandlers() {
	if q.ctx == nil {
		q.logger.Error("Context is nil, cannot register event handlers")

		return
	}

	runtime.EventsOn(q.ctx, events.RequestPlay, func(_ ...any) {
		q.logger.Info("Received RequestPlay")
		q.Play()
	})

	runtime.EventsOn(q.ctx, events.RequestNext, func(_ ...any) {
		q.logger.Info("Received RequestNext")
		q.Next()
	})

	runtime.EventsOn(q.ctx, events.RequestPrevious, func(_ ...any) {
		q.logger.Info("Received RequestPrevious")
		q.Previous()
	})

	runtime.EventsOn(q.ctx, events.RequestSetQueue, func(data ...any) {
		q.logger.Info("Received RequestSetQueue")
		q.handleSetQueue(data...)
	})

	runtime.EventsOn(q.ctx, events.RequestAddToQueue, func(data ...any) {
		q.logger.Info("Received RequestAddToQueue")
		q.handleAddToQueue(data...)
	})

	runtime.EventsOn(q.ctx, events.RequestPlayNext, func(data ...any) {
		q.logger.Info("Received RequestPlayNext")
		q.handlePlayNext(data...)
	})

	runtime.EventsOn(
		q.ctx,
		events.RequestRemoveFromQueue,
		func(data ...any) {
			q.logger.Info("Received RequestRemoveFromQueue")
			q.handleRemoveFromQueue(data...)
		},
	)

	runtime.EventsOn(
		q.ctx,
		events.RequestToggleShuffle,
		func(_ ...any) {
			q.logger.Info("Received RequestToggleShuffle")
			q.ToggleShuffle()
		},
	)

	runtime.EventsOn(
		q.ctx,
		events.RequestCycleRepeat,
		func(_ ...any) {
			q.logger.Info("Received RequestCycleRepeat")
			q.CycleRepeat()
		},
	)

	runtime.EventsOn(
		q.ctx,
		events.RequestAddTracksToQueue,
		func(data ...any) {
			q.logger.Info("Received RequestAddTracksToQueue")
			q.handleAddTracksToQueue(data...)
		},
	)

	runtime.EventsOn(
		q.ctx,
		events.RequestPlayTracksNext,
		func(data ...any) {
			q.logger.Info("Received RequestPlayTracksNext")
			q.handlePlayTracksNext(data...)
		},
	)

	runtime.EventsOn(
		q.ctx,
		events.RequestPlayQueueIndex,
		func(data ...any) {
			q.logger.Info("Received RequestPlayQueueIndex")
			q.handlePlayQueueIndex(data...)
		},
	)

	runtime.EventsOn(
		q.ctx,
		events.RequestRemoveTracksFromQueue,
		func(data ...any) {
			q.logger.Info(
				"Received RequestRemoveTracksFromQueue",
			)
			q.handleRemoveTracksFromQueue(data...)
		},
	)

	runtime.EventsOn(
		q.ctx,
		events.RequestInsertTracksAtIndex,
		func(data ...any) {
			q.logger.Info(
				"Received RequestInsertTracksAtIndex",
			)
			q.handleInsertTracksAtIndex(data...)
		},
	)

	runtime.EventsOn(
		q.ctx,
		events.RequestMoveQueueTracks,
		func(data ...any) {
			q.logger.Info(
				"Received RequestMoveQueueTracks",
			)
			q.handleMoveQueueTracks(data...)
		},
	)

	runtime.EventsOn(
		q.ctx,
		events.RequestClearQueue,
		func(_ ...any) {
			q.logger.Info("Received RequestClearQueue")
			q.Clear()
		},
	)
}

// toStringSlice extracts strings from a Wails event argument.
func toStringSlice(raw []interface{}) []string {
	result := make([]string, 0, len(raw))

	for _, v := range raw {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}

	return result
}

// toIntSlice extracts ints (from float64) from a Wails event argument.
func toIntSlice(raw []interface{}) []int {
	result := make([]int, 0, len(raw))

	for _, v := range raw {
		if f, ok := v.(float64); ok {
			result = append(result, int(f))
		}
	}

	return result
}

// handleSetQueue processes the RequestSetQueue event payload.
// Expects data[0] = []interface{} of file path strings,
// data[1] = float64 start index, data[2] = bool shuffleStart (optional).
func (q *Queue) handleSetQueue(data ...any) {
	if len(data) < 2 {
		q.logger.Error("RequestSetQueue: missing data")

		return
	}

	filePathsRaw, ok := data[0].([]interface{})
	if !ok {
		q.logger.Error("RequestSetQueue: invalid filePaths type")

		return
	}

	filePaths := toStringSlice(filePathsRaw)

	startIndex := 0

	if si, ok := data[1].(float64); ok {
		startIndex = int(si)
	}

	shuffleStart := false

	if len(data) > 2 {
		if ss, ok := data[2].(bool); ok {
			shuffleStart = ss
		}
	}

	q.SetQueue(filePaths, startIndex, shuffleStart)
}

// handleAddToQueue processes the RequestAddToQueue event payload.
// Expects data[0] = string file path.
func (q *Queue) handleAddToQueue(data ...any) {
	if len(data) < 1 {
		q.logger.Error("RequestAddToQueue: missing data")

		return
	}

	filePath, ok := data[0].(string)
	if !ok {
		q.logger.Error(
			"RequestAddToQueue: invalid filePath type",
			"got", data[0],
		)

		return
	}

	q.AddTrack(filePath)
}

// handlePlayNext processes the RequestPlayNext event payload.
// Expects data[0] = string file path.
func (q *Queue) handlePlayNext(data ...any) {
	if len(data) < 1 {
		q.logger.Error("RequestPlayNext: missing data")

		return
	}

	filePath, ok := data[0].(string)
	if !ok {
		q.logger.Error(
			"RequestPlayNext: invalid filePath type",
			"got", data[0],
		)

		return
	}

	q.InsertNext(filePath)
}

// handleRemoveFromQueue processes the RequestRemoveFromQueue event payload.
// Expects data[0] = float64 position.
func (q *Queue) handleRemoveFromQueue(data ...any) {
	if len(data) < 1 {
		q.logger.Error("RequestRemoveFromQueue: missing data")

		return
	}

	position, ok := data[0].(float64)
	if !ok {
		q.logger.Error(
			"RequestRemoveFromQueue: invalid position type",
			"got", data[0],
		)

		return
	}

	q.RemoveTrack(int(position))
}

// handleRemoveTracksFromQueue processes the RequestRemoveTracksFromQueue
// event payload. Expects data[0] = []interface{} of float64 positions.
func (q *Queue) handleRemoveTracksFromQueue(data ...any) {
	if len(data) < 1 {
		q.logger.Error(
			"RequestRemoveTracksFromQueue: missing data",
		)

		return
	}

	positionsRaw, ok := data[0].([]interface{})
	if !ok {
		q.logger.Error(
			"RequestRemoveTracksFromQueue: invalid positions type",
			"got", data[0],
		)

		return
	}

	q.RemoveTracks(toIntSlice(positionsRaw))
}

// handleAddTracksToQueue processes the RequestAddTracksToQueue event payload.
// Expects data[0] = []interface{} of file path strings.
func (q *Queue) handleAddTracksToQueue(data ...any) {
	if len(data) < 1 {
		q.logger.Error("RequestAddTracksToQueue: missing data")

		return
	}

	filePathsRaw, ok := data[0].([]interface{})
	if !ok {
		q.logger.Error(
			"RequestAddTracksToQueue: invalid filePaths type",
			"got", data[0],
		)

		return
	}

	q.AddTracks(toStringSlice(filePathsRaw))
}

// handleInsertTracksAtIndex processes the RequestInsertTracksAtIndex event
// payload. Expects data[0] = []interface{} of file path strings,
// data[1] = float64 target index.
func (q *Queue) handleInsertTracksAtIndex(data ...any) {
	if len(data) < 2 {
		q.logger.Error(
			"RequestInsertTracksAtIndex: missing data",
		)

		return
	}

	filePathsRaw, ok := data[0].([]interface{})
	if !ok {
		q.logger.Error(
			"RequestInsertTracksAtIndex: invalid filePaths type",
			"got", data[0],
		)

		return
	}

	idx, ok := data[1].(float64)
	if !ok {
		q.logger.Error(
			"RequestInsertTracksAtIndex: invalid index type",
			"got", data[1],
		)

		return
	}

	q.InsertTracksAt(toStringSlice(filePathsRaw), int(idx))
}

// handleMoveQueueTracks processes the RequestMoveQueueTracks event payload.
// Expects data[0] = []interface{} of float64 source indices,
// data[1] = float64 target index.
func (q *Queue) handleMoveQueueTracks(data ...any) {
	if len(data) < 2 {
		q.logger.Error(
			"RequestMoveQueueTracks: missing data",
		)

		return
	}

	indicesRaw, ok := data[0].([]interface{})
	if !ok {
		q.logger.Error(
			"RequestMoveQueueTracks: invalid indices type",
			"got", data[0],
		)

		return
	}

	toIdx, ok := data[1].(float64)
	if !ok {
		q.logger.Error(
			"RequestMoveQueueTracks: invalid toIndex type",
			"got", data[1],
		)

		return
	}

	q.MoveQueueTracks(toIntSlice(indicesRaw), int(toIdx))
}

// handlePlayQueueIndex processes the RequestPlayQueueIndex event payload.
// Expects data[0] = float64 index.
func (q *Queue) handlePlayQueueIndex(data ...any) {
	if len(data) < 1 {
		q.logger.Error("RequestPlayQueueIndex: missing data")

		return
	}

	index, ok := data[0].(float64)
	if !ok {
		q.logger.Error(
			"RequestPlayQueueIndex: invalid index type",
			"got", data[0],
		)

		return
	}

	q.PlayIndex(int(index))
}

// handlePlayTracksNext processes the RequestPlayTracksNext event payload.
// Expects data[0] = []interface{} of file path strings.
func (q *Queue) handlePlayTracksNext(data ...any) {
	if len(data) < 1 {
		q.logger.Error("RequestPlayTracksNext: missing data")

		return
	}

	filePathsRaw, ok := data[0].([]interface{})
	if !ok {
		q.logger.Error(
			"RequestPlayTracksNext: invalid filePaths type",
			"got", data[0],
		)

		return
	}

	q.InsertNextTracks(toStringSlice(filePathsRaw))
}

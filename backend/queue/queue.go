// Package queue manages the playback queue and auto-advance logic.
package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
)

// RepeatMode represents the queue repeat behavior.
type RepeatMode string

// Repeat mode values.
const (
	RepeatOff RepeatMode = "off"
	RepeatAll RepeatMode = "all"
	RepeatOne RepeatMode = "one"
)

// PreviousRestartThreshold is the number of seconds into a track before
// "Previous" restarts the current track instead of going to the prior one.
const PreviousRestartThreshold = 3

// maxSQLiteVars is the maximum number of bind variables SQLite supports
// per statement. We use a conservative limit for batching.
const maxSQLiteVars = 900

// initialBatchSize is the number of tracks resolved eagerly in the first
// phase of SetQueue so the queue panel is populated immediately.
const initialBatchSize = 50

// trackMeta holds the result of a batch metadata lookup.
type trackMeta struct {
	AudioFileID int64
	FilePath    string
	Title       string
	Artist      string
}

// TrackLoader is the interface the queue uses to tell the player to load a file.
type TrackLoader interface {
	LoadFile(filePath string) error
	Play() error
	IsPlaying() bool
	CurrentPositionSeconds() (int, error)
	UnloadTrack()
}

// Track represents a track in the queue with its metadata.
type Track struct {
	ID          int64  `json:"id"`
	AudioFileID int64  `json:"audioFileId"`
	FilePath    string `json:"filePath"`
	Position    int64  `json:"position"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
}

// State is the full state emitted to the frontend.
type State struct {
	Tracks           []Track    `json:"tracks"`
	CurrentIndex     int        `json:"currentIndex"`
	ShuffleMode      bool       `json:"shuffleMode"`
	RepeatMode       RepeatMode `json:"repeatMode"`
	SourcePlaylistID int64      `json:"sourcePlaylistId"`
}

// IndexChanged is the payload for the QueueIndexChanged event.
type IndexChanged struct {
	CurrentIndex int `json:"currentIndex"`
}

// ModeChanged is the payload for the QueueModeChanged event.
type ModeChanged struct {
	ShuffleMode bool       `json:"shuffleMode"`
	RepeatMode  RepeatMode `json:"repeatMode"`
}

// TracksModified is the payload for the QueueTracksModified event.
type TracksModified struct {
	Action       string  `json:"action"`
	Tracks       []Track `json:"tracks,omitempty"`
	Index        int     `json:"index"`
	Positions    []int   `json:"positions,omitempty"`
	CurrentIndex int     `json:"currentIndex"`
}

// Queue manages an ordered list of tracks for playback.
type Queue struct {
	ctx    context.Context
	logger *slog.Logger
	db     *database.DB
	player TrackLoader

	mu               sync.Mutex
	tracks           []Track
	currentIndex     int
	shuffleMode      bool
	repeatMode       RepeatMode
	shuffleOrder     []int
	sourcePlaylistID int64

	// setQueueGen is incremented each time SetQueue is called. Background
	// goroutines check this to detect if they have been superseded.
	setQueueGen atomic.Int64
}

// NewQueue creates a new queue manager.
func NewQueue(logger *slog.Logger, db *database.DB) *Queue {
	return &Queue{
		logger:     logger.WithGroup("queue"),
		db:         db,
		repeatMode: RepeatOff,
	}
}

// SetContext sets the Wails runtime context and registers event handlers.
func (q *Queue) SetContext(ctx context.Context) {
	q.ctx = ctx
	q.registerEventHandlers()
}

// SetPlayer provides the queue with a reference to the player for auto-advance.
func (q *Queue) SetPlayer(player TrackLoader) {
	q.player = player
}

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
		q.PlayFromStart()
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
}

// handleSetQueue processes the RequestSetQueue event payload.
// Expects data[0] = []interface{} of file path strings, data[1] = float64 start index.
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

	filePaths := make([]string, 0, len(filePathsRaw))

	for _, fp := range filePathsRaw {
		if s, ok := fp.(string); ok {
			filePaths = append(filePaths, s)
		}
	}

	startIndex := 0

	if si, ok := data[1].(float64); ok {
		startIndex = int(si)
	}

	q.SetQueue(filePaths, startIndex)
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

	positions := make([]int, 0, len(positionsRaw))

	for _, p := range positionsRaw {
		if f, ok := p.(float64); ok {
			positions = append(positions, int(f))
		}
	}

	q.RemoveTracks(positions)
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

	filePaths := make([]string, 0, len(filePathsRaw))

	for _, fp := range filePathsRaw {
		if s, ok := fp.(string); ok {
			filePaths = append(filePaths, s)
		}
	}

	q.AddTracks(filePaths)
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

	filePaths := make([]string, 0, len(filePathsRaw))

	for _, fp := range filePathsRaw {
		if s, ok := fp.(string); ok {
			filePaths = append(filePaths, s)
		}
	}

	q.InsertNextTracks(filePaths)
}

// SetQueue replaces the entire queue with new tracks and starts playing.
// It uses a two-phase approach: the first batch of tracks (up to
// initialBatchSize) is resolved immediately so playback begins and the
// queue panel is populated without delay. The remaining tracks are then
// resolved in the background. A generation counter ensures stale
// background work is discarded if SetQueue is called again.
func (q *Queue) SetQueue(filePaths []string, startIndex int) {
	gen := q.setQueueGen.Add(1)

	if startIndex < 0 || startIndex >= len(filePaths) {
		startIndex = 0
	}

	// Phase 1: resolve an initial window of tracks centered on startIndex
	// so the queue panel is populated around the playing track immediately.
	windowStart := max(0, startIndex-initialBatchSize/2)
	windowEnd := min(len(filePaths), windowStart+initialBatchSize)
	windowStart = max(0, windowEnd-initialBatchSize)

	initialPaths := filePaths[windowStart:windowEnd]

	batchMeta := q.lookupTrackMetaBatch(initialPaths)

	q.mu.Lock()

	// Build the initial tracks slice preserving original order.
	tracks := make([]Track, 0, len(initialPaths))

	for i, fp := range initialPaths {
		m, ok := batchMeta[fp]
		if !ok {
			continue
		}

		tracks = append(tracks, Track{
			AudioFileID: m.AudioFileID,
			FilePath:    m.FilePath,
			Position:    int64(i),
			Title:       m.Title,
			Artist:      m.Artist,
		})
	}

	if len(tracks) == 0 {
		q.logger.Warn("No tracks found in initial batch")
		q.mu.Unlock()

		return
	}

	q.tracks = tracks
	q.sourcePlaylistID = 0
	q.shuffleOrder = nil

	// Find the start track within the initial batch.
	q.currentIndex = 0
	startPath := filePaths[startIndex]

	for i, t := range q.tracks {
		if t.FilePath == startPath {
			q.currentIndex = i

			break
		}
	}

	// Start playing immediately.
	q.playCurrentTrack()
	q.emitQueueChanged()
	q.mu.Unlock()

	// Phase 2: if there are more tracks beyond the initial batch,
	// resolve them in the background. If everything fits in the initial
	// batch we can persist and finish synchronously.
	if len(filePaths) <= initialBatchSize {
		q.mu.Lock()
		q.persistTracks()
		q.persistState()
		q.mu.Unlock()

		return
	}

	go q.resolveRemainingTracks(gen, filePaths, startIndex)
}

// resolveRemainingTracks runs in a goroutine to batch-resolve all tracks
// for a SetQueue call. It checks the generation counter before applying
// results to avoid overwriting a newer SetQueue call.
func (q *Queue) resolveRemainingTracks(
	gen int64,
	filePaths []string,
	startIndex int,
) {
	allMeta := q.lookupTrackMetaBatch(filePaths)

	// Check if we have been superseded before acquiring the mutex.
	if q.setQueueGen.Load() != gen {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Double-check under the lock.
	if q.setQueueGen.Load() != gen {
		return
	}

	tracks := make([]Track, 0, len(filePaths))

	for i, fp := range filePaths {
		meta, found := allMeta[fp]
		if !found {
			q.logger.Warn(
				"Could not find audio file in database",
				"path", fp,
			)

			continue
		}

		tracks = append(tracks, Track{
			AudioFileID: meta.AudioFileID,
			FilePath:    meta.FilePath,
			Position:    int64(i),
			Title:       meta.Title,
			Artist:      meta.Artist,
		})
	}

	q.tracks = tracks

	// Recalculate startIndex: the original index might be shifted if
	// earlier tracks were missing from the database. Find the track that
	// matches the originally requested start path.
	startPath := filePaths[startIndex]
	q.currentIndex = 0

	for i, t := range q.tracks {
		if t.FilePath == startPath {
			q.currentIndex = i

			break
		}
	}

	if q.shuffleMode {
		q.generateShuffleOrder()
	}

	q.persistTracks()
	q.persistState()
	q.emitQueueChanged()
}

// AddTrack appends a track to the end of the queue.
// If the queue was empty, it starts playing the added track immediately.
func (q *Queue) AddTrack(filePath string) {
	meta := q.lookupTrackMetaBatch([]string{filePath})

	q.mu.Lock()
	defer q.mu.Unlock()

	m, ok := meta[filePath]
	if !ok {
		q.logger.Error(
			"Could not find audio file",
			"path", filePath,
		)

		return
	}

	wasEmpty := len(q.tracks) == 0

	track := Track{
		AudioFileID: m.AudioFileID,
		FilePath:    m.FilePath,
		Position:    int64(len(q.tracks)),
		Title:       m.Title,
		Artist:      m.Artist,
	}

	q.tracks = append(q.tracks, track)

	// Persist.
	_, insertErr := q.db.Queries.InsertQueueTrack(
		q.db.Ctx,
		sqlcgen.InsertQueueTrackParams{
			AudioFileID: m.AudioFileID,
			Position:    track.Position,
		},
	)
	if insertErr != nil {
		q.logger.Error("Failed to persist queue track", "err", insertErr)
	}

	// Update shuffle order if shuffle is on.
	if q.shuffleMode {
		q.shuffleOrder = append(q.shuffleOrder, len(q.tracks)-1)
	}

	// Auto-play if this is the first track added to an empty queue.
	if wasEmpty {
		q.currentIndex = 0
		q.playCurrentTrack()
	}

	q.persistState()
	q.emitTracksModified(
		"add",
		[]Track{track},
		len(q.tracks)-1,
		nil,
	)
}

// AddTracks appends multiple tracks to the end of the queue.
// If the queue was empty, it starts playing the first added track immediately.
func (q *Queue) AddTracks(filePaths []string) {
	allMeta := q.lookupTrackMetaBatch(filePaths)

	q.mu.Lock()
	defer q.mu.Unlock()

	wasEmpty := len(q.tracks) == 0
	insertIndex := len(q.tracks)

	var newTracks []Track

	for _, fp := range filePaths {
		m, ok := allMeta[fp]
		if !ok {
			q.logger.Warn(
				"Could not find audio file",
				"path", fp,
			)

			continue
		}

		track := Track{
			AudioFileID: m.AudioFileID,
			FilePath:    m.FilePath,
			Position:    int64(len(q.tracks)),
			Title:       m.Title,
			Artist:      m.Artist,
		}

		q.tracks = append(q.tracks, track)

		newTracks = append(newTracks, track)
	}

	if q.shuffleMode {
		q.generateShuffleOrder()
	}

	q.persistTracks()
	q.persistState()

	if wasEmpty && len(q.tracks) > 0 {
		q.currentIndex = 0
		q.playCurrentTrack()
	}

	q.emitTracksModified(
		"add",
		newTracks,
		insertIndex,
		nil,
	)
}

// InsertNextTracks inserts multiple tracks as a contiguous block after the current track.
func (q *Queue) InsertNextTracks(filePaths []string) {
	allMeta := q.lookupTrackMetaBatch(filePaths)

	q.mu.Lock()
	defer q.mu.Unlock()

	insertPos := q.currentIndex + 1
	if insertPos > len(q.tracks) {
		insertPos = len(q.tracks)
	}

	wasEmpty := len(q.tracks) == 0

	var newTracks []Track

	for _, fp := range filePaths {
		m, ok := allMeta[fp]
		if !ok {
			q.logger.Warn(
				"Could not find audio file",
				"path", fp,
			)

			continue
		}

		newTracks = append(newTracks, Track{
			AudioFileID: m.AudioFileID,
			FilePath:    m.FilePath,
			Title:       m.Title,
			Artist:      m.Artist,
		})
	}

	if len(newTracks) == 0 {
		return
	}

	// Insert the block into the slice at insertPos.
	tail := make([]Track, len(q.tracks[insertPos:]))
	copy(tail, q.tracks[insertPos:])
	q.tracks = append(q.tracks[:insertPos], newTracks...)
	q.tracks = append(q.tracks, tail...)

	q.reindexPositions()

	if q.shuffleMode {
		q.generateShuffleOrder()
	}

	q.persistTracks()
	q.persistState()

	if wasEmpty {
		q.currentIndex = 0
		q.playCurrentTrack()
	}

	q.emitTracksModified(
		"insert",
		newTracks,
		insertPos,
		nil,
	)
}

// InsertNext inserts a track right after the currently playing track.
func (q *Queue) InsertNext(filePath string) {
	meta := q.lookupTrackMetaBatch([]string{filePath})

	q.mu.Lock()
	defer q.mu.Unlock()

	m, ok := meta[filePath]
	if !ok {
		q.logger.Error(
			"Could not find audio file",
			"path", filePath,
		)

		return
	}

	insertPos := q.currentIndex + 1
	if insertPos > len(q.tracks) {
		insertPos = len(q.tracks)
	}

	track := Track{
		AudioFileID: m.AudioFileID,
		FilePath:    m.FilePath,
		Position:    int64(insertPos),
		Title:       m.Title,
		Artist:      m.Artist,
	}

	// Insert into slice.
	q.tracks = append(q.tracks, Track{})
	copy(q.tracks[insertPos+1:], q.tracks[insertPos:])
	q.tracks[insertPos] = track

	// Reindex positions.
	q.reindexPositions()

	// Regenerate shuffle order if needed.
	if q.shuffleMode {
		q.generateShuffleOrder()
	}

	q.persistTracks()
	q.persistState()
	q.emitTracksModified(
		"insert",
		[]Track{track},
		insertPos,
		nil,
	)
}

// RemoveTrack removes a track at the given position from the queue.
func (q *Queue) RemoveTrack(position int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if position < 0 || position >= len(q.tracks) {
		q.logger.Warn(
			"RemoveTrack: position out of range",
			"position", position,
		)

		return
	}

	removingCurrent := q.currentIndex >= 0 &&
		position == q.currentIndex

	q.tracks = append(q.tracks[:position], q.tracks[position+1:]...)

	// Adjust current index if needed. A currentIndex of -1 means no track
	// is loaded, so only shift when a valid track is selected.
	if q.currentIndex >= 0 && position < q.currentIndex {
		q.currentIndex--
	} else if position == q.currentIndex &&
		q.currentIndex >= len(q.tracks) && len(q.tracks) > 0 {
		q.currentIndex = len(q.tracks) - 1
	}

	q.reindexPositions()

	if q.shuffleMode {
		q.generateShuffleOrder()
	}

	q.persistTracks()
	q.persistState()
	q.emitTracksModified(
		"remove",
		nil,
		0,
		[]int{position},
	)

	if removingCurrent {
		q.handleCurrentTrackRemoved()
	}
}

// RemoveTracks removes multiple tracks at the given positions from the queue.
// Positions are deduplicated, validated, and removed in descending order so
// that indices remain stable during removal.
func (q *Queue) RemoveTracks(positions []int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(positions) == 0 {
		return
	}

	// Deduplicate and filter out-of-range positions.
	seen := make(map[int]bool, len(positions))

	valid := make([]int, 0, len(positions))

	for _, p := range positions {
		if p < 0 || p >= len(q.tracks) || seen[p] {
			continue
		}

		seen[p] = true

		valid = append(valid, p)
	}

	if len(valid) == 0 {
		return
	}

	removedCurrent := q.currentIndex >= 0 && seen[q.currentIndex]

	// Sort ascending so we can iterate in reverse for descending removal.
	slices.Sort(valid)

	// Remove in descending order to keep earlier indices stable.
	for i := len(valid) - 1; i >= 0; i-- {
		pos := valid[i]
		q.tracks = append(q.tracks[:pos], q.tracks[pos+1:]...)

		if q.currentIndex >= 0 && pos < q.currentIndex {
			q.currentIndex--
		} else if pos == q.currentIndex &&
			q.currentIndex >= len(q.tracks) &&
			len(q.tracks) > 0 {
			q.currentIndex = len(q.tracks) - 1
		}
	}

	q.reindexPositions()

	if q.shuffleMode {
		q.generateShuffleOrder()
	}

	q.persistTracks()
	q.persistState()
	q.emitTracksModified(
		"remove",
		nil,
		0,
		valid,
	)

	q.logger.Info(
		"Removed tracks from queue",
		"count", len(valid),
	)

	if removedCurrent {
		q.handleCurrentTrackRemoved()
	}
}

// Next advances to the next track. If the player was paused, the next
// track is loaded but not played. In RepeatOne mode, the current track
// is replayed instead of advancing.
func (q *Queue) Next() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 {
		return
	}

	wasPlaying := q.player != nil && q.player.IsPlaying()

	// Repeat One: replay the current track.
	if q.repeatMode == RepeatOne {
		q.playOrLoadCurrentTrack(wasPlaying)
		q.emitIndexChanged()

		return
	}

	nextIdx := q.nextIndex()
	if nextIdx == -1 {
		q.onQueueExhausted()

		return
	}

	q.currentIndex = nextIdx
	q.playOrLoadCurrentTrack(wasPlaying)
	q.emitIndexChanged()
}

// Previous goes to the previous track (or restarts current if >3s in).
// If the player was paused, the track is loaded but not played.
// In RepeatOne mode, the current track is replayed instead of navigating.
func (q *Queue) Previous() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 || q.currentIndex < 0 {
		return
	}

	wasPlaying := q.player != nil && q.player.IsPlaying()

	// Repeat One: replay the current track.
	if q.repeatMode == RepeatOne {
		q.playOrLoadCurrentTrack(wasPlaying)
		q.emitIndexChanged()

		return
	}

	// If more than 3 seconds into the track, restart it.
	if q.player != nil {
		posSecs, err := q.player.CurrentPositionSeconds()
		if err == nil && posSecs > PreviousRestartThreshold {
			q.playOrLoadCurrentTrack(wasPlaying)
			q.emitIndexChanged()

			return
		}
	}

	prevIdx := q.previousIndex()
	if prevIdx == -1 {
		// At the beginning — just restart the current track.
		q.playOrLoadCurrentTrack(wasPlaying)
		q.emitIndexChanged()

		return
	}

	q.currentIndex = prevIdx
	q.playOrLoadCurrentTrack(wasPlaying)
	q.emitIndexChanged()
}

// PlayFromStart restarts playback from the beginning of the queue.
// If shuffle is enabled, a new shuffle order is generated and playback
// starts from a random track. This is a no-op when a track is already
// active (currentIndex != -1) or the queue is empty.
func (q *Queue) PlayFromStart() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.currentIndex != -1 {
		return
	}

	if len(q.tracks) == 0 {
		return
	}

	if q.shuffleMode {
		q.generateShuffleOrder()
		q.currentIndex = q.shuffleOrder[0]
	} else {
		q.currentIndex = 0
	}

	q.playCurrentTrack()
	q.emitIndexChanged()
}

// PlayIndex jumps to and plays the track at the given index.
func (q *Queue) PlayIndex(index int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 {
		return
	}

	if index < 0 || index >= len(q.tracks) {
		q.logger.Warn(
			"PlayIndex: index out of range",
			"index", index, "trackCount", len(q.tracks),
		)

		return
	}

	q.currentIndex = index
	q.playCurrentTrack()
	q.emitIndexChanged()
}

// ToggleShuffle toggles shuffle mode on/off.
func (q *Queue) ToggleShuffle() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.shuffleMode = !q.shuffleMode

	if q.shuffleMode {
		q.generateShuffleOrder()
	} else {
		q.shuffleOrder = nil
	}

	q.persistState()
	q.emitModeChanged()
}

// CycleRepeat cycles through repeat modes: off -> all -> one -> off.
func (q *Queue) CycleRepeat() {
	q.mu.Lock()
	defer q.mu.Unlock()

	switch q.repeatMode {
	case RepeatOff:
		q.repeatMode = RepeatAll
	case RepeatAll:
		q.repeatMode = RepeatOne
	case RepeatOne:
		q.repeatMode = RepeatOff
	}

	q.persistState()
	q.emitModeChanged()
}

// GetState returns the current queue state for the frontend.
func (q *Queue) GetState() State {
	q.mu.Lock()
	defer q.mu.Unlock()

	tracks := make([]Track, len(q.tracks))
	copy(tracks, q.tracks)

	return State{
		Tracks:           tracks,
		CurrentIndex:     q.currentIndex,
		ShuffleMode:      q.shuffleMode,
		RepeatMode:       q.repeatMode,
		SourcePlaylistID: q.sourcePlaylistID,
	}
}

// EmitCurrentState emits the current queue state to the frontend.
// This is called after the frontend DOM is ready.
func (q *Queue) EmitCurrentState() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.emitQueueChanged()
}

// SaveState persists the queue state to the database.
func (q *Queue) SaveState() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.persistTracks()
	q.persistState()
	q.logger.Info("Queue state saved",
		"trackCount", len(q.tracks),
		"currentIndex", q.currentIndex,
		"shuffleMode", q.shuffleMode,
		"repeatMode", q.repeatMode,
	)
}

// RestoreState loads the queue state from the database.
func (q *Queue) RestoreState() {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Restore queue metadata.
	state, err := q.db.Queries.GetQueueState(q.db.Ctx)
	if err != nil {
		q.logger.Error("Failed to load queue state", "err", err)

		return
	}

	q.currentIndex = int(state.CurrentPosition)
	q.shuffleMode = state.ShuffleMode
	q.repeatMode = RepeatMode(state.RepeatMode)

	if state.SourcePlaylistID.Valid {
		q.sourcePlaylistID = state.SourcePlaylistID.Int64
	}

	// Restore shuffle order.
	if state.ShuffleOrder.Valid && state.ShuffleOrder.String != "" {
		var order []int

		if err := json.Unmarshal(
			[]byte(state.ShuffleOrder.String), &order,
		); err != nil {
			q.logger.Warn("Failed to parse shuffle order", "err", err)
		} else {
			q.shuffleOrder = order
		}
	}

	// Restore queue tracks.
	rows, err := q.db.Queries.GetQueueTracks(q.db.Ctx)
	if err != nil {
		q.logger.Error("Failed to load queue tracks", "err", err)

		return
	}

	q.tracks = make([]Track, 0, len(rows))

	for _, row := range rows {
		q.tracks = append(q.tracks, Track{
			ID:          row.ID,
			AudioFileID: row.AudioFileID,
			FilePath:    row.FilePath,
			Position:    row.Position,
			Title:       row.Title,
			Artist:      row.Artist,
		})
	}

	// Clamp current index. A value of -1 is valid and means "no current
	// track" (e.g. the queue was exhausted before shutdown). Only clamp
	// when the index exceeds the restored track count.
	if q.currentIndex >= len(q.tracks) && len(q.tracks) > 0 {
		q.currentIndex = len(q.tracks) - 1
	}

	q.logger.Info("Queue state restored",
		"trackCount", len(q.tracks),
		"currentIndex", q.currentIndex,
		"shuffleMode", q.shuffleMode,
		"repeatMode", q.repeatMode,
	)
}

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

// playOrLoadCurrentTrack loads the current track and optionally starts
// playback. When autoPlay is true it behaves like playCurrentTrack;
// when false it only loads the file (leaving the player paused).
func (q *Queue) playOrLoadCurrentTrack(autoPlay bool) {
	if autoPlay {
		q.playCurrentTrack()
	} else {
		q.loadCurrentTrack()
	}
}

// loadCurrentTrack tells the player to load the current track without
// starting playback. It persists the updated queue state. Returns true
// if the file was loaded successfully.
func (q *Queue) loadCurrentTrack() bool {
	if q.player == nil {
		q.logger.Error("No player set, cannot load track")

		return false
	}

	if q.currentIndex < 0 || q.currentIndex >= len(q.tracks) {
		q.logger.Warn(
			"Current index out of range",
			"index", q.currentIndex,
			"trackCount", len(q.tracks),
		)

		return false
	}

	track := q.tracks[q.currentIndex]
	q.logger.Info(
		"Loading track from queue",
		"filePath", track.FilePath,
		"position", q.currentIndex,
	)

	err := q.player.LoadFile(track.FilePath)
	if err != nil {
		q.logger.Error(
			"Failed to load file from queue",
			"filePath", track.FilePath, "err", err,
		)

		return false
	}

	q.persistState()

	return true
}

// playCurrentTrack tells the player to load and play the current track.
func (q *Queue) playCurrentTrack() {
	if !q.loadCurrentTrack() {
		return
	}

	err := q.player.Play()
	if err != nil {
		track := q.tracks[q.currentIndex]
		q.logger.Error(
			"Failed to play file from queue",
			"filePath", track.FilePath, "err", err,
		)
	}
}

// handleCurrentTrackRemoved handles the case where the currently loaded
// track was removed from the queue. If tracks remain it loads the track
// now at currentIndex (paused); otherwise it exhausts the queue.
func (q *Queue) handleCurrentTrackRemoved() {
	if len(q.tracks) == 0 {
		q.onQueueExhausted()

		return
	}

	q.loadCurrentTrack()
}

// onQueueExhausted is called when there are no more tracks to play.
// It unloads the current track, resets the index to -1 (no current track),
// and notifies the frontend.
func (q *Queue) onQueueExhausted() {
	q.logger.Info("Queue exhausted, unloading track")

	q.currentIndex = -1

	if q.player != nil {
		q.player.UnloadTrack()
	}

	q.emitIndexChanged()
	q.persistState()
}

// reindexPositions updates the Position field of all tracks to match slice index.
func (q *Queue) reindexPositions() {
	for i := range q.tracks {
		q.tracks[i].Position = int64(i)
	}
}

// lookupTrackMetaBatch fetches audio file IDs and metadata for a batch of
// file paths using a single query per chunk (instead of 2 queries per track).
// Returns a map keyed by file path. This is safe to call without holding q.mu.
func (q *Queue) lookupTrackMetaBatch(
	filePaths []string,
) map[string]trackMeta {
	result := make(map[string]trackMeta, len(filePaths))

	// Deduplicate paths to avoid redundant work.
	unique := make([]string, 0, len(filePaths))
	seen := make(map[string]bool, len(filePaths))

	for _, fp := range filePaths {
		if !seen[fp] {
			seen[fp] = true

			unique = append(unique, fp)
		}
	}

	// Process in chunks to stay under the SQLite bind variable limit.
	for i := 0; i < len(unique); i += maxSQLiteVars {
		end := i + maxSQLiteVars
		if end > len(unique) {
			end = len(unique)
		}

		chunk := unique[i:end]
		q.lookupChunk(chunk, result)
	}

	return result
}

// lookupChunk executes a single batch query for a chunk of file paths.
func (q *Queue) lookupChunk(
	paths []string,
	result map[string]trackMeta,
) {
	if len(paths) == 0 {
		return
	}

	placeholders := make([]string, len(paths))
	args := make([]any, len(paths))

	for i, fp := range paths {
		placeholders[i] = "?"
		args[i] = fp
	}

	query := fmt.Sprintf(
		`SELECT af.id, af.file_path,
			COALESCE(r.name, '') AS title,
			COALESCE(ac.text, '') AS artist
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		WHERE af.file_path IN (%s)`,
		strings.Join(placeholders, ","),
	)

	rows, err := q.db.QueryContext(query, args...)
	if err != nil {
		q.logger.Error("Batch metadata lookup failed", "err", err)

		return
	}

	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			q.logger.Error(
				"Failed to close rows",
				"err", closeErr,
			)
		}
	}()

	for rows.Next() {
		var m trackMeta

		if scanErr := rows.Scan(
			&m.AudioFileID, &m.FilePath, &m.Title, &m.Artist,
		); scanErr != nil {
			q.logger.Error(
				"Failed to scan batch metadata row",
				"err", scanErr,
			)

			continue
		}

		result[m.FilePath] = m
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		q.logger.Error(
			"Error iterating batch metadata rows",
			"err", rowsErr,
		)
	}
}

// persistTracks writes the current queue tracks to the database atomically
// using a transaction with batched multi-row inserts.
func (q *Queue) persistTracks() {
	tx, err := q.db.BeginTx()
	if err != nil {
		q.logger.Error("Failed to begin transaction", "err", err)

		return
	}

	committed := false

	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				q.logger.Error(
					"Failed to rollback transaction",
					"err", rbErr,
				)
			}
		}
	}()

	// Clear existing tracks.
	txQueries := q.db.Queries.WithTx(tx)

	if clearErr := txQueries.ClearQueueTracks(q.db.Ctx); clearErr != nil {
		q.logger.Error("Failed to clear queue tracks", "err", clearErr)

		return
	}

	// Batch insert tracks. Each row needs 2 bind vars (audio_file_id, position).
	const varsPerRow = 2

	batchSize := maxSQLiteVars / varsPerRow

	for i := 0; i < len(q.tracks); i += batchSize {
		end := i + batchSize
		if end > len(q.tracks) {
			end = len(q.tracks)
		}

		batch := q.tracks[i:end]

		if insertErr := q.insertTrackBatch(tx, batch); insertErr != nil {
			q.logger.Error(
				"Failed to batch insert queue tracks",
				"err", insertErr,
			)

			return
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		q.logger.Error("Failed to commit transaction", "err", commitErr)

		return
	}

	committed = true
}

// insertTrackBatch inserts a batch of tracks in a single multi-row INSERT.
func (q *Queue) insertTrackBatch(tx *sql.Tx, batch []Track) error {
	if len(batch) == 0 {
		return nil
	}

	valuePlaceholders := make([]string, len(batch))
	args := make([]any, 0, len(batch)*2)

	for i, track := range batch {
		valuePlaceholders[i] = "(?, ?)"

		args = append(args, track.AudioFileID, track.Position)
	}

	query := "INSERT INTO queue_tracks (audio_file_id, position) VALUES " +
		strings.Join(valuePlaceholders, ",")

	_, err := tx.ExecContext(q.db.Ctx, query, args...)
	if err != nil {
		return fmt.Errorf("batch insert failed: %w", err)
	}

	return nil
}

// persistState writes the queue metadata to the database.
func (q *Queue) persistState() {
	var shuffleOrderJSON sql.NullString

	if len(q.shuffleOrder) > 0 {
		data, err := json.Marshal(q.shuffleOrder)
		if err != nil {
			q.logger.Error(
				"Failed to marshal shuffle order",
				"err", err,
			)
		} else {
			shuffleOrderJSON = sql.NullString{
				String: string(data),
				Valid:  true,
			}
		}
	}

	sourcePlaylistID := sql.NullInt64{}
	if q.sourcePlaylistID > 0 {
		sourcePlaylistID = sql.NullInt64{
			Int64: q.sourcePlaylistID,
			Valid: true,
		}
	}

	err := q.db.Queries.UpdateQueueState(
		q.db.Ctx,
		sqlcgen.UpdateQueueStateParams{
			SourcePlaylistID: sourcePlaylistID,
			CurrentPosition:  int64(q.currentIndex),
			ShuffleMode:      q.shuffleMode,
			RepeatMode:       string(q.repeatMode),
			ShuffleOrder:     shuffleOrderJSON,
		},
	)
	if err != nil {
		q.logger.Error("Failed to persist queue state", "err", err)
	}
}

// emitQueueChanged emits the full queue state to the frontend.
func (q *Queue) emitQueueChanged() {
	if q.ctx == nil {
		return
	}

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

	runtime.EventsEmit(q.ctx, events.QueueChanged, state)
}

// emitIndexChanged emits only the current index to the frontend.
func (q *Queue) emitIndexChanged() {
	if q.ctx == nil {
		return
	}

	runtime.EventsEmit(
		q.ctx,
		events.QueueIndexChanged,
		IndexChanged{CurrentIndex: q.currentIndex},
	)
}

// emitModeChanged emits only the shuffle/repeat mode to the frontend.
func (q *Queue) emitModeChanged() {
	if q.ctx == nil {
		return
	}

	runtime.EventsEmit(
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
	if q.ctx == nil {
		return
	}

	runtime.EventsEmit(
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

// Sentinel errors.
var (
	ErrEmptyQueue = errors.New("queue is empty")
	ErrNoPlayer   = errors.New("no player set")
)

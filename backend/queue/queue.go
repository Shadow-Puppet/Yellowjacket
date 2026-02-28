// Package queue manages the playback queue and auto-advance logic.
package queue

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"yellowjacket/backend/database"
	"yellowjacket/backend/profiling"
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

// toTrack converts metadata lookup results into a queue Track.
func (m trackMeta) toTrack(position int64) Track {
	return Track{
		AudioFileID: m.AudioFileID,
		FilePath:    m.FilePath,
		Position:    position,
		Title:       m.Title,
		Artist:      m.Artist,
	}
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

// SetContext sets the Wails runtime context for event emission.
func (q *Queue) SetContext(ctx context.Context) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.ctx = ctx
}

// SetPlayer provides the queue with a reference to the player for auto-advance.
func (q *Queue) SetPlayer(player TrackLoader) {
	q.player = player
}

// SetQueue replaces the entire queue with new tracks and starts playing.
// When shuffleStart is true and shuffle mode is active, a random first
// track is chosen instead of the one at startIndex. This is intended for
// "Play All" type actions where no specific track was selected.
// It uses a two-phase approach: the first batch of tracks (up to
// initialBatchSize) is resolved immediately so playback begins and the
// queue panel is populated without delay. The remaining tracks are then
// resolved in the background. A generation counter ensures stale
// background work is discarded if SetQueue is called again.
func (q *Queue) SetQueue(
	filePaths []string,
	startIndex int,
	shuffleStart bool,
) {
	defer profiling.TimeOp(q.logger, "queue.SetQueue")()

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

		tracks = append(tracks, m.toTrack(int64(i)))
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

	// When the caller signals that shuffle should pick the first track
	// (e.g. "Play All" rather than a specific track click) and shuffle
	// mode is active, generate a shuffle order and start from its first
	// element — a random track.
	if shuffleStart && q.shuffleMode && len(q.tracks) > 1 {
		q.currentIndex = -1
		q.generateShuffleOrder()
		q.currentIndex = q.shuffleOrder[0]
	}

	// Start playing immediately.
	q.playCurrentTrack()
	q.emitQueueChanged()

	// Record the path actually playing so Phase 2 can find it after the
	// full track list is rebuilt.
	playingPath := q.tracks[q.currentIndex].FilePath

	q.mu.Unlock()

	// Phase 2: if there are more tracks beyond the initial batch,
	// resolve them in the background. If everything fits in the initial
	// batch we can persist and finish synchronously.
	if len(filePaths) <= initialBatchSize {
		q.mu.Lock()

		if shuffleStart && q.shuffleMode {
			q.generateShuffleOrder()
		}

		q.persistTracks()
		q.persistState()

		q.mu.Unlock()

		return
	}

	go q.resolveRemainingTracks(gen, filePaths, playingPath)
}

// resolveRemainingTracks runs in a goroutine to batch-resolve all tracks
// for a SetQueue call. It checks the generation counter before applying
// results to avoid overwriting a newer SetQueue call. playingPath is the
// file path of the track that is currently playing so the correct
// currentIndex can be located in the rebuilt track list.
func (q *Queue) resolveRemainingTracks(
	gen int64,
	filePaths []string,
	playingPath string,
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

		tracks = append(tracks, meta.toTrack(int64(i)))
	}

	q.tracks = tracks

	// Recalculate currentIndex: find the track that is actually playing.
	// This may differ from the original startIndex when shuffleStart was
	// used to pick a random first track.
	q.currentIndex = 0

	for i, t := range q.tracks {
		if t.FilePath == playingPath {
			q.currentIndex = i

			break
		}
	}

	q.commitMutation(false)
	q.emitQueueChanged()
}

// AddTrack appends a track to the end of the queue.
// If the queue was empty, it loads the added track in a paused state.
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

	track := m.toTrack(int64(len(q.tracks)))

	q.tracks = append(q.tracks, track)

	// Load (paused) if this is the first track added to an empty queue.
	if wasEmpty {
		q.currentIndex = 0
		q.loadCurrentTrack()
	}

	q.commitMutation(false)
	q.emitTracksModified(
		"add",
		[]Track{track},
		len(q.tracks)-1,
		nil,
	)
}

// AddTracks appends multiple tracks to the end of the queue.
// If the queue was empty, it loads the first added track in a paused state.
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

		track := m.toTrack(int64(len(q.tracks)))
		q.tracks = append(q.tracks, track)

		newTracks = append(newTracks, track)
	}

	// Load (paused) if this is the first track added to an empty queue.
	if wasEmpty && len(q.tracks) > 0 {
		q.currentIndex = 0
		q.loadCurrentTrack()
	}

	q.commitMutation(false)
	q.emitTracksModified(
		"add",
		newTracks,
		insertIndex,
		nil,
	)
}

// InsertNextTracks inserts multiple tracks as a contiguous block after the current track.
// If the queue was empty, it loads the first inserted track in a paused state.
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

		newTracks = append(newTracks, m.toTrack(0))
	}

	if len(newTracks) == 0 {
		return
	}

	q.tracks = slices.Insert(q.tracks, insertPos, newTracks...)

	if wasEmpty {
		q.currentIndex = 0
		q.loadCurrentTrack()
	}

	q.commitMutation(true)
	q.emitTracksModified(
		"insert",
		newTracks,
		insertPos,
		nil,
	)
}

// InsertNext inserts a track right after the currently playing track.
// If the queue was empty, it loads the inserted track in a paused state.
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

	wasEmpty := len(q.tracks) == 0

	insertPos := q.currentIndex + 1
	if insertPos > len(q.tracks) {
		insertPos = len(q.tracks)
	}

	track := m.toTrack(int64(insertPos))
	q.tracks = slices.Insert(q.tracks, insertPos, track)

	// Load (paused) if this is the first track added to an empty queue.
	if wasEmpty {
		q.currentIndex = 0
		q.loadCurrentTrack()
	}

	q.commitMutation(true)
	q.emitTracksModified(
		"insert",
		[]Track{track},
		insertPos,
		nil,
	)
}

// InsertTracksAt inserts multiple tracks at the given index.
// If the queue was empty, it loads the first inserted track in a paused state.
func (q *Queue) InsertTracksAt(filePaths []string, index int) {
	allMeta := q.lookupTrackMetaBatch(filePaths)

	q.mu.Lock()
	defer q.mu.Unlock()

	wasEmpty := len(q.tracks) == 0

	// Clamp index to valid range.
	if index < 0 {
		index = 0
	}

	if index > len(q.tracks) {
		index = len(q.tracks)
	}

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

		newTracks = append(newTracks, m.toTrack(0))
	}

	if len(newTracks) == 0 {
		return
	}

	q.tracks = slices.Insert(q.tracks, index, newTracks...)

	// Shift currentIndex if insertion is at or before it.
	if q.currentIndex >= 0 && index <= q.currentIndex {
		q.currentIndex += len(newTracks)
	}

	if wasEmpty {
		q.currentIndex = 0
		q.loadCurrentTrack()
	}

	q.commitMutation(true)
	q.emitTracksModified(
		"insert",
		newTracks,
		index,
		nil,
	)
}

// MoveQueueTracks moves tracks at the given indices to a new position
// as a contiguous block. The toIndex is the target position in the
// original (pre-move) array.
func (q *Queue) MoveQueueTracks(
	fromIndices []int,
	toIndex int,
) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(fromIndices) == 0 || len(q.tracks) == 0 {
		return
	}

	// De-duplicate and sort source indices.
	seen := make(map[int]bool, len(fromIndices))

	var sorted []int

	for _, idx := range fromIndices {
		if idx >= 0 && idx < len(q.tracks) && !seen[idx] {
			seen[idx] = true

			sorted = append(sorted, idx)
		}
	}

	if len(sorted) == 0 {
		return
	}

	slices.Sort(sorted)

	// Clamp toIndex.
	if toIndex < 0 {
		toIndex = 0
	}

	if toIndex > len(q.tracks) {
		toIndex = len(q.tracks)
	}

	// Check if this is a no-op: all source indices are contiguous
	// and already start at the target position.
	isContiguous := true

	for i := 1; i < len(sorted); i++ {
		if sorted[i] != sorted[i-1]+1 {
			isContiguous = false

			break
		}
	}

	lastSorted := sorted[len(sorted)-1]

	if isContiguous &&
		(sorted[0] == toIndex || lastSorted+1 == toIndex) {
		return
	}

	// Find where currentIndex ends up after the move.
	currentTrackIdx := q.currentIndex

	// Extract the tracks to move.
	moving := make([]Track, len(sorted))
	for i, idx := range sorted {
		moving[i] = q.tracks[idx]
	}

	// Build a new slice without the moved tracks.
	remaining := make([]Track, 0, len(q.tracks)-len(sorted))
	removeSet := make(map[int]bool, len(sorted))

	for _, idx := range sorted {
		removeSet[idx] = true
	}

	for i, t := range q.tracks {
		if !removeSet[i] {
			remaining = append(remaining, t)
		}
	}

	// Calculate adjusted insertion index in the remaining slice.
	adjustedIdx := toIndex

	for _, idx := range sorted {
		if idx < toIndex {
			adjustedIdx--
		}
	}

	if adjustedIdx < 0 {
		adjustedIdx = 0
	}

	if adjustedIdx > len(remaining) {
		adjustedIdx = len(remaining)
	}

	// Insert the moved block at the adjusted position.
	q.tracks = slices.Insert(remaining, adjustedIdx, moving...)

	// Track currentIndex through the move.
	if currentTrackIdx >= 0 {
		if removeSet[currentTrackIdx] {
			// The current track was moved — find its new position.
			for ri, orig := range sorted {
				if orig == currentTrackIdx {
					q.currentIndex = adjustedIdx + ri

					break
				}
			}
		} else {
			// The current track was not moved. Find its position
			// in 'remaining', then account for the insertion.
			posInRemaining := currentTrackIdx

			for _, idx := range sorted {
				if idx < currentTrackIdx {
					posInRemaining--
				}
			}

			if adjustedIdx <= posInRemaining {
				q.currentIndex = posInRemaining + len(sorted)
			} else {
				q.currentIndex = posInRemaining
			}
		}
	}

	q.commitMutation(true)
	q.emitTracksModified(
		"move",
		moving,
		toIndex,
		sorted,
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

	q.commitMutation(true)
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

	q.commitMutation(true)
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

// Play handles a play request by either resuming the current track or
// starting playback from the beginning of the queue. When a track is
// already active (currentIndex != -1) the player is told to resume;
// otherwise playback starts from the first track (or a random one when
// shuffle is enabled).
func (q *Queue) Play() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.tracks) == 0 {
		return
	}

	// A track is already active — ask the player to resume.
	if q.currentIndex != -1 {
		if q.player == nil {
			q.logger.Error(
				"No player set, cannot resume",
			)

			return
		}

		if err := q.player.Play(); err != nil {
			q.logger.Warn(
				"Resume requested but player not ready",
				"err", err,
			)
		}

		return
	}

	// No active track — start from the beginning.
	q.playFromStart()
}

// playFromStart restarts playback from the beginning of the queue.
// If shuffle is enabled, a new shuffle order is generated and playback
// starts from a random track. This is a no-op when a track is already
// active (currentIndex != -1) or the queue is empty.
// The caller must hold q.mu.
func (q *Queue) playFromStart() {
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

// Clear removes all tracks from the queue, stops playback, and
// resets the queue state.  It persists the cleared state and
// notifies the frontend.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.logger.Info("Clearing queue")

	q.tracks = nil
	q.currentIndex = -1
	q.shuffleOrder = nil
	q.sourcePlaylistID = 0

	if q.player != nil {
		q.player.UnloadTrack()
	}

	q.commitMutation(false)
	q.emitQueueChanged()
}

// EmitCurrentState emits the current queue state to the frontend.
// This is called after the frontend DOM is ready.
func (q *Queue) EmitCurrentState() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.emitQueueChanged()
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

// commitMutation persists the current queue state after a mutation.
// When reindex is true, track positions are renumbered first.
// The caller must hold q.mu.
func (q *Queue) commitMutation(reindex bool) {
	if reindex {
		q.reindexPositions()
	}

	if q.shuffleMode {
		q.generateShuffleOrder()
	}

	q.persistTracks()
	q.persistState()
}

// Package events contains centralized event name constants for
// Wails frontend/backend communication. These names must match
// the corresponding event names in the TypeScript frontend.
package events

// Playback control events.
const (
	PlaybackStateChanged = "PlaybackStateChanged"
	PlaybackFinished     = "PlaybackFinished"
	RequestPlay          = "RequestPlay"
	RequestPause         = "RequestPause"
	RequestLoadFile      = "RequestLoadFile"
)

// Track events.
const (
	TrackChanged = "TrackChanged"
)

// Seek events.
const (
	Seek       = "Seek"
	SeekFailed = "SeekFailed"
)

// Volume events.
const (
	RequestSetVolume = "RequestSetVolume"
	VolumeChanged    = "VolumeChanged"
)

// Queue events.
const (
	QueueChanged                 = "QueueChanged"
	QueueIndexChanged            = "QueueIndexChanged"
	QueueModeChanged             = "QueueModeChanged"
	QueueTracksModified          = "QueueTracksModified"
	RequestNext                  = "RequestNext"
	RequestPrevious              = "RequestPrevious"
	RequestSetQueue              = "RequestSetQueue"
	RequestAddToQueue            = "RequestAddToQueue"
	RequestPlayNext              = "RequestPlayNext"
	RequestRemoveFromQueue       = "RequestRemoveFromQueue"
	RequestToggleShuffle         = "RequestToggleShuffle"
	RequestCycleRepeat           = "RequestCycleRepeat"
	RequestAddTracksToQueue      = "RequestAddTracksToQueue"
	RequestPlayTracksNext        = "RequestPlayTracksNext"
	RequestPlayQueueIndex        = "RequestPlayQueueIndex"
	RequestRemoveTracksFromQueue = "RequestRemoveTracksFromQueue"
	RequestInsertTracksAtIndex   = "RequestInsertTracksAtIndex"
	RequestMoveQueueTracks       = "RequestMoveQueueTracks"
	RequestClearQueue            = "RequestClearQueue"
)

// Config events.
const (
	LibraryConfigChanged = "LibraryConfigChanged"
	ThemeConfigChanged   = "ThemeConfigChanged"
)

// Playlist events.
const (
	PlaylistCreated       = "PlaylistCreated"
	PlaylistDeleted       = "PlaylistDeleted"
	PlaylistRenamed       = "PlaylistRenamed"
	PlaylistTracksChanged = "PlaylistTracksChanged"
	PlaylistsRestored     = "PlaylistsRestored"
)

// Library events.
const (
	LibraryScanStarted  = "LibraryScanStarted"
	LibraryScanComplete = "LibraryScanComplete"
)

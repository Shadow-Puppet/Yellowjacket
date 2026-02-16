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
	QueueChanged            = "QueueChanged"
	RequestNext             = "RequestNext"
	RequestPrevious         = "RequestPrevious"
	RequestSetQueue         = "RequestSetQueue"
	RequestAddToQueue       = "RequestAddToQueue"
	RequestPlayNext         = "RequestPlayNext"
	RequestRemoveFromQueue  = "RequestRemoveFromQueue"
	RequestToggleShuffle    = "RequestToggleShuffle"
	RequestCycleRepeat      = "RequestCycleRepeat"
	RequestAddTracksToQueue = "RequestAddTracksToQueue"
	RequestPlayTracksNext   = "RequestPlayTracksNext"
	RequestPlayQueueIndex   = "RequestPlayQueueIndex"
)

// Config events.
const (
	LibraryConfigChanged = "LibraryConfigChanged"
)

// Library events.
const (
	LibraryScanComplete = "LibraryScanComplete"
)

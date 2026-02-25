// Package events contains centralized event name constants for
// Wails frontend/backend communication. These names must match
// the corresponding event names in the TypeScript frontend.
package events

// Playback events (backend → frontend push).
const (
	PlaybackStateChanged = "PlaybackStateChanged"
	PlaybackFinished     = "PlaybackFinished"
	TrackChanged         = "TrackChanged"
	SeekFailed           = "SeekFailed"
	VolumeChanged        = "VolumeChanged"
)

// Queue events (backend → frontend push).
const (
	QueueChanged        = "QueueChanged"
	QueueIndexChanged   = "QueueIndexChanged"
	QueueModeChanged    = "QueueModeChanged"
	QueueTracksModified = "QueueTracksModified"
)

// Config events.
const (
	LibraryConfigChanged   = "LibraryConfigChanged"
	ThemeConfigChanged     = "ThemeConfigChanged"
	TrackListConfigChanged = "TrackListConfigChanged"
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

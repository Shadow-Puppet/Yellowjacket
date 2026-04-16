// Package events contains centralized event name constants for
// Wails frontend/backend communication. These names must match
// the corresponding event names in the TypeScript frontend.
package events

//go:generate go run ./cmd/genevents -source events.go -output ../../frontend/src/events.ts

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
	FavoritesConfigChanged = "FavoritesConfigChanged"
	ShortcutsConfigChanged = "ShortcutsConfigChanged"
)

// Playlist events.
const (
	PlaylistCreated        = "PlaylistCreated"
	PlaylistDeleted        = "PlaylistDeleted"
	PlaylistRenamed        = "PlaylistRenamed"
	PlaylistTracksChanged  = "PlaylistTracksChanged"
	PlaylistsRestored      = "PlaylistsRestored"
	DefaultPlaylistChanged = "DefaultPlaylistChanged"
)

// Library events.
const (
	LibraryScanStarted  = "LibraryScanStarted"
	LibraryScanProgress = "LibraryScanProgress"
	LibraryScanComplete = "LibraryScanComplete"
)

// Scan control events.
const (
	LibraryScanCancelled = "LibraryScanCancelled"
	LibraryScanPaused    = "LibraryScanPaused"
	LibraryScanResumed   = "LibraryScanResumed"
)

// Scan queue events.
const (
	LibraryScanQueued       = "LibraryScanQueued"
	LibraryScanQueueDrained = "LibraryScanQueueDrained"
)

// Library CRUD events.
const (
	LibraryAdded   = "LibraryAdded"
	LibraryRenamed = "LibraryRenamed"
	LibraryRemoved = "LibraryRemoved"
)

// Tag writing events.
const (
	TrackMetadataChanged = "TrackMetadataChanged"
	BatchWriteProgress   = "BatchWriteProgress"
)

// Explore / search index events.
const (
	IndexStatusChanged = "IndexStatusChanged"
)

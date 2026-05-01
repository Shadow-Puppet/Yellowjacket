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

// Autotag apply events — emitted while an async ApplyAsync job is in flight so the review UI can render per-folder progress.
const (
	AutotagApplyStarted  = "AutotagApplyStarted"  // {groupKey: string, total: int}
	AutotagApplyProgress = "AutotagApplyProgress" // {groupKey, current, total, succeeded, failed}
	AutotagApplyFinished = "AutotagApplyFinished" // {groupKey, succeeded, failed, error}
)

// Autotag prefetch events — emitted by the background worker that scores pending tagging items so sidebar pills populate without the user having to open each folder.
const (
	AutotagPrefetchProgress = "AutotagPrefetchProgress" // {processed, total} — debounce on frontend
	AutotagPrefetchFinished = "AutotagPrefetchFinished" // {processed, total}
)

// Explore / search index events.
const (
	IndexStatusChanged = "IndexStatusChanged"
)

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
	MuteChanged          = "MuteChanged"

	// PlaybackPositionChanged carries the player's own position
	// (payload: player.PositionInfo) once a second while playing and
	// immediately after any seek, pause, resume or track change.  The
	// seek bar renders what it is told and interpolates only between
	// ticks, so it can be at most one tick wrong and can never
	// accumulate error the way a pure local counter did.
	PlaybackPositionChanged = "PlaybackPositionChanged"

	// PlaybackFailed (payload: {filePath, reason}) fires when a track
	// could not be loaded or started — a moved file, an unreadable
	// one, an unsupported codec.  Without it the failure was a silent
	// no-op: the queue reverted its index and nothing reached the UI.
	PlaybackFailed = "PlaybackFailed"
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
	GeneralConfigChanged   = "GeneralConfigChanged"
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
//
// TrackMetadataChanged means "tags on disk were rewritten", and the
// frontend answers it by throwing the whole library cache away and
// refetching — which is correct, because a retag can change an album
// name, an artist, a genre, and therefore every derived collection.
//
// It must therefore not be reused for anything cheaper.  Finishing a
// track used to emit it, so every song cost a full refetch: ~37 MB
// across the IPC per track at 50 000 tracks, and the user's track
// selection cleared while music played (audit perf.C1/C2).  That is
// what TrackPlayCountChanged below exists to separate.
const (
	TrackMetadataChanged = "TrackMetadataChanged"
	BatchWriteProgress   = "BatchWriteProgress"
)

// Track removal events.
//
// TracksRemovedFromLibrary means "these rows are gone and these paths
// will not be imported again", and like TrackPlayCountChanged it
// carries everything a consumer needs to patch rather than invalidate:
// {filePaths: []string, count: int}.  The library store splices those
// paths out of its tracks array — which is the expensive collection —
// and refetches only the album/artist/genre summaries, whose counts
// really did change.
const (
	TracksRemovedFromLibrary = "TracksRemovedFromLibrary"
)

// Play statistics events.
//
// TrackPlayCountChanged carries everything needed to patch the one
// track in place, precisely so no consumer has any reason to invalidate
// a collection: {audioFileId, filePath, playCount, lastPlayed}.
const (
	TrackPlayCountChanged = "TrackPlayCountChanged"
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

// Background job events.
const (
	// JobsChanged carries a full snapshot of every known background job
	// (see backend/jobs).  A full snapshot rather than a delta means a
	// component mounting mid-scan is correct from its first event.
	// Emitted coalesced, at most every 250ms.
	JobsChanged = "JobsChanged"
)

// Explore / search index events.
const (
	IndexStatusChanged = "IndexStatusChanged"

	// ArtistDiscographyReady fires (payload: artist MBID string) after a
	// lazy background discography fetch persists into the index, so the
	// artist detail page can re-fetch its top tracks / top releases
	// without the initial request having blocked on a live fetch.
	ArtistDiscographyReady = "ArtistDiscographyReady"

	// ArtistSimilarReady fires (payload: artist MBID string) after a lazy
	// background similar-artists fetch persists into similar_artist_map,
	// so the artist detail page can re-fetch that section without the
	// initial request having blocked on a live LB labs call.
	ArtistSimilarReady = "ArtistSimilarReady"

	// AlbumReleasesReady fires (payload: release-group MBID string) after a
	// lazy background BrowseReleases fetch populates the response cache, so
	// the album detail page can re-fetch its versions / tracklist without
	// the initial request having blocked on a live MusicBrainz browse.
	AlbumReleasesReady = "AlbumReleasesReady"

	// AlbumReleasesFailed fires (payload: release-group MBID string) when
	// that same background browse returns an error, so the album page can
	// say the catalog did not answer at the moment it did not answer.
	//
	// Without it the only signal is the absence of AlbumReleasesReady,
	// which a slow browse and a failed one produce alike — leaving a
	// timer to guess between them, and a page queued behind the
	// prefetch's rate limiter to be reported as a failure.
	AlbumReleasesFailed = "AlbumReleasesFailed"

	// DownloadProvidersChanged fires after a download client is added,
	// edited, enabled/disabled or removed, so the settings page and any
	// open download picker re-read the provider list.
	DownloadProvidersChanged = "DownloadProvidersChanged"

	// DownloadsChanged fires when the set of downloads changes (started,
	// picked, cancelled, cleared).  Per-transfer progress does not use
	// this — it flows through the jobs registry's JobsChanged, which
	// already coalesces high-frequency updates.
	DownloadsChanged = "DownloadsChanged"

	// RequestsChanged fires when the request list gains, loses or
	// retires an entry — including from a background reconcile pass,
	// which is why the list is event-driven rather than fetched once on
	// mount.
	RequestsChanged = "RequestsChanged"
)

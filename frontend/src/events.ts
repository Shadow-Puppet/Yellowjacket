// Centralized event name constants for Wails frontend/backend communication.
// These names must match the corresponding event names in the Go backend.

export const Events = {
    // Playback events (backend → frontend push)
    PlaybackStateChanged: "PlaybackStateChanged",
    PlaybackFinished: "PlaybackFinished",
    TrackChanged: "TrackChanged",
    SeekFailed: "SeekFailed",
    VolumeChanged: "VolumeChanged",

    // Queue events (backend → frontend push)
    QueueChanged: "QueueChanged",
    QueueIndexChanged: "QueueIndexChanged",
    QueueModeChanged: "QueueModeChanged",
    QueueTracksModified: "QueueTracksModified",

    // Playlist events
    PlaylistCreated: "PlaylistCreated",
    PlaylistDeleted: "PlaylistDeleted",
    PlaylistRenamed: "PlaylistRenamed",
    PlaylistTracksChanged: "PlaylistTracksChanged",
    PlaylistsRestored: "PlaylistsRestored",
    DefaultPlaylistChanged: "DefaultPlaylistChanged",

    // Config events
    ThemeConfigChanged: "ThemeConfigChanged",
    TrackListConfigChanged: "TrackListConfigChanged",
    FavoritesConfigChanged: "FavoritesConfigChanged",

    // Library events
    LibraryScanStarted: "LibraryScanStarted",
    LibraryScanComplete: "LibraryScanComplete",
} as const;

export type EventName = (typeof Events)[keyof typeof Events];

// Centralized event name constants for Wails frontend/backend communication.
// These names must match the corresponding event names in the Go backend.

export const Events = {
    // Playback control events
    PlaybackStateChanged: "PlaybackStateChanged",
    PlaybackFinished: "PlaybackFinished",
    RequestPlay: "RequestPlay",
    RequestPause: "RequestPause",
    RequestLoadFile: "RequestLoadFile",

    // Track events
    TrackChanged: "TrackChanged",

    // Seek events
    Seek: "Seek",
    SeekFailed: "SeekFailed",

    // Volume events
    RequestSetVolume: "RequestSetVolume",
    VolumeChanged: "VolumeChanged",

    // Queue events
    QueueChanged: "QueueChanged",
    QueueIndexChanged: "QueueIndexChanged",
    QueueModeChanged: "QueueModeChanged",
    QueueTracksModified: "QueueTracksModified",
    RequestNext: "RequestNext",
    RequestPrevious: "RequestPrevious",
    RequestSetQueue: "RequestSetQueue",
    RequestAddToQueue: "RequestAddToQueue",
    RequestPlayNext: "RequestPlayNext",
    RequestRemoveFromQueue: "RequestRemoveFromQueue",
    RequestToggleShuffle: "RequestToggleShuffle",
    RequestCycleRepeat: "RequestCycleRepeat",
    RequestAddTracksToQueue: "RequestAddTracksToQueue",
    RequestPlayTracksNext: "RequestPlayTracksNext",
    RequestPlayQueueIndex: "RequestPlayQueueIndex",
    RequestRemoveTracksFromQueue: "RequestRemoveTracksFromQueue",
    RequestInsertTracksAtIndex: "RequestInsertTracksAtIndex",
    RequestMoveQueueTracks: "RequestMoveQueueTracks",

    // Library events
    LibraryScanStarted: "LibraryScanStarted",
    LibraryScanComplete: "LibraryScanComplete",
} as const;

export type EventName = (typeof Events)[keyof typeof Events];

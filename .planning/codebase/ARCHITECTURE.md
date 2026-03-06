# Architecture

**Analysis Date:** 2026-02-26

## Pattern Overview

**Overall:** Wails v2 Desktop Application — Go backend with embedded web frontend

YellowJacket is a cross-platform desktop music player. The Wails framework hosts a Go backend that manages audio playback, library scanning, queue management, and data persistence. The frontend is a TypeScript/Lit web application rendered in a native webview. Communication between the two layers uses Wails' bidirectional event system and auto-generated function bindings.

**Key Characteristics:**
- Backend is the single source of truth for all application state
- Frontend stores are reactive mirrors that cache backend state for rendering
- Event-driven communication replaces direct function calls for state synchronization
- Two-phase initialization pattern separates object creation from Wails runtime wiring
- SQLite with WAL mode and single-writer constraint for all persistent data
- Code generation via sqlc (SQL → Go) and templ (Go templates → Go)

## Layers

**Application Shell (`main.go`, `backend/app.go`):**
- Purpose: Bootstrap the application, wire dependencies, manage Wails lifecycle
- Location: `main.go`, `backend/app.go`
- Contains: `YellowJacketApp` struct, lifecycle hooks (`OnStartup`, `OnDomReady`, `OnBeforeClose`, `OnShutdown`), dependency wiring, frontend binding registration
- Depends on: All backend packages, Wails runtime
- Used by: Wails framework (lifecycle callbacks)

**Domain Layer (backend packages):**
- Purpose: Implement all business logic — playback, queue management, library scanning, playlists
- Location: `backend/player/`, `backend/queue/`, `backend/library/`, `backend/playlist/`
- Contains: Core domain structs, state management, audio decoding, metadata extraction, scan pipeline
- Depends on: `backend/database/`, `backend/events/`, `backend/metadata/`, `backend/coverart/`, Wails runtime (for event emission)
- Used by: Application shell (via lifecycle hooks), frontend (via Wails bindings and events)

**Data Layer (`backend/database/`):**
- Purpose: SQLite database access with type-safe queries
- Location: `backend/database/database.go`, `backend/database/search.go`, `backend/database/sql/`
- Contains: DB wrapper, schema migrations, FTS5 search queries, sqlc-generated query code
- Depends on: `modernc.org/sqlite` (pure-Go SQLite driver), `backend/system/` (for data directory)
- Used by: All domain packages (player, queue, library, playlist)

**Events Layer (`backend/events/`, `frontend/src/events.ts`):**
- Purpose: Centralized event name constants ensuring backend/frontend parity
- Location: `backend/events/events.go` (Go), `frontend/src/events.ts` (TypeScript)
- Contains: String constants for all event names — must match exactly between files
- Depends on: Nothing
- Used by: All backend packages (emission), all frontend stores (subscription)

**Frontend Store Layer (`frontend/src/store/`):**
- Purpose: Cache backend state as reactive data for Lit components
- Location: `frontend/src/store/`
- Contains: Singleton store classes (`PlayerStore`, `QueueStore`, `ThemeStore`, etc.) with subscription system
- Depends on: Wails event system (`@runtime/runtime`), Wails Go bindings (`@go/*`)
- Used by: Frontend controllers and components

**Frontend Controller Layer (`frontend/src/store/controllers/`):**
- Purpose: Connect Lit components to stores via Lit's `ReactiveController` pattern
- Location: `frontend/src/store/controllers/`
- Contains: Controller classes implementing `ReactiveController` — subscribe on `hostConnected()`, unsubscribe on `hostDisconnected()`
- Depends on: Stores
- Used by: Lit components

**Frontend Component Layer (`frontend/src/components/`):**
- Purpose: UI rendering via Lit Web Components with shadow DOM
- Location: `frontend/src/components/`
- Contains: Custom elements for player controls, track list, queue panel, sidebar, cover grid, config page, etc.
- Depends on: Controllers, stores, Wails bindings
- Used by: HTML entry point (`frontend/index.html`)

**Infrastructure Layer:**
- Purpose: Cross-cutting concerns — config persistence, asset serving, OS integration, logging
- Location: `backend/config/`, `backend/assets/`, `backend/system/`, `backend/logging/`, `backend/mediacontrols/`, `backend/coverart/`, `backend/frontendutil/`
- Contains: TOML config management, custom asset server with cover art routing, OS-specific user directories, MPRIS media controls, profiling utilities
- Depends on: `backend/events/`, Wails runtime
- Used by: Application shell, domain packages

## Data Flow

**Track Playback Flow:**

1. User clicks track in frontend `track-list` component
2. Component calls `queueStore.setQueue(filePaths, startIndex)` → delegates to `Queue.SetQueue()` via Wails binding
3. `Queue.SetQueue()` in Go resolves track metadata from DB, sets queue state, calls `q.playCurrentTrack()`
4. `playCurrentTrack()` calls `player.LoadFile(filePath)` then `player.Play()`
5. `Player.LoadFile()` opens file, decodes via `metadata.DecodeFile()`, builds beep streamer chain (resample → ctrl → volume), registers with speaker
6. Player emits `TrackChanged` and `PlaybackStateChanged` events via `runtime.EventsEmit()`
7. Frontend `PlayerStore` receives events, updates cached state, notifies subscribers
8. `PlayerController` triggers `host.requestUpdate()` on connected Lit components
9. Components re-render with new track info and playback state

**Library Scan Flow:**

1. Config change triggers `LibraryConfigChanged` event (or user initiates rescan)
2. `Library.Scan()` executes multi-phase pipeline:
   - Phase 1: Load existing audio files from DB into `sync.Map`
   - Phase 2: Walk filesystem directory tree, dispatch new/updated files to work channel
   - Phase 3: Worker pool extracts metadata (tags + duration) concurrently
   - Phase 4: Single DB writer goroutine batches results into transactions
   - Phase 5: Orphan cleanup — remove DB entries for deleted files
   - Phase 6: Generate missing cover art thumbnails
3. `LibraryScanComplete` event emitted with `ScanMetrics` payload
4. Frontend receives event, refreshes track list

**Queue Auto-Advance Flow:**

1. `beep.Callback` fires when track stream ends (runs with speaker lock held)
2. Callback dispatches `player.onPlaybackFinished()` to a new goroutine (avoids deadlock)
3. `onPlaybackFinished()` sets state to Stopped, emits `PlaybackFinished` and `PlaybackStateChanged` events
4. Calls `playbackFinishedHandler` (wired to `queue.OnPlaybackFinished()`) without holding `p.mu`
5. Queue determines next track (respecting shuffle/repeat modes), loads and plays it
6. Queue emits `QueueIndexChanged` event for frontend sync

**State Management:**

- **Backend is source of truth**: Player state (volume, position, current track), queue state (tracks, index, shuffle/repeat modes), library data, playlists — all owned by Go
- **Frontend stores are mirrors**: `PlayerStore`, `QueueStore`, `ThemeStore` etc. subscribe to backend events and cache state for reactive rendering
- **Startup synchronization**: After frontend DOM is ready, `index.ts` calls `Player.EmitCurrentState()` and `Queue.EmitCurrentState()` via Wails bindings. These methods push the full current state to the frontend via events, ensuring stores are populated on app launch
- **State persistence**: Player state (volume, muted, last track, position) and queue state (tracks, index, modes) are persisted to SQLite. On startup, `RestoreState()` loads from DB; `SaveState()` writes on shutdown and on significant changes

## Key Abstractions

**Player (`backend/player/player.go`):**
- Purpose: Audio file decoding, playback control (play/pause/seek), volume management, state persistence
- Pattern: Mutex-protected state with beep audio library streamer chain (decode → resample → ctrl → volume → speaker)
- Lock ordering: Always acquire `p.mu` before `speaker.Lock()`
- Key types: `Player`, `State` (playing/paused/stopped), `TrackInfo`, `UserVolume`

**Queue (`backend/queue/queue.go`, `navigation.go`, `handlers.go`, `emit.go`, `persistence.go`):**
- Purpose: Ordered track list management, auto-advance, shuffle/repeat, track loading coordination
- Pattern: Mutex-protected state, delegates to `TrackLoader` interface (player) for file loading
- Uses `TrackLoader` interface to avoid circular dependency with player package
- Two-phase SetQueue: initial batch resolves immediately for instant UI, remaining tracks resolve in background goroutine with generation counter for staleness detection

**Library (`backend/library/library.go`, `query.go`, `rescan.go`, `coverart.go`):**
- Purpose: Music collection scanning, metadata extraction, database population, query interface
- Pattern: Multi-phase concurrent pipeline (walk → extract → write → cleanup) with configurable worker count based on storage type (SSD vs HDD)
- Entity caching during scan to avoid redundant DB upserts for repeated artists/albums
- `RescanHooks` pattern for cross-cutting orchestration without circular dependencies

**Database (`backend/database/database.go`, `search.go`):**
- Purpose: SQLite access layer with embedded schema management and FTS5 full-text search
- Pattern: Embedded SQL schemas applied on startup, incremental migrations via `PRAGMA user_version`, sqlc-generated type-safe queries
- WAL mode with `SetMaxOpenConns(1)` for single-writer safety
- FTS5 `search_index` virtual table for title/artist/album/filepath search

**Playlist (`backend/playlist/playlist.go`, `m3u.go`, `favorites.go`, `match.go`):**
- Purpose: Playlist CRUD, M3U8 file import/export, phantom track resolution
- Pattern: Dual storage — DB rows for resolved tracks + M3U8 files as persistent backup. Phantom tracks represent unresolved M3U8 entries (file moved/renamed) with fuzzy matching for resolution

**Config (`backend/config/config.go`):**
- Purpose: Application settings persistence and event-driven propagation
- Pattern: TOML file on disk, loaded at startup, saved on changes. `SetContext()` enables Wails event emission. Config changes emit typed events (`ThemeConfigChanged`, `TrackListConfigChanged`, etc.) so listeners react automatically

## Entry Points

**`main.go`:**
- Location: `main.go`
- Triggers: OS process start
- Responsibilities: Create logger, initialize asset handler, create `YellowJacketApp`, configure Wails options (window size, lifecycle hooks, bindings), call `wails.Run()`

**`backend/app.go` — `NewYellowJacketApp()`:**
- Location: `backend/app.go`
- Triggers: Called from `main.go` before `wails.Run()`
- Responsibilities: Phase 1 initialization — create database, config, library, player, queue, playlist service, cover art handler. Register Wails frontend bindings (`FEBindings` slice). No Wails runtime access yet.

**`backend/app.go` — `OnStartup(ctx)`:**
- Location: `backend/app.go`
- Triggers: Wails calls this after the runtime is initialized
- Responsibilities: Phase 2 initialization — call `SetContext(ctx)` on all components, initialize speaker hardware, wire cross-cutting hooks (player↔queue, library↔queue/playlist), initialize MPRIS media controls

**`backend/app.go` — `OnDomReady(ctx)`:**
- Location: `backend/app.go`
- Triggers: Wails calls this when frontend DOM is fully loaded
- Responsibilities: Check for startup errors and quit if fatal. State sync is driven by frontend calling `EmitCurrentState()` methods.

**`frontend/index.html`:**
- Location: `frontend/index.html`
- Triggers: Wails loads this as the webview content
- Responsibilities: Define page layout structure, load `index.ts` module, instantiate root custom elements (`<search-bar>`, `<app-sidebar>`, `<track-list>`, `<queue-panel>`, `<now-playing>`, `<audio-player>`)

## Two-Phase Initialization

Components that need Wails runtime (for events, dialogs, window APIs) use a two-phase pattern because the runtime is unavailable when objects are first created for Wails binding registration:

**Phase 1 — `New*()`** (called in `NewYellowJacketApp`, before `wails.Run`):
- Create struct with injected dependencies (logger, database)
- Initialize internal state to safe defaults
- Do NOT access Wails runtime or emit events

**Phase 2 — `SetContext(ctx context.Context)`** (called in `OnStartup`, after runtime ready):
- Store the Wails context
- Register event handlers via `runtime.EventsOn()`
- Restore persisted state from database
- Begin emitting events

Components using this pattern:
- `backend/player/player.go` → `NewPlayer()` + `SetContext()` + `InitSpeaker()`
- `backend/queue/queue.go` → `NewQueue()` + `SetContext()` + `SetPlayer()` + `RestoreState()`
- `backend/library/library.go` → `NewLibrary()` + `SetContext()`
- `backend/playlist/playlist.go` → `NewService()` + `SetContext()`
- `backend/config/config.go` → `NewConfig()` + `SetContext()`
- `backend/frontendutil/frontendutil.go` → `NewFrontendUtil()` + `SetContext()`

## Error Handling

**Strategy:** Errors are wrapped with context at each layer, surfaced via structured logging, and propagated to callers. Fatal startup errors cause application exit. Runtime errors are logged and the operation is gracefully degraded.

**Patterns:**
- Sentinel errors as package-level vars: `var errNoAudioFileLoaded = errors.New("no audio file loaded")`
- Error wrapping: `fmt.Errorf("failed to open file: %w", err)`
- `errors.Join()` for accumulating multiple non-fatal errors during scans
- Early return with blank line after error checks (enforced by `nlreturn` linter)
- Startup errors accumulated via `errors.Join(startupErr, ...)` and checked in `OnDomReady` — fatal errors cause `wailsruntime.Quit(ctx)`

## Cross-Cutting Concerns

**Logging:** `log/slog` with structured key-value pairs. Logger injected via constructors and scoped with `logger.WithGroup("player")`. Dev builds use `devslog` handler with debug level; prod builds use info level.

**Validation:** Config validation at load time and before save. Library config validates directory existence. Theme config validates hex color and shade values. TrackList config validates column IDs.

**Authentication:** Not applicable — local desktop application with no network auth.

**OS Integration:**
- MPRIS2 media controls on Linux (`backend/mediacontrols/mpris_linux.go`), no-op stub on other platforms (`backend/mediacontrols/stub.go`)
- OS-specific user data/config directories (`backend/system/userdata.go`)
- Disk type detection for scan concurrency optimization (`backend/system/disktype_linux.go`)

**Asset Serving:** Custom `assets.Handler` wraps Wails' default asset handler with additional routes (cover art serving via `coverart.Handler`). The handler uses `http.ServeMux` for custom routes with fallback to Wails asset handler.

**Profiling:** Dev-only pprof server and operation timing via `backend/profiling/`. Production builds compile to no-ops.

---

*Architecture analysis: 2026-02-26*

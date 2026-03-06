# External Integrations

**Analysis Date:** 2026-02-26

## Wails Runtime Bridge (Go ↔ TypeScript)

**Primary Communication Mechanism: Events**

The Wails runtime provides a bidirectional event bus between Go and TypeScript. Event names are defined as string constants that must match exactly between both sides:

- Go: `backend/events/events.go` - Centralized event name constants
- TypeScript: `frontend/src/events.ts` - Mirrored constants

**Event Categories:**

| Category | Direction | Events |
|---|---|---|
| Playback | Backend → Frontend | `PlaybackStateChanged`, `PlaybackFinished`, `TrackChanged`, `SeekFailed`, `VolumeChanged` |
| Queue | Backend → Frontend | `QueueChanged`, `QueueIndexChanged`, `QueueModeChanged`, `QueueTracksModified` |
| Config | Backend → Frontend | `LibraryConfigChanged`, `ThemeConfigChanged`, `TrackListConfigChanged`, `FavoritesConfigChanged` |
| Playlist | Backend → Frontend | `PlaylistCreated`, `PlaylistDeleted`, `PlaylistRenamed`, `PlaylistTracksChanged`, `PlaylistsRestored`, `DefaultPlaylistChanged` |
| Library | Backend → Frontend | `LibraryScanStarted`, `LibraryScanComplete` |

**Go event emission pattern:**
```go
runtime.EventsEmit(p.ctx, events.TrackChanged, trackInfo)
runtime.EventsOn(l.ctx, events.LibraryConfigChanged, func(data ...any) { ... })
```

**TypeScript event subscription pattern:**
```typescript
EventsOn(Events.TrackChanged, (trackInfo: TrackInfo | null) => { ... });
```

**Wails Bindings (Direct Function Calls):**

Go structs listed in `FEBindings` in `backend/app.go` are automatically exposed as callable functions from TypeScript. Auto-generated binding stubs live in `frontend/wailsjs/go/` (do not edit).

Bound services:
- `backend/frontendutil/frontendutil.go` → `@go/frontendutil/FrontendUtil` - Directory/file picker dialogs
- `backend/config/config.go` → `@go/config/Config` - Get/set all configuration
- `backend/library/library.go` → `@go/library/Library` - Library scanning and queries
- `backend/playlist/playlist.go` → `@go/playlist/Service` - Playlist CRUD
- `backend/queue/queue.go` → `@go/queue/Queue` - Queue management
- `backend/player/player.go` → `@go/player/Player` - Playback control (play, pause, seek, volume, load)

**State Synchronization Pattern:**

The backend is the source of truth. The frontend requests initial state after its stores are ready:
```typescript
// frontend/index.ts (after all stores import and register listeners)
void Player.EmitCurrentState();
void Queue.EmitCurrentState();
```

Backend responds by emitting the full current state via events, which the stores receive and cache.

## Data Storage

**Database: SQLite**
- Driver: `modernc.org/sqlite` v1.45.0 (pure-Go, no CGo)
- DB file: `~/.local/share/yellowjacket/yj.db` (Linux)
- Connection: `backend/database/database.go`
- Pragmas: WAL journal mode, `busy_timeout=5000`, `foreign_keys=ON`
- Constraint: `SetMaxOpenConns(1)` (single writer)
- Code generation: sqlc (`backend/database/sqlc.yaml`)
  - Schemas: `backend/database/sql/schemas/*.sql` (30 schema files)
  - Queries: `backend/database/sql/queries/*.sql` (15 query files)
  - Generated output: `backend/database/sql/sqlcgen/` (DO NOT EDIT)
- Schema migration: Custom migration system using `PRAGMA user_version` (`backend/database/database.go`, `runMigrations()`)
  - Migration 1: Audio file property columns (sample_rate, bit_depth, channels, bitrate, file_size)
  - Migration 2: Basename column, FTS5 search index

**Database Schema (key tables):**

| Table | Purpose |
|---|---|
| `audio_files` | Tracks with file paths, metadata references, audio properties |
| `recordings` | Track metadata (title, track number, year, genre, etc.) |
| `artists` | Artist entities |
| `artist_credit` | Artist credit display names |
| `artist_credit_artist` | M:N link between artists and credits |
| `release_groups` | Albums |
| `release_group_recordings` | M:N link between albums and recordings |
| `cover_art` | Cover art file references |
| `genres` | Genre entities |
| `genre_recordings` | M:N link between genres and recordings |
| `playlists` / `playlist_tracks` | User playlists |
| `queue` / `queue_tracks` | Playback queue with persistence |
| `player_state` | Persisted player state (volume, last track, position) |
| `file_types` | Supported audio file type registry |
| `search_index` | FTS5 full-text search index (file_path, title, artist, album) |

**File Storage:**
- Cover art cache: `~/.local/share/yellowjacket/covers/` (Linux)
  - Managed by `backend/coverart/coverart.go` and `backend/library/coverart.go`
  - Size variants: original, `_sm` (small), `_md` (medium), `_lg` (large)
  - Served via custom asset handler at `/covers/` prefix
- Config file: `~/.config/yellowjacket/config.toml` (Linux)
  - Managed by `backend/config/config.go`
  - Format: TOML via `github.com/BurntSushi/toml`

**Caching:**
- In-memory entity cache during library scans (`entityCache` in `backend/library/library.go`) - caches artist credits, artists, release groups, cover art, genres to avoid redundant DB upserts
- No external caching service

## Audio Playback

**Library: `github.com/gopxl/beep/v2` v2.1.1**

Core audio engine providing decode → resample → control → volume → speaker pipeline.

- Decoder: `backend/metadata/decoder.go` - Routes by file extension to beep decoders
- Player: `backend/player/player.go` - Manages streamer chain and playback state
- Speaker: Initialized at 44100 Hz sample rate, 100ms buffer (`time.Second/10`)

**Supported Formats:**
| Format | Decoder | Extension |
|---|---|---|
| MP3 | `github.com/gopxl/beep/v2/mp3` (via `github.com/hajimehoshi/go-mp3`) | `.mp3` |
| FLAC | `github.com/gopxl/beep/v2/flac` (via `github.com/mewkiz/flac`) | `.flac` |
| Ogg Vorbis | `github.com/gopxl/beep/v2/vorbis` (via `github.com/jfreymuth/oggvorbis`) | `.ogg` |
| WAV | `github.com/gopxl/beep/v2/wav` | `.wav` |

**Audio Pipeline (per track):**
1. File opened → decoded to `beep.StreamSeekCloser`
2. Resampled from source sample rate to speaker rate (44100 Hz, quality=4)
3. Wrapped in `beep.Ctrl` for play/pause control
4. Wrapped in `effects.Volume` for volume control (base=2, range -5 to 0 internal)
5. Registered with `speaker.Play()` with a `beep.Callback` for end-of-track notification

**Speaker hardware** uses `github.com/ebitengine/oto/v3` (indirect dependency via beep) for cross-platform audio output.

**Volume System:**
- User-facing: 0–100 integer scale (`player.UserVolume`)
- Internal: -5.0 to 0.0 float scale (`player.Volume`)
- Conversion: `backend/player/volume.go`

## Metadata Extraction

**Library: `github.com/dhowden/tag`**

- Extracts ID3v2, Vorbis Comment, and FLAC tags
- Implementation: `backend/metadata/tags.go` (`ExtractTags`, `ExtractTagsFromReader`)
- Extracted fields: title, artist, album, album artist, composer, genre, year, track/disc numbers, lyrics, comment, embedded cover art

**Custom Duration Parsers:**
- MP3: `backend/metadata/mp3duration.go` - Custom header parser for accurate duration (handles multiple ID3v2 tags that inflate `go-mp3`'s `Len()`)
- FLAC: `backend/metadata/flacduration.go` - Custom FLAC STREAMINFO header parser
- General: `backend/metadata/duration.go` - Fallback using beep decoder for WAV/OGG

**Combined Extraction:**
- `backend/metadata/metadata.go` → `ExtractAllMetadata()` - Single-pass extraction of tags, duration, and audio properties (sample rate, bit depth, channels, bitrate, file size)

## System Integrations

### MPRIS2 Media Controls (Linux)

- Implementation: `backend/mediacontrols/mpris_linux.go` (`//go:build linux`)
- D-Bus library: `github.com/godbus/dbus/v5`
- Bus name: `org.mpris.MediaPlayer2.yellowjacket`
- Object path: `/org/mpris/MediaPlayer2`
- Interfaces: `org.mpris.MediaPlayer2` (root), `org.mpris.MediaPlayer2.Player`
- Capabilities: Play, Pause, PlayPause, Stop, Next, Previous, Seek, SetPosition, Volume, Metadata push
- Non-Linux: No-op stub (`backend/mediacontrols/stub.go`, `//go:build !linux`)

**Architecture:** All D-Bus property updates are dispatched via a buffered channel (`updateChanSize = 64`) to a dedicated goroutine, preventing deadlocks between the player mutex and godbus property mutex.

### File System

- Library scanning: `backend/library/library.go` - Recursive `fs.WalkDir` with concurrent worker pool (`errgroup`)
- Disk type detection: `backend/system/disktype_linux.go` / `backend/system/disktype_other.go` - Detects HDD vs SSD for adaptive scan concurrency
- User data directories: `backend/system/userdata.go` - OS-specific paths for config and data
- Native dialogs: `backend/frontendutil/frontendutil.go` - Directory picker, file picker (for M3U import)

### Playlist Import/Export

- M3U/M3U8 parsing: `backend/playlist/m3u.go`
- Playlist matching: `backend/playlist/match.go` - Fuzzy matching of playlist entries to library tracks
- Favorites system: `backend/playlist/favorites.go` - Special playlist designated as favorites

### Cover Art System

- Extraction: Embedded art from audio file tags (`backend/library/coverart.go`)
- Storage: Hash-based filenames in `~/.local/share/yellowjacket/covers/`
- Size variants: Small (100px), Medium (200px), Large (400px) - generated via `golang.org/x/image`
- Serving: Custom HTTP handler at `/covers/` prefix (`backend/coverart/handler.go`)
- URL resolution: `backend/coverart/coverart.go` → `ResolveURLs()` converts filesystem paths to URL paths

### Custom Asset Server

- Implementation: `backend/assets/handler.go`
- Serves embedded frontend dist files via Wails asset server
- Supports custom route registration (used by cover art handler)
- Middleware pattern captures Wails' default handler for fallback

## Frontend Architecture

### Entry Points

- Main app: `frontend/index.html` → `frontend/index.ts`
- View routing: DOM-based navigation via `navigate` CustomEvent in `frontend/index.ts`
- Views: tracks, albums, playlists, artists, genres, libraries, settings, artist-details, genre-details

### State Management

Singleton stores in `frontend/src/store/`:
- `player-store.ts` - Playback state, current track, volume
- `queue-store.ts` - Queue tracks, current index, play mode
- `library-store.ts` - Library track listing
- `playlist-store.ts` - Playlist data
- `favorites-store.ts` - Favorites state
- `theme-store.ts` - Theme accent color and background shade
- `search-store.ts` - Search query and results
- `tracklist-store.ts` - Track list column configuration

Each store subscribes to Wails events and delegates actions to backend via Wails bindings.

### ReactiveController Pattern

Controllers in `frontend/src/store/controllers/` connect Lit components to stores:
- `player-controller.ts`, `queue-controller.ts`, `library-controller.ts`, `playlist-controller.ts`, `favorites-controller.ts`, `theme-controller.ts`, `search-controller.ts`, `tracklist-controller.ts`
- Subscribe in `hostConnected()`, unsubscribe in `hostDisconnected()`

## Profiling & Observability

**Development Only (eliminated in production builds):**
- pprof HTTP server: `localhost:6060` (`backend/profiling/profiling.go`, `//go:build dev`)
- Endpoints: `/debug/pprof/`, `/debug/trace`
- Block and mutex profiling enabled
- Custom `TimeOp()` function for operation timing

**Logging:**
- Framework: `log/slog` (structured, key-value pairs)
- Dev handler: `github.com/golang-cz/devslog` (pretty-printed to stdout)
- Wails logger bridge: `backend/logging/logging.go` (routes Wails logs through slog)
- Pattern: Logger injected via constructors, scoped with `logger.WithGroup("component")`

## External APIs & Services

**None.** YellowJacket is a fully local, offline application. There are no external API calls, cloud services, analytics, telemetry, or network requests. All data lives on the local filesystem.

## CI/CD & Deployment

**CI Pipeline:** Not detected in the repository (no `.github/workflows/`, `.gitlab-ci.yml`, etc.)

**Git Hooks (lefthook):**
- `lefthook.yml` - Pre-commit: go vet, golangci-lint, codegen check, frontend typecheck
- Pre-push: protect main branch, go test, go mod verify

**Distribution:** Binary builds via `make build-prod` (obfuscated + UPX compressed)

## Webhooks & Callbacks

**Incoming:** None
**Outgoing:** None

---

*Integration audit: 2026-02-26*

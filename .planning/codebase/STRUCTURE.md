# Codebase Structure

**Analysis Date:** 2026-02-26

## Directory Layout

```
yellowjacket/
├── backend/                # Go backend — all application logic
│   ├── app.go              # Main app struct, lifecycle hooks, dependency wiring
│   ├── assets/             # Custom HTTP asset handler for Wails webview
│   ├── config/             # Application config (TOML persistence, event emission)
│   ├── coverart/           # Cover art extraction, thumbnail generation, HTTP serving
│   ├── database/           # SQLite database layer with sqlc-generated queries
│   │   └── sql/            # SQL source files and generated code
│   │       ├── schemas/    # CREATE TABLE DDL (embedded at build time)
│   │       ├── queries/    # sqlc query definitions
│   │       └── sqlcgen/    # Auto-generated Go code (DO NOT EDIT)
│   ├── events/             # Centralized event name constants (must match frontend)
│   ├── favorites/          # Favorites config types
│   ├── ffmpeg/             # FFmpeg binary embedding (Linux/Windows)
│   │   └── bin/
│   ├── frontendutil/       # Frontend-bound utility functions (dialogs)
│   ├── library/            # Music library scanning, querying, cover art management
│   ├── logging/            # Wails logger adapter for slog
│   ├── mediacontrols/      # OS media controls (MPRIS on Linux, stub elsewhere)
│   ├── metadata/           # Audio file metadata extraction (tags, duration, decoding)
│   ├── player/             # Audio playback engine (beep library)
│   ├── playlist/           # Playlist management, M3U8 import/export, phantom resolution
│   ├── profiling/          # Dev-only pprof server and timing utilities
│   ├── queue/              # Playback queue with shuffle/repeat/persistence
│   ├── system/             # OS-specific utilities (user dirs, disk type detection)
│   ├── theme/              # Theme config types (accent color, background shade)
│   ├── tracklist/          # Track list column config types
│   └── ui/                 # UI-related backend types
├── frontend/               # TypeScript/Lit frontend
│   ├── index.html          # Main HTML entry point
│   ├── index.css           # Global styles
│   ├── package.json        # Node dependencies (Lit, Vite, WebAwesome)
│   ├── tsconfig.json       # TypeScript config with path aliases
│   ├── vite.config.mts     # Vite build config with alias resolution
│   ├── dist/               # Built frontend assets (gitignored)
│   ├── src/                # Source code
│   │   ├── events.ts       # Event name constants (must match backend)
│   │   ├── assets/         # Static assets (fonts, images, icons)
│   │   ├── components/     # Lit Web Components (UI)
│   │   ├── store/          # Singleton stores (backend state mirrors)
│   │   │   ├── index.ts    # Barrel exports for stores
│   │   │   └── controllers/ # ReactiveControllers connecting stores to components
│   │   └── utils/          # Shared frontend utilities
│   └── wailsjs/            # Auto-generated Wails bindings (DO NOT EDIT)
│       ├── go/             # Go function bindings for TypeScript
│       └── runtime/        # Wails runtime API (events, window, etc.)
├── internal/               # Internal Go packages
│   └── dev/                # Build-tag-based dev/prod detection
├── pkg/                    # Shared Go packages
│   └── templcomp/          # Shared templ component utilities
├── test_data/              # Test fixtures (audio files for testing)
│   └── music_library_test/ # Mock music library directory
├── build/                  # Build artifacts
│   └── bin/                # Compiled binaries
├── scripts/                # Development scripts (profiling)
├── docs/                   # Documentation
│   └── dev/                # Developer docs
├── .github/                # GitHub Actions workflows
│   └── workflows/
├── main.go                 # Application entry point
├── go.mod                  # Go module definition
├── go.sum                  # Go dependency checksums
├── Makefile                # Build commands (dev, build, test, lint, generate)
├── wails.json              # Wails project config
├── .golangci.yml           # golangci-lint v2 config
├── lefthook.yml            # Git hooks config
├── .releaserc.yml          # Semantic release config
├── renovate.json5          # Dependency update automation
└── AGENTS.md               # AI coding agent guidelines
```

## Directory Purposes

**`backend/`:**
- Purpose: All Go server-side application logic
- Contains: Domain packages, infrastructure, data access
- Key files: `app.go` (main app struct and lifecycle)

**`backend/player/`:**
- Purpose: Audio playback engine using the beep library
- Contains: Player struct, volume management, state persistence/restoration, track info emission
- Key files: `player.go` (main player logic, ~1105 lines), `volume.go` (volume type conversions)

**`backend/queue/`:**
- Purpose: Playback queue management — ordering, navigation, shuffle, repeat, persistence
- Contains: Queue struct, track management, auto-advance logic, shuffle/repeat navigation, event emission, DB persistence
- Key files: `queue.go` (main queue logic), `navigation.go` (next/previous/shuffle), `handlers.go` (playback finished), `emit.go` (event emission), `persistence.go` (DB save/restore)

**`backend/library/`:**
- Purpose: Music library scanning, metadata extraction pipeline, query interface
- Contains: Library struct, concurrent scan pipeline, cover art processing, database queries for tracks/albums/artists/genres
- Key files: `library.go` (scan pipeline), `query.go` (data access methods for frontend), `rescan.go` (full rescan with clear), `coverart.go` (cover art extraction/thumbnails), `config.go` (library config types), `metrics.go` (scan metrics)

**`backend/playlist/`:**
- Purpose: Playlist CRUD, M3U8 file management, phantom track resolution
- Contains: Playlist service, M3U8 parser/writer, track matching/scoring for phantom resolution
- Key files: `playlist.go` (main service, ~1779 lines), `m3u.go` (M3U8 parsing/writing), `match.go` (phantom track scoring), `favorites.go` (default playlist management)

**`backend/database/`:**
- Purpose: SQLite database access layer
- Contains: DB wrapper, schema management, migrations, FTS5 search
- Key files: `database.go` (connection, schema, migrations), `search.go` (FTS5 full-text search queries)

**`backend/databasekom/sql/schemas/`:**
- Purpose: SQLite CREATE TABLE statements embedded at build time
- Contains: 17 `.sql` files defining all tables
- Key tables: `audio_files`, `recordings`, `artists`, `artist_credit`, `release_groups`, `cover_art`, `genres`, `playlists`, `playlist_tracks`, `queue`, `queue_tracks`, `player_state`, `search_index` (FTS5)

**`backend/database/sql/queries/`:**
- Purpose: sqlc query definitions that generate type-safe Go code
- Contains: 13 `.sql` files with named queries
- Key files: `audio_files.sql`, `recordings.sql`, `playlists.sql`, `queue.sql`, `player_state.sql`

**`backend/database/sql/sqlcgen/`:**
- Purpose: Auto-generated Go code from sqlc (DO NOT EDIT)
- Contains: Type-safe query functions, model structs
- Regenerate: `make generate` or `go generate ./...`

**`backend/events/`:**
- Purpose: Centralized event name string constants for Go side
- Contains: Single file with const groups for playback, queue, config, playlist, library events
- Key file: `events.go`

**`backend/config/`:**
- Purpose: Application configuration management
- Contains: Config struct (TOML-backed), getter/setter methods that validate + save + emit events
- Key files: `config.go` (main config), `window.go` (window size config)
- Sub-configs: Library, Theme, Window, TrackList, Favorites — each defined in their own packages

**`backend/metadata/`:**
- Purpose: Audio file metadata extraction — tags, duration, genre parsing, decoding
- Contains: Tag extraction, custom MP3/FLAC duration parsers, audio file decoder
- Key files: `metadata.go` (tag extraction), `decoder.go` (audio format decoding), `duration.go` (duration calculation), `genre.go` (genre string parsing), `mp3duration.go`, `flacduration.go`

**`backend/coverart/`:**
- Purpose: Cover art storage, thumbnail generation, HTTP serving
- Contains: Cover art handler (HTTP), file management, sized variant generation
- Key files: `coverart.go` (path/URL resolution), `handler.go` (HTTP handler)

**`backend/assets/`:**
- Purpose: Custom HTTP asset handler wrapping Wails' default handler
- Contains: ServeMux-based routing with fallback to Wails asset handler
- Key file: `handler.go`

**`backend/mediacontrols/`:**
- Purpose: OS media control integration (MPRIS2 on Linux)
- Contains: Handler interface, Linux MPRIS implementation, no-op stub for other platforms
- Key files: `mediacontrols.go` (interface), `mpris_linux.go` (Linux), `stub.go` (fallback)

**`backend/system/`:**
- Purpose: OS-specific system utilities
- Contains: User directory paths (config/data), disk type detection
- Key files: `userdata.go` (user dir paths), `disktype_linux.go` / `disktype_other.go`

**`backend/profiling/`:**
- Purpose: Dev-only profiling (pprof server, operation timing)
- Contains: Build-tagged profiling code — dev builds start pprof on :6060, prod builds are no-ops
- Key files: `profiling.go` (dev), `profiling_prod.go` (prod no-op), `timing.go` / `timing_prod.go`

**`backend/logging/`:**
- Purpose: Wails logger adapter that routes Wails log calls to slog
- Key file: `logging.go`

**`backend/frontendutil/`:**
- Purpose: Utility Go functions bound to the frontend (file/directory dialogs)
- Key file: `frontendutil.go`

**`backend/theme/`:**
- Purpose: Theme configuration types (accent color, background shade)
- Key file: `config.go`

**`backend/tracklist/`:**
- Purpose: Track list column configuration types
- Key file: `config.go`

**`backend/favorites/`:**
- Purpose: Favorites/default playlist configuration types
- Key file: `config.go`

**`frontend/src/components/`:**
- Purpose: All Lit Web Components (custom elements)
- Contains: Each component in its own subdirectory with `.ts` file(s)
- Key components:
  - `audio-player/` — Player controls, seekbar, volume control
  - `track-list/` — Main track listing table with column config and search ranking
  - `queue-panel/` — Queue display and management
  - `sidebar/` — Navigation sidebar
  - `cover-grid/` — Album cover grid with virtual scrolling
  - `now-playing/` — Current track info display
  - `config-page/` — Settings UI
  - `playlist-view/` — Playlist display and management
  - `artists-view/` — Artist listing
  - `genres-view/` — Genre listing
  - `search-bar/` — Search input

**`frontend/src/store/`:**
- Purpose: Singleton state stores mirroring backend state
- Contains: Store classes with event bridge, state access, actions (delegated to backend), subscription system
- Key files: `player-store.ts`, `queue-store.ts`, `library-store.ts`, `playlist-store.ts`, `theme-store.ts`, `search-store.ts`, `favorites-store.ts`, `tracklist-store.ts`
- Barrel: `index.ts` re-exports stores and types

**`frontend/src/store/controllers/`:**
- Purpose: ReactiveControllers connecting Lit components to stores
- Contains: Controller classes that subscribe on `hostConnected()` and unsubscribe on `hostDisconnected()`
- Pattern: `new PlayerController(this)` in component constructor
- Key files: `player-controller.ts`, `queue-controller.ts`, `library-controller.ts`, `playlist-controller.ts`, `theme-controller.ts`, `search-controller.ts`, `favorites-controller.ts`, `tracklist-controller.ts`

**`frontend/src/utils/`:**
- Purpose: Shared frontend utility functions and controllers
- Key files: `format.ts` (display formatting), `time.ts` (time formatting), `context-menu-controller.ts`, `drag-controller.ts`, `selection-controller.ts`, `drag-image.ts`

**`frontend/src/assets/`:**
- Purpose: Static assets (fonts, images, icons)
- Contains: Font files, SVG icons organized by category (`icons/music/`, `icons/ui/`)

**`frontend/wailsjs/`:**
- Purpose: Auto-generated Wails bindings (DO NOT EDIT)
- Contains: TypeScript wrappers for Go functions and Wails runtime API
- Key directories: `go/` (bindings for each bound Go package), `runtime/` (Wails runtime API)
- Regenerated automatically by Wails on build

**`internal/dev/`:**
- Purpose: Build-tag-based dev/prod detection
- Contains: Two files with opposite build tags
- Key files: `devbuild.go` (`//go:build dev` → `IsDev = true`), `nondevbuild.go` (`//go:build !dev` → `IsDev = false`)

**`test_data/`:**
- Purpose: Test fixtures for audio file tests
- Contains: Sample audio files in `music_library_test/` directory
- Used by: `*_test.go` files that need real audio data

## Key File Locations

**Entry Points:**
- `main.go`: Application entry point — logger setup, asset handler, app creation, `wails.Run()`
- `backend/app.go`: Main app struct `YellowJacketApp`, lifecycle hooks, dependency wiring
- `frontend/index.html`: Frontend HTML entry point loaded by Wails webview

**Configuration:**
- `wails.json`: Wails project config (name, frontend commands)
- `frontend/tsconfig.json`: TypeScript config with strict mode and path aliases
- `frontend/vite.config.mts`: Vite build config with path alias resolution
- `frontend/package.json`: Node.js dependencies and scripts
- `.golangci.yml`: golangci-lint v2 configuration
- `Makefile`: Build commands (dev, build-dev, build-prod, test, lint, generate)
- `go.mod`: Go module definition and dependencies
- `lefthook.yml`: Git hook configuration

**Core Logic:**
- `backend/player/player.go`: Audio playback engine (~1105 lines)
- `backend/queue/queue.go`: Queue management (~1169 lines)
- `backend/library/library.go`: Library scan pipeline (~1329 lines)
- `backend/playlist/playlist.go`: Playlist service (~1779 lines)
- `backend/database/database.go`: Database connection and schema management
- `backend/database/search.go`: FTS5 search implementation
- `backend/config/config.go`: Application config management

**Event Contracts:**
- `backend/events/events.go`: Go event name constants
- `frontend/src/events.ts`: TypeScript event name constants (must match Go)

**Frontend State:**
- `frontend/src/store/player-store.ts`: Player state mirror
- `frontend/src/store/queue-store.ts`: Queue state mirror with delta event handling
- `frontend/src/store/index.ts`: Barrel exports for all stores

## Naming Conventions

**Files:**
- Go: `snake_case.go` — e.g., `player.go`, `queue_tracks.go`, `cover_art.go`
- Go tests: `*_test.go` co-located with source — e.g., `player_test.go`
- TypeScript: `kebab-case.ts` — e.g., `player-store.ts`, `audio-player.ts`
- SQL schemas: `snake_case.sql` — e.g., `audio_files.sql`, `player_state.sql`

**Directories:**
- Go packages: `lowercase` single word — e.g., `player`, `queue`, `library`, `metadata`
- Multi-word Go: `lowercase` concatenated — e.g., `frontendutil`, `mediacontrols`, `coverart`
- Frontend components: `kebab-case` — e.g., `audio-player/`, `track-list/`, `queue-panel/`
- Frontend stores: flat in `store/` directory

## Where to Add New Code

**New Backend Feature/Package:**
- Create directory: `backend/{feature}/`
- Add package doc comment
- Wire into `backend/app.go` — create in `NewYellowJacketApp()`, call `SetContext()` in `OnStartup()`
- If frontend-callable: add to `FEBindings` slice in `backend/app.go`
- If emitting events: add event names to `backend/events/events.go` AND `frontend/src/events.ts`

**New Frontend Component:**
- Create directory: `frontend/src/components/{component-name}/`
- Create main file: `{component-name}.ts`
- Use `@customElement('{component-name}')` decorator
- Connect to store via controller: `private player = new PlayerController(this);`
- Use path aliases for imports: `@store/*`, `@components/*`, `@go/*`, `@utils/*`

**New Frontend Store:**
- Create file: `frontend/src/store/{name}-store.ts`
- Create matching controller: `frontend/src/store/controllers/{name}-controller.ts`
- Export from `frontend/src/store/index.ts`
- Subscribe to backend events in constructor
- Delegate actions to Go via Wails bindings

**New Database Table:**
- Add schema: `backend/database/sql/schemas/{table_name}.sql`
- Add queries: `backend/database/sql/queries/{table_name}.sql`
- Run `make generate` to regenerate `backend/database/sql/sqlcgen/`
- Never edit files in `sqlcgen/` directly

**New SQL Query:**
- Add to appropriate file in `backend/database/sql/queries/`
- Run `make generate`
- Use generated methods via `db.Queries.{MethodName}()`

**New Event:**
- Add Go constant: `backend/events/events.go`
- Add TypeScript constant: `frontend/src/events.ts` (must match exactly)
- Emit in Go: `runtime.EventsEmit(ctx, events.EventName, payload)`
- Subscribe in TypeScript store: `EventsOn(Events.EventName, handler)`

**Utilities:**
- Go shared helpers: `pkg/` for cross-package utilities
- Go internal helpers: `internal/` for project-internal utilities
- Frontend shared helpers: `frontend/src/utils/`

## Special Directories

**`frontend/wailsjs/`:**
- Purpose: Auto-generated Wails TypeScript bindings for Go functions
- Generated: Yes — by Wails build tooling
- Committed: Yes
- DO NOT EDIT — regenerated on every build

**`backend/database/sql/sqlcgen/`:**
- Purpose: Auto-generated Go code from sqlc query definitions
- Generated: Yes — by `go tool sqlc generate` via `make generate`
- Committed: Yes
- DO NOT EDIT — regenerate with `make generate`

**`frontend/dist/`:**
- Purpose: Built frontend assets (Vite output)
- Generated: Yes — by `pnpm build`
- Committed: No (gitignored)

**`build/bin/`:**
- Purpose: Compiled application binaries
- Generated: Yes — by Wails build
- Committed: No

**`*_templ.go` files:**
- Purpose: Auto-generated Go code from templ templates
- Generated: Yes — by `go tool templ generate` via `make generate`
- Committed: Yes
- DO NOT EDIT — regenerate with `make generate`

**`test_data/`:**
- Purpose: Audio test fixtures for unit tests
- Generated: No — manually curated test files
- Committed: Yes

**`internal/dev/`:**
- Purpose: Build-tag-based dev/prod detection flag
- Generated: No
- Committed: Yes
- `devbuild.go` (`//go:build dev`): `IsDev = true`
- `nondevbuild.go` (`//go:build !dev`): `IsDev = false`

---

*Structure analysis: 2026-02-26*

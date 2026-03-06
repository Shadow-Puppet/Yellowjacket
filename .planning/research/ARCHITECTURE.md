# Architecture Patterns: v1.1 Feature Integration

**Domain:** Desktop music player — new feature integration with existing Wails/Lit/beep/SQLite architecture
**Researched:** 2026-03-06
**Confidence:** HIGH (derived from complete codebase read + official docs for beep, MusicBrainz API, dhowden/tag)

## Recommended Architecture

YellowJacket v1.1 adds 8 features to an existing, well-structured codebase. The architecture approach is **integration-first**: each feature slots into established patterns (two-phase init, event-driven sync, mutex-protected state, sqlc codegen) rather than introducing new architectural paradigms. The one exception is the plugin system, which necessarily introduces a new extension mechanism.

### High-Level Integration Map

```
                    ┌─────────────────────────────────────────┐
                    │              app.go (wiring)             │
                    │  New: TagEditor, SmartPlaylist,          │
                    │       MusicBrainz, Shortcuts, Layout     │
                    └────────────────┬────────────────────────┘
                                     │
        ┌────────────────────────────┼────────────────────────────┐
        │                            │                            │
   ┌────▼────┐                 ┌─────▼─────┐               ┌─────▼─────┐
   │ player  │                 │ database  │               │  events   │
   │         │                 │           │               │           │
   │ +gapless│                 │ +smart_pl │               │ +TagsEdit │
   │ +xfade  │                 │ +shortcuts│               │ +ScanCanc │
   │         │                 │ +layout   │               │ +SmartPL  │
   └─────────┘                 └───────────┘               │ +Shortcut │
                                                           │ +Layout   │
                                                           │ +MBrainz  │
                                                           └───────────┘
```

### Component Boundaries

| Component | Responsibility | New vs Modified | Communicates With |
|-----------|---------------|-----------------|-------------------|
| `backend/tageditor/` | Read/write audio file tags, coordinate DB updates | **NEW** package | metadata, database, library, events |
| `backend/library/` | Scan cancellation via context | **MODIFIED** | database, events, coverart |
| `backend/smartplaylist/` | Rule-based dynamic playlists | **NEW** package | database, events |
| `backend/shortcuts/` | Keyboard shortcut registry + dispatch | **NEW** package | config, events, player, queue |
| `backend/player/` | Gapless playback + crossfade | **MODIFIED** | beep, events, queue |
| `backend/musicbrainz/` | MusicBrainz API client + caching | **NEW** package | database (cache tables), events |
| `backend/layout/` | Layout section configuration | **NEW** package | config, events |
| `backend/plugin/` | Plugin loading, lifecycle, API surface | **NEW** package | all packages (via API) |
| `frontend/src/store/tageditor-store.ts` | Tag edit state | **NEW** store | backend tageditor bindings |
| `frontend/src/store/smartplaylist-store.ts` | Smart playlist state | **NEW** store | backend smartplaylist bindings |
| `frontend/src/store/shortcut-store.ts` | Shortcut config state | **NEW** store | backend shortcuts bindings |
| `frontend/src/store/musicbrainz-store.ts` | MB browsing state | **NEW** store | backend musicbrainz bindings |
| `frontend/src/store/layout-store.ts` | Layout config state | **NEW** store | backend layout bindings |

### Data Flow

**Existing pattern preserved**: Backend is source of truth. Frontend stores are reactive mirrors. Events flow backend-to-frontend. Actions flow frontend-to-backend via Wails bindings.

**New data flows:**

1. **Tag Edit Flow**: Frontend collects edits → Wails binding → `tageditor.SaveTags()` → write file tags → update DB records → emit `TagsEdited` event → frontend refreshes affected views
2. **Scan Cancel Flow**: Frontend sends cancel request → Wails binding → `library.CancelScan()` → cancel context propagation → scan goroutines check `ctx.Done()` → emit `LibraryScanCancelled` event
3. **Smart Playlist Flow**: User defines rules via frontend → Wails binding → `smartplaylist.Create()` → persist rules to DB → evaluate rules → emit `SmartPlaylistChanged` event → frontend refreshes
4. **Gapless Flow**: Player pre-decodes next track in background → when current track ends, swap streamer chains without speaker interruption → seamless transition
5. **MusicBrainz Flow**: Frontend search query → Wails binding → `musicbrainz.Search()` → HTTP GET to MB API (rate-limited) → cache results in SQLite → return to frontend → display

---

## Feature 1: Tag Editing

### Architecture

**New package: `backend/tageditor/`**

The existing `dhowden/tag` library is **read-only**. Tag writing requires a separate library. Use `bogem/id3v2` for MP3 files and `go-flac/go-flac` (or equivalent) for FLAC Vorbis comments. OGG and WAV tag writing can be deferred (LOW priority formats).

**Confidence:** HIGH — `dhowden/tag` has no write support (confirmed from source). `bogem/id3v2` is the standard Go ID3v2 writer.

```go
// backend/tageditor/tageditor.go
type TagEditor struct {
    mu     sync.Mutex
    ctx    context.Context
    logger *slog.Logger
    db     *database.DB
    lib    *library.Library // for FTS re-indexing
}

type TagUpdate struct {
    FilePath string
    Title    string
    Artist   string
    Album    string
    // ... all editable fields
}

func (te *TagEditor) SaveTags(update TagUpdate) error {
    // 1. Write tags to audio file (format-specific writer)
    // 2. Update recording/artist_credit/release_group in DB
    // 3. Re-index in FTS5 search_index
    // 4. Emit TagsEdited event with affected file paths
}
```

### Integration Points

| Touchpoint | Change | Risk |
|------------|--------|------|
| `backend/app.go` | Add TagEditor to `FEBindings`, wire in `OnStartup` | LOW — follows existing pattern |
| `backend/events/events.go` | Add `TagsEdited`, `TagEditFailed` events | LOW — codegen handles sync |
| `backend/database/` | New queries: `UpdateRecordingMetadata`, `GetRecordingByAudioFileID` | LOW — sqlc pattern |
| `backend/metadata/tags.go` | Add `WriteTags()` function alongside existing `ExtractTags()` | MEDIUM — new dependency |
| `frontend/src/components/` | New `<tag-editor>` component (modal/panel) | LOW |
| Library/Queue/Player stores | Must react to `TagsEdited` to refresh displayed metadata | MEDIUM — cross-store coordination |

### Critical Design Decision

**Write tags to file first, then update DB.** If the file write fails, the DB stays consistent. If the DB update fails after file write, the next scan will reconcile. This matches the existing pattern where the filesystem is the primary source and the DB is derived.

### DB Update Strategy

Tag edits must cascade through the normalized schema:
1. Update `recordings.name` (title)
2. Upsert `artist_credit` + `artists` + link table (if artist changed)
3. Upsert `release_groups` (if album changed)
4. Re-link `release_group_recordings`
5. Re-index FTS5 `search_index`

All within a single transaction. The existing `entityCache` pattern from library scanning can be reused for lookups.

---

## Feature 2: Scan Cancellation

### Architecture

**Modified: `backend/library/library.go`**

The scan pipeline already uses `l.ctx` for context propagation. Cancellation requires:
1. A dedicated `context.CancelFunc` stored on the Library struct
2. All scan phases checking `ctx.Done()` (most already do via `select` in the walk and worker phases)

```go
type Library struct {
    mu           sync.Mutex
    ctx          context.Context
    // ... existing fields ...
    scanCancel   context.CancelFunc  // NEW: cancel function for active scan
    scanning     bool                // NEW: flag for active scan
}

func (l *Library) CancelScan() {
    l.mu.Lock()
    defer l.mu.Unlock()
    if l.scanCancel != nil {
        l.scanCancel()
    }
}

func (l *Library) Scan() (*ScanMetrics, error) {
    scanCtx, cancel := context.WithCancel(l.ctx)
    l.mu.Lock()
    l.scanCancel = cancel
    l.scanning = true
    l.mu.Unlock()
    defer func() {
        l.mu.Lock()
        l.scanCancel = nil
        l.scanning = false
        l.mu.Unlock()
    }()
    // ... existing scan code, but use scanCtx instead of l.ctx ...
}
```

### Integration Points

| Touchpoint | Change | Risk |
|------------|--------|------|
| `backend/library/library.go` | Add `scanCancel` field, `CancelScan()` method, wrap scan in child context | LOW — surgical change |
| `backend/events/events.go` | Add `LibraryScanCancelled` event | LOW |
| `frontend/src/store/library-store.ts` | Listen for cancelled event, update scan state | LOW |
| Frontend scan progress UI | Add cancel button | LOW |

### Key Constraint

The scan's DB writer goroutine commits in batches of 50. Cancellation should allow the current batch to complete (don't leave a half-committed transaction). Check `scanCtx.Done()` between batches, not mid-batch.

---

## Feature 3: Smart Playlists

### Architecture

**New package: `backend/smartplaylist/`**

Smart playlists are rule-based queries that dynamically produce track lists. They are **not** persisted as `playlist_tracks` — they're evaluated on demand from the rule definition.

```go
// backend/smartplaylist/smartplaylist.go
type Service struct {
    mu     sync.Mutex
    ctx    context.Context
    logger *slog.Logger
    db     *database.DB
}

type Rule struct {
    Field    string // "genre", "year", "artist", "album", "play_count", "date_added"
    Operator string // "is", "is_not", "contains", "greater_than", "less_than", "between"
    Value    string
    Value2   string // for "between" operator
}

type SmartPlaylist struct {
    ID        int64
    Name      string
    Rules     []Rule
    MatchAll  bool   // AND vs OR
    OrderBy   string
    Limit     int
}

func (s *Service) Evaluate(id int64) ([]Track, error) {
    // 1. Load smart playlist rules from DB
    // 2. Build SQL WHERE clause from rules
    // 3. Query track_metadata VIEW with dynamic conditions
    // 4. Return results
}
```

### Database Schema

```sql
-- New table
CREATE TABLE smart_playlists (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    rules_json TEXT NOT NULL,  -- JSON-encoded []Rule
    match_all BOOLEAN NOT NULL DEFAULT 1,
    order_by TEXT NOT NULL DEFAULT 'title',
    max_tracks INTEGER NOT NULL DEFAULT 0,  -- 0 = unlimited
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### Query Generation Strategy

Rules map to the existing `track_metadata` VIEW columns. Build parameterized WHERE clauses:

```go
func buildWhereClause(rules []Rule, matchAll bool) (string, []any) {
    // Each rule becomes: "column OPERATOR ?"
    // Combined with AND (matchAll) or OR (!matchAll)
    // All values are parameterized — no SQL injection risk
}
```

**Use raw `db.QueryContext()` for dynamic queries** — sqlc cannot generate dynamic WHERE clauses. Document with `// SAFETY:` comments per existing convention.

### Integration Points

| Touchpoint | Change | Risk |
|------------|--------|------|
| `backend/app.go` | Add SmartPlaylist service to `FEBindings` | LOW |
| `backend/database/` | New schema for `smart_playlists` table, migration 6 | LOW |
| `backend/events/events.go` | Add `SmartPlaylistChanged`, `SmartPlaylistDeleted` events | LOW |
| Frontend | New `<smart-playlist-editor>` component, rules builder UI | MEDIUM — most complex frontend work |

---

## Feature 4: Customizable Keyboard Shortcuts

### Architecture

**New package: `backend/shortcuts/`**

Shortcuts are stored in the TOML config and dispatched via Wails events. The backend holds the definitive shortcut map; the frontend registers a global `keydown` listener that sends key combos to the backend for resolution.

```go
// backend/shortcuts/shortcuts.go
type Service struct {
    mu       sync.Mutex
    ctx      context.Context
    logger   *slog.Logger
    bindings map[string]string // key combo → action name
    actions  map[string]func() // action name → handler
}

type Shortcut struct {
    Action string `toml:"Action" json:"action"`
    Key    string `toml:"Key" json:"key"` // e.g., "Ctrl+Space", "MediaPlayPause"
}
```

### Config Integration

Add a `[Shortcuts]` section to `config.toml`:

```toml
[Shortcuts]
PlayPause = "Space"
NextTrack = "Ctrl+Right"
PrevTrack = "Ctrl+Left"
VolumeUp = "Ctrl+Up"
VolumeDown = "Ctrl+Down"
# ...
```

### Frontend Dispatch Pattern

```typescript
// Frontend: global keydown handler
document.addEventListener('keydown', (e) => {
    const combo = buildComboString(e); // e.g., "Ctrl+Space"
    Shortcuts.Execute(combo); // Wails binding → backend resolves + executes
});
```

**Why backend dispatch?** The backend already owns all action handlers (player.Play, queue.Next, etc.). Having the backend resolve shortcuts avoids duplicating action dispatch logic in the frontend. The frontend's only job is translating DOM KeyboardEvents into combo strings.

### Integration Points

| Touchpoint | Change | Risk |
|------------|--------|------|
| `backend/config/config.go` | Add `Shortcuts` config section | LOW |
| `backend/app.go` | Wire Shortcuts service, register action handlers | LOW |
| `frontend/index.ts` | Add global keydown listener | LOW |
| MPRIS callbacks | Already wired in `app.go OnStartup` — shortcut actions reuse same handler functions | LOW |

---

## Feature 5: Gapless Playback + Crossfade

### Architecture

**Modified: `backend/player/player.go`**

This is the most architecturally complex feature because it fundamentally changes how track transitions work.

### Gapless Playback

**Current behavior:** `beep.Callback` fires → `onPlaybackFinished()` goroutine → queue calls `player.LoadFile()` → decode + resample + register with speaker. This gap (file open + decode) causes audible silence.

**Gapless approach:** Pre-decode the next track while the current one is still playing. When the current track's streamer is near exhaustion, seamlessly swap to the pre-decoded next track.

```go
type Player struct {
    // ... existing fields ...
    
    // Gapless pre-loading
    nextFile       *os.File
    nextStreamer    beep.StreamSeekCloser
    nextFormat     beep.Format
    nextBuffered   *BufferedStreamer
    nextFilePath   string
    gaplessEnabled bool
}

// PreloadNext is called by the queue when it knows what track comes next.
func (p *Player) PreloadNext(filePath string) error {
    p.mu.Lock()
    defer p.mu.Unlock()
    // Open file, decode, build resampled chain, store in next* fields
    // Do NOT register with speaker yet
}
```

**Speaker integration:** Use `beep.Seq()` to chain current + next streamer, or use a custom `GaplessStreamer` that automatically drains the current streamer and transitions to the next one without the `beep.Callback` → goroutine → LoadFile delay.

The key insight: beep's `Seq(a, b)` already provides gapless transition between two streamers. The challenge is having `b` ready before `a` ends.

### Crossfade

Crossfade uses beep's `Mixer` to overlap two tracks:

```go
// When crossfade is enabled and we're N seconds from track end:
// 1. Start fading out current track's volume
// 2. Start next track at low volume, fade in
// 3. Mix both through beep.Mixer
```

This requires:
1. A `crossfadeDuration` config setting (default 0 = disabled, range 1-12 seconds)
2. A crossfade mixer that handles the volume ramping
3. Knowing the remaining duration of the current track to trigger crossfade at the right time

### Queue Integration

The queue must tell the player what's next:

```go
// In queue.go, after track advance logic:
func (q *Queue) notifyNextTrack() {
    nextIdx := q.peekNextIndex() // look ahead without advancing
    if nextIdx >= 0 && nextIdx < len(q.tracks) {
        q.player.PreloadNext(q.tracks[nextIdx].FilePath)
    }
}
```

This notification happens:
- After `SetQueue` (next track is known)
- After `Next`/`Previous` (new next track)
- After `OnPlaybackFinished` auto-advance (next-next track)

### TrackLoader Interface Change

```go
type TrackLoader interface {
    LoadFile(filePath string) error
    Play() error
    IsPlaying() bool
    CurrentPositionSeconds() (int, error)
    UnloadTrack()
    PreloadNext(filePath string) error  // NEW
}
```

### Integration Points

| Touchpoint | Change | Risk |
|------------|--------|------|
| `backend/player/player.go` | Pre-loading, gapless streamer chain, crossfade mixer | **HIGH** — core audio pipeline |
| `backend/queue/queue.go` | `TrackLoader` interface extension, next-track notification | MEDIUM |
| `backend/config/config.go` | Crossfade duration setting | LOW |
| `backend/events/events.go` | Potentially `CrossfadeStarted` event | LOW |
| Speaker initialization | May need larger speaker buffer for crossfade overlap | MEDIUM |

### Risk Mitigation

- **Start with gapless only**, defer crossfade. Gapless is the higher-value feature.
- Gapless can be implemented by pre-decoding and using `beep.Seq()` to chain streamers — this is well-supported by beep.
- Crossfade is additive — build it on top of working gapless.
- The existing `BufferedStreamer` read-ahead pattern provides a foundation for pre-loading.

---

## Feature 6: MusicBrainz Browser

### Architecture

**New package: `backend/musicbrainz/`**

Read-only catalog browsing. The MusicBrainz API is rate-limited to **1 request per second** and requires a meaningful User-Agent header.

**Confidence:** HIGH — MusicBrainz API docs confirmed. JSON format via `fmt=json` or `Accept: application/json`.

```go
// backend/musicbrainz/client.go
type Client struct {
    mu         sync.Mutex
    ctx        context.Context
    logger     *slog.Logger
    db         *database.DB  // for response caching
    httpClient *http.Client
    rateLimiter *time.Ticker  // 1 req/sec
    userAgent  string
}

const apiBaseURL = "https://musicbrainz.org/ws/2/"

func (c *Client) SearchArtist(query string, limit, offset int) (*ArtistSearchResult, error)
func (c *Client) GetArtist(mbid string) (*Artist, error)
func (c *Client) GetArtistReleaseGroups(mbid string, limit, offset int) (*ReleaseGroupBrowse, error)
func (c *Client) GetReleaseGroup(mbid string) (*ReleaseGroup, error)
func (c *Client) GetRelease(mbid string) (*Release, error)
```

### Rate Limiting

```go
// Enforce 1 request per second globally
func (c *Client) doRequest(url string) ([]byte, error) {
    <-c.rateLimiter.C  // Block until rate limit allows
    // ... execute HTTP GET with User-Agent header ...
}
```

### Response Caching

Cache MB API responses in SQLite to avoid redundant requests:

```sql
CREATE TABLE musicbrainz_cache (
    url TEXT PRIMARY KEY,
    response_json TEXT NOT NULL,
    fetched_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

Cache TTL: 24 hours for search results, 7 days for entity lookups (data changes infrequently).

### Integration Points

| Touchpoint | Change | Risk |
|------------|--------|------|
| `backend/app.go` | Add MusicBrainz client to `FEBindings` | LOW |
| `backend/database/` | New `musicbrainz_cache` table, migration | LOW |
| `go.mod` | No new dependencies — use `net/http` from stdlib | LOW |
| Frontend | New `<musicbrainz-browser>` component with search, artist, album views | MEDIUM |

### Key Constraint

The app is currently fully offline. MusicBrainz browsing introduces the first network dependency. Handle network errors gracefully — cache-first with fallback, clear "offline/error" states in the UI, timeouts on HTTP requests.

---

## Feature 7: Layout Customization System

### Architecture

**New package: `backend/layout/`**

MusicBee-style section-based UI customization. The layout defines which component renders in each section of the UI.

### Section Model

The current `index.html` defines a fixed layout:
```
┌─────────────────────────────────────────┐
│ header (title + search-bar)             │
├─────────┬───────────────────┬───────────┤
│ sidebar │ main-panel        │ queue-    │
│         │ (track-list)      │ panel     │
│         │                   │           │
├─────────┴───────────────────┴───────────┤
│ footer (now-playing + audio-player)     │
└─────────────────────────────────────────┘
```

Make sections configurable:

```go
// backend/layout/layout.go
type Section struct {
    ID        string `toml:"ID" json:"id"`
    Component string `toml:"Component" json:"component"` // component tag name
    Visible   bool   `toml:"Visible" json:"visible"`
}

type Layout struct {
    Sections []Section `toml:"Sections" json:"sections"`
}
```

### Config Integration

```toml
[Layout]
[[Layout.Sections]]
ID = "sidebar"
Component = "app-sidebar"
Visible = true

[[Layout.Sections]]
ID = "main-panel"
Component = "track-list"
Visible = true

[[Layout.Sections]]
ID = "right-panel"
Component = "queue-panel"
Visible = true
```

### Frontend Implementation

The layout engine lives in the frontend. It reads the layout config and dynamically instantiates components in their designated sections:

```typescript
// frontend/src/layout/layout-engine.ts
class LayoutEngine {
    private sectionMap: Map<string, HTMLElement>;
    
    applyLayout(config: LayoutConfig) {
        for (const section of config.sections) {
            const container = this.sectionMap.get(section.id);
            if (container) {
                container.innerHTML = '';
                if (section.visible) {
                    const el = document.createElement(section.component);
                    container.appendChild(el);
                }
            }
        }
    }
}
```

### Component Registry

Each component declares its size constraints (min width, preferred width, etc.) so the layout engine can validate configurations:

```typescript
interface LayoutComponent {
    tagName: string;
    displayName: string;
    allowedSections: string[]; // which sections this can go in
    minWidth?: number;
    minHeight?: number;
}

const COMPONENT_REGISTRY: LayoutComponent[] = [
    { tagName: 'track-list', displayName: 'Track List', allowedSections: ['main-panel'] },
    { tagName: 'cover-grid', displayName: 'Album Grid', allowedSections: ['main-panel'] },
    { tagName: 'queue-panel', displayName: 'Queue', allowedSections: ['right-panel', 'main-panel'] },
    // ...
];
```

### Integration Points

| Touchpoint | Change | Risk |
|------------|--------|------|
| `backend/config/config.go` | Add `Layout` config section | LOW |
| `frontend/index.html` | Replace hard-coded components with section containers | MEDIUM |
| `frontend/index.ts` | Initialize layout engine, apply config | MEDIUM |
| All existing components | No changes — they're already self-contained Web Components | LOW |

---

## Feature 8: Plugin System

### Architecture

**New package: `backend/plugin/`**

This is the most complex architectural addition. The plugin system provides extensibility hooks for both backend logic and frontend UI.

### Plugin Loading

Plugins are directories in `~/.local/share/yellowjacket/plugins/`, each containing:
- `manifest.json` — name, version, entry points, permissions
- `main.js` — frontend code (Lit component)
- `backend.go` (optional) — Go plugin via `plugin` package or WASM

**Recommended approach for v1.1: JavaScript-only plugins.** Go's `plugin` package has severe limitations (Linux-only, same Go version required, no unloading). WASM is possible but adds complexity. JS-only plugins can:
- Register new UI components
- Subscribe to events
- Call exposed backend APIs via Wails bindings
- Add sidebar items, context menu entries, toolbar buttons

### Plugin API Surface

```typescript
// frontend/src/plugin/api.ts
interface YellowJacketAPI {
    // Events
    on(event: string, callback: Function): void;
    emit(event: string, data: any): void;
    
    // Player
    player: {
        play(): void;
        pause(): void;
        seek(seconds: number): void;
        getState(): PlayerState;
    };
    
    // Queue
    queue: {
        addTrack(path: string): void;
        getState(): QueueState;
    };
    
    // Library
    library: {
        search(query: string): Promise<Track[]>;
        getTrackMetadata(path: string): Promise<TrackInfo>;
    };
    
    // UI
    ui: {
        registerSidebarItem(item: SidebarItem): void;
        registerContextMenuItem(item: ContextMenuItem): void;
        registerComponent(tagName: string, component: typeof LitElement): void;
    };
}
```

### Plugin Lifecycle

```
1. App startup → scan plugins directory
2. Parse manifest.json for each plugin
3. Validate permissions
4. Load JS entry point in sandboxed context
5. Call plugin.init(api) with the API surface
6. Plugin registers its components/handlers
7. App shutdown → call plugin.destroy() for each
```

### Sandboxing

Plugins run in the same webview context (no iframe sandbox — too restrictive for Lit components). Instead, use an API-mediated approach: plugins can only interact with the app through the provided API object, not by reaching into internal stores or DOM directly.

### Backend Plugin Hooks

For backend extensibility, expose hooks rather than full plugin loading:

```go
// backend/plugin/hooks.go
type Hooks struct {
    OnTrackChanged    []func(trackInfo player.TrackInfo)
    OnLibraryScanDone []func(metrics *library.ScanMetrics)
    OnConfigChanged   []func(key string, value any)
}
```

### Integration Points

| Touchpoint | Change | Risk |
|------------|--------|------|
| `backend/app.go` | Plugin loader initialization | MEDIUM |
| `backend/plugin/` | New package with loader, manifest parser, hook registry | HIGH — significant new code |
| `frontend/src/plugin/` | New directory with API, loader, registry | HIGH |
| `frontend/index.ts` | Plugin initialization after DOM ready | MEDIUM |
| Security | Plugin code is untrusted — API surface must be carefully scoped | HIGH |

### v1.1 Scope Recommendation

For v1.1, implement the **foundation**:
1. Plugin directory scanning + manifest parsing
2. JS plugin loading mechanism
3. Core API surface (events, player, queue)
4. One example plugin demonstrating the pattern

Defer to later: backend Go plugins, WASM plugins, plugin marketplace, permissions system.

---

## Patterns to Follow

### Pattern 1: Two-Phase Initialization for New Packages

**What:** All new packages that need Wails runtime follow `New*()` + `SetContext()`.

**When:** Any new struct that emits events or uses Wails dialogs.

**Example:**
```go
// backend/tageditor/tageditor.go
func NewTagEditor(logger *slog.Logger, db *database.DB) *TagEditor {
    return &TagEditor{
        logger: logger.WithGroup("tageditor"),
        db:     db,
    }
}

func (te *TagEditor) SetContext(ctx context.Context) {
    te.mu.Lock()
    defer te.mu.Unlock()
    te.ctx = ctx
}
```

### Pattern 2: Event-Driven Frontend Sync

**What:** Backend emits events; frontend stores subscribe.

**When:** Any state change that the frontend needs to reflect.

**Example:**
```go
// Backend emits
runtime.EventsEmit(te.ctx, events.TagsEdited, map[string]any{
    "filePaths": affectedPaths,
})

// Frontend subscribes
EventsOn(Events.TagsEdited, (data: {filePaths: string[]}) => {
    // Refresh affected track displays
});
```

### Pattern 3: Config Section Pattern

**What:** New config sections follow the `ApplyDefaults()` + `Validate()` pattern.

**When:** Any new user-configurable setting.

**Example:**
```go
// backend/shortcuts/config.go
type Config struct {
    Bindings []Shortcut `toml:"Bindings"`
}

func (c *Config) ApplyDefaults() { /* ... */ }
func (c *Config) Validate() error { /* ... */ }
```

### Pattern 4: Database Migration for New Tables

**What:** New tables use `CREATE TABLE IF NOT EXISTS` in schema files + `PRAGMA user_version` migration for any ALTER operations.

**When:** Adding new persistent data.

**Example:** Smart playlists table in `backend/database/sql/schemas/smart_playlists.sql` with `CREATE TABLE IF NOT EXISTS`, plus Migration 6 in `database.go` for any column additions.

---

## Anti-Patterns to Avoid

### Anti-Pattern 1: Frontend-Owned State

**What:** Storing authoritative state in frontend stores rather than the backend.

**Why bad:** Violates the single-source-of-truth principle. State gets out of sync on refresh, loses persistence.

**Instead:** All state changes go through backend. Frontend stores are mirrors.

### Anti-Pattern 2: Direct DB Access from New Packages

**What:** New packages opening their own DB connections or using raw `sql.DB` directly.

**Why bad:** Violates single-writer constraint. Bypasses sqlc type safety.

**Instead:** All DB access goes through the shared `*database.DB` instance with sqlc-generated queries. Use `db.BeginTx()` for transactions. Only use raw queries for dynamic SQL (smart playlists), with `// SAFETY:` comments.

### Anti-Pattern 3: Circular Package Dependencies

**What:** `tageditor` importing `library` which imports `tageditor`.

**Why bad:** Go doesn't allow circular imports.

**Instead:** Use interface-based decoupling (like `TrackLoader` interface) or hook patterns (like `RescanHooks`). The tageditor can accept a `LibraryRefresher` interface rather than importing the library package.

### Anti-Pattern 4: Blocking the Wails Event Loop

**What:** Long-running operations in Wails binding methods without goroutines.

**Why bad:** Freezes the UI.

**Instead:** Long operations (tag writing, MB API calls, scan) run in goroutines and emit progress events. The binding method returns immediately or returns a "started" acknowledgement.

### Anti-Pattern 5: Uncontrolled HTTP Requests

**What:** MusicBrainz API calls without rate limiting.

**Why bad:** IP gets blocked. MusicBrainz enforces 1 req/sec strictly.

**Instead:** Single rate-limited HTTP client with `time.Ticker`. Cache all responses. Queue requests.

---

## Build Order (Dependency-Aware)

### Phase 1: Independent Foundations

These features have no inter-dependencies and can be built in any order:

1. **Scan Cancellation** — Smallest change. Modifies existing code minimally. Tests scan pipeline resilience.
2. **Customizable Keyboard Shortcuts** — Config + new package + frontend keydown listener. No data model changes.

### Phase 2: Data Model Extensions

These features add new database tables/queries:

3. **Tag Editing** — New dependency (`bogem/id3v2`), new DB queries, new package. Validates that tag write → DB update → event → frontend refresh pipeline works.
4. **Smart Playlists** — New table, new package, dynamic SQL. Independent of tag editing but benefits from validated DB migration patterns.

### Phase 3: Complex Backend Changes

5. **Gapless Playback** — Core audio pipeline modification. Start with gapless, add crossfade later. Most technically risky feature.
6. **MusicBrainz Browser** — First network feature. HTTP client, caching, rate limiting. Independent of other features.

### Phase 4: Extensibility Foundations

These are the "foundation" features — functional but not necessarily feature-complete:

7. **Layout Customization** — Requires all existing components to be working well. Modifies `index.html` structure.
8. **Plugin System** — Must be last — it depends on having a stable API surface from all other features.

### Rationale for This Order

- **Scan cancellation first** because it's a quick win that validates context cancellation patterns used throughout.
- **Shortcuts early** because they're simple config + dispatch with no data model changes.
- **Tag editing before smart playlists** because smart playlists query against track metadata that tag editing modifies — testing both together reveals integration issues.
- **Gapless after tag editing** because tag editing validates the "modify player behavior → event → frontend update" pipeline at a simpler level.
- **MusicBrainz after gapless** because it introduces network complexity that's orthogonal to audio — building it later keeps the audio work focused.
- **Layout and plugins last** because they're meta-features that wrap existing features. Building them last means the thing they're wrapping is stable.

---

## Wails Bridge Implications

### New FEBindings

Every new backend service added to `FEBindings` in `app.go` generates TypeScript stubs in `frontend/wailsjs/go/`. After adding new bindings:

```bash
make generate  # regenerates Wails bindings + sqlc + events codegen
```

### New Events (All Features)

Estimated new events across all features:

```go
// Tag editing
TagsEdited = "TagsEdited"
TagEditFailed = "TagEditFailed"

// Scan cancellation
LibraryScanCancelled = "LibraryScanCancelled"

// Smart playlists
SmartPlaylistCreated = "SmartPlaylistCreated"
SmartPlaylistUpdated = "SmartPlaylistUpdated"
SmartPlaylistDeleted = "SmartPlaylistDeleted"

// Shortcuts
ShortcutConfigChanged = "ShortcutConfigChanged"

// MusicBrainz
MusicBrainzSearchComplete = "MusicBrainzSearchComplete"

// Layout
LayoutConfigChanged = "LayoutConfigChanged"

// Gapless/Crossfade
CrossfadeConfigChanged = "CrossfadeConfigChanged"
```

All go through the existing AST-based codegen pipeline (`go generate` + pre-commit hook).

### Database Migrations

New migration sequence (current version = 5):

| Migration | Feature | What |
|-----------|---------|------|
| 6 | Smart Playlists | `CREATE TABLE smart_playlists` |
| 7 | MusicBrainz | `CREATE TABLE musicbrainz_cache` |
| 8 | Shortcuts | Config-based (no table needed) |
| 9 | Layout | Config-based (no table needed) |
| 10 | Plugins | `CREATE TABLE plugin_state` (optional, for persistent plugin data) |

Most features use config (TOML) rather than DB for their settings, keeping migrations minimal.

---

## Scalability Considerations

| Concern | Current (~1K tracks) | At 10K tracks | At 100K tracks |
|---------|---------------------|---------------|----------------|
| Smart playlist eval | <10ms | <100ms | May need indexing |
| Tag edit (single file) | ~50ms | ~50ms | ~50ms (file-level) |
| Tag edit (batch 100) | — | ~5s (serial writes) | Same |
| FTS5 re-index (tag edit) | ~1ms | ~1ms | ~1ms (single row) |
| MB API browse | Network-bound | Same | Same |
| Layout render | ~5ms | Same | Same |

The main scalability concern is **smart playlist evaluation** at large library sizes. The `track_metadata` VIEW already has a 5-table JOIN. Adding WHERE clauses for smart playlist rules adds no extra JOINs — the VIEW handles the complexity. SQLite's query planner should handle 100K rows with proper indexes.

---

## Sources

- Codebase analysis: Complete read of all Go packages and TypeScript sources (2026-03-06)
- beep v2.1.1 API: `pkg.go.dev/github.com/gopxl/beep/v2` — Mixer, Seq, Buffer, Ctrl types confirmed
- MusicBrainz API: `musicbrainz.org/doc/MusicBrainz_API` — rate limiting (1 req/sec), JSON format, entity types
- dhowden/tag: Read-only library confirmed from source (`tag.ReadFrom` only, no write methods)
- SQLite WAL mode + single writer: Existing `database.go` configuration confirmed
- Wails v2 binding generation: Existing `app.go` FEBindings pattern confirmed

---

*Architecture research: 2026-03-06*

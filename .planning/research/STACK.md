# Technology Stack Additions: v1.1 Features & Extensibility

**Project:** YellowJacket v1.1
**Researched:** 2026-03-06
**Overall confidence:** HIGH (tag writing, beep audio) / MEDIUM (MusicBrainz API, plugin architecture)

This document covers **only new libraries and patterns** needed for v1.1 features. The existing stack (Go 1.25, Wails v2.10.2, Lit 3.2.1, beep v2.1.1, modernc.org/sqlite, dhowden/tag, BurntSushi/toml, etc.) is validated and unchanged.

---

## Recommended Stack Additions

### 1. Tag Writing — MP3 (ID3v2)

| Technology | Version | Import Path | Purpose | Why |
|------------|---------|-------------|---------|-----|
| n10v/id3v2 | v2.1.4 | `github.com/n10v/id3v2/v2` | Read/write ID3v2.3 and v2.4 tags for MP3 files | The only maintained pure-Go library with full ID3v2 write support. 359 stars, 43 releases, active (last release Feb 2023, stable). dhowden/tag (existing) is read-only — it cannot write tags back. |

**Confidence:** HIGH — verified via GitHub repo, pkg.go.dev. The v2 module path uses the `/v2` subdirectory pattern (`github.com/n10v/id3v2/v2`).

**API surface used:**
```go
tag, err := id3v2.Open("file.mp3", id3v2.Options{Parse: true})
defer tag.Close()
tag.SetArtist("New Artist")
tag.SetTitle("New Title")
tag.SetAlbum("New Album")
tag.SetGenre("Electronic")
tag.SetYear("2024")
// Write back to file
err = tag.Save()
```

**Integration notes:**
- Operates on the file directly (open, modify, save). Does not need the existing beep pipeline.
- Must close the file before beep can play it — coordinate with player via a "stop playback → write tags → reload" flow.
- Keep `dhowden/tag` for reading (scan pipeline uses it). Use `n10v/id3v2` only for writing MP3 files.
- Thread safety: id3v2 file operations are not concurrent-safe. The tag editor backend service should serialize writes.

### 2. Tag Writing — FLAC (Vorbis Comments)

| Technology | Version | Import Path | Purpose | Why |
|------------|---------|-------------|---------|-----|
| go-flac/go-flac | v2.x | `github.com/go-flac/go-flac/v2` | Parse and reassemble FLAC file metadata blocks | Low-level FLAC metadata manipulation. 44 stars. Provides `ParseFile`, modify `Meta` slice, `Save`. |
| go-flac/flacvorbis | v2.x | `github.com/go-flac/flacvorbis/v2` | Read/write Vorbis comment metadata blocks within FLAC files | Companion to go-flac. Provides `ParseFromMetaDataBlock`, `Add`, `Set` for FLAC vorbis comments. 11 stars, but the only option in the Go ecosystem. |

**Confidence:** MEDIUM — both libraries are small and niche but are the standard Go solution for FLAC tag writing. v2 modules exist in `/v2` subdirectories.

**API surface used:**
```go
f, err := flac.ParseFile("file.flac")
// Find existing vorbis comment block
var cmt *flacvorbis.MetadataBlockVorbisComment
var cmtIdx int
for idx, meta := range f.Meta {
    if meta.Type == flac.VorbisComment {
        cmt, _ = flacvorbis.ParseFromMetaDataBlock(*meta)
        cmtIdx = idx
    }
}
if cmt == nil {
    cmt = flacvorbis.New()
}
cmt.Add(flacvorbis.FIELD_TITLE, []byte("New Title"))
cmt.Add(flacvorbis.FIELD_ARTIST, []byte("New Artist"))
cmtMeta := cmt.Marshal()
if cmtIdx > 0 {
    f.Meta[cmtIdx] = &cmtMeta
} else {
    f.Meta = append(f.Meta, &cmtMeta)
}
f.Save("file.flac")
```

**Integration notes:**
- go-flac reads the entire FLAC file into memory (metadata + audio frames). For large FLAC files (100MB+), this uses significant memory. The write operation is atomic (writes full file).
- Same coordination needed: stop playback → write → reload.

### 3. Tag Writing — OGG Vorbis and WAV

| Format | Approach | Why |
|--------|----------|-----|
| OGG Vorbis | Defer to v1.2 or use external tool | No mature pure-Go library exists for writing OGG Vorbis comments. The OGG container format makes in-place tag editing complex. Consider shelling out to `vorbiscomment` CLI tool if needed, or defer. |
| WAV | Not needed for v1.1 | WAV files rarely have meaningful tags (no standard tagging convention). INFO chunks exist but are rarely used in music libraries. |

**Confidence:** HIGH — exhaustive search found no viable pure-Go OGG Vorbis tag writer.

**Recommendation:** Implement tag editing for MP3 and FLAC first (covers ~95% of music libraries). Show "read-only" indicator for OGG/WAV files in the tag editor UI. Add OGG support later if demand exists.

### 4. MusicBrainz API Client

| Technology | Version | Import Path | Purpose | Why |
|------------|---------|-------------|---------|-----|
| **Direct HTTP + encoding/json** | stdlib | — | Query MusicBrainz REST API (JSON format) | Use Go's standard library rather than a third-party client. See rationale below. |

**Confidence:** HIGH — MusicBrainz API is well-documented REST/JSON. The API is simple enough that a custom thin client is better than available libraries.

**Why NOT use `michiwend/gomusicbrainz`:**
- Last meaningful commit was years ago, no Go modules support initially (added by community), uses XML parsing. 64 stars but effectively unmaintained.
- The library only supports search and lookup — no browse requests.
- MusicBrainz API supports JSON natively (`fmt=json` or `Accept: application/json`), making XML parsing unnecessary.

**Why NOT use `go.uploadedlobster.com/musicbrainzws2`:**
- Hosted on SourceHut, harder to verify maintenance status.
- Low adoption (not visible on GitHub).

**Custom client approach (recommended):**
```go
// backend/musicbrainz/client.go
package musicbrainz

type Client struct {
    httpClient *http.Client
    baseURL    string
    userAgent  string
    rateLimiter *time.Ticker  // MusicBrainz requires max 1 req/sec
}

func NewClient(appName, appVersion, contactURL string) *Client {
    return &Client{
        httpClient: &http.Client{Timeout: 10 * time.Second},
        baseURL:    "https://musicbrainz.org/ws/2",
        userAgent:  fmt.Sprintf("%s/%s (%s)", appName, appVersion, contactURL),
        rateLimiter: time.NewTicker(time.Second), // 1 request per second
    }
}
```

**MusicBrainz API integration points:**
- **Rate limiting:** MANDATORY — max 1 request per second. Use `time.Ticker` with channel-based throttling.
- **User-Agent:** MANDATORY — must include app name, version, and contact URL. MusicBrainz blocks requests without meaningful user-agents.
- **Endpoints needed for read-only browser:**
  - `GET /ws/2/artist/<MBID>?inc=release-groups&fmt=json` — Artist lookup with discography
  - `GET /ws/2/release-group/<MBID>?inc=releases&fmt=json` — Album editions
  - `GET /ws/2/release/<MBID>?inc=recordings+media&fmt=json` — Track listings
  - `GET /ws/2/artist?query=<QUERY>&fmt=json` — Artist search
  - `GET /ws/2/release-group?query=<QUERY>&fmt=json` — Album search
- **Response caching:** Cache API responses in SQLite with TTL (e.g., 7 days). MusicBrainz data is slow-changing. Reduces API calls and improves UI responsiveness.
- **No authentication needed:** Read-only lookups and searches are unauthenticated.

### 5. Gapless Playback + Crossfade

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| **beep.Mixer** | v2.1.1 (existing) | Mix two streams for crossfade | Already in the dependency tree. `beep.Mixer` dynamically adds/removes streamers and mixes them. `KeepAlive(true)` keeps it playing silence when no streamers are active. |
| **beep.Seq** | v2.1.1 (existing) | Chain streams for gapless | Already used in `startPaused()`. `beep.Seq(s1, s2)` plays s1 then s2 without gap. |
| **effects.Volume** | v2.1.1 (existing) | Per-stream volume for fade curves | Already used for main volume. Create separate Volume wrappers for fade-in/fade-out. |

**Confidence:** HIGH — all primitives already exist in beep v2.1.1.

**No new dependencies needed.** Gapless and crossfade are implemented by changing how the streamer chain is composed, not by adding new libraries.

**Gapless architecture:**
```go
// Instead of: speaker.Play(beep.Seq(currentStream, beep.Callback(onFinished)))
// Use: pre-decode next track and Seq them together.

// When current track nears end (e.g., 2 seconds remaining):
nextStreamer, nextFormat := decodeNextTrack()
resampled := beep.Resample(4, nextFormat.SampleRate, speakerSampleRate, nextStreamer)
// The beep.Seq already playing will seamlessly transition to the next stream.
```

**Crossfade architecture:**
```go
// Use a Mixer as the root streamer instead of a single chain.
type CrossfadeMixer struct {
    mixer     beep.Mixer
    fadeInMs  int
    fadeOutMs int
}

// When transitioning:
// 1. Create fade-out volume wrapper on current stream
// 2. Create fade-in volume wrapper on next stream  
// 3. Add both to mixer
// 4. Use beep.StreamerFunc to drive the volume ramps over time
```

**Key integration changes:**
- The `Player` struct currently uses `speaker.Play(beep.Seq(...))` for single-stream playback. For gapless/crossfade, switch to a persistent `beep.Mixer` registered with the speaker once at init.
- Add/remove streams from the mixer rather than calling `speaker.Play()` per track.
- The `beep.Callback` for end-of-track still works but fires per-stream in the mixer, not per-speaker-play.
- Pre-decoding the next track requires knowing what the next track IS. This means the player needs awareness of the queue (currently it only knows about the current file). Wire this via a "next track provider" interface.

### 6. Plugin System Architecture

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| **Go `plugin` package** | stdlib | ❌ NOT recommended | Linux-only, same Go version required, fragile. |
| **hashicorp/go-plugin** | — | ❌ NOT recommended | gRPC-based, heavy for a desktop app, designed for server-side tools. |
| **Custom interface + registration** | — | ✅ Recommended | Define Go interfaces for backend hooks. Plugins implement interfaces and register at init. |

**Confidence:** MEDIUM — plugin architecture is inherently design-specific. No off-the-shelf solution fits perfectly.

**Recommended approach: Compiled-in plugin system with runtime-loaded UI**

**Backend plugins (Go):**
```go
// backend/plugin/api.go
package plugin

// Plugin is the interface all backend plugins must implement.
type Plugin interface {
    ID() string
    Name() string
    Version() string
    Init(ctx PluginContext) error
    Shutdown() error
}

// PluginContext provides access to app services.
type PluginContext struct {
    DB        *database.DB
    Events    EventEmitter
    Config    ConfigAccess
    Logger    *slog.Logger
}

// Hook interfaces — plugins implement the ones they care about.
type OnTrackChangeHook interface {
    OnTrackChange(track TrackInfo) error
}
type OnScanCompleteHook interface {
    OnScanComplete(metrics ScanMetrics) error
}
```

For v1.1, backend plugins are compiled into the binary (via Go build tags or registration in main.go). True dynamic loading can come later via process-based plugins (subprocess + JSON-RPC).

**Frontend plugins (TypeScript/Lit):**
- Plugins provide Lit web components that register themselves via `customElements.define()`.
- The layout system (see below) allows placing plugin components in UI sections.
- Plugin JS bundles are loaded at runtime from a plugins directory via dynamic `import()`.

**No new Go dependencies needed** for the initial plugin system. The complexity is in API design, not in libraries.

### 7. Layout Customization System

| Technology | Purpose | Why |
|------------|---------|-----|
| **Existing: Lit + config.toml** | Section-based layout config | The existing TOML config system and Lit component architecture are sufficient. No new dependencies needed. |

**Confidence:** HIGH — this is a UI architecture problem, not a library problem.

**Architecture:**
```toml
# config.toml additions:
[Layout]
  [Layout.Sidebar]
  components = ["navigation", "now-playing-art"]
  width = 250
  
  [Layout.MainPanel]
  components = ["track-list"]
  
  [Layout.BottomBar]
  components = ["audio-player", "queue-mini"]
```

**Frontend implementation:**
```typescript
// A layout-section component that renders configured child components
@customElement('layout-section')
class LayoutSection extends LitElement {
    @property() section: string = '';
    @property({ type: Array }) components: string[] = [];
    
    render() {
        return html`${this.components.map(name => {
            const tag = document.createElement(name);
            return tag;
        })}`;
    }
}
```

**Component registry pattern:**
```typescript
// Each component declares its constraints
interface LayoutComponent {
    tagName: string;
    displayName: string;
    minWidth?: number;
    minHeight?: number;
    allowedSections: string[];
}

const registry = new Map<string, LayoutComponent>();
```

**No new npm dependencies needed.** Lit's `customElements.define()` provides the dynamic component loading mechanism. The config system already handles TOML persistence and live reload.

### 8. Smart Playlists

| Technology | Purpose | Why |
|------------|---------|-----|
| **Existing: SQLite + sqlc** | Dynamic query builder for filter rules | Smart playlists are SQL WHERE clauses stored as structured data. No new dependencies needed. |

**Confidence:** HIGH — smart playlists are a database query problem.

**Architecture:**
```go
// Smart playlist rule stored in SQLite
type SmartPlaylistRule struct {
    Field    string // "genre", "year", "artist", "play_count", "date_added"
    Operator string // "equals", "contains", "greater_than", "less_than", "between"
    Value    string // The comparison value(s)
}

type SmartPlaylist struct {
    ID        int64
    Name      string
    Rules     []SmartPlaylistRule // Stored as JSON in SQLite
    MatchAll  bool                // AND vs OR for combining rules
    SortBy    string
    SortOrder string
    Limit     int                 // 0 = unlimited
}
```

**Query generation (not sqlc — dynamic WHERE clauses):**
```go
// Hand-crafted SQL builder for smart playlists.
// Cannot use sqlc because the WHERE clause is dynamic.
func (sp *SmartPlaylist) BuildQuery() (string, []any) {
    // Build parameterized query from rules.
    // Always use parameterized queries — never interpolate values.
}
```

**Schema addition:** New `smart_playlists` table with JSON rules column. New migration in the existing `PRAGMA user_version` system.

**No new dependencies needed.** The existing `encoding/json` handles rule serialization.

### 9. Customizable Keyboard Shortcuts

| Technology | Purpose | Why |
|------------|---------|-----|
| **Existing: Wails runtime + config.toml + Lit** | Frontend keyboard event handling with configurable bindings | Keyboard shortcuts are a frontend concern in WebView. No new dependencies. |

**Confidence:** HIGH — standard web keyboard event handling.

**Architecture:**
```toml
# config.toml additions:
[KeyboardShortcuts]
play_pause = "Space"
next_track = "MediaTrackNext"
prev_track = "MediaTrackPrevious"
volume_up = "ArrowUp"
volume_down = "ArrowDown"
seek_forward = "ArrowRight"
seek_backward = "ArrowLeft"
toggle_queue = "Q"
search = "Ctrl+F"
```

**Frontend implementation:**
```typescript
// Global keyboard handler — listens on document, maps keys to actions
class KeyboardShortcutManager {
    private bindings: Map<string, string>; // key combo → action name
    private actions: Map<string, () => void>; // action name → handler
    
    handleKeyDown(e: KeyboardEvent) {
        const combo = this.normalizeCombo(e);
        const action = this.bindings.get(combo);
        if (action) {
            e.preventDefault();
            this.actions.get(action)?.();
        }
    }
}
```

**No new dependencies needed.** The Web platform's `KeyboardEvent` API provides everything. Store bindings in TOML config, load on startup, emit config change events on update.

### 10. Scan Cancellation

| Technology | Purpose | Why |
|------------|---------|-----|
| **Existing: `context.WithCancel`** | Cancel in-progress library scan | Go's context cancellation is the standard pattern. The scan pipeline already uses `errgroup` which respects context cancellation. |

**Confidence:** HIGH — standard Go pattern.

**Implementation:**
```go
// In Library struct:
type Library struct {
    scanCancel context.CancelFunc  // nil when no scan is running
    // ...
}

func (l *Library) Scan() {
    ctx, cancel := context.WithCancel(l.ctx)
    l.scanCancel = cancel
    defer func() { l.scanCancel = nil }()
    
    // Pass ctx to errgroup and all scan phases
    g, gctx := errgroup.WithContext(ctx)
    // Workers check gctx.Done() and exit early
}

func (l *Library) CancelScan() {
    if l.scanCancel != nil {
        l.scanCancel()
    }
}
```

**No new dependencies needed.** The existing `golang.org/x/sync/errgroup` already propagates context cancellation to worker goroutines.

---

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| MP3 tag writing | n10v/id3v2 v2 | bogem/id3v2 (old path) | Same library — `n10v/id3v2` is the current canonical path after maintainer rename |
| FLAC tag writing | go-flac/go-flac + flacvorbis | mewkiz/flac | mewkiz/flac is a decoder/encoder, not a metadata editor. Would require full re-encode to change tags. |
| MusicBrainz client | Custom HTTP client | michiwend/gomusicbrainz | Unmaintained, XML-only, missing browse API, no Go modules initially |
| MusicBrainz client | Custom HTTP client | go-musicbrainzws2 (SourceHut) | Low adoption, hard to verify maintenance, adds unfamiliar dependency |
| Gapless/crossfade | beep.Mixer (existing) | External audio library | beep already provides all needed primitives (Mixer, Seq, Volume, Resample) |
| Plugin system | Interface-based registration | hashicorp/go-plugin | gRPC overhead is inappropriate for a desktop app; designed for distributed systems |
| Plugin system | Interface-based registration | Go `plugin` package | Linux-only, same Go version required, CGo required for loading, extremely fragile |
| Plugin system | Interface-based registration | Wasm runtime (wazero) | Massive complexity for v1.1; good future option for sandboxed plugins |
| Smart playlists | Dynamic SQL builder | SQLite views | Views can't be parameterized at query time; rules need runtime evaluation |
| Keyboard shortcuts | Web KeyboardEvent API | Frontend hotkey library | No library needed for the scope of shortcuts in a music player |

---

## What NOT to Add

These are things the existing stack already handles. Do NOT add duplicate libraries:

| Capability | Already Handled By | DON'T Add |
|-----------|-------------------|-----------|
| Tag reading | `github.com/dhowden/tag` | Any other tag reading library — keep dhowden/tag for the scan pipeline |
| Audio decoding | `gopxl/beep/v2` (mp3, flac, vorbis, wav) | Any other audio decoder |
| Config persistence | `BurntSushi/toml` | YAML, JSON, or any other config library |
| Database | `modernc.org/sqlite` | Any other database or ORM |
| HTTP client | Go stdlib `net/http` | Any HTTP client library for MusicBrainz |
| JSON parsing | Go stdlib `encoding/json` | Any JSON library for MusicBrainz responses |
| Concurrency | Go stdlib `context`, `sync`, `golang.org/x/sync` | Any additional concurrency primitives |
| Frontend reactivity | Lit 3.2.1 + @lit-labs/signals | Any state management library |
| Virtual scrolling | @lit-labs/virtualizer | Any other virtual scrolling solution |

---

## Installation

```bash
# New Go dependencies (tag writing + FLAC metadata):
go get github.com/n10v/id3v2/v2@v2.1.4
go get github.com/go-flac/go-flac/v2
go get github.com/go-flac/flacvorbis/v2

# No new frontend (npm) dependencies needed for v1.1.
# All features use existing Lit + Web platform APIs.
```

**Total new dependencies: 3 Go packages, 0 npm packages.**

This is intentionally minimal. The v1.1 features are primarily architecture and design challenges, not library selection challenges. The existing stack is comprehensive enough that most features require new code, not new dependencies.

---

## Integration Points with Existing Stack

### Tag Editing → Player Coordination
The player holds an open file handle (`p.currentFile`) during playback. Tag writing libraries also need exclusive file access. The workflow must be:
1. Player.Pause() or Player.Stop() — release the file
2. Write tags via id3v2/go-flac
3. Rescan the file's metadata into the database
4. Player.LoadFile() with the same path — resume

### MusicBrainz → Database Caching
MusicBrainz API responses should be cached in SQLite (new tables: `mb_cache_artists`, `mb_cache_releases`, etc.) with a TTL column. This reuses the existing database infrastructure and avoids redundant API calls. The 1-request-per-second rate limit makes caching essential for a responsive UI.

### Gapless/Crossfade → Speaker Architecture
Current: `speaker.Play()` called per track, creates new beep.Seq each time.
New: Register a persistent `beep.Mixer` with the speaker once at init. Add/remove per-track streamers to the mixer. This is the biggest architectural change — it affects Player, Queue auto-advance, and the playback-finished callback chain.

### Smart Playlists → Existing Query Infrastructure
Smart playlists generate SQL queries against the existing `track_metadata` VIEW and related tables. They use the same `*database.DB` connection with the same `SetMaxOpenConns(1)` constraint. Rules are stored as JSON in a new `smart_playlists` table (schema migration via existing `PRAGMA user_version` system).

### Layout Customization → Config + Frontend
New `[Layout]` section in config.toml, loaded via existing `BurntSushi/toml` config system. Layout changes emit config change events via existing Wails event bus. Frontend components register themselves in a component registry and the layout section components render them dynamically.

### Plugin System → Everything
Backend plugins get a `PluginContext` with access to DB, events, config, logger. Frontend plugins load as JS modules via `import()` and register Lit web components. Both hook into the existing architecture rather than requiring new infrastructure.

---

## Sources

- n10v/id3v2: https://github.com/n10v/id3v2 — **HIGH confidence** (verified GitHub repo, 359 stars, 43 releases, MIT license)
- go-flac/go-flac: https://github.com/go-flac/go-flac — **MEDIUM confidence** (verified, 44 stars, v2 module available, Apache-2.0 license)
- go-flac/flacvorbis: https://github.com/go-flac/flacvorbis — **MEDIUM confidence** (verified, 11 stars, v2 module available, Apache-2.0 license)
- MusicBrainz API: https://musicbrainz.org/doc/MusicBrainz_API — **HIGH confidence** (official documentation, comprehensive)
- MusicBrainz rate limiting: https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting — **HIGH confidence** (official)
- beep v2.1.1 API: https://pkg.go.dev/github.com/gopxl/beep/v2 — **HIGH confidence** (official Go package docs, verified Mixer, Seq, Volume types)
- beep Mixer documentation: verified from pkg.go.dev — Add(), Clear(), KeepAlive(), Stream() methods confirmed
- michiwend/gomusicbrainz: https://github.com/michiwend/gomusicbrainz — **HIGH confidence** (verified, 64 stars, only search+lookup, no modules, effectively unmaintained)
- Go plugin package limitations: https://pkg.go.dev/plugin — **HIGH confidence** (official docs, Linux+macOS only, same Go version requirement documented)

---

*Stack research for: YellowJacket v1.1 Features & Extensibility*
*Researched: 2026-03-06*

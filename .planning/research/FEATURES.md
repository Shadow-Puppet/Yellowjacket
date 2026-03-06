# Feature Landscape: v1.1 Features & Extensibility

**Domain:** Desktop music player — new capabilities milestone
**Researched:** 2026-03-06
**Confidence:** HIGH (grounded in codebase analysis, official documentation, established desktop music player patterns)

---

## Overview

This research covers 8 feature areas for YellowJacket v1.1: tag editing, scan cancellation, smart playlists, customizable keyboard shortcuts, gapless playback + crossfade, MusicBrainz browser, layout customization, and plugin system. Each is categorized as table stakes, differentiator, or anti-feature relative to the desktop music player domain.

---

## 1. Tag Editing

### Table Stakes

| Feature | Why Expected | Complexity | Dependencies |
|---------|--------------|------------|--------------|
| Edit title, artist, album, genre, year, track number | Every music manager (MusicBee, foobar2000, Clementine, Strawberry) supports this. Users expect to correct metadata without leaving the app. | MEDIUM | Existing metadata extraction pipeline, new tag writing libraries |
| Edit single track | Right-click → edit properties is the universal pattern | LOW | Tag writing backend |
| Batch edit multiple tracks | Select multiple → edit shared fields (e.g., set all to same album). This is the primary workflow for fixing album imports. | MEDIUM | Single-track editing must work first |
| Write changes to actual audio files | Tags must persist to the file on disk, not just the DB. Users expect changes to survive re-imports and transfers to other players. | MEDIUM | Tag writing libraries (format-specific) |
| Update DB after tag write | After writing tags to file, the DB must reflect the new metadata without requiring a full rescan. | LOW | Existing DB update queries |
| Cover art assignment | Set/replace embedded cover art from an image file | MEDIUM | Image handling + tag writing |

### Differentiators

| Feature | Value Proposition | Complexity | Dependencies |
|---------|-------------------|------------|--------------|
| Undo/redo for tag edits | Safety net — rare in music players, very valued when present | HIGH | Requires edit history tracking |
| Auto-capitalize/clean tag values | Consistent library appearance with minimal effort | LOW | String utilities |
| Filename-to-tag inference | Parse "Artist - Title.mp3" patterns to pre-fill fields | MEDIUM | Regex/pattern engine |
| Tag-to-filename rename | Rename files based on tag template (e.g., "%artist% - %title%.%ext%") | HIGH | File system operations, template engine |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Auto-tag from online DB in tag editor | Conflates two features — tag editing and metadata lookup. MusicBrainz browser is the separate feature for this. | Keep tag editing purely manual; MusicBrainz browser is the lookup tool |
| Destructive batch operations without confirmation | Mass edits can corrupt a library. | Always show preview/confirmation dialog for batch edits |
| Writing tags during playback of that file | File locking conflicts on Windows; potential corruption on any OS | Queue the write for after playback stops, or copy-on-write |

### Implementation Notes

**Tag writing requires format-specific libraries (the existing `dhowden/tag` is read-only):**

- **MP3 (ID3v2):** `github.com/bogem/id3v2/v2` — mature, pure Go, supports ID3v2.3/2.4 read+write, handles text frames, pictures, comments. Confirmed: `tag.Open()` → `tag.SetArtist()` → `tag.Save()` pattern. v2.1.4 is current.
- **FLAC (Vorbis Comments):** `github.com/go-flac/go-flac` + `github.com/go-flac/flacvorbis` — parse FLAC file, modify vorbis comment metadata blocks, save back. Confirmed: `flac.ParseFile()` → modify `Meta` slice → `f.Save()`. v1.0.0/v0.2.0 current (v2 exists).
- **OGG Vorbis:** No mature pure-Go write library exists. Options: (a) skip OGG tag writing initially, (b) use `go-flac/flacvorbis`-style approach with raw vorbis comment manipulation if a library surfaces, or (c) shell out to `vorbiscomment` CLI tool.
- **WAV:** WAV metadata (INFO chunks, ID3 headers) is rarely edited. Skip for v1.1.

**Critical constraint:** The existing `dhowden/tag` library is read-only. Tag writing is a completely separate code path requiring new dependencies. Tag reading continues through `dhowden/tag`; writing uses format-specific libraries.

**DB sync pattern:** After writing tags to file, update the specific DB rows rather than triggering a full rescan. Extract the new metadata from the written file (or trust the values just written), update the `recordings`, `artists`, `release_groups`, and `audio_files` tables, then emit a `TrackMetadataChanged` event to sync the frontend.

---

## 2. Scan Cancellation

### Table Stakes

| Feature | Why Expected | Complexity | Dependencies |
|---------|--------------|------------|--------------|
| Cancel button during scan | Large libraries take minutes to scan. Users expect to be able to stop a scan in progress. Every file manager and media player with scanning provides this. | LOW | Existing scan pipeline with `context.Context` |
| Graceful stop (don't corrupt DB) | Cancellation must not leave the DB in an inconsistent state. Complete in-flight transactions, skip remaining files. | LOW | Existing transaction batching |
| Scan progress reporting | Users need to see what's happening — "Processing 340/2000 files" — to decide whether to wait or cancel. | LOW | Existing `ScanProgress` event (already partially implemented) |

### Differentiators

| Feature | Value Proposition | Complexity | Dependencies |
|---------|-------------------|------------|--------------|
| Pause and resume scan | Stop temporarily, resume later without re-scanning already-processed files | HIGH | Would need scan state persistence |
| Background scan with low priority | Scan without impacting playback or UI responsiveness | LOW | Already partially handled by worker pool concurrency tuning |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Immediate hard kill (kill goroutines) | Data corruption risk — partial writes, broken entity caches | Use context cancellation for cooperative shutdown |
| Auto-cancel on any error | Users want the scan to continue past individual file failures | Continue scanning, accumulate warnings (already the pattern) |

### Implementation Notes

**The existing scan pipeline already uses `context.Context` — `l.ctx` is available throughout the scan.** The implementation pattern is straightforward:

1. Create a cancellable context: `scanCtx, cancelScan := context.WithCancel(l.ctx)`
2. Store `cancelScan` so the frontend can trigger it via a Wails binding (e.g., `Library.CancelScan()`)
3. Check `scanCtx.Done()` in the filesystem walk loop, the worker pool dispatch, and the DB writer
4. On cancellation, the `errgroup` returns `context.Canceled`, which is caught and treated as a clean stop
5. Emit `LibraryScanCancelled` event (distinct from `LibraryScanComplete`)

**Key insight:** The existing scan already uses `errgroup` which respects context cancellation. The DB writer goroutine processes whatever is in its batch channel, so in-flight batches complete cleanly. The only new code needed is: (a) storing/exposing the cancel function, (b) checking context in the walk loop, (c) a new event for cancellation.

**Complexity is LOW** because the architecture already supports this pattern. The scan pipeline's multi-phase design means cancellation at any phase is naturally bounded.

---

## 3. Smart Playlists

### Table Stakes

| Feature | Why Expected | Complexity | Dependencies |
|---------|--------------|------------|--------------|
| Filter by genre | "All Jazz tracks" — the most basic smart playlist rule | LOW | Existing genre data in DB |
| Filter by year/year range | "Tracks from 1990-1999" | LOW | Existing year field in DB |
| Filter by artist | "All tracks by Artist X" | LOW | Existing artist data |
| Combine multiple rules (AND) | "Jazz tracks from the 1990s" — users expect to stack filters | MEDIUM | Rule evaluation engine |
| Auto-update when library changes | Smart playlists should refresh when tracks are added/removed. This is the defining feature vs. manual playlists. | MEDIUM | Event subscription to library changes |
| Name and save smart playlists | Persist rule definitions, show in sidebar alongside regular playlists | LOW | New DB table for rule definitions |

### Differentiators

| Feature | Value Proposition | Complexity | Dependencies |
|---------|-------------------|------------|--------------|
| Filter by play count | "Most played" / "Never played" — requires play count tracking (not currently implemented) | MEDIUM | New `play_count` column or table |
| Filter by date added | "Recently added" — very popular smart playlist | LOW | Existing file modification time or new `added_at` column |
| Filter by rating | Requires rating system (not currently implemented) | MEDIUM | New rating feature |
| OR logic and nested groups | "(Genre=Jazz OR Genre=Blues) AND Year>1980" — powerful but complex UI | HIGH | Recursive rule evaluation, complex UI builder |
| Random/limit results | "Random 50 Jazz tracks" — playlist-as-radio | LOW | SQL `ORDER BY RANDOM() LIMIT N` |
| Sort order in rules | "Newest first" / "Alphabetical by artist" | LOW | SQL `ORDER BY` clause |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Full SQL WHERE clause as input | Exposes DB internals, injection risk, terrible UX | Structured rule builder with defined fields and operators |
| Complex nested boolean logic in v1 | Overwhelms users, complex UI, rarely used | Start with flat AND rules; add OR/nesting later if demanded |
| Real-time updating during playback | Unnecessary overhead — smart playlists don't need sub-second freshness | Refresh on library scan completion and on explicit refresh |

### Implementation Notes

**Rule model — keep it simple for v1:**

```
SmartPlaylistRule {
  Field:    "genre" | "year" | "artist" | "album" | "title" | "date_added"
  Operator: "equals" | "not_equals" | "contains" | "greater_than" | "less_than" | "between"
  Value:    string (or string pair for "between")
}

SmartPlaylist {
  ID:        int64
  Name:      string
  Rules:     []SmartPlaylistRule  // all ANDed together
  SortField: string (optional)
  SortOrder: "asc" | "desc"
  Limit:     int (0 = unlimited)
}
```

**Storage:** New `smart_playlists` table (id, name, rules_json, sort_field, sort_order, limit_count) with rules stored as JSON in a TEXT column. This avoids a complex relational schema for rules and is trivially extensible.

**Query generation:** Each rule maps to a SQL WHERE clause fragment. Rules are joined with AND. The existing `track_metadata` VIEW provides all the needed columns for filtering. Generated SQL uses parameterized queries (NOT string concatenation) to prevent injection.

**Refresh strategy:** Smart playlists evaluate lazily — results are computed on access and cached. Cache is invalidated on `LibraryScanComplete` events. This avoids expensive re-evaluation on every library change.

**Depends on:** Existing `track_metadata` VIEW, playlist sidebar UI, event system.

---

## 4. Customizable Keyboard Shortcuts

### Table Stakes

| Feature | Why Expected | Complexity | Dependencies |
|---------|--------------|------------|--------------|
| Play/pause hotkey | Space bar is universal; must work | LOW | Existing player controls |
| Next/previous track | Arrow keys or media key equivalents | LOW | Existing queue navigation |
| Volume up/down | Standard audio app functionality | LOW | Existing volume control |
| Mute toggle | Expected in any audio application | LOW | Existing mute functionality |
| Search focus | Ctrl+F or / to focus search — standard in any list-heavy app | LOW | Existing search bar |
| Default keybindings that work out of box | Users shouldn't have to configure anything to get basic shortcuts | LOW | Hardcoded defaults with override capability |

### Differentiators

| Feature | Value Proposition | Complexity | Dependencies |
|---------|-------------------|------------|--------------|
| Full customization UI | Visual keybinding editor with conflict detection | MEDIUM | Settings page extension |
| Import/export keybindings | Share/backup custom configs | LOW | TOML serialization (already used for config) |
| Scoped shortcuts (global vs. component-specific) | Different bindings when focus is in search vs. track list | MEDIUM | Focus tracking |
| "When focused" context awareness | Arrows navigate track list when it's focused, but control volume when player is focused | MEDIUM | Component focus management |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Global OS-level hotkeys (outside app window) | Platform-specific, conflicts with OS shortcuts, security concerns on Wayland | App-scoped shortcuts only; MPRIS2 handles media keys |
| Vim-mode or complex modal keybindings | Niche appeal, confusing for 99% of users | Simple single/modifier key combos (Ctrl+X, Shift+X) |
| Shortcut for every possible action | Overwhelming configuration UI | Cover the 10-15 most common actions; rest accessible via menus |

### Implementation Notes

**Architecture — event-driven, backend-aware:**

The shortcut system has two layers:
1. **Frontend key listener:** Captures keyboard events at the document level, maps keystrokes to action names using a binding table
2. **Action dispatch:** Frontend calls the appropriate Wails binding or emits a frontend event for UI-only actions

**Binding table structure:**

```typescript
interface KeyBinding {
  action: string;        // "play_pause", "next_track", "volume_up", etc.
  key: string;           // "Space", "ArrowRight", etc. (KeyboardEvent.key)
  modifiers: string[];   // ["ctrl"], ["shift"], ["ctrl", "shift"], []
  scope?: string;        // "global" | "tracklist" | "queue" (optional, default "global")
}
```

**Storage:** Add `[Shortcuts]` section to TOML config. Default bindings are hardcoded; user overrides merge on top. Config change emits `ShortcutConfigChanged` event.

**Conflict detection:** When user changes a binding, check for conflicts within the same scope. Show warning if two actions share the same keystroke.

**Default bindings (the 12 essentials):**

| Action | Default Key | Scope |
|--------|-------------|-------|
| Play/Pause | Space | global |
| Stop | . (period) | global |
| Next Track | Ctrl+Right | global |
| Previous Track | Ctrl+Left | global |
| Volume Up | Ctrl+Up | global |
| Volume Down | Ctrl+Down | global |
| Mute | M | global |
| Search Focus | Ctrl+F | global |
| Toggle Queue | Q | global |
| Toggle Shuffle | S | global |
| Toggle Repeat | R | global |
| Select All (track list) | Ctrl+A | tracklist |

**Key insight:** Keyboard shortcuts must NOT interfere with text input. When a text input or textarea has focus, the shortcut system must be disabled (except for Escape to blur). This is the #1 pitfall in keyboard shortcut implementations.

---

## 5. Gapless Playback + Crossfade

### Table Stakes

| Feature | Why Expected | Complexity | Dependencies |
|---------|--------------|------------|--------------|
| Gapless playback (no silence between tracks) | Expected by any serious music listener. Albums are meant to flow. Strawberry, foobar2000, Deadbeef, Audacious all support this. | HIGH | Fundamental change to audio pipeline |
| Crossfade setting (on/off, duration) | Standard feature in every modern music player. Even basic mobile players have this. | MEDIUM | Gapless infrastructure + mixer |
| Crossfade duration control | Users expect 1-10 second configurable fade | LOW | UI slider + config storage |
| Gapless without crossfade (default) | Pure gapless (no overlap) should be the default. Crossfade is opt-in. | HIGH | Pre-decode/buffer next track |

### Differentiators

| Feature | Value Proposition | Complexity | Dependencies |
|---------|-------------------|------------|--------------|
| Per-album gapless (auto-detect live albums) | Disable crossfade within albums, enable between albums | MEDIUM | Album boundary detection in queue |
| ReplayGain normalization | Consistent volume across tracks from different sources | HIGH | ReplayGain tag parsing + volume adjustment |
| Fade-in on play, fade-out on pause | Smoother start/stop experience | LOW | Volume envelope on play/pause |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| DSP effects chain (equalizer, reverb, etc.) | Scope explosion — not part of gapless/crossfade | Defer to plugin system if ever needed |
| Crossfade for all transitions (including manual skip) | Crossfade on skip feels sluggish | Only crossfade on auto-advance; manual skip is instant |
| Pre-loading entire tracks into memory | Memory explosion with FLAC files (50-100MB per track) | Buffer only the crossfade overlap region (last/first N seconds) |

### Implementation Notes

**This is the highest-complexity feature in v1.1.** The current audio pipeline plays one track at a time with a single streamer chain. Gapless playback requires pre-decoding the next track and seamlessly transitioning.

**Current pipeline:** `file → decode → resample → BufferedStreamer → Ctrl → Volume → Speaker`

**Gapless pipeline (conceptual):**
1. When current track is N seconds from ending, pre-load next track's decoder + resampler
2. For pure gapless: use `beep.Seq()` to chain current and next streamer — but Seq doesn't support the pre-decode timing
3. For crossfade: use `beep.Mixer` to overlap the fade-out of current with fade-in of next

**beep library support:**
- `beep.Mixer` — adds/mixes multiple streamers. This is the foundation for crossfade.
- `beep.Seq()` — sequences streamers end-to-end. Foundation for gapless without crossfade.
- `effects.Volume` — volume control already used; can create fade curves by adjusting volume over time.
- `beep.Take()` — extract N samples from a streamer. Useful for defining crossfade regions.

**Architecture change required:**
- The `Player` must manage TWO streamer chains simultaneously during crossfade
- A `TransitionManager` or equivalent coordinates pre-loading the next track
- The `playbackFinishedHandler` (callback from beep when track ends) must trigger next-track pre-loading rather than waiting for the callback
- The `Queue` must expose a "peek next" capability (already has `tracks` and `currentIndex`)

**Crossfade implementation sketch:**
```
[Track A ~~~~~~~~ fade-out]
                [fade-in ~~~~~~~~ Track B]
                |-- overlap (N seconds) --|
```
- Track A's volume ramps from 1.0 → 0.0 over N seconds
- Track B's volume ramps from 0.0 → 1.0 over N seconds
- Both feed into a `beep.Mixer` during the overlap period
- After overlap, Track A is closed, Track B continues alone

**Config addition:** `[Playback]` section with `GaplessEnabled` (bool, default true), `CrossfadeEnabled` (bool, default false), `CrossfadeDurationMs` (int, default 3000, range 500-10000).

**Critical constraint:** The beep `speaker.Play()` can only be called once; the speaker's mixer is the root. All track management must happen within the streamer chain that the speaker is already playing. This means using a persistent `beep.Mixer` as the root streamer, adding/removing track streamers from it.

---

## 6. MusicBrainz Browser

### Table Stakes

| Feature | Why Expected | Complexity | Dependencies |
|---------|--------------|------------|--------------|
| Search artists by name | The entry point — user types artist name, gets results | MEDIUM | MusicBrainz API integration, HTTP client |
| View artist discography (release groups) | Browse albums/EPs/singles by an artist | MEDIUM | API browse: release-groups by artist |
| View album track listing | See what tracks are on a release | MEDIUM | API lookup: release with recordings |
| View album editions (releases within a release group) | Different pressings, reissues, deluxe editions | MEDIUM | API browse: releases by release-group |
| Rate limiting compliance | MusicBrainz requires max 1 request/second with meaningful User-Agent | LOW | HTTP rate limiter, User-Agent header |
| Offline-safe (read-only, no writes) | Read-only browsing — no MusicBrainz account needed | LOW | No authentication required for reads |

### Differentiators

| Feature | Value Proposition | Complexity | Dependencies |
|---------|-------------------|------------|--------------|
| Link local tracks to MusicBrainz recordings | Associate library tracks with MBIDs for definitive identity | HIGH | Matching algorithm, DB schema changes |
| Show cover art from Cover Art Archive | Display album art from MusicBrainz's linked image archive | MEDIUM | coverartarchive.org API |
| Cache API responses locally | Avoid re-fetching on every browse session | MEDIUM | SQLite cache table with TTL |
| Search recordings | Find specific songs across all releases | LOW | MusicBrainz recording search API |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Auto-tag from MusicBrainz | This is Picard's domain — extremely complex matching logic | Read-only browsing only. Users can manually apply info from browse to tag editor. |
| Write data to MusicBrainz | Requires OAuth, community guidelines compliance, edit approval | Strictly read-only |
| Download/stream from MusicBrainz | MusicBrainz is a metadata database, not a music source | Display metadata only |
| Background MusicBrainz scanning of entire library | Rate limiting makes this impractical (1 req/sec = 3600 tracks/hour max) | On-demand browsing only |

### Implementation Notes

**MusicBrainz API:** REST API at `https://musicbrainz.org/ws/2/`. JSON format via `fmt=json` parameter. No API key required, but must set meaningful User-Agent header: `YellowJacket/<version> (contact-url-or-email)`.

**Rate limiting:** Strict 1 request/second. Implement with a `time.Ticker`-based rate limiter in the Go backend. All API calls go through a single rate-limited HTTP client.

**Go libraries available:**
- `github.com/michiwend/gomusicbrainz` — Go client, but may be outdated
- `go.uploadedlobster.com/musicbrainzws2` — another Go client on SourceHut
- **Recommended: Build a thin HTTP client** — the API is simple REST/JSON. A custom client with rate limiting, User-Agent, and JSON parsing is ~200 lines and avoids third-party dependency risk.

**API patterns needed for read-only browsing:**
1. **Search artist:** `GET /ws/2/artist?query=<name>&fmt=json&limit=25`
2. **Artist discography:** `GET /ws/2/release-group?artist=<mbid>&fmt=json&limit=100&inc=artist-credits`
3. **Release group releases:** `GET /ws/2/release?release-group=<mbid>&fmt=json&inc=media+recordings`
4. **Release track listing:** `GET /ws/2/release/<mbid>?fmt=json&inc=recordings+media+artist-credits`

**Frontend architecture:** New view (`musicbrainz-browser` component) accessible from sidebar. Search bar, results list, detail panels for artist/album/release. Navigation is drill-down: search → artist → release group → release → tracks.

**Caching strategy:** Cache API responses in SQLite (`mb_cache` table: url, response_json, fetched_at). TTL of 24 hours for search results, 7 days for entity lookups (MusicBrainz data changes infrequently). Cache reduces API calls and improves responsiveness.

**This is YellowJacket's first network feature** — the app is currently fully offline. Need to handle: network errors gracefully, timeout configuration, offline mode (show cached data), user notification of network status.

---

## 7. Layout Customization System

### Table Stakes

| Feature | Why Expected | Complexity | Dependencies |
|---------|--------------|------------|--------------|
| Resizable panels (sidebar, queue, main) | Basic expectation in any multi-panel desktop app. Users want wider sidebar or hidden queue. | MEDIUM | CSS grid/flexbox with drag handles |
| Show/hide queue panel | Already partially implemented (queue toggle button exists) | LOW | Existing queue panel toggle |
| Show/hide sidebar sections | Collapse navigation sections user doesn't need | LOW | Sidebar configuration |
| Persist layout across restarts | Layout changes must survive app restart | LOW | TOML config section |

### Differentiators

| Feature | Value Proposition | Complexity | Dependencies |
|---------|-------------------|------------|--------------|
| Section-based component placement (MusicBee-style) | Users choose what goes where — put album art in sidebar, now-playing at top, etc. This is MusicBee's signature feature. | HIGH | Component registry, layout engine, size constraints |
| Component size constraints | Components declare min/max sizes; layout engine respects constraints | MEDIUM | Component metadata system |
| Layout presets | "Compact", "Full", "Mini player" — quick switch between configurations | MEDIUM | Preset definitions + switch mechanism |
| Detachable panels | Pop out queue or now-playing to separate window | HIGH | Wails multi-window support (limited in v2) |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Free-form drag-and-drop layout | Overwhelming complexity, hard to make look good | Section-based: defined slots with selectable components |
| CSS theme editor | Users don't want to write CSS | Extend existing theme system (accent color, background shade) |
| Mobile-responsive layout | This is a desktop app with fixed minimum size | Optimize for 1024x768 minimum |

### Implementation Notes

**MusicBee-style layout means section-based composition:**

The UI is divided into named sections (slots):
- `header` (top bar)
- `sidebar` (left panel)
- `main` (center content area)
- `footer` (bottom bar — now playing + player controls)
- `right-panel` (queue panel or other content)

Each section has a list of components it can host. Components declare their size constraints (min width/height). Users configure which component goes in which section via a settings UI.

**Implementation approach:**

1. **Component registry:** Each component registers itself with metadata (name, description, supported sections, min/max size). This is a TypeScript Map, not a plugin system yet.
2. **Layout configuration:** Stored in TOML config under `[Layout]` section. Maps section names to component names.
3. **Layout renderer:** A root `<app-layout>` component reads config and instantiates the right components in the right sections using dynamic imports.
4. **Resize handles:** CSS resize or custom drag handles on section boundaries. Store widths/heights as percentages in config.

**Start simple for v1.1:**
- Phase 1: Resizable panels (sidebar width, queue width) with drag handles + persistence
- Phase 2: Show/hide sections + layout presets
- Phase 3: Component-in-section customization (the full MusicBee-style system)

The full section-based system is the v1.1 "foundation" — functional but not complete.

**Depends on:** Config system (TOML), existing component architecture, CSS grid layout.

---

## 8. Plugin System

### Table Stakes

| Feature | Why Expected | Complexity | Dependencies |
|---------|--------------|------------|--------------|
| Defined plugin API (what plugins can do) | Without clear API boundaries, plugins break on every update | HIGH | API design + stability commitment |
| Plugin loading/unloading | Install/remove plugins without rebuilding the app | HIGH | Dynamic loading mechanism |
| Plugin configuration | Plugins need their own settings that persist | MEDIUM | Extend config system |
| Plugin isolation (one plugin crash doesn't kill app) | Critical for stability | HIGH | Error boundaries, sandboxing |

### Differentiators

| Feature | Value Proposition | Complexity | Dependencies |
|---------|-------------------|------------|--------------|
| UI component plugins (custom panels, visualizations) | Plugins can add new views to the layout system | HIGH | Layout customization system + component registry |
| Backend hook plugins (custom metadata sources, scrobblers) | Plugins can intercept/extend backend operations | HIGH | Hook system in Go backend |
| Plugin marketplace/registry | Discover and install plugins | HIGH | External infrastructure |
| TypeScript/JavaScript plugin runtime | Lowest barrier to entry for plugin authors | MEDIUM | Webview already runs JS |

### Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Go plugin system (`plugin` package) | Linux-only, version-fragile, build-tag sensitive, widely considered broken | Use process-based or embedded scripting approach |
| Full filesystem access for plugins | Security nightmare | Sandboxed API with explicit permissions |
| Plugin binary distribution | Build reproducibility, platform issues | Source-based distribution (TypeScript/JS bundles) |
| Network access for plugins without user consent | Privacy concern | Require explicit network permission declaration |

### Implementation Notes

**Plugin systems in Go desktop apps are notoriously difficult.** The `plugin` package is Linux-only and requires exact build-tag matching. Wails v2 doesn't have a plugin framework.

**Recommended approach for v1.1 "foundation":**

1. **Frontend-first plugins (TypeScript):**
   - Plugins are JS/TS bundles loaded dynamically into the webview
   - They register with the component registry (layout system) to add UI
   - They access backend data through the existing Wails binding layer
   - Isolation via Shadow DOM for UI, try/catch for errors

2. **Backend hooks (Go):**
   - Define hook points as interfaces: `OnTrackPlay`, `OnLibraryScan`, `OnMetadataChange`, etc.
   - Internal Go "plugins" implement these interfaces
   - For v1.1, hooks are compile-time (not dynamic) — the plugin system defines the API, but plugins are compiled in
   - Dynamic loading deferred to future (hashicorp/go-plugin RPC, or WASM)

3. **Plugin manifest:**
   ```json
   {
     "name": "my-plugin",
     "version": "1.0.0",
     "description": "Does a thing",
     "entry": "index.js",
     "hooks": ["onTrackPlay", "onLibraryScan"],
     "ui": [{"component": "my-panel", "sections": ["sidebar", "right-panel"]}],
     "permissions": ["network"]
   }
   ```

4. **Plugin directory:** `~/.config/yellowjacket/plugins/<name>/` containing manifest + JS bundle

**v1.1 scope should be the API definition and loading mechanism** — not a full marketplace. "Working foundation" means: plugins can be loaded, they can register UI components, they can subscribe to backend events. The API surface is deliberately small and stable.

**Depends on:** Layout customization system (for UI plugins), event system (for hook subscriptions), config system (for plugin settings).

---

## Feature Dependencies

```
Scan Cancellation ──── (standalone, no dependencies)
     │
Tag Editing ────────── (standalone, needs new libraries)
     │
Smart Playlists ────── depends on: existing DB/track_metadata VIEW
     │
Keyboard Shortcuts ─── (standalone, frontend-primary)
     │
Gapless + Crossfade ── depends on: audio pipeline refactor
     │
MusicBrainz Browser ── depends on: HTTP client (new), network handling (new)
     │
Layout Customization ── depends on: component registry (new)
     │
Plugin System ──────── depends on: Layout Customization, Event system, Config system
```

**Dependency ordering (what blocks what):**
1. **Nothing blocks:** Scan cancellation, tag editing, keyboard shortcuts, smart playlists, MusicBrainz browser
2. **Layout blocks plugins:** Plugin UI registration needs the layout component registry
3. **Gapless is self-contained** but is the highest-risk change (audio pipeline)

---

## MVP Recommendation

### Build First (low risk, high value, unblocked)
1. **Scan cancellation** — lowest complexity, immediate UX win, architecture already supports it
2. **Keyboard shortcuts** — low complexity, massive usability improvement, no backend changes
3. **Smart playlists** — medium complexity, high value, builds on existing DB infrastructure

### Build Second (medium risk, foundational)
4. **Tag editing** — medium complexity, requires new dependencies, needed before MusicBrainz becomes useful
5. **MusicBrainz browser** — medium complexity, first network feature, independent of others
6. **Layout customization** — medium-high complexity, needed before plugins

### Build Last (high risk, high complexity)
7. **Gapless playback + crossfade** — highest complexity, fundamental audio pipeline change, can ship independently
8. **Plugin system** — highest complexity, depends on layout system, explicitly a "foundation" for v1.1

### Defer (explicitly)
- Tag-to-filename rename
- Undo/redo for tag edits
- Play count tracking (needed for some smart playlist rules)
- Rating system
- Plugin marketplace
- Dynamic Go plugin loading
- Detachable panels (Wails v2 limitation)

---

## Sources

- MusicBrainz API documentation: https://musicbrainz.org/doc/MusicBrainz_API (HIGH confidence — official docs, verified 2026-03-06)
- MusicBrainz rate limiting: https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting (HIGH confidence — official docs)
- `github.com/bogem/id3v2/v2` v2.1.4: https://pkg.go.dev/github.com/bogem/id3v2/v2 (HIGH confidence — official pkg.go.dev)
- `github.com/go-flac/go-flac` v1.0.0: https://pkg.go.dev/github.com/go-flac/go-flac (HIGH confidence — official pkg.go.dev)
- `github.com/go-flac/flacvorbis` v0.2.0: https://pkg.go.dev/github.com/go-flac/flacvorbis (HIGH confidence — official pkg.go.dev)
- `github.com/gopxl/beep/v2` v2.1.1: https://pkg.go.dev/github.com/gopxl/beep/v2 (HIGH confidence — official pkg.go.dev, confirms Mixer, Seq, Loop2, effects)
- YellowJacket codebase analysis: `.planning/codebase/` (HIGH confidence — direct code inspection)
- Desktop music player patterns: foobar2000, MusicBee, Strawberry, Deadbeef, Audacious (MEDIUM confidence — training data knowledge of established players)

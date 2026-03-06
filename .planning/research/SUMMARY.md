# Project Research Summary

**Project:** YellowJacket v1.1 — Features & Extensibility
**Domain:** Desktop music player — feature expansion of existing Go/Wails/Lit/SQLite application
**Researched:** 2026-03-06
**Confidence:** HIGH

## Executive Summary

YellowJacket v1.1 adds 8 features to a well-structured existing codebase: tag editing, scan cancellation, smart playlists, customizable keyboard shortcuts, gapless playback + crossfade, MusicBrainz browser, layout customization, and a plugin system foundation. The research confirms this is overwhelmingly an **architecture and design challenge, not a library selection challenge**. Only 3 new Go packages are needed (tag writing for MP3 and FLAC); the remaining features build entirely on the existing stack (beep v2.1.1, SQLite, Lit 3.2.1, Wails v2, stdlib). Zero new npm dependencies are required.

The recommended approach is **integration-first**: every feature slots into established codebase patterns (two-phase init, event-driven sync, mutex-protected state, sqlc codegen, TOML config) rather than introducing new paradigms. Features vary dramatically in complexity — scan cancellation requires ~50 lines of changes to existing code, while gapless playback requires a fundamental restructuring of the audio pipeline. The build order should exploit this variance: ship quick wins first (scan cancel, keyboard shortcuts) to validate integration patterns, then tackle data model extensions (tag editing, smart playlists), then high-risk backend changes (gapless, MusicBrainz), and finally the extensibility foundations (layout, plugins).

The primary risks are: (1) **tag writing corrupting audio files** — mitigated by write-to-temp-then-rename and blocking writes on playing files; (2) **gapless playback breaking the existing lock ordering and callback contract** — mitigated by pre-decoding in a separate goroutine and using beep's Mixer/Seq primitives; (3) **scan cancellation causing silent data loss** via orphan cleanup on partial scan data — mitigated by skipping orphan cleanup on cancelled scans; and (4) **MusicBrainz rate limiting** — mitigated by a strict 1 req/s rate limiter, aggressive SQLite caching, and proper User-Agent header. The plugin system is the highest architectural risk but is scoped to "foundation only" for v1.1, which limits blast radius.

## Key Findings

### Recommended Stack

The existing stack is comprehensive. v1.1 adds only 3 new Go dependencies and 0 npm dependencies. This is the right call — most features are solved by new code, not new libraries.

**New dependencies (all Go):**
- **n10v/id3v2 v2.1.4**: MP3 tag writing (ID3v2.3/v2.4) — the only maintained pure-Go library with full write support (359 stars, active). Replaces nothing; `dhowden/tag` stays for reads.
- **go-flac/go-flac v2**: FLAC metadata block manipulation — low-level but the only Go option for FLAC tag writing.
- **go-flac/flacvorbis v2**: Vorbis comment read/write within FLAC files — companion to go-flac.

**Reused from existing stack (no new deps):**
- **Gapless/Crossfade**: `beep.Mixer`, `beep.Seq`, `effects.Volume` — all already in beep v2.1.1.
- **MusicBrainz**: Custom HTTP client using stdlib `net/http` + `encoding/json`. Thin wrapper (~200 lines) beats unmaintained third-party clients.
- **Smart Playlists**: Dynamic SQL against existing `track_metadata` VIEW. No ORM needed.
- **Shortcuts**: Web platform `KeyboardEvent` API + TOML config persistence.
- **Layout**: Lit `customElements.define()` + component registry. CSS Container Queries for responsive components.
- **Plugins**: Interface-based Go hooks (compiled-in for v1.1) + dynamic JS module loading for frontend.

**Critical version requirement:** OGG Vorbis and WAV tag writing should be deferred — no mature pure-Go libraries exist. MP3 + FLAC cover ~95% of music libraries.

### Expected Features

**Must have (table stakes):**
- Tag editing: single track + batch edit for title/artist/album/genre/year + write to file + DB sync
- Scan cancellation: cancel button, graceful stop (no DB corruption), progress reporting
- Smart playlists: filter by genre/year/artist, combine rules with AND, auto-update on library changes, save and name
- Keyboard shortcuts: play/pause, next/prev, volume, search focus, defaults that work out of box
- Gapless playback: no silence between tracks (this is expected by serious music listeners)
- Crossfade: on/off toggle with configurable duration (1-10 seconds)
- MusicBrainz browser: artist search, discography view, album track listing, rate limit compliance
- Layout: resizable panels, show/hide queue, persist across restarts

**Should have (differentiators):**
- Batch tag editing with preview/confirmation
- Smart playlists with random/limit results ("random 50 Jazz tracks")
- Per-album gapless (disable crossfade within albums)
- MusicBrainz response caching in SQLite
- Layout presets (Compact, Full, Mini player)
- Full shortcut customization UI with conflict detection
- Cover art assignment in tag editor

**Defer (v2+):**
- Tag-to-filename rename, undo/redo for tag edits
- Play count tracking and rating system (needed for advanced smart playlist rules)
- Plugin marketplace and dynamic Go plugin loading
- Auto-tag from MusicBrainz (this is Picard's domain)
- Detachable panels (Wails v2 limitation)
- OGG Vorbis tag writing
- DSP effects chain (equalizer, reverb)

### Architecture Approach

Integration-first: 6 new backend packages + 5 new frontend stores/components slot into established patterns. Backend remains source of truth. Frontend stores are reactive mirrors. Events flow backend→frontend. Actions flow frontend→backend via Wails bindings. The one paradigm shift is the audio pipeline: switching from single-streamer to persistent `beep.Mixer` as the root speaker streamer.

**Major new components:**
1. **`backend/tageditor/`** — Format-specific tag writing + DB cascade update + FTS5 re-index
2. **`backend/smartplaylist/`** — Rule-based dynamic query evaluation against `track_metadata` VIEW
3. **`backend/musicbrainz/`** — Rate-limited HTTP client + SQLite response cache
4. **`backend/shortcuts/`** — Shortcut registry mapping key combos to backend action handlers
5. **`backend/layout/`** — Section-based layout config read from TOML, exposed to frontend
6. **`backend/plugin/`** — Plugin manifest parsing, JS loader, hook registry, API surface

**Modified components:**
- **`backend/player/`** — Gapless pre-loading, crossfade mixer, persistent speaker mixer
- **`backend/library/`** — Scan-specific cancellable context, suppressed orphan cleanup on cancel
- **`backend/queue/`** — `TrackLoader` interface gains `PreloadNext()`, queue exposes "peek next" capability

**Database migrations** (current version = 5): +2 new tables (`smart_playlists`, `musicbrainz_cache`), most features use TOML config not DB.

### Critical Pitfalls

1. **Tag writing corrupts audio files (P1)** — `dhowden/tag` is read-only; new write libraries must use write-to-temp-then-rename. Block writes on currently-playing file (beep holds `*os.File` handle). Preserve all existing tag frames when editing; never create tags from scratch.

2. **Gapless playback breaks lock ordering (P2)** — The existing `p.mu → speaker.Lock()` ordering assumes one streamer at a time. Pre-decoding a second track with crossfade means two concurrent streamer chains. Must suppress `onPlaybackFinished` callback during transitions, pre-decode in background goroutine, and close old `BufferedStreamer` only after crossfade completes.

3. **Scan cancellation triggers orphan cleanup on partial data (P3)** — If walk is cancelled early, `existingPaths` sync.Map still contains valid files → orphan cleanup deletes them. **Must skip orphan cleanup on cancelled scans.** Check cancellation between DB writer batches, not mid-batch.

4. **Plugin system crashes host app (P4)** — Go `plugin` package is Linux-only and fragile. For v1.1: JS-only frontend plugins (loaded via dynamic `import()`), Go hooks compiled-in (not dynamic). Wrap all plugin callbacks in `recover()`. Give plugins read-only DB access.

5. **MusicBrainz rate limiting (P5)** — Strict 1 req/s enforced by IP ban. Must set meaningful User-Agent, cache responses in SQLite (24hr for searches, 7 days for entities), use `time.Ticker` rate limiter, handle 503 with exponential backoff.

## Implications for Roadmap

Based on research, suggested phase structure:

### Phase 1: Quick Wins — Scan Cancellation + Keyboard Shortcuts
**Rationale:** Lowest complexity, highest certainty, no new dependencies. Validates core integration patterns (context cancellation, config extension, event-driven sync) that every subsequent phase depends on.
**Delivers:** Cancellable library scans with graceful stop; configurable keyboard shortcuts with sensible defaults.
**Addresses:** Scan cancellation (all table stakes), keyboard shortcuts (all table stakes)
**Avoids:** P3 (skip orphan cleanup on cancel), P7 (capture phase listener, skip shortcuts on input focus), P12 (config backward compat — test with old config files)
**Stack:** No new dependencies. stdlib `context.WithCancel`, TOML config extension, Web `KeyboardEvent` API.

### Phase 2: Tag Editing
**Rationale:** Introduces the 3 new Go dependencies and validates the "write file → update DB → emit event → refresh frontend" pipeline. This pipeline is reused by smart playlists (DB updates trigger re-evaluation) and is a prerequisite for MusicBrainz becoming useful (users see MB data then want to apply it to their files).
**Delivers:** Single-track and batch tag editing for MP3 and FLAC files; cover art assignment; DB cascade updates; FTS5 re-indexing.
**Addresses:** Tag editing (all table stakes), cover art assignment
**Avoids:** P1 (write-to-temp-then-rename, block writes on playing file, preserve unedited frames), P9 (block tag edits during active scans)
**Stack:** n10v/id3v2 v2.1.4, go-flac/go-flac v2, go-flac/flacvorbis v2

### Phase 3: Smart Playlists
**Rationale:** Builds on validated DB infrastructure from Phase 2. Independent of audio pipeline. Medium complexity with well-understood patterns (SQL WHERE clause generation). Benefits from tag editing being complete (edited metadata changes smart playlist membership).
**Delivers:** Rule-based dynamic playlists with AND logic, configurable sort/limit, auto-refresh on library changes, sidebar integration.
**Addresses:** Smart playlists (all table stakes + random/limit differentiator)
**Avoids:** P6 (lazy evaluation — only re-evaluate on view, not on every library change; dedicated indexed queries, not VIEW-based full scans)
**Stack:** No new dependencies. Dynamic SQL with parameterized queries, new `smart_playlists` table (migration 6).

### Phase 4: Gapless Playback + Crossfade
**Rationale:** Highest technical risk — must be built with full focus and thorough testing. No dependencies on other v1.1 features. The audio pipeline refactor (switching from per-track `speaker.Play()` to persistent `beep.Mixer`) is the biggest architectural change in v1.1. Build gapless first, then layer crossfade on top.
**Delivers:** Seamless track transitions; optional crossfade with configurable duration; pre-decoded next track for zero-gap playback.
**Addresses:** Gapless playback (table stakes), crossfade (table stakes), crossfade duration control
**Avoids:** P2 (pre-decode in background goroutine, suppress callback during transitions, close old BufferedStreamer after crossfade completes), P11 (always crossfade post-resample)
**Stack:** No new dependencies. beep.Mixer, beep.Seq, effects.Volume (all existing).

### Phase 5: MusicBrainz Browser
**Rationale:** First network feature — introduces HTTP client, caching, offline handling, rate limiting. Orthogonal to audio pipeline work. Can be developed independently. Becomes more valuable after tag editing exists (users can browse MB, then manually apply metadata).
**Delivers:** Artist search, discography browsing, release/track listing, response caching, offline-safe degradation.
**Addresses:** MusicBrainz browser (all table stakes + caching differentiator)
**Avoids:** P5 (1 req/s rate limiter, proper User-Agent, SQLite cache, exponential backoff on 503), P10 (separate cache table, display-only DTOs — never merge MB data into library schema), P13 (use bindings for data retrieval, events for notifications only)
**Stack:** No new dependencies. stdlib net/http + encoding/json, new `musicbrainz_cache` table (migration 7).

### Phase 6: Layout Customization + Plugin Foundation
**Rationale:** Meta-features that wrap all other features. Must come last because they need a stable API surface and complete component set. Layout customization is the prerequisite for plugin UI registration. Plugin system defines the extensibility API but ships as "foundation" (working loader + core API surface + example plugin).
**Delivers:** Section-based layout config (MusicBee-style); resizable panels with persistence; component registry; JS plugin loading; plugin API surface (events, player, queue, library); one example plugin.
**Addresses:** Layout customization (table stakes + section-based differentiator), plugin system (foundation — API definition, loading mechanism, core hooks)
**Avoids:** P4 (JS-only plugins, recover() wrappers, read-only DB for plugins, namespaced events), P8 (section-level operation not component-level, CSS Container Queries, explicit height for virtualized sections), P14 (extend existing stores where possible, component-local state for view-specific data)
**Stack:** No new dependencies. Lit customElements, dynamic import(), TOML config extension.

### Phase Ordering Rationale

- **Dependency chain:** Scan cancel → validates context patterns used everywhere. Tag editing → validates file-write-DB-update-event pipeline. Smart playlists → uses validated DB patterns. Layout → provides component registry needed by plugins. Plugins → last because it depends on everything being stable.
- **Risk isolation:** Gapless playback (Phase 4) is the highest-risk change. Placing it mid-sequence means foundational patterns are proven and later features (MusicBrainz, layout, plugins) don't block on audio work.
- **Value delivery curve:** Phases 1-3 are low-to-medium risk and deliver immediate user-facing value. If the project stalls after Phase 3, users still get scan cancellation, keyboard shortcuts, tag editing, and smart playlists — a strong v1.1.
- **Feature grouping:** Each phase touches a distinct subsystem (config, files+DB, DB queries, audio pipeline, network, UI architecture), minimizing merge conflicts for parallel development.

### Research Flags

**Phases likely needing deeper research during planning:**
- **Phase 4 (Gapless + Crossfade):** The beep library's Mixer/Seq composition for real-time crossfade is not well-documented beyond basic examples. Need to prototype the persistent-mixer architecture and validate lock ordering with two concurrent BufferedStreamers before committing to implementation approach.
- **Phase 6 (Plugin System):** The plugin API surface needs careful design — what's exposed, what's sandboxed, how errors are contained. No off-the-shelf solution fits; this is bespoke design work. Consider a spike/prototype before full implementation.

**Phases with standard patterns (skip deep research):**
- **Phase 1 (Scan Cancel + Shortcuts):** Well-documented Go context cancellation + standard web keyboard handling. The codebase already has the patterns.
- **Phase 2 (Tag Editing):** Tag writing libraries have clear APIs. The DB cascade is the main design work.
- **Phase 3 (Smart Playlists):** Dynamic SQL generation is a solved problem. Rules → WHERE clause mapping is straightforward.
- **Phase 5 (MusicBrainz):** REST API with excellent official documentation. Rate limiting patterns are standard.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Only 3 new deps, all verified on pkg.go.dev. Existing stack covers 7/10 features with no additions. |
| Features | HIGH | Grounded in codebase analysis + established desktop music player patterns (foobar2000, MusicBee, Strawberry). |
| Architecture | HIGH | Derived from complete codebase read. Integration patterns validated against existing code structure. |
| Pitfalls | HIGH | 15 pitfalls identified with specific line-number references to codebase. Critical pitfalls have concrete prevention strategies. |

**Overall confidence:** HIGH

### Gaps to Address

- **OGG Vorbis tag writing:** No pure-Go solution exists. Deferred to v1.2+. Need to show "read-only" indicator in tag editor UI for OGG files. May need to revisit if user demand is high.
- **Play count tracking:** Required for advanced smart playlist rules ("most played", "never played") but not in current schema. Needs a schema migration and playback-completion hook. Defer to Phase 3 as an optional add-on.
- **Plugin security model:** The v1.1 foundation intentionally skips a permissions system. Plugins run with full API access. This is acceptable for "power user installs plugins manually" but needs a permissions model before any marketplace/discovery feature.
- **FLAC memory usage during tag writes:** `go-flac` reads entire files into memory. For 100MB+ FLAC files, this is significant. May need a streaming approach in the future, but acceptable for v1.1.
- **Crossfade timing accuracy:** Detecting "N seconds from track end" requires comparing `seeker.Position()` to `seeker.Len()` at the speaker sample rate. Accuracy depends on the polling interval. Need to prototype during Phase 4 to determine if a polling approach is sufficient or if a sample-counting approach is needed.

## Sources

### Primary (HIGH confidence)
- YellowJacket codebase: complete analysis of all Go packages and TypeScript sources (2026-03-06)
- n10v/id3v2: https://github.com/n10v/id3v2 — 359 stars, v2.1.4, MIT license, full ID3v2 read/write
- beep v2.1.1: https://pkg.go.dev/github.com/gopxl/beep/v2 — Mixer, Seq, Volume, Resample confirmed
- beep wiki: https://github.com/gopxl/beep/wiki/Composing-and-controlling — speaker.Lock(), Seq chaining, Ctrl pause
- MusicBrainz API: https://musicbrainz.org/doc/MusicBrainz_API — rate limiting, JSON format, entity types
- MusicBrainz rate limiting: https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting — 1 req/s, User-Agent requirement
- dhowden/tag: confirmed read-only (no Save/Write methods in API)

### Secondary (MEDIUM confidence)
- go-flac/go-flac: https://github.com/go-flac/go-flac — 44 stars, v2 available, Apache-2.0
- go-flac/flacvorbis: https://github.com/go-flac/flacvorbis — 11 stars, v2 available, Apache-2.0
- Desktop music player patterns: foobar2000, MusicBee, Strawberry, Deadbeef, Audacious (training data knowledge)
- michiwend/gomusicbrainz: https://github.com/michiwend/gomusicbrainz — 64 stars, confirmed unmaintained

### Tertiary (LOW confidence)
- Plugin architecture recommendations: based on Go ecosystem analysis and desktop app patterns; no direct precedent for Wails plugin systems exists

---
*Research completed: 2026-03-06*
*Ready for roadmap: yes*

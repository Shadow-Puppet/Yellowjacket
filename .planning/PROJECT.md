# YellowJacket

## What This Is

YellowJacket is a cross-platform desktop music player built with Go (Wails v2) and TypeScript (Lit Web Components). It plays local music files (MP3, FLAC, OGG, WAV), manages multiple music library directories via SQLite, and provides queue management, playlists with cross-library support, cover art, configurable keyboard shortcuts, and MPRIS media controls on Linux. The v1.2 Tag Editing milestone added full metadata editing — users can edit any track's tags and cover art (single or batch) with crash-safe file writes and instant database synchronization, no rescan needed.

## Core Value

The music player works reliably and feels solid. Every interaction is correct, responsive, and trustworthy — the foundation that all future features will build on.

## Requirements

### Validated

- ✓ Audio playback (play, pause, stop, seek, volume) for MP3, FLAC, OGG, WAV — existing
- ✓ Library scanning with concurrent metadata extraction pipeline — existing
- ✓ Queue management with shuffle, repeat modes, and auto-advance — existing
- ✓ Queue and player state persistence across app restarts — existing
- ✓ Full-text search across tracks, artists, albums, file paths (FTS5) — existing
- ✓ Playlist CRUD with M3U8 import/export and phantom track resolution — existing
- ✓ Favorites system with dedicated playlist — existing
- ✓ Cover art extraction, thumbnail generation (sm/md/lg), and serving — existing
- ✓ MPRIS2 media controls on Linux — existing
- ✓ Theme configuration (accent color, background shade) — existing
- ✓ Track list column configuration — existing
- ✓ Adaptive scan concurrency based on disk type (SSD vs HDD) — existing
- ✓ Two-phase queue initialization for instant UI response — existing
- ✓ Event-driven frontend/backend synchronization — existing
- ✓ TOML-based user configuration with live reload — existing
- ✓ Browse by albums, artists, genres with detail views — existing
- ✓ Virtual scrolling for large lists — existing
- ✓ Concurrency race-free SetContext across Queue, Library, Playlist, Player — v1.0
- ✓ Error handling: startupErr moved to struct, config 0o644, MPRIS errors logged, scan warnings separated — v1.0
- ✓ FTS5 JOIN pattern consolidated into track_metadata VIEW — v1.0
- ✓ Event name codegen (Go→TypeScript) with pre-commit hook enforcement — v1.0
- ✓ Queue batch lookups use sqlc.slice(), all hand-crafted SQL documented with SAFETY comments — v1.0
- ✓ Incremental queue persistence (O(1) add/remove) and SetQueue Phase 2 dedup — v1.0
- ✓ Library store deferred loading for instant app shell — v1.0
- ✓ SQLite performance PRAGMAs (synchronous, cache_size, mmap_size) — v1.0
- ✓ Frontend repeat() with stable keys, queueMicrotask coalescing, classMap directives — v1.0
- ✓ Design token system and visual consistency across all 15 components — v1.0
- ✓ 84 unit tests: queue (29), config/player (10+), FTS5 search (15), library scan (13), entity cache (13+) — v1.0
- ✓ Cancellable/pausable library scans with per-scan context cancellation — v1.1
- ✓ Configurable keyboard shortcuts with record-style capture, scope-aware dispatch, conflict detection — v1.1
- ✓ Multi-library schema (libraries table, library_id FK, phantom columns) with seamless migration — v1.1
- ✓ Per-library scan pipeline with sequential queue coordination and per-library progress UI — v1.1
- ✓ Library CRUD (add/rename/remove) with atomic orphan cleanup and phantom metadata preservation — v1.1
- ✓ Library filter dropdown with all views (tracks, albums, artists, genres, search) respecting active filter — v1.1
- ✓ Cross-library playlists with phantom track auto-resolution via ScanHooks + M3U8 matching — v1.1
- ✓ Performance: CSS containment, view caching, event delegation, content-visibility, scroll polish — v1.1
- ✓ FTS5 contentless_delete migration for safe row-level tag edit sync — v1.2
- ✓ Atomic file write utility (write-to-temp-then-rename) for corruption-safe tag writes — v1.2
- ✓ MP3 tag writing via ID3v2 (title, artist, album, genre, year, track#, disc#, composer, cover art) — v1.2
- ✓ FLAC tag writing via Vorbis Comments + PICTURE blocks with 7 round-trip tests — v1.2
- ✓ Cover art embedding in MP3 and FLAC files — v1.2
- ✓ WriteTrackTags pipeline: file write → transactional DB sync (entity relink + FTS5 + orphan cleanup) → event emission — v1.2
- ✓ Player safety: currently-playing file stopped before tag write — v1.2
- ✓ Scan/write mutual exclusion via pipelineMu — v1.2
- ✓ Single-track editor with 8 editable fields, cover art pick/replace/remove, diff-only saves — v1.2
- ✓ Batch editor with three-state field model, confirmation guard, progress bar, cancellation, partial failure reporting — v1.2
- ✓ Batch cover art set/clear across all selected tracks — v1.2

### Active

- [ ] OGG Vorbis tag writing — write Vorbis Comments to .ogg files (approach TBD after research)
- [ ] WAV tag writing — write metadata to .wav files (ID3v2 vs RIFF INFO TBD after research)
- [ ] Cover art embedding for OGG and WAV — feasibility TBD after research
- [ ] General cleanup — lint warnings and small issues from v1.2

### Deferred (Future Milestones)

- [ ] Smart playlists — auto-generated playlists with simple filter rules (genre, year, play count, etc.)
- [ ] Gapless playback + crossfade — seamless track transitions with optional crossfade setting
- [ ] MusicBrainz browser — read-only catalog browsing (artists, discographies, album editions, track listings)
- [ ] Layout customization system — section-based UI customization, components declare size constraints, users configure per-section
- [ ] Plugin system — full-access API for UI components and backend hooks, extensibility foundation

### Out of Scope

- Separate databases per library — overly complex, defeats unified presentation
- Auto-dedup across libraries — complex matching logic, not table stakes
- User access control per library — desktop app, single user
- Parallel library scanning — SQLite single-writer makes it pointless
- OGG Vorbis tag writing (revisiting in v1.2.1) — previously assessed as medium-high risk; researching approaches now
- Cross-platform media controls (macOS/Windows) — feature work
- Database health checking / reconnection — low priority, desktop app context
- File decomposition for its own sake — only extract when it enables reuse or fixes problems
- ORM or query builder — would fight existing sqlc architecture
- Connection pooling for SQLite — meaningless with SetMaxOpenConns(1)

## Shipped Milestones

- **v1.0 Consolidation** (2026-03-05) — Foundation: races fixed, tests added, SQL consolidated, performance optimized
- **v1.1 Multi-Library Support** (2026-03-16) — Multi-library: CRUD, per-library scanning, filtered views, cross-library playlists, phantom tracks
- **v1.2 Tag Editing** (2026-03-18) — Tag editing: single + batch metadata editing, cover art embed, crash-safe writes, instant DB sync (MP3 + FLAC)

## Current Milestone: v1.2.1 Format Parity

**Goal:** Complete tag writing support for all four audio formats — add OGG Vorbis and WAV tag writing so every file YellowJacket plays can also be edited.

**Target features:**
- OGG Vorbis tag writing (Vorbis Comments in OGG container)
- WAV tag writing (metadata approach TBD after research)
- Cover art embedding for OGG and WAV (feasibility TBD)
- General cleanup from v1.2 (lint warnings, small issues)

## Context

**Current state (v1.2 shipped 2026-03-18):**
- Go 1.25, Wails v2.10.2, Lit 3.2.1, SQLite via modernc.org/sqlite
- ~31,200 Go LOC + ~30,400 TypeScript LOC + ~1,200 SQL LOC
- ~16 backend packages (added tagwriter, fileutil), ~22 frontend components, 8 DB migrations
- Strict linting (golangci-lint v2) and TypeScript strict mode
- 84+ unit tests covering queue, config, player, database, library, migration packages + 7 FLAC round-trip tests
- Multi-library architecture: libraries table, library_id FK, ScanHooks/RemovalHooks/RescanHooks callback patterns
- Tag writing pipeline: format-specific writers (MP3/FLAC) → AtomicWrite → DB sync (entity relink + FTS5 + orphan cleanup)
- SQL: track_metadata VIEW, sqlc-generated + hand-crafted with SAFETY comments, ByLibrary query variants
- Frontend: design token system, virtual scrolling, view caching, event delegation, library filter state, track-details dialog with single/batch edit modes
- Player tests still require hardware (skipped in CI)
- No frontend unit tests (deferred to future milestone)

**Codebase analysis available in:**
- `.planning/codebase/ARCHITECTURE.md`
- `.planning/codebase/CONCERNS.md`
- `.planning/codebase/CONVENTIONS.md`
- `.planning/codebase/INTEGRATIONS.md`
- `.planning/codebase/STACK.md`

## Constraints

- **Tech stack**: Go + Wails v2 + Lit + SQLite — no changes to the fundamental stack
- **Build tags**: All Go commands require `-tags webkit2_41` on Linux
- **Single writer**: SQLite with WAL mode and `SetMaxOpenConns(1)` — design around this
- **Backward compatibility**: Existing user config and database must continue working after changes
- **Linting**: All code must pass `make lint` (golangci-lint v2 with strict rules)
- **No CGo**: Pure-Go SQLite driver (`modernc.org/sqlite`) — cannot switch to CGo-based drivers

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Consolidation before features | Technical debt compounds — fixing it now is cheaper than fixing it later under more code | ✓ Good — solid foundation established |
| Tests support refactoring, not standalone goal | Testing is a means to safe refactoring, not a coverage target | ✓ Good — 84 tests enabled safe SQL and perf refactoring |
| No cosmetic file splitting | Large files are only a problem if they cause real issues; extract only for reuse or correctness | ✓ Good — avoided unnecessary churn |
| All improvement areas equal priority | Correctness, performance, code quality, UX, and testing are interdependent | ✓ Good — balanced approach worked well |
| Fix races → tests → refactoring order | Can't run `-race`-clean tests with active data races; can't safely refactor without tests | ✓ Good — each phase built on the last |
| SQLite VIEW for JOIN dedup | track_metadata VIEW consolidates 5-table JOIN; migration keeps inline for upgrade | ✓ Good — 60 lines eliminated, tests unchanged |
| AST-based event codegen | Deterministic declaration-order output, no regex fragility | ✓ Good — found LibraryConfigChanged gap automatically |
| queueMicrotask over setTimeout | Synchronous microtask batching is more predictable than macrotask scheduling | ✓ Good — coalesces 8+ notifications per scan |
| Design tokens via :host scope | Component-level token scope matches Lit's shadow DOM encapsulation | ✓ Good — consistent visual language achieved |
| Hybrid model (library_id on audio_files only) | Physical files belong to libraries; logical entities shared | ✓ Good — clean separation, efficient orphan cleanup |
| Libraries in DB, not TOML | CRUD through UI shouldn't require TOML manipulation | ✓ Good — seamless migration path |
| SET NULL for playlist_tracks FK | Phantom tracks preserve playlist structure when library removed | ✓ Good — cross-library playlists work naturally |
| Backend filtering, not frontend | Don't load 150K tracks when viewing one library | ✓ Good — ByLibrary SQL variants keep UI responsive |
| Sequential scanning (scan queue) | SQLite single-writer makes parallel scans pointless | ✓ Good — simple, correct, no contention |
| ScanHooks callback pattern | Breaks circular dep between library→playlist for phantom resolution | ✓ Good — follows RemovalHooks precedent |
| M3U8-based phantom resolution | M3U8 files are source of truth for playlist file paths | ✓ Good — works for pre-existing and new phantoms |
| View caching with display toggle | Instant navigation by keeping DOM alive, hiding with display:none | ✓ Good — zero-cost navigation between primary views |
| Event delegation on virtualizer | Zero per-item closures; data-index + closest() pattern | ✓ Good — eliminated GC pressure on large lists |
| AtomicWrite with `.yj-tmp` suffix | Deterministic temp file naming enables orphan cleanup; same-directory rename avoids cross-device issues | ✓ Good — zero corruption risk |
| go-flac ecosystem for FLAC writing | Small library (44 stars) but only option for pure-Go FLAC; 7 round-trip tests validated | ✓ Good — dhowden/tag reads what go-flac writes |
| Upsert-and-relink for tag edit DB sync | Never mutate shared entities; create new or relink existing | ✓ Good — follows v1.1 precedent, safe for concurrent views |
| suppressEvents flag for batch coalescing | Single TrackMetadataChanged after batch, not N individual events | ✓ Good — avoids N full library store invalidations |
| Three-state field model via dirty tracking | Implicit keep/set/clear without explicit state enum; `editValues` presence is the signal | ✓ Good — simple, no extra state management |
| PlayerStopper interface for tagwriter→player | Breaks import cycle; playerAdapter in app.go wraps *player.Player | ✓ Good — clean decoupling |

---
*Last updated: 2026-03-18 after v1.2.1 Format Parity milestone started*

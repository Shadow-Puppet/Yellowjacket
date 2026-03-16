# YellowJacket

## What This Is

YellowJacket is a cross-platform desktop music player built with Go (Wails v2) and TypeScript (Lit Web Components). It plays local music files (MP3, FLAC, OGG, WAV), manages multiple music library directories via SQLite, and provides queue management, playlists with cross-library support, cover art, configurable keyboard shortcuts, and MPRIS media controls on Linux. The v1.1 Multi-Library Support milestone added full library lifecycle management — users can add, rename, and remove library directories, scan them independently, filter all views by library, and playlists gracefully survive library removal with phantom track preservation and auto-resolution.

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

### Active

- [ ] Single track metadata editing (title, artist, album, genre, year, track number, disc number, composer)
- [ ] Batch editing shared fields across multiple selected tracks
- [ ] Cover art set/replace from image file
- [ ] Write-to-temp-then-rename for file safety during tag writes
- [ ] Inline DB + FTS5 update after tag writes (no rescan needed)
- [ ] Tag writing for MP3 (ID3v2), FLAC (Vorbis Comments), OGG (Vorbis Comments)

### Deferred (Future Milestones)

- [ ] Tag editing — edit track metadata (title, artist, album, etc.) from within the app
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
- Cross-platform media controls (macOS/Windows) — feature work
- Database health checking / reconnection — low priority, desktop app context
- File decomposition for its own sake — only extract when it enables reuse or fixes problems
- ORM or query builder — would fight existing sqlc architecture
- Connection pooling for SQLite — meaningless with SetMaxOpenConns(1)

## Shipped Milestones

- **v1.0 Consolidation** (2026-03-05) — Foundation: races fixed, tests added, SQL consolidated, performance optimized
- **v1.1 Multi-Library Support** (2026-03-16) — Multi-library: CRUD, per-library scanning, filtered views, cross-library playlists, phantom tracks

## Current Milestone: v1.2 Tag Editing

**Goal:** Enable users to edit track metadata and cover art directly within YellowJacket, with safe file writes and instant database synchronization.

**Target features:**
- Single track tag editing (title, artist, album, genre, year, track/disc number, composer)
- Batch tag editing across multiple selected tracks
- Cover art set/replace from image file (embedded in audio file)
- Write-to-temp-then-rename for corruption-safe file writes
- Inline DB + FTS5 index update (no rescan needed after edits)
- Format support: MP3 (ID3v2), FLAC (Vorbis Comments), OGG (Vorbis Comments)

**"Done" criteria:** Users can select tracks, edit metadata fields, set cover art, save changes to the actual audio files, and see updates reflected immediately in all views and search — without requiring a library rescan.

## Context

**Current state (v1.1 shipped 2026-03-16):**
- Go 1.25, Wails v2.10.2, Lit 3.2.1, SQLite via modernc.org/sqlite
- ~27,700 Go LOC + ~28,800 TypeScript LOC + ~1,200 SQL LOC
- ~15 backend packages, ~22 frontend components, 7 DB migrations
- Strict linting (golangci-lint v2) and TypeScript strict mode
- 84+ unit tests covering queue, config, player, database, library, migration packages
- Multi-library architecture: libraries table, library_id FK, ScanHooks/RemovalHooks/RescanHooks callback patterns
- SQL: track_metadata VIEW, sqlc-generated + hand-crafted with SAFETY comments, ByLibrary query variants
- Frontend: design token system, virtual scrolling, view caching, event delegation, library filter state
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

---
*Last updated: 2026-03-16 after v1.2 Tag Editing milestone started*

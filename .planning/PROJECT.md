# YellowJacket

## What This Is

YellowJacket is a cross-platform desktop music player built with Go (Wails v2) and TypeScript (Lit Web Components). It plays local music files (MP3, FLAC, OGG, WAV), manages a music library via SQLite, and provides queue management, playlists, cover art, and MPRIS media controls on Linux. The v1.0 Consolidation milestone strengthened the foundation — all known concurrency races are fixed, error handling is honest, SQL patterns are consolidated, performance bottlenecks are resolved, the frontend follows a consistent design language, and 84 unit tests provide a safety net for future work.

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
- ✓ Multiple library directory support — existing
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

### Active

- [ ] Tag editing — edit track metadata (title, artist, album, etc.) from within the app
- [ ] Scan cancellation — cancel in-progress library scans
- [ ] Smart playlists — auto-generated playlists with simple filter rules (genre, year, play count, etc.)
- [ ] Customizable keyboard shortcuts — configurable key bindings for common player actions
- [ ] Gapless playback + crossfade — seamless track transitions with optional crossfade setting
- [ ] MusicBrainz browser — read-only catalog browsing (artists, discographies, album editions, track listings)
- [ ] Layout customization system — section-based UI customization, components declare size constraints, users configure per-section
- [ ] Plugin system — full-access API for UI components and backend hooks, extensibility foundation

### Out of Scope

- Tag writing (track metadata editing) — feature work, not consolidation
- Scan cancellation — feature work, deferred to future milestone
- Cross-platform media controls (macOS/Windows) — feature work
- Database health checking / reconnection — low priority, desktop app context
- File decomposition for its own sake — only extract when it enables reuse or fixes problems
- ORM or query builder — would fight existing sqlc architecture
- Connection pooling for SQLite — meaningless with SetMaxOpenConns(1)

## Current Milestone: v1.1 Features & Extensibility

**Goal:** Add core missing features and build the foundations for a customizable, extensible music player.

**Target features:**
- Tag editing (track metadata editing from within the app)
- Scan cancellation (cancel in-progress library scans)
- Smart playlists (simple filter rules — genre, year, play count, etc.)
- Customizable keyboard shortcuts (configurable key bindings)
- Gapless playback + crossfade (seamless transitions, optional crossfade)
- MusicBrainz browser (read-only catalog: artists, discographies, album editions)
- Layout customization system (MusicBee-style section-based UI configuration)
- Plugin system (full-access API for UI + backend extensibility)

**"Done" criteria:** Core features complete and working. Big features (layout, plugins, MusicBrainz) have working foundations — functional but not necessarily feature-complete.

## Context

**Current state (v1.0 shipped 2026-03-05):**
- Go 1.25, Wails v2.10.2, Lit 3.2.1, SQLite via modernc.org/sqlite
- ~22,450 Go LOC + ~28,600 TypeScript LOC + ~5,200 Go test LOC
- ~15 backend packages, ~20 frontend components
- Strict linting (golangci-lint v2) and TypeScript strict mode
- 84 unit tests covering queue, config, player, database, library packages
- All concurrency races fixed, app runs clean under `-race`
- SQL consolidated: track_metadata VIEW, sqlc.slice(), SAFETY comments
- Frontend: design token system, virtual scrolling with stable keys, debounced store notifications
- Player tests still require hardware (skipped in CI)
- No frontend unit tests (deferred to v2)

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

---
*Last updated: 2026-03-06 after v1.1 milestone start*

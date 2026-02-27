# YellowJacket — Consolidation Milestone

## What This Is

YellowJacket is a cross-platform desktop music player built with Go (Wails v2) and TypeScript (Lit Web Components). It plays local music files (MP3, FLAC, OGG, WAV), manages a music library via SQLite, and provides queue management, playlists, cover art, and MPRIS media controls on Linux. This milestone focuses on strengthening the existing foundation — correctness, performance, code quality, UX polish, and test coverage — before adding new features.

## Core Value

The music player works reliably and feels solid. Every interaction is correct, responsive, and trustworthy — the foundation that all future features will build on.

## Requirements

### Validated

<!-- Existing capabilities confirmed working in the codebase. -->

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

### Active

<!-- Current scope: consolidation and quality improvements. -->

- [ ] Fix concurrency races in Queue, Library, and Playlist SetContext patterns
- [ ] Fix error handling gaps (swallowed errors in lifecycle callbacks, silent artist credit failures)
- [ ] Eliminate duplicated FTS5 JOIN query patterns across search functions
- [ ] Migrate raw SQL in queue persistence and search to sqlc-generated or type-safe queries
- [ ] Optimize library store to avoid eager full-library fetch on startup
- [ ] Optimize queue persistence to use incremental updates instead of full rewrites
- [ ] Fix SetQueue Phase 2 to skip already-resolved tracks from Phase 1
- [ ] Improve frontend rendering performance for large libraries
- [ ] Polish UI interactions — responsiveness, visual consistency, transitions
- [ ] Add unit tests for queue operations (SetQueue, navigation, shuffle, repeat, persistence)
- [ ] Add unit tests for library scan logic (metadata processing, entity cache, orphan cleanup)
- [ ] Add unit tests for database layer (FTS5 queries, migrations)
- [ ] Add unit tests for config (load/save roundtrip, validation, defaults)
- [ ] Extract testable pure logic from player (volume math, state serialization)
- [ ] Fix config file permissions (0o666 → 0o644)
- [ ] Address package-level startupErr variable (move to struct field)
- [ ] Add event name parity validation between Go and TypeScript

### Out of Scope

<!-- Explicit boundaries for this milestone. -->

- Tag writing (track metadata editing) — feature work, not consolidation
- Scan cancellation — feature work, deferred to future milestone
- Cross-platform media controls (macOS/Windows) — feature work
- Database health checking / reconnection — low priority, desktop app context
- New features of any kind — this milestone is purely about improving what exists
- File decomposition for its own sake — only extract when it enables reuse or fixes problems

## Context

YellowJacket is a personal project built by a single developer. The core music player functionality is complete and working. The developer uses the app daily and notices quality-of-life issues that accumulate. Before adding new features (which are planned but not yet scoped), the goal is to reach a confidence level where the foundation can be trusted.

**Codebase state (as of 2026-02-26):**
- Go 1.25, Wails v2.10.2, Lit 3.2.1, SQLite via modernc.org/sqlite
- ~15 backend packages, ~20 frontend components
- Strict linting (golangci-lint v2) and TypeScript strict mode
- No unit tests for queue, library, database, config packages
- Player tests require hardware (skipped in CI)
- No frontend tests
- Several known concurrency races (documented but not fixed)
- Performance bottlenecks identified in library loading and queue persistence
- Large frontend components (1400-2600 lines) with mixed concerns

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
| Consolidation before features | Technical debt compounds — fixing it now is cheaper than fixing it later under more code | — Pending |
| Tests support refactoring, not standalone goal | Testing is a means to safe refactoring, not a coverage target | — Pending |
| No cosmetic file splitting | Large files are only a problem if they cause real issues; extract only for reuse or correctness | — Pending |
| All improvement areas equal priority | Correctness, performance, code quality, UX, and testing are interdependent | — Pending |

---
*Last updated: 2026-02-27 after initialization*

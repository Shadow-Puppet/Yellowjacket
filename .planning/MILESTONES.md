# Milestones

## v1.0 Consolidation (Shipped: 2026-03-05)

**Phases completed:** 8 phases, 17 plans, 34 tasks
**Timeline:** 6 days (2026-02-27 → 2026-03-05)
**Stats:** 107 commits, 67 source files changed, +5,654/-465 lines, 84 tests added

**Delivered:** Strengthened the existing foundation — correctness, performance, code quality, UX polish, and test coverage — transforming YellowJacket from a working-but-fragile music player into a solid, trustworthy platform for future features.

**Key accomplishments:**
- Eliminated all concurrency races — 4 SetContext methods mutex-protected, app runs clean under `-race` detector
- Closed all error handling gaps — moved startupErr to struct, fixed config permissions, logged MPRIS errors, separated scan warnings from fatals
- Built comprehensive test suite — 84 new unit tests (queue, config, player, FTS5 search, library scan, entity cache) with shared in-memory test DB infrastructure
- Consolidated SQL and enforced code quality — `track_metadata` VIEW eliminating 60 lines of duplicated JOINs, `sqlc.slice()` migration, SAFETY comments on all 12 hand-crafted SQL statements, AST-based Go→TS event codegen
- Optimized backend performance — incremental queue persistence (O(1) add/remove), SetQueue Phase 2 dedup, deferred library loading for instant app shell
- Polished frontend performance and UX — queueMicrotask notification coalescing, design token system, classMap directives, visual consistency audit across all 15 components

**Archive:** [v1.0-ROADMAP.md](milestones/v1.0-ROADMAP.md) | [v1.0-REQUIREMENTS.md](milestones/v1.0-REQUIREMENTS.md)

---


## v1.1 Multi-Library Support (Shipped: 2026-03-16)

**Phases completed:** 6 phases, 18 plans
**Timeline:** 10 days (2026-03-06 → 2026-03-16)
**Stats:** ~85 commits, ~57,700 LOC (27.7K Go + 28.8K TS + 1.2K SQL), 31 requirements fulfilled

**Delivered:** Transformed YellowJacket from a single-directory player into a multi-library music manager — users can add, rename, and remove library directories through the UI, scan them independently, filter all views to a specific library, and playlists gracefully survive library removal with phantom track preservation and auto-resolution.

**Key accomplishments:**
- Cancellable/pausable library scans with per-scan context cancellation and sequential queue coordination
- Configurable keyboard shortcuts with record-style capture UI, scope-aware dispatch, and conflict detection
- Multi-library database schema (migration 6) with seamless single-directory migration preserving all user data
- Per-library scan pipeline with scan queue, progress UI per library, and cancel scope (single vs all)
- Full library CRUD API with 17-step atomic removal (orphan cleanup, phantom metadata, FTS5, cover art, queue compaction)
- Library filter dropdown in top bar — all views (tracks, albums, artists, genres, search) respect the active filter
- Cross-library playlists with phantom track auto-resolution via ScanHooks + M3U8 path matching
- Performance optimization: CSS containment, view caching, event delegation, content-visibility, scroll polish

**Archive:** [v1.1-ROADMAP.md](milestones/v1.1-ROADMAP.md) | [v1.1-REQUIREMENTS.md](milestones/v1.1-REQUIREMENTS.md)

---


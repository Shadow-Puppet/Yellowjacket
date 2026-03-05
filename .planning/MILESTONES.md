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


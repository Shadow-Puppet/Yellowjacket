# Project Research Summary

**Project:** YellowJacket — Desktop Music Player Consolidation
**Domain:** Go/Wails/Lit desktop application — codebase quality & reliability improvement
**Researched:** 2026-02-27
**Confidence:** HIGH

## Executive Summary

YellowJacket is a Go/Wails/Lit desktop music player with a functional feature set but known correctness issues: three data races in `SetContext` patterns, swallowed errors throughout the backend, zero test coverage on critical paths (queue, library, database, config), and O(n) queue persistence for single-track mutations. The consolidation milestone is not about new features — it's about making the existing codebase reliable, testable, and performant. The existing stack (Go 1.25, modernc.org/sqlite, beep/v2, Lit 3, sqlc) is correct and should not change. The work is purely internal quality improvement.

The recommended approach is **tests-first, then refactoring**. The research consistently shows that every optimization and consolidation change (FTS5 query deduplication, queue incremental persistence, lazy library loading) is risky without tests to verify behavior is preserved. The critical dependency chain is: fix concurrency bugs → build test infrastructure → write tests → refactor safely. This ordering emerges independently from all four research files — STACK recommends in-memory SQLite testing, FEATURES shows test infrastructure as the top enabler, ARCHITECTURE proposes the same phase ordering, and PITFALLS warns that every refactoring without tests creates invisible regressions.

The key risks are: (1) deadlock from player mutex + speaker lock ordering violations during refactoring, (2) FTS5 query consolidation silently changing search ranking, and (3) queue persistence migration losing queue state on restart. All three are mitigated by the same strategy: write characterization tests before changing the code. The player's lock ordering (`p.mu` before `speaker.Lock()`, goroutine dispatch in beep callback) is the one area requiring extreme caution — the recommendation is to extract pure testable logic and leave lock-sensitive paths alone unless absolutely necessary.

## Key Findings

### Recommended Stack

The existing stack is correct. No changes needed. See [STACK.md](./STACK.md) for full details.

**Core technologies (all already in use):**
- **Go 1.25 + modernc.org/sqlite v1.45**: Pure-Go SQLite driver with WAL mode, `SetMaxOpenConns(1)` — correct setup, needs missing PRAGMAs (`synchronous=NORMAL`, `cache_size=-8000`, `mmap_size=67108864`)
- **beep/v2 + ebitengine/oto**: Audio playback with streamer composition — lock ordering documented, goroutine dispatch pattern critical
- **Lit 3 + @lit-labs/virtualizer**: Web components with virtual scrolling — already handles large lists, needs lazy loading instead of eager fetch
- **sqlc v1.30**: Type-safe SQL code generation — works well for standard queries, FTS5 queries must remain hand-crafted
- **golangci-lint v2, lefthook, govulncheck**: Already configured, no changes needed

**Critical version note:** Match modernc.org/libc version exactly per upstream warning when updating modernc.org/sqlite.

### Expected Features

This is a consolidation milestone — "features" are quality improvements, not user-facing functionality. See [FEATURES.md](./FEATURES.md) for full details.

**Must fix (table stakes — codebase is unreliable without these):**
- Fix 3 SetContext data races (Queue, Library, Playlist) — textbook race, LOW effort
- Fix package-level `startupErr` → struct field — LOW effort
- Fix config file permissions (0o666 → 0o644) — one-line fix
- Fix swallowed errors in MPRIS callbacks and artist credit links — LOW effort
- Separate scan warnings from fatal errors in Library.Scan — MEDIUM effort
- Create in-memory SQLite test infrastructure (`database.NewTestDB()`) — MEDIUM effort, enables everything else
- Write unit tests for queue, library, database, config — HIGH effort, critical safety net

**Should do (significant quality improvement):**
- Consolidate duplicated FTS5 JOIN pattern (5+ copies → SQLite VIEW) — MEDIUM effort
- Optimize queue persistence to incremental updates — MEDIUM effort
- Remove `eagerFetch()` from library store constructor (lazy loading infrastructure already exists) — LOW effort
- Fix SetQueue Phase 2 redundant metadata lookups — LOW effort
- Add event name parity validation (Go ↔ TypeScript) — LOW effort
- Extract testable pure logic from Player (volume math, state serialization) — LOW effort

**Defer (not this milestone):**
- Frontend component testing (expensive setup, backend is source of truth)
- Paginated data providers for 100k+ libraries (measure first)
- Full UI polish / transitions (CSS-only, independent)
- Rewriting the event system (works fine, just needs codegen parity check)

### Architecture Approach

The architecture is sound and shouldn't change structurally. The consolidation work is about fixing correctness issues within the existing patterns and adding test infrastructure. See [ARCHITECTURE.md](./ARCHITECTURE.md) for full details.

**Six issues identified, in dependency order:**
1. **SetContext race fixes** — Add mutex guards to Queue, Library, Playlist `SetContext()`. Combine Player's double-lock into single acquisition. Move `startupErr` to struct field.
2. **Event name codegen** — Generate `frontend/src/events.ts` from `backend/events/events.go` using `go/ast`. Wire into `go generate` + pre-commit hook.
3. **Library store lazy loading** — Remove `eagerFetch()` from constructor. Lazy infrastructure already exists. Optional: paginated data providers for 100k+ libraries.
4. **Queue incremental persistence** — Use existing sqlc queries (`InsertQueueTrack`, `RemoveQueueTrackByPosition`, etc.) for single-track operations. Keep full rewrite for `SetQueue`/`Clear`.
5. **FTS5 query consolidation** — Create SQLite VIEW `track_metadata` encapsulating the 5-table JOIN. Migrate search queries to use VIEW. Keep inline JOINs in migrations.
6. **Test architecture** — `database.NewTestDB()` for in-memory SQLite. `internal/testdb` helper package. Mock only narrow interfaces (`TrackLoader`). Use `context.Background()` for Wails context in tests.

### Critical Pitfalls

Top 5 from [PITFALLS.md](./PITFALLS.md), ordered by severity:

1. **Refactoring concurrency without tests creates invisible regressions** — Write characterization tests BEFORE fixing races. Fix `SetContext` first (lowest risk), Player last (most complex). The race detector is the oracle.
2. **Player deadlock from mutex + speaker lock ordering violation** — NEVER remove the `go p.onPlaybackFinished()` goroutine dispatch. NEVER refactor player lock code without drawing the full lock acquisition graph. Extract pure logic; leave lock-sensitive paths alone.
3. **FTS5 query consolidation breaks search ranking** — Write search tests BEFORE consolidating. Consolidate the JOIN clause only, not full queries. Verify `COALESCE` behavior is identical across all copies.
4. **Queue persistence migration loses queue state** — New persistence code must read old format. Test old-write → new-read compatibility. Keep full rewrite as fallback for complex operations.
5. **SQLite in-memory tests behave differently from file-based production** — Test helper must mirror production `NewDB()` exactly: same PRAGMAs, same migration sequence, `PRAGMA foreign_keys = ON`. Use `t.TempDir()` for file-based tests when WAL behavior matters.

## Implications for Roadmap

Based on dependency analysis across all four research files, with convergent recommendations:

### Phase 1: Correctness Fixes & Test Foundation

**Rationale:** Every other phase depends on either the concurrency fixes (to unblock `-race`-clean tests) or the test infrastructure (to safely refactor). This is the critical enabler. All four research files independently recommend this as the first step.

**Delivers:** Race-free `SetContext` in all packages, `startupErr` moved to struct, config permissions fixed, swallowed errors surfaced, in-memory SQLite test helper, event name codegen, extracted testable player logic.

**Features addressed:** All "Must fix" table stakes items + test infrastructure.

**Pitfalls avoided:** Pitfall 1 (concurrency without tests), Pitfall 2 (in-memory test divergence), Pitfall 5 (config migration failures via roundtrip test).

**Estimated items:** ~10 discrete changes, all LOW-MEDIUM effort individually.

### Phase 2: Core Test Suite

**Rationale:** With concurrency fixed and test infrastructure in place, write the safety net that protects all subsequent refactoring. Tests target the code AS IT IS (characterization tests), not as it will be after optimization.

**Delivers:** Queue unit tests (~15-20), database/search tests (~10-15), config roundtrip tests (~8-10), player pure logic tests (~5-8), event parity test (1). Approximately 40-55 tests total.

**Features addressed:** All test coverage items from FEATURES.md.

**Pitfalls avoided:** Pitfall 1 (provides the safety net), Pitfall 4 (search tests before consolidation), Pitfall 6 (queue persistence tests before optimization).

**Estimated effort:** HIGH — this is the largest phase by work volume, but it's the foundation for everything else.

### Phase 3: SQL & Performance Optimization

**Rationale:** With tests as a safety net, refactor the SQL layer and persistence. Schema changes (VIEW creation) should precede query pattern changes. Queue persistence optimization uses existing but unwired sqlc queries.

**Delivers:** Deduplicated FTS5 queries via SQLite VIEW, incremental queue persistence for add/remove operations, SetQueue Phase 2 redundant lookup fix, scan warnings separated from fatal errors.

**Features addressed:** FTS5 consolidation, queue persistence optimization, SetQueue Phase 2 fix, scan error separation.

**Pitfalls avoided:** Pitfall 3 (FTS5 consolidation verified by Phase 2 tests), Pitfall 6 (queue persistence verified by Phase 2 tests).

**Estimated effort:** MEDIUM — changes are well-scoped and verified by existing tests.

### Phase 4: Frontend Performance & Polish

**Rationale:** Frontend changes are independent of backend refactoring and lowest risk. The library store lazy loading is nearly zero-effort (removing code, not adding it). UI polish is last because it's the lowest priority for a consolidation milestone.

**Delivers:** Lazy library loading (remove `eagerFetch()`), optimized re-renders with `repeat()` directive and stable keys, documentation of intentional exceptions (hand-crafted SQL, singleton store lifecycle).

**Features addressed:** Library store lazy loading, frontend rendering optimization, documentation.

**Pitfalls avoided:** Pitfall 5 (eager-to-lazy UX regression — mitigate by keeping eager for default view, audit all `getCached*` call sites).

**Estimated effort:** LOW-MEDIUM — mostly removing code and CSS changes.

### Phase Ordering Rationale

- **Phase 1 → Phase 2:** You cannot write `-race`-clean tests without fixing the SetContext races first. Test infrastructure (`NewTestDB`) must exist before any DB-dependent tests.
- **Phase 2 → Phase 3:** Refactoring SQL and persistence without tests is the #1 pitfall identified by research. The tests characterize current behavior, then the refactoring is verified against them.
- **Phase 3 → Phase 4:** Frontend changes don't depend on backend refactoring, but doing them last means the backend API is stable. The SQLite VIEW from Phase 3 doesn't affect the frontend.
- **Within Phase 1:** SetContext fixes → test helper → event codegen (independent items, can be parallelized).
- **Within Phase 3:** SQL VIEW creation → query migration → queue persistence (schema before queries before consumers).

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 2 (Core Test Suite):** The queue test architecture needs careful design — mock player interface, test data seeding patterns, event verification strategy. `/gsd-research-phase` recommended for the queue test design.
- **Phase 3 (SQL Optimization):** sqlc's handling of SQLite VIEWs with FTS5 virtual tables needs validation. The VIEW concept is sound but edge cases in sqlc's SQLite parser are unknown. Quick validation needed before committing to VIEW approach.

Phases with standard patterns (skip research-phase):
- **Phase 1 (Correctness Fixes):** All fixes are mechanical (add lock, move field, fix permissions). Well-documented Go patterns.
- **Phase 4 (Frontend):** Removing `eagerFetch()` is a one-line change. Lit `repeat()` directive is well-documented.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All recommendations come from official docs (SQLite, Go stdlib, Lit, beep). Existing stack is correct; only PRAGMAs need addition. |
| Features | HIGH | All improvements grounded in direct codebase analysis + CONCERNS.md. Priority ordering validated by dependency analysis across all research files. |
| Architecture | HIGH | Patterns from Go stdlib, sqlc official docs. One MEDIUM area: sqlc VIEW support for SQLite needs validation. |
| Pitfalls | HIGH | All pitfalls derived from actual code paths (lock ordering, FTS5 duplication, persistence pattern). Recovery strategies are concrete. |

**Overall confidence:** HIGH

### Gaps to Address

- **sqlc + SQLite VIEW + FTS5 compatibility:** MEDIUM confidence that sqlc correctly parses queries against VIEWs that JOIN with FTS5 virtual tables. Validate during Phase 3 planning — if it doesn't work, fall back to Go string constant for the JOIN clause.
- **`@lit-labs/signals` stability:** Used for signal-based reactivity in the frontend. Experimental API (v0.2.0) may change. Not blocking for consolidation but worth noting for future milestones.
- **Library scan test fixtures:** Testing the library scan requires audio file fixtures or a mock filesystem. `testing/fstest.MapFS` may not be sufficient for the metadata parsing paths. May need real (tiny) audio files as test fixtures. Validate during Phase 2 planning.
- **Lazy loading measurement:** The recommendation to remove `eagerFetch()` is based on architecture analysis, not profiling data. Before Phase 4, measure actual startup time with a large library to confirm lazy loading is beneficial.

## Sources

### Primary (HIGH confidence)
- SQLite WAL documentation: https://www.sqlite.org/wal.html
- SQLite PRAGMA documentation: https://www.sqlite.org/pragma.html
- modernc.org/sqlite API: https://pkg.go.dev/modernc.org/sqlite@v1.46.1
- Go race detector: https://go.dev/doc/articles/race_detector
- gopxl/beep wiki: https://github.com/gopxl/beep/wiki/Composing-and-controlling
- Lit rendering docs: https://lit.dev/docs/components/rendering/
- Lit repeat directive: https://lit.dev/docs/templates/lists/#the-repeat-directive
- sqlc official docs: https://docs.sqlc.dev/en/stable/
- Codebase analysis: `.planning/codebase/CONCERNS.md`, `.planning/codebase/STACK.md`
- Direct code inspection of all backend and frontend source files

### Secondary (MEDIUM confidence)
- beep speaker.Lock() behavior — inferred from beep wiki + codebase lock ordering comments
- sqlc VIEW support for SQLite — documented for PostgreSQL, inferred for SQLite
- Wails v2 binding generation and event system limitations — based on codebase patterns

---
*Research completed: 2026-02-27*
*Ready for roadmap: yes*

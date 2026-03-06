# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.0 — Consolidation

**Shipped:** 2026-03-05
**Phases:** 8 | **Plans:** 17 | **Tasks:** 34
**Timeline:** 6 days (2026-02-27 → 2026-03-05)

### What Was Built
- Race-free concurrency across all 4 SetContext entry points
- Honest error handling: startupErr to struct, config permissions, MPRIS logging, scan warning separation
- 84 unit tests covering queue, config, player, FTS5 search, library scan, entity cache
- SQL consolidation: track_metadata VIEW, sqlc.slice() migration, SAFETY comments on 12 hand-crafted queries
- AST-based Go→TypeScript event codegen with pre-commit enforcement
- Incremental queue persistence (O(1) add/remove) and SetQueue Phase 2 dedup
- Deferred library store loading for instant app shell
- Frontend design token system, classMap directives, queueMicrotask coalescing
- Visual consistency audit across all 15 components

### What Worked
- **Dependency-ordered phases:** Fixing races → building test infra → writing tests → refactoring → performance → UX created a clean progression where each phase built on the last
- **Characterization tests before refactoring:** Writing tests in Phase 4-5 before SQL consolidation in Phase 6 caught zero regressions — the tests were accurate safety nets
- **Small, focused plans:** 2-3 tasks per plan kept execution fast and context fresh — most plans completed in under 10 minutes
- **Research phase for SQL consolidation:** Phase 6 research validated sqlc + VIEW + FTS5 compatibility before planning, avoiding mid-execution discovery
- **Internal package tests:** Testing queue/library as package-internal (not `_test` suffix) gave access to unexported fields for thorough state verification

### What Was Inefficient
- **Phase 8 repeat() regression:** Migrating virtualizers to `repeat()` directive in Plan 02 broke virtualization (repeat as child content bypasses lit-virtualizer's DOM management). Required a hotfix (72ef719) reverting to `.renderItem` + `.keyFunction`. Research should have caught this API distinction.
- **Task count tracking:** STATE.md only tracked tasks-per-plan for later phases (5-8), making total task count harder to derive at milestone completion
- **No startup time measurement:** TODO to measure startup time before Phase 7 lazy loading was never done — can't quantify the improvement

### Patterns Established
- **Mutex-protected setter pattern:** Lock → write field → release lock → call callbacks (prevents deadlock from callback re-entry)
- **ScanWarning + addWarning pattern:** Mutex-protected warning collection for non-fatal errors during long-running operations
- **applyPRAGMAs shared function:** Single source of truth for SQLite PRAGMAs, shared between production NewDB and test NewTestDB
- **SAFETY comment convention:** Two-part format (why + safety assurance) for hand-crafted SQL that bypasses sqlc
- **AST-based codegen over regex:** go/ast + go/parser for cross-language constant synchronization
- **Design token CSS custom properties:** `--yj-icon-sm/md/lg`, `--yj-text-xs/sm/md/lg/xl` scoped to `:host` in Lit components
- **queueMicrotask coalescing:** Batch multiple synchronous store notifications into single subscriber update

### Key Lessons
1. **Test the API contract, not the implementation surface:** repeat() inside lit-virtualizer looks correct syntactically but violates the component's rendering contract. Always verify how a library expects to be consumed, not just what compiles.
2. **Research before planning pays off immediately:** Phase 6 research confirmed sqlc + VIEW compatibility, saving mid-execution discovery and potential re-planning.
3. **Incremental persistence is O(complexity) not O(code):** The incremental queue persistence (Phase 7) was conceptually simple but required careful position-shift SQL for insert/remove operations — more thought than code.
4. **Design tokens must precede visual consistency work:** Phase 8 correctly defined tokens in Plan 01 before applying them in Plan 04 — reversing this order would have required double work.
5. **Contentless FTS5 has deletion limitations:** Cannot DELETE from tables with `content=''`. Document this in tests rather than fighting it — stale entries are harmless for the use case.

### Cost Observations
- Model mix: Primarily opus for planning + execution, sonnet for research
- Total commits: 107 across 6 days
- Notable: Plans averaging 2-6 minutes execution time; Phase 8 Plan 04 (visual audit across 15 components) was the longest at 8 minutes
- Efficiency: 17 plans × ~5 min avg = ~85 min total execution time for 34 tasks across 67 source files

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Days | Phases | Plans | Key Change |
|-----------|------|--------|-------|------------|
| v1.0 | 6 | 8 | 17 | First milestone — established GSD workflow, research-before-plan pattern |

### Cumulative Quality

| Milestone | Tests Added | Total Tests | Key Quality Win |
|-----------|-------------|-------------|-----------------|
| v1.0 | 84 | 84 | From 0 backend tests to comprehensive coverage of queue, config, player, database, library |

### Top Lessons (Verified Across Milestones)

1. Dependency-ordered phases (fix → test → refactor → optimize) prevent rework and ensure each phase builds on a stable foundation
2. Small plans (2-3 tasks, <10 min) maintain consistent quality — no context degradation
3. Research phases for unfamiliar domains (sqlc + VIEW, lit-virtualizer API) prevent mid-execution surprises

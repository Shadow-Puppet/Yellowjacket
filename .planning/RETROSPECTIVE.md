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

## Milestone: v1.1 — Multi-Library Support

**Shipped:** 2026-03-16
**Phases:** 6 | **Plans:** 18
**Timeline:** 10 days (2026-03-06 → 2026-03-16)

### What Was Built
- Cancellable/pausable library scans with per-scan context cancellation and sequential queue coordination
- Configurable keyboard shortcuts with record-style capture UI, scope-aware dispatch, and conflict detection
- Multi-library database schema (migration 6) with seamless single-directory migration
- Per-library scan pipeline with scan queue, per-library progress UI, and cancel scope
- Full library CRUD API with 17-step atomic removal (orphan cleanup, phantom metadata, FTS5, cover art, queue compaction)
- Library filter dropdown — all views (tracks, albums, artists, genres, search) respect active filter
- Cross-library playlists with phantom track auto-resolution via ScanHooks + M3U8 path matching
- Performance: CSS containment, view caching, event delegation, content-visibility, scroll polish

### What Worked
- **4-phase multi-library progression (schema → scan → CRUD → views):** Each phase had clear boundaries and verifiable outputs. Schema first meant scan pipeline had stable types; scan pipeline meant CRUD had working add-then-scan; CRUD meant views could demonstrate the full lifecycle.
- **Locked decisions from /gsd-discuss-phase:** "Backend filtering, not frontend" and "SET NULL for playlist_tracks FK" were decided once and never revisited — eliminated mid-execution design debates.
- **Performance phase running in parallel:** Phase 14 (performance) was independent of the multi-library phases (10-13), allowing it to execute when multi-library phases were blocked on human verification.
- **Checkpoint-driven bugfinding:** The human-verify checkpoint in Phase 13 found 3 bugs (virtualizer event delegation race, missing phantom auto-resolution, M3U8-based resolution needed) that wouldn't have been caught by automated verification alone.
- **Hook patterns for cross-package communication:** ScanHooks, RemovalHooks, and RescanHooks cleanly broke circular dependencies between library, playlist, and queue packages without coupling.

### What Was Inefficient
- **Phantom resolution required 3 iterations:** First attempt (pure SQL with phantom_file_path) missed pre-existing phantoms. Second attempt (backfill) was fragile. Third attempt (M3U8-based ScanHooks) was the right approach from the start. Should have analyzed the M3U8 data flow before designing the resolution.
- **Phase 14 virtualizer bug surfaced late:** The event delegation race condition from Phase 14-03 wasn't caught until Phase 13's checkpoint. The Phase 14 verification should have included testing with empty-then-loaded data states.
- **Quick task 19 (phantom path resolution) overlapped with Phase 13:** The fix for multi-root path resolution in playlists was done as a quick task but directly related to Phase 13's phantom track work. Could have been folded into Phase 13 planning.

### Patterns Established
- **ScanHooks callback pattern:** Post-scan processing without circular imports — library calls hook, playlist implements
- **ByLibrary query variants:** Parallel filtered/unfiltered sqlc queries with conditional dispatch in store layer
- **phantom_file_path column:** Preserves original file path at removal time for future re-linking
- **M3U8 as source of truth for phantom matching:** Position-based + path-based dual matching strategy
- **View caching with display:none toggle:** Keeps DOM alive for instant navigation, bounded cache (6 entries)
- **Event delegation via data-index + closest():** Zero per-item closures in virtualizer renderItem functions
- **attachDelegation guard pattern:** Retry event delegation in updated() for conditionally-rendered elements
- **changeGeneration counter:** Simple monotonic counter replaces typed subscription system for store change detection

### Key Lessons
1. **Analyze data flow before designing resolution strategies:** The phantom track resolution should have started with "what data do we have?" (M3U8 files have the paths) rather than "where can we store new data?" (phantom_file_path column). The M3U8 approach was simpler and more robust.
2. **Human checkpoints catch integration bugs that automated tests miss:** The virtualizer race condition and phantom auto-resolution gap were both found during manual testing, not by build/lint/verify. Budget for checkpoint time.
3. **Hook patterns scale well for cross-cutting concerns:** ScanHooks, RemovalHooks, and RescanHooks all follow the same pattern — define struct with function fields, set via method, call at lifecycle points. This pattern can be reused for future cross-package coordination.
4. **Conditional rendering + lifecycle hooks need careful testing:** Components that conditionally render children (lit-virtualizer appears only when data loads) must handle the case where firstUpdated fires before the child exists. Test with both fast and slow data loading.
5. **Performance optimization and feature work can truly run in parallel:** Phase 14 had zero file conflicts with Phases 10-13 and was executed out of order. Independent subsystem identification at planning time enables this parallelism.

### Cost Observations
- Model mix: Primarily opus for planning + execution, sonnet for verification
- Total commits: ~85 across 10 days
- Notable: Most plans completed in 2-10 minutes. Phase 12-02 (frontend library management UI) was the longest at 38 minutes due to complexity (19 files, 3 tasks, new components)
- Efficiency: 18 plans across 6 phases with 4 quick tasks interleaved

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Days | Phases | Plans | Key Change |
|-----------|------|--------|-------|------------|
| v1.0 | 6 | 8 | 17 | First milestone — established GSD workflow, research-before-plan pattern |
| v1.1 | 10 | 6 | 18 | Locked decisions, parallel phase execution, hook patterns for cross-package coordination |

### Cumulative Quality

| Milestone | Tests Added | Total Tests | Key Quality Win |
|-----------|-------------|-------------|-----------------|
| v1.0 | 84 | 84 | From 0 backend tests to comprehensive coverage of queue, config, player, database, library |
| v1.1 | ~5 | ~89 | Migration tests, multi-root path resolution tests; human checkpoint caught 3 integration bugs |

### Top Lessons (Verified Across Milestones)

1. Dependency-ordered phases (fix → test → refactor → optimize; schema → scan → CRUD → views) prevent rework and ensure each phase builds on a stable foundation
2. Small plans (2-3 tasks, <10 min) maintain consistent quality — no context degradation
3. Research phases for unfamiliar domains (sqlc + VIEW, lit-virtualizer API) prevent mid-execution surprises
4. Human checkpoints catch integration bugs that automated verification misses — budget time for them
5. Analyze existing data flows before designing new storage — the simplest solution often uses data that already exists

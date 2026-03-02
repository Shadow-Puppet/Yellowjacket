# Roadmap: YellowJacket Consolidation

**Created:** 2026-02-27
**Depth:** Comprehensive
**Phases:** 8
**Requirements:** 26/26 mapped

## Phases

- [x] **Phase 1: Concurrency Race Fixes** — Eliminate all SetContext data races across Queue, Library, Playlist, and Player
- [ ] **Phase 2: Backend Correctness** — Fix error handling gaps, file permissions, package-level state, and scan error separation
- [ ] **Phase 3: Test Infrastructure** — Create in-memory SQLite test helper and apply production SQLite PRAGMAs
- [ ] **Phase 4: Queue, Config & Player Tests** — Write unit tests for queue operations, config roundtrip, and extracted player pure logic
- [ ] **Phase 5: Database & Library Tests** — Write unit tests for FTS5 search queries, migrations, library scan, and entity cache
- [ ] **Phase 6: SQL Consolidation & Code Quality** — Deduplicate FTS5 queries via VIEW, add event codegen, migrate to sqlc where feasible, document exceptions
- [ ] **Phase 7: Backend Performance** — Optimize queue persistence, fix SetQueue Phase 2 redundancy, enable lazy library loading
- [ ] **Phase 8: Frontend Performance & UX** — Optimize frontend rendering for large libraries and fix visual inconsistencies

## Phase Details

### Phase 1: Concurrency Race Fixes
**Goal:** All SetContext patterns across the codebase are race-free and the app can run under `-race` without data race reports
**Depends on:** Nothing (first phase)
**Requirements:** CORR-01, CORR-02, CORR-03, CORR-04
**Success Criteria** (what must be TRUE):
  1. Running the app with `go test -race` produces zero data race reports for SetContext calls in queue, library, playlist, and player packages
  2. Queue.SetContext(), Library.SetContext(), and Playlist.Service.SetContext() each acquire their mutex before writing the ctx field
  3. Player.SetContext() uses a single lock acquisition instead of the double-lock pattern
  4. Concurrent calls to SetContext from multiple goroutines do not corrupt shared state
**Plans:** 1 plan
Plans:
- [x] 01-01-PLAN.md — Add mutex protection to all SetContext methods and collapse Player double-lock

### Phase 2: Backend Correctness
**Goal:** All known error handling gaps are closed, configuration is secure, and the backend reports problems honestly instead of swallowing them
**Depends on:** Phase 1 (race-free code is prerequisite for reliable error paths)
**Requirements:** CORR-05, CORR-06, CORR-07, CORR-08, CORR-09
**Success Criteria** (what must be TRUE):
  1. The package-level `startupErr` variable no longer exists; startup errors are stored in a YellowJacketApp struct field
  2. Config files are written with 0o644 permissions (owner read/write, group/other read-only)
  3. MPRIS lifecycle callback errors (Pause, Seek) appear in the application log instead of being silently discarded
  4. Artist credit link creation checks the actual error — only UNIQUE constraint violations are ignored, all other errors are surfaced
  5. Library.Scan() returns warnings (skipped files, partial failures) in ScanMetrics and fatal errors (database failures) in the error return, so callers can distinguish between "scan completed with issues" and "scan failed"
**Plans:** 2 plans
Plans:
- [ ] 02-01-PLAN.md — Fix startupErr global state, config permissions, and MPRIS callback error logging
- [ ] 02-02-PLAN.md — Add IsUniqueViolation helper, migration 3, and separate scan warnings from fatal errors

### Phase 3: Test Infrastructure
**Goal:** A reliable, production-mirroring test foundation exists so that all subsequent test phases can write database-backed tests with confidence
**Depends on:** Phase 1 (race-free code required for `-race`-clean test runs), Phase 2 (correct error handling needed for accurate test assertions)
**Requirements:** TEST-01, PERF-04
**Success Criteria** (what must be TRUE):
  1. `database.NewTestDB(t)` returns a clean in-memory SQLite database that applies the same migrations and PRAGMAs as the production `NewDB()`
  2. Production SQLite connection applies `synchronous=NORMAL`, `cache_size=-8000`, and `mmap_size=67108864` PRAGMAs at database open
  3. Each test gets an isolated database instance — no shared state between test functions
  4. Tests using `NewTestDB` pass with `-race` flag enabled
**Plans:** TBD

### Phase 4: Queue, Config & Player Tests
**Goal:** The queue, config, and player packages have comprehensive unit tests that characterize current behavior and serve as a safety net for later refactoring
**Depends on:** Phase 3 (queue tests need NewTestDB for persistence tests)
**Requirements:** TEST-02, TEST-04, TEST-05
**Success Criteria** (what must be TRUE):
  1. Queue package has ~15-20 tests covering SetQueue, Next, Previous, shuffle mode, repeat modes (off, one, all), and state persistence across save/load cycles
  2. Config package has ~8-10 tests covering load/save roundtrip fidelity, validation rule enforcement, default value application, and graceful handling of missing or empty config files
  3. Player pure logic (UserVolume↔Volume conversion, state serialization/deserialization, format detection from file extension) is extracted into standalone functions with ~5-8 unit tests
  4. All tests in this phase pass with `-race` flag enabled
**Plans:** TBD

### Phase 5: Database & Library Tests
**Goal:** Database queries (especially FTS5 search) and library scan logic have unit tests that lock down current behavior before SQL consolidation and performance optimization
**Depends on:** Phase 3 (database tests need NewTestDB), Phase 4 (queue tests validate persistence patterns reused here)
**Requirements:** TEST-03, TEST-06
**Success Criteria** (what must be TRUE):
  1. Database package has ~10-15 tests covering FTS5 search (basic terms, empty query, special characters, multi-word), search index rebuild, and schema migration application
  2. Library scan logic has ~10-15 tests covering metadata extraction processing, entity cache hit/miss behavior, and orphan track cleanup
  3. FTS5 search tests verify that search ranking produces consistent, expected ordering for known test data
  4. All tests in this phase pass with `-race` flag enabled
**Plans:** TBD

### Phase 6: SQL Consolidation & Code Quality
**Goal:** Duplicated SQL patterns are eliminated, event names are provably synchronized between Go and TypeScript, and intentional SQL exceptions are documented
**Depends on:** Phase 5 (FTS5 search tests verify consolidation doesn't break ranking; database tests verify migration safety)
**Requirements:** QUAL-01, QUAL-02, QUAL-03, QUAL-04
**Success Criteria** (what must be TRUE):
  1. The duplicated 5-table FTS5 JOIN pattern is consolidated into a single SQLite VIEW (`track_metadata` or similar), and all search queries use the VIEW instead of inline JOINs
  2. A code generator reads Go event constants from `backend/events/events.go` and produces `frontend/src/events.ts`, wired into `go generate` and the pre-commit hook — adding an event in Go without regenerating TypeScript fails the hook
  3. Queue batch lookups in `persistence.go` use `sqlc.slice()` for IN clauses where sqlc supports it, replacing `fmt.Sprintf` placeholder construction
  4. Every hand-crafted SQL statement that intentionally bypasses sqlc has a `// SAFETY:` comment explaining why (batch INSERT, dynamic IN clauses, etc.)
**Plans:** TBD

### Phase 7: Backend Performance
**Goal:** Queue mutations and library loading are fast — single-track queue changes are O(1) instead of O(n), and the library doesn't block startup with a full data fetch
**Depends on:** Phase 4 (queue tests verify persistence optimization doesn't lose data), Phase 5 (library tests verify lazy loading doesn't break data access)
**Requirements:** PERF-01, PERF-02, PERF-03
**Success Criteria** (what must be TRUE):
  1. Adding or removing a single track from the queue uses incremental INSERT/DELETE via existing sqlc queries, not a full table rewrite
  2. SetQueue Phase 2 (`resolveRemainingTracks`) skips file paths that were already resolved in Phase 1, eliminating redundant database lookups
  3. Library store constructor no longer calls `eagerFetch()` — data loads lazily on first access via the existing `getTracks()`/`getAlbums()`/etc. getters, and the app starts without blocking on a full library load
**Plans:** TBD

### Phase 8: Frontend Performance & UX
**Goal:** The app feels smooth and visually consistent — large libraries render without jank, and the UI follows a coherent visual language
**Depends on:** Phase 7 (backend lazy loading changes the data availability pattern the frontend consumes)
**Requirements:** PERF-05, UX-01, UX-02
**Success Criteria** (what must be TRUE):
  1. Track and album lists use Lit `repeat()` directive with stable keys (filePath for tracks, albumId for albums) for efficient DOM reuse during scrolling and filtering
  2. Store notifications during rapid updates (e.g., library scan) are debounced via `queueMicrotask()` to prevent layout thrashing
  3. Visual inconsistencies (spacing, colors, typography, icon sizing) are audited and follow a consistent pattern across all components
  4. Scrolling, view switching, and search filtering in a 10k+ track library are smooth with no visible jank or dropped frames
**Plans:** TBD

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Concurrency Race Fixes | 1/1 | Complete | 2026-02-28 |
| 2. Backend Correctness | 0/2 | Planned | — |
| 3. Test Infrastructure | 0/? | Not started | — |
| 4. Queue, Config & Player Tests | 0/? | Not started | — |
| 5. Database & Library Tests | 0/? | Not started | — |
| 6. SQL Consolidation & Code Quality | 0/? | Not started | — |
| 7. Backend Performance | 0/? | Not started | — |
| 8. Frontend Performance & UX | 0/? | Not started | — |

---
*Roadmap created: 2026-02-27*
*Last updated: 2026-03-02*

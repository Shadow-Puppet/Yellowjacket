# Requirements: YellowJacket Consolidation

**Defined:** 2026-02-27
**Core Value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.

## v1 Requirements

Requirements for the consolidation milestone. Each maps to roadmap phases.

### Correctness

- [x] **CORR-01**: Queue.SetContext() acquires q.mu before writing q.ctx, eliminating the data race
- [x] **CORR-02**: Library.SetContext() and field setters (ctx, conf, rescanHooks) are protected by a mutex
- [x] **CORR-03**: Playlist.Service.SetContext() acquires lock before writing s.ctx, eliminating the data race
- [x] **CORR-04**: Player.SetContext() combines the double-lock pattern into a single lock acquisition
- [x] **CORR-05**: Package-level startupErr variable is moved to a YellowJacketApp struct field
- [x] **CORR-06**: Config file is written with 0o644 permissions instead of 0o666
- [x] **CORR-07**: MPRIS lifecycle callback errors (Pause, Seek) are logged instead of silently swallowed
- [x] **CORR-08**: Artist credit link creation error is checked; only UNIQUE constraint violations are ignored
- [x] **CORR-09**: Library.Scan() separates warnings from fatal errors — warnings returned in ScanMetrics, fatal errors in the error return

### Code Quality

- [x] **QUAL-01**: Duplicated FTS5 JOIN pattern (5+ copies) is consolidated into a single SQLite VIEW (track_metadata or similar)
- [x] **QUAL-02**: Event name constants are generated from Go source (backend/events/events.go) to TypeScript (frontend/src/events.ts) via codegen, wired into go generate and pre-commit hook
- [x] **QUAL-03**: Queue batch lookups in persistence.go use sqlc.slice() instead of fmt.Sprintf placeholder construction where feasible
- [x] **QUAL-04**: Intentional hand-crafted SQL exceptions (batch INSERT, dynamic IN clauses) are documented with // SAFETY: comments explaining why they bypass sqlc

### Performance

- [ ] **PERF-01**: Queue single-track mutations (add, remove) use incremental INSERT/DELETE via existing sqlc queries instead of full table rewrite
- [ ] **PERF-02**: SetQueue Phase 2 (resolveRemainingTracks) skips file paths already resolved in Phase 1, avoiding redundant database lookups
- [x] **PERF-03**: Library store constructor no longer calls eagerFetch(); data loads lazily on first access via existing getTracks()/getAlbums()/etc. getters
- [x] **PERF-04**: SQLite connection applies performance PRAGMAs (synchronous=NORMAL, cache_size=-8000, mmap_size=67108864) at database open
- [ ] **PERF-05**: Frontend track/album lists use Lit repeat() directive with stable keys (filePath/albumId) for efficient DOM reuse, and store notifications are debounced via queueMicrotask() during rapid updates

### Testing

- [x] **TEST-01**: In-memory SQLite test helper (database.NewTestDB) exists, applies same migrations and PRAGMAs as production NewDB, returns a clean DB per test
- [x] **TEST-02**: Queue package has unit tests covering SetQueue, Next, Previous, shuffle mode, repeat modes, and state persistence (~15-20 tests)
- [x] **TEST-03**: Database package has unit tests covering FTS5 search queries (basic, empty, special characters), search index rebuild, and schema migrations (~10-15 tests)
- [x] **TEST-04**: Config package has unit tests covering load/save roundtrip, validation rules, default application, and behavior with missing/empty config files (~8-10 tests)
- [x] **TEST-05**: Player pure logic (UserVolume-to-Volume conversion, state serialization, format detection) is extracted into testable functions with unit tests (~5-8 tests)
- [x] **TEST-06**: Library scan logic has unit tests covering metadata processing, entity cache behavior, and orphan cleanup (~10-15 tests)

### UX

- [ ] **UX-01**: Visual inconsistencies across components are audited and fixed (spacing, colors, typography, icon sizing follow a consistent pattern)
- [ ] **UX-02**: Frontend rendering for large libraries (10k+ tracks) is smooth — no jank during scrolling, view switching, or search filtering

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### UX

- **UX-V2-01**: UI transitions and responsive feedback — CSS transitions for panel open/close, list item hover states, loading skeletons

### Testing

- **TEST-V2-01**: Frontend unit tests for component-local logic (search ranking, column sorting, selection controller)
- **TEST-V2-02**: Integration tests with virtual audio device for player package

### Performance

- **PERF-V2-01**: Paginated data providers for libraries exceeding 100k+ tracks
- **PERF-V2-02**: Library store view-specific loading (only load data for active view, release inactive)

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Tag writing (track metadata editing) | Feature work, not consolidation |
| Scan cancellation | Feature work, deferred to future milestone |
| Cross-platform media controls (macOS/Windows) | Feature work, different milestone |
| Database health checking / reconnection | Low priority for desktop app with local SQLite |
| New user-facing features of any kind | This milestone is purely about improving what exists |
| File decomposition for line count | Only extract when it enables reuse or fixes problems |
| Full event system rewrite | Current system works; codegen parity check is sufficient |
| ORM or query builder | Would fight existing sqlc architecture |
| Frontend component testing framework | Expensive setup; backend is source of truth |
| Connection pooling for SQLite | Meaningless with SetMaxOpenConns(1) |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| CORR-01 | Phase 1: Concurrency Race Fixes | Complete |
| CORR-02 | Phase 1: Concurrency Race Fixes | Complete |
| CORR-03 | Phase 1: Concurrency Race Fixes | Complete |
| CORR-04 | Phase 1: Concurrency Race Fixes | Complete |
| CORR-05 | Phase 2: Backend Correctness | Complete |
| CORR-06 | Phase 2: Backend Correctness | Complete |
| CORR-07 | Phase 2: Backend Correctness | Complete |
| CORR-08 | Phase 2: Backend Correctness | Complete |
| CORR-09 | Phase 2: Backend Correctness | Complete |
| QUAL-01 | Phase 6: SQL Consolidation & Code Quality | Complete |
| QUAL-02 | Phase 6: SQL Consolidation & Code Quality | Complete |
| QUAL-03 | Phase 6: SQL Consolidation & Code Quality | Complete |
| QUAL-04 | Phase 6: SQL Consolidation & Code Quality | Complete |
| PERF-01 | Phase 7: Backend Performance | Pending |
| PERF-02 | Phase 7: Backend Performance | Pending |
| PERF-03 | Phase 7: Backend Performance | Complete |
| PERF-04 | Phase 3: Test Infrastructure | Complete |
| PERF-05 | Phase 8: Frontend Performance & UX | Pending |
| TEST-01 | Phase 3: Test Infrastructure | Complete |
| TEST-02 | Phase 4: Queue, Config & Player Tests | Complete |
| TEST-03 | Phase 5: Database & Library Tests | Complete |
| TEST-04 | Phase 4: Queue, Config & Player Tests | Complete |
| TEST-05 | Phase 4: Queue, Config & Player Tests | Complete |
| TEST-06 | Phase 5: Database & Library Tests | Complete |
| UX-01 | Phase 8: Frontend Performance & UX | Pending |
| UX-02 | Phase 8: Frontend Performance & UX | Pending |

**Coverage:**
- v1 requirements: 26 total
- Mapped to phases: 26
- Unmapped: 0

---
*Requirements defined: 2026-02-27*
*Last updated: 2026-02-27 after roadmap creation (traceability updated)*

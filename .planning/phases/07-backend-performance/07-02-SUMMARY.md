---
phase: 07-backend-performance
plan: 02
subsystem: ui
tags: [performance, startup, deferred-loading, dom-ready, wails]

# Dependency graph
requires:
  - phase: 06-sql-consolidation-code-quality
    provides: stable frontend store and library data access patterns
provides:
  - Deferred LibraryStore eagerFetch — app shell renders before backend data roundtrips
affects: [08-frontend-polish]

# Tech tracking
tech-stack:
  added: []
  patterns: [deferred-initialization via DOMContentLoaded event]

key-files:
  created: []
  modified:
    - frontend/src/store/library-store.ts

key-decisions:
  - "DOMContentLoaded over load event — fires earlier (after HTML parsed) without waiting for all resources, still defers past module evaluation"

patterns-established:
  - "Deferred singleton initialization: singleton constructors should not fire async work; defer to DOM ready events"

requirements-completed: [PERF-03]

# Metrics
duration: 1min
completed: 2026-03-05
---

# Phase 7 Plan 2: Defer Library Data Loading Summary

**Deferred LibraryStore eagerFetch from constructor to DOMContentLoaded event, ensuring app shell renders instantly before 4 backend data roundtrips begin**

## Performance

- **Duration:** 1 min
- **Started:** 2026-03-05T01:53:27Z
- **Completed:** 2026-03-05T01:54:49Z
- **Tasks:** 1
- **Files modified:** 1

## Accomplishments
- Removed `eagerFetch()` call from LibraryStore constructor so module evaluation no longer triggers 4 backend roundtrips
- Added `deferEagerFetch()` method that waits for `DOMContentLoaded` event (or calls immediately if DOM already parsed)
- App shell now renders before data fetching competes for resources
- All 4 data types (tracks, albums, artists, genres) still eagerly loaded once DOM is ready
- Post-scan invalidation behavior unchanged — `invalidate()` still calls `eagerFetch()` directly

## Task Commits

Each task was committed atomically:

1. **Task 1: Defer eagerFetch from constructor to post-DOM-ready** - `cd98ad6` (perf)

## Files Created/Modified
- `frontend/src/store/library-store.ts` - Removed eagerFetch from constructor, added deferEagerFetch with DOMContentLoaded listener

## Decisions Made
- Used `DOMContentLoaded` instead of `load` event — fires earlier (after HTML parsed, before stylesheets/images finish) which minimizes delay in data availability while still deferring past the initial module evaluation. The `load` event would unnecessarily wait for all resources before beginning data fetches.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Plan 02 complete — deferred library loading implemented
- Plan 01 (lazy module loading) may still be pending
- Frontend data loading is now deferred to post-DOM-ready, providing instant app shell render

## Self-Check: PASSED

- [x] `frontend/src/store/library-store.ts` exists
- [x] Commit `cd98ad6` exists in git history

---
*Phase: 07-backend-performance*
*Completed: 2026-03-05*

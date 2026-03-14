---
phase: 14-performance-optimization
plan: 04
subsystem: frontend
tags: [scroll, requestAnimationFrame, profiling, pprof, devtools, performance]

# Dependency graph
requires:
  - phase: 14-performance-optimization
    provides: CSS containment and GPU scroll layer promotion (Plan 01), view caching navigation (Plan 02)
provides:
  - RAF-throttled scroll position saving for cover grid
  - overflow-anchor CSS on queue panel virtualizer
  - Performance profiling guide (docs/PROFILING.md)
affects: [cover-grid, queue-panel, developer-documentation]

# Tech tracking
tech-stack:
  added: []
  patterns: [RAF-throttled scroll saves instead of debounce, overflow-anchor for virtualizer scroll containers]

key-files:
  created:
    - docs/PROFILING.md
  modified:
    - frontend/src/components/cover-grid/scroll-manager.ts
    - frontend/src/components/queue-panel/queue-panel.ts

key-decisions:
  - "Keep monkey-patch alongside overflow-anchor: CSS overflow-anchor disables browser scroll anchoring but not lit-virtualizer's internal _correctScrollError, so the monkey-patch is still needed for scrollbar drag on large lists"
  - "RAF throttle over debounce for scroll saves: saves position once per frame during scrolling instead of only after scrolling stops, preventing lost positions on quick navigation"

patterns-established:
  - "RAF-throttled scroll position saving: use requestAnimationFrame guard pattern instead of setTimeout debounce for continuous scroll position tracking"

requirements-completed: [PERF-SCROLL-03, PERF-DIAG-01]

# Metrics
duration: 2min
completed: 2026-03-14
---

# Phase 14 Plan 04: Scroll Optimization & Profiling Guide Summary

**RAF-throttled scroll position saves for cover grid, overflow-anchor on queue panel virtualizer, and comprehensive performance profiling guide**

## Performance

- **Duration:** 2 min
- **Started:** 2026-03-14T17:49:56Z
- **Completed:** 2026-03-14T17:52:54Z
- **Tasks:** 2 completed, 1 pending checkpoint
- **Files modified:** 3

## Accomplishments
- Cover grid scroll position saves are now RAF-throttled (once per ~16ms frame) instead of 100ms debounced, capturing position during continuous scrolling
- Queue panel lit-virtualizer has `overflow-anchor: none` CSS; monkey-patch retained with expanded documentation explaining why CSS alone is insufficient
- Comprehensive profiling guide created covering pprof backend profiling, Chrome DevTools frontend profiling, and specific diagnostic workflows

## Task Commits

Each task was committed atomically:

1. **Task 1: Optimize scroll event handling and clean up queue panel scroll hack** - `6ca0b3c` (perf)
2. **Task 2: Create performance profiling guide** - `1ec8f82` (docs)
3. **Task 3: Verify performance improvements** - *pending checkpoint:human-verify*

## Files Created/Modified
- `frontend/src/components/cover-grid/scroll-manager.ts` - Replaced debounced scroll save with RAF-throttled save
- `frontend/src/components/queue-panel/queue-panel.ts` - Added overflow-anchor: none CSS, expanded monkey-patch comments
- `docs/PROFILING.md` - Performance profiling guide with backend, frontend, and workflow sections

## Decisions Made
- **Keep monkey-patch alongside overflow-anchor:** CSS `overflow-anchor: none` disables browser-native scroll anchoring but does not affect lit-virtualizer's internal `_correctScrollError()` method. The monkey-patch is still needed to suppress that internal correction during native scrollbar drag on large lists (20k+ items).
- **RAF throttle over debounce:** requestAnimationFrame guard pattern fires once per frame (~16ms at 60fps) during active scrolling, unlike debounce which only fires after scrolling stops. This prevents lost positions if the user navigates away during scrolling.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Task 3 (human-verify checkpoint) pending: user verification of scroll smoothness, navigation speed, and profiling guide
- After checkpoint approval, Phase 14 is complete and ready for transition

---
*Phase: 14-performance-optimization*
*Completed: 2026-03-14*

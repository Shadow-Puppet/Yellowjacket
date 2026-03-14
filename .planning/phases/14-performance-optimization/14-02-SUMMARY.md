---
phase: 14-performance-optimization
plan: 02
subsystem: ui
tags: [navigation, view-caching, dom, performance, display-toggle]

# Dependency graph
requires: []
provides:
  - View caching navigation system — primary views created once and visibility-toggled
  - viewCache Map with bounded 6-entry cache for primary views
  - Ephemeral detail view lifecycle (artist-details, playlist-details, genre-details)
affects: [14-performance-optimization]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "View cache Map<string, HTMLElement> for DOM-persistent primary views"
    - "display:none/display:'' toggle instead of innerHTML destruction"
    - "Ephemeral detail views (remove + create) vs cached primary views"

key-files:
  created: []
  modified:
    - frontend/index.ts

key-decisions:
  - "Primary views cached, detail views ephemeral — detail views depend on entity IDs that change per navigation"
  - "Inline style.display toggle over CSS class — simpler, no specificity issues, direct JS control"
  - "viewCache bounded at 6 entries (one per primary view) — negligible memory overhead"

patterns-established:
  - "View caching: create once, toggle visibility, never destroy primary views"
  - "Detail view lifecycle: hide primary view, remove old detail, create new detail"

requirements-completed: [PERF-NAV-01, PERF-NAV-02]

# Metrics
duration: 1min
completed: 2026-03-14
---

# Phase 14 Plan 02: View Caching Navigation Summary

**DOM-persistent view cache replacing innerHTML destruction — primary views created once and visibility-toggled for instant navigation**

## Performance

- **Duration:** 1 min
- **Started:** 2026-03-14T17:43:20Z
- **Completed:** 2026-03-14T17:44:52Z
- **Tasks:** 1
- **Files modified:** 1

## Accomplishments
- Replaced innerHTML-based navigation with a view caching system using a `Map<string, HTMLElement>`
- Primary views (tracks, albums, artists, genres, playlists, settings) created once and kept in DOM
- Navigation toggles `display:none` / `display:''` instead of destroying and recreating components
- Detail views (artist-details, playlist-details, genre-details) remain ephemeral with proper lifecycle (remove old, create new)
- Scroll positions naturally preserved since DOM is never destroyed
- No virtualizer reinit, no data refetch, no cover art image reload on navigation

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement view caching navigation system** - `ad91043` (perf)

## Files Created/Modified
- `frontend/index.ts` - Replaced switch/innerHTML navigation with VIEW_TAGS map, viewCache Map, display toggle, and ephemeral detail view lifecycle

## Decisions Made
- **Primary views cached, detail views ephemeral** — Detail views (artist-details, playlist-details, genre-details) depend on entity IDs/names that change per navigation, so caching them would show stale content. Primary views are stateless navigation targets that benefit from persistence.
- **Inline style.display toggle** — Using `element.style.display = 'none'` / `element.style.display = ''` rather than CSS classes avoids specificity issues and gives direct control. The empty string restores the element's natural display value from CSS (`.main-panel > * { height: 100% }`).
- **viewCache bounded at 6 entries** — One entry per primary view tag in VIEW_TAGS. Memory overhead is negligible since the data each view holds is already in store caches regardless.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- View caching complete — ready for remaining Phase 14 plans (14-01 CSS containment, 14-03 store optimizations, 14-04 rendering optimizations)
- Existing scroll save/restore logic in components preserved as fallback for data invalidation scenarios

---
## Self-Check: PASSED

- ✅ frontend/index.ts exists
- ✅ Commit ad91043 exists

*Phase: 14-performance-optimization*
*Completed: 2026-03-14*

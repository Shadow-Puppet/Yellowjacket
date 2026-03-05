---
phase: 08-frontend-performance-ux
plan: 01
subsystem: frontend
tags: [lit, queueMicrotask, debounce, css-custom-properties, design-tokens]

# Dependency graph
requires: []
provides:
  - queueMicrotask-based notification coalescing in library store
  - debounced search input (150ms) with instant clear
  - design token CSS custom properties for icon sizes and type scale
affects: [08-02, 08-03, 08-04]

# Tech tracking
tech-stack:
  added: []
  patterns: [queueMicrotask coalescing, debounced input propagation, design tokens via Lit css tagged templates]

key-files:
  created:
    - frontend/src/styles/tokens.css.ts
  modified:
    - frontend/src/store/library-store.ts
    - frontend/src/components/search-bar/search-bar.ts

key-decisions:
  - "queueMicrotask coalescing over setTimeout for synchronous-batch notification"
  - "150ms debounce with instant clear on empty input for responsive UX"
  - ":host scoped design tokens for component-level adoption"

patterns-established:
  - "queueMicrotask coalescing: coalesce multiple notify() calls per microtask tick into one subscriber notification"
  - "Design token import pattern: import { designTokens } from styles/tokens.css and include in static styles array"

requirements-completed: [PERF-05, UX-01]

# Metrics
duration: 1min
completed: 2026-03-05
---

# Phase 08 Plan 01: Performance Plumbing & Design Tokens Summary

**queueMicrotask notification coalescing in library store, 150ms debounced search input, and design token CSS custom properties for icon/type sizing**

## Performance

- **Duration:** 1 min
- **Started:** 2026-03-05T04:13:30Z
- **Completed:** 2026-03-05T04:15:16Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments
- Library store notify() coalesces 8+ notifications during scan invalidation into a single subscriber notification per microtask tick
- Search input debounces store propagation by 150ms while maintaining instant visual feedback and instant clear
- Design token file defines --yj-icon-sm/md/lg and --yj-text-xs/sm/md/lg/xl CSS custom properties for consistent sizing

## Task Commits

Each task was committed atomically:

1. **Task 1: Add queueMicrotask debouncing to library store and search input debounce** - `3bf66ed` (perf)
2. **Task 2: Define design token CSS custom properties for icon sizes and type scale** - `1444a66` (feat)

## Files Created/Modified
- `frontend/src/store/library-store.ts` - Added notifyScheduled flag and queueMicrotask coalescing in notify()
- `frontend/src/components/search-bar/search-bar.ts` - Added 150ms debounce timer for search store propagation
- `frontend/src/styles/tokens.css.ts` - New design token file with icon sizes and type scale custom properties

## Decisions Made
- Used queueMicrotask over setTimeout for notification coalescing — synchronous microtask batching is more predictable and lower latency than macrotask scheduling
- 150ms debounce with instant clear on empty input — balances responsiveness with avoiding unnecessary computation; empty clears are immediate for snappy UX
- Design tokens scoped to :host — each component that imports the stylesheet gets its own token scope, matching Lit's shadow DOM encapsulation model

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Performance plumbing and design tokens in place
- Ready for Plan 02 (subsequent frontend work can import designTokens)
- Library store subscribers will automatically benefit from coalesced notifications

---
*Phase: 08-frontend-performance-ux*
*Completed: 2026-03-05*

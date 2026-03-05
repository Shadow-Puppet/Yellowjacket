---
phase: quick-9
plan: 01
subsystem: frontend/queue-panel
tags: [bugfix, css, virtualizer, scrollbar]
dependency-graph:
  requires: []
  provides: [stable-queue-scrollbar]
  affects: [queue-panel]
tech-stack:
  patterns: [fixed-height-virtualizer-items]
key-files:
  modified:
    - frontend/src/components/queue-panel/queue-panel.ts
decisions:
  - Fixed height of 49px chosen (8px pad-top + ~32px content + 8px pad-bottom + 1px border = 49px with border-box)
metrics:
  duration: 43s
  completed: "2026-03-05"
---

# Quick Task 9: Fix Queue Panel Scroll Bar Not Following

**One-liner:** Fixed-height queue track items (49px) to stabilize lit-virtualizer scroll size estimation on large (20k+) queues, eliminating scrollbar thumb lag when dragging downward.

## What Was Done

### Task 1: Set fixed height on queue track items and contain overflow
**Commit:** `ebde5e5`

Added `height: 49px` and `overflow: hidden` to `.track-item` CSS rule, and `overflow: hidden` to `.track-details` CSS rule in `queue-panel.ts`.

**Root cause:** lit-virtualizer's flow layout computes `_scrollSize` from the average of *measured* items only. With 20k items but only ~15-20 visible at any time, the initial estimate (100px default) vs actual size (~48px) caused the scroll container height to shrink dramatically during downward scrolling as items got measured. This made the scrollbar thumb "lag" behind the mouse because the scroll height kept changing underneath the drag.

**Fix:** Setting a fixed explicit height ensures every item's measured height is identical from the very first render. The formula `_scrollSize = items.length * (averageMargin + averageSize) + averageMargin` becomes completely stable because the average never changes — it equals the actual (fixed) size of every item.

**Files modified:**
- `frontend/src/components/queue-panel/queue-panel.ts` — Added `height: 49px; overflow: hidden` to `.track-item`, added `overflow: hidden` to `.track-details`

### Task 2: Human Verification (checkpoint)
**Status:** Needs human verification

Verification steps:
1. Start the app with a large queue (20k+ tracks)
2. Click the scrollbar thumb in the queue panel and drag it DOWNWARD slowly
3. Verify the scrollbar follows mouse position 1:1 (no lag, no fixed-speed movement)
4. Drag the scrollbar UP — verify it still follows 1:1 (regression check)
5. Scroll rapidly up and down — verify smooth, consistent behavior
6. Verify track items still look correct (no clipped text, proper spacing)
7. Inspect a `.track-item` in DevTools — confirm all visible items have identical height (49px)

## Deviations from Plan

None — plan executed exactly as written.

## Verification

- [x] TypeScript compiles cleanly (`npx tsc --noEmit` passes)
- [x] `.track-item` has explicit fixed `height: 49px` and `overflow: hidden`
- [x] `.track-details` has `overflow: hidden`
- [ ] Scrollbar tracks mouse 1:1 in both directions on 20k+ track queue (needs human verification)
- [ ] No visual regression in track item appearance (needs human verification)

## Self-Check: PASSED

- FOUND: `frontend/src/components/queue-panel/queue-panel.ts`
- FOUND: commit `ebde5e5`
- FOUND: `9-SUMMARY.md`

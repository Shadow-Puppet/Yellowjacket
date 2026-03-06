---
phase: quick-12
plan: 12
subsystem: frontend/cover-grid
tags: [favorites, ui, album-dropdown]
dependency-graph:
  requires: [favorites-controller, wa-icon]
  provides: [album-dropdown-favorites]
  affects: [album-dropdown]
tech-stack:
  added: []
  patterns: [FavoritesController reactive pattern, classMap directive]
key-files:
  modified:
    - frontend/src/components/cover-grid/album-dropdown.ts
decisions: []
metrics:
  duration: 69s
  completed: "2026-03-05"
---

# Quick Task 12: Add Favorite Icon to Album Grid Track List Summary

Per-track favorite icon in album dropdown, matching track-list pattern with compact sizing (18px width, 11px font) for the 12px dropdown context.

## What Was Done

### Task 1: Add favorite icon to album dropdown track rows
**Commit:** `12a0bbc`

Added FavoritesController integration to `<album-dropdown>` component:

- **Imports:** Added `FavoritesController` from `@store/controllers/favorites-controller` and `classMap` from `lit/directives/class-map.js`
- **Controller:** Added `private favCtrl = new FavoritesController(this)` alongside existing `player` controller
- **CSS:** Added `.fav-icon` styles with compact sizing (18px width, 11px font) proportional to the dropdown's 12px track rows. Includes tertiary color default, primary on hover, accent color when favorited
- **Template:** Inserted favorite icon `<div>` with `<wa-icon>` between track number and track title in `renderTrackRow()`. Icon uses `classMap` for dynamic `.favorited` class and `stopPropagation()` on click to prevent track selection

## Deviations from Plan

None — plan executed exactly as written.

## Verification

- `npx tsc --noEmit` — TypeScript compilation passed with zero errors
- `lefthook` pre-commit hook (`frontend-typecheck`) passed

## Commits

| # | Hash | Message |
|---|------|---------|
| 1 | `12a0bbc` | feat(quick-12): add favorite icon to album dropdown track rows |

## Self-Check: PASSED

- ✅ `frontend/src/components/cover-grid/album-dropdown.ts` exists
- ✅ Commit `12a0bbc` exists in git log

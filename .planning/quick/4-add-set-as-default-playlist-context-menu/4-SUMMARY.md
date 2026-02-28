---
phase: quick
plan: 4
subsystem: frontend
tags: [context-menu, playlist, favorites, UX]
dependency_graph:
  requires: [favorites-controller, playlist-view]
  provides: [set-default-playlist-context-action]
  affects: [playlist-view]
tech_stack:
  added: []
  patterns: [context-menu-action, favCtrl-integration]
key_files:
  modified:
    - frontend/src/components/playlist-view/playlist-view.ts
decisions: []
metrics:
  duration: "39s"
  completed: "2026-02-28T19:25:39Z"
  tasks_completed: 1
  tasks_total: 1
---

# Quick Task 4: Add "Set as Default Playlist" Context Menu Option Summary

**One-liner:** Right-click context menu option to set any single playlist as the default/favorites playlist via `favCtrl.setDefaultPlaylist()`

## What Was Done

### Task 1: Add "Set as Default Playlist" context menu item and handler
**Commit:** `9971b63`

Two changes to `playlist-view.ts`:

1. **Handler case** — Added `'set-default'` case in `onPlaylistContextAction()` switch statement, between `'rename'` and `'delete'`. Calls `this.favCtrl.setDefaultPlaylist(entry.summary.ID)` with error handling matching the pattern from `config-page.ts`.

2. **Menu item** — Added `<wa-dropdown-item>` with star icon inside the existing `selectedPlaylists.size <= 1` guard block, after the Rename item. This ensures the option only appears when right-clicking a single playlist, not during multi-select.

## Verification

- ✅ TypeScript compilation passes (`npx tsc --noEmit` — zero errors)
- ✅ Pre-commit hook (frontend-typecheck) passes
- ✅ Menu item is inside single-select guard — hidden during multi-select
- ✅ Handler calls `favCtrl.setDefaultPlaylist()` with correct playlist ID

## Deviations from Plan

None — plan executed exactly as written.

## Commits

| # | Hash | Message |
|---|------|---------|
| 1 | `9971b63` | feat(quick-4): add 'Set as Default Playlist' context menu option |

## Self-Check: PASSED

- ✅ `frontend/src/components/playlist-view/playlist-view.ts` exists
- ✅ Commit `9971b63` exists in git log

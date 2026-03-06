---
phase: quick
plan: 3
subsystem: frontend/playlist-view
tags: [multi-select, batch-delete, UX, playlist]
dependency_graph:
  requires: []
  provides: [playlist-multi-select, playlist-batch-delete]
  affects: [playlist-view]
tech_stack:
  added: []
  patterns: [Set-based-selection, modifier-key-handling]
key_files:
  modified:
    - frontend/src/components/playlist-view/playlist-view.ts
decisions:
  - Used simple Set<number> for playlist selection (matching cover-grid pattern) instead of a second SelectionController — playlists are index-based and few in number
  - Playlist-level and track-level selections are mutually exclusive to prevent confusing UX
metrics:
  duration: 2 min
  completed: "2026-02-28T19:15:35Z"
---

# Quick Task 3: Add Multi-Select to Playlist View with Batch Delete Summary

**One-liner:** Playlist-level Ctrl+Click/Shift+Click multi-select with adaptive context menu and batch delete

## What Was Done

### Task 1: Add playlist-level multi-select state and selection handling
**Commit:** `e13151f`

- Added `selectedPlaylists: Set<number>` state and `lastSelectedPlaylistIndex` anchor for range selection
- Replaced `handleToggle` with `handlePlaylistHeaderClick` that handles three modes:
  - **Ctrl/Cmd+Click:** Toggle individual playlist in/out of selection
  - **Shift+Click:** Range-select from anchor to clicked playlist (inclusive)
  - **Plain click:** Clear selection and expand/collapse as before
- Added mutual exclusion: entering track selection scope (`ensureSelectionScope`) clears playlist selection
- Added `.playlist-header.selected` CSS class with blue highlight (`--yj-selection-bg`)
- Updated `clearSelectionHandler` to also clear playlist selection on outside clicks
- Wired header `@click` to new handler and added `selected` class binding in template

### Task 2: Wire playlist context menu to support batch delete
**Commit:** `c92ced2`

- Updated `handlePlaylistContextMenu` to respect existing multi-selection: if right-clicked playlist is already selected, preserve the selection; otherwise replace with single selection
- Made `onPlaylistContextAction` async to support awaiting batch delete operations
- Added batch delete: when `selectedPlaylists.size > 1`, iterates all selected playlist IDs calling `DeletePlaylist` for each, then refreshes once
- Context menu adapts based on selection count:
  - **Multi-select (>1):** Shows "Delete N Playlists" only (rename hidden)
  - **Single (<=1):** Shows "Rename" + "Delete Playlist" as before
- Clears playlist selection after any context action completes

## Deviations from Plan

None — plan executed exactly as written.

## Verification

- TypeScript compilation passes with zero errors (`npx tsc --noEmit`)
- Pre-commit hooks (frontend-typecheck) pass on both commits

## Self-Check: PASSED

- All modified files exist on disk
- Both task commits verified in git history (e13151f, c92ced2)

---
phase: quick-006
plan: 1
subsystem: frontend
tags: [ui, playlist, icons]
dependency_graph:
  requires: [favorites-controller]
  provides: [conditional-playlist-icons]
  affects: [playlist-view]
tech_stack:
  patterns: [conditional-lit-rendering, nothing-sentinel]
key_files:
  modified:
    - frontend/src/components/playlist-view/playlist-view.ts
decisions:
  - Used `nothing` from lit instead of empty string for clean DOM when no icon needed
metrics:
  duration: 1 min
  completed: "2026-03-01T14:44:26Z"
---

# Quick Task 6: Remove List Icon from Playlist Names and Add Favorites Icon

Conditional icon rendering in playlist list — favorites icon (heart/star per user config) on default playlist, no icon on others, tighter body padding.

## What Changed

### Task 1: Remove list icon, add conditional favorites icon
**Commit:** `3c19766`

- **Removed** the static `<wa-icon name="list">` that appeared before every playlist name
- **Added** conditional rendering: if `entry.summary.ID === this.favCtrl.playlistId`, renders the user-configured favorites icon (`heart` or `star`); otherwise renders `nothing` (no DOM element)
- **Reduced** `.playlist-body` left padding from `42px` to `32px` to tighten track list indentation now that most rows lack the icon
- The empty-state `<wa-icon name="list">` (line 2818) was intentionally left untouched

## Deviations from Plan

None — plan executed exactly as written.

## Verification

- `vite build` compiled 283 modules successfully
- Pre-commit hook `frontend-typecheck` passed
- Default playlist shows favorites icon (heart/star per user config)
- Non-default playlists show chevron directly followed by name (no icon)

## Self-Check: PASSED

- [x] `frontend/src/components/playlist-view/playlist-view.ts` exists
- [x] Commit `3c19766` exists in git history

---
phase: quick-17
plan: 1
subsystem: frontend/playlist
tags: [refactor, navigation, ux-consistency]
dependency-graph:
  requires: []
  provides: [playlist-details-component, playlist-subpage-navigation]
  affects: [playlist-view, index-routing, search-store]
tech-stack:
  added: []
  patterns: [subpage-navigation, detail-view-with-back-button]
key-files:
  created:
    - frontend/src/components/playlist-details/playlist-details.ts
  modified:
    - frontend/src/components/playlist-view/playlist-view.ts
    - frontend/index.ts
    - frontend/src/store/search-store.ts
decisions:
  - Playlist-details is a standalone component (not reusing track-list) to preserve playlist-specific interactions (phantom handling, playlist-scoped drag, remove from playlist)
  - Search filtering in playlist-details filters by track title/artist; playlist-view now only filters by playlist name
metrics:
  duration: 6 min
  completed: "2026-03-08T03:10:34Z"
---

# Quick Task 17: Refactor Playlist View to Use Subpages Summary

Refactored playlist navigation from expand/collapse inline pattern to dedicated subpage navigation, matching genre-details and artist-details UX patterns.

## Completed Tasks

| # | Task | Commit | Key Changes |
|---|------|--------|-------------|
| 1 | Create playlist-details component | dc5c7d6 | New `playlist-details` component with header (back button, list icon, title, track count), full track list with all interactions, context menus, drag support, search filtering, routing in index.ts |
| 2 | Simplify playlist-view to navigate | 955cd68 | Removed inline track expansion, chevrons, track-level interactions. Plain click navigates to playlist-details. Kept playlist management (create, import, rename, delete, sort, drag-drop target) |

## What Changed

### New: `playlist-details` component
- Header with back button, 80x80 playlist avatar (list icon), playlist name, track count
- Full track list with all interactions from the old inline expansion:
  - Click to select, double-click to play, right-click context menu
  - Drag tracks to queue or other playlists
  - Phantom track handling (locate, remove, phantom-resolver dialog)
  - Track details dialog, duplicate tracks dialog
- Drop target for adding tracks from other views
- Search filtering (tracks by title/artist)
- Select-all (Ctrl+A) support
- Listens for `PlaylistTracksChanged` and `PlaylistDeleted` events

### Simplified: `playlist-view` component
- Removed ~1300 lines of inline track expansion code
- Plain click dispatches `navigate` event with `playlist-details` view
- Ctrl/Shift+Click still multi-selects playlists for bulk operations
- Right-click context menu still works (rename, delete, set-default)
- Drag-drop onto playlist items still adds tracks
- Create/import playlist still works
- Sort toolbar still works
- Search now filters by playlist name only (no inline track search)
- Removed: `SelectionController`, `ContextMenuController`, `PlayerController`, track-info, track-details, phantom-resolver imports

### Updated: `index.ts` routing
- Added `playlist-details` case to navigation switch
- Passes `playlistId` and `playlistName` as element attributes

### Updated: `search-store.ts`
- Added `playlist-details` to `SEARCHABLE_VIEWS`

## Deviations from Plan

None - plan executed exactly as written.

## Verification

- `npx tsc --noEmit` passes with no errors
- Pre-commit hook (frontend-typecheck) passed on both commits
- No references to `expanded`, `renderPlaylistBody`, `handleTrackClick`, `SelectionController`, or expand/collapse chevron in playlist-view
- Navigation routing case exists in index.ts
- playlist-details component has @customElement decorator, back button, header, track rendering, context menus, drag support

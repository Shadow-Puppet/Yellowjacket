---
phase: quick
plan: 8
subsystem: playlist
tags: [playlist, duplicate-detection, dialog, ux]
dependency_graph:
  requires: []
  provides: [duplicate-track-detection, duplicate-tracks-dialog]
  affects: [playlist-picker, playlist-view]
tech_stack:
  added: []
  patterns: [wa-dialog, wa-switch, lit-component]
key_files:
  created:
    - frontend/src/components/duplicate-tracks-dialog/duplicate-tracks-dialog.ts
  modified:
    - backend/playlist/playlist.go
    - frontend/src/components/playlist-picker/playlist-picker.ts
    - frontend/src/components/playlist-view/playlist-view.ts
    - frontend/wailsjs/go/models.ts
    - frontend/wailsjs/go/playlist/Service.d.ts
    - frontend/wailsjs/go/playlist/Service.js
decisions:
  - Used DuplicateCheckResult wrapper struct for Wails (T, error) return signature compatibility
  - Reused GetPlaylistTracksWithMetadata query for duplicate detection (avoids new SQL query)
metrics:
  duration: 12 min
  completed: 2026-03-01
  tasks: 3/3
---

# Quick Task 8: Add Duplicate Tracks Dialog to Playlist Summary

Backend duplicate detection using existing playlist track queries, new Lit dialog component stepping through duplicates one-by-one with Add/Skip and batch-apply toggle, wired into both playlist-picker context menu and playlist-view drag-drop flows.

## What Was Built

### Backend: FindDuplicateTracksInPlaylist (Task 1)

- Added `DuplicateTrackInfo` and `DuplicateCheckResult` types to `backend/playlist/playlist.go`
- Implemented `FindDuplicateTracksInPlaylist(playlistID, filePaths)` method on `Service`
- Uses existing `GetPlaylistTracksWithMetadata` query to build a map of existing file paths
- Partitions incoming file paths into duplicates (with metadata) and unique paths
- Wails TypeScript bindings regenerated with proper type mappings

### Frontend: DuplicateTracksDialog Component (Task 2)

- New `<duplicate-tracks-dialog>` Lit component at `frontend/src/components/duplicate-tracks-dialog/`
- Follows existing wa-dialog patterns from `track-details.ts` and `phantom-resolver.ts`
- Shows duplicate track count header, progress indicator (Track N of M)
- Track card displays Title, Artist, Album, Duration for the current duplicate
- "Add" button includes duplicate in final add; "Skip" excludes it
- `wa-switch` toggle "Apply to all remaining" batch-applies the current choice
- `finalize()` combines unique paths + user-approved duplicates, calls `AddTracksToPlaylist`, dispatches `playlist-action-complete`

### Frontend: Integration (Task 3)

- **playlist-picker.ts**: `handleSelectPlaylist` now calls `FindDuplicateTracksInPlaylist` before adding. If duplicates found, opens the dialog instead. Otherwise adds directly as before.
- **playlist-view.ts**: `onPlaylistDrop` drag-drop handler similarly checks for duplicates before adding. Shows dialog when duplicates found.
- Both components render `<duplicate-tracks-dialog>` and listen for `playlist-action-complete` to trigger refresh.

## Commits

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Add backend FindDuplicateTracksInPlaylist | `83de934` | backend/playlist/playlist.go, wailsjs bindings |
| 2 | Create duplicate-tracks-dialog component | `9f3ba2b` | frontend/src/components/duplicate-tracks-dialog/duplicate-tracks-dialog.ts |
| 3 | Wire duplicate detection into playlist-picker and playlist-view | `917a79a` | playlist-picker.ts, playlist-view.ts |

## Deviations from Plan

None - plan executed exactly as written.

## Verification

- [x] `go build ./...` — backend compiles
- [x] `npx tsc --noEmit` — frontend typechecks
- [x] Wails bindings regenerated with `FindDuplicateTracksInPlaylist`
- [x] `DuplicateCheckResult` and `DuplicateTrackInfo` types in generated models.ts

## Self-Check: PASSED

All created files exist, all commits found, all modified files present.

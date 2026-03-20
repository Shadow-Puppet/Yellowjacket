---
phase: 17-single-track-edit
plan: 01
subsystem: api
tags: [tagwriter, wails-bindings, file-picker, context-menu, library-store, events]

# Dependency graph
requires:
  - phase: 16-tag-writing-database-sync
    provides: WriteTrackTags pipeline, TagChanges type, TrackMetadataChanged event
provides:
  - WriteTrackTagsByPath method (filePath → trackID resolution)
  - ImageFilePicker native file dialog for cover art selection
  - TrackMetadataChanged event handler in LibraryStore
  - Track Details context menu accessible from any right-clicked track
affects: [17-single-track-edit]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Path-based wrapper pattern — frontend identifies tracks by FilePath, backend resolves to ID internally"
    - "Full cache invalidation on low-frequency edit events"

key-files:
  created:
    - frontend/wailsjs/go/tagwriter/TagWriter.js
    - frontend/wailsjs/go/tagwriter/TagWriter.d.ts
  modified:
    - backend/tagwriter/pipeline.go
    - backend/frontendutil/frontendutil.go
    - frontend/wailsjs/go/frontendutil/FrontendUtil.js
    - frontend/wailsjs/go/frontendutil/FrontendUtil.d.ts
    - frontend/src/store/library-store.ts
    - frontend/src/components/track-list/track-list.ts
    - frontend/src/components/queue-panel/queue-panel.ts
    - frontend/src/components/cover-grid/cover-grid.ts
    - frontend/src/components/playlist-details/playlist-details.ts

key-decisions:
  - "Manually added Wails bindings since wails generate runs at dev/build time, not via go generate"
  - "Track Details opens for first selected track when multiple are selected"

patterns-established:
  - "Path-based wrapper: WriteTrackTagsByPath resolves filePath to trackID, then delegates to WriteTrackTags"

requirements-completed: [EDIT-01, EDIT-04]

# Metrics
duration: 11min
completed: 2026-03-18
---

# Phase 17 Plan 01: Backend Bridge & Frontend Plumbing Summary

**WriteTrackTagsByPath path→ID resolver, ImageFilePicker for cover art, TrackMetadataChanged store handler, and unrestricted Track Details context menu**

## Performance

- **Duration:** 11 min
- **Started:** 2026-03-18T00:53:49Z
- **Completed:** 2026-03-18T01:05:17Z
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments
- WriteTrackTagsByPath method resolves frontend FilePath to backend trackID via GetAudioFileByPath, then delegates to WriteTrackTags pipeline
- ImageFilePicker opens native OS file dialog filtered to JPEG/PNG for cover art selection
- LibraryStore now listens for TrackMetadataChanged event and invalidates all caches + re-fetches data
- "Track Details" context menu item appears for any right-clicked track regardless of selection state across all 4 views

## Task Commits

Each task was committed atomically:

1. **Task 1: Add WriteTrackTagsByPath and ImageFilePicker backend methods** - `4235b4a` (feat)
2. **Task 2: Add TrackMetadataChanged handler and fix context menu conditions** - `fc5cf70` (feat)

## Files Created/Modified
- `backend/tagwriter/pipeline.go` - Added WriteTrackTagsByPath method
- `backend/frontendutil/frontendutil.go` - Added ImageFilePicker method
- `frontend/wailsjs/go/tagwriter/TagWriter.js` - Wails binding for WriteTrackTagsByPath
- `frontend/wailsjs/go/tagwriter/TagWriter.d.ts` - TypeScript declaration for WriteTrackTagsByPath
- `frontend/wailsjs/go/frontendutil/FrontendUtil.js` - Wails binding for ImageFilePicker
- `frontend/wailsjs/go/frontendutil/FrontendUtil.d.ts` - TypeScript declaration for ImageFilePicker
- `frontend/src/store/library-store.ts` - Added TrackMetadataChanged event listener
- `frontend/src/components/track-list/track-list.ts` - Removed selection gate on Track Details
- `frontend/src/components/queue-panel/queue-panel.ts` - Removed selection gate on Track Details
- `frontend/src/components/cover-grid/cover-grid.ts` - Changed condition to check only track context (not selection size)
- `frontend/src/components/playlist-details/playlist-details.ts` - Removed selection gate on Track Details

## Decisions Made
- Manually created Wails TypeScript bindings rather than running `wails generate` (which requires full dev server startup). The binding pattern matches existing generated files exactly.
- Track Details action uses `filePaths[0]` / `indices[0]` when multiple tracks are selected, opening details for the first selected (or right-clicked) track.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Backend methods ready for Plan 02 to wire the track-details dialog edit mode
- LibraryStore will automatically refresh all views when tag writes complete
- Context menu shows "Track Details" for any right-clicked track in all views

---
*Phase: 17-single-track-edit*
*Completed: 2026-03-18*

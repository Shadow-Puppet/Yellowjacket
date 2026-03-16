---
phase: 13-library-views-phantom-tracks
plan: 02
subsystem: frontend, ui
tags: [lit, wails, library-filter, dropdown, multi-library]

# Dependency graph
requires:
  - phase: 13-library-views-phantom-tracks
    provides: Library-filtered Go query methods and Wails bindings
provides:
  - Library filter dropdown component in top bar
  - All browse views (tracks, albums, artists, genres) filter by selected library
  - Detail views (artist-details, genre-details) respect library filter
  - Search results respect library filter (via filtered data source)
  - Playlists remain unfiltered (cross-library by design)
affects: [frontend-navigation, queue-context]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - Conditional ByLibrary/unfiltered call pattern in library-store
    - Library filter state as store singleton (null = All Libraries)
    - Native select dropdown for compact filter UI

key-files:
  created:
    - frontend/src/components/library-filter/library-filter.ts
  modified:
    - frontend/src/store/library-store.ts
    - frontend/src/store/controllers/library-controller.ts
    - frontend/index.html
    - frontend/index.ts
    - frontend/src/components/cover-grid/cover-grid.ts
    - frontend/src/components/cover-grid/album-selection.ts
    - frontend/src/components/artists-view/artists-view.ts
    - frontend/src/components/genres-view/genres-view.ts
    - frontend/src/components/genre-details/genre-details.ts
    - frontend/wailsjs/go/library/Library.d.ts
    - frontend/wailsjs/go/library/Library.js

key-decisions:
  - "Client-side search with library-filtered data source — no backend SearchTracksByLibrary needed since search is already client-side via rankTracks"
  - "Null selectedLibraryId = All Libraries (no persistence, resets on restart)"
  - "Native select element for dropdown — compact, accessible, no custom widget overhead"
  - "getAlbumsByArtistNameCached returns null when library filter active — forces backend query for consistency"

patterns-established:
  - "ByLibrary conditional call pattern: check selectedLibraryId, call ByLibrary or unfiltered variant"

requirements-completed: [VIEW-01, VIEW-02, VIEW-03, VIEW-04, PLAY-01, PLAY-02, PLAY-03]

# Metrics
duration: 9min
completed: 2026-03-16
---

# Phase 13 Plan 02: Library Filter UI & View Wiring Summary

**Library filter dropdown in top bar with conditional ByLibrary queries across all browse views, detail views, and search — playlists remain unfiltered**

## Performance

- **Duration:** 9 min
- **Started:** 2026-03-16T13:32:45Z
- **Completed:** 2026-03-16T13:41:49Z
- **Tasks:** 1 (of 2 — Task 2 is human verification checkpoint)
- **Files modified:** 12

## Accomplishments
- Created library-filter dropdown component that loads library list from backend and sets store filter
- Modified library-store to conditionally call ByLibrary or unfiltered queries based on selectedLibraryId
- Wired all browse views (tracks, albums, artists, genres) to automatically use filtered data
- Wired detail views (genre-details direct Wails call, artist-details via store, cover-grid album dropdown) to respect filter
- Updated album-selection helper for library-aware drag/context-menu file path resolution
- Verified playlists use separate PlaylistStore — not affected by library filter
- Verified search is client-side (rankTracks on loaded data) — inherits filter automatically

## Task Commits

Each task was committed atomically:

1. **Task 1: Add library filter state + dropdown + wire all views** - `42b8cf9` (feat)

**Task 2:** checkpoint:human-verify (pending user verification)

## Files Created/Modified
- `frontend/src/components/library-filter/library-filter.ts` - NEW: Library filter dropdown component
- `frontend/src/store/library-store.ts` - selectedLibraryId state, ByLibrary conditional calls, getLibraries()
- `frontend/src/store/controllers/library-controller.ts` - Pass-through for selectedLibraryId, setSelectedLibrary, getLibraries
- `frontend/index.html` - Added `<library-filter>` to top bar
- `frontend/index.ts` - Import library-filter component
- `frontend/src/components/cover-grid/cover-grid.ts` - GetAlbumTracksByLibrary in dropdown
- `frontend/src/components/cover-grid/album-selection.ts` - Library-aware fetchAlbumTracks helper
- `frontend/src/components/artists-view/artists-view.ts` - GetAlbumsByArtistByLibrary + GetAlbumTracksByLibrary
- `frontend/src/components/genres-view/genres-view.ts` - GetTracksByGenreByLibrary for context menu
- `frontend/src/components/genre-details/genre-details.ts` - GetTracksByGenreByLibrary for track loading
- `frontend/wailsjs/go/library/Library.d.ts` - Regenerated with ByLibrary bindings
- `frontend/wailsjs/go/library/Library.js` - Regenerated with ByLibrary bindings

## Decisions Made
- Search is entirely client-side (rankTracks filters already-loaded tracks), so no backend SearchTracksByLibrary call needed — the library filter naturally scopes search results via the filtered track list
- Used native `<select>` element for dropdown rather than a custom Lit component — compact, accessible, matches search bar height
- Library filter resets on app restart (no localStorage persistence) per CONTEXT.md requirement
- `getAlbumsByArtistNameCached` returns null when library filter active to avoid stale cross-library data

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Wails bindings not yet generated for ByLibrary methods**
- **Found during:** Task 1 (start)
- **Issue:** Plan 13-01 added Go methods but bindings weren't regenerated
- **Fix:** Ran `wails generate module` before implementation
- **Files modified:** frontend/wailsjs/go/library/Library.d.ts, Library.js
- **Verification:** All ByLibrary imports resolve correctly
- **Committed in:** 42b8cf9

**2. [Rule 1 - Bug] Info type uses lowercase property names (id, name) not uppercase**
- **Found during:** Task 1 (TypeScript typecheck)
- **Issue:** library-filter.ts used `lib.ID` and `lib.Name` but Wails-generated Info type uses `lib.id` and `lib.name`
- **Fix:** Changed to lowercase property access
- **Files modified:** frontend/src/components/library-filter/library-filter.ts
- **Verification:** `tsc --noEmit` passes cleanly
- **Committed in:** 42b8cf9

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 bug)
**Impact on plan:** Both fixes required for compilation. No scope creep.

## Issues Encountered
None

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- All automated implementation complete — awaiting human verification checkpoint (Task 2)
- After verification: Phase 13 complete, multi-library support fully delivered
- v1.1 milestone only needs final Phase 13 checkpoint approval

---
*Phase: 13-library-views-phantom-tracks*
*Completed: 2026-03-16*

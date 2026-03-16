---
phase: 13-library-views-phantom-tracks
plan: 02
subsystem: ui, api
tags: [lit, wails, library-filter, phantom-tracks, playlist-resolution, typescript]

# Dependency graph
requires:
  - phase: 13-library-views-phantom-tracks
    plan: 01
    provides: Library-filtered sqlc queries and Go methods for tracks, albums, artists, genres, search
  - phase: 12-library-crud-data-integrity
    provides: Library CRUD API, orphan cleanup, phantom track metadata pre-population
  - phase: 10-schema-migration
    provides: Libraries table, library_id FK, phantom columns on playlist_tracks
provides:
  - Library filter dropdown UI in top bar with All Libraries default
  - All browse views (tracks, albums, artists, genres) respect active library filter
  - Search respects active library filter
  - Detail views (artist-details, genre-details, album tracks) respect library filter
  - Phantom track auto-resolution after library scan via ScanHooks callback
  - phantom_file_path column on playlist_tracks for phantom-to-track matching
affects: [multi-library-complete, v1.1-milestone]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Conditional Wails binding dispatch: store methods call ByLibrary variant when filter active, default variant otherwise"
    - "ScanHooks callback for cross-package phantom resolution (mirrors RemovalHooks/RescanHooks pattern)"
    - "Deferred event delegation retry in updated() for virtualizer race condition"
    - "phantom_file_path stored on removal for post-scan matching"

key-files:
  created:
    - frontend/src/components/library-filter/library-filter.ts
    - backend/playlist/playlist.go (ResolvePhantomTracksAfterScan)
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
    - frontend/src/components/track-list/track-list.ts
    - frontend/src/components/queue-panel/queue-panel.ts
    - backend/library/crud.go
    - backend/library/library.go
    - backend/database/database.go
    - backend/database/sql/schemas/playlist_tracks.sql
    - backend/app.go

key-decisions:
  - "Client-side search with filtered data source — rankTracks filters already-loaded tracks; no backend SearchTracksByLibrary call needed"
  - "Native select for library filter dropdown — compact, accessible, matches top bar height; no custom component overhead"
  - "getAlbumsByArtistNameCached returns null when filter active — avoids stale cross-library data; forces backend query"
  - "ScanHooks callback pattern for phantom resolution — mirrors RemovalHooks/RescanHooks for cross-package communication"
  - "phantom_file_path column on playlist_tracks — enables post-scan matching of phantoms to re-added tracks"
  - "M3U8-based phantom resolution — reads playlist files to match phantoms by position and file path"

patterns-established:
  - "Conditional ByLibrary dispatch: check selectedLibraryId, call ByLibrary variant or unfiltered default"
  - "ScanHooks pattern: cross-package callbacks registered at app init to avoid circular imports"
  - "Deferred delegation guard: retry event delegation in updated() when virtualizer not ready on firstUpdated()"

requirements-completed: [VIEW-01, VIEW-02, VIEW-03, VIEW-04, PLAY-01, PLAY-02, PLAY-03]

# Metrics
duration: 45min
completed: 2026-03-16
---

# Phase 13 Plan 02: Library Filter UI & Phantom Track Resolution Summary

**Library filter dropdown in top bar with conditional ByLibrary queries across all views, plus ScanHooks-based phantom track auto-resolution after library re-scan**

## Performance

- **Duration:** ~45 min (including checkpoint verification and bugfixes)
- **Started:** 2026-03-16T13:32:45Z
- **Completed:** 2026-03-16T14:18:00Z
- **Tasks:** 2 (1 auto + 1 checkpoint:human-verify — APPROVED)
- **Files modified:** 33 (across 4 code commits)

## Accomplishments
- Created `<library-filter>` dropdown component in top bar — shows "All Libraries" default plus all configured libraries
- Wired all browse views (tracks, albums, artists, genres) and detail views (artist-details, genre-details, album track expansion) to respect active library filter via conditional ByLibrary Wails binding calls
- Search inherits library filter automatically — client-side rankTracks operates on filtered track data
- Playlists remain unfiltered (cross-library by design) — verified during checkpoint
- Fixed pre-existing virtualizer event delegation race condition from Phase 14-03 optimization
- Added phantom track auto-resolution: ScanHooks callback triggers M3U8-based phantom matching after library scan
- Added `phantom_file_path` column (migration 7) to playlist_tracks for reliable phantom→track matching
- All 7 Phase 13 requirements verified end-to-end: VIEW-01 through VIEW-04, PLAY-01 through PLAY-03

## Task Commits

Each task was committed atomically:

1. **Task 1: Add library filter state + dropdown + wire all views** - `42b8cf9` (feat)
2. **Task 2: Verify library filter, cross-library playlists, and phantom tracks** - Checkpoint APPROVED

### Bugfix Commits (during checkpoint verification)

3. **Fix: Defer virtualizer event delegation until element exists** - `f05d2bb` (fix)
4. **Fix: Auto-resolve phantom playlist tracks after library scan** - `93262b9` (fix)
5. **Fix: Resolve phantom playlist tracks using M3U8 paths after scan** - `9f595b7` (fix)

**Plan metadata:** `70e3814` (docs: complete library filter UI plan)

## Files Created/Modified

### Frontend — Library filter UI (Task 1)
- `frontend/src/components/library-filter/library-filter.ts` — NEW: Library filter dropdown component (native select, design tokens)
- `frontend/src/store/library-store.ts` — selectedLibraryId state, conditional ByLibrary dispatch, getLibraries(), invalidation
- `frontend/src/store/controllers/library-controller.ts` — Pass-through for selectedLibraryId, setSelectedLibrary, getLibraries
- `frontend/index.html` — `<library-filter>` element added to top bar header
- `frontend/index.ts` — Import for library-filter component
- `frontend/src/components/cover-grid/cover-grid.ts` — GetAlbumTracksByLibrary for album expansion dropdown
- `frontend/src/components/cover-grid/album-selection.ts` — Library-aware fetchAlbumTracks helper
- `frontend/src/components/artists-view/artists-view.ts` — GetAlbumsByArtistByLibrary + GetAlbumTracksByLibrary
- `frontend/src/components/genres-view/genres-view.ts` — GetTracksByGenreByLibrary for context menu
- `frontend/src/components/genre-details/genre-details.ts` — GetTracksByGenreByLibrary for track loading
- `frontend/wailsjs/go/library/Library.d.ts` — Regenerated with ByLibrary bindings
- `frontend/wailsjs/go/library/Library.js` — Regenerated with ByLibrary bindings

### Frontend — Virtualizer race condition fix
- `frontend/src/components/track-list/track-list.ts` — Deferred event delegation with guard flag in updated()
- `frontend/src/components/queue-panel/queue-panel.ts` — Deferred event delegation with guard flag in updated()

### Backend — Phantom track auto-resolution
- `backend/database/database.go` — Migration 7: phantom_file_path column on playlist_tracks
- `backend/database/sql/schemas/playlist_tracks.sql` — phantom_file_path column definition
- `backend/database/sql/sqlcgen/models.go` — Generated model with PhantomFilePath field
- `backend/database/sql/sqlcgen/playlists.sql.go` — Generated query updates
- `backend/library/crud.go` — Store file_path as phantom_file_path on RemoveLibrary
- `backend/library/library.go` — ScanHooks registration, phantom resolution trigger after scan
- `backend/playlist/playlist.go` — ResolvePhantomTracksAfterScan: M3U8-based phantom matching
- `backend/app.go` — ScanHooks wiring at app initialization
- `frontend/wailsjs/go/models.ts` — Updated generated models
- `frontend/wailsjs/go/playlist/Service.d.ts` — Updated generated bindings
- `frontend/wailsjs/go/playlist/Service.js` — Updated generated bindings

## Decisions Made
- **Client-side search filtering:** rankTracks already operates on filtered track data from library store — no separate backend SearchTracksByLibrary call needed
- **Native `<select>` for library filter:** Compact, accessible, 32px height matching search bar, no custom dropdown overhead
- **getAlbumsByArtistNameCached returns null when filter active:** Forces backend query to avoid showing stale cross-library cached albums
- **ScanHooks callback for phantom resolution:** Mirrors established RemovalHooks/RescanHooks pattern — avoids circular dependency between library and playlist packages
- **M3U8-based phantom resolution over SQL-only:** Reads playlist files to match phantoms by both position and phantom_file_path — handles pre-existing phantoms (match by position) and new ones (match by stored path)
- **phantom_file_path column:** Stored at removal time so post-scan resolution can match even when M3U8 position changes

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Wails bindings not yet generated for ByLibrary methods**
- **Found during:** Task 1 (start)
- **Issue:** Plan 13-01 added Go methods but Wails bindings weren't regenerated
- **Fix:** Ran `wails generate module` before implementation
- **Files modified:** frontend/wailsjs/go/library/Library.d.ts, Library.js
- **Verification:** All ByLibrary imports resolve correctly
- **Committed in:** `42b8cf9`

**2. [Rule 1 - Bug] Info type uses lowercase property names**
- **Found during:** Task 1 (TypeScript typecheck)
- **Issue:** library-filter.ts used `lib.ID` and `lib.Name` but Wails-generated Info type uses `lib.id` and `lib.name`
- **Fix:** Changed to lowercase property access
- **Files modified:** frontend/src/components/library-filter/library-filter.ts
- **Verification:** `tsc --noEmit` passes cleanly
- **Committed in:** `42b8cf9`

**3. [Rule 1 - Bug] Virtualizer event delegation race condition**
- **Found during:** Checkpoint verification (Task 2)
- **Issue:** Pre-existing race from Phase 14-03 — event delegation on virtualizer failed when element wasn't rendered yet on firstUpdated()
- **Fix:** Added retry in updated() with guard flag; delegation happens once virtualizer exists
- **Files modified:** frontend/src/components/track-list/track-list.ts, frontend/src/components/queue-panel/queue-panel.ts
- **Verification:** Track list and queue panel click/context-menu events work reliably on app launch
- **Committed in:** `f05d2bb`

**4. [Rule 2 - Missing Critical] Phantom track auto-resolution after library scan**
- **Found during:** Checkpoint verification (Task 2)
- **Issue:** Phantom tracks not auto-resolved when library re-added and scanned — users would need to manually resolve each one
- **Fix:** Added phantom_file_path column (migration 7), store file_path on removal, ResolvePhantomTracksAfterScan via ScanHooks callback with M3U8 path comparison
- **Files modified:** backend/database/database.go, backend/database/sql/schemas/playlist_tracks.sql, backend/library/crud.go, backend/library/library.go, backend/playlist/playlist.go, backend/app.go
- **Verification:** Remove library → re-add → scan → phantoms automatically resolve to real tracks
- **Committed in:** `93262b9`, `9f595b7`

---

**Total deviations:** 4 auto-fixed (2 bugs, 1 blocking, 1 missing critical)
**Impact on plan:** All fixes essential for correctness. Virtualizer fix resolved pre-existing race condition exposed by multi-library testing. Phantom auto-resolution is critical UX — users should not need to manually fix playlist tracks after re-adding a library.

## Issues Encountered
None beyond the deviations documented above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- **Phase 13 complete** — All 7 requirements verified (VIEW-01 through VIEW-04, PLAY-01 through PLAY-03)
- **v1.1 Multi-Library Support milestone complete** — Phases 9-13 all done
- **Phase 14 (Performance Optimization) already complete** — executed in parallel during v1.1 development
- All browse views, search, playlists, and phantom tracks working correctly in multi-library context

## Self-Check: PASSED

- All 10 key files exist on disk
- All 5 commits found in git log (42b8cf9, f05d2bb, 93262b9, 9f595b7, 70e3814)
- SUMMARY.md exists at expected path

---
*Phase: 13-library-views-phantom-tracks*
*Completed: 2026-03-16*

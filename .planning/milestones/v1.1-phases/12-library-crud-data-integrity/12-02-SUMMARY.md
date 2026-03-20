---
phase: 12-library-crud-data-integrity
plan: 02
subsystem: ui
tags: [lit, wails, library-management, config-page, sidebar, folder-picker, toast, overflow-menu]

# Dependency graph
requires:
  - phase: 12-library-crud-data-integrity
    provides: AddLibrary, RenameLibrary, RemoveLibrary, GetRemovalImpact backend API, LibraryAdded/Renamed/Removed events
  - phase: 11-per-library-scan-pipeline
    provides: ScanLibrary, ScanAllLibraries, scan queue coordinator, per-library progress events
provides:
  - Full library management UI in settings page (list, add, rename, remove with confirmation + toast)
  - Selectable library checkboxes for targeted scanning
  - Inline per-library progress bar during scan
  - Collapsible config sections
  - Sidebar cleaned up (no Libraries nav item)
affects: [13-library-views-phantom-tracks]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Checkbox selection model for multi-library scan targeting"
    - "Inline progress bar per library row during scan"
    - "Collapsible config-section with chevron dropdown"
    - "Overflow menu with document click dismiss"
    - "Toast notification with auto-dismiss timer"

key-files:
  created: []
  modified:
    - frontend/src/components/config-page/config-page.ts
    - frontend/src/components/config-page/config-section.ts
    - frontend/src/components/sidebar/app-sidebar.ts
    - frontend/index.ts
    - frontend/src/store/library-store.ts
    - backend/library/crud.go
    - backend/library/metrics.go
    - backend/library/query.go
    - backend/library/rescan.go
    - backend/library/scan_queue.go

key-decisions:
  - "Selectable library checkboxes — user selects which libraries to scan instead of scan-all-or-nothing"
  - "Scan buttons above library list with selection count indicator"
  - "Inline progress bar per library row — replaces global-only progress"
  - "Collapsible config sections with chevron dropdown — keeps settings page organized"
  - "8-second toast auto-dismiss timer for removal summaries"
  - "Library store invalidation on LibraryRemoved event to refresh all views"

patterns-established:
  - "Checkbox selection model: Set<number> with select-all/indeterminate header"
  - "Collapsible config-section component with chevron toggle"

requirements-completed: [LIB-01, LIB-02, LIB-03, LIB-06]

# Metrics
duration: 38min
completed: 2026-03-15
---

# Phase 12 Plan 02: Frontend Library Management UI Summary

**Full library management UI in settings with add/rename/remove, selectable scan targeting, inline per-library progress bars, and collapsible config sections**

## Performance

- **Duration:** 38 min (execution across previous session + finalization)
- **Started:** 2026-03-15T13:43:37Z
- **Completed:** 2026-03-15T14:21:35Z
- **Tasks:** 3 (2 auto + 1 human-verify checkpoint)
- **Files modified:** 19

## Accomplishments

- Library management UI in settings page: list with name, path, track count per library; Add Library with folder picker; inline rename with Enter/Escape; overflow menu (Rename, Rescan, Remove); removal confirmation dialog with real impact counts; toast notification with removal summary
- Selectable library checkboxes with select-all/indeterminate header for targeted scan operations
- Inline scan progress bar per library row showing phase and percentage
- Collapsible config-section component with chevron dropdown for all settings sections
- Sidebar "Libraries" nav item removed; library-manager component import removed from router
- Library store invalidated on LibraryRemoved event to refresh all data views

## Task Commits

Tasks were committed atomically with extensive follow-up refinements:

1. **Task 1: Replace config-page library section with library management UI** — `ffc5d96` (feat) + 20 follow-up fix/feat/perf commits
2. **Task 2: Remove Libraries sidebar nav item and view routing** — `e199712` (feat)
3. **Task 3: Verify library management UI end-to-end** — Human verified ✅ (all 9 checks passed)

Key follow-up commits:
- `13a42ae` feat: selectable library list with checkbox scan targeting
- `df824c6` feat: show scan progress bar inline in library list entry
- `12c6782` feat: make config sections collapsible with chevron dropdown
- `890284d` fix: delete artist_credit_artist before artist_credit in removal pipeline
- `30f4461` perf: skip FTS5 rebuild during library removal
- `21ea71e` perf: increase scan batch size from 50 to 300
- `b093fbb` fix: invalidate library store cache on LibraryRemoved event

Full commit list (25 commits): `ffc5d96..12c6782`

## Files Created/Modified

- `frontend/src/components/config-page/config-page.ts` — Full library management UI with CRUD, selection, progress, toast, overflow menus
- `frontend/src/components/config-page/config-section.ts` — Collapsible section component with chevron toggle
- `frontend/src/components/sidebar/app-sidebar.ts` — Removed 'libraries' from View type and nav items
- `frontend/index.ts` — Removed library-manager import and routing case
- `frontend/src/store/library-store.ts` — Added LibraryRemoved invalidation handler
- `backend/library/crud.go` — Bug fixes in orphan cleanup ordering
- `backend/library/metrics.go` — ScanWarning.Err serialized as string
- `backend/library/query.go` — GetAllLibrariesWithTrackCounts binding
- `backend/library/rescan.go` — Scan batch size increase, soft scan optimization
- `backend/library/scan_queue.go` — Wait for scan stop before removal
- `frontend/wailsjs/go/library/Library.d.ts` — Regenerated bindings
- `frontend/wailsjs/go/library/Library.js` — Regenerated bindings
- `frontend/wailsjs/go/models.ts` — Regenerated model types

## Decisions Made

- **Selectable library checkboxes:** Added a Set<number> selection model with select-all/indeterminate header checkbox. Users select specific libraries before clicking Scan, rather than scan-all-or-nothing. Selection count shown on button.
- **Scan buttons above library list:** Moved scan actions (Add Library, Scan, Full Rescan, Pause, Cancel) above the library list instead of below, with none selected by default.
- **Inline progress bar per library row:** Each library row shows its scan phase and progress percentage inline, replacing the global-only progress indicator.
- **Collapsible config sections:** All config-section elements now collapse with a chevron dropdown, keeping the settings page organized as it grows.
- **8-second toast timer:** Toast auto-dismisses after 8 seconds (longer than typical 4s) since removal summaries contain important information.
- **Library store invalidation on LibraryRemoved:** Ensures all data views (tracks, albums, artists, genres) refresh after library removal.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed orphan cleanup FK ordering**
- **Found during:** Task 1 refinement
- **Issue:** artist_credit_artist rows must be deleted before artist_credit rows (FK constraint)
- **Fix:** Reordered DELETE statements in removal pipeline
- **Files modified:** backend/library/crud.go
- **Committed in:** `890284d`

**2. [Rule 1 - Bug] ScanWarning.Err serialized as error interface**
- **Found during:** Task 1 refinement
- **Issue:** Go error interface doesn't serialize to JSON string — frontend got empty object
- **Fix:** Serialize Err field as string in ScanWarning
- **Files modified:** backend/library/metrics.go
- **Committed in:** `ac8cbb3`

**3. [Rule 1 - Bug] Library store not invalidated on LibraryRemoved**
- **Found during:** Task 1 refinement
- **Issue:** Removing a library left stale tracks/albums/artists in library store cache
- **Fix:** Added LibraryRemoved event listener to library store that triggers full invalidation
- **Files modified:** frontend/src/store/library-store.ts
- **Committed in:** `b093fbb`

**4. [Rule 2 - Missing Critical] Phantom tracks from empty library root**
- **Found during:** Task 1 verification
- **Issue:** TOML cleanup left empty DirectoryPath, causing all tracks to appear as phantom
- **Fix:** Resolved empty library root detection and cleanup
- **Files modified:** backend/library/crud.go
- **Committed in:** `717e249`

**5. [Rule 3 - Blocking] Replaced removed Scan() import**
- **Found during:** Task 2
- **Issue:** Removing library-manager import broke a reference to deleted Scan() method
- **Fix:** Replaced with ScanAllLibraries() call
- **Files modified:** frontend/index.ts
- **Committed in:** `0559822`

---

**Total deviations:** 5 auto-fixed (3 bugs, 1 missing critical, 1 blocking)
**Impact on plan:** All auto-fixes necessary for correctness. No scope creep. Additional features (selectable scanning, inline progress, collapsible sections) were discovered needs during verification.

## Issues Encountered

None — all issues were resolved through iterative refinement.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- Phase 12 complete — all library CRUD backend and frontend implemented
- Ready for Phase 13: Library Views & Phantom Tracks
- All library management operations verified end-to-end through human checkpoint
- Library store properly invalidates on CRUD events, ready for filtered views

## Self-Check: PASSED

All key files verified present on disk, all referenced commits verified in git log.

---
*Phase: 12-library-crud-data-integrity*
*Completed: 2026-03-15*

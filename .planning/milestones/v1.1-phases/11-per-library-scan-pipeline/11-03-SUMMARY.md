---
phase: 11-per-library-scan-pipeline
plan: 03
subsystem: library
tags: [scan-pipeline, startup, auto-scan, legacy-cleanup]

# Dependency graph
requires:
  - phase: 11-per-library-scan-pipeline
    provides: ScanLibrary, ScanAllLibraries, scanInternal, scan queue coordinator
provides:
  - Auto-scan all libraries on app launch via ScanAllLibraries in OnDomReady
  - FullRescan using DB-sourced library (not config DirectoryPath)
  - Cleaned-up Library with no legacy single-directory handler
affects: [12-library-crud-data-integrity]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Auto-scan goroutine in OnDomReady — non-blocking startup scan"
    - "FullRescan resolves library from DB via GetAllLibraries"

key-files:
  created: []
  modified:
    - backend/app.go
    - backend/library/library.go
    - backend/library/rescan.go

key-decisions:
  - "FullRescan uses first library from GetAllLibraries — per-library rescan deferred to Phase 12"
  - "LibraryConfigChanged handler removed entirely rather than updated — multi-library model uses CRUD API"
  - "Scan() wrapper deleted — only callers were handleConfigUpdate and FullRescan, both updated"

patterns-established:
  - "Auto-scan pattern: goroutine in OnDomReady calling ScanAllLibraries"

requirements-completed: [LSCAN-01, LSCAN-02]

# Metrics
duration: 10min
completed: 2026-03-09
---

# Phase 11 Plan 03: Wire Auto-Scan and Clean Up Legacy Code Summary

**Auto-scan all libraries on app launch via ScanAllLibraries goroutine, FullRescan from DB-sourced library, legacy single-directory handlers removed**

## Performance

- **Duration:** 10 min
- **Started:** 2026-03-09T20:07:03Z
- **Completed:** 2026-03-09T20:17:10Z
- **Tasks:** 1
- **Files modified:** 3

## Accomplishments
- Auto-scan on startup calls `ScanAllLibraries()` in a goroutine from `OnDomReady` — same codepath as UI button
- Legacy `LibraryConfigChanged` event handler removed from `registerEventHandlers`
- Legacy `handleConfigUpdate` method deleted (single-directory model)
- Deprecated `Scan()` wrapper deleted (replaced by `ScanLibrary`/`ScanAllLibraries`)
- `errLibraryDirNotConfigured` sentinel error removed
- `FullRescan` now resolves library from DB via `GetAllLibraries` instead of config DirectoryPath
- `FullRescan` calls `scanInternal` directly instead of the removed `Scan()` wrapper

## Task Commits

Each task was committed atomically:

1. **Task 1: Wire auto-scan on startup and clean up legacy single-directory code** - `1aaf536` (feat)

_Note: Code changes were included in the 11-02 metadata commit due to staging overlap. All changes are verified present and correct._

## Files Created/Modified
- `backend/app.go` - Added ScanAllLibraries goroutine in OnDomReady, added early return after startupErr
- `backend/library/library.go` - Removed LibraryConfigChanged handler, handleConfigUpdate, Scan(), errLibraryDirNotConfigured; updated NewLibrary doc comment
- `backend/library/rescan.go` - FullRescan resolves first library from DB, calls scanInternal directly, added errNoLibrariesConfigured sentinel

## Decisions Made
- **FullRescan uses first library from DB:** Per-library full rescan will be added in Phase 12. For now, `FullRescan()` takes the first library from `GetAllLibraries()` — this preserves backward compatibility for the config-page "Rescan" button in the single-library case.
- **Complete removal of LibraryConfigChanged handler:** Rather than updating the handler for multi-library, it was removed entirely. In the multi-library model, libraries are managed through the CRUD API (Phase 12) and scanning is triggered explicitly via `ScanLibrary`/`ScanAllLibraries`.
- **Scan() wrapper deleted:** The only callers were `handleConfigUpdate` (deleted) and `FullRescan` (updated to use `scanInternal` directly). No backward-compatible wrapper needed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed golangci-lint wsl and err113 violations**
- **Found during:** Task 1 (commit attempt)
- **Issue:** Pre-commit hook flagged: (1) wsl — block ending with comment in registerEventHandlers, (2) err113 — dynamic errors.New in rescan.go
- **Fix:** (1) Moved comment to function doc comment, removed empty return before close brace. (2) Created static `errNoLibrariesConfigured` sentinel error variable.
- **Files modified:** backend/library/library.go, backend/library/rescan.go
- **Verification:** golangci-lint passes with 0 issues
- **Committed in:** 1aaf536 (part of task commit)

---

**Total deviations:** 1 auto-fixed (blocking — lint compliance)
**Impact on plan:** Necessary for pre-commit hook compliance. No scope creep.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Phase 11 complete — all 3 plans executed
- Per-library scan pipeline fully wired: ScanLibrary(id), ScanAllLibraries(), auto-scan on launch
- Ready for Phase 12: Library CRUD & Data Integrity
- Frontend already has per-library progress display and queue-aware cancel dialog (Plan 02)
- Phase 12 can build library management UI (add/rename/remove) on top of existing scan infrastructure

---
*Phase: 11-per-library-scan-pipeline*
*Completed: 2026-03-09*

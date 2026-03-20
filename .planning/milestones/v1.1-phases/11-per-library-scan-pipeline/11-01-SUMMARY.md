---
phase: 11-per-library-scan-pipeline
plan: 01
subsystem: library
tags: [scan-queue, per-library, wails-bindings, sqlc, events]

# Dependency graph
requires:
  - phase: 10-schema-migration
    provides: libraries table, library_id column on audio_files, GetLibrary/GetAllLibraries/GetLibraryByPath queries
provides:
  - ScanLibrary(id) Wails-bound method for per-library scanning
  - ScanAllLibraries() Wails-bound method for bulk sequential scanning
  - Scan queue coordinator with FIFO sequential execution and silent dedup
  - CancelCurrentScan() and CancelAllScans() for queue-aware cancellation
  - GetScanQueueLength() and QueuedLibraryNames() for UI display
  - Library-aware ScanProgress and ScanMetrics with libraryId, libraryName, queuedCount
  - LibraryScanQueued and LibraryScanQueueDrained events
  - CreateAudioFile with library_id parameter
affects: [12-library-crud-data-integrity, 13-library-views-phantom-tracks]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Scan queue coordinator pattern: FIFO queue with single-active-scan mutex"
    - "scanInternal() as reusable per-library scan engine"
    - "Silent dedup for scan requests (no-op if already scanning or queued)"

key-files:
  created:
    - backend/library/scan_queue.go
  modified:
    - backend/library/library.go
    - backend/library/scan_control.go
    - backend/library/metrics.go
    - backend/events/events.go
    - backend/database/sql/queries/audio_files.sql
    - backend/database/sql/sqlcgen/audio_files.sql.go
    - frontend/src/events.ts
    - frontend/wailsjs/go/library/Library.d.ts
    - frontend/wailsjs/go/library/Library.js

key-decisions:
  - "Library identification threaded through importResult.libraryID rather than adding field to Library struct"
  - "scanInternal returns *ScanMetrics instead of (*ScanMetrics, error) — errors are logged and warnings accumulated"
  - "Worker count auto-detected per library path (ScanConcurrencyAuto) rather than using global config value"
  - "Backward-compatible Scan() retained as deprecated wrapper for handleConfigUpdate"

patterns-established:
  - "Scan queue coordinator: scanQueue []scanQueueEntry + drainQueue() pattern for sequential execution"
  - "mkProgress closure for DRY ScanProgress event construction with library identification"

requirements-completed: [LSCAN-01, LSCAN-02, LSCAN-04]

# Metrics
duration: 7min
completed: 2026-03-09
---

# Phase 11 Plan 01: Per-Library Scan Pipeline Summary

**ScanLibrary(id) with FIFO queue coordinator, per-library file association via library_id, and queue-aware cancel/pause controls**

## Performance

- **Duration:** 7 min
- **Started:** 2026-03-09T19:56:10Z
- **Completed:** 2026-03-09T20:03:14Z
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments
- `ScanLibrary(id)` resolves library path from DB and scans only that directory, associating files with library_id
- FIFO scan queue ensures only one scan runs at a time, with silent dedup for duplicate requests
- `ScanAllLibraries()` queries all libraries and queues them sequentially
- `CancelCurrentScan()` stops current library and auto-starts next queued; `CancelAllScans()` clears queue too
- Pause freezes current scan AND queue (drainQueue only runs on scan completion)
- All scan events (progress, started, complete, cancelled) include library name and queue count

## Task Commits

Each task was committed atomically (note: lint fix amend merged both into single commit):

1. **Task 1: Add library_id to CreateAudioFile + update events and progress types** - `943db1c` (feat)
2. **Task 2: Create scan queue coordinator and refactor Library for per-library scanning** - `943db1c` (feat)

_Note: Tasks were merged into a single commit due to lint fix amend during pre-commit hook._

## Files Created/Modified
- `backend/library/scan_queue.go` - Scan queue coordinator: ScanLibrary, ScanAllLibraries, CancelCurrentScan, CancelAllScans, drainQueue
- `backend/library/library.go` - Refactored Scan() → scanInternal() with library ID/name/path parameters, per-library DB queries
- `backend/library/scan_control.go` - Deprecated CancelScan() in favor of queue-aware methods
- `backend/library/metrics.go` - Added LibraryID, LibraryName to ScanMetrics; LibraryID, LibraryName, QueuedCount to ScanProgress
- `backend/events/events.go` - Added LibraryScanQueued and LibraryScanQueueDrained constants
- `backend/database/sql/queries/audio_files.sql` - Added library_id to CreateAudioFile INSERT
- `backend/database/sql/sqlcgen/audio_files.sql.go` - Regenerated with LibraryID in CreateAudioFileParams
- `frontend/src/events.ts` - Regenerated with scan queue events
- `frontend/wailsjs/go/library/Library.d.ts` - Auto-generated Wails bindings for new methods
- `frontend/wailsjs/go/library/Library.js` - Auto-generated Wails bindings for new methods
- `frontend/wailsjs/go/models.ts` - Auto-generated model updates

## Decisions Made
- **Library ID threading via importResult:** Rather than adding a libraryID field to the Library struct, the ID is threaded through the scan pipeline via the importResult struct and set in the DB writer goroutine. This keeps the data flow explicit and avoids mutation of shared state.
- **scanInternal returns only metrics:** Changed signature from `(*ScanMetrics, error)` to `*ScanMetrics` since the scan queue coordinator calls it in a goroutine where error return is impractical. Errors are logged and accumulated in ScanMetrics.Warnings.
- **Auto worker count per library:** Each library path may reside on different storage (SSD vs HDD), so worker count uses `ScanConcurrencyAuto` with per-path detection rather than the global config value.
- **Backward-compatible Scan():** Retained as deprecated wrapper that resolves the library from `l.conf.DirectoryPath` via `GetLibraryByPath`. This keeps `handleConfigUpdate` and `FullRescan` working without changes.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Per-library scan pipeline complete, ready for Phase 11 Plan 02 (if exists) or Phase 12 (Library CRUD & Data Integrity)
- Frontend can now call `ScanLibrary(id)`, `ScanAllLibraries()`, `CancelCurrentScan()`, `CancelAllScans()`
- Progress events include library identification for UI display
- Phase 12 can build library management UI on top of these Wails bindings

---
*Phase: 11-per-library-scan-pipeline*
*Completed: 2026-03-09*

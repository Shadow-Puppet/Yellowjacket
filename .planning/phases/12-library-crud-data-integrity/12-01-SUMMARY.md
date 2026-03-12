---
phase: 12-library-crud-data-integrity
plan: 01
subsystem: library
tags: [crud, orphan-cleanup, data-integrity, queue-compaction, phantom-tracks, events]

# Dependency graph
requires:
  - phase: 11-per-library-scan-pipeline
    provides: ScanLibrary, ScanAllLibraries, scan queue coordinator
  - phase: 10-schema-migration
    provides: libraries table, library_id FK, phantom columns on playlist_tracks
provides:
  - AddLibrary, RenameLibrary, RemoveLibrary, GetRemovalImpact backend API
  - Orphan cleanup pipeline (recordings → genres → release_groups → artist_credits → artists → cover_art)
  - Queue CompactAfterLibraryRemoval method
  - Phantom metadata population before cascade delete
  - LibraryAdded, LibraryRenamed, LibraryRemoved events
affects: [12-02-frontend-library-ui, 13-library-views-phantom-tracks]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "RemovalHooks callback struct — breaks circular dependency between library, player, and queue packages"
    - "Bottom-up orphan cleanup in single transaction — reference-counting DELETE WHERE NOT IN subqueries"
    - "Pre-populate phantom metadata BEFORE cascade delete — avoids lost join data"
    - "querySingleInt64 helper for hand-crafted SQL returning single aggregate values"

key-files:
  created:
    - backend/library/crud.go
  modified:
    - backend/library/library.go
    - backend/events/events.go
    - frontend/src/events.ts
    - backend/queue/queue.go
    - backend/app.go

key-decisions:
  - "Application-level name uniqueness check (iterate GetAllLibraries) rather than DB UNIQUE constraint — avoids migration 7"
  - "RemovalHooks struct pattern (StopPlayback + CompactQueue callbacks) wired in app.go — mirrors existing RescanHooks pattern"
  - "querySingleInt64 helper wraps DB.QueryContext returning *sql.Rows since DB has no QueryRowContext method"
  - "Sentinel errors for all validation (errLibraryNameEmpty, errLibraryNameTooLong, errLibraryNameDuplicate, errLibraryPathNotExist) per err113 linter rule"
  - "Context parameter placed first in querySingleInt64 per revive context-as-argument rule"

patterns-established:
  - "RemovalHooks callback struct for cross-package lifecycle coordination"
  - "querySingleInt64 for hand-crafted aggregate SQL queries"

requirements-completed: [LIB-01, LIB-02, LIB-03, DATA-02, DATA-03, PLAY-04]

# Metrics
duration: 6min
completed: 2026-03-12
---

# Phase 12 Plan 01: Library CRUD Backend API Summary

**Backend CRUD API with AddLibrary/RenameLibrary/RemoveLibrary, full orphan cleanup pipeline, phantom track preservation, FTS5 rebuild, queue compaction, and event emission**

## Performance

- **Duration:** 6 min
- **Started:** 2026-03-12T23:32:27Z
- **Completed:** 2026-03-12T23:38:30Z
- **Tasks:** 2
- **Files created:** 1
- **Files modified:** 5

## Accomplishments

- **AddLibrary(path)** — validates path exists, auto-names from folder base, creates DB row via sqlc, emits LibraryAdded, starts async ScanLibrary
- **RenameLibrary(id, newName)** — validates 1-50 char length, checks name uniqueness across all libraries (application-level), updates via sqlc, emits LibraryRenamed
- **GetRemovalImpact(libraryID)** — read-only queries returning track count, affected playlists count, queue items count for confirmation dialog
- **RemoveLibrary(id)** — the critical 23-step method:
  1. Cancel active scan for library
  2. Stop playback if current track belongs to library
  3. Pre-count metrics for summary
  4. Begin transaction
  5. Populate phantom metadata on playlist_tracks (BEFORE cascade delete)
  6. DELETE audio_files WHERE library_id (CASCADE on queue_tracks, SET NULL on playlist_tracks)
  7. Bottom-up orphan cleanup: recordings → recording_genres → release_group_recordings → release_groups → artist_credits (dual FK check) → artist_credit_artists → artists → genres → cover_art
  8. DELETE library row
  9. Commit transaction
  10. Post-commit: RebuildSearchIndex (FTS5), delete cover art files, CompactQueue, emit events
- **CompactAfterLibraryRemoval()** on Queue — reloads surviving tracks from DB, detects if current track survived, resets index, unloads player if needed, clears shuffle order, emits QueueChanged
- **RemovalHooks** wired in app.go: StopPlayback → player.UnloadTrack(), CompactQueue → queue.CompactAfterLibraryRemoval()
- Three new event constants: LibraryAdded, LibraryRenamed, LibraryRemoved — auto-generated to frontend events.ts

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement Library CRUD methods and orphan cleanup pipeline** — `bd44f83` (feat)
   - Created backend/library/crud.go (525 lines) with all CRUD methods
   - Added 3 event constants to backend/events/events.go
   - Added removalHooks field to Library struct
   - Regenerated frontend/src/events.ts
2. **Task 2: Add queue compaction method and wire removal hooks in app.go** — `5995dfd` (feat)
   - Added CompactAfterLibraryRemoval() to backend/queue/queue.go (80 lines)
   - Wired RemovalHooks in backend/app.go OnStartup

## Files Created/Modified

- `backend/library/crud.go` (NEW) — AddLibrary, RenameLibrary, RemoveLibrary, GetRemovalImpact, cancelLibraryScan, currentTrackBelongsToLibrary, querySingleInt64, RemovalHooks type, sentinel errors
- `backend/library/library.go` — Added removalHooks RemovalHooks field to Library struct
- `backend/events/events.go` — Added LibraryAdded, LibraryRenamed, LibraryRemoved constants
- `frontend/src/events.ts` — Regenerated with new library CRUD event constants
- `backend/queue/queue.go` — Added CompactAfterLibraryRemoval method
- `backend/app.go` — Wired RemovalHooks in OnStartup (StopPlayback + CompactQueue callbacks)

## Decisions Made

- **Application-level name uniqueness:** Iterate GetAllLibraries to check for duplicate names rather than adding a UNIQUE constraint to the libraries table. Avoids needing migration 7; the check is only done during rename which is infrequent.
- **RemovalHooks callback struct:** Follows the existing RescanHooks pattern to break circular dependencies between library → player and library → queue packages. Wired in app.go where all subsystems are accessible.
- **querySingleInt64 helper:** The project's `database.DB` type exposes `QueryContext` returning `*sql.Rows` but no `QueryRowContext`. The helper wraps the full scan-close cycle for single-value aggregate queries.
- **Sentinel errors per err113:** Defined `errLibraryNameEmpty`, `errLibraryNameTooLong`, `errLibraryNameDuplicate`, `errLibraryPathNotExist` as package-level vars to satisfy the golangci-lint err113 rule.
- **Context-first parameter order:** `querySingleInt64(ctx, db, query, args...)` follows `revive` linter's context-as-argument rule.

## Deviations from Plan

None — plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None — no external service configuration required.

## Next Plan Readiness

- Plan 12-01 complete — backend CRUD API fully implemented
- Ready for Plan 12-02: Frontend library management UI in settings + sidebar cleanup
- All Wails-bindable methods (AddLibrary, RenameLibrary, RemoveLibrary, GetRemovalImpact) are available for frontend consumption
- Events (LibraryAdded, LibraryRenamed, LibraryRemoved) are defined for frontend reactive updates

## Self-Check: PASSED

All files verified present, all commits verified in git log.

---
*Phase: 12-library-crud-data-integrity*
*Completed: 2026-03-12*

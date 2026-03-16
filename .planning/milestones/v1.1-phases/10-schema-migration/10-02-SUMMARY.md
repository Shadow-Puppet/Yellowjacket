---
phase: 10-schema-migration
plan: 02
subsystem: database
tags: [sqlite, sqlc, queries, phantom-tracks, migration-tests, multi-library]

# Dependency graph
requires:
  - phase: 10-schema-migration plan 01
    provides: libraries table, audio_files.library_id, playlist_tracks phantom columns, migration 6
provides:
  - sqlc CRUD queries for libraries table (7 queries)
  - Updated playlist queries with phantom metadata support and LEFT JOINs
  - GetTrackPhantomMetadata helper query for eager phantom population
  - Audio file queries filtered by library_id
  - Migration 6 integration tests (5 test functions)
  - NewTestDBWithLibrary helper for downstream test usage
affects: [11-per-library-scan, 12-library-crud, 13-library-views]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "LEFT JOIN for nullable FK columns in sqlc queries"
    - "COALESCE fallback chain: live metadata → phantom metadata → empty string"
    - "is_phantom computed column via CASE WHEN for phantom track detection"
    - "NewTestDBWithLibrary helper for tests needing pre-populated library"

key-files:
  created:
    - backend/database/sql/queries/libraries.sql
    - backend/database/sql/sqlcgen/libraries.sql.go
    - backend/database/database_test.go
  modified:
    - backend/database/sql/queries/audio_files.sql
    - backend/database/sql/queries/playlists.sql
    - backend/database/sql/sqlcgen/audio_files.sql.go
    - backend/database/sql/sqlcgen/playlists.sql.go
    - backend/database/testhelper.go

key-decisions:
  - "COALESCE fallback chain for phantom metadata: prefer live data over phantom data over empty string"
  - "Computed is_phantom column via CASE WHEN rather than requiring callers to check audio_file_id"
  - "GetPlaylistTrackFilePaths filters out NULLs with audio_file_id IS NOT NULL"

patterns-established:
  - "LEFT JOIN + COALESCE pattern for nullable FK queries"
  - "is_phantom computed column pattern for phantom track detection"
  - "NewTestDBWithLibrary(t, name, path) for integration tests needing libraries"

requirements-completed: [LIB-04, LIB-05]

# Metrics
duration: 5min
completed: 2026-03-09
---

# Phase 10 Plan 2: sqlc Queries & Migration Tests Summary

**Library CRUD queries, phantom-aware playlist queries with LEFT JOIN + COALESCE fallback, and 5 migration 6 integration tests**

## Performance

- **Duration:** 5 min
- **Started:** 2026-03-09T13:45:05Z
- **Completed:** 2026-03-09T13:50:34Z
- **Tasks:** 2
- **Files modified:** 9

## Accomplishments
- Created 7 library CRUD queries (create, get, get-by-path, list, update, delete, count) with sqlc-generated Go code
- Updated all playlist track queries to use LEFT JOIN for nullable audio_file_id, with COALESCE fallback chain from live metadata to phantom metadata
- Added GetTrackPhantomMetadata helper query for eager phantom population at insert time
- Added is_phantom computed column to GetPlaylistTracksWithMetadata and GetAllPlaylistTracksWithMetadata
- Added GetAudioFilesByLibrary and CountAudioFilesByLibrary queries
- Created 5 comprehensive migration 6 integration tests covering fresh DB, CRUD, phantom tracks, FK enforcement, and VIEW validation
- Added NewTestDBWithLibrary helper for downstream test usage

## Task Commits

Each task was committed atomically:

1. **Task 1: Add sqlc queries for libraries and update playlist queries** - `02548dd` (feat)
2. **Task 2: Migration integration tests and NewTestDB update** - `bc15189` (feat)

## Files Created/Modified
- `backend/database/sql/queries/libraries.sql` - 7 CRUD queries for libraries table
- `backend/database/sql/queries/playlists.sql` - Updated with phantom support, LEFT JOINs, GetTrackPhantomMetadata
- `backend/database/sql/queries/audio_files.sql` - Added GetAudioFilesByLibrary, CountAudioFilesByLibrary
- `backend/database/sql/sqlcgen/libraries.sql.go` - Generated Go code for library queries
- `backend/database/sql/sqlcgen/playlists.sql.go` - Regenerated with phantom columns, is_phantom, LEFT JOINs
- `backend/database/sql/sqlcgen/audio_files.sql.go` - Regenerated with library filter queries
- `backend/database/database_test.go` - 5 migration 6 integration tests
- `backend/database/testhelper.go` - Added NewTestDBWithLibrary helper

## Decisions Made
- COALESCE fallback chain: live data → phantom data → empty string ensures callers always get usable values regardless of whether a track is phantom or not
- Added `is_phantom` as a computed column (`CASE WHEN pt.audio_file_id IS NULL THEN 1 ELSE 0 END`) to eliminate null-checking logic in callers
- GetPlaylistTrackFilePaths now filters `WHERE audio_file_id IS NOT NULL` to exclude phantom tracks from file path lists

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed NewTestDBWithLibrary path collision with sentinel library**
- **Found during:** Task 2 (migration tests)
- **Issue:** Tests using `NewTestDBWithLibrary(t, "Test", "/test")` collided with the sentinel library at `(0, 'Test', '/test')` from NewTestDB, causing UNIQUE constraint violation
- **Fix:** Changed test paths to unique values (`/test/music`, `/test/fk-lib`, `/test/view-lib`) to avoid collision with sentinel
- **Files modified:** backend/database/database_test.go
- **Verification:** All 5 TestMigration6 tests pass
- **Committed in:** bc15189 (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug)
**Impact on plan:** Minor path collision fix in tests. No scope creep.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Phase 10 complete: schema files, migration 6, sqlc queries, and migration tests all in place
- Ready for Phase 11 (per-library scan pipeline) — libraries table and library_id queries available
- Ready for Phase 12 (library CRUD API) — all 7 library queries generated and tested
- Ready for Phase 13 (library views & phantom tracks) — phantom metadata queries with is_phantom column available

---
*Phase: 10-schema-migration*
*Completed: 2026-03-09*

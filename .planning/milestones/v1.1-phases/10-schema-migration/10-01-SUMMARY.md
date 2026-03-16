---
phase: 10-schema-migration
plan: 01
subsystem: database
tags: [sqlite, migration, multi-library, phantom-tracks, schema]

# Dependency graph
requires: []
provides:
  - libraries table (name, path, created_at)
  - audio_files.library_id FK column with index
  - playlist_tracks phantom metadata columns (6 fields)
  - playlist_tracks SET NULL FK (was CASCADE)
  - track_metadata VIEW with library_id
  - migration 6 function (multi-library upgrade)
  - pre-migration backup function
  - TOML config cleanup (DirectoryPath removal)
affects: [11-per-library-scan, 12-library-crud, 13-library-views]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Underscore prefix for schema file ordering (_libraries.sql sorts before audio_files.sql)"
    - "Sentinel library row (id=0) in test DB for FK satisfaction"
    - "Dynamic DEFAULT in ALTER TABLE ADD COLUMN for backfill"
    - "TOML read/write with generic map[string]any to preserve unknown sections"

key-files:
  created:
    - backend/database/sql/schemas/_libraries.sql
  modified:
    - backend/database/database.go
    - backend/database/sql/schemas/audio_files.sql
    - backend/database/sql/schemas/playlist_tracks.sql
    - backend/database/sql/schemas/track_metadata_view.sql
    - backend/database/sql/sqlcgen/audio_files.sql.go
    - backend/database/sql/sqlcgen/models.go
    - backend/database/sql/sqlcgen/playlists.sql.go
    - backend/database/testhelper.go
    - backend/playlist/playlist.go

key-decisions:
  - "Underscore prefix _libraries.sql for embedded FS sort order (libraries table must exist before audio_files FK)"
  - "Sentinel library id=0 in NewTestDB so existing tests using DEFAULT library_id=0 continue working"
  - "TOML cleanup uses generic map[string]any to preserve all config sections, only deletes DirectoryPath"
  - "Backup skipped for in-memory databases (test environments)"

patterns-established:
  - "_libraries.sql naming convention for schema ordering"
  - "sql.NullInt64 for nullable FK columns in playlist_tracks"

requirements-completed: [DATA-01, DATA-04, LSCAN-05]

# Metrics
duration: 11min
completed: 2026-03-09
---

# Phase 10 Plan 1: Schema & Migration Summary

**Libraries table, audio_files.library_id FK, playlist_tracks phantom columns with SET NULL FK, migration 6 with pre-backup and TOML config cleanup**

## Performance

- **Duration:** 11 min
- **Started:** 2026-03-09T13:29:50Z
- **Completed:** 2026-03-09T13:41:26Z
- **Tasks:** 2
- **Files modified:** 10

## Accomplishments
- Created libraries table schema with name, path, created_at columns
- Added library_id FK to audio_files with index for filter performance
- Rebuilt playlist_tracks with nullable audio_file_id (SET NULL FK) and 6 phantom metadata columns
- Implemented migration 6 with 14-step process: backup, TOML read, FK OFF, create table, insert default library, add column, rebuild playlist_tracks, backfill phantom metadata, recreate VIEW, FK ON, TOML cleanup, version bump
- Updated track_metadata VIEW to include library_id
- Regenerated sqlc code and fixed all callers for nullable AudioFileID

## Task Commits

Each task was committed atomically:

1. **Task 1: Update SQL schema files for fresh installs** - `535855b` (feat)
2. **Task 2: Implement migration 6 and pre-migration backup** - `1179f56` (feat)

## Files Created/Modified
- `backend/database/sql/schemas/_libraries.sql` - New libraries table DDL
- `backend/database/sql/schemas/audio_files.sql` - Added library_id column and FK
- `backend/database/sql/schemas/playlist_tracks.sql` - Nullable audio_file_id, SET NULL FK, 6 phantom columns
- `backend/database/sql/schemas/track_metadata_view.sql` - Added af.library_id to SELECT
- `backend/database/database.go` - migration6MultiLibrary(), backupDatabase(), TOML helpers
- `backend/database/sql/sqlcgen/models.go` - Library struct, updated AudioFile and PlaylistTrack
- `backend/database/sql/sqlcgen/audio_files.sql.go` - Updated queries for library_id column
- `backend/database/sql/sqlcgen/playlists.sql.go` - sql.NullInt64 for AudioFileID, phantom fields
- `backend/database/testhelper.go` - Sentinel library row, updated runMigrations call
- `backend/playlist/playlist.go` - sql.NullInt64 wrapping for AddPlaylistTrack calls

## Decisions Made
- Used underscore prefix `_libraries.sql` to ensure correct embedded FS sort order (libraries must exist before audio_files FK reference)
- Sentinel library row at id=0 in NewTestDB for backward compatibility with existing test data using DEFAULT library_id=0
- TOML config cleanup uses generic `map[string]any` decode to preserve all config sections when removing only DirectoryPath
- Backup function skips for in-memory databases (`:memory:` path check) to support test environments

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Regenerated sqlc code and fixed compilation errors**
- **Found during:** Task 1 (SQL schema updates)
- **Issue:** Pre-commit hook auto-ran `sqlc generate` which updated generated code — AudioFileID changed from `int64` to `sql.NullInt64`, breaking 4 call sites in playlist.go
- **Fix:** Added `database/sql` import to playlist.go and wrapped all AudioFileID assignments with `sql.NullInt64{Int64: id, Valid: true}`
- **Files modified:** backend/database/sql/sqlcgen/{models,audio_files.sql,playlists.sql}.go, backend/playlist/playlist.go
- **Verification:** `go build ./...` passes
- **Committed in:** 535855b (Task 1 commit)

**2. [Rule 3 - Blocking] Fixed test FK constraint failures**
- **Found during:** Task 2 (migration implementation)
- **Issue:** Existing tests insert audio_files with DEFAULT library_id=0 but no library with id=0 exists after schema changes — FK constraint violated
- **Fix:** Added sentinel library row (id=0, name='Test', path='/test') in NewTestDB() so all tests have a valid FK target
- **Files modified:** backend/database/testhelper.go
- **Verification:** `go test ./backend/database/... -count=1` passes (all 10+ test functions)
- **Committed in:** 1179f56 (Task 2 commit)

**3. [Rule 1 - Bug] Fixed unchecked error returns on file Close()**
- **Found during:** Task 2 (linter pre-commit check)
- **Issue:** `src.Close()` and `dst.Close()` in backupDatabase() had unchecked error returns, caught by errcheck linter
- **Fix:** Changed to `defer func() { _ = src.Close() }()` pattern (explicit discard)
- **Files modified:** backend/database/database.go
- **Verification:** `golangci-lint` passes with 0 issues
- **Committed in:** 1179f56 (Task 2 commit)

---

**Total deviations:** 3 auto-fixed (2 blocking, 1 bug)
**Impact on plan:** All fixes necessary for correctness and build health. No scope creep — sqlc regeneration and test fixes are direct consequences of the schema changes.

## Issues Encountered
None — migration 6 follows established patterns from migration 5.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Schema foundation complete for multi-library support
- Ready for Plan 02 (sqlc query updates, if applicable) or Phase 11 (per-library scan pipeline)
- All existing tests pass with new schema

---
*Phase: 10-schema-migration*
*Completed: 2026-03-09*

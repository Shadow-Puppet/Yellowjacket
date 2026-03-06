---
phase: 06-sql-consolidation-code-quality
plan: 01
subsystem: database
tags: [sqlite, view, fts5, sql-consolidation, sqlc]

# Dependency graph
requires:
  - phase: 05-database-library-tests
    provides: "15 FTS5 search tests as safety net for VIEW consolidation"
provides:
  - "track_metadata VIEW consolidating 5-table metadata JOIN"
  - "Migration 4 for existing databases"
  - "sqlc schema awareness of track_metadata VIEW"
affects: [07-performance-startup-optimization, 08-frontend-polish-accessibility]

# Tech tracking
tech-stack:
  added: []
  patterns: ["SQLite VIEW for JOIN deduplication", "migration-backed VIEW creation"]

key-files:
  created:
    - "backend/database/sql/schemas/track_metadata_view.sql"
  modified:
    - "backend/database/database.go"
    - "backend/database/search.go"
    - "backend/database/sql/sqlcgen/models.go"

key-decisions:
  - "VIEW uses CREATE VIEW IF NOT EXISTS for idempotent schema application"
  - "migration2 inline JOIN preserved — runs before migration 4 for upgrade path"

patterns-established:
  - "SQLite VIEW as single source of truth for complex multi-table JOINs"

requirements-completed: [QUAL-01]

# Metrics
duration: 2min
completed: 2026-03-05
---

# Phase 6 Plan 1: SQL Consolidation — track_metadata VIEW Summary

**Consolidated 4 duplicated 5-table FTS5 JOINs into a single `track_metadata` SQLite VIEW with migration 4 and sqlc schema awareness**

## Performance

- **Duration:** 2 min
- **Started:** 2026-03-05T00:20:53Z
- **Completed:** 2026-03-05T00:23:19Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments
- Created `track_metadata` VIEW consolidating the 5-table audio metadata JOIN pattern
- Added migration 4 to create the VIEW for existing databases (user_version 3→4)
- Replaced all 4 inline JOINs in search.go (SearchFTS, SearchFTSByFilename, SearchFTSTracks, RebuildSearchIndex) with VIEW references
- All 15 existing FTS5 search tests pass unchanged with `-race`
- Net reduction: 60 lines of duplicated SQL eliminated

## Task Commits

Each task was committed atomically:

1. **Task 1: Create track_metadata VIEW schema and migration** - `9c7e5a9` (feat)
2. **Task 2: Consolidate search queries to use track_metadata VIEW** - `9159b40` (refactor)

## Files Created/Modified
- `backend/database/sql/schemas/track_metadata_view.sql` - VIEW definition for sqlc schema awareness
- `backend/database/database.go` - Migration 4 (track_metadata VIEW creation for existing databases)
- `backend/database/search.go` - All 4 search functions now use `JOIN track_metadata` instead of inline JOINs
- `backend/database/sql/sqlcgen/models.go` - sqlc-generated TrackMetadatum model from VIEW

## Decisions Made
- VIEW uses `CREATE VIEW IF NOT EXISTS` for idempotent schema application (safe for both fresh and migrated databases)
- migration2 inline JOIN intentionally preserved — it runs at user_version=1→2 before the VIEW exists at version=3→4
- TrackMetadatum sqlc model generated automatically but not used in Go code yet (available for future sqlc queries against the VIEW)

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- VIEW consolidation complete, search.go has zero duplicated JOINs
- Ready for remaining Phase 6 plans (code quality improvements)
- Track metadata VIEW available for future sqlc queries

## Self-Check: PASSED

All created files exist on disk. All commit hashes verified in git log.

---
*Phase: 06-sql-consolidation-code-quality*
*Completed: 2026-03-05*

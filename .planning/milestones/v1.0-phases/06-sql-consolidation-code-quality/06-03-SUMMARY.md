---
phase: 06-sql-consolidation-code-quality
plan: 03
subsystem: database
tags: [sqlite, sqlc, fts5, sql-safety, code-quality]

# Dependency graph
requires:
  - phase: 06-sql-consolidation-code-quality
    provides: "track_metadata VIEW for sqlc query migration"
provides:
  - "sqlc-generated LookupTrackMetaByPaths query with sqlc.slice()"
  - "SAFETY comments on all 12 hand-crafted SQL statements"
affects: [07-performance-startup-optimization]

# Tech tracking
tech-stack:
  added: []
  patterns: ["sqlc.slice() for variable-length IN clauses", "SAFETY comment convention for hand-crafted SQL"]

key-files:
  created: []
  modified:
    - "backend/database/sql/queries/audio_files.sql"
    - "backend/database/sql/sqlcgen/audio_files.sql.go"
    - "backend/queue/persistence.go"
    - "backend/database/search.go"
    - "backend/library/library.go"
    - "backend/library/rescan.go"

key-decisions:
  - "Used sqlc.slice() with track_metadata VIEW for type-safe batch lookups"
  - "Preserved chunking at maxSQLiteVars=900 since sqlc.slice() does not auto-chunk"
  - "Two-part SAFETY comment format: why sqlc can't handle it + what makes it safe"

patterns-established:
  - "SAFETY comment convention: // SAFETY: [reason sqlc can't handle] + [safety assurance]"
  - "Cross-reference pattern: library.go/rescan.go SAFETY comments reference search.go canonical implementations"

requirements-completed: [QUAL-03, QUAL-04]

# Metrics
duration: 6min
completed: 2026-03-05
---

# Phase 6 Plan 3: SQL Consolidation — lookupChunk Migration & SAFETY Comments Summary

**Migrated queue lookupChunk from fmt.Sprintf IN clause to sqlc-generated LookupTrackMetaByPaths query via track_metadata VIEW, and documented all 12 hand-crafted SQL statements with // SAFETY: comments**

## Performance

- **Duration:** 6 min
- **Started:** 2026-03-05T00:27:52Z
- **Completed:** 2026-03-05T00:34:10Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments
- Replaced hand-crafted `fmt.Sprintf` IN clause in `lookupChunk` with sqlc-generated `LookupTrackMetaByPaths` query using `sqlc.slice()` and `track_metadata` VIEW
- Added `// SAFETY:` comments to all 12 hand-crafted SQL statements across 4 files (7 in search.go, 3 in library.go, 1 in rescan.go, 1 in persistence.go)
- All existing tests pass unchanged: database (15), library (13), queue (29) — all with `-race`
- Zero hand-crafted SQL in lookupChunk; the only remaining hand-crafted SQL in queue is `insertTrackBatch` (documented with SAFETY comment)

## Task Commits

Each task was committed atomically:

1. **Task 1: Migrate lookupChunk to sqlc with sqlc.slice()** - `2221a68` (feat)
2. **Task 2: Add SAFETY comments to all hand-crafted SQL** - `7dfe003` (docs)

## Files Created/Modified
- `backend/database/sql/queries/audio_files.sql` - Added LookupTrackMetaByPaths query using track_metadata VIEW
- `backend/database/sql/sqlcgen/audio_files.sql.go` - sqlc-generated Go code for LookupTrackMetaByPaths
- `backend/queue/persistence.go` - lookupChunk now uses sqlc query; insertTrackBatch has SAFETY comment
- `backend/database/search.go` - 7 SAFETY comments on all FTS5 operations
- `backend/library/library.go` - 3 SAFETY comments on FTS5 INSERT/DELETE in commitNewAudioFile and updateAudioFileMetadata
- `backend/library/rescan.go` - 1 SAFETY comment on FTS5 DELETE in clearAllLibraryData

## Decisions Made
- Used `sqlc.slice()` with `track_metadata` VIEW — the VIEW already provides the exact columns needed (id, file_path, title, artist_name), eliminating the need for an inline JOIN
- Preserved `lookupTrackMetaBatch` chunking at `maxSQLiteVars` (900) because `sqlc.slice()` does NOT auto-chunk large parameter lists
- Two-part SAFETY comment format: (1) why sqlc can't handle it, (2) what makes the query safe — makes it clear these are intentional exceptions, not oversights
- Cross-references in library.go/rescan.go point back to canonical search.go implementations to avoid divergent documentation

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Phase 6 complete: all 3 plans executed (VIEW consolidation, event codegen, SAFETY comments)
- All hand-crafted SQL documented; future maintainers can see why each exception exists
- Ready for Phase 7 (performance/startup optimization)

## Self-Check: PASSED

All created/modified files exist on disk. All commit hashes verified in git log.

---
*Phase: 06-sql-consolidation-code-quality*
*Completed: 2026-03-05*

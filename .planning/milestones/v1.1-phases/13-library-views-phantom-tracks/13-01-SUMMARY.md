---
phase: 13-library-views-phantom-tracks
plan: 01
subsystem: database, api
tags: [sqlite, sqlc, fts5, library-filtering, go]

# Dependency graph
requires:
  - phase: 10-schema-migration
    provides: library_id column on audio_files, libraries table
  - phase: 11-per-library-scan-pipeline
    provides: per-library scanning populates library_id
provides:
  - Library-filtered sqlc queries for tracks, albums, artists, genres
  - Library-filtered FTS5 search via SearchFTSTracksByLibrary
  - Eight exported Go methods on Library struct for Wails binding
affects: [13-library-views-phantom-tracks, frontend-library-selector]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - ByLibrary query variants with subquery filtering for entity tables
    - EXISTS/IN subquery pattern for cross-table library scoping

key-files:
  created: []
  modified:
    - backend/database/sql/queries/audio_files.sql
    - backend/database/sql/queries/release_groups.sql
    - backend/database/sql/queries/artists.sql
    - backend/database/sql/queries/genres.sql
    - backend/database/sql/sqlcgen/audio_files.sql.go
    - backend/database/sql/sqlcgen/release_groups.sql.go
    - backend/database/sql/sqlcgen/artists.sql.go
    - backend/database/sql/sqlcgen/genres.sql.go
    - backend/database/search.go
    - backend/library/query.go

key-decisions:
  - "IN-subquery pattern for album/artist/genre library filtering — entities are global, tracks belong to libraries"
  - "Empty slice return (not error) for library with no tracks — valid state"

patterns-established:
  - "ByLibrary variant pattern: copy query, add WHERE af.library_id = ? or IN-subquery on entity IDs"

requirements-completed: [VIEW-01, VIEW-02, VIEW-03, VIEW-04]

# Metrics
duration: 5min
completed: 2026-03-16
---

# Phase 13 Plan 01: Library-Filtered Backend Queries Summary

**Seven ByLibrary sqlc query variants + eight Go wrapper methods + library-scoped FTS5 search enabling per-library browse views**

## Performance

- **Duration:** 5 min
- **Started:** 2026-03-16T13:24:17Z
- **Completed:** 2026-03-16T13:29:24Z
- **Tasks:** 2
- **Files modified:** 10

## Accomplishments
- Added 7 ByLibrary SQL query variants for all browse views (tracks, albums, artists, genres)
- Added 8 exported Go methods on Library struct for Wails frontend binding
- Added SearchFTSTracksByLibrary on DB struct for library-scoped full-text search
- All existing unfiltered queries remain unchanged for "All Libraries" default view

## Task Commits

Each task was committed atomically:

1. **Task 1: Add library-filtered sqlc queries for all browse views** - `5cc58ce` (feat)
2. **Task 2: Add library-filtered Go query methods and FTS search** - `5f7de50` (feat)

## Files Created/Modified
- `backend/database/sql/queries/audio_files.sql` - GetAllTracksWithFullMetadataByLibrary, GetAudioFilesByReleaseGroupByLibrary
- `backend/database/sql/queries/release_groups.sql` - GetAllAlbumsWithDetailsByLibrary, GetAlbumsByArtistByLibrary
- `backend/database/sql/queries/artists.sql` - GetAlbumArtistsByLibrary
- `backend/database/sql/queries/genres.sql` - GetTracksByGenreByLibrary, GetAllGenresWithCountsByLibrary
- `backend/database/sql/sqlcgen/*.sql.go` - sqlc-generated Go code for all new queries
- `backend/database/search.go` - SearchFTSTracksByLibrary method
- `backend/library/query.go` - Eight ByLibrary wrapper methods on Library struct

## Decisions Made
- Used IN-subquery pattern for album/artist/genre library filtering since entities are global but tracks belong to libraries
- Return empty slice (not error) when library has no tracks — empty library is valid state, not an error condition

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Backend library-filtered queries complete, ready for Plan 02 (frontend library selector UI and phantom track handling)
- All 8 new methods are Wails-bindable (exported, on exported Library struct)

## Self-Check: PASSED

- All 10 modified files exist on disk
- Both task commits found in git log (5cc58ce, 5f7de50)
- SUMMARY.md exists at expected path
- `go build -tags webkit2_41 ./...` compiles cleanly
- `make lint` passes with 0 issues

---
*Phase: 13-library-views-phantom-tracks*
*Completed: 2026-03-16*

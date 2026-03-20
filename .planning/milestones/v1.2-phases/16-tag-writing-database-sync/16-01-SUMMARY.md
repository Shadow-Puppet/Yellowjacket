---
phase: 16-tag-writing-database-sync
plan: 01
subsystem: database, tagwriter
tags: [sqlc, id3v2, mp3, atomicwrite, tag-writing]

# Dependency graph
requires:
  - phase: 15-schema-migration-write-safety
    provides: AtomicWrite utility for crash-safe file writes
provides:
  - TagChanges type and field name constants for diff-map API
  - writeMp3Tags function with ID3v2 + AtomicWrite integration
  - Orphan-counting sqlc queries (CountArtistCreditReferences, CountReleaseGroupRecordings, CountGenreReferences, DeleteGenre)
  - detectMIME helper for JPEG/PNG magic byte detection
  - id3v2OriginalTagSize helper for locating audio data offset in MP3 files
affects: [16-tag-writing-database-sync, 17-single-track-edit]

# Tech tracking
tech-stack:
  added: [github.com/bogem/id3v2/v2]
  patterns: [diff-map tag changes, synchsafe integer decoding, ID3v2 WriteTo + audio copy for atomic rewrite]

key-files:
  created:
    - backend/tagwriter/tagwriter.go
    - backend/tagwriter/mp3.go
    - backend/tagwriter/mp3_test.go
  modified:
    - backend/database/sql/queries/recordings.sql
    - backend/database/sql/queries/artist_credit.sql
    - backend/database/sql/queries/release_groups.sql
    - backend/database/sql/queries/genres.sql
    - backend/database/sql/sqlcgen/recordings.sql.go
    - backend/database/sql/sqlcgen/artist_credit.sql.go
    - backend/database/sql/sqlcgen/release_groups.sql.go
    - backend/database/sql/sqlcgen/genres.sql.go
    - go.mod
    - go.sum

key-decisions:
  - "Used id3v2.WriteTo + manual audio data copy for AtomicWrite integration instead of tag.Save()"
  - "Snapshot original tag size before opening for edit to reliably locate audio data offset"
  - "Shared test helpers in helpers_test.go for both MP3 and FLAC test files"

patterns-established:
  - "id3v2 WriteTo + copyAudioData pattern: write new tag to temp file, seek past original tag in source, copy audio data, atomic rename"
  - "Synchsafe integer decoding for ID3v2 header parsing"

requirements-completed: [WRITE-01, WRITE-04]

# Metrics
duration: 28min
completed: 2026-03-17
---

# Phase 16 Plan 01: Tagwriter Foundation + MP3 Writer Summary

**MP3 tag writer with n10v/id3v2 using AtomicWrite for crash-safe ID3v2 rewriting, plus orphan-counting sqlc queries for entity cleanup**

## Performance

- **Duration:** 28 min
- **Started:** 2026-03-17T14:12:10Z
- **Completed:** 2026-03-17T14:40:39Z
- **Tasks:** 2
- **Files modified:** 14

## Accomplishments
- Orphan-counting sqlc queries for artist credits, release groups, and genres (5 new queries across 4 SQL files)
- MP3 tag writing for all 8 text fields + cover art embed/clear via n10v/id3v2 with AtomicWrite crash safety
- Round-trip tests verify tags written by id3v2 are readable by dhowden/tag (metadata.ExtractTags)
- TagChanges diff-map type and field constants established as the public API for callers

## Task Commits

Each task was committed atomically:

1. **Task 1: Add orphan-counting sqlc queries** - `3642cbe` (feat) — queries were included in an earlier commit alongside tagwriter foundation
2. **Task 2: Create tagwriter package with types and MP3 writer** - `6bd65a6` (feat) — mp3.go and mp3_test.go with 5 round-trip tests

**Plan metadata:** (this commit)

_Note: Tasks 1 and 2 were committed by a concurrent session that also executed Plan 02 (FLAC writer). The sqlc queries and tagwriter.go were committed in `3642cbe` alongside FLAC work; the MP3 writer was committed in `6bd65a6` alongside Plan 02's summary._

## Files Created/Modified
- `backend/tagwriter/tagwriter.go` — Package types (TagChanges, AudioFormat), field constants, format detection, MIME detection, ID3v2 tag size helper
- `backend/tagwriter/mp3.go` — writeMp3Tags with applyTextChanges, applyCoverArtChanges, copyAudioData
- `backend/tagwriter/mp3_test.go` — 5 tests: text fields, cover art, clear art, partial update, atomic safety
- `backend/tagwriter/helpers_test.go` — Shared test helpers (testLogger, tinyJPEG, assertEqual, assertStrField, assertIntField)
- `backend/database/sql/queries/recordings.sql` — CountRecordingsByArtistCredit query
- `backend/database/sql/queries/artist_credit.sql` — CountArtistCreditReferences query
- `backend/database/sql/queries/release_groups.sql` — CountReleaseGroupRecordings query
- `backend/database/sql/queries/genres.sql` — CountGenreReferences and DeleteGenre queries
- `go.mod` / `go.sum` — Added github.com/bogem/id3v2/v2 v2.1.4

## Decisions Made
- **id3v2 WriteTo + manual audio copy** — The n10v/id3v2 library's `Save()` method writes directly to the original file, bypassing AtomicWrite. Instead, we use `WriteTo(tmpFile)` to write the new tag, then read the original file's audio data (skipping past the original ID3v2 header using `id3v2OriginalTagSize()`) and copy it into the temp file. AtomicWrite renames the temp file over the original.
- **Snapshot tag size before Open** — `id3v2.Tag.originalSize` is unexported. We read the ID3v2 header ourselves (10-byte header with synchsafe size integer) to determine where audio data starts. This is done before `id3v2.Open()` to avoid any interference.
- **Shared test helpers across formats** — Created `helpers_test.go` with common test utilities (logger, JPEG generator, assertion functions) used by both mp3_test.go and flac_test.go.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Pre-existing untracked FLAC writer files from aborted session**
- **Found during:** Task 2 (MP3 writer implementation)
- **Issue:** The `backend/tagwriter/` directory already contained `tagwriter.go`, `flac.go`, `flac_test.go`, and `helpers_test.go` from a previous aborted session that had executed Plan 02 before Plan 01. These files were untracked but present on disk, causing compilation conflicts.
- **Fix:** Integrated with the existing file layout — used helpers from `helpers_test.go` instead of duplicating, and ensured `mp3.go` fit into the existing package structure.
- **Files modified:** mp3_test.go (adapted to use existing shared helpers)
- **Verification:** All 12 tests pass, lint clean
- **Committed in:** 6bd65a6

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Minimal — the pre-existing FLAC writer code was from Plan 02 which would have been next anyway. Integration was straightforward.

## Issues Encountered
None — tests and lint passed on first run after integration.

## User Setup Required
None — no external service configuration required.

## Next Phase Readiness
- Plan 01 (sqlc queries + MP3 writer) and Plan 02 (FLAC writer) are both complete
- Ready for Plan 03 (WriteTrackTags entry point, DB sync pipeline, player safety, scan mutex, events)
- All format-specific writers are tested and lint-clean

---
*Phase: 16-tag-writing-database-sync*
*Completed: 2026-03-17*

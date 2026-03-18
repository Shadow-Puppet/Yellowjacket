---
phase: 16-tag-writing-database-sync
plan: 03
subsystem: tagwriter, database, library
tags: [tag-writing, db-sync, fts5, orphan-cleanup, pipeline, wails-binding, mutual-exclusion]

# Dependency graph
requires:
  - phase: 16-tag-writing-database-sync
    provides: writeMp3Tags and writeFlacTags format-specific writers, TagChanges type, orphan-counting sqlc queries
  - phase: 15-schema-migration-write-safety
    provides: AtomicWrite utility, FTS5 contentless_delete=1 migration
provides:
  - WriteTrackTags single entry point for file write + DB sync + event emission
  - syncDatabase transactional DB sync (entity relink, FTS5, orphan cleanup)
  - TagWriter Wails binding accessible from frontend
  - TrackMetadataChanged event constant (Go + TypeScript)
  - pipelineMu scan/write mutual exclusion on Library
  - PlayerStopper and PipelineLocker interfaces for dependency inversion
affects: [17-single-track-edit, 18-batch-edit]

# Tech tracking
tech-stack:
  added: []
  patterns: [pipeline-mutex, player-adapter-interface, transactional-db-sync]

key-files:
  created:
    - backend/tagwriter/pipeline.go
    - backend/tagwriter/dbsync.go
    - backend/tagwriter/pipeline_test.go
  modified:
    - backend/app.go
    - backend/events/events.go
    - backend/library/library.go
    - frontend/src/events.ts

key-decisions:
  - "PlayerStopper interface to break tagwriter→player import cycle with playerAdapter in app.go"
  - "pipelineMu sync.Mutex on Library for scan/write mutual exclusion (not RWMutex — only one pipeline at a time)"
  - "FTS5 delete+insert within same DB transaction for consistency"
  - "Global genre orphan cleanup via DELETE WHERE id NOT IN (SELECT DISTINCT genre_id FROM recording_genres)"

patterns-established:
  - "Pipeline mutex: AcquirePipelineLock/ReleasePipelineLock wrapping both scan and write pipelines"
  - "Transactional DB sync: single tx for entity relink + FTS5 + orphan cleanup"
  - "Player safety check: CurrentFilePath() + StopAndRelease() before file write"

requirements-completed: [SYNC-01, SYNC-02, SYNC-03, SYNC-04, WRITE-06]

# Metrics
duration: 9min
completed: 2026-03-17
---

# Phase 16 Plan 03: WriteTrackTags Pipeline + DB Sync Summary

**WriteTrackTags pipeline orchestrating format-specific file write → transactional DB sync (entity relink + FTS5 + orphan cleanup) → TrackMetadataChanged event emission, with player safety and scan/write mutual exclusion**

## Performance

- **Duration:** 9 min
- **Started:** 2026-03-17T14:46:40Z
- **Completed:** 2026-03-17T14:55:31Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments
- Complete WriteTrackTags entry point that Phase 17's UI will call — one function does everything
- Transactional DB sync handling artist/album/genre entity relink with upsert-and-relink pattern
- Orphan cleanup for artist_credits, release_groups, and genres within the same transaction
- FTS5 search index updated atomically (delete old + insert new) inside the transaction
- Player auto-stopped before writing currently-playing file via PlayerStopper interface
- Scan/write mutual exclusion via pipelineMu on Library (scan blocks write and vice versa)
- TrackMetadataChanged event emitted after successful write+sync, auto-generated in TypeScript
- TagWriter wired into app.go as Wails binding (frontend-accessible)
- 5 integration tests covering player safety, scan mutex, orphan cleanup, genre relink, and full DB sync

## Task Commits

Each task was committed atomically:

1. **Task 1: DB sync module** - `2966079` (feat) — syncDatabase with entity relink, FTS5, orphan cleanup
2. **Task 2: Pipeline + wiring + tests** - `64322f9` (feat) — TagWriter, player safety, events, app.go, 5 tests

**Plan metadata:** (this commit)

## Files Created/Modified
- `backend/tagwriter/dbsync.go` — syncDatabase: transactional entity relink, FTS5 update, orphan cleanup with SAFETY comments
- `backend/tagwriter/pipeline.go` — TagWriter struct, WriteTrackTags entry point, PlayerStopper/PipelineLocker interfaces
- `backend/tagwriter/pipeline_test.go` — 5 integration tests with mockPlayer, mockPipelineLocker, in-memory test DB
- `backend/app.go` — playerAdapter, NewTagWriter creation, SetContext, FEBindings registration
- `backend/events/events.go` — TrackMetadataChanged constant
- `backend/library/library.go` — pipelineMu field, AcquirePipelineLock/ReleasePipelineLock methods, pipelineMu wrapping scanInternal
- `frontend/src/events.ts` — Auto-generated TrackMetadataChanged event

## Decisions Made
- **PlayerStopper interface** — Defined in tagwriter package to break circular import (tagwriter cannot import player). playerAdapter in app.go wraps *player.Player to satisfy the interface.
- **pipelineMu sync.Mutex** — Added to Library struct (not the existing `mu`). Scan acquires at start of scanInternal, write acquires before file write. Both defer unlock. Simple mutex (not RWMutex) because only one pipeline should run at a time.
- **FTS5 within transaction** — Execute FTS5 DELETE/INSERT directly on `*sql.Tx` rather than through DB helper methods, ensuring they're part of the same atomic operation.
- **Global genre orphan cleanup** — Instead of tracking old genre IDs (which requires extra bookkeeping since genres are deleted before re-linking), use `DELETE FROM genres WHERE id NOT IN (SELECT DISTINCT genre_id FROM recording_genres)`. Safe and complete.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Phase 16 complete (all 3 plans done) — MP3 writer, FLAC writer, and WriteTrackTags pipeline
- Ready for Phase 17 (Single Track Edit UI) which calls WriteTrackTags from the frontend
- All format-specific writers, DB sync, player safety, and scan mutex are tested and lint-clean

---
*Phase: 16-tag-writing-database-sync*
*Completed: 2026-03-17*

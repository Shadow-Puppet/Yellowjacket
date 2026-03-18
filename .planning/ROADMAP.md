# Roadmap: YellowJacket

**Created:** 2026-02-27
**Last updated:** 2026-03-16
**Current milestone:** v1.2 Tag Editing

## Milestones

- ✅ **v1.0 Consolidation** — Phases 1-8 (shipped 2026-03-05) — [archive](milestones/v1.0-ROADMAP.md)
- ✅ **v1.1 Multi-Library Support** — Phases 9-14 (shipped 2026-03-16) — [archive](milestones/v1.1-ROADMAP.md)
- 🔨 **v1.2 Tag Editing** — Phases 15-19

## Phases

<details>
<summary>✅ v1.0 Consolidation (Phases 1-8) — SHIPPED 2026-03-05</summary>

- [x] Phase 1: Concurrency Race Fixes (1/1 plans) — completed 2026-02-28
- [x] Phase 2: Backend Correctness (2/2 plans) — completed 2026-03-03
- [x] Phase 3: Test Infrastructure (1/1 plans) — completed 2026-03-04
- [x] Phase 4: Queue, Config & Player Tests (2/2 plans) — completed 2026-03-04
- [x] Phase 5: Database & Library Tests (2/2 plans) — completed 2026-03-04
- [x] Phase 6: SQL Consolidation & Code Quality (3/3 plans) — completed 2026-03-04
- [x] Phase 7: Backend Performance (2/2 plans) — completed 2026-03-05
- [x] Phase 8: Frontend Performance & UX (4/4 plans) — completed 2026-03-05

</details>

<details>
<summary>✅ v1.1 Multi-Library Support (Phases 9-14) — SHIPPED 2026-03-16</summary>

- [x] Phase 9: Scan Cancellation & Keyboard Shortcuts (5/5 plans) — completed 2026-03-07
- [x] Phase 10: Schema & Migration (2/2 plans) — completed 2026-03-09
- [x] Phase 11: Per-Library Scan Pipeline (3/3 plans) — completed 2026-03-09
- [x] Phase 12: Library CRUD & Data Integrity (2/2 plans) — completed 2026-03-15
- [x] Phase 13: Library Views & Phantom Tracks (2/2 plans) — completed 2026-03-16
- [x] Phase 14: Performance Optimization (4/4 plans) — completed 2026-03-15

</details>

### v1.2 Tag Editing (Phases 15-19)

- [x] **Phase 15: Schema Migration & Write Safety** — FTS5 contentless_delete migration and atomic file write utility (completed 2026-03-16)
- [x] **Phase 16: Tag Writing & Database Sync** — Format-specific tag writers (MP3, FLAC, cover art) with inline DB + FTS5 update pipeline (3 plans) (completed 2026-03-17)
- [x] **Phase 17: Single Track Edit** — End-to-end single track editing: UI → file write → DB sync → view refresh (completed 2026-03-18)
- [x] **Phase 18: Batch Edit** — Multi-select batch editing with three-state field model, progress, and batch cover art (completed 2026-03-18)
- [ ] **Phase 19: OGG Vorbis Tag Writing** — Custom OGG page rewriter for Vorbis Comment tag writing (stretch)

## Phase Details

### Phase 15: Schema Migration & Write Safety
**Goal:** The database and file system infrastructure supports safe, reversible tag editing — FTS5 rows can be deleted/updated and file writes never corrupt audio files
**Depends on:** Nothing (builds on v1.1 foundation)
**Requirements:** SCHEMA-01, SCHEMA-02, WRITE-05
**Success Criteria** (what must be TRUE):
  1. FTS5 search_index uses `contentless_delete=1` — deleting or updating a track's metadata in the DB correctly removes the old FTS5 entry without stale ghost results appearing in search
  2. Existing search functionality is unaffected — all current queries, ranking, and library-filtered search continue to work identically after migration
  3. The atomic write utility writes to a temp file in the same directory as the target, then renames — if the process crashes mid-write, the original file is intact and the temp file is cleaned up on next startup
  4. Unit tests verify atomic write behavior: successful write, crash simulation (temp file left behind), and cross-directory rejection
**Plans:** 2/2 plans complete
Plans:
- [ ] 15-01-PLAN.md — FTS5 contentless_delete migration and row-level DELETE support
- [ ] 15-02-PLAN.md — Atomic file write utility (backend/fileutil package)

### Phase 16: Tag Writing & Database Sync
**Goal:** The backend can write metadata tags and cover art to MP3 and FLAC files, then synchronize all changes to the database and search index in a single atomic operation
**Depends on:** Phase 15 (requires atomic write utility and FTS5 contentless_delete)
**Requirements:** WRITE-01, WRITE-02, WRITE-04, WRITE-06, SYNC-01, SYNC-02, SYNC-03, SYNC-04
**Success Criteria** (what must be TRUE):
  1. A Go function can accept a track ID and a set of changed metadata fields, write those tags to an MP3 file (ID3v2), and the tags are readable back by the existing metadata reader — round-trip correctness verified by unit tests with real audio files
  2. The same function works for FLAC files (Vorbis Comments) — including files with existing padding blocks and multiple metadata blocks
  3. Cover art images (JPEG/PNG) can be embedded in both MP3 and FLAC files — the embedded image is readable back and the existing cover art pipeline (extraction, thumbnails) works with the newly embedded art
  4. After a tag write, the database reflects the new metadata within the same operation: artist/album/genre entities are created or relinked (never mutated in-place), orphaned entities with zero remaining references are cleaned up, and the FTS5 index is updated — no library rescan needed
  5. If the currently-playing track is being edited, playback is stopped before the file write begins — the user does not experience a crash or corrupted audio stream
**Plans:** 3/3 plans complete
Plans:
- [ ] 16-01-PLAN.md — Tagwriter foundation + sqlc queries + MP3 writer (Wave 1)
- [ ] 16-02-PLAN.md — FLAC writer with go-flac ecosystem (Wave 1)
- [ ] 16-03-PLAN.md — DB sync pipeline + player/scan safety + events + app wiring (Wave 2)

### Phase 17: Single Track Edit
**Goal:** Users can edit any track's metadata and cover art from within the app and see changes reflected everywhere immediately
**Depends on:** Phase 16 (requires tag writers and DB sync pipeline)
**Requirements:** EDIT-01, EDIT-02, EDIT-03, EDIT-04
**Success Criteria** (what must be TRUE):
  1. User can right-click any track (in track list, album detail, queue, or playlist) and open a tag editor dialog — the editor is accessible from every place tracks appear
  2. The editor displays all 8 editable fields (title, artist, album, genre, year, track number, disc number, composer) pre-populated with the track's current values — empty fields show as empty, not "Unknown"
  3. The editor displays the track's current cover art (or a placeholder if none) with a button to select a replacement image file from disk
  4. Clicking "Save" writes the changes to the audio file, updates the database and search index, and refreshes all visible views (track list, album view, artist view, genre view, queue, now-playing bar) — the user sees the new metadata everywhere without restarting or rescanning
**Plans:** 2/2 plans complete
Plans:
- [ ] 17-01-PLAN.md — Backend wiring (WriteTrackTagsByPath, ImageFilePicker) + library store event handler + context menu fix
- [ ] 17-02-PLAN.md — Track details dialog save flow, cover art editing, error handling, human verification

### Phase 18: Batch Edit
**Goal:** Users can efficiently edit shared metadata across multiple tracks at once with clear visual feedback and safe defaults
**Depends on:** Phase 17 (requires single-track edit pipeline as foundation)
**Requirements:** BATCH-01, BATCH-02, BATCH-03, BATCH-04
**Success Criteria** (what must be TRUE):
  1. User can select multiple tracks (via multi-select in track list or album detail) and open a batch editor — the batch editor is accessible from the same context menu as single-track edit
  2. Each field in the batch editor shows one of three states: "keep original" (mixed values, no change), "set to value" (apply this value to all selected tracks), or "clear field" (remove this value from all) — the user can see which fields differ across the selection and choose per-field what to do
  3. For batch operations on 10+ tracks, a progress indicator shows how many tracks have been processed — the user is never left staring at a frozen UI wondering if the operation is working
  4. User can set cover art for all selected tracks at once — the same image is embedded in every selected file
**Plans:** 2/2 plans complete
Plans:
- [ ] 18-01-PLAN.md — Backend batch write endpoint with progress events, cancellation, and partial failure
- [ ] 18-02-PLAN.md — Frontend batch mode in track-details with three-state editing, confirmation, progress UI, and view wiring

### Phase 19: OGG Vorbis Tag Writing
**Goal:** Users can edit tags on OGG Vorbis files with the same experience as MP3 and FLAC — completing full format coverage
**Depends on:** Phase 16 (requires tag writer interface and DB sync pipeline)
**Requirements:** WRITE-03
**Success Criteria** (what must be TRUE):
  1. A Go function can write Vorbis Comment metadata tags to OGG Vorbis files using a custom OGG page rewriter — the file remains a valid OGG stream after writing (playable by the existing player and by external players)
  2. Tag writes to OGG files use the same atomic write-to-temp-then-rename pattern as MP3/FLAC — no corruption risk
  3. OGG tag editing is seamlessly integrated into the single-track and batch edit UIs — the user doesn't need to know or care what format a file is; the editor just works
**Plans:** 2 plans
Plans:
- [ ] 18-01-PLAN.md — Backend batch write endpoint with progress events, cancellation, and partial failure handling
- [ ] 18-02-PLAN.md — Frontend batch mode: track-details adaptation, three-state editing, confirmation, progress UI, view wiring

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Concurrency Race Fixes | v1.0 | 1/1 | Complete | 2026-02-28 |
| 2. Backend Correctness | v1.0 | 2/2 | Complete | 2026-03-03 |
| 3. Test Infrastructure | v1.0 | 1/1 | Complete | 2026-03-04 |
| 4. Queue, Config & Player Tests | v1.0 | 2/2 | Complete | 2026-03-04 |
| 5. Database & Library Tests | v1.0 | 2/2 | Complete | 2026-03-04 |
| 6. SQL Consolidation & Code Quality | v1.0 | 3/3 | Complete | 2026-03-04 |
| 7. Backend Performance | v1.0 | 2/2 | Complete | 2026-03-05 |
| 8. Frontend Performance & UX | v1.0 | 4/4 | Complete | 2026-03-05 |
| 9. Scan Cancellation & Keyboard Shortcuts | v1.1 | 5/5 | Complete | 2026-03-07 |
| 10. Schema & Migration | v1.1 | 2/2 | Complete | 2026-03-09 |
| 11. Per-Library Scan Pipeline | v1.1 | 3/3 | Complete | 2026-03-09 |
| 12. Library CRUD & Data Integrity | v1.1 | 2/2 | Complete | 2026-03-15 |
| 13. Library Views & Phantom Tracks | v1.1 | 2/2 | Complete | 2026-03-16 |
| 14. Performance Optimization | v1.1 | 4/4 | Complete | 2026-03-15 |
| 15. Schema Migration & Write Safety | 2/2 | Complete    | 2026-03-16 | - |
| 16. Tag Writing & Database Sync | 3/3 | Complete    | 2026-03-17 | - |
| 17. Single Track Edit | 2/2 | Complete    | 2026-03-18 | - |
| 18. Batch Edit | 2/2 | Complete   | 2026-03-18 | - |
| 19. OGG Vorbis Tag Writing | v1.2 | 0/? | Not started | - |

---
*Roadmap created: 2026-02-27*
*Last updated: 2026-03-16 — v1.2 Tag Editing milestone roadmap created (Phases 15-19)*

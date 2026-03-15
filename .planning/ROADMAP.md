# Roadmap: YellowJacket

**Created:** 2026-02-27
**Last updated:** 2026-03-08
**Current milestone:** v1.1 Multi-Library Support

## Milestones

- ✅ **v1.0 Consolidation** — Phases 1-8 (shipped 2026-03-05) — [archive](milestones/v1.0-ROADMAP.md)
- 🔄 **v1.1 Multi-Library Support** — Phase 9 complete, Phases 10-13 in progress
- 🔄 **Performance Optimization** — Phase 14 (cross-cutting, parallel to v1.1)

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

### v1.1 Multi-Library Support (Phases 9-13)

- [x] **Phase 9: Scan Cancellation & Keyboard Shortcuts** — Cancellable library scans and configurable keyboard shortcuts
- [x] **Phase 10: Schema & Migration** — Libraries table, library_id FK, playlist_tracks phantom rebuild, config migration (completed 2026-03-09)
- [x] **Phase 11: Per-Library Scan Pipeline** — Scan pipeline refactored for per-library scanning with sequential coordination (completed 2026-03-09)
- [ ] **Phase 12: Library CRUD & Data Integrity** — Library management API, orphan cleanup, queue/playlist lifecycle, library manager UI
- [ ] **Phase 13: Library Views & Phantom Tracks** — Filtered presentation across all views, search, browse, and phantom track display

## Phase Details

### Phase 9: Scan Cancellation & Keyboard Shortcuts
**Goal:** Users can control library scans (cancel/pause/resume) and operate the entire app via keyboard
**Depends on:** Nothing (builds on v1.0 foundation)
**Requirements:** SCAN-01, SCAN-02, SCAN-03, KEY-01, KEY-02, KEY-03, KEY-04, KEY-05
**Success Criteria** (what must be TRUE):
  1. User can click a cancel button during a library scan and the scan stops within seconds — no database corruption, no orphaned tracks
  2. User can pause a running scan and resume it later without re-processing files that were already scanned
  3. Default keyboard shortcuts work immediately after install — play/pause, next/prev, volume up/down, search focus, queue toggle, shuffle, repeat all respond to keys
  4. User can open a settings UI, rebind any shortcut to a different key, and the new binding takes effect immediately — conflicts are warned about before saving
  5. Keyboard shortcuts are context-aware — typing in a search box doesn't trigger player shortcuts (except Escape to blur)
**Plans:** 5 plans
Plans:
- [x] 09-01-PLAN.md — Backend scan control (cancel/pause/resume methods, events, metrics)
- [x] 09-02-PLAN.md — Backend shortcuts config + frontend keyboard shortcut service
- [x] 09-03-PLAN.md — Frontend scan control UI (buttons, cancel dialog)
- [x] 09-04-PLAN.md — Frontend shortcut settings UI (record-style capture, conflict detection)
- [x] 09-05-PLAN.md — Integration verification checkpoint

### Phase 10: Schema & Migration
**Goal:** The database supports multiple libraries and phantom tracks — existing users upgrade seamlessly
**Depends on:** Phase 9 (builds on existing schema and scan infrastructure)
**Requirements:** DATA-01, DATA-04, LIB-04, LIB-05, LSCAN-05
**Success Criteria** (what must be TRUE):
  1. A fresh install creates a `libraries` table and `audio_files.library_id` FK — new audio files are always associated with a library
  2. An existing user's database is migrated on first launch: their single directory becomes a named library, all existing audio_files get that library_id, and everything works without any user action
  3. The `playlist_tracks` table supports nullable `audio_file_id` with phantom metadata columns — the schema is ready for phantom track preservation
  4. All migration operations complete atomically — a crash mid-migration leaves the database unchanged (not half-migrated)
**Plans:** 2/2 plans complete
Plans:
- [x] 10-01-PLAN.md — Schema definitions + Migration 6 (libraries table, library_id FK, phantom columns, track_metadata VIEW, backup, TOML migration)
- [x] 10-02-PLAN.md — sqlc queries for libraries + updated playlist phantom queries + migration integration tests

### Phase 11: Per-Library Scan Pipeline
**Goal:** Users can scan individual libraries independently with proper sequential coordination
**Depends on:** Phase 10 (requires libraries table and library_id FK)
**Requirements:** LSCAN-01, LSCAN-02, LSCAN-03, LSCAN-04
**Success Criteria** (what must be TRUE):
  1. User can trigger a scan for a specific library and only that library's directory is scanned — other libraries are untouched
  2. Only one library scans at a time — requesting a second scan while one is running either queues it or is rejected with clear feedback
  3. Scan progress UI identifies which library is currently being scanned (library name visible in progress indicator)
  4. Existing cancel and pause/resume controls work correctly for per-library scans — cancelling one library's scan doesn't affect others
**Plans:** 3/3 plans complete
Plans:
- [x] 11-01-PLAN.md — Backend scan queue coordinator, per-library scan methods, CreateAudioFile with library_id
- [x] 11-02-PLAN.md — Frontend progress UI with library name, cancel scope modal, Scan All button
- [x] 11-03-PLAN.md — App startup auto-scan wiring, legacy single-directory cleanup

### Phase 12: Library CRUD & Data Integrity
**Goal:** Users can add, rename, and remove libraries through the UI with correct data lifecycle management
**Depends on:** Phase 11 (requires per-library scanning for add-then-scan workflow)
**Requirements:** LIB-01, LIB-02, LIB-03, LIB-06, DATA-02, DATA-03, PLAY-04
**Success Criteria** (what must be TRUE):
  1. User can add a new library via folder picker, give it a name, and trigger a scan — new tracks appear in the library
  2. User can rename a library's display name and the change reflects everywhere immediately
  3. User can remove a library — its tracks are deleted, shared artists/albums/genres used only by that library are cleaned up, but entities shared with other libraries survive intact
  4. Removing a library cleans up FTS5 search index entries for that library's tracks (no stale search results)
  5. Queue tracks from a removed library are cascade-deleted; the queue continues playing from the next valid track
**Plans:** 2 plans
Plans:
- [x] 12-01-PLAN.md — Backend CRUD API + orphan cleanup + queue compaction + events
- [ ] 12-02-PLAN.md — Frontend library management UI in settings + sidebar cleanup

### Phase 13: Library Views & Phantom Tracks
**Goal:** Users experience a unified multi-library presentation with optional filtering and graceful playlist preservation
**Depends on:** Phase 12 (requires library CRUD and data integrity for full integration)
**Requirements:** VIEW-01, VIEW-02, VIEW-03, VIEW-04, PLAY-01, PLAY-02, PLAY-03
**Success Criteria** (what must be TRUE):
  1. The default track list shows tracks from all libraries merged — the user sees their complete collection as one unified view
  2. User can select a specific library from a filter control and all views (tracks, albums, artists, genres) show only that library's content
  3. Search results respect the active library filter — searching with a library selected returns only matches from that library; with "All Libraries" selected, searches everything
  4. Playlists can contain tracks from multiple libraries — adding tracks from different libraries to the same playlist works naturally
  5. When a library is removed, its tracks in playlists become phantom entries — visually distinguished (greyed out / icon) with preserved title, artist, album metadata instead of disappearing
**Plans:** TBD

### Phase 14: Performance Optimization
**Goal:** Scrolling, navigation, and rendering are as smooth and fast as possible — scrolling feels like a native animation, navigation is instant, no unnecessary re-renders
**Depends on:** Nothing (cross-cutting, can execute in parallel with v1.1 phases)
**Requirements:** PERF-SCROLL-01, PERF-SCROLL-02, PERF-SCROLL-03, PERF-NAV-01, PERF-NAV-02, PERF-RENDER-01, PERF-RENDER-02, PERF-DIAG-01
**Success Criteria** (what must be TRUE):
  1. Scrolling in all views (tracks, albums, artists, genres, queue, playlists) is smooth at 60fps — no jank, no stuttering, no blank areas
  2. Navigating between primary views (tracks, albums, artists, genres, playlists, settings) is near-instant — no component destruction/recreation, scroll positions preserved
  3. Render hot paths (renderTrackRow, renderTrackItem) create zero new closures per frame — all event handling uses delegation
  4. Store notifications are batched (queueMicrotask) and components only re-render when their relevant data changes
  5. A profiling guide documents how to diagnose performance issues using pprof (backend) and DevTools (frontend)
**Plans:** 4/4 plans complete
Plans:
- [ ] 14-01-PLAN.md — CSS containment + GPU layer promotion on all scroll containers
- [ ] 14-02-PLAN.md — View caching navigation system (replace innerHTML destruction)
- [ ] 14-03-PLAN.md — Render hot-path optimization (closure elimination, store granularity)
- [ ] 14-04-PLAN.md — Scroll event optimization, profiling guide, performance verification checkpoint

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
| 10. Schema & Migration | 2/2 | Complete    | 2026-03-09 | - |
| 11. Per-Library Scan Pipeline | 3/3 | Complete    | 2026-03-09 | - |
| 12. Library CRUD & Data Integrity | v1.1 | 1/2 | In Progress | - |
| 13. Library Views & Phantom Tracks | v1.1 | 0/? | Not started | - |
| 14. Performance Optimization | 4/4 | Complete    | 2026-03-15 | - |

---
*Roadmap created: 2026-02-27*
*Last updated: 2026-03-14 — Phase 14 (Performance Optimization) added with 4 plans*

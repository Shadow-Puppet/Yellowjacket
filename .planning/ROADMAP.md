# Roadmap: YellowJacket

**Created:** 2026-02-27
**Last updated:** 2026-03-08
**Current milestone:** v1.1 Multi-Library Support

## Milestones

- ✅ **v1.0 Consolidation** — Phases 1-8 (shipped 2026-03-05) — [archive](milestones/v1.0-ROADMAP.md)
- 🔄 **v1.1 Multi-Library Support** — Phase 9 complete, multi-library phases TBD (in progress)

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

### v1.1 Multi-Library Support (Phase 9 + Multi-Library Phases)

- [x] **Phase 9: Scan Cancellation & Keyboard Shortcuts** — Cancellable library scans and configurable keyboard shortcuts
- [ ] **Phases 10+: Multi-Library Support** — TBD (awaiting roadmap creation from gsd-roadmapper)

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

### Multi-Library Phases (10+)

**Awaiting roadmap creation.** The gsd-roadmapper will create phased breakdown covering 20 requirements:
- LIB-01..06 (Library Management)
- LSCAN-01..05 (Library Scanning)
- VIEW-01..04 (Unified Presentation)
- PLAY-01..04 (Playlists & Queue)
- DATA-01..04 (Data Integrity)

**Architecture decisions:**
- Hybrid model: `library_id` FK on `audio_files` only; artists/albums/genres stay global
- Libraries stored in SQLite, not TOML config
- Sequential scanning (one library at a time)
- Phantom tracks for playlist preservation on library removal
- Backend filtering for library views

See `.planning/research/SUMMARY.md` for full research.

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
| 10+ Multi-Library phases | v1.1 | 0/? | Awaiting roadmap | - |

---
*Roadmap created: 2026-02-27*
*Last updated: 2026-03-08 — v1.1 restructured for multi-library support, old phases 10-14 deferred*

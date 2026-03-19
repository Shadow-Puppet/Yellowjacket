# Roadmap: YellowJacket

**Created:** 2026-02-27
**Last updated:** 2026-03-18

## Milestones

- ✅ **v1.0 Consolidation** — Phases 1-8 (shipped 2026-03-05) — [archive](milestones/v1.0-ROADMAP.md)
- ✅ **v1.1 Multi-Library Support** — Phases 9-14 (shipped 2026-03-16) — [archive](milestones/v1.1-ROADMAP.md)
- ✅ **v1.2 Tag Editing** — Phases 15-18 (shipped 2026-03-18) — [archive](milestones/v1.2-ROADMAP.md)
- 🔄 **v1.2.1 Format Parity** — Phases 19-21 (in progress)

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

<details>
<summary>✅ v1.2 Tag Editing (Phases 15-18) — SHIPPED 2026-03-18</summary>

- [x] Phase 15: Schema Migration & Write Safety (2/2 plans) — completed 2026-03-16
- [x] Phase 16: Tag Writing & Database Sync (3/3 plans) — completed 2026-03-17
- [x] Phase 17: Single Track Edit (2/2 plans) — completed 2026-03-18
- [x] Phase 18: Batch Edit (2/2 plans) — completed 2026-03-18

**Deferred:** Phase 19 (OGG Vorbis Tag Writing) — stretch goal, deferred to v1.2.1

</details>

### v1.2.1 Format Parity (Phases 19-21)

- [x] **Phase 19: WAV Tag Writer** — Full metadata and cover art writing for WAV files via ID3v2-in-RIFF (completed 2026-03-19)
- [ ] **Phase 20: OGG Vorbis Tag Writer** — Full metadata and cover art writing for OGG Vorbis files via custom page rewriter
- [ ] **Phase 21: Cleanup** — Fix lint warnings and small issues carried forward from v1.2

## Phase Details

### Phase 19: WAV Tag Writer
**Goal**: Users can edit metadata and cover art on WAV files with the same experience as MP3/FLAC
**Depends on**: Nothing (extends existing tag writing pipeline)
**Requirements**: WAV-01, WAV-02, WAV-03, WAV-04, WAV-05, WAV-06
**Success Criteria** (what must be TRUE):
  1. User can open a WAV file in the single-track editor, change any of the 8 text fields, save, and see the changes persist after re-scanning the library
  2. User can embed, replace, or remove cover art on a WAV file and see the updated artwork in the track list and player
  3. Editing a WAV file's tags does not alter audio playback — the file sounds identical before and after
  4. Existing metadata in the WAV file that wasn't edited (RIFF INFO chunks, bext, cue markers) survives the tag write unchanged
  5. If the app crashes or loses power during a WAV tag write, the original file is intact (not corrupted or truncated)
**Plans:** 2/2 plans complete

Plans:
- [ ] 19-01-PLAN.md — WAV RIFF parser/writer and writeWavTags function
- [ ] 19-02-PLAN.md — WAV tag writer round-trip tests

### Phase 20: OGG Vorbis Tag Writer
**Goal**: Users can edit metadata and cover art on OGG Vorbis files with the same experience as MP3/FLAC/WAV
**Depends on**: Phase 19 (pipeline extension pattern proven)
**Requirements**: OGG-01, OGG-02, OGG-03, OGG-04, OGG-05, OGG-06
**Success Criteria** (what must be TRUE):
  1. User can open an OGG Vorbis file in the single-track editor, change any of the 8 text fields, save, and see the changes persist after re-scanning the library
  2. User can embed, replace, or remove cover art on an OGG Vorbis file via METADATA_BLOCK_PICTURE and see the updated artwork in the track list and player
  3. Editing an OGG file's tags does not alter audio playback — the file sounds identical before and after
  4. Existing Vorbis Comments that weren't edited (ReplayGain, lyrics, custom fields) survive the tag write unchanged
  5. If the app crashes or loses power during an OGG tag write, the original file is intact (not corrupted or truncated)
**Plans:** 2 plans

Plans:
- [ ] 20-01-PLAN.md — OGG page parser/writer, Vorbis Comment serializer, writeOggTags, pipeline integration
- [ ] 20-02-PLAN.md — OGG tag writer round-trip tests

### Phase 21: Cleanup
**Goal**: Codebase is clean — no lint warnings or loose ends from tag editing work
**Depends on**: Phase 20 (cleanup after all format work is done)
**Requirements**: CLEAN-01, CLEAN-02
**Success Criteria** (what must be TRUE):
  1. `make lint` passes with zero warnings in dbsync.go and tagwriter.go (nlreturn/wsl violations resolved)
  2. Any small issues discovered during v1.2 tag editing milestone are resolved
**Plans**: TBD

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
| 15. Schema Migration & Write Safety | v1.2 | 2/2 | Complete | 2026-03-16 |
| 16. Tag Writing & Database Sync | v1.2 | 3/3 | Complete | 2026-03-17 |
| 17. Single Track Edit | v1.2 | 2/2 | Complete | 2026-03-18 |
| 18. Batch Edit | v1.2 | 2/2 | Complete | 2026-03-18 |
| 19. WAV Tag Writer | 2/2 | Complete    | 2026-03-19 | - |
| 20. OGG Vorbis Tag Writer | v1.2.1 | 0/2 | Planned | - |
| 21. Cleanup | v1.2.1 | 0/? | Not started | - |

---
*Roadmap created: 2026-02-27*
*Last updated: 2026-03-18 — v1.2.1 Format Parity roadmap created*

---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
status: in-progress
last_updated: "2026-03-05T01:54:49Z"
progress:
  total_phases: 8
  completed_phases: 6
  total_plans: 13
  completed_plans: 12
---

# YellowJacket — Consolidation Milestone State

## Project Reference

**Core value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.
**Current focus:** Phase 7 in progress — deferred library loading complete (Plan 02). Plan 01 pending.
**Milestone:** Consolidation (correctness, performance, code quality, UX polish, test coverage)

## Current Position

**Phase:** 07-backend-performance (in progress)
**Plan:** 1/2 (Plan 02 complete)
**Status:** In progress

```
Phase Progress: [######..] 6/8 phases — Phase 7: 1/2 plans complete
```

## Performance Metrics

| Metric | Value |
|--------|-------|
| Phases complete | 6/8 |
| Plans complete | 1/2 (Phase 7) |
| Requirements delivered | 18/26 |
| Tests added | 84 |
| Bugs fixed | 9 |
| 01-01 duration | 11 min |
| 02-01 duration | 12 min |
| 02-02 duration | 50 min |
| 03-01 duration | 3 min |
| 04-01 duration | 3 min |
| 04-02 duration | 4 min |
| 05-01 duration | 9 min |
| 05-02 duration | 4 min |
| Phase 05 P01 | 9 min | 2 tasks | 1 files |
| Phase 05 P02 | 4 min | 2 tasks | 1 files |
| Phase 06 P01 | 2 min | 2 tasks | 4 files |
| Phase 06 P02 | 2 min | 2 tasks | 3 files |
| Phase 06 P03 | 6 min | 2 tasks | 7 files |
| Phase 07 P02 | 1 min | 1 tasks | 1 files |

## Accumulated Context

### Key Decisions

| Decision | Rationale | Phase |
|----------|-----------|-------|
| Fix races before tests | Can't run `-race`-clean tests with active data races | Phase 1 → 3 |
| PRAGMAs with test infra | NewTestDB must mirror production DB setup; PRAGMAs change production NewDB | Phase 3 |
| Tests before refactoring | Research unanimously recommends characterization tests as safety net | Phase 4-5 → 6-7 |
| SQL consolidation after DB tests | FTS5 search tests verify VIEW doesn't change ranking | Phase 5 → 6 |
| Frontend last | Backend API should be stable before frontend adapts | Phase 8 |
| Release mutex before Wails runtime calls | Library/Playlist SetContext releases lock before registerEventHandlers/migrateExistingPlaylists to avoid blocking | Phase 1 |
| Player SetContext single-lock | Collapsed double-lock to prevent partially-initialized observable state | Phase 1 |
| MPRIS closures inline, Warn level | Non-fatal OS media control failures logged at Warn, kept as inline closures | Phase 2 |
| Pass metrics through cachedLinkArtist | Consistent void-return pattern; warnings collected via addWarning | Phase 2 |
| Fatal vs warning error classification | tx.Commit failures are fatal; all other scan errors are warnings in ScanMetrics | Phase 2 |
| applyPRAGMAs unexported, shared | Package-internal function ensures NewDB and NewTestDB have identical PRAGMA config | Phase 3 |
| NewTestDB uses t.Fatalf not error return | Test DB setup failures are always fatal — no partial test execution | Phase 3 |
| Internal queue tests (package queue) | Access unexported fields (shuffleOrder, mu) for thorough state verification | Phase 4 |
| Persistence roundtrip verifies shuffleOrder JSON | Safety net for Phase 7 incremental persistence refactoring | Phase 4 |
| Volume roundtrip ±1 tolerance | ToUserVolume uses int truncation not rounding, causing up to 1 unit drift | Phase 4 |
| Direct Library construction in tests | Bypasses Config.Validate os.Stat; entity cache functions only need ctx + db | Phase 5 |
| Contentless FTS5 DELETE limitation | DeleteSearchIndex errors on content='' tables; production logs warning, stale entries are harmless | Phase 5 |
| SQLite VIEW for JOIN dedup | track_metadata VIEW consolidates 5-table JOIN; migration2 keeps inline JOIN for upgrade path | Phase 6 |
| AST-based event codegen | Iterate f.Decls directly for deterministic declaration-order output; atomic writes via temp+rename | Phase 6 |
| sqlc.slice() for batch lookups | LookupTrackMetaByPaths uses track_metadata VIEW; chunking preserved at 900 since sqlc.slice() doesn't auto-chunk | Phase 6 |
| SAFETY comment convention | Two-part format (why + safety assurance); cross-references from library.go/rescan.go to search.go | Phase 6 |
| DOMContentLoaded over load event | Fires earlier (after HTML parsed) without waiting for all resources; still defers past module evaluation | Phase 7 |

### TODOs

- [x] Plan Phase 1 (complete)
- [x] Execute Phase 1 Plan 01 (complete)
- [x] Plan Phase 2 (complete)
- [x] Execute Phase 2 Plan 01 (complete)
- [x] Execute Phase 2 Plan 02 (complete)
- [x] Plan Phase 3 (complete)
- [x] Execute Phase 3 Plan 01 (complete)
- [x] Validate sqlc + SQLite VIEW + FTS5 compatibility during Phase 6 planning (validated — sqlc generates TrackMetadatum model, all tests pass)
- [x] Design queue test architecture during Phase 4 planning (complete)
- [x] Determine library scan test fixture strategy during Phase 5 planning (complete — inline construction, setupTestLibrary helper)
- [ ] Measure startup time with large library before Phase 7 lazy loading work

### Blockers

None currently.

### Warnings

- Player lock ordering (`p.mu` before `speaker.Lock()`, goroutine dispatch in beep callback) — do NOT refactor lock-sensitive paths; extract pure logic only
- modernc.org/libc version must match exactly when updating modernc.org/sqlite
- `@lit-labs/signals` is experimental (v0.2.0) — not blocking but noted

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 001 | Multi-playlist import support | 2026-02-28 | 50c8a33 | [001-multi-playlist-import-support](./quick/001-multi-playlist-import-support/) |
| 002 | Auto-rename duplicate playlists on import | 2026-02-28 | 8ba8bbe | [002-auto-rename-duplicate-playlists-on-import](./quick/002-auto-rename-duplicate-playlists-on-import/) |
| 003 | Add multi-select to playlist view with context menu delete support | 2026-02-28 | c92ced2 | [3-add-multi-select-to-playlist-view-with-c](./quick/3-add-multi-select-to-playlist-view-with-c/) |
| 004 | Add "set as default playlist" context menu option for single playlist selection | 2026-02-28 | 9971b63 | [4-add-set-as-default-playlist-context-menu](./quick/4-add-set-as-default-playlist-context-menu/) |
| 005 | Add sort dropdown to playlist view | 2026-03-01 | 5c07485 | [5-add-sort-dropdown-to-playlist-view](./quick/5-add-sort-dropdown-to-playlist-view/) |
| 006 | Remove list icon from playlist names, add favorites icon to default | 2026-03-01 | 3c19766 | [6-remove-list-icon-from-playlist-names-and](./quick/6-remove-list-icon-from-playlist-names-and/) |
| 007 | Pin default playlist to top of playlist view | 2026-03-01 | e6378e1 | [7-pin-default-playlist-to-top-of-playlist-](./quick/7-pin-default-playlist-to-top-of-playlist-/) |
| 008 | Add duplicate tracks dialog to playlist | 2026-03-01 | 917a79a | [8-add-duplicate-tracks-dialog-to-playlist](./quick/8-add-duplicate-tracks-dialog-to-playlist/) |

## Session Continuity

### Last Session

**Date:** 2026-03-05
**What happened:** Executed Phase 7 Plan 02 — deferred LibraryStore eagerFetch from constructor to DOMContentLoaded event
**Where we stopped:** Completed 07-02-PLAN.md (1 task, all verification passed). Phase 7: 1/2 plans complete.
**Next action:** Execute 07-01-PLAN.md (lazy module loading) or continue to next phase

### Context for Next Session

- Phase 7 Plan 02 complete: deferred library loading
- LibraryStore no longer fires backend roundtrips during module evaluation
- App shell renders before data fetches begin
- Plan 01 (lazy module loading) still pending

---
*State initialized: 2026-02-27*
Last activity: 2026-03-05 - Completed 07-02: deferred LibraryStore eagerFetch to DOMContentLoaded
*Last updated: 2026-03-05*

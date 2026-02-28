# YellowJacket — Consolidation Milestone State

## Project Reference

**Core value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.
**Current focus:** Phase 1 complete, ready for Phase 2 planning.
**Milestone:** Consolidation (correctness, performance, code quality, UX polish, test coverage)

## Current Position

**Phase:** 01-concurrency-race-fixes (complete)
**Plan:** 1/1 (complete)
**Status:** Phase 1 complete, ready for Phase 2 planning

```
Phase Progress: [#.......] 1/8 phases complete
```

## Performance Metrics

| Metric | Value |
|--------|-------|
| Phases complete | 1/8 |
| Plans complete | 1/1 (Phase 1) |
| Requirements delivered | 4/26 |
| Tests added | 0 |
| Bugs fixed | 4 |
| 01-01 duration | 11 min |

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

### TODOs

- [x] Plan Phase 1 (complete)
- [x] Execute Phase 1 Plan 01 (complete)
- [ ] Validate sqlc + SQLite VIEW + FTS5 compatibility during Phase 6 planning (research flag)
- [ ] Design queue test architecture during Phase 4 planning (research flag)
- [ ] Determine library scan test fixture strategy during Phase 5 planning (research flag)
- [ ] Measure startup time with large library before Phase 7 lazy loading work

### Blockers

None currently.

### Warnings

- Player lock ordering (`p.mu` before `speaker.Lock()`, goroutine dispatch in beep callback) — do NOT refactor lock-sensitive paths; extract pure logic only
- modernc.org/libc version must match exactly when updating modernc.org/sqlite
- `@lit-labs/signals` is experimental (v0.2.0) — not blocking but noted

## Session Continuity

### Last Session

**Date:** 2026-02-28
**What happened:** Executed Phase 1 Plan 01 — added mutex protection to all SetContext methods across Queue, Library, Playlist, and Player
**Where we stopped:** Completed 01-01-PLAN.md (all tasks, verification passed)
**Next action:** `/gsd-plan-phase 2` to create execution plan for Backend Correctness

### Context for Next Session

- Phase 1 complete: all SetContext data races eliminated (CORR-01 through CORR-04)
- All four packages pass `go test -race`, `go vet`, `golangci-lint` with 0 issues
- Library and Playlist gained struct-level mutexes; Queue and Player already had them
- Ready for Phase 2 (Backend Correctness) — error handling, config permissions, MPRIS errors

---
*State initialized: 2026-02-27*
*Last updated: 2026-02-28*

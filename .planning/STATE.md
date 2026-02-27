# YellowJacket — Consolidation Milestone State

## Project Reference

**Core value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.
**Current focus:** Roadmap created, awaiting Phase 1 planning.
**Milestone:** Consolidation (correctness, performance, code quality, UX polish, test coverage)

## Current Position

**Phase:** — (not started)
**Plan:** — (not started)
**Status:** Roadmap complete, ready for phase planning

```
Phase Progress: [........] 0/8 phases complete
```

## Performance Metrics

| Metric | Value |
|--------|-------|
| Phases complete | 0/8 |
| Plans complete | 0/? |
| Requirements delivered | 0/26 |
| Tests added | 0 |
| Bugs fixed | 0 |

## Accumulated Context

### Key Decisions

| Decision | Rationale | Phase |
|----------|-----------|-------|
| Fix races before tests | Can't run `-race`-clean tests with active data races | Phase 1 → 3 |
| PRAGMAs with test infra | NewTestDB must mirror production DB setup; PRAGMAs change production NewDB | Phase 3 |
| Tests before refactoring | Research unanimously recommends characterization tests as safety net | Phase 4-5 → 6-7 |
| SQL consolidation after DB tests | FTS5 search tests verify VIEW doesn't change ranking | Phase 5 → 6 |
| Frontend last | Backend API should be stable before frontend adapts | Phase 8 |

### TODOs

- [ ] Plan Phase 1 (next step)
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

**Date:** 2026-02-27
**What happened:** Project initialized — codebase analysis, research, requirements definition, roadmap creation
**Where we stopped:** Roadmap created with 8 phases covering 26 requirements
**Next action:** `/gsd-plan-phase 1` to create execution plan for Concurrency Race Fixes

### Context for Next Session

- All 26 v1 requirements mapped across 8 phases
- Dependency chain: correctness → test infra → tests → SQL/perf optimization → frontend
- Phase 1 is 4 requirements (CORR-01 to CORR-04), all mechanical mutex additions
- Research says Phase 1 fixes are "textbook race, LOW effort" — standard patterns, skip research-phase

---
*State initialized: 2026-02-27*
*Last updated: 2026-02-27*

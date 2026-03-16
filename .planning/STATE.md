---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: Tag Editing
status: executing
last_updated: "2026-03-16T22:13:45.000Z"
progress:
  total_phases: 5
  completed_phases: 1
  total_plans: 2
  completed_plans: 2
---

# YellowJacket — Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-03-16)

**Core value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.
**Current focus:** v1.2 Tag Editing

## Current Position

Phase: Phase 15 — Schema Migration & Write Safety (complete)
Plan: 2 of 2 complete
Status: Phase 15 complete — ready for Phase 16 planning
Last activity: 2026-03-16 — Completed 15-02 (AtomicWrite utility)

### Phase Overview

| Phase | Status |
|-------|--------|
| 15. Schema Migration & Write Safety | In progress (1/2 plans) |
| 16. Tag Writing & Database Sync | Not started |
| 17. Single Track Edit | Not started |
| 18. Batch Edit | Not started |
| 19. OGG Vorbis Tag Writing | Not started |

### v1.2 Requirement Coverage

| Category | Requirements | Phase(s) |
|----------|-------------|----------|
| Schema & Safety | SCHEMA-01, SCHEMA-02 | Phase 15 |
| Tag Writing | WRITE-01, WRITE-02 | Phase 16 |
| Tag Writing | WRITE-03 | Phase 19 |
| Tag Writing | WRITE-04, WRITE-05, WRITE-06 | Phase 15, 16 |
| Database Sync | SYNC-01, SYNC-02, SYNC-03, SYNC-04 | Phase 16 |
| Single Track Edit | EDIT-01, EDIT-02, EDIT-03, EDIT-04 | Phase 17 |
| Batch Edit | BATCH-01, BATCH-02, BATCH-03, BATCH-04 | Phase 18 |

## Performance Metrics

**v1.0 baseline:** 8 phases, 17 plans, 34 tasks in 6 days (107 commits)
**v1.1 baseline:** 6 phases, 18 plans in 10 days (~85 commits)
**v1.2 scope:** 5 phases, 20 requirements

| Phase | Plan | Duration | Tasks | Files |
|-------|------|----------|-------|-------|
| 15 | 01 | 15min | 2 | 5 |

## Accumulated Context

### Key Decisions

Decisions from v1.0 and v1.1 are archived in PROJECT.md Key Decisions table. Key patterns to carry forward:

- Mutex-protected setter pattern (lock → write → release → callbacks)
- SAFETY comment convention for hand-crafted SQL
- AST-based codegen for cross-language constant sync
- Design tokens via `:host` scoped CSS custom properties
- queueMicrotask coalescing for store notifications
- `.renderItem` + `.keyFunction` (not `repeat()` children) for lit-virtualizer
- Upsert-and-relink for shared entities (never mutate shared artist/album/genre rows)
- ScanHooks/RemovalHooks/RescanHooks callback patterns for cross-package coordination

### v1.2 Execution Decisions

| Decision | Rationale |
|----------|-----------|
| Inlined migration 8 SQL rather than calling DB struct methods | `runMigrations` receives raw `*sql.DB`, not `*DB` — cannot call receiver methods |

### v1.2 Roadmap Decisions

| Decision | Rationale |
|----------|-----------|
| 5 phases (15-19) for 20 requirements | Natural clustering: foundation → writers → single edit → batch edit → stretch OGG |
| WRITE-05 in Phase 15 (not 16) | Atomic write utility is foundational infrastructure, not format-specific |
| Cover art embed (WRITE-04) in Phase 16 | Cover art embedding is format-specific writer work, shares test infrastructure with tag writing |
| Cover art UI (EDIT-03) in Phase 17 | Cover art selection UX is part of the single-track edit dialog |
| OGG as separate Phase 19 (stretch) | Custom OGG page rewriter is MEDIUM-HIGH risk; MP3+FLAC covers vast majority of libraries |
| SYNC-04 (scan pause during edits) in Phase 16 | Scan/edit mutual exclusion is part of the write pipeline, not the UI layer |
| Phase 18 depends on Phase 17 | Batch editing is N × single with UI complexity on top; pipeline must be solid first |
| Phase 19 depends on Phase 16 (not 17) | OGG writing is a backend writer addition; UI integration is format-transparent |

### Warnings (carry forward)

- Player lock ordering (`p.mu` before `speaker.Lock()`, goroutine dispatch in beep callback)
- modernc.org/libc version must match exactly when updating modernc.org/sqlite
- `@lit-labs/signals` is experimental (v0.2.0) — not blocking but noted
- ~~FTS5 contentless can't DELETE rows~~ — **RESOLVED: SCHEMA-01 completed** — contentless_delete=1 migration applied
- Orphan cleanup must not delete shared entities across libraries (reference-counting bottom-up)
- FLAC files require full rewrite for tag changes — atomic write-to-temp-then-rename mandatory
- Currently-playing file must be stopped before writing (WRITE-06) — Windows file locking is especially strict
- Shared entity fan-out — editing one track's artist must NOT mutate the shared artist_credit row

### Research Flags

- **Phase 16:** go-flac libraries (44 stars) — verify round-trip with edge-case FLAC files early
- **Phase 19:** Custom OGG page rewriter — prototype before committing; consider dropping if too complex
- **Phase 16:** Album artist storage — not currently a separate entity; resolve during planning

### Deferred Improvements

- **Bulk phantom matching performance** — O(n×3) round trips per phantom. Revisit if large external playlist imports occur.

## Session Continuity

### Last Session

**Date:** 2026-03-16
**What happened:** Executed Phase 15 Plan 01 — migrated FTS5 search_index to contentless_delete=1, implemented real DeleteSearchIndex, added migration 8, added 3 new tests.
**Where we stopped:** Completed 15-01-PLAN.md
**Next action:** Execute 15-02-PLAN.md (atomic write utility)

---
*State initialized: 2026-02-27*
### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 16 | add ctrl+a hotkey to multi-select views to select all items | 2026-03-07 | 043c74c | [16-add-ctrl-a-hotkey-to-multi-select-views-](./quick/16-add-ctrl-a-hotkey-to-multi-select-views-/) |
| 17 | refactor playlist view to use subpages | 2026-03-08 | 955cd68 | [17-refactor-playlist-view-to-use-subpages-l](./quick/17-refactor-playlist-view-to-use-subpages-l/) |
| 18 | add multi-column metadata display to playlist-details | 2026-03-08 | ce23177 | [18-add-multi-column-metadata-display-to-pla](./quick/18-add-multi-column-metadata-display-to-pla/) |
| 19 | fix phantom playlist tracks with multi-root path resolution | 2026-03-16 | 9144ded | [19-fix-phantom-playlist-tracks](./quick/19-fix-phantom-playlist-tracks/) |

Last activity: 2026-03-16 - Completed quick task 19: fix phantom playlist tracks with multi-root path resolution
*Last updated: 2026-03-16 — Completed 15-01 (FTS5 migration + delete support)*

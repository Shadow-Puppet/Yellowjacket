---
gsd_state_version: 1.0
milestone: v1.1
milestone_name: Multi-Library Support
status: planning
last_updated: "2026-03-08"
progress:
  total_phases: 5
  completed_phases: 1
  total_plans: 5
  completed_plans: 5
---

# YellowJacket — Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-03-08)

**Core value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.
**Current focus:** v1.1 Multi-Library Support — Phase 10 (Schema & Migration)

## Current Position

Phase: 10 — Schema & Migration (next up)
Plan: TBD (awaiting plan creation)
Status: Roadmap created — ready for phase planning
Progress: ████░░░░░░░░░░░░░░░░ 1/5 phases complete (Phase 9)
Last activity: 2026-03-08 — Multi-library roadmap created (Phases 10-13)

### Phase Overview

| Phase | Status |
|-------|--------|
| 9. Scan Cancellation & Keyboard Shortcuts | Complete (5/5 plans) ✅ |
| 10. Schema & Migration | Not started |
| 11. Per-Library Scan Pipeline | Not started |
| 12. Library CRUD & Data Integrity | Not started |
| 13. Library Views & Phantom Tracks | Not started |

## Performance Metrics

**v1.0 baseline:** 8 phases, 17 plans, 34 tasks in 6 days (107 commits)
**v1.1 scope:** Phase 9 complete (5 plans), Phases 10-13 pending (20 requirements across 4 phases)

| Phase | Plan | Duration | Tasks | Files |
|-------|------|----------|-------|-------|
| 09-01 | scan control backend | 16 min | 2 | 5 |
| 09-02 | keyboard shortcuts config & service | 35 min | 2 | 12 |
| 09-04 | keyboard shortcuts settings UI | 5 min | 2 | 2 |
| 09-03 | scan control UI | 2 min | 1 | 3 |
| 09-05 | integration testing & verification | 3 min | 2 | 1 |

## Accumulated Context

### Key Decisions

Decisions from v1.0 are archived in PROJECT.md Key Decisions table. Key patterns to carry forward:

- Mutex-protected setter pattern (lock → write → release → callbacks)
- SAFETY comment convention for hand-crafted SQL
- AST-based codegen for cross-language constant sync
- Design tokens via `:host` scoped CSS custom properties
- queueMicrotask coalescing for store notifications
- `.renderItem` + `.keyFunction` (not `repeat()` children) for lit-virtualizer

### v1.1 Roadmap Decisions

| Decision | Rationale |
|----------|-----------|
| Phase 9 = Scan Cancel + Shortcuts | Quick wins, validate context cancellation and config extension patterns |
| v1.1 restructured for multi-library | Tag editing, smart playlists, gapless, MusicBrainz, layout, plugins deferred to future milestones |
| Hybrid model (library_id on audio_files only) | Physical files belong to libraries; logical entities (artists, albums, genres) are global/shared |
| Libraries in DB, not TOML | CRUD through UI shouldn't require TOML manipulation; DB is source of truth |
| SET NULL for playlist_tracks FK | Phantom tracks preserve playlist structure when library removed |
| CASCADE for queue_tracks FK | Queue is ephemeral, not user-curated like playlists |
| Sequential scanning | SQLite single-writer makes parallel scans pointless |
| Backend filtering, not frontend | Don't load 150K tracks when viewing one library |
| 4 multi-library phases (10-13) | Natural delivery boundaries: schema → scan → CRUD → views, each phase delivers verifiable capability |

### Warnings (carry forward)

- Player lock ordering (`p.mu` before `speaker.Lock()`, goroutine dispatch in beep callback) — carry forward
- modernc.org/libc version must match exactly when updating modernc.org/sqlite
- `@lit-labs/signals` is experimental (v0.2.0) — not blocking but noted
- Scan cancellation: skip orphan cleanup on cancelled scans — Phase 9 ✅ (implemented in 09-01)
- Volume mutations (ChangeVolume/MuteToggle) must emit events + persist state — Phase 9 ✅ (fixed in 09-05)
- ALTER TABLE ADD COLUMN requires DEFAULT for NOT NULL — create libraries table first
- Table rebuild must audit ALL CASCADE FKs (playlist_tracks AND queue_tracks)
- FTS5 contentless can't DELETE rows — stale entries accumulate after library removal; consider contentless_delete migration
- Orphan cleanup must not delete shared entities across libraries (reference-counting bottom-up)
- Existing user migration must be seamless (TOML DirectoryPath to DB libraries table)

### Research Flags

- **Multi-library research complete** — see `.planning/research/` (STACK.md, FEATURES.md, ARCHITECTURE.md, PITFALLS.md, SUMMARY.md)

## Session Continuity

### Last Session

**Date:** 2026-03-08
**What happened:** Created multi-library roadmap (Phases 10-13) from 20 requirements. Phase 10 = Schema & Migration (DATA-01, DATA-04, LIB-04, LIB-05, LSCAN-05). Phase 11 = Per-Library Scan Pipeline (LSCAN-01..04). Phase 12 = Library CRUD & Data Integrity (LIB-01..03, LIB-06, DATA-02, DATA-03, PLAY-04). Phase 13 = Library Views & Phantom Tracks (VIEW-01..04, PLAY-01..03).
**Where we stopped:** Roadmap written, ready for phase planning
**Next action:** Run `gsd-plan-phase 10` to create plans for Schema & Migration

---
*State initialized: 2026-02-27*
### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 16 | add ctrl+a hotkey to multi-select views to select all items | 2026-03-07 | 043c74c | [16-add-ctrl-a-hotkey-to-multi-select-views-](./quick/16-add-ctrl-a-hotkey-to-multi-select-views-/) |
| 17 | refactor playlist view to use subpages | 2026-03-08 | 955cd68 | [17-refactor-playlist-view-to-use-subpages-l](./quick/17-refactor-playlist-view-to-use-subpages-l/) |
| 18 | add multi-column metadata display to playlist-details | 2026-03-08 | ce23177 | [18-add-multi-column-metadata-display-to-pla](./quick/18-add-multi-column-metadata-display-to-pla/) |

Last activity: 2026-03-08 - Completed quick task 18: add multi-column metadata display to playlist-details
*Last updated: 2026-03-08 — Multi-library roadmap created (Phases 10-13)*

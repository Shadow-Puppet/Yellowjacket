---
gsd_state_version: 1.0
milestone: v1.1
milestone_name: Multi-Library Support
status: unknown
last_updated: "2026-03-09T20:23:44.728Z"
progress:
  total_phases: 3
  completed_phases: 3
  total_plans: 10
  completed_plans: 10
---

# YellowJacket — Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-03-08)

**Core value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.
**Current focus:** v1.1 Multi-Library Support — Phase 11 (Per-Library Scan Pipeline)

## Current Position

Phase: 12 — Library CRUD & Data Integrity
Plan: 1 of 2 in Phase (Plan 01 complete)
Status: Plan 12-01 complete — backend CRUD API implemented
Progress: ████████████████░░░░ 3/5 phases complete (Phase 12 in progress)
Last activity: 2026-03-12 — Completed 12-01 library CRUD backend API

### Phase Overview

| Phase | Status |
|-------|--------|
| 9. Scan Cancellation & Keyboard Shortcuts | Complete (5/5 plans) ✅ |
| 10. Schema & Migration | Complete (2/2 plans) ✅ |
| 11. Per-Library Scan Pipeline | Complete (3/3 plans) ✅ |
| 12. Library CRUD & Data Integrity | In progress (1/2 plans) |
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
| Phase 10-01 P01 | 11 min | 2 tasks | 10 files |
| Phase 10-02 P02 | 5 min | 2 tasks | 9 files |
| Phase 11-01 P01 | 7 min | 2 tasks | 11 files |
| Phase 11-03 P03 | 10 min | 1 task | 3 files |
| Phase 11-02 P02 | 4 min | 2 tasks | 2 files |
| Phase 12-01 P01 | 6 min | 2 tasks | 6 files |

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

### Phase 10 Decisions

| Decision | Rationale |
|----------|-----------|
| Underscore prefix `_libraries.sql` for schema ordering | Go embed.FS ReadDir sorts alphabetically; `_` < `a` ensures libraries table created before audio_files FK |
| Sentinel library id=0 in test DB | Existing tests use DEFAULT library_id=0; sentinel row satisfies FK without modifying every test |
| TOML cleanup via generic map[string]any | Preserves all config sections when removing only DirectoryPath; no dependency on full Config struct |
| sql.NullInt64 for nullable playlist_tracks.audio_file_id | Phantom tracks have NULL audio_file_id; generated sqlc code requires sql.Null types |
| COALESCE fallback chain: live → phantom → empty | Playlist queries use 3-level COALESCE so callers always get usable string values |
| is_phantom computed column via CASE WHEN | Eliminates null-checking logic in callers; simple int64 boolean (0/1) |

### Phase 11 Decisions

| Decision | Rationale |
|----------|-----------|
| Library ID threaded via importResult | Explicit data flow through scan pipeline, avoids mutating shared Library struct state |
| scanInternal returns *ScanMetrics only | Called from goroutine in scan queue, error return impractical; errors logged + accumulated in Warnings |
| Auto worker count per library path | Each library may be on different storage (SSD/HDD), auto-detect per scan |
| Backward-compatible Scan() wrapper | Keeps handleConfigUpdate and FullRescan working without changes |
| Queue-aware cancel dialog scope choice | Two-option "Cancel This Library / Cancel All" only when queuedCount > 0; single-scan keeps existing pattern |
| handleScanComplete defers reset when queue draining | Prevents premature scanning=false before next library starts |
| FullRescan uses first library from GetAllLibraries | Per-library rescan deferred to Phase 12; preserves backward compatibility for config-page "Rescan" button |
| LibraryConfigChanged handler removed entirely | Multi-library model uses CRUD API (Phase 12), not event-driven config updates |
| Scan() wrapper deleted | Only callers were handleConfigUpdate (deleted) and FullRescan (updated to scanInternal) |

### Phase 12 Decisions

| Decision | Rationale |
|----------|-----------|
| Application-level name uniqueness check (iterate GetAllLibraries) | Avoids migration 7; rename is infrequent, check is simple |
| RemovalHooks callback struct (StopPlayback + CompactQueue) | Mirrors RescanHooks pattern; breaks circular dependency between library, player, queue packages |
| querySingleInt64 helper for hand-crafted SQL aggregates | DB type has QueryContext (returns *sql.Rows) but no QueryRowContext; helper wraps scan-close cycle |
| Sentinel errors for all validation per err113 | errLibraryNameEmpty, errLibraryNameTooLong, errLibraryNameDuplicate, errLibraryPathNotExist |
| Pre-populate phantom metadata BEFORE cascade delete | Avoids lost join data — playlist_tracks need track metadata after audio_files rows are gone |

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

**Date:** 2026-03-12
**What happened:** Executed Phase 12, Plan 01 — implemented backend Library CRUD API (AddLibrary, RenameLibrary, RemoveLibrary, GetRemovalImpact) with full orphan cleanup pipeline, phantom track preservation, FTS5 rebuild, queue compaction via CompactAfterLibraryRemoval, and RemovalHooks wiring in app.go.
**Where we stopped:** Completed 12-01-PLAN.md — Plan 01 of Phase 12 complete
**Next action:** Execute Phase 12 Plan 02 (Frontend library management UI)

---
*State initialized: 2026-02-27*
### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 16 | add ctrl+a hotkey to multi-select views to select all items | 2026-03-07 | 043c74c | [16-add-ctrl-a-hotkey-to-multi-select-views-](./quick/16-add-ctrl-a-hotkey-to-multi-select-views-/) |
| 17 | refactor playlist view to use subpages | 2026-03-08 | 955cd68 | [17-refactor-playlist-view-to-use-subpages-l](./quick/17-refactor-playlist-view-to-use-subpages-l/) |
| 18 | add multi-column metadata display to playlist-details | 2026-03-08 | ce23177 | [18-add-multi-column-metadata-display-to-pla](./quick/18-add-multi-column-metadata-display-to-pla/) |

Last activity: 2026-03-08 - Completed quick task 18: add multi-column metadata display to playlist-details
*Last updated: 2026-03-12 — Completed 12-01-PLAN.md (Phase 12 Plan 01 complete)*

---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: Tag Editing
status: defining_requirements
last_updated: "2026-03-16T20:03:38.282Z"
progress:
  total_phases: 6
  completed_phases: 6
  total_plans: 18
  completed_plans: 18
---

# YellowJacket — Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-03-16)

**Core value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.
**Current focus:** v1.2 Tag Editing

## Current Position

Phase: Not started (defining requirements)
Plan: —
Status: Defining requirements for v1.2 Tag Editing
Last activity: 2026-03-16 — Milestone v1.2 started

### Phase Overview

| Phase | Status |
|-------|--------|
| 9. Scan Cancellation & Keyboard Shortcuts | Complete (5/5 plans) ✅ |
| 10. Schema & Migration | Complete (2/2 plans) ✅ |
| 11. Per-Library Scan Pipeline | Complete (3/3 plans) ✅ |
| 12. Library CRUD & Data Integrity | Complete (2/2 plans) ✅ |
| 13. Library Views & Phantom Tracks | Complete (2/2 plans) ✅ |
| 14. Performance Optimization | Complete (4/4 plans) ✅ |

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
| Phase 14-02 P02 | 1 min | 1 task | 1 file |
| Phase 14-03 P03 | 4 min | 2 tasks | 5 files |
| Phase 14-01 P01 | 3 min | 2 tasks | 7 files |
| Phase 14-04 P04 | 2 min | 2 tasks | 3 files |
| Phase 12-02 P02 | 38 min | 3 tasks | 19 files |
| Phase 13 P01 | 5 min | 2 tasks | 10 files |
| Phase 13 P02 | 9 min | 1 task | 12 files |

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
| Selectable library checkboxes for scan targeting | User selects which libraries to scan instead of scan-all-or-nothing; Set<number> model with select-all/indeterminate |
| Inline progress bar per library row | Each library row shows scan phase and percentage, replacing global-only indicator |
| Collapsible config sections with chevron dropdown | Keeps settings page organized as it grows; new config-section component |
| Library store invalidation on LibraryRemoved event | Ensures all data views refresh after library removal |

### Phase 14 Decisions

| Decision | Rationale |
|----------|-----------|
| Primary views cached, detail views ephemeral | Detail views depend on entity IDs that change; caching would show stale content |
| Inline style.display toggle over CSS class | Simpler, no specificity issues, empty string restores natural display value |
| viewCache bounded at 6 entries | One per primary view — negligible memory since data is already in store caches |
| contain: strict only on .main-panel | Has explicit dimensions (flex: 1, overflow: hidden); elsewhere use layout style to not break flex |
| will-change: transform only on scroll containers | Not on :host — avoids wasting GPU memory on non-scrolling elements |
| content-visibility: auto on album cards with contain-intrinsic-size | Prevents layout shift during scroll while skipping off-screen rendering |
| Event delegation via data-index + closest() for virtualizer items | Zero per-item closures in renderItem; all events delegated on virtualizer element |
| changeGeneration counter in library store | Simpler than typed subscriptions; skips requestUpdate on loading-only transitions |
| RAF throttle over debounce for scroll saves | Saves position once per frame during scrolling, not just after stop; prevents lost positions on quick navigation |
| Keep monkey-patch alongside overflow-anchor CSS | CSS overflow-anchor disables browser anchoring but not lit-virtualizer's internal _correctScrollError |

### Phase 13 Decisions

| Decision | Rationale |
|----------|-----------|
| IN-subquery pattern for album/artist/genre library filtering | Entities are global, tracks belong to libraries; subquery filters entity IDs by library membership |
| Empty slice return (not error) for library with no tracks | Empty library is valid state, not error condition; callers handle empty UI |
| Client-side search with filtered data source | rankTracks filters already-loaded tracks; no backend SearchTracksByLibrary call needed |
| Native select for library filter dropdown | Compact, accessible, matches top bar height; no custom component overhead |
| getAlbumsByArtistNameCached returns null when filter active | Avoids stale cross-library data; forces backend query for consistency |
| ScanHooks callback for phantom resolution | Mirrors RemovalHooks/RescanHooks pattern; avoids circular dependency between library and playlist packages |
| phantom_file_path column on playlist_tracks | Stored at removal time for post-scan matching; enables automatic phantom resolution |
| M3U8-based phantom resolution over SQL-only | Reads playlist files to match by position and file path; handles both pre-existing and new phantoms |

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

### Deferred Improvements

- **Bulk phantom matching performance** — `FindPhantomMatches` runs 3 sequential DB queries per phantom track (basename search, FTS filename, FTS keywords). With hundreds of phantoms this is O(n×3) round trips. Could batch the basename query (WHERE basename IN (...)), pre-load FTS results, or parallelize with goroutines. Not urgent now that the false-phantom bug is fixed (tracks appeared phantom due to empty library root, not actual missing files). Revisit if users import large playlists from external sources with genuinely unresolved paths.

### Research Flags

- **Multi-library research complete** — see `.planning/research/` (STACK.md, FEATURES.md, ARCHITECTURE.md, PITFALLS.md, SUMMARY.md)

## Session Continuity

### Last Session

**Date:** 2026-03-16
**What happened:** Completed Plan 13-02 — library filter dropdown in top bar, all views wired to ByLibrary queries, phantom track auto-resolution via ScanHooks. Checkpoint APPROVED — all 7 requirements verified (VIEW-01–04, PLAY-01–03). Additional bugfixes: virtualizer event delegation race condition, phantom auto-resolution after re-scan.
**Where we stopped:** Completed 13-02-PLAN.md — Phase 13 complete (2/2 plans), v1.1 milestone complete
**Next action:** v1.1 milestone shipped — all phases (9-14) complete

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
*Last updated: 2026-03-16 — Completed quick task 19*

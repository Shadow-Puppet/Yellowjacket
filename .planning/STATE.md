---
gsd_state_version: 1.0
milestone: "v1.2.1"
milestone_name: "Format Parity"
status: roadmap_complete
last_updated: "2026-03-18T20:00:00.000Z"
progress:
  total_phases: 3
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
---

# YellowJacket — Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-03-18)

**Core value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.
**Current focus:** v1.2.1 Format Parity — roadmap complete, ready for phase planning

## Current Position

Phase: 19 — WAV Tag Writer (not yet planned)
Plan: —
Status: Roadmap complete, awaiting `/gsd-plan-phase 19`
Last activity: 2026-03-18 — Roadmap created for v1.2.1

```
v1.2.1 Format Parity
[░░░░░░░░░░░░░░░░░░░░] 0/3 phases
```

## Performance Metrics

**v1.0 baseline:** 8 phases, 17 plans, 34 tasks in 6 days (107 commits)
**v1.1 baseline:** 6 phases, 18 plans in 10 days (~85 commits)
**v1.2 baseline:** 4 phases, 9 plans, 17 tasks in 3 days (~40 commits)

## Accumulated Context

### Key Decisions

All decisions archived in PROJECT.md Key Decisions table and RETROSPECTIVE.md. Key patterns to carry forward:

- Mutex-protected setter pattern (lock → write → release → callbacks)
- SAFETY comment convention for hand-crafted SQL
- AST-based codegen for cross-language constant sync
- Design tokens via `:host` scoped CSS custom properties
- queueMicrotask coalescing for store notifications
- `.renderItem` + `.keyFunction` (not `repeat()` children) for lit-virtualizer
- Upsert-and-relink for shared entities (never mutate shared artist/album/genre rows)
- ScanHooks/RemovalHooks/RescanHooks callback patterns for cross-package coordination
- AtomicWrite with `.yj-tmp` suffix for crash-safe file operations
- suppressEvents flag for batch event coalescing
- Three-state field model via implicit dirty tracking (editValues map presence)

### Warnings (carry forward)

- Player lock ordering (`p.mu` before `speaker.Lock()`, goroutine dispatch in beep callback)
- modernc.org/libc version must match exactly when updating modernc.org/sqlite
- `@lit-labs/signals` is experimental (v0.2.0) — not blocking but noted
- Orphan cleanup must not delete shared entities across libraries (reference-counting bottom-up)
- FLAC files require full rewrite for tag changes — atomic write-to-temp-then-rename mandatory
- Currently-playing file must be stopped before writing (WRITE-06) — Windows file locking is especially strict
- Shared entity fan-out — editing one track's artist must NOT mutate the shared artist_credit row
- OGG CRC32 uses non-standard MSB-first bit ordering — Go's `hash/crc32` produces wrong checksums
- OGG page sequence numbers must be renumbered when comment header page count changes
- WAV RIFF chunks must start at even byte offsets — odd-length chunks need a padding byte

### Deferred Improvements

- **Bulk phantom matching performance** — O(n×3) round trips per phantom. Revisit if large external playlist imports occur.

## Session Continuity

### Last Session

**Date:** 2026-03-18
**What happened:** Created v1.2.1 Format Parity roadmap. 3 phases (19-21): WAV Tag Writer → OGG Vorbis Tag Writer → Cleanup. All 14 requirements mapped.
**Where we stopped:** Roadmap created, ready for phase planning.
**Next action:** `/gsd-plan-phase 19` to plan WAV Tag Writer

---
*State initialized: 2026-02-27*
### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 16 | add ctrl+a hotkey to multi-select views to select all items | 2026-03-07 | 043c74c | [16-add-ctrl-a-hotkey-to-multi-select-views-](./quick/16-add-ctrl-a-hotkey-to-multi-select-views-/) |
| 17 | refactor playlist view to use subpages | 2026-03-08 | 955cd68 | [17-refactor-playlist-view-to-use-subpages-l](./quick/17-refactor-playlist-view-to-use-subpages-l/) |
| 18 | add multi-column metadata display to playlist-details | 2026-03-08 | ce23177 | [18-add-multi-column-metadata-display-to-pla](./quick/18-add-multi-column-metadata-display-to-pla/) |
| 19 | fix phantom playlist tracks with multi-root path resolution | 2026-03-16 | 9144ded | [19-fix-phantom-playlist-tracks](./quick/19-fix-phantom-playlist-tracks/) |

Last activity: 2026-03-18 - v1.2.1 roadmap created
*Last updated: 2026-03-18 — v1.2.1 roadmap created*

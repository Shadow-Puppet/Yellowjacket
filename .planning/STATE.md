---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
status: in-progress
last_updated: "2026-03-19T17:53:47Z"
progress:
  total_phases: 1
  completed_phases: 1
  total_plans: 4
  completed_plans: 3
---

# YellowJacket — Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-03-18)

**Core value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.
**Current focus:** v1.2.1 Format Parity — Phase 20 in progress (OGG Vorbis tag writer)

## Current Position

Phase: 20 — OGG Vorbis Tag Writer (IN PROGRESS)
Plan: 1 of 2 complete
Status: Plan 01 complete (implementation) — Plan 02 pending (tests)
Last activity: 2026-03-19 — Completed 20-01-PLAN.md (OGG Vorbis tag writer implementation)

```
v1.2.1 Format Parity
[███████░░░░░░░░░░░░░] 1/3 phases complete
```

## Performance Metrics

**v1.0 baseline:** 8 phases, 17 plans, 34 tasks in 6 days (107 commits)
**v1.1 baseline:** 6 phases, 18 plans in 10 days (~85 commits)
**v1.2 baseline:** 4 phases, 9 plans, 17 tasks in 3 days (~40 commits)

| Phase | Plan | Duration | Tasks | Files |
|-------|------|----------|-------|-------|
| 19-01 | WAV tag writer impl | 5 min | 2 | 4 |
| 19-02 | WAV tag writer tests | 9 min | 3 | 2 |
| 20-01 | OGG Vorbis tag writer impl | 3 min | 1 | 4 |

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
- Custom RIFF parser for WAV: lenient-read/strict-write, ID3v2 chunk at end of file
- WAV writer reuses MP3's applyTextChanges/applyCoverArtChanges for ID3v2 tag manipulation
- WAV test read-back uses bogem/id3v2.ParseReader (dhowden/tag ReadFrom does not support WAV)
- Custom OGG CRC32 with precomputed lookup table (hash/crc32 uses incompatible reflected bit ordering)
- Raw byte preservation for Vorbis Comment entries — [][]byte instead of []string for non-UTF-8 safety
- METADATA_BLOCK_PICTURE + legacy COVERART/COVERARTMIME stripping on all OGG cover art operations

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

**Date:** 2026-03-19
**What happened:** Executed 20-01-PLAN.md — OGG Vorbis tag writer implementation. Created custom OGG page parser/writer with MSB-first CRC32, Vorbis Comment packet serializer with METADATA_BLOCK_PICTURE cover art, and pipeline integration.
**Where we stopped:** Completed 20-01-PLAN.md
**Next action:** Execute 20-02-PLAN.md (OGG Vorbis tag writer tests)

---
*State initialized: 2026-02-27*
### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 16 | add ctrl+a hotkey to multi-select views to select all items | 2026-03-07 | 043c74c | [16-add-ctrl-a-hotkey-to-multi-select-views-](./quick/16-add-ctrl-a-hotkey-to-multi-select-views-/) |
| 17 | refactor playlist view to use subpages | 2026-03-08 | 955cd68 | [17-refactor-playlist-view-to-use-subpages-l](./quick/17-refactor-playlist-view-to-use-subpages-l/) |
| 18 | add multi-column metadata display to playlist-details | 2026-03-08 | ce23177 | [18-add-multi-column-metadata-display-to-pla](./quick/18-add-multi-column-metadata-display-to-pla/) |
| 19 | fix phantom playlist tracks with multi-root path resolution | 2026-03-16 | 9144ded | [19-fix-phantom-playlist-tracks](./quick/19-fix-phantom-playlist-tracks/) |

Last activity: 2026-03-19 - completed 20-01-PLAN.md
*Last updated: 2026-03-19 — completed 20-01-PLAN.md (OGG Vorbis tag writer implementation)*

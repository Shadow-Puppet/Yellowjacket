---
phase: quick-14
plan: 14
subsystem: queue
tags: [bug-fix, queue, player-sync, rollback]
dependency_graph:
  requires: []
  provides: [roll-back-on-failure-pattern]
  affects: [backend/queue]
tech_stack:
  added: []
  patterns: [roll-back-on-failure for index advancement]
key_files:
  created: []
  modified:
    - backend/queue/queue.go
    - backend/queue/handlers.go
decisions:
  - Extended roll-back pattern to PlayIndex and playFromStart (not in plan but same bug pattern)
metrics:
  duration: 567s
  completed: "2026-03-05"
  tasks_completed: 2
  tasks_total: 2
---

# Quick Task 14: Fix Queue/Player Desync After Track Load Failure

Roll-back-on-failure semantics for all queue index advancement paths, ensuring currentIndex always reflects the track the player actually has loaded.

## What Changed

### Task 1: Make playOrLoadCurrentTrack and playCurrentTrack return bool (6eeddda)

- `playCurrentTrack()` now returns `bool` — false on load failure or play error
- `playOrLoadCurrentTrack()` now returns `bool` — propagates from `playCurrentTrack`/`loadCurrentTrack`
- `loadCurrentTrack()` already returned `bool` — no change needed

### Task 2: Add roll-back-on-failure to all index advancement call sites (2820de2)

- **Next()**: Saves `prevIndex` before advancing; rolls back on failure; RepeatOne path guards emit
- **Previous()**: All three branches (RepeatOne, restart >3s, navigate-to-previous) guard emit or roll back
- **OnPlaybackFinished()**: RepeatOne path guards emit; main advance path rolls back on failure
- **PlayIndex()**: Rolls back to previous index on failure
- **playFromStart()**: Rolls back to -1 on failure

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Extended roll-back to PlayIndex and playFromStart**
- **Found during:** Task 2
- **Issue:** `PlayIndex()` and `playFromStart()` had the same desync bug — they set `currentIndex` and called `playCurrentTrack()` without checking the return, then emitted `QueueIndexChanged` unconditionally
- **Fix:** Applied the same roll-back pattern: save previous index, attempt load, roll back on failure
- **Files modified:** backend/queue/queue.go
- **Commit:** 2820de2

## Verification

- `go build ./backend/...` — passes
- `go test ./backend/queue/... -v -count=1` — 28/28 tests pass
- `go vet ./backend/queue/...` — no warnings

## Self-Check: PASSED

All files exist, all commits verified.

---
phase: quick-14
plan: 14
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/queue/queue.go
  - backend/queue/handlers.go
autonomous: true
must_haves:
  truths:
    - "Next/Previous/OnPlaybackFinished do not emit QueueIndexChanged if the track fails to load"
    - "playOrLoadCurrentTrack returns a bool indicating success"
    - "playCurrentTrack returns a bool indicating success"
    - "On load failure, currentIndex is rolled back to its previous value"
  artifacts:
    - path: "backend/queue/queue.go"
      provides: "Roll-back-on-failure pattern for Next, Previous, Play, RepeatOne, and handleCurrentTrackRemoved"
    - path: "backend/queue/handlers.go"
      provides: "Roll-back-on-failure pattern for OnPlaybackFinished"
  key_links:
    - from: "playOrLoadCurrentTrack"
      to: "loadCurrentTrack / playCurrentTrack"
      via: "bool return value propagation"
      pattern: "if !q\\.playOrLoadCurrentTrack"
---

<objective>
Fix the queue/player desync that occurs when Next/Previous is called and the track fails to load into the player. Currently, `Next()`, `Previous()`, `OnPlaybackFinished()`, and related methods unconditionally advance `currentIndex` and emit `QueueIndexChanged` even when `loadCurrentTrack()` or `playCurrentTrack()` fails. This causes the queue panel to highlight a different track than what the player actually has loaded.

Purpose: Ensure the queue index always reflects the track the player actually has loaded. If a track load fails, roll back the index to its previous value and do not emit `QueueIndexChanged`.

Output: Patched `queue.go` and `handlers.go` with roll-back-on-failure semantics.
</objective>

<execution_context>
@/home/caleb/.config/opencode/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/opencode/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@backend/queue/queue.go
@backend/queue/handlers.go
@backend/queue/navigation.go

<interfaces>
<!-- Key functions and their current signatures -->

From backend/queue/queue.go (lines 1115-1182):
```go
// Currently returns nothing — needs to return bool
func (q *Queue) playOrLoadCurrentTrack(autoPlay bool) {
    if autoPlay {
        q.playCurrentTrack()
    } else {
        q.loadCurrentTrack()
    }
}

// Already returns bool
func (q *Queue) loadCurrentTrack() bool { ... }

// Currently returns nothing — needs to return bool
func (q *Queue) playCurrentTrack() { ... }
```

From backend/queue/queue.go (lines 871-945):
```go
// Next() — unconditionally emits after advancing index
func (q *Queue) Next() {
    q.currentIndex = nextIdx
    q.playOrLoadCurrentTrack(wasPlaying)  // return value discarded
    q.emitIndexChanged()                  // always fires
}

// Previous() — same pattern, multiple paths
func (q *Queue) Previous() {
    // ... restart paths also call playOrLoadCurrentTrack without checking
    q.currentIndex = prevIdx
    q.playOrLoadCurrentTrack(wasPlaying)
    q.emitIndexChanged()
}
```

From backend/queue/handlers.go (lines 1-33):
```go
func (q *Queue) OnPlaybackFinished() {
    q.currentIndex = nextIdx
    q.playCurrentTrack()      // return value ignored (void)
    q.emitIndexChanged()      // always fires
}
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Make playOrLoadCurrentTrack and playCurrentTrack return bool</name>
  <files>backend/queue/queue.go</files>
  <action>
Change `playOrLoadCurrentTrack` to return `bool`:

```go
func (q *Queue) playOrLoadCurrentTrack(autoPlay bool) bool {
    if autoPlay {
        return q.playCurrentTrack()
    }
    return q.loadCurrentTrack()
}
```

Change `playCurrentTrack` to return `bool`:

```go
func (q *Queue) playCurrentTrack() bool {
    if !q.loadCurrentTrack() {
        return false
    }
    err := q.player.Play()
    if err != nil {
        track := q.tracks[q.currentIndex]
        q.logger.Error(
            "Failed to play file from queue",
            "filePath", track.FilePath, "err", err,
        )
        return false
    }
    return true
}
```

Update the doc comment on `playOrLoadCurrentTrack` to document the bool return value (true = success, false = load failed).
Update the doc comment on `playCurrentTrack` to document the bool return value.

Note: `loadCurrentTrack` already returns `bool` — no change needed there.
  </action>
  <verify>go build ./backend/...</verify>
  <done>Both functions return bool; the codebase compiles.</done>
</task>

<task type="auto">
  <name>Task 2: Add roll-back-on-failure to Next, Previous, OnPlaybackFinished, and related call sites</name>
  <files>backend/queue/queue.go, backend/queue/handlers.go</files>
  <action>
Apply the roll-back-on-failure pattern to every call site that advances `currentIndex` and then calls `playOrLoadCurrentTrack`/`playCurrentTrack`.

**In `Next()` (queue.go ~line 871):**

The main advance path (after the RepeatOne early return):
```go
prevIndex := q.currentIndex
q.currentIndex = nextIdx
if !q.playOrLoadCurrentTrack(wasPlaying) {
    q.currentIndex = prevIndex
    return
}
q.emitIndexChanged()
```

For the RepeatOne path (replay current track), the index doesn't change so there's nothing to roll back, but we should still guard the emit:
```go
if q.repeatMode == RepeatOne {
    if q.playOrLoadCurrentTrack(wasPlaying) {
        q.emitIndexChanged()
    }
    return
}
```

**In `Previous()` (queue.go ~line 904):**

Same pattern for every branch:

1. RepeatOne path (~line 914-919): Guard the emit with the return value:
```go
if q.repeatMode == RepeatOne {
    if q.playOrLoadCurrentTrack(wasPlaying) {
        q.emitIndexChanged()
    }
    return
}
```

2. Restart-current-track path (>3 seconds, ~line 922-930): The index doesn't change here either, just guard the emit:
```go
if q.player != nil {
    posSecs, err := q.player.CurrentPositionSeconds()
    if err == nil && posSecs > PreviousRestartThreshold {
        if q.playOrLoadCurrentTrack(wasPlaying) {
            q.emitIndexChanged()
        }
        return
    }
}
```

3. Navigate-to-previous path (~line 933-onwards): Apply full roll-back:
```go
prevIdx := q.previousIndex()
if prevIdx == -1 {
    // At the beginning — just restart the current track.
    if q.playOrLoadCurrentTrack(wasPlaying) {
        q.emitIndexChanged()
    }
    return
}

prevCurrentIndex := q.currentIndex
q.currentIndex = prevIdx
if !q.playOrLoadCurrentTrack(wasPlaying) {
    q.currentIndex = prevCurrentIndex
    return
}
q.emitIndexChanged()
```

**In `OnPlaybackFinished()` (handlers.go):**

Apply roll-back to the main advance path:
```go
// RepeatOne path — index doesn't change, guard emit:
if q.repeatMode == RepeatOne {
    if q.playCurrentTrack() {
        q.emitIndexChanged()
    }
    return
}

nextIdx := q.nextIndex()
if nextIdx == -1 {
    q.onQueueExhausted()
    return
}

prevIndex := q.currentIndex
q.currentIndex = nextIdx
if !q.playCurrentTrack() {
    q.currentIndex = prevIndex
    return
}
q.emitIndexChanged()
```

**In `handleCurrentTrackRemoved()` (queue.go ~line 1184):** Check what this does and apply same pattern if it calls `loadCurrentTrack`.

IMPORTANT: Do NOT change `loadCurrentTrack()` or `loadFileLocked()` themselves — they already work correctly. Only change the call sites that consume their return values.

IMPORTANT: Preserve the mutex-protected setter pattern (lock → write → release → callbacks). The `emitIndexChanged()` calls already happen inside the lock, which is correct. Just make them conditional.
  </action>
  <verify>go build ./backend/... && go test ./backend/queue/... -v -count=1</verify>
  <done>All Next/Previous/OnPlaybackFinished paths check the bool return from playOrLoadCurrentTrack/playCurrentTrack. On failure, currentIndex is rolled back (when it was changed) and QueueIndexChanged is NOT emitted. Tests pass.</done>
</task>

</tasks>

<verification>
go build ./backend/...
go test ./backend/queue/... -v -count=1
go vet ./backend/queue/...
</verification>

<success_criteria>
- `playOrLoadCurrentTrack` returns `bool` propagated from `loadCurrentTrack`/`playCurrentTrack`
- `playCurrentTrack` returns `bool` (true if load + play succeeded)
- `Next()` rolls back `currentIndex` and skips `emitIndexChanged` on failure
- `Previous()` rolls back `currentIndex` and skips `emitIndexChanged` on failure (all branches)
- `OnPlaybackFinished()` rolls back `currentIndex` and skips `emitIndexChanged` on failure
- All existing tests pass
- Code compiles with no vet warnings
</success_criteria>

<output>
After completion, create `.planning/quick/14-fix-queue-player-desync-after-hot-reload/14-SUMMARY.md`
</output>

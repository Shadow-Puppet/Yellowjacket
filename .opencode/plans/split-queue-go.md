# Plan: Split and Refactor `backend/queue/queue.go`

Addresses refactoring catalog #3 (split `queue.go`), #14 (unused sentinels), and #18 (custom `sortInts`), plus two bug fixes and four DRY improvements discovered during analysis.

## Current State

`backend/queue/queue.go` is a single 2297-line file containing:
- Type definitions (9 types/constants)
- Constructor and lifecycle methods
- 11 event handler methods (~310 lines of boilerplate)
- 15+ queue operation methods (add, insert, remove, move, play, etc.)
- 6 navigation/shuffle functions
- 7 database I/O functions
- 4 event emission helpers

The file is hard to navigate, hard to review, and mixes unrelated concerns.

---

## Part 1: File Split

### 1a. `queue.go` (~1200 lines) — Types, struct, constructor, business logic

**Keep:**
- Package doc comment
- All type/const definitions: `RepeatMode`, `PreviousRestartThreshold`, `maxSQLiteVars`, `initialBatchSize`, `trackMeta`, `TrackLoader`, `Track`, `State`, `IndexChanged`, `ModeChanged`, `TracksModified`, `Queue` struct
- Constructor: `NewQueue`
- Lifecycle: `SetContext`, `SetPlayer`
- All public queue operations: `SetQueue`, `resolveRemainingTracks`, `AddTrack`, `AddTracks`, `InsertNext`, `InsertNextTracks`, `InsertTracksAt`, `MoveQueueTracks`, `RemoveTrack`, `RemoveTracks`, `Play`, `playFromStart`, `PlayIndex`, `ToggleShuffle`, `CycleRepeat`, `GetState`, `Clear`, `EmitCurrentState`
- Playback helpers: `playOrLoadCurrentTrack`, `loadCurrentTrack`, `playCurrentTrack`, `handleCurrentTrackRemoved`, `onQueueExhausted`, `reindexPositions`
- New helpers: `trackMeta.toTrack()`, `commitMutation()`

**Imports:** `context`, `log/slog`, `slices`, `sync`, `sync/atomic`, `github.com/wailsapp/wails/v2/pkg/runtime`, `yellowjacket/backend/database`, `yellowjacket/backend/profiling`

### 1b. `handlers.go` (~280 lines) — Event handlers and external callbacks

**Move:**
- `OnPlaybackFinished` (external callback from player — same dispatch pattern as event handlers)
- `registerEventHandlers`
- All 10 `handle*` methods
- New helpers: `toStringSlice()`, `toIntSlice()`

**Imports:** `github.com/wailsapp/wails/v2/pkg/runtime`, `yellowjacket/backend/events`

**Rationale:** Pure dispatch boilerplate. Adding/modifying event handlers only touches this file plus event constants. `OnPlaybackFinished` is included because it's an inbound callback invoked from outside (the player), same conceptual layer as the event handlers.

### 1c. `persistence.go` (~330 lines) — All database I/O

**Move:**
- `lookupTrackMetaBatch`, `lookupChunk` (metadata lookup)
- `persistTracks`, `insertTrackBatch` (track persistence)
- `persistState` (state persistence)
- `SaveState` (public wrapper)
- `RestoreState` (public, loads from DB)

**Imports:** `database/sql`, `encoding/json`, `fmt`, `strings`, `yellowjacket/backend/database/sql/sqlcgen`, `yellowjacket/backend/profiling`

**Rationale:** All database interaction in one place. Schema changes, query optimizations, or persistence strategy changes only affect this file.

### 1d. `navigation.go` (~130 lines) — Index navigation and shuffle order

**Move:**
- `nextIndex`, `previousIndex` (linear/shuffled dispatch with repeat logic)
- `nextShuffledIndex`, `previousShuffledIndex`
- `currentShufflePosition`
- `generateShuffleOrder` (Fisher-Yates)

**Imports:** `math/rand/v2`

**Rationale:** The catalog suggested `shuffle.go`, but these 6 functions form a cohesive "navigation" group — `nextIndex`/`previousIndex` contain both the linear (repeat-aware) and the shuffle dispatching logic. Naming it `shuffle.go` would be misleading since half the file handles non-shuffle navigation. These functions only access `q.tracks`, `q.currentIndex`, `q.shuffleOrder`, and `q.repeatMode` — a cleanly bounded dependency set.

### 1e. `emit.go` (~75 lines) — Event emission helpers

**Move:**
- `emitQueueChanged`
- `emitIndexChanged`
- `emitModeChanged`
- `emitTracksModified`

**Imports:** `github.com/wailsapp/wails/v2/pkg/runtime`, `yellowjacket/backend/events`

**Rationale:** Clean boundary — the rest of the code calls `q.emit*()` without knowing event names or payload shapes.

---

## Part 2: Bug Fixes (behavior-preserving — fixing existing broken behavior)

### 2a. Fix `InsertNext` empty-queue bug

**Location:** `queue.go:991-1041` (current)

**Problem:** `InsertNext` does not handle the empty-queue case. When called on an empty queue:
- `insertPos = currentIndex + 1 = 0 + 1 = 1` (out of bounds clamped to 0 by the guard)
- A track is inserted, but `currentIndex` stays at 0 and `loadCurrentTrack` is never called
- The user sees a queue with one track but nothing loaded

Compare with `InsertNextTracks` (line 977-980) which correctly checks `wasEmpty` and loads the first track.

**Fix:** Add after the persist calls in `InsertNext`:
```go
wasEmpty := len(q.tracks) == 0
// ... existing insert logic ...
// After commitMutation:
if wasEmpty && len(q.tracks) > 0 {
    q.currentIndex = 0
    q.loadCurrentTrack()
}
```

### 2b. Fix `AddTracks` persist-before-index ordering

**Location:** `queue.go:906-912` (current)

**Problem:** `AddTracks` calls `persistTracks()` + `persistState()` at lines 906-907, then sets `currentIndex = 0` and calls `loadCurrentTrack()` at lines 909-912. If the app crashes between persist and index update, the restored state has the wrong `currentIndex`. `AddTrack` does this correctly (sets index before persist).

**Fix:** Move the `wasEmpty` check and `currentIndex = 0` assignment to before the `commitMutation()` call, matching the pattern in `AddTrack`.

---

## Part 3: DRY Improvements (behavior-preserving)

### 3a. Extract `toStringSlice` and `toIntSlice` helpers (in `handlers.go`)

**Problem:** The `[]interface{} -> []string` conversion is copy-pasted in 4 handlers (`handleSetQueue`, `handleAddTracksToQueue`, `handleInsertTracksAtIndex`, `handlePlayTracksNext`). The `[]interface{} -> []int` conversion is in 2 handlers (`handleRemoveTracksFromQueue`, `handleMoveQueueTracks`).

**New helpers:**
```go
// toStringSlice extracts strings from a Wails event argument.
func toStringSlice(raw []interface{}) []string {
    result := make([]string, 0, len(raw))
    for _, v := range raw {
        if s, ok := v.(string); ok {
            result = append(result, s)
        }
    }
    return result
}

// toIntSlice extracts ints (from float64) from a Wails event argument.
func toIntSlice(raw []interface{}) []int {
    result := make([]int, 0, len(raw))
    for _, v := range raw {
        if f, ok := v.(float64); ok {
            result = append(result, int(f))
        }
    }
    return result
}
```

Eliminates ~30 lines of repetition, centralizes type-coercion logic.

### 3b. Extract `trackMeta.toTrack(position)` method (in `queue.go`)

**Problem:** The `trackMeta` -> `Track` struct literal appears 7 times across `SetQueue`, `resolveRemainingTracks`, `AddTrack`, `AddTracks`, `InsertNextTracks`, `InsertNext`, `InsertTracksAt`.

**New method:**
```go
// toTrack converts metadata lookup results into a queue Track.
func (m trackMeta) toTrack(position int64) Track {
    return Track{
        AudioFileID: m.AudioFileID,
        FilePath:    m.FilePath,
        Position:    position,
        Title:       m.Title,
        Artist:      m.Artist,
    }
}
```

Eliminates ~35 lines. Creates one authoritative mapping point — if a field is added to `Track`, only one place needs updating.

### 3c. Extract `commitMutation(reindex bool)` helper (in `queue.go`)

**Problem:** The post-mutation epilogue (reindex positions → regenerate shuffle order → persist tracks → persist state) is repeated in 8+ methods: `InsertNextTracks`, `InsertNext`, `InsertTracksAt`, `MoveQueueTracks`, `RemoveTrack`, `RemoveTracks`, `AddTracks`, `SetQueue` (small-batch path), `AddTrack` (after unification).

**New helper:**
```go
// commitMutation persists the current queue state after a mutation.
// When reindex is true, track positions are renumbered first.
func (q *Queue) commitMutation(reindex bool) {
    if reindex {
        q.reindexPositions()
    }
    if q.shuffleMode {
        q.generateShuffleOrder()
    }
    q.persistTracks()
    q.persistState()
}
```

Eliminates ~40 lines. Ensures every mutation consistently applies the full epilogue — no risk of forgetting one of the steps.

### 3d. Use `slices.Insert` for slice insertions (in `queue.go`)

**Problem:** The manual tail-copy insertion pattern appears 3 times:
```go
tail := make([]Track, len(q.tracks[insertPos:]))
copy(tail, q.tracks[insertPos:])
q.tracks = append(q.tracks[:insertPos], newTracks...)
q.tracks = append(q.tracks, tail...)
```
in `InsertNextTracks`, `InsertTracksAt`, and `MoveQueueTracks`. `InsertNext` has a variant.

**Fix:** Replace all with `q.tracks = slices.Insert(q.tracks, insertPos, newTracks...)`. The `slices` package is already imported.

### 3e. Unify `AddTrack` persistence strategy (in `queue.go`)

**Problem:** `AddTrack` is the only method that uses a single-row `InsertQueueTrack` DB call (line 834), while every other mutating method uses `persistTracks` (full table rewrite). This dual strategy means:
- If the single-row insert fails, the in-memory state diverges from the DB
- `AddTrack` has different error recovery behavior than all other methods
- The shuffle order append (line 847) is an optimization that `AddTracks` doesn't share, creating inconsistency

**Fix:** Replace `AddTrack`'s custom DB insert with `commitMutation(false)` (no reindex needed since it appends). This makes it consistent with every other method. The performance cost of a full table rewrite for a single-track add is negligible for music-player queue sizes (typically <10K tracks).

---

## Part 4: Cleanup (bundled from catalog #14 and #18)

### 4a. Delete `sortInts`, use `slices.Sort` (catalog #18)

**Location:** `queue.go:1274-1281` (current)

Delete the hand-rolled insertion sort. Replace its one call site in `MoveQueueTracks` (`sortInts(sorted)` → `slices.Sort(sorted)`). `slices.Sort` is already used elsewhere in the same file (line 1364).

### 4b. Remove exported `PlayFromStart` wrapper

**Location:** `queue.go:1521-1530` (current)

`PlayFromStart` is exported but has zero callers outside the package. The unexported `playFromStart` already exists. Remove the exported wrapper — if external access is ever needed, it can be re-added.

---

## Execution Order

The order matters because later steps depend on earlier ones:

1. **Replace `sortInts` with `slices.Sort`** — single-line change, eliminates a function before the split
2. **Remove `PlayFromStart`** — eliminates dead code before the split
3. **Add `trackMeta.toTrack()` method** — replace all 7 call sites
4. **Add `commitMutation()` helper** — replace all 8+ call sites
5. **Fix `InsertNext` empty-queue bug** — add `wasEmpty` guard
6. **Fix `AddTracks` persist ordering** — move index assignment before persist
7. **Unify `AddTrack` persistence** — replace custom insert with `commitMutation`
8. **Use `slices.Insert`** — replace 3-4 manual insertion patterns
9. **Extract `handlers.go`** — move `OnPlaybackFinished`, `registerEventHandlers`, all `handle*` methods; add `toStringSlice`/`toIntSlice` helpers; update all 6 call sites
10. **Extract `emit.go`** — move all 4 `emit*` methods
11. **Extract `navigation.go`** — move all 6 navigation/shuffle functions
12. **Extract `persistence.go`** — move all 7 persistence/lookup functions
13. **Clean up `queue.go` imports** — remove now-unused imports (`encoding/json`, `fmt`, `strings`, `math/rand/v2`, `errors`, `yellowjacket/backend/events`, `yellowjacket/backend/database/sql/sqlcgen`)
14. **Delete `ErrEmptyQueue` and `ErrNoPlayer`** — unused sentinels (catalog #14)
15. **Run `make lint`** — fix any formatting/import-order issues
16. **Run `make test`** — verify nothing is broken (note: no queue-specific tests exist, but this catches compilation errors and any tests that depend on queue indirectly)
17. **Update refactoring catalog** — mark #3, #14, #18 as solved

## Risk Assessment

**Very low risk.** All files remain in the same `queue` package — field access, unexported methods, and mutex sharing work identically across files within a package. The Go compiler catches any missing imports or broken references at build time. The two bug fixes change behavior only in edge cases that are currently broken. The DRY extractions are mechanical transformations that preserve identical behavior.

## What This Does NOT Change

- No changes to the public API surface (except removing unused `PlayFromStart` and the unused sentinels)
- No changes to the mutex strategy or locking granularity
- No changes to the event system or frontend
- No changes to database schema or query logic
- No new dependencies

# Plan: #12 — Move queue frontend→backend communication to Wails bindings

## Goal

Replace the 15 `Request*` events (frontend→backend) with direct Wails bindings while keeping the 4 backend→frontend push events (`QueueChanged`, `QueueIndexChanged`, `QueueModeChanged`, `QueueTracksModified`) intact. This eliminates ~420 lines of handler boilerplate in Go and aligns the queue's communication pattern with playlists.

## Rationale

**Why bindings for frontend→backend (replacing events):**
- Eliminates 420 lines of hand-written type-assertion boilerplate in `handlers.go`
- Provides compile-time type safety — Wails auto-generates typed TypeScript bindings from Go method signatures, so `float64`→`int` casting, `[]interface{}`→`[]string` conversion, and `len(data)` validation all disappear
- Adding a new queue operation becomes a 1-file change (add Go method) vs the current 4-file change (Go event constant, TS event constant, Go handler, TS store method)
- Aligns with the playlist pattern, reducing cognitive overhead

**Why keep events for backend→frontend (not replacing with invalidate-and-refetch):**
- The queue's delta system (`QueueTracksModified` with add/insert/remove/move actions) is genuinely good architecture for a data structure that changes frequently during playback
- Avoids unnecessary round-trips — the backend pushes only what changed
- The playlist's invalidate-and-refetch pattern works for playlists (infrequent mutations) but would be wasteful for a queue (changes on every track advance)

## Prerequisites

The queue must be created in `NewYellowJacketApp()` (before `wails.Run()`) rather than in `OnStartup()`, because Wails v2 consumes the `Bind` slice eagerly at startup via reflection. The struct pointer must be non-nil and fully constructed at `Bind` time.

This is safe because `queue.NewQueue()` only needs `logger` and `db` (both already available in `NewYellowJacketApp`). The player dependency and context are set later via `SetPlayer()` and `SetContext()` during `OnStartup`, which is the existing two-phase initialization pattern used by all other bound services.

## Detailed Steps

### Step 1: Move queue construction to `NewYellowJacketApp` and add to `FEBindings`

**File:** `backend/app.go`

In `NewYellowJacketApp()`, after the playlist service is created (~line 104), add:

```go
yjApp.queue = queue.NewQueue(yjApp.logger, yjApp.database)
```

Add the queue to `FEBindings`:

```go
yjApp.FEBindings = []any{
    yjApp.FrontendUtil,
    yjApp.appConfig,
    yjApp.library,
    yjApp.playlist,
    yjApp.queue,
}
```

In `OnStartup()`, remove `queue.NewQueue(...)` and keep only the deferred initialization:

```go
yj.queue.SetContext(ctx)
yj.queue.SetPlayer(yj.player)
yj.queue.RestoreState()
```

### Step 2: Remove `registerEventHandlers()` and all handler boilerplate

**File:** `backend/queue/handlers.go`

Remove:
- `registerEventHandlers()` — all 16 `runtime.EventsOn` registrations (lines 41-172)
- All 10 `handle*` functions (lines 201-461): `handleSetQueue`, `handleAddToQueue`, `handlePlayNext`, `handleRemoveFromQueue`, `handleRemoveTracksFromQueue`, `handleAddTracksToQueue`, `handleInsertTracksAtIndex`, `handleMoveQueueTracks`, `handlePlayQueueIndex`, `handlePlayTracksNext`
- The two helper functions `toStringSlice` and `toIntSlice` (lines 175-199)

Keep:
- `OnPlaybackFinished()` (lines 11-38) — this is domain logic, not event boilerplate

**File:** `backend/queue/queue.go`

In `SetContext()`, remove the call to `q.registerEventHandlers()`. The method becomes:

```go
func (q *Queue) SetContext(ctx context.Context) {
    q.ctx = ctx
}
```

### Step 3: Remove the 15 `Request*` queue event constants

**File:** `backend/events/events.go`

Remove from the "Queue events" const block (lines 38-52):
- `RequestNext`
- `RequestPrevious`
- `RequestSetQueue`
- `RequestAddToQueue`
- `RequestPlayNext`
- `RequestRemoveFromQueue`
- `RequestToggleShuffle`
- `RequestCycleRepeat`
- `RequestAddTracksToQueue`
- `RequestPlayTracksNext`
- `RequestPlayQueueIndex`
- `RequestRemoveTracksFromQueue`
- `RequestInsertTracksAtIndex`
- `RequestMoveQueueTracks`
- `RequestClearQueue`

Keep `RequestPlay` — it's in the "Playback control events" block and is used by the queue's event handler. Since we're removing `registerEventHandlers`, also remove `RequestPlay` from the queue's event handler. But check if `RequestPlay` is still used by the player package first.

> **Note:** `RequestPlay` is currently handled by the queue (in `handlers.go:48`), not the player. After this refactor, the queue's `Play()` method will be callable directly via bindings, so the `RequestPlay` event handler in the queue is no longer needed. However, `RequestPlay` may still be emitted by the frontend for player-related actions — audit all `RequestPlay` usages before removing the constant.

**File:** `frontend/src/events.ts`

Remove the corresponding 15 `Request*` constants from lines 28-42. Keep the 4 backend→frontend queue events (lines 24-27).

### Step 4: Rewrite the queue store to use Wails bindings

**File:** `frontend/src/store/queue-store.ts`

Replace the 14 action methods that call `EventsEmit(Events.Request*)` with direct calls to the auto-generated Wails bindings.

**Before** (example):
```typescript
import { EventsOn, EventsEmit } from '@runtime/runtime';
import { Events } from '../events';

// ...
next(): void {
    EventsEmit(Events.RequestNext);
}

setQueue(filePaths: string[], startIndex: number, shuffleStart = false): void {
    EventsEmit(Events.RequestSetQueue, filePaths, startIndex, shuffleStart);
}
```

**After** (example):
```typescript
import { EventsOn } from '@runtime/runtime';
import { Events } from '../events';
import * as QueueService from '@go/queue/Queue';

// ...
next(): void {
    QueueService.Next();
}

setQueue(filePaths: string[], startIndex: number, shuffleStart = false): void {
    QueueService.SetQueue(filePaths, startIndex, shuffleStart);
}
```

Keep the entire `initializeEventListeners()` method unchanged — the 4 backend→frontend event subscriptions (`QueueChanged`, `QueueIndexChanged`, `QueueModeChanged`, `QueueTracksModified`) and the `applyTracksDelta()` logic remain as-is.

Remove the `EventsEmit` import if no longer needed after removing all `Request*` emissions.

**Complete action method mapping** (queue store method → Wails binding):

| Store method | Current event | Wails binding call |
|---|---|---|
| `next()` | `RequestNext` | `QueueService.Next()` |
| `previous()` | `RequestPrevious` | `QueueService.Previous()` |
| `setQueue(filePaths, startIndex, shuffleStart)` | `RequestSetQueue` | `QueueService.SetQueue(filePaths, startIndex, shuffleStart)` |
| `addToQueue(filePath)` | `RequestAddToQueue` | `QueueService.AddTrack(filePath)` |
| `playNext(filePath)` | `RequestPlayNext` | `QueueService.InsertNext(filePath)` |
| `removeFromQueue(position)` | `RequestRemoveFromQueue` | `QueueService.RemoveTrack(position)` |
| `removeTracksFromQueue(positions)` | `RequestRemoveTracksFromQueue` | `QueueService.RemoveTracks(positions)` |
| `addTracksToQueue(filePaths)` | `RequestAddTracksToQueue` | `QueueService.AddTracks(filePaths)` |
| `playTracksNext(filePaths)` | `RequestPlayTracksNext` | `QueueService.InsertNextTracks(filePaths)` |
| `toggleShuffle()` | `RequestToggleShuffle` | `QueueService.ToggleShuffle()` |
| `cycleRepeat()` | `RequestCycleRepeat` | `QueueService.CycleRepeat()` |
| `playAtIndex(index)` | `RequestPlayQueueIndex` | `QueueService.PlayIndex(index)` |
| `insertTracksAtIndex(filePaths, index)` | `RequestInsertTracksAtIndex` | `QueueService.InsertTracksAt(filePaths, index)` |
| `moveTracksInQueue(fromIndices, toIndex)` | `RequestMoveQueueTracks` | `QueueService.MoveQueueTracks(fromIndices, toIndex)` |
| `clearQueue()` | `RequestClearQueue` | `QueueService.Clear()` |

> **Note:** Some store method names don't match Go method names (e.g., `addToQueue` → `AddTrack`, `playNext` → `InsertNext`). The store method names can remain unchanged for API stability — only the implementation changes.

### Step 5: Handle `Play()` specifically

The queue's `Play()` method is currently triggered by the `RequestPlay` event, which is in the "Playback control events" group and is also emitted by `player-controls.ts`. After this refactor:

- The `RequestPlay` event handler in `handlers.go:48` is removed along with all other handlers
- The frontend should call `QueueService.Play()` directly instead of `EventsEmit(Events.RequestPlay)`

Audit all places that emit `RequestPlay`:
- `frontend/src/components/audio-player/controls/player-controls.ts` — the play button emits `RequestPlay`. This should be changed to call `QueueService.Play()` (or more likely, the queue store should expose a `play()` method that delegates to the binding)

If `RequestPlay` has no other consumers after this change, remove the event constant from both `events.go` and `events.ts`.

### Step 6: Regenerate Wails bindings

Run `wails generate module` (or `make dev` which triggers binding generation) to produce the auto-generated files:

- `frontend/wailsjs/go/queue/Queue.js` — JavaScript bridge calling `window['go']['queue']['Queue'][method](...)`
- `frontend/wailsjs/go/queue/Queue.d.ts` — TypeScript declarations with proper types
- `frontend/wailsjs/go/models.ts` — Updated with `queue.Track`, `queue.State`, `queue.RepeatMode`, etc.

> **Important:** The auto-generated TypeScript types will mirror the Go struct JSON tags, so the frontend types already defined in `queue-store.ts` (`QueueTrack`, `QueueState`, `IndexChanged`, `ModeChanged`, `TracksModified`) will have matching auto-generated equivalents in `models.ts`. We should keep the manually-defined types in the store (they're used by the event listeners which still need them) but could optionally import the model types where convenient.

### Step 7: Handle `SetContext` visibility

When a struct is added to Wails `FEBindings`, **all exported methods** become callable from JavaScript. `SetContext(ctx context.Context)` and `SetPlayer(player TrackLoader)` would be exposed, which is undesirable — they're internal lifecycle methods, not frontend API.

Options:
1. **Unexport them** — rename to `setContext`/`setPlayer`. This requires updating `app.go` to call `q.setContext(ctx)` etc. But unexported methods on structs in other packages aren't accessible, so this won't work without making them package-internal.
2. **Create a thin facade struct** — a `Service` (or `API`) struct that embeds or wraps `*Queue` and only exposes the methods the frontend should call. This is the playlist pattern (`playlist.Service`).
3. **Accept the exposure** — Wails will generate bindings for `SetContext` and `SetPlayer`, but the frontend simply won't call them. They'll be inert in the generated JS. This is what happens with `playlist.Service.SetContext` — it's in the generated `Service.js` but never imported by the frontend.

**Recommendation:** Option 3 — accept it. The playlist already has `SetContext` exposed in its generated bindings (`frontend/wailsjs/go/playlist/Service.js:69`) and it's not a problem. Wails bindings are not a security boundary (the frontend and backend are in the same process). The generated bindings are auto-generated artifacts, not a public API. No one will accidentally call `SetContext` from the frontend.

If `SetPlayer` is a concern because `TrackLoader` is an interface type that Wails can't serialize, Wails may skip it or error during binding generation. If so, either unexport `SetPlayer` only, or have `app.go` set it via an unexported package-level function. This needs testing during step 6.

### Step 8: Update `queue-controller.ts` (no changes needed)

The `QueueController` (`frontend/src/store/controllers/queue-controller.ts`) proxies all actions through `queueStore.*()`. Since we're only changing the store's internal implementation (from `EventsEmit` to binding calls), the controller needs zero changes. All 14 action proxy methods remain identical.

### Step 9: Update components that call `queueStore` directly (no changes needed)

These 6 components import `queueStore` and call its action methods:
- `player-controls.ts` — `queueStore.next()`, `.previous()`, `.toggleShuffle()`, `.cycleRepeat()`
- `track-list.ts` — `queueStore.setQueue()`, `.addTracksToQueue()`, `.playTracksNext()`
- `cover-grid.ts` — `queueStore.setQueue()`, `.addTracksToQueue()`, `.playTracksNext()`
- `genres-view.ts` — `queueStore.setQueue()`, `.addTracksToQueue()`, `.playTracksNext()`
- `artists-view.ts` — `queueStore.setQueue()`, `.addTracksToQueue()`, `.playTracksNext()`
- `playlist-view.ts` — `queueStore.setQueue()`, `.addTracksToQueue()`, `.playTracksNext()`

Since the store's public API (method signatures) is unchanged, none of these components need modifications.

**Exception:** `player-controls.ts` currently emits `Events.RequestPlay` directly for the play/pause button (not through the queue store). This specific call site needs to be updated to either:
- Call `queueStore.play()` (add a `play()` method to the store), or
- Call `QueueService.Play()` directly

### Step 10: Run tests, lint, and build

```bash
make test          # Verify Go tests pass (especially queue tests)
make lint          # Verify linting passes
cd frontend && pnpm exec tsc --noEmit   # Verify TypeScript types
make build-dev     # Full build to verify Wails binding generation works
```

## Files Modified

| File | Action | Description |
|---|---|---|
| `backend/app.go` | Edit | Move queue construction; add to `FEBindings` |
| `backend/queue/handlers.go` | Major edit | Remove all `handle*` functions, `registerEventHandlers`, `toStringSlice`, `toIntSlice`. Keep only `OnPlaybackFinished` |
| `backend/queue/queue.go` | Edit | Remove `registerEventHandlers()` call from `SetContext` |
| `backend/events/events.go` | Edit | Remove 15 `Request*` queue constants |
| `frontend/src/events.ts` | Edit | Remove 15 `Request*` queue constants |
| `frontend/src/store/queue-store.ts` | Edit | Replace `EventsEmit` action methods with Wails binding calls |
| `frontend/src/components/audio-player/controls/player-controls.ts` | Edit | Replace `RequestPlay` event emission with binding call |
| `frontend/wailsjs/go/queue/Queue.js` | Auto-generated | New file from `wails generate` |
| `frontend/wailsjs/go/queue/Queue.d.ts` | Auto-generated | New file from `wails generate` |
| `frontend/wailsjs/go/models.ts` | Auto-generated | Updated with queue types |

## Files NOT Modified

| File | Reason |
|---|---|
| `backend/queue/emit.go` | Backend→frontend push events are kept as-is |
| `frontend/src/store/controllers/queue-controller.ts` | Proxies through store; no API change |
| `frontend/src/components/queue-panel/queue-panel.ts` | Uses controller; no API change |
| `frontend/src/components/track-list/track-list.ts` | Calls store methods; no API change |
| `frontend/src/components/cover-grid/cover-grid.ts` | Calls store methods; no API change |
| `frontend/src/components/genres-view/genres-view.ts` | Calls store methods; no API change |
| `frontend/src/components/artists-view/artists-view.ts` | Calls store methods; no API change |
| `frontend/src/components/playlist-view/playlist-view.ts` | Calls store methods; no API change |

## Risk Assessment

**Low risk:**
- The queue's public Go methods are already well-tested and have clear type signatures
- The store's public API doesn't change, so no component-level regressions
- The backend→frontend event system is untouched
- The pattern is proven by the playlist package

**Medium risk:**
- `SetPlayer(TrackLoader)` exposure in Wails bindings — Wails may not handle the interface parameter. If binding generation fails, we'll need to unexport `SetPlayer` and wire it via a package-level function or an exported setter that takes concrete types
- `RequestPlay` event has cross-cutting usage in `player-controls.ts` — needs careful auditing to avoid breaking play/pause

## Net Effect

- **~420 lines removed** from `handlers.go` (boilerplate)
- **~20 lines removed** from `events.go` and `events.ts` (15 event constants each)
- **~30 lines changed** in `queue-store.ts` (swap `EventsEmit` for binding calls)
- **~10 lines changed** in `app.go` (move construction, add to bindings)
- **~3 auto-generated files** created/updated by Wails
- Adding a new queue operation goes from a 4-file change to a 1-2 file change

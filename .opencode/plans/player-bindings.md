# Plan: Move player frontend→backend communication to Wails bindings

## Goal

Replace the 4 remaining `EventsEmit` calls (frontend→backend) in `player-store.ts` with direct Wails bindings, eliminating ~90 lines of handler boilerplate in Go. This completes the pattern established by the queue refactoring (#12) — after this change, **all** frontend→backend communication uses Wails bindings.

## Rationale

Same benefits as the queue refactor:
- Eliminates untyped `data[0].(float64)` casting boilerplate
- Provides compile-time type safety via auto-generated TypeScript declarations
- Adding a new player operation becomes a 1-file change (Go method) instead of 4 files
- Completes the architectural consistency — every frontend→backend call uses bindings, every backend→frontend push uses events

## Key Challenge: `speaker.Init()` in constructor

The player is currently created in `OnStartup` (after `wails.Run()`) because `NewPlayer` calls `speaker.Init()` to initialize audio hardware. Wails bindings must be registered before `wails.Run()`, so we need to split the constructor.

**Solution:** Extract `speaker.Init()` into a separate `InitSpeaker()` method. `NewPlayer` creates the struct with all fields initialized (logger, db, state, default format) but does NOT touch audio hardware. `InitSpeaker()` is called during `OnStartup` when hardware is available.

This is safe because:
- `NewPlayer` already initializes all struct fields before `speaker.Init()` runs
- `speaker.Init()` doesn't depend on any struct state — it only uses the sample rate constant
- The player's methods that touch the speaker (`Play`, `Pause`, `Seek`, `LoadFile`) are only called after `OnStartup` completes, so the speaker will always be initialized before any method is invoked via binding

## Detailed Steps

### Step 1: Split `NewPlayer` — extract `InitSpeaker`

**File:** `backend/player/player.go`

Change `NewPlayer` to accept only `logger` and `db` (remove the `ctx` parameter — context is set later via `SetContext`). Remove `speaker.Init()` from the constructor.

Add a new `InitSpeaker() error` method that does the `speaker.Init()` call.

**Before:**
```go
func NewPlayer(ctx context.Context, logger *slog.Logger, db *database.DB) (*Player, error) {
    player := &Player{ctx: ctx, logger: logger, db: db, state: Stopped, ...}
    err := speaker.Init(...)
    if err != nil { return nil, ... }
    return player, nil
}
```

**After:**
```go
func NewPlayer(logger *slog.Logger, db *database.DB) *Player {
    return &Player{logger: logger, db: db, state: Stopped, ...}
}

func (p *Player) InitSpeaker() error {
    err := speaker.Init(p.format.SampleRate, p.format.SampleRate.N(time.Second/10))
    if err != nil { return fmt.Errorf("failed to initialize speaker: %w", err) }
    return nil
}
```

Note: `NewPlayer` no longer returns an error (struct creation can't fail) and no longer takes `ctx` (set via `SetContext`).

### Step 2: Remove `registerEventHandlers` from player

**File:** `backend/player/player.go`

Delete the entire `registerEventHandlers()` method (lines 154-272) — all 4 `runtime.EventsOn` registrations and their handler closures.

Update `SetContext` to remove the `registerEventHandlers()` call. Keep only the context assignment and state restoration:

```go
func (p *Player) SetContext(ctx context.Context) {
    p.mu.Lock()
    p.ctx = ctx
    p.mu.Unlock()

    p.mu.Lock()
    p.restoreStateLocked()
    p.mu.Unlock()
}
```

Remove the `"fmt"` import if it becomes unused (it was used by `fmt.Sprintf("%T", data[0])` in the handlers). Check if `fmt` is still used elsewhere in the file — yes, it's used in `loadFileLocked`, `seekLocked`, etc. Keep it.

Remove the `"yellowjacket/backend/events"` import — check first. It's used by:
- `registerEventHandlers` (being removed) — uses `events.RequestPause`, `events.RequestLoadFile`, `events.Seek`, `events.RequestSetVolume`
- `emitPlaybackStateChanged` — uses `events.PlaybackStateChanged`
- `emitPlaybackFinished` — uses `events.PlaybackFinished`
- `emitVolumeChanged` — uses `events.VolumeChanged`
- `emitTrackChanged` — uses `events.TrackChanged`
- `seekLocked` — uses `events.SeekFailed`
- `UnloadTrack` — uses `events.TrackChanged`

So `events` import stays (it's still used by the emit helpers).

The `runtime` import also stays (used by emit helpers and `UnloadTrack`).

### Step 3: Update `SetVolume` to include side effects

**File:** `backend/player/player.go`

The current `SetVolume` only calls `setVolumeLocked()`. The event handler also called `emitVolumeChanged()` and `saveState()`. Update `SetVolume` to match what the event handler did:

**Before:**
```go
func (p *Player) SetVolume(desiredVolume UserVolume) error {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.setVolumeLocked(desiredVolume)
    return nil
}
```

**After:**
```go
func (p *Player) SetVolume(desiredVolume UserVolume) {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.setVolumeLocked(desiredVolume)
    p.emitVolumeChanged()
    p.saveState()
}
```

Note: changed return type from `error` to void — `setVolumeLocked` never fails, and this avoids Wails generating a Promise rejection for a method that can't error. Check if any Go code calls `SetVolume` and checks the error — no callers exist (confirmed above).

### Step 4: Update `app.go` — create player early, add to `FEBindings`

**File:** `backend/app.go`

In `NewYellowJacketApp`, create the player early (after db is available):

```go
yjApp.player = player.NewPlayer(yjApp.logger.WithGroup("player"), yjApp.database)
```

Add to `FEBindings`:

```go
yjApp.FEBindings = []any{
    yjApp.FrontendUtil,
    yjApp.appConfig,
    yjApp.library,
    yjApp.playlist,
    yjApp.queue,
    yjApp.player,
}
```

In `OnStartup`, replace player creation with deferred initialization:

```go
if err := yj.player.InitSpeaker(); err != nil {
    startupErr = errors.Join(startupErr, fmt.Errorf("could not initialize speaker: %w", err))
}
yj.player.SetContext(ctx)
```

### Step 5: Update player test

**File:** `backend/player/player_test.go`

Update the test to match the new two-phase constructor:

**Before:**
```go
p, err := NewPlayer(context.Background(), slog.Default(), nil)
if err != nil { t.Fatalf(...) }
p.SetContext(t.Context())
```

**After:**
```go
p := NewPlayer(slog.Default(), nil)
if err := p.InitSpeaker(); err != nil { t.Fatalf(...) }
p.SetContext(t.Context())
```

### Step 6: Remove player `Request*` event constants from Go and TS

**File:** `backend/events/events.go`

Remove from "Playback control events" block:
- `RequestPause`
- `RequestLoadFile`

Remove the entire "Seek events" block — `Seek` was only used as a frontend→backend event. Keep `SeekFailed` by moving it elsewhere (e.g., into a "Playback control events" block or its own group).

Remove from "Volume events" block:
- `RequestSetVolume`

**File:** `frontend/src/events.ts`

Remove:
- `RequestPause`
- `RequestLoadFile`
- `Seek`
- `RequestSetVolume`

Keep:
- `PlaybackStateChanged`, `PlaybackFinished` (backend→frontend push)
- `SeekFailed` (backend→frontend push, even though unused — separate issue #15)
- `TrackChanged` (backend→frontend push)
- `VolumeChanged` (backend→frontend push)

### Step 7: Regenerate Wails bindings

Run `wails generate module` to produce:
- `frontend/wailsjs/go/player/Player.js`
- `frontend/wailsjs/go/player/Player.d.ts`
- Updated `frontend/wailsjs/go/models.ts` with `player.TrackInfo`, `player.UserVolume`, etc.

Expected generated bindings for the methods we need:
- `Pause(): Promise<void>` (from `func (p *Player) Pause() error`)
- `LoadFile(arg1: string): Promise<void>` (from `func (p *Player) LoadFile(filePath string) error`)
- `Seek(arg1: number): Promise<void>` (from `func (p *Player) Seek(targetSeconds int) error`)
- `SetVolume(arg1: number): Promise<void>` (from `func (p *Player) SetVolume(desiredVolume UserVolume)`)

Note: `UserVolume` is `type UserVolume int`, so Wails will serialize it as a plain number. The generated TS type will be `number` (or `player.UserVolume` which maps to `number`).

### Step 8: Rewrite `player-store.ts` actions to use Wails bindings

**File:** `frontend/src/store/player-store.ts`

Replace `EventsEmit` action methods with Wails binding calls:

| Store method | Current | After |
|---|---|---|
| `pause()` | `EventsEmit(Events.RequestPause)` | `Player.Pause()` |
| `loadTrack(filePath)` | `EventsEmit(Events.RequestLoadFile, filePath)` | `Player.LoadFile(filePath)` |
| `seek(seconds)` | `EventsEmit(Events.Seek, seconds)` | `Player.Seek(seconds)` |
| `setVolume(level)` | `EventsEmit(Events.RequestSetVolume, level)` | `Player.SetVolume(level)` |

Remove the `EventsEmit` import (only `EventsOn` will be needed).

Add import: `import * as Player from '@go/player/Player';`

The 4 backend→frontend event subscriptions (`PlaybackStateChanged`, `TrackChanged`, `PlaybackFinished`, `VolumeChanged`) remain unchanged.

### Step 9: Run tests, lint, and TypeScript type check

```bash
make test
make lint
cd frontend && pnpm exec tsc --noEmit
cd frontend && pnpm build
```

## Files Modified

| File | Action | Description |
|---|---|---|
| `backend/player/player.go` | Edit | Split `NewPlayer`, add `InitSpeaker`, remove `registerEventHandlers`, update `SetVolume` |
| `backend/app.go` | Edit | Move player creation early, add to `FEBindings`, call `InitSpeaker` in `OnStartup` |
| `backend/player/player_test.go` | Edit | Update test to use new constructor + `InitSpeaker` |
| `backend/events/events.go` | Edit | Remove `RequestPause`, `RequestLoadFile`, `Seek`, `RequestSetVolume` |
| `frontend/src/events.ts` | Edit | Remove same 4 constants |
| `frontend/src/store/player-store.ts` | Edit | Replace `EventsEmit` with Wails binding calls |
| `frontend/wailsjs/go/player/Player.js` | Auto-generated | New |
| `frontend/wailsjs/go/player/Player.d.ts` | Auto-generated | New |
| `frontend/wailsjs/go/models.ts` | Auto-generated | Updated with player types |

## Files NOT Modified

| File | Reason |
|---|---|
| `frontend/src/store/controllers/player-controller.ts` | Proxies through store; no API change |
| `frontend/src/components/audio-player/` | Uses controller/store; no API change |
| All other component files | No direct player store interaction for these actions |

## Risk Assessment

**Low risk:**
- The player's public methods (`Pause`, `LoadFile`, `Seek`) already have the correct behavior — the event handlers were just thin wrappers
- `SetVolume` is the only method that needs side effects added, and it has zero existing callers
- The test is an integration test that skips by default

**Medium risk:**
- `InitSpeaker()` splitting — if any code path calls a player method that touches the speaker before `InitSpeaker()` runs, it will panic. This is safe because all player method calls happen after `OnStartup` completes, but worth being aware of.
- Wails may expose lifecycle methods (`SetContext`, `SetPlaybackFinishedHandler`, `InitSpeaker`) as callable bindings. Same non-issue as queue — these are harmless in generated JS.

## Net Effect

- **~120 lines removed** from `player.go` (event handlers + boilerplate)
- **~8 lines removed** from event constants (Go + TS)
- **~8 lines changed** in `player-store.ts` (swap `EventsEmit` for binding calls)
- **~10 lines changed** in `app.go` (move construction)
- After this change, **zero** `EventsEmit` calls remain in the frontend for backend requests — all frontend→backend communication uses Wails bindings

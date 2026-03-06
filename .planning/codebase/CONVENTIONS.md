# Coding Conventions

**Analysis Date:** 2026-02-26

## Go Code Style

### Package Documentation

Every package begins with a doc comment ending with a period. Use `// Package <name> <description>.` format:

```go
// Package player provides audio playback functionality.
package player

// Package queue manages the playback queue and auto-advance logic.
package queue

// Package events contains centralized event name constants for
// Wails frontend/backend communication. These names must match
// the corresponding event names in the TypeScript frontend.
package events
```

Enforced by `godot` linter. Multi-line doc comments are acceptable:

```go
// Package profiling provides dev-only performance profiling via pprof and runtime/trace.
//
// In dev builds (build tag "dev"), Start launches an HTTP server on localhost:6060...
package profiling
```

### Import Organization

Three groups separated by blank lines, enforced by `gci` formatter:
1. **Standard library** (e.g., `context`, `fmt`, `log/slog`)
2. **Third-party** (e.g., `github.com/...`)
3. **Internal** (prefix `yellowjacket/...`)

```go
import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    "sync"

    "github.com/gopxl/beep/v2"
    "github.com/wailsapp/wails/v2/pkg/runtime"

    "yellowjacket/backend/database"
    "yellowjacket/backend/events"
    "yellowjacket/backend/metadata"
)
```

Use import aliases sparingly and only when needed to resolve conflicts:

```go
import (
    wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
    goruntime "runtime"
)
```

Blank identifier imports for side effects include a comment:

```go
import (
    _ "modernc.org/sqlite" // Register sqlite driver.
)
```

### Error Handling

**Wrap errors with context** using `fmt.Errorf` and `%w`:

```go
return fmt.Errorf("failed to open file: %w", err)
return fmt.Errorf("could not connect to sqlite database: %w", err)
```

**Define sentinel errors as package-level vars** (enforced by `err113`). Never use `errors.New()` inline in return statements:

```go
// Exported sentinels for external consumers:
var ErrUnsupportedFileType = errors.New("unsupported file type")

// Unexported sentinels for internal use:
var (
    errNoControlStreamer = errors.New("no control streamer")
    errNoAudioFileLoaded = errors.New("no audio file loaded")
    errNoStreamerToPlay  = errors.New("no streamer to play")
    errLibraryDirNotConfigured = errors.New("library directory not configured")
)
```

**Use `errors.Join()`** for accumulating multiple non-fatal errors:

```go
var batchErr error
for _, result := range batch {
    if saveErr := l.saveAudioFile(...); saveErr != nil {
        batchErr = errors.Join(batchErr, saveErr)
    }
}
```

**Return early on errors** with a blank line after the early-return block (enforced by `nlreturn`):

```go
if err != nil {
    return fmt.Errorf("failed to open file: %w", err)
}

// continue with normal flow
```

## Naming Conventions

### Exported vs Unexported

- **Structs/types**: `PascalCase` for exported, `camelCase` for unexported
- **Functions/methods**: `PascalCase` for exported, `camelCase` for unexported
- **Constants**: `PascalCase` for exported, `camelCase` for unexported
- **Variables**: `PascalCase` for exported, `camelCase` for unexported

### Custom Domain Types

Use typed aliases for domain-specific values rather than raw primitives:

```go
// backend/player/volume.go
type UserVolume int
type Volume float64

// backend/player/player.go
type State string

// backend/metadata/metadata.go
type AudioFileExtension string

// backend/queue/queue.go
type RepeatMode string

// backend/library/config.go
type Directory string
type ScanConcurrency string
```

### No Stuttering (enforced by `revive`)

Exported types must not repeat the package name. Consumers write `queue.Track`, not `queue.QueueTrack`:

```go
// Good — in package queue:
type Track struct { ... }
type State struct { ... }

// Bad — would stutter:
type QueueTrack struct { ... }
type QueueState struct { ... }
```

### Constants

Group related constants with `const (...)`:

```go
const (
    Playing State = "playing"
    Paused  State = "paused"
    Stopped State = "stopped"
)

const (
    MinUserVol     UserVolume = 0
    MaxUserVol     UserVolume = 100
    DefaultUserVol UserVolume = 50
)
```

### JSON Tags

Use `camelCase` JSON tags on exported struct fields for frontend serialization:

```go
type TrackInfo struct {
    FileName       string `json:"fileName"`
    FilePath       string `json:"filePath"`
    State          State  `json:"state"`
    TrackLength    int    `json:"trackLength"`
    TrackChangeID  uint64 `json:"trackChangeId"`
}
```

## Constructor Pattern

Use `New*` constructors with dependency injection. Accept `*slog.Logger` and scope it with `logger.WithGroup()`:

```go
// backend/queue/queue.go
func NewQueue(logger *slog.Logger, db *database.DB) *Queue {
    return &Queue{
        logger:     logger.WithGroup("queue"),
        db:         db,
        repeatMode: RepeatOff,
    }
}

// backend/player/player.go
func NewPlayer(logger *slog.Logger, db *database.DB) *Player {
    return &Player{
        logger:       logger,
        db:           db,
        state:        Stopped,
        baseStreamer: generators.Silence(-1),
        format: beep.Format{
            SampleRate: speakerSampleRate,
        },
    }
}

// backend/database/database.go
func NewDB(logger *slog.Logger) (*DB, error) {
    // ...
    return &DB{
        db:      db,
        Ctx:     dbCtx,
        Queries: queries,
        logger:  logger,
    }, err
}
```

Logger scoping with `.WithGroup()` or `.With()`:

```go
logger.WithGroup("queue")
logger.WithGroup("player")
logger.WithGroup("config").With("config", conf)
```

## SetContext Pattern (Two-Phase Initialization)

Components needing the Wails runtime use two phases because the runtime is unavailable until `OnStartup`:

1. **Phase 1**: `New*()` constructor — created before `wails.Run` for binding registration
2. **Phase 2**: `SetContext(ctx context.Context)` — called after runtime starts; registers event handlers, restores state

```go
// Phase 1: in NewYellowJacketApp()
yjApp.player = player.NewPlayer(yjApp.logger.WithGroup("player"), yjApp.database)
yjApp.queue = queue.NewQueue(yjApp.logger, yjApp.database)

// Phase 2: in OnStartup()
yj.player.SetContext(ctx)
yj.queue.SetContext(ctx)
yj.library.SetContext(ctx)
yj.appConfig.SetContext(ctx)
```

SetContext implementations vary by component:

```go
// backend/player/player.go — restores persisted state
func (p *Player) SetContext(ctx context.Context) {
    p.mu.Lock()
    p.ctx = ctx
    p.mu.Unlock()

    p.mu.Lock()
    p.restoreStateLocked()
    p.mu.Unlock()
}

// backend/queue/queue.go — simple context assignment
func (q *Queue) SetContext(ctx context.Context) {
    q.ctx = ctx
}

// backend/library/library.go — registers event handlers
func (l *Library) SetContext(ctx context.Context) {
    l.ctx = ctx
    l.registerEventHandlers()
}
```

## Logging Conventions

Use `log/slog` with structured key-value pairs. Logger injected via constructors and scoped with `WithGroup`:

```go
// Info-level with structured data:
p.logger.Info("File loaded, state set to paused", "file", filePath)
p.logger.Info("Player state saved",
    "volume", volume,
    "muted", muted,
    "trackPath", trackPath,
    "positionSeconds", positionSeconds,
)

// Error-level:
p.logger.Error("Failed to decode", "path", filePath, "err", err)

// Warning-level:
p.logger.Warn("failed to close previous audio file", "err", closeErr)

// Debug-level:
p.logger.Debug("attempting to seek",
    "target-seconds", targetSeconds,
    "song-length", lengthSecs,
    "samples", samples,
)
```

**sloglint enforces**: consistent key-value pair formatting. Always use string keys and structured values.

### Operation Timing

Use `profiling.TimeOp` (dev-only, no-op in production) with defer:

```go
defer profiling.TimeOp(p.logger, "player.LoadFile")()
defer profiling.TimeOp(logger, "database.NewDB")()
defer profiling.TimeOp(q.logger, "queue.SetQueue")()
```

## Comment & Documentation Requirements

### Doc Comments (enforced by `godot`)

All doc comments on exported types and functions must end with a period:

```go
// Player handles audio playback and state management.
type Player struct { ... }

// NewPlayer creates a player. Call InitSpeaker separately to
// initialize the audio output device.
func NewPlayer(logger *slog.Logger, db *database.DB) *Player {

// SetVolume sets the playback volume (0-100), emits a
// VolumeChanged event, and persists the new level.
func (p *Player) SetVolume(desiredVolume UserVolume) {
```

### Section Comments

Use separator comments to organize large files into logical sections:

```go
// ---------------------------------------------------------------
// Emit helpers (must be called with p.mu held)
// ---------------------------------------------------------------

// ---------------------------------------------------------------
// Streamer management (must be called with p.mu held)
// ---------------------------------------------------------------

// ---------------------------------------------------------------
// LoadFile
// ---------------------------------------------------------------
```

### Internal Implementation Comments

Unexported functions get concise comments explaining purpose and lock requirements:

```go
// saveState is the internal helper that writes the current player
// state to the database. Must be called with p.mu held.
func (p *Player) saveState() {
```

## Linting Rules

### golangci-lint v2 Configuration

Config: `.golangci.yml` — version 2 format with `default: standard`.

**Enabled linters:**
- `gocritic` — common Go pitfalls
- `errorlint` — proper error wrapping with `%w`
- `err113` — sentinel errors must be package-level vars
- `godot` — doc comments end with periods
- `revive` — Go best practices (no stuttering, etc.)
- `sloglint` — consistent slog usage
- `nlreturn` — blank line after early returns
- `wsl` — whitespace linting (cuddled declarations)
- `perfsprint` — prefer `strconv` over `fmt.Sprintf` for simple conversions
- `misspell` — spelling in comments
- `nakedret` — no naked returns in long functions
- `dupword` — duplicated words in comments
- `whitespace` — trailing whitespace
- `usetesting` — prefer `t.Context()` and `t.TempDir()`

**Enabled formatters:**
- `gci` — import ordering (stdlib → third-party → `yellowjacket/`)
- `gofmt`, `gofumpt` — standard formatting
- `goimports` — import management
- `golines` — line length (keep under 100 characters)

### Common Linting Pitfalls

**Line length (`golines`)** — Keep under 100 characters. Break long function calls:

```go
// Bad — over 100 characters:
q.logger.Warn("Current index out of range", "index", q.currentIndex, "trackCount", len(q.tracks))

// Good — broken across lines:
q.logger.Warn(
    "Current index out of range",
    "index", q.currentIndex,
    "trackCount", len(q.tracks),
)
```

**Blank line after early returns (`nlreturn`)** — An `if` block ending with `return`/`continue`/`break` must be followed by a blank line:

```go
if err != nil {
    return err
}

doNextThing()
```

**Cuddled declarations (`wsl`)** — `var` and `const` must be separated from preceding statements by a blank line:

```go
// Good:
wasEmpty := len(q.tracks) == 0

var newTracks []Track

// Bad:
wasEmpty := len(q.tracks) == 0
var newTracks []Track
```

**Sentinel errors (`err113`)** — Never use `errors.New(...)` or `fmt.Errorf("...")` inline in returns. Define package-level sentinels:

```go
var errNotFound = errors.New("not found")
```

**Doc comments (`godot`)** — End with a period:

```go
// Track represents a track in the queue with its metadata.
type Track struct { ... }
```

**Stuttering (`revive`)** — Don't repeat the package name in type names.

## Concurrency Patterns

### Mutex Usage

Use `sync.Mutex` with `Lock()/defer Unlock()` for public methods. Internal `*Locked` suffix functions assume lock is held:

```go
// Public method acquires lock:
func (p *Player) Play() error {
    p.mu.Lock()
    defer p.mu.Unlock()
    // ...
}

// Internal helper — caller must hold p.mu:
func (p *Player) loadFileLocked(filePath string) error {
    // no lock acquired here
}
```

Document lock ordering in struct comments:

```go
// Player handles audio playback and state management.
//
// Lock ordering: always acquire p.mu BEFORE speaker.Lock().
type Player struct {
    mu sync.Mutex
    // ...
}
```

### Atomic Counters

Use `atomic.Int64` for cross-goroutine counters that don't need mutex protection:

```go
var added, skipped, updated atomic.Int64
added.Add(1)
metrics.Added = added.Load()
```

## Build Tags

Dev/prod detection via `internal/dev/`:
- `internal/dev/devbuild.go`: `//go:build dev` → `IsDev = true`
- `internal/dev/nondevbuild.go`: `//go:build !dev` → `IsDev = false`

Package-level functions use this for conditional behavior (e.g., `profiling.TimeOp` is a no-op in prod builds).

---

## TypeScript/Lit Conventions

### Component Pattern

Use `@customElement` decorator with `LitElement` base class:

```typescript
@customElement('now-playing')
export class NowPlaying extends LitElement {
    // ReactiveControllers for store connection
    private player = new PlayerController(this);
    private favCtrl = new FavoritesController(this);

    // Component-local reactive state
    @state()
    private isDragging = false;

    // Static styles (override keyword required)
    static override styles = css`
        :host { display: block; }
    `;

    // Lifecycle (override keyword required)
    override connectedCallback() {
        super.connectedCallback();
        // setup
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        // cleanup
    }

    override render() {
        return html`...`;
    }

    // Private event handlers as arrow functions
    private handleMouseDown = (e: MouseEvent) => {
        e.preventDefault();
        this.isDragging = true;
    };

    private handleCoverMouseEnter = () => {
        // ...
    };
}

// Register in global element map
declare global {
    interface HTMLElementTagNameMap {
        'now-playing': NowPlaying;
    }
}
```

**Key rules:**
- `override` keyword required on all lifecycle methods (`noImplicitOverride: true`)
- Private event handlers as arrow functions (auto-bound `this`)
- `@state()` decorator for component-local reactive state
- `static override styles` for CSS-in-JS with `css` tag

### Store Pattern (Singleton + ReactiveController)

Backend is source of truth. Frontend stores cache backend state via Wails events.

**Store** (`frontend/src/store/player-store.ts`):

```typescript
class PlayerStore {
    private state: PlayerState = { isPlaying: false, currentTrack: null, volume: 50 };
    private subscribers = new Set<Subscriber>();

    constructor() {
        this.initializeEventListeners();
    }

    private initializeEventListeners(): void {
        EventsOn(Events.PlaybackStateChanged, (data: { state: string }) => {
            this.update({ isPlaying: data.state === 'playing' });
        });
    }

    getState(): Readonly<PlayerState> { return this.state; }
    subscribe(callback: Subscriber): () => void { ... }
    private update(partial: Partial<PlayerState>): void { ... }
    private notify(): void { ... }
}

// Singleton instance
export const playerStore = new PlayerStore();
```

**Controller** (`frontend/src/store/controllers/player-controller.ts`):

```typescript
export class PlayerController implements ReactiveController {
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    hostConnected(): void {
        this.unsubscribe = playerStore.subscribe(() => {
            this.host.requestUpdate();
        });
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    // Convenience getters
    get isPlaying(): boolean { return this.state.isPlaying; }
    get currentTrack(): TrackInfo | null { return this.state.currentTrack; }
}
```

### Import Organization

Use path aliases from `frontend/tsconfig.json`. Use `import type` for type-only imports (`verbatimModuleSyntax`):

```typescript
// Third-party
import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';

// Runtime/generated bindings
import { EventsOn, EventsEmit } from '@runtime/runtime';
import * as Player from '@go/player/Player';

// Internal stores/controllers
import type { TrackInfo } from '@store/player-store';
import { PlayerController } from '@store/controllers/player-controller';

// Components
import '@components/audio-player/audio-player';
```

**Available aliases:**
- `@go/*` → `./wailsjs/go/*` (Wails-generated Go bindings)
- `@components/*` → `./src/components/*`
- `@store/*` → `./src/store/*`
- `@runtime/*` → `./wailsjs/runtime/*` (Wails runtime)
- `@utils/*` → `./src/utils/*`
- `@assets/*` → `./src/assets/*`
- `@pages/*` → `./src/pages/*`

### TypeScript Strictness

Configured in `frontend/tsconfig.json`:

- `strict: true` — all strict checks
- `noUncheckedIndexedAccess: true` — array/object index checks
- `noImplicitOverride: true` — require `override` keyword
- `verbatimModuleSyntax: true` — require `import type`
- `noUnusedLocals: true`, `noUnusedParameters: true`
- `noImplicitReturns: true`
- `noFallthroughCasesInSwitch: true`
- `experimentalDecorators: true` — for Lit decorators
- `useDefineForClassFields: false` — for Lit property definitions
- Plugins: `ts-lit-plugin`, `typescript-lit-html-plugin`

### Event System

Events bridge Go backend and TypeScript frontend. Names must match **exactly** in both files:

- Go: `backend/events/events.go`
- TypeScript: `frontend/src/events.ts`

```go
// Go constants
const (
    PlaybackStateChanged = "PlaybackStateChanged"
    TrackChanged         = "TrackChanged"
    QueueChanged         = "QueueChanged"
)
```

```typescript
// TypeScript constants (as const object)
export const Events = {
    PlaybackStateChanged: "PlaybackStateChanged",
    TrackChanged: "TrackChanged",
    QueueChanged: "QueueChanged",
} as const;

export type EventName = (typeof Events)[keyof typeof Events];
```

### Store Barrel File

`frontend/src/store/index.ts` re-exports stores and types:

```typescript
export { playerStore } from './player-store';
export type { PlayerState, TrackInfo } from './player-store';
export { PlayerController } from './controllers/player-controller';
```

---

*Convention analysis: 2026-02-26*

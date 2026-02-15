# AGENTS.md - YellowJacket

Guidelines for AI coding agents working in this repository.

## Project Overview

YellowJacket is a cross-platform desktop music player built with:
- **Backend**: Go 1.25 with Wails v2 framework
- **Frontend**: TypeScript with Lit Web Components
- **Database**: SQLite (pure-Go driver via `modernc.org/sqlite`)
- **Build Tools**: Make, Wails CLI, Vite, pnpm

## Build Commands

```bash
make dev          # Development with hot-reload
make build-dev    # Debug build
make build-prod   # Production build (obfuscated + UPX compressed)
make generate     # Run all code generators (sqlc, templ)
make clean        # Clean frontend build artifacts
make lint         # Run golangci-lint
make test         # Run all Go tests (race detector, no cache, 2min timeout)
```

### Frontend Only
```bash
cd frontend && pnpm install   # Install dependencies
cd frontend && pnpm dev       # Vite dev server
cd frontend && pnpm build     # Production build
```

## Testing

**Important**: Tests require the `-tags webkit2_41` build tag.

```bash
make test                                                              # All tests (preferred)
go test -tags webkit2_41 ./...                                         # All tests manually
go test -tags webkit2_41 ./backend/player/                             # Single package
go test -tags webkit2_41 -run TestFunctionName ./backend/player/       # Single test
go test -tags webkit2_41 -v -run TestFunctionName ./backend/player/    # Verbose single test
```

Test files are colocated with source as `*_test.go`. Test fixtures live in `test_data/`. Some tests skip in CI when they require hardware (audio device, Wails runtime).

## Linting

golangci-lint v2 config (`.golangci.yml`) with strict rules. Key linters:
- `gocritic`, `errorlint`, `err113`, `godot`, `revive`, `sloglint`, `nlreturn`, `wsl`
- Formatters: `gci`, `gofmt`, `gofumpt`, `goimports`, `golines`

```bash
make lint                                                # Lint all Go code
golangci-lint run --build-tags webkit2_41 ./...           # With build tags explicitly
```

Frontend type checking: `cd frontend && pnpm exec tsc --noEmit`

### Avoiding Common Linting Errors

Always run `make lint` before considering a task complete. Below are the most common linting violations and how to avoid them.

**Line length (`golines`)**: Keep lines under 100 characters. Break long function calls, especially `slog` calls, across multiple lines:
```go
// Bad — over 100 characters:
q.logger.Warn("Current index out of range", "index", q.currentIndex, "trackCount", len(q.tracks))

// Good — broken across lines:
q.logger.Warn(
    "Current index out of range",
    "index", q.currentIndex, "trackCount", len(q.tracks),
)
```

**Stuttering type names (`revive`)**: Exported types must not repeat the package name. Consumers would write `queue.Track`, not `queue.QueueTrack`:
```go
// Bad — stutters as queue.QueueTrack:
type QueueTrack struct { ... }

// Good:
type Track struct { ... }
```

**Cuddled declarations (`wsl`)**: `var` and `const` declarations must be separated from the preceding statement by a blank line:
```go
// Bad:
wasEmpty := len(q.tracks) == 0
var newTracks []Track

// Good:
wasEmpty := len(q.tracks) == 0

var newTracks []Track
```

**Blank line after early returns (`nlreturn`)**: An `if` block that ends with `return`, `continue`, or `break` must be followed by a blank line:
```go
if err != nil {
    return err
}

doNextThing()
```

**Error sentinels (`err113`)**: Never use `errors.New(...)` or `fmt.Errorf("...")` inline in return statements. Define package-level sentinel errors instead:
```go
var errNotFound = errors.New("not found")
```

**Doc comments (`godot`)**: All doc comments on exported types and functions must end with a period:
```go
// Track represents a track in the queue with its metadata.
type Track struct { ... }
```

**Import order (`gci`)**: Three groups separated by blank lines — stdlib, third-party, internal (`yellowjacket/...`). Let the formatter handle this, but be aware of the expected grouping.

## Code Generation

`go:generate` directives live in `backend/app.go` (templ) and `backend/database/database.go` (sqlc). After modifying `.templ` files or SQL in `backend/database/sql/`, run `make generate`. **Never edit files in `backend/database/sql/sqlcgen/` or `*_templ.go` — they are generated.**

## Go Code Style

### Package Documentation
Every package must have a doc comment ending with a period:
```go
// Package player provides audio playback functionality.
package player
```

### Import Organization
Three groups separated by blank lines (enforced by `gci`): stdlib, third-party, internal.
```go
import (
    "context"
    "fmt"

    "github.com/wailsapp/wails/v2/pkg/runtime"

    "yellowjacket/backend/events"
)
```

### Error Handling
- Wrap errors with context: `fmt.Errorf("failed to open file: %w", err)`
- Sentinel errors as package-level vars (enforced by `err113`):
  ```go
  var ErrUnsupportedFileType = errors.New("unsupported file type")
  ```
- Unexported sentinels for internal use: `var errNotDirectory = errors.New("not a directory")`
- Use `errors.Join()` for accumulating multiple errors
- Return early on errors; blank line required after early returns (`nlreturn`)

### Naming Conventions
- Structs/exported: `PascalCase` — Unexported: `camelCase`
- Constants: `PascalCase` for exported, grouped with `const (...)`
- Custom domain types: `type PlayerState string`, `type UserVolume int`, `type AudioFileExtension string`

### Logging
`log/slog` with structured key-value pairs. Logger injected via constructors, scoped with `logger.WithGroup("player")`:
```go
p.logger.Info("File loaded", "file", filePath)
p.logger.Error("Failed to decode", "path", filePath, "err", err)
```

### Comments & Formatting
- Doc comments on all exported functions/types, ending with periods (enforced by `godot`)
- Blank line after early returns (enforced by `nlreturn`)

### Constructor Pattern
```go
func NewPlayer(ctx context.Context, logger *slog.Logger, db *database.DB) (*Player, error) {
    player := &Player{ctx: ctx, logger: logger.WithGroup("player"), state: Stopped}
    return player, nil
}
```

### SetContext Pattern (Two-Phase Initialization)
Components needing Wails runtime use two phases (runtime unavailable until `OnStartup`):
1. `New*()` constructor — created before Wails runtime is available
2. `SetContext(ctx context.Context)` — called after runtime starts; registers event handlers, restores state

### Build Tags
Dev/prod detection via `internal/dev/`: `//go:build dev` → `IsDev = true`, `//go:build !dev` → `IsDev = false`.

## TypeScript/Lit Code Style

### Import Organization
Use path aliases from `tsconfig.json`. Use `import type` for type-only imports (`verbatimModuleSyntax`).
```typescript
import { EventsOn, EventsEmit } from '@runtime/runtime';
import type { TrackInfo } from '@store/player-store';
```
Aliases: `@go/*`, `@components/*`, `@store/*`, `@runtime/*`, `@utils/*`, `@assets/*`, `@pages/*`

### Lit Component Pattern
```typescript
@customElement('component-name')
export class ComponentName extends LitElement {
  @state() private someState: Type = initialValue;
  static override styles = css`...`;
  override connectedCallback() { super.connectedCallback(); }
  override disconnectedCallback() { super.disconnectedCallback(); }
  override render() { return html`...`; }
}
```
- `override` keyword required (`noImplicitOverride: true`)
- Private event handlers as arrow functions: `private handleClick = () => { ... }`
- `strict: true`, `noUncheckedIndexedAccess: true`, `verbatimModuleSyntax: true`, `experimentalDecorators: true`, `noUnusedLocals: true`, `noUnusedParameters: true`
- Singleton stores in `frontend/src/store/` (backend is source of truth). `ReactiveController` pattern connects Lit components to stores — subscribe in `hostConnected()`, unsubscribe in `hostDisconnected()`.

## Frontend-Backend Communication

### Event System
Events are the primary communication mechanism. **Event names must match exactly** in both files:
- Go: `backend/events/events.go` — TypeScript: `frontend/src/events.ts`

```go
runtime.EventsEmit(p.ctx, events.TrackChanged, trackInfo)
runtime.EventsOn(p.ctx, events.RequestPlay, func(_ ...any) { p.Play() })
```
```typescript
EventsEmit(Events.RequestPlay);
EventsOn(Events.TrackChanged, (trackInfo: TrackInfo) => { ... });
```

### HTMX
The config page uses HTMX for HTML fragment loading. Backend serves fragments via templ templates (`backend/config/config-form.templ`, `backend/library/config.templ`). Config has a separate entry point (`src/pages/config/`).

## Database

SQLite with sqlc for type-safe queries. Schemas in `backend/database/sql/schemas/`, queries in `backend/database/sql/queries/`, generated code in `backend/database/sql/sqlcgen/`. SQLite opened with WAL mode and `SetMaxOpenConns(1)` (single-writer). After modifying SQL files, run `make generate`.

## Directory Structure

- `backend/` — Go: `config/`, `database/`, `events/`, `library/`, `metadata/`, `models/`, `player/`, `queue/`, `system/`, `logging/`, `frontendutil/`, `assets/`
- `frontend/src/` — TypeScript/Lit: `components/`, `pages/`, `store/`, `utils/`
- `frontend/wailsjs/` — Auto-generated Wails bindings (do not edit)
- `internal/dev/` — Build-tag-based dev/prod detection
- `pkg/templcomp/` — Shared templ component utilities
- `test_data/` — Audio test fixtures

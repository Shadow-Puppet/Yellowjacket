# AGENTS.md - YellowJacket

Guidelines for AI coding agents working in this repository.

## Project Overview

YellowJacket is a cross-platform desktop music player built with:
- **Backend**: Go 1.24+ with Wails v2 framework
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
```

### Frontend Only
```bash
cd frontend
pnpm install    # Install dependencies
pnpm dev        # Vite dev server
pnpm build      # Production build
```

## Testing

```bash
go test ./...                                       # Run all tests
go test ./backend/player/                           # Run tests in a specific package
go test -run TestFunctionName ./backend/player/     # Run a single test by name
go test -v -run TestFunctionName ./backend/player/  # Verbose output
```

Test files are located alongside source files as `*_test.go`. Test fixtures live in `test_data/`.

## Linting

The project uses golangci-lint (v2 config) with strict rules. Key enabled linters:
- `gocritic`, `errorlint`, `err113`, `godot`, `revive`, `sloglint`, `nlreturn`, `wsl`
- Formatters: `gci`, `gofmt`, `gofumpt`, `goimports`, `golines`

```bash
golangci-lint run
```

## Code Generation

`go:generate` directives live in:
- `backend/app.go` — templ: generates `*_templ.go` from `.templ` files
- `backend/database/database.go` — sqlc: generates type-safe DB code from SQL

After modifying `.templ` files or SQL in `backend/database/sql/`, run `make generate`.

## Code Style Guidelines

### Go Code Style

#### Package Documentation
Every package must have a doc comment:
```go
// Package player provides audio playback functionality.
package player
```

#### Import Organization
Imports are grouped and ordered by gci/goimports (three groups separated by blank lines):
1. Standard library  2. Third-party packages  3. Internal packages (`yellowjacket/...`)

```go
import (
    "context"
    "fmt"
    "log/slog"

    "github.com/wailsapp/wails/v2/pkg/runtime"

    "yellowjacket/backend/events"
    "yellowjacket/backend/metadata"
)
```

#### Error Handling
- Always wrap errors with context: `fmt.Errorf("failed to open file: %w", err)`
- Define sentinel errors as package-level vars (enforced by `err113`):
  ```go
  var ErrUnsupportedFileType = errors.New("unsupported file type")
  ```
- Use `errors.Join()` for accumulating multiple errors
- Return early on errors; blank line required after early returns (`nlreturn`)

#### Naming Conventions
- Structs: `PascalCase` (e.g., `Player`, `AudioFile`)
- Exported methods: `PascalCase`
- Unexported methods/fields: `camelCase`
- Constants: `PascalCase` for exported, grouped with `const (...)`
- Custom domain types: `type PlayerState string`, `type UserVolume int`

#### Logging
Use `log/slog` with structured key-value pairs. Logger instances are injected via constructors:
```go
p.logger.Info("File loaded", "file", filePath)
p.logger.Error("Failed to decode", "path", filePath, "err", err)
```
Logger groups via `logger.WithGroup("player")` for component-scoped logging.

#### Constructor Pattern
```go
func NewPlayer(ctx context.Context, logger *slog.Logger, db *database.DB) (*Player, error) {
    player := &Player{ctx: ctx, logger: logger, state: Stopped}
    // initialization...
    return player, nil
}
```

#### SetContext Pattern (Two-Phase Initialization)
Backend components that need the Wails runtime use a two-phase pattern because Wails runtime features (events, dialogs) are unavailable until `OnStartup`:
1. Constructor (`New*`) — created before Wails runtime is available
2. `SetContext(ctx context.Context)` — called after Wails runtime starts; registers event handlers

#### Comments
- Doc comments on all exported functions/types
- End sentences with periods (enforced by `godot`)
- Blank line after early returns (enforced by `nlreturn`)

#### Build Tags
Dev/prod detection uses build tags in `internal/dev/`:
- `//go:build dev` → `IsDev = true` (used by `make dev`)
- `//go:build !dev` → `IsDev = false` (production builds)

### TypeScript/Lit Code Style

#### Import Organization
Use path aliases defined in `tsconfig.json`:
```typescript
import { EventsOn, EventsEmit } from '@runtime/runtime';
import type { TrackInfo } from '@store/player-store';
```
Available aliases: `@go/*`, `@components/*`, `@store/*`, `@runtime/*`, `@utils/*`, `@assets/*`, `@pages/*`

#### Lit Component Pattern
```typescript
@customElement('component-name')
export class ComponentName extends LitElement {
  @state() private someState: Type = initialValue;

  override connectedCallback() { super.connectedCallback(); }
  override render() { return html`...`; }
}
```

#### TypeScript Strictness
- `strict: true` enabled
- `noUncheckedIndexedAccess: true` — check array/object access
- `noImplicitOverride: true` — must use `override` keyword
- `verbatimModuleSyntax: true` — use `import type` for type-only imports
- `experimentalDecorators: true` — required for Lit decorators

## Frontend-Backend Communication

### Event System
Events are the primary communication mechanism. **Event names must match exactly** in both files:
- Go: `backend/events/events.go`
- TypeScript: `frontend/src/events.ts`

```go
runtime.EventsEmit(p.ctx, events.TrackChanged, trackInfo)
runtime.EventsOn(p.ctx, events.RequestPlay, func(_ ...any) { p.Play() })
```

```typescript
EventsEmit(Events.RequestPlay);
EventsOn(Events.TrackChanged, (trackInfo: TrackInfo) => { ... });
```

### State Management
- Singleton stores in `frontend/src/store/` — backend is source of truth
- ReactiveController pattern (`PlayerController`) connects Lit components to stores

### HTMX
The config page uses HTMX for HTML fragment loading. Backend serves HTML fragments via templ templates (`backend/config/config-form.templ`, `backend/library/config.templ`). Config is a separate entry point (`src/pages/config/`).

## Database

SQLite with sqlc for type-safe queries. Schemas in `backend/database/sql/schemas/`, queries in `backend/database/sql/queries/`, generated code in `backend/database/sql/sqlcgen/`. After modifying SQL files, run `make generate`.

## Directory Structure

- `backend/` — Go backend: `config/`, `database/`, `events/`, `library/`, `metadata/`, `player/`, `system/`
- `frontend/src/` — TypeScript/Lit: `components/`, `pages/`, `store/`, `utils/`
- `frontend/wailsjs/` — Generated Wails bindings
- `internal/dev/` — Build-tag-based dev/prod detection
- `pkg/templcomp/` — Shared templ component utilities
- `test_data/` — Audio test fixtures

## Key Dependencies

- **Wails v2**: Desktop app framework bridging Go and web frontend
- **beep**: Audio playback library (custom fork `TheCodeOfCaleb/beep`)
- **sqlc**: Type-safe SQL code generation
- **templ**: Go HTML templating
- **Lit**: Web component framework
- **Web Awesome**: Web component UI library (`@awesome.me/webawesome`)

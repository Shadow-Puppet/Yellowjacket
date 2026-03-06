# Technology Stack

**Analysis Date:** 2026-02-26

## Languages

**Primary:**
- Go 1.25 - Backend application logic, audio playback, database, system integrations
- TypeScript ~5.9 - Frontend UI with Lit Web Components

**Secondary:**
- SQL - SQLite schemas and queries (via sqlc code generation)
- HTML/CSS - Frontend layout and styling (Lit `css` tagged templates, `index.html`, `index.css`)
- Bash - Build/profiling scripts (`scripts/profile.sh`)

## Runtime

**Environment:**
- Wails v2 runtime (WebView2 on Windows, WebKitGTK on Linux, WKWebView on macOS)
- Linux builds require `webkit2_41` build tag (passed to all Go commands)

**Package Manager:**
- Go modules (`go.mod`) - lockfile: `go.sum`
- pnpm - Frontend package manager; lockfile: `frontend/pnpm-lock.yaml`

## Frameworks

**Core:**
- Wails v2 (`github.com/wailsapp/wails/v2` v2.10.2) - Desktop application framework bridging Go backend to WebView frontend
- Lit (`lit` ^3.2.1) - Web Component framework for the frontend UI
- Web Awesome (`@awesome.me/webawesome` ^3.2.1) - Icon library and component toolkit (icons via `<wa-icon>`)

**Testing:**
- Go standard `testing` package with `go test`
- Race detector enabled: `-race` flag

**Build/Dev:**
- Make - Build orchestration (`Makefile`)
- Wails CLI (`go tool wails`) - Dev server, production builds
- Vite (^7.0.0) - Frontend bundler with HMR
- golangci-lint v2 - Go linting and formatting

## Key Dependencies

### Go (Critical)

- `github.com/gopxl/beep/v2` v2.1.1 - Audio playback engine (MP3, FLAC, OGG, WAV decoding; speaker output; resampling; volume effects)
- `modernc.org/sqlite` v1.45.0 - Pure-Go SQLite driver (no CGo required)
- `github.com/wailsapp/wails/v2` v2.10.2 - Desktop app framework (Go ↔ JS bridge, event system, window management)
- `github.com/dhowden/tag` v0.0.0-20240417053706 - Audio metadata/tag extraction (ID3, Vorbis, FLAC tags)

### Go (Infrastructure)

- `github.com/BurntSushi/toml` v1.6.0 - TOML config file parsing/writing (`config.toml`)
- `github.com/godbus/dbus/v5` v5.1.0 - D-Bus integration for MPRIS2 media controls (Linux)
- `github.com/golang-cz/devslog` v0.0.15 - Pretty-printed structured logging for development
- `golang.org/x/sync` v0.19.0 - `errgroup` for concurrent library scanning
- `golang.org/x/image` v0.12.0 - Image processing for cover art thumbnail generation
- `golang.org/x/text` v0.34.0 - Unicode normalization for text processing
- `github.com/a-h/templ` v0.3.977 - Type-safe HTML templating (used for config page fragments)

### Go (Build Tools - declared in `tool` directive)

- `github.com/sqlc-dev/sqlc` - SQL-to-Go code generator
- `github.com/a-h/templ/cmd/templ` - Templ HTML template compiler
- `github.com/golangci/golangci-lint/v2/cmd/golangci-lint` - Linter
- `github.com/evilmartians/lefthook` - Git hooks manager
- `golang.org/x/vuln/cmd/govulncheck` - Vulnerability scanner
- `github.com/wailsapp/wails/v2/cmd/wails` - Wails CLI

### Frontend (npm)

- `lit` ^3.2.1 - Web Component framework (decorators, reactive properties, shadow DOM)
- `@awesome.me/webawesome` ^3.2.1 - Web component library (icons)
- `@lit-labs/signals` ^0.2.0 - Signal-based reactivity for Lit
- `@lit-labs/virtualizer` ^2.1.1 - Virtual scrolling for large lists
- `vite` ^7.0.0 - Build tool with HMR
- `typescript` ^5.9.3 - TypeScript compiler
- `ts-lit-plugin` ^2.0.2 - Lit template type checking
- `vite-plugin-static-copy` ^3.0.0 - Static asset copying during build
- `stylelint-config-standard` ^40.0.0 - CSS linting

## Configuration

**Application Config:**
- `config.toml` in user config directory (`~/.config/yellowjacket/config.toml` on Linux)
- TOML format, managed by `backend/config/config.go`
- Sections: `[Library]`, `[Theme]`, `[Window]`, `[TrackList]`, `[Favorites]`

**Build Configuration:**
- `wails.json` - Wails project configuration (app name, frontend commands)
- `frontend/vite.config.mts` - Vite bundler config with path aliases
- `frontend/tsconfig.json` - TypeScript config (strict mode, decorators, path aliases)
- `.golangci.yml` - golangci-lint v2 config (standard + extra linters, formatters)
- `backend/database/sqlc.yaml` - sqlc code generation config
- `lefthook.yml` - Git hooks (pre-commit: vet, lint, codegen-check, typecheck; pre-push: test, mod-verify, protect-main)

**TypeScript Path Aliases** (defined in both `tsconfig.json` and `vite.config.mts`):
- `@go/*` → `frontend/wailsjs/go/*` (Wails Go bindings)
- `@components/*` → `frontend/src/components/*`
- `@store/*` → `frontend/src/store/*`
- `@runtime/*` → `frontend/wailsjs/runtime/*` (Wails runtime JS)
- `@utils/*` → `frontend/src/utils/*`
- `@assets/*` → `frontend/src/assets/*`
- `@pages/*` → `frontend/src/pages/*`

**Environment:**
- No `.env` files detected - application is self-contained
- Dev/prod detection via Go build tags: `internal/dev/devbuild.go` (`//go:build dev`) and `internal/dev/nondevbuild.go` (`//go:build !dev`)

## Build System

**Development:**
```bash
make dev           # Full dev mode: install deps, generate, clean, wails dev with HMR
make lint          # golangci-lint v2 with all enabled linters
make test          # go test -tags webkit2_41 -race -count=1 -timeout 120s ./...
```

**Production:**
```bash
make build-prod    # wails build with -obfuscated -upx -ldflags "-s -w"
```

**Key Differences (Dev vs Prod):**
| Aspect | Development | Production |
|---|---|---|
| Build tag | `dev` (enables `IsDev = true`) | `!dev` (default, `IsDev = false`) |
| Log level | `slog.LevelDebug` | `slog.LevelInfo` |
| Profiling | pprof server on `localhost:6060`, block/mutex profiling enabled | No-op (zero overhead, code eliminated by compiler) |
| Binary | Uncompressed, debug symbols | Obfuscated + UPX compressed, stripped (`-s -w`) |
| Version | `dev` (default) | Set via `LDFLAGS` from git tag/commit |
| Frontend | Vite dev server with HMR | Embedded in binary via `//go:embed all:frontend/dist` |

**Code Generation:**
```bash
make generate      # Runs: go generate ./...
```
Triggers:
- `backend/app.go`: `//go:generate go tool templ generate` (compiles `.templ` → `*_templ.go`)
- `backend/database/database.go`: `//go:generate go tool sqlc generate` (compiles SQL → Go in `backend/database/sql/sqlcgen/`)

**Git Hooks (lefthook):**
- Pre-commit: `go vet`, `golangci-lint`, codegen freshness check, frontend TypeScript typecheck
- Pre-push: protect main branch, `go test`, `go mod verify`

## Platform Requirements

**Development:**
- Go 1.25+
- pnpm (for frontend package management)
- Linux: WebKitGTK development headers (webkit2gtk-4.1)
- All Go commands require `-tags webkit2_41` build tag

**Production (Linux):**
- WebKitGTK 4.1 runtime libraries
- D-Bus session bus (for MPRIS2 media controls)

**Cross-Platform Support:**
- Linux: Full support (MPRIS2 media controls via D-Bus)
- macOS/Windows: Supported via Wails; media controls use no-op stub (`backend/mediacontrols/stub.go`)
- User data paths: `~/.local/share/yellowjacket/` (Linux), `~/Library/Application Support/yellowjacket/` (macOS), `%LOCALAPPDATA%\yellowjacket\` (Windows)

---

*Stack analysis: 2026-02-26*

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

YellowJacket is a cross-platform desktop music player built with Go (backend) and TypeScript/Lit (frontend), using the Wails framework to bridge them. It supports MP3, FLAC, OGG Vorbis, and WAV playback.

## Planning

Active and historical plans live in `.planning/`:

- `.planning/NOTES.md` — gotchas, deferred items, open architecture questions, the "we already considered and rejected" list.
- `.planning/plans/active/` — work currently in progress (read first).
- `.planning/plans/pending/` — sequenced future work.
- `.planning/plans/completed/` — one concise recap per shipped milestone.

Numbering is sequential and stable across status moves (a plan keeps its `NNN-` prefix as it migrates between `pending → active → completed`). Abandoned plans are deleted; paused work stays in `pending/`.

## Commands

```bash
make dev              # Hot-reload development (installs deps, generates code, cleans frontend)
make dev-debug        # Same as dev but with YJ_LOG_LEVEL=debug
make build-dev        # Debug build with symbols
make build-prod       # Production build (stripped, UPX-compressed)
make generate         # Run code generators (sqlc + templ via go generate)
make lint             # golangci-lint v2 (strict), both build configurations
make test             # All tests with race detector, both build configurations
make vulncheck        # govulncheck for CVEs
make setup            # Install go tools, frontend deps, git hooks (lefthook)
```

### Running tests

All Go test commands require the `-tags webkit2_41` build tag:

```bash
go test -tags webkit2_41 ./...                           # All tests
go test -tags webkit2_41 ./backend/player/               # Single package
go test -tags webkit2_41 -run TestName ./backend/player/  # Single test
```

The central index builder is behind a second tag and is **not** covered
by the command above — `make test` runs both passes, but a manual run
needs it spelled out:

```bash
go test -tags "webkit2_41 indexbuild" ./backend/explore/... ./cmd/...
```

Audio playback integration tests require `YELLOWJACKET_INTEGRATION=1`.

## Architecture

**Wails app lifecycle** (`main.go` → `backend/app.go`): `YellowJacketApp` is the root struct bound to Wails. Its methods are callable from the frontend. Lifecycle hooks: `OnStartup` (init audio), `OnDomReady` (start library scan), `OnBeforeClose` (save window state), `OnShutdown` (persist player/queue state).

**Backend packages** (under `backend/`):
- `player` — Audio playback via beep. `BufferedStreamer` provides a ring buffer for smooth seeking.
- `queue` — Track queue with shuffle (Fisher-Yates), repeat modes, auto-advance, and session persistence.
- `library` — Concurrent library scanning, metadata extraction, cover art deduplication, incremental rescan.
- `database` — SQLite via pure-Go driver. Schema in `database/sql/schemas/`, queries in `database/sql/queries/`. **sqlc** generates Go code into `database/sql/sqlcgen/` — never edit that directory by hand.

  **Schema changes need two things, not one.** `sql/schemas/*.sql` is
  `CREATE ... IF NOT EXISTS` and is what sqlc reads — it's the single
  source of truth for "what the schema looks like right now", and it's
  what a fresh install gets verbatim. But it's a no-op against a
  database that already has the table, so an existing install needs a
  matching file in `sql/schemas/../migrations/` (e.g.
  `NNNN_description.sql`, `ALTER TABLE ... ADD COLUMN ...` /
  `CREATE INDEX ...`) to actually reach that shape. Both run on every
  open, migrations after schema files, tracked in `schema_migrations`
  so each applies once; a migration's `ALTER TABLE ADD COLUMN` failing
  with "duplicate column name" on an already-current database is
  expected and tolerated, not an error.

  A few things that bite if forgotten:
  - **Column order must match between the two paths.** `ALTER TABLE
    ADD COLUMN` always appends at the end, so a migrated column must
    also be declared *last* in the `CREATE TABLE` in `sql/schemas/`
    — otherwise a fresh install and an upgraded install disagree on
    column order, and a `SELECT *` query (sqlc binds those
    positionally) silently reads the wrong field on one of them. See
    `backend/database/migrations_test.go`'s
    `TestMigrations_ColumnOrderMatchesFreshInstall`, which is the
    regression test for exactly this.
  - **Don't put an index on a migrated column in `sql/schemas/`.**
    Schema files run before migrations, against a database that may
    not have that column yet — the index's predicate would fail
    (this is precisely the bug an earlier session shipped and a user
    hit at `make sandbox`). Declare it in the migration file instead,
    after the `ALTER TABLE` that adds the column.
  - This project **had** a 48-step migration chain before and tore it
    out (see `.planning/NOTES.md`, "No migration chain") because
    `sql/schemas/` had drifted from what the migrations actually
    produced and sqlc silently generated against the stale version.
    The design here avoids that by keeping `sql/schemas/` as the
    literal target shape (not a hand-maintained description of it)
    and letting migrations replay tolerantly against it — but the
    same drift is possible again if a schema change ships without
    updating both files. Don't reintroduce a *second* description of
    the schema anywhere else.
  - **Squashing is fine pre-1.0.** While this hasn't shipped to real
    users, periodically folding `sql/migrations/` into `sql/schemas/`
    and deleting the migration files (then wiping your own dev/sandbox
    DB) is a legitimate way to keep the migrations directory from
    accumulating dev-only churn — same effect as the old "just nuke
    it" workflow, opt-in instead of mandatory. Stop doing that once
    real user databases exist in the wild.
- `metadata` — Tag extraction (ID3v2, Vorbis Comments, FLAC).
- `config` — TOML-based settings. Settings page uses HTMX + templ for server-rendered HTML fragments.
- `playlist` / `smartplaylist` — Playlist CRUD and rule-based smart playlists.
- `mediacontrols` — MPRIS integration on Linux via D-Bus.
- `system` — OS-specific paths (XDG on Linux, `%LOCALAPPDATA%` on Windows).
- `explore` — Catalog search and browse over `explore_index`. See below.
- `profiling` — pprof server on `:6060`, compiled out in non-dev builds via build tags (`internal/dev/`).

**Explore catalog** (`backend/explore/`): the searchable MusicBrainz/
ListenBrainz catalog in `explore_index`. Deriving it from the MetaBrainz
dumps means streaming ~89 GB from a server that caps a client near
2 MB/s — half a day, for a catalog identical for every user. So that
work happens **once, centrally**, and users download the result:

- `cmd/indexbuild` builds the catalog from the dumps; `cmd/indexexport`
  cuts it down to a shippable core and stamps its provenance.
  `.gitea/workflows/index-artifact.yml` runs both and publishes the
  compressed artifact under a fixed `latest` version.
- The app fetches and merges that artifact (`artifactfetch.go`,
  `artifactimport.go`) — about a minute, versus a day.
- Everything the app does **not** need is behind the `indexbuild` build
  tag (`dumpimport.go`, `dumpcounts.go`, `dumpcatalog.go`,
  `dumpproject.go`, `dumpparallel.go`, `indexpatch.go`) so it is not
  linked into the binary. `dumpbuild_stub.go` is the app-side entry
  point; `dumpshared.go` holds what both sides use.
- The app keeps popularity current with the daily incremental dumps
  (`dumpincremental.go`), and resolves artists outside the artifact's
  coverage lazily on first view.

**Frontend** (`frontend/`): Lit 3.2 web components + Web Awesome UI library + HTMX. State management via singleton reactive stores in `src/store/`. Wails bindings auto-generated in `frontend/wailsjs/` — don't edit by hand.

**Event-driven communication**: Backend emits events via Wails runtime; frontend stores subscribe to them. Event names are constants in `backend/events/`.

## Code Generation

Two generators run via `go generate ./...` (or `make generate`):
1. **sqlc** — SQL → Go. Config at `backend/database/sqlc.yaml`. Add queries in `backend/database/sql/queries/`, get generated Go in `database/sql/sqlcgen/`.
2. **templ** — `.templ` files → `*_templ.go` files (same directory).

Pre-commit hooks verify generated code is fresh — always run `make generate` after changing `.sql` or `.templ` files.

## Code Style

- **Go**: golangci-lint v2 with strict linters including `err113` (static errors), `nlreturn`, `wsl_v5` (whitespace), `godot` (comment periods), `sloglint`, `perfsprint`. Imports grouped: stdlib → third-party → `yellowjacket/...` (enforced by gci).
- **TypeScript**: Strict mode, no implicit any, no unused locals/parameters.
- **Commits**: Conventional commits format (enforced by commitlint in CI). Semantic release uses these for versioning.

## Testing

Tests use `database.NewTestDB(t)` for in-memory SQLite, built by the same
`applySchema` production uses so the two cannot diverge. Test audio fixtures live in `test_data/music_library_test/`. Table-driven tests are the norm.

## Git Workflow

Feature branches and PRs are the norm, but direct pushes to `main` are allowed. Pre-commit runs vet, lint, codegen check, and frontend typecheck in parallel. Pre-push runs the full test suite.

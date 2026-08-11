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
make dev-headless     # Start headless in the background and return (SEED=<name> to seed)
make dev-stop         # Stop it (SIGTERM, so shutdown hooks run)
make dev-logs         # Tail .dev/app.log
make testdata         # Generate the deterministic fixture music library
make sandbox-seed NAME=<n>  # Build a seeded YJ_HOME by *running* the app
make build-dev        # Debug build with symbols
make build-prod       # Production build (stripped, UPX-compressed)
make generate         # Run code generators (sqlc + templ via go generate)
make e2e              # Playwright smoke suite against a running dev-headless app
make e2e-setup        # Install the e2e runner + its browser (once)
make ui-test          # Vitest component/store suite in a real browser (no app)
make ui-visual        # Same, including toMatchScreenshot comparisons
make ui-setup         # Install the Vitest provider's own Chromium (once)
make bindings-check   # Fail if frontend/wailsjs is stale vs the Go bindings
make skill-check      # Fail if .pi/ documents a make target that doesn't exist
make lint             # golangci-lint v2 (strict), all three build configurations
make test             # All tests with race detector, all three build configurations
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

`backend/testctl` is behind a third tag and needs its own pass too
(`make test` runs all three):

```bash
go test -tags "webkit2_41 dev" ./backend/testctl/...
```

Audio playback integration tests require `YELLOWJACKET_INTEGRATION=1`.

### Fixtures and the headless harness

`test_data/music_library_test/` is **generated, not committed**: run
`make testdata` (~1 s) before anything that needs audio. Tests reach it
through `internal/testfixtures`, selecting files by *case*
(`CaseCoverDedup`, `CaseUnicode`, `CaseDuplicates`, …) rather than by
path, and skip themselves when it has not been generated.

The app itself can be run without a blocking window — `make
dev-headless` — and driven with `playwright-cli` against the dev server
on `:34115`, which is the real app with real bindings on `window.go`,
bridged to the same Go backend a desktop window would use.

**The operational half of all this lives in the
`yellowjacket-dev` skill** (`.pi/skills/yellowjacket-dev/`): which tier
to reach for, the exact command sequences, seed lifecycle, and the
failure modes worth knowing before you meet them. It is deliberately
not repeated here — this section describes what exists, the skill says
what to run.

Two things ride on top of the headless launch, both from plan 005
phase 3:

- **The event bridge.** `.playwright/cli.config.json` loads
  `.playwright/init-events.js` as an `initScript`, which records every
  backend event on `window.__yjEvents`. Half this app is push-driven,
  so assertions **await an event, not a timeout**:
  `await window.__yjEvents.wait('LibraryScanComplete', {timeoutMs: 60000})`.
  It also provides `ready()` and `call('queue.Queue.GetState', [])`,
  which times out instead of hanging.
- **The dev-only control surface**, `backend/testctl`, mounted at
  `/__test/` on the same port: `health`, `db/snapshot`, `db/restore`,
  `emit` (force any backend event, which renders push-driven views
  without staging the work that would produce them) and `sql`. It is
  compiled out of non-dev builds and additionally requires
  `YJ_TESTCTL=1`, which `dev-headless.sh` sets and `make dev` does not.

Frozen regression specs live in `e2e/` (its own npm package, so the
Vitest browser mode does not share a package with the Playwright
runner): `make e2e` against an already-running app.

**The cheapest tier needs none of that.** `make ui-test` runs 313
Vitest tests in a real Chromium in ~2 s with no Wails, no backend, no
seeded library and no virtual display, because `frontend/wailsjs/` is a
pure passthrough to `window.go` / `window.runtime` and
`frontend/test/support/wails-fake.ts` replaces just those two globals —
so the tests exercise the real generated bindings and the real store
code.

**`frontend/wailsjs/` is generated by `wails`, not `go generate`**, so
the pre-commit codegen check does not cover it. `make bindings-check`
(~1.5 s, also a pre-commit hook) regenerates it and fails on a dirty
tree; `make bindings` regenerates it for real.

**Seeds are produced by running the app**, never by hand-writing a
`config.toml` and DB rows — the same discipline `sql/schemas/` gets,
for the same reason.

See `.planning/plans/active/005-agent-development-harness.md`.

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
- `home` — The home page's "start listening" shelves. Each shelf is a
  *reason* (what you played last, what you never played, a genre you
  have depth in) rather than a filter, and carries the sentence that
  says so. Its queries (`sql/queries/home.sql`) return album ids only
  and are joined back to `GetAllAlbumsWithDetails` in Go, so the album
  projection has one definition. A shelf with nothing behind it is
  omitted, never rendered empty.
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

Two cross-cutting pieces of that UI are worth knowing before touching
a list or a detail view:

- **`utils/explore-link.ts`** renders every track/album/artist name in
  the app. A name always navigates: to the MusicBrainz page when the
  entity is tagged, and to the *library* page for the same thing when
  it is not (`explore-album-details` and `explore-artist-details` both
  accept a local id instead of an MBID). It fires on a genuine single
  click only — the navigation is held for one double-click interval
  and dropped if a second click arrives, because the title is the
  widest thing in a row and double-clicking a row plays it. Rows do
  not need to know links exist.
- **`<catalog-scope-notice>`** is how a detail page admits what it is
  showing: catalog data (silent), a library stand-in while a fetch is
  in flight, library-only because the entity has no MBID, or a failed/
  empty catalog answer with a retry. Both detail views track
  `catalogPending`/`catalogLoaded` separately from their loading flags,
  since "something is renderable" and "this is the catalog's answer"
  are different questions.

**Event-driven communication**: Backend emits events via Wails runtime; frontend stores subscribe to them. Event names are constants in `backend/events/`.

Emit through **`events.Emit(ctx, name, data...)`**, never
`runtime.EventsEmit` — wails `log.Fatalf`s (unrecoverably) on any
context that does not carry its runtime, which includes every
`context.Background()`, so a direct call cannot run under test and can
kill the app from a background worker. `TestNoDirectRuntimeEmits` fails
the build on a direct call anywhere outside `backend/events`.

That wrapper is what makes services testable in-process: install a
recorder with `events.WithSink(ctx, rec)` and assert on the payload the
frontend would receive (`backend/queue/emit_test.go` is the model).
`events.Deliver` is the same call returning an error instead of
dropping, and has one legitimate caller — `/__test/emit`.

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

## CI

Four workflows in `.gitea/workflows/`. Three of them package and
publish (`arch-package`, `homebrew-formula`, `index-artifact`); only
`ci.yml` gates, and it is the one to look at when deciding whether a
push was healthy.

Two jobs, both in an `ubuntu:24.04` container:

- **`check`** — no display: `make lint` and `make test` (three build
  configurations each), `tsc --noEmit`, `make ui-test`,
  `make bindings-check`, `make skill-check`.
- **`e2e`** — under Xvfb and a private D-Bus: fixtures, a seed built by
  running the app, `make dev-headless`, then the Playwright suite
  against **both** Chromium and WebKit. Playwright's Linux WebKit links
  Ubuntu 24.04 libraries that Arch does not provide, so CI is the only
  place it can run, and it is the closest available approximation of
  the WebKit2GTK renderer that ships.

Two things the container needs that a developer machine does not. It
has no PulseAudio socket, so `/etc/asound.conf` makes ALSA's `null`
plugin the default device — that plugin advances its pointer on a
timer, so playback is consumed at real-time rate and the elapsed clock
moves, which `e2e/specs/playback.spec.ts` asserts. And
`YJ_CORE_INDEX_URL` points at a dead address so no run fetches the real
explore artifact, matching what `scripts/seed-sandbox.sh` already does.

**`make lint`'s tag sets must stay identical to `make test`'s.**
Without `webkit2_41` wails resolves `webkit2gtk-4.0`, which Arch still
ships and Ubuntu 24.04 does not — so a mismatch lints a configuration
that only builds on one developer's distro, and says nothing about what
ships.

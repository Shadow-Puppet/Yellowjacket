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
make bulkdata         # Generate the ~50k-track measurement library (BULK_TRACKS=)
make sandbox-seed NAME=<n>  # Build a seeded YJ_HOME by *running* the app
make sandbox-seed-bulk # Same, from the bulk library (minutes; it is a real scan)
make perf LABEL=<n>   # Measure a running app; writes .dev/perf/<n>.json
make perf-compare BEFORE=<a> AFTER=<b>  # Print the before/after table
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
make commit-check     # Fail if a commit subject is not a Conventional Commit
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

**A fifth tier exists for questions whose answer is a number**, not a
pass: `make bulkdata` generates a ~50 000-track library (11 s, 466 MB,
gitignored), `make sandbox-seed-bulk` seeds from it by running the app
like any other seed, and `make perf LABEL=x` measures startup, the
bundle's shape and each view's first open, keystroke cost, what a
finished track provokes, what one favourite toggle costs, what sitting
idle on Settings costs, and heap after a scripted browse. A measurement
that needs state the seed does not have stages it itself, idempotently,
so a before and an after see the same shape — the favourite number is
meaningless against the seed's one empty playlist, so it builds ten
500-track ones first.
It wraps every bound Go method, so "did that refetch the library" is a
fact rather than an inference. It is not a spec and does not run in CI.

**The cheapest tier needs none of that.** `make ui-test` runs 480
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

See `.planning/plans/completed/005-agent-development-harness.md`.

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
  - **A write wearing a query's shape still needs the writer.**
    `DB.QueryContext`/`QueryContextWith`/`QueryRow` route to a
    *query-only* read pool (a second `sql.DB` over the same file), so
    an `INSERT ... RETURNING` issued through one fails at runtime with
    "attempt to write a readonly database (8)" — which is exactly what
    `CreateSmartPlaylist` did, meaning no smart playlist could be
    created at all. Use `ExecContext`, or `QueryRowWriter` when the
    statement really does return a row. Nothing caught this because
    `NewTestDB` shares one in-memory connection and leaves `readDB`
    nil, so `reader()` returns the *writer* under test and the unit
    tests exercised a handle the app does not have.
    `TestNoWritesOnTheReadPool` walks the tree for it, in the same
    spirit as `TestNoDirectRuntimeEmits` and for the same reason — a
    lint pass only sees one build configuration.
  - **Squashing is fine pre-1.0.** While this hasn't shipped to real
    users, periodically folding `sql/migrations/` into `sql/schemas/`
    and deleting the migration files (then wiping your own dev/sandbox
    DB) is a legitimate way to keep the migrations directory from
    accumulating dev-only churn — same effect as the old "just nuke
    it" workflow, opt-in instead of mandatory. Stop doing that once
    real user databases exist in the wild.
- `metadata` — Tag extraction (ID3v2, Vorbis Comments, FLAC).
- `jobs` — The registry every long-running operation reports through:
  progress, pause/cancel, a global indicator and (for scans) a pause
  that survives a restart. Library scans, index builds, downloads and
  the autotag apply are registered; anything that is not registered has
  none of that, which is exactly how the three gaps the audit found
  came about.
- `config` — TOML-based settings. Settings page uses HTMX + templ for server-rendered HTML fragments.
- `playlist` / `smartplaylist` — Playlist CRUD and rule-based smart playlists.
- `mediacontrols` — MPRIS integration on Linux via D-Bus.
- `system` — OS-specific paths (XDG on Linux, `%LOCALAPPDATA%` on Windows).
- `explore` — Catalog search and browse over `explore_index`. See below.
  Its **shelves** (`shelves.go`) are the page Explore shows before
  anyone types, on `home`'s terms — a shelf is a reason, it carries the
  sentence that says so, and an empty one is omitted. Queries return
  `explore_index` row ids and are joined back by `rowsByIDs`, so a card
  has one definition (`artistFromIndex` / `releaseGroupFromIndex` /
  `recordingFromIndex`, shared with the search path, which is where
  they were inlined).

  Three things about it are load-bearing. **"No shelves" is three
  different statements here and the page says which** — Home can omit
  an empty shelf honestly, because a library with no history really has
  less to say, but Explore's data is a *downloaded artifact* that can
  be absent or still arriving, so `ShelfPage.State` is `ready`,
  `building` or `no-index` and the empty page names the missing catalog
  and points at Settings. **Whether there is a catalog is asked of the
  database, not of a flag**: `GetIndexStatus().TotalRows` is refreshed
  only between build tiers (0 beside a full catalog on an ordinary
  launch) and `IsReady()` is set once at startup (so rows staged by a
  spec afterwards are invisible) — both are the shape `emitStatus`
  warns about, and one `SELECT 1 … LIMIT 1` cannot be stale. And **two
  shelves with disjoint ids still repeat each other**: ordered by raw
  listen count the top albums are one act and its members and the
  artists row underneath was the same people, which `home`'s
  duplicate guard cannot see because the rows hold different entity
  types. Shelves are one album per artist and skip whoever a row above
  already showed. Found by reading a screenshot.

  Two of the four shelves the plan named **cannot be built**, and the
  schema decides that rather than the design: `explore_index` has no
  genre column to join a genre shelf to, and `similar_artist_map` is
  not in the shipped artifact and is filled lazily from the network, so
  a "similar artists" shelf is empty exactly when the page most needs
  content. The library-joining shelf reads `in_library`, which is set
  by MBID, so it is correctly absent on an untagged library — the
  fixture one included.
- `home` — The home page's "start listening" shelves. Each shelf is a
  *reason* (what you played last, what you never played, a genre you
  have depth in) rather than a filter, and carries the sentence that
  says so. Its queries (`sql/queries/home.sql`) return album ids only
  and are joined back to `GetAllAlbumsWithDetails` in Go, so the album
  projection has one definition. A shelf with nothing behind it is
  omitted, never rendered empty — **and so is a shelf that repeats the
  one above it**, which is the same rule one step further: "On repeat"
  was "Pick up where you left off" reordered, because a small library
  has one signal and answers several questions with the same albums.
  Two guards make that safe, and both were arrived at by breaking the
  existing tests: only shelves of three or more albums are judged (two
  rows of one overlap by 100% whenever they agree at all), and only
  when the shelf is **not showing the whole library** — a repeat is a
  fault only if a different row was possible. Measured against a fixed
  shelf size instead, an 11-album library kept three identical shelves
  while a 13-album one lost them.

  **The app lands here**, from `index.ts` after the stores are wired.
  `index.html` still renders the track list eagerly and it is still what
  paints first — it is the cached `tracks` view, so the navigation is a
  class toggle plus one chunk rather than a second render of the shell.
  `app-sidebar`'s default `activeView` is `home` to match, because the
  sidebar does not hear a `navigate` it did not send.
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

**A view is a chunk, and three components are not.** `index.ts` holds a
loader table (`VIEW_LOADERS`, `DETAIL_LOADERS`) and `await`s a view's
module before creating its element — `document.createElement` on an
undefined tag yields an inert `HTMLElement` rather than throwing, so a
missing entry is a blank page, not an error. Navigations are numbered
and anything after the `await` re-checks it is still the newest, or a
slow chunk lands on top of a faster navigation. Every chunk is then
warmed on idle, so the split is paid once at startup rather than on
every first visit. **`notification-host`, `inline-notice` and
`confirm-dialog` stay eager on purpose**: a failure surface that has to
fetch a chunk before it can speak is not a failure surface, and the
moment it is most needed is the likeliest moment loading one fails.
`first-run-wizard` and the startup chrome are eager for the ordinary
reason — they are the first paint.

**A primary view is cached, not unmounted.** `index.ts` keeps every
primary view in the DOM and toggles a `.view-hidden` class, because that
is what preserves `scrollTop` across navigation — so
`disconnectedCallback` never fires for one, and anything registered
there runs for the life of the session from pages it is not on. The
missing half is `utils/view-lifecycle.ts`: navigation calls
`viewDeactivated()` on the outgoing view and `viewActivated()` on the
incoming one, and a view registers its document listeners, timers and
backend subscriptions through `listenWhileActive` /
`intervalWhileActive` / `whileActive`, which are torn down on the way
out. An off-screen view also does not render. A shared reactive
controller gets the same treatment via `registerViewAware`.

**The player's position comes from the player.** `seek-bar` renders
`PlaybackPositionChanged` (payload `player.PositionInfo`), emitted at
1 Hz while playing and immediately on load, play, pause, seek and
natural finish. Its local `setInterval` is interpolation *between*
reports only, stopped and restarted by every one of them — it used to
be the clock, and counted itself 30 s away from the backend across four
keyboard seeks. A report carries `trackChangeId` (the store is a
singleton, so a bar mounting later must not adopt a report about the
previous track) and a `seq` (the same second reported twice still has
to reset the interpolation).

What the player cannot do, it says: `PlaybackFailed` is emitted from
both the load and the play path, auto-advance **skips** the failed
track (bounded by the queue length, so a disconnected drive stops after
one pass), and the bottom bar shows one coalescing line — "Skipped 12
tracks that could not be played." That line is the Inline level of the
app's one notification surface, below.

**Failure has one voice, and the caller picks how loud.**
`store/notification-store.ts` is the only notification surface; before
it, 84 `catch` blocks ended at `console.error` and two components had
grown private toasts. Four levels, chosen by the call site from one
rule — *a failure is only worth interrupting for if the user can do
something about it that they are not already doing*:

- **Blocking** (`wa-dialog`, must be acknowledged) for data at risk: a
  folder left holding a mix of old and new tags. Two callers are
  anticipated; a third should be argued for.
- **Persistent** (stays, with an action) for something the user asked
  for that did not happen and retrying is meaningful.
- **Transient** (a toast) for a small action whose state visibly
  reverted anyway — a favourite that came back.
- **Inline**, rendered by `<inline-notice region="…">` in the panel
  that failed, never as a toast.

Three things about it are load-bearing. **Coalescing lives in the
store**, keyed by `(level, region, key)` within a window, so 200
unplayable files are one message with a count and no future caller has
to remember that. **An inline notification carries a region**, because
"inline" says *not global*, not *where*. And **the bottom band belongs
to the player** — the app-level stack sits under the header, since the
player's own floating notice grows upward by however many lines it
needs and a bottom-anchored stack collides with it on a small window.

What reaches a person is a sentence: `utils/describe-error.ts` maps the
causes a user can act on (offline, timeout, not found, permission,
database busy) to copy, `explainError` repeats a backend message when it
is one of *our* sentinels rather than a Go wrapping chain, and the raw
text stays in `console.error`. The one documented exception is a
download client's connection test, whose verbatim error is the user's
debugging tool.

Destructive actions ask once, through `confirmAction()`
(`components/confirm-dialog/`), which is a `wa-dialog` and so brings the
focus trap and Escape the hand-rolled overlays do not have.

**Every dialog in the app is a `wa-dialog`, and there is no sixth
pattern.** The four hand-rolled autotag overlays and the remove-library
confirmation had no `role`, no `aria-modal`, no focus trap and no focus
restore — including the two gating an irreversible on-disk metadata
rewrite. The split is by *shape*, not by owner: a dialog that only asks
a question is a `confirmAction()` call (title, message, impact,
confirm/cancel), and a dialog carrying **input** is a `<wa-dialog>` in
the host's own template. Both remaining autotag dialogs render
unconditionally with `?open` deciding which is up — mounting one on
demand puts the element and its `showModal()` in the same update.
`autotag-view`'s last document keydown listener died with them; it
existed only because its dialogs could not close themselves.

**None of them had an accessible name, and one helper gives all of them
one.** Every call site passes `label`; Web Awesome renders it into an
`<h2 id="title">` in the same shadow root as the native `<dialog>` and
never points `aria-labelledby` at it — so for eleven dialogs
`getByRole('dialog', {name})` matched nothing and a screen reader
announced an unnamed dialog. `utils/name-dialog.ts` sets that IDREF
(and falls back to `aria-label` under `without-header`, which renders
no heading to point at), called from each host's `updated()`.

Three things about it are load-bearing. It **reaches into another
library's shadow root**, which is open but is not API — acceptable
here only because the failure is bounded: if Web Awesome moves the
structure the query misses, nothing is written, and the dialog is as
unnamed as it was. It uses **`aria-labelledby`, not `aria-label`**,
because three call sites compute their label at render time and an
IDREF to the heading Web Awesome re-renders stays correct with nothing
resyncing it. And it **waits for the dialog's own first update**, not
its host's: `wa-dialog` is a Lit element whose shadow root is populated
in *its* update, so a query at the host's `firstUpdated` finds an empty
root and names nothing — the same lifecycle trap that hid
`wa-dropdown-item`'s role from the menu keyboard model.

Two awkwardnesses remain, and they are about *locating* one rather than
naming it. The host is `display: contents`, so the element carrying the
testid always reports hidden — what is visible is the `<dialog>` inside
it — and what holds the slotted content is the *host's* shadow root,
not the dialog's subtree. A third is worth knowing before checking any
of this: the Playwright **a11y snapshot never prints a dialog's name**,
named or not, so it cannot tell you whether this works. `getByRole`
can, and CDP's `Accessibility.getFullAXTree` gives the browser's own
answer.

**A disclosure is a button, and it says what it controls.**
`config-section`'s header was a bare `<div @click>` with no `tabindex`,
no `role` and no `aria-expanded`, and every section defaults to
collapsed — so every setting in the app sat behind a control that could
not be tabbed to (the audit's last Critical). It is a
`<button aria-expanded aria-controls>` now, on the pattern
`explore-artist-details` has had five of all along, and **the body
renders unconditionally and is toggled with `hidden`**: `aria-controls`
has to name an element that is in the DOM, and a conditional `<slot>`
only stops projecting light-DOM children that exist either way.
Downloads' two tabs are the same fix one page over — `role=tablist` over
`role=tab`, one roving tab stop, Left/Right/Home/End, and a
`role=tabpanel` whose id and `aria-labelledby` swap with the tab.

**A menu has a keyboard model, and it is one model.**
`utils/context-menu-controller.ts` exports **`MenuKeyboard`** — focus
the first item on open, Arrow/Home/End to move (wrapping, as a menu
does and a listbox does not), Enter/Space to activate, Escape or Tab to
close, and focus back to the element it opened from. It is standalone
rather than part of `ContextMenuController` because `playlist-view`
renders a menu without that controller, and two menus with two keyboard
models is exactly what this is for. `isContextMenuKey()` is the
Shift+F10 / ContextMenu-key test, and `openFrom(el)` is the keyboard
open: anchored to the element, restoring focus to it.

Four things in it are load-bearing, and two of them are only visible
against the real components:

- **The items are not items yet when the host finishes updating.**
  `wa-dropdown-item` sets its `role` in its *own* first update, so a
  `[role^="menuitem"]` query at `updateComplete` finds nothing — which
  reads exactly like a menu that opened and refused to take focus.
- **`focus()` on a popup that has not positioned itself is a silent
  no-op**, so the first focus is retried across a few frames.
- **Focus is only taken back if the menu had it.** A click elsewhere
  closes the menu too, and pulling focus to the row the user
  right-clicked a moment ago is worse than leaving it.
- **Web Awesome keys an item's tabindex and highlight off `active`**, so
  moving focus without setting it leaves the highlight on whichever
  item the mouse last touched.

Three lists had no focused row to open a menu *from* — the queue panel
and both playlist detail views — and gained a roving tab stop through
`utils/roving-rows.ts`. **`track-list` deliberately does not use it**:
its equivalent predates this, carries selection semantics (shift-extend,
ctrl-toggle) the other three do not have, and is pinned by its own
tests.

**Async surfaces say what they are doing.** `styles/sr-only.css.ts`
carries the visually-hidden class and the rule that comes with it: a
live region must be **in the DOM before the text it announces is**,
because most screen readers announce a change to a region they are
already watching and ignore one that appears with its content already
in it. So these regions render unconditionally and empty, and only
their text changes. Four surfaces have one — the track list (loading,
failed, and how many rows a search matched), Explore's search,
`now-playing` (in **both** render branches, so it exists before the
first track arrives) and `job-indicator`, whose label swings between
"Scanning Music", "3 background jobs" and "Finished". The notification
surface already had one from Phase 3.

**A selectable grid is a listbox.** The four grids that ctrl/shift-select
(`artists-view`, `genres-view`, `cover-grid`, and the queue) are
`role="listbox" aria-multiselectable` over `role="option"` cards, not
rows of `role="button"`: `aria-selected` on a button is *invalid* and is
dropped outright, so the state the whole ctrl/shift interaction exists
to produce was invisible to anything but a sighted user. `track-list`'s
column headers carry `aria-sort` (Phase 1 added `role="columnheader"`
without it) and are activated by Enter/Space as well as by a click.

**One keyboard authority.** No component owns a document keydown
listener for its own shortcuts; it registers *panel-scoped* bindings
(`autotag.*`, `tracklist.*` in `backend/shortcuts/config.go`) and
claims a scope by setting `shortcutScope` on the mixin, which publishes
`data-shortcut-scope` and claims it as the ambient scope
(`services/shortcut-scope.ts`) while it is on screen. The shortcut
service resolves focus → panel → global, and yields a key to a focused
control that owns it (button/select/slider/checkbox, or anything inside
an open dialog) so the unmodified single-key global bindings do not
steal Space and the arrows.

**A list owns the arrows it moves on, which is the vertical ones.**
Granting a `row`/`option`/`grid` all six took `←`/`→` away from seeking
and gave them to nobody: `track-list`'s own handler and
`utils/roving-rows.ts` both take Up/Down/Home/End and ignore
Left/Right, so a focused track row produced zero `Player.Seek` calls
against one per press from the body. Grant them back in
`keysOwnedBy` if a list ever moves horizontally.

**The keys are written down in one place and shown in two.**
`services/shortcut-meta.ts` is the label, category, scope and default
for every action; `?` opens `<shortcuts-overlay>` and Settings edits
the same table. It used to be a private static in `config-page`, which
listed three of the four categories by hand — so the autotag bindings
were written down nowhere. **The overlay is not a toggle**: a dialog
owns every unmodified key while it is up, so a second `?` never reaches
the service; Escape closes it. And a shifted character does not report
Shift (`?`, not `Shift+?`) — the character already carries it.

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

**Icons are bundled, and the app works offline.** `src/icons/`
overrides Web Awesome's `default` icon library, whose resolver fetches
every `<wa-icon>` from `ka-f.fontawesome.com` at runtime — so the app
had no icons at all offline, and `setBasePath()` does not affect it
(only the component autoloader reads that). Overriding the library
fixes all 165 call sites without changing one of them. Three things
about it are load-bearing. The set is **Font Awesome Free** (CC BY 4.0,
vendored with its licence by `frontend/scripts/fetch-icons.mjs`)
because the kit CDN serves **Pro**, which cannot be redistributed. The
names are a committed list (`src/icons/names.txt`) rather than anything
derived, because twenty call sites compute their icon name from state
and no static pass can enumerate them. And a name that is not bundled
is therefore **reported at runtime** to `window.__yjIconMisses` and
drawn as a fallback — an e2e sweep asserts there are none — since a
missing icon used to be impossible, the CDN having had everything.

**An album page says how much of the album is yours.**
`explore-album-details` is a *catalog* page and there is no
library-side album detail page at all, so the album on it may be
wholly the user's, partly theirs, or not theirs — and its primary
action has to mean the same thing in each case. It says which: **Play**
when the whole release is owned, **Play 7 of 12** when some of it is,
and **no play button at all** when none is, because a Play button that
plays nothing (or seven tracks of forty) is worse than none.

`albumLibraryStatus()` is deliberately *not* what decides that. It is
four claims of decreasing confidence OR'd into one tick — a local album
id, the backend's cross-reference, a cached MBID match, and finally
*any single track* marked `inLibrary` — which is a fine answer to "is
any of this mine" and a useless basis for a button. `ownership()`
counts the displayed tracklist instead, whose `inLibrary` flags the
backend sets per recording MBID.

**And the key it plays by is not the key it looks owned by.** The local
album id is used wherever there is one, because a library-only album
has *no* recording MBIDs — its tracks are synthesised from
`GetAlbumTracks` with `mbid: RecordingMBID || ''` — so an MBID-keyed
lookup on an untagged library resolves to nothing and Play queues
nothing while looking entirely correct.
`GetFilePathsByRecordingMBIDs` is the catalog-only fallback and the
third member of the `GetFilePathsBy…` family: one query, paths only,
grouped so the caller keeps the tracklist's order. It exists rather
than a lookup by track id because **`MBTrack.LocalID` is declared and
nothing in the backend ever writes it**.

**Ask for what the caller uses, once.** "Play this artist" resolved
file paths with one `GetAlbumTracks` per album, sequentially, and every
one of the four sites doing that asked for whole track rows to read
`FilePath` off them — 5 genres cost 6 MB over the IPC (`perf.m2`).
`GetFilePathsByAlbums(ids, libraryID)` and `GetFilePathsByGenres(names,
libraryID)` answer in one query and carry only the paths. They return
the paths **grouped by album id / genre name**, because the caller owns
the order — an album list is sorted by name, not by id, and a flattened
result would silently reorder a queue — and because `cover-grid`'s drag
cache stores them per album. A `libraryID` of 0 means "every library",
matching an unset library filter.

**A badge is not a button, and a control that cannot act is worse than
none.** `library-status-indicator` — the tick/plus on every Explore
card and track row — was a `<button>` whose click handler was a
`stopPropagation()` and a comment saying to wire up the download client
later: 20 of the 66 tab stops on a results page announced themselves as
buttons and did nothing (46 and 0 after). It is `role="img"` with a
label until there is something to click, and its unowned label says
"… is not in your library" rather than "Add … to library", which was
the button's promise written into the copy. When the download client
lands, the change is a `<button>` *with* a handler — not a handler
bolted onto something already shaped like one. Two smaller things came
with it: a `<span>` does not get `box-sizing: border-box` from the UA
stylesheet the way a `<button>` does (the badge grew 36→38px, caught by
a stored screenshot), and with no click of its own the badge is part of
its card, so a click on it means what the card means.

**A grid moves by a row, and `offsetTop` cannot tell you how wide a row
is.** `utils/roving-grid.ts` measured columns by counting cards sharing
an `offsetTop`, and every card in these grids is positioned by
`lit-virtualizer` with a **transform**, which `offsetTop` does not see —
so all of them reported 0, every rendered card counted as one row, and
ArrowDown was `min(i + everything, last)` while ArrowUp was
`max(i - everything, 0)`. The vertical arrows were End and Home in the
albums, artists and genres grids alike, from the day it was written.
`getBoundingClientRect().top`, rounded, is the measurement. Two things
behind it are only visible once `cover-grid` splits: `scrollToIndex`
must pick the half that holds the index and rebase it (it was
`querySelector('lit-virtualizer')`, always the first), and the focus is
retried on a **time** budget rather than taken once at `updateComplete`
— a scroll of 5 000 rows produces the card a few hundred ms later, so
the tab stop moved and nothing took focus, which is indistinguishable
from the key not being handled.

**A view says what it is, in one component.** `<page-header>`
(`components/page-header/`) is title, count, sort and actions, and the
nine primary views use it rather than each writing its own — which is
how four came to have a heading and four not, and how the same sort
toolbar came to exist three times with different bugs. Three rules in
it are load-bearing. **An empty `heading` is a mode, not a missing
value**: `cover-grid` and `track-list` are also embedded in the artist
and genre pages, which have a heading already, so there they keep the
count and the sort and drop the title rather than growing a second
arrangement. **The header renders while the view loads** — a heading
that arrives with the data is the shifting layout it exists to stop —
and the *count* is `null` until there is an answer, because "0 albums"
that corrects itself a moment later is worse than saying nothing. And
**the header asks for a sort, it does not perform one**: the host owns
the field, the direction and their persistence, so the control cannot
disagree with the list.

**The header search box is view-scoped, and now says so.** It sits in
the app header and reads as global; typing `tide` on Playlists answered
"No playlists match your search" with three *Tideline* tracks in the
library. The scope was always real — the fix is that the placeholder
names it ("Search albums"), the page header repeats it ("Showing
artists matching ‘tide’"), and `search-store` holds the one map of
what each view searches. It also **keeps its slot everywhere and is
disabled** where it cannot serve, rather than being hidden: its
appearing and disappearing is what moved the library filter and the job
indicator on every navigation. On Explore, which has its own catalog
search, the disabled box points at it. **A view that filters on the
term belongs in that map**, detail views included —
`smart-playlist-details` narrowed its list as you typed under a
placeholder saying there was nothing to search here, because its
sibling was in the map and it was not.

**The window's minimum is measured, not aspirational.** `MinWidth`/
`MinHeight` are 800×600 because that is where the shell was checked to
still work: below ~780 the header subtitle wraps and pushes the title
out of the 4em bar, and below ~600 tall the eleven sidebar items stop
fitting at once. The old 512×384 promised a size at which Settings and
Jobs were unreachable — the sidebar clipped them with `overflow:
hidden` and nothing scrolled. The sidebar scrolls now, and collapses to
its (long-existing, previously drag-only) icon mode below 900px, which
is the same breakpoint that hides the subtitle.

**A row's columns must fit the row.** `track-list`'s
`computeDefaultWidths` shared out the host's whole `clientWidth` while
every row spends 24px on the favourite column and 2×8px on its own
padding first, so the grid was always exactly 40px too wide and the
last column was clipped at every size. Both numbers are constants read
by the three places that need them (the default widths, the
normaliser, and the resize handles' positions), because they were
written out separately and that is how they came to disagree.

**The default columns are declared twice and must agree.**
`tracklist.DefaultColumns` is what a fresh install persists;
`DEFAULT_COLUMN_IDS` in `track-list/columns.ts` is what the list draws
until the config arrives. Album is in both (a library manager with
duplicate detection whose rows are track/artist/duration cannot tell
its own duplicates apart) — and changing either is invisible against an
existing `YJ_HOME`, whose `config.toml` already holds the old list, so
`make sandbox-seed NAME=default` before believing the app.

**Event-driven communication**: Backend emits events via Wails runtime; frontend stores subscribe to them. Event names are constants in `backend/events/`.

`frontend/src/events.ts` is **generated** from `backend/events/events.go`
by `backend/events/cmd/genevents` — never edit it. It renders a const
block's doc comment as TypeScript line comments, every line of it: it
used to prefix only the first, so a comment that ran to a second
paragraph emitted bare prose into the object literal and `make generate`
— a pre-commit hook — produced a file that does not parse.

**An event's cost is part of its meaning.** `TrackMetadataChanged`
means *the tags on disk were rewritten*, which can change an album, an
artist or a genre — so `library-store` answers it by discarding every
cached collection and refetching. That makes it the most expensive
event in the app, and it must not be reused for something cheap:
finishing a track used to emit it, which cost ~37 MB across the IPC and
~0.8 s of blocked main thread *per song* at 50 000 tracks, and cleared
the user's track selection while it did. `TrackPlayCountChanged`
carries `{audioFileId, filePath, playCount, lastPlayed}` — deliberately
everything needed to patch one track in place, so no consumer has any
reason to invalidate anything. The store replaces the tracks array
(consumers key memoized filter/sort caches on its identity) while
sharing every unchanged Track, and `track-list` **retains** its
selection across a refetch rather than clearing it, since the keys are
file paths and those survive one.

The same rule reaches the other way: **an event carries what a consumer
needs so it never has to invalidate.** `PlaylistTracksChanged` carries
the playlist id, and `playlist-store` refetches *that* playlist
(`GetPlaylistTracks`, plus `GetAllPlaylists` for the summaries, since
`UpdatedAt` is a sort key) rather than `GetAllPlaylistsWithTracks`,
which is every row of every playlist — 2.6 MB and 172 ms for one heart
before the fix. It falls back to a full invalidate only where a patch
cannot be shown to be equivalent: no id (the bulk paths emit one), a
cold cache, an unknown id, or a fetch already in flight. And **a store
with no subscriber fetches nothing**: `playlist-view` is the only
reader and is created lazily, so neither the invalidation nor — more
expensively — the singleton's own construction warms a cache for a page
that may never open.

**An unchanged payload is not an event.** `explore`'s index status used
to be pushed on a 3 s ticker for the life of the process, byte-identical
once the index was ready, and `config-page` assigns it to a `@state`
field — so a user who had once opened Settings paid a full re-render of
a 2 000-line template every 3 s, forever, for no news. `emitStatus` now
drops a status equal to the last one it sent, which is the rule stated
once instead of at twenty call sites. The corollary is load-bearing:
**every mutation of something the status derives must call `emitStatus`
itself**, because there is no longer a poll to notice it. `si.ready`
and `si.cancel` are both derived, and both were relying on the ticker.

**A cache on a cached view needs a ceiling, and so does everything
else holding what it holds.** `explore-view` never unmounts, and its
two art caches were plain `Map`s: twenty-four searches retained
20.58 MB and were still accelerating, because a cover thumbnail is a
~27 kB base64 data URL and an artist photo is a ~128 kB one. They are
`LRUMap`s now (`utils/lru-map.ts` — a `Map` re-inserted on read and
trimmed from the front), capped from the measured size of an entry and
kept several times larger than a screenful, since a cap below the
visible count evicts art that is still rendered and the re-render
fetches it straight back.

Two things about it are load-bearing. **A cap is only a bound if it
covers every reference**: the artist photo's data URL is held by both
`artistImageCache` and `exploreCache.artists`, so capping either alone
frees nothing at all and reads as a fix that did not work —
`ARTIST_IMAGE_CACHE_LIMIT` is exported and shared for that reason. And
**a bound has to stay checkable**: caches register with
`utils/cache-stats.ts`, so `window.__yjCacheStats()` reports entries,
retained chars and cap in one eval, rather than the next session having
to rebuild the twenty-four-search reproduction before it can tell
whether the ceiling still holds.

**Expanding an album shows its tracks, and the code to do it was
written and never called.** `cover-grid`'s dropdown — the album's
tracks drawn between the two halves of a split grid — was reachable
only from Enter/Space on a focused card (a plain *click* navigates to
`explore-album-details`), and that path fetched the tracks over the
IPC, ran the whole split state machine and then rendered the single
grid, because `render()` never consulted `splitMode`.
`connectedCallback` referenced `renderSplitGrid` purely to satisfy
`noUnusedLocals`. `perf.p2` files this as dead code in the bundle; it
is the only route from the albums grid to `track-details`.

Two things it needed that are not in the audit. **The grid could not
scroll at all**: `.grid-scroll-container` is the same markup
`artists-view` and `genres-view` use, and `cover-grid` had the class
with *no rule for it*, so the container grew to its full content height
inside an `overflow: hidden` host — 186 984 px of albums in a 772 px
box at 5 000 albums, unreachable by wheel, keyboard or scrollbar, and
invisible on the eight-album fixture. That is also the element
`scroll-manager.ts` saves and restores, so its `scrollTop` was
permanently 0; with a real scroller the manager works as designed
(2891 preserved exactly across an expand). And the shared context-menu
panel was **labelled "Album actions" unconditionally**, which nothing
could observe while the only menu that could open on a track was
unreachable.

The manager **moves the scroll to reveal the dropdown** rather than
preserving it — on a small library that is most of the way back to the
top (80 → 4, with the content *taller* after, so it is not clamping).
"The position is preserved" is the wrong assertion; "the dropdown is on
screen" is the contract.

**A list pays per row, and only while scrolling.** The track list's Art
column rendered `CoverArtPath` — the original artwork — into a 24 px
box while `CoverArtSmall` sat unused on the same model, and
`artists-view` linear-scanned every cached album per card per frame to
find an avatar fallback. Both are invisible to every test tier: nothing
renders differently and nothing fails, the app is just slower to
scroll. Pick the tier for the box you are drawing (`cover-grid`'s
`getCoverUrl()` is the model), always `loading="lazy" decoding="async"`
on a row image, and build a lookup keyed on the store array's identity
rather than searching it — the store replaces that array when its
contents change and shares the unchanged members, which is the same
signal `track-list`'s memoized caches key on.

**The same rule, on the selection path, was the worst stall in the
app.** Five components turned selected file paths back into tracks with
`filePaths.map(fp => tracks.find(…))`, so "Select all → Edit tags" at
50 000 tracks blocked the main thread for **three to six seconds**
(`perf.m6`). `utils/track-index.ts` is that lookup written once: a
`WeakMap` from the array's identity to a `Map<FilePath, Track>`, safe
for exactly the reason above and collected for free when the store
drops the array. **68 ms after.**

Its neighbour is a deliberate non-fix. `getSelectedKeysOrdered()` walks
the *list* rather than the selection, which the same finding calls out —
measured at **3 ms** for 50 000 items, so it stays a walk, with an early
exit once everything is found (which helps a selection near the top of
the list and, honestly, almost nothing at the bottom). The obvious fix —
keep each key's index beside it — is the one thing that cannot be done
here: an index goes stale on any re-sort, re-filter or refetch while a
file path survives all three, which is precisely why `retain()` drops
`lastSelectedIndex` and keeps the keys. Three milliseconds does not buy
a silently mis-ordered queue insert.

**Work in `updated()` runs on every pass, so it has to say what it
depends on.** `now-playing` measured and rewrote its text geometry
every update — six `querySelector`s and a read/write interleave — and
`player-store` notifies while playing, so it did that several times a
second about a component whose DOM had not changed (`perf.m5`). It now
runs only when its geometry key changes: the rendered title, the
rendered artist, **the two scroll flags**, or the ResizeObserver
reporting the panel resized. The scroll flags are in that list because
`.will-scroll .scroll-content` carries `padding-right: 2em`, so
applying the class changes the distance the marquee travels (−128 px
before it, −158 px after) — a guard on the text alone leaves every
first hover scrolling short, and nothing would fail. Reads before
writes in one place, so the interleave cannot come back.

Two corollaries. **Cost is measured where the work runs, not where it
is written**: those same reads cost 3 µs when the layout is clean and
0.103 ms when it is dirty, so the finding's magnitude only appears if
the DOM actually changed. And **a drag's document listeners belong to
the drag** — `track-list` and `now-playing` attach `mousemove`/`mouseup`
on `mousedown` and drop them on `mouseup` (and on disconnect, for a
drag interrupted by the component going away).

**A virtualized list repaints only when you tell it to, and the
accidental way you were telling it may be the thing you are about to
delete.** `<lit-virtualizer>`'s rows are produced by the `virtualize`
directive, which runs when one of the *virtualizer's own* properties
changes — not when its parent re-renders. So a list with memoized
`items` and a stable `renderItem` never repaints on host state, and
selection silently stops highlighting while the controller holds
exactly the right keys. Both playlist detail views virtualize now
(`perf.M5`: 22 090 elements and 2 000 eager cover requests for a
2 000-track playlist, against 487 and 0), and both therefore push
`virtualizer.requestUpdate()` on a selection change and on a
playing-track change, which is what `track-list` has always done.

The corollary is that **`artists-view` and `genres-view`'s per-render
arrow functions are load-bearing.** `perf.m1` asks for them to be
hoisted to stable fields the way `cover-grid` does; hoisting them is
what *stops* the virtualizer seeing a changed property, so the cards
keep whatever classes they had — measured at 1 highlighted card before
and 0 after, with no compensating win to pay for it.
`frontend/test/components/card-grid-repaint.test.ts` fails on that
change and exists for no other reason.

Two smaller rules from the same pass. A row inside a virtualizer needs
`width: 100%`, because the virtualizer positions its children
absolutely and a grid row otherwise shrinks to fit its content and
stops lining up with the header above it. And a panel that is closed
renders no list at all (`perf.m7`): `width: 0` and `contain` bound the
damage but do not stop a virtualizer inside from measuring its window
on every change, or `scrollToIndex` from calling `scrollIntoView()` on
something invisible.

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
- **Commits**: Conventional Commits, enforced by `scripts/commit-check.sh` —
  a lefthook `commit-msg` hook locally, and a CI step over every commit in
  a push. It is twenty lines of shell rather than commitlint, because the
  grammar is one regex and commitlint would mean a Node dependency tree at
  the root of a Go repo. **Its type list is `.releaserc.yml`'s**; keep the
  two in step or semantic-release will decline to release something the
  check accepted.

  `.releaserc.yml` is a complete semantic-release config that **nothing
  currently runs** — no workflow invokes it, and `CHANGELOG.md` is not
  being written by it. That is deliberate for now (wiring it means pushing
  tags, committing a changelog back, and interacting with the three
  publish workflows); it is recorded here rather than implied, because
  this file claimed for five phases that commitlint gated CI and that
  semantic release ran, and neither was true.

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

- **`check`** — no display: `make commit-check` over the push's commits,
  `make lint` and `make test` (three build configurations each),
  `tsc --noEmit`, `make ui-test`, `make bindings-check`,
  `make skill-check`.
- **`e2e`** — under Xvfb and a private D-Bus: fixtures, a seed built by
  running the app, `make dev-headless`, then the Playwright suite
  against **both** Chromium and WebKit. Playwright's Linux WebKit links
  Ubuntu 24.04 libraries that Arch does not provide, so CI is the only
  place it can run, and it is the closest available approximation of
  the WebKit2GTK renderer that ships. The WebKit step carries
  `if: ${{ !cancelled() }}`, without which a chromium failure skips it
  — which is how the one source of WebKit signal came to produce none
  for two sessions while the plan recorded it as "unverified".

**A failing job's log is readable, and `gitea_ci job_logs` is not the
only way.** That endpoint 404s on this Gitea build; the REST API does
not. `GET /api/v1/repos/{owner}/{repo}/actions/runs/{run}/jobs` gives
per-step status (which is how "WebKit was skipped" was found) and
`GET /api/v1/repos/{owner}/{repo}/actions/jobs/{job_id}/logs` returns
the whole log, with `Authorization: token $GITEA_TOKEN`.

Two things the container needs that a developer machine does not.
**It needs an audio device that keeps time**, because the player's
position is derived from what has been consumed — so a device that
accepts audio instantly makes every track finish instantly and the
clock never move. That is what ALSA's `null` plugin does, contrary to
a year of this file and `ci.yml` saying it paces on a timer: measured
through beep and oto with `player.InitSpeaker`'s own arguments,
**3000 ms of audio consumed in 2.96 ms**. It is a PulseAudio null sink
now (the same 3000 ms takes 3762 ms), started in system mode because
the job runs as root, with a step that plays three seconds and fails
if they take under two — a dependency with a rate, checked like one.
And
`YJ_CORE_INDEX_URL` points at a dead address so no run fetches the real
explore artifact, matching what `scripts/seed-sandbox.sh` already does.

**`make lint`'s tag sets must stay identical to `make test`'s.**
Without `webkit2_41` wails resolves `webkit2gtk-4.0`, which Arch still
ships and Ubuntu 24.04 does not — so a mismatch lints a configuration
that only builds on one developer's distro, and says nothing about what
ships.

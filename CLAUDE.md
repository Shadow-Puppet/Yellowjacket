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
search, the disabled box points at it.

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

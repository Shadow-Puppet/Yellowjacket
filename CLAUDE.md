# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

YellowJacket is a cross-platform desktop music player built with Go (backend) and TypeScript/Lit (frontend), using the Wails framework to bridge them. It supports MP3, FLAC, OGG Vorbis, and WAV playback.

## Issues

**The tracker is the source of truth for what is wanted and what is
already being worked on**, and it is shared with a collaborator who
cannot see this session. `scripts/issue.sh` is the whole interface to
it (`list`, `mine`, `search`, `show`, `new`, `claim`, `unclaim`,
`comment`, `close`, `label`, `depends`, `labels`); it needs a
`GITEA_TOKEN` with `write:issue`.

**Search the tracker before starting any work, and claim what you
find.** Fifty-odd issues make that a real lookup rather than a
formality. `./scripts/issue.sh search <terms>` covers open and closed —
closed matters, because "that was fixed three weeks ago" is the
cheapest possible answer.

**Claiming happens before the first edit, not before the commit.** The
whole point is that the collaborator can see the work is taken *while
it is being done*, so `claim` sets the assignee, applies
`Status/In Progress` and posts a comment naming the branch and the
approach — all three, or none. It refuses outright if somebody else
already holds it, and that refusal is the feature: talk to them rather
than working around it.

**If no issue covers the work, open one first.** The issue exists
before the branch does. That is what makes the tracker a description
of the project rather than a description of the past.

**Findings get filed.** A bug tripped over while doing something else
is an issue with a reproduction, not a sentence in a chat message
nobody can search. So is a piece of work deliberately not done — the
issue is where "we decided not to, and here is why" survives.

Four conventions are already established and are not up for
reinvention:

- **The labels are a taxonomy**, not tags: `Kind/*`, `Area/*`,
  `Priority/*`, `Platform/*`, plus `Reviewed/Confirmed` (the code was
  read and the defect confirmed) and the `Status/*` family. `Status/*`
  and `Reviewed/*` are **exclusive scopes** — one of each at most, so
  applying a second replaces the first.
- **#73 is the roadmap.** It states the order the backlog should be
  worked in and the soft relations that are not expressible as
  blockers. Picking work off the open list by eye when a meta issue
  states the sequence is how the sequence stops meaning anything.
- **Hard blockers are real Gitea dependencies**, which render on the
  issue itself, and the blocked issue carries `Status/Blocked`.
- **A PR body carries a commit-to-issue table, the verification
  actually run, and a `Closes` list** — PR #83 is the shape. That list
  is for whoever reads the PR; what actually closes an issue is the
  footer below.

**The closing keyword goes in the commit body, one issue per line.**

```
docs: delete four documents that contradict the code

<body>

Closes #98
```

**Gitea parses commit messages that reach `main`; it does not parse the
PR body**, which only closes anything if the merge happens to copy it
into the merge commit. Both halves of that were measured. #83's merge
commit carried `Closes #9, #13, #14, …` and closed **five of ten** — a
comma list is partially matched. #93's merge commit body was one
`Reviewed-on:` trailer, so #92 stayed open behind a perfectly correct
`Closes` line in the PR description.

A footer costs nothing elsewhere: Conventional Commits allows one,
`scripts/commit-check.sh` only regexes the subject, and
semantic-release reads the type from the subject — so this changes no
release decision. The rule that the issue number stays out of the
**subject** is unaffected, and was never about the body.

**Check it anyway.** A squash, or a merge message edited by hand,
still drops the footer. `./scripts/issue.sh list --state open` after a
merge, looking for what you just shipped; `./scripts/issue.sh close
<n>` for whatever did not take, with a comment naming the commit.

**Unclaiming is automatic, and it is hooked to the close rather than
to the merge.** Gitea's auto-close changes state and nothing else, so a
footer left `Status/In Progress` on a closed issue — #100 was closed
and marked as being actively worked on at the same time.
`.gitea/workflows/unclaim.yml` runs on `issues: [closed]`, which covers
the footer, `issue.sh close` and a click in the web UI alike; stripping
the label in the PR instead would have been a per-PR habit, and habits
are what the footer removed. It is not instant — the runner has
capacity 1 — and reopening deliberately does not restore the label.
`./scripts/issue.sh list --state closed --label "Status/In Progress"`
is how you find out it has stopped firing.

## Planning

`.planning/` is **design documents and measured history**, not a queue
— the queue is the tracker, and a plan file that describes work nobody
has started is a second, staler answer to "what are we doing next".

- `.planning/NOTES.md` — gotchas, measured facts, open architecture
  questions, and the "we already considered and rejected" list. Dated,
  because several are properties of someone else's server. **This is
  where a decision reached on an issue gets written down** when it
  outlives the issue.
- `.planning/plans/completed/` — one recap per shipped milestone, kept
  for the arguments in it. Where a plan shipped incompletely, its
  header says which issue carries the remainder.
- `.planning/audits/` — the read-only audits that produced the
  reconciliation plans. Historical evidence; not a backlog.
- `.planning/plans/active/` — a multi-phase design document for work
  **in flight**, linked from the issue that tracks it. Empty is the
  normal state. There is no `pending/`: a plan nobody is executing is
  an issue.

Numbering is sequential and stable across status moves (a plan keeps
its `NNN-` prefix). Abandoned plans are deleted.

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
make build-prod       # Production build (stripped and trimmed; no UPX)
make generate         # Run code generators (sqlc + templ via go generate)
make e2e              # Playwright smoke suite against a running dev-headless app
make e2e-setup        # Install the e2e runner + its browser (once)
make ui-test          # Vitest component/store suite in a real browser (no app)
make ui-visual        # Same, including toMatchScreenshot comparisons
make ui-setup         # Install the Vitest provider's own Chromium (once)
make bindings-check   # Fail if frontend/bindings is stale vs the Go bindings
make skill-check      # Fail if .pi/ documents a make target that doesn't exist
make commit-check     # Fail if a commit subject is not a Conventional Commit
make lint             # golangci-lint v2 (strict), all three build configurations
make test             # All tests with race detector, all three build configurations
make vulncheck        # govulncheck for CVEs
make setup            # Install go tools, frontend deps, git hooks (lefthook)
```

### Running tests

Go test commands need no build tag for the app configuration. The
`webkit2_41` tag every command here used to carry is gone with wails
v2: v3 builds against GTK4 + WebKitGTK 6.0 by default, which both Arch
and ubuntu:24.04 ship.

```bash
go test ./...                           # All tests
go test ./backend/player/               # Single package
go test -run TestName ./backend/player/  # Single test
```

The central index builder is behind a second tag and is **not** covered
by the command above — `make test` runs both passes, but a manual run
needs it spelled out:

```bash
go test -tags indexbuild ./backend/explore/... ./cmd/...
```

`backend/testctl` is behind a third tag and needs its own pass too
(`make test` runs all three):

```bash
go test -tags dev ./backend/testctl/...
```

Audio playback integration tests require `YELLOWJACKET_INTEGRATION=1`.

### Fixtures and the headless harness

`test_data/music_library_test/` is **generated, not committed**: run
`make testdata` (~1 s) before anything that needs audio. Tests reach it
through `internal/testfixtures`, selecting files by *case*
(`CaseCoverDedup`, `CaseUnicode`, `CaseDuplicates`, …) rather than by
path, and skip themselves when it has not been generated.

The app itself can be run without a blocking window — `make
dev-headless` — and driven with `playwright-cli` against it on
`:34115`. That is wails v3's first-class `-tags server` mode: the real
app, the real bindings, the same Go backend a desktop window would use,
served over HTTP with **no display at all**. The Xvfb this used to
require is gone, from the script and from CI; `dbus-run-session` stays,
for MPRIS.

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
  It also provides `ready()` and `call('queue.Queue.GetState', [])`.
  Both hook v3's own seams rather than an internal: inbound is
  `window._wails.dispatchWailsEvent`, the entry point the backend's push
  uses, and outbound is **`fetch`** — v3 routes every runtime call
  through one POST to `/wails/runtime`, so one hook sees calls from any
  module and cannot miss one made before the harness looked. `call()`
  posts by method name, so it depends on nothing in the app's bundle and
  works on a page with no init script — which is why `seed-sandbox.sh`
  is `curl` now and needs no browser. It no longer races a timeout
  either: v3 rejects bad arguments and unknown methods cleanly.
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

**The cheapest tier needs none of that.** `make ui-test` runs 776
Vitest tests in a real Chromium with no Wails, no backend, no seeded
library and no virtual display, because **v3 routes every runtime call
— bindings, event emits, window, dialogs, clipboard — through one IPC
transport**, and `frontend/test/support/wails-fake.ts` replaces it via
`setTransport()`, which is public documented API. So the fake covers
strictly more than v2's two globals did while being shorter, and the
tests exercise the real generated bindings, the real runtime and the
real store code. A binding carries an **ID**, not a name
(`$Call.ByID(2822423495)` is FNV-1a over
`yellowjacket/backend/home.Service.GetShelves`), so the fake derives
that map from the generated tree rather than writing it down.

**A test file does not get its own origin, so `setup.ts` clears
`localStorage` between tests.** `@vitest/browser-playwright` opens one
BrowserContext per session and runs several files in it one after
another, so everything a component persists — the track list's sort and
column widths, the cover size, `now-playing`'s scroll mode — is still
there when the next file mounts the same component. Which files share a
tab, and in what order, changes run to run, so the symptom is a spec
that fails about one test in three and passes every time it is run on
its own: #138 cost three scheduled runs, one of them a PR whose diff
held no frontend code at all. Measured on the build before the fix, a
single full run started **24** tests with storage already set. Two
things follow. The clear is safe precisely because the leak is
sequential — files in a session do not overlap, so it cannot wipe
storage a concurrently-running file is in the middle of using — and it
belongs in `setup.ts` rather than in the specs that write, because the
spec that *reads* is never the one that knows. And a spec whose
assertion depends on an order still **states that order** rather than
inheriting a default, or the next change to a default is the same
mystery again.

**`frontend/bindings/` is generated by `wails3`, not `go generate`**, so
the pre-commit codegen check does not cover it. `make bindings-check`
(~3.5 s warm, ~20 s on a cold build cache, also a pre-commit hook)
regenerates it and fails on a dirty tree; `make bindings` regenerates it
for real. It is slower than v2's because v3's generator is a **static
analyser** over the whole package graph rather than runtime reflection
— which is also why the tag set it runs under matters, and why it is
the *default* one: that is the configuration users run, and neither
`indexbuild` nor `dev` adds a bound service. See
`scripts/bindings-check.sh`.

**Seeds are produced by running the app**, never by hand-writing a
`config.toml` and DB rows — the same discipline `sql/schemas/` gets,
for the same reason.

See `.planning/plans/completed/005-agent-development-harness.md`.

## Architecture

**Wails app lifecycle** (`main.go` → `backend/app.go`): `main.go` is
`application.New(opts)` → `app.Window.NewWithOptions(…)` → `app.Run()`.
Each bound service takes its context from `ServiceStartup(ctx,
application.ServiceOptions{})` — v3 calls it on every service, in
registration order, on the main goroutine — and gives it back in
`ServiceShutdown()`. That context is **cancelled on app shutdown**,
which `SetContext` never was.

Three things about it are load-bearing.

**`ServiceShutdown()` takes no context.** A method with a
`context.Context` parameter does not satisfy the interface and is
**silently never called** — no error, no warning.

**The cross-service wiring is a service, not an event.** v3 has no
`OnStartup`/`OnDomReady` option, and the obvious replacement —
`app.Event.OnApplicationEvent(events.Common.ApplicationStarted, …)` —
is the right *moment* and the wrong *mechanism*: **server mode emits no
application events at all** (`setupCommonEvents` is an explicit no-op
under `-tags server`), so the desktop build wired itself and the
headless harness did not. `backend/startup.go` is registered last
instead, which takes the ordering from the mechanism rather than from
an event and therefore holds in every mode. Anything else keyed on
`Common.*` is suspect for the same reason.

**The quit veto is asynchronous now.** v2's `MessageDialog` blocked and
returned the button; v3's `Show()` returns immediately and the answer
arrives on a `Button.OnClick` callback, so `ShouldQuit` cannot ask and
answer in one call — it vetoes, shows the dialog, and calls
`app.Quit()` from the callback. `quitConfirmed` is what stops that
second `Quit()` asking again; `quitAsking` stops a second close attempt
stacking dialogs. Window state moved off that path entirely, onto a
`Common.WindowClosing` hook, because the size has to be read while the
window still exists and `OnShutdown` has neither a context nor a
window.

**An activity is a view onto the process, and `main()` runs once per
process.** On Android the Wails entry point is `nativeInit`, which
`MainActivity.onCreate` calls — and it does two things: it re-points the
native library's global JNI reference at the calling `WailsBridge`, and
it runs `go mainFunc()`. Android destroys and recreates an activity
**without restarting the process** (a configuration change the manifest
does not declare, memory pressure, or every background under "Don't keep
activities"), so `main()` ran again on a live app. `application.New`
returns the *existing* app rather than building a second one,
`app.Run()` then refuses — `a.starting` is still true, because Android's
`platformRun` is `select{}` and never returns — and the `os.Exit(1)`
under that error took the **first**, healthy app down with it: its
database, its queue, and the audio a `mediaPlayback` foreground service
was holding the process alive to play. `mainStarted` latches it, first
statement in `main()`.

Four things about it are load-bearing.

**The answer to "restore the session or cold-start" is settled by
playback, not by preference.** The audio lives in the Go process, so a
cold start on every activity recreation would stop the music mid-song —
which is the exact thing the foreground service exists to prevent. The
activity is a view; the app is the process. The frontend already
cooperates, because a recreated WebView loads the page fresh and fetches
its state from a backend that never went away.

**Returning early is not a degraded mode, and that is why the latch is
in Go rather than in Java.** The obvious fix — making
`WailsBridge.initialized` `static`, so the second `nativeInit` is
skipped — keeps the process alive and silently breaks the app, because
skipping `nativeInit` skips the reference re-point too: Go would keep
executing JavaScript against the *destroyed* activity's WebView, and the
app would open, render, and never receive another backend event. The
latch lets `nativeInit` do its first job and declines only its second.

**`ServiceShutdown` has never run on Android**, and nothing should be
built on the assumption that it will. `App.Quit()` reaches an
`androidApp.destroy()` that is an empty method, and `Run()`'s deferred
`shutdownServices()` cannot fire behind `select{}`. Durability on this
platform is the persist writers, which submit on every mutation rather
than at exit — which is also why `MainActivity.onDestroy` no longer
calls `bridge.shutdown()`: the activity going away is not the app
shutting down, and there is no callback for the process going away
because Android simply kills it.

**No tier here can see any of this**, so the guard is split. A source
sweep (`TestMainClaimsBeforeItDoesAnything`) asserts the latch is the
*first* statement of `main()` — the failure it exists for is not
deletion, which is loud, but a line creeping in above it, since a second
`NewYellowJacketApp` opens the SQLite database again on every
recreation. The rest is a documented device check in
`.pi/skills/yellowjacket-dev/references/android-tier.md`, with the
logcat signature and a one-line way to force a recreation.

`internalServiceMethods` auto-excludes `ServiceStartup`,
`ServiceShutdown`, `ServiceName` and `ServeHTTP` from bindings, so this
shape **removed** 12 spurious bindings and the bogus `context` model
rather than renaming them.

**Backend packages** (under `backend/`):
- `player` — Audio playback via beep. `BufferedStreamer` provides a ring buffer for smooth seeking.
- `queue` — Track queue with shuffle (Fisher-Yates), repeat modes, auto-advance, and session persistence.
- `library` — Concurrent library scanning, metadata extraction, cover art deduplication, incremental rescan. Also **removal**, below.
- `database` — SQLite via pure-Go driver. Schema in `database/sql/schemas/`, queries in `database/sql/queries/`. **sqlc** generates Go code into `database/sql/sqlcgen/` — never edit that directory by hand.

  **The schema is one description, and there is no migration chain.**
  `sql/schemas/*.sql` is `CREATE ... IF NOT EXISTS`, declares the
  current shape of every table, and is what sqlc reads and what an
  install gets verbatim. That is the whole mechanism: `sql/migrations/`,
  `applyMigrations` and `schema_migrations` were squashed away with the
  file-shaped rewrite (plan 013). A schema change is one edit to one
  file plus `make generate`. Reintroducing a chain means reintroducing
  the drift it caused before — `sql/schemas/` and the migrations
  disagreed, and sqlc generated against the stale one.

  **What that costs an existing database is repaired once, at open.**
  `CREATE ... IF NOT EXISTS` reaches an existing table only if its shape
  already matches and otherwise silently no-ops, so a *changed* table
  never migrates. Plan 014 added `total_tracks` to `explore_index` and
  to `indexRowFields` — the projection every explore read uses — and no
  database that already existed grew the column: **every** Explore
  search, browse, artist and album page on such an install failed with
  `no such column: total_tracks`, while a fresh install was perfectly
  healthy, which is exactly why no test saw it. Plan 013 was worse on
  the same install: `applySchema` could not be applied at all over a
  pre-013 `audio_files`, so the app did not open.

  `backend/database/staleshape.go` runs before `applySchema` and
  retires what is stale, so the create is a create. Five things about
  it are load-bearing:
  - **It parses `sql/schemas/` for the expectation** rather than
    writing the column list down a second time, because a second list
    is a second thing to forget — the fault it exists to repair.
  - **It notices a changed *type*, not just a missing column.** 013
    moved `mbid` from TEXT to BLOB, and SQLite does not coerce between
    them: a comparison against 16 raw bytes returns no rows rather than
    an error. `ALTER TABLE ADD COLUMN` would have handled
    `total_tracks` alone and cannot express this at all, which is why
    the repair drops rather than migrates.
  - **`Authored` is never retired**, and that boundary is a test
    (`TestAuthoredTablesAreNeverRetired`), not a comment. Everything
    else is rebuildable: `Cache` by definition, `Owned` by a rescan —
    plan 013's stated "delete and rescan" — and `Derived` from Owned.
    A table the schema no longer describes at all goes too; 013 left
    seven behind plus `schema_migrations`.
  - **Whether a stale `Cache` table may be rebuilt is a build tag**, and
    it is the most expensive thing in this file to get wrong. In the app
    the catalog is *downloaded*, so a wrong shape costs a minute of
    re-fetching the artifact and keeping it costs every Explore read. In
    `cmd/indexbuild` the catalog is *derived*, and the only way back is
    the ~205 GB dump stream the `/cache` volume exists to avoid — so
    `retireStaleCache` is false there (`staleshape_policy_indexbuild.go`)
    and `TestTheCatalogSurvivesAStaleShape` fails the moment it is not.
    `TestNoCacheTableIsRetiredHere` is the same assertion made of *every*
    `datamap` Cache table rather than one, because the risk is not that
    shape recurring — it is the next destructive repair added to
    `database.NewDB`, the chokepoint every binary here shares, without
    asking which binary it is in.
    This is written down because it already happened: the repair shipped
    without the distinction and dropped the real CI catalog on its first
    run, with `reason="column entity_type is TEXT, schema declares
    INTEGER"`. The mismatch was genuine — that database is deliberately
    kept in the older encoding, which `fix(indexexport): read an index
    older than the binary` exists to tolerate — so it would have been
    dropped on *every* run. The consequence is that a future
    `explore_index` column fails the index job loudly on `applySchema`
    rather than silently costing it a rebuild, which is the trade a
    human should get to make.
  - **The drops are one transaction with `defer_foreign_keys`.** Those
    legacy tables reference each other, so dropping them in any order
    fails on whichever goes first, and turning foreign keys *off*
    instead would silently take `playlist_tracks.audio_file_id`'s
    ON DELETE SET NULL with it — leaving playlist entries pointing at
    ids a rescan reissues to *different songs*. Nulled entries are
    empty; stale ones are wrong, and wrong quietly.
  - **The order is sorted, so a failure reproduces.** Map order is
    random, and the foreign-key bug above passed its own regression
    test on two runs in three until the order was fixed.

  Retiring `explore_index` takes its FTS and its meta with it, because
  the `dump_import_done` marker is what would otherwise stop the
  artifact ever being fetched again.

  **What that costs an existing database is that it does not open**, and
  "delete and rescan" is the answer (plan 013, open question 1) — free
  for everyone except one machine. The index job's `/cache` volume is a
  real `YJ_HOME` that survives between runs, and half of it is the
  catalog: deleting it means re-downloading ~205 GB. So `cmd/indexbuild`
  repairs it instead (`staleschema.go`), dropping every table `datamap`
  does not classify as `Cache` **before** the schema is applied. Nothing
  scans, plays or authors in that database, so its non-catalog half is
  empty by construction and a shape left over from an older schema is
  pure liability. 013's reshaped `audio_files` failed every run of that
  job on `CREATE INDEX ... album_id` against the old table until this;
  `TestRetireLibraryTables` reproduces exactly that, symptom first.

  **The local library is shaped like files, not like MusicBrainz.**
  `audio_files` carries its own tags — title, artist credit, track and
  disc numbers, year, composer, the recording MBID — and points at two
  shared rows: `albums` (many files to one) and `artists` (many albums
  to one). `file_genres` is the one genuine many-to-many. That is the
  entire local model.

  It used to be MusicBrainz's: `recordings`, `release_group_recordings`,
  `artist_credit` and `artist_credit_artist` sat between a file and its
  own tags. Measured on a real 25,966-file library, **every**
  many-to-many that model expressed was 1:1 in the data — no recording
  had two files, none belonged to two release groups, and 3 credits of
  2,823 listed more than one artist. What it cost was a six-way join in
  every read, a `MIN(release_group_id)` subquery in eleven queries and a
  first-credited-artist subquery in nine to undo fan-outs that never
  happened, and a class of bugs where a metadata row **outlived the file
  that created it**: retagging a file created a new recording and
  abandoned the old one, so that library carried 812 orphaned
  recordings, 216 release groups and 260 artists — and 129 catalog rows
  that confidently claimed to be owned by files that no longer existed.

  A few things that follow, and bite if forgotten:
  - **Ownership is a file.** "Do I own this" is asked of `audio_files`
    and nothing else — `GetFilePathsByRecordingMBIDs`,
    `LibraryMBIDIndex.CheckMBIDs`, `collectLibraryEntities` and
    `pruneStaleLocalCrossReferences` all join it. A metadata row is not
    ownership; that was the bug, and it is now structurally impossible
    for a row to exist without its file.
  - **The projection is defined once, in the `track_metadata` view.**
    Every query that returns a track selects from it, which is why
    there is one row type (`sqlcgen.TrackMetadatum`) and one mapper
    (`trackFromRow`). It existed before and only the raw-SQL search
    paths used it, so nine hand-rolled copies had already drifted: the
    view preferred the album's original year and one copy used the
    track's, and the same library reported different years on different
    screens. The FTS searches cannot be sqlc queries (MATCH is not in
    its grammar) and spell the column list out in `search.go` — that is
    the one exception and it is one constant.
  - **`library_id = 0` means every library.** Each list query used to
    exist twice, scoped and unscoped, with a branch at every call site
    and a separate binding for each. One query answers both, and the
    scoped form costs nothing measurable (23 ms against 21 ms over 26k
    rows).
  - **A slice and a named parameter do not compose in sqlc.**
    `sqlc.slice` expands to N placeholders but a named argument is
    numbered independently, so `GetFilePathsByAlbums([1,2], 0)` read
    album id 2 as the library id. Where a query needs both, return
    `library_id` and filter in Go (`inLibrary`).
  - **A cache without a ceiling is a leak with a schedule.** Every
    store that grows with use declares a budget beside its retention
    (`browsedArtBudget`, `httpCacheBudget`), because an age bound does
    not bound anything a user can outrun in an afternoon.
  - **A query file must be ASCII.** sqlc's parameter rewriter works on
    byte offsets, so one non-ASCII character in a *query* comment
    corrupts the generated Go into garbage like `SELECid`. Schema files
    are not rewritten and may contain anything.
  - **A view is dropped and recreated, not migrated.**
    `CREATE VIEW IF NOT EXISTS` no-ops against a database holding the
    old definition, so `track_metadata.sql` opens with
    `DROP VIEW IF EXISTS`; a view holds no data, so rebuilding it on
    every open costs nothing.
  - **A write wearing a query's shape still needs the writer.**
    `DB.QueryContext`/`QueryContextWith`/`QueryRow` route to a
    *query-only* read pool (a second `sql.DB` over the same file), so
    an `INSERT ... RETURNING` issued through one fails at runtime with
    "attempt to write a readonly database (8)" — which is exactly what
    `CreateSmartPlaylist` did, meaning no smart playlist could be
    created at all. Use `ExecContext`, or `QueryRowWriter` when the
    statement really does return a row. Nothing caught this because
    `NewTestDB` shares one in-memory connection and leaves `readDB`
    nil, so `reader()` returns the *writer* under test.
    `TestNoWritesOnTheReadPool` walks the tree for it, in the same
    spirit as `TestNoDirectRuntimeEmits` and for the same reason — a
    lint pass only sees one build configuration.
  - **A new table has to say what kind of data it holds.**
    `backend/datamap` is a catalogue of every table's Kind and
    Lifetime, and `TestCatalogCoversSchema` fails on a table missing
    from it. `TestAuthoredCascadesAreDeliberate` then makes an
    *authored* table that cascades an explicit, argued exemption —
    authored data is what a user cannot get back. Two entries say
    **MIXED KIND** and mean it: `audio_files` is an owned projection
    except for `play_count`, `last_played` and `tag_status`, which are
    authored; `lyrics` carries a `source` column because a lyric read
    from a tag is free to rebuild and one fetched from LRCLIB is not.
  - **Test data has one seeder.** `database.InsertTestTrack` inserts a
    file with its artist, album and genres. Twenty test files used to
    carry their own, each assembling the old FK chain in a slightly
    different order.
- `metadata` — Tag extraction (ID3v2, Vorbis Comments, FLAC).
- `jobs` — The registry every long-running operation reports through:
  progress, pause/cancel, a global indicator and (for scans) a pause
  that survives a restart. Library scans, index builds, downloads and
  the autotag apply are registered; anything that is not registered has
  none of that, which is exactly how the three gaps the audit found
  came about.

  **Its rows are shown where the work is started, not on a page of
  their own.** #27 folded the Jobs destination away, and the shape it
  folded into is `<job-panel kinds="…">` embedded four times — scans in
  Settings → Libraries, index and enrichment in Settings → Search
  Index, downloads under the download clients, the autotag apply in
  `autotag-view`. One "Background jobs" section in Settings was the
  obvious reading of the report and is the tab again under another
  name.

  Four things about it are load-bearing. **Four of the five kinds
  already had a home** that showed their work — the tier list, the
  download list, the apply ring — and what none of them had is the
  *generic* affordances, so the panel carries pause, cancel, Details
  and the log to each rather than replacing what is there. **The
  controls are `applyJobControl`**, not a reimplementation, which is
  what keeps the "you will discard hours of downloading" confirmation
  alive: it is keyed on `KindIndexBuild` inside the shared handler, and
  a host drawing its own buttons would drop it silently. **A panel with
  nothing to say is `hidden`**, host margin included, because an idle
  panel in four places is four pieces of furniture describing an
  absence. And **there is no "Clear finished"** in it, because
  `ClearFinishedJobs` is global — a Clear under Libraries would discard
  the index build's history too; a finished row dismisses itself.

  The header `job-indicator` is still the one view of everything at
  once, from every page — **on a desktop.** One consequence worth
  knowing before writing a spec: a section holding a `job-panel` also
  holds a `job-details-drawer`, whose own header carries `.header` — so
  `config-section .header` is ambiguous the moment a job exists.

  **Below 600px that indicator stands down and `<job-band>` takes
  over** (#62), because a popover is a *disclosure* and background work
  is the one thing a phone should not make you open something to see —
  and because #57 deletes the bar it is anchored to and is blocked on
  it having somewhere else to live. The band is the same `job-panel`,
  so `applyJobControl` and its index-build confirmation come along
  rather than being reimplemented; `kinds="*"` is how it says "every
  kind", which is what the indicator was for.

  Three things about it are load-bearing. **It is in the layout, not
  over it**, as its own grid row above the main panel: the first
  version put it in `notification-host`'s fixed band, which reads fine
  in a screenshot and is unusable — at 424×439 a compact panel is
  ~200px of a 439px screen and it *covers* what is under it, which four
  e2e specs caught by failing on taps it was intercepting. **It shows
  active work only** (`active-only`), because in flow a finished row is
  furniture that keeps the content pushed down after the work is done;
  finished rows stay where the work was started, which is #27's rule.
  And **it renders nothing above 600px**, from `matchMedia` rather than
  a media query, because that decides whether the element *exists* —
  Settings already holds four `job-panel`s and a fifth answering for
  every kind is `bottom-nav`'s "resolved to 2 elements" trap again.
  `index.css` keeps it `display: none` outside the phone for a second
  reason: an in-flow grid child with no named area is auto-placed into
  one of the shell's rows, which is what the skip link is absolutely
  positioned to avoid.
- `config` — TOML-based settings. Settings page uses HTMX + templ for server-rendered HTML fragments.
- `playlist` / `smartplaylist` — Playlist CRUD and rule-based smart playlists.
- `mediacontrols` — OS media controls behind one `Handler`: MPRIS over
  D-Bus on desktop Linux, a MediaSession on Android, a no-op stub
  elsewhere. The split is by build tag and `android` implies `linux`,
  so the three files read `linux && !android`, `android` and `!linux`.
  Its Android half needs no JNI beyond what Wails exports — a JSON
  payload out through `application.Android.StartForegroundService`, a
  command event back through `WailsBridge.emitEvent` — and the Java it
  talks to is `build/android/.../WailsForegroundService.java`. That
  contract (payload keys, state words, command names) is in
  `androidpayload.go` **without** the build tag, because a tagged file
  is compiled by nothing `make lint` or `make test` runs and is
  untestable off a phone.

  `OnDuck` is the one callback MPRIS does not use: Android asks for
  attenuation rather than a pause when something short needs the
  output. `Player.SetDuck` keeps it as an offset on top of the user's
  level rather than writing through to the volume, so it cannot
  accumulate and nothing persists or emits a level the user did not
  choose — and it only ever fires below API 26, where the framework
  does not already duck the app itself. On that platform "the user's
  level" is a constant, since #64 pins it at maximum and refuses every
  way to move it; the duck is the one thing that still may, and it
  works unchanged because it was always an offset applied *to* that
  level rather than a write of it.
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

**The catalog stores ids as bytes, and that is a size decision.**
`explore_index` is 2,052,200 rows, and its MBIDs and entity types were
half of it: three 36-character text columns and one storing the words
"artist", "release_group", "recording" two million times. They are 16
raw bytes and a small integer now. Measured on a real catalog, the
table and its six indexes went **780 MB to 405 MB** — the largest
single saving available in this app, and the reason a fresh install is
~0.6 GB rather than ~1.0 GB.

`backend/explore/mbid.go` is the only place that encoding is known.
Everything above it speaks dashed strings and entity names —
`SearchIndexResult`, the bindings, the frontend — and `dbMBID` /
`dbEntityType` convert at the SQL boundary. That confinement is the
point: the alternative is blobs reaching code that has no use for them.

Four things about it are load-bearing, and they exist because of *how*
this fails when it fails: **SQLite does not coerce between TEXT and
BLOB**, so a query comparing the column against a 36-character string
returns no rows rather than an error, and a scan into a plain string
yields sixteen bytes of mojibake. Neither is visible except as a result
that is quietly empty.

- **The column checks itself.** `CHECK(length(mbid) = 16)` means a
  stringly *write* fails at the insert that made it. It also caught
  every fixture that had been using `"rh"` as an MBID; `testMBID()`
  hashes a label into a real one so they stay readable.
- **The projection is one constant and one scanner.**
  `indexRowColumns` / `scanIndexRow` replaced four copies of a 22-column
  list and four matching `Scan` calls — four chances to decode wrongly.
  `indexRowColumnsFor("i")` is the same list qualified, for the FTS join
  where both sides have a `title`.
- **A query that names an entity type inline writes the code with the
  name beside it** — `entity_type = 1 /* artist */`. Splicing a Go
  constant in would keep them in step automatically but makes every such
  query a concatenation; `TestEntityCodesAreStable` pins the mapping
  instead, because it is a storage format and changing one is not a
  refactor.
- **`TestStoredEncodingRoundTrips` sweeps every read path** — lookup,
  top-N, exact match, FTS search, popularity batch, the CAA map — and
  asserts each returns something with a dashed id. A missed conversion
  site shows up there and essentially nowhere else.

**The artifact is read in either encoding.** A published artifact
carries whichever form the exporter that built it used, and there is one
already out there in the old text form. `artifactStoresText` asks the
artifact (`typeof(mbid)`) rather than trusting a version number, and
`artifactSelectColumns` converts on the way in — one `unhex` per row on
a once-a-month import, against requiring a rebuilt artifact before a new
build can read anything. That probe **must** run on the writer:
`core` is attached to that one connection, so `QueryContext` asks a
pool where the artifact does not exist, and the error would silently
select the conversion path for an artifact that needs none.

**Its shape is the pattern for every column added after the fact.**
`artifactHasTotals` is the same question about `total_tracks`, on the
same handle: an artifact built before a column existed is still a
perfectly good catalog, so it is *asked* and the missing column is
selected as a literal `0`. Adding the column to the importer's SELECT
list without that is how a published artifact — which nobody can re-cut
retroactively — starts failing with `no such column`.

**A credit is ordered parts, and the string is derived from them.** A
track credited to several artists had exactly one navigable artist and
the rest were punctuation: `primaryArtist()` string-parses the credit,
strips a " feat. " clause and discards the guest, and deliberately does
not split on `&`, `with` or `,` because those live inside real artist
names ("Simon & Garfunkel"). Measured on a real 26,069-file library,
**13%** of recordings are multi-artist upstream while only **0.86%** of
files carry a structured multi-artist tag — mp3 carries *zero* files
with multiple `MUSICBRAINZ_ARTISTID` across 19,840 — so this cannot be
a tag-parsing feature. (The "3 credits of 2,823" figure that justified
plan 013's removal of the credit tables measured our own *writer*:
`cachedLinkArtist` ran once per credit, so a collaboration could never
have been recorded. Dropping the join table was still right on cost.)

`artist_credit_part` / `artist_credit_ref` carry the decomposition for
multi-artist credits only — a single-artist credit is already
`explore_index`'s own `artist_name`, and storing those would triple the
table to say nothing. Five things about it are load-bearing:

- **Join phrases are assembly instructions, not disassembly ones.**
  `creditLink` concatenates parts, so link boundaries are known by
  construction. Locating a `credited_name` *inside* the stored credit
  string would reintroduce the fault this exists to fix: that string may
  come from the file's tags while the parts come from the catalog, and
  the two disagree for ~1 in 3 multi-artist credits (`'Skrillex feat.
  Swae Lee'` tagged against `'Skrillex & Swae Lee'` upstream).
- **`credited_name` is stored per row**, never joined from `artists`:
  MusicBrainz credits "Snoop Dogg" on a track by the artist called
  "Snoop Doggy Dogg". Display follows the credit, navigation the MBID.
- **The lookup is keyed on the recording MBID**, which the catalog and a
  local file both carry (`library.Track.RecordingMBID`), so one binding
  serves Explore and the library's own lists — which is why this needed
  no local table. `file_artists` remains the offline-resilience step and
  is deliberately *not* declared until something writes it.
- **Absence is cached as an answer.** `credit-store.ts` stores `[]` for
  a single-artist credit — *asked*, not *answered* — or the ~87% that
  have nothing to decompose are re-requested on every render forever.
  `request()` is per-row and coalesces into one call per frame, because
  a virtualized list cannot hand over "the whole list": 50,000 rows is
  100 queries for the ~30 on screen.
- **The dump is a third source, and it had to be.** The canonical dump
  CI already streams has no join phrases and no as-credited names, and
  the JSON dumps cover 153,691 recordings of ~35M with *zero* overlap
  against a real library. So `mbdump.tar.bz2` — 7.1 GB, ~13.7 min in
  pure-Go bzip2, whose members are alphabetical, which is what lets one
  pass resolve an entity's credit without buffering 35M recordings. The
  pass runs on **every** mode, because a complete import means
  `refresh`, which never enters the importer at all, and it reports
  whether it populated anything so `changed` republishes the artifact.

**A 0.6 GB download asks about the connection first.** `explore`'s
catalog artifact had no network awareness at all, which on a phone is a
month's data allowance spent without being asked (plan 016 B4).
`netpolicy.go` is the gate, and its shape is dictated by one constraint:
`explore` is imported by `cmd/indexbuild`, which is built with
`CGO_ENABLED=0` and must not link Wails — so the *policy* and the
*parsing* live here and are tested on every platform, while the platform
call is a closure injected from `app.go`. It is
`application.Mobile.NetworkJSON()`, not `application.Android`'s: the
latter exists only under the `android` build tag, and `Mobile`'s desktop
implementation is a stub returning `""`.

Three rules in it are load-bearing. **An unknown answer is not a metered
one** — only mobile answers at all, so treating silence as metered would
refuse the download on every desktop. **Cellular is the only signal
available**: the runtime reports `wifi|cellular|ethernet|none` and no
metered flag, so a metered *Wi-Fi* (a hotspot, a hotel) cannot be
detected and is not refused, which is a documented gap rather than an
oversight. And **the gate runs before anything is staged**, so declining
is a no-op rather than a job in the indicator and a status the user has
to dismiss. The permission (`AllowMeteredCatalogDownload`, default
false, so an existing config is careful without a migration) is read at
the moment a download would start, so turning it on takes effect on the
next attempt rather than the next launch.

**Background work yields, and says so in the context.** The post-scan
backfills share MusicBrainz's rate limiters with every page the user
can open, and both were FIFO — so a thousand-artist enrichment put an
album page behind an hour of queued work.
`RateLimiter.WithBackgroundLane(perSecond)` adds a second, slower lane
and `WithBackgroundPriority(ctx)` marks a caller as belonging to it: a
marked wait takes no token at all while any interactive wait is
outstanding, and is then paced at MB's own 1/s rather than the
interactive burst rate. It is a **context marker rather than a
parameter** because a backfill calls the same `MusicBrainzClient`
methods a detail page does — `GetArtistImage` takes a `ctx` for no
other reason than to carry it. One request of slippage is accepted and
documented at `waitBackground`: cancelling an already-granted
reservation is not something a token bucket can express, and the cost
is one request-time.

The other half is that a long backfill has to be **visible and
stoppable**: `jobs.KindCatalogEnrich` and `startBackfillJob`
(`backfilljob.go`) register both backfills with progress and cancel.
Two rules in it are load-bearing. The job is registered *after* the
work is counted, because these passes are a no-op on every launch once
the library is covered and an empty job in the indicator is noise. And
the kind is distinct from `index-build` rather than reused, because
`job-controls.ts` keys its "you will discard hours of downloading"
confirmation on that kind — wrong prompt for a pass that is resumable
per artist and free to stop.

**An owned artist's discography is fetched, not sampled.**
`BackfillLibraryDiscographies` is two fetches per owned artist, each
skipped by its own persistent mark, because they fail independently and
one boolean covering both either over-claims or forces repeats:
the ListenBrainz top release groups and recordings
(`explore_index.discog_fetched`) and the **full** MusicBrainz browse
(`artist_enrichment.browsed_at`).

**What it does not fetch is the point.** It ran for hours against a
900-artist library and marked nothing, because three of the four things
it did per artist were work nobody had asked for. Similar artists were
fetched for every owned artist, when the artist page already resolves
them on view through `SimilarArtists` → `ensureSimilarArtistsAsync` —
which is what stamps `similar_at` now. And `indexOneArtist` reached the
MB artist lookup it wants (`GetArtistDetails` reads that cache) by
calling `GetArtistImage`, which additionally queried fanart.tv,
TheAudioDB, Wikidata and Wikipedia and downloaded up to ten full-size
portraits; `EnsureArtistRels` is the lookup on its own. The corollary
is written into the query: **`similar_at` must not be one of the
conditions** in `unenrichedLibraryArtistMBIDs`, because testing a mark
this pass no longer sets makes every owned artist a candidate on every
run, forever.

The rest is that the pass was **serial across artists** while every
limiter that keeps us polite is per-host and idle — so one artist's
slowest upstream set the pace for the whole run. It runs
`discogBackfillWorkers` artists at once (concurrency here raises no
origin's request rate), each under `discogBackfillArtistTimeout`,
because the MB client retries a 503 five times honouring Retry-After
and one throttled artist could otherwise outlast a hundred healthy
ones. A timed-out artist goes unmarked and is retried next run, which
is what every other failure here already does. SQLite's writer pool is
`MaxOpenConns(1)`, so the workers queue at the Go level rather than
racing for the file.

Five things about the marks are load-bearing. **A mark records that the
upstream was asked, not that it answered with something.** ListenBrainz
returns 200 and `[]` for an artist it has no popularity data for — which
is most of a long-tail library, and the same is true of an artist whose
every row falls under `indexMinPopularity` — and keying
`discog_fetched` on "did rows come back" made those artists permanent
candidates: "Filling in artist details" re-ran for up to
`discogBackfillMaxPerRun` of them on **every launch**, forever, doing
the same two fetches to the same empty answer. So `indexOneArtist` sets
the mark when both fetches *succeeded* (`fetchTopReleaseGroups` and
`fetchTopRecordings` return an error for that reason), and only a real
failure — transport, non-2xx, unreadable body — leaves the artist for
the next run. `browseFullDiscography` already had this right: an artist
with genuinely no release groups is still marked browsed. The marks are **a table, not
more `explore_index` columns**, because `artifactimport.go` merges the
downloaded catalog by column list — a flag added there is a second
place to remember, and forgetting it silently wipes every mark on the
next catalog update. `discog_fetched` stays in `explore_index` for the
opposite reason: the artifact legitimately answers it for artists it
covers. **The artifact answering it is not "we have their
discography"** — its per-artist coverage is graded, so an artist can
arrive `discog_fetched = 1` and never have been browsed, which is why
the unenriched query ORs its conditions instead of testing the
first. **`BrowseReleaseGroupsAll` pages to exhaustion** where
`BrowseReleaseGroups` asks for `MaxLimit` once and takes what comes
back — a prolific artist was silently cut at 100 release groups, and a
hundred albums looks like a complete answer unless you count. And the
per-artist mark **replaced a heuristic that could never be satisfied**:
`BrowseReleaseGroups` used to re-browse whenever no indexed row carried
a secondary type, which is permanently true for an artist whose
releases are all plain albums.

**One artist portrait is downloaded; the rest are remembered as URLs.**
`resolveAllSources` asked five upstreams what images they had for an
artist and then downloaded **every** candidate, up to ten, full size,
serially — while nothing in the app has ever read anything but
`primary.jpg` and its three tiers. Measured on a real cache: 5.3 GB,
of which 4.1 GB was candidates no code path can reach, ~940 kB per
artist against the ~217 kB that is actually used.

It is split now. `resolveCandidates` does the metadata lookups and
returns an ordered list; `fetchPrimary` walks that list downloading
until one **succeeds**, makes that the primary, and records the rest
with an empty `file_path` — known, not fetched — so replacing a
portrait later is one download rather than five lookups again. Taking
the first that succeeds rather than the first outright is also a fix:
the old loop keyed `is_primary` on the index, so a failed candidate 0
left the artist with a stored image, no `primary.jpg`, and a `.miss`
marker claiming there was no artwork at all. The winning candidate is
no longer also written under its own name, since `setPrimary` writes
the same bytes to `primary.jpg`.

Two janitor jobs go with it, and the first is why the waste survived.
`OrphanedArtistImagesJob` joined the bare MBID onto the images
directory — but artist directories are **sharded** under a two-character
prefix, so it named a path that has never existed, `RemoveAll`
succeeded on it, and the job deleted the rows that were the only record
of the files it left behind. `explore.ArtistImageDir` is that layout's
one definition, passed in the way `OrphanedCoverFilesJob` takes
`expandVariants`, and the test lays its fixtures out with it — a test
that invents its own flat layout agrees with the bug.
`StrayArtistImageFilesJob` reclaims what earlier versions downloaded,
keeping only `explore.ArtistImageKeepNames()` and refusing an empty
keep set for the reason the covers sweep refuses an empty live set.

**An age is not a ceiling, and a cache needs one.** Art for an artist
the user owns is kept indefinitely; everything else aged out after 90
days and nothing counted it, so the same install held portraits for
**5,770 artists in a 1,301-artist library** — every artist page opened
in Explore fetches one, and a browsing afternoon is entirely inside the
retention window. `browsedArtBudget` (256 MB) is the second pass:
oldest browsed artist first, until what is left fits, with owned
artists outside the budget entirely. `httpCacheBudget` is the same rule
one cache over, and it is what makes the year-long entity TTL below
safe — once answers stop expiring, expiry stops being a bound.

**"Owned" is a file here too.** The sweep's live set used to be "there
is an `artists` row", which the file-shaped schema made meaningless;
it joins `audio_files` now, like every other ownership question. Its
test had seeded an artists row with no file and called it owned — the
exact phantom, in the fixture of the test that guards it.

**Only the tiers of a cover are stored.** `saveCoverArt` writes
`_sm`/`_md`/`_lg` and records the largest as `cover_art.file_path`;
`coverart.ResolveURLs` reports that one as `Original` too, because it
is the largest kept. The full-resolution image used to be written
beside them and was **1,134 MB of a 1.4 GB covers directory** against
110 MB for all three tiers — with nothing rendering it, since the grid
caps at 350 px and the largest tier is 400. The bytes are still in the
audio file, which is where they came from, so the repair pass that
regenerated tiers *from the stored original* went with it.

**Frontend** (`frontend/`): Lit 3.2 web components + Web Awesome UI library + HTMX. State management via singleton reactive stores in `src/store/`. Wails bindings auto-generated as TypeScript in `frontend/bindings/`, nested by Go import path — don't edit by hand. The `@go` alias absorbs the constant prefix, so a call site imports `@go/library/library.js`.

**One seam states what the generated types get wrong, rather than 78 patches.** v3's generator is honest where v2's lied: a Go `nil` slice marshals to JSON `null` and always has (v2 typed it `T[]`), and a Go named string type is a closed set (v2 typed it `string`). There is no flag to turn either off, correctly. So `utils/binding.ts` states the app's actual contract at the only place it is true — `list` yields `[]` for a nil slice, `dict`/`dictByName` yield `{}` for a nil map and drop null-valued keys (which loses nothing: `noUncheckedIndexedAccess` already makes every read `V | undefined`), and `compact` is the same for a map arriving as a *field*. Where a nullable slice is a model field there is no boundary to put it at, and those are `?? []` at the point of use.

All three also return a **plain `Promise`**: v3 bindings return a `CancellablePromise` and nothing in this app cancels one, so letting it inward would put a Wails type in every store signature for a capability none of them use.

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

**A navigation is a history entry, and that is the whole back stack.**
`index.ts` records each navigation with `pushState` (same URL — the app
has no routes, and a path a reload cannot resolve is worse than none)
and replays `popstate` with `_isBack`. It exists for Android, whose back
button is not a key the page can bind: the scaffold's
`MainActivity.onBackPressed` asks `webView.canGoBack()` and finishes the
activity otherwise, so an app that never touched `history` quit from any
depth — which is what a device reported. Hooking the platform's own
mechanism rather than adding a JNI callback is also what makes it
testable in a browser (`page.goBack()`), and the Java half needed no
change at all.

Two rules hold it up. The **first** navigation *replaces* the launch
entry rather than pushing one, or every launch costs a back press before
the app will close. **There are two launch navigations**, which is what
defeated that rule for five phases: the eager `navigate → home` at the
foot of `index.ts` and the configured page `GetDefaultPage()` resolves
to later. Only the first replaced, so a fresh session was already one
entry deep, the first back press replayed home over home, and on Android
`canGoBack()` was true so the press that should have exited the app did
nothing (#142). The landing-page navigation carries `_replace`, honoured
only while still at index 0 — past that the user has navigated during
the backend call, and a slow answer must not overwrite an entry they
made. And the in-app back buttons (`navigate-back`, fired
by the detail views and `now-playing-view`) go through `history.back()`
rather than a stack of their own: the old `navStack` is **deleted**, not
kept beside it, because two stacks is precisely how a view's own back
button and the phone's gesture come to disagree about what one press
means.

**And there is one statement of which view is active**, for the same
reason: `popstate` calls `handleNavigate()` directly and dispatches no
`navigate`, so the two nav components — which learned the active view
from that event — kept highlighting the view the user had just *left*.
`store/active-view-store.ts` is the shell saying where the user is, and
both navs read it through `ActiveViewController` rather than holding an
`activeView` of their own.

Four things about it are load-bearing.

**"Please go to X" and "the active view is now X" are different
statements**, and only the first existed — dispatched from 28 call
sites across 18 files. A re-dispatch from inside `handleNavigate` is
not the fix and cannot be: that function is the `document` listener for
`navigate`, so it is an infinite loop.

**It is a store rather than an event, because a component that mounts
after a navigation still has to know.** `bottom-nav`'s "More" sheet
creates its `<app-sidebar>` on open, and that copy had heard no
`navigate` at all — standing on Albums, it opened highlighting
Home. An event has no answer for a listener that was not there.

**A detail view is not a view here**, so the destination it was opened
from stays lit. `app-sidebar` did that by accident (it guarded on
`navItems.some(...)`, so an unmatched name left its highlight alone)
and `bottom-nav` had no such guard and so lit *nothing* — which is why
one looked right and the other looked broken on the same screen.
Whether a view is primary is the shell's fact: `view in VIEW_TAGS` is
passed to `setView`, never re-derived, because a second copy of that
list is a second thing to forget.

**Nothing is lit until the shell has navigated.** The store starts
empty rather than defaulting to `home`, which is what `app-sidebar`'s
field used to do to match the landing view — a default that is correct
only while `GetDefaultPage()` agrees with it.

**Back and forward are chrome, and the depth is the shell's own
count.** `<nav-history>` in the top bar is #6: the stack was always
global — every navigation is an entry and `popstate` restores any of
them in either direction — so what was missing was an affordance, since
the only way back was a detail view's own button, which leaves the
screen with the view it belongs to. The buttons dispatch
`navigate-back` / `navigate-forward` and the shell owns both guards,
for the reason the old `navStack` was deleted: a second caller reaching
for `history` is how two stacks come to disagree.

Three things about it are load-bearing. **Forward is not back
negated**, so the single `pushedEntries` counter could not express it —
`popstate` carries no direction and fires identically both ways, so a
counter decremented on every pop reads a forward as a second back. Each
entry carries its index (`yjIdx`) and the shell keeps the current one
and a high-water mark; that also survives a jump of more than one,
which `history.go(-n)` and a long-press on a browser's back button both
produce. **A control that cannot act is `disabled` here**, which is the
documented exception to `library-status-indicator`'s rule: the two are
a pair whose positions the user learns, and hiding one moves the other
under the cursor. And **it stands down below 900px** — the top bar is
what runs out of room first below that (it already overflows 600px by
11px, #143), and nothing becomes unreachable: `nav.back` / `nav.forward`
(`Alt+Left` / `Alt+Right`, the browser's own combination, and clear of
the bare arrows that seek) are global at every width, and the phone has
the platform's gesture.

The assertion is `aria-current="page"`, in
`e2e/specs/back-navigation.spec.ts`. That file existed throughout the
bug, covered exactly these journeys, and asserted only
`data-active-view` — the shell's own bookkeeping, which was right the
whole way through — so it was green on the broken build. Same trap as
`layout-overflow.spec.ts` and `page-header`: a spec named for the
behaviour, measuring the plumbing.

**Which destinations exist is configuration, and hiding one takes away
the nav item and nothing else.** Eleven sidebar entries is more than
most libraries need (#25), so each is toggleable from Settings →
Navigation, Autotag is off until asked for, and Downloads is absent
until there is a client to download with — a destination for a feature
that cannot work is worse than none. `navigate` still resolves a hidden
view, which is not a nicety: detail views navigate into these and the
launch page is one of them. Nothing needed a special case for the
highlight either, because the paragraph above moved that onto
`active-view-store`: the sidebar asks `isActive(id)` per *rendered*
item, so a hidden view lights nothing exactly as a detail view does.

Five things about it are load-bearing.

**The stored shape is a map keyed by view id, and an absent key means
that view's own default** (`backend/config.Views`). That is what makes
this need no migration in either direction, and it is the polarity rule
`AllowMeteredCatalogDownload` states: the zero value is the intended
answer. A `HiddenViews []string` cannot express "Autotag off by
default" at all — its zero value is *hide nothing* — and a struct with
a boolean per view turns a view that later stops existing into stored
garbage. Here an unknown key is dropped on load and a view added later
gets its own default rather than being invisible or forcibly visible.
It is also what makes #73's `#25 → #27` order safe rather than
backwards: when Jobs folds into Settings, `jobs = true` in somebody's
config is a key nothing asks about.

**Two states the user could not get out of are refused, in the config
and not in the checkbox.** Settings is never hideable and the launch
page is not hideable while it is the launch page. `config.toml` is
hand-editable, so a disabled checkbox is the affordance and
`SetViewVisible` is the rule — an app that can be locked out of its own
Settings by a typo in TOML is a support problem nobody can debug
remotely. On *load* the launch page is instead un-hidden rather than
refused: there is nobody to tell, and the honest reading of "my launch
page is Autotag" is that this user wants Autotag, not that their launch
page should be silently reset to something they did not choose.

**Downloads is gated at the nav and not in the config**, on
`downloadStore.available`, so switching it on in Settings still means
what it says once a client exists and the tab appears without a restart
(#37's rule). `available` is false until the providers have loaded,
which makes the item *appear* on a fresh launch rather than appearing
and then vanishing.

**The tab bar honours the toggles too, and the reason is local rather
than a general rule about phones.** `PHONE_COLUMN_IDS` is the precedent
for "what a phone shows is a different question", and it would apply —
except that `bottom-nav`'s "More" opens the *same* `<app-sidebar>`,
which filters, so an unfiltered bar would contradict its own sheet one
tap away. Which four tabs is still plan 016's committed subset; this
only removes from it, and "More" is never filtered because it is how
everything else stays reachable.

**A retired destination is the one shape this does not make free.** An
absent visibility key takes its default and an unknown one is dropped,
but `DefaultPage` is a *value*: a launch page naming a view that no
longer exists fails validation, and on the load path that means the app
refuses to start for whoever had it selected. `RetiredViews` is that
list, and `ApplyDefaults` treats a retired name as a zero value while
an unknown-but-not-retired one still errors — a typo is worth being
told about. #27 retiring `jobs` is its first entry.

**The list of destinations is `services/view-meta.ts`**, on
`shortcut-meta.ts`'s pattern, because #25 gave it a second reader:
Settings renders a toggle per view and needs the same labels in the
same order. Which views exist and what an unconfigured install shows is
Go's (`backend/config.Views`, which `DefaultPage`'s validation reads
too, so the launchable set is not a second list); how they are *drawn*
is the frontend's, beside the rest of the icon vocabulary. The binding
returns the **resolved** map for every view, so the frontend holds no
copy of the defaults — which would be the copy that shipped in the
binary rather than the one being edited.

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
- **`focus()` on a surface that has not shown itself is a silent
  no-op**, so the first focus is retried on a *time* budget rather
  than a frame count — the thing being waited for is another
  component's animation. And a retry is not enough on its own for the
  sheet below: `wa-dialog` focuses `[autofocus]` or *itself* on the
  frame after `showModal()`, and it cannot see the first menu item to
  prefer it, because the panel is slotted through `menu-surface` and
  the dialog's own `querySelector` stops at the `<slot>`. The first
  attempt therefore *succeeds* and is then overwritten, which no
  amount of waiting fixes — so the surface announces `menu-shown` when
  it has settled and `MenuKeyboard.refocus()` re-asserts.
- **Focus is only taken back if the menu had it.** A click elsewhere
  closes the menu too, and pulling focus to the row the user
  right-clicked a moment ago is worse than leaving it.
- **Web Awesome keys an item's tabindex and highlight off `active`**, so
  moving focus without setting it leaves the highlight on whichever
  item the mouse last touched.

**And a menu is drawn where it fits: a popup on a desktop, a bottom
sheet on a phone** (#60). `components/menu-surface/` is that one
decision. The host renders the panel it always rendered and slots it
into whichever surface is up, so `ContextMenuController` still drives
`.active` and `.anchor` as though it were talking to a `wa-popup`, and
fourteen call sites changed one tag name each and nothing else.

**It is a correctness fix, not a taste one, and the failure was
measured on the device rather than inferred.** Chrome 113 has no
Popover API, so `wa-popup` takes its own documented fallback and
positions with `strategy: "fixed"`; `.main-panel` carries
`contain: layout style paint`, and paint containment *clips* fixed
descendants. On the reference device the main panel spans 0-318 of a
439px viewport while the open menu spanned 191-401 — three of its seven
items cut off, with no way to reach them. `showModal()` is Chrome 37
and uses the real top layer, so a dialog is immune by construction.

Seven things about it are load-bearing.

**"Dialogs are fine" needed checking, because every other dialog in
this app is mounted in `index.html`** — outside `.main-panel` — so it
was not evidence about one opened from inside a view. A probe dialog
appended to `track-list`'s shadow root paints to y=439, over the mini
player and the tab bar, with the contained ancestor still in place. A
top-layer element's containing block is the viewport, contained
ancestor or not.

**The sheet has to un-do the UA stylesheet.** A native `<dialog>`
carries `max-width: calc(100% - 6px - 2em)` and `margin: auto`, which
drew a 354px panel floating in the middle of a 424px screen.
`max-width: none` plus explicit margins is what makes it a sheet.

**The row sizing lives in `contextMenuStyles`, not in the component.**
The panel is the *host's* light DOM — it stays in the host's shadow
root, so only the host's stylesheet can reach it. `menu-surface` puts
`data-sheet` on the panel and that shared stylesheet does the rest,
which is how fourteen menus went from 29px rows to 48px ones in one
edit.

**A dismissal has to travel back.** `wa-dialog` closes itself on
Escape, which would leave the controller believing the menu is open —
and the failure mode is not a stuck sheet but the *next* long-press
doing nothing, which reads as the gesture breaking. `menu-dismiss` is
that signal; the three surfaces that do not use `ContextMenuController`
bind it themselves.

**A sheet that scrolls says so, and `background-attachment` is what
asks whether it does** (#207). The sheet is capped at 85vh — a surface
covering the whole screen is a page, not a sheet — so a long menu's
body scrolls, and for three phases it scrolled *silently*: measured at
424x439, eight items ended at y=470 with the fold at 439, and where the
cut lands on a row boundary the sheet ends in a clean edge that reads
as the end of the list. The fade is two background layers on
`wa-dialog::part(body)` — a shadow pinned to the box (`scroll`) under a
cover of the sheet's own colour painted at the end of the *content*
(`local`), which scrolls up over the shadow exactly when there is
nothing more to see. So it is absent on a menu that fits, present the
moment one does not, and gone again at the end of the list, with no
scroll listener and nothing reaching into `wa-dialog`'s shadow root for
the scroller. **The curve is steep because the rows under it stay
live**: a scrim over a menu item is that item's text surface, and the
4.5:1 rule applies to it — 32px already down to a quarter strength at
14px spends its weight below the last legible label, measured at 9.9:1
on the light ramp, whose `bgElevated` is `#e9ecef`.

**The playlist submenu is a sheet too, and it had to be.** It is a
`placement="right-start"` flyout, and making the menu full-width moved
its anchor — measured at x −182 to 0, entirely off-screen, so "Add to
Playlist" led nowhere. It stacks as a second sheet over the first,
which is also why `menu-shown` does not re-assert focus while the
submenu is open.

**And which call sites exist is swept, not remembered.** A thirteenth
menu written as a bare `<wa-popup>` works perfectly in every tier here
and is clipped on the device, so `menu-surface.test.ts` reads the
source and fails on one outside a three-file allowlist —
`menu-surface` itself, `job-indicator` (in `.top-bar`, which no
ancestor contains — the contrast that proved the diagnosis on #62) and
`now-playing`'s cover preview (a hover affordance, which a touch device
never opens). **The sweep found two of the fourteen**; twelve were
converted by hand.

**And a menu opens from a finger, through the event it already has.**
`utils/touch-gestures.ts` is one document-capture listener installed
once from `index.ts` — `utils/long-press.ts` until #63 replaced it,
rather than adding a second listener claiming the same 500 ms hold. It
**announces** rather than acts: `yj-tap`, `yj-long-press` and
`yj-swipe-start` are composed and cancelable, and a component claims
one with `preventDefault()`. That is what let #63 reassign the hold
without touching one of the fourteen context menus: an *unclaimed*
`yj-long-press` still becomes a synthetic `contextmenu`, so all six
components that bind one — delegated on a virtualizer, per row, per
card — behave exactly as they did, and only the lists that opt in get
selection mode. The target is `composedPath()[0]` rather than
`elementFromPoint`, which stops at the outermost shadow host and so
reaches a delegated listener and no per-row one; and the click that
ends a *claimed* gesture is swallowed, keyed on the gesture rather than
on a time window so the first tap on the menu it opened is not eaten
too.

Three things about it are load-bearing, and all three were found on the
device rather than in a tier.

**A browser that fires its own long-press `contextmenu` is a trigger,
not a competitor.** Chromium does, WebKit and the WebView vary. The old
rule was to stand down when a trusted one arrived, which was right
while both paths ended in a context menu and is wrong the moment a hold
can mean something else — standing down silently does the *old* thing.
So the gesture is announced from the native event, and only a component
that claims it suppresses that event. Ours and the browser's are told
apart by **identity** rather than `isTrusted`, since no test can
dispatch a trusted event.

**That arrives in either order, and both have to be handled.** The
native `contextmenu` mid-hold is one case; the other is our own 500 ms
timer firing first and Chrome delivering its menu **50–70 ms later**,
which nothing suppressed — measured over four holds on the reference
phone, two took that order, so the context menu opened over the
selection bar intermittently, on the one surface #63 changed. A press
that has produced its outcome therefore suppresses a late
`contextmenu` whichever branch it took.

**A horizontal swipe runs on touch events, and needs two things that
look like one.** Chrome 113's WebView cancels the *pointer* stream
~16 px into any drag — measured at `auto`, `pan-y` and `none` alike,
one or two `pointermove`s and then `pointercancel`, while `touchmove`
kept firing throughout. So the recogniser is `touchmove`, the surface
declares **`touch-action: pan-y`** *and* a claimed swipe calls
**`preventDefault()`** on a non-passive listener. Neither works alone:
with the `preventDefault` in place but `touch-action` back at `auto`
the gesture died after one move, because `auto` lets the browser commit
to a horizontal pan before any threshold can be crossed. `none` is the
value to avoid — it takes the list's own vertical scrolling with it.
**Both are correct in Chromium either way**, which is why this is
written down rather than tested. The tie breaks toward scrolling, in
that order: vertical drift past the tolerance vetoes the swipe for the
rest of the press (a scroll that curves is still a scroll), and a
gesture that is not *strictly* more horizontal than vertical is the
scroller's.

**And what a finger *means* on a row is the inversion of what a mouse
means, decided per event** (#63). A click selects and a double-click
plays; a tap **plays** and a hold enters **selection mode**, in which a
tap toggles. The predicate is `pointerType`, never a viewport width and
never a platform flag — #64's rule, and with #64's warning: keyed on a
width, an Android tablet over 600px gets desktop semantics on a
touchscreen, a touchscreen laptop cannot be described at all, and a
narrow desktop window gets phone semantics with a mouse.

There is deliberately **no double-tap**, which #63 asked for. The first
tap of one is indistinguishable from a single tap until the interval
expires, so tapping would have to wait `DOUBLE_CLICK_GRACE_MS` before
acting — 250ms on top of a measured ~100ms play, 3.5x the app's primary
interaction, to reach a menu a hold already reaches. So the menu and
the action bar are the same surface: `components/selection-bar/` is
presentational (a count and a list of actions, no store, no selection),
`SelectionController` carries the mode for all four selecting surfaces,
and #60's bottom sheet is the overflow behind "More" — so
`contextMenuStyles`, `MenuKeyboard` and `menu-surface` are reused
rather than reimplemented.

Three things about it are load-bearing. **A control inside a row keeps
its own tap**: the gesture is simply not claimed there, so the click
behind it falls through, which is what stops the 44px favourite target
(#56) becoming a 44px play target — the queue row's × is the second
instance. **A swipe right queues**, and its affordance is
`utils/swipe-to-queue.ts` once rather than in each of the three lists
that draw it: the row does not move, its *children* do (a row here is
`contain: strict` with `overflow: hidden`, so a pane held at the row's
original position while the row translates is at a negative offset
inside a clipping box and is not painted), the travel is written to the
row's own style rather than rendered, and the threshold is a fraction
of the row because the row is 424x52 on the reference device. **The
queue panel takes the tap and the hold and refuses the swipe**, because
a right swipe means *add to the queue* everywhere it exists and a queue
row is already in it — the only thing it could mean there is *remove*,
which is the same gesture with the opposite effect one screen away.
A tap there plays that *position*, too: setting the queue to the queue
reads as a no-op and discards its source, its shuffle order and
anything inserted by hand.

Escape leaves the mode, from `selection-bar` rather than from each
host, since that element exists only while the mode does — the same
exception the overlaid queue's Escape is, *a dismissal, not a
shortcut*. The platform's back gesture deliberately does **not** reach
it: the shell owns the history stack and four lists each reaching for
`history` is four stacks, which is the fault `navStack` was deleted
for. That wants one shell-owned register of dismissible surfaces, which
is #200.

**A control revealed by `:hover` is gated on the device having hover,
and which way round depends on whether it is the only route to its
action.** The gate itself is not optional: a touch long-press
synthesises a hover state in the WebView, so every one of these flashed
into view during the 500 ms hold above — a control appearing because
the user was reaching for a different one. Where the action is reachable
another way the control is **absent** on a touch device (the home card's
play button, #68; the queue row's remove, which the row's bottom-sheet
menu carries since #60), and that is `display: none` outside
`(hover: hover) and (pointer: fine)` rather than `opacity: 0` or
`visibility: hidden`, both of which leave a button holding its hit area
and its place in the accessibility tree. Where the control is the
**only** route it is instead always visible under
`@media not all and (hover: hover)` — `track-details`'s cover-art
overlay and remove, `shortcut-capture`'s reset (#137) — because hiding
it takes the action away entirely.

**Always-visible is not the same as always-in-the-way.** The cover-art
overlay is `inset: 0` at 50% black, which is fine as a hover state and
is not fine as the permanent appearance of the artwork being edited —
and it is only a *hint*, since `.cover-art-edit` carries the click and
tapping the art always worked. Off hover it becomes a corner chip in
the remove button's own language. The × beside it stays full-size,
because that one really is the only route to its action.

One thing to know before checking either: **no *committed* tier renders
as a touch device.** CDP's `Emulation.setEmulatedMedia` does not reach
the component tier's iframe, and the e2e projects are Desktop Chrome
and Desktop Safari, neither of which has touch — a Playwright project
using a mobile descriptor would report `hover: none`, so this is a
choice not to carry one rather than a thing that cannot be done. So
`hover-affordance.test.ts` asserts the *parsed stylesheet* — which rule
sits inside which media query — and says so; the regression it exists
for is someone hoisting a rule out of its query as a tidy-up, which
nothing on a desktop renders differently.

**The web view's own tap highlight is gone, and what replaced it is a
press state** (#54). `-webkit-tap-highlight-color` is an *inherited*
property, so one declaration on `html` in `index.css` reaches every
shadow root in the app and takes away the grey box a phone drew over
the bounding rect of whatever was tapped — measured at
`rgba(0, 0, 0, 0.18)` with the rule removed. `user-select` is the same
argument and was already done: `index.css`'s first rule is `*, *::before,
*::after { user-select: none }`, which reaches the shadow roots for the
same reason.

Three things about it are load-bearing.

**Removing the highlight removes the only touch feedback several
surfaces had**, so the press state is part of the same change rather
than a later polish item: the four lists' rows, `bottom-nav`'s tabs,
`app-sidebar`'s destinations (which are also the phone's "More" sheet)
and the shared `contextMenuStyles` menu item all take
`--yj-press-overlay` on `:active`. The cards already had one
(`transform: scale(0.97)`) and are untouched.

**A press selector carries a state class or it does nothing where it
matters.** A row is `.track-row.selected.active`, so a bare
`.track-row:active` is one class short of it and the press is invisible
on exactly the row a phone is most likely to press — the one it has
just selected. The rule is last and lists `.selected:active` /
`.active:active` beside the bare form.

**And the hover tints on those same surfaces moved behind
`(hover: hover) and (pointer: fine)`**, which is #68's gate applied to
a tint rather than to a revealed control and for the same mechanism: a
hold synthesises a hover in the WebView, so an ungated tint arrives
because a finger touched the row and stays there after it has gone —
measured, since with the press rule removed a held row reads
`rgba(255, 255, 255, 0.05)`, the hover tint, rather than nothing.
`touch-action: manipulation` was considered and declined: the 300ms
delay it is offered for is already absent on a `width=device-width`
viewport, and what it would really change is the gesture stack #63
tuned by measurement on a device this session cannot measure.

The split of tiers is `hover-affordance.test.ts`'s: `press-feedback.
test.ts` reads the parsed stylesheet, because `:active` cannot be
forced there either, and `native-touch-feel.spec.ts` *measures* — it
holds the button down on a real row of the real list, and it is the
only tier that loads `index.css` at all.

Three lists had no focused row to open a menu *from* — the queue panel
and both playlist detail views — and gained a roving tab stop through
`utils/roving-rows.ts`. **`track-list` deliberately does not use it**:
its equivalent predates this, carries selection semantics (shift-extend,
ctrl-toggle) the other three do not have, and is pinned by its own
tests.

**A shared panel computes its own name, and its items ask what the
target can do.** `explore-artist-details` had a menu for its top
*tracks* and none on the release cards, which are most of the page. It
has one now on both release shapes — the top section's
`LBTopReleaseGroup` and the discography's `MBReleaseGroup` — normalised
to a `ReleaseMenuTarget` at the moment the menu opens, so the union
does not reach five action handlers. `ctxMenuTarget` is a discriminated
union rather than one nullable field per kind **because the panel is
shared**: that is what keeps `aria-label` moving with the target, which
is the fault `cover-grid` shipped (every menu announced as "Album
actions").

Which items appear is decided by what the release can actually do, and
those are three different questions. Playback is gated on a **local
album id**, not on the badge's "owned": a release matched by MBID with
no local album behind it has nothing to queue, and `GetFilePathsBy‐
Albums` is keyed on the id for the reason the album page is — an owned
but untagged release has no recording MBIDs, so an MBID-keyed lookup
returns nothing while looking entirely correct. The request item needs
the opposite, a catalog MBID, so it is absent for a library-only
release (`local:<n>`, unwrapped the same way `navigateToAlbum` does) —
which is also the one case where wanting it makes no sense.

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

**A colour's role decides whether it can be fixed across themes.** A
*fill* — `--yj-error`, `--yj-success` — is "what colour is a danger
button", which is red in every theme and stays fixed. A *text* colour
— `--yj-error-text` — is "what colour is the word *failed* on this
background", which cannot be: one value cannot clear 4.5:1 against both
a near-black and a near-white surface, and the old fixed set measured
2.31–4.28:1 on nearly all of them. And every fill carries a **computed**
foreground (`--yj-accent-fg`, `--yj-*-fg`) rather than a written-down
one, because the accent is a colour picker: white on the default
`#ffd43b` is 1.43:1. `readableOn()` keeps white where white clears and
flips to black where it does not, and `accentTextOn()` mixes the accent
along its own hue until it clears the ramp's surface — returning it
unchanged on both dark ramps. Two accent buttons used to take their
foreground from `--yj-bg-base`, which *inverts with the ramp*; that is a
token used for the wrong meaning, and it only shows in the theme nobody
looks at.

**Contrast is a property of the ramp, and the ramps are data.**
`theme-store`'s `SHADE_PALETTES` — not `tokens.css.ts`, which holds only
the type scale and icon sizes — is where the colours live, applied to
`:root` at runtime, which is why the `var(--yj-…, #fallback)` at every
call site is dead in practice. Every text colour clears 4.5:1 against
every surface it can sit on, and `theme-contrast.test.ts` computes that
from the table rather than trusting it. Three rules hold it up.
**`bgOverlay` is not a text surface on the dark ramp** — sizing tertiary
to clear it needs a grey lighter than *secondary*, and an inverted ramp
is a worse answer than the problem, so the one component that put text
there uses primary. **A generated colour is a family, not a colour**:
`utils/avatar-color.ts` exists because `hsl(hue, 45%, 35%)` behind white
initials failed for 35 of the 360 hues, so the failure came and went
with how an artist's name hashed — the test walks all 360. And
**`make ui-visual` cannot see any of this**: the component tier renders
the fallbacks, because the theme only reaches `:root` in the real app.

**A stated motion preference outranks an app setting, and the state a
fix lands in is a state nobody has looked at.** `now-playing`'s marquee
ran for as long as a track played with no way to pause it (WCAG 2.2.2),
and the guard is in `shouldScroll()` rather than in CSS: the cycle is a
transition out, a `transitionend` and a transition back, so suppressing
the animation strands the text off its own box with nothing to bring it
back. It covers `hover` as well as `always` — `reduce` is a request
about motion, not about autoplay. The two bugs behind it were both in
the *fallback*: `text-overflow` sat on the outer span while the box that
overflows is the inline-block child, so the non-scrolling state had
never produced an ellipsis **in any mode**, including the default; and
moving the ellipsis to the child stops the parent overflowing, which
silently disabled overflow *detection* and would have stopped anything
scrolling ever again. Both measurements come from the child now. The
first was found by reading a screenshot, the second by the new test's
positive case.

**Roles have to be wired to each other.** `combobox`, `listbox` and
`option` were all present on `<yj-combobox>` and nothing connected them,
so arrowing through nineteen options moved a highlight and announced
nothing. Ids on the listbox and every option, `aria-controls`,
`aria-activedescendant` — and `aria-selected` meaning *chosen*, which is
the distinction the pattern rests on: the highlight is what
`activedescendant` points at. Unlike `config-section`'s disclosure this
IDREF may dangle while closed, because the popup genuinely does not
exist then and `aria-expanded` says so. Checked against
`Accessibility.getFullAXTree`, not against a snapshot — and read the
whole property, since `activedescendant` reports `value.type: "idref"`
and an extraction expecting a string reports `(none)` on a working
build.

**The queue's order is reachable from the keyboard.** Alt+ArrowUp/Down
moves the focused row, with a live region saying where it went. It is
in `queue-panel`'s own *delegated* keydown beside Enter and the roving
arrows, not a backend panel binding: it cannot collide with the global
Up/Down volume bindings, and a reordering key does not belong in a
user-editable table where it could be rebound onto something
unmodified. Two things in it are load-bearing. **The index arithmetic
is not symmetric** — `MoveQueueTracks` takes an index into the array
*before* the move, so down-by-one asks for `i + 2`, because `i + 1` is
where the row already is once its own removal is accounted for and the
backend's contiguous-block guard correctly treats it as a no-op. And
**the index comes off the row the event came from**: `focusedIndex` was
only ever moved by an arrow key, so a row reached by a click or by Tab
left it at 0 and `Enter` played the first track in the queue from any
focused row.

**A name is computed where the role is, and that is rarely where you
wrote it.** Four surfaces wrote a name somewhere the accessibility tree
never looked. `wa-slider` puts `role="slider"` on a div in its own
shadow root pointing `aria-labelledby` at an empty internal `<label>`,
which outranks the host's `aria-label` — so both sliders computed a
name of `""`, and `a11y.md` lists both under *what is already correct*.
The name comes from `label` now, the library's own API, and
`styles/wa-slider-label.css.ts` hides it: preferred over reaching into
the shadow root the way `name-dialog.ts` must, because if Web Awesome
renames the part the label becomes *visible and correctly named*
rather than silently nameless. Its second rule is load-bearing —
`#slider` takes an 8px margin the moment a label exists, which grows
the bar 6px → 14px and moves the transport with it.
`wa-progress-bar`'s `label` *is* an `aria-label` and is invisible, so
there it is just the right attribute.

The same thing in the light DOM: `config-field` rendered a `<label>`
as a **sibling** with no `for`, so **24 of 93 controls on Settings**
computed an empty name. They use `for`/`id` (a fixed id, safe only
because each field is its own shadow root) rather than `aria-label`,
for what it buys beyond the name — the label text becomes a click
target. And three surfaces are named but identify nothing, which is
the same fault one step milder: three shortcut buttons announced
themselves as "S", thirty-six column arrows as "Move up", and every
queue row's remove button as "Remove from queue".

**Checking any of this needs the browser's own answer, and "0 unnamed"
is not it.** A `placeholder` is an accname fallback, so an
`Accessibility.getFullAXTree` sweep of all eleven views reported
Explore's search box — the audit's own `a11y.26` — as clean. A sweep
for *empty* names cannot see a *weak* one.

**The shell scrolls sideways and not down.** `body` is
`overflow-x: auto; overflow-y: hidden`, and both halves are measured.
Vertically there is nothing to fix: the middle grid row is `1fr` and
absorbs the 4em bars exactly — at 200% text on an 800×600 window the
bars go 64 → 128px, the main panel 472 → 344px, and the footer still
lands on 600. Horizontally the shell is 784px inside a 320px viewport
(400% page zoom, the width WCAG 1.4.10 names) and 464px of it,
including the job indicator and the queue button, used to sit behind
`overflow: hidden`. Keeping the vertical axis fixed is what keeps the
transport where a desktop player's transport belongs. At every size
this app promises, no scrollbar appears. Note that `overflow: hidden`
still permits *programmatic* scrolling, so a probe that sets
`scrollLeft` passes on the broken build; the spec uses a wheel gesture.

**Below 600px it reflows instead, and that is the phone.** The sideways
scroll above was the concession available while the shell had one
layout; plan 016 B2 gives it a second. Under 600px the grid drops its
sidebar column *and* (since #57) its top-bar row, `<bottom-nav>` takes
over as the primary navigation, and the shell measures
exactly 320px in a 320px viewport — so `layout-overflow.spec.ts` now
asserts *nothing needs scrolling to*, which is what WCAG 1.4.10 wanted
all along. 600 rather than the sidebar's 900 because 900 is a laptop:
the answer there is a narrower sidebar, which is still a sidebar.

Three rules in it are load-bearing, and the second cost 30 specs.

**A grid item's implicit minimum is its content**, so one child that
insists on 580px makes the *body* 580px wide inside a 360px viewport
and `overflow-x: hidden` then hides a third of the app rather than
fitting it. Every box between the viewport and the content that must
shrink carries `min-width: 0`, and the things that cannot shrink say so
in their own stylesheet — `search-bar`'s 200px floor, `job-indicator`'s
label, `audio-player`'s seek bar and volume. A media query inside a
shadow root is answered by the viewport, so a component states what it
drops at phone width itself rather than the shell reaching in.

**A duplicated component duplicates its handles.** `bottom-nav`'s
"More" opens the *same* `<app-sidebar>` in a `wa-drawer` rather than
listing the destinations again — but rendering it unconditionally put a
second copy of every `data-testid="nav-*"` in the DOM, and 30 existing
specs failed with "strict mode violation: resolved to 2 elements" on a
desktop viewport where the element is not even visible. It renders only
while the sheet is open, and `bottom-nav.test.ts` asserts its absence
before that.

**The tab bar is four destinations and a way to the rest.** Three to
five is where touch targets stop being thumb-sized; eleven over 360px
is 32px each. Which four is plan 016's committed subset, and everything
else — Settings included, because a phone still needs it — is behind
"More".

**And "More" rises from the bottom, on #60's sheet rather than a
second one** (#71). It was a `wa-drawer placement="start"`: a 200px
column of a 424px screen, opening away from the thumb that asked for
it, with the rest of its 400px band empty. It is the *same element*
with `placement="bottom"` and `without-header`, which is what keeps
the change to where it comes from — `wa-drawer` renders a native
`<dialog>` and opens it with `showModal()`, so #60's containment
finding carries over with nothing new to prove, and the focus trap,
Escape, tap-outside and `wa-after-hide` all come along. Measured at
424x439: 424 wide, 373 tall (85vh, so there is an outside to tap),
48px rows.

Three things about it are load-bearing. **The sidebar is mounted
rather than re-listed as data**, which the issue offers as the
alternative: the shell's own `<app-sidebar>` is `display: none` below
600px rather than removed, so a second list drawing `nav-*` handles is
the duplication above, and it would be a second place to add the next
view to. **There is one scroller, and it is the sheet's body** — the
reported "only part of the screen scrolls under my finger" is three
nested ones (the dialog, its body, and the sidebar's own
`overflow-y: auto` host), so which box a drag moves depends on where
the finger landed; `overscroll-behavior: contain` is the other half.
And **`expanded` means the host owns the box, not just the labels**:
`app-sidebar` writes an *inline* width and caps itself at 400px, which
beats any rule the host could write, so the width, the scrolling and
the mouse-only resize handle all follow that attribute.

**There are three supported size bands, and the queue is part of the
promise.** Plan 018 (#24) wrote them down: **Phone** below 600 (bottom
nav, reflows, fits 320px exactly), **Compact** 600–899 (icon sidebar),
**Desktop** from 900 (labelled sidebar) — plus one sentence across all
three, *no action is ever unreachable at any supported size*. The bands
themselves already existed; what was new is that they are a promise and
that the queue panel is inside it.

**And below 600px there is no top bar at all** (#57). The row is gone
from the phone's grid template — not the header hidden, the row deleted
— which is 3.25em of a 439 CSS px viewport, the single biggest vertical
win the reference device has to give. Each of its five children has
somewhere else to be there: `nav-history` is the platform's own back
gesture (already gone from 899 down), the job indicator is `<job-band>`
(#62, which is why this was blocked on it), the search box is a
`wa-dialog` opened from the view's own header, the library filter is
Settings → Libraries (#148), and the wordmark stays exactly where it is.

Three things about it are load-bearing. **The header is visually hidden
rather than `display: none`**, because that `h1` is the document's
top-level heading and several pages have no other one — `page-header`
renders no `h1` when `heading` is `''`, and Settings has no
`page-header` at all. Its four *controls* are `display: none` inside it,
which is what keeps them out of the tab order: a visually-hidden
container is still focusable, and tabbing into a search box nobody can
see is worse than not having one. **The fit pass stands down**, from the
bar's computed `position` rather than from a width — with the bar out of
flow there is no content box to measure children against, and a pass
that ran would collapse the wordmark every time and report success about
a 1px box. And **`top-bar-fit.spec.ts` keeps 390 in its list and asserts
the stronger property there**: "nothing hangs out of the bar" is
trivially true of a bar with no row, and would have passed on a build
that merely broke it, so what that width asks now is that the content
starts where the row above it ends.

**Above 600px the top bar decides what it can afford, and what it gives
up is never an action.** Its five children do not fit at the bottom of
the Compact band: the bar was 611px inside a 600px viewport idle and **862px while
a scan ran**, because `job-indicator` is `hidden` when idle and 235px
wide showing a real library's scan title (#143). So `services/
top-bar-fit.ts` is `page-header`'s treatment one bar up — a
ResizeObserver, every pass starting from all-visible, hiding the
lowest-priority child until it fits.

Five things about it are load-bearing.

**It is measured rather than breakpointed for a reason specific to this
bar**: three of its five children are as wide as their *content* — the
library filter is a `<select>` sized by the longest library name, the
indicator by the running job's title, the search box by its view-scoped
placeholder — so any width picked is right for one library, one job and
one view. Swept with a long-titled scan staged, the bar overflowed at
**every** width from 600 to 899 *and* at 900 where `nav-history`
appears, while 899 fits; a breakpoint fixing "600 to 610" would have
fixed whichever case happened to be idle when it was measured.

**What yields is decided by the promise above, which rules out the two
cheapest answers.** Hiding the library filter takes away an action —
`library-filter` was the only control in the app that called
`setSelectedLibrary` — so it trades this promise for the same promise.
That is #148, and #57 fixed it by giving the selection a *second
placement* rather than a second definition: the same component, in
Settings → Libraries under a "Showing" label, at every width. A
phone-only copy was the obvious cheaper answer and is the fault, not the
fix — "where do I change which library I am browsing" having two answers
by viewport is exactly what one control in two places avoids.
Collapsing the search box to an icon is what #57 wanted and #57 was
blocked behind #62, so building it here would have been building it
without the thing that blocked it. The two that yield are the two that are **not** actions: the wordmark, which the
window's own title bar repeats and which #48 wants down to "YJ" at
every width anyway, and then the job indicator's *label*, leaving the
ring — which is not a new judgement, since the component already drops
it below 600px and its `sr-only` live region is what announces the
state either way.

**The wordmark yields its width, not its existence.** The collapsed
rule is visually-hidden rather than `display: none`, because that `h1`
is the document's top-level heading as well as the brand.

**"Fits" is the children against the content box, and `scrollWidth`
cannot express it.** `scrollWidth` counts a box's left padding and not
its right, so with 2em gutters it under-reports by 32px: the first fix
read `700/700` — a perfect fit — with the indicator sitting in the
whole right gutter. Same family as #69's title trap, and found only
because `top-bar-fit.spec.ts` measures **per child**, which is what
`layout-overflow.spec.ts` cannot do and why that spec was green
throughout the defect.

And **the bar does not resize when a job starts**, which is the case the
whole thing is for — a ResizeObserver on the header alone never fires,
so every element child is observed too.

**The bottom bar is three columns whose outer two are the same width,
and that is what "centred" means.** It was `320px 1fr auto`, so the
transport sat in the middle of the space the metadata and the queue
button did not use — its centre was ~140px right of the window's at
every size (#23). The outer tracks are now the same expression, so the
middle one is centred by construction rather than by arithmetic that
has to be redone whenever a control joins the bar.

Four things about it are load-bearing.

**The side width is the metadata's, capped at a quarter of the bar**,
and the cap is not tidiness — it was measured as a regression first.
Reserving the full `--now-playing-width` on *both* sides costs the
transport twice: at 800px the outer pair wanted 640 of 800 and the seek
bar's track fell from **257px to 61px**, and to 0 at 200% text. The
control you drag was being squeezed to centre the buttons above it.
With the cap it is 246px at 800, which is parity with the uncentred
layout.

**The cap is a `min()` rather than a breakpoint** because
`--now-playing-width` is *user state* — the metadata panel has a drag
handle — and the same reasoning the queue panel's overlay mode uses
applies: a rule that assumed the default 320 would be wrong by whatever
the user dragged. Tying both sides to that variable is also what keeps
the handle meaningful; a plain `1fr … 1fr` would centre the transport
just as well and silently make dragging a no-op.

**The volume moved out of `audio-player` and into the bar** (#42),
because the transport column has to hold the transport and nothing
else or "centred" means centred with a slider bolted to one side. It
lives in `.bar-end` with the queue button — one cell, not two columns,
since the centring compares *columns* and a separate volume track would
make the outer pair unequal by whatever the slider measures.

And **the slider is inline by default, with the popup as a setting**
whose stored flag names the *popup*: `backend/config`'s polarity rule,
where the zero value has to be the intended answer, so an existing
`config.toml` with no key gets the new default without a migration.
Inline, the icon becomes the mute toggle and is named after that action
rather than after the state, because with the slider beside it there is
nothing left to disclose. The bar's copy stands down below 600px, which
is about *room*: five controls and a slider do not fit a 360px bar, and
`now-playing-view` is where seeking and volume go on a phone.

**Whether there is a volume to control at all is a different question,
and it is asked of the player** (#64). On Android the hardware keys are
the volume control and the framework mixes our stream against the
device level, so `player`'s own level is pinned at maximum, `SetVolume`
/ `ChangeVolume` / `MuteToggle` are refused, and `volume-control`
renders `nothing` — in both of its mount points, at every width.
`mediacontrols`' Android handler implementing no volume callback is the
same fact one layer down.

Five things about it are load-bearing.

**It could not be a width, and that is not a preference.** Every other
stand-down rule in this app is keyed on a viewport, because a width is
what a browser can answer and what every tier can test. This one is a
property of the build: keyed on width, an Android *tablet* at 600px or
more draws the bottom bar's slider over a pinned level — a control that
cannot act, on exactly the platform the rule exists for, which
`library-status-indicator` already settled is worse than none. The
same rule is wrong in the other direction below 600px, where a narrow
desktop window has no hardware keys to fall back on.

**The predicate is named after the capability, not the platform.**
`SystemOwnsVolume` is what the frontend asks; `platformOwnsVolume` is
the one build-tagged constant behind it, in two files that declare
nothing else. That is `mediacontrols`' split with
`androidpayload.go`'s reasoning: a tagged file is compiled by nothing
`make lint` or `make test` runs, so everything decidable off a phone is
decided against `Player.systemVolume`, a field a test sets either way.
The frontend's absent branch is therefore testable in the component
tier with a stubbed binding, and the constant itself is covered by a
source sweep plus, once, a real arm64 device answering `true` — which
is the only tier that compiles the `android` file at all.

**Mute goes with it, because it is a level of zero by another name** —
and because with no control rendered it is the one state on such a
platform the user could not get out of.

**Nothing persists a level nobody chose.** The maximum the player runs
at is synthetic, so `restoreStateLocked` *remembers* the stored volume
instead of applying it and `saveState` writes that same value back.
The alternative — a second query that omits the column — buys nothing
and is a second write path to keep in step.

**And ducking is untouched, which is what makes the pin safe.**
`SetDuck` applies its attenuation by re-applying the *user's* level
through `setVolumeLocked`, so pinning that level to maximum leaves the
offset arithmetic exactly as it was. It is the only thing that may move
the output on such a platform, and it is the one volume-shaped path
that is not refused.

One thing to know before anyone offers to test it on a phone: **the
duck is unreachable above API 25.** `WailsForegroundService` builds its
`AudioFocusRequest` without `setWillPauseWhenDucked` from Oreo, so the
framework attenuates us itself and never sends
`AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK` — a device confirms it by logging
`requestAudioFocus() … flags=0x0`. `minSdk` is 21, so this is live code
rather than dead, on Android 5.0 to 7.1 and nowhere else.

**And below 600px that bar carries three controls, not five** (#59).
Shuffle, repeat and the queue button leave it; what is left is art,
title/artist, favourite, and prev/play/next. `player-controls` is one
component in two places and **the context is a property rather than a
media query**, which is the exception to the rule two paragraphs down:
on a phone the bar wants three controls and `now-playing-view` wants
five, larger still, *at the same viewport* — so the host states the
context and the viewport states the size band, and neither alone can
express it. Sizes come from `--yj-control-*` custom properties set per
context; play/pause alone goes above the 44px floor, because a row of
identical squares says every action is equally likely and that is not
true of play. Measured before #56: every one of them was **33×21px**,
and the mini bar's favourite was **18×14**, the smallest control in the
app.

Four things about it are load-bearing.

**The phone draws three buttons rather than hiding two**, from
`matchMedia` — `job-band`'s pattern, and the rule that a decision about
whether an element *exists* is not a stylesheet's to make. A
`display: none` control is still in the shadow root and still something
a positional query finds, so "the phone has three controls" would have
been true of the pixels and false of the element.

**Removing a control is only allowed because it is still reachable.**
Plan 018's matrix promises no action is unreachable at any supported
size, and all three are on `now-playing-view`, one tap away through the
mini player's art. That promise is what `phone-transport.spec.ts`
asserts — it walks the route — rather than counting buttons.

**So the route to Now Playing must not depend on what is playing**, and
it did. `now-playing` renders two branches and the no-track one had no
`.expand` button on its placeholder, so with nothing loaded there was
no way to the full-screen view — which, once the queue button left the
bar, made the *queue* unreachable. The queue is persisted across
restarts, so "tracks queued, nothing playing" is a state the app
launches into.

**The desktop bar is untouched and a spec says so with a literal.**
Both issues are `Platform/Android`. The trap is that a `<button>` does
not inherit its font from its parent — the UA stylesheet gives it one —
so a generic `font-size: inherit` is not the no-op it reads as: it took
every desktop button from 33×21 to 36×24, silently. The sizes are
asserted as `'33x21'` rather than as a range, because the regression
was three pixels.

**What that bar lost is how far through the song it is, and
`<player-progress-line>` is where it went** (#58). Plan 016 B2 took the
seek bar off the phone's transport, so the one thing a mini player is
expected to say without being opened had nowhere left to be said. It is
a 2px line on the border between the mini player and the tab bar: the
**shell's** element and its own `auto` grid row between `bottom-bar`
and `bottom-nav`, because those two are separate components and either
one drawing it means reaching into the other's box for two pixels.

Four things about it are load-bearing. **It never counts** — the fill is
`scaleX()` off the same `PlaybackPositionChanged` the seek bar renders,
with the same `trackChangeId` and `seq` guards and an interval that is
stopped and restarted by every report, which is the rule that exists
because a local clock drifted 30 s away across four keyboard seeks.
**It is not a control and cannot become one**: `aria-hidden` on the host
and `pointer-events: none` throughout, because Now Playing's seek bar
is what announces the position and a 2px strip on the top edge of the
tab bar is exactly where a thumb aiming at a tab lands. **It renders
nothing above 600px**, from `matchMedia` rather than a media query, for
`job-band`'s reason plus one of its own — a stylesheet cannot stop a
1 Hz interval running for the life of every desktop session about a
line nobody can see. And **its phone rule sits at the foot of
`index.css`, beside `job-band`'s**, not in the phone block above: a
media query adds no specificity, so a `display: block` written before
the `display: none` that takes it out of the desktop grid loses to it
and the line never appears at any width, silently.

**900 is the worst desktop width, not the 800×600 minimum.** The
sidebar collapses to icons *below* 900, so the main panel is 843px at
899 and 700px at 900 — the narrowest content area any desktop width
produces is at the top of the Compact band, not at the enforced floor.
Every viewport list that stopped at "the minimum" was therefore missing
its own worst case, which is why `layout-overflow.spec.ts` carries 900
now. And **both reasons in `MinWidth`'s comment had expired** — the
subtitle is `display: none` from 899 down and the sidebar host scrolls
(`overflow-y: auto`; at 600×460 its `scrollHeight` is 434 against a
332px client) — so 800×600 is a *comfort* floor for desktop chrome and
not a correctness one. Below it the phone layout takes over, which is
also why a very small window reflows rather than becoming a
mini-player: **#12 is a second always-on-top window, not a mode of this
one**, and making it a mode would discard navigation state on a resize
and put the process-level MPRIS question on a path a drag can trigger.

**The queue panel is a column only while the content can spare the
width, and that cannot be a media query.** In flow the host is
`flex-shrink: 0`, so an open queue is paid for by the main panel: it
left 379px at 900×600 (with all three of the Playlists header's actions
clipped), 69px at 390, and **0px** at 320 — the content was not
degraded but gone. It goes to an overlay with a scrim when
`available - panelWidth < 480`, where `available` is
`.content-area`'s width and therefore already accounts for the
sidebar's collapse.

Four things about it are load-bearing. **The mode is computed, not
breakpointed**, because the panel's width is user state — drag-resizable
200–500px and persisted — so a viewport breakpoint silently assumes the
default 320 and is wrong by up to 180px in the direction that hurts;
widening the panel at a fixed window size must flip it, and
`queue-overlay-mode.test.ts` is written around exactly that. **480 is a
judgement and says so**: there is no cliff to derive it from (the track
list rescales continuously, 213px to 124px columns with no row
overflow), so it is anchored to keep the default 1100px window inline
and put every measured-broken case on the overlay side. **The scrim
covers the content area only** — not the sidebar or the transport —
because the queue is not modal, and it is subtle on a dark ramp by
arithmetic rather than by accident (33,37,41 → 18,20,23). And **the
overlay is a presentation, not a fork**: #55 asks for one component
with two mount points, so the roving tab stop, Alt+Arrow reorder, drag
reorder, selection semantics and `virtualizer.requestUpdate()` all come
along untouched. Escape closes it and returns focus, and is attached
only while the overlay is up — it is a dismissal, not a shortcut, which
is why it is not a panel-scoped binding.

**And an overlaid queue is a place, which is the whole of #55.** The
pixels were already right: measured at the reference device's 424×439,
the overlaid panel is 424×318 — `.main-panel`'s rect exactly — so a
`DETAIL_LOADERS` mount would draw the same rectangle in the same spot.
What was missing was the navigation model, and the defect was one
measurement: opening the queue on Artists and pressing back moved the
page *underneath* to Albums and left the queue up. So opening an
**overlay** queue dispatches `navigate {view: 'queue'}` and opening a
**column** sets the attribute as it always did — `utils/open-queue.ts`
is that one decision, and both routes end at the same `open` attribute
on the same element.

Five things about it are load-bearing.

**The queue is a screen exactly while it is an overlay**, which is the
rule above rather than a second one: a column is a thing the user
docked, so back must not undock it and a navigation must not take it
away, while an overlay is covering the content and has to answer the
platform's gesture. That also inherits the *computed, not
breakpointed* property for free — the panel is drag-resizable, so a
viewport breakpoint would be wrong by up to 180px.

**It is in neither `VIEW_TAGS` nor `DETAIL_LOADERS`**, because there is
nothing to mount; the panel is already in the document. That is not
tidiness. `.main-panel > *` is paint-contained under a `.main-panel`
that is, and `contain: paint` clips the `position: fixed` a `wa-popup`
falls back to on the reference device's Chrome 113 (#60) — so the
detail-view mount asked for in #55's Direction would have broken
`queue-panel`'s working context menu on the one device the issue is
about. Measured: the panel's ancestry is `layout style` all the way to
`body`; a view inside the main panel is `content` under `content`.
**No tier here can see that consequence** — CI's Chromium and WebKit
both have the Popover API — so `queue-as-a-screen.spec.ts` asserts the
*mechanism*, that the panel is not under a paint-contained ancestor.

**A navigation to `queue` deliberately writes neither
`dataset.activeView` nor `searchStore.setCurrentView`**, because both
describe what is *in* the main panel and the queue covers that panel
without replacing it. It publishes itself through `activeViewStore`
with `isPrimary: false`, so the tab it was opened from stays lit —
the same rule a detail view gets.

**The entry is unwound from the panel's `open` attribute**, in the
mutation observer `index.ts` already ran for `aria-expanded`, rather
than at each of the four ways out. Escape, the scrim, the close button
and the toggle all take that route, and a fifth added later gets it
free. Without it the entry is orphaned and the *next* back press is the
one that closes the queue — the reported defect moved one press later,
which looks exactly like a press that did nothing.

**And the way out is 44px on a phone.** With the panel spanning the
whole width there is no scrim there at all, so the close button is the
only pointer route out of a full-screen surface; it was **25×21px**.

**The scrim is drawn only where it can be tapped** (#171). Below 600px
`.panel-content` is `width: 100%`, so the scrim sat entirely underneath
an opaque panel — measured at 424×439, host, panel and scrim all
424×318 — dimming nothing and dismissing nothing while wearing
`cursor: pointer`. #24's tap-outside-to-close cannot exist on a surface
with no outside, and the screen above is what answers it instead: back,
and a 44px close button. The alternative — a gutter, which is the
drawer pattern — was declined, because it buys the affordance by taking
width off a full-screen surface on a 424px viewport. Two things about
it are load-bearing. Its **existence** is `matchMedia`, not
`display: none`, on `job-band`'s rule: a hidden scrim is still an
element carrying the dismissal handler. And **the 600–899 band is
untouched**, where the panel is a 320px column of a wider content area
and the scrim has real uncovered pixels — which is why the e2e half
asserts *absence* at 424×439 rather than clicking, since a phone-width
case that clicks the scrim's centre hits the panel and passes on the
broken build.

What this does **not** fix is `page-header` overflowing on its own:
at 900×600 "New Smart Playlist" is still clipped to 114 of 162px with
the queue *closed*. That is #69, and it cannot be fixed in
`page-header` alone — actions arrive through `<slot name="actions">` as
arbitrary light-DOM markup with their own handlers, so collapsing them
into a "More actions" menu needs an actions *API* (data, not markup)
across all three hosts that slot them.

**The phone section of `index.css` is last on purpose.** A media query
adds no specificity, so a `@media (max-width: 599px)` block placed
above the plain rules it overrides loses to them — which is how phase 1
shipped a header that kept its 2em gutters and 24px title on a 390px
phone with every declaration dead and nothing failing. The shell fitted
anyway, because the fitting is done by `min-width: 0` and by each
component's own media query, which live in their own stylesheets and
have no later rule to lose to. Cosmetic declarations are exactly what
no assertion sees; a screenshot found it.

**`<now-playing-view>` is where the seek bar and volume went.** It is a
*detail* view (`DETAIL_LOADERS`, so the nav stack carries the way out —
a tab you cannot leave by pressing again is not a tab), reached from a
phone-only button over the mini player's art, and it **composes the
real `<seek-bar>`, `<player-controls>` and `<volume-control>`** rather
than reimplementing them. While it is up, `index.css` hides the bottom
bar through `body:has(#main-content[data-active-view="now-playing"])` —
the active view is already published as an attribute, and a class
toggled from `index.ts` would be a second expression of the same fact.
The view therefore carries its own queue button, because that button
lives in the bar it hides.

**And below 500px of height its art and its names share a row** (#51).
The stacked arrangement's budget is fixed — 48px of header, 143px of
transport since #64, 78px of names, 68px of padding and gaps — so the
art gets `height - 386`, which at the reference device's 424x439 is
**53px**: the one thing a Now Playing screen exists to show, smallest
on it. #172 named the two ways out and this is the second, because the
first — a floor on the art with the block scrolling — scrolls the
transport off the bottom, and *controls never scroll off* is #51's own
Direction and plan 018's promise. Sideways the art is bounded by the
row's height instead of by the column's leftover: **53px to 143px**,
measured on the device, nothing scrolling, the transport untouched.

Three things about it are load-bearing.

**500 is where the two layouts cross rather than a round number.** In a
row the art is `height - 296` and the names get what is left of 392px,
so the names hold 176px at exactly 500 and less above it; stacked, the
art is `height - 386`, which passes 176px at 562. Below 500 the row is
the bigger art *and* the readable one — above it the column is, which
is why a tall phone keeps the arrangement it has. It is keyed on height
alone and not on the phone's width, because it answers vertical room: a
900x450 window has the same problem and the same fix.

**The art was not a small square, it was a crop, and that was never
only the phone.** `aspect-ratio` is specified not to re-derive the
width when `max-height` clamps the height — unlike an intrinsic ratio,
which is preserved under both bounds — so a definite `width: min(100%,
60vh)` kept its width while the height was clipped, and `object-fit:
cover` cropped a square cover into the band: **264x53** on the device.
The leftover exceeds the width only above ~843px of viewport, so every
height from ~500 to ~843 drew one too. `max-width`/`max-height: 100%`
with `width`/`height: auto` is the fix and is the replaced-element
path; it also never upscales past the natural size, and the largest
tier `saveCoverArt` keeps is 400px, so nothing is lost.

**The placeholder needs its own rule, and a non-replaced box cannot
express this one.** With no intrinsic size, auto/auto collapses it to
its icon (13x58, measured). It is driven from the height instead, with
`min-width: 0` because a flex item's automatic minimum is its content —
without it the icon's width becomes a floor the moment the row is
shorter than the icon, which is exactly what a job band does to this
screen. And since whichever max clamps does not re-derive the other, a
height-driven box goes **380x484** on a tall phone; `max-height:
calc(100vw - 2rem)` closes it, which is sound here for the reason
`60vh` was not — this is a phone-width detail view, so its content box
really is the viewport less the host's gutters, and it is a *max*, so
the failure mode is a square bounded early rather than a crop.

**The playing row is a shape, not a hue.** `track-list` and
`queue-panel` draw a `::before` triangle in each row's own left
padding, plus `aria-current` — before, both rows were a background tint
and a text colour and nothing else (WCAG 1.4.1). It is in the padding
because the track list's grid columns are computed from the host width,
so a marker in the flow moves every cell on the playing row and nothing
else. Both tiers assert it is **absent** on the other rows: a marker
that renders everywhere satisfies "the playing row has one" for free.
One thing to know before checking it — a track started from the *track
list* leaves the queue's `currentIndex` at −1, so the panel has no
current row at all in that flow, which reads exactly like the marker
not working.

**The first thing Tab reaches is a skip link.** Two details are
load-bearing and neither is the link's text. It is `position: absolute`
in **both** states, because `body` is a grid with named areas and an
in-flow extra child is auto-placed into one of them. And `<main>`
carries `tabindex="-1"`, or the fragment moves the scroll, leaves the
tab sequence exactly where it was, and looks like it worked. The
subtitle beside it is a `<p>`, which is also what an `hgroup` is
supposed to contain — and dropping the `<h3>`'s bottom margin shortened
the flex-centred title block enough to move it down into the 4em bar's
clip, so `.title` zeroes both margins.

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

  **Below 600px a name is not a link, and the row's menu is where it
  went** (#67). Every sentence above is a *desktop* compromise: the
  double-click grace means nothing on touch, a few characters of text
  is not a touch target, and since #63 a claimed `yj-tap` has its click
  swallowed, so the link was unreachable as well as fiddly. The rule is
  in the utility rather than at twenty call sites, and
  `utils/go-to-menu.ts` is the other half — "Go to Artist" / "Go to
  Album", drawn under exactly the condition the link is not, from
  `explore-link`'s own exported routing so an untagged artist reaches
  the library page by the same lookup.

  Three things about it are load-bearing. **Suppressing a link without
  a menu behind it is not a smaller affordance**, it is a destination
  the phone cannot reach — so `keepOnPhone` is the documented exception
  for the three surfaces with no row menu (`now-playing-view`,
  `explore-album-details`' header credit, `top-results-row`), and
  nothing else may pass it. **One row or none**: the items are the Play
  item's rule one step on, since "go to the album" of five different
  albums means nothing. And **there is no "Go to Genre"**, because
  there is no genre link anywhere to lose — that would be new
  navigation rather than a replacement, and belongs in its own issue.
  `track-list` is the one list that gains rather than moves: its phone
  column set stacks title over artist as plain text already, so those
  names have never been links there.
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

**What each icon *means* is a second table, and it is
`utils/icon-language.ts`.** Bundling answers "does this name resolve";
nothing answered "does this name mean what the one next to it means",
and a wrong-but-real icon renders perfectly. So `plus` came to mean add
to the queue, add to a playlist, make a new playlist **and** you do not
own this — the first two *adjacent in the same context menu* — while
`list` meant the queue, the Playlists destination and adding to the
queue.

The rule the table is built on: **an icon names the noun it acts on,
not the verb.** "Add to queue" and "add to playlist" are one verb on
two nouns, so the noun is what has to differ — which is why adding to a
playlist wears the Playlists destination's own icon, and why the queue
got `bars-staggered` and stopped wearing Playlists'. `plus` keeps the
one meaning it is unambiguous about, making something that is not there
yet.

Four things about it are load-bearing:

- **The request toggle is one glyph in two weights**
  (`regular/bookmark` → `solid/bookmark`), because two states of a
  toggle have to read as each other's opposite and a plus against a
  bookmark does not. The pair was *already in the app and already
  right* on `explore-album-details`'s "Request this" button while the
  badge forty pixels away showed a plus — `utils/library-status.ts`'s
  fault one layer down, having made the two agree on what wanting means
  and left them disagreeing on what it looks like.
- **Downloads keeps the solid bookmark, deliberately.** That is the
  same word twice, not two words: the badge says "this is on your
  list" and the nav item is that list.
- **`icon-language.test.ts` sweeps the source**, because the rule is
  about every call site and checking one checks nothing — the same
  shape as `TestNoDirectRuntimeEmits`. It reads every `src/**/*.ts` as
  raw text and fails on a literal `name="plus"` or `icon: 'list'`
  outside the table, and its **first assertion is that it read
  anything at all**, since a sweep over an empty glob passes.
- **It also asserts every `ICON_*` is bundled**, which closes the loop
  the runtime cannot: `bookmark-check` is Font Awesome **Pro** and sat
  on `explore-artist-details`'s Follow button, drawn for every followed
  artist as a circled question mark. `offline-icons.spec.ts` sweeps
  `__yjIconMisses` and could not see it, because no spec had ever
  followed an artist — the same fault `requested-badge.spec.ts` was
  written for, one component over, still live. A name computed from
  state was only checkable from the state; now it is checkable from the
  table.

**An album page says how much of the album is yours.**
`explore-album-details` is a *catalog* page and there is no
library-side album detail page at all, so the album on it may be
wholly the user's, partly theirs, or not theirs — and its primary
action has to mean the same thing in each case. It says which: **Play**
when the whole release is owned, **Play 7 of 12** when some of it is,
and **no play button at all** when none is, because a Play button that
plays nothing (or seven tracks of forty) is worse than none.

**The page asks one question, once, and it is "is there a file".**
Ownership used to be several claims of decreasing confidence OR'd
together — a local album id, the backend's cross-reference, a cached
MBID match, and finally *any single track* flagged `inLibrary` — none
of which is "there is a file to play", which is why the tick could be
green on an album whose every action did nothing, and why the
tracklist's context menu asked the backend on **hover** whether the row
it was drawing was owned. `filePaths` is the one answer: a map from a
displayed track to its path, filled once from `updated()` by a single
batched `GetFilePathsByRecordingMBIDs`, and read by the badge, the Play
button's count, the dimmed rows and every menu item. `askedFor` is a
separate set from `filePaths` because the guard has to be *asked*, not
*answered* — an unowned MBID never lands in the map, so guarding on the
map re-requests it on every render, forever.

The other half is that the *displayed* tracklist is also one thing.
`buildVersionEntries` synthesises the "Your Library" entry from
`localTracks`, so an album the catalog cannot answer for is exactly the
case that needs the version list rebuilt — `loadLocalTracks` guarded
that rebuild on `releases.length > 0` and so rendered "No release data
available" over a tracklist it was holding in memory.

**And the key it plays by is not the key it looks owned by.** The local
album id is used wherever there is one, because a library-only album
has *no* recording MBIDs — its tracks are synthesised from
`GetAlbumTracks` with `mbid: RecordingMBID || ''` — so an MBID-keyed
lookup on an untagged library resolves to nothing and Play queues
nothing while looking entirely correct. Those synthesised tracks carry
their `FilePath` from the same rows, so they populate `filePaths`
directly and cost no lookup at all; the batched
`GetFilePathsByRecordingMBIDs` is what answers for a *catalog*
tracklist. It is the third member of the `GetFilePathsBy…` family: one
query, paths only, grouped so the caller keeps the tracklist's order.
It exists rather than a lookup by track id because **`MBTrack.LocalID`
is declared and nothing in the backend ever writes it**.

**How much of an album is here is a question the files can answer.**
`ownership()` above counts the *displayed* tracklist, which for a
library copy is a tautology — every local track has a file, so owned
always equals total and "do I have all of this" had no local answer.
The album page therefore asked MusicBrainz, and
`BrowseReleases` is the most expensive call the app makes: releases
plus every version's full tracklist, on a 1 req/s limiter shared with
`PrefetchReleases`, which fires up to eight when an artist page
renders.

The denominator was already on disk. `metadata` has read the "5/12"
totals off every file since forever (`m.Track()`, `m.Disc()`) and
discarded them; they persist to `audio_files.total_tracks` now, and
`GetAlbumCompleteness` sums them. **A complete, MBID-matched album
makes no catalog call at all** — identity from the MBID, tracklist from
the tags, which between them are what the browse was being spent on.

Three things about it are load-bearing. **Totals are declared per
disc**, so the expectation is a sum over discs and not one number, and
a disc whose files declared nothing leaves the whole album unknowable
rather than being covered by the discs that did. **Unknown is a third
state and must render as neither** — a great deal of any untagged
library has no total, and a ring drawn from its absence would mark most
of a library incomplete on no evidence; `Known` is what guards that,
and the badge falls back to the plain tick. And **complete is `>=`,
not `==`**, because bonus and hidden tracks routinely put a folder over
its declared total and that is a complete album, not a broken one.
Owned counts *distinct track numbers* for the same reason in reverse:
this app detects duplicates, and counting two files of track 3 twice
would report a short album as complete.

**Where the tags have no total, the catalog does.** `explore_index`
carries a per-release-group `total_tracks` — ~2 bytes across 400,677
rows, about 800 kB of artifact — and `completenessAnswer()` merges the
two: the numerator stays local (distinct track numbers on disk) and
only the denominator is borrowed, because a catalog total describes the
canonical release while the files' own total, where they declare one,
describes the release the user actually has. Zero still means "the
catalog does not say", so an album neither side can total keeps the
plain tick.

Two rules hold up the column itself. **It is counted before the
popularity filter**: `cmd/indexbuild` counts the canonical dump's rows
per kept release, and counting only the *kept recordings* would say "9"
about a twelve-track album whose other three nobody has played — the
same confident lie that kept whole tracklists out of the artifact.
And **adding a column to the importer's SELECT is how you break every
artifact already published**, so `artifactHasTotals()` asks the
attached artifact whether the column is there (on the writer, where
`core` is attached) and selects a literal `0` when it is not — the same
shape as the encoding probe beside it.

What neither side can give is *which* tracks are missing, only how many
— so an incomplete album still browses, and that is now the exception
rather than every album load. One smaller consequence: existing databases
read "unknown" until a rescan repopulates the column, which degrades to
exactly the old behaviour, so nothing breaks.

**And our own writers declare the total, because for a long time they
did not.** `tagwriter` wrote track and disc *numbers* and dropped the
totals, so autotagging an album actively **erased** the evidence this
rests on: the release became MBID-matched — a green tick — while the
field `GetAlbumCompleteness` reads stayed absent, which is exactly the
"2 of 10 tracks, reported as in your library" the report described.
`FieldTotalTracks` / `FieldTotalDiscs` are written by the autotag apply
pass and by the download importer, and `dbsync` persists the track
total to the row so the album page agrees with the file without waiting
for a rescan.

Five things about it are load-bearing, and four of them fail silently:

- **The total is per *disc*, not per release**, because that is what
  the tag form declares and what `GetAlbumCompleteness` **sums** per
  disc — a release total written on every file multiplies a two-disc
  album's expectation by two, and no library can then satisfy it.
  `backend/tagtotals` is that derivation, once, because the two callers
  must not import each other or the writer.
- **The Vorbis names are `TRACKTOTAL` and `DISCTOTAL` and no other
  spelling.** `dhowden/tag`'s Vorbis reader looks at exactly those two
  keys, so a perfectly reasonable `TOTALTRACKS`, or a `1/12` inside
  `TRACKNUMBER`, is written successfully and reads back as no total at
  all. The tests assert the round trip through the reader the *scan*
  uses rather than through the bytes, for that reason.
- **ID3's number and total share one frame**, so writing either alone
  has to read the other off the existing tag or it silently discards
  it. A total with no number is not written: `/12` is what a reader
  parses as track 0.
- **The totals are written unconditionally, not on a diff.** The case
  this exists for is a file that declares *no* total, which compares
  equal to nothing and is exactly what a "only if it changed" guard
  skips.
- **A single-track download must not be totalled.** A `RecordingMBID`
  anchor resolves `Expected` to that one track, so the same code would
  tag a track off a twelve-track album "1 of 1" — and a declared total
  outranks the catalog total that would otherwise have answered
  correctly. Confidently wrong is worse than absent here, which is the
  same rule `Known` exists for.

One gap this did not close and #104 did: **`dhowden/tag` has no RIFF
reader**, so nothing the tag writer put in a WAV's `id3 ` chunk was
visible to `metadata.ExtractTags` — not the totals and not the title
either, on files the app itself had just tagged. `wav_test.go` read
that chunk itself, which is why no test noticed: a round trip asserted
through the writer's own parser is a test of the writer.

`backend/riff` is where the container is now read, and it is its own
package because the alternative is an import cycle — `tagwriter`
imports `metadata`, so `metadata` cannot reach back for `parseRIFF`.
`backend/tagtotals` is the precedent.

Three things about it are load-bearing. **The two readers are
deliberately different**: `Parse` holds every chunk in memory, which is
what rewriting a file needs, and a WAV's audio *is* a chunk — so the
scan path uses `ID3Chunk`, which seeks over what it is not looking for.
**The container decides, before `tag.ReadFrom` rather than after it
fails**, because that library's last resort is an ID3v1 trailer and a
WAV carrying both would otherwise be read by the wrong one. And **an
untagged WAV is a file with no tags, not a file with a problem**: no
chunk, an RF64 container or a tag holding no frames all read as empty
metadata with no `TagReadWarning`, since the scanner's filename
fallback is the right answer and a warning would put a fault on a file
that has none.

The gap was pinned by a test that said so, which failed the moment the
reader learned and carried the instructions for what to update in its
own comment. So it is deleted, `TestFixturesMatchManifest` no longer
skips `wav`, and `totals_test.go`'s WAV case goes through
`metadata.ExtractTags` like the other three formats. The fixture
library's two WAV tracks scan with their tags and their cover now,
which is a change to what every seeded tier sees.

**The absence is what gets marked, not the presence.** The tracklist
put a green tick against every owned track and a legend underneath
explaining the tick — a positive mark on the *common* case, so an album
you own outright wore a column of circles and a key for them. It is the
streaming-service treatment now: rows not in the library are **dimmed
in place**, and nothing marks the ones that are. Two things follow.
Dimming is a colour, so it cannot be the only signal — the row carries
`aria-disabled`, which is what reaches anyone not seeing it. And the
dimmed rows are why the `loading` banner could go: tracks arriving
dimmed reads as the album filling in, so a line of text about the
page's own plumbing earns nothing. `unavailable` survives because it is
not about plumbing — it says rows may be missing from the page
altogether, which nothing on screen can show. (`explore-artist-details`
still uses `loading`; it has no equivalent per-row signal.)

**And that treatment is the app's, not the page's.**
`utils/ownership.ts` is the rule written once, because it was written
at eight call sites and so none of them had the whole of it: Explore's
cards, `top-results-row` and the artist page's three card shapes all
mixed owned and unowned with a small badge as the only difference, and
the badge on the *owned* ones was a green tick — the mark on the common
case this tracklist removed. Owned is plain and draws no badge at all;
unowned is dimmed, says so in its accessible name, and keeps its
request affordance; a partly-held album says how partly.

Four things about it are load-bearing.

**Ownership is `localId`, and `inLibrary` is deliberately not
consulted.** The album page answers with `filePaths`, a real file per
displayed track, and a card grid cannot afford that — but it does not
need to, because `explore_index.local_*_id` is built by
`collectLibraryEntities` from queries that every one join `audio_files`
and cleared by `pruneStaleLocalCrossReferences`, whose existence test
is a file test in all three cases. That is the same "ownership is a
file" rule computed once per scan instead of once per screenful.
`in_library` is written by the same pass, so the two agree in a healthy
database, but it is a one-way ratchet
(`MAX(in_library, excluded.in_library)`) whose only clearing pass is
gated on a non-null local id: it cannot be un-set on its own (#118).
One is a fact with an owner; the other is a flag that happens to agree.
Both `explore-view` and `explore-artist-details` additionally kept a
`libraryMBIDs` set that accumulated every MBID ever seen with the flag
and cleared it never, in views that never unmount; both are gone.

**The two answers used to sit on one card.**
`renderReleaseMenuItems` gates Play on `release.localId > 0` while the
badge used `inLibrary`, so an album with the flag and no local row drew
a tick saying it was in your library, offered no Play, and — the
request item being gated on *not* owned — offered no way to ask for it
either. Any new surface that asks the question twice will reproduce it.

**`aria-disabled` goes on rows and not on cards.** An unowned *row*
cannot be activated; an unowned *card* still navigates to the catalog
page for it, which is a perfectly good thing to do with something you
do not own. The accessible name carries the state either way, which is
why it is one helper and not a class.

**The count is batched, not looked up.** `store/completeness-store.ts`
is `credit-store` one question over: `request()` is per-card and
coalesces a screenful into one `GetAlbumsCompleteness`, absence is
cached as an answer (or the albums with no totals re-ask forever), and
the whole cache is dropped on a scan, a retag or a removal rather than
aged. `library-status.ts`'s `albumBadgeFor` is where that meets
`Known`: a total that was never declared is a plain `in-library`, never
a ring at 0%. One consequence in the badge itself — a `partial` badge
is *actionable*, and a control named after its action alone dropped the
count from the one state the ring exists for, so its name is both.

**A partly-owned album draws the release, not the part.** Once the tags
say nine of twelve, `buildLibraryEntry` shows the *catalog's* twelve
with three dimmed, rather than the nine on disk — the missing tracks
are the useful information and a tracklist trimmed to what is owned
cannot show them. It is guarded on `completeness.known` rather than on
"fewer tracks than the cluster", which would swap a catalog tracklist
in for every album whose tags simply never declared a total. A
side-effect worth knowing: this is what finally makes `ownership()`
say something true here, since counting the displayed tracklist of a
library-only entry could only ever produce "9 of 9".

**And it can be asked, because the rule alone reaches too few albums.**
That guard depends on two inputs the user does not control: the files
declaring a per-disc total, and the catalog's own `total_tracks`. Where
neither says — which is a great deal of any library, and *every* library
until an artifact carrying the column is published — a partly-owned
album showed only the tracks on disk with nothing to say the rest
existed. `renderTracklistScope()` is the explicit route: a
"Show the whole album" switch that flips the synthetic "Your Library"
entry between the local files and the release, which is the rendering
the page could already do and could only be *triggered* automatically.

Three things about it are load-bearing. **`showFullTracklist` is a
tri-state**, `null` meaning "follow the automatic rule": the rule is
right when it fires and the switch has to be able to agree with the page
it sits on rather than starting out contradicting it, which a plain
boolean would need recomputed every time the completeness answer moved
underneath it. **`fullReleaseCluster()` falls back to the
highest-scoring cluster**, because `findLibraryCluster` is a guess over
the `inLibrary` flags and returns *nothing* when none are set — which is
exactly the untagged library the switch exists for, so without the
fallback the control would be absent precisely where it is needed. And
**it is shown only where it can change what is on screen**: against the
library entry, with a release to switch to, and only when the two
tracklists differ — the same test the version dropdown answers, one
control over.

**A dropdown is only a choice if the choices differ.** The version
selector tested `versionEntries.length`, but a release group routinely
has several releases — reissues, regional pressings, a remaster — whose
tracklists are identical, and the synthetic "Your Library" entry is
often a third name for the same one, so the control appeared with every
option showing the same rows. `distinctTracklistCount()` is the real
test. It deliberately does **not** use `fingerprint()`, which keys on
recording MBIDs alone: a library entry built from untagged files has
none, so every such tracklist fingerprints to the same run of empty
strings and compares equal to every other. It falls back to the title,
which is what lets a local copy be recognised as the same tracklist the
catalog is describing.

**Say which version you own, not that you own one.** A synthetic "Your
Library" entry used to stand in for the matching release, which hid the
thing worth knowing: you could see that you owned *a* version but not
*which*, while the real release — its date, country and release count —
sat underneath under a different name. The matching release carries
`inLibrary` and is marked (★ **and** the words "in your library", since
a `<select>` cannot be styled per option and a bare glyph is the
unexplained symbol the tracklist's green ticks were). The synthetic
survives only where there is nothing to mark: local files matching no
release, or the no-local-album overlap guess.

**A merged cluster shows the order the most releases agree on.**
`mergeNearDuplicateClusters` folds by track *set*, so a resequenced
pressing — same songs, different running order — merges correctly. But
the survivor was whichever release came first in the browse response,
which is meaningless ordering. On a real album one 2021 pressing
arrived ahead of eleven 2013 ones, so the cluster wore the 2021 running
order; the user's files then matched no cluster **fingerprint**, and
the page both called their copy unlinked to MusicBrainz and offered a
second "version" whose only difference was an ordering almost nothing
was pressed in. `withConsensusRepresentative` re-picks by how many
releases share each exact ordering, earliest date breaking the tie —
**and moves the cluster's fingerprint with it**, since that is what the
library match is tested against. Note the consequence for the version
list: a resequence is not a separate version, because the merge folds
it before any of this runs.

**A slow catalog fetch is not a failed one.** The same page used to
reach `unavailable` — "No catalog details for this album right now" —
from a **12-second timer**, which is the only signal it had, because
`ensureReleasesAsync` emitted `AlbumReleasesReady` on success and
nothing at all on failure. Against a browse queued behind eight
prefetches at 1 req/s, that reported healthy fetches as catalog
failures on correctly-matched albums. `AlbumReleasesFailed` is the
missing half; `catalogFailed` is the only route to `unavailable` now,
and the timer is a 60 s backstop for a genuine hang rather than the
verdict.

**On a phone that page is one scroll container, and the header is in
it** (#66). It was built as a fixed header over a scrolling tracklist,
which is the desktop arrangement: at the reference device's 424×439 the
header owned **253 of the panel's 318px** and the list scrolled inside
the 64 that were left. Below 600px the *host* is the scroller and
`.content` stops being one, so the whole page moves together — which is
only available because this tracklist is plain DOM rather than a
virtualizer, and because `.main-panel > *` already gives the host a
definite height.

Three things about it are load-bearing. **Another `min-width: 0` was
not the fix**: `.album-info` carries one and was shrinking exactly as
asked, to 112px beside a 200px cover — so the title drew as `G…` and
"Shuffle album" ended at x=443 inside a 424px box, clipped by the
component's own `overflow: hidden` and reachable by no gesture. A row
with a fixed-size sibling has to **stack** at that width, or the column
that must shrink has nothing to be wide with. **`layout-overflow.spec.ts`
cannot see any of this** — `body.scrollWidth` equalled the viewport
throughout, because the overflow was *inside* a component; the spec
measures each header control against the host's own box, which is
`top-bar-fit.spec.ts`'s shape for the same reason. And **the phone block
is last in the stylesheet**, on `index.css`'s rule: a media query adds
no specificity, so written above the plain rules it overrides every
declaration in it is silently dead.

**Activating a row plays the list the row is in, from that row.** A
double-click — and Play on a single row's context menu — queues the
list as *displayed* with `startIndex` on that row, not a queue of one
track that stops when the song ends. The two playlist views and
`cover-grid`'s album dropdown always did this; the album page and
`track-list` did not, so playing anything from the two largest
tracklists in the app discarded the album around it. Three rules come
with it. **A menu asks how much is selected**: one row is a position
and means "from here", several rows are an explicit choice of *those*
tracks and become the queue on their own (which is also the only case
where `shuffleStart` still applies, since no one row was named as the
place to start). **The index is into the paths, not into the rows** —
`explore-album-details` queues `ownedFilePaths()` and its dimmed rows
are not in it, so an index taken from the tracklist starts an album
somewhere else entirely, or past its end. And **the index is looked up
when it is used, not remembered**: selection keys are file paths
because those survive a re-sort, a re-filter and a refetch, and an
index survives none of the three — `displayIndexOf` is that lookup,
against `cachedSortedTracks`, which is the only order the user can see
and therefore the only one they can mean.

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

**A badge is a button only where it can act, and it says which.**
`library-status-indicator` — the tick/hourglass/plus on every Explore
card and track row — was a `<button>` whose click handler was a
`stopPropagation()` and a comment saying to wire up the download client
later: 20 of the 66 tab stops on a results page announced themselves as
buttons and did nothing. 007 made it `role="img"` on the rule that a
control which cannot act is worse than none, and named the condition
that would change the answer: a `<button>` again *with* a handler,
never a handler bolted onto something already shaped like one.

It is that now, and three rules hold it up. **A call site opts in** by
passing `request-mbid`, so a redundancy is visible in the template
rather than hidden in the component — `explore-album-details`'s header
has "Want this" in words directly below it and does not opt in. **An
owned entity is never a button**, because there is nothing left to ask
for, which is what stops the returned tab stops being spent on nothing.
And **the name is the action and the action is a request**: "Want album
X" / "Cancel the request for album X". Clicking adds a row to the
request list and nothing to the library, which is exactly what made the
original "Add … to library" a promise the control could not keep.

Two entities and not the third. A track is requestable because
`EntityRecording` is real work in the backend — `Reconciler.tracklistFor`
has a branch for it, since one expected title is what lets filename
matching score a single-track download at all. An artist is not: there
is no artist badge anywhere (`top-results-row` renders `nothing` for
one), and a discography subscription — never satisfied, expanding into
child requests — belongs on `explore-artist-details`'s Follow button,
which can say what it commits to.

Two smaller things, both still true: a `<span>` does not get
`box-sizing: border-box` from the UA stylesheet the way a `<button>`
does (the badge grew 36→38px, caught by a stored screenshot, and both
branches now set it), and the click is swallowed again — for the
opposite reason to before. With no action of its own the badge was part
of its card and a click on it meant what the card means; with one, it
does not. Enter and Space are stopped for the same reason, since every
card holding one is a `role="button"` or `role="option"` with its own
handler.

**Its third state was declared for a year and produced by nothing.**
`queued` was styled amber, given an hourglass and given the sentence
"… is queued for download", and all eight call sites were a two-way
ternary — so an album already on the request list showed a plus and
said it was not in the library, on the same page as a filled button
reading "Wanted". `utils/library-status.ts` is that rule written once:
`libraryStatusFor()` (owning outranks wanting; a *satisfied* request is
not queued, because nothing is coming; a request is by MBID, so a track
inside a requested album is not itself requested) and `toggleRequest()`
beside it, because the "Want this" button asks the same question and
two definitions of *what wanting means* is the fault this replaced.

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

**And an action is data, on that same rule: the header decides what
fits, the host decides what happens.** Playlists slotted three buttons
totalling 390px into a header that gets 700px at 900×600, so "New Smart
Playlist" rendered **114 of its 162px** with the queue closed — and on a
phone none of them could be reached at all, which is what #69 reported.
A host passes `PageAction[]` (`{id, label, icon, onSelect, priority,
drop?}`) and `page-header` renders each one as a button or as an item in
one "More actions" menu.

**It could not have been a rule added in one place**, and that is a fact
about the API rather than an effort estimate: actions used to arrive
through `<slot name="actions">` as arbitrary light-DOM markup, and a
component cannot move another component's light-DOM children into a
dropdown and keep their behaviour — there is nothing generic in markup
to render as a menu item. The slot survives for markup a data list
cannot express, at the stated cost that **a slotted action does not
collapse** and must therefore fit at 800×600.

Six things about it are load-bearing:

- **The fit is measured, never breakpointed.** A ResizeObserver drives
  it, and each pass starts from *all visible* and hides the
  lowest-priority action until it fits — so the collapsed set is a pure
  function of the current width rather than of how the window got
  there. A rule that only ever added to the set would never give a
  button back, and one that adjusted by a step would need a hysteresis
  band to stop it oscillating on the pixel where a button exactly fits.
- **"Fits" means nothing is clipped, which is not the same as the
  header not overflowing.** The title can ellipsis, and the moment it
  can it absorbs the pressure: `scrollWidth` reports a header that fits
  perfectly while the heading reads "Playlis…". That is this bug moved
  from the button to the title, invisible to the same measurement that
  missed it the first time — so the heading's own truncation counts as
  not fitting, and an action is collapsed before the title gives way.
  Below that, at 320px, the title *is* what yields: the navigation also
  says which page you are on, and an action has nowhere else to be said.
- **The measurement flips `hidden` on the rendered nodes rather than
  re-rendering between steps.** Reading `scrollWidth` forces layout,
  which is the point; awaiting a Lit update between steps instead lets
  the intermediate all-visible state paint, so the fix would flash the
  overflow it exists to prevent.
- **Priority is what a *capability* costs, not what a button is worth.**
  New Playlist is highest because it is the **drop target** and a closed
  menu cannot be one; that is also why `PageAction.drop` carries the
  host's own `dragover`/`dragleave`/`drop` handlers rather than the
  header owning a notion of dropping, and why the affordance is simply
  absent from the overflow rather than approximated there.
- **`aria-controls` names a panel that is always in the DOM** —
  `config-section`'s rule, and `wa-popup` hides it when inactive — and
  the keyboard model is `MenuKeyboard`, shared with every other menu in
  the app so this is not a second one.
- **It is checked per button, because `layout-overflow.spec.ts` cannot
  see this.** That spec asserts the *shell* needs no sideways
  scrolling and passed on the broken build; clipping *inside* a
  component is invisible to it, which is exactly why the defect
  survived a spec named for it.
  `e2e/specs/header-action-overflow.spec.ts` measures each button
  against its header at 900×600, 800×600, 390×780 and 320×600, and
  asserts buttons **plus** menu account for every declared action —
  without that half it would pass vacuously on a build that renders no
  actions at all.

**The count is the last thing to yield, and only at 320px.** Four
things compete for that row and three of them cannot go: the title
yields first and is allowed to ellipsis away entirely, because the
navigation also says which page you are on; the sort control and the
actions are each the only place they are said, which is what the
overflow menu exists for. That leaves the count, which is the one
purely informational item there — an empty page says so in its empty
state and a full one is being looked at. It became reachable rather
than theoretical with #57, since below 600px this header also carries
the phone's search button: measured on Playlists at 320px, title 0,
count 50, sort 143, search 40, "More actions" 38, five 12px gaps and
32px of gutters — 363 in 320, with the More button ending 27px past
the edge. It is rendered and hidden with an attribute rather than
returned as `nothing`, for the reason the action buttons are: every
pass starts from all-visible and needs a node to un-hide, or the first
320px window costs the count for the rest of the session.

One thing it deliberately does **not** grow is a phone mode for the
actions. `PHONE_COLUMN_IDS` is the precedent for "what is drawn and
what can be sorted are different questions", but it exists because the
track list's columns cannot be derived from a width; these can, and a
second declaration of what a phone shows is a second thing to keep in
step. What the header *does* state at phone width is one word: below
600px the sort control's "Sort:" label is visually hidden — 172px of a
320px header for a label the adjacent direction arrow implies — and it
stays in the accessibility tree, because it is the select's accessible
name and hiding it outright is `config-field`'s bug one component over.

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

**On a phone the box is a modal, and the map is what decides who gets
one** (#57). There is no header to hold it below 600px, so
`<search-trigger>` is a button in the row that already says which page
you are on and `<search-dialog>` is where the box goes — and both ask
`searchStore.isSearchableView()` rather than being told, which is the
whole reason the trigger is an element and not a `PageAction`. Seven
hosts each declaring a search action would be a second list of
searchable views, and putting the decision inside `page-header` would
be the phone mode for actions that component documents its refusal to
grow.

Four things about it are load-bearing.

**It is a `wa-dialog`, and that is a mechanism rather than a taste.**
#60 read out of the Web Awesome source that `wa-popup` renders
`<div popover="manual">` and feature-detects the Popover API, falling
back to `strategy: "fixed"` where there is none — which is Chrome 113,
the reference device, since `popover` is Chrome 114. `position: fixed`
escapes ancestor overflow but **not** `contain: paint`, which
`.main-panel` carries, so a popup-shaped search panel opened from a
view's header is structurally clipped on that device. `<dialog>` /
`showModal()` is Chrome 37 and uses the real top layer. **No tier here
can see the difference** — CI's Chromium and WebKit both have the
Popover API, so the popup would be top-layered and correct and a spec
asserting "not clipped" would pass on the broken build. The component
tier asserts the *mechanism* instead: that there is a native `<dialog>`
in the tree.

**It carries the real `<search-bar>`**, not a second input, which is
what keeps one debounce, one clear button and one view-scoped
placeholder. `--yj-search-max-width` is the one thing the modal changes
about it: 360px is a cap for a header, not for a control that has the
whole of a 424px screen.

**The results are the page, not a list in the modal.** The term is
view-scoped and the view behind already filters on it and says
"Showing tracks matching …", so Enter closes and hands the screen back.
Rendering results in the dialog would be a second implementation of
every view's filtering, and one that could not offer the row actions
the view does.

**Escape closes and keeps the term.** `search-bar`'s own input treats
Escape as *clear the search*, which is right in a header where the box
is on screen either way; in a modal it would mean dismissing the search
surface silently discarded the search. The dialog takes the key in the
capture phase on its own host, which is the only listener that runs
before the input inside `search-bar`'s shadow root.

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

**A phone draws one column of two lines, and that is a column set
rather than a second row template.** Measured on the device: at 424 px
the four configured columns fit the row *exactly* (`--grid-cols` came
out `24px 102px 101px 101px 80px`) and not one of them fit its content
— "Duration" did not fit its own header. The columns were never too
wide; there were too many of them. `PHONE_COLUMN_IDS` is `titleArtist`
(title over artist, sharing the row's whole width) plus the duration, so
the row, the delegated events, the selection semantics, the playing
marker and the virtualizer are all untouched: from their side only the
number of columns changed. Three rules come with it. **The row height
lives in two places and they must agree** — `PHONE_ROW_HEIGHT` and the
CSS rule — because the virtualizer positions rows from that number, so a
taller row overlaps its neighbour. **What is drawn and what can be
sorted are different questions**: the page header's sort list is built
from `configuredColumns`, or a phone (which has no column headers
either) could sort by nothing but title and duration. And **a phone's
widths are neither loaded nor saved**: `loadColumnWidths` is keyed by
column *id* and fills a gap with the minimum, so the stacked column —
which nothing can ever have saved a width for — came out at 148 px
beside a duration column of 236, and saving would have replaced the
width the user dragged on a desktop for the same id.

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

**Playing a track does not wait for the database to hear about it.**
Every write in this app goes through one connection —
`database.DB` is `MaxOpenConns(1)`, because SQLite has one writer — and
a background pass can hold it for a long time. The player and the queue
used to write inline from paths that hold their own mutexes, so a
contended writer did not merely slow persistence down: `SetQueue`
blocked in `LoadFile`'s `saveState` and then in `persistState`, **while
holding `q.mu` and `p.mu`**. The user's report is the exact shape of
that — the track changed and the transport sat at paused (`LoadFile`
had emitted `TrackChanged` and `PlaybackStateChanged(paused)`, and
`p.Play()` is *after* the writes), nothing appeared in the queue
(`emitQueueChanged` is after them too), and the play button did nothing
because `Queue.Play` was waiting on the same held `q.mu`. It was
diagnosed by profiling the running app: 91% of its CPU was
`explore.BackfillLibraryDiscographies` → `upsertBatch`, with four more
of its six workers parked in `sql.(*DB).conn`.

So a write is **submitted, not performed**
(`queue/persistwriter.go`, `player/persistwriter.go`): jobs run in
submission order on one goroutine per component, each carrying its own
snapshot — which is what keeps "clear and rewrite the queue" and
"insert three tracks at 4" meaning what they meant when they were
called. Two rules come with it. A job **must not touch the component's
fields**: it holds no lock and the state has moved on, which is why
`persistTracks` clones. And `SaveState` — shutdown, and the tests —
still flushes and waits, because that is the one caller for which the
row has to exist on return.

The corollary for anything new: **a durability write is not a step in a
user action**. If a mutation path needs the database to have finished
before it returns, that is a claim worth arguing for, not a default.

**"Remove from library" removes the row and excludes the path, and
never touches the file.** `RemoveFromLibrary(filePaths)` deletes the
`audio_files` rows the way the scan's own orphan cleanup does (tagging
group bookkeeping, FTS entry, `pruneOrphanedMetadata` for an album
whose last track just went) and records each path in `excluded_paths`.
The exclusion is not an enhancement — without it the next scan finds
the file, sees no row and imports it again, so the button undoes itself
and is worse than no button. It is reached from the track list's
context menu behind `confirmAction()`, whose **impact line says the
files are not deleted**, and from `tracklist.delete` (Delete), which is
bound to *opening that dialog* and nothing else: one keystroke from a
focused row, a key that asks is defensible and a key that acts is not.

Four things about it are load-bearing, and two are invisible from the
track list. **The soft scan compares files on disk against rows in the
database**, so an excluded path — on disk, deliberately not a row —
makes the two disagree forever and queues a full scan of the whole
library on *every* launch; `surveyAudioFiles` and `countAudioFiles`
therefore both take the exclusion set, because they answer "how many
files would a scan import", not "how many files are there". **Deleting
an `audio_files` row cascades to `queue_tracks`**, so the removal calls
the same `CompactQueue` hook `RemoveLibrary` does, which reloads the
queue and unloads the player if the removed track was the one playing.
**A full rescan clears the exclusions**, which is the only way back for
a path removed by mistake until there is a UI for the list. And
`TracksRemovedFromLibrary` carries `{filePaths, count}` so
`library-store` splices the tracks array in place and refetches only
the album/artist/genre summaries — falling back to a full invalidate
only when a tracks fetch is already in flight.

**Deleting the file from disk is deliberately not this**, and is not
foreclosed; it needs its own argument.

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
`app.Event.Emit`. `TestNoDirectRuntimeEmits` fails the build on a
direct call anywhere outside `backend/events`.

**Its justification changed with v3 and is now the weaker one.** Under
v2 this was a safety rule: `runtime.EventsEmit` `log.Fatalf`'d —
unrecoverably, taking the process down — on any context not carrying
the runtime, which includes every `context.Background()`, so a
background worker could kill the app by emitting. That is gone. v3's
emit takes **no context at all**, and `application.Get()` with no app
running returns `nil` rather than dying, so `Deliver` is
`if app == nil { return ErrNoRuntime }` where it used to probe the
**v2-private context key** `ctx.Value("events")`. What remains worth
pinning is narrower and still real: one emit path is what lets
`emitStatus` drop an unchanged payload for every caller at once.

**`events.Emit` keeps its `ctx` anyway, and it is now purely a test
seam.** Delivery does not go through it; `events.WithSink(ctx, rec)`
does, which is how a service is asserted on in-process
(`backend/queue/emit_test.go` is the model). Dropping the parameter
would have churned 45 call sites and every test for no gain.

That wrapper is what makes services testable in-process: install a
recorder with `events.WithSink(ctx, rec)` and assert on the payload the
frontend would receive (`backend/queue/emit_test.go` is the model).
`events.Deliver` is the same call returning an error instead of
dropping, and has one legitimate caller — `/__test/emit`.

**Naming the Wails application costs cgo, so exactly two files may.**
v3's `application` package is GTK/WebKit bindings on Linux, and
`cmd/indexbuild` / `cmd/indexexport` are built in a plain `golang`
container with `CGO_ENABLED=0` — the index workflow says so and it is
the one job that must not fail, since it owns the ~205 GB checkpoint.
So the single `app.Event.Emit` lives in `backend/events/runtime_wails.go`
under `//go:build !indexbuild` (with `runtime_indexbuild.go` returning
`ErrNoRuntime`, which is what the app itself returns before Run), and
`explore`'s `ServiceStartup` — the only other thing in that dependency
tree naming `application` — sits in `backend/explore/servicestartup.go`
under the same tag. `TestIndexToolsDoNotImportWails` walks the dependency
graph with `go list -deps -tags indexbuild` and is what keeps it that
way; a `ServiceStartup` hook added to a package the index tools import
is the way this comes back.

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

  `.releaserc.yml` **is** what runs now, from `release.yml`, and it is why
  the commit grammar is load-bearing rather than decorative: a merge to
  `main` whose commits are all `chore`/`ci`/`docs` releases nothing, and a
  mistyped `feat` ships a minor version. `make release-dry` answers "what
  would this merge release" without pushing.

  **The analyzer reads the type and ignores the scope, so a CI-only change
  is `ci:` and never `fix(ci):`.** The scope is decoration; `fix` is a
  patch whatever is in the brackets. Two commits touching nothing but
  `.gitea/workflows/unclaim.yml` were written `fix(ci):` and cut `v0.2.1`
  and `v0.2.2` — real releases, published to Arch, Homebrew and the APK
  registry, containing no user-facing change. They were left in place
  rather than deleted, because a version that vanishes is worse for
  whoever pulled it than one that turns out to be empty.

  **The blast radius is bigger than the version number**, which is what
  makes this worth a paragraph. A merge to `main` starts two workflows;
  if `release.yml` then pushes a tag, that tag push starts **four more**
  (`arch-package`, `homebrew-formula`, `android-apk`, `desktop-assets`) —
  on a runner with capacity 1, where the APK build alone is tens of
  minutes. `make release-dry` before merging is how you find out, and it
  is cheaper than every one of those.

  **`@semantic-release/github` is not in that config and must not be.**
  Gitea's API is `/api/v1` and is not GitHub's surface, so
  `@semantic-release/exec` calls `scripts/gitea-release.sh` instead — one
  `POST`, which is the whole of the Gitea-shaped work. The community
  plugin (`@saithodev/semantic-release-gitea`) was considered and
  rejected: last published 2022, on `got@10`, declaring no peer
  dependency on semantic-release at all.

  Two things in it fail *silently* and are therefore pinned with their
  reasons. **The notes come from `CHANGELOG.md`, not from an argument**:
  release notes are rendered commit messages — arbitrary text carrying
  backticks, quotes and `$` — so templating `${nextRelease.notes}` into
  `publishCmd` would be a shell injection whose input is the commit log.
  And **`conventional-changelog-conventionalcommits` is held at 9**,
  because at 10 it is quietly incompatible with the writer
  `release-notes-generator@14` pulls in: every release note renders as a
  bare `## 0.0.1 (date)` heading with no sections and no commits beneath
  it, no step fails, and the release ships with an empty body. Check the
  rendered notes, never the exit code.

## Testing

Tests use `database.NewTestDB(t)` for in-memory SQLite, built by the same
`applySchema` production uses so the two cannot diverge. Test audio fixtures live in `test_data/music_library_test/`. Table-driven tests are the norm.

**`make ui-visual` is the one tier nothing but a person runs, and it
cannot become one.** Its ten `toMatchScreenshot` baselines were recorded
on a developer's Arch box; replayed in a bare `ubuntu:24.04` container
— CI's `check` image — three of them fail on font metrics and
compositing alone (`track-info` and one `page-header` shot at a 0.03
mismatch ratio against a 0.02 allowance, `seek-bar` one pixel shorter),
and two components disagree about their own height between the two
machines. So CI keeps running `make ui-test`, which is the same suite
with the comparisons off, and a pre-push hook would be the same fault
with the machines swapped. What replaces the gate is a rule, in
`.pi/skills/yellowjacket-dev/references/ui-tier.md`: **a change that
moves a component's geometry refreshes that component's baseline in the
same commit, having read the image, and never one it did not cause**.
That is #196, which was four stale references accumulated across three
unrelated merges — a red tier nobody could read, which is how it stayed
red. A visual case must also **state the world it photographs**, since
the stores are singletons and a case that sets nothing records whatever
the previous one left in them.

## Git Workflow

Feature branches and PRs are the only way in: **`main` is a protected
branch** (`enable_push: false`, an empty push whitelist, and `CI / check*`
+ `CI / e2e*` as required status checks), so a direct push is rejected by
the pre-receive hook. This file said otherwise for a long time. Tags are
*not* protected, which is what lets `release.yml` push one.

**A branch answers a claimed issue** — see "Issues" above. The commit
grammar is unchanged and is load-bearing for a different reason
(semantic-release reads it), so the issue number lives in the branch
name and the PR body rather than in the commit subject.

**A batch of small fixes can be one PR**, which is what #83 did: eight
branches preserved as merges under one integration branch, so
authorship survives and the batch lands as one release rather than
eight. The cost is that its `Closes` list has to be checked afterwards
— it half-worked.

Pre-commit runs vet, lint, codegen check, and frontend typecheck in parallel. Pre-push runs the full test suite.

## CI

Eight workflows in `.gitea/workflows/`. Five of them package and
publish (`arch-package`, `homebrew-formula`, `index-artifact`,
`android-apk`, `desktop-assets`); `release.yml` decides *whether* four of
those run at all; `unclaim.yml` is housekeeping on the tracker and
touches no code; only `ci.yml` gates, and it is the one to look at when
deciding whether a push was healthy.

**`release.yml` is the entry point for all of it, and it is triggered by
hand.** It reads the Conventional Commits since the last tag and, if any
is releasable, writes the changelog, pushes the tag and creates the Gitea
release whose body is that changelog section. `arch-package`,
`homebrew-formula`, `android-apk` and `desktop-assets` are all keyed on
`v*`, so **the tag push is what starts them** — the version, the notes
and the packaging are still nobody's manual work; *when* is the only
decision left to a person.

**It used to fire on every push to `main`, which made the trigger "a PR
was merged".** That is a version per unit of *work* rather than per
*shipment*: eight releases in twenty-two hours (`v0.0.1` → `v0.3.1`) for
one session, each fanning out to four publishers on a runner with
capacity 1 — ~40 packaging jobs to ship three issues, with ordinary PR CI
queued behind them. Nothing else had to change to batch them, because
**semantic-release already reads every commit since the last tag**: five
`fix`es and two `feat`s become one minor release with all seven in the
notes. Release frequency was only ever how often the workflow fired.

This is the same rule `index-artifact.yml` states — *a job that mutates
state which cannot be rebuilt in ten minutes is triggered deliberately,
not by a push* — and the two are now the only workflows with no push
trigger. A schedule was considered and rejected: a cron batches without
anyone having to remember, but it puts the decision back on a timer,
which is the thing being removed. A `beta` integration branch was
considered and rejected too (#115): it relocates the trigger rather than
removing one, needs a second protected branch carrying the same required
checks, and *adds* a full `check` + `e2e` run per batch on the very
runner whose queue is the complaint.

**`dry_run` is why the manual trigger is usable.** The point of pulling
a lever by hand is being able to look first, so the dispatch takes a
flag that runs `semantic-release --dry-run`: the version and the notes,
no tag, no release, no publishers. Anything but the literal string
`true` releases for real — a typo in a dispatch box must not silently
turn a shipment into a green no-op.

**A prerelease tag is not a shipment, and all four publishers now say
so.** Their trigger is `v*`, which matches `v0.4.0-beta.1`; they guarded
`v0.0.0` and nothing else. Nothing produces a prerelease today — the
guard is there because the thing that would is `prerelease: true` in
`.releaserc.yml`, one line whose blast radius is a public Homebrew tap
and a credential-free APK registry that Obtainium polls. `android-apk`
is the worst of the four twice over, since its `versionCode` maths
splits on dots and would read `1` out of `0-beta` — a wrong number
rather than a failed build, and Android refuses anything not greater
than what is installed.

Four things about it are load-bearing:

- **The tag is pushed with a user PAT, not the Actions token.** Gitea,
  like GitHub, does not start a workflow from a ref pushed by a
  workflow's own token (go-gitea#33123). The token is what decides this,
  so `PACKAGE_TOKEN` is handed to semantic-release as the
  `repositoryUrl` credential and the push is attributed to a person.
- **That same limitation is used deliberately, once.** semantic-release
  calls the first release of a tagless repo `1.0.0` and offers no way to
  say otherwise, so a `v0.0.0` floor tag is what makes the first release
  `0.0.1` — and it is pushed with the *Actions* token precisely so it
  triggers nothing. All four publishers additionally skip `v0.0.0`
  explicitly, cleanly rather than by failing, because a floor is not a
  shipment.
- **The release page is the changelog, and that follows from the branch
  protection.** `@semantic-release/git` would push a `chore(release):`
  commit back to `main`, which the pre-receive hook rejects — *after* the
  tag had been pushed, leaving a tagged release the run then reports as
  failed. Whitelisting the CI user was the alternative and was declined:
  it weakens a protection someone set on purpose and lets a bot push to
  `main` without the checks every human PR passes. So the plugin is
  absent, `@semantic-release/changelog` writes to a gitignored
  `.release-notes.md` purely to carry the notes into
  `scripts/gitea-release.sh`, and `CHANGELOG.md` is a signpost to the
  releases page rather than a file that would silently stop updating.
  The workflow keeps its `chore(release):` guard anyway, for the day
  someone adds the plugin back.
- **An asset upload waits for the release to exist.** semantic-release
  pushes the tag in `prepare` and creates the release in `publish`, so
  the tag push that starts these workflows happens *before* there is a
  release id to attach to. `scripts/release-asset.sh` polls for it. The
  capacity-1 runner serialises things enough that this would usually work
  by accident, which is the worst kind of bug.

**Releases restarted at `0.0.1`, which is a downgrade on every channel.**
pacman and Homebrew both silently offer no upgrade from the old `1.x`,
and Android refuses the install outright — its remedy is an uninstall
that takes the user's library. This was chosen over pacman's `epoch` and
over offsetting `versionCode`, on the grounds that both are permanent and
a reinstall is once. `packaging/homebrew/README.md` and
`docs/android-release.md` say so where a user would look.

**`desktop-assets.yml` publishes Linux and nothing else, and macOS is not
an oversight.** `GOOS=darwin CGO_ENABLED=0` fails at
`wails/v3/pkg/mac: build constraints exclude all Go files` — the darwin
backend is Objective-C behind cgo, so a `.app` needs a macOS host and the
runner is a Linux container. That is exactly why the Homebrew formula
builds from source on the user's own Mac. Windows *does* cross-compile
cleanly (`GOOS=windows CGO_ENABLED=0`, a couple of seconds — oto uses
WinMM through `x/sys`, sqlite is modernc's pure-Go driver, WebView2 is
COM syscalls, MPRIS is `linux && !android`-tagged) and is deliberately
not published: no Windows build of this app has ever been *run*, and no
tier here can exercise one.

**`android-apk.yml` is the one that can lose something irrecoverable.** It builds the signed
`arm64-v8a` APK (the only ABI Android can run this app on — see
`app/build.gradle`) on every `v*` tag and publishes it to the *generic* registry, which is
readable without credentials — the reason Obtainium can poll a plain
URL. Android refuses to update an app whose signing certificate
changed, and the only remedy is an uninstall that takes the user's
library with it, so the job **refuses to build** without the keystore
secret rather than falling through to Gradle's debug-key default, and
**refuses to publish** an artifact whose certificate says `CN=Android
Debug`. It is deliberately not a job in `ci.yml`: that workflow runs on
every branch push, this one takes tens of minutes on a cold cache, and
the runner has capacity 1. `docs/android-release.md` is the operating
document.

Two jobs, both in an `ubuntu:24.04` container:

- **`check`** — no display: `make commit-check` over the push's commits,
  `make lint` and `make test` (three build configurations each),
  `tsc --noEmit`, `make ui-test`, `make bindings-check`,
  `make skill-check`.
- **`e2e`** — under a private D-Bus and **no display at all**, since
  v3's `-tags server` is a real headless mode: fixtures, a seed built by
  running the app (`curl` against the runtime endpoint — no browser),
  `make dev-headless`, then the Playwright suite
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

**`make lint`'s tag sets must stay identical to `make test`'s**, or
lint is checking configurations nothing builds. There are three, and
the app's is now the *default* tag set: v3 resolves GTK4 +
WebKitGTK 6.0, which Arch and ubuntu:24.04 both ship, so the
`webkit2_41` tag that used to be mandatory everywhere is gone. A
machine without `webkitgtk-6.0` can still build with `-tags gtk3`, but
that is an escape hatch, not what CI or a release builds.

## Packaging

**The Makefile is the front door and `Taskfile.yml` is an
implementation detail behind it.** `make dev`, `make build-dev`,
`make build-prod`, `make bindings` and `make e2e` all keep their names;
what changed underneath is that a build is now a Taskfile tree
(`Taskfile.yml` → `build/<platform>/Taskfile.yml`) rather than one
`wails build` invocation, and `wails3` is still a **vendored Go tool**
(`go tool wails3`), never a global install.

Four things about that tree bite anything outside the Makefile, and all
four bit the packaging recipes:

- **The tasks invoke `wails3` by bare name**, in 54 places across the
  scaffold files. `scripts/toolbin/wails3` puts that name on PATH
  pointing back at the vendored tool; without it a build dies at its
  first sub-task with `wails3: command not found`. The Makefile
  prepends it, and so must `packaging/arch/PKGBUILD` and the Homebrew
  formula.
- **`wails3 build` has no `-ldflags`, `-trimpath` or `-clean`** — those
  were v2's. `-trimpath` and `-w -s` are already in the production
  task's own flags; the version stamp goes through **`LDFLAGS_EXTRA`**,
  this repo's one edit to the scaffold Taskfiles (linux and darwin
  alike), passed as `wails3 task build LDFLAGS_EXTRA="-X 'main.version=…'"`.
- **The output is `bin/`, not v2's `build/bin/`.** `build/` is *tracked
  build assets* now.
- **Bundling is a separate step from building.** `task build` produces
  a bare binary on every platform; the macOS `.app` is `task package`.

**`build/`'s platform metadata is generated from `build/config.yml`.**
`wails3 task common:update:build-assets` rewrites `Info.plist`, the
`.desktop` template, `nfpm.yaml` and the Windows manifest from that
one file — so a hand edit to any of them is lost on the next refresh.
nfpm's `homepage` and `license` say in place that the refresh does not
own them, and **that comment is wrong**: a refresh reset them to
`https://wails.io` and `MIT`. Re-check those two after any refresh.

**That refresh does not touch the mobile trees**, contrary to what this
file said for five phases. `update build-assets` extracts only
`updatable_build_assets` (darwin/ios/linux/windows); `build/android/`
and `build/ios/` come from `generate build-assets`, which rewrites the
whole of `build/`. So `build/android/` is **committed and hand-edited
like source** — it was generated once into a scratch directory and
copied across (plan 015), it carries one deliberate edit to its
`Taskfile.yml`, and only its output is gitignored. `build/ios/` is
still not carried and its `includes:` entry is still dropped.
**Its `MainActivity` owns the safe area, because `targetSdk 35` does
not leave that to the theme.** Android 15 lays every app out
edge-to-edge and ignores the `statusBarColor`/`navigationBarColor` the
scaffold's theme sets, and the WebView is `match_parent`, so the page's
bottom band — the transport and, on a phone, the tab bar — would be
drawn under the gesture bar. `applyWindowInsets()` pads the container by
`systemBars | displayCutout | ime` and returns the insets rather than
consuming them; the window background is black to match the app's own
ramp, since that padding is what shows through. It is **pre-emptive**:
the phone this was checked against is Android 14, where the system still
insets the window, and the enforcement applies to an app *running on*
15. No browser tier can see this class of fault either way — a viewport
has no system bars.

**And a device is an engine, not just a screen.** The phone this app was
first run on renders in **Chrome 113** — two years behind every browser
any other tier uses — at a 424x439 CSS px viewport. It has `:has()`,
`color-mix()` and `dialog.showModal()`; it does **not** have relaxed CSS
nesting (Chrome 120, so a nested rule beginning with a bare element
selector is silently dropped), the Popover API (114, which Web Awesome's
popups set `popover="manual"` for), `light-dark()` or relative colour
syntax. So "it renders at that size in Chromium" is not evidence about
the phone, and resizing a spec cannot recover the missing signal. `make
android-inspect` forwards the WebView's devtools socket and `make
android-eval` asks the real page — raw CDP, because `connectOverCDP`
calls `Browser.setDownloadBehavior` and a WebView refuses it.

**One of those gaps is checked rather than remembered.** A nested rule
whose selector starts with an element name is not a parse error anyone
would notice on 113 — the rule simply does not exist, there and nowhere
else, which is how the bottom bar's `text-overflow: ellipsis` came to
have never truncated on the device. `make css-check`
(`frontend/scripts/check-css-nesting.mjs`, a pre-commit hook and a CI
step) fails on one, over every `frontend/*.css` and the `css` literals
alike — a glob rather than `index.css` by name, because the hook fires
on `frontend/**/*.{ts,css}` and a sweep that names one file goes green
over a stylesheet it never opened — and
says the fix is a leading `&` — valid in both syntaxes, so no nested
rule here has a reason to omit it. Two things it has to get right, and
both follow from asking whether a *style* rule is anywhere above rather
than what the immediate parent is: `@media (…) { bottom-nav { … } }` at
the top level is an ordinary rule and is the majority of what a regex
over the file would report, while the same rule one level inside
`.bar { @media (…) { … } }` is nested and is flagged. The check is the
cheap version of the answer; a build-time downlevel (Lightning CSS
targeting 113) would fix the class permanently and is a dependency and
a build step rather than twenty lines.

`build/config.yml`'s `version` is the
*metadata* version and is not what the app reports — `main.version` is
stamped at link time from the packaging recipe's git-derived version.

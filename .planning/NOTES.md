# Notes

Gotchas, measured facts, and things already considered and rejected.
Measurements carry the date they were taken — several of these are
properties of someone else's server and can change.

## MetaBrainz caps a client at ~2 MB/s (measured 2026-07-29)

`data.metabrainz.org` serves a single client at roughly 2.1 MB/s, and
**concurrency does not help**: one Range stream and four concurrent
lanes delivered 32 MB at 2,111,195 B/s and 2,209,000 B/s respectively,
while the same machine pulled 66.9 MB/s from a CDN. One of the four
lanes starved to 0.5 MB/s. The lanes divide a fixed cap; they do not
raise it.

Consequences:

- No client-side concurrency change will speed up a dump download.
  Pushing harder earns 503s (the reason `dumpLanes` is 4).
- Stage 1 of a full import costs ~11.8 h at best (89 GB after column
  projection). Before projection it was 205 GB — about 27 h.

This is the entire reason the catalog is built centrally and shipped as
an artifact rather than derived per install.

## Further stage-1 reductions, not yet taken

Both are CI-side options; neither is safe as a silent client default
because each changes *what gets counted*.

- **Project `recording_mbid` only** (24.1% of row-group bytes instead of
  43.4%): ~49 GB, ~6.5 h. `canonical_musicbrainz_data.csv` already
  carries `recording_mbid`, `release_mbid`, `artist_mbids` and
  `release_group_mbid`, so release/RG/artist counts can be rolled up
  locally. Cost: listens with no recording MBID are dropped, and artist
  totals become "sum of their recordings" rather than direct attribution.
- **Stride-sample members** (1-in-4): ~12 GB, ~1.6 h. The dump is flat
  numbered members (`0.parquet`, …). Sampling is viable because the
  counts only feed a *ranking* for a top-N cut. Must be a stride, never
  a prefix — if members are time-ordered a prefix biases hard toward one
  era.

## Incremental dump retention is 30 days (measured 2026-07-29)

The incremental directory held 30 dumps (series 2579–2610), and full
dumps land roughly monthly. An artifact older than ~30 days cannot be
topped up: the dailies bridging the gap are gone. That is a permanent
undercount of that window, not corruption — but it pins the artifact
republish cadence at monthly.

## Anonymous package download is UNVERIFIED

The client fetches the artifact from a fixed `latest` URL because Gitea's
package *listing* API requires a token while a plain file GET appears not
to — a probe of the not-yet-published artifact returned 404 rather than
401. **That is suggestive, not proof.** No artifact has been published
yet to test against. Confirm before relying on it.

Also worth deciding deliberately: every install pulling from a personal
Gitea makes its bandwidth and uptime a user-facing dependency.

## Migrations came back (2026-08-08), scoped to avoid the old failure mode

The "no migration chain" design below lasted until a real `make sandbox`
DB (schema pre-dating the `tagging_items.synthetic`/`parent_group_key`
columns) hit `no such column: parent_group_key` — `IF NOT EXISTS` had
silently no-op'd the `CREATE TABLE` on the existing table, columns and
all. A database written by an older build genuinely needed an upgrade
path; there wasn't one.

What came back is **not** the old 48-step chain. `sql/schemas/*.sql`
stays the single source of truth for the current shape (still what sqlc
reads, still what a fresh install gets verbatim). `sql/migrations/*.sql`
holds small numbered files — `ALTER TABLE ADD COLUMN`, `CREATE INDEX`,
etc. — that run after the schema files, tracked in `schema_migrations`,
tolerating "duplicate column name" as a no-op so the exact same files run
unconditionally on both a fresh database and an old one and converge on
one shape. See the "Schema changes need two things, not one" section in
CLAUDE.md for the column-order and index-placement gotchas this
implies, and `backend/database/migrations_test.go` for the regression
tests. Squashing `sql/migrations/` back into `sql/schemas/` and deleting
the migration files is fine pre-1.0 (see CLAUDE.md); stop once real user
databases exist.

The original decision this replaces, kept for why the old chain died:

`applySchema` created the whole schema from `sql/schemas/*.sql` on every
open; all DDL was `IF NOT EXISTS`. A database written by an older build
was not supported and there was no upgrade path, by design.

Two things that removal fixed, worth not reintroducing:

- The 48-step chain was ~3,700 of `database.go`'s 4,061 lines, plus
  helpers that existed only to serve it (`backupDatabase`,
  `readLibraryDirFromTOML`, `isDuplicateColumnErr`, …).
- `sql/schemas/` had drifted badly from the real schema — it still
  described a `genre_recordings` table that migrations had renamed, and
  omitted `explore_index`, `http_cache`, `artist_images`,
  `similar_artist_map`, `release_to_rg`, `lyrics_index` and
  `artist_metadata` entirely. sqlc reads that directory, so it had been
  generating against a stale schema and silently missed columns such as
  `audio_files.modified_at`.

  The new design's answer to this specific risk: `sql/schemas/` is
  never edited to describe something migrations already did elsewhere
  — it's edited to directly declare the target shape, and migrations
  exist only to carry an old on-disk database to that same shape. There
  is exactly one hand-maintained description of "what does the schema
  look like", same as before; migrations don't add a second one.

**When regenerating schema files from a live database, remember the seed
rows.** `file_types` (the four supported extensions), `player_state` and
`queue` each carry `INSERT OR IGNORE` rows that `sqlite_master` does not
contain. Dropping them breaks every audio-file foreign key.

## ANALYZE runs after the catalog merge, not at schema creation

The old migration 45 ran `ANALYZE` once. With the migration chain gone
there is no equivalent moment — an empty database has nothing to measure
— so it runs at the end of the artifact import instead
(`SearchIndex.analyzeIndex`). Without current statistics the planner
mis-estimates the partial expression indexes on `explore_index`
(`idx_explore_title_lower`, `idx_explore_artist_lower`) and scans a
million rows for queries that should seek.

If another path ever populates the catalog, it needs the same call.

## Writers, not readers, are responsible for name quality

`resolveArtistName` falls back to returning the artist MBID when it
cannot find a name. That is fine for a one-off render but must never be
persisted — an MBID stored as a title is unsearchable and shows as a
UUID in the UI.

This used to be defended at every read (`title != mbid` predicates) and
in the upsert's conflict rules. Those defenses are gone; `AddFromCache`
now refuses to write a name equal to the MBID and lets the upsert's
"non-empty wins" rule fill it in when a real name arrives.
`TestAddFromCacheNeverStoresMBIDAsName` guards this.

## Attached databases are invisible to the read pool

`database.DB` holds two handles: a single-writer connection and a
separate query-only pool. `ATTACH` binds to one connection, so anything
touching an attached database must use `ExecContext`/`QueryRowWriter`
(the writer) — `QueryContext` routes to the pool, where the attachment
does not exist and the query fails with "no such table".

## FTS triggers are defined in Go, not in the schema

`explore_index`'s three FTS sync triggers live in
`exploreIndexFTSTriggers` in `database.go` rather than in
`sql/schemas/explore_index.sql`, because the bulk-load path drops and
recreates them (`SuspendExploreIndexFTS`). Defining them in both places
would be two copies free to drift.

Bulk loads must suspend them: measured on a real import, assembly runs at
~31 rows/s with the triggers attached and ~4,700 rows/s without.

They are now **dropped and recreated** on every open rather than created
with "already exists" tolerated, because a trigger is a definition: an
existing install would otherwise keep the first one it ever got, and
these definitions are where this table's write cost is decided.
`explore_index_au` is scoped to `UPDATE OF title, artist_name, aliases`
with a `WHEN` guard on them having changed, so the common write — an
upsert whose merge rules keep every existing value — re-indexes nothing.

## The writer stall that broke playback is only half explained (2026-08-14)

The user's report — play an album, the track changes, the transport
stays paused, nothing appears in the queue, the play button does
nothing — was diagnosed on the running app: 91% of its CPU was
`BackfillLibraryDiscographies` → `upsertBatch`, with four of its six
workers parked in `sql.(*DB).conn` waiting for the single write
connection, while the play path did its writes inline under `q.mu` and
`p.mu`. The locking half is fixed and tested (see CLAUDE.md, "Playing a
track does not wait for the database to hear about it").

**What is not explained is why those upserts were so expensive.**

> **Recovered note, 2026-08-14.** The rest of this section was lost to a
> mishandled `git stash --keep-index` before it was ever committed; what
> survives above is verbatim, and the two arguments that followed
> "Two things argue against the obvious answer" are gone. Re-derive them
> from a profile before acting on this — do not treat the question as
> answered.

## Explore "library only" toggle was removed (2026-08-06)

The Explore UI used to have a "library only" mode toggle
(`frontend/src/store/explore-settings.ts`, `explore:libraryOnly` in
localStorage) that filtered the Explore UI to owned content only. It was
removed outright — the app now always shows full (network-enriched)
Explore data. If offline/library-only mode is wanted again, it should be
built from scratch rather than restored; the old implementation gated
several component code paths in ad hoc ways. A separate
`deep_catalog_enabled` backend flag briefly existed for the same idea and
was removed earlier, when the dump importer left the app binary.

## The dev server is a real, drivable app (verified 2026-08-10)

`wails dev` binds an HTTP + WebSocket server on `localhost:34115`
(`internal/frontend/devserver/devserver.go`) that serves the frontend
with the generated bindings on `window.go` and bridges every call and
every `runtime.EventsEmit` to the **same** Go backend the desktop
window uses. A browser pointed at it is not a mock — measured:
`queue.Queue.GetState()` returned real JSON, `SetVolume(42)` produced a
real `VolumeChanged`. Multiple clients are supported by design
(`notifyExcludingSender` fans events to the other web clients *and* the
desktop frontend). See plan 005.

Four facts that cost time to find:

- **The GTK window cannot be suppressed.** `devserver.Run` ends in
  `d.Frontend.Run(ctx)` with no flag to skip it, so headless needs
  Xvfb. Nobody upstream has a way around this.
- **Build the dev binary directly.** `app_dev.go` parses `-devserver`,
  `-assetdir`, `-loglevel` from `os.Args`, so
  `go build -tags "dev webkit2_41"` plus those flags gives the same
  server with no watcher, no reload broadcast and one PID.
- **A binding call with wrong argument types hangs forever.** The
  backend logs `error parsing arguments` and never fires the callback,
  so the caller's promise never settles. Always use a timeout; the app
  log is the only place the reason shows up.
- **`dbus-run-session` does not break audio, and fixes MPRIS.** It
  replaces the bus, not `/run/user/1000`, so PulseAudio still works and
  `org.mpris.MediaPlayer2.yellowjacket` registers on the private bus.

## Playwright's WebKit does not run on Arch (measured 2026-08-10)

`playwright-cli install-browser webkit` downloads fine and then fails to
link: its Linux build wants Ubuntu 24.04 libraries (`libicu74`,
`libWPEWebKit-2.0.so.1`, `libflite`) that Arch does not provide, and the
dependency check emits `apt-get` advice. So `--browser=webkit` — the
cheap way to approximate the WebKit2GTK renderer we actually ship — is a
CI-only capability. Local browser work is Chromium, which is unaffected.

## The fixture library is generated, and generated by our own writers

`test_data/music_library_test/` is produced by `cmd/gentestdata`
(`make testdata`), not committed — 31 tracks across MP3, FLAC, Ogg
Vorbis and WAV, ~700 KB, ~1 s to build. Two rules keep it honest:

- **Tags are written by `backend/tagwriter`, not by ffmpeg.** ffmpeg
  only encodes (with `-map_metadata -1`); every tag comes from the same
  writers the app uses, so a fixture and the reader under test cannot
  drift into agreeing with each other and disagreeing with reality.
  `tagwriter.WriteFileTags` exists for this — it is the format switch
  `WriteUntrackedFileTags` already had, lifted out so tooling with no
  app to construct can call it.
- **The manifest hash covers the spec, not the bytes.** ffmpeg stamps
  encoder version strings, so identical specs produce different bytes
  on different ffmpeg builds. `test_data/music_library_test.manifest.json`
  hashes paths, formats, durations, tags and cover identity instead,
  and lives *outside* the library root so the scanner never sees it.

Fixtures are selected in tests by *case* (`testfixtures.CaseCoverDedup`,
`CaseUnicode`, `CaseDuplicates`, …) rather than by path. Deliberately
malformed files live in a sibling root, `test_data/music_library_broken/`
— the clean library's track count has to stay at exactly 31 for seeds
to be verifiable, and a zero-byte `.flac` in the scanned tree used to
get swept into `testFlacFiles` and fail the duration parser.

## WAV tags are write-only (found 2026-08-10)

`backend/tagwriter` writes WAV tags into a RIFF `id3 ` chunk, and
`backend/metadata` reads through `dhowden/tag`, which recognises MP3,
FLAC, OGG, MP4 and DSF and has **no RIFF parser at all**. So every tag
the app writes to a WAV is invisible to the app that wrote it, and WAV
tracks always scan in untitled — visible in the Artists view, where the
fixture library's WAV tracks produce no "Field Recordings" artist.

The fix is small (unwrap the `id3 ` chunk and hand the payload to
`tag.ReadFrom`) but was out of scope for plan 005.
`TestWAVTagsAreNotReadableYet` asserts the gap so that fixing the
reader turns into a failing test rather than nothing at all.

## The headless harness: how to run this app without a window

`make dev-headless [SEED=<name>]` starts the app in the background and
returns; `make dev-stop`, `make dev-logs`. It runs the dev *binary*
(`go build -tags "dev webkit2_41"`), not `wails dev`, under
`dbus-run-session -- xvfb-run`. See plan 005 and
`scripts/dev-headless.sh` for why each of those three is load-bearing.

**Seeds are built by running the app.** The first-run wizard's
dismissal condition is not a config file — it is
`GetAllLibrariesWithTrackCounts()` returning something — so
`make sandbox-seed NAME=<n>` boots a fresh `YJ_HOME`, calls the real
`AddLibrary` binding through `playwright-cli`, waits for the real scan
to reach the manifest's track count, stops the app with SIGTERM so the
shutdown hooks persist state, and tars the result. Never hand-write a
`config.toml` and DB rows: that is a second description of a valid
`YJ_HOME`, free to drift, exactly like the migration chain was.

Seeding points `YJ_CORE_INDEX_URL` at a dead address on purpose, so no
seed depends on what the explore artifact server was serving that day.

Two parsing traps, both already paid for:

- `playwright-cli` echoes the evaluated source back after the result,
  so scraping its output for bare digits picks up numbers from your own
  JavaScript. Return a tagged sentinel (`'YJTRACKS' + '=' + n`) and
  grep for that.
- Waiting on a fixed sleep or on a scan event is worse than waiting on
  the observable outcome. Polling the track count the app itself
  reports also validates the fixture manifest against the real scanner.

## Restoring a database needs foreign keys *off*, not deferred

`/__test/db/restore` (`backend/testctl`) copies every ordinary table
out of an ATTACHed snapshot. The obvious implementation — one
transaction with `PRAGMA defer_foreign_keys = ON` — fails at COMMIT
with a bare `FOREIGN KEY constraint failed (787)` that names nothing.

Deferring postpones the *check*; it does not stop `ON DELETE CASCADE`
from firing. Tables are copied in name order, which is not dependency
order, so `DELETE FROM libraries` cascades away rows of a child table
that was already restored earlier in the loop, and the final state is
genuinely inconsistent.

`PRAGMA foreign_keys` is a no-op inside a transaction, so it has to be
set on the connection around it. That is safe only because the writer
is a single connection (`SetMaxOpenConns(1)`); the restore re-enables
enforcement afterwards and runs `PRAGMA foreign_key_check`, so a bad
restore is reported instead of left in place.
`TestRestoreRoundTrip` pins it.

Two related traps in the same path: FTS5 virtual tables cannot be
written with `SELECT *` and their shadow tables (`_data`, `_idx`,
`_docsize`, `_config`) must be rebuilt rather than copied — but the
prefix test that excludes them must not swallow `explore_index`, an
ordinary table whose name is a prefix of two virtual ones.

## The event bridge hooks EventsNotify, not EventsOn

`.playwright/init-events.js` records backend events by wrapping
`window.wails.EventsNotify`. That is the single choke point: wails'
`ipc_websocket.js` does `case "n": window.wails.EventsNotify(msg)` and
fans out to listeners from there, so one wrap captures all 46 events
whether or not the app subscribes to them. Wrapping `EventsOn` would
have needed 46 registrations and would have missed anything the app
does not listen for.

`window.wails` does not exist when an initScript runs, so the script
installs an accessor on `window` and wraps at assignment time (wails'
`main.js` does a plain `window.wails = {...}`), then redefines the
property as a plain value so nothing downstream can tell.

The buffer lives on `window.__yjEvents` with `wait()`, `reset()`,
`names()`, a `ready()` that resolves only when a binding actually
round-trips, and a `call()` that **times out** — a binding invoked with
wrong argument types never fires its callback, and a 2s rejection
naming `.dev/app.log` is worth more than an infinite hang.

## Small harness traps, each of which cost a cycle

- **Paths in `.playwright/cli.config.json` resolve against the config
  file's directory**, not the repo root. `".playwright/init-events.js"`
  becomes `.playwright/.playwright/init-events.js`.
- **`playwright-cli` and `@playwright/test` have separate browser
  caches.** The CLI working is no guarantee `npx playwright test` can
  launch; it needs its own `npx playwright install chromium` (the
  runner wants `chrome-headless-shell`, which the CLI never fetched).
- **`getByRole('button', { name: 'Play' })` also matches "Add queue to
  playlist".** Accessible-name matching is substring by default; the
  transport controls need `exact: true`.
- **Every fixture track except one is 2–6 seconds.** A spec that plays
  a track and then clicks pause races the track ending and fails
  against a correct UI. Use the 90-second `Long Player`
  (`edge-lengths`), exported as `LONG_TRACK` from `e2e/support`.
- **`e2e/` needs `"type": "module"`** or Playwright transpiles the
  specs to CJS and every `import.meta` in the support code throws
  "Cannot use 'import.meta' outside a module" — reported as
  "No tests found".

## The component tier fakes two globals, and that is all it fakes

`frontend/wailsjs/` is a pure passthrough: every generated binding is
`window['go'][svc][Type][Method](args)` and every runtime call is
`window.runtime.X(...)`. So `frontend/test/support/wails-fake.ts`
replaces those two globals and nothing else, and the tests then run the
*real* generated bindings and the *real* store code. No module mocking,
and no second description of the Wails layer to drift from the first —
the same discipline `sql/schemas/` and the seeds get.

The dispatcher mirrors wails' own `internal/frontend/runtime/desktop/
events.js`, which matters in two places: listeners registered with
`maxCallbacks` expire and are removed mid-iteration, and `EventsEmit`
from the frontend notifies local JS listeners *before* it notifies Go
(so a frontend emit is observable in-page).

Four things that cost time:

- **Store singletons are constructed at module import**, so the fake
  must be installed from `setupFiles`, and any store that reads config
  in its constructor loads before a test can stub it. `test/setup.ts`
  carries import-time defaults for exactly those. Without them a store
  caches `undefined` where Go would have sent `[]`, and four
  components then crash on `.length` — which reads as a component bug
  and is not one.
- **`vitest.config.mts`, not `.ts`.** The repo's vite config is
  `vite.config.mts`; a `.ts` sibling cannot import it, and `mergeConfig`
  is how the `@go`/`@store`/`@components` aliases get reused rather
  than restated.
- **Vitest 4 takes a provider factory, not a string.**
  `provider: playwright()` from `@vitest/browser-playwright`, which is
  a third package beyond `vitest` and `@vitest/browser`, and needs its
  own `npx playwright install chromium` — a third browser cache after
  `playwright-cli`'s and `@playwright/test`'s.
- **Pre-bundle Web Awesome or Vite reloads mid-test.** Its components
  are one deep import per element; discovering them lazily makes Vite
  re-optimise and reload the page underneath a running test. The glob
  `@awesome.me/webawesome/dist/components/*/*.js` in `optimizeDeps.
  include` settles it.

Screenshots need the app's surface, not the default white page: the
setup file imports `@store/theme-store` for its side effect (it applies
the `--yj-*` ramp to `:root`) and sets the two `index.css` declarations
that matter, or a component renders white-on-white and the baseline is
blank. And a `@lit-labs/virtualizer` list never produces two identical
frames, so `toMatchScreenshot` on `<queue-panel>` fails with "could not
capture a stable screenshot" rather than a diff — assert its rows
instead.

## Binding drift is now checked, and it is fast

`frontend/wailsjs/` is generated by `wails`, **not** by `go generate`,
so the pre-commit codegen check never covered it: a renamed Go bound
method or struct field first showed up at runtime, in a window, as a
call that never settles. `wails generate module` (v2.10.2) rebuilds it
in ~1.5 s, which is cheap enough to gate a commit on —
`scripts/bindings-check.sh`, `make bindings-check`, and a `lefthook`
pre-commit entry. Verified by renaming `queue.GetState` and watching it
fail.

One quirk: the generator rewrites the three `wailsjs/runtime/` files as
mode 755 every run. That is not drift, so the check compares with
`git -c core.fileMode=false` and restores the modes afterwards.

## Emitting an event is now one call, and it cannot kill the process

`events.Emit(ctx, name, data...)` (`backend/events/emit.go`) is the
only supported way to push a Wails event; `TestNoDirectRuntimeEmits`
fails the build on any other `runtime.EventsEmit` in the tree.

The reason is that `runtime.getEvents` (wails `runtime.go:47`)
`log.Fatalf`s — `os.Exit`, unrecoverable — whenever the context lacks
its internal `"events"` value. That is any `context.Background()`, so
in-process service tests were impossible and background workers that
outlived their context could take the app down at launch.

Four packages had independently discovered this and hand-rolled a
guard (`library.emit`, `download.emit`, `playlist.emitEvent`, and
`autotagservice.emitEvent` with a whole `ctxReady` field). Nine other
sites guarded on `ctx != nil`, **which does not help** — a non-nil
context without the runtime is exactly the fatal case. The wrapper
replicates wails' own precondition once and drops at debug level.

Three things worth knowing:

- **The test sink rides in the context**, `events.WithSink(ctx, rec)`,
  not in a package global. A global cannot survive `t.Parallel()` and
  would put a mutex on every production emit.
- **`events.Deliver` is `Emit` that returns `ErrNoRuntime`**, and has
  exactly one caller: `/__test/emit` in `backend/testctl`. That
  endpoint exists to *impersonate* a backend emit, so answering `200`
  for an event that reached nobody would send you debugging the
  frontend for a backend no-op. Ordinary emitters want `Emit`.
- **Enforcement is a walk of the tree, not a lint rule.**
  golangci-lint runs once per build configuration, so a stray emit in
  an `indexbuild`- or `dev`-tagged file is only visible to the pass
  that compiles it. One text walk sees all three, plus anything tagged
  out entirely. Two traps if you touch that test: the needle has to be
  built at runtime or the file matches itself, and it has to be the
  qualified selector (`.EventsEmit(`) or it matches the test's own
  function name.

What it unblocks is a fourth test tier — services, in-process, with no
app: `backend/queue/emit_test.go`, `backend/config/emit_test.go`,
`backend/playlist/emit_test.go` assert on the payload the *frontend
receives*, which had never been covered. Two gotchas found writing
them: `config.Save` refuses to write a config that was never
`Load`ed (so a test that only calls `applyDefaults` sees its second
setter fail, not its first), and `queue.SetQueue` resolves anything
over `initialBatchSize` in a background phase, so assert with
`rec.Wait` rather than immediately after the call.

## Docs are split by tense, and the split is checkable

Three places now describe this repo, and the rule for which one a new
paragraph goes in is **grammatical, not topical** — a topical split
("architecture here, testing there") is what rots, because every new
fact has two plausible homes.

- `.planning/NOTES.md` — **past**: measured, dated, append-only.
- `CLAUDE.md` — **present**: what the system is, and why.
- `.pi/skills/yellowjacket-dev/` — **imperative**: what to run, in what
  order, and what it looks like when it fails.

So the skill carries the checklist for a schema change and CLAUDE.md
carries the reasoning behind the two-file rule; the skill carries the
headless lifecycle and CLAUDE.md carries only the invariant that seeds
are produced by running the app. Phase 6 deleted about half of
CLAUDE.md's harness section on those grounds.

`make skill-check` (`scripts/skill-check.sh`, pre-commit) makes it
enforceable: every `make <target>` mentioned under `.pi/**/*.md` must
exist. That is the actual anti-drift mechanism — the Makefile is the
source of truth for *how* to invoke something and the skill only decides
*which*, so a renamed target fails a commit instead of sending an agent
confidently at a command that no longer exists. A skill that documents a
command slightly wrong is worse than no skill.

One shell trap it cost: under `set -euo pipefail`,
`x="$(make -pqRr | awk … )"` sinks the whole assignment, because
`make -q` exits non-zero whenever a target is out of date and `pipefail`
propagates that. Wrap it in `{ …; || true; }`.

## The skill was followed cold, and lost time in exactly one place

An agent that did not write `.pi/skills/yellowjacket-dev/` brought the
app up from a wiped `.dev/` and no fixture library, drove a flow the
skill does not describe (open the queue panel, toggle shuffle, assert
on `QueueModeChanged`, confirm against `queue.Queue.GetState`) and
stopped it — about a minute of wall clock, no dead ends. All four
tiers then re-ran green from that cold state: 313 ui-test, 0 issues ×
3 lint configurations, 3 test passes, 19/19 e2e.

The one expensive thing was a genuine config bug, not a doc error.
`.playwright/cli.config.json` had `outputDir: "../.playwright-cli"`,
written on the belief — which `references/harness.md` stated as a flat
rule — that every path in that file resolves against the config file's
directory. **Only `initScript` does.** `outputDir` resolves against the
shell's cwd, so every snapshot and console log was landing in
`/home/logan/Development/.playwright-cli`, one level *above* the repo:
outside `.gitignore`, outside `find`, and invisible to the obvious
`ls .playwright-cli/`. That directory still held a stale snapshot from
the previous session, so the obvious `ls -t | head -1` returned it
silently, and the transport buttons appeared to have lost their
accessible names — a fabricated regression in phase 3's work that took
a DOM walk to disprove. Reading a *stale* artifact is much worse than
reading none, because it answers.

Four smaller corrections, all now in the skill:

- `make sandbox-seed` already depends on `make testdata`, so listing
  both made the fixture step look separately required. It also takes
  ~10 s with warm caches, not the ~30 s claimed.
- `make ui-setup` and `make e2e-setup` are once-per-clone
  prerequisites and are *not* dependencies of `make ui-test` /
  `make e2e`. The skill never mentioned them; on a fresh clone both
  fail with a missing-browser error that reads like a broken test.
  This matters for CI, which has no warm caches by definition.
- `snapshot` prints a *path*, not the tree. Not said anywhere.
- `make dev-stop` does not close the browser session;
  `playwright-cli -s=yj close` is a separate step.

And one place the tooling taught the opposite of the skill:
`scripts/dev-headless.sh`'s own success banner suggested
`eval "async () => await window.go.queue.Queue.GetState()"` — a bare
`window.go` call with no timeout, which is precisely the hang the
banner's next paragraph warns about. A gotcha documented in prose and
contradicted by the copy-pasteable line three inches above it will lose
every time; the banner now prints the `__yjEvents.call` form.

## An ALSA null PCM is enough for CI audio, and it clocks

Phase 7's job 2 needs playback to actually advance, because
`e2e/specs/playback.spec.ts` asserts the elapsed clock moves — a
missing audio device fails it in a way that reads like flake, since
`app.go` joins `InitSpeaker` failure into `startupErr` and lets
everything else work.

Measured locally, with PulseAudio made unreachable
(`XDG_RUNTIME_DIR` pointed at an empty dir, `PULSE_SERVER=none`) and
`ALSA_CONFIG_PATH` pointing at four lines:

```
</usr/share/alsa/alsa.conf>
pcm.!default { type null }
ctl.!default { type null }
```

`InitSpeaker` succeeded in 36 ms and all six playback/queue specs
passed, including "the elapsed time advances". oto/v3 talks to
libasound directly, and ALSA's `null` plugin advances its pointer on a
timer rather than discarding instantly, so beep's stream is consumed at
real-time rate. **No PipeWire, no PulseAudio and no daemon of any kind
is required in the container** — one env var and a file.

Also found while setting this up: `scripts/dev-headless.sh` does *not*
set `YJ_CORE_INDEX_URL`; only `scripts/seed-sandbox.sh` does. So a
seeded run started by hand still reaches for the real explore artifact.
Harmless locally, a network dependency and a minute of wall clock in
CI — job 2 must set the dead-address override itself.

## CI was prototyped in a container before it was written, and it found a real bug

Both jobs of `.gitea/workflows/ci.yml` were built as shell scripts and
run to green in a bare `ubuntu:24.04` container (`docker run -v
repo:/src -v cache:/cache`) before a line of YAML existed, then the
YAML was transcribed back out of the workflow and re-run in the same
container to prove the transcription. That is worth the extra half
hour on a self-hosted runner: the alternative is push-and-see, and a
Gitea Actions run that never starts looks exactly like one that passed.

**`make lint` was linting three configurations nothing builds.** All
three passes omitted `webkit2_41`, so wails resolved `webkit2gtk-4.0`.
Arch still ships `webkit2gtk-4.0.pc`, so it passed locally and had
done for the life of the repo; Ubuntu 24.04 dropped 4.0, and the
**`dev` pass** fails there — wails' own `app_dev.go` is `dev`-tagged
and drags in the 4.0 assetserver, which the other two passes never
compile. The tag sets now match `make test` exactly
(`webkit2_41`, `webkit2_41 indexbuild`, `webkit2_41 dev`). Still 0
issues × 3 on Arch, and now 0 × 3 on Ubuntu too. Note what this means:
"lint passes" and "the thing lint compiled is the thing we ship" were
different claims, and only a second distro could tell them apart.

Five smaller container facts, all now comments in the workflow:

- **`libasound2-dev`, not just `libasound2t64`.** oto/v3 dies at
  `pkg-config --cflags -- alsa` before a line is compiled.
- **`PLAYWRIGHT_BROWSERS_PATH` unifies the location, not the
  revisions.** `@playwright/cli` bundles its own `playwright-core`
  pinned to a different Chromium build than `e2e/`'s
  `@playwright/test`, so both must install into the shared directory.
  Installing one gives the other "Browser chromium is not installed;
  expected executable at …/chromium-1237/…". The "three separate
  browser caches" trap survives being pointed at one path.
- **`git config --global --add safe.directory`** or `bindings-check`
  fails on a clone the container user does not own.
- **The runner already mounts and exports `GOMODCACHE`, `GOCACHE` and
  `GOLANGCI_LINT_CACHE`** for every job via `container.options`, and
  `valid_volumes` is a glob over the cache root
  (`/home/logan/docker/gitea/data/runner/cache/**`), so new caches need
  no runner-side change. Only the Node-side ones had to be declared.
- **The fixture hash is deterministic per ffmpeg, not across
  versions**: `5425fbb454a2` on Arch (ffmpeg n8.1.2), `599a8dd4f152`
  on Ubuntu 24.04. Nothing asserts a literal hash, so this is
  harmless — but a test that pinned one would be portable only by
  accident.

**Setting `YJ_CORE_INDEX_URL` for the *app* run, not just for seeding,
is worth 8x on the suite.** `scripts/dev-headless.sh` never set it —
only `seed-sandbox.sh` did — so a seeded local run still fetches the
real explore artifact, and `testctl.spec.ts`'s restore then copies
every table of a database full of catalogue: 42 s locally, versus a
whole 19-spec suite in 7.3 s in CI with the artifact stubbed out.

## Playwright's WebKit passes, so it gates

19/19, in the same container, ~11 s on top of Chromium's ~7 s. It had
never been run anywhere before — Arch cannot start it — so the honest
default would have been advisory. Running it once in a throwaway
container turned a coin flip into a decision: it is a **required**
step.

Two things make that safe rather than brave. Nothing in `e2e/`
compares pixels — every assertion is an event payload, a `data-testid`,
an attribute or backend state, and the `toMatchScreenshot` baselines
live in the Chromium-only Vitest tier — so a WebKit failure cannot be
antialiasing noise; it is an engine difference in custom-element
upgrade, a11y-tree shape or event ordering, which is exactly the
WebKit2GTK signal we otherwise have no way to get. And it is cheap
enough that the earlier plan to scope it (skip `testctl.spec.ts`,
which tests Go and has no engine content) is not worth the
complexity at 11 s.

## Every Go typecheck needs a built frontend, and a shared prototype dir hid it

`main.go` embeds the built assets (`//go:embed all:frontend/dist`), so
`make lint`, `make test` and `make bindings-check` all fail on a fresh
clone with `pattern all:frontend/dist: no matching files found
(typecheck)` until `pnpm build` has run once. It never bites locally
because anyone who has started the app has a `dist/` lying around, and
it is not a Go dependency anything declares — which is why CI is the
only place it shows up. Job 1 now builds the frontend before linting.

**The prototype missed it for an embarrassing and reusable reason.**
Both job scripts were run against the *same* mounted directory, and
job 2 runs `dev-headless`, which builds the frontend. So job 1 was
silently consuming an artifact job 2 had produced on an earlier run,
in an order CI never uses. A container proved the commands work; it did
not prove the *inputs* were what CI would have, because the directory
had accumulated state exactly the way a developer machine does.

The fix for the technique, not just the workflow: verify each job in a
**fresh `git clone --no-hardlinks` of the pushed commit** (a plain
`git clone` of a repo on the same filesystem fails with "Invalid
cross-device link" into `/tmp` on a different device), not in an rsync
of the working tree, and never two jobs in one directory. The
distinction that matters is not clean-vs-dirty but *whose* dirt: a
working-tree copy carries a developer's accumulated build output, which
is the one thing CI is supposed to be checking you do not depend on.

## A cached view needs a lifecycle, and so do its controllers

Plan 007 phase 1. `index.ts` caches primary views and hides them with a
class so `scrollTop` survives navigation — a deliberate, good decision
that nothing else was told about. `disconnectedCallback` therefore
never fires, and every document listener, interval and subscription a
view registers runs for the session. The measured cost was not a leak:
pressing `s` on **Settings** skipped albums out of the Autotag queue,
because `autotag-view`'s document keydown handler was still live.

Three things that were not obvious before doing it:

- **A focus-only scope rule would have been a regression.** The
  shortcut service resolves a panel scope by walking up from the
  focused element, and this app is driven with the mouse: focus sits on
  `<body>` almost always. Panel bindings would only have worked after
  a click landed inside the panel, where the old document listener
  worked always. Hence the *ambient* scope claimed by the active view
  (`services/shortcut-scope.ts`) as a fallback after the focus walk.
- **Shared reactive controllers have the same bug.**
  `ContextMenuController` bound three document listeners in
  `hostConnected`, which for a cached host never un-happens. A
  controller cannot know whether its host is cached, so
  `registerViewAware` lets it ask, and it keeps connection-based
  behaviour on hosts that are not.
- **Off-screen views were still rendering.** Store controllers call
  `requestUpdate()` on every subscriber, so one keystroke in the search
  box re-rendered eleven pages, ten of them invisible. The mixin
  withholds the update and replays it on activation, which is why
  coming back to a view still shows current state.

Re-running a view's *load* on activation is not free and is not always
right: `autotag-view`'s `startQueue()` resets the selected folder and
refetches candidates over the network, so it stays once-per-mount and
only the local folder list refreshes on return.

## A local timer is not a clock, and a fixed grid row is not a notice board

Plan 007 phase 2. Both halves of the finding were reproduced by hand
first, and both reproduced exactly as measured in August: the seek bar
read `00:44` against a backend at `73` after four keyboard seeks, and
a queue with a moved file in the middle stopped dead at index 0 with
nothing emitted and `IsPlaying` false.

Four things worth keeping:

- **Phase 1 moved the reproduction.** With a track row focused the
  arrows belong to the grid, so the keyboard seek does not fire from
  the track list at all any more — the 30 s desync only reproduces
  with focus off the grid. A fix verified against a stale
  reproduction would have "passed" without ever running the code
  path. Re-run the reproduction on the current build, not on the
  audit's description of it.
- **A push of state needs an identity and a sequence.** The store is
  a singleton and keeps the last position, so a seek bar mounting
  later adopts it: without `trackChangeId` on the payload that is a
  stale reading rendered as current. And without a monotonic `seq`,
  a report of the same second as the last one is indistinguishable
  from no report, so the interpolation it is supposed to reset keeps
  running. Both are cheap on the emit side and impossible to add
  later without touching every consumer.
- **`.bottom-bar` is a fixed `4em` grid row.** An inline message laid
  out inside it squeezes the transport out of its own footer, which
  looks like a broken player rather than a message. It floats above
  the bar (`position: absolute; bottom: calc(100% + 4px)`), which is
  also the right answer for anything else that wants to speak from
  down there.
- **Reporting by event beat returning an error.** The plan wanted the
  queue bindings to return `error`; the failure that mattered most —
  auto-advance onto a bad file — has no caller to return to. An event
  covers both, and the stores kept a `.catch()` per call for the
  bridge-level rejections that a return value never described anyway.

## A level says how loud, not where, and the bottom band is taken

Plan 007 phase 3. The audit's ~30 "the failure is invisible" findings
were one problem wearing thirty hats — there was nowhere to put a
message — so the surface shipped whole: four levels, one store, one
presentation, and the callers routed through it in the same pass.

Five things worth keeping:

- **A level is not a location.** Blocking, Persistent and Transient say
  *how loud*; Inline says *not global*, which is not the same as
  saying where. An inline notification therefore carries a **region**
  (`player`, and whatever comes next) and the app-level host ignores
  it. Without that field the "one component with four presentations"
  would have become two components with two stores, which is the exact
  thing this phase existed to delete.
- **The bottom of the window belongs to the player.** The stack was
  first anchored above the player bar, beside the player's own floating
  notice. That looked right at 1440×900 and overlapped at 800×600,
  because the player's notice grows *upward* by however many lines its
  sentence needs. The stack moved under the header. Anything anchored
  to the bottom edge is sharing a band with something whose height is
  not known in advance.
- **Some backend errors are already sentences.** `describeError` maps
  runtime causes to copy, but the sentinels this app writes for its own
  conditions ("a library with that name already exists") are the most
  useful thing that could be shown, and mapping them to a generic line
  would have been a regression. `explainError` repeats a message with
  no Go/HTTP noise markers and defers to the map otherwise. The
  distinction is whether *we* wrote the string, not how long it is.
- **C4 and M1 are the same bug from either end.** The library store
  cached a stale answer because a fetch outlived the selection, and
  hung its waiters forever because a failed fetch never satisfied the
  "loaded and not loading" predicate they watched for. Both go away by
  holding the request itself and stamping it with a cache generation —
  one change, two findings, and the four hand-written `waitFor*`
  helpers deleted.
- **A reproduction can fail for the wrong reason.** The e2e spec for a
  rejected binding renamed the decoy library to its own name, which the
  backend accepts as a no-op: red at the right assertion, having never
  induced the failure it was named for. It only became a reproduction
  once it picked its row by the seeded library's name. A failing test
  is evidence of nothing until you have watched *why* it fails.

One operational note: `make bindings-check` requires a clean working
tree for `frontend/wailsjs/` and reports staged changes as dirty, so it
cannot pass mid-phase on an uncommitted tree. Regenerating and diffing
by hand (`go tool wails generate module -tags webkit2_41`, then
`git diff -- frontend/wailsjs`) is the equivalent check.

## Measuring is the work; the icon CDN was serving Pro

Plan 007 phase 4, items 1–2 of 8. This phase is verified by numbers
rather than by assertions, which changes what "first" means: the first
deliverable is not a fix, it is a 50 000-track library and a script
that takes four measurements against it. Both fixes then landed with a
before/after, and both had a reproduction that was watched failing.

Six things worth keeping:

- **A measurement library is not a fixture library, and should share
  nothing but its generator.** `test_data/music_library_test` is
  curated *cases* selected by name; `.dev/music_library_bulk` is a pile
  whose only interesting property is its size. Generating 50 000 files
  through ffmpeg is ~40 minutes, so the bulk one encodes six clips once
  and copies them — but it still tags every file through
  `backend/tagwriter`, because a library the app cannot read back
  measures nothing. 11 s, 466 MB, gitignored, and deliberately not a
  dependency of `make test`.
- **The first cover renderer made a 2 GB library.** The fixture cover's
  diagonal band is ~37 hard edges at 300 px, which is the worst case
  for a JPEG DCT: ~35 kB per album, nearly all of it artefacts around a
  pattern nobody looks at. A smooth gradient is ~6 kB and just as
  distinguishable. 466 MB instead of 2 GB.
- **Instrument the bindings, not the symptoms.** "Finishing a track
  refetches the library" became a fact rather than an inference by
  wrapping every method on `window.go` and recording call, duration and
  serialized size. The generated bindings look their target up at call
  time (`window['go']['library']['Library']['GetAllTracks']()`), so
  post-hoc wrapping catches a store that imported the wrapper long ago.
  Pair it with a `longtask` PerformanceObserver: a 25 MB JSON parse on
  the main thread appears there and nowhere else.
- **A debounce will happily measure nothing.** "Keystroke to paint"
  against the next frame gave 16 ms on every build, because
  `search-bar` debounces 150 ms and 16 ms is the input echoing its own
  character — a number that cannot move, and therefore cannot be
  evidence. The measurement has to wait past the debounce for the
  render the keystroke caused.
- **The icon CDN was serving Font Awesome _Pro_.** Every SVG fetched
  from the kit host carries a "Commercial License" comment, so the
  obvious fix — save what the app already downloads — is a licence
  violation. Font Awesome **Free** 7.3.1 (CC BY 4.0) has all 64 names
  the app uses, is redistributable with attribution, and moved no
  `ui-visual` baseline. Check what a CDN is actually serving before
  vendoring it.
- **Some icon names cannot be found statically.** Twenty call sites
  compute one from state (`jobIcon(job)`, `TONE_ICONS[tone]`,
  `this.favCtrl.iconName`), so the list is committed and *checked at
  runtime*: the resolver records a miss and renders a fallback, and an
  e2e sweep across every view asserts there are none. A missing icon
  used to be invisible because the CDN had everything; it now has to be
  findable instead.

And two findings that did not survive contact:

- **`perf.M1`/`M2` no longer reproduce.** One keystroke costs 49.9 ms
  net of the debounce with **zero** long-task blocking at 50 000
  tracks, not the predicted 50–100 ms across every mounted view —
  because Phase 1 stopped off-screen views rendering, which was M1's
  mechanism. An audit finding can be fixed by an unrelated phase, and
  re-measuring before fixing is how you find out.
- **`perf.M7`/`M8`'s unbounded caches did not show as heap growth**
  across a ten-view scripted browse (37 → 38 MB post-GC). They are
  real by inspection, but the reproduction has to be a long Explore
  session, and it should exist before the LRU does.

One correction to the audit's own numbering, since two phases cite it:
in `perf.md` the icons are **M9** (the plan's Phase 4 prose calls them
C1), the whole-library refetch is **C1**, the selection wipe is **C2**,
and **C3/C4 were already fixed in Phase 3**.

## A ticker is a hidden dependency for everything that forgot to speak

Plan 007 phase 4, items 3–5 (`C5`, `M6`/`H-14`, `M10`). Three fixes,
three new measurements, and one bug shipped-and-caught inside the same
session — the useful part of which is *how* it was caught.

Five things worth keeping:

- **Deleting a polling loop is never only a deletion.** The explore
  index emitted its status every 3 s forever, with an identical payload
  once ready, which re-rendered the whole settings page for the life of
  the session. Every path that *mutates* the status already emitted, so
  the ticker looked purely redundant. It was not: `si.ready = true` and
  `si.cancel = nil` both change what `emitStatus` derives and neither
  announced itself, so the ticker was carrying two transitions within
  three seconds of their happening. Removing it left the header badge
  reading "Building search index" over an index the settings page
  called ready. Before removing a poll, enumerate the writes to
  everything it reports — `rg 'si\.ready = |\.cancel = '` was the whole
  audit, and it should have come first rather than second.
- **The screenshot found it; no test did.** The Go tests passed, the
  436 component tests passed, all 36 e2e specs passed, and the numbers
  were exactly the improvement predicted. The contradiction was two
  labels 700 px apart in a PNG. "Read the PNG" earns its place in the
  gate on cases like this — the app was *self-inconsistent*, which no
  assertion was looking for because nobody had thought to.
- **A "0 ms" measurement is usually a broken measurement.** View-open
  time waited for `#main-content > :not(.view-hidden)`, which matches
  the view being navigated *away from* — it is still on screen until
  the incoming chunk resolves. Every view, every build, 0 ms. This is
  the same failure as the 150 ms debounce from the first pass, and the
  same tell: a number that cannot move is not evidence. Both times the
  fix was to wait for the specific thing, not for a generic selector.
- **A before/after must differ in exactly one thing, and `git stash` is
  not a way to arrange that** on a tree carrying four uncommitted
  phases. Stashing `frontend/index.ts` to measure the pre-split bundle
  also reverted the bundled-icon registration living in the same file:
  22 cross-origin requests, and a baseline for a build that has never
  existed. The honest baseline was made by *adding the static imports
  back* to the current file — a change that undoes the one thing being
  measured and nothing else.
- **The cheapest half of a fix is often the one the audit did not
  name.** `perf.C5` is written as an event handler that over-fetches,
  and it is. But `playlistStore` is a singleton constructed at import
  time and eagerly warmed itself as well, so every launch paid the same
  2.6 MB whether or not Playlists was ever opened — the event costs
  that on a user action, the constructor costs it on every start. "Only
  when there is a subscriber" turned out to be a two-line change that
  beat the patching logic it was written to support.

And one thing about splitting a bundle: **report the trade, not the
win.** Route splitting moved 666 kB out of the pre-paint path (1 480 →
814 kB) and cost up to 6 ms on the *first* open of a view, once per
session, hidden further by warming the chunks on idle. But first
contentful paint did not move at all, because at localhost speeds over
a warm cache 666 kB of JS is not what the paint was waiting for. The
number that improved is real and is the one that costs on a cold start
and under WebKit2GTK; the number a user watches did not change. Saying
both is the difference between a measurement and an advertisement.

## "We looked and saw nothing" is only evidence if the thing that fills it ran

Plan 007 phase 4, items 6 and 7 (`M7`/`M8`, `M3`/`M4`). Two fixes, two
new measurements, and one finding that two previous sessions had come
within a sentence of deleting as unreproducible.

`perf.M7` says the Explore art caches are never evicted. Two sessions
measured a ten-view scripted browse, saw the heap go 37 → 38 MB
post-GC, and recorded the finding as "real by inspection but it does
not show up". Both were right about the number and wrong about what it
meant: **the browse script navigates to Explore and never types in it,
and both caches are filled only by a search.** It was measuring a view
with two empty maps. A session of twenty-four searches grows the heap
20.58 MB and is still accelerating at the end.

Six things worth keeping:

- **A negative result inherits the coverage of the thing that produced
  it.** "We browsed ten views and the heap was flat" sounds like
  evidence about caches; it is evidence about ten navigations. Before
  believing a finding did not reproduce, check that the code path it
  names actually executed — here, one `console.log` of
  `thumbnailCache.size` would have ended the question two sessions
  earlier. Phase 4 has now had three findings evaporate on contact
  (`M1`, `M2`, and half of `M8`) which makes the fourth *look* like the
  same thing, and that prior is exactly what made it cheap to accept.
- **A bound cannot be verified by a run that never reaches it.** The
  first bounded build measured identical to the unbounded one: twelve
  searches cached 180 thumbnails against a cap of 192, so nothing was
  ever evicted. This is the same trap as the 150 ms debounce and the
  `:not(.view-hidden)` selector, in its third costume — *a number that
  cannot move is not evidence* — and the tell is the same one every
  time: before and after are suspiciously equal.
- **Two caches holding the same string means bounding one frees
  nothing.** `explore-view`'s `artistImageCache` and
  `exploreCache.artists` both hold the artist photo's base64 data URL,
  ~128 kB each, measured at 2.30 M chars in *both* maps. Capping either
  alone leaves every string pinned by the other, and the measurement
  would have read as a fix that did not work. The cap is a shared
  exported constant now. Before bounding a cache, find every reference
  to what it holds.
- **The audit named the wrong two maps.** `M8` calls out `artistAlbums`
  and `artistTopTracks` as holding discographies and top-track lists.
  Nothing in the app has ever written to either — their only callers
  were a component test. Deleted rather than bounded. The map that
  actually retains is one the audit does not mention.
- **A measurement library optimised for size can remove the property a
  finding is about.** `M3` is "the Art column renders a 1500×1500
  original into a 24 px box". The bulk library's covers are 300×300 and
  3.7 kB, because generating 50 000 realistic covers made a 2 GB
  library and a smooth gradient made a 466 MB one. So the bytes saved
  here are 3.7 kB → 1.1 kB and prove nothing. The number that is not
  hostage to the fixture is **which tier was requested** — 26 of 26
  originals before, 0 after, true on any library. When the rig cannot
  show the magnitude, measure the mechanism.
- **An audit's arithmetic is a hypothesis too.** `M4` predicts 250 000
  comparisons per scroll frame from 5 000 albums × ~50 visible cards.
  Measured: 24 visible cards, and the scan breaks on its first match,
  so it costs **1.46 ms per frame** — real, 146× improvable, and far
  below the long-task threshold, so it moves no user-visible number
  today. Worth fixing because it stops scaling with the library, not
  because anything was stuttering. Say which of those two it is.

One operational trap that cost a cycle and is now in the skill:
**`make e2e` needs `SEED=default`.** Run against the bulk seed left
over from a measurement session, 13 of 36 specs fail on fixture
content — unicode tracks, fixture artists, the seeded playback file —
and the failure list reads exactly like a regression in the change you
are holding.

## A virtualizer repaints on its own properties, and the sloppy thing doing that may be load-bearing

Plan 007 phase 4, item 8 (`M5`) and part of the tail (`p3`, `m1`, `m7`,
`p4`). One large fix, three tail items settled, one audit
recommendation rejected as a bug, and a broken feature that no audit
had noticed.

The mechanism under most of it is one sentence: **`<lit-virtualizer>`'s
rows are rendered by the `virtualize` directive, and that directive
runs when one of the *virtualizer's own* properties changes — not when
its parent re-renders.** Everything below follows from that.

Seven things worth keeping:

- **Memoising `items` and hoisting `renderItem` together is how you
  build a list that never repaints.** Virtualizing the playlist views
  needed both (that is the point), and selection went silently dead:
  the controller held exactly the right keys and no row ever showed
  one. Nothing failed — 447 component tests, 36 e2e specs and every
  Go test stayed green. A click in the real app found it in ten
  seconds. The fix is what `track-list` has always done and nobody had
  written down: push `virtualizer.requestUpdate()` on a selection
  change and on a playing-track change.
- **The same fact makes `perf.m1` a regression.** It asks for
  `artists-view` and `genres-view` to hoist their per-render arrow
  functions to stable fields "as `cover-grid` already does". That fresh
  closure is the only thing changing a virtualizer property on a host
  update, i.e. the only thing repainting the cards. Measured in the
  running app: 1 highlighted card before the change, 0 after, both
  views. There is no compensating win — the host mostly re-renders
  *because* card state changed — so the closures stay, and
  `card-grid-repaint.test.ts` fails on the change and exists for no
  other reason. **An audit's suggested fix is a hypothesis too**, and
  this is the first one in this phase that was actively harmful rather
  than merely wrong about magnitude.
- **Two of `M5`'s four stated mechanisms did not survive
  measurement.** "lit removes and re-adds 10 000 listeners per pass" is
  false on any build: instrumenting `EventTarget.prototype` recorded
  **zero** add/remove calls per pass, because lit-html's `EventPart` is
  itself the listener (`handleEvent`) and a changed listener value
  updates a field rather than the DOM. And one update pass cost 5.3 ms,
  not a stall. What was real, and worse than predicted, was elements
  retained: **22 090** for a 2 000-track playlist against the audit's
  16 000, and 2 000 eager cover requests. Fixing the two real halves
  gives 487 elements and 0.
- **The suggested fix would have cost two features.** "Render these
  through `<track-list>` the way `genre-details` does" holds for
  `genre-details` because a genre list is just tracks. Both playlist
  views render phantom rows for missing files, and `playlist-details`
  is a drag source and a drop target; `track-list` has never had
  either. Virtualizing in place got the same 45× on elements with none
  of the risk, and left `track-list` alone for its four other callers.
  Check what the reference implementation *does not* do before adopting
  it.
- **A row inside a virtualizer needs `width: 100%`.** The virtualizer
  positions children absolutely, so a grid row shrinks to fit its
  content: the columns silently stopped lining up with the header above
  them. Caught by reading the screenshot, not by any assertion — the
  second time in this phase that a PNG found what the suite could not.
- **A write with a `RETURNING` clause is still a write.**
  `CreateSmartPlaylist` issued its `INSERT ... RETURNING` through
  `DB.QueryContext`, which routes to the query-only read pool, and
  failed with "attempt to write a readonly database (8)" — so **no
  smart playlist could be created at all**, in any real build. It was
  invisible because `NewTestDB` shares one in-memory connection and
  leaves `readDB` nil, so `reader()` hands back the *writer* under test:
  every unit test of that path exercised a handle production does not
  have. `TestNoWritesOnTheReadPool` now walks the tree for the class,
  watched failing on the bug first. A test double that collapses two
  handles into one cannot see a bug about which handle you used.
- **`p3` is right about one store and wrong about the other.**
  Coalescing `search-store`'s notify to a microtask makes a subscriber
  that unsubscribes synchronously after a `setTerm` miss the
  notification entirely — a semantic change, and one an existing test
  had already pinned deliberately. `playlist-store` took the fix; the
  keystroke store did not. "Make these five consistent" is a fine
  instinct and a bad rule when one of them is on a different path.

And two operational notes, both now in the skill:

- **A frontend edit is not live until the app restarts.** Vite HMR
  updates the module, but an already-registered custom element class
  cannot be re-registered, so the running page keeps the old one — the
  edit reads as having done nothing. Worse, a *build error* leaves the
  dev server serving the last good bundle, silently: a stray backtick
  inside a comment in a `css` tagged template literal ended the literal,
  esbuild failed, and the page kept rendering the previous CSS while
  `make dev-headless` printed nothing about it.
- **`tsc --noEmit` is in CI and was not in the documented gate.** The
  previous pass left the tree failing it, under a fully green
  `make lint && make test && make ui-test && make e2e` — none of which
  typechecks `frontend/test/`.

## An audit's magnitude and its mechanism are two claims, and the fix is a third

Plan 007 phase 4, fifth pass: the `track-details` chunk split and `m6`.
Two items, both landed, and the pass's one useful generalisation is
that a finding is really *three* hypotheses — how big it is, why it is
that big, and what to do about it — which can be independently right
and wrong.

`perf.m6` got the first right, the second wrong, and the third half
wrong:

- **Right about size.** "Select all → Edit tags at 50 000 tracks will
  hang the renderer." Measured through the real opener: **3.0–6.3 s**
  of blocked main thread, varying that much run to run on one build.
  It is the largest single stall this phase has found, and it was in
  the *minor* tier of the audit.
- **Wrong about why.** The audit calls it O(selection × total) —
  2.5 × 10⁹ comparisons. It is not: select-all hands the opener its
  keys *in list order*, so each `find` matches at index *i* and the
  real cost is N²/2, quadratic in the **selection**. That matters for
  what it predicts about everything else: the audit's formula says a
  ten-track selection costs 500 000 comparisons (it costs about 50),
  and says nothing about the genuine worst case, which is a selection
  built from the *bottom* of the list.
- **Half wrong about the fix.** "Keep an index-ordered selection, and
  build a `Map<FilePath, Track>` for the batch lookup." The map is the
  entire 50× (**3 051–6 298 ms → 68 ms**), and it is now
  `utils/track-index.ts`, a `WeakMap` keyed on the array's identity —
  the invalidation signal this app already relies on everywhere else.
  The index-ordered selection is the unsafe half: an index goes stale
  on any re-sort, re-filter or refetch while a file path survives all
  three, which is exactly why `retain()` drops `lastSelectedIndex` and
  keeps the keys. The helper it would have replaced measures **3 ms**.
  Three milliseconds does not buy a silently mis-ordered queue insert.

That is the second audit recommendation in two passes that would have
shipped a bug, after `m1`. Both times the reason was the same: the
audit reasoned from the shape of the code and not from what the rest of
the file already knew about it.

Five more things worth keeping:

- **A `longtask` entry arrives after the task that produced it.** The
  new measurement's first run reported `blocking: 0 ms` next to a
  six-second wall time, because it read the buffer synchronously after
  the await. Sixth variant of this phase's most-repeated trap, and the
  first one caught by *another number in the same row* contradicting
  it rather than by suspicion. Two numbers that must agree are worth
  more than one number you have to be sceptical about.
- **The first load after a rebuild is not a measurement of first
  load.** FCP read 96–112 ms on every run taken immediately after
  `make dev-headless`, and 28–32 ms on the very next run of the same
  build. A cold Vite module graph, not variance. The plan had been
  describing this as "±100 ms run to run" for three passes without
  naming it.
- **Measurement labels are a flat namespace; audit IDs are case
  sensitive.** `before-m6`/`after-m6` already existed — the *second*
  pass's capital `M6`, an unrelated finding about a 3 s ticker. Naming
  a baseline after a finding would have overwritten two of them.
- **An unreachable code path still costs bundle size, and “dead code”
  can mean “missing feature”.** `cover-grid` is one of the five
  components that opened `track-details`, and it cannot: its album
  dropdown is rendered by `renderSplitGrid`, which `connectedCallback`
  references only to satisfy `noUnusedLocals` and which is, by its own
  comment, never invoked. Expanding an album fetches its ten tracks and
  draws nothing. The audit files this as `perf.p2`, "an unreferenced
  `renderSplitGrid`", under housekeeping. It is a whole interaction
  that does not exist, and it was only visible from trying to use it.
- **What keeps a chunk out of a bundle is the absence of an import,
  which nothing notices.** Five static imports were what put
  `track-details`'s 42 kB before first paint; adding one back costs
  nothing anybody would see, because the chunk is also warmed on idle
  and the dialog carries on working. `lazy-track-details.test.ts` reads
  the five sources and fails on a returning import — the same shape as
  `TestNoDirectRuntimeEmits`, and for the same reason: the invariant is
  about what the code *does not* say.

## A finding's magnitude is measured where the work runs, not where it is written

Plan 007 phase 4, sixth pass: `m5`, `m4`, `m2` — the end of the tail,
and the phase. Three items, one of which was measured and then
*dropped*, which is the outcome the discipline exists to allow.

The generalisation the pass added to the previous one's "an audit's
magnitude and its mechanism are two claims": **a mechanism can be
exactly as described and still cost nothing, because the cost depends
on state the reading cannot see.** `perf.m5` is right that
`now-playing.updated()` interleaves layout reads with style writes on
every pass, and right that the component updates while playing. It is
wrong by two orders of magnitude, because a 1 Hz position report
changes nothing that component renders — so the layout is clean when
the reads happen and they cost 3 µs. The interleave only flushes when
the DOM actually changed, measured at 0.103 ms, 34× more. The fix is
still right (52 forced layouts over six seconds of playback became 2),
but the number that justifies it had to be found by making the DOM
dirty on purpose.

Seven things worth keeping:

- **A guard is only correct if it lists everything the measurement
  depends on, including things a CSS rule adds.**
  `.will-scroll .scroll-content` has `padding-right: 2em`, so applying
  the scroll class changes the distance the marquee has to travel:
  −128 px before the class, −158 px after it. The audit's "guard on the
  value/flag they already track" reads as "guard on the text", and a
  text-only guard would have left every first hover scrolling 30 px
  short — silently, with no test in any tier able to see it. That is
  the **third** audit recommendation in three passes that would have
  shipped a bug, after `m1` and `m6`, and all three failed the same
  way: reasoning from the shape of a function instead of from what the
  rest of the file already knows about it.
- **Measuring is also how you decline to fix something.** The same
  finding names `artists-view` and `genres-view`, which do one
  `querySelector` and two `style.setProperty` per pass and **no layout
  read at all** — 0.0033 ms, one percent of their own update pass. They
  are the two files `perf.m1` was rejected in, where a guard risks
  stopping the virtualizer seeing a changed property. Three
  microseconds does not buy that risk, and "measured, declined" is a
  better record than a silent omission.
- **A finding can be half-fixed by a phase that was not about it.**
  `m4` describes two components registering document `mousemove` in
  `connectedCallback` "for the process lifetime". Phase 1 had already
  moved `track-list`'s onto `listenWhileActive`, so half the finding
  described a build a year of work had passed. Check the line the audit
  cites still says what it said.
- **An N+1 finding is usually also an N-bytes finding, and the audit's
  fix may only address the N.** All three `m2` sites want `FilePath`
  and ask for whole track rows to get it: five genres cost **6 MB over
  the IPC**, which the suggested `GetTracksByGenres([]string)` would
  have preserved exactly while removing four round trips. Returning
  paths made it 1.29 MB. Ask what the caller does with the answer
  before batching the question.
- **Return grouped, not flattened, when the caller owns the order.**
  An album list is sorted by name and a genre selection by click order;
  a flattened result would have reordered a queue silently. The new
  bindings return `map[int64][]string` / `map[string][]string`, which
  also serves the drag cache — a fourth N+1 site the audit does not
  name, and the one that fires most, since it warms on every selection
  change rather than on a menu action.
- **`make generate` was emitting TypeScript that does not parse.**
  `genevents` prefixed only the *first* line of a const block's doc
  comment with `//`; Phase 4's first pass gave `events.go` two
  multi-paragraph comments; so regenerating `frontend/src/events.ts`
  wrote bare prose into an object literal. It is a pre-commit hook, so
  the failure was waiting for whoever next touched a `.sql`, a `.templ`
  or an event constant. Nothing caught it because nobody had run the
  generator since the comments were written. **A generator is only
  verified by running it**, and a hook that regenerates is a hook that
  can break a clean tree.
- **The `wailsjs` delta is 13 lines across *two* files**, both
  `autotagservice/Service.*`, not five as three sessions of notes have
  said. It is 25 across four now, the extra 12 being this pass's two
  library bindings.

And three on measuring, all of which produced a wrong number first:

- **"First run cold, second warm" is not a rule.** First contentful
  paint read 100 then 96 on one build this pass, and 28 then 76 on
  another — the second run warmer in neither. FCP varies ±50 ms here
  for reasons the harness does not control. The honest response is to
  report it as unattributable, not to take a third run until it agrees.
- **A confirming run against the wrong seed looks like a result.** A
  re-run taken straight after `make e2e` measured the *default*
  library, because `make e2e` needs `SEED=default` and the app was
  still on it: "Play 20 albums" went from a number to a dash and the
  artist's bytes fell 40×. Plausible in shape, meaningless. The tell
  was a row that stopped having a value at all.
- **Selection highlighting read from an inactive view measures Phase 1,
  not a repaint bug.** Driving `artists-view` after navigating with a
  raw `navigate` event showed the controller holding one selected
  artist and zero highlighted cards — the exact signature of the
  virtualizer hazard, and entirely an artifact: `viewActive` was
  `false` and an off-screen view does not render. Through a real
  sidebar click: one highlighted card, `aria-selected="true"` on the
  right one, in both card grids. Check `viewActive` before believing a
  view did not repaint.

## A reproduction read too early is a fix applied to nothing

Plan 007 phase 5, first pass: the track list's arithmetic, a window
minimum the layout can actually hold, and one page header for nine
views. Three items, one inherited one-liner, and the pass's own
contribution to this plan's longest-running theme — *a number that
cannot move is not evidence* — which appeared twice more here, both
times in a **reproduction** rather than in a measurement.

`config-page`'s rename bug is real: the library name's click bubbles to
the document handler that closes the rename editor, so it opens and
closes it in the same click. But the probe that "reproduced" it read
`.edit-input` synchronously after a synthetic `.click()`, and Lit
renders on a microtask — so it reported "not editing" on the broken
build *and* on the fixed one. The fix looked like it had done nothing,
which nearly bought a second, unnecessary fix; the real check needed one
`await`. The same shape then failed an e2e spec of mine, which captured
a header count immediately after a navigation and got `null`, making
every assertion after it vacuous.

Seven things worth keeping:

- **A screenshot disagreeing with a number is the useful signal, not a
  puzzle to explain away.** A viewport ladder reported the top bar
  overflowing by 0 px at every width while the PNG showed the job
  indicator cut off at the edge. The badge was `display: none` — no job
  was running by then — so nothing was overflowing and the "bug" was a
  rendering of the app working. Two numbers that must agree are worth
  more than one number you have to be sceptical about, and a picture
  counts as one of the two.
- **An audit's symptom can outlive its mechanism.** `H-11` says the app
  title "wraps into the nav" below 700 px. It does — but the title
  block is 80 px tall inside a 64 px bar at *every* width, including
  1440; what changes at 780 px is the *subtitle* taking a second line.
  Fixing the visible half is a breakpoint on the subtitle. The
  permanent 16 px was never the finding and is still there.
- **Fixing the stated cause does not always remove the stated
  symptom.** With the 40 px arithmetic fixed, Duration still reads
  "Durat…" at 800 px — because the column is at its 50 px floor and the
  *label* no longer fits, while the values do. Same screenshot,
  different mechanism. Worth writing down, or the next reader
  reasonably concludes the arithmetic fix did not land.
- **Count the copies before calling something a duplicate pair.** The
  audit names Albums and Tracks as the two views with a sort toolbar;
  `playlist-view` had a third copy of the same twenty lines. The fix
  was worth 1.5× what the finding implied.
- **What a model carries decides what a control can offer.**
  "Artists and Genres have no sort control" is one finding and two
  different fixes: genres have a track count to sort by, and
  `library.Artist` carries nothing countable at all. A select with one
  option is a control that does nothing, so that view gets a label and
  a direction button.
- **Backend state outlives the page, and a spec that spends it fails
  the *next* run.** `view-lifecycle.spec.ts` toggled shuffle and never
  toggled it back, so a second `make e2e` against the same app failed
  `playback.spec`'s shuffle assertion — in a list that reads exactly
  like a regression in the change you are holding, which is what I
  assumed for half an hour. Stashing the phase's source changes and
  re-running the same specs is what proved it pre-existing; the same
  file also skips an autotag album per run, out of eleven, which is
  inherent and now in the skill. **Restart the app before believing an
  e2e failure you did not cause.**
- **A header that appears with the data is the layout problem it was
  meant to fix.** The first version of `<page-header>` rendered only
  once a view had loaded, and showed "0 playlists" while loading. Both
  were caught by reading a screenshot rather than by any assertion. The
  header now renders during load and omits the count until there is an
  answer — `null` meaning "no answer yet", which is a different thing
  from zero and has to be a different value.

## An audit ages against the code, and the oldest claims are the least checked

Plan 007 phase 5, second pass: the five hand-rolled dialogs, the context
menu's keyboard model, the ARIA tail, and landing on Home. `a11y.md` was
the least verified material in the repo — three of its 34 findings had
been touched before this pass — and treating each one as a hypothesis
was worth it three times over.

The generalisation the pass adds to "an audit's magnitude and its
mechanism are two claims": **a finding also has a date, and the code has
moved since.** Three of the findings here describe a build that no
longer exists, in three different ways:

- **Fixed by a phase that was not about it.** `a11y.12` lists five
  silent async surfaces. The first is `config-page`'s private toast,
  which Phase 3 *deleted* — and the surface that replaced it has had
  `role="status" aria-live="polite"` since the day it was written. Two
  of five bullets were already closed.
- **Half-fixed, so the stated mechanism is now wrong.** `H-9` says the
  Home card's missing-art placeholder "has no background". It has one;
  it is `--yj-bg-surface`, which is almost exactly the page colour, and
  it holds a `wa-icon` — which has rendered at all only since Phase 4
  bundled the icons. "Renders as nothing" was *literally* true offline
  when the audit was written and is now merely nearly true. Fixing the
  stated cause would have changed one line and nothing visible.
- **Reproduces differently.** `H-9`'s other half says "all three shelves
  show the same seven albums". There are five shelves and the
  duplication is one adjacent *pair*. The fix is still right; a rule
  written from the sentence rather than from the page would have been
  aimed at three shelves that do not exist.

Seven more things worth keeping:

- **The existing tests caught two bad versions of a new rule; the new
  test caught neither.** "Suppress a shelf that repeats the one above"
  is one line of intent and three of policy. Version one collapsed a
  four-album library to a single shelf. Version two, guarded by a fixed
  shelf size, let an 11-album library keep three identical shelves while
  a 13-album one lost them. The rule that survives is **"a repeat is a
  fault only if a different row was possible"** — the shelf must not be
  showing the whole library — and the reason it is right is that it is
  about the library rather than about a constant. A test written for a
  change tests the change; the tests already there are what test the
  system.
- **A default that is a behaviour has to be changed in two places, and
  one of them is a test suite.** Landing on Home broke **eleven** e2e
  specs. Nine assumed the track list is on screen at startup. One failed
  because every primary view stays in the DOM and Home names the same
  artists, so an unscoped `getByText().first()` matched a card on a
  `.view-hidden` page. And one was a real bug: `getByRole('button',
  {name: 'Shuffle'})` resolved to *two* elements, because Home's
  page-header action and the transport's shuffle mode had the same
  accessible name and had never been on screen together. A cached view
  is in the accessibility tree from the first paint, so "these two
  controls are on different pages" stopped being true the moment the app
  started on one of them.
- **A component test against hand-built markup cannot see a web
  component's own lifecycle.** Two of the three things that made the
  menu keyboard model work are invisible to it: `wa-dropdown-item` sets
  its `role` in its *own* first update, so a query at the host's
  `updateComplete` finds no items at all; and `focus()` on a `wa-popup`
  that has not positioned itself is a silent no-op. Both produce a menu
  that opens and refuses to take focus. Both were found by driving the
  real app, and the e2e spec exists because the component test passes
  either way.
- **The rule for a live region is about ordering, not markup.** Most
  screen readers announce a *change* to a region they are already
  watching and ignore one that appears with its content already in it —
  which is why `catalog-scope-notice` had a `role="status"` that
  announced nothing. So the regions render unconditionally and empty
  and only their text changes, and `now-playing`'s is in **both** render
  branches, because the branch with no track is the one that has to be
  mounted before the first track arrives.
- **`aria-selected` on `role="button"` is not useless, it is dropped.**
  Four grids whose entire ctrl/shift interaction exists to produce a
  selection were publishing it into a void. The fix is not an attribute
  but a role: `listbox`/`option`.
- **A backtick in a comment inside a `css` tagged template literal ends
  the literal.** The skill has warned about this for two plans. I did it
  twice in one session — once in a component, where `tsc` pointed at the
  line, and once in `tokens.css.ts`, where **every test file in the
  suite failed to import** and the output reads like a broken test
  runner. If the whole tier dies at once, suspect the shared module.
- **Migrating a dialog can delete feedback nobody listed.**
  `config-page`'s remove-library spinner lived *in* the hand-rolled
  overlay, so moving the confirmation to `confirmAction()` left a
  backend call of unknown length with no indication it had started. The
  state field it used had no reader afterwards, which is the tell:
  `removingLibraryId` now means "which row is busy" and the row says so.

And one on the harness, stated carefully because it is the kind of
thing that gets over-claimed: **`e2e` has failed in CI on every push
this session, including one that changed no application code at all** —
`9f03b3f`, a workflow file and a shell script. `check` passed on all of
them. Together with `9e92721` (docs only, recorded last pass) that is
two failures which cannot have been caused by the commit they ran on.

But **it does not follow that this session's failures are the same
one**, and I could not check: this Gitea build does not expose the
job-log endpoint, and the runner is not on this machine, so the log
tail the skill describes is out of reach from here. The suite passes
locally, twice consecutively, all 48 specs — **on Chromium only**. CI
also runs **WebKit**, which cannot run on Arch at all, and this pass
changed focus management, dialog modality and roles, which is exactly
the area where the two engines differ. Treat the WebKit half as
unverified rather than as the known audio-clock flake until someone
reads the log.

## "Out of reach" is a claim about a tool, not about the information

Plan 007 phase 5, third pass: Settings' keyboard reach, the `?` overlay
and the arrows, an Album column, and the CI question two sessions had
recorded as unanswerable.

The generalisation the pass adds: **when a tool says it cannot get
something, that is a fact about the tool.** `gitea_ci job_logs` returns
a 404 on this Gitea build and says so clearly, and two sessions read
that as "the log is out of reach from here" and reasoned from the
commits instead. The REST API on the same server answers fine, with the
token that was already in the environment:

```
GET /api/v1/repos/{owner}/{repo}/actions/runs/{run}/jobs   # per-step status
GET /api/v1/repos/{owner}/{repo}/actions/jobs/{id}/logs    # the whole log
```

Ten minutes, after a session and a half of careful hedging about what
the failure might be. The hedging was correct — it just cost more than
checking would have.

What the log said, in two parts:

- **The failure is the container's audio clock**, on both engines. Not
  a regression in the dialog/focus/menu work, which was the live worry.
  `playback.spec`'s elapsed time and two `player-truth.spec` cases: the
  UI interpolates while the backend position stays at zero, 17–18 s
  adrift. `ci.yml` says the ALSA null plugin advances at real time; it
  was measured once and no longer does.
- **WebKit had never run.** The step had no `if:`, so a chromium
  failure skipped it — `conclusion: skipped`, in the same JSON that
  held the answer. The previous pass's "treat the WebKit half as
  unverified" was more literally true than intended: the one place
  WebKit gets any coverage had produced no signal at all for as long
  as chromium had been red. With `if: !cancelled()` it runs, and both
  engines pass 48 and fail the same three.

Seven more things worth keeping:

- **A reproduction of the *fix* can be as invalid as one of the bug.**
  After making `←`/`→` reach the player again, the check measured zero
  `Player.Seek` calls — the same answer as the broken build, because
  nothing was playing and the dispatch records nothing with no track
  loaded. Seventh costume of this plan's most-repeated trap, and the
  first on the *after* side: "the fix did nothing" and "the probe
  cannot see anything" produce identical output.
- **A shortcut a dialog swallows is a promise the shortcut layer
  cannot keep.** The `?` overlay was written as a toggle. It cannot
  be: `focusedControlOwnsKey` yields every unmodified key to anything
  inside an open dialog, so the second `?` never reaches the service.
  Escape closes it, as it does every dialog here. The rule that
  protects focused controls is the rule that forbids the toggle, and
  an e2e spec is what noticed.
- **Every `wa-dialog` in this app is an unnamed dialog.** `a11y.md`
  lists them under "what is already correct" and says every one passes
  a `label` — true, and the label never reaches the accessibility tree.
  Web Awesome renders it into an `<h2 id="title">` in the same shadow
  root as the `<dialog>` and never sets `aria-labelledby`. Found by
  writing `getByRole('dialog', {name})` and getting nothing. Two more
  facts about locating one, both costing a spec run: the host is
  `display: contents` so it always reports hidden, and the slotted
  content lives in the *host's* shadow root, not in the dialog's
  subtree.
- **A fix can be right while the finding's stated benefit is wrong.**
  `H-15` wants an Album column so the three `Tideline / Aurora Fields /
  00:06` rows can be told apart. They are duplicates of the same
  album, so they still read identically; what distinguishes them is
  the duplicate-detection feature or a path column. The column is
  still the right default for every other row. Visible only in a
  screenshot — nothing failed, and the finding's sentence would have
  been ticked off without looking.
- **A section of controls that do nothing is worse than admitting the
  section does not exist.** `H-22` asks for a Playback/Audio section;
  `backend/config` has no output device, gapless, crossfade or replay
  gain to expose. Same judgement as "Artists cannot have a sort
  *select*" two passes ago, and the same tell: the audit describes the
  UI it wants without checking what the model carries.
- **A default the seed has already persisted is not a default you can
  see.** Changing `tracklist.DefaultColumns` changed nothing in the
  running app, because `.dev/seeds/default.tar` carries a `config.toml`
  from before it — while CI builds its seed by running the app and
  would have exercised the *new* one. A local run and a CI run testing
  different defaults is worse than either being wrong. `make
  sandbox-seed NAME=default`.
- **`aria-controls` has to name an element that exists**, which decides
  how a disclosure renders: `config-section`'s body is rendered
  unconditionally and toggled with `hidden` rather than added and
  removed. Nothing is paid for it — the slot's light-DOM children are
  in the DOM either way; a conditional `<slot>` only stops projecting
  them.

## A dependency with a rate has to be checked at its rate

Same pass, after the plan's three landings: the `e2e` job's red history
turned out to be one measurement being wrong, and the fix was worth the
generalisation.

`ci.yml` had made ALSA's `null` plugin the container's default device,
with a comment saying it "advances its pointer on a timer, so playback
is consumed at real-time rate". It does not. Measured in the CI image
under Docker, through beep and oto with the same `speaker.Init`
arguments `player.InitSpeaker` uses:

| default device | 3000 ms of audio consumed in |
|---|---|
| `type null` | **2.96 ms** |
| PulseAudio null sink | **3762 ms** |

A thousand times too fast. Every track finished instantly, so the
position reset to zero while the UI kept interpolating, and three specs
failed on a clock that never moved — 17–19 s adrift, which reads
exactly like the `H-3` bug Phase 2 fixed. `check` and `e2e` are both
green now, 54 specs on Chromium and 54 on WebKit.

Five things worth keeping:

- **A dependency with a *rate* needs a check at its rate.** Installing
  a sound device and asserting it exists is not the same as asserting
  it plays. The job now plays three seconds and fails if they take
  under two, in a step called "The sink plays at real time" — so the
  next regression names itself instead of surfacing three steps later
  as an app bug.
- **A failure that succeeds quietly is the expensive kind.**
  `InitSpeaker` returns nil in ~3 ms against both devices. Nothing
  logged, nothing errored; the only symptom was arithmetic in three
  specs. Two sessions read that as a flake and one as a possible
  WebKit regression.
- **The CI container is reproducible locally, and nobody had tried.**
  `docker run --rm ubuntu:24.04` reproduced the whole thing in four
  minutes and let the fix be verified — including under the private
  session bus and Xvfb `dev-headless.sh` runs the app in — before it
  was pushed. Every previous attempt to reason about this job reasoned
  from the *commits* instead, because the log looked unreachable
  (which it was not either).
- **Test the stack you ship, not one that resembles it.** `aplay`
  showed the same 1000× gap and would have been enough to *diagnose*.
  It would not have shown that oto opens the pulse-backed device at
  all, which is the thing that had to be true for the fix to work; a
  fifteen-line Go program using the app's own `speaker.Init` did.
- **The comment was the bug's hiding place.** "Measured: InitSpeaker
  succeeds in ~36 ms and all six playback specs pass" was true when
  written and had been carried forward through every subsequent read of
  that file, including two this session. A measurement in a comment
  needs the same expiry as one in a plan.

## A finding creates the conditions for the next one

Plan 007 phase 5, fourth pass: naming every `wa-dialog`, drawing
`cover-grid`'s album dropdown, and giving the album page a primary
action.

The generalisation this pass adds: **a finding is a door, and what is
behind it has never been looked at.** Three of the four items here had
a second bug hiding *behind* the one the audit named, and none of them
could have been found by reading — each was only reachable once the
first fix made the code path run for the first time.

- `perf.p2` files `renderSplitGrid` as "dead code carried in the
  bundle", housekeeping, delete it. It was a **missing feature** whose
  data path already worked, and behind it sat 1 463 lines that had
  never executed: `scroll-manager.ts` (916) and `album-dropdown.ts`
  (461). Drawing it revealed that **the albums grid could not scroll
  at all** — `.grid-scroll-container` is the markup `artists-view`
  uses and `cover-grid` had the class with no rule for it, so 186 984
  px of albums sat in a 772 px box at 5 000 albums, unreachable by
  wheel, keyboard or scrollbar — which in turn revealed that the
  scroll manager had spent its whole life saving and restoring a
  `scrollTop` that was permanently 0. And *that* revealed the shared
  context menu labelled "Album actions" on a track row, which nothing
  could observe while the only menu that could open on a track was
  unreachable.
- `H-13` asks for a Play button. The button needs file paths; the
  obvious key is `MBTrack.LocalID`, which is declared, is in the
  generated bindings, and **is never written by anything in the
  backend**. Ownership is decided by recording MBID. Keying on that
  produced a button that was wired, labelled correctly, clicked
  cleanly and **queued nothing** — a library-only album has no
  recording MBIDs, because its tracks are synthesised with
  `mbid: RecordingMBID || ''`. Every component test passed. It was
  caught by clicking the button in the running app and reading the
  queue.

Seven more things worth keeping:

- **A count of call sites in an audit is a count as of its date.**
  `a11y.md` lists five `wa-dialog`s; last pass estimated eight; there
  are eleven. Three of the six added since were added by *this plan*.
  The fix was written as a helper called from each host rather than a
  list, so the number stopped mattering — which is the right response
  to a number that drifts.
- **Reaching into another library's shadow root is acceptable when the
  failure is bounded.** `name-dialog.ts` queries Web Awesome's open
  shadow root, which is not API. If the structure moves, the query
  misses, nothing is written, and the dialog is exactly as unnamed as
  it was — no state to get wrong, nothing to throw. The alternative,
  patching `WaDialog.prototype`, fixes every call site for free and
  fails loudly and strangely instead. The bound is the argument, not
  the tidiness.
- **`aria-labelledby` beats `aria-label` when the label can change.**
  Three call sites compute their label at render time. An IDREF to the
  heading the component re-renders anyway stays correct with nothing
  resyncing it, so the helper can be a one-shot call.
- **A web component's shadow root is populated in its own update, not
  its host's.** Naming a `wa-dialog` from the host's `firstUpdated`
  finds an element with an empty shadow root and names nothing. Same
  lifecycle trap as `wa-dropdown-item`'s role two passes ago, which
  suggests it is not a trap so much as a rule: **never query inside a
  child custom element without awaiting its `updateComplete`.**
- **The a11y snapshot cannot see a dialog's accessible name.**
  `playwright-cli snapshot` prints a bare `- dialog [ref=…]` whether
  the dialog is named by `aria-labelledby`, by `aria-label`, or not at
  all — checked all three ways against the running app. Twenty minutes
  went into "the fix did not work" before the probe was suspected.
  `getByRole('dialog', {name})` and CDP's
  `Accessibility.getFullAXTree` both answer, and CDP additionally
  reports *where* the name came from. The e2e spec was watched failing
  on a probe-disabled build before it was believed, which is the only
  reason the twenty minutes did not become an hour.
- **An assertion that cannot fail will pass a bug, and the fix is to
  make the probe move.** "The scroll position is preserved when the
  dropdown opens" passed against a `scrollTop` of 0 both times on an
  eight-album fixture. Shrinking the viewport until the grid genuinely
  scrolled turned it red — and the red was **correct**:
  `scrollToShowDropdown` deliberately moves the scroll to reveal the
  dropdown (80 → 4, with the content *taller* after, so not clamping).
  The premise was wrong, not the app. Two lessons in one: a vacuous
  assertion hides a bug *and* a false claim, and the way to tell them
  apart is to make the number move before deciding what it means.
- **A parameter name is not a specification.** `Queue.SetQueue`'s
  `shuffleStart` does not start a shuffle: it picks a random first
  track *if shuffle mode is already on*. A Shuffle button written from
  the name plays track 1 and looks broken. Reading the Go was thirty
  seconds.

And one on the audit's own accounting: **`H-13`'s "unexplained ✓
badges" was half-aged before it was read.** The indicator has carried a
`title` and an `aria-label` reading "Album “X” is in your library" all
along, so a hover and a screen reader were both already answered; what
was missing was a key for a sighted user scanning a column of green
circles. The same element turns out to be a `<button>` whose click
handler is a comment saying "wire this up later" and a
`stopPropagation` — 30-odd keyboard stops per page that promise an
action and perform none. Not fixed here, and recorded rather than
implied.

## A shelf that shares no ids can still be the shelf above it

Plan 007 phase 6: the two inherited one-liners from the fourth pass,
and then the only part of this plan that *adds* rather than repairs —
Explore's shelves.

The generalisation: **a rule is written against a mechanism, and the
thing it exists to prevent is not the mechanism.** `backend/home`
suppresses a shelf that repeats the one above it, by comparing album
ids. Explore's first two shelves hold *different entity types*, so
their ids are disjoint by construction and no overlap is possible — I
wrote that in a comment as the reason the guard was unnecessary, and it
is true, and the page repeated itself anyway. Ordered by raw
ListenBrainz listen count, the catalog's top twelve albums are seven
records by one act and its members, and the artists row underneath is
then the same seven people. One fandom, twice, with nothing in common
by the only measure the rule knew how to take.

It was found by **reading the screenshot** — the shelves rendered, the
counts were right, four component tests and six Go tests were green,
and the page was obviously wrong to anyone looking at it. The fix is
one album per artist (a shelf is a selection, not a leaderboard) and
skipping whoever a row above already showed. The existing Go test
caught the semantic change immediately: an artist seeded as the maker
of the album shelf's only album correctly stopped appearing in the
artists shelf, which read as a regression and was the rule working.

Nine more things worth keeping, and the first four are all one theme —
**a plan's shelf list is a design; the schema is the constraint**:

- **Two of the four planned shelves cannot be built at all.**
  `explore_index` has no genre or tag column, so "big in a genre you
  already have depth in" has nothing to join to — genre exists only in
  the library's own `recording_genres`. And "artists next to ones you
  own" needs `similar_artist_map`, which `cmd/indexexport` does not
  ship and which is filled lazily by network calls from artist pages:
  empty on a fresh install, empty offline, which is exactly when this
  page most needs content. Both were answerable in ten minutes by
  reading two schema files, before writing anything.
- **The third is empty on every library the repo can look at.** It
  reads `in_library`, set from MusicBrainz IDs, and the fixture library
  has **0 artists with an MBID** — so a shelf about the user's own
  music is correctly absent on the seed, in CI, and on any untagged
  library however large. A feature you cannot see locally has to be
  designed so that the state you *can* see is a legitimate one.
- **"The shipped artifact already contains the answer" is false in the
  place the tests run.** `ci.yml` points `YJ_CORE_INDEX_URL` at a dead
  address, so CI's app has **0 catalog rows** — as does every user's
  first run. A developer machine silently downloads the real 1.1 M-row
  artifact at launch, which is why the local page looked finished. The
  empty world was reproduced locally by setting the same variable, and
  it is now the world the e2e spec stages a catalog into.
- **A spec that skips is not a spec that passes.** The first version
  branched on the row count and skipped three of its four cases where
  there was no catalog — which is CI, i.e. the only place both browser
  engines run. Staging six rows through `/__test/sql` costs nothing
  and turns "skipped" into signal.
- **…and it only works because the readiness gate is a question rather
  than a flag.** Two cached answers were tried first and were wrong in
  the same way. `GetIndexStatus().TotalRows` is refreshed between build
  tiers, so on an ordinary launch it reads 0 next to a full catalog and
  hid every shelf. `IsReady()` is set once at startup by counting, so
  rows staged afterwards are invisible to it. Both are the shape the
  `emitStatus` note warns about — a derived value with nothing polling
  behind it. `SELECT 1 FROM explore_index LIMIT 1` cannot be stale and
  costs nothing.
- **A setup step whose failure is not checked is not setup.** The
  staging fetch passed six values to seven placeholders and never read
  the response: every insert failed, the table stayed empty, and the
  helper looked exactly like a helper that had worked. Same family as
  every "probe that cannot move" in this plan, on the *arrange* side
  rather than the assert side.
- **A reproduction of a fix at one scale is not one at another.** End
  in a split grid worked on the eight-album fixture, because everything
  is rendered and the scroll is a no-op. At 5 000 albums the fix moved
  the index and focused nothing: the card arrives a few hundred ms
  after the host's `updateComplete`, and a ten-*frame* retry budget
  expired first. The retry is a time budget now. (And a probe that
  scrolled the grid to demonstrate the *old* behaviour left it
  somewhere that broke the next measurement in the same eval —
  measuring the before can spoil the after.)
- **The audit did not contain the biggest bug this pass fixed.** "Check
  what Home/End mean across a split grid" was a one-line hunch from the
  previous session. What it found is that `offsetTop` inside a
  `lit-virtualizer` is always 0 — the children are positioned by
  transform — so ArrowDown and ArrowUp have been End and Home in the
  albums, artists *and* genres grids since the roving controller was
  written. Reproduced at 700×700 with three real rows: ArrowDown from
  card 0 landed on card 7. One `getBoundingClientRect` fixed all three.
- **A label can promise what the control cannot do.**
  `library-status-indicator` was recorded last pass as a button that
  does nothing. Its *label* was also an offer — "Add artist “Eno” to
  library" — from an element that cannot accept it. Making it a badge
  meant changing the copy too, which is the part a mechanical fix would
  have left saying the wrong thing. Also worth knowing: a `<span>` does
  not inherit `box-sizing: border-box` from the UA stylesheet the way a
  `<button>` does, so swapping the tag grew the badge 36→38px. Nothing
  but the stored screenshot would have noticed.

## The state a fix lands in is a state nobody has looked at

Plan 008 phase 1: the three `a11y.md` findings that lose function — a
marquee that cannot be stopped, a combobox that announces nothing, and a
queue whose order needs a mouse.

The generalisation, and it is the mirror of "a finding creates the
conditions for the next one": that note is about the code path a fix
*opens*. This one is about the code path a fix **sends people to**. A
guard, a fallback, an empty state, a disabled variant — the branch a fix
makes people live in has usually never been looked at by anyone,
precisely because until now nobody arrived there.

The reduced-motion guard is two lines. Both bugs it exposed were in the
place it sends you:

- **The non-scrolling fallback hard-clipped**, and always had.
  `text-overflow: ellipsis` was on the outer span while the box that
  overflows is the `inline-block` child — so it produced an ellipsis in
  **no mode**, including `hover`, which is the default every user has.
  A title read "An Exhaustively Overlong Trac|". Found by reading the
  screenshot of the fix, which is the fourth regression in two plans
  that only a PNG has caught.
- **And fixing *that* broke overflow detection.** Giving the child its
  own `overflow: hidden` stops the parent overflowing, so
  `titleOverflows` went false and nothing would have scrolled again for
  anybody. Caught by the new test's *positive* case — which existed
  only because a guard that suppresses everything passes the negative
  case for free, which is this repo's oldest rule wearing its eighth
  costume.

Eight more things worth keeping:

- **A grep triage is a good answer to "is it still there" and no answer
  to "why".** Checking all 34 findings against the tree took ten
  minutes and closed at least five the coverage map still showed open,
  including three (`17`, `19`, `27`) fixed by phases that were not about
  them. It said nothing about mechanism, and mechanism is what decided
  that `15`'s obvious CSS-only fix is wrong.
- **A finding's stated scope can be half-closed by an unrelated phase.**
  `a11y.11` is "drag-and-drop has no keyboard equivalent anywhere" and
  its stated symptom is "there is no keyboard path to add a track to
  the queue or a playlist" — which Phase 5's `MenuKeyboard` closed. What
  was actually left is the queue's *order*, the one thing a menu cannot
  express. Fixing the sentence rather than the residue would have built
  three menu commands that already exist.
- **A count in an audit is scoped by how it was taken.** `a11y.6` says
  two buttons are "the only truly unnamed controls" — and says, in the
  same line, that it scanned every `<button>`. The AX tree has two
  unnamed `combobox` roles that are native `<select>`s, one of them the
  page header's sort control on nine views. The claim was never wrong;
  it was answering a narrower question than it reads as.
- **The probe was wrong, not the fix — twice more, both on the *after*
  side.** Reading `activedescendant` out of `getFullAXTree` as
  `relatedNodes[0].text` reported `(none)` against a working build,
  because the property carries `value.type: "idref"`. And
  `__yjEvents.last('QueueChanged')` returned a stale payload, so a
  reorder that had happened read as one that had not. Dump the whole
  property; ask `GetState`.
- **An operation's index convention is part of its contract, and the
  symmetric-looking version fails silently.** `MoveQueueTracks` takes an
  index into the array *before* the move, so up-by-one asks for `i - 1`
  and down-by-one has to ask for `i + 2` — `i + 1` is where the row
  already is once its own removal is accounted for, and the backend's
  contiguous-block guard correctly returns without doing anything. The
  first version moved rows up and did nothing at all downward, with no
  error anywhere.
- **A roving tab stop that only moves on arrow keys is not where the
  focus is.** `focusedIndex` was never synced from a click or a Tab, so
  `Enter` played the first track in the queue from *any* focused row —
  pre-existing, invisible for as long as the keys only read state, and
  obvious the moment a key moved something.
- **Watch the new spec fail on the old build.** Done for all three
  landings, by neutering one line rather than by stashing (which
  reverts every uncommitted change in the file). Two of the three would
  have passed against the broken build in at least one case if the
  positive direction had been left out.
- **CI's concurrency cancels the previous run's `e2e` when you push
  again**, and `cancelled` sits one line from `success` in the run
  list. The first landing's e2e never ran; the signal came from the
  second push's run, read step by step through
  `/api/v1/repos/{owner}/{repo}/actions/runs/{run}/jobs`. Same family as
  the `skipped` WebKit step, one layer out.

And the one that is no longer worth calling a lesson: **a backtick
inside a comment in a `css` tagged template literal ends the literal.**
Third session running. It is written in `CLAUDE.md`, in the skill, and
in `NOTES.md`, and it was read twice in the session it then cost a
cycle in. Knowledge is not working here; it wants a lint rule.

## A parked measurement is a finding of unknown size, and this one was nine times bigger

Plan 008 phase 2: the two items `a11y.md` never measured. One closed on
measurement; the other turned out to be nine times the size of its own
description and to contain two findings larger than itself.

The generalisation: **"worth measuring before planning" is a debt with
no stated size, and the estimate attached to it is not a bound.** The
audit said `--yj-text-tertiary` on `--yj-bg-surface` is "≈ 4.1:1,
borderline", from a hand calculation over two hex values in a file that
does not contain them. Every part of that sentence was approximately
true and the conclusion it invited — *borderline, low priority* — was
wrong by an order of magnitude:

| | audit | measured |
|---|---|---|
| pairs considered | 1 | 12 (three ramps × four surfaces) |
| failing | "borderline" | 9 of 12 |
| worst ratio | ≈ 4.1 | **2.31** (dark overlay), **2.55** (light) |
| failing nodes on screen | — | **110** across twelve views |

Nine things worth keeping:

- **A number quoted from the wrong file is still a number, and it
  travels.** The audit cites the palette as `tokens.css.ts`. That file
  holds the type scale and icon sizes and no colours at all; the ramps
  live in `theme-store`, applied to `:root` at runtime — which also
  means the `var(--yj-…, #fallback)` at ~500 call sites is dead code,
  and four different fallbacks behind one name never mattered. I spent
  twenty minutes concluding the tokens "are never defined" before
  asking the *running app* what `:root` carried. Ask the app.
- **Measuring one state of three answers one third of the question.**
  The whole first sweep was the `dark` ramp, because that is the
  default. `light` was the worst of the three and had never been looked
  at by the audit or by me. A palette is data — enumerate it.
- **A generated colour is a family, not a colour.** The avatar
  background is `hsl(nameToHue(name), 45%, 35%)`, and 35 of the 360
  hues put white text below 4.5:1. The rendered sweep found *two*,
  because two artists happened to hash into the yellow-green band. Had
  I fixed the two, the bug would have returned with the next search.
  The unit of the fix is the generator; the unit of the test is all 360.
- **A fix that makes the ramp pass can also destroy the ramp.** Sizing
  tertiary to clear 4.5:1 on `bgOverlay` needs a grey *lighter than
  secondary*. Passing an automated check by inverting the visual
  hierarchy is the kind of accessibility fix that makes the product
  worse, so `bgOverlay` is documented as not a text surface and the one
  component using it that way now uses primary. The test encodes the
  exception rather than pretending it away, and a second case asserts
  the ramp stays ordered.
- **My probe was wrong before the code was, twice, and a screenshot
  caught both.** Source-over compositing that forces `a: 1` makes two
  stacked `rgba(255,255,255,0.05)` surfaces composite to opaque white —
  which reported a perfectly readable button as white-on-white at
  1.00:1. And later I read a screenshot taken *after* a sweep had left
  the app on a different ramp, and concluded the light theme was not
  applying at all. Both times the tell was the same: **the picture and
  the number disagreed**, and both times the number was mine.
- **The cheapest tier is blind to a whole class of change.** `make
  ui-visual` passed unchanged across a palette rewrite, because the
  component tier has no `:root` and renders the fallbacks. Six stored
  screenshots said nothing at all about the change they most looked
  like they were about.
- **A finding that closes on measurement is worth the measurement.**
  `a11y.28` (mouse-only resize handles) was dropped by reading. At
  800×600 the track list clips exactly one thing — the *Duration header
  label* — and zero data cells, and that sort has a keyboard-reachable
  dropdown anyway. Same conclusion, now with a number, and the next
  reader does not have to re-derive it.
- **The measurement found two things larger than what it was measuring.**
  The semantic colours are fixed across ramps, and one fixed colour
  cannot serve both a near-black and a near-white surface — `--yj-error`
  is 2.55:1 on dark's elevated. And with the greyscale fixed the light
  ramp still fails 50 nodes: an invisible warning banner, a
  white-on-yellow primary button, chrome that stays dark while the body
  goes light. Recorded, not fixed. "Does the light theme ship?" is not
  a question a contrast pass gets to answer on its own.
- **Fixing the ubiquitous case makes the rare ones visible.** With
  tertiary raised, the remaining dark-ramp failures were three nodes
  and every one was a *different* mechanism. A finding at 110 nodes
  hides them; at 3 they are individually obvious. Cheap tail, only
  reachable from the other side of the main fix.

## A role, not a value, decides whether a colour can be fixed

Plan 008 phase 2, third landing: the two findings the contrast pass had
recorded as too big to fix in it, fixed — plus the check for the trap
that has now cost four sessions.

The generalisation: **a token that is used for two different jobs will
be wrong at one of them, and no amount of choosing a better value
fixes it.** `--yj-error` was "the colour of error", which is two
questions. As the *background of a danger button* it wants to stay red
in every theme, and it does. As *the word `failed` on a surface* it
cannot be one value at all — a single colour cannot clear 4.5:1 against
both a near-black and a near-white background, which is why the fixed
set measured 2.31–4.28:1 on nearly every combination. Splitting the
token by the question it answers made both answerable; picking better
hexes never would have.

The same shape twice more in the same landing:

- **A fill's foreground cannot be written down**, because the accent is
  a colour picker. `color: #000` is right for the current default
  yellow and wrong for a navy one. `readableOn()` computes it — white
  where white clears 4.5:1, black otherwise — which keeps a red danger
  button conventional (4.51:1) and flips a green one (3.45:1).
- **`var(--yj-bg-base)` as a foreground is a token used for the wrong
  meaning.** Two accent buttons did that. It reads as "the opposite of
  the accent" and it is not: it inverts with the ramp, so the light
  theme rendered white on yellow at 1.43:1. The bug is not the value,
  it is that the *name* did not mean what the call site needed.

Six more things worth keeping:

- **The picture and the number disagreed, and the number was mine —
  again.** I recorded "the header and player chrome stay dark while the
  body goes light" as a finding, in the plan and in these notes. It is
  false: `.top-bar` is `#e9ecef` and `.sidebar` `#f8f9fa` under the
  light ramp, and a re-taken screenshot agrees with the DOM. The
  original was captured before the theme had propagated. That is the
  third time in two passes a screenshot read at the wrong *moment*
  produced a confident wrong claim, after a spec reading the DOM before
  a fetch and a sweep that had moved the app to another ramp. **A
  screenshot has a timestamp and a state; check both before quoting
  it.**
- **A `color:` regex matches `border-color:`.** Twice in one landing —
  3 borders while rewriting semantic text, then 30 more while rewriting
  accent text. Both caught by grepping the *result*, not by any test,
  because a border in the wrong shade fails nothing and looks fine.
  When a mechanical rewrite is the right tool, the review is a grep of
  what it did, not a run of the suite.
- **A fix at the ubiquitous case makes the rare ones findable.** The
  greyscale fix took the dark ramp to 0 and the light ramp from 50 to a
  list short enough to read individually — at which point every
  remaining item was a *different* mechanism, and two of them were the
  findings above. A queue of 110 hides its own structure.
- **Knowledge that has been ignored three times is not a knowledge
  problem.** The backtick-in-a-`css`-comment trap is documented in
  `CLAUDE.md`, in the skill and here; I read it twice in the session it
  then cost a cycle in. It is `make css-check` now — a pre-commit hook
  and a CI step. The detection is exact rather than heuristic: if a
  backtick in a comment closed the literal early, the text the parser
  took as the literal contains an unterminated `/*`, and nothing else
  produces that.
- **The value of that check is the sentence, not the failure.** `tsc`
  already failed on it — with `Class static side 'typeof NowPlaying'
  incorrectly extends base class static side` and `Property 'scroll'
  does not exist on type 'CSSResult'`, pointing at a line of prose. The
  check was verified by breaking a file on purpose and reading both
  reports side by side, which is also the only way to know it fires.
- **A change can be invisible to the tier that looks the most like it
  covers it.** `make ui-visual` passed unchanged through a whole
  palette rewrite, twice, because the component tier has no `:root` and
  renders the fallbacks. The tier that *did* catch things was a unit
  test over the palette table and a probe against the running app.

## A name lives where the role is, and neither the audit nor the sweep looks there

Plan 008 phase 3: the tail of `a11y.md`, which closes it — and with it
all four audits from 2026-08-11.

The generalisation, and it is the whole of this pass: **an accessible
name is computed on the element carrying the role, and every way we
have of checking one looks somewhere else.** The audit read the
*source* and credited a name that was never computed. My AX sweep read
the *tree* and reported a weak name as no problem. A component test
asserted the *attribute* and pinned the bug it was written to prevent.
Three tiers, three different wrong answers, all about the same
property.

Concretely, and each of these is a finding:

| where it was written | where the role is | computed name |
|---|---|---|
| `aria-label` on `<wa-slider>` | a div in its shadow root, `aria-labelledby="label"` | `""` |
| `<label>` beside a `<select>` in `config-field` | the select | `""` |
| `placeholder` on Explore's search input | the input | the placeholder |
| `label` on `<wa-progress-bar>` | inner div's `aria-label` | correct |

The first is `wa-dialog`'s trap one component over and cost two
sessions in 007. The fix is different, though, and the difference is
worth keeping: `wa-progress-bar`'s `label` *is* an `aria-label` and is
invisible, so it is just the right API; `wa-slider`'s `label` is
**visible**, so the name comes from the library's own property and
`styles/wa-slider-label.css.ts` hides it by part. That is preferred
over `name-dialog.ts`'s reach into the shadow root for one reason —
if Web Awesome renames the part, the label becomes *visible* and
correctly named, rather than silently nameless again. Choose the
failure you would rather have.

Nine more things worth keeping:

- **A sweep for empty names cannot see a weak one.** A `placeholder`
  is an accname fallback, so `getFullAXTree` reported the whole Explore
  view *clean* — which is why `a11y.26` survived four phases of people
  looking for exactly this class of bug. "0 unnamed" answers a
  narrower question than it reads as, which is the third time in this
  plan a *count* has done that.
- **The count that sent Phase 1 hunting was wrong in both halves.**
  "Two unnamed native `<select>`s, one of them the page header's sort
  control on nine views": the sort control is named `Sort: ` by its
  wrapping `<label>` (`from: relatedElement`) on all nine, and the two
  unnamed roles were one `config-field` select and the *seek bar*.
  Recorded as a finding in the plan, believed for a phase, false.
- **…and the thing it was pointing at was nine times bigger.** With
  Settings' sections expanded: **24 of 93 controls unnamed**, every
  `config-field` select and toggle and all eighteen column checkboxes.
  No finding names it, and `a11y.6` is not wrong — it says in its own
  line that it scanned every `<button>`. Same shape as phase 2's
  contrast number.
- **A fix's own test can be pinning the bug.** `transport.test.ts`
  asserted `aria-label` on the `wa-slider` host under the title
  "carries an accessible name". It passed for six phases. *Run the
  existing tests* found it — third plan running that this is the rule
  that pays.
- **`a11y.21`'s mechanism does not exist, and the real one is on the
  other axis.** "The 4em bars grow while the viewport does not and
  anything that no longer fits is clipped" — the middle grid row is
  `1fr` and absorbs them exactly: at 200% text on 800×600 the bars go
  64 → 128px, the panel 472 → 344px, and the footer still lands on 600.
  Nothing is clipped vertically. Horizontally the shell is 784px inside
  a 320px viewport (400% page zoom, the width 1.4.10 names) with 464px
  of it behind `overflow: hidden`. Measure the axis the finding does
  not mention.
- **`overflow: hidden` still permits programmatic scrolling**, so
  `scrollLeft = 9999` returns a healthy 464 on the build that has the
  bug. My first spec passed against the broken build for that reason.
  A wheel gesture is the probe. Fifth entry in this plan's "the probe
  was wrong, not the code" column, and the tell was the oldest one
  there is: **it could not fail.**
- **A synthetic `MouseEvent` does not reach a delegated handler.**
  Three probes in a row reported a queue row as never becoming active;
  `getByTestId('queue-row').dblclick()` made it active immediately.
  Delegation reads things a hand-built event does not carry.
- **A finding can be half-closed by a phase that was not about it,
  and the half that remains is smaller than the sentence.** `a11y.34`
  reads "the sort direction is a 10px glyph *or nothing*" — Phase 1's
  `aria-sort` closed the *or nothing*, leaving one declaration. Second
  time in this plan (`a11y.11` was the first), and both times reading
  the sentence rather than the residue would have built something that
  already existed.
- **The state a fix lands in, again, and it was three pixels.**
  `a11y.29` takes the subtitle's bottom margin away with the `<h3>`,
  which *shortens* the flex-centred title block and moves it **down**
  into the bar's clip — the hgroup had measured 67px inside a 64px bar
  since before any of this, and the descenders of "meant to bee." were
  cut. Found by reading a screenshot of the fix, which is the fifth
  regression in three plans that only a PNG has caught.

And one thing that went right and is worth copying: **the marker for
`a11y.22` is a shape drawn in padding the row already had.** The track
list's grid columns are computed from the host width, so anything in
the flow moves every cell on the playing row and nothing else. A
`::before` triangle in the 8px left padding costs no layout, and both
tiers assert it is *absent* on the other rows — a marker that renders
everywhere satisfies "the playing row has one" for free.

## A guard is only a feature if everything that counts agrees with it

Plan 008 phase 4: "remove from library" — the row goes, the path is
excluded from future scans, the file is never touched — and with it
`tracklist.delete`, which had been advertised in Settings for six
phases with nothing on the other end of it.

The generalisation: **an operation that changes what counts as "in the
library" has to be applied everywhere that number is computed, and the
places that compute it do not look like the feature.** The scan walk is
the obvious one, and skipping an excluded path there is the whole
feature as written in the plan. But the *startup soft scan* decides
whether to scan at all by comparing files on disk against rows in the
database, and an excluded path is on disk and deliberately not a row —
so the fix as specified would have left the two counts disagreeing
forever and queued a full scan of the entire library on **every
launch**. Nothing fails, nothing renders differently, and no tier looks
at it; the app is just permanently rescanning. The same shape one step
over: deleting an `audio_files` row cascades to `queue_tracks`, so the
queue's in-memory copy — and the playing track — goes stale unless the
removal calls the reload hook `RemoveLibrary` has had all along.

Six more things worth keeping:

- **A new table needs one schema file, and the two-file discipline is
  not about it.** `applySchema` runs every file in `sql/schemas/` on
  every open, so `CREATE TABLE IF NOT EXISTS` reaches an existing
  install verbatim. The migration the plan asked for would have been a
  *second description of the same table*, which is precisely what tore
  out the old 48-step chain. Column order and "no index on a migrated
  column" are rules about `ALTER TABLE ADD COLUMN`, and neither applies
  when nothing is being altered.
- **The repo asked the question the plan did not.** `backend/datamap`
  failed the build twice for the new table: once for having no entry at
  all, then again because an *authored* table that cascades needs an
  argued exemption rather than a default. Two gates, both right, and
  neither in `references/schema-change.md` until now. A catalogue that
  fails the build is worth more than a catalogue that is accurate.
- **Reversibility is a claim until something implements it.** The
  decision picked shape A over deleting the file partly because it is
  reversible — and nothing in the plan made it so. An exclusion with no
  UI to clear it is a one-way door with the file sitting on disk the
  whole time. A full rescan clears the table, which is the escape hatch
  until there is a list to manage, and it is now written down instead
  of assumed.
- **The copy was wrong for the case it will be used in most.** The
  confirmation's message and impact were written for a multi-select and
  used for both, so removing one track said "**They** are removed" under
  a singular title. Nothing failed. Read in the first screenshot of the
  dialog — sixth regression in four plans that only a PNG has caught,
  and the one where it mattered most, since the copy is the only thing
  standing between this feature and a user's music.
- **Both halves of a guard need their own test, or one of them is
  decorative.** The walk's exclusion and the survey's exclusion are two
  lines in two functions; neutering each in turn failed exactly one
  test. Had they shared a test, either could have rotted invisibly.
  Same reason the e2e case asserts a *control* path still returns from
  the same scan: a guard that excluded everything passes "the removed
  path did not come back" for free.
- **A Playwright hook gets 30 seconds regardless of the test's
  timeout.** A `db/restore` in `afterAll` passed in isolation and timed
  out in the full suite, where earlier specs have staged an explore
  catalog and the copy takes longer. `test.setTimeout()` *inside the
  hook* is what raises it — and a spec that spends the shared database
  has to give it back, since the 90 specs share one backend in file
  order.

## A state nothing produces is a state nobody has checked

Plan 009 phase 1: `library-status-indicator`'s third state, wired.

The generalisation: **an enum whose last value is never constructed is
not unfinished, it is wrong** — because everything around it has been
written, reviewed and tested against the two values that do occur, and
the code reads as complete from every angle except the one that
produces the third. `LibraryStatus` has had `queued` since it was
written: styled amber, given an hourglass, given the sentence "… is
queued for download". All eight call sites were a two-way ternary. So
an album on the request list rendered a plus and announced "is not in
your library" — on the same page, forty pixels from a filled button
reading "Wanted".

Nothing was going to find that. `make ui-test` and `make e2e` both
covered the badge; both asserted the states it produced. 007 phase 6
had rewritten this exact component, and the note it left behind
("when the download-client integration lands…") was itself the reason
nobody looked: it names a *future* condition for work that was already
possible, since `backend/download` was 16 541 lines and 20 bound
methods on the day it was written. **A written-down reason not to look
ages worse than the code it is about.**

Six more things worth keeping:

- **The second bug was in the screenshot of the first.** The "Wanted"
  button rendered a question mark — the missing-icon fallback —
  because `bookmark-check` is Font Awesome **Pro** and has never been
  bundled. `offline-icons.spec.ts` asserts `__yjIconMisses` is empty
  and passed the whole time: no spec had ever put the app in a state
  where an album was requested. The bundled-icon design anticipated
  exactly this ("twenty call sites compute their icon name from
  state") and the *sweep* still could not see it, because a sweep only
  sees the states it visits. Seventh regression in five plans that
  only a PNG has caught, and the first found in a PNG taken of a
  different bug.
- **A property that does not change does not re-render a child.**
  `top-results-row` reads the request list, and its host handing back
  the same `results` array means Lit stops at the property — the row
  keeps its old badges while the store holds the right answer. The
  virtualizer rule (`requestUpdate()` on host state) one level milder,
  and the same fix: subscribe where the state is *read*.
- **A spec that gives state back has to be run twice to know it did.**
  The `afterAll` cleanup called `callBinding`, which goes through
  `window.__yjEvents` — installed by the `app` fixture, not by a bare
  `browser.newPage()`. It threw where nothing was watching, left the
  request behind and failed the *next* run with a stale `queued`. One
  run proves the assertions; the second proves the teardown.
- **A freshly launched app cannot search its own catalog for ~40 s.**
  The core artifact merge has to land (`core artifact: merge complete`
  in `.dev/app.log`), and until it does Explore's search returns
  nothing at all — *including for rows staged directly into
  `explore_index` a moment earlier*, which makes it look like the
  staging failed. Cost a cycle here reading as a failure of the neuter
  the run was under. Budget 60 s, or wait for the log line.
- **The neuter has to be per line, not per feature.** Two fixes landed
  together and each got its own one-line neuter, which is what made
  the two failures distinguishable: one spec reported the wrong badge
  status, the other reported `["bookmark-check"]`. Neutered together
  they would both have failed and either could have been decorative.
- **The fix is where the rule is, and the rule was in eight places.**
  Every one of the eight sites was individually reasonable; the third
  state was missing from all of them because no site owns the
  question. Same shape as `getCoverUrl()`, `track-index.ts` and
  `page-header` — when a rule is written per call site, the call sites
  do not disagree, they are all incomplete in the same way.

## A decision phase earns its keep by finding it was not a decision

Plan 009 phases 2 and 3: the badge becomes a button where it can act.

The generalisation: **the questions worth taking a phase over are the
ones the code can answer, and you cannot tell which those are without
asking them.** Phase 2 was written as three judgement calls. Two turned
out not to be:

- "An artist badge would commit a user to a whole discography" —
  describing a badge that **does not exist**. `top-results-row` renders
  `nothing` for an artist and no other site passes
  `entity-type="artist"` to the component at all. Artist subscription
  already had a labelled Follow button.
- "Should a track inside a requested album show something different" —
  evaporated. It read as noise only while a plus on a track meant
  nothing; once it means *want just this one*, the mixed row is the
  interface working.

The third — whether a track can be requested at all — went the other
way and is the more useful lesson. **`EntityRecording` reads like a
placeholder and is load-bearing.** It would have cost nothing to rule
tracks out as unsupported, and `Reconciler.tracklistFor` has an
explicit branch for them whose comment explains that a one-entry
expected tracklist is what lets filename matching score a single-track
download at all. A feature removed by assumption leaves no trace that
it was ever there.

Five more things worth keeping:

- **A test that passes on the neutered build is not a test, and the
  vacuous ones are the negative assertions.** "Keeps its click off the
  card it sits on" asserted that nothing bubbled — free when there is
  no button, since `?.click()` on null is a silent no-op. It passed on
  the neutered build while its seven neighbours failed. It asserts the
  click *did the thing it was swallowed for* as well now. Same family
  as `overflow: hidden` permitting programmatic scrolling, and the tell
  was identical: **it could not fail.**
- **A measured coordinate is stale before it is used.** The e2e gesture
  read a bounding box the moment the search settled; cover art is still
  arriving then and a card that grows moves the badge, so the click
  landed on the card and opened the album — reported as *a failure to
  file a request*, which is a different bug. A Playwright locator
  re-resolves and waits for the element to stop moving. Prefer one to
  `mouse.click(x, y)` whenever the thing being clicked is in a list
  that is still loading, which is most lists here.
- **A fix moves its own assertions, and that is not churn.** Phase 1's
  spec asserted the badge announced "… is queued for download". A
  *control* is named after what activating it does, so two commits
  later it is "Cancel the request for …". Naming a thing after its
  state is correct right up until it grows an action.
- **An opt-in makes a redundancy visible.** The badge could have known
  which pages have a "Want this" button; instead a call site passes
  `request-mbid` or does not, so `explore-album-details`'s header
  declines in its own template. The rule is greppable and the component
  has no list of exceptions to go stale.
- **Verify a control with the gesture, not with the event.** A synthetic
  `MouseEvent` does not prove hit-testing, and a `.click()` on a shadow
  child does not prove the icon inside it is `pointer-events: none`.
  Both were checked with a real mouse (`mousemove`/`mousedown`/
  `mouseup`) and a real Tab/Enter before either was believed.

## A queue of work has to ask whether there is work

Reported as "the autotag page has every album in it, even the MB-tagged
ones". Both halves of that were true and they were two different bugs,
which is why the first answer found (the pending list) accounted for
nine rows out of 2172.

**Every scanned folder gets a `tagging_items` row.**
`UpsertTaggingItemOnTrackAdd` runs per track with `status = 'pending'`
and no condition, so the table is a row per album folder, not a queue.
`saveAudioFile` *does* record the answer — `audio_files.tag_status` is
`user_confirmed` on import for any file carrying a recording MBID, and
`idx_audio_files_tag_status_untagged` was declared for the filter — but
no query in the app read the column. Pending therefore meant "has a
row". The four queue queries ask the files now
(`EXISTS … tag_status = 'untagged'`), which matters most where it is
least visible: `startPrefetch` was scoring every album in a tagged
library against MusicBrainz.

**The 2094 were the Completed section, and nobody completed them.** A
backfill stamped `status = 'confirmed'` on every folder whose files
already had MBIDs, and the sidebar keeps confirmed rows deliberately —
so the review page's history became a list of the library. They are
distinguishable without new state: every status flip the app performs
goes through `SetTaggingItemStatus` or `SetTaggingItemBestMatch` and
both stamp `last_checked_at`, so *confirmed with no `last_checked_at`,
no score and no best match* is the backfill's row and not the user's.
All 2094 were that shape; the two the app had actually applied were
not. Migration 0007 sets `cleared_at` on them — the column exists for
exactly this, and it keeps the row so a rescan cannot reset review
state.

Two smaller things fell out. The reviewed states are **exempt** from
the untagged-files predicate, or an applied folder would vanish from
Completed the instant it succeeded — the section is history, not work.
And `tag_status` was only ever written by the *insert* path, so a file
another tagger stamped after import kept `untagged` for ever and its
folder kept asking; `updateAudioFile` promotes it now, guarded on
`untagged` so a deliberate `user_skipped_permanent` survives a rescan.

## Android cross-compiles, unchanged (measured 2026-08-16)

Plan 015's phase 0 gate, and it passed further than it was asked to: the
whole app builds for Android and produces a working 27 MB fat APK with
**no source changes at all**.

Environment: Arch's `android-ndk-26` (`/opt/android-ndk`, r26d /
26.3.11579264 — the pinned version), platform `android-35` and
build-tools 34.0.0 from `~/Android/Sdk`. Note that Arch's
`/opt/android-sdk` carries *no* platforms, so `ANDROID_HOME` has to
point at `~/Android/Sdk` for the Gradle half while `ANDROID_NDK_HOME`
points at `/opt/android-ndk` for the Go half.

```
export ANDROID_NDK_HOME=/opt/android-ndk
export ANDROID_HOME="$HOME/Android/Sdk" ANDROID_SDK_ROOT="$HOME/Android/Sdk"
cd frontend && pnpm build && cd ..          # main.go embeds frontend/dist
PATH="$PWD/scripts/toolbin:$PATH" go tool wails3 task android:package:fat
```

Results, all first-try:

| | |
|---|---|
| `libwails.so` arm64-v8a | 29.9 MB, production, stripped |
| `libwails.so` x86_64 | 31.8 MB, production, stripped |
| `bin/yellowjacket.apk` | 27.3 MB, both ABIs |
| Go compile, per ABI | ~9 s |
| Gradle assemble | ~13 s cold |

**The dependency that looked fatal is fine.** A `CGO_ENABLED=0` probe of
`./backend/... ./internal/...` for `android/arm64` compiles *everything*
except two packages, and both fail only because their Android
implementation is cgo: `ebitengine/oto/v3` (`driver_android.go` needs its
bundled **oboe** C++ backend) and `wails/v3/pkg/application` (the JNI
bridge). Both are exactly what the NDK supplies. `modernc.org/sqlite` —
the whole database layer, and the thing most likely to have no Android
target — is clean. Confirmed in the linked object rather than inferred:
`nm -D` shows `oto_oboe_Play` and the `oboe::` symbols, `readelf -d`
shows `libOpenSLES.so` as NEEDED, and the
`Java_com_wails_app_WailsBridge_native*` exports are present. The audio
backend is genuinely linked, not stubbed.

Four things found on the way that are not obvious:

- **`wails3 update build-assets` does not generate `build/android/`.** In
  beta.8 it extracts only `internal/commands/updatable_build_assets`,
  which is darwin/ios/linux/windows. The android tree comes from
  `generate build-assets`, which extracts the *whole* asset FS and would
  rewrite all of `build/`. So it was generated into a scratch dir and
  `android/` copied across. CLAUDE.md claimed the refresh regenerates it;
  that was wrong, and is corrected.
- **`update build-assets` does clobber nfpm's `homepage` and
  `license`**, which `build/linux/nfpm/nfpm.yaml` says in a comment it
  leaves alone. It reset them to `https://wails.io` and `MIT`. The
  comment is wrong; those two fields need re-checking after any refresh.
- **The scaffold's `package:fat` shipped a debug arm64 library.**
  `build` forwards `ARCH` to `compile:go:shared` but not `PRODUCTION`,
  so the arm64 leg recomputed `BUILD_FLAGS` against an unset
  `.PRODUCTION` and took the debug branch — while amd64, which
  `package:fat` calls directly with `PRODUCTION: "true"`, was correct.
  A release APK therefore carried a 40 MB unstripped debug library for
  the phone ABI and a 31 MB production one for the emulator. Fixed in
  `build/android/Taskfile.yml`, which is this repo's one edit to that
  scaffold file and is commented as such. 34 MB APK before, 27 after.
- **The generated APK is not yet an identity.** `com.wails.app`,
  `versionCode 1`, `versionName 1.0`, signed `CN=Android Debug`. That is
  plan 015 phase 2 and none of it is a surprise, but it is worth knowing
  that the scaffold happily produces an installable-once,
  never-updatable APK by default.

**Not established:** that it *runs*. There is no AVD or system image on
this machine and no device attached, so nothing has launched the APK.
Every runtime concern plan 015 lists as out of scope is still out of
scope and still real — MPRIS in particular is compiled *in*, because
Go's `android` GOOS implies the `linux` build tag.

## The Android build runs, and stops on one line (measured 2026-08-16)

The APK installs and launches on an emulator. `libwails.so` loads, the
JNI bridge comes up — and the process is gone six milliseconds later.

**The cause is `backend/system/buildUserDirPath`.** It switches on
`runtime.GOOS` with cases for `darwin`, `linux` and `windows` and a
`default:` returning `errUnsupportedOS`. `runtime.GOOS` is `"android"`,
so it takes the default, `NewYellowJacketApp` fails, and `main()` calls
`os.Exit(1)`. `YJ_HOME` overrides that path on every OS, so an
`android` case pointing at the app-private directory is the shape of
the fix. It is the *first* thing that stops it, not the only one.

**What cost the time was not finding the bug, it was that the failure
is invisible in all three places you would look.** Worth knowing before
meeting it:

- **Go's stdout does not reach logcat.** An app's fd 1 and 2 go to
  `/dev/null`, so the `slog` line naming the error is discarded.
  `setprop log.redirect-stdio true` does not help — that redirects the
  *Java* runtime's `System.out`, not a c-shared native library's.
- **`os.Exit` leaves no evidence.** No panic, no `AndroidRuntime`
  stack, nothing in `/data/tombstones`, nothing in `logcat -b crash` or
  dropbox. The only signal present is `Zygote: exited due to signal 9`,
  which reads as "the system killed it" and sends you looking at the
  low-memory killer.
- **ActivityManager restarts it faster than you can observe it.**
  `pidof` always answers and `am start` always says `Status: ok`, so
  the app looks alive while crash-looping several times a second. The
  honest check is whether it is the *same pid* a few seconds later,
  which is what `make android-smoke` asserts.

The tell is `I/WailsBridge: Wails bridge initialized` followed
immediately by a new pid doing the same thing.

**Emulator environment**, which is not the obvious one on Arch: Gradle
needs a *platform*, and `/opt/android-sdk` (the `android-sdk` package)
has an NDK and build-tools but an empty `platforms/`. So `ANDROID_HOME`
points at `~/Android/Sdk` (user-owned, where sdkmanager writes) while
`ANDROID_NDK_HOME` points at `/opt/android-ndk` — two SDKs, one for
each half of the build. The image is
`system-images;android-35;google_apis;x86_64` (~3.5 GB with the
emulator sdkmanager pulls alongside it): `google_apis` rather than
`default` because this is a WebView app and that image carries the
Chrome-based WebView. KVM is present and usable here; without it a 30 s
boot becomes tens of minutes, which reads as a hung target.

Operating all of this is `scripts/android-emulator.sh` and the
`make android-*` targets, documented in
`.pi/skills/yellowjacket-dev/references/android-tier.md`.

## What the Wails v3 Android docs say, and where they are wrong (2026-08-16)

Read after phase 0, before phase 2. Sources: `ANDROID.md` shipped inside
`wails/v3@v3.0.0-beta.8` (authoritative for our exact version) and
`v3.wails.io/guides/mobile/*`.

**Two claims in `ANDROID.md` are wrong for beta.8, and both were
checked.** Its Configuration section says to put `APP_ID: com.example.
myapp` in `build/config.yml` and that this "controls the package name".
Neither half holds. `wails3 task` builds its variable set from CLI
`KEY=VALUE` arguments and the Taskfile tree and **never reads
`config.yml`** (`internal/commands/task.go`); adding `APP_ID` there and
running `android:run:device --dry` still emits
`am start -n com.wails.app/`. And `APP_ID` feeds only the adb commands
in the android Taskfile — uninstall, launch, log filter — never Gradle,
whose `applicationId` is a literal in `app/build.gradle`. So the
identity is necessarily declared **twice** and nothing enforces
agreement. Both are set now, each with a comment pointing at the other.

**The fix for the crash we found is a documented API.**
`application.Mobile.StoragePath()` returns the app's private internal
files directory (`getFilesDir()` on Android, Application Support on
iOS) and — the useful part — is **build-tag-free**: `mobile.go` declares
the interface and `mobile_stub.go` returns `""` on desktop. Since
`resolveUserDirPath` already lets `YJ_HOME` override the path on every
OS, the whole fix is to set that override from `StoragePath()` early in
`main()` when it is non-empty. No `//go:build` split, no new import in
`backend/system` (which must stay Wails-free — the `indexbuild` tag
split exists for exactly that), and desktop behaviour is untouched
because the stub returns empty.

The same section gives the general rule: branch on
`application.System.IsMobile()` / `IsPlatform(application.PlatformAndroid)`
rather than build tags, because it compiles everywhere.

**`android` implies `linux` is documented**, which confirms rather than
discovers the MPRIS problem: `//go:build linux` files are in the Android
build and desktop-Linux-only ones need `linux && !android`.

**A finding for the runtime plan, not this one: the folder picker does
not exist on Android.** Open-*directory* dialogs "return an error — SAF
yields tree URIs, not filesystem paths", and save-file dialogs likewise.
This app's entire first run is "choose your music folder", and its
library model is filesystem paths. That is a design problem, not a
porting detail, and it is larger than the data-directory one.

**The scaffold ships its own android tasks**, and they are worth knowing
before writing anything: `android:run`, `run:device`, `deploy-emulator`,
`deploy-device`, `package`, `package:fat`, `bundle`/`bundle:fat` (AAB
for Play), `studio`, `device:list`, `logs`, `logs:all`, `clean`, and an
internal `ensure-emulator`. `make android-*` deliberately does not wrap
most of them. Two reasons it does not just use `android:logs`: that task
greps logcat for `(Wails|yellowjacket)`, which matches the `WailsBridge`
tag but **not** the app's own process tag (`app.yellowjacket`, lowercase)
and **not** `ActivityManager`'s "has died" line — the one that tells you
it crashed. And `ensure-emulator` takes whatever `-list-avds | tail -1`
returns, with no pidfile and no boot wait, so it cannot be stopped or
sequenced by a Makefile.

Two smaller things. Debug builds log framework diagnostics to logcat
under the `Wails` tag and are inspectable from `chrome://inspect`;
production builds compile that out — so a debug APK is the more
informative one when something is wrong. And the docs recommend
`build-tools;35.0.0`; 34.0.0 is what is installed here and builds fine.

## The app starts on Android; x86_64 Android cannot run it (2026-08-16)

Two findings, and the second is the one with consequences.

**The startup bug is fixed.** `backend/system`'s `buildUserDirPath`
switched on `runtime.GOOS` and Android took the `default:` branch, so
`main()` called `os.Exit(1)` six milliseconds after the JNI bridge came
up. `main()` now calls
`system.UseHomeOverride(application.Mobile.StoragePath())` before
anything asks for a path. `StoragePath()` is `getFilesDir()` on
Android, Application Support on iOS and `""` on desktop — where
`UseHomeOverride` is a no-op — so the change needs no build tag and
alters nothing off mobile. `backend/system` gained no import of the
Wails application package, deliberately: that is the same constraint
the `indexbuild` split protects in `backend/events`.

**And then it takes SIGSYS on the x86_64 emulator.**

```
F/libc: Fatal signal 31 (SIGSYS), code 1 (SYS_SECCOMP), syscall 6
F/DEBUG: Cause: seccomp prevented call to disallowed x86_64 system call 6
```

Syscall 6 on x86_64 is `lstat`, and the caller is **not our code and
not Go's**. Go's `syscall` package already routes both `Stat` and
`Lstat` through `fstatat` on amd64 *and* arm64. The caller is
`modernc.org/libc`, which `modernc.org/sqlite` sits on and therefore
the entire database layer: `libc_linux_amd64.go`'s `Xlstat64` issues
`unix.Syscall(unix.SYS_LSTAT, …)` directly. Android's seccomp filter
forbids it because bionic never issues it.

**arm64 is unaffected, structurally rather than by luck.** arm64 has no
`lstat` syscall at all, so `ccgo_linux_arm64.go`'s `Xlstat` is
`Xfstatat(…, AT_SYMLINK_NOFOLLOW)` → `SYS_newfstatat` (79), which is
permitted. `grep -c SYS_LSTAT ccgo_linux_arm64.go` returns 0 against 1
for amd64.

Three consequences:

- **The default emulator cannot verify this app.** `make android-smoke`
  on an x86_64 AVD reports a tombstone that says nothing about your
  change. Verification needs an `arm64-v8a` image (full software
  emulation on an x86_64 host, so slow) or a real device.
- **The x86_64 half of the fat APK is dead weight on every Android**,
  not just emulators — an x86 Chromebook would hit exactly this. It is
  31 MB of a 27 MB compressed artifact. Dropping it is a real option;
  keeping it costs size and buys an emulator target that does not work.
  Not decided here.
- The failure is at least *legible*. Unlike the `os.Exit` it replaced,
  SIGSYS leaves a tombstone with a backtrace into `libwails.so`, which
  is how it was identified in one pass.

Worth knowing for anything else that reaches for a pure-Go C library:
this class of bug is invisible to every build and every desktop test,
and appears only under a platform's syscall filter.

### …and the arm64 emulator is not an option on an x86_64 host

Emulator 37.1.11 refuses outright, after the 3.8 GB image download:

```
FATAL | Avd's CPU Architecture 'arm64' is not supported by the QEMU2
        emulator on x86_64 host. System image must match the host
        architecture.
```

Google dropped cross-architecture emulation and there is no flag for
it. So the arm64 claim above rests on reading modernc's two code paths,
not on having run it: verifying the shipped ABI needs an arm64 host, a
physical device, or `adb connect` to one. The image was deleted again;
do not re-download it.

## Android media controls need no new JNI and no new dependency (2026-08-16)

Plan 016's A4 — playback that survives the screen locking — turned out
to be reachable entirely through seams that already exist, which is the
finding worth keeping. The obvious blocker is that Wails' `androidBridge*`
helpers are unexported, so Go cannot call arbitrary Java. It does not
need to:

- **Go → Java** is `application.Android.StartForegroundService(json)`,
  which *is* exported, and `build/android/` is our tree — so widening
  the JSON that `WailsBridge.startForegroundService` accepts is a local
  edit, not a fork of the runtime.
- **Java → Go** is `WailsBridge.emitEvent(name, json)` →
  `nativeEmitEvent` → `app.Event.Emit`, which a Go `app.Event.On`
  subscriber receives with `Data` as a `map[string]any`.

So the handler is one JSON document out and one command event back, and
`backend/mediacontrols`' existing `Handler`/`Callbacks` interface — written
for MPRIS — needed one addition (`OnDuck`) to cover a MediaSession.

**The Java side needs no androidx.media either.** `MediaSessionCompat`
is the documented route, but `android.media.session.MediaSession` and
`Notification.MediaStyle` are both API 21 and minSdk here is 21, so the
platform API covers it with two `Build.VERSION` branches (the channel,
and PendingIntent mutability flags) and no new Gradle dependency.

Four things measured or reasoned along the way, each of which would
have been a bug:

- **From API 26 the framework ducks the app itself** and sends no
  `AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK`. So a duck implemented in the
  player is a *pre-Oreo* path, and `setWillPauseWhenDucked(true)` —
  which is how you get the callback back — would mean pausing for
  every notification tone. Implementing both attenuates twice.
- **A duck must not touch the user's volume.** `Player.SetDuck` holds
  the attenuation as a separate offset and re-applies the user's level
  through `setVolumeLocked`, so it cannot accumulate across repeated
  ducks and `getUserVolume` — which feeds the event, the persisted
  state and every relative change — still reports what the user chose.
- **From Android 12 a background app may not *start* a foreground
  service**, but it may keep delivering intents to one already running.
  Every update after the first is exactly that case (a track change
  with the screen off), so `WailsBridge` picks `startService` over
  `startForegroundService` once `WailsForegroundService.running` is set.
- **A service started with `startForegroundService` that returns from
  `onStartCommand` without calling `startForeground` is killed**, so
  the transport-button intents call it too rather than only the payload
  path.

**`make lint` does not see any of this.** Its three passes are the app,
`indexbuild` and `dev` tag sets, all on linux/amd64, and `android.go` is
behind the `android` build tag — the only thing that compiles it is the
cross-compiler in `make android`. That is why the payload keys, the
state words and the command names live in `androidpayload.go` *without*
a build tag, with a test: it is the half that can be checked on the
machine doing the work. A quick manual check of the tagged half is

```bash
B=$(echo /opt/android-ndk/toolchains/llvm/prebuilt/*/bin)
CC=$B/aarch64-linux-android21-clang CXX=$B/aarch64-linux-android21-clang++ \
  GOOS=android GOARCH=arm64 CGO_ENABLED=1 go build ./backend/...
```

— `CXX` matters: without it the oboe C++ sources compile against the
host sysroot and fail on `android/log.h`, which reads like a missing NDK.

**None of it has run.** The APK builds for both ABIs and the Go and Java
halves compile; everything above about behaviour is read from the
Android documentation and the source. The x86_64 emulator still cannot
run this app (modernc `lstat`/seccomp, above) and an arm64 AVD still
cannot exist on an x86_64 host, so A4's first real test is a device.

## Dropping x86_64 cut the APK by 41% (measured 2026-08-16)

Plan 016's B1, decided: the ABI is gone.

| | fat (arm64 + x86_64) | arm64 only |
|---|---|---|
| `bin/yellowjacket.apk` | 27,059,130 B | 15,898,465 B |
| `lib/` entries | 2 | 1 |

It buys nothing to keep. x86_64 Android takes SIGSYS the first time it
touches the database (modernc's raw `lstat` against Android's seccomp
filter, above), which is *every* x86_64 device — emulators and x86
Chromebooks alike — not merely the emulator here.

Three places had to agree, and the third is the one that would have
made this a silent no-op: `abiFilters` in `build/android/app/
build.gradle` (what Gradle packages), `android:package` rather than
`android:package:fat` in the Makefile (what Go compiles — otherwise the
31 MB library is still built and then discarded), and the `native-code`
assertion in `android-apk.yml`'s Verify step, which is now
`native-code: 'arm64-v8a'$` and fails if a second ABI ever comes back.
The anchor is deliberate and was checked against a real artifact:
without it the pattern also matches the fat APK's line.

One consequence for the dev tier was written down before it was
checked, and checking it proved it false — see the next entry.

## arm64 translation runs Go until Go asks the CPU what it is (measured 2026-08-16)

Predicted, when the x86_64 ABI was dropped: `make android-install`
against the emulator would now fail with
`INSTALL_FAILED_NO_MATCHING_ABIS`. **Measured: it installs and
launches.** Google's `google_apis` x86_64 images carry arm64
translation —

```
ro.product.cpu.abilist = x86_64,arm64-v8a
```

— so the loader maps `lib/arm64/libwails.so` and executes it; the
tombstone confirms it with `ABI: 'x86_64'` / `Guest architecture:
'arm64'`.

It dies anyway, before a line of our code, and the instruction says
exactly why. The fault is at `libwails.so+0x15911d0`:

```
signal 4 (SIGILL), code -6 (SI_TKILL)
15911d0: d5380600   mrs  x0, ID_AA64ISAR0_EL1
```

That is Go's `internal/cpu` reading the arm64 feature-ID system
register during runtime init. The translator does not implement it, so
**no Go binary starts under it** — this is not a property of this app
and no work here would change it. (`code -6 (SI_TKILL)` also means the
signal was re-raised by the process itself: Go's handler caught the
SIGILL, printed a traceback to a stdout that goes to `/dev/null`, and
re-raised. The invisible-failure rule again.)

So there are now three distinct ways this app fails on an x86_64
Android, none of them a bug in it:

| build | cause | signal |
|---|---|---|
| x86_64 | modernc's raw `lstat` vs seccomp | SIGSYS, syscall 6 |
| arm64, translated | Go reads `ID_AA64ISAR0_EL1` | SIGILL |
| arm64, real device | — | still unverified |

**A physical arm64 device is still the only verification path**, which
is the conclusion the previous session reached by a different route.
The value of this entry is that it closes the remaining plausible
shortcut, with the instruction that closes it.

### Two bugs the attempt found in the harness itself

Both were on `main`, and the first had made the whole tier unusable
since the commit that added it.

**`scripts/android-emulator.sh` did not parse.** A `case` pattern read
`*signatures do not match*)`, and `do` is a reserved word: bash fails
the parse of the *entire file*, so `make android-emulator`,
`android-install`, `android-smoke` and `android-logs` all died with
`line 190: syntax error near unexpected token 'do'`. Quoting the inner
words fixes it. A shell script that is only run interactively can carry
a syntax error indefinitely — `bash -n` in the pre-commit hook would
have caught it, and does not exist.

**A bare `adb` addresses whatever is attached.** With a second emulator
present (another project's, or a stale `offline` entry from a previous
run), every adb call fails with "more than one device", and
`cmd_install` reported that as *"no device — run 'make
android-emulator' first"* — directly after that had printed "waiting
for boot ok". `pick_device` now resolves `ANDROID_SERIAL` from
`ro.boot.qemu.avd_name`, since serials are assigned in boot order and
the AVD name is the stable identity. Verified with both emulators
running: it selects `yj-test` and installs.

## The phone shell fits, and what it cost to make it fit (2026-08-16)

Plan 016 B2, phase 1: the shell below 600px. Measured at 360×780 and
390×844 against the real app (`make dev-headless` + Playwright, which
is the tier that can answer this — server mode serves the same document
an Android WebView renders).

**What overflowed, and by how much.** The body was 652px wide in a
360px viewport before any of this. Walking every element and its shadow
roots for a `right` past the viewport named the causes in order:

| element | width | why |
|---|---|---|
| `header.top-bar` | 580 | its children's minimums, summed |
| `search-bar` | 320 | `.search-container { min-width: 200px }` |
| `job-indicator` | 157 | the label, "3 background jobs" |

A `min-width` in a flex row is a *hard* floor — it does not shrink — and
a grid item's implicit minimum is `auto`, i.e. its content. So the
header could not get smaller than the sum of what it held, the body grew
to the header, and `overflow-x: hidden` would then have hidden a third
of the app rather than fitting it. `min-width: 0` on the boxes between
the viewport and the content, plus each component standing its own
non-essential parts down in its own stylesheet, takes 360 → 360 exactly.
At 320px (400% zoom, the width WCAG 1.4.10 names) it is also exact.

**So an existing spec now asserts the opposite of what it did**, and
that is the fix landing rather than the test being weakened.
`layout-overflow.spec.ts` used to assert that the 464px of app behind
`overflow: hidden` *could be scrolled to* with a wheel gesture, which
was the remedy available when the shell had one layout. It reflows now,
which is what 1.4.10 asks for; scrolling to the overflow was the
concession.

**And a shared component brings its test handles with it.**
`bottom-nav`'s "More" opens the *existing* `<app-sidebar>` in a drawer —
the whole point being not to write a second list of destinations — but
rendering it unconditionally put a second `data-testid="nav-home"` (and
ten siblings) in the DOM. **30 existing specs failed** with "strict mode
violation: resolved to 2 elements", on a *desktop* viewport where
`bottom-nav` is `display: none` and the drawer can never open. Lazy
rendering fixes it; the component test asserts the absence, because the
failure is invisible from inside the component and appears in files
nobody touched.

Three smaller things worth keeping:

- **A new icon name is a runtime failure, not a build one.** `bars` was
  not in `src/icons/names.txt`, so `offline-icons.spec.ts` caught it —
  the sweep asserts `window.__yjIconMisses` is empty. `node
  frontend/scripts/fetch-icons.mjs` re-vendors after adding a line.
- **A `wa-drawer` animates, so a test asserts its events**, not its
  `open` property: setting `open = false` starts a hide that has not
  finished on the next microtask, and a test reading the property in
  between sees the state it is leaving.
- **`update(el)` in the component tier takes two arguments**
  (`update(el, {})`), which is only visible from `tsc`, not from a
  failing test.

### The local e2e tier was not running the same app CI runs

`requested-badge.spec.ts` failed two of three tests locally while CI was
green, and the reason is worth more than the fix: **`dev-headless.sh`
was the only place that did not neutralise `YJ_CORE_INDEX_URL`.**
`seed-sandbox.sh` and `ci.yml` both point it at `127.0.0.1:1`; the dev
launcher did not, so the app downloaded and built the real ~1M-row
Explore catalog into the run's `YJ_HOME`, and a local `make e2e` then
ran against a world CI never sees.

Found by reading the failure screenshot: the spec had searched Explore
for its fixture album and the page was full of *real* ones — Real
Estate, Arrested Youth, The Yes Album. The staged row was there and
invisible among a million others.

`dev-headless.sh` now defaults the variable to the dead address and
takes an explicit one if you want the real catalog for exploring by
hand. `make e2e` locally: 97 passed / 3 failed before, 100 passed
after.

The second half of the same problem is that **the backend is one shared
process with one database, and specs leave rows in it.**
`explore-shelves` staged its catalog only `IfEmpty`, so a single album
row left behind by `requested-badge` satisfied that gate, the shelves
were drawn from one foreign row, and the artist card the spec clicks did
not exist. It fails on the *second* local run and passes on the first,
which is the least useful order, and never in CI, where every run gets a
fresh `YJ_HOME`.

"Is the catalog empty" was the wrong question; "are my rows there" is
the right one. The staging is unconditional now (`INSERT OR IGNORE`
keyed on the MBID) and the assertion moved from *this insert wrote a
row* to *every fixture row is present* — which is both idempotent and a
stronger check, since an MBID failing `CHECK(length(mbid) = 16)` is
silently dropped by OR IGNORE and would otherwise show up as an empty
page rather than a failed setup.

**Verified: the full suite runs twice against the same app, 100 passed
both times.** That is the property to keep — a spec tier whose second
run differs from its first is a tier that will one day blame the wrong
commit.

## A media query adds no specificity, and dead CSS looks like working CSS (2026-08-16)

Plan 016 B2 phase 2 shipped the full-screen now-playing view, and
checking it with a screenshot found that **phase 1's shell rules had
never applied**.

`index.css` is base rules then component rules, and the phone block had
been inserted in the middle — above the plain `.top-bar` and `.title`
rules it meant to override. A media query is not a specificity boost,
so with equal specificity the *later* declaration wins. Measured at
390px before the fix:

| declared for the phone | actually computed |
|---|---|
| `padding-left: 0.75em` | 32px (the 2em base) |
| `gap: 0.5em` | 16px (base) |
| `font-size: 1.1em` | 24px (the 1.5em base) |
| `grid-template-columns: minmax(0,1fr) auto auto` | `320px 1fr auto` (base) |

After moving the block to the end of the file: 12px, 8px, 17.6px, and
`154px 187px 33px`.

**Nothing failed while they were dead**, which is the part worth
keeping. The phone spec asserts that the shell does not scroll
sideways, and it did not — because the fitting was being done by
`min-width: 0` and by each component's *own* media query, which live in
their own stylesheets and so had no later rule to lose to. The
declarations that did nothing were the cosmetic ones, and no assertion
was ever going to see them. A screenshot did, in about ten seconds.

The file now ends with one phone section, and says why it is last.

### What the same screenshot found about the view itself

The bottom bar was still rendering the mini player *underneath* the
full-screen view — 4em of a 844px phone spent saying exactly what the
view above it says, and invisible to every assertion about either one
(both were correct on their own). `index.css` hides `.bottom-bar` while
`#main-content[data-active-view="now-playing"]`, through `:has()`
rather than a class toggled from `index.ts`: which view is showing is
already published as an attribute, and a second expression of the same
fact is a second thing to keep in step.

That took the queue button away with it, since that button lives in the
bar — so the view carries its own, toggling the same `open` attribute
on the same panel element.

**And a css`` literal cannot contain a backtick.** A comment reading
"the track size is set on the `wa-slider` inside its shadow root"
terminates the tagged template, and the failure arrives as
`Expected "]" but found "wa"` from the CSS parser, at a line number in
the *comment*. `make css-check` exists for this and named it
immediately.

## The index artifact could not be exported, and the reason is a rule this repo already had (2026-08-16)

`maintain-index` failed on an unrelated push:

```
indexexport: copy rows: SQL logic error: no such column: total_tracks (1)
```

Three minutes in, on the one job that owns the ~205 GB checkpoint and
publishes the catalog every user downloads.

**The cause is the exception that keeps that checkpoint alive.** The
index job's `/cache` is a real `YJ_HOME` that survives between runs, so
`explore_index` there is classified `Cache` and is deliberately *not*
dropped and recreated by `cmd/indexbuild`'s schema repair
(`staleschema.go`). A column added to the schema afterwards is
therefore simply absent from that database — and `total_tracks` was
added by the album-completeness work. The exporter selected it anyway.

**The fix is the rule the importer already follows.**
`artifactHasTotals()` exists precisely because "adding a column to the
importer's SELECT is how you break every artifact already published";
the mirror image — *reading* an index older than the binary — had no
such guard. `sourceColumns()` asks
`pragma_table_info('explore_index', 'main')` and selects a literal `0`
when the column is not there, which is what the column already means by
"the catalog does not say" and what the app already renders as unknown
rather than as incomplete. The destination keeps every column, so an
importer needs no second shape.

So the pattern generalises, and is worth stating once: **any query that
crosses a version boundary in either direction asks the schema rather
than trusting it.** There are now three of these — `artifactStoresText`
(encoding), `artifactHasTotals` (import), `sourceColumns` (export).

Two things about the test are worth keeping.

It reproduces the failure **symptom first**: with the fix removed it
fails with the CI message verbatim, `copy rows: SQL logic error: no
such column: total_tracks (1)`. That was checked, not assumed.

And its first version silently proved nothing. `oldColumns` was
`strings.Replace(catalogColumns, "total_tracks, ", "", 1)` — which
matches *nothing*, because the list is formatted across lines and the
name is followed by a newline rather than a space. So the "old" index
had every current column, the probe correctly said so, and the only
reason this was caught is that the assertion about the probe ran before
the assertion about the export. A fixture built by string surgery on a
formatted constant needs to be whitespace-independent; it filters the
list now.

## Long-press is one document listener, and the header row is a row (2026-08-17)

Plan 016 B2 phase 3. A phone has no right-click, and every context menu
in this app opens from a `contextmenu` event — six components' worth,
bound three different ways (delegated on a virtualizer, per row, per
card). `frontend/src/utils/long-press.ts` is one document-capture
listener installed once from `index.ts`: a touch that holds still for
500 ms dispatches a synthetic `contextmenu` at the touch point, and
**every existing handler runs unchanged**. No component opted in, and
none can forget to.

Four things it has to get right, and each is a way the obvious version
fails:

- **The target is `composedPath()[0]`, not `elementFromPoint`**, which
  stops at the outermost shadow host. Every menu here is bound inside
  one, so a host-targeted event reaches a delegated listener and no
  per-row one.
- **A browser that fires its own must win.** Chromium already dispatches
  `contextmenu` on long-press; WebKit and the WebView vary. One arriving
  during the press cancels ours; one arriving after ours is swallowed at
  document capture.
- **Ours is told from theirs by identity** (a `WeakSet`), not by
  `isTrusted`. `isTrusted` would work in the app and is untestable — no
  test can dispatch a trusted event — so the suppression path would have
  been the one thing with no coverage.
- **The click ending the gesture is swallowed**, keyed on the gesture
  (cleared by the next `pointerdown`) rather than a time window, or a
  quick tap on the menu that just opened is eaten too.

**What cost the time was the assertion, not the code.** The e2e spec
pressed `[role="row"]` — which is the *column header*, and it is the
first one. The gesture fired correctly, the header correctly ignored it,
and the failure looked exactly like a menu that would not open. Found by
probing the running app (`playwright-cli eval`, dispatching the same
pointer events and logging what saw the `contextmenu`), which showed the
event reaching the row's own listener with no menu behind it — i.e. the
handler was refusing it, not missing it. `.track-row` is the selector.

Verified by execution: 8 component tests (real browser, real shadow
boundary, real timings) and 2 e2e specs against the running app, twice
in a row. Not verified: any of it under a real finger on a real
WebView — the pointer events are dispatched, because neither Desktop
Chrome nor Desktop Safari has touch and there is no device tier.

## The first device run: A4 works, and two things only a phone could say (2026-08-17)

The published v1.5.0 APK, on a real phone, owner-reported. **This is the
first runtime evidence any of the Android work has ever had** — A4
shipped entirely reasoned from source.

**What holds.** Playback survives the screen locking. The MediaSession
notification appears in the status pane *with album art* — which
answers, in one observation, four of the open questions from plan 016:
the foreground service starts, POST_NOTIFICATIONS was granted and the
notification is visible, the session is picked up, and **cover art
decoded from a `MANAGE_EXTERNAL_STORAGE` path by a service is
readable**. The last was the one nobody could argue from documentation.

**Two bugs, and neither is visible from any tier we have.**

*Back did not navigate back.* The scaffold's
`MainActivity.onBackPressed` asks `webView.canGoBack()` and finishes the
activity otherwise — and this app had never touched `history`, so that
was false at every depth and back quit from anywhere. The fix is in the
frontend, not in Java: a navigation is a `history` entry now
(`recordNavigation` in `index.ts`, same URL, the destination in the
entry's state) and `popstate` replays it with `_isBack`. The Java half
needs no change, because the mechanism it already uses is the one we
were failing to feed.

Two rules keep it honest. The **first** navigation replaces the launch
entry rather than pushing one, or every launch costs a back press before
the app will close. And the in-app back buttons go through
`history.back()` rather than popping a stack of their own — `navStack`
is **deleted**, not kept alongside, because two stacks is exactly how
the detail view's own button and the phone's gesture come to disagree
about how far back one press goes. `back-navigation.spec.ts` pins that
invariant.

*The transport was off screen.* **`targetSdk 35` is Android 15, which
lays every app out edge-to-edge**, ignores the deprecated
`statusBarColor`/`navigationBarColor` the theme still sets, and hands
the app a window the size of the screen. The WebView is `match_parent`,
so the page's bottom band — the transport, and on a phone the tab bar —
was drawn underneath the gesture bar. `applyWindowInsets()` pads the
container by `systemBars | displayCutout | ime` and returns the insets
rather than consuming them. The window background goes black to match
the app's own ramp, or the padding shows as a blue-grey band.

**Neither is findable in the browser tier, and that is the lesson worth
keeping**: a viewport has no system bars, so `phone-shell.spec.ts` at
390x844 renders a shell that fits perfectly while the device cuts 48dp
off the bottom — and `page.goBack()` was never called because nothing in
a desktop shell has a back gesture. The Android tier's own note says
failure there is invisible; this is the milder version, where the app
works and is simply wrong in ways only the platform can show you.

Verified by execution: the APK builds with the Java change; 3 e2e specs
cover the history behaviour, on Chromium locally and WebKit in CI.
Not verified: the insets themselves, which need the next APK on the
owner's phone. What to look for is one thing — the transport and the tab
bar clear of the gesture bar, and the header clear of the status bar.

## The phone is a Chrome 113 WebView, and that reframes everything (2026-08-17)

The device is reachable over adb now, so the tier can be *asked* rather
than reported on. `make android-inspect` + `make android-eval` are that:
a debug build (`applicationIdSuffix ".dev"`, so it installs **beside**
the release app rather than needing the uninstall that would take the
library with it) opens `webview_devtools_remote_<pid>`, and raw CDP over
Node's built-in WebSocket evaluates in the real page. **Playwright
cannot do this** — `connectOverCDP` calls `Browser.setDownloadBehavior`
and a WebView answers "Browser context management is not supported",
killing the connection before the first evaluate.

Measured on the device (Light Phone III, TLP301):

| fact | value |
| --- | --- |
| Android | 14, SDK 34 |
| screen | 1080x1240, density 408 |
| WebView viewport | **424 x 439 CSS px**, DPR 2.55 |
| WebView engine | **Chrome 113.0.5672.136** (mid-2023) |

**The first correction: the insets commit does not explain the report.**
Edge-to-edge is forced for apps *running on* Android 15, and this phone
is Android 14 — the screenshot shows the app correctly inset, with the
status bar and the gesture bar outside it. `applyWindowInsets()` is
right and stays (the next phone, or one OS update, is Android 15), but
it is **pre-emptive, not the fix for "the controls are off screen"**.
That was an inference from a version number, and the device disagreed.

**The second correction: the black `fill` proves nothing.** A wa-icon on
the device has the right `color` (#ffd43b) and an `<svg>` in its shadow
root, and `getComputedStyle(svg).fill` is black — but that is the *svg
root*, and every vendored Font Awesome path carries
`fill="currentColor"` itself, so the root's fill is irrelevant. Measuring
the wrong node produced a diagnosis-shaped result. `__yjIconMisses` is
empty, so no name is unbundled either. Why the icons do not appear in the
screenshot is **still open**.

**What the engine version does explain, and what to check next.**
Chrome 113 has `:has()`, `color-mix()` and `dialog.showModal()`, and
lacks three things this app's dependencies use:

- **Relaxed CSS nesting** (Chrome 120): a nested rule starting with a
  bare element selector is dropped. `.x { svg { ... } }` parses to
  nothing; `.x { & svg { ... } }` parses. Any Web Awesome or app
  stylesheet written the modern way silently loses declarations here,
  and dropped declarations are exactly the failure that looks like
  "rendered but wrong".
- **The Popover API** (Chrome 114). Web Awesome's popup calls
  `showPopover?.()` — optional, so nothing throws — but also sets
  `popover="manual"`, which on 113 is an unknown attribute doing
  nothing. Every context menu, dropdown and the whole menu keyboard
  model rides on that, so it is the first thing to test with a library
  present.
- `light-dark()` and relative colour syntax (`rgb(from ...)`).

**The lesson for the tier: a device is an engine, not just a screen.**
Every browser tier here runs a current Chromium or WebKit, and the phone
that will actually run this app is two years behind — so "it renders at
424x439 in Chromium" (checked, the transport is on screen) says nothing
about whether it renders on the phone. The e2e tier cannot be fixed by
resizing; the missing signal is version, and CDP against the device is
the only place to get it.

Verified by execution: every number in the table, the four feature
probes, and that the hardware back button no longer kills the app (the
`.dev` build carries the history fix; pid survived a BACK press).
Unverified: what happened to the icons and the transport controls, which
is where this resumes.

## What the device actually said, with both builds side by side (2026-08-17)

The phone inspectable and awake, the same Light Phone III running two
builds of this app in turn. This closes both questions the previous entry
left open, and **neither answer was the one the symptom suggested**.

**"The playback controls are off screen" was true, literal, and already
fixed.** The installed build is from B2 **phase 1** — it carries
`bottom-nav` and no `now-playing-view`, which dates it between 57bfbdf
and 1b05dde. Settled (30 s after launch, not 6), its player bar shows
art, title, favourite, shuffle, prev — and stops. Play/pause, next,
repeat and queue are past the right edge, because at 424 px the bar was
still carrying the seek bar and volume that **phase 2 moved into
`now-playing-view`**. On the current build, on the same phone and the
same engine, `document.body.scrollWidth` equals `clientWidth` (424) and
`player-controls` measures 200..380 inside 424. So the fix was already
on main, unreleased, and the device is what proved it rather than
argued it.

**"No icons" was an artefact of my own screenshot.** A `wa-icon` on the
device has `path` computed fill `rgb(255,212,59)` and paints; the first
capture was six seconds after a cold start, before the icon fetches had
landed. Two corrections in two entries from the same misreading: measure
the node that paints, and let the app settle before believing a picture.

**Chrome 113's missing Popover API does not break the menus.** This was
the leading worry and it is unfounded: a long-press on a row opens the
real panel at (212,145), 162x193, `visibility: visible`, seven
`role=menuitem`s, all seven inside the panel and clear of the player bar
— confirmed by screenshot as well as by measurement. Web Awesome's
`showPopover?.()` is an optional call and `wa-popup` positions itself,
so the attribute being inert costs nothing. **Long-press itself works on
real hardware**, over a real 1,744-track library, which is the phase 3
verification the browser tier could only approximate.

**The one genuine fault the device adds is phase 4's.** `track-list` at
424 px computes `--grid-cols: 24px 102px 101px 101px 80px` — which fits
the host exactly, so nothing overflows — but "Duration" does not fit in
80 px and neither does most content. The columns are not too wide; there
are simply too many of them for a phone, which is what phase 4 already
says. It is now a measurement rather than a prediction.

Two operational notes. The debug sibling scanned the phone's real music
and its data directory is **414 MB**, so it is worth uninstalling when
done (`adb uninstall app.yellowjacket.dev` — the sibling id is exactly
what makes that safe). And `am start` does not reliably take focus while
another app is foreground: check `topResumedActivity` before trusting a
screenshot, or you will read someone else's app.

## The phone track list, and the bug a viewport could not have found (2026-08-17)

B2 phase 4. A phone draws `titleArtist` — the title with the artist
under it — plus the duration, and drops the column headers and the
resize handles. It is a **column set, not a second row template**: the
row, its delegated events, the selection semantics, the playing marker
and the virtualizer never learn that anything changed, because from
their side only the number of columns did.

Three rules, each one a way it breaks otherwise. The row height is in
two places (`PHONE_ROW_HEIGHT` and the CSS) and they must agree, since
the virtualizer positions rows from that number. What is *drawn* and
what can be *sorted* are separate questions — the sort list is built
from `configuredColumns`, or a phone with no headers could sort by
nothing but title and duration. And a phone's widths are neither loaded
nor saved.

**That last one is the finding, and it came from the device.** With the
arrangement passing five component tests and five e2e specs at
424x439, the phone showed `24px 148px 236px`: the duration column with
55% of the row. `loadColumnWidths` is keyed by column *id* and fills a
gap with `MIN_COLUMN_WIDTH`, so the stacked column — which nothing can
ever have saved a width for, there being no handles to drag — came out
at the minimum while `trackLength` inherited a width saved for a
four-column desktop row. The mirror image is worse and was never
reachable from a phone at all: `saveColumnWidths` would have written the
computed phone widths back under the same ids, replacing the width the
user dragged on a desktop.

**Why every browser test missed it.** The specs assert the *shape* — how
many grid tracks, no header, no overflow, the title's share of the row —
and the width bug depends on what is in `localStorage` for a *different*
column set. dev-headless's seed happened to hold widths that split the
other way, so the same assertion passed in the browser and failed on the
phone. The unit test now carries the desktop map as a fixture, which is
the reproduction the browser needed to have.

**Confirmed on the phone afterwards**, with the fix installed:
`24px 304px 80px`, 52 px rows, no header row, the title 298 px and not
truncated, `body.scrollWidth == clientWidth`. The same numbers the
browser gives at that viewport, which is the point of having measured
both.

Two tooling notes worth keeping. `playwright-cli` holds its page across
a `make dev-headless` restart, so a probe after a rebuild can be
answering for the *old* bundle — it reported the desktop layout at 424 px
until the page was reopened. And wireless adb dropped twice more mid-
session when the screen slept; USB for anything longer than a few
probes.

## The catalog download now asks about the connection (2026-08-17)

Plan 016 B4. ~0.6 GB had no network awareness at all; it is skipped on a
cellular connection unless the user says otherwise
(`AllowMeteredCatalogDownload`, default false, toggle in Settings' Search
Index section).

**The shape is dictated by the cgo rule, not by taste.** `explore` is
imported by `cmd/indexbuild`, which builds with `CGO_ENABLED=0` and must
not link Wails, so `netpolicy.go` holds the policy and the JSON parsing —
tested on every platform — while the one platform call is a closure
injected from `app.go`, where naming `application` is already legitimate.

Four things measured or corrected in the doing:

- **The portable name is `application.Mobile`, not `application.Android`**
  (which the plan and `CLAUDE.md` both named). `Android` exists only
  under the `android` build tag; `Mobile`'s desktop implementation is a
  stub whose `NetworkJSON()` returns `""`.
- **The runtime reports no metered flag.** `{"connected":bool,
  "type":"wifi|cellular|ethernet|none"}` is all there is, so cellular is
  the signal and a metered *Wi-Fi* — a phone hotspot, a hotel — cannot be
  detected. Android itself knows (`NET_CAPABILITY_NOT_METERED`) and the
  runtime does not pass it on. Documented gap, not an oversight.
- **An unknown answer must not read as metered.** Every desktop answers
  `""`, so the obvious defensive default would have disabled the catalog
  download for every desktop user in the world.
- **The gate belongs before the first status write.** Declining is a
  no-op — no job in the indicator, no error tier to dismiss — which is
  what makes the refusal safe to have on by default.

## The stale-shape repair dropped the CI catalog (2026-08-17)

Not our change, but it is the operational state everything else now runs
in, and the restore condition needs to be written down somewhere that is
not a commit message.

`fix(database): retire a table whose shape the schema moved past` added
`staleshape.go`: before `applySchema`, drop any non-Authored table whose
live shape disagrees with the schema. That is the right rule for an
install — a client's catalog is *downloaded*, so a stale one costs a
minute of re-fetching the artifact, and keeping it costs every Explore
read.

It runs inside `database.NewDB`, which `cmd/indexbuild` also calls. On
the first run after it landed, 19 seconds in:

```
16:15:51 retiring a table ... table=explore_index
           reason="column entity_type is TEXT, schema declares INTEGER"
16:16:05 index maintenance mode=build reason="no completed import yet"
           lastImported=never baselineSeries=0
```

**The premise was false for the one database where it was expensive.**
That catalog is not stale; it is deliberately kept in the older text
encoding, which `artifactStoresText` and `sourceColumns` exist to
tolerate — so it would have been judged stale and dropped on *every*
run. And `retireLibraryTables`, in the same package, already documents
the opposite rule for this database: drop everything the datamap does
**not** call Cache.

`fix(database): never retire the catalog the index build derives` makes
the policy a build tag (`retireStaleCache`, false under `indexbuild`),
which is how this project already separates the index tools. It prevents
recurrence and cannot undo the drop: that volume was the only copy.

**What it cost, and the shape of the cost.** A full re-import from the
MetaBrainz dumps, resumed across runs from a checkpoint, at a rate that
swung between 2 and 15 MB/s. The job runs on **every push to main** with
a 3 h budget on a runner of capacity 1 — so until the import completes,
every push books three hours and ordinary CI queues behind it. That is
the real damage: not one lost job, but a repeating one.

So the `push:` trigger in `index-artifact.yml` is **commented out**
until a run reports `complete=true`; the weekly cron and
`workflow_dispatch` still resume the build, which is all it needs.
Restoring those two lines is the whole revert.

Three things worth keeping from it:

- **A repair belongs where its assumptions hold.** `NewDB` is the one
  chokepoint every binary in this project shares, including the one
  whose database cannot be re-derived cheaply. Anything destructive
  there needs to ask which binary it is in — the build tag was available
  and is what the fix used.
- **The only copy of a 205 GB derived asset is one Docker volume.**
  There is no snapshot, so the restore time is "however long
  MetaBrainz takes today". A periodic copy would turn this class of
  incident into twenty minutes.
- **The fix's residual trade is now the thing to watch**: with Cache
  tables never retired under `indexbuild`, a future `explore_index`
  column fails that job loudly at build time instead of silently
  rebuilding. That is the right default, and it means the next schema
  change touching `explore_index` needs a deliberate plan for this one
  database rather than none.

## Two guards for the index cache, and what each one is worth (2026-08-17)

Both come out of the incident above, and they protect different halves
of it.

**`TestNoCacheTableIsRetiredHere` asserts the outcome, not the
mechanism.** The test that shipped with the fix pins one table in one
wrong shape, which is the failure that happened; what actually cost the
rebuild was a destructive repair added at `database.NewDB` — the
chokepoint every binary here shares — without asking which binary it was
in. The next one will have a different name and a different reason. So
this puts *every* `datamap` Cache table into a shape the schema has
moved past, opens the database the way `cmd/indexbuild` does, and
requires all of them to still be there.

Three things it got right by being written this way. The table list is
`datamap.ByKind(Cache)`, so the two credit tables added the same day
were covered without anyone adding them — flipping the policy back fails
on **five** tables including `artist_credit_part` and
`artist_credit_ref`, where the single-table test fails on one. It
asserts rows survive as well as the table, because SQLite does an
implicit DELETE before a DROP and a repair that recreated the table
would otherwise look identical. And it *accepts* an error from `NewDB`,
because that is the trade the fix documents: loud failure instead of a
silent day of downloading.

**`scripts/index-cache-snapshot.sh` covers the half no test can.** The
volume held the only copy of a catalog whose rebuild is hours of someone
else's bandwidth. `VACUUM INTO` rather than `cp`, because a byte copy of
a live SQLite file is a corrupt file of plausible size; the staging
directory is deliberately not copied, since a build resumes without it;
and the snapshot is reopened and asked for its catalog row count before
any rotation happens. Both failure paths were exercised rather than
argued: a corrupt source and an empty catalog each exit non-zero, delete
their own output, and leave the previous snapshots in place.

`docs/index-cache.md` is the restore procedure, and the number that
makes it worth having: a restored snapshot resolves to `refresh` and
folds in the incremental listens since — minutes, against the 3–23 h a
rebuild was estimating.

## A green release pipeline can ship an empty changelog (2026-08-18)

`conventional-changelog-conventionalcommits@10` is silently incompatible
with the writer `@semantic-release/release-notes-generator@14` depends on
(`conventional-changelog-writer@^8`). Every release note renders as a bare
`## 0.0.1 (date)` heading with **no sections and no commits under it**, no
step fails, and the release ships with an empty body.

It is pinned to `9` in `.gitea/workflows/release.yml` and in
`make release-dry`, which must stay identical. **Check the rendered notes,
never the exit code** — this is invisible to every tick in the pipeline.

## semantic-release needs push rights to the branch even when it never pushes to it (2026-08-18)

Core runs `git push --dry-run HEAD:<branch>` as a permission check, before
and independently of any plugin. With `@semantic-release/git` removed
nothing ever pushes to `main`, and the check still runs.

Two things this looked like and was not:

- **Not branch protection.** A `--dry-run` push does not reach the
  pre-receive hook: pushing one to protected `main` with a write-scoped
  token succeeds. So `main`'s `enable_push: false` is not what fails here.
- **A flat `403 Forbidden`, not Gitea's protection message.** That is the
  tell. `PACKAGE_TOKEN` had package-write and repo-*read* — enough to
  clone a private repo, so every other workflow was fine — and needed
  `write:repository`.

## A tag-triggered workflow runs the workflow file at the *tagged* commit (2026-08-18)

Not the one on `main`. Moving `v0.0.0` onto a pre-merge commit ran that
commit's version of `homebrew-formula.yml`, which predated the `v0.0.0`
skip guard added in the same plan, and it pushed a `0.0.0` formula to the
public tap.

A guard added today does not protect a tag that points at yesterday. When
re-pointing a tag, check what the workflows looked like *there*.

## A tag reader looks at exactly one spelling of "total" (measured 2026-08-18)

Writing #16's totals means matching the reader, which is
`dhowden/tag`, and it is narrower than the specs are:

- **Vorbis (FLAC, OGG): `TRACKTOTAL` and `DISCTOTAL` only.**
  `vorbis.go`'s `Track()` reads `tracknumber` and `tracktotal` and
  nothing else, so `TOTALTRACKS` — which several taggers write and
  which xiph lists — and a `1/12` packed into `TRACKNUMBER` both read
  back as *no total*. They write successfully. Nothing errors.
- **ID3v2 (MP3): `TRCK`/`TPOS` as `n/N`**, via `parseXofN`. That is one
  frame carrying two facts, which is why `applyPositionFrame` reads the
  existing frame before writing either half.
- **WAV: nothing at all.** There is no RIFF reader in the module, so a
  WAV's `id3 ` chunk is invisible to `metadata.ExtractTags` — every
  field, not just the totals. Filed as #104.

The general shape, and the reason this is written down: a tag written
under a name the reader does not look at is indistinguishable from one
never written. So the tests assert the round trip through
`metadata.ExtractTags` — the reader the *scan* uses — rather than
through the bytes the writer produced.

## The published catalog artifact predates `total_tracks` (measured 2026-08-18)

```
$ curl -sSI .../generic/yellowjacket-core-index/latest/core-index.db.zst
last-modified: Mon, 10 Aug 2026 04:38:16 GMT
content-length: 75417037

$ sqlite3 core-index.db \
    "SELECT COUNT(*) FROM pragma_table_info('explore_index') WHERE name='total_tracks';"
0
$ sqlite3 core-index.db "SELECT COUNT(*) FROM explore_index;"
1079667
```

The column landed in the schema on 2026-08-16; the artifact is from
08-10, and `index-artifact.yml` is a weekly cron, not a push trigger.
So `completenessAnswer()`'s catalog fallback answers 0 for **every**
user today — the machinery is correct and `artifactHasTotals()` is
doing precisely its job, there is just no data behind it. Same position
the credit tables are in; both ride on the next publish (#88).

The general point, which is why this is written down rather than just
fixed: **a probe that makes a column optional also makes its absence
silent.** `artifactHasTotals` and `artifactHasCredits` are both correct
and both mean a feature can ship, pass every test, and produce nothing
for anybody without a single failure anywhere. Checking the *published
file* is one query and is not implied by any tick in CI.

## "Do I own this" has two answers in the schema, and one of them is a flag (2026-08-19)

Decided while doing #38, and it outlives it because every future
catalog surface has to pick one.

`explore_index` carries both `in_library` and `local_artist_id` /
`local_release_group_id` / `local_recording_id`. They are written by
the same pass (`collectLibraryEntities`), so on a healthy database they
agree, and the code read them as an OR — `inLibrary || localId > 0` —
at eight call sites.

They are not the same kind of thing:

- **`local_*_id` is a fact with an owner.** Every query that sets one
  joins `audio_files`, and `pruneStaleLocalCrossReferences` clears it
  with an existence test that is a file test in all three cases. It is
  the same rule `explore-album-details`'s `filePaths` implements, one
  layer down and computed once per scan.
- **`in_library` is a ratchet.** `upsertBatch` raises it with
  `MAX(in_library, excluded.in_library)` and the prune is the only
  thing that lowers it — gated on the local id being non-null, so a row
  holding the flag *without* an id is a fixed point nothing can clear.
  Filed as #118; it still drives search scoring, the popularity-floor
  bypass and two Explore shelves, so routing the UI around it was not a
  fix.

What made the choice concrete rather than theoretical: on
`explore-artist-details` the *same card* used both. The context menu
gated Play on `localId > 0`; the badge used `inLibrary`. An album with
the flag and no local row drew a green tick saying it was in your
library, offered no Play, and — the request item being gated on *not*
owned — offered no way to ask for it either.

The rejected alternative is worth keeping: batching a real file lookup
per screenful, the way `credit-store` coalesces. It would have answered
for **recordings** (`GetFilePathsByRecordingMBIDs`) and most of the
cards on these surfaces are release groups, so it would have made track
rows strong, left album cards exactly where they were, and cost a new
store. The batch that *was* worth adding is a different question —
`GetAlbumsCompleteness`, "how much of this album is here", which no
per-card flag can answer at all.

The general point: **two columns that agree today are not one column.**
Which of them a new surface reads should be decided by which one has
something that can un-set it.

## The queue panel was a column that could not afford to be one (measured 2026-08-19)

Plan 018, issue #24. Measured against the running app (`make
dev-headless SEED=default`, Chromium) on Playlists, sweeping the
viewport with the queue open and closed. Main panel width, and how much
of the page header survived:

| viewport | sidebar | main (queue open) | actions clipped |
|---|---|---|---|
| 1280×800 | 200 | 759 | — |
| 1000×700 | 200 | 479 | 2 of 3 |
| **900×600** | 200 | **379** | all three |
| 800×600 | 56 | 423 | all three |
| 390×780 | — | **69** | all three |
| 320×600 | — | **0** | all three |
| 800×600 | 56 | 744 *(closed)* | New Smart Playlist, 158/162px |

Five things came out of it that the issue did not say.

- **The header clips at the enforced minimum with the queue closed.**
  800×600 is the only size this app promises, and "New Smart Playlist"
  loses 4px of its 162 there. The queue makes it dramatic; it is not
  the cause.
- **900×600 is worse than 800×600.** `AUTO_COLLAPSE_VIEWPORT` collapses
  the sidebar *below* 900, so the main panel is 843px at 899 and 700px
  at 900. **The worst desktop case is the top of the Compact band, not
  the enforced floor** — so every viewport list that stopped at "the
  minimum" was missing its own worst case. `layout-overflow.spec.ts`
  carries 900 now.
- **At 320px the main panel was 0px.** The panel is `flex-shrink: 0` in
  the flow of `.content-area`, so an open queue is paid for by the
  content rather than covering it. Not degraded — gone. That is the
  measurement #55 wanted and did not have.
- **Only Playlists overflows.** All ten primary views swept at 900×600
  and 390×780; every other header reports `scrollWidth ==
  clientWidth`, and Albums at 390 renders title, count and sort legibly
  (checked on a screenshot, not just the number). So #69 is one view's
  action set — three text buttons totalling 390px — and not a systemic
  header failure.
- **Both reasons in `MinWidth`'s comment had expired.** The subtitle is
  `display: none` from 899 down, and the sidebar host is
  `overflow-y: auto` (at 600×460, `scrollHeight` 434 against a 332px
  client, Settings reachable after scrolling). The floor is right; its
  stated defence was two mechanisms that can no longer happen, which is
  worse than either answer because nobody can argue with it.

**A correction worth keeping, because it nearly went in the plan.** My
first probe for the sidebar's scroller searched
`shadowRoot.querySelectorAll('*')` and reported "no scroller — items
are unreachable", which reads exactly like a live Settings-unreachable
bug. The scroller is the **host**, and a host is not inside its own
shadow root. CLAUDE.md was right and the probe was wrong.

**And one claim in the plan's first draft was too strong**: that the
overlay "removes the desktop half of #69". After phase 2, at 900×600,
open and closed are now *identical* (main 700, one action clipped)
where open used to be main 379 with all three clipped. The queue's
contribution is gone; the header's own overflow remains and is still a
live defect at a supported size.

### The mode cannot be a media query

The panel is drag-resizable 200–500px and persisted, so a viewport
breakpoint assumes the default 320 and is wrong by up to 180px for a
user who widened it — in the direction that hurts, since a wider queue
is exactly when the content can least afford it. It is computed from
`.content-area`'s width instead (which already accounts for the
sidebar's collapse), and the component test that matters widens the
panel at a *fixed* parent width and asserts the flip.

The floor (480) is a judgement, and the measurement is why: there is no
cliff. The track list rescales its columns continuously — 213px down to
124px between main widths of 900 and 544, `rowOverflow=0` at every step
— and the album grid steps 3 columns to 2 somewhere between 564 and 644
without breaking. So 480 is anchored at both ends instead: it keeps the
default 1100px window inline, and puts every measured-broken case on
the overlay side.

The scrim is perceptible but subtle on a dark ramp, which is worth
knowing before someone "fixes" it as broken: sampled from screenshots at
900×600, the main panel's background goes 33,37,41 → 18,20,23 and a
row's text 242 → 133. It covers the content area only — not the sidebar
or the transport — because the queue is not modal.

## No test tier can see a `hover:` media query (measured 2026-08-19)

Gating an affordance on `(hover: hover) and (pointer: fine)` — #68's fix
for the play button that flashed on a long-press — is invisible to both
browser tiers, in *different* ways, and neither of them fails.

- **`make ui-test`**: CDP's `Emulation.setEmulatedMedia` with a `hover`
  feature does not reach the tier's iframe. The call succeeds and
  `matchMedia('(hover: hover)')` still answers `true` afterwards. So
  there is no way to render a component as a phone would and read the
  computed style.
- **`make e2e`**: both projects are desktop (`Desktop Chrome`,
  `Desktop Safari`), and the phone specs reach phone *width* with
  `setViewportSize`, which changes no media feature but `width`. So the
  phone specs run with `hover: hover` and the gate is never exercised.

What does work, and what the fix was verified with, is a second browser
context under a device descriptor: `chromium.newContext(devices['Pixel
5'])` reports `hover=false pointer:fine=false` and the button computes
`display: none`, against `flex` at 1440px. That is a one-off script, not
a spec — `isMobile` is Chromium-only, so it cannot become an e2e project
without losing the WebKit half.

`hover-affordance.test.ts` therefore asserts the *parsed stylesheet* —
that the reveal rule sits inside the media query — which catches the
regression that actually threatens it: someone hoisting the rule back out
as a tidy-up, a change nothing on a desktop renders differently.

Related: a width-gated decision **is** testable at both tiers, which is
why #61's phone mini player is a `matchMedia` stub in the component test
and needs nothing special.

## A default that is an *absent* key survives an existing seed (2026-08-19)

The skill warns that a seed freezes every default it has already
persisted, so changing one in `backend/config` is invisible against an
existing `YJ_HOME` while CI, which seeds by running the app, tests the
new one. That warning is about defaults stored as *values*.

#25's Autotag-hidden default is stored as the **absence of a key**:
`GeneralConfig.ViewVisibility` is a map, an id it does not mention takes
`backend/config.Views`' answer, and only what the user changed is ever
written. So a seed built before the feature existed showed the new
default immediately — verified against `.dev/seeds/default.tar`, whose
`config.toml` has no `[General.ViewVisibility]` table at all, and whose
sidebar came up without Autotag on the first launch of the new binary.
After toggling it on and off again the file carries exactly one line,
`autotag = false`.

The general form is worth keeping: **a default expressed as a zero value
needs a re-seed to observe; a default expressed as an absent key does
not**, and it needs no migration for existing installs either. It is the
same property that makes removing a view later free (an unknown key is
dropped on load), which is what the `#25 → #27` ordering on #73 rests on.

## A spec cannot assume a destination has a nav item (2026-08-19)

Since #25, `getByTestId('nav-<view>')` is not a reliable way to reach a
view: Autotag is hidden by default and Downloads is absent without a
download client, so four existing specs failed on a 30 s timeout waiting
for a locator that will never resolve. `navigateTo(page, view)` in
`e2e/support/fixtures.ts` dispatches the app's own `navigate` event
instead, which is what every nav item, card and detail view dispatches —
so it is the mechanism and not a test-only door.

Use the nav item when the *nav* is the subject, and `navigateTo` when
the view is.

## `config-section .header` is ambiguous once a job exists (2026-08-19)

#27 embeds `<job-panel>` inside four Settings sections, and a panel with
any job in it also mounts a `job-details-drawer` — whose own header
carries the class `.header`. So `config-page config-section .header`,
which `settings-reach.spec.ts` had used since plan 007, resolves to two
elements and fails Playwright's strict mode the moment a scan has run.

Two things follow. A spec asserting on a section's *disclosure* should
locate it by role and name (`getByRole('button', {name: heading})`) or
scope per section and take `.first()`, not by that class. And this is a
worked example of the more general trap: a class name is not a
selector's contract, and a component that embeds another inherits its
class names into every ancestor query.

It also only appears in a suite that has *done* something — the
sections are empty on a fresh app, so this cannot be reproduced by
opening Settings and looking.

**And it appears on the second engine, not the first.** CI runs
chromium then webkit against **one app**, so a spec that scans in the
chromium pass leaves a finished job the webkit pass then trips over.
Three specs used that selector; two failed locally and the third
(`failure-voice.spec.ts`) was green on chromium and red on webkit in
the same run. Reproducing it locally is running the suite twice against
one `make dev-headless` — which is worth doing for any change that
leaves state behind, since it is the only place a cross-engine order
dependency shows up.

## `scrollWidth` counts the left padding and not the right (measured 2026-08-20)

The obvious predicate for "does this flex row fit" is
`el.scrollWidth <= el.clientWidth`, and on a box with symmetric gutters
it **under-reports by one gutter**. `scrollWidth` is the extent of the
scrollable content area, which includes `padding-left` and excludes
`padding-right`; `clientWidth` includes both. So a child may end up to
`padding-right` past where content is allowed to go while the box
reports a perfect fit.

Measured on the top bar (`padding: 0 2em`) at 700x600 with a long-titled
scan staged: `clientWidth 700`, `scrollWidth 700` — and
`job-indicator`'s right edge at 700 against a content edge of 668, i.e.
sitting in the whole right gutter. `#143`'s first fix passed its own
measurement and left the indicator visibly jammed against the window
edge.

The predicate `services/top-bar-fit.ts` uses instead is the one its
spec asserts: no in-flow child's rect outside the parent's *content*
box, both edges, with half a pixel of slack for fractional flex widths.

This is the same family as #69's title trap — the measurement easiest to
reach for is the one that cannot see the failure — and it is worth
knowing before writing the next one of these: **the fit test and the
assertion that proves it should be the same test.** It was found only
because `top-bar-fit.spec.ts` measures per child rather than asserting
on the container, which is exactly why #69 needed
`header-action-overflow.spec.ts`.

## The top bar's overflow is 11px idle and 262px while working (measured 2026-08-20)

#143 was filed as "11px at 600x600" and re-measured as 171. Both are the
same defect seen with different jobs running: `job-indicator` is
`hidden` when idle, ~144px wide showing "Scanning Music", and **235px**
showing a real library's scan title ("Scanning Music from the external
drive"), because the label is capped at 12rem and gets there.

Swept against the running app with that job staged, `header.top-bar`
client vs scroll:

| width | idle | with the long-titled scan |
|---|---|---|
| 320, 390, 599 | fits | fits (the phone rules drop the filter and the label) |
| 600 | 611 | **862** |
| 700 | fits | 862 |
| 800 | fits | 862 |
| 899 | fits | 899 (fits) |
| 900 | fits | 946 |
| 1100, 1440 | fits | fits |

Two things worth keeping. The band is **600–610 idle and 600–900 while
working**, so "a narrow corner" and "the header is crowded from 900
down" are both true and the difference is entirely what is in flight —
which is the case a seeded, settled app can never show you. And 899
fits while 900 does not, because `nav-history` appears at 900: the worst
width for the header is not the narrowest one, the same way 900 rather
than 800 is the worst width for the content area.

Staging it is `/__test/emit` with a `JobsChanged` snapshot; a job with
`state: "running"` never completes, so it stays up until an empty
snapshot is emitted, which is what makes an idle re-measurement look
like the fix not working.

## Two repaint mechanisms, and neither is pinned alone (measured 2026-08-20)

`CLAUDE.md` already states the rule — *a virtualized list repaints only
when you tell it to, and the accidental way you were telling it may be
the thing you are about to delete* — found in `artists-view` and
`genres-view`. `queue-panel` is a second instance with numbers, and the
numbers are the part worth keeping.

It repaints its rows **two** ways:

- `onSelectionChanged()` calls `virtualizer.requestUpdate()`, which is
  the intended one and the one `track-list` has always had;
- `.keyFunction=${(track) => track.id}` is a **per-render arrow**, so it
  is a changed property on every host update and repaints the rows by
  itself.

Removing *either* alone changes nothing observable. That is why #43
could not be settled by reading the code: the hypothesis in its Findings
(the repaint is missing) was checkable, false, and would have looked
identical either way.

Removing **both** does not break selection either — it delays it. Time
from click to `aria-selected`, three clicks each:

| build | ms to highlight |
|---|---|
| healthy | 5, 16, 17 |
| both mechanisms removed | 134, 3,866, 5,816 |

The highlight arrives on whatever unrelated render happens next (the
player's 1 Hz position report is the usual candidate). **Four seconds is
indistinguishable from broken to a user, and invisible to a spec** —
`expect.poll`'s default 5 s timeout passes the degraded build on every
assertion. `queue-selection.spec.ts` bounds its selection assertions at
500 ms for that reason, which is ~30x the healthy case and an order of
magnitude under the degraded one.

The general form, for the next spec about anything push-driven: **a poll
generous enough to be stable is generous enough to miss a latency
regression entirely.** If "late" is a failure mode worth having, the
timeout has to say so.

## A hit-scan says how much of a row is not selectable (measured 2026-08-20)

`explore-link` stops the click's propagation on purpose — "the row must
not also treat it as a selection" — so a click on a track, album or
artist *name* navigates and selects nothing. That is app-wide and
deliberate, and the useful question about any given list is how much of
its row it costs.

Asking `elementFromPoint` what is under each x across a row, at three
heights:

| list | link coverage |
|---|---|
| queue panel | 12% |
| track list | 21% |

This killed a fix in progress. #43 reads as "selection is broken in the
queue panel, and fine in the track list", the obvious mechanism is that
the queue's narrow rows are mostly name, and it is **wrong**: the panel
is *less* link-covered than the list it is being compared against. The
scan takes a minute and is worth running before demoting anybody's links
— `explore-album-details`'s tracklist (number / title / artist /
duration) is the one that plausibly *is* mostly link, and is the one
#5 is about to add selection to.

## A layout is still moving when a guard says it has arrived (measured 2026-08-20)

`album-dropdown.spec.ts` failed with `Expected 80, Received 10` twice
over two sessions, and #133 already strengthened its guard from
"scrollable at all" to "has at least the range the assertion needs".
That was necessary and could not be sufficient, and the reason is
structural rather than a matter of thresholds: **a guard and the write
it guards are separate CDP round trips**, so the page is free to
re-lay-out between them. Polling harder cannot close a window between
two moments; only removing the window can.

Measured directly, sampling `scrollHeight - clientHeight` on
`.grid-scroll-container` every frame across a 1440x900 → 900x600 resize,
three runs:

| t (ms) | range |
|---|---|
| 0 | 0 |
| 1 | **88** |
| 8–14 | 330 (settled) |

88 satisfies a guard asking for 80 and is not the settled value, so the
guard can pass while the grid is one layout pass from done. Under
full-suite load the transient is worse — the observed failure had 10 —
which is why it shows up on the second run of a suite and not in ten
consecutive runs of the file alone (0/10 both before and after the fix).

The shape to write instead: **one page-side call that performs the
action and returns what it observes**, with `expect.poll` retrying
*that*. `scrollTo()` sets `scrollTop` and returns `scrollTop`, so the
assertion is about what the grid did rather than about what it was
ready to do. `layout-overflow.spec.ts`'s sidebar probe already had the
fused half and was missing the retry; it has both now.

Worth generalising: a spec that resizes and then measures is asserting
about a moving target for the next dozen frames. Fuse, then poll.

## "The first N tracks" is not a way to ask for an ordinary one (2026-08-20)

`queue-selection.spec.ts` staged its queue from the first few rows of
`library.Library.GetTracks(0)` and clicked a track *name*, which
`explore-link` routes to that track's **album** page. Four tracks in the
fixture library have no album at all — `01 Tone A`, `02 Tone B`,
`Title Only`, `no-tags-at-all` — and a name with nothing to route to
renders as **plain text**, not as a link.

Two things follow, and the second is the sharper one.

**The order is the scan's.** `GetTracks` returns `audio_files.id` order,
i.e. the order the scan inserted rows, which depends on concurrency and
directory traversal. Locally the first eight are all from two proper
albums, so the spec passed twice over; CI rebuilds its seed with a real
scan, got a different eight, and failed on both engines. This is the
same family as "a seed freezes every default it has already persisted" —
the fixture library is not a list, it is a *set* with an incidental
order, and no spec should depend on that order.

**A loose locator hid it.** The row was located with
`.locator('.explore-link').first()`, and a row has two — the title and
the artist. When the title is plain text, `first()` silently resolves to
the **artist** link, so the click went somewhere real and the assertion
was about a destination the test had not exercised. `.track-title
.explore-link` is the locator that says which one it means; the loose
one turned a fixture problem into a mystery.

The general rule for this repo's fixture library: it is deliberately
full of edge cases (untagged, unicode, duplicates, extremes), so a spec
that wants an *ordinary* track has to **say so** — filter on the
property it depends on rather than slicing.
## A nested rule starting with an element name is dropped on the phone (2026-08-20)

`CLAUDE.md` records that the device renders in **Chrome 113**, which
does not have relaxed CSS nesting (Chrome 120). The consequence is
sharper than "some syntax is unavailable": a nested rule whose selector
begins with a bare identifier is not a parse error you would notice, it
is **silently dropped**.

Three such rules were live in `frontend/index.css`, all inside
`.bottom-bar`, and all therefore dead on the phone and only on the
phone:

```css
.bottom-bar {
    #track-info { p { … } }   /* the metadata's ellipsis */
    now-playing { overflow: hidden; }
    audio-player { margin: 0.5em 1em; }
}
```

The first is the interesting one: it is the *ellipsis* on the bottom
bar's track title and artist, so on the device that text has never
truncated — the same class of fault as `now-playing`'s marquee, whose
`text-overflow` sat on the wrong box and had never produced an ellipsis
in any mode. Both are invisible to every assertion and visible in a
screenshot.

`& p`, `& now-playing`, `& audio-player` are valid in both, so the fix
is one character per rule. What is worth keeping is the rule of thumb:
**inside a nested block, always write `&`** — and note that a rule
inside `@media` is *not* nested, so `@media … { bottom-nav { … } }`
elsewhere in that file is fine and needs nothing.

`make css-check` does not catch this (it looks for backticks that end a
tagged template early). Filed as an issue: the check is the natural
place for it, being the same shape of trap — a silent, phone-only,
screenshot-only failure.

## Centring a bar costs the control in the middle of it (measured 2026-08-20)

#23 asks for the transport centred in the bottom bar. The obvious
implementation — make the outer two grid tracks the same width, so the
middle is centred by construction — is right, and the first cut of it
was a regression, because "the same width" was taken to mean *the
metadata's* width on both sides.

Measured at 800px, with the seek bar's own track:

| layout | seek track | transport column |
|---|---|---|
| `320px 1fr auto` (before) | 257 | 407 |
| both sides `--now-playing-width` | **61** | 179 |
| both sides `min(--now-playing-width, 25%)` | 246 | 364 |

At 200% text the middle row is worse still: 130 before, **0** with the
uncapped sides. Centring is free at 1440 and expensive at 800, so a
change checked only at a comfortable width looks perfect.

The general form: **a symmetric layout reserves space on the side that
does not need it.** The right-hand group here is ~141px (volume plus
the queue button) and was being given 320 to keep the arithmetic
symmetric. Cap the side tracks against the *bar*, not against their
content, and the middle gets the difference.

The spec that pins this is two assertions, not one, and that split is
deliberate: an uncapped build is *perfectly centred* and fails only the
seek-bar width, so a spec asserting centring alone would have passed
the regression.

## The phone's way into Now Playing was under the artwork (measured 2026-08-20)

`phone-shell.spec.ts`'s "opens the full-screen now playing" failed in CI
on both engines, three times across two branches that could not have
caused it, and passed on re-run each time. It was filed as a flake
(#150). It is not one: **it depends on which track is playing.**

`.expand` — the phone's only route into `<now-playing-view>` — is
`position: absolute; inset: 0` inside `.cover-art-wrapper`, and
`.cover-art` is a **later sibling**. Both have `z-index: auto`, so they
tie on paint order and the later one wins. With an `<img>` that costs
nothing; with no artwork the placeholder `wa-icon` renders and takes
every click aimed at the button underneath it.

Measured at 390px with `elementFromPoint` at the button's centre:

| playing track | hit test |
|---|---|
| has artwork | `button.expand` |
| no artwork | **`wa-icon`** |
| no artwork, with `z-index: 1` | `button.expand` |

So on a phone, the only way into the full-screen player stopped working
whenever the current song had no cover — and this has nothing to do with
the fixture: any library has untagged files.

Three things worth keeping.

**"Flaky in CI" was the wrong diagnosis and it cost three cycles.** The
spec starts the *first* row of the track list, so which track it plays
is the order the scan inserted rows in — the same root cause as #156,
one spec over. A test whose subject is a hit test has to *choose* the
case that breaks it.

**The first two hypotheses were both wrong, and both were plausible.**
A custom element's upgrade replacing its own contents, and the cover
preview's `mouseenter` opening a popup under the pointer. Neither
survived contact with `elementFromPoint`, which took a minute and would
have saved the other two cycles.

**And the spec that pins it needs the 90-second track**, because a
2-second one finishes before the assertions run — the trap
`fixtures.ts` already documents. Note the filter that does *not* work:
`library.Track.CoverArt` is empty for all 31 fixture rows, so "the
first track with no cover art" selects nothing in particular. The
placeholder's presence is asserted instead, which is the property the
test actually depends on.

## An activity recreation kills the process, deterministically (measured 2026-08-20)

#52's report was "sometimes crashes or restarts when reopened after
running in the background". Measured on a real device, the *fault* is
not intermittent at all — only its trigger is.

Device: Light Phone III (TLP301), Android 14 / SDK 34, arm64-v8a,
WebView **Chrome 113** at 424x439 CSS px. Debug build
(`app.yellowjacket.dev`), installed beside the released `v0.3.1` with
`install -r`.

**Conditional on the activity actually being recreated in a live
process, the process died 8 times out of 8** — 3 by hand, then 5/5 in a
scripted loop. The runs where it survived were runs where no recreation
happened (one `Wails bridge initialized` in the log rather than two), so
they are inconclusive rather than passes; a harness that does not check
for the second init reports those as green and reads as flakiness.
After the fix: 5/5 recreations survived, plus 6 background/foreground
cycles and 3 interleaved recreations on one pid.

The mechanism is three log lines:

```
12:47:56.159 I/WailsBridge(22956): Wails bridge initialized
12:48:38.898 I/WailsBridge(22956): Wails bridge initialized   <- same pid
12:48:39.357 I/ActivityManager: Process app.yellowjacket.dev (pid 22956) has died: fg  TOP
```

`nativeInit` runs `go mainFunc()` on every activity creation; the second
`main()` reaches `app.Run()`, which refuses because `a.starting` is
still true behind Android's `select{}`, and `os.Exit(1)` takes the whole
process — including the healthy first app — with it.

Four things worth keeping:

- **`has died: fg  TOP` is not a memory kill.** The system does not
  reclaim the foreground process. This reads as "the OS killed us",
  which is the wrong hypothesis and the reason the issue sat unverified.
- **There is no crash record of any kind**: `logcat -b crash` empty, no
  `AndroidRuntime`, no `libc: Fatal signal`, no tombstone. `os.Exit` is
  not a crash. The one line that named the fault —
  `slog.Error("application error", "err", ...)`, carrying
  `"application is running or a previous run has failed"` — went to
  `/dev/null`. That is #160.
- **"Don't keep activities" does not work on this device.**
  `settings put global always_finish_activities 1` reads back as `1`,
  `am set-always-finish-activities` does not exist on this build, and
  the activity was never finished on backgrounding. The report's own
  suggested lever is a dead end here. What *does* work, deterministically
  and in one line, is a configuration change the manifest does not
  declare: `adb shell settings put system font_scale 1.15`
  (`AndroidManifest.xml` declares `orientation|screenSize|
  keyboardHidden|uiMode`, so none of those are triggers).
- **Surviving is only half the property.** The recreated WebView has to
  still be wired to the running app, which was verified by hooking
  `window._wails.dispatchWailsEvent` and backgrounding/foregrounding:
  `["IndexStatusChanged","JobsChanged","JobsChanged",
  "android:storageAccess"]`. The tempting Java-side fix — making
  `WailsBridge.initialized` static — passes the pid check and fails
  this one, because `nativeInit` is also what re-points the JNI
  reference at the new bridge.

## The Taskfile's device tasks uninstall the released app (2026-08-20)

`android:run:device` builds the **debug** variant
(`applicationIdSuffix ".dev"`) and then runs
`adb uninstall {{.APP_ID}}`, where `APP_ID` defaults to
`app.yellowjacket` — the **release** id. So it deletes the user's
installed app and its library, installs a different package, and then
fails to launch the one it removed. `deploy-device` carries the same
uninstall. Filed as #159; `android-tier.md` had been recommending
`run:device` as the way onto a device.

This is the hazard that file already names — "The identity is declared
twice ... **Nothing enforces that they agree**" — reached by a second
route: the two ids differ not because someone edited one, but because
the debug buildType suffixes it.

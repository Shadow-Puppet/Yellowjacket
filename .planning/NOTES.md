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

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

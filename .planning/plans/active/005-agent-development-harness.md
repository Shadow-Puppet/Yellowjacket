# 005 — Agent development harness

**Status:** complete — all seven phases shipped
**Branch:** —
**Created:** 2026-08-10
**Follows:** 004-wanted-list

## Progress

| Phase | State | Notes |
|---|---|---|
| 1 — Reproducible fixtures | **done** | `cmd/gentestdata`, `make testdata`, `internal/testfixtures` |
| 2 — Headless launch | **done** | `scripts/dev-headless.sh`, `dev-stop.sh`, `seed-sandbox.sh` |
| 3 — Driving and seeing | **done** | event bridge, `data-testid`/aria pass, `backend/testctl`, `e2e/` smoke suite |
| 4 — Component coverage | **done** | Vitest 4 browser mode, 313 tests, `make ui-test`; `make bindings-check` |
| 5 — `events.Emit` wrapper | **done** | `backend/events/emit.go`, `Recorder`, 35 sites converted, service tests in `queue`/`config`/`playlist` |
| 6 — pi affordances | **done** | `.pi/skills/yellowjacket-dev/`, `.pi/prompts/e2e.md`, `.pi/journal.md`, `make skill-check` |
| 7 — CI that gates | **done** | `.gitea/workflows/ci.yml`, two jobs, both prototyped in a container first |

**Verified end to end after phase 7:** both jobs were built as shell
scripts and run to green in a bare `ubuntu:24.04` container before any
YAML existed, then the steps were transcribed *back out of the
workflow* and re-run in the same container to prove the transcription —
job 1 (lint × 3, test × 3, `tsc --noEmit`, 313 ui-tests,
`bindings-check`, `skill-check`) and job 2 (fixtures, seed,
`dev-headless`, 19/19 chromium, 19/19 **webkit**). Push-and-see was
not an acceptable loop here: a Gitea Actions run that never starts
looks identical to one that passed.

It found a real bug on day one. **`make lint` was linting three
configurations that nothing builds** — all three passes omitted
`webkit2_41`, so wails resolved `webkit2gtk-4.0`, which Arch still
ships and Ubuntu 24.04 dropped. The `dev` pass is the one that breaks,
because wails' own `app_dev.go` is `dev`-tagged and drags in the 4.0
assetserver that the other two passes never compile. The tag sets now
match `make test` exactly. "Lint passes" and "lint compiled what we
ship" were different claims, and only a second distro could tell them
apart.

Two decisions were settled by measurement rather than argument. The
**audio sink** is a four-line ALSA `null` PCM, no daemon: `InitSpeaker`
succeeds in 36 ms and the elapsed clock advances, because ALSA's null
plugin advances its pointer on a timer. The **explore artifact** is
stubbed at a dead address as `seed-sandbox.sh` already does — and
setting it for the *app* run too, which `dev-headless.sh` never did,
turned out to be worth 8x on suite wall clock. **Playwright's WebKit
is a required step**, not an advisory one: it had never been run
anywhere, so one throwaway container run turned a coin flip into a
decision (19/19, +11 s), and nothing in `e2e/` compares pixels, so a
WebKit failure is an engine bug rather than baseline noise.

**Verified end to end after phase 6:** all four tiers were re-run
green *before* anything was written — `make ui-test` (313),
`make lint` (0 issues × 3 configurations), `make test` (3 passes),
`make e2e` (19/19 against a seeded `dev-headless` app) — so the skill
documents commands that were observed working, not remembered.
`make skill-check` then verified the 25 make targets the skill cites
all exist, and was itself verified to fail on a missing one. The
remaining check is the one no tooling can do: an agent following
`.pi/skills/yellowjacket-dev/` cold on a real task, which should
happen before phase 7 encodes the same commands into CI.

**That cold run has now happened.** An agent that did not write the
skill brought the app up from a wiped `.dev/` and no fixture library,
drove an undocumented flow (queue panel + shuffle, asserted on
`QueueModeChanged`, confirmed against `queue.Queue.GetState`) and
stopped it, in about a minute with no dead ends; all four tiers then
re-ran green from that cold state. It found one real config bug —
`outputDir` in `.playwright/cli.config.json` resolves against the
shell's cwd, not the config file's directory, so every snapshot was
landing one level *above* the repo where a stale copy from the
previous session answered instead — and four missing or wrong steps
(`sandbox-seed` already runs `testdata`; `make ui-setup` /
`make e2e-setup` are undocumented once-per-clone prerequisites;
`snapshot` prints a path, not a tree; `make dev-stop` leaves the
browser session open). All fixed in place, with the detail in
`.planning/NOTES.md`.

**Verified end to end after phase 5:** all 35 `runtime.EventsEmit`
call sites across 14 files now route through `events.Emit`, and
`TestNoDirectRuntimeEmits` fails the build if a new one appears. Four
packages had each hand-rolled their own guard against the same
`log.Fatalf` (`library.emit`, `download.emit`, `autotagservice.
emitEvent` with its own `ctxReady` field, `playlist.emitEvent`) and
nine more sites guarded on `ctx != nil`, which does not actually
prevent it; all of that collapsed into one place. 16 new tests assert
what the *frontend receives* — queue mode/index/delta payloads,
config theme and shortcut snapshots, playlist create/add/delete —
none of which was reachable before. `make lint` (3 configurations),
`make test` (3 passes), `make bindings-check`, `make ui-test` and
`make e2e` (19/19 against the seeded headless app) all green.

**Verified end to end after phase 4:** `make ui-test` runs 313 tests in
a real Chromium in ~2 s with no app, no backend and no display — 196
covering all 13 stores plus the keyboard shortcut service, 117 covering
components (transport, sidebar, library filter, status indicator,
track-info, now-playing, queue panel) including a smoke mount of all 46
custom elements against an empty backend. `make ui-visual` adds six
`toMatchScreenshot` baselines. `make bindings-check` regenerates
`frontend/wailsjs` in ~1.5 s and was verified to fail on a renamed
bound method. `tsc --noEmit`, `make lint` (all three configurations)
and `make e2e` (19/19) all stayed green, and the one frontend fix the
tier surfaced was confirmed in the running app by screenshot.

**Verified end to end after phase 3:** `make e2e` runs 19 Playwright
specs against the seeded app — harness self-tests, library views
(31 fixture tracks, unicode, sidebar navigation), playback (play,
pause, elapsed time, volume round-trip), queue (population, shuffle
state) and the control surface (snapshot → mutate → restore, forced
event, SQL, input validation). All 19 pass; `make lint` is at 0 issues
across all three build configurations and `make test` is green.

**Verified end to end after phase 2:** `make sandbox-seed NAME=default`
built a seed by driving the real `AddLibrary` binding and waiting for
the real scan to reach 31 tracks; `make dev-headless SEED=default`
restored it and landed *in* the app with no first-run wizard;
`playwright-cli` clicked through to Artists and screenshotted six real
artists with generated cover art, unicode names and the long-artist
truncation case; `LoadFile` + `Play` produced audible playback with the
transport bar at 00:04.

One re-sequencing against the plan below: `sandbox-seed` is described
under phase 1 but shipped at the end of phase 2, because seeding *by
running the app* makes it a consumer of the launcher.

One bug found by the fixtures, not yet fixed: **WAV tags are
write-only.** `backend/tagwriter` writes them into a RIFF `id3 ` chunk;
`backend/metadata` reads through `dhowden/tag`, which has no RIFF
parser, so every WAV scans in untitled. Pinned by
`TestWAVTagsAreNotReadableYet`.

## Problem

A coding agent can develop the Go packages of this repo competently and
cannot develop the *application* at all. It can read 66k lines of
backend, run 31k lines of tests, and lint two build configurations. It
cannot start the app, see a window, click anything, or find out whether
a change it made to a Lit component rendered.

The gap is not "we lack tests". It is that every path to running
YellowJacket ends in a blocking GTK window:

| Entry point | Behaviour |
|---|---|
| `make dev` | launches a WebKit window, blocks the terminal forever |
| `make sandbox <n>` | same, plus an interactive name argument |
| `make fresh-install` | same, and lands on the first-run wizard every time |

So 265 bound methods across 11 services, 46 backend events, 33 Lit
component directories, 13 reactive stores and a 357-line keyboard
shortcut service have exactly one form of verification available to an
agent: `tsc --noEmit`.

Three secondary facts make it worse. `test_data/music_library_test/` is
referenced by three test files, is in `.gitignore`, is not on disk, and
has no generator — so the audio path and `YELLOWJACKET_INTEGRATION=1`
are unreachable from a clean clone. No Gitea workflow runs `make test`
or `make lint`; quality gating exists only in `lefthook.yml`, which is
local and `--no-verify`-skippable. And there is no `.pi/` directory, so
none of the awkward invocations (`-tags "webkit2_41 indexbuild"`,
sandbox lifecycle, log tailing) are wrapped in anything an agent can
call.

## The unlock

`wails dev` already runs a full HTTP + WebSocket dev server on
`localhost:34115` (`internal/frontend/devserver/devserver.go`). It
serves the real frontend assets, injects the real generated bindings,
and bridges every method call and every `runtime.EventsEmit` over a
websocket to the **same running Go backend** the desktop window is
attached to.

A plain Chromium can load `http://localhost:34115` and get a fully
functional YellowJacket. Not a mock, not a stub `wailsjs` layer: the
actual application, talking to the actual `explore`, `library`,
`player` and `queue` services, receiving the actual events. The
bindings land on `window.go`, so anything reachable from the frontend
is reachable from a one-line `page.evaluate`.

This is not a trick we invented. Wails v3's documentation ships an
"End-to-End Testing" guide that is exactly this, and the v2 community
arrived at the same answer independently
(`wailsapp/wails` discussion #4205). It is the sanctioned approach.

**The one caveat:** `devserver.Run` still calls `d.Frontend.Run(ctx)`,
which opens the GTK window and blocks. No flag suppresses it, and
nobody upstream has found a way around it. The app needs a display —
a virtual one.

## Validated end to end, 2026-08-10

The premise was proven before this plan was committed, on a scratch
`YJ_HOME` under `~/.cache/yellowjacket-harness`:

```
go build -tags "dev webkit2_41" -o build/bin/yj-dev .
setsid dbus-run-session -- xvfb-run -a ./build/bin/yj-dev \
  -devserver localhost:34115 -assetdir frontend/dist
playwright-cli -s=yj open http://localhost:34115
```

| Claim | Result |
|---|---|
| App boots headless under Xvfb | yes, ~1 s; `:34115` listening |
| `YJ_HOME` isolates the sandbox | yes, own `yj.db`, untouched real install |
| Chromium loads the real app | yes, console shows `wails dev / Connected to backend` |
| a11y snapshot pierces shadow DOM | yes — sidebar, queue panel, transport buttons, all with stable refs, through Lit **and** Web Awesome roots |
| `window.go` carries the bindings | yes, all 11 services |
| A bound method round-trips to Go | yes — `queue.Queue.GetState()` returned real JSON |
| Events reach the browser | yes — `SetVolume(42)` produced `VolumeChanged` with payload `42` |
| Screenshot is readable by the agent | yes — full render, correct theme, fonts and icons |
| MPRIS registers | yes — `org.mpris.MediaPlayer2.yellowjacket` on the private bus |
| Audio initialises | **yes** — see below |

Five things the run taught that were not obvious beforehand:

- **No null audio sink is needed.** `dbus-run-session` replaces the
  *bus*, not the runtime dir, so `/run/user/1000/pulse` stays reachable
  and `InitSpeaker` succeeded in 17 ms. The mitigation planned for
  Phase 3 is unnecessary on a developer machine. A CI container with no
  `/run/user` will still need one.
- **The first-run wizard blocks every interaction.** The first click
  attempt failed with `<first-run-wizard> intercepts pointer events`.
  Phase 1 is not a convenience; nothing downstream works without it.
- **A malformed binding call hangs forever.** `SetVolume(0.42)` against
  a `player.UserVolume` (an `int`) made the backend log
  `error parsing arguments` and never fire the callback, so the
  in-page promise never settled. Every harness call needs a timeout,
  and the app log is the only place the reason appears.
- **Playwright's WebKit does not run on Arch.** Its Linux build links
  Ubuntu 24.04 libraries — `libicu74`, `libWPEWebKit-2.0.so.1`,
  `libflite` — none of which Arch provides. `--browser=webkit` is a
  **CI-only** capability, not a local one. Chromium is unaffected.
- **Event listeners accumulate across calls.** Hooks registered by one
  `eval` survive into the next, so a naive recorder double-counts. The
  `initScript` must install exactly one recorder, and tests must reset
  its buffer rather than re-register.

## Tooling decisions taken up front

Three things exist that we would otherwise have built badly.

**`@playwright/cli`** (`npm i -g @playwright/cli`) is Microsoft's
CLI-plus-agent-skills front end to Playwright, built specifically
because coding agents do better with terse commands than with MCP tool
schemas. `playwright-cli install --skills` drops the skills where an
agent finds them. It gives us, for free, everything this plan was
otherwise going to hand-roll:

| Need | Command |
|---|---|
| See the page | `snapshot` — a11y tree with stable `ref=eNN` handles, pierces open shadow roots |
| Search a big page | `find <text>` / `find --regex` |
| Call a bound method | `eval "() => window.go.player.Player.Play(1)"` |
| Screenshot for the agent to read | `screenshot --filename=` |
| Frontend errors | `console` — Lit render failures are currently invisible |
| Stub the explore artifact | `route <pattern>` |
| Keep a browser across separate shell calls | `-s=<session>` |
| Watch, and take over | `show` — live dashboard, per-session screencast, click in to grab the mouse |

Plus video and trace recording when a flow needs explaining rather than
asserting. It is v0.1.x and moving; `@playwright/mcp` is the same engine
behind an MCP server and is the fallback if the CLI churns.

**Playwright's WebKit build.** The shipped binary is WebKit2GTK, so a
Chromium-only suite would validate a renderer we do not ship.
`--browser=webkit` is not byte-identical to WebKit2GTK but shares the
engine core, and it is a flag rather than a project. The X11-grab of the
real GTK window drops to an optional spot-check.

**Vitest 4 browser mode.** Stable Browser Mode plus `toMatchScreenshot`
landed in Vitest 4.0, it is the Lit ecosystem's current recommendation
over `@web/test-runner`, and it uses Playwright as its provider — the
same browsers already cached. Components render in a real browser with
real shadow DOM, and get visual regression, **with no Wails, no backend,
no seeded library and no virtual display**. This is a tier the earlier
draft of this plan did not have and is the cheapest coverage available.

So the harness is three tiers, cheapest first:

1. **Vitest browser mode** — components and stores. Seconds. No app.
2. **`playwright-cli` against `:34115`** — real flows against the real
   backend, driven interactively by an agent.
3. **Playwright specs** — the same thing, frozen as a regression suite,
   in CI.

Only tier 2 and 3 need the app running, and therefore Xvfb.

## Phase 1 — Reproducible fixtures *(shipped)*

Nothing can be driven end-to-end against an empty library, and no two
runs are comparable unless the library is identical. No tool provides
this; it is ours to write.

**`cmd/gentestdata`** writes `test_data/music_library_test/`
deterministically: silent/tone audio at known durations across MP3,
FLAC, OGG Vorbis and WAV, with tags written by our own `tagwriter` so
fixtures and reader cannot drift. Coverage must include the cases the
app has code for — embedded cover art shared across an album (dedup),
missing and partial tags, unicode and RTL text, multi-disc, various
artists, and a deliberate duplicate pair for
`duplicate-tracks-dialog`.

`make testdata` generates it; it stays gitignored. A manifest hash lets
a test assert it is looking at the library it thinks it is.

**Seeded sandboxes.** `make sandbox-seed NAME=<n>` builds a `YJ_HOME`
with `config.toml` already pointing at the fixture library and `yj.db`
already scanned, so a run starts *in the app* rather than in the
first-run wizard. A `--fresh` variant deliberately omits config, because
the wizard is itself a surface that needs testing. Seeds rebuild from
scratch in seconds and are never hand-edited.

Explicitly **not** seeded: the explore artifact. `artifactfetch.go`
already honours `YJ_CORE_INDEX_URL` ("overridable for testing"), so
tests point it at a local file server holding a cut-down artifact —
`cmd/indexexport` already produces that shape, so a tiny core is a
config change, not new code. This also makes the failure paths testable
(404, checksum mismatch, the `206` partial-content resume). A nightly
job can use the real artifact.

## Phase 2 — Headless launch *(shipped)*

`scripts/dev-headless.sh` wraps:

```
dbus-run-session -- xvfb-run -a \
  ./build/bin/yj-dev -devserver localhost:34115 -assetdir frontend/dist
```

**Run the dev binary directly, not `wails dev`.** `app_dev.go` parses
`-assetdir`, `-devserver`, `-frontenddevserverurl` and `-loglevel`
straight from `os.Args`, so `go build -tags "dev webkit2_41"` produces a
binary that serves the identical devserver with no file watcher, no
rebuild supervisor and no reload broadcast. One process, one PID,
deterministic startup. `wails dev`'s watcher is a human ergonomic; an
agent that just edited a file knows to rebuild. (`-noreload` and
`-nogorebuild` exist if the watcher is ever wanted anyway.)

**`dbus-run-session` is not incidental.** A private session bus means
`backend/mediacontrols/mpris_linux.go` actually registers, which turns
MPRIS from "untestable" into a surface assertable with `busctl` —
properties out, `Play`/`Pause`/`Next` in.

The script backgrounds the process, writes `.dev/app.pid` and
`.dev/app.log`, polls `:34115` until it answers, then exits, leaving the
app up. `make dev-headless SEED=<n>`, `make dev-stop`, `make dev-logs`.

**Kill by saved PID, never `pkill -f`.** A `pkill -f` whose pattern
appears in the invoking shell's own command line kills that shell and
silently drops the rest of the chain.

New dependency: `xorg-server-xvfb`. Everything else — Playwright and its
Chromium, ffmpeg, `import`, `dbus-run-session`, `busctl`, `pactl` — is
already present.

**Audio needs nothing locally.** Measured: `InitSpeaker` succeeds under
`dbus-run-session` + Xvfb because the PulseAudio socket in
`/run/user/1000` is untouched by a private bus. Only a CI container
without `/run/user` needs a null sink (PipeWire null sink, or an ALSA
`null` PCM via a scoped `asoundrc`), and even then `app.go:325` joins
`InitSpeaker()` failure into `startupErr` rather than aborting, so
everything except playback still runs. Sample-level correctness stays
where it already is, in `backend/player` unit tests.

## Phase 3 — Driving and seeing the app *(shipped)*

What landed, and the five things that were not obvious:

- **`.playwright/cli.config.json`** now sets `testIdAttribute`, a
  1440×900 viewport, timeouts and the `initScript`. Every path in it is
  resolved **relative to the config file**, not the repo root.
- **`.playwright/init-events.js`** is the event bridge, and it hooks
  `window.wails.EventsNotify` rather than `EventsOn` — every backend
  event enters the page at that one call (`case "n"` in wails'
  `ipc_websocket.js`), so one wrap captures all 46 whether or not the
  app subscribes. `window.wails` does not exist when an initScript
  runs, so it is wrapped via an accessor installed on `window` that
  collapses back to a data property on assignment. It also carries
  `ready()` and `call()`, the latter timing out so the
  "malformed binding call hangs forever" trap is paid for once.
- **No closed shadow roots** anywhere: nothing in `frontend/src`
  overrides `createRenderRoot`/`shadowRootOptions` and Web Awesome's
  dist never calls `attachShadow` directly. Snapshots pierce
  everything.
- **The `data-testid` pass was mostly an accessibility fix.** The five
  transport buttons had no accessible name at all, so they were
  unnameable to a screen reader *and* to a selector; they now carry
  `aria-label` plus `aria-pressed` for the shuffle/repeat toggles.
  `data-testid` was added only where a selector would otherwise be
  structural: `track-row`, `queue-row` (both with `data-file-path`),
  `main-content` (plus a `data-active-view` attribute, since which view
  is showing was previously only inferable from which cached child
  lacked `.view-hidden`), the now-playing title/artist and the seek
  bar's two clocks. Sidebar items got `data-testid` and `aria-current`.
- **`backend/testctl`** mounts `/__test/` on the existing asset
  handler: `health`, `db/snapshot`, `db/restore`, `emit`, `sql`. Gated
  twice — the implementation is behind the `dev` build tag with a no-op
  `!dev` twin, and it refuses to register unless `YJ_TESTCTL=1`, which
  `dev-headless.sh` sets and `make dev` does not.
- **`e2e/`** is its own npm package (`make e2e`), deliberately not
  inside `frontend/`, so phase 4's Vitest browser mode does not have to
  share a package with the Playwright runner.

The original plan for the phase follows.

- `playwright-cli install --skills`, and a project
  `.playwright/cli.config.json` setting `testIdAttribute`, viewport, and
  an `initScript`.
- **The `initScript` is the event bridge.** It runs before any app
  script, so it can hook `EventsOn` and buffer all 46 backend events on
  `window.__yjEvents`, read back with `eval`. Half of what this app does
  is push-driven — scan progress, job updates, download progress,
  `WantedListChanged` — and assertions must await an event, not a
  timeout.
- Add `data-testid` where selectors would otherwise be structural. First
  task of the phase is confirming nothing in the Lit / Web Awesome tree
  uses a *closed* shadow root, which would defeat snapshots.
- A **dev-only control surface**. `backend/assets/handler.go` is ours and
  already has `RegisterHandler(pattern, handler)`, so `/__test/...` can
  be mounted on the same port with no new server: seed, snapshot and
  restore the SQLite DB mid-run, force backend-internal state. Roughly
  five endpoints, compiled out of non-dev builds. This is the residue of
  what Playwright genuinely cannot reach — everything browser-side is
  already covered by the CLI.

**Screenshots as the primary agent primitive.** `screenshot --filename=`
then reading the PNG is the feedback loop that makes UI iteration
possible at all, and it matters more than the assertion suite built on
top of it. `snapshot` is the cheaper companion for structure.

**Smoke suite**, once flows are stable, frozen as Playwright specs:
first-run wizard, library views (artists / genres / cover grid / track
list), playback and queue manipulation, playlist and smart-playlist
editing, explore search and detail pages, settings (the HTMX/templ path,
which renders differently from everything else), jobs, downloads.

**Renderer fidelity is CI-only.** Playwright's Linux WebKit is built
against Ubuntu 24.04 and will not start on Arch (missing `libicu74`,
`libWPEWebKit-2.0.so.1`, `libflite`); the download succeeds and the
binary then fails to link. So `--browser=webkit` runs in Job 2 of CI,
where the runner image is Debian-family, and local work is Chromium
only. The X11 grab of the real GTK window stays available as an
optional spot-check for views where WebKit2GTK-specific rendering
matters — and is the *only* WebKit2GTK signal obtainable on this
machine.

**Two live frontends, one backend.** The GTK window and the browser are
both websocket clients of the same backend. This is supported —
`devserver.go` keeps a client map and `notifyExcludingSender`
deliberately fans frontend-emitted events out to the other clients *and*
the desktop frontend; `-browser` exists for exactly this. The risk is
not the transport but our own singletons: 13 stores × 2 instances means
duplicate cover-art fetches on connect and two clients able to issue
`player.Play`. If that proves noisy, the fix is contained — our asset
handler can serve a blank page to the WebKitGTK user agent under
`YJ_HEADLESS=1`, making the window inert. Start without it.

## Phase 4 — Component and store coverage *(shipped)*

What landed:

- **The Wails fake is the whole trick.** Everything in
  `frontend/wailsjs/` is a pure passthrough to `window.go` and
  `window.runtime`, so `test/support/wails-fake.ts` replaces those two
  globals and every test then runs the *real* generated bindings and
  the *real* store code. No module mocking, and no second description
  of the Wails layer free to drift. Its event dispatcher mirrors
  `desktop/events.js`, including `maxCallbacks` expiry and the fact
  that a frontend `EventsEmit` notifies local listeners before Go.
- **Stores are singletons constructed at import**, so the fake is
  installed from `setupFiles`, which runs first. A handful of stores
  read config in their constructor before any test can stub, so the
  setup file carries import-time defaults — without them a store
  caches `undefined` where Go would have sent `[]`, and every consumer
  crashes on `.length` in a way that looks like a component bug.
- **`make ui-test` / `ui-watch` / `ui-visual` / `ui-visual-update`.**
  Visual regression is opt-in (`YJ_VISUAL=1`) because baselines are
  font-hinting and compositing sensitive; the default run asserts
  behaviour only, so nobody's loop breaks over antialiasing.
- **`make bindings-check`** (`scripts/bindings-check.sh`) runs
  `wails generate module` and fails on a dirty tree, ignoring the file
  modes the generator churns. Now in `lefthook.yml` pre-commit; the
  Vitest suite is in pre-push.
- Two frontend bugs the tier found: `ScrollManager.setupResizeObserver`
  threw an unhandled rejection on an empty library (fixed, one guard),
  and `themeStore.loadFromBackend`'s failure handler throws again on
  the state that failed it, so it cannot recover (left alone —
  reachable only if the backend returns an empty accent).

The original plan for the phase follows.

Vitest 4 browser mode with the Playwright provider, in `frontend/`.

- The 13 stores and the keyboard shortcut service. Queue mutation,
  shuffle, repeat transitions, explore cache invalidation and shortcut
  dispatch are near-pure TypeScript with zero tests today.
- Component rendering for the 33 component directories, with
  `toMatchScreenshot` visual regression per component. Real browser,
  real shadow DOM, no app, no display — this is where the bulk of UI
  regression should live, leaving e2e for flows.
- **Binding drift check.** `frontend/wailsjs/` is generated by
  `wails build`, *not* `go generate`, so the existing pre-commit codegen
  check does not cover it. A renamed Go struct field currently surfaces
  at runtime, in a window. Add a target that regenerates bindings and
  fails on a dirty tree.

## Phase 5 — An `events.Emit` wrapper *(shipped)*

What landed:

- **`events.Emit(ctx, name, data...)`** drops an event that has
  nowhere to go, at debug level, instead of taking the process down.
  **`events.Deliver`** is the same call returning `ErrNoRuntime`, for
  the one caller that must know: `/__test/emit`, whose job is to
  impersonate a backend emit and which would otherwise answer `200`
  for an event that reached nobody.
- **The sink is carried in the context**, not in a package-level
  variable — `events.WithSink(ctx, rec)` — so parallel tests cannot
  observe each other's events and production emits pay no
  synchronisation cost. `events.Recorder` implements it with
  `Events`/`Named`/`Names`/`Count`/`Last`/`Reset` and a `Wait` that
  blocks on background emitters (scan progress, `SetQueue` phase 2).
- **Enforcement is a test, not a linter.** golangci-lint runs once per
  build configuration, so a stray emit in an `indexbuild`- or
  `dev`-tagged file would only be seen by the pass that compiles it;
  `TestNoDirectRuntimeEmits` walks the tree and sees all of them.
- **The tier it unblocks, exercised**: `backend/queue` (7),
  `backend/config` (5), `backend/playlist` (4). Playlist is the one
  that matters beyond the wrapper itself — it proves the pattern on a
  service whose emits interleave with SQLite writes and M3U8 file
  writes, and its test reads the playlist back the way the frontend
  would on receipt of the event.

Deferred out of this phase: a general `backend/playlist` CRUD suite.
The service is 2,900 lines with no CRUD coverage today, and that is
its own piece of work rather than a rider on a mechanical refactor.

The original plan for the phase follows.

34 call sites use `runtime.EventsEmit` directly. `runtime.getEvents`
(`runtime.go:47`) `log.Fatalf`s unless `ctx.Value("events")` satisfies
`frontend.Events` — an interface under `wails/v2/internal/`, which we
cannot implement. So none of those code paths can run outside a real
Wails app, and in-process service tests are impossible.

A thin `events.Emit(ctx, name, data...)` in `backend/events`,
delegating to `runtime.EventsEmit` normally and to a recorder when a
test sink is installed, unblocks that. It is mechanical, and it pays for
itself independently as the one place to log or trace all 46 events.

Sequenced after the e2e tiers because it is a refactor touching many
packages, and the tiers above deliver value without it.

## Phase 6 — pi affordances *(shipped)*

What landed, and the one decision that mattered:

- **`.pi/skills/yellowjacket-dev/`**, a directory rather than a flat
  file. Only a skill's description is always in context, so `SKILL.md`
  holds the tier decision table, the canonical command sequences and
  the five gotchas — the last inline rather than in a reference,
  because they are needed *before* the failure — and
  `references/{harness,fixtures,ui-tier,schema-change}.md` hold the
  per-surface depth.
- **The split from `CLAUDE.md` is grammatical, not topical.** A topical
  split is what rots: every new fact has two plausible homes. Three
  docs, three tenses — `NOTES.md` past (measured, dated, append-only),
  `CLAUDE.md` present (what the system is), the skill imperative (what
  to run). CLAUDE.md's harness section lost about half its length to
  this; leaving both would have been exactly the duplicate description
  this repo has a standing rule against.
- **`make skill-check`** makes the rule enforceable rather than
  aspirational: every command in `.pi/**/*.md` must be a real make
  target, so the Makefile stays the source of truth for *how* to invoke
  something and the skill only decides *which* and *in what order*. A
  pre-commit hook; instant.
- **`.pi/prompts/e2e.md`** treats promotion as a transcription with
  four fixed substitutions (snapshot refs → testids, sleeps →
  `waitForEvent`, raw `window.go` → `callBinding`, short fixture →
  `LONG_TRACK`) and three runs — pass, pass again, pass after a DB
  restore — because the characteristic failure of a promoted spec is
  depending on state the hand-driving left behind.
- **`.pi/journal.md`**, per the `/handoff` convention.

The original plan for the phase follows.

With the mechanics settled, wrap them. Much less than the first draft
assumed, because `playwright-cli`'s own skills cover browser work.

`.pi/` gains:

- **`skills/yellowjacket-dev/`** — the build-tag matrix, the two-file
  schema rule, seed and sandbox lifecycle, the harness commands, and
  when to reach for which of the three test tiers. `CLAUDE.md` has the
  architectural half; this is the operational half. It must also carry
  the gotchas the live run surfaced: time out every binding call, check
  `.dev/app.log` when one hangs, and never assume a click will land
  while the first-run wizard is up.
- **`settings.json`** pointing at `../.claude/skills`, because
  `playwright-cli install --skills` writes to `.claude/skills/` and pi
  does not discover that path by default. *(Already in place.)*
- **`.pi/journal.md`**, per the `/handoff` convention.
- A `/e2e` prompt template for promoting an exploratory
  `playwright-cli` session into a committed spec.

No custom extension. Browser control is a solved, actively maintained
problem and a hand-rolled version would be worse and would rot.

## Phase 7 — CI that actually gates *(shipped)*

What landed, and the decisions behind it:

- **One image for both jobs, `ubuntu:24.04`.** Not `golang:1.25`,
  because job 1 runs `make ui-test` — Vitest *browser* mode — so the
  "fast job needs no browser" split does not survive contact. Not the
  Playwright image either, because `e2e/` pins `@playwright/test`
  ^1.56 and `frontend/` pins `playwright` ^1.62, so a prebuilt browser
  set matches at most one of them. Ubuntu 24.04 is also what
  Playwright's WebKit links against, which job 2 needs.
- **Caching needed no runner-side change.** `valid_volumes` is already
  a glob over the runner's cache root, and `GOMODCACHE` / `GOCACHE` /
  `GOLANGCI_LINT_CACHE` are mounted and exported for every job by
  `container.options`. Only the Node-side caches (browsers, pnpm store,
  the Go tarball) are declared in the workflow.
- **The repo is cloned by hand**, as the other three workflows do:
  `actions/checkout` is a JS action and needs node in the container
  before any step has had a chance to install it.
- **Failure output goes to the job log, not only to an artifact.**
  `.dev/app.log` is tailed into the log on failure so `gitea_ci
  job_logs` can reach it, with the Playwright report uploaded
  alongside as `continue-on-error` so a broken upload cannot mask the
  real failure.

The original plan for the phase follows.

`.gitea/workflows/ci.yml` — the repository has three workflows and none
of them test anything, so `gitea_ci` currently reports only packaging
jobs, which actively misleads an agent checking whether a push was
healthy.

- **Job 1 (fast, no display):** `make lint`, both `make test` passes,
  `tsc --noEmit`, Vitest browser mode.
- **Job 2 (display):** Xvfb + `dbus-run-session` + a seeded sandbox +
  `make dev-headless` + the Playwright smoke suite, with screenshots and
  traces uploaded on failure.

Job 2 depends on the fixture generator and the stubbed artifact, so it
lands last.

## Order and why

1 and 2 are the hard blockers and are worth doing even if nothing else
follows — a seeded, scriptable, non-blocking launch is the difference
between an agent that can and cannot run this app. 3 is the payoff and
is now mostly configuration. 4 is the cheapest coverage per hour and can
proceed in parallel with everything else, since it depends on none of
it. 5 is a refactor that unblocks a fourth tier we do not have yet. 6 is
ergonomics and should wait until the commands stop changing. 7 is last
because it depends on all of it.

## Risks

- **`@playwright/cli` is v0.1.x.** Interface churn is likely. The
  mitigation is that `@playwright/mcp` is the same engine behind a
  different front end, so a switch is a config change, and the specs
  written in Phase 3 are plain Playwright either way.
- **Playwright's WebKit is not WebKit2GTK, and does not run here at
  all.** Closer than Chromium in CI, unavailable locally. A
  GTK-specific rendering bug can still escape, and will not be caught
  until CI runs — or ever, for views not in the smoke suite.
- **Xvfb is X11, and the app has a Wayland-specific NVIDIA workaround**
  (`main.go`'s DMABuf disable). CI will not exercise the Wayland path at
  all. Acceptable — that path is a crash workaround, not a feature — but
  it should be a known blind spot rather than a surprise.
- **Seeds are a second description of a valid `YJ_HOME`.** If the
  generator drifts from what the app actually writes, tests pass against
  a state no real install has. Seeds must be produced by *running the
  app*, not by writing config and DB rows by hand — the same discipline
  `sql/schemas/` gets, for the same reason.

## Deferred

- Driving the real WebKit2GTK window directly.
  `WEBKIT_INSPECTOR_SERVER=127.0.0.1:9222` exposes WebKit's remote
  inspector, but the protocol is not CDP and Playwright cannot attach.
  A bespoke client is the only route and is not worth it.
- Wails v3, whose e2e story is better documented and whose dev server is
  the same idea on port 9245. Not a reason to migrate.
- Component testing via `playwright-ct-web`. Vitest browser mode covers
  the same ground with fewer moving parts and a first-party visual
  regression story.

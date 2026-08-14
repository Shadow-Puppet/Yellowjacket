---
name: yellowjacket-dev
description: Operating YellowJacket's development harness — which of the four test tiers to use for a given change, how to run the app headless and drive it with playwright-cli, seed and sandbox lifecycle, the three build-tag passes, and the failure modes that waste a cycle if you meet them cold. Use whenever building, running, testing or debugging this repo.
---

# Working on YellowJacket

`CLAUDE.md` says what this system **is**. This skill says what to
**run**. `.planning/NOTES.md` records what we **measured** and when.
Keep them in those three tenses: if something here is wrong, fix it
here and add the discovery to `NOTES.md` — do not add a corrective
paragraph to `CLAUDE.md`.

Every command below is a `make` target on purpose. The Makefile is the
source of truth for *how* to invoke something; this file only decides
*which* and *in what order*. `make skill-check` fails if a target named
here has disappeared.

## Read this part before you fail

Fifteen things cost a cycle each the first time. They are here, not in a
reference, because you need them *before* the failure, not after.

- **Time out every binding call.** A bound Go method called with wrong
  argument types makes the backend log `error parsing arguments` and
  **never fire the callback**, so the promise hangs forever. Use
  `window.__yjEvents.call(path, args, ms)` (browser) or `callBinding`
  (specs), never a bare `window.go.…`. When one hangs anyway,
  `make dev-logs` — `.dev/app.log` is the only place the reason appears.
- **Nothing is clickable on a fresh `YJ_HOME`.** `<first-run-wizard>`
  intercepts all pointer events until a library exists, and the click
  fails with a Playwright interception error that reads like a selector
  bug. Use a seed unless you are *testing* the wizard, in which case
  `make dev-headless-fresh`.
- **Never `pkill -f`.** The pattern matches the invoking shell's own
  command line, killing it and silently dropping the rest of your
  compound command. `make dev-stop` kills by saved PID.
- **Seeds are produced by running the app**, never by hand-writing a
  `config.toml` and DB rows — a hand-built `YJ_HOME` is a second
  description of a valid one and will drift. `make sandbox-seed` drives
  the real `AddLibrary` binding and waits for the real scan.
- **…and a seed freezes every default it has already persisted.**
  Changing a default in `backend/config` (or `backend/tracklist`) is
  invisible against an existing seed, whose `config.toml` holds the old
  value — while CI builds its seed by running the app and therefore
  tests the *new* one. Re-seed before believing either.
- **A `wa-dialog` is awkward to locate, in three ways.** The host is
  `display: contents`, so the element carrying your testid always
  reports hidden; the visible thing is the native `<dialog>` in its
  shadow root. The slotted content is in the *host's* shadow root, not
  in that dialog's subtree, so `toContainText` on the dialog sees only
  its chrome. And it has an accessible name **only because
  `utils/name-dialog.ts` gives it one** — Web Awesome does not wire
  `label` to `aria-labelledby` — so a new dialog that forgets to call
  the helper from `updated()` is invisible to
  `getByRole('dialog', {name})`.
- **A name is computed on the element carrying the *role*, and Web
  Awesome puts the role in its own shadow root.** `aria-label` on a
  `<wa-slider>` or a `<wa-dialog>` host never reaches the tree. Use the
  component's own `label` (plus `styles/wa-slider-label.css.ts`, since
  a slider's is visible) or `utils/name-dialog.ts`. And in the light
  DOM, a `<label>` that is a *sibling* of its control with no `for`
  names nothing — that was 24 of the 93 controls on Settings.
  **`getFullAXTree` is how you check, and "0 unnamed" is not the whole
  answer**: a `placeholder` is an accname fallback, so a box labelled
  only by one reports clean.
- **The a11y snapshot cannot check an accessible name on a dialog.**
  `playwright-cli snapshot` prints `- dialog [ref=…]` with no name
  whether the dialog is named by `aria-labelledby`, by `aria-label`,
  or not at all — checked all three ways against the running app. Use
  `getByRole('dialog', {name})` in a spec, or CDP
  (`Accessibility.getFullAXTree`) for the browser's own computation,
  which also reports *where* the name came from. A snapshot read as a
  probe here reports failure on a working build.
- **Playwright's WebKit does not run on Arch** (Ubuntu-only libs).
  `--browser=webkit` is CI-only; local work is Chromium. CI runs it
  with `if: !cancelled()` so a chromium failure does not silently
  skip it, which it did for two sessions.
- **CI's `e2e` job is green on both engines** (88 specs each) since the
  container got an audio device that keeps time. If playback specs
  start failing there again, check the **`The sink plays at real time`**
  step first: ALSA's `null` plugin consumes 3000 ms of audio in 2.96 ms,
  so every track finishes instantly and the clock never moves — which
  reads as an app bug and cost two sessions of that suspicion.
- **`make e2e` needs `SEED=default`.** Its specs assert on fixture
  content — unicode tracks, the fixture artists, a known playable file.
  Run against the `bulk` seed a measurement session left behind and a
  third of them fail (13 of 36, when it was measured), in a list that
  reads exactly like a regression in whatever you are holding. `make dev-headless SEED=default` first.
- **…and the suite spends state it cannot always give back.**
  `view-lifecycle.spec.ts` **skips an autotag album** on every run, out
  of the eleven the seed has, and does not put it back — so around the
  eleventh consecutive run against one app it starts failing on an
  empty queue. Restart between runs (`make dev-stop && make
  dev-headless SEED=default`) when a spec starts failing that you have
  not touched, and *before* believing a failure at all. Backend state
  outlives the page: shuffle used to be left on the same way, which
  failed `playback.spec` on the next run — that one is fixed, the
  autotag one is inherent.
- **A frontend edit is not live until you restart the app.** Vite
  updates the module, but an already-registered custom element class
  cannot be re-registered, so a running page keeps the old one and your
  change reads as having done nothing — including across a browser
  reload. `make dev-stop && make dev-headless SEED=…`, then re-check.
  The nastier version: a **build error leaves the dev server serving
  the last good bundle**, so the page still works and still shows the
  old behaviour. `make dev-headless` prints the esbuild error; a
  reload does not. One way to cause one is a stray backtick inside a
  comment in a `css` tagged template literal, which ends the literal.
  **That one is a check now** — `make css-check` (instant, a pre-commit
  hook and a CI step) names the file, the line and the cause, because
  what you otherwise get is `Property 'scroll' does not exist on type
  'CSSResult'` pointing at a line of prose, or every test in the suite
  failing to import. It went in after the trap cost a fourth session in
  which its own warning had been read twice.
- **A failing CI job's log is reachable even when `gitea_ci job_logs`
  says it is not.** That endpoint 404s on this Gitea build. The REST
  API answers, with the `GITEA_TOKEN` already in the environment:
  `/api/v1/repos/yonlu/yellowjacket/actions/runs/<run>/jobs` for
  per-step status (this is how "the WebKit step was *skipped*" was
  found) and `/api/v1/repos/yonlu/yellowjacket/actions/jobs/<id>/logs`
  for the whole log. Two sessions reasoned about the e2e failure from
  the commit list because the first tool's 404 read as "out of reach".
- **`npx tsc --noEmit` is part of the gate, and nothing else runs it.**
  CI does (`.gitea/workflows/ci.yml`), and it typechecks
  `frontend/test/` — which `make lint`, `make test`, `make ui-test` and
  `make e2e` do not. A tree can be green on all four and red in CI.

## Which tier

Four tiers. Start at the cheapest one that can see your change, and
only climb when it cannot.

| You changed | Run | Cost |
|---|---|---|
| A Lit component, a store, the shortcut service | `make ui-test` | ~2 s, no app |
| …and it renders differently | `make ui-visual` | + 6 baselines, opt-in |
| Any Go code | `make test` | 3 passes, ~2 min |
| A service that emits events | `make test` — assert on the payload, see `backend/queue/emit_test.go` | in-process, no app |
| A bound method or a bound struct field | `make bindings` then `make ui-test` | ~1.5 s + 2 s |
| A user-visible flow across frontend *and* backend | `make e2e` (needs the app up) | ~1 min |
| Something you cannot predict — exploring | `make dev-headless SEED=default` + `playwright-cli` | interactive |
| Something whose answer is a *number*, not a pass | `make perf` against a bulk-seeded app | ~1 min + setup |
| A `.sql` or `.templ` file | `make generate`, then the checklist in [references/schema-change.md](references/schema-change.md) | |

Two targets are once-per-clone prerequisites that are **not**
dependencies of the targets needing them, so on a fresh checkout each
fails with a missing-browser error that reads like a broken test:
`make ui-setup` before `make ui-test`, and `make e2e-setup` before
`make e2e`. (`make testdata` *is* a dependency of `make test` and
`make sandbox-seed`; run it by hand only when invoking `go test`
directly, since anything using `internal/testfixtures` **skips**
rather than fails without it — a green run without the library means
less than it looks.)

Two rules about climbing:

- **A component test passing is not the app rendering.** If you touched
  anything in `frontend/src`, verify it in the real app too — start it
  headless, `screenshot --filename=/tmp/shot.png`, and *read the PNG*.
  Two of this repo's worst regressions were only ever visible there: a
  header badge contradicting the settings page, and a virtualized row
  whose columns no longer lined up with its own header. Neither failed
  anything.
- **A list that renders is not a list that repaints.** `lit-virtualizer`
  re-renders its rows when one of its *own* properties changes, not
  when the parent does — so selection highlighting, the playing-track
  row and anything else driven by host state need an explicit
  `virtualizer.requestUpdate()`. Click a row and look, every time you
  touch one of these lists; the controller will hold the right state
  either way. **Check `el.viewActive` first**: dispatching a raw
  `navigate` event does not always activate a view, and an inactive one
  does not render at all — which looks exactly like this bug (the
  controller holds the selection, no row highlights) and is Phase 1
  working as designed. Navigate by clicking the sidebar.
- **Do not write an e2e spec first.** Drive the flow by hand, then
  promote it with `/e2e`. Specs written blind assert on selectors that
  do not exist.

Before a commit, the gate is `make lint`, `make test`, `make ui-test`,
`make bindings-check`, `make css-check` and — from `frontend/` —
`npx tsc --noEmit`. The
first four are lefthook hooks, so skipping them locally only defers the
failure; the typecheck is a hook too but only CI runs it over the test
tree, which is where it has actually broken.

The **message** is gated too: `make commit-check` (a `commit-msg` hook,
and a CI step over every commit in a push) rejects a subject that is not
`type(scope): subject`, is over 72 chars, or ends with a period. A
`--no-verify` commit skips it locally and meets it in CI.

Two things about the e2e tier that are not obvious until they bite.
**The 88 specs share one backend process in file order**, so a spec
that leaves the app somewhere passes alone and fails the suite — leave
the UI as you found it, and *wait* for it rather than trusting the
click to have finished. The queue panel's width is animated and the
transport slides with it, so a click issued while it closes lands on
whichever button moved under the pointer. And **anything asserting on
the queue panel's rows must open it first**: a closed panel renders no
list at all.

## Measuring, when a pass is not the answer

Performance claims need a before and an after on the same machine
against the same library, or they are anecdotes. The fixture library
is a few dozen tracks and cannot show any of it.

```bash
make bulkdata                  # ~11 s, 466 MB into a gitignored .dev/
make sandbox-seed-bulk         # minutes: it is a real scan of 50 000 files
make dev-headless SEED=bulk
make perf LABEL=before         # ... make the change ...
make perf LABEL=after
make perf-compare BEFORE=before AFTER=after
```

Fourteen numbers: startup (and the count of cross-origin requests, which
is whether the app works offline), the bundle's shape and each view's
first open, keystroke-to-paint in the search box, what a naturally
finished track provokes, what one favourite toggle costs, what sitting
idle on Settings costs, what **scrolling** a long list costs (image
bytes and the tier they were requested at, plus frame cost through the
artist grid), what a long **Explore session** retains (heap sampled
after each of twenty-four searches, plus every registered cache's
size), what opening a **2 000-track playlist** costs (elements
retained, eager cover requests, heap, and what one update pass costs
and rebinds), what the **selection** costs (ordering the selected keys
with one row selected at either end of 50 000 and with all of them, and
what "Select all → Edit tags" blocks for), what an **update pass of the
player bar** costs (querySelectors, layout reads, style writes and the
read-after-write interleaves inside `updated()`, measured with a clean
DOM and a dirty one, plus six seconds of real playback), how many
**document pointer listeners** are installed at rest (via CDP, so
nothing else in the run is perturbed), what **"play these"** costs for
an artist, twenty albums and five genres, and heap after a scripted
browse. It wraps every bound Go method, so "did that refetch the
library" is a fact rather than an inference.

`window.__yjCacheStats()` reports every registered cache's entries,
retained chars and cap in one eval — which is how you check a bound is
still holding without rebuilding the reproduction that justified it.

Adding a number is usually the first half of an item's work: most
findings are not among the seven, and the fix cannot be believed
without one. Two rules for adding one.

**Stage what the seed does not have, idempotently and by name.** The
bulk seed has one empty playlist, against which "toggling a heart
refetches every playlist" costs nothing and cannot be reproduced; the
favourite measurement builds ten 500-track playlists first. Staging by
name means a before and an after see the same shape — and
`dev-headless` restores the seed tarball on every launch, so it is
rebuilt each run anyway.

**Measure both halves of a trade.** Route splitting reports bytes
before first paint *and* the slowest first open of a view, because a
split that halves startup by making every page visibly slower has not
helped anyone.

**Measure the state the cost depends on, not just the operation.** A
forced layout costs 3 µs against a clean layout and 0.1 ms against a
dirty one, so a component measured only in its steady state reports
that the finding about it is imaginary. If the work is conditional,
stage both conditions and put both rows in the table — they explain
each other, and one of them is the number the fix has to move.

Fourteen traps, each of which produced a wrong number first:

- **A label is a filename, and audit IDs are case-insensitive as
  filenames.** `.dev/perf/before-m6.json` is the *capital* `M6` (the
  3 s ticker) from an earlier pass; measuring lowercase `m6` under that
  name silently overwrites a baseline three passes of numbers depend
  on. Name a label after the *change*, not the finding.
- **The first run after a rebuild is not a measurement — and the
  second is not reliably a good one either.** A run taken immediately
  after `make dev-headless` often reports first contentful paint at
  96–112 ms against 28–32 ms on the next run of the same build (a cold
  Vite module graph). But the ordering does not hold: one pass saw 100
  then 96, and another 28 then 76. FCP moves ±50 ms for reasons this
  harness does not control, so take two, and if they disagree report it
  as noise rather than taking a third until they agree.
- **A measurement is against whatever seed the app is running.**
  `make e2e` needs `SEED=default`, so a confirming perf run taken
  straight after one measures a few dozen tracks: "Play 20 albums"
  becomes a dash and an artist's bytes fall 40×. Plausible in shape,
  meaningless. Restart on `bulk` before re-measuring anything.
- **A `longtask` entry is delivered *after* the task that produced
  it.** Reading `window.__yjPerf.longtasks` synchronously after the
  operation you just timed reports **0 ms of blocking beside a
  six-second stall**. Wait a couple of hundred milliseconds first. The
  tell is that the two numbers in the row disagree — which is a good
  reason to always measure blocking *and* wall time.


- **`make dev-headless` immediately after `make sandbox-seed`** loses
  the race for port 34115 and comes up with no dev server, while still
  printing `up`. The measurement then attaches to a dying app. Sleep,
  or check `curl -s -o /dev/null -w '%{http_code}' localhost:34115`.
- **`search-bar` debounces 150 ms.** Anything measuring to the next
  frame measures the input echoing its own character.
- **`__yjEvents.wait()` returns an already-buffered event.** Without a
  `reset()` first you get the previous run's answer, which looks like a
  real result and is off by one iteration.
- **A `0 ms` result is usually a broken measurement, not a win.**
  Waiting for `#main-content > :not(.view-hidden)` after a navigation
  matches the view being left — it stays on screen until the incoming
  one is ready — so every view reported 0 ms on every build. Wait for
  the specific element, never a generic selector. Same tell as the
  debounce: **a number that cannot move is not evidence.**
- **`git stash` will not give you a baseline** on a tree carrying
  uncommitted phases: stashing one file reverts *every* uncommitted
  change in it, not the one being measured. Build the before by undoing
  the single change by hand in the current file. For a cap or a
  threshold, setting the constant to `Infinity` is the cleanest
  possible one-variable undo.
- **A bound cannot be verified by a run that never reaches it.** The
  first bounded build measured *identical* to the unbounded one,
  because the session cached 180 entries against a cap of 192 and never
  evicted anything. Same tell as the two traps above — before and after
  suspiciously equal. Make the session overrun the limit.
- **A negative result inherits the coverage of whatever produced it.**
  Two sessions recorded the unbounded Explore caches as "does not
  reproduce" from a browse script that visits Explore and never
  *searches* in it — so both caches were empty the whole time. Before
  believing a finding did not reproduce, check the code path it names
  actually ran.
- **A measurement that warms something has to run after everything
  that reads it.** The playlist-open number pulls ~90 cover images;
  placed before the scroll measurement it filled the HTTP cache and
  took that row's request count from 26 to 0 — a clean, plausible,
  entirely fabricated improvement in a number nothing had touched. It
  runs last now, which costs it its own request count (zero on any
  build, so that row is in the JSON and off the table).
- **The bulk library's covers are 300×300 and ~3.7 kB**, deliberately
  (a realistic cover generator made a 2 GB library). Any finding about
  full-size artwork cannot show its magnitude here; measure the
  mechanism instead — e.g. *which tier the request asked for* rather
  than bytes saved.

## Running the app

The app cannot be started without a display: `devserver.Run` ends in a
blocking GTK window with no flag to suppress it. The harness gives it a
virtual one and returns.

```bash
make sandbox-seed NAME=default    # once (~10 s; runs make testdata itself,
                                  # then builds a seed by running the app)
make dev-headless SEED=default    # starts in the background, returns when :34115 answers
make dev-logs                     # tail .dev/app.log
make dev-stop                     # SIGTERM, so shutdown hooks persist state
```

Then drive it. Run `playwright-cli` **from the repo root** — it picks
up `.playwright/cli.config.json` from the cwd, and writes its snapshots
and console logs to `.playwright-cli/` relative to the cwd too. `playwright-cli`'s own skill covers the commands; what
is specific here is that a session must be *named* so it survives
across separate shell calls:

```bash
playwright-cli -s=yj open http://localhost:34115
playwright-cli -s=yj snapshot                       # a11y tree, pierces shadow DOM
playwright-cli -s=yj screenshot --filename=/tmp/shot.png
playwright-cli -s=yj eval "() => window.__yjEvents.names()"
playwright-cli -s=yj eval "() => window.__yjEvents.call('queue.Queue.GetState', [], 5000)"
playwright-cli -s=yj click e391                     # ref from the snapshot
playwright-cli -s=yj close                          # `make dev-stop` does not do this
```

`snapshot` prints a *path*, not the tree — read the file it names, and
check the timestamp, because a stale one from a previous session sits
in the same directory.

`.playwright/cli.config.json` is picked up automatically: it sets the
viewport, `data-testid`, and the init script that installs the event
bridge. **Assert on an event, not a timeout** — half this app is
push-driven. The bridge and the dev-only `/__test/` control surface are
documented in [references/harness.md](references/harness.md).

Other `YJ_HOME`s exist for humans and block the terminal: `make dev`,
`make sandbox <name>`, `make fresh-install`. Do not use them; you will
never get the shell back.

## Go, and the three build configurations

`make test` and `make lint` already run all three. Spell them out only
when iterating on a single package:

```bash
go test ./backend/player/                        # the app build
go test -run TestName ./backend/player/
go test -tags indexbuild ./backend/explore/... ./cmd/...   # dump importer
go test -tags dev ./backend/testctl/...              # control surface
```

Forgetting the tag gives a build error that looks like a missing
package. Audio integration tests additionally need
`YELLOWJACKET_INTEGRATION=1`.

Three things golangci-lint v2 will reject that are easy to write:
a dynamic `fmt.Errorf` without a sentinel (`err113`), a `return` with
no blank line before it (`nlreturn`), and a long `//nolint` comment on
the same line as its statement (`golines` reflows it and breaks the
directive) — put the directive on its own line above.

**Emit events through `events.Emit(ctx, …)`, never
`runtime.EventsEmit`.** `TestNoDirectRuntimeEmits` walks the tree and
fails the build otherwise, including in files no lint pass compiles.

## References

- [harness.md](references/harness.md) — the event bridge API, the
  `/__test/` endpoints, and the config traps.
- [fixtures.md](references/fixtures.md) — the generated library, the
  manifest, and selecting fixtures by case.
- [ui-tier.md](references/ui-tier.md) — how the Vitest tier fakes Wails,
  and what breaks in it.
- [schema-change.md](references/schema-change.md) — the two-file
  schema/migration checklist.

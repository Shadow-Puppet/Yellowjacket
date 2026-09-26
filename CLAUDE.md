# CLAUDE.md

Guidance for agents working in this repository. `AGENTS.md` is a symlink
to this file (`make skill-check` asserts it).

YellowJacket is a music player for desktop Linux/macOS and Android: Go
backend, TypeScript/Lit frontend, bridged by Wails v3. Plays MP3, FLAC,
OGG Vorbis and WAV.

**Where things are written down.** `README.md` is for users;
`CONTRIBUTING.md` is setup, build and the contributor workflow. This
file is the rules. The *reasons* for a specific shape live in a comment
beside the code that has it — the code here is heavily annotated, and
the comment usually cites the issue — so read the comments of what you
are changing before changing it. `.planning/NOTES.md` holds dated
measurements, gotchas and rejected ideas; the `yellowjacket-dev` skill
(`.pi/skills/yellowjacket-dev/`) holds the operational how-to for each
test tier. Packaging channels keep their own documents
(`packaging/*/README.md`, `docs/android-release.md`).

## Issues and workflow

The Gitea tracker is the source of truth, shared with a collaborator
who cannot see this session. `scripts/issue.sh` is the whole interface
(`list`, `mine`, `search`, `show`, `new`, `claim`, `unclaim`,
`comment`, `close`, `label`, `depends`, `labels`; needs `GITEA_TOKEN`
with `write:issue`).

- **Search before starting** (`issue.sh search <terms>`, which covers
  closed issues too). **If nothing covers the work, open an issue
  first.**
- **Claim before the first edit.** `claim` sets the assignee,
  `Status/In Progress` and a comment naming the branch and approach. If
  someone else holds it, talk to them rather than working around it.
- **File findings.** A bug found along the way, or work deliberately not
  done, is an issue with a reproduction — not a sentence in chat.
- **Labels are a taxonomy**: `Kind/*`, `Area/*`, `Priority/*`,
  `Platform/*`, `Reviewed/Confirmed`, `Status/*` (`Status/*` and
  `Reviewed/*` are exclusive). **#73 is the roadmap**; work in its
  order. Hard blockers are Gitea dependencies plus `Status/Blocked`.
- **`Closes #n` goes in a commit body, one issue per line.** Gitea does
  not parse PR bodies, and a comma list half-works. After a merge, run
  `issue.sh list --state open` and close whatever did not take. Unclaim
  happens automatically on close (`.gitea/workflows/unclaim.yml`).
- **`main` is protected**: feature branch and PR only. The issue number
  goes in the branch name and PR body, never the commit subject. A PR
  body carries a commit-to-issue table, the verification actually run,
  and a `Closes` list (PR #83 is the shape).

`.planning/` is design documents and measured history, not a queue:
`NOTES.md`, `plans/completed/` (one recap per milestone), `audits/`,
and `plans/active/` (in-flight work only; usually empty). A decision
reached on an issue that outlives it goes in `NOTES.md`. The autonomous
loop (`.pi/skills/yj-loop/`, plan 020) follows the same rules.

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
make bindings         # Regenerate frontend/bindings (wails3)
make e2e              # Playwright suite against a running dev-headless app
make e2e-setup        # Install the e2e runner + its browser (once)
make ui-test          # Vitest component/store suite in a real browser (no app)
make ui-visual        # Same, including toMatchScreenshot comparisons
make ui-setup         # Install the Vitest provider's own Chromium (once)
make bindings-check   # Fail if frontend/bindings is stale vs the Go bindings
make skill-check      # Fail if a doc names a make target that doesn't exist
make commit-check     # Fail if a commit subject is not a Conventional Commit
make css-check        # Fail on a nested CSS rule Chrome 113 would drop
make lint             # golangci-lint v2 (strict), all three build configurations
make test             # All tests with race detector, all three build configurations
make vulncheck        # govulncheck for CVEs
make release-dry      # What a release from here would ship
make setup            # Install go tools, frontend deps, git hooks (lefthook)
```

Go tests: `go test ./...`, or `go test -run TestName ./backend/player/`.
Two tagged passes are **not** covered by `./...` (`make test` runs all
three):

```bash
go test -tags indexbuild ./backend/explore/... ./cmd/...
go test -tags dev ./backend/testctl/...
```

Audio playback integration tests need `YELLOWJACKET_INTEGRATION=1`.

## Verifying a change

Cheapest tier first. The skill says which tier a change needs and the
exact commands.

- **Go** — `database.NewTestDB(t)` is in-memory and built by the same
  `applySchema` as production (but shares one connection, so it cannot
  see read-pool bugs). Table-driven. Assert a service's events with
  `events.WithSink(ctx, rec)` (`backend/queue/emit_test.go`). Seed
  tracks with `database.InsertTestTrack`.
- **`make ui-test`** — Vitest in real Chromium, no backend:
  `frontend/test/support/wails-fake.ts` replaces the Wails IPC
  transport, so real bindings and stores run. `setup.ts` clears
  `localStorage` between tests.
- **`make e2e`** — Playwright against `make dev-headless` (the real app
  in Wails `-tags server` mode, no display) on `:34115`. Await backend
  events (`window.__yjEvents.wait(...)`), never timeouts. Dev builds
  with `YJ_TESTCTL=1` expose `/__test/` (`health`, `db/snapshot`,
  `db/restore`, `emit`, `sql`).
- **`make perf`** — for questions whose answer is a number, against
  `make sandbox-seed-bulk`. Not a pass/fail tier.
- **`make ui-visual`** — people only; baselines are machine-specific. A
  change that moves a component's geometry refreshes *that* baseline in
  the same commit, after reading the image.
- **Android** — no CI tier sees the device. See the skill's
  `android-tier.md`, `make android-inspect` and `make android-eval`.

Fixtures (`test_data/music_library_test/`) are generated by `make
testdata` and reached through `internal/testfixtures` by *case*. Seeds
and fixtures are produced **by running the app and our own writers**,
never hand-written.

**A test must be able to fail on the broken build.** Most bugs that
survived here survived a test that was named for the behaviour but
measured plumbing. So:

- Assert the user-visible property, not the internal bookkeeping (e.g.
  `aria-current` on the nav item, not the shell's `data-active-view`).
- When asserting a marker is on the right element, also assert it is
  **absent** from the others.
- A source sweep asserts first that it read something; a sweep over an
  empty glob passes.
- A spec that depends on an order or a store state sets it explicitly.
- Where a tier genuinely cannot observe something (touch, Chrome 113,
  top-layer clipping), assert the *mechanism* and say so in the test.

## Code generation

Never edit generated code: `backend/database/sql/sqlcgen/` (sqlc, from
`sql/queries/`), `*_templ.go` (templ), `frontend/bindings/` (wails3),
`frontend/src/events.ts` (from `backend/events/events.go`). Run `make
generate` after changing `.sql`, `.templ` or events; `make bindings`
after changing a bound Go method. Pre-commit checks all of them.

## Code style

- **Go**: golangci-lint v2 strict (`err113`, `nlreturn`, `wsl_v5`,
  `godot`, `sloglint`, `perfsprint`); imports grouped stdlib →
  third-party → `yellowjacket/...`. `make lint` and `make test` must use
  the same three tag sets.
- **TypeScript**: strict, no implicit any, no unused locals/parameters.
- **Comments** explain *why* a shape is what it is, cite the issue, and
  are the primary design record. Keep that density.
- **Commits**: Conventional Commits (`scripts/commit-check.sh`, whose
  type list must match `.releaserc.yml`). semantic-release reads only
  the **type**: a CI-only change is `ci:`, never `fix(ci):` — `fix`
  ships a real release to Arch, Homebrew and the APK registry.

## Engineering rules

These are the patterns this codebase converged on. Each one was learned
from a bug; a change that breaks one needs an argument in its PR.

### One definition, many readers

- **A fact is stated in one place, and everything else reads it.** A
  second copy is a second thing to forget. Examples: the track
  projection (`track_metadata` view, `trackFromRow`), the catalog row
  (`indexRowColumns`/`scanIndexRow`), the view list
  (`services/view-meta.ts`), shortcuts (`services/shortcut-meta.ts`),
  icon meanings (`utils/icon-language.ts`), ownership
  (`utils/ownership.ts`), request status (`utils/library-status.ts`).
- **Where Go and TypeScript must both declare something, a test reads
  both** (e.g. valid track-list columns). Where a rule is about every
  call site, a source sweep enforces it (`TestNoDirectRuntimeEmits`,
  `TestNoWritesOnTheReadPool`, `icon-language.test.ts`,
  `menu-surface.test.ts`, `make css-check`).
- **Encodings are converted at one boundary.** MBIDs are 16-byte BLOBs
  known only to `backend/explore/mbid.go`; nil slices/maps are
  normalised only in `frontend/src/utils/binding.ts`. SQLite does not
  coerce TEXT↔BLOB, so a missed conversion is a silently empty result,
  not an error — and that includes *bound parameters*, not just
  literals: a Go `string` cursor against a BLOB column made the catalog
  merge loop forever while merging nothing (#258).

### Data

- **The schema is one description; there is no migration chain.**
  `sql/schemas/*.sql` is the current shape. `staleshape.go` repairs an
  old database at open by retiring stale tables — never `Authored` ones,
  and never `Cache` ones under the `indexbuild` tag (that catalog costs
  a ~205 GB re-download).
- **Every table is classified** in `backend/datamap` (Kind, Lifetime).
  Authored data is what a user cannot get back; cascading it needs an
  argued exemption.
- **Ownership is a file.** "Do I have this" is answered by
  `audio_files`, never by a metadata row or a flag.
- **Writes go through the writer.** `QueryContext` and friends use a
  query-only read pool; use `ExecContext` or `QueryRowWriter`.
- **Query files are ASCII** (sqlc rewrites by byte offset). A slice and
  a named parameter don't compose in sqlc; filter in Go instead.
  `library_id = 0` means every library.
- **Optional columns in a downloaded artifact are probed, not assumed**
  (`artifactHasTotals` is the pattern), on the writer connection.
- **Config zero values are the intended default**, so a new key needs
  no migration. **A setter that can reject its value restores the old
  one** (or validates a candidate first): `Save()` validates the whole
  config, so one bad value blocks every later save (#231).

### Work, persistence and the network

- **A durability write is submitted, not performed.** The player and
  queue persist through ordered background writers carrying snapshots;
  never block a user action or hold a component lock on the single
  SQLite writer.
- **Long-running work registers as a job** (`backend/jobs`): visible,
  pausable, cancellable. Register it after counting the work, so an
  empty pass shows nothing.
- **Background network work yields.** Mark it with
  `WithBackgroundPriority(ctx)` so it waits behind interactive requests
  on the shared rate limiters.
- **Record that you asked, not that you got an answer.** An empty
  upstream response is an answer; mark it, or the same work repeats on
  every launch. The frontend caches absence the same way (`[]` in
  `credit-store`, `completeness-store`).
- **A loop that must make progress checks that it did.** A batch walk
  whose bound does not strictly advance, or a merge that lands fewer
  rows than it read, fails loudly — the failure mode otherwise is a
  progress bar that never moves and no error (#258).
- **Don't fetch or store what nothing reads.** Fetch what is displayed,
  record the rest as URLs.
- **Every cache has a size ceiling**, not just an age, and the ceiling
  covers every reference to the data (capping one of two holders frees
  nothing). Frontend caches are `LRUMap`s registered with
  `utils/cache-stats.ts`.
- **Large downloads check the connection first** (`netpolicy.go`); an
  unknown network is not treated as metered.

### Events

- **Emit only through `events.Emit(ctx, name, data...)`.**
- **An event's cost is part of its meaning.** `TrackMetadataChanged`
  makes the frontend discard and refetch everything; never reuse it for
  something cheap. Payloads carry enough for a consumer to patch in
  place instead of invalidating.
- **An unchanged payload is not re-emitted** (`emitStatus`), so every
  mutation of a derived status must emit it itself.
- **Clocks come from the backend.** The frontend's own timers only
  interpolate between reports (playback position).

### Platform and build tags

- **Wails lifecycle**: `ServiceShutdown()` takes no context (a method
  with one is silently never called). Cross-service wiring is
  `backend/startup.go`, registered last — server mode emits no
  `Common.*` application events, so nothing may depend on them.
- **Android recreates the activity, not the process.** `main()` latches
  on `mainStarted` as its first statement; `ServiceShutdown` never runs
  there, so durability cannot depend on it.
- **Keep testable logic out of platform-tagged files.** A tagged file
  holds only the platform call or constant; the contract lives untagged
  (`mediacontrols/androidpayload.go`, `SystemOwnsVolume`), because
  nothing in `make test` compiles the `android` tag.
- **`cmd/indexbuild` and `cmd/indexexport` build with `CGO_ENABLED=0`**
  and must not link Wails (`TestIndexToolsDoNotImportWails`).

### Frontend structure

Lit 3.2 + Web Awesome, singleton stores in `src/store/`, bindings
imported via `@go/...`.

- **Views are lazy chunks** (`VIEW_LOADERS`/`DETAIL_LOADERS` in
  `index.ts`); a missing entry renders a blank page. The failure
  surfaces (`notification-host`, `inline-notice`, `confirm-dialog`)
  stay eager.
- **Primary views are cached, not unmounted**, so `disconnectedCallback`
  never fires for them. Register listeners, timers and subscriptions
  with `listenWhileActive` / `intervalWhileActive` / `whileActive`.
- **Browser history is the only navigation stack.** Back goes through
  `history.back()`; which view is active is `active-view-store`.
- **One surface per kind of UI**: failures through
  `notification-store` (pick the level; copy via
  `utils/describe-error.ts`); destructive actions through
  `confirmAction()`; every dialog is a `wa-dialog`; every menu is
  `menu-surface` with `MenuKeyboard`; every key binding goes through the
  shortcut service with a scope, never a component's own document
  listener.
- **Ask for what the caller uses, once.** Batch per-row lookups into one
  call per frame; return paths rather than whole rows; group results so
  the caller keeps its order.
- **Look up by identity, resolve indexes late.** Build lookups keyed on
  the store array's identity (`utils/track-index.ts`), never `.find` in
  a loop. Selections are keyed by file path; an index is computed when
  used, because it goes stale on any re-sort or refetch.
- **`<lit-virtualizer>` repaints only when its own properties change.**
  Call `requestUpdate()` on selection or playing-track changes; don't
  hoist per-render closures that are what currently triggers a repaint.
- **Work in `updated()` states what it depends on.** Guard it on the
  inputs that change its result.

### Responsive layout and input

Size bands: phone below 600px, compact 600–899, desktop from 900. **No
action may be unreachable at any size**; a control can leave a surface
only if it is reachable elsewhere.

- **Whether an element exists is `matchMedia`; how it looks is CSS.** A
  `display: none` element still carries its handlers, test ids and
  timers.
- **Measure fit where content widths vary** (ResizeObserver, every pass
  starting from all-visible) instead of guessing a breakpoint.
  `scrollWidth` ignores right padding; measure children against the
  content box.
- **Every box that must shrink carries `min-width: 0`.** Phone-width
  rules go last in a stylesheet — a media query adds no specificity.
- **Behaviour keys on capability, never width or platform**:
  `pointerType` for mouse vs touch, `(hover: hover) and (pointer: fine)`
  for hover-revealed controls and tints, `SystemOwnsVolume` for volume.
- **Touch gestures are announced, then claimed** (`yj-tap`,
  `yj-long-press`, `yj-swipe-start` from `utils/touch-gestures.ts`;
  claim with `preventDefault()`).
- **The Android WebView is Chrome 113**: no Popover API (so `wa-popup`
  is clipped by `contain: paint` — use a dialog/`menu-surface`), no
  relaxed CSS nesting (start nested rules with `&`; `make css-check`),
  no `light-dark()`. Chromium passing is not evidence about the device.
- **Overflow inside a component is invisible to page-level checks**;
  measure each control against its container.

### Accessibility

- **A name is computed where the role is.** Verify with `getByRole` or
  CDP `Accessibility.getFullAXTree`; Playwright snapshots don't print
  dialog names, and `title`/`placeholder` are fallbacks, not names.
- **A control that cannot act is absent, not a dead button.**
  `aria-selected` needs a role that supports it (`listbox`/`option` for
  selectable grids). Dimmed rows carry `aria-disabled`.
- **Live regions are in the DOM, empty, before their text arrives.**
- **Colour is never the only signal.** Text colours are per-theme and
  checked at 4.5:1 against every surface (`theme-contrast.test.ts`);
  foregrounds on fills are computed (`--yj-*-fg`), because the accent is
  user-chosen.
- **Motion respects `prefers-reduced-motion`** over any app setting.
- **Querying inside a Web Awesome component waits for *its* first
  update**, not the host's.

## CI and release

- **`ci.yml` is the only gate**: `check` (commit-check, lint, test, tsc,
  ui-test, bindings-check, skill-check) and `e2e` (headless Chromium and
  WebKit). The runner has capacity 1.
- **Releases are cut by hand** by dispatching `release.yml` (use
  `dry_run`, or `make release-dry` locally). It pushes a tag with a user
  PAT, which starts the four publishers (`arch-package`,
  `homebrew-formula`, `android-apk`, `desktop-assets`).
  `@semantic-release/github` and `@semantic-release/git` must not be
  added; check rendered release notes, not the exit code.
- **The APK is never signed with a debug key** — a certificate change
  forces users to uninstall and lose their library.
- **Build**: the Makefile is the front door; Taskfiles and the vendored
  `go tool wails3` (on PATH via `scripts/toolbin`) sit behind it; output
  is `bin/`. `build/android/` is committed source. After refreshing
  `build/` assets from `build/config.yml`, recheck nfpm's `homepage` and
  `license`.
- **Failing job logs**: `GET /api/v1/repos/{owner}/{repo}/actions/runs/{run}/jobs`
  and `.../actions/jobs/{job_id}/logs` with `Authorization: token
  $GITEA_TOKEN`.

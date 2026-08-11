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

Five things cost a cycle each the first time. They are here, not in a
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
- **Playwright's WebKit does not run on Arch** (Ubuntu-only libs).
  `--browser=webkit` is CI-only; local work is Chromium.

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
- **Do not write an e2e spec first.** Drive the flow by hand, then
  promote it with `/e2e`. Specs written blind assert on selectors that
  do not exist.

Before a commit, the gate is `make lint`, `make test`, `make ui-test`
and `make bindings-check` — all four are also lefthook hooks, so
skipping them locally only defers the failure.

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
go test -tags webkit2_41 ./backend/player/                        # the app build
go test -tags webkit2_41 -run TestName ./backend/player/
go test -tags "webkit2_41 indexbuild" ./backend/explore/... ./cmd/...   # dump importer
go test -tags "webkit2_41 dev" ./backend/testctl/...              # control surface
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

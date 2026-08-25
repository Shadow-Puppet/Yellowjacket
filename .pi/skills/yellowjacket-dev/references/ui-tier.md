# The component and store tier (`make ui-test`)

757 tests in a real Chromium with no Wails, no backend, no seeded
library and no virtual display. This is the cheapest coverage available
and where the bulk of UI regression belongs.

```bash
make ui-setup          # once: the Vitest provider's own Chromium
make ui-test           # behaviour only
make ui-watch
make ui-visual         # + toMatchScreenshot baselines (YJ_VISUAL=1)
make ui-visual-update  # re-record them
make ui-test UI_ARGS='store/queue'   # filter
```

## How it works

Wails v3 routes every runtime call — bindings, event emits, window,
dialogs, clipboard — through one IPC transport, and `setTransport()` is
a public seam for replacing it. So
`frontend/test/support/wails-fake.ts` replaces **that and nothing
else**, and the tests then exercise the *real* generated bindings, the
*real* runtime and the *real* store code. No module mocking, and no
second description of the Wails layer.

A binding call carries a *method ID* (an FNV-1a hash of the Go method's
fully-qualified name), not a name, so the fake derives the ID → path
map from the generated tree at setup: each package's `index.ts`
re-exports its service under the Go type's real name, which is the one
place that casing survives. A path that never maps records as `#<id>`
and fails the assertion naming it.

```ts
emit(Events.QueueChanged, payload);      // push a backend event
stub('queue.Queue.GetState', state);     // a value, or a function of the args
stubFailure('queue.Queue.SetQueue');     // reject, as a Go error does
calls('queue.Queue.SetQueue');           // what the frontend called back with
lastArgs('queue.Queue.SetQueue');
const el = await fixture('now-playing'); // mount; shadow()/text() query it
```

Delivery is not mirrored — `emit()` goes through the runtime's own
`window._wails.dispatchWailsEvent`, which is the entry point the
backend's push uses, so listener expiry and ordering are the runtime's
real code. What *is* mirrored is one line of Go: how
`EventManager.Emit` packs variadic data into an event's single `data`
field (none is null, one is the value, more is the slice).

A frontend `Events.Emit` no longer notifies local listeners before Go —
v3 calls the backend, which sends the event back out to every window.
The page still sees its own emit, one round trip later rather than
synchronously.

## Five things that will cost you time

- **Store singletons are constructed at module import**, before any test
  can stub. `test/setup.ts` therefore carries import-time defaults for
  the stores that read config in their constructor. Without one, a store
  caches `undefined` where Go would have sent `[]`, and components crash
  on `.length` — which reads exactly like a component bug and is not.
  Adding a store that reads config on construction means adding its
  default there.
- **`vitest.config.mts`, not `.ts`** — it `mergeConfig`s the repo's
  `vite.config.mts` to reuse the `@go`/`@store`/`@components` aliases,
  and a `.ts` sibling cannot import it.
- **Screenshots need the theme.** The setup file imports
  `@store/theme-store` for its side effect (it applies the `--yj-*`
  ramp to `:root`); without it a component renders white-on-white and
  the baseline is blank.
- **`@lit-labs/virtualizer` never produces two identical frames**, so
  `toMatchScreenshot` on `<queue-panel>` fails with "could not capture a
  stable screenshot" rather than a diff. Assert on its rows instead.
- **A v3 binding settles several microtasks after a v2 one did** — it
  goes through `Call()`, an async `runtimeCallWithID`, the transport and
  a `CancellablePromise`, where v2's `window.go` proxy resolved one
  promise. `fixture()` drains microtasks between two renders so a
  component that loads in `firstUpdated` is loaded when it returns.
  Microtasks and not a timer, deliberately: a timer hangs forever under
  the suites that install fake ones.

## The visual tier does not gate, and that is measured (#196)

`make ui-visual` is the same suite with nine `toMatchScreenshot`
baselines switched on. **Nothing runs it but a person**, deliberately,
and the reason is a number rather than a preference: the committed
baselines were recorded on Arch, and replayed in a bare `ubuntu:24.04`
container — CI's `check` image — three of them fail for reasons that
have nothing to do with any component.

| baseline | Arch | ubuntu:24.04 |
|---|---|---|
| `page-header` filtered-by-search | passes | ratio 0.03 differ, against a 0.02 allowance |
| `track-info` | passes | ratio 0.03 differ |
| `seek-bar` | 1152×18 | 1152×17 |

The two references that were genuinely stale did not even agree about
their *new* size — `now-playing` renders 1152×65 on Arch and 1152×64 in
the container. So moving CI's `check` job from `make ui-test` to
`make ui-visual` is not a one-line change: it needs a second,
container-recorded baseline set, which every local run would then fail
against. That is the same trap the other way round, and a pre-push hook
is the same fault again — one machine's baselines against everybody
else's renderer.

So the tier stays local and opt-in, and the rule that replaces the gate
is:

- **A change that moves a component's geometry refreshes that
  component's reference in the same commit, having read the image.**
  Look at the PNG; the dimensions in the failure message are the cheap
  half of the answer.
- **Never refresh a reference you did not cause.** #196 exists because
  four of them drifted across three unrelated merges, and every red run
  made the next person likelier to stop running the tier than to read
  it.
- **State the world the shot is taken in.** The stores are singletons,
  so a visual case that sets nothing photographs whatever the previous
  case left behind — which is how the sidebar's baseline came to have
  Tracks lit and `now-playing`'s to be playing from a dynamic mix.
- **Record one file with `make ui-visual-update UI_ARGS=<path>`**, and
  check `git status` before committing either way. That filter is only
  honoured since #204: the recipe was a bare `--update`, and vitest
  takes the following positional as the flag's value, so the path was
  swallowed and *every* baseline was re-recorded — blessing any stale
  one in silence.

What the tier is worth, for the record: it is a *layout* check, blind to
colour (the component tier has no `:root`, so it renders the fallbacks —
`make ui-visual` passed unchanged through a whole palette rewrite,
twice), and it has caught one thing nothing else could — swapping
`library-status-indicator`'s `<button>` for a `<span>` lost the UA
stylesheet's `box-sizing` and grew the badge 36→38px.

## Bindings

`frontend/bindings/` is generated by `wails3`, **not** by `go generate`,
so the pre-commit codegen check does not cover it — a renamed Go bound
method first shows up at runtime, as a call that never settles.

```bash
make bindings-check   # ~3.5 s warm, also a pre-commit hook
make bindings         # regenerate for real
```

No build tags are passed: the generator is a static analyser that sees
only the configuration it is told about, and the one that matters is
the one users run, which is the default tag set.

# 009 — Wails v3 migration

**Status:** Phase 0 complete — **go**. Phase 1 **partly done** (toolchain
and build assets landed; Makefile rewrite and the tag swap not started).
Phases 2–7 not started.
**Branch:** `wails-v3`, off `main` at `edb13a6`.
**Created:** 2026-08-13
**Phase 0 run:** 2026-08-13 against **v3.0.0-beta.8**
**Target version:** pin `v3.0.0-beta.8` for the whole migration
**Depends on:** nothing
**Follows:** 005-agent-development-harness (which this must not break)

---

## Verdict

Go — but not urgently, and not in one sitting. The app port is small
and the harness port is not. Phase 0 answered the three questions that
could have killed it, and all three came back favourable, one of them
better than hoped.

The reason to do it is **not** tray icons. It is that v3 deletes an
entire failure class this repo has built scar tissue around, and does
so at the cheapest moment this migration will ever have — before
anything has shipped to real users, the same reasoning that lets
`sql/migrations/` be squashed.

The reason not to rush is that beta churn is real (`beta.3` → `beta.8`
in the lifetime of one nearby reference app) and Phases 1–4 leave the
repo in a state that **must not be merged**.

---

## Why v3: the actual argument

`events.Emit(ctx, name, data...)` exists because v2's
`runtime.EventsEmit` calls `log.Fatalf` — unrecoverably, taking the
process down — on any context that does not carry the Wails runtime.
Everything downstream is scar tissue: the `ErrNoRuntime` contract,
`TestNoDirectRuntimeEmits` walking the whole tree, and worst,
`backend/events/emit.go:83` probing the **v2-private context key**
`ctx.Value("events")` to decide whether emitting is safe.

**v3's emit takes no context at all** — `app.Event.Emit(name, data...)`.
A background worker cannot kill the app by emitting from a
`context.Background()`, because there is no context to get wrong.

Phase 0 verified this rather than inferring it from the signature:
`application.Get()` with no app running returns **`nil`** instead of
`log.Fatalf`-ing, and 20 concurrent emits from detached goroutines
against a created-but-never-`Run()` app completed with no panic and no
crash, headless. The v3 scaffold template itself emits from a bare
`go func()` loop, so this is the blessed pattern, not something we'd be
getting away with.

Secondary wins, in rough order of value: a supported headless server
mode that replaces a hand-rolled script; `ServiceStartup` replacing 12
hand-wired `SetContext` methods *and* deleting 12 spurious bindings;
clean rejection on bad binding args, which deletes the ugliest race in
the e2e harness; and the `webkit2_41` tag disappearing entirely.

---

## Phase 0 — results (2026-08-13, beta.8)

Measured against a `wails3 init -t vanilla` app in a scratchpad.

### Q1 — build environment: **PASS**, better than assumed

`ubuntu:24.04` ships **both** `libwebkitgtk-6.0-dev` (2.52.3) and
`libwebkit2gtk-4.1-dev`. A default-tag build (GTK4 + WebKitGTK 6.0)
**compiles in the CI container** — verified by actually building inside
`docker run ubuntu:24.04`, not by reading package lists. Arch has
`webkitgtk-6.0` (2.52.5) in `extra/`, merely not installed on this
machine.

Both platforms can therefore run v3's *default* path, so **`webkit2_41`
becomes a deletion across ~30 sites, not a translation**.

- Dev machine cost: `sudo pacman -S webkitgtk-6.0`.
- CI cost: `libwebkit2gtk-4.1-dev libgtk-3-dev` → `libwebkitgtk-6.0-dev
  libgtk-4-dev`.
- Fallback if GTK4 misbehaves: `-tags gtk3` builds fine on Arch against
  the installed webkit2gtk-4.1. `wails3 doctor` reports both toolchains
  and labels 4.1 "(legacy)".

### Q2 — headless harness: **PASS**, with one real loss

v3 has a first-class **`-tags server` mode** ("a pure HTTP server
without native GUI dependencies"), a supported replacement for what
`scripts/dev-headless.sh` hand-rolls. Verified with `DISPLAY` and
`WAYLAND_DISPLAY` unset:

- serves the app over HTTP (`WAILS_SERVER_PORT`; defaults to 8080,
  which collides — set it explicitly);
- the runtime loads in a real Chromium; `window._wails` appears,
  exposing `dispatchWailsEvent` and `invoke`;
- **binding calls work** — `Call.ByID(...)` and `Call.ByName(...)` both
  returned correct results;
- **events flow** — 6 events in 3.5 s from the template's 1 Hz
  goroutine emitter, over an SSE broadcaster at `/wails/events`.

Three findings that shape later phases:

1. **`window.go` does not exist, and there is no runtime enumeration
   surface for bound methods.** This is the one genuine regression.
   `e2e/specs/harness.spec.ts:18-19` and `e2e/perf/measure.mjs:130-180`
   both *walk* that object; they lose the mechanism, not just the
   syntax. See Phase 6.
2. **Bad arguments reject cleanly**, with useful messages
   (`expects 1 arguments, got 3`; `could not parse argument #0: json:
   cannot unmarshal object into Go value of type string`). Unknown
   method names reject too. v2's never-fires-its-callback behaviour is
   gone, so `__yjEvents.call`'s timeout race **deletes itself**.
3. **The FQN is the full Go import path**, not the package name.
   `bindings.go:245` builds `fmt.Sprintf("%s.%s.%s", packagePath,
   typeName, methodName)` from `reflect.Type.PkgPath()`. For us that is
   `yellowjacket/backend/library.Library.GetAllTracks` — verbose but
   deterministic. (`main.GreetService.Greet` resolved; `changeme.…` and
   bare `GreetService.…` did not.)

One caveat recorded honestly: the server build **still required a
webkit toolchain at compile time** despite the "no native GUI
dependencies" summary — it failed until pointed at an installed webkit.
Whether that is intended or a beta gap was not determined. It is moot
if we adopt the GTK4 deps anyway.

### Q3 — the emit footgun: **PASS**, decisively

- `application.Get()` with no app running returns `nil`; the process
  survives. The `if app == nil { return ErrNoRuntime }` design is right.
- 20 concurrent emits from detached goroutines, app created but never
  `Run()`, no display: no panic, no crash.
- `application.New()` itself works headless (reports
  `Webkit2Gtk=v2.52.5` under `-tags gtk3`).

### Bonus findings

- **`internalServiceMethods` auto-excludes `ServiceStartup`,
  `ServiceShutdown`, `ServiceName`, `ServeHTTP`** from bindings
  (`bindings.go:238-243`) — confirming the `SetContext` port *removes*
  12 bindings and the bogus `context` model rather than renaming them.
- **Generated bindings are TypeScript, nested by Go import path**
  (`frontend/bindings/<module>/<pkg>/<service>.ts` + a per-package
  `index.ts` re-export). This **disproves** the earlier assumption that
  the 93 `@go` import sites wouldn't change — see Phase 4.
- Calls compile to `$Call.ByID(<fnv hash>, …)` where
  `methodID = hash.Fnv(fqn)`, with an explicit-ID registration escape
  hatch — a harness can compute IDs itself if it ever needs to.
- Bindings return a **`CancellablePromise`**, not a bare `Promise`.
- **Binding generation is build-tag sensitive** (static analyser).
  Wails' own Taskfile passes `BUILD_FLAGS: "-tags server,production"`
  to binding generation so it "analyses the same build the Docker image
  compiles, not the default-tag build."
- **`application.RegisterEvent[string]("name")`** yields typed events
  and a generated typed TS event API — overlaps with what
  `backend/events/cmd/genevents` does by hand.

---

## Ground truth: what we actually touch

Measured, not assumed. The Go surface is small; the harness surface is
the job.

### Go — six files import `wails/v2`

| File | Subpackage | Uses |
|---|---|---|
| `main.go:11-13` | `wails`, `options`, `options/linux` | `wails.Run`, `options.App`, GPU policy |
| `backend/app.go:15` | `pkg/runtime` | `WindowGetSize`, `MessageDialog`, `QuestionDialog`, `Quit` |
| `backend/assets/handler.go:9` | `options/assetserver` | `assetserver.Options{Assets, Middleware}` |
| `backend/events/emit.go:8` | `pkg/runtime` | `EventsEmit` — the only emit in the tree |
| `backend/frontendutil/frontendutil.go:9` | `pkg/runtime` | file/dir dialogs, `LogInfo` |

Plus `backend/logging/`, which implements v2's `logger.Logger`
**structurally** — it does not import wails, so `grep wailsapp` misses
it.

### Bound surface

12 services, ~279 exported methods (`backend/app.go:204-225`).
`YellowJacketApp` itself is not bound; only its lifecycle hooks are
wired — which is already close to v3's service model.

| Service | Methods | | Service | Methods |
|---|---|---|---|---|
| `explore.Service` | 56 | | `player.Player` | 21 |
| `library.Library` | 47 | | `jobs.Service` | 7 |
| `playlist.Service` | 36 | | `tagwriter.TagWriter` | 6 |
| `config.Config` | 30 | | `frontendutil.FrontendUtil` | 5 |
| `queue.Queue` | 25 | | `home.Service` | 1 |
| `download.Service` | 23 (conditional) | | `autotagservice.Service` | 22 |

**12 services expose a public `SetContext(ctx)`**, all called from
`OnStartup` (`backend/app.go:321-330`), all currently exported as
bindings.

### Frontend surface

- **93 `@go/...` import sites** (`@go/models` alone is 42).
- **23 `@runtime/runtime` sites — 22 import only `EventsOn`.**
- App source never touches `window.go`/`window.runtime`; only generated
  code, the Vitest fake, and the e2e harness do.

### Harness surface — the real work

| Artifact | Lines | Fate |
|---|---|---|
| `.playwright/init-events.js` | 302 | **Full rewrite** (wraps v2 internals) |
| `frontend/test/support/wails-fake.ts` | 243 | Two factories rewritten; 480 tests ride on it |
| `backend/testctl/` | 891 | Light — one indirection may simplify |
| `scripts/bindings-check.sh` | 43 | Rewrite |
| `scripts/dev-headless.sh` | ~140 | Possibly replaced by `-tags server` |
| `e2e/perf/measure.mjs` | — | Loses binding enumeration |

### `webkit2_41` — ~30 sites, all deletions

`Makefile` (`:11,14,129,161,221,231,234`, lint matrix `:280-282`, test
matrix `:290-295`), `lefthook.yml:17,21,64`,
`scripts/bindings-check.sh:26`, `scripts/dev-headless.sh:15,134`,
`packaging/arch/PKGBUILD`, `packaging/homebrew/Formula/yellowjacket.rb`,
`CLAUDE.md:53-73` and `:1083`, `.planning/NOTES.md`,
`.pi/skills/yellowjacket-dev/SKILL.md`,
`.pi/skills/yellowjacket-dev/references/schema-change.md`,
`.pi/journal.md`.

---

## Design decisions taken up front

Recorded here so they are not re-litigated mid-phase.

**D1 — `events.Emit` keeps its `ctx` parameter.** v3 doesn't need it for
delivery, but `events.WithSink(ctx, rec)` is the test seam used by 7
test files, and 45 call sites across 13 production files pass a context
already. The context stops being a delivery mechanism and stays a
test-injection mechanism. **One file changes.** The alternative —
dropping the parameter — churns 45 call sites and every test for no
gain.

**D2 — the Makefile stays the front door.** Taskfile becomes an
implementation detail behind existing target names. `make dev`,
`make build-prod`, `make bindings`, `make e2e` all keep their names and
behaviour. `.pi/skills/yellowjacket-dev/` and `make skill-check` depend
on those names, and CLAUDE.md documents them.

**D3 — `wails3` stays a vendored Go tool.** The v2 CLI is in `go.mod`'s
`tool` block, invoked as `go tool wails`. Keep that shape; a global
install would be the first undeclared dependency in this repo's build.

**D4 — the `@runtime` alias becomes a local shim.** 22 files import
`EventsOn` from `@runtime/runtime`. Rather than editing 22 imports to
v3's `Events.On`, point the alias at a small local module that exports
an `EventsOn`-shaped function over `@wailsio/runtime`. Keeps the diff
small and gives Phase 5's fake exactly one seam to target.

**D5 — pin `beta.8` for the entire migration.** Upgrade deliberately,
never incidentally. Registration order and late-registration semantics
changed across betas (`wailsapp/wails#4066`).

---

## Phase 1 — Toolchain and build system

**Goal:** the repo builds and runs under `wails3`, with every `make`
target keeping its name.

`wails.json` (9 lines) is gone; v3 uses `build/config.yml` plus a
Taskfile tree — a genuinely larger and more visible build surface.

**Steps**

1. `sudo pacman -S webkitgtk-6.0` on the dev machine.
2. Scaffold a v3 project *beside* the repo and copy its `build/` tree
   in wholesale, rather than hand-writing `config.yml`. Same discipline
   as "seeds are produced by running the app."
3. Fill `build/config.yml`'s `info` block from `wails.json`'s `name`,
   `outputfilename` and `author`; delete `wails.json`.
4. Swap the `tool` block: `wails/v2/cmd/wails` → `wails/v3/cmd/wails3`.
   Add `github.com/wailsapp/wails/v3 v3.0.0-beta.8`.
5. Rewrite the Makefile's wails invocations behind unchanged target
   names (`:11,14,129,161,221,231,234`).
6. Delete `webkit2_41` from all ~30 sites (see inventory).
7. Point `frontend:install`/`frontend:build` equivalents at `pnpm` —
   the scaffold assumes `npm`; this repo uses pnpm
   (`frontend/package.json.md5` is part of the dep-caching scheme).

**Acceptance:** `make build-dev` produces a running binary;
`make build-prod` still strips and UPX-compresses; `make skill-check`
passes; `grep -r webkit2_41` returns nothing.

**Est.** Half a session. Low risk, high churn.

### Phase 1 — what actually landed (`e7873bd`)

Steps 2, 3 (partly), 4 and 7 are done; 1, 5 and 6 are not.

- **Done.** `wails/v3 v3.0.0-beta.8` pinned and `wails3` added to the
  `tool` block beside the v2 CLI. `build/` holds the scaffold's asset
  tree with `config.yml`'s `info` block filled from `wails.json`.
  `Taskfile.yml` defaults `PACKAGE_MANAGER` to pnpm.
- **Not done.** The Makefile still calls `go tool wails` (v2)
  throughout, `webkit2_41` is still on all ~30 sites, and `wails.json`
  is still present — deliberately, because the v2 CLI reads it and the
  app has not moved to the v3 API yet. Steps 5 and 6 are entangled
  with Phase 2 and should land with it: swapping the tag before
  `main.go` is ported breaks the only build that currently works.

**Two things the plan did not anticipate.**

**`build/` was already taken.** This repo used it as *ignored* build
output (`build/bin/` held three v2 binaries), and v3 wants it for
*tracked* build assets. `.gitignore` now names `build/bin/` and `bin/`
rather than `build`, and the assets are committed. The mobile platform
trees (`build/android`, `build/ios`, ~40 files) are not carried — this
is a desktop player and cannot target them — and their `includes:`
entries are dropped from `Taskfile.yml`.

**GTK4 is unavailable on the dev machine, so the gtk3 fallback is in
use.** `webkitgtk-6.0` is not installed and installing it needs sudo.
The consequence is sharper than the plan's "fallback if GTK4
misbehaves": **`go tool wails3` does not work at all**, because the
CLI itself links `internal/operatingsystem`, which `pkg-config`s
`gtk4 webkitgtk-6.0` under default tags. `go run -tags gtk3
github.com/wailsapp/wails/v3/cmd/wails3` builds and runs fine
(`doctor` reports `-tags gtk3` and Webkit2Gtk v2.52.5), so that — not
`go tool wails3` — is the invocation the Makefile must use until
`sudo pacman -S webkitgtk-6.0` happens. Phase 0's claim that
"`wails3 doctor` reports both toolchains" was not true on this
machine.

---

## Phase 2 — Go bootstrap and services

**Goal:** the app starts, shows a window, and every service is bound.

**2a — `main.go:75-97`.** Split `wails.Run(&options.App{…})` into
`application.New(opts)` → `app.Window.NewWithOptions(…)` → `app.Run()`.

- `Title`/`Width`/`Height`/`MinWidth`/`MinHeight`/`BackgroundColour` →
  `WebviewWindowOptions`.
- `Linux.WebviewGpuPolicy` survives (v3 keeps Always/OnDemand/Never).
- `Logger` → `slog`; `backend/logging/`'s adapter likely deletes
  outright, since the repo already uses `slog` everywhere else.
- `AssetServer` → `application.AssetOptions{Handler: …}`.
- Re-check the NVIDIA/Wayland `WEBKIT_DISABLE_DMABUF_RENDERER=1`
  workaround (`main.go:32-39,134-155`) — v3's `operatingsystem` package
  detects the proprietary driver and may already do this.

**2b — `Bind` → `Services`.** `backend/app.go:204-225` becomes
`[]application.Service` via `application.NewService(...)`. The
conditional `download.Service` append still works.

**2c — the 12 `SetContext` methods → `ServiceStartup`.** This is the
largest structural port and v3 has a better answer than ours:

```go
ServiceStartup(ctx context.Context, options application.ServiceOptions) error
ServiceShutdown() error
```

The context is cancelled on app shutdown — strictly better than
`SetContext`. And because `internalServiceMethods` excludes these
names, the port **removes 12 spurious bindings** and the fake `context`
namespace from the generated models.

Sites: `autotagservice/service.go:204`, `config/config.go:284`,
`download/service.go:51`, `explore/explore.go:129`,
`explore/searchindex.go:265`, `frontendutil/frontendutil.go:23`,
`jobs/jobs.go:210`, `library/library.go:187`, `player/player.go:190`,
`playlist/playlist.go:167`, `queue/queue.go:196`,
`tagwriter/pipeline.go:81`.

> **Trap:** `ServiceShutdown()` takes **no context**. A method with a
> `context.Context` parameter does not satisfy the interface and is
> **silently never called** — no error, no warning. Grep for it after
> the port.

**2d — `backend/app.go`'s runtime calls.**

- `WindowGetSize(ctx)` (`:521`) → `window.Size()`/`window.Bounds()`.
  **Keep the sub-minimum guard** (`:526-536`); it exists because v2
  reports garbage sizes during teardown and there is no reason to
  assume v3 doesn't.
- `MessageDialog`/`QuestionDialog` (`:566-579`) → v3 dialogs API.
- `Quit(ctx)` (`:608`) → `app.Quit()`.
- `OnBeforeClose` returning `true` to veto → v3's cancellable window
  event (`event.Cancel()`). This is the quit-during-tag-writes veto —
  a data-safety path, so test it deliberately.

**2e — `backend/frontendutil/`** — five dialog methods, mechanical.

**2f — `backend/assets/handler.go`** — v3 changes asset serving. Note
`RegisterHandler` (`:65`) mounts testctl at `/__test/`; Phase 6 may
replace it with `ServiceOptions{Route:}` instead.

**Acceptance:** app launches, window is the persisted size, all 12
services callable, quit-during-writes still vetoes.

**Est.** One session. This is the "1–4 hours" the official guide prices.

---

## Phase 3 — Events

**Goal:** one file changes on the Go side; 22 imports get a shim.

Per **D1**:

```go
func Deliver(ctx context.Context, name string, data ...any) error {
	if sink := sinkFrom(ctx); sink != nil {
		sink.Emit(name, data...)
		return nil
	}
	app := application.Get()
	if app == nil {
		return ErrNoRuntime // replaces the ctx.Value("events") probe
	}
	app.Event.Emit(name, data...)
	return nil
}
```

**Unchanged:** 45 `events.Emit` call sites across 13 files;
`events.WithSink` in 7 test files; `backend/events/recorder.go`;
`/__test/emit`'s use of `events.Deliver`
(`backend/testctl/handlers_dev.go:118-123`);
`backend/events/cmd/genevents` and `frontend/src/events.ts` (that
generator reads a const block and knows nothing about Wails).

**Changed:** `backend/events/emit.go` only.

**`TestNoDirectRuntimeEmits`** (`noemit_test.go`): keep it, retarget the
needle from `.EventsEmit(` to v3's emit. Its original justification
weakens (no more `log.Fatalf`), but "there is exactly one emit path in
this tree" remains worth pinning — it is what keeps `emitStatus`-style
dedup honest.

**Frontend:** create the `@runtime` shim (D4) exporting `EventsOn` over
`@wailsio/runtime`'s `Events.On`. 22 import sites unchanged.

**Deferred, not done here:** `application.RegisterEvent[T]` overlaps
with `genevents`. Do not fold them together during the migration —
note it as follow-up work so a port doesn't become a redesign.

**Acceptance:** `make test` green; a `/__test/emit` still renders
push-driven views.

**Est.** Half a session.

---

## Phase 4 — Bindings

**Goal:** the frontend imports real generated v3 bindings.

**Steps**

1. Generate against the real services and **inspect the tree first** —
   the exact nesting decides the codemod.
2. Remap `@go` in `frontend/vite.config.mts:7` and
   `frontend/tsconfig.json:28`; drop the `wailsjs/go/**/*.js` exclude
   at `tsconfig.json:50` (v3 emits `.ts`).
3. **Codemod all 93 `@go/...` import sites.** Phase 0 disproved the
   hope that an alias absorbs this: `@go/library/Library` becomes
   `@go/yellowjacket/backend/library`, a change of *shape*.
4. `@go/models` (42 sites) — v3 has no single `models.ts`; types come
   from the per-package modules. This is the largest single cluster and
   should be scripted, not hand-edited.
5. Rewrite `scripts/bindings-check.sh`. Its `chmod` dance and
   `core.fileMode=false` diff exist purely because v2's generator wrote
   three runtime files 755 — likely all deletable.
6. **Pin an explicit tag set for binding generation** and make it the
   one the shipped binary uses. The generator is a static analyser, so
   it sees only the configuration it is told about; we have three
   (`webkit2_41`, `+indexbuild`, `+dev`) and `backend/testctl` is
   `//go:build dev`. Getting this wrong means the generated API
   reflects a configuration users never run. v2 had no such hazard
   (runtime reflection).
7. Check whether any call site depends on the return being a plain
   `Promise` — v3 returns `CancellablePromise`.

**Acceptance:** `tsc --noEmit` clean; `make bindings-check` passes and
is still a pre-commit hook and a CI step (`ci.yml:176`); the 12
`SetContext` bindings and the `context` model are **gone**.

**Est.** One session, mostly codemod-and-verify.

---

## Phase 5 — The Vitest fake (`make ui-test`, 480 tests)

**Goal:** 480 tests still run in ~2 s with no Wails, backend, or display.

`frontend/test/support/wails-fake.ts` (243 lines) fakes exactly two
globals, which is *why* the suite is that fast. The design survives;
the targets change.

- `makeGoProxy()` (`:179-193`) and `makeRuntimeProxy()` (`:197-225`)
  are the whole change. The recursive `Proxy` is schema-free, so it
  does not need to learn v3's binding surface — it needs to intercept
  wherever v3 routes calls, now that `window.go` is gone. With D4's
  shim in place, that is one seam.
- The `Listener` class (`:23-44`) and `notify()` (`:105-125`)
  deliberately mirror v2's
  `internal/frontend/runtime/desktop/events.js`, including
  `maxCallbacks` expiry and the ordering where `EventsEmit` notifies
  local JS listeners **before** Go. **Re-derive this against v3's
  actual implementation rather than porting it.** If v3 changed the
  ordering, failures will look like store bugs, not fake bugs.
- `reset()` (`:163-169`) keeps listeners on purpose, because store
  singletons are never re-imported. That constraint is unchanged.

**Acceptance:** `make ui-test` green, **zero test-file edits**. Any test
that needs changing is evidence the fake is wrong, not the test.

**Est.** One session. This is where the official estimate stops
applying.

---

## Phase 6 — E2E harness and testctl

**Goal:** `make e2e` green with **zero spec edits**. That is the
acceptance test for the whole migration.

**6a — `.playwright/init-events.js` is a full rewrite (302 lines).** It
does not use the public API by design; its own header says so. It wraps
`window.wails.EventsNotify` — in v2 every backend event enters the page
at exactly one place (`ipc_websocket.js`:
`case "n": window.wails.EventsNotify(message)`) — and installs a
property accessor on `window` to wrap at assignment time, because
`window.wails` doesn't exist when an initScript runs.

None of that survives. What **must** survive is the public surface on
`window.__yjEvents`: `wait()`, `ready()`, `call()`, `all`, `since`,
`names`, `count`, `last`, `reset`. `e2e/support/fixtures.ts`, every
spec, and `e2e/perf/measure.mjs` are written against it.

v3 equivalents, all settled by Phase 0:
- Hook `window._wails.dispatchWailsEvent` (same accessor-on-assignment
  trick still applies) for inbound events.
- `call()` routes through
  `Call.ByName('yellowjacket/backend/queue.Queue.GetState', …)` and
  **drops its timeout race entirely** — v3 rejects on bad args and
  unknown methods.
- `ready()` likewise becomes a `ByName` call rather than a
  `window.go?.queue?.Queue?.GetState` poll.

**6b — the `window.go` regression.** `harness.spec.ts:18-19` asserts
"all 11 bound services land on `window.go`" (11 where the count is now
12 — download is conditional), and `perf/measure.mjs:130-180`
*enumerates* bindings to wrap every bound method, which is what makes
"did that refetch the library" a fact rather than an inference. v3 has
no runtime enumeration surface. Two options:

1. **Preferred.** Generate the list at build time from
   `frontend/bindings/` — it is a real module tree, so it can be
   imported and walked — and wrap that.
2. Wrap an explicit hand-maintained list. Cheaper, and silently goes
   stale — exactly the failure mode `bindings-check` exists to prevent.

If (2), say so in `measure.mjs` and add it to what `bindings-check`
guards.

**6c — `backend/testctl/` gets easier.** Its only Wails coupling is
`Deps.Context func() context.Context` (`testctl.go:46-53`) — a function
rather than a value "because the context only exists after OnStartup."
`ServiceStartup(ctx, opts)` may make that indirection unnecessary.
Better still, v3 supports a service implementing `http.Handler`
registered with
`application.NewServiceWithOptions(svc, application.ServiceOptions{Route: "/__test"})`
— a first-class replacement for mounting a mux on the asset server.
The double gate (`//go:build dev` + `YJ_TESTCTL=1`) stays exactly as is.

**6d — `scripts/dev-headless.sh` and the port.** Evaluate replacing the
hand-rolled headless launch with `-tags server`. Two constraints:
`e2e/playwright.config.ts` expects `:34115` (set `WAILS_SERVER_PORT`),
and testctl must still mount. If server mode complicates the mount,
keep the existing script — the win is tidiness, not capability.

**Acceptance:** `make e2e` green on **both** Chromium and WebKit, zero
spec edits.

**Est.** One to two sessions. The largest and riskiest phase.

---

## Phase 7 — CI and packaging

- `.gitea/workflows/ci.yml:74,220`: `libwebkit2gtk-4.1-dev libgtk-3-dev`
  → `libwebkitgtk-6.0-dev libgtk-4-dev`.
- The PulseAudio null-sink setup and its three-second timing check are
  unrelated and stay exactly as they are.
- The WebKit Playwright project (`:364-369`, `if: ${{ !cancelled() }}`)
  matters **more** after this, not less — it is the only approximation
  of the shipping renderer, and v3 may change which WebKit that is.
  Keep the `!cancelled()` guard; it is why WebKit signal was silently
  absent for two sessions before.
- `packaging/arch/PKGBUILD` and
  `packaging/homebrew/Formula/yellowjacket.rb` carry the build tag and
  dependency lists.
- `make skill-check` fails if `.pi/` documents a nonexistent make
  target — update `.pi/skills/yellowjacket-dev/SKILL.md` and
  `references/schema-change.md` in the **same commit** as any rename.
- Update `CLAUDE.md`: the `webkit2_41` mandate (`:53-73`), the
  Arch/Ubuntu tag rationale (`:1083`), the events-wrapper section, and
  the harness description.

**Acceptance:** a green CI run on both jobs.

**Est.** Half a session.

---

## Phase 8 — What v3 unlocks (explicitly out of scope)

Listed so nobody smuggles them into the port and calls it a migration.

- **System tray with menus.** v2 has no first-class tray API; v3 does
  (`systray-basic`, `systray-menu`: attached window, left-click toggle,
  right-click menu, light/dark icon variants). For a music player this
  is real — play/pause/skip without raising the window, minimise to
  tray. Most likely thing to make the migration worth *scheduling*.
- **Multi-window** — a detached mini-player as a first-class window.
- **Native menus** (`window.SetMenu`, `app.NewMenu`).
- **Single-instance** with `OnSecondInstanceLaunch`.
- **Typed events** via `RegisterEvent[T]`, possibly retiring
  `genevents`.
- Richer bindings (real param names, preserved doc comments) — a DX
  nicety, not a driver.

`backend/mediacontrols` (MPRIS over raw D-Bus) and `backend/profiling`
(pprof, build-tag-gated) touch no Wails API and are unaffected.

---

## Risk register

| Risk | Severity | Status |
|---|---|---|
| v3 can't drive the headless harness | ~~fatal~~ | **Retired.** `-tags server` verified: calls + events, no display |
| Arch/Ubuntu need different webkit tags | ~~high~~ | **Retired.** Both ship webkitgtk-6.0; default builds in the CI container |
| No `window.go` → e2e/perf lose binding enumeration | **high** | *New, confirmed.* Decide 6b option (1)/(2) |
| 93 `@go` sites need editing after all | **high** | *Confirmed.* Bindings nest by import path; codemod required |
| Binding generation analyses the wrong build config | **high** | *New.* Pin tags in Phase 4 step 6 |
| Beta churn mid-migration | high | Pin `beta.8`. `beta.3`→`beta.8` in one app's lifetime |
| E2E rewrite silently weakens coverage | high | Acceptance = `make e2e` green, **zero spec edits** |
| v3 event ordering differs from v2's | medium | Re-derive the fake; don't port it |
| `ServiceShutdown()` signature trap | medium | Silent no-call; grep after Phase 2c |
| Quit-during-writes veto breaks | medium | Data-safety path; test deliberately in 2d |
| Regression no tier covers | medium | `make perf` before/after on the same seed |
| GTK4 changes rendering vs GTK3 | low | Unmeasured; visual check on first run |

---

## Sequencing and staging

**Phases 1–4 must not be merged.** They leave the app building and
running with the harness broken, and plan 005's whole point is that a
broken harness means a coding agent cannot develop this repo at all.
Phases 5 and 6 are what make the branch mergeable — and they are the
majority of the work.

Recommended shape:

1. Land the **`webkit2_41` deletion + `pacman -S webkitgtk-6.0`**
   independently if desired — it is useful on its own and touches
   nothing else. *(Optional; can also ride along in Phase 1.)*
2. Branch `wails-v3` off a clean `wip`. Phases 1–4 as separate commits
   on it, kept local.
3. Phases 5, 6, 7 onto the same branch.
4. One merge to `main` when `make test`, `make ui-test`, `make e2e`
   (both browsers) and `make lint` are all green.

**Before starting:** `wip` currently has ~75 uncommitted files. Commit,
stash, or use a worktree — do not begin Phase 1 on a dirty tree.

**Total estimate:** 4–6 focused sessions. The official guide's "1–4
hours" covers roughly Phase 2 alone.

---

## Open questions

- Does v3 handle the NVIDIA/Wayland DMABuf workaround itself
  (`main.go:32-39,134-155`)? Its `operatingsystem` package detects the
  driver, which suggests it might. *Check in Phase 2a.*
- Is `backend/logging/`'s `logger.Logger` adapter deletable outright
  once v3 uses `slog`? *Check in Phase 2a.*
- Does GTK4 change anything visible about rendering vs GTK3?
  *Unmeasured; visual check on first run.*
- Should `application.RegisterEvent[T]` replace
  `backend/events/cmd/genevents`? *Deliberately deferred past the
  migration.*
- Does `-tags server` complicate mounting testctl? *Decides Phase 6d.*

**Answered by Phase 0** (kept so they aren't re-asked): which beta to
target (`beta.8`); whether v3's call-by-name rejects on bad args (yes,
cleanly — the timeout race goes); whether the headless dev surface
survives (yes, and improves).

---

## References

- [Migration guide](https://v3.wails.io/migration/v2-to-v3/) — feature
  mapping, testing checklist, the "1–4 hours" estimate
- [What's New in v3](https://v3.wails.io/whats-new/)
- [v3 beta announcement](https://v3.wails.io/blog/wails-v3-beta/)
- [Application lifecycle](https://v3.wails.io/concepts/lifecycle/)
- [`pkg/application` API](https://pkg.go.dev/github.com/wailsapp/wails/v3/pkg/application)
- [v2→v3 discussion #4509](https://github.com/wailsapp/wails/discussions/4509)
- [Late service registration #4066](https://github.com/wailsapp/wails/pull/4066)
- **Reference v3 app:** `/mnt/vault/dev/ljos` — project layout,
  `build/config.yml`, Taskfile scaffold, `application.Service`,
  `SingleInstanceOptions`. **Caveat:** it deliberately uses no generated
  bindings and no events (its frontend talks HTTP to a separate
  server), so it models Phase 1 well and Phases 3–6 not at all.
- Local Phase 0 artifacts (scratchpad, ephemeral): scaffolded `spike/`
  app, `q2.mjs`/`q2b.mjs` browser probes, `q3_test.go` emit-safety
  tests.

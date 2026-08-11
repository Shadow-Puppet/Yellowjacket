# 005 — Agent development harness

**Status:** implemented
**Branch:** main
**Created:** 2026-08-10
**Shipped:** 2026-08-11 (`5ca6cad`, `ccacd67`)
**Follows:** 004-wanted-list

## Problem

A coding agent could develop this repo's Go packages competently and
could not develop the *application* at all. It could read 66k lines of
backend, run 31k lines of tests and lint two build configurations. It
could not start the app, see a window, click anything, or find out
whether a change to a Lit component rendered.

The gap was not missing tests. Every path to running YellowJacket ended
in a blocking GTK window — `make dev`, `make sandbox <n>` and
`make fresh-install` all launch a WebKit window and never return the
shell. So 265 bound methods across 11 services, 46 backend events, 33
component directories, 13 reactive stores and a 357-line keyboard
shortcut service had exactly one form of verification available:
`tsc --noEmit`.

Three secondary facts made it worse. `test_data/music_library_test/` was
referenced by three test files, gitignored, absent, and had no
generator, so the audio path was unreachable from a clean clone. No
workflow ran `make test` or `make lint` — gating existed only in
`lefthook.yml`, which is local and `--no-verify`-skippable. And there
was no `.pi/`, so none of the awkward invocations were wrapped in
anything an agent could call.

## The unlock

`wails dev` already runs an HTTP + WebSocket dev server on
`localhost:34115` (`internal/frontend/devserver/`). It serves the real
frontend assets, injects the real generated bindings, and bridges every
method call and every event over a websocket to the **same running Go
backend** a desktop window attaches to. A plain Chromium pointed at
that port gets a fully functional YellowJacket — not a mock, not a stub
`wailsjs` layer. This is the sanctioned approach; Wails v3 ships a guide
for it and the v2 community reached the same answer independently
(discussion #4205).

The one caveat: `devserver.Run` still calls `d.Frontend.Run(ctx)`, which
opens the GTK window and blocks, with no flag to suppress it. So the app
needs a display — a virtual one.

## What shipped

117 files, ~14.6k lines. Four test tiers, cheapest first:

| Tier | Command | Cost | Needs the app? |
|---|---|---|---|
| Components and stores | `make ui-test` | ~2 s, 313 tests | no |
| Services, in-process | `make test` | 3 passes | no |
| Exploration | `make dev-headless` + `playwright-cli` | interactive | yes |
| Frozen regressions | `make e2e` | ~20 s, 19 specs × 2 browsers | yes |

**Fixtures** (`cmd/gentestdata`, `make testdata`, `internal/testfixtures`).
31 tracks across MP3/FLAC/OGG/WAV in ~1 s, deterministic, gitignored,
covering the cases the app has code for: shared album art (dedup),
missing and partial tags, unicode and RTL, multi-disc, various artists,
a deliberate duplicate pair. Tests select by *case*
(`CaseCoverDedup`, `CaseUnicode`, …) rather than by path, and skip
themselves when the library has not been generated.

**Headless launch** (`scripts/dev-headless.sh`, `dev-stop.sh`,
`seed-sandbox.sh`). `dbus-run-session -- xvfb-run -a` around the
`dev`-tagged binary, backgrounded, writing `.dev/app.pid` and
`.dev/app.log` and returning once `:34115` answers. The dev binary is
run directly rather than through `wails dev`: `app_dev.go` parses
`-devserver`/`-assetdir` from `os.Args`, so one process with a
deterministic startup replaces a file watcher and rebuild supervisor an
agent does not want. `dbus-run-session` is not incidental — a private
session bus makes MPRIS actually register.

**Driving and seeing.** `.playwright/init-events.js` records every
backend event on `window.__yjEvents` by wrapping
`window.wails.EventsNotify`, the single choke point all 46 events pass
through, so assertions await an event rather than a timeout. It also
provides `ready()` and a `call()` that times out. `backend/testctl`
mounts `/__test/` on the existing asset handler — `health`,
`db/snapshot`, `db/restore`, `emit`, `sql` — gated twice, behind the
`dev` build tag and behind `YJ_TESTCTL=1`. A `data-testid`/aria pass
turned out to be mostly an accessibility fix: the five transport
buttons had no accessible name at all.

**Component coverage** (`frontend/test/`, Vitest 4 browser mode).
`frontend/wailsjs/` is a pure passthrough to `window.go` /
`window.runtime`, so faking just those two globals runs the *real*
generated bindings and the *real* store code — no module mocking, and
no second description of the Wails layer free to drift.
`make bindings-check` regenerates `frontend/wailsjs` in ~1.5 s and
fails on a dirty tree, closing the gap where a renamed Go field first
appeared at runtime in a window.

**`events.Emit`** (`backend/events/`). `runtime.getEvents` `log.Fatalf`s
on any context lacking wails' internal `"events"` value — any
`context.Background()` — so 35 emit sites could not run under test and a
background worker could take the app down. All 35 now route through one
wrapper that drops at debug level instead. Four packages had each
hand-rolled the same guard; nine more guarded on `ctx != nil`, which
does not help. The test sink rides in the context
(`events.WithSink`), and `TestNoDirectRuntimeEmits` walks the tree —
not a lint rule, because lint runs once per build configuration and
would miss a stray emit in a tagged-out file.

**pi affordances** (`.pi/`). `skills/yellowjacket-dev/` is the
operational manual; `prompts/e2e.md` promotes a hand-driven session
into a spec; `journal.md` is the work log. `make skill-check` fails a
commit if the skill cites a make target that does not exist.

**CI that gates** (`.gitea/workflows/ci.yml`). Two jobs in
`ubuntu:24.04`: `check` (lint ×3, test ×3, `tsc --noEmit`, `ui-test`,
`bindings-check`, `skill-check`) and `e2e` (Xvfb + private bus +
fixtures + seed + `dev-headless` + Playwright on **Chromium and
WebKit**). The other three workflows only package, so `gitea_ci`
previously reported nothing about whether a push was healthy.

## Decisions worth keeping

- **The split between the three docs is grammatical, not topical.**
  `NOTES.md` past, `CLAUDE.md` present, the skill imperative. A topical
  split rots because every new fact gets two plausible homes.
- **Seeds are produced by running the app**, never by hand-writing
  `config.toml` and DB rows — the same discipline `sql/schemas/` gets,
  for the same reason. A hand-built `YJ_HOME` is a second description
  of a valid one and will drift.
- **The Makefile is the source of truth for *how* to invoke something**;
  the skill only decides *which* and *in what order*, and
  `make skill-check` enforces it.
- **Verify in a fresh clone, not a copy of the working tree.** The CI
  prototype ran both jobs in one mounted directory and so consumed a
  `frontend/dist` an earlier job had built — hiding that `main.go`
  embeds it and every Go typecheck needs it. The question is not
  clean-vs-dirty but *whose* dirt.
- **`make lint`'s tag sets must equal `make test`'s.** Without
  `webkit2_41` wails resolves `webkit2gtk-4.0`, which Arch ships and
  Ubuntu 24.04 does not, so lint was checking a configuration that only
  built on one distro. CI caught this on its first run.
- **Playwright's WebKit gates** because it was measured (19/19) rather
  than assumed, and because nothing in `e2e/` compares pixels — so a
  failure is an engine difference, not baseline noise. It is the only
  WebKit2GTK signal obtainable, since it cannot start on Arch at all.

## Known blind spots

- **Xvfb is X11**, and `main.go` carries a Wayland-specific NVIDIA
  DMABuf workaround. CI never exercises that path. Acceptable — it is a
  crash workaround, not a feature — but it is a blind spot, not a
  surprise.
- **Playwright's WebKit is not WebKit2GTK.** Closer than Chromium,
  still not the shipped renderer. A GTK-specific rendering bug can
  escape, and will for any view not in the smoke suite.
- **The fixture hash is deterministic per ffmpeg, not across versions**
  (`5425fbb454a2` on Arch, `599a8dd4f152` on Ubuntu 24.04). Nothing
  asserts a literal hash; a test that did would be portable by accident.

## Left open, deliberately

- **WAV tags are write-only.** `backend/tagwriter` writes them into a
  RIFF `id3 ` chunk; `backend/metadata` reads through `dhowden/tag`,
  which has no RIFF parser, so every WAV scans in untitled. Found by
  the fixtures and pinned by `TestWAVTagsAreNotReadableYet`. The fix is
  small: unwrap the chunk, hand the payload to `tag.ReadFrom`.
- **`themeStore.loadFromBackend`'s failure handler cannot recover** — it
  re-derives the colour ramp from the state that just failed it. One
  line; reachable only if the backend returns an empty accent.
- **`backend/playlist` has no CRUD suite.** 2,900 lines; phase 5 added
  four emit-focused tests. Its own piece of work.
- **Driving the real WebKit2GTK window.**
  `WEBKIT_INSPECTOR_SERVER` exposes WebKit's remote inspector, but the
  protocol is not CDP and Playwright cannot attach. A bespoke client is
  the only route and is not worth it.

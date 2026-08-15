# The harness: event bridge and control surface

Two things ride on top of the headless app. Both exist only in dev
builds; neither is reachable from a shipped binary.

## The event bridge (`.playwright/init-events.js`)

Loaded as an `initScript` by `.playwright/cli.config.json` and by
`e2e/support/fixtures.ts`, so an exploratory session and a committed
spec see an identical page. It records every backend event by wrapping
`window.wails.EventsNotify` — the single choke point all 46 events pass
through, whether or not the app subscribes to them.

```js
window.__yjEvents.wait('LibraryScanComplete', { timeoutMs: 60000 })
window.__yjEvents.names()          // name -> count; use this to find out
                                   // what actually fired before asserting
window.__yjEvents.last('QueueChanged')
window.__yjEvents.since(seq)
window.__yjEvents.reset()          // drop the buffer
window.__yjEvents.ready(20000)     // resolves when a binding round-trips,
                                   // which is later than DOM-ready and true
window.__yjEvents.call('queue.Queue.GetState', [], 5000)
```

- **`wait` resolves against already-buffered events as well as future
  ones**, so there is no race between doing the thing and listening.
- **Install exactly one recorder.** Listeners survive across `eval`
  calls; a second recorder double-counts. Call `reset()`, never
  re-register.
- **`call` times out on purpose.** A binding with wrong argument types
  never fires its callback. A 5 s rejection naming `.dev/app.log` beats
  an infinite hang.

In specs, use the wrappers rather than `page.evaluate`:
`waitForEvent`, `resetEvents`, `eventNames`, `callBinding`, and the
`app` fixture (a page with the bridge installed and the backend
actually answering) from `e2e/support/fixtures.ts`.

## The control surface (`backend/testctl`, mounted at `/__test/`)

Gated twice: behind the `dev` build tag (with a no-op `!dev` twin) and
behind `YJ_TESTCTL=1`, which `scripts/dev-headless.sh` sets and
`make dev` does not.

| Endpoint | Use |
|---|---|
| `GET /__test/health` | is this a seeded dev build, and which library |
| `POST /__test/db/snapshot?name=X` | save the SQLite state |
| `POST /__test/db/restore?name=X` | put it back (see below) |
| `POST /__test/emit` `{name, data}` | force any backend event |
| `POST /__test/sql` `{sql, args}` | read rows, or a write count |

`TestCtl` in `e2e/support/fixtures.ts` is the typed client.

- **`emit` is the fast way to render a push-driven view** without
  staging the work that would produce it — job progress, download
  progress, scan progress. It calls `events.Deliver`, which *errors*
  when the event reaches nobody, so a `200` means it really arrived.
- **`restore` is slow** (~40 s in the suite) because it copies every
  table. Prefer snapshotting once and restoring only when a spec
  genuinely mutates state.

## Traps in the config

- **The two path keys in `.playwright/cli.config.json` resolve
  differently.** `initScript` is relative to the *config file's*
  directory (`"init-events.js"`, not `".playwright/init-events.js"`);
  `outputDir` is relative to the *shell's cwd*. Set `outputDir` to
  `".playwright-cli"` and run `playwright-cli` from the repo root, or
  snapshots land somewhere neither `.gitignore` nor your next `ls`
  will find, and you will read a stale one from a previous session
  and think a component regressed.
- **`snapshot` writes a file, it does not print the tree.** The
  command prints a path under `outputDir`; read that. Only the tail
  is echoed.
- **Two separate browser caches.** `@playwright/test`
  (`make e2e-setup`) and the Vitest provider (`make ui-setup`) each
  download their own Chromium. One working is no guarantee for the
  other. There used to be a third: `playwright-cli` was a *required*
  dependency because `scripts/seed-sandbox.sh` drove `AddLibrary`
  through a real page, `window.go` being v2's only way in. v3 answers
  the same call over HTTP, so the seed is `curl` now and the CLI is
  only an exploratory convenience.
- **`getByRole('button', { name })` matches substrings.** "Play" also
  matches "Add queue to playlist"; transport controls need
  `exact: true`.
- **`e2e/` is its own npm package** with `"type": "module"`. Without
  that, Playwright transpiles the specs to CJS and every `import.meta`
  throws — reported, unhelpfully, as "No tests found".

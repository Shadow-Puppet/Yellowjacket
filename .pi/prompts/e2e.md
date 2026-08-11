---
description: Promote a hand-driven playwright-cli session into a committed spec in e2e/
argument-hint: "[name of the flow]"
---
Promote the flow I just drove by hand into a committed Playwright spec.
Flow: ${@:-infer it from the playwright-cli commands in this session}

This is a transcription with fixed substitutions, not a fresh test.
Work from what actually happened in this session, not from what the UI
looks like it should do.

**1. Recover the flow.** List the `playwright-cli` calls made this
session, in order, and the assertion each one was really checking. Then,
before writing anything, ask the running app what fired:

```
playwright-cli -s=yj eval "() => window.__yjEvents.names()"
```

Await the events that are actually in that list. Do not guess event
names from `backend/events/`.

**2. Substitute, one for one.**

- `click e15` → a role or `data-testid` selector. Snapshot refs are
  per-snapshot and meaningless in a spec. If the only stable selector
  would be structural, add a `data-testid` to the Lit component and
  re-run `make ui-test`.
- any sleep, or "it looked settled" → `waitForEvent(app, 'X')`.
- `window.go.…` → `callBinding(app, path, args)`, which times out.
- a short fixture track → `LONG_TRACK`, if the flow needs playback to
  still be running on the next line. Every other fixture is 2–6 s.
- `getByRole('button', { name })` → add `exact: true`.

**3. Place it.** `e2e/specs/<area>.spec.ts`, importing `test`, `expect`
and the helpers from `../support/fixtures.js` — never `@playwright/test`
directly. Match the surrounding specs' comment style: say what the test
is protecting against, not what the lines do.

**4. Prove it is a spec and not a recording.** Three runs, in order:

```
make e2e E2E_ARGS='--grep "<name>"'    # it passes
make e2e E2E_ARGS='--grep "<name>"'    # again — catches dependence on
                                       #   state the first run left
```

then once more after restoring the database through `/__test/`, which
catches dependence on state *my hand-driving* left behind — the single
most likely way a promoted spec passes here and fails in CI. Leave the
database as you found it: snapshot/restore, or reset in `beforeEach`.

**5. Then the whole suite:** `make e2e`. If the promotion turned up a
new trap, append it to `.planning/NOTES.md`; if it turned up a bug,
tell me rather than asserting the broken behaviour.

# Work log

Temporal memory: what happened and what's next. Structure lives in
`CLAUDE.md`, operational instructions in `.pi/skills/yellowjacket-dev/`,
measured discoveries in `.planning/NOTES.md`. Don't duplicate those here.

## Current state

Plan 005 (agent development harness) is **complete — all seven
phases**. Everything from phase 1 onward is still **uncommitted**: one
large but coherent working-tree diff, nothing pushed.

All four tiers verified green from a cold, cleaned state:
`make ui-test` 313 passed, `make lint` 0 issues × 3 configurations,
`make test` green × 3 passes, `make e2e` 19 passed. Both CI jobs
verified green in a bare `ubuntu:24.04` container, including 19/19 on
WebKit.

- [ ] **Push `.gitea/workflows/ci.yml` and confirm with `gitea_ci`.**
      Nothing has run on the shared runner yet — the whole workflow was
      validated locally in Docker. A run that never starts looks
      identical to one that passed, so the push is not done until
      `gitea_ci` says so.
- [ ] Decide whether to commit phases 1–7 as one commit or split by
      phase (waiting on the user; nothing is committed yet).

Unverified on the real runner, and the likeliest first failures:
`actions/upload-artifact` needs node in the container (it is installed
by an earlier step, and the step is `continue-on-error`, so a failure
there cannot mask a real one); and the `npm_config_store_dir` pnpm
cache is best-effort — if pnpm ignores it we lose warmth, nothing else.

Open items deliberately not fixed: WAV tags are write-only
(`TestWAVTagsAreNotReadableYet`), `themeStore.loadFromBackend`'s failure
handler cannot recover, `backend/playlist` has no CRUD suite.

## Log

### 2026-08-11 — cold skill run, then phase 7 (CI)

- **Followed the skill cold first**, as the last session asked. It
  works: app up from a wiped `.dev/`, an undocumented flow driven
  (queue panel + shuffle, asserted on `QueueModeChanged`), stopped —
  ~1 minute, no dead ends. One real config bug: `outputDir` in
  `.playwright/cli.config.json` resolves against **cwd**, not the
  config file's directory (only `initScript` does that), so snapshots
  were landing above the repo and a *stale* one from the previous
  session answered `ls -t` instead. That cost a DOM walk to disprove a
  regression that did not exist. Four smaller doc gaps fixed
  (`sandbox-seed` already runs `testdata`; `ui-setup`/`e2e-setup` were
  undocumented prerequisites; `snapshot` prints a path; `dev-stop`
  leaves the browser open), plus `dev-headless.sh`'s own banner, which
  was suggesting the bare `window.go` call its next paragraph warns
  against.
- **Built both CI jobs as container scripts before writing any YAML**,
  then transcribed the YAML back out and re-ran it to prove the
  transcription. Push-and-see is a bad loop on a self-hosted runner.
- **It found a real bug immediately**: `make lint` omitted
  `webkit2_41` on all three passes, so it was linting configurations
  nothing builds. Invisible on Arch (which still ships
  `webkit2gtk-4.0.pc`), fatal on Ubuntu 24.04. Tag sets now match
  `make test`.
- **Both open decisions settled by measurement**: ALSA `null` PCM for
  audio (no daemon; the elapsed clock really advances), dead-address
  stub for the explore artifact (and setting it for the *app* run, not
  just seeding, is worth 8x on suite wall clock). **WebKit is a
  required step** — it had never been run anywhere, so one throwaway
  container run replaced a coin flip with 19/19 at +11 s.

### 2026-08-10 — phase 6, pi affordances

- Added `.pi/skills/yellowjacket-dev/` as a directory rather than a flat
  file: only the description is always in context, so `SKILL.md` stays
  short enough that reading it whole is never a decision, and the deeper
  material sits in `references/{harness,fixtures,ui-tier,schema-change}.md`.
- Settled the CLAUDE.md-vs-skill split **grammatically, not topically**,
  because a topical split is what rots — every new fact gets two
  plausible homes. Three docs, three tenses: NOTES.md is past
  (measured, dated, append-only), CLAUDE.md is present (what the system
  is), the skill is imperative (what to run). A new paragraph's tense
  decides where it goes.
- The five gotchas (binding timeouts, first-run wizard, `pkill -f`,
  seeds-by-running, WebKit-is-CI-only) went **inline in SKILL.md**, not
  into a reference: you need them before the failure, not after.
- Trimmed CLAUDE.md's "Fixtures and the headless harness" section by
  about half — the command sequences and gotchas it was carrying are now
  the skill's, and leaving both would have created exactly the duplicate
  description this repo has a standing rule against.
- Added `make skill-check` / `scripts/skill-check.sh` + a pre-commit
  hook: every command in `.pi/**/*.md` must be a real `make` target, so
  the Makefile stays the source of truth for invocation and a renamed
  target fails a commit instead of misleading an agent later. Verified
  it fails (it caught its own not-yet-created target) and passes.
- Added the `/e2e` prompt template: promoting a hand-driven
  `playwright-cli` session into a spec is a transcription with four
  fixed substitutions (refs → testids, sleeps → `waitForEvent`, raw
  `window.go` → `callBinding`, short fixture → `LONG_TRACK`), plus three
  runs — pass, pass again, pass after a DB restore — because the usual
  failure is a spec depending on state the hand-driving left behind.
- One shell trap: under `set -euo pipefail`, `x="$(make -pqRr | …)"`
  fails the whole assignment, because `make -q` exits non-zero when a
  target is out of date and `pipefail` propagates it.

### Earlier

Phases 1–5 of plan 005: fixture generator and manifest, headless launch
and seeds, the event bridge + `data-testid` pass + `backend/testctl` +
`e2e/`, the Vitest component tier + `make bindings-check`, and the
`events.Emit` wrapper with its in-process service-event tests. Recaps
and the five "verified end to end" blocks are in
`.planning/plans/active/005-agent-development-harness.md`; the lessons
are in `.planning/NOTES.md`.

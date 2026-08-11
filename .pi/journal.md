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

**Committed and pushed** as `5ca6cad` (the harness) + `ccacd67` (a CI
fix), and **green on the real runner**: job `check` ~4 min, job `e2e`
~3 min with 19/19 chromium *and* 19/19 webkit. One commit rather than
seven because the working tree was the end state, not per-phase
snapshots — `Makefile`, `CLAUDE.md` and `lefthook.yml` are touched by
nearly every phase, so a split would have been fabricated history.

Still unverified, because no run has failed yet: the
`actions/upload-artifact` step (`continue-on-error`, so it cannot mask
a real failure) and whether pnpm honours `npm_config_store_dir` for
store caching. Worth checking the next time a spec legitimately fails.

- [ ] `gitea_ci`'s `job_logs` returns 404 on Gitea 1.27.1 — the endpoint
      is not exposed. Logs come from the VPS instead: `zstdcat` the file
      under `gitea/actions_log/<owner>/<repo>/<xx>/<task_id>.log.zst`,
      and note `zstdcat` is not in the gitea container, so
      `docker cp` it out first. Job status is `action_run_job.status`
      (1 success, 2 failure, 4 skipped, 5 waiting, 6 running).
      Probably belongs in the `gitea` skill, not here.

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

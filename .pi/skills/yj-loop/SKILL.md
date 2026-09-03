---
name: yj-loop
description: Operating the autonomous backlog loop — the crank that works the YellowJacket tracker one issue at a time (tick mechanics, the state machine in Gitea, which agent and model take each leg, the escalation ladder, merge authority and the rails that stop it doing damage). Use whenever a scheduled tick fires, and when piloting or debugging the loop.
---

# The YellowJacket backlog loop

Design and arguments: `.planning/plans/active/020-autonomous-backlog-loop.md`.
This skill is the **operating procedure**; the plan is the reasoning.
`yellowjacket-dev` is the harness doctrine (tiers, seeds, traps); this
skill is the loop doctrine (who acts, on what model, with what authority).
Read the plan first, once. Then this file every tick.

## The one-sentence discipline

**Every leg is a fresh subagent session on a pinned tier; the token, the
tracker and the loop worktree are the only things passed between legs.
Never switch a model mid-session, never let two writers exist at once,
never keep state in a conversation.**

## Tick skeleton

A tick is one leg of the state machine, and the leg is picked by
reconciling first. Execute in this order:

1. **Lock.** `/tmp/yj-loop.lock` holds `pid + start-iso`. If a live
   process owns it and is younger than 2 h: exit immediately, report
   "tick skipped (lock held)". If the PID is dead, take the lock.
   Remove it before every exit.
2. **Reconcile.** Fresh reads, never cached: open issues
   (`scripts/issue.sh list`), PRs and CI via the REST API, branches via
   `git ls-remote --heads origin`, `.pi/loop/state.json`. GITEA_TOKEN
   refusing = the tick reports and exits; the identity rails below are
   not optional.
3. **Pick the leg.** See the state machine below; the leg follows the
   issue's lifecycle (claim→plan→…→merge→…→housekeep). Exactly one leg.
4. **Execute** — the leg table below says who acts and what they must
   return.
5. **Journal** — one line per tick in the state file (issue, leg, result,
   tick cost if leg reports it).
6. **Report** — three lines: issue taken or continued, its state now,
   anomalies. Then stop. A tick that reports is a tick that can leave a
   conversation behind.

## The state machine

The tracker is the truth. The state file (`.pi/loop/state.json`,
gitignored) is an index plus flags (`emulator`, `drain`); the tracker
wins every disagreement.

| Stage | Where it lives | Leg → actor |
|---|---|---|
| selected | nothing written until claim is possible | select |
| in flight | `Status/In Progress`, assignee, comment with branch+approach | claim (orchestrator, `scripts/issue.sh`) |
| plan done | plan as an issue comment | plan |
| implemented | commits on `origin/<branch>` | work |
| validated | handoff + a comment on the issue summarizing evidence | validate (+ visual) |
| critiqued | review findings applied or argued; fix commits on the branch | review + diffreview, fix round by work |
| shipped | PR open, body per the contract, CI green | ship (orchestrator + scribe) |
| merged | PR merged, issue closed (footer verified) | merge (orchestrator) |
| done | diary entries, unclaim happened | diary (scribe) |
| cleaned | stale own branches/PRs handled | housekeep (orchestrator, daily) |

## Legs and their agents

Delegation is by agent name; the model is pinned in the agent file and is
**not** an argument. Every leg prompt names: the issue, the evidence so
far (plan comment, handoffs), what the leg must produce, and its stop
rules. Never "go fix it" — the leg contract is in this file.

| Leg | Agent | Model (tier) | Produces |
|---|---|---|---|
| gather/mechanical dump | `yj-loop.inspect` | go/mimo-v2.5 (T0) | tracker/PR/CI/branch digest, verbatim |
| select next issue | `yj-loop.select` | glm/glm-5.3 (T2) | one issue + reasons, or "nothing qualifies" |
| plan | `yj-loop.plan` | glm/glm-5.3 (T2) | a plan comment on the issue |
| implement | `yj-loop.work` | qwen/deepseek-v4-pro-0813 (T1) | commits + a handoff (see contract below) |
| validate | `yj-loop.validate` | glm/glm-5.3 (T2) | pass/fail with evidence per acceptance item |
| visual evidence | `yj-loop.visual` | glm/glm-5.3-flash (T2) | what the screenshot actually shows |
| consequences review | `yj-loop.review` | glm/glm-5.3 (T2) | blockers / fix-worthy / optional findings |
| understood-diff review | `yj-loop.diffreview` | qwen/deepseek-v4-pro-0813 (T1) | same shape, scope-tight |
| escalation | `yj-loop.escalate` | go/kimi-k3 (T3) | same leg re-run, seeded with failure summary |
| prose (PR body, commit msgs, journal) | `yj-loop.scribe` | go/mimo-v2.5 (T0) | text only, from supplied facts |

Orchestrator-only legs: **claim** (`issue.sh claim --branch` — atomic,
refuses if held), **ship's PR/CI polling** (REST API below — `gitea_ci`
job_logs 404s on this Gitea; the REST endpoints are the way), **merge**
(API below), **housekeep**.

## Selection rules (`select`)

The rules from `.pi/prompts/next-issue.md` stay — priority order, #73's
sequence overriding labels where it speaks, skipping `Status/*` states
that mean busy, branch-collision check, verifiability, flakes. The
emulator flag **adds** emulator-verifiable Android issues; it never
reaches device-only ones. A "nothing qualifies" answer is a correct
tick, not a failure — report it and stop.

## The implementation contract (`work`)

The worker implements **from the plan comment**, in the loop worktree,
on the claimed branch, and nothing else:

- runs the tiers the change demands (`yellowjacket-dev` decides which —
  the loop never outvotes it), including `npx tsc --noEmit`;
- e2e only if `ss -ltn | grep 34115` is empty; `make dev-headless
  SEED=default` before and `make dev-stop` after;
- discoveries outside the issue become new issues (`issue.sh new`), never
  bigger diffs; a materially-larger-than-implied issue stops the leg with
  a comment and a label removal, not a hail-mary;
- handoff must state: changed files, what was left undone, commands run
  with exit codes, verification evidence, surprises, decisions needing
  approval. A handoff without that list is a failed leg.

## Validate and critique

Validation is **claim-first**: re-read the issue, then check each piece
of evidence against the acceptance items; a green suite that never
touched the reported surface is a finding. Screenshots go to `visual`,
never to a text-only tier.

Critique is the standing fan-out (`subagent` parallel: `yj-loop.review`
consequences + `yj-loop.diffreview` scope-tight, both fresh). The
orchestrator synthesizes: blockers and fix-worthy findings go back to
`work` as one bounded fix round (maximum three rounds total; then the
issue gets a `⟦loop⟧` comment stating what will not be fixed and why,
and the ship leg proceeds unless a finding is a blocker). Reviewers do
not edit files.

## Escalation ladder

When a leg fails twice on its tier, do not re-prompt bigger:

1. The failing session writes its summary: what it tried, what failed,
   what it observed.
2. A **new** session on the next tier up is seeded with that summary and
   the original leg contract.
3. T3 is the ceiling: fresh session, never parallel, **once per day**.
   A day's escalation is spent — the issue waits until tomorrow.

Routing down is free; routing up is the budget.

## Ship and the PR body contract

Push the branch (SSH; never to `main`, never force). The PR body —
written by `scribe` from the validator's and reviewers' output — states:
what the issue was, what changed and why, **which verification tiers ran
and their results**, what was deliberately not done, the commit-to-issue
table, and `Closes #n`. `Closes` also sits one-per-line in a commit body
**inside the branch** — both, regardless of merge strategy, because the
pairing was measured.

Poll CI until `check` and `e2e` finish. On failure: read the log via
`GET /api/v1/repos/yonlu/yellowjacket/actions/runs/<run>/jobs` (per-step)
and `…/actions/jobs/<id>/logs` (full). Fix on the branch. **Two
consecutive identical failures = stop**: comment what is known on the
PR and the issue, leave both, report. Do not burn ticks on a red wall.

## Merge authority

Merge when, and only when, **all** hold:

- the PR was opened by this loop (it is in the state file's index);
- the protection contexts `CI / check` and `CI / e2e` are green on the
  PR's head, read from the API, not from the PR page's badge;
- the PR reports mergeable;
- the critique leg ran and no open blocker stands.

```
curl -sS -X POST -H "Authorization: token $GITEA_TOKEN" \
  -H "Content-Type: application/json" \
  https://git.ljones.me/api/v1/repos/yonlu/yellowjacket/pulls/<n>/merge \
  -d '{"Do":"merge","merge_message_field":"default","force_manually_merged":false}'
```

Afterwards: `scripts/issue.sh list --state open` and check the footer
took. Close stragglers with `issue.sh close`, naming the merge commit.
`unclaim.yml` handles the label; it is not instant; reopening does not
restore it. Merging fans out to nothing (releases are the manual
`release.yml`, which the loop never runs) — the criticism stands before
the merge because nothing stands after it.

## Rails — the loop's absolute rules

1. **Touch only its own.** Issues it claimed, branches it made, PRs it
   opened. `issue.sh claim` enforces the front gate; never work around a
   refusal.
2. **One writer, one issue.** The loop worktree is the only dirty tree.
3. **Never merge a PR it did not open.** Any merge that violates this is
   a hard stop.
4. **Human work is holy.** Human branches, PRs, assignees: leave exactly
   as found. Cleanup never names them.
5. **The token is identity.** If GITEA_TOKEN misbehaves, the tick stops.
6. **New findings are new issues**, never scope creep. The tracker
   vocabulary (`Kind/`, `Area/`, `Priority/`) stays intact in one
   taxonomy; use `scripts/issue.sh new` with correct labels.
7. **Conventional Commits**, enforced by `scripts/commit-check.sh`; the
   type list and `.releaserc.yml`'s must agree — a loop commit is a
   release grammar token even after months of no manual releases.
8. **Tiers over vibes.** `yellowjacket-dev`'s tier table decides what a
   change must pass; a skipped tier is stated, never silent.
9. **Two strikes on CI, three rounds of critique, one kimi a day.** The
   loop's patience is finite on purpose.
10. **Every leg writes its evidence.** A leg that leaves nothing behind
    is indistinguishable from a leg that did not run — which is how the
    next tick re-does it.
11. **The loop may not re-schedule itself** (the scheduler refuses it
    anyway — treat as an invariant, not a limitation).
12. **Drain means drain.** `drain: true` = finish in flight, take
    nothing new, then stop.

## Emulator mode

Flag `emulator: true` in the state file **and** an already-booted
emulator (`adb devices` answers) opts in: `make android` (build), `make
android-install`, `make android-smoke` (crash check — the same pid
surviving is the only signal that means started), `make
android-screenshot` and `make android-eval` as evidence for `visual`.
The loop never boots or stops an emulator; that is the user's machine.
Device-only issues stay open under either setting. One-time setup the
user performs: `make android-setup` (~3.5 GB, creates the `yj-test`
AVD), then `make android-emulator` per session.

## ON / OFF / drain

- **Worktree:** `git worktree add ~/.paseo/worktrees/loop/jumpy-hound
  origin/main` (from any clone; branch from origin/main in the loop
  tree, never `git checkout main`).
- **Session:** pi in that worktree, `/name loop`. Add the job via
  `/schedule-prompt` (name `yj-loop`, cron
  `0 0 10-18 * * 1-5`, prompt: "Read `.pi/skills/yj-loop/SKILL.md` and
  run exactly one tick. Stop.") — session-bound by default.
- **OFF:** toggle the job, or close the session. **ON:** `pi --resume
  loop` in the worktree, job enabled. Courses of the tick appear in
  that session's transcript.
- **Tune in:** the same resume. Talk to it only between; a tick is
  atomic.

## Troubleshooting

- `issue.sh: GITEA_TOKEN is not set` or a 401 — the token is the whole
  identity (rails 5). Stop, do not fall back to anything.
- `gitea_ci`'s job log 404s — the REST endpoints above answer; this is
  a Gitea build, not a fault.
- A spec fails that the tier doc says can fail from stale backend state
  — restart the app tier before believing it (`yellowjacket-dev`).
- A tick that "did nothing" — reconcile again; the tracker usually says
  which leg it really is.
- The job did not fire — the scheduler fires only while a session is
  open in its directory (documented); "the loop is off" is the correct
  reading, not a bug.
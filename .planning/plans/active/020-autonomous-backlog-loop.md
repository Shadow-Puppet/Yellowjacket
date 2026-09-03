# 020 — The autonomous backlog loop

**Issue:** #236 (`Kind/Enhancement`, `Priority/Low`)
**Status:** active — phase 0, supervised pilot
**Relates:** #73 (the roadmap the loop follows), plan 005 (the harness the
loop drives). Cost and model-tier doctrine is the `pi-session-reference`
card handed to the session that designed this; the loop's copies of it
are deliberate one-paragraph summaries, not the authority.

A pi coding-agent configuration that, toggled on, works the Gitea tracker
one issue at a time — triage, claim, plan, implement, validate, critique,
PR, CI, merge, verify-close, diary — and then does it again. The tracker is
the state machine: whoever reads Gitea sees exactly where the loop is,
which is the property this document's rails exist to protect.

---

## The shape: a crank, not a resident brain

Half the design is that **nothing lives in a conversation**. Each tick is a
fresh, bounded unit of work; every transition writes evidence to Gitea
(label, comment, branch, PR) or to the loop's own state file; a tick that
dies mid-leg loses nothing, because the next tick resumes from what Gitea
says.

The other half is that **no leg trusts the one before it**. The worker
implements from the plan, not from the issue alone; the validator checks
the *claim*, not the green CI row; the merger merges only after reading the
protection contexts itself; the diary leg is what makes the next issue's
triage cheaper.

One issue in flight at a time. That is a pacing decision, not a
concurrency limit of the tooling — CI has a capacity-1 runner and the e2e
tier owns one headless port on this machine, so two writers would serialize
on infrastructure they cannot see and appear to be doing fine.

## The state machine

| Leg | Writes | Actor / model |
|---|---|---|
| reconcile | — | orchestrator + `inspect` (mimo-v2.5) |
| select | nothing on the tracker; decision logged in the tick transcript | `select` (glm-5.3) |
| claim | assignee + `Status/In Progress` + comment naming branch & approach | `scripts/issue.sh claim` |
| plan | plan as an issue comment | `plan` (glm-5.3) |
| implement | commits on the issue branch, in the loop worktree | `work` (qwen/deepseek-v4-pro-0813) |
| validate | verification evidence in the handoff | `validate` (glm-5.3), `visual` (glm-5.3-flash) for screenshots |
| critique | review findings; fix commits | `review` (glm-5.3) + `diffreview` (qwen) + fix round by `work` |
| ship | push, PR with body contract, CI read + fixes | orchestrator + `scribe` (mimo-v2.5) |
| merge | the merge; post-merge issue verification | orchestrator |
| diary | `.pi/journal.md`, `CLAUDE.md` if structural | `scribe` |
| housekeep | stale-branch/PR cleanup, state-file prune | orchestrator |

### Legs that are the orchestrator's alone

The orchestrator (the loop session) delegates every deliberative leg and
keeps three for itself because they are script-shaped and must not be
re-implemented by a model: claim (`issue.sh claim`, which refuses when
someone else holds the issue — the backstop), merge (API calls below), and
housekeep (branch deletion). If a tick does nothing else, it reconciles.

## Model routing

The routing authority is the card's four tiers, reproduced here as the
loop's assignment, not as an argument:

- **T0 `go/mimo-v2.5`** — mechanical gathering, commit/PR/journal prose,
  any fan-out. Effectively free; wrong only where wrongness costs a
  debugging session, so nothing above takes its word for a *fact*.
- **T1 `qwen/deepseek-v4-pro-0813`** — implement-from-a-written-plan,
  understood-diff review, the orchestrator itself. The default session
  model; half price 10:00–20:00 EDT, which the cron is shaped around.
- **T2 `glm/glm-5.3`** — repo-scale reasoning: selection, planning,
  consequences review, validation judgement. Weekly credits with no
  rollover: the loop draws them every week by construction, which is the
  correct posture. **`glm-5.3-flash`** for anything multimodal
  (screenshots, UI inspection).
- **T3 `go/kimi-k3`** — escalation only: two lower tiers already failed,
  or the issue is a named gnarly one. A fresh session seeded with the
  failing tier's own summary, never a mid-session switch, never parallel,
  at most once per day.

The invariant behind all four, from the card: **routing down is cheap,
routing up is expensive.** An implementation that stalls is escalated by
having the T1 session write *what it tried, what failed, what it observed*
and handing that to a new session one tier up. Escalating a session in
place is forbidden in both directions.

Fan-out is allowed on T0 and T1 only (the Go plan's $12/5 h constraint
makes T3 fan-out self-defeating). Critique is the one standing fan-out:
two reviewers, two angles, one synthesis.

## Scheduling

`0 0 10-18 * * 1-5` (local = EDT): hourly on weekdays inside Qwen's
half-price window, clear of the card's ⚠ 2–6am band (DeepSeek peaks, GLM
loses its off-peak discount — the window the old `yj-backlog` cron sat in,
which this replaces as the loop supersedes it).

- A tick takes a lock (`/tmp/yj-loop.lock`, PID + timestamp). An overrun
  tick makes the next fire exit immediately; serialization survives
  whatever the scheduler does with overlapping fires.
- ~9 ticks/day; an issue is 2–5 ticks; **one to two issues per day** is
  the natural rate. That also paces the bills without a budget flag.
- The port check is part of reconcile: if `34115` is occupied, the tick
  refuses any leg that needs the headless app and defers to the next
  tick, without complaint. A human's interactive tier always wins.

## Runtime and ON/OFF

The scheduler (`pi-schedule-prompt`) fires only while a pi session is open
in the job's directory — that limitation is the switch:

- **Worktree:** `git worktree add` a dedicated clone at
  `~/.paseo/worktrees/loop/jumpy-hound`. Loop edits happen only there; a
  dirty tree there is the loop's business and nobody else's. **Provision
  it once before its first push:** `make build-frontend` + `make testdata`
  — the pre-push `go-test` hook needs both and refuses without them.
- **Session:** pi in that worktree, `/name loop`. The job is bound to that
  session, so another pi elsewhere in the same directory does not
  double-fire it.
- **ON:** resume the loop session (`pi --resume loop`) and enable the job.
  **OFF:** toggle the job off in `/schedule-prompt`, or close the session.
  **Drain** (stop taking new work, finish in flight): set `drain: true` in
  the state file.
- **Tune in:** the same `pi --resume loop` — the chat transcript *is* the
  loop's log, each tick's reasoning inline, each leg reporting in.

## Identity, claims, and what the loop may touch

The loop operates **as the owner** via `GITEA_TOKEN` (scopes: `read:user`,
`write:issue`, `write:pull`, `write:repository`); pushes ride SSH and need
no token. Every tracker comment the loop writes is prefixed `⟦loop⟧`, so
the collaborator reads it as the pump and not as a person.

It may only ever touch work it created: issues it claimed, branches it
made, PRs it opened. Two mechanisms make that enforced rather than
intentional: `issue.sh claim` refuses an issue somebody else holds, and
reconcile checks `git ls-remote --heads origin` so a branch name collision
from a concurrent session is caught before the first edit.

## Merge lifecycle

- **Only PRs the loop opened.** A collaborator's PR is never merged, never
  commented on for pressure, never touched.
- **Every branch is refreshed against main before its merge**, in the
  loop worktree — the refresh is where a textual conflict surfaces, as
  diff text: hunks the loop authored are resolved there, anything else
  is left to a human with a `⟦loop⟧` comment. The protection's
  `block_on_outdated_branch` makes the refresh mandatory for adopted
  (pre-loop) branches: behind `main`, a PR cannot merge at all.
  Required contexts are re-polled on the refreshed head.
- **Merges are one at a time**, each re-reading state — the previous
  merge moved `main`, and the next PR's mergeability is recomputed at
  its own turn.
- **Post-merge, the `push` run on `main` is watched.** A red main after
  a loop merge halts the loop. That run is the only guard against the
  class no mergeability check sees: two PRs touching the same file,
  merging cleanly, contradicting each other.
- The gate is the protection rule itself, read from the API: contexts
  `CI / check*` and `CI / e2e*` green, PR mergeable. (Required approvals
  is 0 today; if a second person changes protection rules, the merge
  endpoint refuses and the tick stops and reports — human business.)
- `Closes #n` goes **in a commit body inside the branch, one line per
  issue, and in the PR body**. Both, because a squash route and a merge
  route parse different texts, and this pairing was measured: a comma
  list partially matched, five of ten issues.
- After merging: verify against `issue.sh list --state open` that the
  issue actually closed; close any straggler naming the merge commit.
  `unclaim.yml` strips `Status/In Progress` automatically; it is not
  instant, and a re-open does not restore it — the verification is
  against the open list, not against the label.
- Merging to `main` fans out to nothing: releases are the manual
  `release.yml`, which this loop never runs. The blast radius of a
  merge is the main branch's CI, and the critique leg is what stands
  before it.

## Verification contract

The tier table is `yellowjacket-dev`'s; the loop re-states nothing above
it except the *division of duty*: the worker runs the tiers the change
demands, and the validator re-reads the issue and checks that the tier
evidence actually answers the claim — a green suite that never touched
the reported surface is a finding, not a pass. Cosmetics are read by a
model that can see (`visual`, the multimodal tier); a change that moves
geometry refreshes its `ui-visual` baseline in the same commit.
`tsc --noEmit` is part of the gate and nothing else runs it. The e2e app
is seeded (`SEED=default`) and stopped after.

## Android / emulator mode

The loop is **device-free by default**: issues whose verification is
physical-device behaviour stay open for humans (the repo's own tags say
which those are). One step of the ladder exists for the rest:

- `{"emulator": true}` in `.pi/loop/state.json` **plus an already-booted
  emulator** (`adb devices` answers) opts the loop into building the APK
  and using `android-smoke` (crash verification), and `android-screenshot`
  / `android-eval` as rendering evidence for `visual`.
- The loop **never boots or stops an emulator** — that is the user's
  machine and their gesture. Boot it with `make android-emulator`
  (one-time `make android-setup`, ~3.5 GB, creates the AVD), and
  `make android-emulator-stop` when done.
- Real-device-only issues are skipped under either setting.

## Budgets and pacing

Expected spend: dominated by the T1 implementation leg inside the
half-price window (pennies to tens of cents) and T2 on weekly credits;
T3 bounded at one fresh call per day. The card's numbers ($12 per rolling
5 h, $30/week as burst headroom not allowance, GLM reset weekly) are the
sanity cells; the loop's own weekly check compares against them rather
than against the month.

## Cleanup (housekeep leg, once per day)

- Loop-owned branches whose commits are in `origin/main`: deleted, local
  and remote.
- Loop-owned PRs open >7 days or red on a second identical CI cause:
  commented with what is known (`⟦loop⟧`), and left — never silently
  deleted.
- Anything not the loop's (assignee, branch, PR): left exactly as found.

## Pilot phases

- **P0 — supervised.** One tick, user watching the transcript: reconcile,
  select, claim, plan. No merge.
- **P1 — observed.** Two ticks ending in the loop's first merge, watched
  through CI → merge → verify-close.
- **P2 — unattended.** The schedule left on. Weekly check against the
  card's two-minute ritual.
- **Hard stops** (any of these halts the loop and leaves a comment, never
  a silent retry): a tick dies twice with no explanation; a merge happens
  for a PR the loop did not open; spend outside the cells above by 2×.

## Not now, on purpose

- **Parallel worktrees** — blocked on e2e's exclusive port; viable only
  with per-worktree headless ports or CI-only e2e. The shape (
  supervisor + per-issue worktrees) is the target, not the first cut.
- **Weekend batch refactors** — DeepSeek off-peak is real but is a
  scheduling knob on top of a working pump.
- **More chain files** — the critique fan-out is a chain; the rest stay
  orchestrator-legs until two weeks of unattended runs say which legs
  are actually fixed-shape.
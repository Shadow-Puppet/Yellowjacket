---
description: Take on the next actionable backlog issue end to end, and stop
---
Take on exactly one issue from the YellowJacket backlog, end to end, and stop.

Repo: yonlu/yellowjacket at https://git.ljones.me — API base
https://git.ljones.me/api/v1/repos/yonlu/yellowjacket, auth with
`-H "Authorization: token $GITEA_TOKEN"`. Default branch is `main`.

## 1. Orient before you pick

Read, in this order: `CLAUDE.md` (the architecture and the reasons behind
it), `.pi/journal.md` (what happened last), `.planning/NOTES.md` (what was
already considered and rejected), and `.planning/plans/active/`. Do not skip
this because the issue looks small — most of this codebase's traps are
written down in exactly one of those four places, and the ones that bite are
the ones you didn't read.

## 2. Pick the issue

List open issues. Choose the single highest-value one that is *actionable
right now*:

- Order by `Priority/Critical` → `High` → `Medium` → `Low`. Within a tier,
  prefer `Reviewed/Confirmed`, then `Kind/Bug` over `Kind/Enhancement` over
  `Kind/Feature`.
- Consult issue #73 (the roadmap) — if it sequences the candidates, that
  ordering wins over the label ordering.
- **Skip** anything labelled `Status/Blocked`, `Status/In Progress`,
  `Status/Abandoned`, `Reviewed/Won't Fix`, `Reviewed/Duplicate`,
  `Reviewed/Invalid`, or already carrying an open PR.
- **Skip anything someone else is already on.** The label is not the only
  claim, because a concurrent session may not have applied it — several pi
  sessions run against this repo from separate worktrees under
  `~/.paseo/worktrees/`. Run `git ls-remote --heads origin` and skip any
  issue whose number or slug matches an existing branch (`60-…`,
  `fix/<slug>`). A duplicated fix costs more than a skipped issue.
- **Skip** anything that cannot be verified without hardware you do not
  have: physical-device Android behaviour (audio output, on-device file
  writes, real gesture input). A browser at 424px is not a phone — see the
  Chrome 113 section of `CLAUDE.md`.
- **Skip** intermittent-failure issues unless you can reproduce the failure
  on demand within a few minutes. Chasing a 1-in-3 flake is an unbounded
  task and does not belong in a scheduled run.
- If nothing qualifies, say so, do nothing, and stop. An empty run is a
  correct outcome.

## 3. Claim it

Add `Status/In Progress` to the issue and comment that you are picking it
up. Then branch:

```
git fetch origin && git checkout -b <type>/<short-slug> origin/main
```

`<type>` matches the issue's `Kind` (`fix/`, `feat/`, `refactor/`, `test/`,
`docs/`, `ci/`). Branch from `origin/main`, never by checking out `main`
itself — this repo is worked from several git worktrees at once and `main`
is checked out in one of them, so `git checkout main` fails outright.

## 4. Do the work

Fix the issue that was reported and nothing else. Match the surrounding
code's style. Follow the constraints in `CLAUDE.md` rather than reasoning
from first principles — where it explains why something is shaped the way it
is, that shape is load-bearing and there is usually a test pinning it.

**Anything else you discover becomes a new issue, not a bigger diff.** File
it with the right `Area/`, `Kind/`, `Priority/` labels, describe the
symptom before the theory, and link it from your PR. Scope creep is the
failure mode this instruction exists to prevent.

If the work turns out to be materially larger than the issue implied, stop:
comment on the issue with what you found and what it would actually take,
remove `Status/In Progress`, push nothing, and end the run.

## 5. Verify — the right tier, not the cheapest one

Run `make generate` if you touched `.sql` or `.templ`, and `make bindings`
if you changed a bound Go signature. Then run what the change actually
demands:

- Go change → `make lint` and `make test` (both cover all three build
  configurations).
- Frontend component or store → `make ui-test`.
- User-visible flow → `make e2e` against `make dev-headless`. **Check the
  port first**: `ss -ltn | grep 34115`. If it is occupied, another worktree
  is already running the app — do not start a second one and do not run
  `make e2e`. Attaching to someone else's build produces a green result
  about code that is not yours, which is worse than no result. Either
  choose an issue that does not need this tier, or stop and say why.
- Anything cosmetic or layout-related → look at a screenshot. Several bugs
  in this repo's history were invisible to every assertion and obvious in an
  image.

A tier you skipped is a claim you did not check. If a tier fails for reasons
unrelated to your change, say so explicitly rather than quietly moving on.

## 6. Keep the documentation true

If you changed structure, behaviour, or a constraint, update `CLAUDE.md` in
the same commit. That file is this project's memory; a change that leaves it
describing the old shape is worse than no change. Append a short entry to
`.pi/journal.md` covering what you did, what you verified, and what you left
open.

## 7. Commit and open the PR

Conventional Commits, imperative subject, ≤72 chars, scope optional. The
body explains *why*. Push the branch — never push to `main`, never
force-push.

Open the PR:

```
curl -sS -X POST \
  -H "Authorization: token $GITEA_TOKEN" \
  -H "Content-Type: application/json" \
  https://git.ljones.me/api/v1/repos/yonlu/yellowjacket/pulls \
  -d '{"head":"<branch>","base":"main","title":"<subject>","body":"<body>"}'
```

The body states: what the issue was, what you changed and why, **which
verification tiers you ran and their results**, anything you deliberately
did not do, and `Closes #<n>`.

Then wait for CI (`ci.yml`, jobs `check` and `e2e`) and report the result on
the PR. If it fails, read the log — `gitea_ci`'s `job_logs` 404s on this
Gitea build, so use
`GET /api/v1/repos/yonlu/yellowjacket/actions/runs/<run>/jobs` for per-step
status and `GET /api/v1/repos/yonlu/yellowjacket/actions/jobs/<id>/logs` for
the log — and fix it. Two consecutive failed CI runs on the same cause: stop,
comment what you know on the PR, and leave it for a human.

**Do not merge.** Comment on the issue linking the PR, leave
`Status/In Progress` on, and end the run.

## Finally

Report in three lines: which issue you took, what state it is in
(PR open / CI green / stopped and why), and any issues you filed.

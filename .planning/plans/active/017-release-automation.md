# 017 — Releases that happen by themselves

> **Status: built, not yet run.** Phases 0–4 have landed on this branch;
> phase 5 is the merge itself and cannot be done until then. The old
> `v1.x` tags are already deleted from `origin`. Verified locally against
> a scratch remote: semantic-release computes **0.0.1** from these
> commits and renders correct sectioned notes.
>
> **One thing found by testing that no amount of reading would have
> caught.** `conventional-changelog-conventionalcommits@10` — the current
> release, and my first pin — is silently incompatible with the writer
> `release-notes-generator@14` depends on: the version is right, the tag
> is right, every step reports success, and the release body is a bare
> `## 0.0.1 (date)` heading with **nothing under it**. It is pinned to 9
> in both `release.yml` and `make release-dry`, with the reason written
> beside it. Four of my seven original pins were wrong majors besides;
> they were guesses, and `npm view` was the fix.

The goal in one sentence: **a merge to `main` computes the next version
from the commits it contains, cuts a tag and a Gitea release whose body
is the changelog, and every publishing channel builds that tag.** The
first release under this scheme is `v0.0.1`, and the five existing `v1.x`
tags go.

## What is there now

Measured, not remembered:

- **Five tags and zero releases.** `v1.3.0`, `v1.4.0`, `v1.4.1`,
  `v1.5.0`, `v1.6.0` exist on `origin`;
  `GET /api/v1/repos/yonlu/yellowjacket/releases` returns `[]`. So there
  is no release page to preserve and nothing but the tags to remove.
- **`CHANGELOG.md` is stale and belongs to another repo.** Its newest
  entry is `1.3.0` and every link in it points at
  `github.com/onion-4-dinner/yellowjacket` — it was written by a
  semantic-release run against a GitHub remote this project no longer
  has.
- **`.releaserc.yml` is a complete semantic-release config that nothing
  invokes**, which CLAUDE.md already says in as many words.
- **Root `package.json` is literally `{}`** — the stub left behind by
  whatever was going to run it.
- The triggers today are: `arch-package` on **push to `main`**,
  `homebrew-formula` on **`v*`**, `android-apk` on **`v*`**, `ci` on
  every branch, `index-artifact` on cron/dispatch. So Arch publishes a
  `git describe` version on every merge and the other two publish only
  when a human remembers to push a tag.

## Decision 1 — semantic-release, with `exec` in place of the `github` plugin

**Revised: the first draft of this plan proposed a shell script and the
argument for it does not hold.** Recorded here rather than deleted,
because the reasoning is what the decision rests on.

What I said, and what checking it showed:

- *"The two plugins that would carry the work do not fit."* Half true.
  `@semantic-release/github` genuinely does not speak Gitea's `/api/v1`
  — but the replacement is **`@semantic-release/exec`**, which is
  first-party, published 2026-06, and peer-deps `semantic-release >=24.1`.
  Its `publishCmd` is one `curl` at the Gitea release endpoint with
  `${nextRelease.notes}` as the body. The Gitea-shaped part of this is
  five lines, and the part I proposed to hand-roll — parsing conventional
  commits, ordering semver, rendering grouped notes — is the part with
  the edge cases and none of it is Gitea-shaped at all.
- *"`@semantic-release/git` commits the changelog back to `main`, which
  re-triggers everything."* True, and it is the one real risk — but it
  is a two-line guard (skip the job when `HEAD`'s subject is
  `chore(release):`), not a reason to write a version calculator. That
  guard is needed under **either** design, since either one writes a
  changelog commit.
- *"A Node dependency tree at the root of a Go repo."* The commitlint
  precedent does not transfer. commitlint was a dependency to regex one
  line; this is a dependency to do something with real complexity, it is
  `npx`-only so nothing lands in the repo, and Node is already installed
  in CI for the frontend.
- *"It cannot be told to produce `0.0.1`."* Wrong — that is a property
  of which commits are in the range, not of the tool. Identical under
  both designs. See below.

Note also that **`@saithodev/semantic-release-gitea` is a dead end** and
should not be reached for: last published 2022, depends on `got@10` and
`fs-extra@8`, and declares no peer dependency on semantic-release at all
— i.e. it is untested against anything since v19, against a core now at
v25. `exec` + `curl` is both simpler and maintained.

So `.releaserc.yml` stays, and its plugin list becomes five **first-party**
plugins, all published within the last six months:

| plugin | job |
| --- | --- |
| `commit-analyzer` | the version |
| `release-notes-generator` | the notes |
| `changelog` | writes `CHANGELOG.md` |
| `git` | commits it back |
| `exec` | `curl`s the Gitea release |

The `releaseRules` and `presetConfig` blocks already in the file are
kept verbatim — they are the same bump table `commit-check.sh` already
enforces the grammar for, and nothing about the project's commit
convention changes.

Two mechanical details that decide whether this works at all:

- **semantic-release pushes the tag itself**, as core behaviour, using
  `repositoryUrl`. The remote here is `ssh://git@git.ljones.me:2222/…`,
  which would need an SSH key in CI — so the run passes
  `--repository-url "https://x-access-token:$PACKAGE_TOKEN@git.ljones.me/yonlu/yellowjacket.git"`
  on the command line rather than committing a token to the config.
  **That is also what satisfies Decision 2**: the tag push is attributed
  to a real user, not to the Actions token.
- **The empty root `package.json` (`{}`) goes.** semantic-release does
  not need one when `--repository-url` is explicit, and leaving a
  package manifest at the root of a Go repo invites the npm plugin and
  every tool that looks for one.

Invocation is pinned in the workflow, not installed into the repo:

```
npx --yes \
  -p semantic-release@25 \
  -p @semantic-release/commit-analyzer@14 \
  -p @semantic-release/release-notes-generator@15 \
  -p @semantic-release/changelog@6 \
  -p @semantic-release/git@10 \
  -p @semantic-release/exec@7 \
  -p conventional-changelog-conventionalcommits@9 \
  semantic-release --repository-url "…"
```

(Exact majors get pinned from `npm view` at implementation time;
`conventional-changelog-conventionalcommits` is in the list because both
the analyzer and the notes generator name that preset and neither
depends on it.)

## Decision 2 — how the publish workflows learn about the tag

**Gitea, like GitHub, does not start a workflow from a tag pushed by a
workflow's own token** (go-gitea#33123, and the forum thread it points
at). This is the one load-bearing unknown in the plan.

The remedy is to push the tag with a *user* PAT — `secrets.PACKAGE_TOKEN`
is already in this repo and already used by `arch-package` and
`android-apk` to clone and to publish — so the push is attributed to a
person and the `v*` triggers fire normally. That keeps the three publish
workflows completely unchanged in shape.

**It is verified in phase 5, not assumed.** The fallback, if it does not
fire, is an explicit `POST
/api/v1/repos/{owner}/{repo}/actions/workflows/{file}/dispatches` per
channel from the release job. That needs `workflow_dispatch` (with a
`version` input) added to `homebrew-formula.yml` and `arch-package.yml`;
`android-apk.yml` already has both. **Add those inputs in phase 3
regardless** — a hand-triggered rebuild of one channel is worth having
whether or not the fallback is needed.

The alternative — one `release.yml` with the three publishes as
`needs:` jobs — is rejected: it means either copying ~400 lines of
Android and Arch setup into it or relying on `workflow_call`, and it
puts every merge to `main` behind an up-to-60-minute Android build on a
runner with capacity 1.

## Decision 3 — 1.6.0 → 0.0.1 is a downgrade, and the answer is reinstall

**Decided: no version-code offset, no epoch. The version number stays
honest and existing installs are replaced by hand.** Every channel is a
downgrade and each declines differently, so what to expect:

- **Arch: no upgrade is offered, silently.** `pkgver()` derives from
  `git describe`, so after the wipe it reads `0.0.1.rN.gHASH`, which
  pacman orders *below* the `1.3.0.rN.*` in the registry. `pacman -R
  yellowjacket && pacman -S yellowjacket` is the remedy. (`epoch=1` in
  the PKGBUILD would have avoided it for one line — but an epoch can
  never be removed, and it puts a permanent `1:` in front of every
  version string this project will ever have.)
- **Homebrew: no upgrade is offered, silently.** Brew has no epoch at
  all. `brew uninstall yellowjacket && brew install …`.
- **Android: a hard refusal.** `versionCode` is
  `maj*10000 + min*100 + pat`, so `0.0.1` is **1** against the **10300**
  an installed 1.3.0 carries, and the install fails with
  `INSTALL_FAILED_VERSION_DOWNGRADE`. Uninstall first — **and that takes
  the app's library and config with it**, which is the same data loss
  `android-apk.yml`'s keystore guard exists to prevent, arrived at from
  the other direction. The workflow's own `code -le 0` guard still passes
  at 1, so nothing in CI stops or warns about this.

All three go in the release notes for `v0.0.1` and in
`packaging/homebrew/README.md` / `docs/android-release.md`, because a
channel that silently offers no upgrade is indistinguishable from a
broken pipeline six months from now.

## Landing exactly `v0.0.1`

Determinism comes from two things:

1. **Seed `v0.0.0` on `6fb7b5e`** (current `origin/main`) after wiping
   the old tags. That is the floor, and the analyser's range starts
   there.
2. **This branch carries no `feat:` commit.** Everything in it is
   `ci:`/`docs:`/`chore:`/`build:`, plus at least one `fix:` — which is
   honest, since wiring up release machinery that was configured and
   never run *is* a fix. One patch-level commit in `v0.0.0..HEAD`
   computes `0.0.1` and nothing else can.

This is a property of the commit range, not of the tool — it would have
been the same constraint under the shell script.

This is a real constraint on the branch, not an accounting trick: a
single `feat:` commit here makes the first release `v0.1.0`.

`v0.0.0` itself gets no release object — it is a floor, not a shipment.

## Decision 4 — what the release page carries

Four artifacts, and the fourth is the interesting one. Measured on this
machine rather than assumed:

| asset | built by | state |
| --- | --- | --- |
| `yellowjacket-<v>-android-arm64.apk` | `android-apk.yml` | already built, verified, signed |
| `yellowjacket-<v>-linux-amd64.tar.gz` | new job | binary + `.desktop` + icon |
| `yellowjacket-<v>-x86_64.pkg.tar.zst` | `arch-package.yml` | already built; free to attach |
| `yellowjacket-<v>-windows-amd64.zip` | new job | **compiles; has never been run** |

**macOS cannot be one of them.** `GOOS=darwin CGO_ENABLED=0` fails at
`wails/v3/pkg/mac: build constraints exclude all Go files` — the darwin
backend is Objective-C behind cgo, so a `.app` needs a macOS host and
the runner is a Linux container. That is precisely why the Homebrew
channel builds from source on the user's own Mac, and it stays the
answer for macOS.

**Windows is newly possible and should be labelled honestly.**
`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags production`
succeeds in 2.5 s and produces a 40 MB `.exe` — nothing in the audio,
database or webview path needs cgo on Windows (oto uses WinMM through
`x/sys`, sqlite is modernc's pure-Go driver, WebView2 is COM syscalls,
and MPRIS is `linux && !android`-tagged). But **compiling is not
running**: no Windows build of this app has ever been started, no CI tier
can exercise one, and `backend/system`'s `%LOCALAPPDATA%` path has never
resolved on a real machine. It ships marked as untested in the release
notes, or it does not ship — an unlabelled Windows download is a promise
nothing here can keep.

### The race the ordering creates

semantic-release runs **prepare** (changelog commit, tag push) before
**publish** (the `exec` curl that creates the release object). The tag
push is what starts the publishing workflows — so a fast one can reach
its upload step *before the release exists*, and
`POST /releases/{id}/assets` needs an id.

The capacity-1 runner serialises things enough that this would usually
work, which is the worst kind of bug. So each upload step **polls
`GET /api/v1/repos/…/releases/tags/{tag}` with a bounded retry** before
uploading, and fails loudly on timeout rather than skipping the asset.
That is ~8 lines of shell, shared by all three publishers.

## Phases

**Phase 0 — clear the ground.**
Delete `v1.3.0`–`v1.6.0` locally and on `origin`; push `v0.0.0` at
`6fb7b5e` — this is the floor semantic-release reads, and without it the
first release is `1.0.0` by its own rule. Delete the empty root
`package.json`. Truncate `CHANGELOG.md` to a header plus a line saying
history before `0.0.1` is in `git log` — the existing content is another
repo's links and cannot be repaired, only replaced, and the `changelog`
plugin prepends to whatever it finds.

**Phase 1 — `.releaserc.yml`.**
Swap `@semantic-release/github` for `@semantic-release/exec`, whose
`publishCmd` POSTs to
`/api/v1/repos/yonlu/yellowjacket/releases` with `tag_name`, `name` and
`body` taken from `${nextRelease.*}`. Keep `commit-analyzer`,
`release-notes-generator`, `changelog` and `git` exactly as written; fix
the `git` plugin's commit message so it passes `commit-check`
(`chore(release): ${nextRelease.version}` — the existing one already
does, but the trailing `${nextRelease.notes}` in the body is worth
keeping deliberate rather than incidental). `make release-dry` wraps
`semantic-release --dry-run` so the next version is answerable without
pushing anything.

`scripts/commit-check.sh`'s header already points at `.releaserc.yml`
for the type list and stays correct — that coupling survives this plan
rather than being broken by it.

**Phase 2 — `.gitea/workflows/release.yml`.**
On `push: branches: [main]`. Node 22, the pinned `npx` line from
Decision 1, `--repository-url` carrying `PACKAGE_TOKEN`. Concurrency
group `release-main` with `cancel-in-progress: false` — cutting a tag is
not a thing to cancel halfway.

The one guard that matters: **the job exits early when `HEAD`'s subject
starts `chore(release):`**, so the changelog commit the `git` plugin
pushes cannot re-enter this workflow. That is checked in shell rather
than left to `[skip ci]`, whose handling in Gitea is one more thing that
would have to be verified.

**Phase 3 — rewire the publish workflows.**
`arch-package.yml` moves from `push: branches: [main]` to
`push: tags: ['v*']` plus `workflow_dispatch`, so a merge no longer
publishes an untagged package. `homebrew-formula.yml` gains
`workflow_dispatch` with a `version` input and takes its version from
the input when there is no tag. `android-apk.yml` needs neither.

**Phase 3b — the assets.**
`scripts/release-asset.sh` is the shared uploader: wait for the release
by tag, then `POST /releases/{id}/assets?name=…`. `android-apk.yml` and
`arch-package.yml` each call it with the artifact they already built.
A new `desktop-assets` job — `push: tags: ['v*']`, in the same
`ubuntu:24.04` container `ci.yml` uses — builds the Linux binary via
`make build-prod` and the Windows one via the `CGO_ENABLED=0`
cross-compile, and uploads both. It is a separate job from the Arch one
because that runs in an `archlinux` container as an unprivileged
`makepkg` user, and grafting two unrelated builds onto it would make one
failure look like the other.

**Phase 4 — say that the upgrade is a reinstall, and that Windows is untried.**
No code change: a note in `packaging/homebrew/README.md`, one in
`docs/android-release.md`, the three-channel downgrade warning written
into the `v0.0.1` release notes, and a standing line in the notes
template marking the Windows asset unverified until someone runs it.

**Phase 5 — cut it and watch.** *(the only phase left)*
Merge, then verify with `gitea_ci` that (a) `release.yml` ran, seeded
`v0.0.0` and produced `v0.0.1`, (b) the release exists **with a non-empty
body** — check the body, not the exit code — and (c) **all four publish
workflows started from the tag**. If (c) is empty, that is Decision 2's
fallback and the `workflow_dispatch` inputs added in phase 3 are already
there to drive it.

The expected sequence on the merge is: `release.yml` seeds `v0.0.0`
(triggering nothing), releases `0.0.1`, and pushes both the changelog
commit and the tag — at which point `release.yml` fires a second time on
the changelog commit and exits at the `chore(release):` guard, while the
four `v*` workflows start. On a capacity-1 runner they will queue behind
each other, Android last and longest.

**Phase 6 — the documentation that will otherwise be wrong.**
CLAUDE.md's *Commits* section currently explains `.releaserc.yml` and
says nothing runs it; the CI section says there are five workflows and
that only `ci.yml` gates. Both change. `docs/android-release.md`
describes tags as hand-pushed. `make skill-check` fails on a `.pi/`
reference to a make target that does not exist, so `make release-dry`
gets documented or nothing does.

## Open questions for you

1. **Ship the Windows `.exe` or not?** It builds, and it has never run.
   Marked-as-untested is the assumption; say if you would rather hold it
   back until someone boots it.

Resolved: semantic-release stays, with `exec` in place of the `github`
plugin (Decision 1). Reinstalls are accepted, so no epoch and no
versionCode offset (Decision 3). The release carries the APK, a Linux
tarball, the Arch package and — pending (1) — a Windows zip; macOS is
not buildable here and stays a Homebrew-from-source channel (Decision 4).
`v0.0.0` has to be a real tag under this design — semantic-release reads
git tags for its floor and has no "treat absence as 0.0.0" knob that
also stops it calling the first release `1.0.0`.

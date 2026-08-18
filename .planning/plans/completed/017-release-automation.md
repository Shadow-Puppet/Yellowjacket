# 017 — Releases that happen by themselves

**Shipped as `v0.0.1`.** A merge to `main` now reads the Conventional
Commits since the last tag, cuts the tag and the Gitea release whose body
is the generated changelog, and the four publishing workflows build that
tag and attach their artifacts. Nothing is released by hand.

## What it looks like now

`release.yml` on push to `main` → semantic-release → tag → four `v*`
workflows in parallel (serialised in practice by the capacity-1 runner):

| workflow | publishes | attaches |
| --- | --- | --- |
| `arch-package` | pacman registry | `…-x86_64.pkg.tar.zst` |
| `android-apk` | generic registry (Obtainium) | `…-android-arm64.apk` |
| `desktop-assets` | — | `…-linux-amd64.tar.gz` |
| `homebrew-formula` | the public tap | — (builds from source) |

Verified on the real thing: all five green, three assets on the release,
the tap at `0.0.1`, and the Obtainium `latest` URL serving 200.

## The five decisions, and what they cost

1. **semantic-release, not a shell script.** The first draft of this plan
   proposed hand-rolling it and the argument did not survive checking:
   `@semantic-release/exec` is first-party and current, and the
   Gitea-shaped part is one `curl`. What I would have hand-rolled —
   commit parsing, semver ordering, note rendering — is the part with the
   edge cases and none of it is Gitea-shaped.
2. **`@saithodev/semantic-release-gitea` is a dead end** and was offered
   before it was checked: last published 2022, `got@10`, and no peer
   dependency on semantic-release at all.
3. **No `@semantic-release/git`.** `main` is protected, so a changelog
   commit-back is rejected by the pre-receive hook — and would be
   rejected *after* the tag was pushed, leaving a tagged release the run
   reports as failed. The release page is the changelog;
   `.release-notes.md` is a gitignored carrier and `CHANGELOG.md` is a
   signpost.
4. **Versions restart at `0.0.1`**, a downgrade on every channel. No
   `epoch`, no `versionCode` offset: both are permanent, a reinstall is
   once. Documented in `packaging/homebrew/README.md` and
   `docs/android-release.md`.
5. **No macOS and no Windows.** `GOOS=darwin CGO_ENABLED=0` fails at
   `wails/v3/pkg/mac` and there is no macOS runner, so Homebrew-from-source
   stays that channel. Windows cross-compiles in ~2.5 s and is withheld
   because no build of it has ever been *run*.

## Four things that only showed up by running it

- **`conventional-changelog-conventionalcommits@10` renders empty
  notes.** Silently: right version, right tag, every step green, and a
  release body that is a bare `## 0.0.1 (date)` heading with nothing
  beneath it. Held at `9`, in `release.yml` and `make release-dry`, with
  the reason beside both. **Check the rendered notes, never the exit
  code.**
- **semantic-release core dry-run-pushes to the release branch** as a
  permission check, independently of any plugin. `PACKAGE_TOKEN` had
  package-write and repo-*read* — enough to clone, not enough for this —
  and it failed with a flat `403 Forbidden` that reads exactly like
  branch protection. It is not: a `--dry-run` push never reaches the
  pre-receive hook, which a one-line experiment settled. The token needed
  `write:repository`.
- **The floor tag must go on `HEAD^`, not `HEAD`.** Seeded on the merge
  commit itself it leaves nothing between the floor and HEAD, and
  semantic-release correctly reports there is nothing to release. The
  first run did exactly that and cut nothing.
- **A tag-triggered workflow runs from the tagged commit's tree.**
  Moving `v0.0.0` back to `6fb7b5e` ran the *pre-merge* homebrew
  workflow, which predates the `v0.0.0` skip guard, and pushed a `0.0.0`
  formula to the public tap. Self-corrected at `0.0.1`. The corollary is
  general: a guard added today does not protect a tag pointing at
  yesterday.

## Two mechanisms confirmed, having been assumptions

- **A tag pushed with a user PAT does start the `v*` workflows**; one
  pushed with the Actions token does not (go-gitea#33123). Both halves
  are load-bearing and both were observed: the floor seed triggered
  nothing, and the release tag triggered all four.
- **Tags are not protected** on this repo, only `main` — which is what
  lets semantic-release tag at all.

## Left behind deliberately

`v0.0.0` stays on `origin` as the floor. It carries no release, and all
four publishers skip it by name.

# 015 — Android release pipeline

> **Completed.** The pipeline ships a signed APK from CI on every `v*` tag; `docs/android-release.md` is its operating document.

Ship an Android APK from CI on every version tag, published to the Gitea
generic package registry so Obtainium can poll a plain URL.

The baseline is `~/Development/ljos`, whose `.gitea/workflows/ci.yml`
`android:` job has been through the failure modes already. Most of what
follows is a transcription of that job onto this repo's conventions;
where it differs, the difference is argued.

## What this is not

**This ships a pipeline, not a usable Android music player.** The
success criterion is a signed, installable APK that launches — not an
app anyone would want. Explicitly out of scope, and each is real:

- `backend/mediacontrols/mpris_linux.go` **will be compiled on Android**.
  Go's `android` GOOS implies the `linux` build tag, so the `//go:build
  linux` file is in the build and MPRIS will look for a session bus that
  does not exist. It compiles; it will error at runtime.
- `backend/system` resolves XDG paths. Android has no XDG.
- The explore catalog artifact is ~0.6 GB. Nothing on a phone wants that.
- The shell is a desktop shell: an eleven-item sidebar, a 800×600
  measured minimum, a transport bar. None of that is a phone layout.
- The library scanner walks a filesystem Android does not grant.

Those are the *next* plan, if there is one. Conflating them with this one
is how a build pipeline takes six weeks.

## Phase 0 — the gate  [DONE 2026-08-16]

**Passed, further than asked.** No source changes were needed; a full
27 MB fat APK built first try, both ABIs, production-stripped. Numbers,
the environment and four non-obvious findings are in
`.planning/NOTES.md` — including a scaffold bug that put a *debug*
library in the release APK's phone ABI, fixed here.

**It also installs and launches on an emulator, and then exits.** One
line stops it: `backend/system/buildUserDirPath` switches on
`runtime.GOOS` and Android takes the `default:` branch returning
`errUnsupportedOS`, so `main()` hits `os.Exit(1)` six milliseconds
after the JNI bridge comes up. That is the *first* thing that stops it,
not the only one — see the "not this" section above, all of which is
still true and still out of scope.

The emulator tier that found it is now part of the harness:
`scripts/android-emulator.sh`, the `make android-*` targets, and
`.pi/skills/yellowjacket-dev/references/android-tier.md`. It exists
because the failure is invisible in all three places anyone would look
(no panic, no tombstone, no crash buffer) and ActivityManager restarts
the app fast enough that `pidof` always answers — so the tier's
assertion is "same pid after N seconds", not "it started".

Original phase 0 text follows, kept because its reasoning is what the
later phases rest on.


Everything downstream is wasted if the c-shared link fails. Establish it
by hand, locally, before writing a line of YAML.

Already established, by probe rather than by assumption:

```
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./backend/... ./internal/...
```

compiles the entire tree. Exactly two packages fail, and both fail only
because their Android implementation is cgo:

- `ebitengine/oto/v3` — `driver_android.go` needs the bundled **oboe**
  C++ backend. Oto supports Android natively; there is no Java audio
  glue to write.
- `wails/v3/pkg/application` — `mobile_features_android.go` needs the
  JNI bridge.

`modernc.org/sqlite` (the whole database layer), `beep`, `godbus` and
every `backend/` package are clean. **No source changes are known to be
required**, which is the single most surprising finding here and the
reason this plan is worth doing at all.

What Phase 0 must actually verify:

1. Install NDK **r26d** (`26.3.11579264`) locally. Pinned, not "whatever
   sdkmanager gives you" — ljos's AGENTS.md records newer NDKs breaking
   this build.
2. Generate the scaffolding (Phase 1) and run
   `wails3 task android:compile:go:shared ARCH=arm64` by hand.
3. Confirm `build/android/app/src/main/jniLibs/arm64-v8a/libwails.so`
   exists and is an ARM64 shared object.
4. Repeat for `amd64` (the emulator ABI).

**If the link fails, stop and re-plan.** The likely culprits, in order:
alsa (oto must select oboe, not ALSA — if it reaches for `alsa.pc` the
build tags are wrong), and `main.go`'s `//go:embed all:frontend/dist`
combined with the generated `main_android.gen.go` overlay.

Deliverable: a note in `.planning/NOTES.md` recording the exact command
and the NDK version that produced a `.so`, or the reason it cannot.

## Phase 1 — un-ignore and commit the Android scaffolding  [DONE]

Done as a side-effect of phase 0, which could not run without it. One
correction to the text below: **step 1 is wrong.** `update
build-assets` does not generate the android tree (NOTES.md explains);
it was generated with `generate build-assets` into a scratch dir and
`android/` copied across. CLAUDE.md is corrected to match. Steps 2-5
were done as written.


`build/android/` is gitignored (`.gitignore:72`) and its `includes:`
entry was dropped from `Taskfile.yml` during plan 009. That was correct
when nothing could target Android and is what has to be undone.

1. `wails3 task common:update:build-assets` — beta.8 embeds
   `internal/commands/build_assets/android/`, so this generates the tree.
2. Remove `build/android/` from `.gitignore`; add `build/ios/`'s reason
   to a comment so the asymmetry is explained rather than looking like an
   oversight.
3. Add `android: ./build/android/Taskfile.yml` to `Taskfile.yml`'s
   `includes:`.
4. **Gitignore the tree's own output**, or the repo grows a few hundred
   Gradle intermediates. ljos has exactly this problem — its
   `app/build/android/app/build/**` is committed. Ignore:
   - `build/android/app/build/`
   - `build/android/app/src/main/jniLibs/`
   - `build/android/overlay.json` and `build/android/gen/`
5. `make build-prod` and `make test` still pass — the new include must
   not perturb the desktop path.

**The refresh hazard has to be written down.** CLAUDE.md's Packaging
section already says `build/`'s platform metadata is regenerated from
`build/config.yml` and hand edits are lost. Phase 2 edits `build.gradle`
by hand. Extend that paragraph to name `build/android/app/build.gradle`
specifically, because the loss is silent and the symptom (a debug-signed
APK) appears months later as a failed update.

## Phase 2 — make the APK identifiable and updatable  [DONE 2026-08-16]

**Narrower than planned, because beta.8's scaffold is ahead of ljos's
beta.3: the release signing config already exists** and reads the four
`ANDROID_KEYSTORE_*` variables with a debug-keystore fallback. So this
phase was identity and versioning only. Verified end to end:

| | |
|---|---|
| package | `app.yellowjacket` (was `com.wails.app`) |
| versionCode / versionName | `10301` / `1.3.1`, from `YJ_VERSION_CODE` / `YJ_VERSION` |
| label | `YellowJacket` |
| signing | throwaway keystore -> `Signer #1 DN: CN=YellowJacket Test`, not the debug key |
| ABIs | arm64-v8a + x86_64, both production-stripped |

Installs and launches under the new identity. Still exits on the known
`buildUserDirPath` bug, which is phase 0's finding and not this phase's.

Two things this phase learned that the text below did not know:

- **The identity has to be declared twice.** `applicationId` in
  `app/build.gradle` is what Gradle installs; `APP_ID` in
  `build/android/Taskfile.yml` is what every adb-driven task targets.
  `ANDROID.md` says to set `APP_ID` in `build/config.yml` — that does
  nothing in beta.8, verified with `--dry`. Both are set, each
  commented pointing at the other.
- **The launcher activity is not under the applicationId.** It stays
  `com.wails.app.MainActivity` (the scaffold's Java package), so
  `am start -n app.yellowjacket/.MainActivity` resolves the dot against
  the wrong package and fails. `scripts/android-emulator.sh` carries the
  fully-qualified name and a comment saying why.

The `keytool` PKCS12 note below was confirmed verbatim: given a
`-keypass` differing from `-storepass` it prints "Different store and
key passwords not supported for PKCS12 KeyStores. Ignoring
user-specified -keypass value."

Original phase 2 text follows.


Edit `build/android/app/build.gradle`, following ljos's, whose comments
are worth reading before writing this:

- `applicationId "app.yellowjacket"` — matches `config.yml`'s
  `productIdentifier`. The `namespace` stays `com.wails.app` (it is the
  Java package, not the app identity).
- `versionCode Integer.parseInt(System.getenv("YJ_VERSION_CODE") ?: "1")`
  — **`Integer.parseInt`, not `(...) as Integer`**. Groovy binds the
  parentheses to `versionCode` first, so the cast reads as
  `versionCode("1") as Integer`, which sets a String and then casts the
  setter's null return; Gradle fails the whole project with "Value is
  null" at that line.
- `versionName System.getenv("YJ_VERSION") ?: "0.0.0"`.
- `abiFilters 'arm64-v8a', 'x86_64'`.
- A `release` signing config reading `ANDROID_KEYSTORE_FILE` /
  `_PASSWORD` / `ANDROID_KEY_ALIAS` / `ANDROID_KEY_PASSWORD`, falling
  back to the debug keystore only when no keystore is supplied.

**Android orders releases by an integer and refuses anything not greater
than what is installed.** A hardcoded `versionCode 1` means the first
install is the last: every later build is rejected as a downgrade and the
only fix is an uninstall. `1.3.1 -> 10301`, monotonic as long as minor
and patch stay under 100.

**Signing is not optional past the first install.** Android refuses to
update an app whose signing key changed, and the debug keystore differs
between every machine and every runner — so an unsigned CI build is a
decision to reinstall by hand forever. The job must **refuse to build**
without the keystore rather than quietly produce an APK that can never be
updated.

There is **one password and two required secrets**. keytool has defaulted
to PKCS12 since JDK 9 regardless of the `.jks` extension, and PKCS12
cannot hold a separate key password — given `-keypass` it warns and
ignores it. So `ANDROID_KEY_PASSWORD` defaults to the store password and
`ANDROID_KEY_ALIAS` to `yellowjacket`. Asking for a second password that
cannot exist is how someone sets a wrong value and debugs Gradle at
midnight.

Add `make android` → `PATH="$(TOOLBIN):$$PATH" go tool wails3 task
android:package:fat`, beside `build-prod`. `make skill-check` fails on a
documented target that does not exist, so document it only once it does.

## Phase 3 — the workflow  [DONE 2026-08-16]

`.gitea/workflows/android-apk.yml`, plus `docs/android-release.md` as
the operating document its error messages point at (phase 4's
documentation half; the secrets themselves still have to be created by
hand — see the table there).

Three departures from the text below, all argued in the file:

- **No `continue-on-error`.** The plan inherited it from ljos, where
  the Android job shares a pipeline with a server deploy that must
  never go red over a phone build. Here it is standalone and can
  neither delay nor redden anything, so a release step that fails
  silently is strictly worse than one that fails visibly.
- **No cached `wails3` binary.** The plan budgeted for ljos's
  `tools-bin` copy. Unnecessary: the CLI is a vendored `go tool`, and
  the runner already bind-mounts `GOCACHE`/`GOMODCACHE` for every job,
  so it is warm from `ci.yml`'s own `make bindings-check`. The GTK and
  WebKit *dev* headers are still installed, because `go tool wails3`
  links them.
- **A fourth cache volume, `/cache/gradle`.** Not in the plan and worth
  ~700 MB a run.

Four publish-gates were added and each was checked against a real APK:
both ABIs present, `versionCode` equal to the one derived from the tag,
a non-empty artifact, and **not signed with the debug key** — verified
by pointing the check at a deliberately debug-signed build, which it
refused.

Rehearsed locally with the exact CI invocation
(`make android ANDROID_SDK=... ANDROID_NDK=...`, `YJ_VERSION`,
`YJ_VERSION_CODE`, a throwaway keystore): `app.yellowjacket`,
versionCode 10301, versionName 1.3.1, label YellowJacket, both ABIs,
`Signer #1 DN: CN=YellowJacket`. Not yet run on the runner.

Original phase 3 text follows.


New file: `.gitea/workflows/android-apk.yml`. **Not a job in `ci.yml`.**
`ci.yml` runs on every branch push and is the workflow that gates; the
runner is capacity 1, and a 45-minute Android build in it would put every
push behind an SDK download.

```yaml
on:
  push:
    tags: ["v*"]
  workflow_dispatch:
```

This is where the baseline genuinely diverges. ljos computes its version
in CI (`scripts/next-version.sh`) and gates the Android job on
`needs.release.outputs.version != ''`, with an `always()` whose absence
would silently kill the manual path. **This repo has no release
automation** — tags are pushed by hand and `homebrew-formula.yml` already
keys on `v*`. So there is no `needs:`, no `always()`, and no status
function to get wrong: the tag *is* the version, and a dispatch falls
back to `git describe --tags --abbrev=0`.

Container, matching `ci.yml`'s conventions (`ubuntu:24.04`, clone by hand
with `PACKAGE_TOKEN` rather than `actions/checkout`, which is a JS action
needing node before any step has installed it):

```yaml
container:
  image: ubuntu:24.04
  volumes:
    - /home/logan/docker/gitea/data/runner/cache/tool:/cache/tool
    - /home/logan/docker/gitea/data/runner/cache/android-sdk:/cache/android-sdk
```

The SDK path must be inside the runner's `valid_volumes` allowlist —
a directory outside it makes the job **fail to start**, not silently skip
the mount. `/cache/tool` is already allowed and already holds the Go
toolchain `ci.yml` downloads.

`continue-on-error: true` and `timeout-minutes: 45`. Advisory, because a
tag's other three workflows must not go red over a phone build, and a
backstop because a wedged SDK download must not hold the only runner slot
for hours.

Steps:

1. **System packages.** `ci.yml`'s set plus `unzip` and `openjdk-17-jdk`.
   `libasound2-dev` stays — it is for the *host* `wails3` build, not the
   Android cross-build, which uses oboe.
2. **Go toolchain** — reuse `ci.yml`'s `/cache/tool/go` block verbatim.
3. **Android SDK and NDK (cached).** ljos's `install_if_missing`
   idempotent guard, unchanged: cmdline-tools 11076708, `platform-tools`,
   `platforms;android-34`, `build-tools;34.0.0`, `ndk;26.3.11579264`.
   sdkmanager is itself idempotent but still spends minutes verifying,
   which is why the explicit directory guards are there. ~3 GB and most of
   the job's wall clock on the first run; a directory listing after.
4. **wails3.** Cheaper here than in ljos, which pins
   `go install …/wails3@$version` against `app/go.mod`. This repo vendors
   the CLI (`go tool wails3`, `scripts/toolbin/wails3`), so the version is
   already pinned by `go.mod` and there is nothing to drift. It still
   *links* GTK and WebKit, so cache the built binary in
   `/cache/android-sdk/tools-bin` keyed on the wails version — and note
   ljos's finding that **caching the binary alone turned a slow job into
   a broken one**: `wails3` is dynamically linked, so the runtime
   packages are needed even on a cache hit. Here they are already in
   step 1.
5. **Frontend + codegen.** `pnpm install --frozen-lockfile && pnpm build`
   (pnpm, not ljos's npm), then `make generate`. `main.go` embeds
   `frontend/dist`, so nothing Go-side typechecks without it.
6. **Decode the keystore.** Refuse to build if `ANDROID_KEYSTORE_B64` is
   unset, with the sentence explaining why (Phase 2). Decide the absolute
   path *here* and export it via `$GITHUB_ENV` — **`${{ env.HOME }}`
   evaluates to an empty string in Gitea's expression context**, which
   turned `$HOME/x.jks` into `/x.jks` and surfaced as a missing file
   fifty-five seconds into a Gradle run.
7. **Build.** Compute `YJ_VERSION_CODE` from the tag, verify the keystore
   opens with `keytool -list` *before* Gradle does (Gradle only notices at
   `:app:validateSigningRelease`, a minute in, and reports it as a missing
   file), then `make android`.
8. **Verify the signature.** `apksigner verify --print-certs`, and print
   the SHA-256 with the note that a change to it breaks every future
   update. **Nothing here pipes into `head`**: under `set -o pipefail`,
   `head -1` exits early, the producer takes SIGPIPE, and the step fails
   with 141 *after* printing a perfectly good APK. Use `find … -print
   -quit` and a captured variable.
9. **Publish** to `api/packages/${OWNER}/generic/yellowjacket-android`,
   authenticating `--user "${OWNER}:${PACKAGE_TOKEN}"` — the same
   credential pair `arch-package.yml` already uses, not ljos's
   `REGISTRY_USER`/`REGISTRY_TOKEN`. Two copies: a versioned one for
   history and a fixed `latest/yellowjacket.apk` that Obtainium watches.
   Gitea refuses to overwrite, so delete `latest` first. The generic
   registry is readable **without credentials**, which is what lets
   Obtainium poll a plain URL with no token and no public source mirror.

## Phase 4 — secrets and documentation

Secrets to create on the repo (all under Settings → Actions → Secrets):

| Secret | Required | Note |
|---|---|---|
| `ANDROID_KEYSTORE_B64` | yes | `base64 -w0 yellowjacket-release.jks` |
| `ANDROID_KEYSTORE_PASSWORD` | yes | |
| `ANDROID_KEY_ALIAS` | no | defaults to `yellowjacket` |
| `ANDROID_KEY_PASSWORD` | no | defaults to the store password |
| `PACKAGE_TOKEN` | already exists | used by `arch-package.yml` |

Write the keytool command, the Obtainium URL and the signing-key warning
into a docs page — this is the part of ljos's setup that lives in
`docs/clients.md` and is referenced from the workflow's error messages,
so the messages have somewhere to point.

Then extend CLAUDE.md's CI section: it currently says "four workflows,
three of them package and publish; only `ci.yml` gates". That becomes
five, with the same sentence still true.

## Order and stopping points

Phase 0 gates everything. Phases 1–2 are one commit's worth of work and
are verifiable locally without CI. Phase 3 is the only part that needs a
runner, and its first run will be slow and will probably fail once on
something in the SDK step — budget for that rather than treating it as a
setback.

**Stop after Phase 0 if the c-shared link does not work.** Every later
phase is scaffolding for a build that does not exist, and the honest
outcome is a NOTES.md entry saying which package cannot cross-compile and
what it would take.

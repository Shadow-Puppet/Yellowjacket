# Releasing the Android APK

`.gitea/workflows/android-apk.yml` builds a signed fat APK
(`arm64-v8a` + `x86_64`) on every `v*` tag and publishes it to Gitea's
**generic** package registry, which is readable without credentials —
which is what lets Obtainium poll a plain URL with no token.

```
https://git.ljones.me/api/packages/yonlu/generic/yellowjacket-android/latest/yellowjacket.apk
```

A versioned copy is kept alongside it at
`…/yellowjacket-android/<version>/yellowjacket-<version>.apk`.

The app on the device is **`app.yellowjacket`**. Its launcher activity is
`com.wails.app.MainActivity` — a different package, because that is the
Wails scaffold's Java package and renaming it would mean renaming its
source. Every `am start` needs the fully-qualified form.

## The signing key is the thing you cannot lose

**Android refuses to update an app whose signing certificate changed.**
There is no override and no recovery: the only way to install a build
signed with a different key is to uninstall first, which takes the
user's library, playlists and play counts with it. The key therefore
outlives every other secret in this repo.

Two consequences are wired into the workflow rather than left to
discipline. It **refuses to build** when `ANDROID_KEYSTORE_B64` is
absent, instead of falling through to Gradle's debug-keystore default —
a debug key differs between every machine and every CI runner, so a
build signed with one is un-updatable from the moment it is installed.
And it **refuses to publish** an APK whose certificate reads
`CN=Android Debug`, which is the same rule enforced one step later, on
the artifact rather than the configuration.

### Creating it

```bash
keytool -genkeypair -v \
  -keystore yellowjacket-release.jks \
  -alias yellowjacket \
  -keyalg RSA -keysize 2048 -validity 10000 \
  -storepass '<a long random password>' \
  -dname "CN=YellowJacket, O=Shadow-Puppet, C=GB"
```

**Do not pass `-keypass`.** keytool has produced PKCS12 keystores by
default since JDK 9 — the `.jks` extension does not change the format —
and PKCS12 cannot hold a key password distinct from the store password.
Given one it tells you so and ignores it:

```
Warning: Different store and key passwords not supported for PKCS12
KeyStores. Ignoring user-specified -keypass value.
```

So there is **one** password. Asking for a second is how someone sets a
wrong value and then debugs Gradle at midnight.

Back the `.jks` up somewhere that is not this repository and not this
server. Record the certificate fingerprint the build prints
(`Signer #1 certificate SHA-256 digest`); if it ever changes, updates
have already broken.

### The secrets

Repository → Settings → Actions → Secrets.

| Secret | Required | Notes |
|---|---|---|
| `ANDROID_KEYSTORE_B64` | yes | `base64 -w0 yellowjacket-release.jks` |
| `ANDROID_KEYSTORE_PASSWORD` | yes | the `-storepass` above |
| `ANDROID_KEY_ALIAS` | no | defaults to `yellowjacket` |
| `ANDROID_KEY_PASSWORD` | no | defaults to the store password, and per the PKCS12 note it cannot differ |
| `PACKAGE_TOKEN` | already set | shared with `arch-package.yml`; publishes to the registry |

```bash
base64 -w0 yellowjacket-release.jks   # paste as ANDROID_KEYSTORE_B64
```

## Cutting a release

```bash
git tag v1.3.1
git push origin v1.3.1
```

That is the whole trigger. `homebrew-formula.yml` keys on the same tag,
so the desktop formula and the APK are cut together. The version code
Android orders releases by is derived from the tag — `1.3.1` → `10301`,
monotonic as long as minor and patch stay below 100 — so **do not
publish `1.100.0`**, and never move a tag that has already been built.

`workflow_dispatch` rebuilds without a new tag, taking an explicit
`version` input or falling back to the latest `v*` tag.

## What the workflow checks before publishing

- the APK exists and is non-empty;
- it carries **both** ABIs (`native-code: 'arm64-v8a' 'x86_64'`), or it
  is not the fat APK it claims to be;
- its `versionCode` is the one derived from the tag;
- it is **not** signed with the debug key.

The keystore is also opened with `keytool -list` before Gradle runs,
because Gradle only notices a bad password at
`:app:validateSigningRelease` — a minute of build time in — and reports
it as a missing file rather than a wrong password.

## Caches, and the first run

The job mounts four cache volumes; on a cold runner the first build is
slow and everything after it is not.

| Volume | Holds | Cold cost |
|---|---|---|
| `/cache/android-sdk` | SDK, platform, build-tools, NDK r26d | ~2 GB |
| `/cache/gradle` | `GRADLE_USER_HOME` — wrapper + AGP graph | ~700 MB |
| `/cache/tool` | the Go toolchain (shared with `ci.yml`) | ~200 MB |
| `/cache/pnpm-store` | pnpm store (shared with `ci.yml`) | — |

Every path must be inside the runner's `valid_volumes` allowlist. One
that is not makes the job **fail to start** rather than silently skip
the mount.

The NDK is pinned to **r26d** (`26.3.11579264`). Newer NDKs have broken
the Wails Android build before; it is a version, not a floor.

## Building one locally

```bash
make android          # unsigned-ish: debug key, versionCode 1, 0.0.0
YJ_VERSION=1.3.1 YJ_VERSION_CODE=10301 \
ANDROID_KEYSTORE_FILE=$PWD/yellowjacket-release.jks \
ANDROID_KEYSTORE_PASSWORD=... ANDROID_KEY_ALIAS=yellowjacket \
  make android        # what CI produces
```

Running it is a separate tier — see
`.pi/skills/yellowjacket-dev/references/android-tier.md`, and read its
first section before you try, because a failing Android build looks
exactly like a working one.

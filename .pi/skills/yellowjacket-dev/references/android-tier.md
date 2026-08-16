# The Android tier

A sixth tier, and the only one where **the app failing looks exactly
like the app working**. Read the first section before you run anything;
it is the difference between a diagnosis and an afternoon.

This tier answers "does the phone build run", nothing else. It is not a
spec tier, it does not run in CI, and the app is not a usable Android
player yet (plan 015 says why, at length).

## Three facts that make failure invisible

**Go's stdout does not reach logcat.** An Android app's fd 1 and 2 go to
`/dev/null`. Every `slog` line the app writes is discarded — including
the one naming the error it is about to exit on. `setprop
log.redirect-stdio true` does not help: it redirects the *Java*
runtime's `System.out`, and the Go code is a c-shared native library.

**`os.Exit` is a silent death.** `main()` ends several failure paths in
`os.Exit(1)`. From Android's side that is a process that vanished:
`ActivityManager: Process com.wails.app has died`, `Zygote: exited due
to signal 9`, and **no** panic, **no** `AndroidRuntime` stack, **no**
tombstone under `/data/tombstones` and nothing in `logcat -b crash` or
dropbox. All three of the places you would look are empty, and the one
signal that is present — SIGKILL — reads as "the system killed it",
which is the wrong hypothesis.

**ActivityManager restarts it, so a dead app looks alive.** A
crash-looping app is respawned several times a second, so `pidof` always
answers and `am start` always reports `Status: ok`. "Did it start" is
the wrong question. `make android-smoke` asks the right one — is it the
*same pid* a few seconds later.

The tell, once you know it: `I/WailsBridge: Wails bridge initialized`
followed immediately by a new pid doing the same thing. That means the
native library loaded, the JNI bridge came up, Go's `main()` ran, and
`main()` left. Work backwards through its `os.Exit(1)` paths.

## What to run

One-time, ~3.5 GB:

```bash
make android-setup          # SDK pieces + the yj-test AVD, idempotent
```

Then:

```bash
make android                # fat APK (arm64 + x86_64) -> bin/yellowjacket.apk
make android-emulator       # boot headless in the background, wait for boot
make android-install        # adb install -r
make android-smoke          # launch, then assert the same pid survives 10s
make android-logs           # filtered logcat, follow
make android-emulator-stop  # console kill, then the saved PID
```

`make android-smoke SECONDS=30` for a longer window. On failure it
prints the last 40 app-relevant logcat lines and how to read them.

Never `pkill -f emulator` — the pattern matches the invoking shell's own
command line and kills it, silently dropping the rest of your compound
command. The emulator is addressed by its saved pid in
`.dev/emulator.pid`, same discipline as `make dev-stop`.

## Things that cost a cycle

- **`ANDROID_HOME` must carry a platform, and Arch's does not.**
  `/opt/android-sdk` (the `android-sdk` package) has an NDK and
  build-tools but `platforms/` is *empty*, so Gradle fails with a
  compileSdk error that reads like a version mismatch. The Makefile
  defaults `ANDROID_SDK` to `~/Android/Sdk` (user-owned, writable,
  where sdkmanager puts things) and `ANDROID_NDK` to `/opt/android-ndk`
  separately, because the Go half wants the NDK and the Gradle half
  wants the platform and they are in different places.
- **The NDK is pinned to r26d** (`26.3.11579264`, Arch's
  `android-ndk-26`). Newer NDKs have broken the Wails Android build
  before. CI pins the same one.
- **Without KVM the emulator still works and is unusably slow** — a 30 s
  boot becomes tens of minutes, which reads as a hung target rather than
  a slow one. `make android-setup` checks and warns.
- **`-no-snapshot` is deliberate.** A snapshot-resumed emulator carries
  the previous run's app state, and a smoke result that depends on what
  the last run left behind is not a result.
- **The logcat filter is not optional.** The emulator emits thousands of
  lines a second, nearly all WindowManager transitions; an unfiltered
  `adb logcat` buries the six lines that matter. `make android-logs`
  filters to `WailsBridge`, the app's own tag, `GoLog`, `AndroidRuntime`,
  `DEBUG` and `libc:F`.
- **`run-as` does not work on a release-signed APK** (`package not
  debuggable`), so you cannot read the app's data directory or its
  environment that way. Ask the device instead, or build a debug variant.
- **The `google_apis` system image, not `default`.** This app is a
  WebView app; `google_apis` ships the Chrome-based WebView that
  actually renders it.

## The current state of the build

`make android-smoke` **fails today, and the cause is known.**
`backend/system`'s `buildUserDirPath` switches on `runtime.GOOS` with
cases for darwin, linux and windows, and a `default:` that returns
`errUnsupportedOS`. `runtime.GOOS` is `"android"`, so it takes the
default, `NewYellowJacketApp` fails, and `main()` calls `os.Exit(1)` —
about 6 ms after the bridge initialises, which is exactly the signature
described above.

**The fix is a documented Wails API and needs no build tags.**
`application.Mobile.StoragePath()` returns the app's private files
directory and returns `""` on desktop (`mobile_stub.go`), and
`resolveUserDirPath` already lets `YJ_HOME` override the path on every
OS — so setting that override from `StoragePath()` early in `main()`,
when it is non-empty, is the whole change. Do *not* import
`pkg/application` into `backend/system`: that package is deliberately
Wails-free, which is what the `indexbuild` tag split is protecting.

It is the *first* thing that stops it, not the only one. MPRIS is
compiled in (`android` implies the `linux` build tag, so
`mpris_linux.go` is in the build and will look for a session bus that
does not exist), and the desktop shell is still a desktop shell. Fixing
one and re-running the smoke is how you find the next.

**And one that no amount of porting will fix:** open-*directory*
dialogs return an error on Android — the Storage Access Framework gives
tree URIs, not filesystem paths — as do save-file dialogs. This app's
first run is "choose your music folder" and its library model is
filesystem paths, so that is a design question, not a port.

## The scaffold's own tasks

`build/android/Taskfile.yml` ships more than the Makefile wraps, and
they are the right thing to reach for when you want something one-off:

```
wails3 task android:run              # debug build + emulator install + launch
wails3 task android:run:device       # same, first connected physical device
wails3 task android:deploy-device    # production APK to a device
wails3 task android:bundle:fat       # AAB, for a Play Store upload
wails3 task android:studio           # open build/android/ in Android Studio
wails3 task android:device:list
wails3 task android:logs:all
wails3 task android:clean
```

Two are deliberately **not** wrapped. `android:logs` greps logcat for
`(Wails|yellowjacket)`, which catches the `WailsBridge` tag but misses
the app's own process tag (`app.yellowjacket` — lowercase, so `Wails`
does not match it) and misses `ActivityManager`'s "has died" line, which
is the one that tells you it crashed; `make android-logs` filters by tag
instead. And `ensure-emulator` boots whatever `-list-avds | tail -1`
returns, with no pidfile and no boot wait, so it cannot be stopped or
sequenced.

## The identity is declared twice

`applicationId` in `build/android/app/build.gradle` is what Gradle
installs. `APP_ID` in `build/android/Taskfile.yml` is what every
adb-driven task uninstalls, launches and filters. **Nothing enforces
that they agree**, and `ANDROID.md`'s advice to set `APP_ID` in
`build/config.yml` does not work in beta.8 — `wails3 task` never reads
that file (verified with `--dry`), and even when set it feeds only the
adb commands, never Gradle. Change both or the official `run`/`deploy`
tasks address a package that is not installed.

Related, and it will bite once: the launcher activity is
`com.wails.app.MainActivity` and the applicationId is
`app.yellowjacket`. `am start -n app.yellowjacket/.MainActivity`
resolves the leading dot against the *applicationId* and fails with a
class-not-found that reads like a broken build. Always the
fully-qualified form.

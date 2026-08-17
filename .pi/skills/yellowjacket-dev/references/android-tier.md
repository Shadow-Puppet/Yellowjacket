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
make android                # arm64-v8a APK -> bin/yellowjacket.apk (~16 MB)
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

**adb is addressed by AVD name, not by whatever is plugged in.** The
script resolves `ANDROID_SERIAL` from `ro.boot.qemu.avd_name` before
any device command, because a second emulator (another project's, or
this one's own corpse left `offline` by a previous run) makes a bare
`adb` fail with "more than one device" — which `cmd_install` reported
as *"no device — run 'make android-emulator' first"* immediately after
that had succeeded. Serials are assigned in boot order and change
between runs, so the AVD name is the identity. Set `ANDROID_SERIAL`
yourself and it is honoured; one device that is not ours (a phone) is
taken as the target.

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

**The app starts. The x86_64 emulator cannot run it, and that is not a
bug in the app.**

`modernc.org/libc` — which `modernc.org/sqlite`, and therefore the whole
database layer, sits on — issues a **raw `lstat` syscall on
linux/amd64** (`libc_linux_amd64.go`'s `Xlstat64` calls
`unix.Syscall(unix.SYS_LSTAT, …)`). Android's seccomp policy forbids
syscall 6 on x86_64, because bionic never issues it, so the process
takes `SIGSYS` the first time anything touches the database:

```
F/libc: Fatal signal 31 (SIGSYS), code 1 (SYS_SECCOMP), syscall 6
F/DEBUG: Cause: seccomp prevented call to disallowed x86_64 system call 6
```

**arm64 is unaffected, and structurally so.** There is no `lstat`
syscall on arm64 at all, so `ccgo_linux_arm64.go`'s `Xlstat` is
`Xfstatat(…, AT_SYMLINK_NOFOLLOW)` → `SYS_newfstatat` (79), which
Android permits. `grep -c SYS_LSTAT ccgo_linux_arm64.go` is 0. Go's own
`syscall` package already uses `fstatat` on both architectures, which
is why this is *only* the modernc path.

So: **verify on arm64, and on this machine that means a real device.**
`make android-smoke` on an x86_64 AVD reports a `SIGSYS` tombstone that
says nothing about your change.

**Do not reach for an arm64 system image — it will not run here, and
finding that out costs a 3.8 GB download.** Emulator 37 refuses
outright:

```
FATAL | Avd's CPU Architecture 'arm64' is not supported by the QEMU2
        emulator on x86_64 host. System image must match the host
        architecture.
```

Google dropped cross-architecture emulation; there is no flag. The
options are an arm64 host, a physical device, or `adb connect` to one.

**The x86_64 ABI is therefore gone from the build** (`abiFilters` in
`build/android/app/build.gradle`, `android:package` rather than
`package:fat` in the Makefile, and a `native-code: 'arm64-v8a'$`
assertion in `android-apk.yml` that fails if it comes back). It could
not run on any Android until modernc fixes this — x86 Chromebooks
included — and dropping it took the artifact from 27 MB to 15.9 MB.
The tombstone was at least honest while it lasted: unlike the
`os.Exit` that came before it, it left a real crash record with a
backtrace.

### The emulator still installs it, and it still does not run

The obvious guess about dropping x86_64 — that `make android-install`
would now refuse with `INSTALL_FAILED_NO_MATCHING_ABIS` — is **wrong,
and was measured wrong before it was written down.** Google's
`google_apis` x86_64 images carry arm64 translation:

```
ro.product.cpu.abilist = x86_64,arm64-v8a
```

So the arm64-only APK installs, the loader maps `lib/arm64/libwails.so`
and runs it (the tombstone says `Guest architecture: 'arm64'`). It then
dies **before any of our code**, with SIGILL rather than SIGSYS:

```
signal 4 (SIGILL), code -6 (SI_TKILL)
#00 pc 00000000015911d0  .../lib/arm64/libwails.so
```

Disassembling that offset names the reason exactly:

```
15911d0: d5380600   mrs  x0, ID_AA64ISAR0_EL1
```

That is Go's `internal/cpu` reading the arm64 CPU-feature ID register
at runtime init, which the translator does not implement. So it is not
"our Go program is unlucky": **no Go binary starts under this
translation layer**, and no amount of work on this app changes it.

The three failures are worth holding side by side, because each looks
like the app's fault and none is:

| build | on x86_64 Android | signal |
|---|---|---|
| x86_64 | modernc's raw `lstat` vs seccomp | SIGSYS, syscall 6 |
| arm64, translated | Go reads `ID_AA64ISAR0_EL1` | SIGILL |
| arm64, real device | — | unverified, still |

**A physical arm64 device remains the only verification path.**

### What was fixed to get here

`backend/system`'s `buildUserDirPath` switched on `runtime.GOOS` with a
`default:` returning `errUnsupportedOS`, so Android failed at startup
and `main()` called `os.Exit(1)` six milliseconds after the bridge came
up. `main()` now calls `system.UseHomeOverride(application.Mobile.
StoragePath())` before anything asks for a path — a documented,
build-tag-free API that returns `""` on desktop, where the setter is a
no-op. `backend/system` gained no import of the Wails application
package, which matters for the same reason `backend/events` is split by
the `indexbuild` tag.

### What is still not done

The shell is still a desktop shell, and the x86_64 half of the APK is
still dead weight. Everything in plan 016's section A is now built:
storage access, an in-app folder picker (Android's directory dialog
returns an error, since the Storage Access Framework yields tree URIs
rather than paths), MPRIS excluded, and a MediaSession with a transport
notification and audio focus.

### Compiling the `android`-tagged Go by hand

`make lint` and `make test` never see it: their three tag sets are all
linux/amd64, so the only thing that compiles `backend/mediacontrols/
android.go` is `make android` — a full APK build for a Go type error.
The short way round:

```bash
B=$(echo /opt/android-ndk/toolchains/llvm/prebuilt/*/bin)
CC=$B/aarch64-linux-android21-clang CXX=$B/aarch64-linux-android21-clang++ \
  GOOS=android GOARCH=arm64 CGO_ENABLED=1 go build ./backend/...
```

**`CXX` is not optional.** Without it the oboe C++ sources in `oto`
compile against the host sysroot and fail on `android/log.h` and
`sys/system_properties.h`, which reads like a broken or missing NDK.
Restrict it to `./backend/...`: `./...` additionally builds
`build/android/gen`, a scaffold shim that only resolves inside the
wails task and fails with `undefined: main` on its own.

A Go method added to a bound service also reaches the frontend unless
it says not to — `//wails:ignore` above the func, which `make bindings`
then honours. `Player.SetDuck` is driven by OS audio focus and carries
one.

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

## What only a device can answer

The emulator cannot run this app (three separate reasons, none of them
ours — see plan 016), so the phone in someone's pocket is a tier, and
asking for it is cheap. The first run of it, on 2026-08-17, confirmed
the whole of A4 and found two faults **no other tier can see**:

- **The back gesture.** `MainActivity.onBackPressed` asks
  `webView.canGoBack()`. Nothing in a desktop shell has a back gesture,
  so no spec had ever called `page.goBack()` and the app had never
  pushed a history entry — back quit from any depth. It is a history
  entry per navigation now, which is also what made it assertable in the
  browser tier (`e2e/specs/back-navigation.spec.ts`).
- **The safe area.** `targetSdk 35` forces edge-to-edge, so the
  transport and the tab bar sat under the gesture bar. **A browser
  viewport has no system bars**: `phone-shell.spec.ts` at 390x844 will
  keep passing on a build the device is clipping 48dp off. Insets are
  handled in `applyWindowInsets()`.

So when asking for a device run, ask about what the platform *adds* —
system bars, the back gesture, focus and audio interruptions,
permission dialogs, the keyboard — not about what the app draws. The
drawing is what the other five tiers already cover.

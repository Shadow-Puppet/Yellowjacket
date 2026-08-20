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
| arm64, real device | **runs** (2026-08-20) | — |

**A physical arm64 device remains the only verification path**, and it
has now been walked: a Light Phone III (TLP301, Android 14 / SDK 34,
arm64-v8a, WebView Chrome 113 at 424x439). The app builds, installs,
launches and stays up; `make android-smoke SECONDS=60` passes on it.
What that run *found* is the lifecycle fault below.

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

**And `main()` is now latched to one run per process** (#52). That is
the second `os.Exit(1)` in this file's history and it had the same
signature as the first, which is the argument for #160: both were named
exactly by an `slog` line that went to `/dev/null`.

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

> **These four were unsafe until #159 and are now the way in.** All of
> them began with `adb uninstall {{.APP_ID}}`, where `APP_ID` defaulted
> to `app.yellowjacket` — the **release** id — while `run` and
> `run:device` build the **debug** variant, whose id is
> `app.yellowjacket.dev`. So they uninstalled the user's app, taking
> the library with it, installed a different package, and then failed
> to launch the one they had removed.
>
> They share `scripts/android-deploy.sh` now, which **never**
> uninstalls (`install -r`, and a changed signing certificate is
> reported with the command rather than acted on), reads the package id
> back out of the built APK, and refuses a target that is not the kind
> the task names. There is nothing left to avoid; the manual sequence
> below is kept because it is still the smallest thing that works.

```
wails3 task android:run              # debug build + emulator install + launch
wails3 task android:run:device       # debug build + install + launch on a phone
wails3 task android:deploy-device    # release build, same
wails3 task android:bundle:fat       # AAB, for a Play Store upload
wails3 task android:studio           # open build/android/ in Android Studio
wails3 task android:device:list
wails3 task android:logs:all
wails3 task android:clean
```

**`run` and `deploy-emulator` mean the emulator, and now say so to
adb.** They used a bare `adb install`, which with exactly one device
attached picks that device whatever it is — so with a phone plugged in
and no emulator running, the task whose summary reads "in the Android
Emulator" installed on the phone. They pass `--target emulator` and
refuse with `make android-emulator` as the remedy.

**`DEVICE_ID=<serial>` still names a device, and several attached
devices is now an error rather than a silent pick of the first.**

Two are deliberately **not** wrapped. `android:logs` greps logcat for
`(Wails|yellowjacket)`, which catches the `WailsBridge` tag but misses
the app's own process tag (`app.yellowjacket` — lowercase, so `Wails`
does not match it) and misses `ActivityManager`'s "has died" line, which
is the one that tells you it crashed; `make android-logs` filters by tag
instead. And `ensure-emulator` boots whatever `-list-avds | tail -1`
returns, with no pidfile and no boot wait, so it cannot be stopped or
sequenced.

## The identity is read back from the APK

It used to be **declared twice**, and that is what #159 was.
`applicationId` in `build/android/app/build.gradle` is what Gradle
installs; `APP_ID` in `build/android/Taskfile.yml` was what every
adb-driven task uninstalled, launched and filtered, and nothing
enforced that they agree. They did not: the debug buildType carries
`applicationIdSuffix ".dev"`, so every task that assembles a debug APK
addressed the release id. This file flagged the hazard for five phases
and it cashed out twice — once as a wrong `am start`, once as an
uninstall of the user's library.

**`scripts/android-pkgid.sh` is the one answer now.** It prints the
package id an APK declares (`aapt2 dump packagename`, falling back to
`aapt dump badging`), and the deploy path installs and launches *that*.
The APK is the authority because the task that installs it has just
built it: whatever Gradle resolved the applicationId to, suffixes and
flavours included, is in the file, and no default can disagree with it.
An APK it cannot read is a hard failure, never a fallback to a written
down default — guessing is the bug.

**`APP_ID` survives as an assertion, not a setting**, and has no
default. `wails3 task android:run APP_ID=app.yellowjacket` says "this
build had better declare that id" and is refused, naming both, *before*
anything is installed or a device is even chosen. It could never have
been a setting: `ANDROID.md`'s advice to put it in `build/config.yml`
does not work in beta.8 — `wails3 task` never reads that file (verified
with `--dry`) — and even when set it fed only the adb commands, never
Gradle.

`scripts/android-emulator.sh` derives `PKG` the same way, from
`bin/yellowjacket.apk` when one is built, so `make android-install`,
`android-launch`, `android-logs` and `android-smoke` follow whichever
variant is actually in `bin/`. `YJ_ANDROID_PKG` still overrides, and
the old literal survives only for a tree with no APK built yet.

**The uninstall is gone and is not coming back.** It existed to make
the bare `install` on the next line work at all — without `-r` Android
refuses an install over an existing package — so `install -r` removes
the *reason* for it rather than merely removing it. What is left is the
one case an uninstall really is the remedy, a changed signing
certificate, and that is exactly the case where performing it silently
costs the user their library. So it is named and not done, which is the
answer `scripts/android-emulator.sh` had already reached for
`make android-install`.

Related, and it will bite once: the launcher activity is
`com.wails.app.MainActivity` and the applicationId is
`app.yellowjacket`. `am start -n app.yellowjacket/.MainActivity`
resolves the leading dot against the *applicationId* and fails with a
class-not-found that reads like a broken build. Always the
fully-qualified form.

**`wails3 task android:run:device` is the way to put a debug build on a
real device**, since #159. What #52 used, before it was safe, was the
longer form, and it is still the smallest thing that works if you want
no script between you and adb:

```bash
wails3 task android:build ARCH=arm64 && wails3 task android:assemble:apk
adb install -r bin/yellowjacket.apk      # -r, never uninstall
adb shell am start -n app.yellowjacket.dev/com.wails.app.MainActivity
```

The id in that last line is the one thing to keep an eye on by hand —
`./scripts/android-pkgid.sh bin/yellowjacket.apk` is what the tasks ask,
and it is a good habit before any `am start` written out in full.

`YJ_ANDROID_PKG=app.yellowjacket.dev` still overrides what
`scripts/android-emulator.sh` — and therefore `make android-smoke`,
`android-logs`, `android-launch` — addresses, but it is rarely needed
now: that default is read from `bin/yellowjacket.apk`, so it already
follows whichever variant was built last.

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

**The third such fault was the activity lifecycle** (#52), and it is
the one to re-check after touching `main()`, `WailsBridge` or
`MainActivity`. Android destroys and recreates an activity **without
restarting the process**, and Wails' `nativeInit` — which
`MainActivity.onCreate` calls — runs `go mainFunc()` every time. So
Go's `main()` ran again on a live app, `app.Run()` refused (`a.starting`
is still true behind Android's `select{}`), and the `os.Exit(1)` under
it took the healthy first app down with it.

### The lifecycle check, and how to trigger it on demand

This is the regression guard for #52 on this tier, because no other
tier runs `main()` on Android at all. The Go-side guard
(`TestMainClaimsBeforeItDoesAnything`) catches work creeping above the
latch; only the device catches the latch not working.

**Trigger a relaunch with a configuration change the manifest does not
declare.** `AndroidManifest.xml` lists
`orientation|screenSize|keyboardHidden|uiMode`, so those are handled
in-place and are *not* triggers. `fontScale` is not listed, and it is a
one-liner:

```bash
adb shell settings put system font_scale 1.15   # restore the old value after
```

That is the same in-process destroy/recreate that "Don't keep
activities", a locale change and a memory trim produce, but on demand.

**"Don't keep activities" is the report's own lever and did not work on
this device**: `settings put global always_finish_activities 1` reads
back as `1`, `am set-always-finish-activities` does not exist on this
build, and the activity was never finished on backgrounding. Do not
spend an afternoon on it; use the config change.

**The assertion is the pid, and the tell is two bridge inits in one.**

```bash
adb logcat -d | grep -E "Wails bridge initialized|has died|finishDrawing of relaunch"
```

Healthy is one pid appearing twice — the process surviving the
recreation:

```
I/WailsBridge(28420): Wails bridge initialized
I/WailsBridge(28420): Wails bridge initialized     <- same pid, recreated
```

Broken is that pair followed within a second by:

```
I/WindowManager: finishDrawing of relaunch: Window{...MainActivity} 603ms
I/ActivityManager: Process app.yellowjacket.dev (pid 22956) has died: fg  TOP
W/ActivityTaskManager: Force removing ActivityRecord{...}: app died, no saved state
```

Two things about reading that. **`has died: fg  TOP` is not a memory
kill** — the system does not reclaim the foreground process, so this is
the app leaving of its own accord. And there is **no crash record
anywhere**: `logcat -b crash` is empty, no `AndroidRuntime`, no
`libc: Fatal signal`, no tombstone. That is the `os.Exit` signature,
and it is why "the system killed it" is the wrong first hypothesis.

**Surviving is only half of it — check the recreated WebView is still
wired to the running app.** A plausible-looking fix (making
`WailsBridge.initialized` static, so the second `nativeInit` is skipped)
keeps the process alive and silently breaks this, because `nativeInit`
is also what re-points the JNI reference at the new bridge. Go would go
on executing JavaScript against the destroyed activity's WebView: the
app opens, renders, and never receives another backend event.

Ask the page, after a relaunch and a resume:

```bash
make android-inspect
make android-eval EXPR='(()=>{window.__probe=[];const o=window._wails.dispatchWailsEvent.bind(window._wails);window._wails.dispatchWailsEvent=(e)=>{window.__probe.push(e&&e.name);return o(e)};return "ok"})()'
# background and foreground the app, then:
make android-eval EXPR='JSON.stringify(window.__probe)'
```

A healthy build answers with events from the live services —
`["IndexStatusChanged","JobsChanged","JobsChanged","android:storageAccess"]`.
`[]` means the bridge reference is stale.

## Asking the device, not just looking at it

A real phone can be inspected, and that turns this tier from "reported
symptoms" into evidence. Three commands:

```bash
make android-screenshot          # what the screen shows (.dev/ by default)
make android-inspect             # forward the WebView's devtools socket
make android-eval EXPR='JSON.stringify({vp:[innerWidth,innerHeight]})'
```

Four things about it, each of which costs an hour if met cold:

- **Only a `debuggable` build has a devtools socket**, and a debug build
  carries `applicationIdSuffix ".dev"` so it installs **beside** the
  release app. That matters more than convenience: the two are signed by
  different certificates, and Android's only remedy for a changed
  certificate is an uninstall, which takes the user's library with it.
  Never uninstall to make room for a build.
- **Playwright cannot drive it.** `connectOverCDP` calls
  `Browser.setDownloadBehavior`, a WebView answers "Browser context
  management is not supported", and the connection dies before the first
  evaluate. `scripts/android-eval.mjs` is raw CDP over Node's built-in
  WebSocket for that reason.
- **Wireless adb drops when the screen sleeps.** The symptoms are
  `device offline` mid-session and a `fetch failed` from the eval
  script. Plug in over USB for anything longer than a couple of probes.
- **The socket name carries the pid**, which changes on every launch, so
  it is resolved rather than remembered.
- **A reinstall resets the runtime permissions**, and the grant dialog
  is a separate activity that takes focus — so the app is up, `am start`
  reports "delivered to currently running top-most instance", and
  `pidof` is empty because it never got to the foreground.
  `dumpsys window | grep mCurrentFocus` naming
  `GrantPermissionsActivity` is the tell. `adb shell pm grant
  app.yellowjacket.dev android.permission.READ_MEDIA_AUDIO` (and
  `POST_NOTIFICATIONS`) ahead of the launch skips it.

### Calling a binding on the device

**The runtime call does not go over HTTP on Android**, and this is worth
knowing before an hour is spent on it. The WebView cannot deliver a
`fetch()` POST body to `shouldInterceptRequest`, so v3 routes runtime
calls through the `addJavascriptInterface` bridge instead: the
@wailsio/runtime installs a `customTransport` that calls
`window.wails.invokeAsync(id, payload)` and receives the answer on
`window._wailsAndroidCallback`. Two consequences:

- **`.playwright/init-events.js` does not transfer to the device.** Its
  outbound half hooks `fetch`, which sees nothing here, and its
  `call()` posts to `/wails/runtime`, which answers
  `Invalid runtime call: missing object value` — the interceptor got the
  URL with no body. Its *inbound* half is still right, because
  `dispatchWailsEvent` is the entry point in every mode.
- **Hooking `fetch` from an eval is too late anyway**, on any platform:
  the bundle captured its reference at module scope, so a wrapper
  installed afterwards records nothing. That is why the harness is an
  `initScript` and not a step in a spec.

What works is to borrow the bridge, chaining the runtime's own callback
so its pending calls still resolve:

```js
const pending = new Map();
const prev = window._wailsAndroidCallback;
window._wailsAndroidCallback = (id, response, error) => {
  if (!pending.has(id)) return prev && prev(id, response, error);
  const p = pending.get(id); pending.delete(id);
  const env = JSON.parse(response || "{}");
  return env.ok ? p.resolve(env.data ?? env.text) : p.reject(new Error(env.error));
};
window.__yj = { call(name, args) {
  return new Promise((resolve, reject) => {
    const id = "yj" + Math.random().toString(36).slice(2);
    pending.set(id, { resolve, reject });
    window.wails.invokeAsync(id, JSON.stringify({
      object: 0, method: 0, windowName: "",
      args: { "call-id": id, methodName: "yellowjacket/backend/" + name, args: args || [] },
      clientId: window._wails.clientId,
    }));
  });
} };
```

That turns the device into a tier that can be *driven* rather than only
looked at — `__yj.call("player.Player.LoadFile", [path])` and
`__yj.call("library.Library.AddLibrary", ["/sdcard/Music/..."])` are how
#53 was measured. Names are the Go ones (`GetTracks`, not
`GetAllTracks`); an unknown one comes back as a plain
`unknown bound method name`, so a wrong guess is loud.

**Getting audio onto the phone**: `adb push` into
`/sdcard/Android/data/<pkg>/files/` looks like it works and then the
files are not there — scoped storage. `/sdcard/Music/...` plus
`pm grant … READ_MEDIA_AUDIO` does work, and `AddLibrary` takes the
plain path. The generated fixtures are **~2 seconds** each, which is
fine for a scan and useless for watching a seek bar, so synthesise a
long one: `ffmpeg -f lavfi -i sine=frequency=440:duration=240`.

**And the reason to bother: the phone is an engine, not a screen.** The
first device here renders in **Chrome 113** at 424x439 CSS px. Every
other tier runs a current Chromium or WebKit, so a spec that passes at
that viewport says nothing about the phone — 113 has no Popover API and
no relaxed CSS nesting, and a dropped CSS declaration renders as
"present but wrong", which is the hardest failure to read from a
picture. Get the version first; it reframes every other symptom.

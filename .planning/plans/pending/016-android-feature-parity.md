# 016 — What Android parity would actually take

> **Status: all of section A is done.** A1–A3 landed with "let the app
> reach the user's music"; A4 (MediaSession, transport notification,
> audio focus) landed with "survive the screen locking". The direction
> taken is **option 1, the full librarian**: `MANAGE_EXTERNAL_STORAGE`
> plus an in-app folder browser, which keeps the path-keyed model
> intact. B1/B2 remain, both awaiting a decision rather than work. The
> sections below are kept as written, because they are the argument the
> decision rests on — see "What is left" at the end for the current
> state.

Plan 015 shipped a *pipeline*: the app cross-compiles, is signed and
versioned, and publishes from CI. This is the assessment of what stands
between that and an Android app worth installing.

**The headline: parity is the wrong target, and choosing it would be
the expensive mistake.** Four of the blockers below are not porting work
— they are the Android platform declining to support the model this app
is built on. The decision to make first is in "The fork in the road" at
the end; everything before it is evidence for that decision.

Severity is what the app *does* today, verified against the source and
the generated manifest, not guessed.

## A. It cannot work at all until these are fixed

### A1. The app can read no music. (deepest)

`build/android/app/src/main/AndroidManifest.xml` requests INTERNET,
VIBRATE, ACCESS_NETWORK_STATE, USE_BIOMETRIC, POST_NOTIFICATIONS, the
two location permissions, CAMERA and the two FOREGROUND_SERVICE ones.
**There is no storage or media permission of any kind.** At
`targetSdk 35` that means the app can see its own private directory and
nothing else.

Adding `READ_MEDIA_AUDIO` is necessary and *not sufficient*, because it
grants access through **MediaStore**, not through the filesystem. This
app's entire model is absolute paths: `audio_files.file_path` is the
primary key of ownership, `AddLibrary(path)` takes a directory, the
scanner walks it with `os.ReadDir`, and every one of
`GetFilePathsByAlbums` / `ByGenres` / `ByRecordingMBIDs` exists to hand
paths to the player. Scoped storage does not offer a stable directory
to walk.

The honest options are three, and they are not close in cost:

- **MediaStore as the library source.** Query the content resolver,
  keep MediaStore IDs (or content URIs) beside or instead of paths, and
  open audio through a `ContentResolver` file descriptor. This is the
  Android-native answer and it touches the schema, the scanner, the
  player's file opening and every path-keyed query.
- **`MANAGE_EXTERNAL_STORAGE`.** Keeps the path model intact and is
  effectively barred from Google Play except for genuine file managers.
  Viable *only* because we distribute through Obtainium — which is a
  real point in its favour here, and worth stating plainly rather than
  dismissing.
- **App-private storage only**, i.e. the user copies music into the
  app's sandbox. Trivial to build, and nobody wants it.

### A2. The first-run flow cannot complete.

`first-run-wizard.ts` calls `DirectoryPicker()`, which is
`frontendutil.DirectoryPicker` → `app.Dialog.OpenFile().
CanChooseDirectories(true)`. Wails' own `ANDROID.md` lists open-directory
dialogs as **"❌ Returns an error — SAF yields tree URIs, not filesystem
paths"**. So the one action the wizard exists to perform fails, and
`<first-run-wizard>` intercepts all pointer events until a library
exists — so the app is not merely empty, it is inert.

Whatever A1 resolves to decides this: a MediaStore library needs no
picker at all, and a SAF tree needs the picker to return a URI the
backend can use.

### A3. MPRIS is compiled into the Android build.

`mpris_linux.go` is `//go:build linux`, and **`android` implies
`linux`** (documented, and the reason it is in the APK). It will look
for a session bus that does not exist. It needs `//go:build linux &&
!android`, and its Android counterpart is A4.

This one is cheap and should be done regardless — it is a two-character
build-tag change plus whatever `mediacontrols.New` returns instead.

### A4. Playback will be killed the moment the screen locks.

The scaffold's `WailsForegroundService` is typed **`dataSync`**
(`foregroundServiceType="dataSync"`, `FOREGROUND_SERVICE_TYPE_DATA_SYNC`),
and the manifest requests `FOREGROUND_SERVICE_DATA_SYNC`. A music player
needs `mediaPlayback` and `FOREGROUND_SERVICE_MEDIA_PLAYBACK`, plus a
`MediaSession` for lock-screen and notification transport controls,
plus **audio focus** — pause on a phone call, duck for a notification,
pause on headphone unplug. None of that exists today. `oto` will happily
keep writing to a stream nobody can hear.

This is the difference between "an app that plays audio" and "a music
player", and it is Java-side work in the scaffold plus a Go-side bridge.

## B. It works, but wrongly

### B1. The x86_64 half of the APK cannot run on any Android.

Established in plan 015: `modernc.org/libc`'s `Xlstat64` issues a raw
`lstat` on linux/amd64, which Android's seccomp forbids, so the process
takes `SIGSYS` the first time it touches the database. arm64 is
structurally unaffected (no `lstat` syscall exists; it routes through
`fstatat`).

So ~31 MB of the artifact is dead weight on *every* Android device,
including x86 Chromebooks. Options: drop `x86_64` from `abiFilters`
(smaller APK, no emulator target — which does not work anyway), or
carry it against a future modernc fix. **Dropping it is the honest
default**; it is also the only item in this plan that is a five-minute
change.

### B2. The UI is a desktop shell.

`MinWidth`/`MinHeight` are 800×600 and were *measured* — below ~780 the
header subtitle wraps the title out of its bar. A phone is ~360–430 CSS
px wide. The sidebar collapses to icons below 900px, which is a
laptop-sized breakpoint, not a phone one. Beyond width: the app is built
on hover (the marquee's `hover` mode, tooltips), right-click context
menus, a keyboard shortcut layer with its own overlay and settings page,
multi-select with ctrl/shift, and a resizable-column track list. None of
those are gestures.

This is not a stylesheet pass. It is a second front end for the views
worth having on a phone, sharing the stores and bindings — which the
architecture supports, since a view is already a lazily-loaded chunk
behind `VIEW_LOADERS`.

### B3. Tag writing cannot reach the user's files.

`tagwriter` rewrites tags in place, and autotag's whole purpose is
applying them to a folder. Under scoped storage that is impossible
outside the sandbox without a SAF write grant per tree. If A1 lands on
MediaStore, in-place tag writing needs `MediaStore` write requests and
user confirmation per file on Android 11+.

Autotagging is arguably a desktop-only feature and saying so is a
legitimate answer.

### B4. The Explore catalog is a ~0.6 GB download into app-private storage.

It works — but with no awareness of a metered connection and no
accounting for a device where that is a meaningful fraction of free
space. At minimum it needs to be opt-in on mobile and to refuse a
metered network by default. `Android.NetworkJSON()` reports
`{connected,type}`, so the signal is available.

## C. Inert, and fine

Window geometry, menus and the system tray are documented no-ops on
mobile. The keyboard shortcut layer is harmless but its Settings page
is dead weight. `profiling` is already compiled out of production
builds. These cost nothing and need no work.

## D. Unknown until it runs on a device

**Nothing in section A or B has been observed on Android**, because the
x86_64 emulator cannot run the app (B1) and emulator 37 refuses arm64
images on an x86_64 host. Everything above is read from the source, the
generated manifest and Wails' own documentation. The first real device
run will find things this list does not have, and the most likely
places are audio latency and buffering under `oto`/oboe, and SQLite
behaviour on app-private storage.

## The fork in the road

The four blockers in section A are all the same question wearing
different clothes: **is the Android app a librarian, or a player?**

YellowJacket on the desktop is a *librarian*. It scans folders,
deduplicates covers, detects duplicate tracks, reconciles against
MusicBrainz, rewrites tags on disk, and manages downloads. That model
rests on owning a filesystem, which is precisely what Android declines
to give.

Three coherent products, and only the first is "parity":

1. **Full librarian on Android.** Requires `MANAGE_EXTERNAL_STORAGE`
   (Obtainium-only distribution, which we already have), a phone UI for
   every view, and media-session playback. Largest scope by far; the
   result is an app almost nobody has asked for on a phone.
2. **A player for music already on the phone.** MediaStore as the
   source, no scanner, no autotag, no downloads; the library, queue,
   playlists, favourites and Explore-as-browsing all still make sense.
   This is a genuinely good Android app and it is *not* parity — it is
   a subset with a different data source.
3. **A companion to the desktop app.** The phone browses and controls
   the desktop's library over the network, or syncs a subset. Smallest
   Android surface, and it leans on the thing that already works.

**Option 2 is the recommendation** if the goal is an app people use;
option 3 if the goal is the least work for the most value. Option 1 is
the only one that answers "feature parity" literally, and it is the one
worth arguing hardest against.

> **Decided:** option 1's *data model* (the librarian keeps its
> filesystem and its scanner — A1 shipped that) with option 2's
> *surface*. The phone is a player over the library this app already
> builds; it does not get every view. The list is below.

## The phone gets a subset (decided)

B2 is not a stylesheet pass and not a second front end either. A view
is already a lazily-loaded chunk behind `VIEW_LOADERS` /
`DETAIL_LOADERS` in `index.ts`, and the stores and bindings are shared,
so the phone build is **a different loader table and a different
chrome**, over the same stores.

**In**, because each is something a person does with a phone in their
hand:

- **Home** — the shelves are already a phone-shaped surface.
- **Library browse** — albums, artists, genres. The grids are already
  virtualized and card-shaped.
- **Now playing** — which on a phone is a *view*, not a 4em bar.
- **The queue.**
- **Search** — the header box, scoped as it already is.
- **Playlists**, including smart ones, as lists to play rather than to
  edit.

**Out**, and each for a reason rather than by omission:

- **Autotag** — the review UI is a wide table and the action rewrites
  files on disk; B3 has not been verified even as *possible* yet.
- **Downloads** — two tab panels of client configuration.
- **Explore** — the catalog is a ~0.6 GB download (B4); browsing it is
  the last thing to earn a phone's storage.
- **Settings** — not the page. The phone needs a handful of settings
  (theme, the library folder, playback) and not the 93 controls the
  desktop page carries.
- **Jobs**, **shortcuts overlay**, **column configuration** — a phone
  has no keyboard and no resizable columns, and the jobs indicator is
  enough.

What the shell has to lose, from the audit at the top of this section:
the 800×600 minimum, the 11-item sidebar (a phone wants a bottom tab
bar over the five things above), hover as a route to anything,
right-click as the only route to a context menu (long-press is the
gesture), and ctrl/shift multi-select.

One rule for the work: **no view forks.** A phone layout that copies a
view's template is two templates to fix every bug in. Where a view
cannot serve both, the split belongs at the chunk boundary that already
exists.

Phase 1 followed that rule and found its cost: reusing `<app-sidebar>`
inside the drawer means reusing its `data-testid`s too, and a second
copy standing by in the DOM broke 30 specs that had nothing to do with
the phone. The rule holds — a second list of destinations would be
worse — but a shared component must be rendered only when it is wanted,
and the guard belongs in a test that names the reason.

## What is worth doing regardless of that decision

Cheap, independently useful, and each unblocks measurement:

1. **Drop `x86_64` from `abiFilters`** (B1) — or keep it and document
   why. Five minutes.
2. **`//go:build linux && !android` on `mpris_linux.go`** (A3), so the
   Android build stops carrying a D-Bus client. Small.
3. **A device smoke run**, which needs someone's phone and the published
   APK. Everything in D depends on it, and it is the single highest
   information-per-minute action available.
4. **Make the first-run wizard fail legibly** rather than inertly (A2)
   — the picker's error already routes through `describeError`, but the
   wizard still blocks pointer events, so an Android user sees a dead
   screen rather than a sentence. Even under option 3 this is the right
   behaviour.


## What is left (updated after A4)

**A4 is done.** `backend/mediacontrols/android.go` is a `Handler`
beside the MPRIS one, and the Java half is
`WailsForegroundService.java`: a `MediaSession`, a `MediaStyle`
transport notification and audio focus. It needed no new JNI and no new
Gradle dependency — `application.Android.StartForegroundService(json)`
going out, `WailsBridge.emitEvent` → the application event bus coming
back, and the platform `android.media.session` API rather than
androidx.media, which minSdk 21 makes available anyway.

Four decisions in it are worth keeping:

- **Ducking is a player concept, not a volume change.**
  `Player.SetDuck` re-applies the *user's* level with an attenuation
  offset, so `getUserVolume` still reports what the user chose and
  nothing is persisted or emitted. A duck that wrote through to the
  volume would let one notification tone permanently turn the music
  down.
- **The duck path is pre-Oreo only.** From API 26 the framework ducks
  the app itself and sends no `CAN_DUCK` focus change, so asking to be
  told instead (`setWillPauseWhenDucked`) would mean pausing for every
  notification tone, and doing both would attenuate twice.
- **An unchanged payload is not an event here either.** Every push
  crosses JNI and re-delivers an Intent, and the player pushes state on
  several paths that can agree.
- **After the first start, updates use `startService`.** From Android
  12 an app in the background may not *start* a foreground service, but
  it may keep delivering intents to one it already has — which is every
  track change with the screen off.

The contract with Java — the payload keys, the state words, the command
names — is in `androidpayload.go`, deliberately *without* the `android`
build tag, so `go test` exercises it on every platform. Everything left
in `android.go` is untested by construction: it compiles only under a
cross-compiler and runs only on a phone.

**B1 is done: x86_64 is dropped.** 27.1 MB → 15.9 MB, measured. Three
places had to agree — `abiFilters`, the Makefile's `android:package`
(or Go still compiles a library Gradle then discards) and the
`native-code: 'arm64-v8a'$` assertion in `android-apk.yml`, whose
anchor is what stops it also matching the fat APK's line. Adding the
ABI back, if modernc ever fixes `Xlstat64`, is those same three edits.

**B2, the desktop shell.** Scope decided (below); **all four phases are
done.**

- *Phase 1, the shell.* Below 600px the sidebar column is gone,
  `<bottom-nav>` is the primary navigation, and the shell fits 320px
  exactly — measured, from 652px in a 360px viewport before.
- *Phase 2, the full-screen now-playing view.* Where phase 1's seek bar
  and volume went. A detail view, so Back pops the nav stack; it
  composes the real transport components rather than copying them; and
  it hides the bottom bar while it is up, so it carries its own queue
  button.
- *Phase 3, long-press.* `utils/long-press.ts`: one document-capture
  listener, installed once from `index.ts`, which turns a 500 ms
  stationary touch into a synthetic `contextmenu` at the touch point.
  Every menu in the app opens from that event, so all six components
  gained the gesture without one of them changing — which is the same
  argument `ContextMenuController` rests on, one layer lower. The
  details that are not obvious are in `NOTES.md` (2026-08-17); the one
  worth repeating is that ours is told from the browser's own
  long-press event by **identity**, not `isTrusted`, because a test
  cannot dispatch a trusted event and that path would otherwise be the
  only uncovered one.

- *Phase 4, the track list.* A phone draws `titleArtist` (title over
  artist) plus the duration, and drops the column headers and the resize
  handles — a column set rather than a second row template, so the row
  and everything delegated on it is unchanged. Verified at the device's
  own 424x439: `24px 304px 80px`, 52 px rows, no truncation, no
  overflow. The device also found the bug in it, which no browser
  viewport would have: saved *desktop* column widths reached the phone
  through an id-keyed store and gave the duration column 55% of the row.

**B2 is complete.** What is left in this plan is B3 (tag writing, which
needs a device), B4 (the catalog download on a metered connection), and
the standing question of the Light Phone's Chrome 113 — which so far has
cost nothing: menus, dialogs and long-press all work on it.

**B3/B4** are unchanged, and B3 is now *possible* where it was not:
with all-files access, `tagwriter` can write in place.

### What the first device run answered (2026-08-17)

A4 **works**: playback survives the screen locking, and the transport
notification appears with cover art — which also settles the service's
access to a `MANAGE_EXTERNAL_STORAGE` path, the permission grant and
the lock-screen session in one observation. Everything below in "what
none of section A answered" was written before this and is now answered
except the OEM permission-flow variance.

It also found two faults no browser tier can see, both fixed and both
awaiting the next APK for confirmation (`NOTES.md`, same date):

- **Back quit the app from any depth.** The scaffold asks
  `webView.canGoBack()`; the frontend had never used `history`. A
  navigation is a history entry now, and `navStack` is gone rather than
  kept beside it.
- **The transport was under the gesture bar** — or so the version
  number said. `applyWindowInsets()` in `MainActivity` is right and
  stays, but the phone is **Android 14**, where the system still insets
  the window: the fix is pre-emptive and the symptom has another cause.
  Still open, along with icons that do not appear at all. The phone's
  WebView is **Chrome 113**, which is the lead (no Popover API, no
  relaxed CSS nesting), and `make android-inspect` / `android-eval` are
  how it gets asked.

The standing item is unchanged in kind: **B3 (tag writing) and the
permission flow still need a device**, and so does confirming these two.

### What none of section A answered

Nothing here has been observed on a device. The permission flow in
particular is the kind of thing that behaves differently across OEM
builds — `ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION` is
implemented inconsistently, which is why there is a fallback to the
global list, and neither path has been exercised.

A4 adds its own list of things only a device can answer, and they are
the likely first failures: whether the notification appears at all
(POST_NOTIFICATIONS is requested from `startForegroundService`, so a
user who declines gets a service with an invisible notification),
whether audio focus arrives while `oto`/oboe holds the output, whether
the lock screen picks up the session, and whether cover art decoded
from a `MANAGE_EXTERNAL_STORAGE` path is readable by the service.

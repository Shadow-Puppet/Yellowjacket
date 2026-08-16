# 016 — What Android parity would actually take

> **Status: A1, A2 and A3 are done** (commit "let the app reach the
> user's music"). The direction taken is **option 1, the full
> librarian**: `MANAGE_EXTERNAL_STORAGE` plus an in-app folder browser,
> which keeps the path-keyed model intact. A4 (MediaSession and audio
> focus) and B1/B2 remain. The sections below are kept as written,
> because they are the argument the decision rests on — see "What is
> left" at the end for the current state.

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


## What is left (updated after A1-A3)

**A4, playback that survives the screen locking.** The manifest and the
service are typed `mediaPlayback` now and the permission is declared,
so the foundation is in place; what is missing is a `MediaSession`, a
transport notification and audio-focus handling. The plumbing for it
exists and needs no new JNI: Go can call
`application.Android.StartForegroundService(json)` (exported by Wails),
and Java can call `WailsBridge.emitEvent(name, json)` back into the
application event bus, which Go subscribes to. So the shape is a JSON
payload of title/artist/state going out and transport commands coming
back, with `backend/mediacontrols` gaining an Android handler beside
the MPRIS one — the interface it already defines is the right shape.

Audio focus is the half that is easy to forget and the more important
one: pause on a phone call, duck for a notification, pause on headphone
unplug. `oto` will happily keep writing to a stream nobody can hear.

**B1, the x86_64 half of the APK**, which cannot run on any Android
because of the modernc `lstat` seccomp trap. Still undecided; dropping
it is a five-minute change that halves the artifact.

**B2, the desktop shell.** Untouched and the largest remaining piece.

**B3/B4** are unchanged, and B3 is now *possible* where it was not:
with all-files access, `tagwriter` can write in place.

### What A1-A3 did not answer

Nothing here has been observed on a device. The permission flow in
particular is the kind of thing that behaves differently across OEM
builds — `ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION` is
implemented inconsistently, which is why there is a fallback to the
global list, and neither path has been exercised.

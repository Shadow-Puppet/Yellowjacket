# Failure UX audit — YellowJacket

Scope: error handling, empty/loading states, destructive actions, and failure UX
across the frontend/backend boundary. Read-only; nothing was changed.

Method: `backend/app.go`, every bound service in `FEBindings` (`backend/app.go:194-215`),
the generated bindings under `frontend/wailsjs/go/**`, all 13 stores/controllers in
`frontend/src/store/`, and every component in `frontend/src/components/` that calls a
binding. Counts: 165 `catch` blocks in `frontend/src`, 84 of which end in
`console.error`/`console.warn` and nothing else.

**Headline:** there is no application-level notification surface. Two components grew
private, mutually-unaware toasts (`config-page.ts:1168`, `autotag-view.ts:1318`), and
everything else logs to a console the user cannot open. The single most common failure
in a music player — *this file will not play* — is one of the paths that reaches the
user as complete silence.

---

## Critical

### C1. A track that fails to load or play is a silent no-op, forever
**`backend/queue/queue.go:1181-1239`** (`loadCurrentTrack`, `playCurrentTrack`),
reached from `Queue.Play/PlayIndex/Next/Previous/SetQueue`.

`LoadFile` or `Play` returning an error is logged and turns into `return false`; the
caller reverts `currentIndex` (`queue.go:1069-1074`, `queue.go:920-925`) and returns.
No event is emitted. Every Wails binding on the path returns `Promise<void>`
(`frontend/wailsjs/go/queue/Queue.d.ts`) because the Go methods return nothing, so the
frontend cannot even observe the failure — and `queue-store.ts:192-263` does not
`await` or `.catch()` any of them regardless.

Symptom: double-click a track whose file was moved, is corrupt, or has an unsupported
codec — nothing happens. No row highlight, no error, no skip. Double-click it again —
still nothing. Mid-queue auto-advance onto a bad file stops playback dead with no
explanation (`queue.go:920-925`), and pressing Next does nothing because Next hits the
same bad track and reverts.

Fix: add a `PlaybackFailed` event carrying `{filePath, reason}`, emit it from
`loadCurrentTrack`/`playCurrentTrack`, and have `Next`/auto-advance skip the failed
track rather than reverting.

### C2. `SeekFailed` is emitted by the backend and nobody listens
**`backend/player/player.go:776`** emits `events.SeekFailed`; **`frontend/src/events.ts:8`**
declares it; there is no `EventsOn(Events.SeekFailed, ...)` anywhere in `frontend/src`
(verified by grep — the only other hits are `events.go` and `emit_test.go`).

Symptom: dragging the seek bar on a track that has no loaded seeker snaps the thumb
back to where it was, with no indication why.

Fix: subscribe in `player-store.ts` and surface it (revert the optimistic seek position
plus a message), or delete the event so it stops implying coverage that does not exist.

### C3. Autotag apply writes to the user's files with no cancel, no undo, and no presence outside its own page
**`backend/autotagservice/service.go:1078-1181`**, **`frontend/src/components/autotag-view/autotag-view.ts:1624-1662`**.

`ApplyAsync` spawns `go s.runApply(...)` which calls `s.applier.Apply(s.ctx, ...)` —
it rewrites tags in place across a whole folder. There is:
- no cancel (`grep 'jobs\.' backend/autotagservice/*.go` → nothing; it is not registered
  with the `jobs.Registry`, unlike scans, index builds and downloads),
- no undo,
- no visibility once the user leaves the autotag page — the progress lives entirely in
  `autotag-view`'s local `applyJobs` map (`autotag-view.ts:1180`), which is discarded on
  `disconnectedCallback` (`autotag-view.ts:1239`),
- no drain on shutdown — `OnShutdown` (`backend/app.go:498-510`) saves player and queue
  state and returns; `OnBeforeClose` (`backend/app.go:461`) unconditionally returns
  `false`. Quitting mid-apply cancels `s.ctx` and leaves the folder half-retagged with
  nothing recording where it stopped.

Symptom: the user starts an apply, navigates away or quits, and comes back to a folder
where some tracks carry the new tags and some the old, with no way to tell which.

Fix: register the apply with `jobs.Registry` (giving it the existing cancel/progress
surface for free) and make `OnBeforeClose` return `true` while a file-writing job is in
flight.

`backend/tagwriter/pipeline.go:286-360` (batch tag writes) has the same absence from
the job registry, but is mitigated — see the note under **M8**.

### C4. `libraryStore` serves the previous library's data after a filter switch
**`frontend/src/store/library-store.ts:339-343, 445-467, 128-152`**.

`setSelectedLibrary()` → `invalidate()` sets `this.tracks = null` and calls
`eagerFetch()`. If the previous library's `GetAllTracksByLibrary` is still in flight,
`getTracks()` sees `tracks === null && tracksLoading === true` and returns
`waitForTracks()` (`library-store.ts:494`) — which waits for the *old* request. That
request's `try` block then assigns `this.tracks = <library A's tracks>`
(`library-store.ts:145`) and bumps `changeGen`, so the store is now caching A's tracks
while `selectedLibraryIdValue` is B.

Symptom: switch the library filter twice quickly and the track/album/artist/genre lists
show the wrong library's contents until the next scan or filter change.

Fix: stamp each fetch with a `fetchGen` captured at request time and drop the
assignment when `fetchGen !== this.changeGen` (the same version-guard pattern
`explore-view.ts:703/793/821` already uses correctly).

---

## Major

### M1. A failed library fetch hangs every waiter forever
**`frontend/src/store/library-store.ts:128-152, 494-506`** (and the identical
`waitForAlbums`/`waitForArtists`/`waitForGenres` at 508-548).

`getTracks()` rejects → `finally` sets `tracksLoading = false` and notifies → the
`waitForTracks` subscriber tests `!this.tracksLoading && this.tracks !== null`, which is
false because `tracks` is still `null` → the promise never settles and the subscription
is never removed.

Symptom: any component that called `getTracks()` while another fetch was in flight
hangs on an unresolved promise (permanent spinner) and leaks a store subscription.

Fix: give the four `waitFor*` helpers a reject path, or store the in-flight promise and
return it instead of re-deriving it from subscriber notifications.

### M2. The track list conflates "empty", "loading" and "failed" into one permanent "Loading tracks…"
**`frontend/src/components/track-list/track-list.ts:1901-1902`**:
`this.tracks.length === 0 ? html\`<p>Loading tracks...</p>\``.
`loadTracks()` (`track-list.ts:1242-1257`) `console.error`s on failure and leaves
`this.tracks` at `[]`.

Symptom: three different situations render as an infinite "Loading tracks…" —
a genuinely empty library, a backend query that failed, and a library filter with
nothing in it. `genre-details.ts:194-198` makes it worse: on error it sets
`this.tracks = []` and hands that to `<track-list>`, so a failed genre query is
indistinguishable from a slow one.

Fix: track `loading`/`error` as separate state and render three distinct bodies —
the `home-view.ts:263-280` `renderBody()` is the correct model already in this repo.

### M3. The Settings search-index panel says "Loading status…" forever
**`frontend/src/components/config-page/config-page.ts:186, 195, 1016-1022, 1034, 1530`**.

`indexStatus` is only ever assigned from the `IndexStatusChanged` event listener, and
that event is emitted from exactly one place — `backend/explore/searchindex.go:692`,
inside `emitStatus()`, which only fires on build status *mutations*. `indexPollTimer`
is declared (195) and cleared (1034) but **never assigned**. The pull binding
`GetIndexStatus()` exists (`frontend/wailsjs/go/explore/Service.d.ts:42`) and is never
called from `frontend/src`.

Symptom: open Settings when no index build is running — which is the steady state —
and the Search Index section shows "Loading status…" indefinitely, even though the
index is fully built.

Fix: call `GetIndexStatus()` in `connectedCallback` to seed `indexStatus` before the
first event arrives.

### M4. Job pause/resume/cancel failures are unhandled promise rejections
**`frontend/src/components/jobs/job-controls.ts:17-35`**, wired as
`@job-control=${applyJobControl}` at `jobs-view.ts:330`,
`job-details-drawer.ts:335`, `job-indicator.ts:397`.

`applyJobControl` is `async` and is used directly as a DOM event listener, so its
returned promise is discarded. `jobStore.pause/resume/cancel/dismiss`
(`job-store.ts:189-204`) `await` the binding with no `catch`.

Symptom: press Pause on a scan and, if the backend rejects, the button does nothing —
no state change, no message. There is also no in-flight guard, so double-clicking
Cancel issues two `CancelJob` calls.

Fix: wrap the switch in try/catch inside `applyJobControl` and surface the failure;
disable the row's controls until the next `JobsChanged` snapshot arrives.

### M5. Scan / full-rescan buttons fail silently
**`frontend/src/components/jobs/jobs-view.ts:276-282, 284-290, 296-314`**.

All three handlers `console.error` and return. `FullRescan` returns
`errNoLibrariesConfigured` when no library is configured
(`backend/library/rescan.go:33-35`), and — unlike "Scan all", which is disabled on
`this.libraries.length === 0` (`jobs-view.ts:434`) — the Full rescan button is only
disabled on `anyScanning` (`jobs-view.ts:470`).

Symptom: with no libraries configured, the user reads a scary confirmation, clicks
"Full rescan", confirms, and absolutely nothing happens.

Also a double-click hazard: `anyScanning` is derived from `jobStore`, which is fed by
`JobsChanged` events coalesced at 250 ms (`backend/events` / `jobs` registry). Two
clicks inside that window both issue `ScanLibrary`.

Fix: surface the error, add `|| this.libraries.length === 0` to the Full rescan
`?disabled`, and add a local `starting` flag that disables the button until the job
snapshot lands.

### M6. Deleting a playlist has no confirmation and no undo
**`frontend/src/components/playlist-view/playlist-view.ts:1352-1372`** (multi-select
path) and **`1381-1392`** (`handleDeletePlaylist`).

The multi-select branch loops `await DeletePlaylist(id)` over every selected playlist
with no prompt. `handleDeletePlaylist` `console.error`s on failure, so a partial
failure looks like a success until the refresh reveals the playlist is still there.

Compare `jobs-view.ts:296` (full rescan) and `job-controls.ts:41-53` (index cancel),
both of which do confirm — the codebase has the convention, this path just skips it.

Fix: `window.confirm` naming the playlist(s) and their track counts, matching the
pattern already used for full rescan.

### M7. Durable download requests are removed with one click, no confirmation, unhandled rejection
**`frontend/src/components/downloads-view/downloads-view.ts:466-476`**
(`void downloadStore.removeRequest(request.id)`), and the same shape at
**`451-460`** (`pauseRequest`) and **`296-300`** (`clearSatisfiedRequests`).

`downloadStore.removeRequest` (`download-store.ts:434-437`) awaits `RemoveRequest` with
no catch, and the call site discards the promise with `void`.

Symptom: click the ✕ next to an artist subscription you have been building for months
— it disappears with no prompt and no undo; or, if the delete fails, it stays put with
no explanation.

Fix: confirm before removing a subscription, and `.catch()` the promise into a visible
message.

### M8. Stale preview overwrites newer rules in the smart-playlist editor
**`frontend/src/components/smart-playlist-editor/smart-playlist-editor.ts:666-707`**.

`schedulePreview()` debounces 300 ms, then `runPreview()` awaits
`PreviewSmartPlaylist(json)` with no request id. Debouncing only coalesces keystrokes
*within* the window; a query that takes longer than 300 ms overlaps the next one, and
whichever resolves last wins.

Symptom: edit a rule, and the preview list settles on the results of the *previous*
rule set. The `finally` block also clears `previewLoading` from the stale response,
so the spinner stops while the current query is still running.

Fix: capture `const v = ++this.previewVersion` and bail on
`if (v !== this.previewVersion) return` in both the success and `finally` paths —
`explore-view.ts:703/793/821/826` does exactly this correctly.

### M9. Raw Go error strings are rendered to the user in six places
No error is ever mapped to human copy. Verbatim `err.Error()` / `String(err)` reaches
the UI at:

| Location | What the user sees |
|---|---|
| `explore-album-details.ts:1755, 1811` (set at `912, 969`) | `Get "https://musicbrainz.org/ws/2/…": context deadline exceeded` |
| `explore-artist-details.ts:2023, 2296` (set at `1294, 1466`) | same class of string |
| `explore-view.ts:1276` (set at `823, 848`) | same |
| `config-page.ts:1142` | `Failed to remove 'Music': sql: database is locked` |
| `config-page.ts:1514` | index tier `${t.error}` verbatim |
| `autotag-view.ts:1289, 1651` | `Apply failed: build plan: …` |
| `download-picker.ts:141, 159`; `download-clients.ts:645, 668, 690` | `String(err)` verbatim |
| `first-run-wizard.ts:239, 259` | `Could not add the folder: ${err}` |

These come straight out of `musicbrainzws2` / `net/http` / `database/sql`
(`backend/explore/musicbrainz.go:267-285` returns the client error unwrapped), so the
string is a Go stack-flavoured HTTP error, not a sentence.

Fix: introduce a small `describeError(err)` helper in `frontend/src/utils/` that maps
the handful of recognisable cases (offline, timeout, not found, permission) to copy and
falls back to a generic line, and route all eight sites through it. Keep the raw text
in `console.error` for debugging.

Genuine counter-example worth preserving: `download-store.ts:337-341` deliberately lets
`TestProvider`'s message through, and documents why — that one is the user's debugging
tool for a misconfigured client. That is the exception, not the rule.

---

## Minor

### m1. Every queue and player action is fire-and-forget
**`frontend/src/store/queue-store.ts:192-266`**, **`frontend/src/store/player-store.ts:96-114`**.

Twenty binding calls (`Queue.Play`, `Queue.SetQueue`, `Queue.Clear`, `Queue.RemoveTracks`,
`Player.Pause`, `Player.LoadFile`, `Player.Seek`, `Player.SetVolume`, …) are invoked
with no `await`, no `.catch()`, and no `void`. Wails still returns a promise, so a
rejection (which happens if the bridge is torn down, or the arg fails to marshal)
becomes an unhandled rejection.

Mostly benign today because the Go methods return nothing (see **C1**), but it means
these methods cannot report failure even after C1 is fixed.

Fix: as part of the C1 fix, change the queue methods to return `error` and have the
store `.catch()` them.

### m2. Favorite toggles revert silently
**`frontend/src/store/favorites-store.ts:137-158`** (and `160-190` for the batch forms).

The optimistic update and its revert are both correct, but the revert is invisible.

Symptom: click the heart, it fills, and half a second later it empties again with no
explanation.

Fix: on the revert path, surface a one-line message.

### m3. Clearing the queue has no confirmation and no undo
**`frontend/src/components/queue-panel/queue-panel.ts:683-685`** →
`queue-store.ts:262` → `backend/queue/queue.go:1138`, which stops playback and
discards the list.

Not catastrophic (the queue is reconstructable), but it is the only mutation in the
panel with no way back, and it sits next to routine controls.

Fix: either confirm when the queue is non-trivially long, or keep the last cleared
queue in memory behind an "Undo" affordance.

### m4. Removing a download client provider has no confirmation
**`frontend/src/components/config-page/download-clients.ts:684-692`**.
Deleting a provider discards its stored credentials
(`backend/download`'s `FileSecretStore`), which cannot be recovered.

Fix: confirm, naming the client.

### m5. `AddLibrary` / `RenameLibrary` failures are console-only
**`frontend/src/components/config-page/config-page.ts:1058-1072`** (add),
**`1082-1097`** (rename). Both `console.error`. Note that *removal* — the more
dangerous operation — is handled correctly in the same file (impact preview at
`1105-1114`, confirmation, `isRemoving` guard, toast at `1129-1143`).

Fix: route these two through the existing `showToast` (`config-page.ts:1168`).

### m6. Autotag warning/skip/leave dialogs stall on a rejected binding
**`frontend/src/components/autotag-view/autotag-view.ts:1328-1334`**
(`onWarningContinue` → `await AckLibraryWarning(...)`),
**`1336-1342`** (`onLeaveConfirm` → `await LeaveAsIs(...)`),
**`1660-1664`** (`onSkip` → `await Skip(...)`).

None is wrapped. A rejection means the lines after the await — including
`this.dialog = 'none'` — never run.

Symptom: press "Continue" on the destructive-write warning and the dialog just sits
there.

Fix: try/catch each, close the dialog in a `finally`, and surface the error.

### m7. Add-to-playlist fails silently after a correct in-flight guard
**`frontend/src/components/playlist-picker/playlist-picker.ts:164-193, 216-231`**.

The `this.loading` guard is right (no double-add), the create button is disabled while
in flight (`playlist-picker.ts:321`) — but the failure path is `console.error` and the
picker just closes.

Symptom: the tracks appear not to have been added, and the user cannot tell whether to
retry.

Same shape at `playlist-details.ts:396-412` (remove tracks), `414-438` (remove
phantoms), `584-601` (remove one phantom).

### m8. The download search cannot be cancelled
**`frontend/src/components/download-picker/download-picker.ts:127-148`**.

`downloadStore.start()` queries every enabled provider. The dialog shows a spinner and
"Searching your download clients…" but the only exit is Close, which does not cancel
the backend work. `search()` also has no stale guard, so a close-and-reopen for a
different album can be overwritten by the first search's result.

Otherwise this file is the strongest failure UX in the codebase — see **What is
already right** below.

---

## Polish

### p1. `console.log` debug output left in shipped views
`explore-album-details.ts:667, 673, 680, 695, 715, 877`;
`explore-artist-details.ts:1017, 1030, 1037, 1071`;
`config-page.ts:1019` (`'IndexStatusChanged event received'`).

### p2. Long-running operation coverage is inconsistent by subsystem

| Operation | Progress | Cancel | Pause/resume | Survives quit |
|---|---|---|---|---|
| Library scan | ✅ jobs registry | ✅ | ✅ | ✅ paused scans restored (`backend/library/scan_jobs.go:300`) |
| Index build | ✅ | ✅ (confirmed, `job-controls.ts:41`) | ✅ | ✅ checkpointed |
| Downloads | ✅ (`download/manager.go:192`) | ✅ | — | ✅ swept on restart |
| Batch tag write | ✅ event | ✅ (`track-details.ts:1767`) | — | ❌ not in registry |
| **Autotag apply** | ⚠️ page-local only | ❌ | ❌ | ❌ (see **C3**) |
| **Download search** | spinner | ❌ | — | ❌ (see **m8**) |
| **Requests reconcile** | `checking` flag (`downloads-view.ts:503`) | ❌ | — | — |

The pattern is clear: everything routed through `jobs.Registry` gets progress, cancel
and a global indicator for free. The three gaps are the three things not registered.

### p3. `EventsOff` is global
**`frontend/src/components/track-details/track-details.ts:1765`** calls
`EventsOff(Events.BatchWriteProgress)`, which removes *all* listeners for that event,
not just this component's. Correct today (single listener) but fragile; prefer the
unsubscribe function `EventsOn` returns, as `jobs-view.ts:246-249` does.

### p4. `OnBeforeClose` never asks
**`backend/app.go:445-484`** always returns `false`. Quitting during a full rescan
leaves the library partially rebuilt — recoverable, because the soft scan re-runs on
next launch (`backend/app.go:568`), but playlists are not restored until that scan
completes (`RestoreAllPlaylists` only runs from the `PostScan` hook,
`backend/app.go:341`). Worth a confirm while a destructive job is running.

---

## What is already right (keep these as the templates)

- **`frontend/src/components/download-picker/download-picker.ts`** — distinct
  searching / auto-picked / empty ("Nothing found. Try a different spelling…") /
  error bodies, an in-flight `picking` guard on `onPick` (`154`), and a footnote that
  explains *why* it is asking rather than deciding (`243-262`). This is the standard
  the rest of the app should be measured against.
- **`frontend/src/components/home-view/home-view.ts:263-280`** — the only place that
  correctly distinguishes loading, failed, and genuinely-empty in three separate
  bodies.
- **`frontend/src/components/explore-view/explore-view.ts:703, 793, 820-828`** — a
  correct monotonic request-version guard on search-as-you-type, checked on the success
  path, the catch path *and* the `finally` that clears the spinner. This is the fix
  pattern for **C4** and **M8**.
- **`frontend/src/components/track-details/track-details.ts:1706-1766`** — the best
  destructive flow in the app: an explicit change summary, a confirmation step, live
  per-file progress, a working cancel, and a per-file failure list afterwards.
- **`frontend/src/components/config-page/config-page.ts:1105-1143`** — removal shows a
  computed impact (`GetRemovalImpact`) *before* asking, guards with `isRemoving`, and
  reports the outcome. The right shape; only the raw error string (**M9**) lets it down.
- **`frontend/src/components/catalog-scope-notice/catalog-scope-notice.ts`** — a
  purpose-built component whose entire job is to admit what the user is looking at, with
  Retry offered only in the one scope where retrying means anything.
- **`backend/library/scan_jobs.go:265-345`** — paused scans survive a restart, and a
  pause that outlived the process resumes as an incremental rescan with a log line
  saying so.
- **`frontend/src/store/favorites-store.ts:137-158`** — optimistic update with a
  correct revert. Only the silence (**m2**) is wrong.

---

## Suggested order

1. **C1** + **C2** — playback failure is the app's core job; it currently fails mute.
2. A minimal app-level notification surface, then route **M9**'s eight sites,
   **M5**, **M6**, **M7**, **m2**, **m5**, **m7** through it. Most of these findings
   are one problem wearing thirty hats.
3. **M3**, **M2** — two permanent fake "loading" states.
4. **C4** + **M1** + **M8** — the three async-correctness bugs; all three are the same
   version-guard fix, and `explore-view.ts` already contains the reference
   implementation.
5. **C3** — register the autotag apply with `jobs.Registry` and it inherits progress,
   cancel and the global indicator at once.

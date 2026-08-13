# 007 — UI reconciliation: lifecycle, truth, voice, scale, shape, and one thing that was never built

**Status:** implemented — all six phases shipped.
**Branch:** main
**Created:** 2026-08-11
**Follows:** 006-orientation-fixes
**Followed by:** 008-the-last-audit
**Source:** `.planning/audits/2026-08-11-ui/` — `hands-on.md` (24
findings reproduced in the running app), `a11y.md` (34), `perf.md`
(30), `errors.md` (30).

## Recap

~118 findings, which were five problems each spread by copying. All six
phases shipped, over eleven passes, and three of the four source audits
are closed: `hands-on.md`, `perf.md` and `errors.md` have nothing open.
`a11y.md` does, and is plan 008.

- **Phase 1 — a cached view now has a lifecycle.** `viewActivated` /
  `viewDeactivated`, the ambient shortcut scope the mechanism was built
  for and had never had a caller, and keyboard reach for the sidebar,
  the track rows, the card grids and the closed queue panel.
- **Phase 2 — the player reports its own position.** 1 Hz from the
  backend, `PlaybackFailed` from both failure paths, auto-advance that
  skips, and a bar that no longer counts itself 30 s adrift.
- **Phase 3 — one notification surface, four levels**, replacing 84
  `catch` blocks that ended at `console.error` and two private toasts.
- **Phase 4 — works offline and at 50 000 tracks**, over six passes:
  bundled icons, route splitting, the `TrackPlayCountChanged` split,
  bounded caches, `utils/track-index.ts` (3–6 s → 68 ms), and the
  virtualizer repaint rule.
- **Phase 5 — one app, not eleven pages**, over four passes: one
  `<page-header>`, a measured window minimum, every dialog a named
  `wa-dialog`, one menu keyboard model, the `?` overlay, and an album
  page with a primary action that means the same thing in three
  different ownership states.
- **Phase 6 — Explore opens with shelves**, plus the two inherited
  one-liners: the badge that was an inert button, and the card grids
  that moved to the end rather than by a row.

**What it is worth reading for**: the seven "where the plan was wrong"
lists below, seventy-nine entries across the passes. About a third are
the audit being wrong rather than the code, and they are the reason
plan 008 treats every remaining `a11y.md` claim as a hypothesis.

**Inherited, unfinished, and carried into 008**: `tracklist.delete`,
which survived all six phases because it needs a "remove from library"
operation that does not exist and a decision about what it removes.

## Problem

A full pass over the UI — the app driven by hand headless, plus three
read-only static reviews — turned up ~118 findings. They are not 118
problems. They are **five**, each of which has spread by being copied
rather than fixed:

1. **A view that is off-screen is still running.** `index.ts:74` caches
   primary views and hides them with a class, deliberately, so
   `scrollTop` survives navigation. Nothing else was told. So
   `disconnectedCallback` never fires for a cached view, and everything
   that was written to clean up there — document listeners, intervals,
   subscriptions — never cleans up. The worst case is not a leak: it is
   that pressing `s` on the **Settings** page skipped two albums out of
   the Autotag queue, because `autotag-view`'s document keydown handler
   is still live. `a` on that same handler rewrites tags on disk.

2. **The player does not report what it is doing.** The seek bar is a
   `setInterval` counter that increments by 1/second and reconciles
   with the backend only on track change. Measured 3 s behind during
   steady playback; **30 s behind after four keyboard seeks**, because
   the seek shortcut never tells the bar. A track that fails to load is
   a silent no-op — `queue.go:1181` logs and returns `false`, the
   bindings return `void`, and nothing is emitted. `SeekFailed` *is*
   emitted and has no listener.

3. **Failure has no voice.** There is no app-level notification
   surface. Two components grew private toasts; the other 84 `catch`
   blocks end at `console.error`. A user with a moved file, a locked
   database or an offline network sees a button that does nothing.
   Where errors *do* surface, eight sites print the raw Go string.

4. **Nothing was built for a large library or a closed network.** Every
   `<wa-icon>` is fetched from `ka-f.fontawesome.com` at runtime, so
   the app has no icons offline. Finishing a track invalidates and
   refetches the entire library because `TrackMetadataChanged` means
   both "tags rewritten" and "play count +1". Two caches are unbounded.
   The bundle is one 1.18 MB chunk containing all 27 views.

5. **The shape of the app disagrees with itself.** Four views have a
   page heading and four do not. Two have sort controls. The header
   search looks global and is view-scoped — searching "tide" on
   Playlists reports "No playlists match your search" with three
   *Tideline* tracks in the library. The track list is 40 px wider than
   its container by arithmetic, so the last column is always clipped.
   The app never lands on Home. An album page has no way to play the
   album.

Cutting across all five: **the app cannot be used without a mouse.**
Fourteen tab stops exist, all of them chrome.

## Ordering principle

Not by severity — by *blast radius, then by dependency*.

Phase 1 first because it is the only finding that loses user data, and
because the lifecycle fix is a precondition for a third of everything
else (the leaks, the wasted renders, the double-fired shortcuts).

Phase 2 next because the seek bar and silent playback failure are the
same surface — the one the user looks at most — and both are lies about
state the backend already knows.

Phase 3 third because it is the enabling work for a long tail: ~30
findings are "the failure is invisible", and they cannot be fixed one
at a time until there is somewhere to put a message.

Phase 4 and 5 are independent of each other and of 1–3.

Phase 6 is last and is the odd one out: it is the only phase that adds
something rather than fixing something. It sits here rather than in its
own plan because it depends on Phase 3's notification levels and
Phase 5's page header, and because the audit found the Explore page's
emptiness to be a UX failure of the same kind as the rest — the app
knowing something and not saying it.

Each phase is independently shippable and independently verifiable.
None of them is a refactor of something that works.

---

## Phase 1 — A view that is not on screen is not listening

### What's wrong

`H-1`, `H-2`, `H-5`, `H-6`, and the root cause behind `perf.m3`,
`perf.M1`, `perf.M6`, `perf.p6`.

- Cached views never deactivate. `autotag-view`'s document keydown
  fires from every other page; `downloads-view`'s 30 s interval ticks
  forever; `config-page` re-renders every 3 s for the session.
- Two document keydown listeners fire for the same key with no
  arbitration: on Autotag, `s` skips the album *and* toggles shuffle;
  `↑`/`↓` move the folder selection *and* change the volume by 5.
- `data-shortcut-scope` is read by `keyboard-shortcut-service.ts:147`
  and **set nowhere in the codebase**, so `resolveScope` can only
  return `text-input` or `global`, and the two panel-scoped bindings
  (Enter = play, Delete = delete) are dead — while Settings advertises
  them as configurable.
- Global bindings are unmodified `Space N P S R M / Q ↑ ↓ ← →` with
  `preventDefault()`, so a focused button cannot be activated with
  Space and a `<select>` cannot be arrowed through.
- Only chrome is focusable. Sidebar items are bare `<li @click>`;
  track rows, cards and context menus have no keyboard path at all.

### What ships

**A view lifecycle.** Keep the cache — preserving `scrollTop` is a real
benefit and unmounting would throw it away. Add the missing half:
`index.ts`'s navigate handler calls `viewDeactivated()` on the outgoing
element and `viewActivated()` on the incoming one, and the primary
views move their document listeners, intervals and event subscriptions
out of `connectedCallback`/`disconnectedCallback` and onto those.
A small mixin or base class so the pattern is written once, not eleven
times.

**One keyboard authority.** `autotag-view` stops owning a document
listener. Its A/S/L/U/F/↑/↓ become panel-scoped bindings registered
through `shortcuts`, and the panel sets `data-shortcut-scope="autotag"`
on its root — which is what the scope mechanism was built for and has
never had a caller. Same for the track list
(`data-shortcut-scope="tracklist"`), which resurrects Enter and Delete.

**Global shortcuts stop stealing keys.** *(Decided: the unmodified
single-key bindings stay.)* Suppress a global binding when the deep
active element is a control that owns the key itself — button, select,
slider, checkbox, `[role=menuitem]`, or anything inside an open dialog.
`getDeepActiveElement` already resolves through shadow roots correctly;
only the predicate needs widening beyond text inputs.

Keeping them raises the cost of their being undiscoverable, so Phase 5
picks up a `?` shortcuts overlay — today the only way to learn that `S`
is shuffle is to go read Settings.

**Keyboard reachability, in the four places it matters:** sidebar
nav (`<nav>` + `<button>` per item, keeping `aria-current`), track rows
(roving tabindex, `role=row`/`gridcell`, Enter to play), cards
(`artists-view`, `genres-view`, `cover-grid`, `top-results-row` — three
of the four already have `role=button tabindex=0`; make the fourth
match and add roving tabindex so the tab sequence is not the length of
the grid), and the context menu (`role=menu`, Shift+F10 to open, focus
the first item, Arrow/Escape, restore focus on close).

**The closed queue panel goes `inert`**, so its buttons stop taking tab
stops and stop being read by screen readers.

### Verification

- A `make ui-test` case per view asserting the document listener count
  does not grow across a simulated navigate cycle.
- An e2e spec that is the H-1 reproduction: open Autotag, note the
  pending count, navigate to Settings, press `s`, assert the count is
  **unchanged** and that `QueueModeChanged` fired exactly once.
- A tab-order spec asserting the first stop after the header is a
  sidebar item and that a track row can be reached and played with the
  keyboard alone.

### Not in this phase

ARIA correctness that is not about *reachability* — the live regions,
the `aria-sort`, the dialog semantics. Those are Phase 5.

### Phase 1 — what actually shipped

Shipped:

- **The view lifecycle.** `utils/view-lifecycle.ts` — a mixin with
  `viewActivated`/`viewDeactivated`, `listenWhileActive`,
  `intervalWhileActive` and `whileActive`, driven by `index.ts`'s
  navigate handler. All eleven cached views adopted it. Two things
  the plan did not anticipate: an off-screen view also had to stop
  *rendering* (shared store controllers `requestUpdate()` every cached
  view on every search keystroke), and a shared reactive controller
  needed the same treatment — `ContextMenuController` bound three
  document listeners in `hostConnected`, which for a cached host is
  "forever". `registerViewAware` gives a controller the view lifecycle
  when its host has one.
- **One keyboard authority.** `autotag-view` no longer owns a document
  keydown listener except for Escape (its hand-rolled dialogs, which
  Phase 5 migrates to `wa-dialog` anyway). A/S/L/U/F/↑/↓ are
  `autotag.*` panel bindings, and the track list claims `tracklist`,
  which resurrects Enter.
- **The ambient scope**, which the plan did not have. `resolveScope`
  walks up from the focused element, and this app is driven with the
  mouse — focus sits on `<body>`, so a focus-only rule would have made
  the autotag keys work only after a click landed inside the panel,
  a regression against today. The active view claims its scope as a
  fallback (`services/shortcut-scope.ts`), released on deactivation.
- **Global shortcuts yield to the focused control** — button, select,
  slider, checkbox, menu, grid row, or anything inside an open dialog.
- **Reachability**: sidebar (`<nav>` + `<button>`, `aria-current`
  preserved), track rows (`role=row`/`gridcell`, roving tabindex,
  arrows/Home/End, Enter plays), the three card grids (roving tabindex
  via `utils/roving-grid.ts`, arrows moving by measured column count),
  `top-results-row` brought level with the other three cards, and the
  closed queue panel made `inert`.

**Deferred, deliberately: the context menu's keyboard model**
(`role=menu`, Shift+F10, focus the first item, Arrow/Escape, restore
focus). It is the one item here that is a new interaction model rather
than an attribute or a lifecycle, it touches every host that renders a
menu, and Phase 5 is already migrating the hand-rolled dialogs to
`wa-dialog` — the two should be one pass with one focus-management
implementation, not two.

**`tracklist.delete` is still dead**, and is now the only advertised
binding with nothing behind it. There is no "remove from library" in
this view to bind it to, and adding a destructive action with no
confirmation would walk straight into what Phase 3 exists to fix.

---

## Phase 2 — The player tells the truth

### What's wrong

`H-3`, `H-16`, `H-17`, `H-18`, `errors.C1`, `errors.C2`, `errors.m1`,
`errors.m2`.

Measured, twice:

| | UI | backend |
|---|---|---|
| steady playback, +10 s | 00:47 → 00:57 | 50 → 60 |
| after 4× `→` (seek +5 s) | 00:08 → **00:10** | 11 → **40** |

And the failure path: `loadCurrentTrack`/`playCurrentTrack`
(`queue.go:1181`) log an error, return `false`, the caller reverts
`currentIndex` and returns. Nothing is emitted; the bindings return
`Promise<void>`; `queue-store.ts:192` does not await them anyway.
Double-clicking a moved file does nothing, twice, forever. Auto-advance
onto a bad file stops playback dead, and Next does not help because
Next hits the same track and reverts.

### What ships

**Position comes from the backend.** The player emits a position tick
(1 Hz while playing, and immediately after any seek, pause, resume or
track change), and `seek-bar` renders what it is told instead of
counting. The local interval survives only as interpolation *between*
ticks, reset by every tick — so it can be at most one tick wrong and
can never accumulate. This also fixes the keyboard-seek desync for
free, without the shortcut service needing to know the seek bar exists.

**`PlaybackFailed{filePath, reason}`**, emitted from both load and play
failure paths. The frontend surfaces it (Phase 3's surface, or a plain
inline message if Phase 3 has not landed) and auto-advance **skips**
the failed track instead of reverting — with a guard against skipping
the whole queue when every file is missing.

**`SeekFailed` gets a listener** in `player-store`, reverting the
optimistic position. It is currently emitted into the void.

**Queue and player bindings return `error`**, and the store `.catch()`es
them — twenty fire-and-forget calls today, which is why C1 could not be
reported even if it wanted to.

**The bottom bar stops lying about smaller things:** label the
countdown (or make it click-to-toggle against total duration — it
currently reads `01:21` next to a track the list calls `01:30`); let
the now-playing text use the ~400 px of empty space next to it instead
of truncating the artist to "The Orchestra Of"; and when a queue ends,
keep the finished track visible and paused at 0:00 rather than blanking
the bar while the queue panel still lists it.

**Favourite reverts say so** (`favorites-store.ts:137` reverts
correctly and silently).

### Verification

- A Go test on the emit path (the `backend/queue/emit_test.go` model)
  asserting `PlaybackFailed` for a missing file and that the queue
  advances past it.
- An e2e spec seeding a queue with a deleted file between two good
  ones, asserting playback reaches the third track and a message
  appeared.
- An e2e spec asserting UI elapsed time tracks
  `Player.CurrentPositionSeconds` within 1 s across a seek — the exact
  measurement that failed by 30 s.

### Phase 2 — what actually shipped

Shipped:

- **Position comes from the backend.** `PlaybackPositionChanged`
  (payload `player.PositionInfo`) is emitted at 1 Hz while playing and
  immediately on load, play, pause, seek and natural finish.
  `seek-bar` renders what it is told; its `setInterval` survives only
  as interpolation between reports and is stopped and restarted by
  every one of them, so its error is bounded by a second and is
  discarded rather than carried. Measured after the fix, in the
  running app: UI `00:34` / backend `34` after two keyboard seeks,
  against `00:44` / `73` before it.
- **`PlaybackFailed{filePath,title,artist,reason}`**, emitted from both
  the load and the play failure path, and **auto-advance skips**:
  `playCurrentOrSkip` steps forward (or backward, for Previous) over
  tracks that will not load, bounded by the queue length so a
  disconnected drive stops after one pass instead of spinning through
  a `RepeatAll` wrap.
- **`SeekFailed` has a listener**, and the backend now also emits it
  when the seek itself fails rather than only when nothing is loaded —
  followed immediately by a position report, so the optimistic move
  is taken back by the same mechanism that fixed H-3.
- **An inline message strip in the player bar** — one line, dismissible,
  self-dismissing after 8 s, coalescing `(kind)` within a 10 s window
  so 200 unplayable files read "Skipped 200 tracks that could not be
  played." It is `role=status`/`aria-live=polite` and lives on
  `player-store`, deliberately *not* a notification store.
- **H-16**: the right-hand clock carries a minus sign, a title, an
  accessible name, and toggles to total duration on click.
  **H-17**: the now-playing column starts at 320 px (max 500) instead
  of 200 (max 350), which is where "The Orchestra Of" came from.
  **H-18**: a queue that simply ran out no longer unloads the player,
  so the finished track stays on the bar at 0:00 — `onQueueExhausted`
  takes an `unload` flag, and only the cases where the track really is
  gone (removed from the queue, queue cleared) pass true.

Four things the plan did not anticipate:

- **Phase 1 changed the H-3 reproduction.** With a track row focused,
  the arrows belong to the grid, so the seek shortcut does not fire
  from the track list at all — the measurement only reproduces with
  focus off the grid. Keyboard seeking being unavailable while a row
  is focused is a real (new, minor) gap; it belongs with Phase 5's
  shortcuts overlay, not here.
- **A position report needs to say which track it belongs to.** The
  store is a singleton that keeps the last report, so a seek bar
  mounting later would otherwise adopt a stale one; `PositionInfo`
  carries `trackChangeId` and the bar ignores anything else. It also
  carries a `seq`, because "the same second, reported again" still has
  to reset the interpolation.
- **The message could not be laid out inside the bar.** `.bottom-bar`
  is a fixed `4em` grid row, so a message in the flow squeezed the
  transport out of its own footer; it floats above the bar instead.
- **Returning `error` from the queue bindings was dropped**, and the
  `.catch()` half of `errors.m1` was kept. The queue's failures are
  now reported by event, which covers the callers a return value never
  could (auto-advance has no caller), and `PlayIndex` deliberately
  still reverts rather than skipping — the user picked *that* track.
  Every queue and player binding call in the stores now has a
  `.catch()`, so a torn-down bridge is logged rather than an unhandled
  rejection.

**Deferred, deliberately: "favourite reverts say so"** (`errors.m2`).
It is a Transient toast by this plan's own table, and the only surface
that exists after this phase is the player bar's — inline, and about
the player. Routing a favourite through it, or building a second
private toast, is exactly what Phase 3 exists to delete. It moves to
Phase 3 with the other Transient callers.

---

## Phase 3 — Failure has a voice

### What's wrong

`errors.M1`–`M9`, `errors.C4`, `errors.m1`–`m8`, `H-12`, and the
"Loading tracks…" family.

165 `catch` blocks; 84 end at `console.error`. Two private,
mutually-unaware toasts (`config-page.ts:1168`,
`autotag-view.ts:1318`). Eight sites render raw Go strings —
`Get "https://musicbrainz.org/ws/2/…": context deadline exceeded` is
shown to a person. Three permanent fake loading states: the track list
cannot tell empty from loading from failed
(`track-list.ts:1901` — visible on the *first screen a new user ever
sees*, behind the first-run wizard), and the Settings index panel says
"Loading status…" forever because `indexStatus` is only ever set from
an event that fires on change, and `GetIndexStatus()` is never called.

Three async races, all the same shape and all with a correct reference
implementation already in the repo (`explore-view.ts:703/793/821`):
the library-filter switch caches the previous library's tracks; the
smart-playlist preview settles on the previous rule set; the four
`waitFor*` helpers never settle on a rejected fetch and leak a
subscriber each time.

### What ships

**One notification surface, with four levels** — a store plus a host
component implementing Blocking / Persistent / Transient / Inline as
specified under Decisions. All four ship together: they are one
component with four presentations, and building them piecemeal is how a
call site ends up picking the wrong one because the right one does not
exist yet.

The level is chosen by the **caller**, from the rule in Decisions —
a failure is only worth interrupting for if the user can do something
about it that they are not already doing. Then route the existing
silent sites through it: scan/rescan failures, playlist delete,
download request removal, job control, add-library/rename,
add-to-playlist, favourite revert, autotag dialog failures.

**Coalescing is part of the surface, not of each call site.** The store
dedupes by `(level, key)` within a window and renders a count, so a
queue of 200 unplayable files produces one message rather than 200.
Phase 2's `PlaybackFailed` is the first consumer and the reason this is
not deferred.

**`describeError()`** in `frontend/src/utils/` — maps the recognisable
cases (offline, timeout, not found, permission denied, database
locked) to a sentence and falls back to something generic, with the
raw text kept in `console.error`. Route all eight raw-string sites
through it. One documented exception:
`download-store.ts:337` deliberately passes the provider's own message
through, because that string is the user's debugging tool for a
misconfigured client.

**Loading / empty / failed become three states, not one**, starting
with `track-list` and `genre-details`. `home-view.ts:263` already does
this correctly and is the model. Seed the Settings index panel with
`GetIndexStatus()` on connect.

**A request-version guard** on the three racing paths, copied from
`explore-view`. `waitFor*` gains a reject path so a failed fetch stops
hanging its waiters.

**Confirmation on the destructive actions that have none:** playlist
delete (including the multi-select loop, which deletes N playlists with
no prompt), download-request removal, download-client removal. The
codebase already has the right shape twice — `config-page.ts:1105`
shows a computed impact *before* asking, and `track-details.ts:1706`
does summary → confirm → progress → per-file failure list. Match those,
do not invent a third pattern.

**Autotag apply joins `jobs.Registry`**, which is where its missing
progress, cancel and global indicator already exist for everything
else. Today it is a bare goroutine whose progress lives in a component
field that is discarded on navigation, with no cancel and no record of
where it stopped if the app quits mid-write. `OnBeforeClose` returns
`false` unconditionally; it should ask while a file-writing job is in
flight.

### Verification

- `make ui-test` for the notification store and `describeError`'s map.
- An e2e spec that induces a binding failure via `/__test/` and asserts
  a message with actionable text appears — not a console line.
- A spec for the library-filter race: switch A→B→A quickly, assert the
  list matches the final selection.

### Phase 3 — what actually shipped

All three reproductions were written first and failed first: the
library-filter race and the never-settling waiter as store tests
(`frontend/test/stores/library-store.test.ts`), `describeError`'s map as
a test against a module that did not exist, and a binding failure
induced through `/__test/sql` as `e2e/specs/failure-voice.spec.ts`.

Shipped:

- **One surface, four levels.** `store/notification-store.ts` owns the
  notifications and the coalescing; `components/notifications/` renders
  them — `notice.ts` is the one presentation, `notification-host.ts`
  renders Blocking (a `wa-dialog`) plus the Persistent/Transient stack,
  and `<inline-notice region="…">` renders the fourth level wherever
  the region lives. Coalescing is by `(level, region, key)` within a
  10 s window, so 200 unplayable files are one line with a count.
- **The player's strip folded into it.** Phase 2's `player-store`
  message was the one existing Inline consumer with its own coalescing;
  keeping both would have been two implementations of the same rule,
  quietly disagreeing. `player-store` now raises
  `notificationStore.inline('player', …)` and `audio-player` renders
  `<inline-notice region="player" floating>`. Same testid, same
  sentences, same 8 s, one implementation.
- **`utils/describe-error.ts`**, plus `explainError` — which the plan
  did not have. Some backend errors *are* sentences (the sentinels this
  app writes: "a library with that name already exists"), and mapping
  those to a generic line would have been a regression; `explainError`
  repeats a message with no runtime-noise markers and falls back to
  `describeError` otherwise. All eight raw-string sites route through
  one or the other, with `download-clients`' connection test kept
  verbatim as the documented exception.
- **The silent sites now speak**: scan/full-rescan (`M5`, with a
  `starting` guard against the double-click the 250 ms coalescing
  allowed), job pause/resume/cancel (`M4`), playlist delete (`M6`),
  download request pause/remove/clear (`M7`), add/rename library
  (`m5`), add-to-playlist and playlist track removal (`m7`), favourite
  reverts (`m2`, deferred here from Phase 2), autotag's dialogs (`m6`)
  and its apply.
- **Both private toasts are gone** (`config-page`, `autotag-view`),
  along with their CSS and `@keyframes`.
- **Three states, not one**: `track-list` and `genre-details`
  distinguish loading, failed (with a retry) and genuinely empty, which
  is also what the first-run screen shows now (`M2`, `H-12`); the
  Settings index panel seeds itself with `GetIndexStatus()` and has a
  failed state with a retry (`M3`).
- **The three races**: the library store guards every fetch with a
  cache generation and holds the request itself instead of deriving a
  promise from subscriber notifications — which fixes `C4` and `M1`
  together, since they are the same bug seen from either end.
  `smart-playlist-editor` and `download-picker` got `explore-view`'s
  version guard (`M8`, `m8`'s stale half).
- **Confirmations** on playlist delete (single *and* the multi-select
  loop), download-request removal, download-client removal, and a
  queue clear over 20 tracks — through one `confirmAction()` helper
  built on `wa-dialog`, so they inherit the focus trap and Escape the
  hand-rolled overlays do not have.
- **Autotag apply joins `jobs.Registry`** (`C3`): `jobs.KindAutotagApply`,
  progress, a cancel wired to the apply's context, and a terminal state
  that tells cancelled from failed. `OnBeforeClose` asks before quitting
  while an apply is writing (`p4`).
- **`p1`/`p3`**: every `console.log` is out of the shipped views, and
  `track-details` unsubscribes with the function `EventsOn` returned
  rather than `EventsOff(name)`, which removed every listener.

Four things the plan did not anticipate:

- **The bottom band belongs to the player.** The stack started above
  the player bar, next to the player's own floating notice, and at
  800×600 a two-line inline message grew straight into it. The stack
  moved under the header (top-right) and the player's notice is
  left-anchored at half width. Anything anchored to the bottom is
  sharing a band with something whose height is not known in advance.
- **A level is not enough to place a message; a region is.** "Inline"
  says *not global*, not *where*, so an inline notification carries a
  region and the host ignores it. Without that, the one component with
  four presentations would have been two components with two stores.
- **Blocking needed a rule the table did not state.** `errors.C3` is
  Blocking, but only when the apply half-succeeded: nothing written is
  something to retry (Persistent), a mix of old and new tags on disk is
  something to interrupt for. The distinction is in
  `onApplyFinished`, not in the level table.
- **A failing reproduction can pass for the wrong reason.** The first
  version of the e2e spec renamed the *decoy* library to its own name,
  which the backend accepts — it failed at the right assertion while
  never inducing the failure. It now picks the row by the seeded
  library's name from `/__test/health`.

**Deferred, deliberately:** the download search's *cancel* (`m8`'s
other half) — the stale guard shipped, but cancelling means propagating
a context into every provider search, which is backend work in
`backend/download` rather than failure UX. And the autotag apply is
registered but not **durable**: quitting mid-apply now asks, and the
job is cancelled cleanly, but nothing records where it stopped for the
next launch. Both belong with the download/jobs work, not here.

**One new finding, not fixed:** clicking a library's name in Settings to
rename it opens the editor and closes it in the same click, because the
name's click bubbles to `config-page`'s own document handler. The
overflow menu's Rename works (it stops propagation). It is a one-line
fix in a file Phase 5 is already reworking.

---

## Phase 4 — Works offline, works at 50 000 tracks

### What's wrong

`H-4`, `H-14`, `perf.C1`, `perf.C2`, `perf.C3`, `perf.C5`, `perf.M1`–
`M10`, `perf.m1`–`m7`.

- **Icons come from the internet.** Confirmed from
  `performance.getEntriesByType('resource')`:
  `https://ka-f.fontawesome.com/releases/v7.1.0/svgs/solid/house.svg`
  and 35 more. `setBasePath()` does not affect the icon resolver and no
  `registerIconLibrary` call exists. Offline, the app has no icons.
- **Finishing a track refetches the whole library.** `recordPlay` emits
  `TrackMetadataChanged`; `library-store.ts:445` treats that as "tags
  were rewritten" and invalidates + eagerly refetches tracks, albums,
  artists and genres. At 50 k tracks that is ~25 MB of JSON across the
  IPC, parsed on the main thread, **once per song** — and it clears the
  user's track selection while it does (`track-list.ts:1246`), so
  selecting 40 tracks to drag into a playlist is impossible while music
  plays.
- **Toggling one heart refetches every track of every playlist.**
- Unbounded caches: `explore-view`'s `thumbnailCache` retains base64
  data URLs (~40–66 kB of heap per album, forever);
  `explore-cache.ts:35` has four `Map`s with no eviction.
- 1.18 MB single chunk, all 27 views eagerly imported and
  side-effect-evaluated before first paint. `autotag-view` alone is
  76 kB and is reachable only from a sidebar click.
- `IndexStatusChanged` every 3 s forever (`searchindex.go:276`), with
  an identical payload and a `console.log` per tick.
- Playlist and smart-playlist track lists are not virtualized and
  rebind ten thousand listeners per render; `track-list` and
  `genre-details` already show how to avoid this via `.externalTracks`.
- The track list's Art column renders the **original** cover art —
  commonly 1500×1500 — into a 24 px box, with no `loading="lazy"`,
  while `CoverArtSmall` sits unused on the same model.

### What ships

Roughly in value order, each independently landable:

1. **Bundle the icons.** Register a local library against the SVGs
   already in `src/assets/images/icons/`. This is the difference
   between working and not working offline.
2. **Split `TrackMetadataChanged`** into it and `TrackPlayCountChanged`,
   and patch the one track in place. Then stop `loadTracks()` clearing
   the selection — the selection is keyed by `FilePath`, which survives
   a refetch.
3. **Stop the 3 s ticker** when nothing is building; emit on change.
   Delete the `console.log`.
4. **Bound the two caches** with an LRU, or return a
   `/coverart/<mbid>` URL so the browser's own cache handles eviction.
5. **Route-level code splitting** — `index.ts`'s navigate handler
   already creates views lazily; only the imports are eager.
6. **Correct thumbnail tier + `loading="lazy"`** in the Art column, and
   the artist-avatar fallback becomes a `Map` lookup instead of a
   linear scan of every album per card per frame.
7. **Playlist detail views render through `<track-list>`**, the way
   `genre-details` does.
8. Then the tail: per-playlist refetch, the `search-store` broadcast
   that re-ranks every mounted list on every keystroke, `rankTracks`'s
   per-track `Set` and closure, the N+1 `GetAlbumTracks` loops, the
   O(total) selection helpers.

### Verification

Measurement, not assertion. Before/after on a synthetic 50 k-track
library for: time between tracks, keystroke-to-paint in the search box,
first paint, and heap after a scripted browse session. `perf.md`
carries the current numbers to beat. Plus an offline run with the
network disabled, asserting icons render.

### Phase 4 — what actually shipped

**Items 1–5 of eight, plus the measurement apparatus the rest of the
phase needs.** (Items 1–2 landed first and are described below; items
3–5 — `C5`, `M6`/`H-14`, `M10` — followed in a second pass and are
recorded under "The second pass" further down.) Stopping here is a
coherent cut: the app works offline, the per-track and per-heart costs
that made a large library unusable are gone, the idle cost of having
once visited Settings is zero, and the bundle no longer parses every
view before it paints anything. Nothing is half-converted.

**First, the ability to measure**, because this phase's verification is
a number and there was no way to produce one:

- **`cmd/gentestdata -bulk N`** (`make bulkdata`, `BULK_TRACKS=50000`)
  generates a ~50 000-track library in **11 s / 466 MB** into a
  gitignored `.dev/`. It shares the fixture generator's command and
  nothing else — the fixture library is curated *cases* selected by
  name, this is a shapeless pile whose only interesting property is its
  size. It avoids ffmpeg per file (40 minutes) by encoding six clips
  once and copying them, but still tags every file through
  `backend/tagwriter`, because a library the app cannot read back
  measures nothing.
- **`make sandbox-seed-bulk`** seeds from it through the same script and
  the same discipline as any other seed — by running the app and
  waiting for the real scan. `seed-sandbox.sh` grew a `--manifest`
  flag and a scan deadline that scales with the track count.
- **`e2e/perf/measure.mjs`** (`make perf LABEL=x`,
  `make perf-compare`) takes the four numbers against a running app and
  writes them to `.dev/perf/<label>.json`. It wraps every bound Go
  method, so "what did finishing a track actually cost" is a fact
  rather than an inference, and records `longtask` entries, which is
  where a 25 MB JSON parse on the main thread shows up and nowhere
  else.

Then the two fixes, each with its reproduction written first:

- **The icons are bundled** (`H-4`, `perf.M9`). `src/icons/` overrides
  Web Awesome's `default` icon library, so all 165 existing `<wa-icon>`
  call sites are fixed without one of them changing.
  `e2e/specs/offline-icons.spec.ts` blocks every non-local request and
  asserts on the `<svg>` *inside* each icon's shadow root — asserting
  the element exists would have passed before the fix too. Verified
  red first: 24 empty icons.
- **`TrackPlayCountChanged`** (`perf.C1`, `perf.C2`). `recordPlay` no
  longer emits `TrackMetadataChanged`; it emits a payload carrying
  everything needed to patch one track in place, read back with
  `UPDATE … RETURNING` so the count cannot drift from the stored one.
  `library-store` patches, replacing the array (consumers key their
  memoized caches on its identity) while sharing every other Track.
  `track-list` gained `selection.retain()` instead of `clear()`.
  **Measured: 8 binding calls / 71.18 MB / 765 ms longest task across
  two track changes → 0 / 0 MB / 0 ms.**
- **`perf.p5`** fell out of the same file: `selectAll()`'s guard
  compared cardinalities, which `retain()` makes reachable — selecting
  four rows and then Select All over a *different* four was a no-op.

#### Measured, 50 000 tracks, 1440×900 Chromium

| Measurement | before | after |
|---|---|---|
| First contentful paint | 104 ms | 48 ms |
| First track row | 1466 ms | 1602 ms |
| JS transferred | 1469 kB | 1476 kB |
| **Cross-origin requests** | **22** | **0** |
| Keystroke → paint (median) | 49.9 ms | 49.9 ms |
| **Track change: binding calls** | **8** | **0** |
| **Track change: bytes over IPC** | **71.18 MB** | **0 MB** |
| **Track change: longest task** | **765 ms** | **0 ms** |
| Heap after browse | 37.25 MB | 38.57 MB |

(FCP and first-row vary ±100 ms run to run and moved for neither
reason; they are here because they are the phase's stated numbers, not
because anything changed them.)

#### Where the plan was wrong

Five things, three of them material:

- **The finding IDs in this phase's prose do not match `perf.md`.**
  Icons are `M9`, not `C1`; the whole-library refetch is `C1`, not
  `C2`; the selection wipe is `C2`. And **`perf.C3` and `perf.C4` were
  already fixed in Phase 3** — they are the library-filter race and the
  never-settling waiter, listed under both phases.
- **The icons could not be sourced the way the plan assumed.** "The
  SVGs already in `src/assets/images/icons/`" are 31 unrelated
  hand-drawn music glyphs under different names; the app uses 64
  Font Awesome names. Worse, the kit CDN the app was hitting serves
  **Font Awesome Pro** — every file carries a Commercial License
  comment — which cannot be redistributed here. They were vendored
  from **Font Awesome Free 7.3.1 (CC BY 4.0)** instead, with
  `LICENSE.txt` alongside; all 64 names happen to exist there, and the
  `ui-visual` baselines did not move.
- **A static list of icon names is not obtainable.** Twenty call sites
  compute their name from state (`jobIcon(job)`, `TONE_ICONS[tone]`,
  `this.favCtrl.iconName`). The list is therefore committed
  (`src/icons/names.txt`) and *checked at runtime*: the resolver
  records a miss to `window.__yjIconMisses` and renders a fallback, and
  an e2e spec sweeps every view for them.
- **The cost was worse than the audit estimated** — 71.18 MB across two
  track changes against a predicted "~25 MB per song", because
  `GetAllTracks` alone is 35.6 MB at this size.
- **`perf.M1`/`M2` did not reproduce.** A keystroke costs 49.9 ms net
  of the 150 ms debounce, with **0 ms** of long-task blocking, not the
  predicted 50–100 ms across the mounted set — because Phase 1 already
  stopped off-screen views rendering, which was M1's actual mechanism.
  Whatever remains is three frames of visible work, not a stall.
  Similarly `M7`/`M8`'s unbounded caches did not show up as heap growth
  in a ten-view browse (37 → 38 MB); reproducing them needs a long
  Explore session, and that reproduction has to exist before the LRU
  does.

### The second pass — items 3, 4 and 5

Three more items, each landed independently with its own before/after,
and each needing a measurement that did not exist yet. `make perf` grew
three numbers in the process: what a favourite toggle costs, what
sitting on Settings costs, and what the bundle's shape costs.

- **`perf.C5` — toggling one heart no longer refetches every playlist.**
  `PlaylistTracksChanged` carries the playlist id and `playlist-store`
  ignored it, answering with `GetAllPlaylistsWithTracks` — every row of
  every playlist, with full track metadata. It now patches the one
  playlist the event names (`GetPlaylistTracks` for the tracks,
  `GetAllPlaylists` for the summaries, because `UpdatedAt` is a sort
  key in `playlist-view`), falling back to a full invalidate for the
  cases where a patch cannot be shown to be equivalent: an event with
  no id (the bulk restore and reorder paths emit one), a cold cache, an
  unknown id, or a fetch already in flight.
  **Measured at ten 500-track playlists: 2 668 kB / 172 ms → 2.0 kB.**
- **…and does nothing at all when nobody is looking.** `invalidate()`
  refetched unconditionally, and the store's constructor did too — so a
  singleton constructed at import time put every track of every
  playlist on the path to first paint, for a view the user might never
  open. Both are now conditional on there being a subscriber;
  `playlist-view` is the only one, is created lazily, and awaits
  `getPlaylists()` when it loads. **2 668 kB → 0 kB for a user who has
  not opened Playlists.**
- **`perf.M6` / `H-14` — the 3 s ticker is gone.** `emitStatus` now
  suppresses a payload identical to the last one it sent, which is the
  fix stated once rather than at each of the twenty call sites, and
  `SetContext` no longer starts a ticker at all. **Measured sitting on
  Settings: 5 status events and 5 full `config-page` re-renders per
  15 s → 0 and 0.** (The `console.log` the audit names was already gone
  — Phase 3's `p1` swept it.)
- **`perf.M10` — route-level code splitting.** `index.ts` now holds a
  loader table per view and awaits the right chunk before creating the
  element, with a sequence guard so a slow chunk cannot land on top of
  a faster navigation. `notification-host`, `inline-notice` and
  `confirm-dialog` stay eager, as do `first-run-wizard` and the
  startup chrome. **JS parsed before first paint: 1 480 kB → 814 kB
  (−45%), in 26 chunks instead of one.**

#### Measured, second pass, 50 000 tracks + 10×500-track playlists

| Measurement | before | after |
|---|---|---|
| **JS evaluated before first paint** | **1 480 kB** | **814 kB** |
| JS after visiting every view | 1 480 kB | 1 484 kB |
| Slowest *first* open of a view | 15 ms | 21 ms |
| **Favourite, Playlists never opened** | **2.61 MB** | **0 MB** |
| **Favourite, Playlists open** | **2.61 MB** | **2.0 kB** |
| **Settings idle: status events / 15 s** | **5** | **0** |
| **Settings idle: re-renders / 15 s** | **5** | **0** |
| Heap after browse | 38.3 MB | 38.4 MB |

First contentful paint did not move (32–36 ms either way) and neither
did first-row. That is worth stating plainly rather than quietly
omitting: at localhost speeds over a warm page cache, 666 kB of JS is
not what the first paint is waiting for. The number that moved is the
work done before the app can show anything, which is what costs on a
cold start, on a slower machine, and under WebKit2GTK rather than
Chromium — none of which this harness measures.

#### Where the plan was wrong — the second pass

Three more, two of them things the audit could not have seen:

- **The 3 s ticker was load-bearing, in two places, invisibly.** Two
  status mutations had no `emitStatus()` behind them — `si.ready = true`
  when an existing index is adopted, and `si.cancel = nil` when a build
  ends — and the ticker was what carried both to the frontend within
  three seconds. Deleting it therefore broke two things the audit
  describes as unrelated: the settings panel would have read "not
  ready" over a fully built index, and the header badge said **"Building
  search index" over an index the settings page called ready**, because
  `syncIndexJob` only resolves the job on a sync reporting `Building`
  false. The second one was caught by looking at a screenshot, not by
  any test. A polling loop is a hidden dependency for every state
  transition that forgot to announce itself, and removing it is
  therefore never only a deletion.
- **`perf.C5`'s fix needed no new binding, and the audit's suggested one
  would have been wrong.** "Refetch that one playlist" has no
  `GetPlaylistWithTracks(id)` behind it, but `GetPlaylistTracks(id)`
  and `GetAllPlaylists()` already exist and compose into exactly that
  — and the summaries half turns out to be necessary rather than
  incidental, since `playlist-view` sorts on `UpdatedAt`, which moves
  with the edit.
- **The store's *constructor* was a bigger C5 than the event was.** The
  audit names the event handler. But `playlistStore` is a singleton
  constructed at import time and warmed itself unconditionally, so
  every launch paid the full `GetAllPlaylistsWithTracks` whether or not
  Playlists was ever opened. The event fires on a user action; the
  constructor fires on every start.

And two notes on measuring, since this phase is measurement:

- **A "0 ms" result is a bug in the measurement more often than a win.**
  The first view-open measurement waited for
  `#main-content > :not(.view-hidden)`, which matches the *outgoing*
  view — still on screen until the incoming chunk resolves — and
  therefore reported 0 ms for every view on every build. Same shape as
  the 150 ms debounce trap: a number that cannot move is not evidence.
- **A before/after has to differ in one thing.** The first `before-m10`
  was taken by stashing `frontend/index.ts`, which also reverted
  Phase 4's `registerBundledIcons()` from the same file — 22
  cross-origin requests, and a baseline for a build that never existed.
  The real baseline was made by *adding the static imports back* to the
  current file, which leaves everything else in place.

#### Not done, and still worth doing (after the second pass)

Items 6–8, in the order they should be taken: `M7`/`M8` (bound the
caches — **after** a reproduction; still not one, see above),
`M3`/`M4` (thumbnail tier, artist-avatar map), `M5` (playlist views
through `<track-list>`), then the tail: `m1`–`m7`, `p3`, `p4`.

*(Items 6 and 7 shipped in the third pass, below. `M5` and the tail
remain.)*

One finding found while splitting the bundle and deliberately left:
**`track-details` (42 kB) cannot be split out from `index.ts`**, because
`track-list`, `cover-grid`, `queue-panel`, `playlist-details` and
`smart-playlist-details` all import it statically. Prising it out means
making those five import it dynamically at the point of use — a change
to five components rather than to the router, and worth doing with
`M5`, which rewrites two of them anyway.

Two unrelated things found and left alone:

- `frontend/wailsjs` was stale against Phase 3's Go changes.
  Regenerating it here was a no-op afterwards, so the delta in the tree
  is Phase 3's, not this phase's.
- **`backend/download`'s per-provider cap tests are flaky**, and were
  before this phase. `TestPerProviderCapSerializesTransfers` fails
  roughly one run in eight with `TempDir RemoveAll cleanup: … directory
  not empty` — a download goroutine still writing into `t.TempDir()`
  after the test returns, i.e. the test does not wait for the work it
  started. It is a real bug (the same missing wait would leak a
  goroutine in production), but it belongs with the download/jobs work
  Phase 3 already deferred, not here.

### The third pass — items 6 and 7

Two more items, each with its own before/after, and each needing a
measurement that did not exist. `make perf` grew two numbers: what a
long **Explore session** retains, and what **scrolling** a long list
costs. The headline is that `M7` finally reproduced — and the reason it
had not is more useful than the fix.

- **`perf.M7` / `M8` — the Explore caches are bounded, and the finding
  was real all along.** It failed to reproduce twice because
  `measureHeapAfterBrowse` *visits* Explore and never types in it, and
  both caches are filled only by a search. A session of twenty-four
  searches grows the heap **20.58 MB and is still accelerating** at the
  end (0.757 MB/search over the first eight, 0.900 MB/search over the
  last eight). `LRUMap` (`utils/lru-map.ts`) caps the thumbnail cache
  at 96 entries and the artist-image cache at 32, chosen from the
  measured cost of an entry — a cover thumbnail is ~27 kB of base64, an
  artist photo ~128 kB — and both are several times a screenful, so
  nothing on screen is ever evicted. **Measured: 20.58 MB → 10.65 MB
  of growth, and the curve plateaus from search 17 (0.900 →
  0.109 MB/search over the last eight).** The shape is the result, not
  the endpoint: bounded means it stops.
- **`perf.M3` — the Art column asks for the right tier.** It rendered
  `CoverArtPath`, the original artwork, into a 24 px box with no
  `loading="lazy"`, while `CoverArtSmall` sat unused on the same model.
  Now `CoverArtSmall || CoverArtMedium || CoverArtPath`, plus
  `loading="lazy" decoding="async"` and explicit `width`/`height` —
  which is what `cover-grid.getCoverUrl()` has done all along.
  **Measured: 26 of 26 image requests were the full-size original tier;
  now 0.**
- **`perf.M4` — the artist grid looks up instead of scanning.** The
  album-art fallback linear-scanned every cached album, lowercasing two
  strings per comparison, inside the virtualizer's `renderItem`. It is
  the common case, not an edge one: a locally-tagged library has no
  artist images at all. Now a `Map` built once per identity of the
  album cache. **Measured directly at 5 000 albums × 24 visible cards:
  1.46 ms/frame → 0.01 ms/frame, built once in 0.5 ms.**

#### Measured, third pass, 50 000 tracks

| Measurement | before | after |
|---|---|---|
| **Explore session (24 searches): heap growth** | **20.58 MB** | **10.65 MB** |
| Explore session: growth over the *last* 8 searches | 0.900 MB/search | **0.109 MB/search** |
| Explore session: thumbnails retained | 357 (uncapped) | 96 (capped) |
| Explore session: artist images retained | 58 (uncapped) | 32 (capped) |
| Explore session: retained chars | 20.52 M | 7.85 M |
| **Track list + Art: full-size originals requested** | **26 / 26** | **0 / 26** |
| Track list + Art: image bytes per screen | 5.7 kB | 2.1 kB |
| Artist avatar fallback (measured directly) | 1.46 ms/frame | 0.01 ms/frame |
| Scroll: worst frame, either view | 14.8–17.4 ms | 16.1–16.7 ms |

#### Where the plan was wrong — the third pass

Five more, three of them about the *rig* rather than the code:

- **`M7` did not fail to reproduce; the browse script could not see
  it.** Two sessions concluded "no heap growth" from a script that
  navigates to Explore and moves on. The caches are filled by a search
  and by nothing else, so the script was measuring a view with two
  empty maps. A finding about a *cache* needs a session that fills it,
  and "we looked and saw nothing" is only evidence if the thing that
  fills it ran. Two sessions nearly deleted a real finding on the
  strength of a measurement that never touched it.
- **`M8`'s two expensive maps are dead code.** The audit names
  `artistAlbums` and `artistTopTracks` as holding full discographies
  and top-track lists. Nothing in the app has ever written to either
  — their only callers were one component test. They are deleted
  rather than bounded; carrying an LRU for an unreachable map is
  ceremony. The real M8 retainer is `exploreCache.artists`, which the
  audit does not mention, at ~128 kB per entry.
- **Two caches holding one string means bounding either alone frees
  nothing.** `artistImageCache` and `exploreCache.artists` both hold
  the artist photo's data URL — the *same string*, measured at 2.30 M
  chars in each. An LRU on one would have shown a zero improvement and
  looked like a fix that did not work. The cap is now a shared exported
  constant so the two cannot drift apart.
- **The bulk library cannot exercise `M3`, by construction.**
  `cmd/gentestdata` generates **300×300, ~3.7 kB** covers on purpose
  (the "466 MB instead of 2 GB" decision from the first pass), so its
  "original" is already thumbnail-sized and the audit's "1500×1500,
  several hundred kB" does not exist here. The fix is still right and
  still lands; the number that shows it had to be *which tier was
  requested* rather than bytes saved. A measurement library optimised
  for size removed the property one finding was about.
- **`M4` is real but an order of magnitude smaller than the audit
  estimated.** Predicted "250 000 comparisons and 500 000 string
  allocations per scroll frame" from 5 000 albums × ~50 visible cards.
  Measured: 24 visible cards, and the scan exits on its first match, so
  the true cost is **1.46 ms per frame** — 9% of a frame budget, below
  the 50 ms long-task threshold and invisible in the scroll trace. The
  fix is 146× on the operation and moves no user-visible number today;
  it is worth having because it stops scaling with the library.

And one more note on measuring, since this pass produced two more
broken numbers before two good ones:

- **A bound cannot be verified by a run that never reaches it.** The
  first `after-m7` was identical to its before, because twelve searches
  cached 180 thumbnails against a cap of 192 — nothing was ever
  evicted. That is the third variant of the same trap in this phase
  (the 150 ms debounce, the `:not(.view-hidden)` selector, and now a
  cap the session never reaches): **the measurement has to be able to
  move before it can be evidence that something did.** The session went
  to twenty-four searches, which overruns both caps.
- **`Infinity` is a better baseline than `git stash`.** The before was
  built by setting the two cap constants to `Infinity` — a genuinely
  unbounded build differing from the after in exactly one thing, on a
  tree where stashing a file reverts four phases of unrelated work.

#### Not done, and still worth doing (after the third pass)

`M5` (playlist and smart-playlist detail views through
`<track-list .externalTracks=…>`, the way `genre-details` already
does), and with it prising `track-details` (42 kB) out of the startup
chunk — it is pulled in statically by `track-list`, `cover-grid`,
`queue-panel`, `playlist-details` and `smart-playlist-details`, and
`M5` rewrites two of those five anyway. Then the tail: `m1`–`m7`, `p3`,
`p4`.

One small thing found and left: **the perf harness navigates by
dispatching a raw `navigate` event, which `app-sidebar` does not
hear**, so a screenshot taken during a measurement run shows the
sidebar highlighting the wrong item. Verified not to be a real
regression — a genuine click sets `aria-current="page"` correctly — but
it is exactly the kind of self-inconsistency the second pass caught by
reading a PNG, and anyone reading a perf-run screenshot should know it
is an artifact.

---

### The fourth pass — item 8 (`M5`), part of the tail, and a bug that was not a finding

`M5` shipped, three tail items were settled, and the pass turned up a
**broken feature that no audit named**: smart playlists could not be
created at all.

`make perf` grew a tenth number, because none of the nine opened a
playlist: **what a 2 000-track playlist costs to open** — elements
retained, eager cover requests, heap, and what one update pass costs
and rebinds. The playlist is staged idempotently by its own name
(`__perfbig_`), separately from the ten 500-track ones the favourite
measurement builds, so every number taken before this one existed still
compares.

- **`perf.M5` — both playlist detail views virtualize.** They rendered
  every track with a plain `.map()`. Measured at 2 000 tracks:
  **22 090 elements in the shadow root and 2 000 eager `<img>`, against
  487 and 0 after**, with retained heap **5.85 MB → 0.81 MB** and one
  update pass **5.3 ms → 0.1 ms**. Rows also ask for the small cover
  tier with `loading="lazy" decoding="async"`, and `getVisibleTracks()`
  is memoised on the identity of the tracks array and the search term
  instead of rebuilding 2 000 wrapper objects per render.
- **`perf.p3` — half of it.** `playlist-store` now coalesces its notify
  to a microtask like the other five stores. `search-store`
  deliberately does not; see below.
- **`perf.m7` — a closed queue panel renders no list.** `width: 0` and
  `contain: layout style paint` bounded the damage without stopping the
  work: the virtualizer inside still measured its window on every queue
  change and `scrollToIndex` still called `scrollIntoView()` on an
  invisible element. `updated()` returns early while closed and resets
  its two cached indices, so opening re-syncs rather than inheriting
  stale ones.
- **`perf.p4` — already fixed.** `track-list` branches on a real
  `loadingTracks` flag, not `tracks.length === 0`. Phase 3 did it.
- **`perf.m1` — does not reproduce, and the fix as written is a
  regression.** See below.
- **Not a finding at all: `CreateSmartPlaylist` was broken.** It issued
  its `INSERT ... RETURNING` through `QueryContext`, which routes to the
  query-only read pool, and failed with "attempt to write a readonly
  database (8)". No smart playlist could be created in any real build.
  Now `QueryRowWriter`, with `TestNoWritesOnTheReadPool` walking the
  tree for the whole class.

#### Measured, fourth pass, 50 000 tracks + a 2 000-track playlist

| Measurement | before | after |
|---|---|---|
| **Playlist open: DOM nodes** | **22 090** | **487** |
| Playlist open: rows in DOM | 2 000 | 36 |
| **Playlist open: eager cover images** | **2 000** | **0** |
| **Playlist open: heap retained** | **5.85 MB** | **0.81 MB** |
| **Playlist open: one update pass** | **5.3 ms** | **0.1 ms** |
| Playlist open: listeners rebound per pass | 0 | 0 |
| Playlist open: first row | 3 266 ms | 3 155 ms |
| Playlist open: worst scroll frame | 18.5 ms | 19.0 ms |

Two rows there are the honest half of the trade. **First row barely
moved**, because the 3.2 s is the backend fetch of 2 000 playlist rows
and not the DOM build — the fix makes the list cheap to *hold*, not
quicker to arrive. And the **worst scroll frame got slightly worse**,
which is what virtualizing costs: a fully-materialised list has no work
left to do while scrolling. Both are within a frame; the numbers that
moved are the ones that scale with playlist length.

#### Where the plan was wrong — the fourth pass

Six more, and one of them is the audit recommending a bug:

- **`M5`'s four mechanisms are not equally real, and one of them is
  false.** The audit says lit "removes and re-adds 10 000 listeners per
  pass" because the row handlers are fresh arrow functions. Measured:
  **zero** add/removeEventListener calls per pass, on any build.
  lit-html's `EventPart` is itself the listener (it implements
  `handleEvent`), so a changed listener value updates a stored field and
  never touches the DOM. The real cost was elements retained and eager
  images; the listener claim was never true.
- **`M5`'s suggested fix would have cost two features.** "Render these
  with `<track-list .externalTracks=…>` the way `genre-details` does"
  works for `genre-details` because a genre list is *just tracks*.
  `playlist-details` renders **phantom rows** (a missing file, with
  locate and remove actions and its own context menu) and is a
  drag source and a drop target; `smart-playlist-details` has the
  phantom rows too. `track-list` has never had either — zero matches
  for `Phantom`, `dragover` or `drop` in its 2 260 lines. Virtualizing
  in place gets the same 45× on the number that matters with none of
  that risk, and leaves `track-list` untouched for its four other
  callers.
- **`perf.m1` is a correctness regression, not an optimisation.** It is
  right that `LitVirtualizer` declares `renderItem`/`keyFunction` as
  plain properties and that a fresh arrow function marks them dirty
  every host update. What it misses is that this is the *only* thing
  making the virtualizer re-render at all: the `virtualize` directive
  runs when one of the virtualizer's own properties changes, and a
  parent re-render is not that. Hoist them and the cards keep the
  classes they had — measured in the running app at **1 highlighted
  card before, 0 after**, for both `artists-view` and `genres-view`.
  Doing it properly means pushing an explicit `requestUpdate()` on
  every piece of host state a card reads, which is nearly every reason
  those views re-render. Reverted, and pinned by
  `frontend/test/components/card-grid-repaint.test.ts`, which fails on
  the change.
- **`perf.p3` is right about one store and wrong about the other.**
  `playlist-store` coalesces now. `search-store` does not: deferring
  makes a subscriber that unsubscribes synchronously after a `setTerm`
  miss the notification entirely, which is a semantic change rather
  than an optimisation — `view-stores.test.ts` already pinned both
  halves deliberately, and this store is the one on the keystroke path,
  where the batching the audit says hides the cost is Lit's own, one
  layer down.
- **`perf.p4` was fixed two phases ago.** Listed as outstanding; it is
  not.
- **The third pass left the tree failing `tsc --noEmit`.**
  `list-render-cost.test.ts` indexed `COLUMN_DEFS[columnId]` without a
  guard. `make lint`, `make test`, `make ui-test` and `make e2e` were
  all green over it, because none of them typechecks the test tree —
  CI's `npx tsc --noEmit` does, and it is not in the documented gate.
  Fixed here; the gate in the skill now says to run it.

#### Not done, and still worth doing (after the fourth pass)

Prising **`track-details` (42 kB) out of the startup chunk** — still
pulled in statically by `track-list`, `cover-grid`, `queue-panel` and
both playlist views. `M5` rewrote two of the five but did not change
how they import it, so this is unchanged work: five components loading
it dynamically at the point of use.

Then the rest of the tail: `m4` (permanent document `mousemove` for
drag), `m5` (unconditional DOM work in `updated()`), `m6` (O(total)
selection helpers — and the audit predicts "Select all → Edit tags"
hangs the renderer at 50 000 tracks, which is worth reproducing before
believing, given this pass's record), and `m2` (N+1 `GetAlbumTracks`
loops, the only tail item with a backend half). `m1`, `p3`'s
search-store half and `p4` are settled above and should not be
reopened.

*(The `track-details` split and `m6` shipped in the fifth pass, below;
`m5`, `m4` and `m2` in the sixth, which finishes the phase.)*

Two things found and deliberately left:

- **`backend/download`'s per-provider cap tests are still flaky**
  (~1 run in 8, `TempDir RemoveAll cleanup: directory not empty`).
  Unchanged by this pass; still belongs with the deferred download/jobs
  work.
- The e2e queue spec now opens the panel before asserting on its rows,
  and **waits for it to close again** before finishing. The panel's
  width is animated and the transport slides with it, so a click issued
  during the close lands on whichever button moved under the pointer —
  for the very next spec that was Repeat rather than Shuffle, and both
  emit `QueueModeChanged`, so it failed on the assertion rather than on
  the wait, one run in two.

---

### The fifth pass — the `track-details` chunk split, and `m6`

Two items, each independently landable and each landed with its own
before/after. `make perf` grew an eleventh measurement, because none of
the ten **selected anything** — so the two helpers `m6` names had never
been on any measured path.

- **`track-details` (42 kB) is out of the startup chunk.** It was
  imported for side effect by all five components that open it, so it
  was evaluated before first paint however `index.ts` split the routes.
  It loads through `utils/lazy-track-details.ts` now — one memoised
  dynamic import, awaited by all ten openers before they touch the
  `<track-details>` their template already rendered, since an
  un-upgraded element is a real `HTMLElement` on which `?.show()`
  *throws*. It is warmed with the views, so the first open still does
  not wait. **JS evaluated before first paint: 814.5 kB → 772.9 kB, in
  27 chunks instead of 26, with the slowest first view open at 19 ms
  against 21 ms** — both halves of the trade, and the second one did not
  get worse.
- **`perf.m6` — the six-second stall is real, and the audit's mechanism
  for it is not.** "Select all → Edit tags" at 50 000 tracks blocks the
  main thread for **3.0–6.3 s** (it varies run to run on one build; the
  audit says it "will hang the renderer", which is fair). It is not
  O(selection × total): select-all hands `openBatchTrackDetails` its
  keys *in list order*, so each `find` matches at index *i* and the true
  cost is N²/2 ≈ 1.25×10⁹, not 2.5×10⁹ — quadratic in the **selection**,
  and a selection built bottom-up would be worse than the one measured.
  `utils/track-index.ts` is the fix: a `WeakMap` from the array's
  identity to a `Map<FilePath, Track>`, shared by all five hosts (which
  is one lookup, since four of them resolve against the same
  `libraryStore.getCachedTracks()` array). **3 051–6 298 ms → 68 ms.**
- **…and its other half is 3 ms, so it stayed a walk.**
  `getSelectedKeysOrdered()` really does iterate all 50 000 items from
  every `dragstart` and every context-menu action. Measured, that is
  **3 ms** — a fifth of a frame. It gained an early exit once the whole
  selection is found (**first row 2.5 → 0.6 ms, last row 2.7 → 2.3 ms**,
  and the second number is in the table because reporting only the
  first would be self-flattering) and nothing else. See below for why
  the audit's actual suggestion was rejected.

#### Measured, fifth pass, 50 000 tracks

| Measurement | before | after |
|---|---|---|
| **JS evaluated before first paint** | **814.5 kB** | **772.9 kB** |
| JS after visiting every view | 1 489.2 kB | 1 490.6 kB |
| Chunks after visiting every view | 26 | 27 |
| Slowest *first* open of a view | 21 ms | 19 ms |
| **Select all → edit tags** | **3 051–6 298 ms** | **68 ms** |
| **Select all → edit tags: blocking** | **3 062–4 438 ms** | **70 ms** |
| Ordered keys, first row selected | 2.5 ms | 0.6 ms |
| Ordered keys, last row selected | 2.7 ms | 2.3 ms |
| Ordered keys, all 50 000 selected | 2.8 ms | 3.0 ms |
| Heap after browse | 38.57 MB | 38.44 MB |

Every other row of the eleven measurements is unchanged within noise.

#### Where the plan was wrong — the fifth pass

Six more, and one of them is a whole interaction that does not exist:

- **`m6`'s magnitude is right and its mechanism is wrong.** See above:
  the cost is quadratic in the selection, not selection × total. It
  matters because it says which *other* selections are slow — the audit's
  formula predicts a 10-track selection costs 500 000 comparisons
  (it costs ~50), and predicts nothing about a selection made from the
  bottom of the list (which is the worst case, not select-all).
- **`m6`'s recommended fix is half right and half unsafe.** "Keep an
  index-ordered selection, and build a `Map<FilePath, Track>` for the
  batch lookup." The map is exactly right and is the whole 50×. The
  index-ordered selection is not: an index goes stale on any re-sort,
  re-filter or refetch while a file path survives all three — which is
  why `retain()` already drops `lastSelectedIndex` and keeps the keys.
  Trading 3 ms for a silently mis-ordered queue insert is not a trade.
  This is the second audit recommendation in two passes that would have
  shipped a bug (`m1` was the first).
- **One of the five hosts cannot reach the dialog at all.**
  `cover-grid`'s expanded album dropdown — the only place its
  `openTrackDetails` is called from — is rendered by `renderSplitGrid`,
  which `connectedCallback` references solely to satisfy
  `noUnusedLocals` (`void this.renderSplitGrid`) and which is, in its
  own comment, "never invoked at runtime". Expanding an album sets
  `expandedAlbumId` and fetches ten tracks into `expandedTracks`, and
  nothing appears. The audit records this as `perf.p2`, "dead code
  carried in the bundle", filed under housekeeping — it is a missing
  feature, and belongs with Phase 5's album work rather than in a list
  of unreferenced symbols.
- **A `longtask` entry is delivered *after* the task that produced it.**
  The first run of the new measurement reported `blocking: 0 ms` beside
  a six-second wall time, because it read `__yjPerf.longtasks`
  synchronously after the await. That is the *sixth* variant of this
  phase's most-repeated trap — and the first one caught by another
  number in the same row disagreeing with it rather than by suspicion.
  The timer waits 250 ms before reading the buffer now.
- **The first load after a rebuild is not a measurement of first load.**
  FCP read 96–112 ms on every run taken immediately after
  `make dev-headless`, against 28–32 ms on the next run of the same
  build — a cold Vite module graph, not variance. Any FCP number needs a
  second run; the plan's earlier "±100 ms run to run" was describing
  this without naming it.
- **`.dev/perf/` labels are a flat namespace and the audit's IDs are
  case-sensitive.** `before-m6`/`after-m6` already existed — from the
  *second* pass's capital-`M6`, the 3 s ticker, which is an unrelated
  finding. Using an audit ID raw as a label would have overwritten two
  of this phase's baselines. This pass used `before-sel`/`after-sel`.

#### Not done, and still worth doing (after the fifth pass)

The rest of the tail, in the order they should be taken: `m5`
(unconditional DOM work in `updated()` in three components, including a
forced synchronous layout in `now-playing` on every player-store
change), `m4` (permanent document `mousemove`/`mouseup` for two drag
interactions — the audit already concedes the cost is small, so measure
before claiming), then `m2` (N+1 `GetAlbumTracks` loops, the only tail
item with a backend half, which is why it is last: a new binding means
`make bindings` and the 13-line `wailsjs` delta becomes harder to reason
about). `m1`, `p3`'s search-store half and `p4` remain settled.

Two things found and deliberately left:

- **`cover-grid`'s dead album dropdown**, above. Phase 5.
- **`e2e/perf/` is still uncommitted**, now eleven measurements and a
  staging harness. It is marked intent-to-add (`git add -N`) so it
  appears in diffs and survives a `git clean`, but the working tree is
  still carrying Phases 1–5 uncommitted by request.

And two verified unchanged: the `wailsjs` delta is still 13 lines
across 5 files, and `backend/download`'s per-provider cap tests passed
this pass (they fail ~1 run in 8; absence of a failure is not a fix).

---

### The sixth pass — `m5`, `m4`, `m2`, and the end of the tail

The last three tail items, in the order the fifth pass proposed, each
landed with its own before/after. `make perf` grew its **twelfth,
thirteenth and fourteenth** measurements, because none of the eleven
saw layout cost, document listeners, or what "play these" costs.

Half of `m5` was dropped on the strength of its own measurement, which
is the point of taking one.

- **`perf.m5` — `now-playing` stopped re-measuring itself once a
  second.** `updated()` did six `querySelector`s and interleaved
  `scrollWidth`/`clientWidth` reads with `style.setProperty` writes on
  every pass, and `player-store` notifies while playing. It now runs
  only when the geometry can have changed: the rendered title, the
  rendered artist, the two scroll flags, or the ResizeObserver saying
  the panel resized. **Over six seconds of playback: 52 forced layouts
  → 2, and 3.2 ms → 0.9 ms inside `updated()`.** Per steady-state pass:
  8 querySelectors, 8 layout reads, 4 style writes and 2 read-after-
  write interleaves → **0 of each**.
- **…and the audit's suggested guard would have broken the marquee.**
  "Guard on the value/flag they already track" reads as "guard on the
  text", and the geometry does not depend only on the text:
  `.will-scroll .scroll-content` carries `padding-right: 2em`, so
  applying the scroll class changes the distance the animation travels.
  Measured in the app: **−128 px before the class, −158 px after it**. A
  text-only guard leaves every first hover scrolling 30 px short, which
  no test would have caught.
- **`perf.m5`'s other two components do not reproduce, and are not
  fixed.** `artists-view` and `genres-view` do 1 `querySelector` and 2
  `style.setProperty` per pass, with **no layout reads at all** — so
  there is no forced synchronous layout there, and `updated()` measures
  **0.0033 ms**, against a whole update pass of 0.3 ms (artists) and
  0.1 ms (genres). One percent of a pass, in the two files `perf.m1`
  was rejected in. Left alone.
- **`perf.m4` — the drag listeners attach on `mousedown`.** Both sites
  now add document `mousemove`/`mouseup` when a drag starts and remove
  them when it ends (plus on disconnect, for a drag interrupted by the
  component going away). **Document `mousemove` listeners: 4 → 2;
  `mouseup`: 5 → 3.** The cost, as the audit concedes, is noise:
  2 000 dispatched moves went 4.1 ms → 3.6 ms, **2.05 µs → 1.80 µs per
  move**. It is in the table rather than omitted because that is the
  honest result.
- **`perf.m2` — one call, and a fraction of the bytes.**
  `GetFilePathsByAlbums(ids, libraryID)` and
  `GetFilePathsByGenres(names, libraryID)` return paths **grouped by
  the entity**, because the caller owns the order (an album list is
  sorted by name, not by id) and the drag cache stores per album.
  Verified path-for-path identical to the old per-album resolution.

#### Measured, sixth pass, 50 000 tracks

| Measurement | before | after |
|---|---|---|
| **Player bar, 6 s of playback: forced layouts** | **52** | **2** |
| **Player bar, 6 s of playback: `updated()` total** | **3.2 ms** | **0.9 ms** |
| Player bar: querySelectors / layout reads per pass | 8 / 8 | 0 / 0 |
| Player bar: style writes / forced layouts per pass | 4 / 2 | 0 / 0 |
| Player bar: `updated()` per pass | 0.003 ms | 0 ms |
| Player bar (DOM changed): forced layouts per pass | 2 | 1 |
| Player bar (DOM changed): `updated()` per pass | 0.103 ms | 0.060 ms |
| **Document mousemove listeners** | **4** | **2** |
| Document mouseup listeners | 5 | 3 |
| Per pointer move | 2.05 us | 1.80 us |
| **Play artist (12 albums): binding calls** | **13** | **2** |
| Play artist: bytes over IPC | 74.2 kB | 19.2 kB |
| Play artist: wall time | 29.3 ms | 26.0–27.6 ms |
| **Play 20 albums: binding calls** | **20** | **1** |
| Play 20 albums: bytes over IPC | 117.5 kB | 26.0 kB |
| Play 20 albums: wall time | 7.8 ms | 1.7–2.0 ms |
| **Play 5 genres: bytes over IPC** | **6 014.4 kB** | **1 291.1 kB** |
| Play 5 genres: binding calls | 5 | 1 |
| **Play 5 genres: wall time** | **213.3 ms** | **32.6 ms** |
| File paths returned (artist / albums / genres) | 120 / 200 / 10 420 | 120 / 200 / 10 420 |

The last row is the one that says the change is safe rather than fast,
and the artist wall time is the honest one: it barely moved, because
that path is dominated by `GetAlbumsByArtist`, which still has to
happen.

#### Where the plan was wrong — the sixth pass

Eight more, and one of them breaks the build:

- **`m5`'s magnitude is a hundredth of its mechanism.** The read/write
  interleave is real and is exactly where the audit says it is, but a
  1 Hz position report **changes nothing this component renders**, so
  the reads hit a clean layout and cost 3 µs. The flush only happens
  when the DOM actually changed — measured at 0.103 ms, 34× the steady
  state, and the number the fix had to move. An audit that reasons from
  the shape of a function cannot see whether the layout was dirty when
  it ran.
- **Two of `m5`'s three components had no layout read in them at all.**
  The finding groups `artists-view`/`genres-view` with `now-playing`
  under "unconditional DOM work". Only the third does a read, and
  therefore only the third can force a layout; the other two write two
  custom properties the browser drops as unchanged.
- **The suggested guard was the third audit recommendation in three
  passes that would have shipped a bug** (after `m1` and `m6`), and
  for the same reason all three times: it reasons from the code's shape
  and not from what the rest of the file already knows — here, that a
  CSS class two hundred lines up changes the geometry being measured.
- **`m4`'s line numbers describe a build from before Phase 1.**
  `track-list` registers those listeners through `listenWhileActive`,
  so they have been scoped to the *active view* since Phase 1; only
  `now-playing`'s were there "for the process lifetime". The finding
  was already half fixed by a phase that was not about it.
- **`m2`'s cost is the bytes, and the audit's suggested fix keeps
  them.** "Add a single `GetTracksByAlbumIDs([]int64)` /
  `GetTracksByGenres([]string)` binding" removes the round trips and
  still ships whole track rows — which is **6 MB over the IPC for five
  genres**, because all three sites want `FilePath` and nothing else.
  Returning paths is 4.7× smaller than returning tracks would have
  been, on top of 5 → 1 calls.
- **`m2` has a fourth site the audit does not name**, and it is the one
  that fires most: `album-selection.warmCache()` pre-resolves the drag
  paths with the same sequential loop **on every album selection
  change**, not on a menu action.
- **`make generate` produced TypeScript that does not parse**, and
  nothing had noticed. `genevents` prefixes only the *first* line of a
  const block's doc comment with `//`; Phase 4's first pass gave
  `events.go` two multi-paragraph comments, so regenerating
  `frontend/src/events.ts` emitted bare prose into an object literal.
  `make generate` is a pre-commit hook, so the next person to run one
  would have broken their own tree. Fixed in the generator, not in the
  output.
- **The `wailsjs` delta was 13 lines across *two* files, not five.**
  Both are `autotagservice/Service.*`, and it is still Phase 3's. It is
  now 25 lines across four, the extra 12 being this pass's two library
  bindings.

And two on measuring:

- **"The first run after a rebuild is cold, the second is warm" is not
  reliable.** This pass saw 100 ms then 96 ms on one build, and 28 ms
  then 76 ms on another — the second run warmer in neither case. FCP
  varies by ±50 ms here for reasons this harness does not control, and
  a number that cannot be attributed should be reported as noise rather
  than defended.
- **A measurement taken against the wrong seed looks like a result.**
  A confirming run taken after `make e2e` reported "Play 20 albums:
  —" and 1.7 kB for an artist, because `make e2e` needs `SEED=default`
  and the app was still on it. The numbers were plausible in shape and
  meaningless; the tell was a row that had gone from a number to a dash.

#### Not done, and still worth doing (after the sixth pass)

Nothing from Phase 4. The tail is finished: `m1`, `p3`'s search-store
half and `p4` are settled, `p2` is a missing feature that belongs to
Phase 5, and `m3`/`m8`/`m9` were closed in earlier passes.

Three things found and deliberately left:

- **`backend/download`'s per-provider cap tests failed 2 runs in 4**
  this pass, against the ~1-in-8 recorded before. Same failure
  (`TempDir RemoveAll cleanup: directory not empty`), same cause, still
  belongs with the deferred download/jobs work — but it is worse than
  the plan says, and a single green run means even less than it did.
- **`cover-grid`'s dead album dropdown** (`perf.p2`). Unchanged, Phase 5.
- **`e2e/perf/` is still uncommitted**, now fourteen measurements.

---

## Phase 5 — One app, not eleven pages

### What's wrong

`H-7`–`H-13`, `H-15`, `H-19`–`H-24`, and the ARIA tail of `a11y.md`.

- The track list is **40 px too wide** by arithmetic:
  `computeDefaultWidths` (`track-list.ts:409`) distributes
  `clientWidth` across the columns and never subtracts the 24 px
  favourite column or the 2×8 px padding that `colBoundaryPositions`
  knows about. Measured `scrollWidth 1280` vs `clientWidth 1240` on
  every row. Duration is always clipped.
- Minimum window is 512×384. At 700×480 the sidebar overflows behind
  the player bar with no scroll and **Settings and Jobs become
  unreachable**. Nothing triggers the sidebar's existing `.collapsed`
  mode.
- The app lands on Tracks (`app-sidebar.ts:124`), never on Home.
- On Home, an album with no cover renders as **nothing** — that card's
  placeholder has no background, while the Albums and Artists grids
  both draw a letter tile. With a small library all three shelves show
  the same seven albums in different orders.
- The header search is view-scoped and looks global.
- Four views have a heading, four do not; two have sort controls; none
  shows a count.
- An album detail page has no Play, no Shuffle, no Add to queue, and
  unexplained green ✓ badges.
- Settings puts "Search Index" first and expanded and "Libraries" last
  and below the fold, and has no Playback/Audio section at all.
- Explore is a search box over a 1.1 M-row catalog with nothing to
  browse.
- Three identical `Tideline / Aurora Fields / 00:06` rows cannot be
  told apart, in an app that has a duplicate-detection feature.
- The ARIA tail: no `aria-live` anywhere for scan progress, toasts,
  search results or track changes; no `aria-sort` on column headers;
  four hand-rolled autotag dialogs and the remove-library confirmation
  with no `role=dialog`, no focus trap and no focus restore — while
  five other places already use `wa-dialog`, which does all of that;
  `aria-selected` on `role=button` (invalid, dropped) in the two grids
  whose entire ctrl/shift interaction exists to produce a selection;
  a hardcoded px type scale, so text-only resize does nothing.

### What ships

**The arithmetic fix, and a layout that survives its own minimum.**
Subtract the fixed columns and padding. Raise `MinWidth`/`MinHeight` to
a size the layout actually supports, and collapse the sidebar to icons
below a breakpoint so the number is honest rather than aspirational.

**A shared page-header component** — title, count, sort, actions — and
every primary view adopts it. This is the single highest-leverage
consistency change: it fixes four missing headings, four missing
counts, and two missing sort controls at once, and stops the next view
from inventing a ninth arrangement.

**Land on Home.** Fix the Home card's missing-art placeholder to match
the other two grids, and suppress a shelf whose contents substantially
duplicate the shelf above it (an empty shelf is already suppressed;
this is the same rule one step further).

**Say what the search searches.** *(Decided: it stays view-scoped.)*
The box names its scope in the placeholder ("Search tracks", "Search
playlists", "Search albums") and in the no-results copy, so "No
playlists match your search" arrives having already told the user it
was only ever looking at playlists. The scope label changes on
navigation; the term persisting across navigation is then correct
rather than confusing.

The box also stops appearing and disappearing between views — that is
what shifts the whole header layout today. On a view with nothing to
search (Home) it keeps its slot and is disabled rather than removed;
on Explore, which has its own catalog search box, the header box is
disabled with a label pointing at the one in the page.

**A `?` shortcuts overlay**, since the single-key bindings are staying
and Settings is currently the only place they are written down.

**Album pages get their primary action**, and the ✓ badges get a legend
or go away. Track list gets an album column by default so duplicates
are distinguishable.

**Settings gets reordered** (Libraries first) and gains the Playback
section it does not have.

**The ARIA tail**, taken as one pass now that Phase 1 has made things
focusable: `aria-live` for the four async surfaces, `aria-sort` on
column headers, the hand-rolled dialogs migrated to `wa-dialog`,
`role=listbox`/`option` on the selectable grids, and a `rem` type
scale.

### Verification

`make ui-visual` baselines for the page header across all eight primary
views; an e2e spec asserting no horizontal overflow on any row at
1440×900, 1024×768 and the new minimum; and a manual pass with a
screen reader on the four surfaces that gained live regions.

### Phase 5 — the first pass: the arithmetic, the minimum, and the header

Items 1 and 2 of the proposed four, plus one of the seven inherited
items. Each landed with its reproduction watched failing first, in the
running app rather than in a test.

- **`H-7` — the track list fits its container.** Reproduced exactly as
  the audit says: `scrollWidth 1280` against `clientWidth 1240` on the
  header row and all 31 track rows, and the 40 px is precisely
  `24 + 2×8`. `computeDefaultWidths`, `normalizeWidths` and
  `onColResizeMove` now share one `availableColumnWidth`, and the two
  constants behind it are read by `colBoundaryPositions` and the grid
  template too — they were written out separately in four places,
  which is how they came to disagree. **0 of 31 rows overflow after,
  at every viewport tested.**
- **`H-11` — the minimum is a size the layout supports.** Reproduced:
  at 700×480 the sidebar's eleven items need 406 px of a 352 px pane,
  and Settings rendered at y=420–454 against a pane ending at y=416 —
  outside its own box, clipped, unreachable. The pane scrolls now, the
  sidebar collapses to icons below 900 px (its `.collapsed` mode
  existed and only a manual drag had ever reached it), the subtitle
  hides at the same breakpoint, and `MinWidth`/`MinHeight` are
  **800×600**, chosen by walking a ladder of nine viewports and
  reading what broke where rather than by picking a round number.
- **`H-19` and `H-10` — one page header, nine views.**
  `<page-header>` is title, count, sort and actions; Artists and
  Genres gain the sort they never had, four views gain a heading, and
  five gain a count. The header search box keeps its slot everywhere,
  names its scope in the placeholder and in the header's own line, and
  is disabled with a reason where it cannot serve.
- **The Settings rename one-liner** (found in Phase 3, routed here):
  the library name's click bubbled to `config-page`'s document
  handler, which exists to close the rename editor, so it opened and
  closed it in the same click.

#### Where the plan was wrong — the first pass

Seven things, and two of them are about how the finding was checked
rather than about the finding:

- **The two reproductions I wrote first were both invalid, in the same
  way, and one of them nearly shipped a fix for nothing.** Reading the
  DOM synchronously after a synthetic `.click()` reports the state
  *before* Lit renders — so the Settings rename probe returned "not
  editing" both before and after the fix. The bug is real (verified
  properly: `false` before, `true` after, with an await), but for
  twenty minutes the evidence for it was a number that could not move.
  Same trap as this plan's `0 ms` view-open and its 150 ms debounce,
  in a third costume, and now also in an e2e spec that read a count
  before the view had one.
- **`job-indicator` is `display: none` when no job is running**, which
  looks exactly like a control squeezed out of an overflowing header.
  A ladder of viewport measurements said the top bar overflowed by
  0 px at every size while a screenshot plainly showed the badge cut
  off at the right edge; the badge was simply *absent* by then,
  because the index build had finished. The contradiction between the
  number and the picture was the useful signal, and chasing it saved
  fixing a layout that was not broken.
- **`H-11`'s "the app title wraps into the nav" is a subtitle
  problem, and it is not new at 700 px.** The title block is 80 px
  tall inside a 64 px bar at *every* width — it merely stops being
  visible about there, when the subtitle takes a second line and it
  becomes 98 px. Hiding the subtitle under the breakpoint fixes the
  visible half; the 16 px of permanent overflow is cosmetic and
  untouched.
- **`H-7` is not the only reason Duration looks clipped.** With the
  arithmetic fixed, at 800 px the column is at its 50 px floor and the
  *label* still ellipsises to "Durat…", because the saved widths are
  scaled proportionally from whatever size they were set at. The
  values fit; the heading does not. Different mechanism, same
  screenshot, and worth knowing before someone "fixes" the arithmetic
  again.
- **The sort toolbar existed three times, not twice.** The audit names
  Albums and Tracks as the two views with sort controls; `playlist-view`
  has a third copy of the same twenty lines. All three are now the
  header's.
- **Artists cannot have a sort *select*.** `library.Artist` carries a
  name, an MBID and three image URLs — nothing countable — so "the two
  missing sort controls" is really one control and one direction
  toggle. The header renders a label instead of a select with a single
  option in it.
- **Two e2e specs were spending state they never gave back**, which is
  not in any audit and cost most of an hour to attribute.
  `view-lifecycle.spec.ts` toggled shuffle and left it on, so the
  *second* `make e2e` against the same app failed `playback.spec`'s
  shuffle assertion — a failure that reads exactly like a regression
  in whatever you are holding, and which I first assumed was mine.
  Stashing the phase's source changes and re-running proved it
  pre-existing. Shuffle is restored now; the same file also skips an
  autotag album per run out of the eleven the seed has, which is
  inherent and is now in the skill instead.

#### Not done, and still worth doing (after the first pass)

Items 3 and 4 of the four, in that order: the dialogs, the context
menu's keyboard model and the ARIA tail as **one** pass (they are one
focus/semantics story, and splitting them is how two focus traps get
built); then the smaller items — landing on Home, the Home card's
missing-art placeholder, the album page's primary action and its
unexplained ✓ badges, the `?` overlay, Settings reordered with a
Playback section, and an Album column in the track list.

Five of the seven inherited items remain: `cover-grid`'s dead album
dropdown (`perf.p2` — still a missing feature, not housekeeping), the
context menu's keyboard model, `tracklist.delete` (which needs a
"remove from library" that does not exist, and a decision about what
it removes), keyboard seeking from a focused track row, and the
header search box on `smart-playlist-details` — which is a *detail*
view and so was outside this pass's nine primary ones.

One finding of my own, not fixed: **the Home shelves' cards render a
missing cover as nothing at all** (`H-9`), which is plainly visible in
any Home screenshot now that the page has a header above it.

And two things about CI, neither mine and both pre-existing on
`main` at `9e92721`:

- **`player-truth.spec.ts` fails in the CI container**, on the elapsed
  clock (17 s and 11 s adrift, against a tolerance of 1) and
  intermittently on the auto-advance skip. It passed 18 h earlier and
  failed on a **docs-only** commit, so it is the container's audio
  clock rather than a regression. All 44 specs pass locally, twice in
  a row against one app.
- **`CLAUDE.md`'s claim that commitlint enforces the commit format in
  CI is stale** — there is no config and no workflow running it — as
  is the implication that semantic-release runs. Left alone pending a
  decision: wire them up, or stop saying it.

---

### Phase 5 — the second pass: the dialogs, the menu, the ARIA tail, and Home

Item 3 of the four as one landing, plus the first of the smaller ones.
Every finding was reproduced in the running app before it was fixed,
and two of them changed shape when it was.

- **`a11y.4` / `a11y.16` — five hand-rolled dialogs are `wa-dialog`s.**
  Split by shape rather than by owner: the three that only ask a
  question (the autotag warning, the leave-as-is confirmation, the
  remove-library confirmation) are `confirmAction()` calls, and the two
  carrying input (paste URL, MusicBrainz search) are `<wa-dialog>`s in
  place. Verified in the app: the native dialog reports `:modal`, focus
  lands in the first field, Escape closes it and the view state follows
  through `@wa-hide`. **`autotag-view`'s last document keydown listener
  died with them** — it existed only for Escape, because its dialogs
  could not close themselves.
- **`a11y.3` — the context menu has a keyboard model.** `MenuKeyboard`
  in `context-menu-controller.ts`: focus the first item, Arrow/Home/End
  (wrapping), Enter/Space, Escape/Tab, focus restored to the row.
  Shift+F10 and the ContextMenu key open it from a focused row in all
  six hosts. It is standalone rather than part of the controller
  because `playlist-view` renders a menu without the controller.
- **Three lists had no focused row to open it from** — the queue panel
  and both playlist detail views — and gained a roving tab stop
  (`utils/roving-rows.ts`). `track-list` keeps its own.
- **The ARIA tail**: `aria-sort` on the column headers (`a11y.9`),
  `role=listbox`/`option` on the four selectable grids with the invalid
  `aria-selected`-on-`button` dropped (`a11y.13`), live regions on the
  four silent async surfaces (`a11y.12`), the `rem` type scale
  (`a11y.19`), `job-indicator`'s unmanaged `role="dialog"` (`a11y.17`)
  and its colour-only failure dot (`a11y.23`).
- **`H-8` / `H-9` — the app lands on Home, and Home is worth landing
  on.** The missing-art placeholder draws the letter tile the other two
  grids draw, and a shelf that repeats the one above it is suppressed
  in `backend/home` — the same rule as omitting an empty one.

#### Where the plan was wrong — the second pass

Eight things, and three of them are the audit describing a build that
had already moved:

- **`a11y.12`'s first bullet was fixed two phases ago, twice.** It names
  `config-page`'s private toast as having no `role="status"`. Phase 3
  deleted that toast, and the surface that replaced it —
  `notification-host` and `inline-notice` — has had `role="status"
  aria-live="polite"` from the day it was written. Two of the finding's
  five bullets were closed by a phase that was not about accessibility.
- **`H-9`'s stated mechanism is not why the card is invisible.** The
  audit says the placeholder "has no background". It has one:
  `--yj-bg-surface`, which is *almost exactly the page colour*, holding
  a `wa-icon` at `--yj-text-tertiary`. And the icon has rendered at all
  only since Phase 4 bundled the icon set — before that the fallback
  was a CDN fetch, so "renders as nothing" was literally true offline
  and is now merely nearly true. The fix is the same; the reason it was
  worth checking is that "no background" would have been fixed by one
  line that changed nothing visible.
- **`H-9`'s duplicate-shelf half does not reproduce as stated, and the
  obvious rule breaks a small library.** The audit says "all three
  shelves show the same seven albums". On the fixture library there are
  five shelves and the duplication is one *pair* — "On repeat" is
  "Pick up where you left off" reordered. The first rule I wrote
  (suppress at two-thirds overlap with the shelf above) **collapsed a
  four-album library to a single shelf**, and the second (guarded by a
  fixed shelf size) let an 11-album library keep three identical
  shelves while a 13-album one lost them. Both were caught by the
  *existing* Go tests, not by the one written for the change. The rule
  that survives is "a repeat is a fault only if a different row was
  possible" — the shelf must not be showing the whole library.
- **Landing on Home broke eleven e2e specs, and one of them was an app
  bug.** Nine assumed the track list is the first thing on screen. One
  failed because Home's shelves name the same artists as
  `artists-view`, and every primary view stays in the DOM — so an
  unscoped `getByText().first()` matched a card on a `.view-hidden`
  page. And one was real: **`getByRole('button', {name: 'Shuffle'})`
  resolved to two elements**, because Home's page-header action and the
  transport's shuffle mode had the same accessible name. They were
  never on screen together before; a cached Home is in the
  accessibility tree from the first paint. It is "Shuffle suggestions"
  now.
- **`a11y.19` and `a11y.20` are one finding, and the second one wins.**
  Converting the type scale to `rem` works — verified at a 24px root, a
  track cell goes 12px to 18px — and does *not* reach the four
  virtualized lists, whose rows are a hardcoded px height duplicated as
  the layout's `_itemSize` and carry `contain: strict`. Measured: the
  row stays 33px while its text grows to 18px, so larger text crops it.
  Left unfixed and documented in the token file, which is what a11y.20
  itself asks for: deriving `_itemSize` from a measured row is a change
  to the scroll maths of four lists, not to a type scale.
- **Two of the three things that made the menu work are invisible to a
  component test.** `wa-dropdown-item` sets its `role` in its own first
  update, so querying by role at the host's `updateComplete` finds no
  items; and `focus()` on a `wa-popup` that has not positioned itself
  is a silent no-op. Both produced a menu that opened and refused to
  take focus, and both were found by driving the real app — a component
  test against hand-built markup passes either way, which is why the
  e2e spec exists.
- **A backtick in a comment inside a `css` template literal ends the
  literal.** The skill warns about this. I did it twice in one session
  anyway, and the second time every test file in the suite failed to
  import, which reads like anything except a stray backtick.
- **The `wa-dialog` migration removed state that had a use.**
  `config-page`'s `isRemoving` had no reader once the dialog owned the
  spinner. Rather than delete it, `removingLibraryId` now means "which
  row is busy" and the row says "Removing…" — the removal is a backend
  call of unknown length, and moving the confirmation out of the page
  had quietly removed the only feedback that it had started.

#### Not done, and still worth doing (after the second pass)

Item 4's remaining smaller items, each independently landable: the
album page's primary action and its unexplained ✓ badges (`H-13`), the
`?` shortcuts overlay (and with it keyboard seeking from a focused
row), Settings reordered with a Playback section (`H-22`) together with
`a11y.1` (`config-section`'s disclosure header is a bare `<div @click>`,
so every setting is behind a control that cannot be tabbed to) and
`a11y.2` (the Downloads tabs), an Album column in the track list
(`H-15`), and `cover-grid`'s dead album dropdown (`perf.p2`).

Three inherited items are unchanged: `tracklist.delete` (below), the
header search box on `smart-playlist-details`, and keyboard seeking
from a focused row.

**`tracklist.delete` was deliberately not built.** It needs a "remove
from library" that does not exist *and* a decision about what it
removes. Neither answer is currently right: removing the row is a lie
unless it also excludes the path, since the next scan brings it back,
and removing the file is a delete-your-music button one keystroke from
a focused row. The honest interim is to stop advertising the binding in
Settings; that is not done yet either.

---

### Phase 5 — the third pass: Settings, the key story, the small ones, and the CI answer

Three independently landable pieces of item 4, plus the question that
was supposed to be first and turned out to be answerable in ten
minutes.

- **`a11y.1` / `a11y.2` / `H-22` — Settings is reachable.**
  Reproduced exactly as written: seven `config-section` headers, seven
  bare `<div @click>`s, `role` and `tabindex` null on every one, all
  collapsed. They are `<button aria-expanded aria-controls>` now, on
  the pattern `explore-artist-details` has had five of the whole time,
  and the body renders unconditionally toggled with `hidden` because
  `aria-controls` has to name an element that exists. Downloads' two
  `<div class="tab">`s are a `role=tablist` with a roving tab stop and
  Left/Right/Home/End. Libraries is first and the only expanded
  section; Search Index, configured once if ever, is second to last.
  Settings also stops advertising `tracklist.delete`.
- **The key story, told once.** `?` opens a `wa-dialog` listing every
  binding, read from `services/shortcut-meta.ts` — moved out of
  `config-page`'s private static, so the overlay and the Settings
  editor share one table. With it, **keyboard seeking from a focused
  row**: Phase 1 gave the grid all six arrows, and no list in this app
  moves horizontally, so `←`/`→` reached nobody. A row owns the
  vertical keys only now.
- **`H-15` and the search scope.** Album is a default track-list
  column, in Go and in the TS fallback. `smart-playlist-details` is in
  `search-store`'s scope map — checked first, as the handoff asked: it
  *does* read the term, so the fix is a scope entry rather than a
  disabled state.
- **The CI e2e failure is answered and is not ours.** Details below.

#### Where the plan was wrong — the third pass

Eight things, and the first is the one that mattered most:

- **The `e2e` CI failure was readable all along, from a different
  endpoint.** `gitea_ci job_logs` 404s on this Gitea build, which two
  sessions took to mean the log was out of reach. The REST API answers
  fine: `/api/v1/repos/{owner}/{repo}/actions/runs/{run}/jobs` lists
  per-step status, and `/actions/jobs/{job_id}/logs` returns the whole
  log. Cost: ten minutes, after two sessions of "cannot check".
- **And WebKit had never run.** The `E2E — webkit` step had no `if:`,
  so a chromium failure skipped it — `conclusion: skipped` on every
  red run. The plan's "CI also runs WebKit, treat that half as
  unverified" was truer than intended: it had produced no signal at
  all for as long as chromium had been failing. With `if:
  !cancelled()` it runs, and the answer is **48 passed on both
  engines, failing exactly the same three specs** —
  `playback.spec`'s elapsed clock and two in `player-truth.spec`. No
  WebKit-specific failure anywhere, so last pass's dialog, focus and
  role work is clean on the renderer we ship. What is red is the
  container's audio clock: the UI interpolates while the backend
  position stays at zero, 17–18 s adrift. `ci.yml`'s claim that the
  null ALSA plugin advances at real time was measured once and is no
  longer true.
- **A reproduction of the *fix* can be as invalid as one of the bug.**
  The seek-from-a-row fix measured zero `Player.Seek` calls after it
  landed — because nothing was playing, and with no track loaded the
  dispatch records nothing on any build. Both the broken and the fixed
  build answer "no seeks", which is this plan's most-repeated trap in
  its seventh costume, this time on the *after* side.
- **`?` cannot be a toggle, and the app is right to stop it.** The
  overlay was written to toggle; the e2e spec asserting it failed,
  because `focusedControlOwnsKey` yields every unmodified key to
  anything inside an open dialog. Escape closes it, as it does every
  dialog here. A shortcut that a dialog swallows is a promise the
  shortcut layer cannot keep, and finding that out cost one spec run.
- **Every `wa-dialog` in this app is an unnamed dialog.** `a11y.md`'s
  "what is already correct" says all five are modal, restore focus and
  "every one of them passes a `label`" — all true, and the label never
  reaches the accessibility tree. Web Awesome renders it into an `<h2
  id="title">` in the same shadow root as the `<dialog>` and never
  points `aria-labelledby` at it, so `getByRole('dialog', {name})`
  matches nothing. Found by writing that locator. Not fixed here: it
  is eight call sites and a helper, and it should be done on purpose.
- **`H-15`'s Album column does not do what `H-15` says it will.** The
  three `Tideline / Aurora Fields / 00:06` rows are duplicates of the
  *same album*, so with an Album column they read identically. The
  column is still the right default; what tells those rows apart is
  the duplicate-detection feature or a path column. Visible only in
  the screenshot — nothing failed.
- **`H-22`'s Playback/Audio section cannot be built honestly yet.**
  There is no output-device, gapless, crossfade or replay-gain setting
  anywhere in `backend/config`; the whole section would be controls
  that do nothing. Same judgement as "Artists cannot have a sort
  *select*" from the first pass. The reorder shipped; the section is a
  feature, not a consistency fix.
- **A default that a seed has already persisted needs the seed
  rebuilt.** Changing `DefaultColumns` changed nothing in the running
  app, because `.dev/seeds/default.tar` carries a `config.toml` with
  the old three columns — while CI builds its seed by running the app
  and would therefore have tested a *different* default from the one
  measured locally. `make sandbox-seed NAME=default` first.

#### Not done, and still worth doing (after the third pass)

**The album page is the one item of 4 left**: `H-13` (no Play, no
Shuffle, no Add to queue on `explore-album-details`, and the green ✓
badges with no legend) together with `cover-grid`'s `renderSplitGrid`,
which is referenced only to satisfy `noUnusedLocals` and is the only
route from the albums grid to `track-details`.

And two things this pass found rather than inherited:

- **Name the dialogs.** One helper, eight call sites.

**CI is green, and the audio clock was the last thing between it and
green.** Fixed the same pass: `ci.yml` used ALSA's `null` plugin on the
belief that it paces, and it does not — measured in the CI image
through beep and oto with `player.InitSpeaker`'s own arguments,
**3000 ms of audio consumed in 2.96 ms**, against **3762 ms** through a
PulseAudio null sink. Every track finished instantly, so the position
reset to zero and three specs failed on a clock that never moved. It
looked like a flake because `InitSpeaker` succeeds either way, in ~3 ms
either way. `check` and `e2e` now both pass, **54 specs on Chromium and
54 on WebKit** — the first fully green run this plan has had.

Two things about how that was found are worth carrying forward. The fix
was **verified in `ubuntu:24.04` under Docker before it was pushed**,
including under the private session bus and Xvfb `dev-headless.sh` uses
— the CI container is reproducible locally, which nothing had tried.
And the sink is now **checked like the dependency-with-a-rate that it
is**: a step plays three seconds and fails if they take under two,
because otherwise the failure surfaces three steps later as "the
elapsed clock is 19 s adrift" and reads as an app bug.

---

### Phase 5 — the fourth pass: the dialogs get names, and the album page gets an action

The last of item 4, in three independently landable pieces. Each was
reproduced in the running app before it was fixed, and two of the three
changed shape when it was.

- **Every `wa-dialog` has an accessible name.** Eleven call sites (not
  the eight the last pass estimated, and not the five `a11y.md` lists —
  six have been added since it was written), one helper,
  `utils/name-dialog.ts`. It points the native `<dialog>` at the `<h2
  id="title">` Web Awesome renders `label` into and never links,
  falling back to `aria-label` under `without-header`. Verified through
  CDP's `Accessibility.getFullAXTree`, which reports the name and that
  it came from `relatedElement`.
- **`perf.p2` — the album dropdown is drawn.** `renderSplitGrid` was
  not dead code but a missing feature whose data path already worked:
  Enter on an album card fetched the tracks and ran the whole split
  state machine, and `render()` ignored `splitMode`. It is the only
  route from the albums grid to `track-details`, since a plain click
  navigates to the catalog page.
- **`H-13` — the album page has a primary action.** Play / Shuffle
  album / Add to queue on `explore-album-details`, labelled by how much
  of the release the user owns, plus the legend the ✓ badges never had.
  Backed by a new `GetFilePathsByRecordingMBIDs` binding, the third
  member of the `GetFilePathsBy…` family.

#### Where the plan was wrong — the fourth pass

Nine things, and three of them are findings that had a second bug
hiding behind them:

- **`perf.p2` is filed as housekeeping and is a two-bug feature.**
  "Dead code carried in the bundle" is `renderSplitGrid` plus
  `scroll-manager.ts` (916 lines) and `album-dropdown.ts` (461) — 1 463
  lines that had never executed. Enabling the render exposed both of
  the others.
- **The albums grid could not scroll at all.**
  `.grid-scroll-container` is the same markup `artists-view` and
  `genres-view` use, and `cover-grid` had the class with **no rule for
  it**, so the container grew to its full content height inside an
  `overflow: hidden` host: 186 984 px of albums in a 772 px box at
  5 000 albums, unreachable by wheel, keyboard or scrollbar. Invisible
  on the eight-album fixture, which is why nothing had ever caught it —
  and it is the element the scroll manager saves and restores, so that
  machinery had been aiming at a `scrollTop` that was permanently 0.
  With a real scroller it works as designed (2891 preserved exactly
  across an expand at 5 000 albums).
- **The shared context menu was labelled "Album actions"
  unconditionally** — including on a track row, which nothing could
  observe while the only menu that could open on one was unreachable. A
  finding creates the conditions for the next one.
- **`H-13`'s "unexplained ✓ badges" has half aged.**
  `library-status-indicator` carries a `title` *and* an `aria-label`
  reading "Album “X” is in your library", so a hover and a screen
  reader both get a full sentence. What was missing was a key for a
  sighted user scanning a column of green circles. Also worth knowing:
  it is a `<button>` whose click handler is a comment saying "wire this
  up later" and a `stopPropagation` — a badge in a button's clothes.
- **`albumLibraryStatus()` is four claims OR'd into one tick**, the
  weakest of which fires when *one* recording of a forty-track release
  matches. Right for a badge, useless for a button — which is why the
  header counts the tracklist instead of reading the status.
- **The obvious key for "play what I own" does not exist.**
  `MBTrack.LocalID` is declared, is in the generated bindings, and
  **nothing in the backend ever writes it**. Ownership is decided by
  recording MBID (`markReleasesInLibrary` → `CheckMBIDs`), so that is
  what the new binding is keyed on.
- **…and keying on it alone shipped a Play button that queued
  nothing.** A library-only album has no recording MBIDs at all — its
  tracks are synthesised with `mbid: RecordingMBID || ''` — so on the
  fixture library the button was wired, labelled correctly, clicked
  cleanly and queued 0 tracks. Every component test passed. Caught by
  clicking it in the running app and reading the queue.
- **`shuffleStart` does not start a shuffle.** `Queue.SetQueue`'s third
  argument picks a random first track *when shuffle mode is already
  on*, so a Shuffle button has to set the mode first. Reading the Go
  rather than the parameter name was the difference between a working
  button and one that plays track 1.
- **"Shuffle" is still two controls with one name.** The first pass hit
  this on Home; the album header would have hit it again, since the
  transport's shuffle mode is on screen whenever this page is. It is
  "Shuffle album".

And two about the probes rather than the findings:

- **The a11y snapshot cannot see a dialog's name.**
  `playwright-cli snapshot` prints `- dialog [ref=…]` whether the
  dialog is named by `aria-labelledby`, by `aria-label`, or not at all
  — checked all three ways. A snapshot read as the oracle here reports
  failure on a working build. `getByRole('dialog', {name})` and CDP
  both answer correctly, and the e2e spec was watched failing on a
  probe-disabled build before it was believed.
- **An e2e assertion about scroll position was vacuous and said so
  under pressure.** "Wherever the scroll was, it stays" passed against
  a `scrollTop` of 0 both times on an eight-album fixture. Shrinking
  the viewport until the grid actually scrolled turned it red — and
  the red was *correct*: `scrollToShowDropdown` deliberately moves the
  scroll to reveal the dropdown (80 → 4, with the content taller
  after, so not clamping). The premise was wrong, not the app; the
  assertion is now "the dropdown is on screen".

#### Not done, and still worth doing (after the fourth pass)

Item 4 is complete. What remains from the inherited list is
`tracklist.delete`, which still needs a "remove from library" that does
not exist and a decision about what it removes.

Two things this pass found and did not fix:

- **`library-status-indicator` is a button that does nothing.** Every
  tick and every "add to library" affordance in Explore is a `<button>`
  whose handler stops propagation and returns. It should be a
  non-interactive `role="img"` with its existing label until the
  download-client integration it is waiting for exists — as written it
  is a keyboard stop that promises an action on 30-odd elements per
  page.
- **The split grid's roving tab stop was not re-examined.** The
  dropdown path renders two virtualizers where there was one;
  `roving-grid` is attached to the scroll container and keeps working,
  but nobody has checked what Home/End mean across a split.

## Phase 6 — Explore starts the conversation

The only phase that adds rather than repairs.

### What's wrong

`H-23`. Explore is a search box over a 1.1 M-row local catalog. A new
user lands on a blank panel reading "Search to discover artists,
albums, and tracks." and has nothing to do but type — into a catalog
whose whole point is that they do not yet know what is in it.

Every other view answers **"what have I got"**. Explore is the only one
that answers **"what exists"**, and it will not start the conversation.
That is the same failure as everything else in this plan wearing a
different hat: the app knows something and does not say it.

### What ships

**Shelves, on the same terms as Home.** `backend/home`'s convention
holds: a shelf is a **reason**, not a filter, and it carries the
sentence that says so. A shelf with nothing behind it is omitted, never
rendered empty.

The data is already local — `explore_index` carries `popularity` and
`listener_count` per row (`artifactimport.go:93`) — so these are index
queries, not network calls, and the page stays usable offline. Starting
set, to be cut down once they can be seen next to each other:

- **Popular right now** — straight `popularity` ordering, the honest
  default for "what exists".
- **Big in a genre you already have depth in** — joins the catalog to
  the user's own library, which is the shelf most likely to land.
- **Artists next to ones you own** — catalog artists sharing a genre or
  release-group neighbourhood with the library, excluding what is
  already owned.
- **You own one album by this artist** — the catalog's answer to a gap
  the library can already see, and the natural bridge into the existing
  "Want this" flow.

**A query layer in `backend/explore`, mirroring `backend/home`:** the
queries return MBIDs only and are joined back to the existing catalog
projection in Go, so there is one definition of an Explore card rather
than five.

**The search box keeps its place at the top.** The shelves are what the
page shows *before* a query and what it returns to when the query is
cleared — not a separate mode, not a tab.

**Every card routes through what already exists:**
`explore-album-details` / `explore-artist-details` for the destination,
`<catalog-scope-notice>` for admitting what is being shown, and
`utils/explore-link.ts` for names. Phase 5's page header and Phase 3's
Inline level for a failed shelf. Nothing new invented at the edges.

### Verification

`make ui-test` for the shelf builder's omit-when-empty and
exclude-what-is-owned rules; a Go test per query against a seeded
index; an e2e spec asserting the page renders shelves on arrival with
no typing, that a cover opens the catalog album page, and that clearing
a search returns to the shelves.

### Not in this phase

Anything requiring a network call on page load. The point is that the
shipped artifact already contains the answer.

### Phase 6 — what actually shipped

The two inherited one-liners from Phase 5's fourth pass, and then the
phase itself. Three landings.

- **`library-status-indicator` is a badge.** It was a `<button>` whose
  click handler was a `stopPropagation()` and a comment. Measured in
  the running app on an Explore results page: **66 tab stops, 20 of
  them inert** → **46 and 0**. It is `role="img"` with its existing
  label, and the unowned label says "… is not in your library" rather
  than "Add … to library", which was the button's promise written out.
- **The card grids move by a row.** Reproduced first, then fixed: at
  700×700 with three real rows of 3/3/2, ArrowDown from card 0 landed
  on card **7**.
- **`H-23` — Explore opens with shelves.** Three of them, on `home`'s
  terms, over `explore_index`; two of the plan's four could not be
  built at all. Plus an honest page for the no-catalog case, which is
  what CI and every first run actually have.

Pinned by `roving-grid.test.ts` (6), `explore-shelves.test.ts` (7),
`shelves_test.go` (8), and `e2e/specs/explore-shelves.spec.ts` (4) plus
one case added to `album-actions.spec.ts`. `make ui-test` 545 → **558**;
`make e2e` 62 → **68**.

#### Where the plan was wrong — Phase 6

Twelve things. This phase's plan text was the least tested material in
the repo — written before any of Phases 1–5 existed — and it shows
most in the shelf list, where **half the named shelves are not
buildable against the schema they were specified over**:

- **"Big in a genre you already have depth in" cannot be built.**
  `explore_index` has no genre or tag column; genre exists only in the
  library's own `recording_genres`. There is nothing to join. Dropped,
  not deferred — building it means changing the dump pipeline.
- **"Artists next to ones you own" cannot be built offline.** It needs
  `similar_artist_map`, which `cmd/indexexport` does not ship (the
  artifact carries `explore_index` and its metadata, nothing else) and
  which is filled lazily by ListenBrainz calls from artist pages. It is
  empty on a fresh install and empty offline — exactly when this page
  most needs something to show. The plan's own "not in this phase"
  rules it out in the same breath as naming it.
- **"You own one album by this artist" is empty on every untagged
  library**, including the fixture one. Ownership is `in_library`, set
  by MusicBrainz ID; the seed has **0 artists with an MBID**, so the
  shelf is correctly absent everywhere it could be looked at locally.
- **The plan says the queries return MBIDs. They return row ids.**
  `rowsByIDs` is keyed on the primary key and preserves the order it is
  given, which is what lets the ordering stay in SQL. MBIDs would mean
  a second lookup for nothing.
- **The card projection existed but not as a function.** "One
  definition of an Explore card" was three inline struct literals
  inside `mergeIndexHits`, tangled with search scoring. Extracting them
  is what made the claim true rather than aspirational — and `Score` is
  deliberately *not* part of them: it is a property of a search, and a
  shelf has no query to be relevant to.
- **"A shelf with nothing behind it is omitted" is the wrong rule for
  this page.** On Home an omitted shelf means a library with no
  history, which is honest. Explore's data is a *downloaded artifact*,
  so an empty page can mean it has not arrived — and rendering nothing
  is the blank panel the phase exists to remove. The page carries a
  `state` (`ready` / `building` / `no-index`) and says which.
- **The premise "the shipped artifact already contains the answer" is
  false in CI and on every first run.** `ci.yml` points
  `YJ_CORE_INDEX_URL` at a dead address, so the e2e job has **0**
  catalog rows. The first version of the spec skipped three of its four
  cases there, which is no signal at all; it stages its own small
  catalog through `/__test/sql` instead, verified by reproducing the
  empty world locally with the same environment variable.
- **…which only works because the readiness gate is a question, not a
  flag.** Two cached answers were tried and both were wrong in the same
  way. `GetIndexStatus().TotalRows` is refreshed only between build
  tiers, so on an ordinary launch it reads 0 beside a full catalog and
  hid every shelf. `IsReady()` is set once at startup, so rows staged
  afterwards are invisible. One `SELECT 1 … LIMIT 1` cannot be stale.
  Both are the shape `emitStatus` warns about — a derived value with
  nothing polling behind it.
- **Two shelves with disjoint ids still repeated each other.** Ordered
  by raw listen count, the catalog's top albums are seven records by
  one act and its members, and the artists row underneath was the same
  seven people. `home`'s adjacent-duplicate guard cannot see it — the
  rows hold different entity types, so no two share an id, which the
  code comment cited as proof the guard was unnecessary. **Found in a
  screenshot, by reading it.** The fix is one album per artist, and
  skipping artists a row above already showed.
- **The e2e staging step staged nothing, and looked like it worked.**
  Six values against seven placeholders, and the response was never
  read. A setup whose failure is not checked is not setup.
- **`library-status-indicator`'s label was only right for one of its
  three states.** "Add artist “Eno” to library" is an offer, from an
  element that cannot accept it.
- **A `<button>` and a `<span>` are not the same box.** Dropping the
  button grew the badge 36px → 38px, because the UA stylesheet gives a
  button `box-sizing: border-box` and a span nothing. Caught by a
  stored screenshot, which is the only thing that would have.

And one about a probe, in this plan's longest-running family: **an e2e
spec that reads the DOM immediately after a navigation reads it before
the fetch it triggered.** `shelfHeadings()` returned `[]`, which is
also what a broken page returns; it passed on the second run of the
same build because the caches were warm. The wait belongs in
`beforeEach`, so no test can start from a page that has not answered.

#### Not done, and still worth doing (after Phase 6)

- **The albums row still leads with one act.** One-per-artist fixed the
  literal repetition; it cannot know that eight artists are one group
  and its solo members, and nothing in `explore_index` expresses that.
  A "related act" notion would need dump-side data.
- **The unowned badge still draws a `+`.** It is no longer a control
  and no longer says "Add", but a plus glyph is an affordance. Left
  alone deliberately: it becomes correct again the day the badge
  becomes a button, and changing it touches four components' visual
  baselines for a judgement call that is better made then.
- **No `make perf` before/after.** Both seeds' catalogs come from the
  artifact rather than from the seed tarball, so a before and an after
  are not measuring the same corpus unless the staging fixture is
  extended to bulk scale. The shelves are three indexed queries behind
  a view activation, not a startup-path cost, so this is a want rather
  than a gap — but it is not measured, and is recorded as such.
- **`tracklist.delete`**, still inherited, still needs a "remove from
  library" that does not exist.

---

## Decisions

**1. Unmodified single-key global shortcuts stay.** *(Decided
2026-08-11.)* Phase 1 keeps `Space N P S R M / Q ↑ ↓ ← →` global and
suppresses a binding only when the focused control owns that key.
Phase 5 adds a `?` overlay so they are discoverable somewhere other
than Settings.

**2. The header search stays view-scoped.** *(Decided 2026-08-11.)*
Phase 5 makes it say so — scope in the placeholder and in the
no-results copy — and stops it appearing and disappearing between
views. No top-results view, no global index of local content.

**3. Explore gets its browse, in this plan.** *(Decided 2026-08-11.)*
Phase 6. Additive work is in scope where it fixes a UX gap, and an
empty page over a 1.1 M-row catalog is one.

**4. All four notification levels ship, and the caller picks.**
*(Decided 2026-08-11.)* Table and rule below; call sites choose the
level using the rule, and the surface owns coalescing.

### The four levels

| Level | Behaviour | Use for | Examples from the audit |
|---|---|---|---|
| **Blocking** | Modal, must be acknowledged | Data at risk; the user must know before continuing | Autotag apply failed partway through a folder (`errors.C3`); a batch tag write failed on some files |
| **Persistent** | Stays until dismissed, with an action | Something the user asked for did not happen and retrying is meaningful | Scan/full-rescan failed (`M5`), playlist delete failed (`M6`), download request removal failed (`M7`), add/rename library failed (`m5`) |
| **Transient** | Toast, auto-dismisses | Small action failed; the state visibly reverted anyway | Favourite revert (`m2`), add-to-playlist failed (`m7`), job pause/resume failed (`M4`) |
| **Inline** | Rendered in the region that failed, never a toast | The failure belongs to one panel and a global message would be noise | Explore search error (`M9`), Settings index status (`M3`), track list load failure (`M2`), seek failed (`C2`) |

The rule that decides the level: **a failure is only worth interrupting
for if the user can do something about it that they are not already
doing.** A track that will not play is Inline-plus-skip, not a modal,
because the useful response is to keep playing. A half-retagged folder
is Blocking, because there is no way to discover it later.

Two standing consequences:

- **Playback failure does not raise a message per bad file.** A queue
  of 200 tracks from a disconnected drive produces one notification
  — "skipped 12 tracks that could not be played", with a way to see
  which. Coalescing lives in the store (`(level, key)` within a
  window), not in each call site, so this holds for every future
  caller without anyone remembering it.
- **Blocking is rare by construction.** Two callers are anticipated
  (autotag apply, batch tag write) and both are "files on disk were
  partially modified". A third should be argued for, not assumed.

## Deliberately not planned

- `perf.p1`/`p2` (a dead dependency and an unreferenced
  `renderSplitGrid`) — real, but housekeeping.
- The mouse-only resize handles (`a11y.28`) — four of them, all
  cosmetic preference, no function lost.
- Colour contrast — flagged as borderline (`--yj-text-tertiary` on
  `--yj-bg-surface` ≈ 4.1:1 against 11 px text) but never measured.
  Worth measuring before planning.
- WebKit2GTK-specific behaviour (whether page zoom is reachable in the
  Wails shell; how Orca traverses the virtualizer's windowed DOM).
  Only answerable on the real shell, and CI is the only place WebKit
  runs.

## Coverage map

Every finding lands somewhere, so nothing is dropped silently.

| Source | P1 | P2 | P3 | P4 | P5 | P6 | Dropped |
|---|---|---|---|---|---|---|---|
| `hands-on.md` H-1…H-24 | 1,2,5,6 | 3,16,17,18 | 12 | 4,14 | 7,8,9,10,11,13,15,19,20,21,22,24 | 23 | — |
| `a11y.md` | 1,2,3,5,7,8,11,27,28,30,33 | — | 4,16 | — | 6,9,10,12,13,14,15,17,18,19,20,21,22,23,24,25,26,29,31,32,34 | 7 (top-results cards) | 28 (partial) |
| `perf.md` | m3, p6 | — | — | C1–C5, M1–M10, m1,m2,m4,m5,m6,m7, p3,p4,p5 | — | — | p1, p2 |
| `errors.md` | — | C1,C2,m1,m2 | C3,C4,M1–M9,m3–m8,p1–p4 | — | — | — | — |

## First step

Phase 1, and within it the H-1 reproduction as an e2e spec **before**
any fix — it is a four-line spec (open Autotag, note count, navigate,
press `s`, assert unchanged) and it currently fails. Everything else in
that phase is verified by making it pass and keeping it passing.

## A note on scope

Six phases is a lot for one plan, and the honest risk is that "007"
never finishes. Each phase is written to be shippable and verifiable on
its own precisely so that stopping after any of them leaves the app
better rather than half-converted. If it starts to sprawl, the clean
cut is after Phase 3 — that is the point at which the data-loss bug,
the lying player and the silent failures are all gone, and what remains
is performance and polish.

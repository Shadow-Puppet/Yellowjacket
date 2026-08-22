# 019 — The Android touch model

**Issue:** #63 (`Area/Library-UI`, `Kind/Feature`, `Priority/High`)
**Depends on:** #60 (bottom-sheet menus) — closed, merged as PR #176
**Relates:** #67 (inline links into the menu), #71 ("More" nav), #54
(native feel), #5/#8 (selection, drag to queue — the desktop semantics
being diverged from)
**Status:** shipped (phases 1-4). Its one deliberate remainder is #200.

#73 puts #60 first in Phase 4 because it is "the presentation every
other item needs", and this is the next one. The Direction on #63 asks
for the interaction model to be designed as one piece before any of it
is built, because it *reassigns an existing gesture* rather than adding
one — `utils/long-press.ts` currently owns the 500ms hold, and every
context menu in the app is downstream of it.

This document is that design. Everything below is a measurement, or an
argument for one of the choices #63 leaves open.

---

## The mapping

| gesture | pointer is a finger | pointer is a mouse |
|---|---|---|
| single tap / click | **play the row** | select the row |
| double | — | play the row |
| long press (500ms) | **enter selection mode** | — |
| right-click | — | context menu |
| swipe right | **add to queue** | — |
| drag | reorder / drag to playlist | reorder / drag to playlist |

Three of those are #63's report unchanged. Two are decisions it left
open, and one is a deliberate divergence.

---

## Decision 1 — the predicate is the pointer, not the platform

#63 says "the row component needs a platform-aware interaction layer
rather than shared handlers". It needs an interaction layer; it should
not be platform-aware.

**The question a row has to answer is not "am I on Android" or "is the
viewport under 600px" but "what made this event".** `pointerType ===
'touch'`, read off the event that is being handled, which is already
how `long-press.ts` decides (`if (e.pointerType !== 'touch') return`)
and is the only such test in the frontend today.

This is #64's rule — the predicate is named after the capability, not
the platform — and it carries #64's warning with it. Keyed on a width:

- an Android **tablet** at 600px or more gets click-selects /
  double-click-plays on a touchscreen, which is the exact inversion
  this issue exists to fix, on the platform it exists for;
- a **touchscreen laptop** cannot be described at all, because both
  pointers are live in the same session on the same row;
- and a narrow desktop window gets phone semantics with a mouse.

Per event, all three are right for free, and there is no second
declaration of what a phone does — the thing CLAUDE.md declines to add
every time it comes up.

**Measured, so this is not an assumption about the WebView.** On the
reference device (TLP301, Android 14, WebView Chrome 113, 424x439),
driving a real tap with `adb shell input tap`:

```
[["down","touch",78,94],["touchstart","touchstart",0,0],["up","touch",78,94]]
```

`PointerEvent` exists, `pointerType` is `"touch"`, `maxTouchPoints` is
5, and `(pointer: coarse)` / `(hover: none)` both match.

---

## Decision 2 — there is no double-tap, and the number is why

#63 asks for *single tap → play* **and** *double tap → context menu*.
Those two cannot both be honoured. The first tap of a double tap is
indistinguishable from a single tap until the interval expires, so
"tap plays" necessarily becomes "tap waits to find out whether you
meant something else, then plays". The app already owns that constant:
`utils/explore-link.ts` holds a navigation for `DOUBLE_CLICK_GRACE_MS
= 250` for precisely this reason.

**What it would be added to, measured on the device.** Six runs, from
the play command to the backend's `TrackChanged`:

```
155, 123, 85, 56, 91 ms          median ~100
```

So the app's primary interaction is ~100ms, and a double-tap
discriminator makes it ~350 — **3.5x, of which 250ms is spent
deliberately doing nothing** — paid on every track anyone ever plays,
in order to reach a menu.

It is also against the platform's convention, which counts for more
than usual here because this is the phone build and nothing else:
long-press is *how you select* on Android (Gmail, Files, Photos),
double-tap is zoom or nothing, and a list's menu is either the
long-press sheet or a per-row overflow.

**So the menu and the selection action bar become the same surface**,
which is the convention and removes a concept rather than adding one.
Long-press selects the row it was made on and raises the action bar;
the bar's actions *are* the context menu's actions, contextualised to
whatever is selected — one row or forty. #60's bottom sheet stays
behind it as the overflow, so `contextMenuStyles`, `MenuKeyboard` and
`menu-surface` are reused rather than reimplemented.

---

## Decision 3 — tap-to-play and selection mode ship together

The obvious phase order is "tap plays first, it is the smallest
change". It is wrong, and the reason is a capability that exists today
and is easy to miss.

**A touch user can already multi-select**: tap selects (the desktop
semantics, which a finger currently gets), and the long-press menu then
acts on the selection. Move tap to play without shipping selection mode
in the same change and there is a window — a release, if it lands — in
which selecting forty tracks to add to a playlist is impossible on a
phone. That is a regression dressed as an increment.

So phase 1 is both, or neither.

---

## What the code looks like now

| surface | how it binds | selection |
|---|---|---|
| `track-list` | delegated on the virtualizer: `click`, `dblclick`, `contextmenu`, `dragstart` | `SelectionController` |
| `queue-panel` | delegated, same shape | `SelectionController` |
| `playlist-details` | per row | `SelectionController` |
| `smart-playlist-details` | per row | `SelectionController` |

All four already share `SelectionController`, and all four resolve a
row from an event by `data-index` / `data-file-path` on the row. So the
gesture layer has one shape to talk to, and "selection mode" is a flag
on the controller they already have rather than a fifth concept.

`utils/long-press.ts` is one document-capture listener that synthesises
a `contextmenu` — the seam that needed no component to opt in. **This
plan keeps that shape and changes what the gesture means**, which is
why it is a rewrite of that file rather than a second listener set: two
document listeners both claiming the 500ms hold is the fault the file's
own header warns about.

---

## Two measurements that decide the implementation

**`touch-action` is `auto` on both the virtualizer and the rows.** With
`auto` the browser owns panning on both axes, so a horizontal drag can
be claimed as a scroll and our gesture ends in `pointercancel`
mid-swipe. A row that wants a horizontal swipe has to declare
`touch-action: pan-y`: the browser keeps the vertical pan (which is the
virtualizer's scroll, and must stay native or the list stutters) and
hands us the horizontal axis. This is the single most likely way for
swipe-to-queue to "work in Chromium and not on the phone".

**The row is 424x52 on the device**, so a swipe threshold in px is a
fraction of a row height, not of a screen.

**And the third one was found by building phase 1 and then running it**
— it is not something any browser tier can report. Chrome 113's Android
WebView **fires its own `contextmenu` on a long press**. `long-press.ts`
stood down when a trusted one arrived, which was right while both paths
ended in the same place; once a hold can mean selection mode they end
in different places, and standing down means the gesture silently does
the *old* thing. Measured, before the fix:

```
{"log":["contextmenu isTrusted=true"],
 "state":{"bar":null,"menuActive":true,"selected":1}}
```

`yj-long-press` was never announced at all, the context menu opened,
and all 26 tests in the component tier passed — dispatched pointer
events do not make a browser synthesise a `contextmenu`.

So the browser's event is a **trigger, not a competitor**: the gesture
is announced from it, and only a component that claims it suppresses
the native menu. Unclaimed, it propagates untouched. That is the same
"browser wins" outcome, reached by asking instead of assuming — and
verified both ways on the device, a track row entering selection mode
and an album card still opening its menu.

The tier could not *find* it and can *hold* it: a test cannot dispatch
a trusted event, but this module has always told its own apart by
identity rather than `isTrusted`, so an untrusted one from a test takes
exactly the browser's path.

---

## A tier note: this one can be driven, not only measured

`adb shell input tap|swipe` reaches the WebView as real pointer events,
which the log above is evidence of. So for the first time the Android
tier can *perform* the thing under test rather than describe the page
afterwards — a long press is `input swipe X Y X Y 600`, a swipe right
is `input swipe X Y X+N Y 120`.

Device CSS pixels from device pixels, on this phone:
`css = (device - 59) / 2.564` vertically, `css = device / 2.564`
horizontally (measured from the tap above: 200,300 arrived as 78,94).

This does not make the device a spec tier — it does not run in CI and
`make ui-test` still has to carry the assertions. It makes "does the
gesture actually fire on Chrome 113" answerable in seconds.

---

## Phases

**Phase 1 — the seam, tap-to-play, selection mode.** `utils/
touch-gestures.ts` replacing `long-press.ts`: pointer-typed
recognition of tap / long-press / horizontal swipe, dispatched as
composed custom events so a delegated listener in any shadow root
still works. `SelectionController` gains a mode. `track-list` acts on
tap and enters the mode on long press. The action bar.

**Phase 2 — swipe right to queue**, with the `touch-action: pan-y`
finding above and a reveal-and-snap affordance. **Shipped**; what the
device said about it is the section below.

**Phase 3 — the other three surfaces**, which is mostly wiring, since
they already share the controller. **Shipped**, and it was not entirely
wiring — see below.

**Phase 4 — what this leaves behind.** The inline `explore-link`s in a
row are a single-click target inside a row whose single tap now plays;
that conflict is #67's, and this plan should not pre-empt its answer
beyond making tap-to-play win on touch. **Shipped.**

## Phase 3 was not symmetric, in two places

**A tap on a queue row plays that position**, not the list. Copying
`track-list`'s tap — which sets the queue to the list the row is in —
would rebuild the queue *from* the queue, discarding its source, its
shuffle order and everything a user had inserted by hand. It reads as a
no-op and is not one.

**The queue panel has no swipe, deliberately.** A right swipe means
*add to the queue* everywhere else it exists, and a queue row is
already in the queue; the only thing it could mean there is *remove*,
which is the same gesture with the opposite effect one screen away —
the fault `utils/icon-language.ts` exists to have fixed for glyphs.
Removing a queue row is on the row itself (the ×), on its bottom sheet
since #60, and on the selection bar this phase gave it. The assertion
is that its rows do **not** carry `data-swipe`, so a swipe there cannot
silently become a second meaning for the app's one horizontal gesture.

And the affordance became `utils/swipe-to-queue.ts` rather than being
copied twice. Three lists want it; three copies of "how far is far
enough" is three chances for them to disagree, which is what
`utils/library-status.ts` and `utils/ownership.ts` each exist to have
stopped happening. The shared stylesheet is keyed on `[data-swipe]`
rather than on a class name, because the three lists call their rows
two different things and the `touch-action` half of the device fix has
to reach all of them.

## Phase 4 was already true, which is why it is asserted

A claimed tap has its click swallowed at document capture, so an
`explore-link` inside the row never sees one and tap-to-play wins with
no rule of its own. Nothing in the suite would have failed if that
stopped covering the link, and the symptom — tapping a track's *title*
navigating to its album instead of playing it — is one a phone user
meets constantly and a mouse user never does.

**Its test was vacuous when written**, in the way this file keeps
finding: the tap helper dispatched `pointerdown` and `pointerup` and no
`click`, so there was nothing to swallow and the assertion held on any
build. It sends the trailing click now, which also strengthened phase
1's "a tap plays and does not also select". The fixture needed an MBID
for the same reason — without one the link asks the backend for a local
album first and gives up when nothing answers, so "it did not navigate"
was true of a working build and a broken one alike.

---

## What phase 2 measured, which was not what phase 2 predicted

The `touch-action` finding above is **half** of the answer, and
shipping only that half would have been the exact failure it warns
about. Driving a real finger with `adb shell input swipe` across a
track row, three values, all three on the device:

```
touch-action: auto    pointerdown, 1 move,  pointercancel
touch-action: pan-y   pointerdown, 2 moves, pointercancel
touch-action: none    pointerdown, 2 moves, pointercancel
```

`touchmove` kept firing in all three. So **Chrome 113's WebView
cancels the pointer stream ~16px into any drag whatever `touch-action`
says**, and a swipe recognised from `pointermove` — which is what the
rest of this module is built on — is a swipe that dies 16px in.

The other half is a **non-passive `touchmove` calling
`preventDefault()`**: with it, the same swipe ran to 12 moves and a
`pointerup` at full travel. And both halves are required, which was
measured rather than assumed — with the `preventDefault` in place and
`touch-action` back at `auto`, the gesture died after **one** move.
The reading is that `auto` lets the browser commit to a horizontal pan
on the first move past slop, before any threshold of ours can have
been crossed, while `pan-y` leaves it undecided long enough for the
second move to claim it.

`none` is the one value to avoid: the list stopped scrolling at all.
With the shipped pair, a vertical drag still scrolls the virtualizer
81px on the same run that a horizontal one survives.

**`draggable="true"` is not a competitor**, which is the other thing
the device was asked. No `dragstart` fires from a touch drag on this
WebView at all, so the drag-to-playlist attribute on every row needs no
pointer-type gate.

### And it found a phase 1 defect that no tier can see

The native `contextmenu` arrives in **either** order, and phase 1 only
handled one of them. `nativeSeen` covers the browser's menu arriving
*during* the hold. The reverse — our 500ms timer firing first, a
component claiming it, and Chrome delivering its own `contextmenu`
50–70ms *later* — was suppressed by nothing, so the context menu
opened on top of the selection bar. Measured over four holds:

```
hold 1  yj-long-press, then contextmenu isTrusted=true   menu open
hold 2  yj-long-press                                    clean
hold 3  yj-long-press, then contextmenu isTrusted=true   menu open
hold 4  yj-long-press                                    clean
```

Two in four, on the one surface #63 exists to have changed, and
invisible to both browser tiers because neither synthesises a
`contextmenu` from a dispatched press. A press that has produced its
outcome now suppresses a late one whichever branch it took; six holds
on the fixed build, six clean.

### The rules phase 2 settled

- **A swipe is not a selection.** It queues the row it was made on,
  unless that row is one of several *explicitly* selected — the same
  rule the context menu answers with, because a bar reading "40
  selected" beside a gesture that quietly queues one of them is two
  answers to one question. It never changes the selection, which is
  where it differs from a right-click.
- **Rightward only.** Nothing is bound to a leftward swipe and
  claiming one would take a gesture away to do nothing with it.
- **The commit threshold is a fraction of the row** (0.3, floor 72px),
  because the row is 424x52 on this device and a bare pixel count is a
  fraction of a row height on one screen and a third of the width on
  the next.
- **The affordance is not only a colour** (WCAG 1.4.1, the rule the
  playing-row marker exists for): the pane carries the queue icon and
  words, the words change at the threshold ("Add to queue" → "Release
  to add" → "Added"), and the outcome is announced in a live region.
- **The row does not move; its cells do.** `.track-row` is
  `contain: strict` with `overflow: hidden`, so a pane held at the
  row's original position while the row translates is a pane at a
  negative offset inside a clipping box and is simply not painted.
  Sliding the cells needs no wrapper element in a row that is already
  a grid.
- **The travel is written to the row's own style, not rendered.** One
  render at the start, one at the threshold, one at the end; a
  virtualizer re-rendering every visible row per frame of one finger's
  travel is the thing `perf.m1` is about.

## Open questions

1. **Does selection mode have an escape other than the bar's own
   close?** *Settled: Escape, here; back, not here.*

   Escape leaves the mode, from `selection-bar` rather than from each
   of the four hosts — that element exists only while the mode does, so
   it is the one place a dismissal can be attached and detached with
   the thing it dismisses. It is the same documented exception the
   overlaid queue's Escape is: **a dismissal, not a shortcut**, so it
   is not a panel-scoped binding.

   The back gesture is the half that is *not* done, and deliberately.
   The obvious version — `selection-bar` pushing a history entry — is
   precisely the fault `navStack` was deleted for: the shell owns the
   stack (#6/#55) and is the only thing that calls `pushState`, so that
   two stacks cannot disagree about what one press means. Four lists
   each reaching for `history` is four stacks. It is also wrong on its
   own terms, since a mode is per-component and a user who enters one,
   navigates away and returns has an entry for a mode that no longer
   exists. #55 settled the shape for a *place*; a mode is not one,
   which is why it could not simply inherit that answer.

   What it wants is one shell-owned register of dismissible surfaces,
   which would retro-fit the queue overlay, the dialogs and this alike
   rather than adding a fourth private answer. **#200.**

2. **Does a tap on a row's favourite icon still toggle it in normal
   mode?** *Settled in phase 1: yes.* A control inside the row keeps
   its own tap — the gesture is simply not claimed there, so the click
   behind it falls through untouched. It is the same rule the shortcut
   service has for a focused control that owns a key, and it is what
   keeps the 44px favourite target (#56) from becoming a 44px play
   target. The queue row's × is the second instance of it.

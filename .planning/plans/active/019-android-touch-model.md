# 019 — The Android touch model

**Issue:** #63 (`Area/Library-UI`, `Kind/Feature`, `Priority/High`)
**Depends on:** #60 (bottom-sheet menus) — closed, merged as PR #176
**Relates:** #67 (inline links into the menu), #71 ("More" nav), #54
(native feel), #5/#8 (selection, drag to queue — the desktop semantics
being diverged from)
**Status:** in flight.

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
finding above and a reveal-and-snap affordance.

**Phase 3 — the other three surfaces**, which is mostly wiring, since
they already share the controller.

**Phase 4 — what this leaves behind.** The inline `explore-link`s in a
row are a single-click target inside a row whose single tap now plays;
that conflict is #67's, and this plan should not pre-empt its answer
beyond making tap-to-play win on touch.

---

## Open questions

1. **Does selection mode have an escape other than the bar's own
   close?** Back is the platform's answer and the shell already owns
   the history stack (#6/#55). Pushing an entry for a *mode* rather
   than a place is the same argument #55 settled for the overlaid
   queue, and it should probably be settled the same way — but the
   queue is a screen and a selection mode is not, so it wants its own
   paragraph rather than an assumption.
2. **Does a tap on a row's favourite icon still toggle it in normal
   mode?** It is inside the row and the row now plays. It has to keep
   working — it is a 44px target since #56 — so the gesture layer needs
   the same "a control inside the row wins" rule the keyboard service
   has for a focused control that owns a key.

# 018 — Supported sizes, and what the queue panel is

**Issue:** #24 (`Area/Shell-Nav`, `Priority/High`, `Reviewed/Confirmed`)
**Unblocks:** #55 (queue as a screen) — a real Gitea dependency
**Relates:** #69 (page-header overflow), #12 (mini-player), #51 (small-screen umbrella)
**Status:** complete — #24 shipped as PR #132, and the matrix's last
unkept promise closed with #69.

#73 puts this first in Phase 2 and hangs the rest of the phase off it,
so the decision has to be written down and arguable before any CSS
moves. This document is the decision. Everything below the matrix is
either a measurement or an argument for one of the four choices #24
asks for.

---

## What is actually wrong, measured

Against the running app (`make dev-headless SEED=default`, Chromium),
Playlists, sweeping the viewport with the queue open and closed. The
number that matters is how much of the page header survives.

| viewport | sidebar | queue | main panel | header needs | actions clipped |
|---|---|---|---|---|---|
| 1280×800 | 200 | open 321 | 759 | 759 | — |
| 1000×700 | 200 | open 321 | 479 | 747 | New Playlist, New Smart Playlist |
| **900×600** | 200 | open 321 | **379** | 747 | **all three** |
| 800×600 | 56 | open 321 | 423 | 747 | all three |
| 700×600 | 56 | open 321 | 323 | 747 | all three |
| 390×780 | — | open 321 | **69** | 747 | all three |
| 320×600 | — | open 321 | **0** | 747 | all three |
| 900×600 | 200 | closed | 700 | 747 | New Smart Playlist |
| **800×600** | 56 | closed | 744 | 747 | **New Smart Playlist (158/162px)** |
| 320×600 | — | closed | 320 | 747 | all three |

Five things in that table are not in the issue.

**The header clips at the supported minimum with the queue closed.**
At 800×600 — the size `backend/config/window.go` enforces and the only
size this app *promises* — "New Smart Playlist" loses 4px of its 162.
#24 reads as a queue-panel bug; the queue makes it dramatic, but the
header overflows on its own at the minimum window.

**900×600 is worse than 800×600, because the sidebar expands at 900.**
`AUTO_COLLAPSE_VIEWPORT` collapses the sidebar to icons *below* 900, so
at 899px the main panel is 843px and at 900px it is 700px. The worst
desktop case is therefore not the minimum window; it is the pixel
immediately above the collapse. Anything that tests "the minimum" and
stops has not tested the worst case, which is what
`layout-overflow.spec.ts` does today.

**At phone widths the queue is not a drawer, it is an amputation.**
`queue-panel`'s host is `flex-shrink: 0; width: 0`, going to
`width: var(--queue-width, 320px)` under `[open]` — it is *in the flow*
of `.content-area`, so it takes its width from the main panel rather
than covering it. At 390px that leaves 69px of the page; at 320px it
leaves **0px**, and the app is not degraded but gone. This is the
measurement #55 needs and did not have.

**Only Playlists overflows.** Sweeping all ten primary views at 900×600
and at 390×780, every other header reports `scrollWidth ==
clientWidth`, and Albums at 390px renders title, count and sort
legibly (checked on a screenshot, not just the number). #69 is
therefore one view's action set — three text buttons totalling 390px —
and not a systemic header failure, though the *rule* still belongs in
`page-header`.

**Both reasons in `MinWidth`'s comment are stale.** It says the floor is
800×600 because "below ~780 the header's subtitle wraps" and "below
~600 tall the eleven sidebar items no longer fit". The subtitle is
`display: none` below 900 (index.css), and the sidebar host is
`overflow-y: auto` — at 600×460 its `scrollHeight` is 434 against a
332px client, and Settings is reachable after scrolling. Neither
mechanism can happen any more. That does not mean the floor should
move; it means its stated reason no longer supports it, which is worse
than either answer.

*(Care needed: my first probe for the sidebar scroller searched
`shadowRoot.querySelectorAll('*')` and reported "items are
unreachable", because the scroller is the **host** and a host is not in
its own shadow root. The claim in CLAUDE.md is correct.)*

---

## Decision 1 — the supported size matrix

Three bands. Two of them already exist and are already argued; what is
new is that they are written down as a *promise*, and that the queue is
part of it.

| band | width | navigation | queue | promise |
|---|---|---|---|---|
| **Phone** | < 600 | `bottom-nav` + drawer | overlay, full width | reflows; nothing needs sideways scrolling; fits 320px |
| **Compact** | 600 – 899 | icon sidebar | overlay + scrim | nothing is clipped or unreachable at any width in the band |
| **Desktop** | ≥ 900 | labelled sidebar | inline where it fits (see decision 2), else overlay | as Compact |

And one promise across all three: **no action is ever unreachable.**
That is the sentence #69 asks for and it is the one the matrix exists
to make checkable.

**400% zoom** keeps the meaning it already has: WCAG 1.4.10 names 320px
as the reflow target, the phone band covers it, and
`layout-overflow.spec.ts` already asserts a 320px viewport needs no
sideways scrolling. What changes is that the *queue* must be part of
that assertion — it is not today, and with the queue open at 320px the
main panel is 0px wide, which no current test can see.

**The window minimum stays 800×600**, and its comment gets the real
reason. The old mechanisms are gone, but the floor is still where the
Compact band's chrome stops being comfortable, and lowering it would
mean promising the desktop layout at sizes where only the phone layout
works. The interesting consequence is decision 4.

---

## Decision 2 — the queue is an overlay when it cannot afford to be a column

**The rule.** The queue panel renders inline — in the flow, as today —
only while

```
viewport − sidebar − queueWidth ≥ 480
```

and as an overlay with a scrim otherwise.

**Why it cannot be a media query**, which is the load-bearing half:
the queue's width is *user state*. It is drag-resizable between 200 and
500px and persisted (`--queue-width`, `MIN_WIDTH`/`MAX_WIDTH` in
`queue-panel.ts`). A breakpoint at a fixed viewport width silently
assumes the default 320, and is wrong by 180px for a user who has
dragged the panel wide — in the direction that hurts, since a wider
queue is exactly when the content can least afford it. So the mode is
computed from the measured widths and published as an attribute, the
way `data-active-view` already is, and the CSS keys off that.

**Why 480, honestly.** There is no cliff to derive it from. The track
list rescales its columns continuously — at main widths from 900 down
to 544 its `--grid-cols` shrink from 213px to 124px with
`rowOverflow=0` throughout — and the album grid steps 3 columns to 2
somewhere between 564 and 644 without breaking. So this is a judgement,
anchored on two things: it keeps the *default* window (1100 wide, main
= 580) inline, because the inline queue is a desktop affordance people
choose and turning it into an overlay for the common case would be a
regression in feel; and it puts every case measured as broken —
900×600 at main=379, and every phone width — on the overlay side.
1024×768 lands at main=504 and stays inline.

**The scrim is the other half of the issue's complaint** ("make the
queue obviously an overlay *over* the content so it reads as something
to close"). An overlay queue gets a scrim, closes on scrim click and on
Escape, and returns focus to `#queue-button`.

**What must not change**: #55's Direction is explicit — one component,
two mount points, do not fork it. The overlay is a *presentation* of
the same `queue-panel`, so the roving tab stop, Alt+Arrow reorder, drag
reorder, selection semantics and the `virtualizer.requestUpdate()` on
selection and current-track change all come along untouched. This
decision deliberately stops short of #55's detail-view mount, but it is
the shape that makes it possible, and it unblocks it.

---

## Decision 3 — #69 is its own PR, and here is the finding that decides it

`page-header` **cannot collapse its own actions**, and that is not an
effort estimate but a fact about the API. Actions arrive through
`<slot name="actions">` as arbitrary light-DOM markup — Playlists slots
a `<div class="header-actions">` of three `<button>`s with click
handlers, drag handlers and a conditional class. A component cannot
move another component's light-DOM children into a dropdown and keep
their behaviour; there is nothing generic to render as a menu item.

So the overflow rule needs an *actions API* — hosts declaring
`{icon, label, handler, priority}` data that `page-header` can render
either as buttons or as menu items — which is a change to all three
hosts that slot actions, not a rule added in one place. That is a
different piece of work from this one, it is independently verifiable,
and the desktop half of #69's symptom is removed by decision 2 anyway
(the queue stops eating the header's width).

It therefore stays #69, gets the finding above recorded on it, and
follows immediately after this. What *this* plan owes it is the
promise in the matrix — no action unreachable at any supported size —
and the measurement that the only offender today is Playlists.

**And the promise is not kept yet, which is the honest version of a
claim this document made in its first draft.** "Decision 2 removes the
desktop half of #69's symptom" was too strong. Measured after phase 2,
at 900×600 on Playlists:

| | before | after |
|---|---|---|
| queue open | main 379px, **all three** actions clipped | main 700px, **one** clipped |
| queue closed | main 700px, one clipped | unchanged |

So the queue's *contribution* is gone — open and closed are now
identical, which is the whole of what this decision owed — and the
residual "New Smart Playlist: 114/162px" is the header overflowing on
its own, at a size the queue never touched. #69 is still a live defect
at a supported size, and the matrix's promise is what will close it.

---

## Decision 4 — a very small window becomes the phone layout, not the mini-player

#24 asks whether a very small window should switch to the mini-player
(#12) "or simply refuse to go there". Both options in the question are
worse than the one the codebase already has.

**#12 is a second window, not a mode.** Its findings say so: v3
supports multiple windows, `AlwaysOnTop` is a window *option*, and the
frontend would need an entry branch mounting only the mini-player root
for a second window loading the same bundle. Turning the main window
into a mini-player at some width conflates the two: it would throw away
the user's navigation state on a resize, and it puts the MPRIS question
(#12's own open question — media controls are process-level and must
not be per-window) on a code path that a drag can trigger by accident.

**And "refuses" is unnecessary, because the reflow already exists.**
The phone band is real, tested, and reached by width alone — a desktop
window narrowed below 600px already gets `bottom-nav` and the phone
shell. That is a better answer than refusing: it is strictly more
usable than a hard minimum, it costs nothing new, and it is the same
code Android runs, so it stays exercised.

So: the main window reflows and never becomes a mini-player; #12 stays
a separate always-on-top window and is not blocked by, or coupled to,
this decision. The window minimum stays 800×600 for the reason in
decision 1 — but the phone band is what happens below it, not a
refusal, which is why the minimum is a comfort floor rather than a
correctness one.

---

## Phases

1. **This document**, linked from #24, with the matrix reported on the
   issue and #55 told whether it is unblocked. *(no code)* — **done**
2. **The queue's overlay mode** — computed mode attribute, scrim,
   Escape and scrim-click close, focus return. The inline path is
   unchanged above the threshold. — **done**
3. **The window minimum's comment** — replace both stale reasons with
   the measured ones. No value change. — **done**
4. **Verification**, below. Including the specs that must change
   because they assert the old behaviour. — **done**

#69 follows as its own branch; #55 became unblocked at phase 2.

## What landed, measured

Main panel width with the queue open, before and after:

| viewport | before | after | mode |
|---|---|---|---|
| 1280×800 | 759 | 759 | inline |
| 1100×720 (default window) | 579 | 579 | inline |
| 1024×768 | 503 | 503 | inline |
| 900×600 | **379** | **700** | overlay |
| 800×600 | 423 | 744 | overlay |
| 390×780 | **69** | **390** | overlay |
| 320×600 | **0** | **320** | overlay |

The scrim is perceptible but subtle on a dark ramp, which is worth
knowing before someone "fixes" it: sampled from the screenshots at
900×600, the main panel's background goes 33,37,41 → 18,20,23 and a
row's text 242 → 133. It covers the **content area only** — not the
sidebar or the transport — on purpose: the queue is not modal, and
leaving the navigation live means the scrim reads as "this is over the
content" (which is what #24 asked for) without pretending the rest of
the app is unavailable.

## What #69 did with the promise, and one thing this plan got wrong

#69 landed on its own branch as decision 3 said it would, and the
matrix's *no action is ever unreachable at any supported size* is now
kept rather than promised. Measured on Playlists, actions clipped:

| viewport | before #24 | after #24 | after #69 |
|---|---|---|---|
| 900×600, queue open | all three | one (114/162px) | none |
| 900×600, queue closed | one | one | none |
| 800×600, queue closed | one (158/162px) | one | none |
| 390×780 | all three | all three | none |
| 320×600 | all three | all three | none |

The shape was the one decision 3 predicted — an actions API first, an
overflow rule second — and all three hosts that slot actions migrated.

**What this document got wrong is smaller and worth keeping.** Decision
1 says the header's minimum is a *comfort* floor and that only the
queue and the actions compete for the header's width. They are not the
only two: every child of that flex row was `flex-shrink: 0`, so
whatever came last lost, and the actions come last. At 320px the sort
control alone is 172px of the header — so with every action already
collapsed into the menu, the *menu button* was 76px off the right edge.
The promise was still broken with nothing left to collapse.

That is why #69 also had to decide what gives way: the title (which the
navigation also states) and, below 600px, the word "Sort:" (which the
direction arrow implies). Neither is an action, which is the rule the
matrix actually encodes — **an action is a capability and everything
else on that row is a label.**

## Verification, and what each tier cannot see

- `make ui-test` — the queue panel's mode logic is component-tier
  work and belongs there. It **cannot** see the shell: the threshold is
  computed from the sidebar and viewport, which do not exist in that
  tier.
- `make e2e` — `layout-overflow.spec.ts` gains the queue-open case at
  every band (it has none today, which is why main=0px at 320px has
  never failed anything) and **gains 900×600**, since the minimum is
  not the worst case. `queue-toggle-state.spec.ts` and
  `phone-shell.spec.ts` both touch the panel and must be re-read before
  editing.
- **Screenshots at every band, read by a human.** This is not optional
  here: `layout-overflow.spec.ts` asserts the *shell* needs no sideways
  scrolling and passes on a build whose album header clips its own
  buttons (measured this session at 390px; filed on #66). Clipping
  *inside* a component is invisible to it, and clipping is this issue.
- `make ui-visual` **cannot help at all** — the component tier renders
  the token fallbacks, because the theme only reaches `:root` in the
  real app.
- Accessible names via `page.getByRole(...)`, never a shadow-root
  query. A drawer with a scrim is exactly the shape that grows a
  nameless control, and this repo has shipped one three times.

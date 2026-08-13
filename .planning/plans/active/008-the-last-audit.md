# 008 — The last audit, and the one binding that outlived six phases

**Status:** active — Phases 1, 2 and 3 shipped. Phase 4 is all that
remains, and `a11y.md` is closed.
**Branch:** main
**Created:** 2026-08-12
**Follows:** 007-ui-reconciliation
**Source:** `.planning/audits/2026-08-11-ui/a11y.md` (34 findings), plus
one item inherited through all six phases of 007.

## Problem

Three of the four audits from 2026-08-11 are closed. `a11y.md` is not,
and it is **the least verified material in the repo** — 007's own notes
say so twice, and every pass that touched it found the audit wrong about
something.

Two things follow from that, and they are the shape of this plan.

**The coverage map lies, and it lies in the direction of more work than
exists.** 007's map assigns `a11y.7` (top-results cards are click-only
divs) to Phase 6; the code has carried `role="button" tabindex="0"` and
an Enter/Space handler since Phase 1. It is not alone. A grep pass over
all 34 findings against the current tree closes at least five the map
still shows open, and three of those (`17`, `19`, `27`) were closed by
phases that were not about them.

**And what survives is not evenly distributed.** Of the ~13 that look
open, three are genuine loss of function and the rest are Minor or
Polish. One of the three — `15` — is a hard WCAG conformance failure
that has been sitting under a "Major" heading being read as a nice-to-
have.

Cutting across both: **two items in this audit were never measured at
all.** Colour contrast is flagged borderline (`--yj-text-tertiary` on
`--yj-bg-surface` ≈ 4.1:1 against 11 px text) with "that needs a real
measurement" written next to it, and 007 parked it under "deliberately
not planned — worth measuring before planning". The mouse-only resize
handles (`28`) were dropped as "cosmetic preference, no function lost",
which is a judgement made by reading. Both are claims with no number
behind them, which by this repo's own standard is not a finding yet.

Separately, and not from any audit: **`tracklist.delete`**. Advertised
in Settings as configurable, bound to nothing, and carried through six
phases because it needs an operation that does not exist.

## The triage, as of 2026-08-12

Grep-level against `1e4a4e6`. **Every row is a hypothesis** — this is
where the audit's claims are, not where the code is. Nothing here is
fixed until it has been reproduced in the running app.

**Closed** (verified present in code): `1`, `2`, `3`, `4`, `5`, `6`,
`7`, `8`, `12`, `13`, `16`, `17`, `19`, `27`, `33`.

**Closed by argument rather than by code**, to confirm by reading:
`20` (the type-scale/`_itemSize` coupling is now documented in
`tokens.css.ts`, which is what the finding asked for), `31` (one of
`cover-grid`'s two `<img>`s has an `alt`; the finding named one).

**Open:**

| # | Level | What the grep says |
|---|---|---|
| `15` | Major | `now-playing` is not among the four files carrying `prefers-reduced-motion`. WCAG 2.2.2: moving content over 5 s with no pause mechanism. |
| `14` | Major | `combobox.ts` has no `aria-controls`, no `aria-activedescendant`, no option ids. |
| `11` | Major | No `altKey` handler in `queue-panel`. The *other* half of this finding — "no keyboard path to add a track to the queue or a playlist" — was closed by Phase 5's `MenuKeyboard`. |
| `21` | Minor | `body { height: 100vh; overflow: hidden }` unchanged. WCAG 1.4.10. **Shipped — and the stated mechanism was wrong; the failure is horizontal.** |
| `22` | Minor | `queue-panel` gained `aria-current`; `track-list` did not, and neither has a non-colour marker. **Shipped.** |
| `24` | Minor | No `title` on the truncating element in `track-info`, `playlist-view`, `queue-panel` or `track-list`. **Shipped.** |
| `25` | Minor | `<wa-progress-bar value=…>` with no label, verbatim as filed. **Shipped — it was named "Progress", not unnamed.** |
| — | ~~new~~ | ~~**Two unnamed native `<select>`s**, one of them `page-header`'s sort control on nine views.~~ **False.** The sort control is named "Sort: " by its wrapping `<label>` on all nine. The two unnamed roles were **one** `config-field` select and the **seek bar**. See Phase 3's list. |
| — | new | **24 of 93 controls on Settings unnamed** — every `config-field` select and toggle, all eighteen column checkboxes. **Shipped, 0 of 93.** |
| — | new | **Both `wa-slider`s have no accessible name**, which `a11y.md` files under *what is already correct*. **Shipped.** |
| `26` | Minor | Explore's search box is named by its placeholder only — which *is* an accname fallback, so an AX sweep reports it clean. `search-bar` was already fixed. **Shipped.** |
| `28` | ~~dropped~~ | **Measured, stays dropped.** One header *label* clips at 800×600; zero data cells do. |
| `29` | Polish | `<h3 class="subtitle">` for type size. **Shipped.** |
| `30` | Polish | No skip link anywhere. **Shipped.** |
| `32` | Polish | `title="Remove from queue"`, not identifying the track. **Shipped.** |
| `34` | Polish | The 10 px sort arrow, unchanged. **Shipped — half of it was closed by Phase 1's `aria-sort`.** |
| — | — | Colour contrast, never measured. **Measured and fixed in Phase 2.** |

## Ordering principle

By **what is lost**, then by containment.

Phase 1 first because it is the only phase where something a user needs
is unavailable: a marquee they cannot stop, a combobox that announces
nothing while they arrow through it, and a queue whose order cannot be
changed without a mouse.

Phase 2 second because both of its items are *questions*, and the
answers change what Phase 3 contains. If the contrast measurement comes
back below 4.5:1 it is a Phase 1 item wearing a Polish hat; if the
resize handles turn out to lose function rather than preference, `28`
stops being dropped.

Phase 3 is the tail, batched, because each item is a line and the cost
is in the verification rather than the change.

Phase 4 is `tracklist.delete`, last, because it is the only work in this
plan that can destroy a user's data and it should not share a pass with
anything.

---

## Phase 1 — The three that lose function

### `15` — the marquee cannot be stopped

`now-playing`'s title and artist scroll continuously while a track plays
when `scrollMode === 'always'` (persisted in localStorage), re-armed in
a loop by `onScrollCycleEnd`.

**Ships:** a `prefers-reduced-motion: reduce` guard that treats `always`
as `never` and disables the transition. The setting stays; the query
overrides it, which is the right precedence — a stated OS-level
accessibility preference outranks an app default the user may never have
touched.

**Reproduce first:** the guard is two lines and will look like it
worked whether or not it did. Check under an emulated
`prefers-reduced-motion` in the running app, and check that the *hover*
scroll (`scrollMode === 'hover'`, the default) is also covered — the
finding names `always` and the mechanism is shared.

**Watch for:** `now-playing`'s geometry work in `updated()` keys on the
two scroll flags, because `.will-scroll .scroll-content` carries
`padding-right: 2em` and changing the class changes the distance the
marquee travels. A guard that suppresses the animation without telling
the geometry key will leave a stale measurement behind.

### `14` — the combobox announces nothing

`role="combobox" aria-expanded aria-autocomplete="list"` on the input
and `role="listbox"`/`role="option"` below it, with no `id` on the
listbox, no `aria-controls`, no `aria-activedescendant`, and no `id` on
the options. `aria-selected` is used to mean "highlighted".

**Ships:** ids on the listbox and each option, `aria-controls`,
`aria-activedescendant` tracking the highlight, and `aria-selected`
meaning *chosen*.

**Reproduce first:** the a11y snapshot could not see a dialog's name and
may not see this either — 007 lost twenty minutes to exactly that.
CDP's `Accessibility.getFullAXTree` reports the computed value and where
it came from; use it, not the snapshot.

### `11` — queue order cannot be changed without a mouse

Reordering is `draggable="true"` with the drop index computed from
cursor Y. There is no keyboard equivalent and no `aria-` substitute.

**Ships:** Alt+ArrowUp / Alt+ArrowDown moves the focused queue item, on
the roving tab stop `utils/roving-rows.ts` already gives that list, with
a live region announcing the new position.

**Watch for:** Alt+Arrow is unmodified-adjacent but not unmodified, so
`focusedControlOwnsKey` does not apply — this is a panel binding in
`backend/shortcuts/config.go`, registered the way `tracklist.*` is, not
a document listener. And the queue panel renders no list at all when
closed, so anything asserting on it has to open it first.

**This is the risky one.** It is a new interaction model, it touches the
backend shortcut table, and the drag path it parallels computes its drop
index geometrically. It lands last in the phase, alone.

### Verification

`make ui-test` per rule, and then **run the existing tests** — that is
what has caught every bad version of a new rule in 007, including twice
in the last pass. `make ui-visual` for `15` (it changes what renders).
An e2e case for `11`, because the queue panel's animated width means a
click issued while it moves lands on whatever slid under the pointer.
A manual pass per landing, with a screenshot read.

### Phase 1 — what actually shipped

Three landings, one per finding, each reproduced in the running app
before anything was written and each watched failing on the pre-fix
build before being believed.

- **`15`.** `shouldScroll()` returns false under
  `prefers-reduced-motion: reduce`, live (a `matchMedia` listener, so
  changing the OS setting is honoured without a reload — verified).
  It covers `hover` as well as `always`.
- **`14`.** Ids on the listbox and every option, `aria-controls`,
  `aria-activedescendant`, and `aria-selected` meaning *chosen* rather
  than *highlighted*.
- **`11`.** Alt+ArrowUp/Down moves the focused queue row, with a live
  region saying where it went.

Pinned by `now-playing.test.ts` (+2), `combobox-aria.test.ts` (5),
`queue-reorder.test.ts` (7), `e2e/specs/reduced-motion.spec.ts` (2) and
`e2e/specs/queue-reorder.spec.ts` (4). `make ui-test` 558 → **572**;
`make e2e` 68 → **74**.

#### Where the plan was wrong — Phase 1

Nine things, and the first group is the triage being right for the
wrong reason.

- **The grep triage was accurate about *what* is open and wrong about
  *why* two of them are.** It is a good first pass and it cannot see
  mechanism. `15` is filed as "no reduced-motion guard", which is true;
  what makes a CSS-only guard wrong is that the cycle is a transition
  out, a `transitionend` and a transition back, so suppressing the
  animation strands the text off its own box with nothing to bring it
  back. That is only visible by reading the cycle.
- **A fix routes people into a state nobody has looked at.** With the
  marquee off, the fallback hard-clipped — "Overlong Trac|", no
  ellipsis — because `text-overflow` was on the outer span while the
  overflowing box is the inline-block child. It had never produced an
  ellipsis **in any mode**, including the default, and no test saw it.
  Found by reading the screenshot of the fix.
- **…and fixing that broke the measurement it depends on.** Giving the
  child its own `overflow: hidden` stops the *parent* overflowing, so
  `titleOverflows` went false and nothing would ever have scrolled
  again, for anyone. Caught by the new test's positive case, which is
  the whole reason it has one.
- **`a11y.6` scanned `<button>`, and says so.** "The only truly
  unnamed controls" is a claim about buttons. The AX tree has two
  unnamed `combobox` roles that are native `<select>`s — one of them
  the page header's sort control, on nine views. Not fixed here; it is
  a sweep of every form control, not a one-liner, and it belongs with
  `a11y.26` in Phase 3.
- **The reproduction of the *fix* was wrong twice, on the probe side
  both times.** Reading `activedescendant` out of the AX tree as
  `relatedNodes[0].text` returned `(none)` on a working build — the
  property is there, with `value.type: "idref"`. And `last('QueueChanged')`
  returned a stale payload, so a reorder that had happened looked like
  one that had not. Ask `GetState`, dump the whole property.
- **`11`'s stated scope is half done and the other half was already
  closed.** The finding is "drag-and-drop has no keyboard equivalent
  anywhere" and lists four sites; its stated *symptom* — "there is no
  keyboard path to add a track to the queue or a playlist" — was closed
  by Phase 5's `MenuKeyboard`. What was left is the queue's order, which
  is the one the menu cannot express. Album→queue drag and
  drop-on-nav-item remain, and are menu commands, not reorder.
- **The plan said a backend panel binding; it should not be one.** The
  queue panel already handles Enter and the roving arrows in its own
  *delegated* (not document) keydown, which is the sanctioned pattern.
  Alt+Arrow joins them: it cannot collide with the global Up/Down
  volume bindings (measured — 0 `VolumeChanged` events from a focused
  row), and it keeps a reordering key out of a user-editable table
  where it could be rebound onto something unmodified.
- **The index arithmetic is not symmetric, and the symmetric version
  fails silently.** `MoveQueueTracks` takes an index into the array
  *before* the move, so down-by-one must ask for `i + 2`; `i + 1` is
  where the row already is once its own removal is accounted for, and
  the backend's contiguous-block guard correctly returns without doing
  anything. Pinned in both tiers.
- **`focusedIndex` was only ever moved by an arrow key.** A row reached
  by a click or by Tab left it at 0, so `Enter` played the first track
  in the queue from any focused row. Pre-existing, invisible until a
  key moved something, fixed by reading the index off the row the event
  came from.

And one that is not about the audit: **the backtick-in-a-`css`-comment
trap cost a cycle again**, in the same session as reading the warning
about it twice. It is worth treating as a lint rule rather than a piece
of knowledge.

---

## Phase 2 — The two that were never measured

Neither is a fix. Both are a number, and the number decides whether
there is work.

**Colour contrast.** Measured against the rendered app, not against the
token file: the tokens are what a component *may* use, and what matters
is the pairs that actually appear. Sample the real computed colours at
the real sizes, report the ratios, and only then decide. The audit's own
number (≈ 4.1:1) is a hand calculation from two hex values and has the
status of a hypothesis.

**`a11y.28`, the resize handles.** Four of them: the sidebar, the queue
panel, the now-playing column, and the track-list column resizers.
"Cosmetic preference, no function lost" is the claim to test. The
track-list one is the suspicious member — a column narrowed to its floor
clips its label (007 phase 5 found "Durat…" at 800 px), so widening a
column may be the only way to read a value, which is function.

**Record both outcomes either way.** A measurement that closes a finding
is worth as much as one that opens it, and this plan's predecessor got
about a third of its value from findings that evaporated.

### Phase 2 — what the measurements said

One opened much wider than filed; one closed.

#### Contrast: worse than "borderline", and it was never one token

The audit's ≈ 4.1:1 was a hand calculation from two hex values, and
plan 007 filed it under "deliberately not planned — worth measuring
before planning". Measured against the rendered app across twelve views
and then across all three ramps: **110 failing nodes**, and
`textTertiary` failing AA in **nine of twelve** text/surface
combinations — 4.35:1 on dark's surface, 3.25:1 on its elevated,
2.31:1 on its overlay, and 2.55–3.32:1 on *every* surface of the light
ramp, which the audit never considered.

Fixed, and now **0 of 659 nodes** on dark and darker. Three mechanisms,
only the first of which is the finding:

- **The ramps.** `textTertiary` per ramp — `#a6a6a6` / `#949494` /
  `#5c636a` — sized to the lightest surface it actually sits on and
  keeping its hue.
- **The avatar generator**, which is not a colour but a *family* of
  them: `hsl(hue, 45%, 35%)` behind white initials failed for **35 of
  360 hues**, so which artists were unreadable depended on how their
  names hashed. 32% clears every hue.
- **Jobs' local `#ff6b6b`**, 4.15:1 on elevated.

Pinned by `theme-contrast.test.ts` and `avatar-color.test.ts` — unit
tests over the data, not sweeps of the DOM. `make ui-test` 572 →
**608**.

#### `a11y.28`: the drop was right, and now for a measured reason

"Cosmetic preference, no function lost" holds. At the window minimum
(800×600, which is where the shell was measured in 007) the track list
clips exactly one thing: the **Duration header label**. Zero data cells
clip, and the sort that label names has a redundant keyboard-reachable
dropdown. The queue panel at its default 321px clips nothing either.
A keyboard-only user cannot change a panel width; they do not lose
access to any value by not being able to. **Stays dropped.**

#### Two things the measurements found that are not in the audit

Both were bigger than what they were found under. Both are now fixed —
see the third landing below.

- **The semantic colours were fixed across ramps, and a fixed colour
  cannot serve a near-black and a near-white background.** `--yj-error`
  measured 3.42:1 on dark's surface and 2.55:1 on its elevated;
  `--yj-info` 3.10:1 and 2.31:1; success and warning failed on dark and
  light both. As *backgrounds* under white text, success (3.45) and
  warning (3.58) failed too.
- **The light ramp was not a usable theme.** With the greyscale fixed
  it still had **50 failing nodes**: the accent yellow under white text
  (1.43:1) and the autotag diff's pale greens and reds on white
  (1.36–2.59:1).

### Phase 2, third landing — the ramp reaches the semantic colours

**2237 nodes across three ramps and twelve views, 0 failing.**

The split is by the question a colour answers. A **fill** is "what
colour is a danger button" — red in every theme, unchanged. A **text**
colour is "what colour is the word *failed* on this background" — per
ramp, because one value cannot clear 4.5:1 against both a near-black and
a near-white surface. `bgOverlay` keeps the exception it already had on
the dark ramp.

And every fill now carries a **computed foreground**, because the accent
is a colour picker and no fixed answer survives one: white if white
clears 4.5:1, else black. That keeps a red danger button white and
flips a green or amber one to black. Accent-as-text goes through
`accentTextOn()`, which mixes along the hue until it clears the ramp's
surface and stops — returning the accent *unchanged* on both dark
ramps, so the dark themes are visually untouched by that half.

`make ui-test` 608 → **649**.

#### Where this pass was wrong

- **"The chrome stays dark while the body goes light" was mine, and it
  was false.** I read it off a screenshot; the DOM says `.top-bar` is
  `#e9ecef` and `.sidebar` `#f8f9fa` under the light ramp, and a
  re-taken screenshot agrees. The first one was captured before the
  theme had propagated. Third time in two passes that a screenshot read
  at the wrong moment produced a confident wrong claim — and the second
  time this pass that **the picture and the number disagreed and the
  number was mine**.
- **A `color:` regex matches `border-color:`.** Twice: once rewriting
  semantic text colours (3 borders) and once rewriting accent text (30
  more). A border is a fill, not text. Caught by grepping the result
  rather than by any test, because nothing renders differently enough
  to fail.
- **Two accent buttons took their foreground from `--yj-bg-base`**,
  which inverts with the ramp — white on yellow at 1.43:1. That is not
  a colour that was chosen badly; it is a token used for the wrong
  meaning, and it only shows up in the theme nobody looks at.

---

## Phase 3 — The tail

`21`, `22`, `24`, `25`, `29`, `30`, `32`, `34`, plus whatever Phase 2
promotes or closes. One landing, batched, each item confirmed against
the code before it is touched.

Two of them are not one-liners and should be treated as such:

- **`21`** (the shell is `100vh; overflow: hidden`) is a layout change
  to the app frame, and 007 phase 5 already measured the frame's real
  minimum at 800×600. Reflow at high zoom is the same question one
  variable over. It may want its own landing.
- **The unnamed `<select>`s** from Phase 1, with `26`.
- ~~**The semantic palette** and **the light ramp**~~ — both landed in
  Phase 2's third pass rather than waiting for this tail.
- **`22`** asks for a non-colour marker on the playing row, which is a
  visual change to the densest list in the app and moves a baseline.

### Phase 3 — what actually shipped

Six landings rather than one, ordered by risk, each reproduced in the
running app before anything was written and each watched failing on the
pre-fix build.

- **Web Awesome's two hidden roles.** `label` on both `wa-slider`s and
  on `wa-progress-bar` (`25`), plus `styles/wa-slider-label.css.ts`,
  which hides the slider's visible label by part and puts back the 8px
  margin `#slider` takes as soon as one exists.
- **Settings' form controls.** `for`/`id` in `config-field`,
  `aria-label` on the eighteen column toggles and thirty-six column
  arrows, and the action's name on every `shortcut-capture`.
  **24 unnamed of 93 → 0.**
- **`24` and `32`.** `title` on the four clipping surfaces, on the
  track-list *cell* rather than on what is inside it; and a queue row's
  remove button named after its own track.
- **`29`, `30`, `34`.** A skip link, `<h3>` → `<p>`, and the sort arrow
  at the type scale's floor. Plus the state that landed in: the hgroup
  measured 67px in a 64px bar and the subtitle's descenders were
  clipped once the h3's bottom margin went with it.
- **`22`.** A triangle in each row's own left padding, in both lists,
  and `aria-current` on the track-list row.
- **`21`.** `overflow-x: auto` — measured, and the finding's stated
  mechanism is not the one that exists.

And `26`'s remaining half, found last: Explore's search box.

Pinned by `wa-control-names.test.ts` (4), `settings-names.test.ts`
(11), `aria-tail.test.ts` (+5), `queue-reorder.test.ts` (+3),
`e2e/specs/control-names.spec.ts` (3), `e2e/specs/skip-link.spec.ts`
(4), `e2e/specs/layout-overflow.spec.ts` (+6) and
`e2e/specs/playback.spec.ts` (+1). `make ui-test` 649 → **672**;
`make e2e` 74 → **88**.

#### Where the plan was wrong — Phase 3

Ten things. The first four are the audit or the plan being wrong about
where a control's name lives.

- **The two unnamed `<select>`s from Phase 1 were one `<select>` and a
  slider, and neither was the page header's.** `page-header`'s sort
  control computes "Sort: " from its wrapping `<label>`, on every one
  of the nine views — checked with `getFullAXTree`, `from:
  relatedElement`. The other unnamed role was the **seek bar**, which
  `a11y.md` lists under *what is already correct*. Fourth probe error
  in two passes, and the same shape as the rest: read at the wrong
  level.
- **`aria-label` on a Web Awesome host does not name the control.**
  `wa-slider` puts `role="slider"` on a div in its own shadow root
  pointing `aria-labelledby` at an empty internal `<label>`, and that
  IDREF outranks the host's `aria-label`. Both sliders computed `""`.
  Exactly `wa-dialog`'s trap one component over, and the audit made
  exactly the same mistake in the opposite direction — it read the
  source and credited a name that was never computed.
  `volume-control` did not even have the `aria-label` it is credited
  with.
- **`a11y.25` is not "unnamed".** `wa-progress-bar` falls back to the
  localised word *progress*, so it announced "Progress, 45%" — named
  after the widget rather than after the work. Same fix, smaller claim.
- **Settings was full of unnamed controls and no finding says so.** 24
  of 93. `a11y.6` is not wrong: it says in its own line that it scanned
  every `<button>`. Third time this pass that a count in the audit was
  answering a narrower question than it reads as.
- **A placeholder is an accessible name.** Explore's search box
  therefore reported *clean* in an AX sweep of all eleven views, which
  is why `a11y.26` outlived four phases of people looking for exactly
  this. A sweep for empty names cannot see a weak one.
- **`a11y.21`'s mechanism does not exist.** "The 4em bars grow while
  the viewport does not, and anything that no longer fits is clipped
  with no scrollbar" — the middle row is `1fr` and absorbs them
  exactly. At 200% text on 800×600 the bars go 64 → 128 and the panel
  472 → 344, footer still on 600. The real failure is horizontal, which
  the finding does not mention: 784px of app in a 320px viewport, 464px
  of it unreachable.
- **…and the obvious probe for it passes on the broken build.**
  `overflow: hidden` still permits *programmatic* scrolling, so
  `scrollLeft = 9999` returns a healthy number on the build with the
  bug. It did. The spec is a wheel gesture now.
- **A fix's own test was pinning the bug.** `transport.test.ts`
  asserted `aria-label` on the `wa-slider` host and called it "carries
  an accessible name". Running the existing suite is what found it,
  for the third plan running.
- **`a11y.34` was half closed by Phase 1 and nobody had noticed.** "The
  sort direction is a 10px glyph *or nothing*" — it is announced now,
  via the `aria-sort` Phase 1 added. What was left is one declaration.
- **The queue's `aria-current` is dead in the common path.** A track
  started from the *track list* leaves the queue's `currentIndex` at
  −1, so the panel has no current row at all — which is why `22`'s
  marker looked broken the first time it was checked in the running
  app. Pre-existing, not fixed here, and the reason the e2e case plays
  from the queue.

And one that is about the harness rather than the audit: **a synthetic
`MouseEvent` does not reach a delegated handler the way a real gesture
does.** Three probes in a row reported the queue row as never becoming
active; `page.getByTestId('queue-row').dblclick()` made it active
immediately. Same family as everything above — the probe was wrong, not
the code.

---

## Phase 4 — `tracklist.delete`, and the operation behind it

### The decision

*(Decided 2026-08-12, before any code.)*

**"Remove from library" removes the database row and excludes the path
from future scans. It does not touch the file.**

The comment at `backend/shortcuts/config.go:36` states the fork exactly:
the row (which the next scan puts back unless the path is also excluded)
or the file (a delete-your-music button one keystroke from a focused
row). Three shapes were considered:

- **A — row + path exclusion.** Reversible, needs an exclusions table,
  so a schema file *and* a migration.
- **B — delete the file**, to the platform trash. Real user intent for
  an app with duplicate detection, genuinely destructive, and a new
  cross-platform dependency.
- **C — ship the operation as a menu command only**, leave `Delete`
  unbound.

**A, delivered as C**, and then the keystroke. Without the exclusion,
A is a button that undoes itself on the next scan, which is worse than
no button — so the exclusion is not an enhancement, it is what makes the
operation mean anything. `Delete` is bound only to *open the
confirmation*, never to perform the removal: that makes the keystroke a
request rather than an action, which is the only version defensible one
key from a focused row.

**B is not foreclosed and is not in this plan.** It deserves its own
argument.

### What ships

- An exclusions table, following the two-file schema discipline
  (`sql/schemas/` for the target shape, `sql/migrations/` for the
  existing install, column order matching, no index on a migrated
  column in the schema file).
- `RemoveFromLibrary(filePaths)` — rows deleted, paths excluded, one
  event carrying enough for the stores to patch rather than invalidate.
  It is a *write*, so it goes through `ExecContext`, not the read pool.
- A context-menu command behind `confirmAction()`, with impact copy
  naming the count and saying explicitly that files on disk are not
  touched.
- `tracklist.delete` re-advertised, bound to opening that dialog.
- The scanner honouring the exclusion list, which is the half that makes
  the rest true.

### Verification

A Go test that a removed path survives a rescan; an e2e case that the
row is gone, the dialog said so, and the file still exists. Both halves
matter — the second is the promise the copy makes.

---

## Deliberately not in this plan

- **Splitting `explore-view.ts`** (1 900 lines). The shelves are in it
  because a separate component would need its own art fetching and
  therefore its own cache, cap and probe, and `perf.M7` exists because
  that view never unmounts. The reasoning holds; the size is the price.
- **A `make perf` before/after for Phase 6's shelves.** Both seeds get
  their catalog from the artifact rather than from the seed tarball, so
  a before and an after are not the same corpus unless the e2e staging
  fixture is extended to bulk scale. Recorded as unmeasured in 007
  rather than implied to be free.
- **The unowned badge's `+` glyph.** It becomes correct the day the
  badge becomes a button. Changing it now touches four components'
  visual baselines for a call better made then.
- **The albums shelf leading with one act.** Needs dump-side data to
  express "these eight artists are one group and its solo members".
  A plan, not a fix.
- **WebKit2GTK-specific behaviour** (page zoom in the Wails shell, how
  Orca traverses the virtualizer's windowed DOM). Only answerable on the
  real shell, and CI is the only place WebKit runs.

## First step

Phase 1, and within it `15` — reproduced under an emulated
`prefers-reduced-motion` **before** the guard is written, because a
two-line CSS change looks identical whether or not it worked, and this
plan's predecessor met that failure in seven different costumes.

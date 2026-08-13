# 008 — The last audit, and the one binding that outlived six phases

**Status:** active
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
| `21` | Minor | `body { height: 100vh; overflow: hidden }` unchanged. WCAG 1.4.10. |
| `22` | Minor | `queue-panel` gained `aria-current`; `track-list` did not, and neither has a non-colour marker. |
| `24` | Minor | No `title` on the truncating element in `track-info`, `playlist-view`, `queue-panel` or `track-list`. |
| `25` | Minor | `<wa-progress-bar value=…>` with no label, verbatim as filed. |
| `28` | dropped | Four `@mousedown` `<div>`s with no `role="separator"`. Never measured. |
| `29` | Polish | `<h3 class="subtitle">` for type size. |
| `30` | Polish | No skip link anywhere. |
| `32` | Polish | `title="Remove from queue"`, not identifying the track. |
| `34` | Polish | The 10 px sort arrow, unchanged. |
| — | — | Colour contrast, never measured. |

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
- **`22`** asks for a non-colour marker on the playing row, which is a
  visual change to the densest list in the app and moves a baseline.

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

# 009 — The badge that cannot act, and the state it already had

**Status:** complete — all three phases shipped.
**Branch:** main
**Created:** 2026-08-13
**Follows:** 008-the-last-audit

## Problem

007 phase 6 turned `library-status-indicator` from a `<button>` that did
nothing into a `role="img"` badge, on the rule that **a control which
cannot act is worse than none**, and wrote down what would change the
answer: *"when the download-client integration lands, the right change
is to make it a `<button>` again with a handler."*

Two things about that are wrong, and both were found by reading the code
and then the running app rather than the note.

**The download client has largely already landed.** `backend/download`
is 16 541 lines: a durable request model with four entity types
(`artist` / `release-group` / `release` / `recording`, `request.go`), a
reconciler, a staging importer, six provider adapters, 20 bound methods,
`downloads-view`, the `download-picker` dialog, and a working **"Want
this"** toggle on `explore-album-details`. What has not landed is the
badge.

**And the badge is not merely inert — it is wrong.** `LibraryStatus`
declares, styles and labels a third state, `queued` ("… is queued for
download"). **Zero of the eight call sites ever produce it**
(`explore-view:1839,1877`, `explore-artist-details:2116,2228,2323`,
`explore-album-details:1641,2283`, `top-results-row:258` — every one is
a two-way ternary). So an album the user has *already requested*
displays a plus and says it is not in their library.

### Reproduced, 2026-08-13, before anything was written

Against `SEED=default` with the real 900 000-row catalog:
`AddRequest({mbid: e51c54ea…, entity: 'release-group'})` for *GOLDEN* by
Jung Kook, then Explore → search "GOLDEN":

```
status  not-in-library
icon    plus
aria    Album "GOLDEN" is not in your library
```

and on the album's **own detail page**, forty pixels apart in the same
screenshot: the button reads **"Wanted"** (filled) and the badge beside
the title reads **plus / "is not in your library"**. One component,
two surfaces, opposite answers. This is the header-badge-contradicting-
Settings failure again, and again only a PNG showed it.

The same PNG showed a second one, which is why it is in this plan:
**`bookmark-check` is not a bundled icon.** `window.__yjIconMisses`
reports exactly `["bookmark-check"]`, so the "Wanted" button renders the
fallback question-mark glyph. `e2e/specs/offline-icons.spec.ts` asserts
that array is empty and passes, because no spec has ever put the app in
a state where an album is requested — precisely the "twenty call sites
compute their icon name from state" case `names.txt` exists for.

## Ordering principle

By **what is a fact and what is a decision**.

Phase 1 is a bug: the badge contradicts the app's own state, and fixing
it needs no interaction design at all. It also produces the evidence
Phase 2 needs — once the badge can say "requested", whether it must also
*become* requestable is a question that can be looked at rather than
assumed.

Phase 2 is a decision made before any code, in the shape 008 phase 4
used, because one 20 px circle would otherwise mean three different
commitments: on an artist card a **discography subscription**
(`scope: 'future'`, `Expands()`, never satisfied), on an album a
release-group request, on a track row a recording request.

Phase 3 is whatever Phase 2 leaves. **"Album only" is a legitimate
outcome** and shrinks this plan rather than inventing work for it.

---

## Phase 1 — the badge tells the truth

**Ships:**

- `utils/library-status.ts` — one definition of the rule, since the
  reason all eight sites are two-state is that the rule is written at
  all eight. Owning something outranks wanting it, so `in-library` wins
  over `queued`.
- The eight call sites using it.
- `explore-view` gaining the `downloadStore` subscription both detail
  views already have (`init()` + `subscribe()`), through
  `view-lifecycle` — it is a **cached primary view**, so a raw
  `connectedCallback` subscription would live for the session.
- `bookmark-check` in `src/icons/names.txt`, and an e2e case that
  reaches the state that exposes it.

**The badge stays `role="img"`.** Telling the truth is not acting.

**Watch for:** `downloadStore.init()` fetches providers, descriptors,
downloads *and* requests, so this warms a singleton on a page that
previously did not construct it — "a store with no subscriber fetches
nothing" cuts the other way here, and the cost belongs in the note.

### Phase 1 — what actually shipped

Three landings. `make ui-test` 677 → **685**; `make e2e` 90 → **92**.

- **The rule, written once.** `utils/library-status.ts`, the eight call
  sites, and `explore-view`'s subscription.
- **The Pro icon.** `regular/bookmark` / `solid/bookmark`, vendored.
- **`e2e/specs/requested-badge.spec.ts`**, which is also the first spec
  that reaches the state the icon sweep needed.

Pinned by `library-status.test.ts` (8) and `requested-badge.spec.ts`
(2). Both e2e cases were watched failing on the pre-fix build by
neutering one line each — the badge reported `not-in-library` where
`queued` was expected, and the sweep returned `["bookmark-check"]`.

#### Where the plan was wrong — Phase 1

Six things, and the first is the plan's own framing.

- **"When the download client lands" had already half happened, and
  the note that said otherwise was written before it.** 007 phase 6
  left a condition ("make it a button *with* a handler") that reads as
  future work; `backend/download` was 16 541 lines and 20 bound methods
  at the time it was written. The badge was not waiting on the download
  client. It was waiting on somebody looking.
- **The bug was one layer below the one in the plan.** The plan says
  the badge cannot act. What the reproduction says is that it could not
  even *report* — three states declared, two produced, at eight sites
  none of which knew about the third. "A control that cannot act" and
  "a control that is wrong" are different faults and only the second
  one is a lie.
- **The second bug was in the screenshot of the first.** The "Wanted"
  button rendered a question mark, which is the missing-icon fallback:
  `bookmark-check` is a **Pro** name. It has been that way for as long
  as anything could be requested, and `offline-icons.spec.ts` — which
  exists to assert exactly this — passed throughout, because it never
  reached a state where an album was requested. Seventh regression in
  five plans that only a PNG has caught, and the first one caught in a
  PNG taken of a *different* bug.
- **A sibling component does not hear its host re-render.**
  `top-results-row` takes `results` as a property; `explore-view`
  re-rendering hands back the same array, so Lit stops at the property
  and the row keeps its old badges. Same shape as the virtualizer rule
  one level milder, and the fix is the same: subscribe where the state
  is read.
- **The cleanup ran on a page that could not run it.** `afterAll` used
  `callBinding`, which goes through `window.__yjEvents` — installed by
  the `app` fixture and not by `browser.newPage()`. It threw where
  nothing was watching, left the request behind, and failed the *next*
  run of the same spec with a stale `queued`. A spec that gives state
  back has to be checked by running it twice, which is what found this.
- **A freshly launched app cannot search its own catalog for ~40 s.**
  The core artifact merge (`core artifact: merge complete` in
  `.dev/app.log`) has to land first, and until it does Explore's search
  returns nothing — *including for rows staged directly into
  `explore_index` a moment earlier*, which is what makes it look like a
  staging bug. It cost a cycle here reading as a failure of the neuter
  it was run under.

---

## Phase 2 — what a badge click means, per entity

*(Decided 2026-08-13, before any code.)*

**A badge is a button where it is the only way to act, and what it
toggles is a request — never a download.**

Two of the three questions were answered by the code rather than by a
judgement, which is the point of asking them before writing anything.

**There is no artist badge, and there never was.** The worry that one
20 px circle would commit a user to a whole discography does not apply:
`top-results-row` renders `nothing` for an artist, and no other site
passes `entity-type="artist"` to this component at all. Artist
subscription already has a home — `explore-artist-details`'s
`renderFollowAction()`, a labelled button with the scope beside it,
which is where a commitment that never completes belongs.

**A track badge is honoured end to end.** `EntityRecording` is not a
placeholder in the request model: `Reconciler.tracklistFor` has a
deliberate branch for it ("A track request is its own tracklist") whose
comment explains that the single expected title is what lets filename
matching score a one-song download at all. So a track badge promises
something the backend can keep, and it is a button too.

That also disposes of the second observation. An hourglass on an album
over a row of plusses read as noise while a plus meant nothing; once a
plus on a track means *want just this one*, the mixed row is the
interface working. No special case, and none of the four surfaces needs
to know what contains what.

**The album detail header keeps its badge read-only.** "Want this" sits
directly below it saying the same thing in words. The rule is not "a
badge is decorative on detail pages" — it is that a call site **opts in
by supplying the MBID to act on**, so a redundancy is visible in the
template rather than hidden in the component.

**And it is a request, not an acquisition.** The old copy said "Add …
to library", which 007 called the button's promise written into the
copy — and it would still be a lie, because clicking adds a row to the
request list and nothing to the library. The name is the action, in the
words the rest of the app already uses: **"Want …"**, and **"Cancel the
request for …"** when it is already wanted. No confirmation: the action
is one click to undo, which is the whole test for whether a dialog is
owed.

---

## Phase 3 — the button

Ships what Phase 2 decided: `request-mbid` as the opt-in, a `<button>`
where a call site passes one and the entity is not already owned, and
`toggleRequest()` beside `libraryStatusFor()` because
`explore-album-details`'s "Want this" asks the same question and two
implementations of *what wanting something means* is what Phase 1 was
about.

### Phase 3 — what actually shipped

Seven of the eight call sites opt in; the album header does not.
`make ui-test` 685 → **695**; `make e2e` 92 → **93**.

Verified in the running app with a **real mouse gesture and a real
keyboard path**, not a synthetic event: click the badge → the request
is filed, the badge becomes an hourglass, the album page does not
open. Tab → the badge takes focus with its own ring inside the card's;
Enter → same, and the card's own Enter handler does not fire.

Pinned by `library-status.test.ts` (+10, watched failing on the
pre-fix build — 8 of 18) and `requested-badge.spec.ts` (+1).

#### Where the plan was wrong — Phase 3

Five things, and the first two are the plan asking questions the code
had already answered.

- **Two thirds of the Phase 2 decision was not a decision.** "An artist
  badge would mean a discography subscription" describes a badge that
  does not exist — `top-results-row` renders `nothing` for an artist
  and no other site passes `entity-type="artist"` at all. And "should a
  track inside a requested album show something different" evaporated
  the moment a plus on a track meant *want just this one*. A decision
  phase is worth having; two of its three items were answered by
  reading rather than by choosing, which is the cheaper half of it
  working.
- **`EntityRecording` is load-bearing and reads like a placeholder.**
  It would have been easy to rule tracks out as unsupported; the
  reconciler has an explicit branch for them whose comment explains
  that a one-entry expected tracklist is what lets filename matching
  score a single-track download at all. Ruling it out would have been a
  feature removed by assumption.
- **A test that passes on the neutered build is not a test.** "Keeps
  its click off the card it sits on" asserted that nothing bubbled —
  which is free when there is no button to click, since `?.click()` on
  null is a silent no-op. It passed on the neutered build. It asserts
  the click *did the thing it was swallowed for* as well now, and fails
  there like the other seven.
- **A measured coordinate is stale before it is used.** The e2e gesture
  read a bounding box the moment the search settled; cover art is still
  arriving then, and a card that grows moves the badge, so the click
  landed on the card and opened the album — reported as a failure to
  file a request, which is a different bug entirely. A locator
  re-resolves and waits for the element to stop moving.
- **A fix moves its own assertions.** Phase 1's spec asserted the
  badge's name was "… is queued for download"; a control is named after
  what activating it does, so it is "Cancel the request for …" now. The
  spec was right when it was written and wrong two commits later, which
  is the ordinary cost of naming a thing after its state.

---

## Deliberately not in this plan

- **Deleting the file from disk** (008 phase 4's explicit sequel). Not
  refused — mis-ordered. 008's own notes record that the *reversible*
  option shipped with **nothing implementing its reversibility**:
  `excluded_paths` has no management surface, and "a full rescan clears
  it" is the escape hatch. Shipping an irreversible delete beside a
  reversible one that cannot yet be undone is backwards, and the
  platform trash is a new cross-platform dependency besides.
- **`a11y.20`, deriving `_itemSize` from a measured row.** Real and
  confirmed in code — `.track-row` is `height: 33px; contain: strict`
  with a `rem` font size, so text scales and the box does not, across
  four lists (33 / 49 / 45 / 45 px). It waits because its only honest
  verification does not exist yet: both surviving comments
  (`track-list.ts:349`, `queue-panel.ts:179`) say a wrong `_itemSize`
  desynchronises the **native scrollbar at 20k+ rows**, and `make perf`
  has no scroll-fidelity row. That measurement is its own first phase
  and belongs to a plan that is about it.

## First step

Phase 1, and within it the helper rather than the call sites — the
reproduction above is already the failing case, and the point of the
helper is that there is one place for the next state to be added.

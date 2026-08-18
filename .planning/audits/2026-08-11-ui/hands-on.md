# UI/UX audit — YellowJacket

Date: 2026-08-11. Method: the app driven by hand headlessly
(`make dev-headless SEED=default` + `playwright-cli`, then
`make dev-headless-fresh` for first run), plus three read-only static
reviews. Nothing was changed.

- `hands-on.md` (this file) — the empirically confirmed findings, i.e.
  things observed happening in the running app, with the reproduction.
- `a11y.md` — accessibility and interaction model.
- `perf.md` — rendering performance, memory, state correctness.
- `errors.md` — error handling, empty/loading states, destructive actions.

Findings below are numbered `H-n` (hands-on) and cross-reference the
static reports where they overlap. The reconciliation plan built from
all four files is `.planning/plans/completed/007-ui-reconciliation.md`.

---

## Critical — confirmed by reproduction

### H-1. A keypress on any page silently mutates the Autotag queue

Every view the user visits stays mounted forever (`index.ts`, class
`view-hidden`), so `disconnectedCallback` never runs and
`autotag-view`'s `document` keydown listener (`autotag-view.ts:1188`,
handler at `:1706`) stays live for the rest of the session.

Reproduced: visited Autotag (Pending 11), navigated to Settings,
dispatched `keydown` `s` twice → **Pending 9**. Two albums skipped from
a page that was not on screen and gave no feedback. `a` on the same
listener is Apply, which rewrites tags on disk.

### H-2. `s` and the arrow keys fire two handlers at once

`autotag-view`'s listener and `keyboard-shortcut-service` are both on
`document` and neither defers. Reproduced on the Autotag page: pressing
`s` emitted `QueueModeChanged` (shuffle toggled) *and* skipped the
album. `ArrowUp`/`ArrowDown` navigate the folder list *and* change the
volume by 5, so walking the autotag list with the keyboard ramps volume
to 0 or 100.

### H-3. The progress bar is a local timer that lies, and a keyboard seek desyncs it by ~30 s

`seek-bar.ts:110-116` increments `seekValue` by 1 every 1000 ms and only
resyncs when `trackChangeId` changes. Nothing reconciles it against
`Player.CurrentPositionSeconds`.

Reproduced twice:

| | UI | backend |
|---|---|---|
| steady playback, +10 s | 00:47 → 00:57 | 50 → 60 (constant 3 s lie) |
| after 4× `ArrowRight` (seek +5 s) | 00:08 → **00:10** | 11 → **40** |

The keyboard seek path (`keyboard-shortcut-service.ts:207-214`) calls
`Player.Seek` and never tells the seek bar, so the bar does not move at
all — the shortcut looks broken, and the displayed time is wrong for
the rest of the track.

### H-4. Every icon in the app is fetched from fontawesome.com at runtime

Confirmed from `performance.getEntriesByType('resource')`:
`https://ka-f.fontawesome.com/releases/v7.1.0/svgs/solid/house.svg`
and 35 more. `setBasePath('/dist/webawesome')` in `index.ts` does not
affect the icon resolver, and no `registerIconLibrary` call exists.
A desktop music player offline, on a captive portal, or behind a
firewall renders **no icons at all**. See `perf.md` M9.

### H-5. The whole app is unusable without a mouse

Tabbing through the entire app yields **14 stops**, all of them chrome
(library filter, search, one unlabelled track-list button, two queue
buttons, five transport buttons, volume, queue toggle, seek). The
sidebar nav (`app-sidebar.ts:202`, bare `<li @click>`), every track
row, every album/artist/genre card and every context menu are
unreachable. `Enter` on a selected track does nothing — reproduced.

The cause of the last part is that `data-shortcut-scope` is **never set
anywhere in the codebase**, so `resolveScope` can only return
`text-input` or `global`, and the two panel-scoped bindings
(`tracklist.play` = Enter, `tracklist.delete` = Delete) are dead
shortcuts that the Settings page still advertises as configurable.

Related: the closed queue panel is `width: 0` but not `inert` and not
`visibility: hidden` (`queue-panel.ts:214`), so its Clear/Add buttons
still take tab stops and are read by screen readers — reproduced, they
appear in the tab order at x=1440.

---

## Major — confirmed by reproduction

### H-6. Global single-key shortcuts hijack keys from focused controls

Defaults (`backend/shortcuts/shortcuts.go:16`) bind unmodified
`Space N P S R M / Q ↑ ↓ ← →` at global scope, and the service calls
`preventDefault()` on a match. Only text inputs are exempt. So a
focused `<button>` cannot be activated with Space, the native
`<select>` library filter cannot be arrowed through, the volume and
seek sliders fight the global handler for arrow keys, and Space/arrow
page scrolling is dead everywhere.

`ArrowUp` also emits `MuteChanged` alongside `VolumeChanged` even when
nothing is muted — reproduced.

### H-7. The last column of the track list is always clipped by exactly 40 px

`computeDefaultWidths` (`track-list.ts:409`) distributes
`this.clientWidth` across the columns but never subtracts the 24 px
favourite column or the 2×8 px row padding that
`colBoundaryPositions` (`:378`) knows about. Measured: every
`.track-row` and the `.header-row` report `scrollWidth 1280` against
`clientWidth 1240`. Duration renders as "Durat…" on a fresh profile at
1440×900, and disappears entirely below ~1000 px.

### H-8. The app never lands on Home

`app-sidebar.ts:124` defaults `activeView = 'tracks'`. The curated Home
page — the one with the "somewhere to start listening" shelves — is
listed first in the nav and is never what the user sees on launch.

### H-9. On the Home page, an album with no cover art renders as nothing

The Home shelf card's missing-art placeholder has no background, so the
tile is invisible against the page and the shelf reads as having holes
in it. The Albums grid and the Artists grid both do this correctly
(letter-on-a-tile), so this is one card renderer disagreeing with the
other two.

Also on Home: with a small library all three shelves ("Fresh in your
library", "Never played", "Take a chance") show the **same seven
albums** in different orders, so the page reads as repeating itself.
A shelf whose contents largely duplicate the shelf above it would be
better suppressed, the way an empty one already is.

### H-10. The header search is view-scoped but looks global

Typing `tide` on the Playlists page produced **"No playlists match your
search"** while three tracks named *Tideline* sat in the library. The
box is in the global header, is placeheld "Search…", and persists its
term across navigation, so it reads as a library-wide search and is
not one. It also vanishes entirely on Home and Explore (Explore has its
own second search box), and its appearing/disappearing shifts the whole
header layout.

### H-11. The layout has no responsive behaviour and the enforced minimum window is too small

`MinWidth/MinHeight` are 512×384 (`backend/config/window.go:15`). At
900×600 the Duration column is off-screen; at 700×480 the sidebar
overflows behind the player bar with no scroll, so **Settings and Jobs
become unreachable**, and the app title wraps into the nav. The sidebar
has a `.collapsed` icon mode but nothing triggers it automatically.

### H-12. First run shows "Loading tracks…" behind an inert copy of the whole app

On an empty `YJ_HOME` the wizard is a modal over a fully rendered app —
sidebar, transport, search, library filter all visible and all inert —
with a permanent "Loading tracks…" in the content area (the track list
cannot tell empty from loading, `track-list.ts:1901`). Meanwhile the
"Building search index" job is already downloading a 1.1 M-row catalog
before the user has chosen a folder or consented to it.

`Get Started` is correctly disabled until a folder is chosen, but it is
the filled accent button and its disabled state is barely visible.

### H-13. The album detail page has no way to play the album

The primary action is missing: no Play, no Shuffle, no Add to queue on
the album header. Nor is there any legend for the green ✓ badges shown
against the album title and every track.

### H-14. `IndexStatusChanged` is emitted every 3 seconds forever

`searchindex.go:276` starts an unconditional 3 s ticker in
`SetContext` and never stops it. The payload is byte-identical once the
index is ready (`building:false, ready:true`) and it keeps firing for
the life of the process. Each tick re-renders the 2 149-line
`config-page` (which never unmounts) and writes a `console.log`
(`config-page.ts:1019`) — the browser console filled with ~200
identical lines during a 20-minute session. See `perf.md` M6.

---

## Minor — confirmed by observation

- **H-15.** Three identical `Tideline / Aurora Fields / 00:06` rows are
  indistinguishable in the track list; the default columns carry no
  album, format or path, so the app's own duplicate fixtures cannot be
  told apart by eye in a library manager that has a duplicate-detection
  feature.
- **H-16.** The remaining-time label is a countdown with no minus sign,
  no label and no toggle to total duration — `01:21` next to a track
  the list says is `01:30`.
- **H-17.** The now-playing artist is truncated to a fixed ~120 px
  ("The Orchestra Of") while ~400 px of empty space sits between it and
  the transport controls.
- **H-18.** When a queue finishes, the now-playing bar empties
  completely, losing the context of what just played, while the queue
  panel still lists the finished track.
- **H-19.** Page headings are inconsistent: Playlists, Downloads, Jobs,
  Settings and Home have a title (and Playlists/Downloads/Jobs have
  header actions); Artists, Genres, Albums and Tracks have none, and
  none of them shows a count. Sort controls exist on Albums and Tracks
  but not on Artists or Genres.
- **H-20.** The sidebar's hover colour (`#343a40`) and its active
  colour (`#495057`) are close enough that a hovered item reads as a
  second selected item.
- **H-21.** The track context menu has no Escape handler, no keyboard
  navigation and no focus movement (`context-menu-controller.ts` binds
  only click/contextmenu/mousedown), and is missing the conventional
  entries: Go to album, Go to artist, Show in file manager, Edit tags,
  Remove from library.
- **H-22.** In Settings, "Libraries" — the section that matters most —
  is last and below the fold, while "Search Index" is first and
  expanded by default. There is no Playback/Audio section at all (no
  output device, gapless, crossfade or replay gain).
- **H-23.** Explore is an empty page with a search box over a 1.1 M-row
  catalog: no browse, no popular-artists entry point, nothing to do
  without typing.
- **H-24.** Long body copy (Downloads' intro, Jobs' descriptions) runs
  the full ~1200 px content width with no measure cap.

---

## Where the bar is already high

Worth naming, because the findings above are the exceptions:

- **`downloads-view`** — the best empty state in the app: it says what
  the feature is, why nothing is happening, and exactly what to do next.
- **`autotag-view`** — genuinely dense and legible: per-field match
  breakdown, your-folder-vs-candidate side by side, confidence stated
  rather than hidden.
- **`jobs-view`** — running / libraries / maintenance / recently
  finished, with the destructive action visually separated and honestly
  described.
- **`track-list`** — a properly built virtualized list (memoized
  filter/sort, delegated handlers, `_itemSize` hint, inline SVG for the
  per-row icon). Its problems are at the edges, not in the core.
- **`player-controls`** — every button labelled, `aria-pressed` on the
  toggles, repeat's three-state mode spelled into the label.

---

## Suggested order

1. **H-1 / H-2** — a hidden page mutating files on a keystroke is the
   only finding here that loses user data. Fix the view lifecycle
   (deactivate hidden views) and make the two keydown listeners agree.
2. **H-3** — drive the seek bar from the backend position; the core
   surface of a music player currently lies.
3. **H-4** — bundle the icons; the app is not usable offline.
4. `errors.md` **C1** — a track that fails to play is a silent no-op,
   which is the same class of problem as H-3 on the same surface.
5. **H-5 / H-6** — keyboard access, and stop the global shortcuts
   stealing keys from focused controls.
6. **H-7 / H-11** — the layout arithmetic and a real minimum size.
7. Then the consistency pass: **H-8, H-9, H-10, H-13, H-19**.

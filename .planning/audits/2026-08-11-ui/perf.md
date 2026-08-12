# Frontend performance / memory / state-correctness audit

**Scope:** `frontend/src/store/**`, `frontend/src/components/**`, `frontend/src/events.ts`,
`frontend/vite.config.mts`, `frontend/package.json`, `frontend/index.ts`, `frontend/index.html`.
Read-only. Nothing in the repo was modified. (Two throwaway production builds were emitted to
`/tmp/yjbuild*` to measure bundle composition; `frontend/dist/` was not touched.)

**Excluded as already-known** (traced for consequences, not re-reported): views never unmount,
`autotag-view`'s document keydown, `IndexStatusChanged` every 3 s, seek-bar drift.

---

## Critical

### C1 — Finishing a track re-downloads the entire library

`backend/queue/playhistory.go:63` → `frontend/src/store/library-store.ts:85` → `:445`

`recordPlay()` emits `TrackMetadataChanged` on **every naturally finished track**
(`backend/queue/handlers.go:24,34,45,52`). `LibraryStore` treats that event exactly like a retag:
`invalidate()` nulls tracks/albums/artists/genres and immediately `eagerFetch()`es all four
(`library-store.ts:445-476`). On a 50 k-track library that is `GetAllTracks` +
`GetAllAlbums` + `GetAllArtists` + `GetAllGenresWithCounts` — roughly 25 MB of JSON across the
Wails IPC, parsed on the main thread — **once per song**, forever, whether or not the user is
looking at a list.

The invalidation itself is correct and deliberate (`frontend/test/stores/library-store.test.ts:94-110`
asserts it); the defect is that the backend reuses one event for "tags were rewritten" and
"play_count went up by one".

*Symptom:* a multi-second main-thread stall between every two tracks on a large library, plus
constant SQLite churn.
*Fix:* emit a distinct `TrackPlayCountChanged` from `recordPlay` and have `LibraryStore` patch the
one track in place instead of invalidating.

### C2 — …and silently wipes the user's selection while it does

`frontend/src/components/track-list/track-list.ts:1198-1211` → `:1242-1246`

`updated()` notices `libraryCtrl.cachedTracks` has a new identity and calls `loadTracks()`, which
does `this.selection.clear()` (`:1246`). Combined with C1, **every track change clears whatever the
user had selected in the track list.** Selecting 40 tracks to drag into a playlist while music plays
is not possible.

*Fix:* re-key the selection against the new array (`selection` is keyed by `FilePath`, which
survives a refetch) instead of clearing it.

### C3 — Library-filter / rescan race caches the wrong library's data

`frontend/src/store/library-store.ts:133-155` (and the identical `getAlbums`/`getArtists`/`getGenres`)

`getTracks()` guards on `tracksLoading`, but `invalidate()` (`:445`) clears `tracks` **without**
clearing `tracksLoading`. Sequence:

1. `getTracks()` starts for library A → `tracksLoading = true`.
2. User picks library B → `setSelectedLibrary` (`:339`) → `invalidate()` → `tracks = null`,
   `eagerFetch()` → `getTracks()` sees `tracks === null && tracksLoading === true` → returns
   `waitForTracks()`.
3. Library A's response lands, is stored as `this.tracks`, `changeGen++`.
4. `waitForTracks()` resolves with library A's tracks — under library B's filter.

The same window exists for `LibraryScanComplete` arriving while a fetch is in flight, in which case
the pre-scan snapshot is cached as if it were post-scan and the newly scanned tracks never appear.

*Fix:* stamp each fetch with a request id (or the `selectedLibraryIdValue` + `changeGen` it started
under) and discard the result if it no longer matches.

### C4 — `waitFor*` never resolves on a failed fetch, and leaks a subscriber forever

`frontend/src/store/library-store.ts:494-547` (4 copies), `frontend/src/store/playlist-store.ts:143-157`

`waitForTracks()` resolves only when `!tracksLoading && tracks !== null`. If the underlying binding
rejects, `finally` sets `tracksLoading = false` but `tracks` stays `null`, so the promise **never
settles** and its `subscribe()` callback is never removed from `LibraryStore.subscribers`. Every
component or `explore-link` lookup awaiting that promise hangs, and each hung wait permanently adds
a closure to the notify set that runs on every subsequent store change. `eagerFetch()`'s
`void this.getTracks()` (`:474-477`) also swallows the rejection into an unhandled promise rejection.

*Fix:* have the fetch record an error state and reject/resolve all waiters in `finally`.

### C5 — Adding one track to one playlist re-downloads every track of every playlist

`frontend/src/store/playlist-store.ts:31-33` → `:124-129` → `:60`

`PlaylistTracksChanged` (emitted from 8 backend sites including `backend/playlist/favorites.go:200,231`)
calls `invalidate()` → `GetAllPlaylistsWithTracks()`, which the backend implements as
`GetAllPlaylists` + `GetAllPlaylistTracksWithMetadata` — **all rows of all playlists with full track
metadata** (`backend/playlist/playlist.go:206-234`).

Toggling a single heart in the track list therefore refetches every playlist in the app. The store
does this unconditionally (`void this.getPlaylists()` inside `invalidate()`), so it fires even when
`playlist-view` — the only subscriber — has never been opened.

*Fix:* the event already carries the playlist id; refetch that one playlist, and only when there is
a subscriber.

---

## Major

### M1 — One keystroke in the search box re-ranks every list in the app

`frontend/src/store/search-store.ts:55-57`, `frontend/src/store/controllers/search-controller.ts:29-32`

`SearchStore.notify()` is an unbatched broadcast to every subscriber, and `SearchController` maps it
straight to `host.requestUpdate()`. Eight components hold a `SearchController`
(`track-list`, `cover-grid`, `artists-view`, `genres-view`, `playlist-view`, `playlist-details`,
`smart-playlist-details`, `search-bar`) and — because views stay mounted — **all of the mounted ones
recompute on every keystroke**, not just the visible one:

- `track-list` → `rankTracks()` over 50 k tracks (`track-list.ts:271-289`)
- `cover-grid` → filter + `[...albums].sort()` over 5 k albums (`cover-grid.ts:215-248`)
- `artists-view`, `genres-view` → their own filter passes

Measured on Node/V8 (WebKit2GTK will be slower): `rankTracks`-equivalent work over 50 k tracks is
**~18 ms**, so a single keystroke costs 50–100 ms of main-thread work across the mounted set even
though four of the five results are invisible.

*Fix:* gate the notify on `searchStore.isSearchableView()` matching the subscriber's own view (the
predicate already exists at `search-store.ts:41-43`), or have `SearchController` skip
`requestUpdate()` when its host carries `view-hidden`.

### M2 — `rankTracks` allocates a `Set` and a closure per track, per keystroke

`frontend/src/components/track-list/search-ranking.ts:98-135`

`scoreTrack()` builds `new Set<string>()` plus a `check` closure for **every** track, then calls
`col.accessor(track).toLowerCase()` (a fresh string allocation) per field. At 50 k tracks × 3 core
fields that is 50 k Sets, 50 k closures and 150 k throwaway strings per keystroke. Benchmarked
against a flat three-field comparison: **18.1 ms vs 5.8 ms** — a 3× tax purely from the dedup
machinery, for a `seen` set that only ever contains 3–6 fixed ids.

*Fix:* hoist the deduped column list out of the per-track loop (compute it once in `rankTracks`) and
drop the closure.

### M3 — Full-size original cover art rendered as a 24 px thumbnail in the track list

`frontend/src/components/track-list/columns.ts:53-63`

The `albumArt` column renders `track.CoverArtPath` — the **original embedded artwork**, commonly
1500×1500 and several hundred KB — scaled to `width:24px;height:24px` by CSS. `CoverArtSmall`
(100 px, quality 75) and `CoverArtMedium` (200 px) already exist on the same model
(`wailsjs/go/models.ts:1583-1586`, generated by `backend/library/coverart.go:41-45`) and are used
correctly everywhere else. There is also no `loading="lazy"` and no `decoding="async"`, so every row
the virtualizer scrolls into view decodes a full-resolution JPEG synchronously on the main thread.

*Symptom:* enabling the Art column makes track-list scrolling stutter and inflates memory by the
decoded bitmap of every album scrolled past.
*Fix:* `track.CoverArtSmall || track.CoverArtPath`, plus `loading="lazy" decoding="async"`.

### M4 — Artist grid does a full linear scan of the album cache per card, per frame

`frontend/src/components/artists-view/artists-view.ts:988-1029`, called from `:1044` /
`.renderItem` at `:1298`

When an artist has no `ImageSmall/Medium/Large` — the common case for a locally-tagged library —
`renderArtistAvatar()` falls back to scanning **all of `libraryStore.cachedAlbums`** with
`a.ArtistName.toLowerCase() === name` until it finds a match, allocating two lowercased strings per
comparison. This runs inside the virtualizer's `renderItem`, i.e. for every visible card on every
render pass. At 5 000 albums × ~50 visible cards that is 250 000 comparisons and 500 000 string
allocations per scroll frame.

*Fix:* build a `Map<lowercasedArtistName, coverUrls>` once when `cachedAlbums` identity changes, and
look up in O(1).

### M5 — Playlist and smart-playlist track lists are not virtualized

`frontend/src/components/playlist-details/playlist-details.ts:1265-1396`,
`frontend/src/components/smart-playlist-details/smart-playlist-details.ts:1176-1250`

Both render **every** track with a plain `.map()` — no `lit-virtualizer`, no `repeat()` key. For a
2 000-track playlist that is 2 000 rows × 8 elements in the DOM, and:

- `getVisibleTracks()` (`playlist-details.ts:750-780`) allocates a fresh `{track, trackIndex}`
  wrapper object for every track on **every** render, so the array identity always changes;
- five event bindings per row (`@click`, `@dblclick`, `@contextmenu`, `@dragstart`, `@dragend`,
  `:1305-1330`) are new arrow functions each render, so lit removes and re-adds 10 000 listeners
  per pass;
- both components hold a `PlayerController` (`playlist-details.ts` imports it), whose subscription
  is unfiltered — so **every** `PlaybackStateChanged` / `TrackChanged` / `VolumeChanged` /
  `MuteChanged` triggers that whole pass;
- the row `<img>` (`:1386`, `smart-playlist-details.ts:1245`) has no `loading="lazy"`, so opening a
  2 000-track playlist fires 2 000 simultaneous cover-art requests at the Go asset handler.

Both files are ~30 kB of the bundle each and duplicate the same list; `track-list` already solves
all of this (delegated handlers via `data-index`, stable `renderItem`, memoized caches) and is
already reused by `genre-details.ts:276-278` via `.externalTracks`.

*Fix:* render these with `<track-list .externalTracks=…>` the way `genre-details` does, or at minimum
add `lit-virtualizer` + delegated handlers.

### M6 — Visiting Settings costs a full re-render (and a console entry) every 3 seconds, forever

`frontend/src/components/config-page/config-page.ts:1016-1022`, `@state` at `:186`

The `IndexStatusChanged` handler assigns a freshly deserialized object to a `@state` field, so the
identity always differs and Lit re-renders the entire 2 149-line `config-page` template every 3 s —
for the rest of the session, since `config-page` is a cached primary view that never unmounts
(`index.ts:71`) and its `disconnectedCallback` cleanup (`:1024-1036`, including
`this.cancelIndexStatus?.()`) never runs.

The handler also does `console.log('IndexStatusChanged event received', status)` on every tick. With
devtools open that retains ~1 200 status objects per hour as a genuine, unbounded leak.

*Fix:* drop the `console.log`; compare the incoming status field-wise and only assign on change.

### M7 — `explore-view` retains base64 image data forever

`frontend/src/components/explore-view/explore-view.ts:99-100`, `:987`, `:1003-1019`, `:936-944`

`thumbnailCache` stores the **data URL** returned by `GetThumbnails` —
`"data:image/jpeg;base64," + base64(front-250 JPEG)` (`backend/explore/coverartproxy.go:114`,
`backend/explore/coverart.go:27-29`). A 250 px CAA JPEG is ~15–25 kB, ~20–33 kB base64, and JS
strings are UTF-16, so **~40–66 kB of retained heap per cached album**, plus the browser's decoded
bitmap keyed off that same multi-kilobyte string.

Neither `thumbnailCache` nor `artistImageCache` is ever evicted, and `explore-view` is a cached
primary view (`index.ts:67`) that never unmounts. A session of browsing — a desktop player runs for
days — grows monotonically: a few hundred searches × ~50 results is on the order of hundreds of MB.

*Fix:* cap both maps with an LRU (a few hundred entries), or return a `/coverart/<mbid>` URL from the
backend instead of a data URL so the browser's own image cache handles eviction.

### M8 — `exploreCache` is a second unbounded, never-evicted cache

`frontend/src/store/explore-cache.ts:35-38`

Four module-level `Map`s (`artists`, `albums`, `artistAlbums`, `artistTopTracks`) with `set` but no
`delete`, no size cap and no TTL. `artistAlbums` holds full `MBReleaseGroup[]` discographies and
`artistTopTracks` full `LBTopRecording[]` lists. Grows for the lifetime of the process.

*Fix:* bound each map (LRU, ~100 entries is plenty for "avoid a refetch when the user hits back").

### M9 — Every `<wa-icon>` is fetched from a remote CDN at runtime

`frontend/index.ts:29-30,47`; resolver in
`@awesome.me/webawesome/dist/chunks/chunk.F5JLNOSF.js` (`library.default`)

WebAwesome's default icon library resolves to
`https://ka-f.fontawesome.com/releases/v7.1.0/svgs/<folder>/<name>.svg`. The literal is present in
the built bundle. `setBasePath('/dist/webawesome')` does **not** change this — `getBasePath` is only
consumed by the component autoloader (`chunk.2PWIIYRH.js:51`), and no
`registerIconLibrary(...)` call exists anywhere in the app.

There are 165 `<wa-icon>` instances across 36 distinct names, so first paint of each view fires up to
36 cross-origin requests. The icon module caches by URL, so it is bounded per session — but a
desktop music player that is offline, on a captive network, or behind a firewall renders **no icons
at all**, and cold start waits on fontawesome.com.

*Fix:* register a local icon library resolving to bundled SVGs (`src/assets/images/icons/` already
holds a set), and add a `vite-plugin-static-copy` rule — the plugin is already a declared devDep
(`package.json`) but is not referenced by `vite.config.mts`, and `dist/webawesome/` does not exist.

### M10 — 1.18 MB single chunk, no route-level code splitting

`frontend/vite.config.mts:16-22`, `frontend/index.ts:1-27`

Verified build (`vite build --outDir /tmp/yjbuild`):

```
assets/main-BAFmIgXb.css     53.46 kB │ gzip:   7.48 kB
assets/main-yB2fsiPY.js   1,183.64 kB │ gzip: 242.14 kB
(!) Some chunks are larger than 500 kB after minification.
```

`rollupOptions` sets only `input`; there is no `manualChunks` and no `import()` anywhere, and
`index.ts` statically imports all 27 views, so every module is downloaded, parsed and
**side-effect-evaluated** (every store singleton constructed, every `@customElement` registered)
before first paint.

Sourcemap-attributed composition of the 1.16 MB of mapped output:

| bytes | source |
|---|---|
| 199 497 | `@awesome.me/webawesome` |
| 76 008 | `components/autotag-view/autotag-view.ts` |
| 52 828 | `components/explore-artist-details/…` |
| 48 519 | `components/config-page/config-page.ts` |
| 42 172 | `components/track-details/track-details.ts` |
| 37 394 | `@lit-labs/virtualizer` |
| 36 666 | `components/playlist-view/playlist-view.ts` |
| 36 333 | `components/explore-album-details/…` |
| 34 457 | `components/explore-view/explore-view.ts` |
| 31 317 | `components/track-list/track-list.ts` |
| 30 989 | `components/playlist-details/…` |
| 30 661 | `components/cover-grid/cover-grid.ts` |
| 30 180 | `wailsjs/go/models.ts` |

The startup-critical path is roughly `track-list` + `cover-grid` + `now-playing` + `audio-player` +
`app-sidebar` + lit + virtualizer ≈ 200 kB. `autotag-view` (76 kB, the single largest app module),
`config-page`, `explore-*`, `track-details`, `jobs-*` and `downloads-view` are all reachable only
from a sidebar click.

*Fix:* replace the static imports in `index.ts` with `await import()` inside the `navigate` handler's
`VIEW_TAGS` branch — the view is already created lazily there (`index.ts:120-127`), only the module
is eager.

---

## Minor

### m1 — `.renderItem` / `.keyFunction` are new closures every render in two virtualized views

`frontend/src/components/artists-view/artists-view.ts:1298-1299`,
`frontend/src/components/genres-view/genres-view.ts:1196-1197`

`LitVirtualizer` declares both as `@property()` with the default `!==` `hasChanged`
(`@lit-labs/virtualizer/LitVirtualizer.js:48-54`), so a fresh arrow function marks the property
dirty and forces the virtualizer's own render pass on every host update. `cover-grid.ts:1893-1894`
and `track-list.ts:1936-1937` correctly bind the stable `this.renderGridEntry` /
`this.renderTrackRow` — these two do not. (`keyFunction` is a fresh closure in all four; `repeat()`
keying limits the DOM damage to re-evaluated templates for the visible window.)

*Fix:* hoist to bound class fields, as `cover-grid` already does.

### m2 — Serial N+1 binding calls behind "play these"

- `frontend/src/components/artists-view/artists-view.ts:945-971` — `GetAlbumsByArtist`, then
  `await GetAlbumTracks(album.ID)` **inside a `for` loop**. A 30-album artist is 31 sequential IPC
  round-trips.
- `frontend/src/components/cover-grid/album-selection.ts:100-112` — same shape; Ctrl+A over 5 000
  albums is 5 000 sequential round-trips (partly mitigated by `albumFilePathCache`).
- `frontend/src/components/genres-view/genres-view.ts:740-751` — one `GetTracksByGenre` per selected
  genre, all fired concurrently, each returning full track rows that are then deduped client-side.

*Fix:* add a single `GetTracksByAlbumIDs([]int64)` / `GetTracksByGenres([]string)` binding.

### m3 — Timers that survive because their view never unmounts

The cleanup is written correctly; it simply never executes for cached primary views.

- `frontend/src/components/downloads-view/downloads-view.ts:216-218` — a 30 s `setInterval` clock,
  cleared at `:226` in `disconnectedCallback`. Once Downloads is visited it ticks and re-renders the
  view for the rest of the session.
- `frontend/src/components/now-playing/now-playing.ts:481,503` — `onScrollCycleEnd` schedules
  `startScrollCycle` (2 s) which schedules the scroll (1.5 s), indefinitely, so a long track title
  drives a state change + re-render every ~3.5 s forever while it plays.

*Fix:* drive these off the `view-hidden` class (a `MutationObserver` on the host, or an explicit
`viewActivated`/`viewDeactivated` hook in `index.ts`) rather than connect/disconnect.

### m4 — Permanent global `mousemove`/`mouseup` listeners for drag interactions

`frontend/src/components/track-list/track-list.ts:1076-1077`,
`frontend/src/components/now-playing/now-playing.ts:240-241`

Column resize and panel resize register document-level `mousemove` in `connectedCallback` and only
remove it in `disconnectedCallback`. Both guard-and-return immediately
(`track-list.ts:622-623`, `now-playing.ts:582-583`), so the cost is small, but they run on every
pointer move anywhere in the app for the process lifetime and defeat the browser's ability to skip
the listener entirely.

*Fix:* attach on `mousedown`, detach on `mouseup` — the standard drag pattern.

### m5 — `updated()` does unconditional DOM work every cycle

- `frontend/src/components/artists-view/artists-view.ts:417-420` and
  `genres-view.ts:409-412` — `updateSizeProperties()` writes 2 `style.setProperty` calls on the host
  unconditionally (`artists-view.ts:671-701`), and `ensureWheelListener()` does a
  `shadowRoot.querySelector` every pass just to check a boolean it already stores
  (`:611-629`). Both should be guarded on the value/flag they already track.
- `frontend/src/components/now-playing/now-playing.ts:259-263` — `checkOverflows()` +
  `applyScrollDistances()` do 6 `querySelector`s and interleave `scrollWidth`/`clientWidth` reads
  with `style.setProperty` writes on every update, i.e. forced synchronous layout followed by
  invalidation, on a component that re-renders on every player-store change.

### m6 — O(total items) helpers on the selection hot path

`frontend/src/utils/selection-controller.ts:160-173`

`getSelectedKeysOrdered()` walks the entire item list (50 k `getItemKey` calls) rather than the
selection. It is called from every context-menu action, every favourite toggle and every
`dragstart` (`track-list.ts:1379-1400`), so starting a drag of one row costs a 50 k-iteration loop.

Related: `frontend/src/components/track-list/track-list.ts:1507-1520` —
`openBatchTrackDetails` does `filePaths.map(fp => this.tracks.find(...))`, i.e. O(selection × total).
"Select all → Edit tags" on 50 k tracks is 2.5 × 10⁹ comparisons and will hang the renderer.

*Fix:* keep an index-ordered selection, and build a `Map<FilePath, Track>` for the batch lookup.

### m7 — The queue list stays live at zero width

`frontend/src/components/queue-panel/queue-panel.ts:214-231` (`:host { width: 0 }` when closed),
`:653-681`

`contain: layout style paint` limits the blast radius, but the `lit-virtualizer` inside still has a
real height and `min-width: 300px`, so it renders and measures its visible window on every queue
change even with the panel closed — and `updated()` calls `scrollToIndex()` (`:675`) on every
current-index change, which is `element(i).scrollIntoView()` on a laid-out but invisible element.

*Fix:* render `nothing` for the list body when the `open` attribute is absent.

### m8 — Backend emits scan progress nothing listens to

`frontend/src/events.ts:34-35`

`LibraryScanStarted` and `LibraryScanProgress` are declared but have **zero** consumers in
`frontend/src/`. During a 50 k-file scan the backend serializes and pushes a progress payload across
the IPC for an empty listener set.

*Fix:* either wire them into a scan indicator or stop emitting them.

### m9 — Remote artist avatars in Explore load eagerly

`frontend/src/components/explore-view/explore-view.ts:1461-1465`

The artist avatar `<img>` has neither `loading="lazy"` nor `decoding="async"`, unlike the album card
20 lines below (`:1515-1519`) which has both. Every artist in a search result starts loading
immediately.

---

## Polish

### p1 — Dead dependency

`@lit-labs/signals` is declared in `frontend/package.json` but imported nowhere in `src/` or
`index.ts`. Rollup tree-shakes it out of the bundle, so this is install-size only — but it also
signals a state-management direction that was never taken, next to five hand-rolled
`Set<Subscriber>` stores.

### p2 — Dead code carried in the bundle

`frontend/src/components/cover-grid/cover-grid.ts:1908-1962` — `renderSplitGrid()` is documented in
its own comment as "Currently unreferenced (the single-grid path is the active rendering mode)",
along with `getBeforeEntries`/`getAfterEntries`/`ensureSplitCache` and the `splitMode` branches that
feed it. `cover-grid.ts` is 30.6 kB of the bundle.

### p3 — Store notify batching is inconsistent

`library-store`, `player-store`, `queue-store`, `job-store` and `download-store` all coalesce with
`queueMicrotask` + a `notifyScheduled` flag. `search-store.ts:55-57` and `playlist-store.ts:133-135`
do not. Lit batches the resulting `requestUpdate()`s anyway, so the impact is small, but the
inconsistency is the kind that hides a real double-notify later.

### p4 — Empty library reads as "Loading tracks..." forever

`frontend/src/components/track-list/track-list.ts:1900-1902` branches on `this.tracks.length === 0`
rather than a loading flag, so a genuinely empty (or fully filtered-out) library shows a permanent
loading message. `libraryCtrl.tracksLoading` already exists for this.

### p5 — `selectAll()` compares sizes, not membership

`frontend/src/utils/selection-controller.ts:148` — `if (next.size === this._selectedItems.size) return;`
short-circuits on cardinality alone. Same-size-different-membership is hard to reach today, but the
guard is wrong as written; comparing against `this.host.getItemCount()` would express the intent.

### p6 — `ResizeObserver` on hidden views writes localStorage on every navigation

`frontend/src/components/track-list/track-list.ts:1079-1085` → `onHostResize` (`:1218-1243`) →
`normalizeWidths` + `saveColumnWidths` (`:515-534`). `.view-hidden` is
`visibility: hidden; height: 0` (`frontend/index.css:162-170`), not `display: none`, so hidden views
stay in the layout tree and their `ResizeObserver`s fire on every navigation. Cheap (localStorage
only), but it is work done for an invisible element.

---

## What is already right

Worth stating plainly, because it is most of the codebase and the findings above are the exceptions:

- **`track-list` is a well-built virtualized list.** Memoized filter/sort caches keyed on input
  identity (`:238-270`), delegated event handlers via `data-index` with zero per-row closures
  (`:1140-1157`, `:1290-1312`), a stable `renderItem`, an `_itemSize` hint that avoids
  lit-virtualizer's scroll-error correction (`:222-228`), RAF-throttled scroll persistence
  (`:1280-1291`), and an inline `<svg>` for the per-row favourite icon instead of a `<wa-icon>` that
  would fetch. All 50 k rows go through this path.
- **`cover-grid` memoizes correctly** — `buildGridEntries()` is keyed on the filtered-albums array
  identity (`:906-926`), so the virtualizer's `items` reference is stable across re-renders, and its
  covers pick the right thumbnail tier with `loading="lazy" decoding="async"` and explicit
  `width`/`height` (`:1803-1814`).
- **`queue-store` is delta-driven**, not snapshot-driven (`queue-store.ts:82-110`) — index, mode and
  track-list mutations each ride their own event.
- **`job-store` is the model for a push store**: microtask-coalesced notify with a documented
  rationale, and it evicts cached logs for jobs the backend has forgotten
  (`job-store.ts:229-236, 263-276`).
- **`favorites-store` is Set-keyed**, so `isFavorited` in a row render is O(1) (`:99-101`).
- **`LibraryController`'s `changeGeneration` guard** correctly suppresses `requestUpdate()` when only
  a loading flag toggled (`library-controller.ts:33-47`) — exactly the granularity most of the other
  controllers lack.
- **`genre-details` and `artist-details` reuse `track-list` / `cover-grid`** via `.externalTracks` /
  `.externalAlbums` instead of reimplementing a list — which is precisely the fix M5 asks for.
- **Detail views are ephemeral** (`index.ts:143-147`), so their `disconnectedCallback` cleanup does
  run and their per-instance caches (e.g. `explore-artist-details`' three `Map`s) are collectable.
  The leaks in M7/M8/m3 are all on the *cached* primary views.

## Things I checked and found no problem with

Recorded so they are not re-audited:

- **`localeCompare` in sort comparators** (`track-list/columns.ts:15`,
  `cover-grid/cover-grid-types.ts:50-76`). Benchmarked 50 k-element sorts: bare `localeCompare`
  **16.5 ms** vs a hoisted `Intl.Collator.compare` **28.7 ms**. V8 already caches the default
  collator; hoisting one would be a pessimization. No finding.
- **Repeated `addEventListener('visibilityChanged', this.onVisibilityChanged)` in
  `track-list.loadTracks()`** (`:1249-1254`). The handler is a stable class-field arrow, so repeat
  registration with the same type+function is a spec-level no-op. Not a leak.
- **WebAwesome's autoloader `MutationObserver`.** `startLoader()` is exported from
  `webawesome.js` but never called by the app, so no global mutation observer is installed. (The
  icon CDN issue in M9 is a separate mechanism.)
- **`layout shift` from row cover art.** Every list container has a fixed pixel box
  (`playlist-details.ts:984-998`, `columns.ts:61`, `cover-grid.ts:1809-1810`), so images do not
  reflow their rows.
- **`job-store` / `download-store` growth.** Both bound their state to the backend snapshot and
  evict.

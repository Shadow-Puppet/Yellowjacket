# Plan: Consolidate `LibraryScanComplete` Handling

Addresses refactoring catalog #7. Eliminates redundant direct `LibraryScanComplete` event listeners from components by making stores eagerly re-fetch data after invalidation, so the existing reactive controller subscription (`requestUpdate()`) delivers fresh data automatically.

---

## Problem Analysis

The refactoring catalog describes 10+ components that each independently listen for `LibraryScanComplete` and re-fetch their data. It claims these listeners are redundant because "the stores already invalidate their caches and notify subscribers."

**This claim is incorrect in the current architecture.** Here is why:

1. When `LibraryScanComplete` fires, `LibraryStore.invalidate()` nulls out cached data (`tracks`, `albums`, `artists`) and calls `notify()`.
2. `notify()` triggers subscriber callbacks, which are `LibraryController.host.requestUpdate()` — a Lit re-render.
3. But `requestUpdate()` only re-runs `render()`, and components read from **local `@state()` properties** (e.g., `this.tracks`, `this.albums`), not from the store. The local data is still stale.
4. Nobody calls the `loadTracks()`/`loadAlbums()` methods again except the direct `LibraryScanComplete` listener.

**The root cause:** the stores use **lazy-fetch** — `invalidate()` clears the cache but does not re-fetch. The next `getTracks()` call will hit the backend, but nothing triggers that call except the component's own event listener.

**The fix:** make stores **eagerly re-fetch** after invalidation, so when the controller calls `requestUpdate()`, the store already has fresh data. Then refactor components to read data reactively from the store/controller instead of from local state populated by imperative load calls.

---

## Guiding Principles

1. **Incremental migration.** The store change (eager refetch) is backwards-compatible. Components are migrated one by one from easiest to hardest. Both patterns (old imperative + new reactive) coexist during migration.

2. **Preserve existing UX.** Scroll restoration, selection clearing, loading indicators, and search filtering must work identically. No regressions.

3. **Three categories of listeners.** Not all `LibraryScanComplete` listeners are the same:
   - **Data refresh listeners** (8 components): re-fetch library/playlist data → these are what we're consolidating.
   - **UI status listeners** (`config-page`, `library-manager`): update scan progress UI and display metrics → these MUST keep their direct listeners since no store handles scan status.
   - **Store-bypassing listeners** (`playlist-picker`): calls Go bindings directly → addressed separately.

4. **Don't fight the `externalAlbums`/`externalTracks` pattern.** Parent-child data delegation (`artist-details` → `cover-grid`, `genre-details` → `track-list`) is a valid pattern. The parent gets migrated; the child already skips the scan listener when driven externally.

---

## Phase 0: Store Eager-Refetch

### 0A. `LibraryStore` — add eager refetch after invalidation

**File:** `frontend/src/store/library-store.ts`

Change `invalidate()` to eagerly re-fetch all three data types after clearing the cache. The existing `getTracks()`/`getAlbums()`/`getArtists()` methods already handle concurrent-request coalescing (via `waitFor*()` helpers) and notify subscribers when loading starts/finishes.

```typescript
// Before:
private invalidate(): void {
    this.tracks = null;
    this.albums = null;
    this.artists = null;
    this.scrollPositions = { tracks: 0, albums: 0, artists: 0, genres: 0 };
    this.notify();
}

// After:
private invalidate(): void {
    this.tracks = null;
    this.albums = null;
    this.artists = null;
    this.scrollPositions = { tracks: 0, albums: 0, artists: 0, genres: 0 };
    this.notify();
    this.eagerRefetch();
}

private eagerRefetch(): void {
    // Fire-and-forget. Each getter handles its own error/loading state
    // and calls notify() when done, which triggers requestUpdate()
    // on all subscribed controllers.
    void this.getTracks();
    void this.getAlbums();
    void this.getArtists();
}
```

**Why this works:** After `eagerRefetch()`, the store is in a `loading=true` state. When the backend responses arrive, the cache is repopulated and `notify()` fires again (from the `finally` block in each getter). Controllers call `requestUpdate()`, and now any component reading from the store gets fresh data.

**Why it's backwards-compatible:** Components with direct listeners will still call their `load*()` methods. The store's `waitFor*()` helpers coalesce concurrent requests, so the eager fetch and the component's fetch share the same in-flight promise — no duplicate backend calls.

**Scroll position reset note:** The `scrollPositions` reset to `0` happens synchronously in `invalidate()`. This is correct — after a library scan, the content has changed and scroll positions are meaningless. Components that read scroll positions during their re-render will see `0`.

### 0B. `PlaylistStore` — add eager refetch after invalidation

**File:** `frontend/src/store/playlist-store.ts`

Same pattern. The `invalidate()` method already exists and is called from multiple event handlers (not just `LibraryScanComplete`).

```typescript
// Before:
invalidate(): void {
    this.playlists = null;
    this.scrollPosition = 0;
    this.notify();
}

// After:
invalidate(): void {
    this.playlists = null;
    this.scrollPosition = 0;
    this.notify();
    void this.getPlaylists();
}
```

**Note:** `PlaylistStore.invalidate()` is public (called by `PlaylistController.invalidate()`). This eager refetch will also run for `PlaylistCreated`, `PlaylistDeleted`, `PlaylistRenamed`, `PlaylistTracksChanged`, and `PlaylistsRestored` events — which is desirable. Currently those events invalidate the cache and wait for a component to lazily re-fetch. Eager refetch means subscribers see fresh data faster.

### 0C. Verification

After Phase 0, both stores eagerly re-fetch on invalidation. Components with existing direct listeners still work (their fetches coalesce with the eager fetch). Components without listeners now get fresh data automatically through the controller subscription path, though they still need to read it reactively (Phase 1+).

---

## Phase 1: Migrate `genre-details` and `artist-details` (LOW effort)

These are thin wrapper components that fetch data, filter/cache it, and pass it to a child via `externalTracks`/`externalAlbums`. The child already skips its own scan listener when receiving external data.

### 1A. `genre-details.ts`

**File:** `frontend/src/components/genre-details/genre-details.ts`

Current flow:
1. `connectedCallback()` → `loadTracks()` → `libraryCtrl.getTracks()` → filter by genre → `this.tracks = filtered`
2. `LibraryScanComplete` → `loadTracks()` again

New flow:
1. `connectedCallback()` → `loadTracks()` (initial load, unchanged)
2. Remove the direct `LibraryScanComplete` listener and its cancellation
3. Add reactive consumption in `willUpdate()` or `updated()`: when the store notifies (cache repopulated after eager refetch), the controller calls `requestUpdate()`, triggering a re-render. In `willUpdate()`, detect that the store's cached tracks have changed (or that loading finished) and re-run the genre filtering.

Implementation approach — use `updated()` to react to controller-triggered re-renders:

```typescript
// Remove from connectedCallback:
//   this.cancelScanComplete = EventsOn(Events.LibraryScanComplete, () => this.loadTracks());
// Remove from disconnectedCallback:
//   this.cancelScanComplete?.();
// Remove the cancelScanComplete field.

// Add a version counter to detect store changes:
private lastStoreVersion = 0;

override updated() {
    // The library controller's subscription calls requestUpdate() when the
    // store notifies. Check if tracks have been refreshed since our last load.
    const storeVersion = this.libraryCtrl.storeVersion;
    if (storeVersion !== this.lastStoreVersion && !this.libraryCtrl.tracksLoading) {
        this.lastStoreVersion = storeVersion;
        this.loadTracks();
    }
}
```

**Alternative (simpler):** Instead of a version counter, check if the store's cached tracks reference has changed. Since `invalidate()` sets tracks to `null` and the eager refetch populates a new array, we can compare object identity:

```typescript
private lastTracksRef: library.Track[] | null = null;

override updated() {
    const cached = this.libraryCtrl.cachedTracks;
    if (cached !== null && cached !== this.lastTracksRef) {
        this.lastTracksRef = cached;
        this.loadTracks();
    }
}
```

**Decision: Use the reference-comparison approach.** It's simpler, doesn't require adding version counters to the store, and leverages the fact that each eager refetch creates a new array instance.

However, there's a subtlety: `updated()` runs after every render, including renders triggered by the component's own `@state()` changes (like `this.tracks` being set). We need to ensure this doesn't create an infinite loop:
- `loadTracks()` calls `libraryCtrl.getTracks()`, which if the cache is already populated returns the same reference.
- `this.tracks` is set to the filtered result, triggering a render.
- `updated()` runs, compares `cachedTracks` — same reference as `lastTracksRef`, so no re-load. Safe.

But the initial load path: `connectedCallback()` calls `loadTracks()` directly. At that point `cachedTracks` might be `null` (store hasn't loaded yet). After `loadTracks()` finishes, the store cache is populated, and `lastTracksRef` is set. Next `requestUpdate()` from the store won't trigger a re-load because the reference matches. Safe.

**Required controller addition:** Add a `storeVersion` or expose `cachedTracks` — the controller already exposes `cachedTracks` (line 72-74 of `library-controller.ts`). No changes needed to the controller.

### 1B. `artist-details.ts`

**File:** `frontend/src/components/artist-details/artist-details.ts`

Same approach. This component has a cache-then-fetch dual-load pattern (`getAlbumsByArtistNameCached` then `getAlbumsByArtist`). The scan-complete handler just calls `loadAlbums()`.

Changes:
1. Remove the direct `LibraryScanComplete` listener and its cancellation.
2. Add reference comparison in `updated()`:

```typescript
private lastAlbumsRef: library.Album[] | null = null;

override updated() {
    const cached = this.libraryCtrl.cachedAlbums;
    if (cached !== null && cached !== this.lastAlbumsRef) {
        this.lastAlbumsRef = cached;
        this.loadAlbums();
    }
}
```

**Note:** `getAlbumsByArtist(artistId)` is NOT cached by the store — it always hits the backend. But that's fine because `loadAlbums()` already handles this. The reference check on `cachedAlbums` (the full album list) serves as a proxy for "the library data has changed."

---

## Phase 2: Migrate `artists-view` (LOW-MEDIUM effort)

**File:** `frontend/src/components/artists-view/artists-view.ts`

Current flow:
1. `connectedCallback()` → `loadArtists()` → `libraryCtrl.getArtists()` → `this.artists = result`
2. `LibraryScanComplete` → `loadArtists()`
3. `willUpdate()` → `recomputeArtistCaches()` (filters by search term)
4. `render()` reads `cachedGridEntries`

New flow:
1. `connectedCallback()` → `loadArtists()` (initial load, unchanged)
2. Remove the direct `LibraryScanComplete` listener.
3. React to store changes in `updated()`:

```typescript
private lastArtistsRef: library.Artist[] | null = null;

override updated() {
    const cached = this.libraryCtrl.cachedArtists;
    if (cached !== null && cached !== this.lastArtistsRef) {
        this.lastArtistsRef = cached;
        this.loadArtists();
    }
}
```

**Scroll position consideration:** `loadArtists()` currently restores scroll position at the end. After a scan, scroll positions are reset to `0` by `invalidate()`. The `restoringScroll` flag and `restoreScrollPosition()` call in `loadArtists()` handle this correctly — they'll restore to position `0`, which is a no-op visually.

**Selection consideration:** `loadArtists()` does not currently clear selection. After migration, selection could reference stale artist IDs. Consider adding `this.selectedArtists.clear()` at the top of `loadArtists()` if not already present. (This is a minor improvement, not a regression from the migration.)

---

## Phase 3: Migrate `genres-view` (HIGH effort)

**File:** `frontend/src/components/genres-view/genres-view.ts`

This component derives genres from tracks — a transformation the store doesn't provide. The store exposes tracks, not genres.

Current flow:
1. `loadGenres()` → `libraryCtrl.getTracks()` → `extractGenres(tracks)` → `this.genres = result`
2. `LibraryScanComplete` → `loadGenres()`

New flow:
1. Same `loadGenres()` for initial load.
2. Remove the direct `LibraryScanComplete` listener.
3. React to store changes in `updated()` using `cachedTracks` reference comparison:

```typescript
private lastTracksRef: library.Track[] | null = null;

override updated() {
    // Existing updated() logic for search term, size properties, etc.
    // stays unchanged. Add this at the end:
    const cached = this.libraryCtrl.cachedTracks;
    if (cached !== null && cached !== this.lastTracksRef) {
        this.lastTracksRef = cached;
        this.loadGenres();
    }
}
```

**Why not move genre extraction to the store?** The store's job is to cache backend data, not derive view-specific aggregations. Genres are only needed by `genres-view` and `genre-details`. Adding genre derivation to the store would couple it to a specific UI concern. The component is the right place for this derivation.

**Scroll/selection considerations:** Same as `artists-view`. `loadGenres()` handles scroll restoration. Consider adding `this.selectedGenres.clear()` if not already present.

---

## Phase 4: Migrate `track-list` (MEDIUM effort)

**File:** `frontend/src/components/track-list/track-list.ts`

This has a dual-source pattern (`externalTracks` vs store fetch). The scan listener is already conditionally registered:

```typescript
if (this.externalTracks) {
    this.tracks = this.externalTracks;
} else {
    this.loadTracks();
    this.cancelScanComplete = EventsOn(Events.LibraryScanComplete, () => this.loadTracks());
}
```

New flow:
1. Keep the `externalTracks` path unchanged — when a parent provides tracks, the parent is responsible for refreshing (and the parent's migration in Phase 1/3 handles this).
2. For the standalone path (no `externalTracks`):
   - `connectedCallback()` → `loadTracks()` (initial load, unchanged)
   - Remove the `LibraryScanComplete` listener registration
   - Add reactive consumption in `updated()`, guarded by `!this.externalTracks`:

```typescript
private lastTracksRef: library.Track[] | null = null;

override updated() {
    // ... existing updated() logic ...

    if (!this.externalTracks) {
        const cached = this.libraryCtrl.cachedTracks;
        if (cached !== null && cached !== this.lastTracksRef) {
            this.lastTracksRef = cached;
            this.loadTracks();
        }
    }
}
```

**Selection consideration:** `loadTracks()` already clears selection. Safe.

---

## Phase 5: Migrate `playlist-view` (HIGH effort)

**File:** `frontend/src/components/playlist-view/playlist-view.ts`

This component reshapes `playlist.WithTracks[]` into `PlaylistEntry[]` with an `expanded` boolean per entry. It has a `refreshPlaylists()` method that preserves expanded state across refetches.

Current flow:
1. `loadPlaylists()` → `playlistCtrl.getPlaylists()` → map to `PlaylistEntry[]` → `this.entries = result`
2. `LibraryScanComplete` → `loadPlaylists()`

New flow:
1. `connectedCallback()` → `loadPlaylists()` (initial load, unchanged)
2. Remove the direct `LibraryScanComplete` listener.
3. React to store changes in `updated()`:

```typescript
private lastPlaylistsRef: playlist.WithTracks[] | null = null;

override updated() {
    // ... existing updated() logic ...

    const cached = this.playlistCtrl.cachedPlaylists;
    if (cached !== null && cached !== this.lastPlaylistsRef) {
        this.lastPlaylistsRef = cached;
        this.refreshPlaylists(); // preserves expanded state
    }
}
```

**Key choice: use `refreshPlaylists()` instead of `loadPlaylists()`.** The `refreshPlaylists()` method preserves which playlists are expanded, providing a better UX after a scan completes. `loadPlaylists()` resets all to collapsed. The current scan-complete handler uses `loadPlaylists()` (collapsing everything), but since we're improving the architecture anyway, switching to `refreshPlaylists()` is a UX improvement.

**Alternative consideration:** If `loadPlaylists()` is preferred (to reset UI state after a scan), that works too. The choice is a UX decision, not a technical constraint.

---

## Phase 6: Migrate `cover-grid` (VERY HIGH effort)

**File:** `frontend/src/components/cover-grid/cover-grid.ts`

The most complex component. Dual-source pattern, split-mode scroll management, sort/filter pipeline.

Current flow:
1. `loadAlbums()` → `libraryCtrl.getAlbums()` or `externalAlbums` → `this.albums = result`
2. Only registers scan listener when `!this.externalAlbums`
3. `LibraryScanComplete` → `loadAlbums()`

New flow:
1. Keep the `externalAlbums` path unchanged.
2. For the standalone path:
   - `connectedCallback()` → `loadAlbums()` (initial load, unchanged)
   - Remove the `LibraryScanComplete` listener registration
   - Add reactive consumption in `updated()`, guarded by `!this.externalAlbums`:

```typescript
private lastAlbumsRef: library.Album[] | null = null;

override updated() {
    // ... existing updated() logic (size properties, wheel listener,
    //     grid layout, search term selection clearing) ...

    if (!this.externalAlbums) {
        const cached = this.libraryCtrl.cachedAlbums;
        if (cached !== null && cached !== this.lastAlbumsRef) {
            this.lastAlbumsRef = cached;
            this.loadAlbums();
        }
    }
}
```

**Split-mode consideration:** If the album dropdown is open (`expandedAlbumId !== null`) when a scan completes, `loadAlbums()` will close it (resets `expandedAlbumId` and `expandedTracks`). This is the same behavior as the current direct listener. The split-mode transition logic in `willUpdate()` will handle the layout change.

**Selection consideration:** `loadAlbums()` already clears album selection. Safe.

---

## Phase 7: Cleanup and Documentation

### 7A. Remove unused imports

After all data-refresh components are migrated, remove unused `EventsOn` and `Events` imports from migrated components (only if no other events are listened to in that component).

### 7B. Components that KEEP their direct listeners

These components are explicitly excluded from migration and should be documented:

| Component | Reason |
|---|---|
| `config-page.ts` | Uses event for UI status (scan progress, metrics display), not data refresh. No store handles scan status. |
| `library-manager.ts` | Same as config-page — UI status listener for scan progress/metrics. |
| `playlist-picker.ts` | Bypasses store entirely, calls `GetAllPlaylists()` directly for lightweight summary data. See refactoring catalog #19 for a future plan to route this through a store. |

### 7C. Update refactoring catalog

Mark item #7 as solved in `.opencode/plans/refactoring-catalog.md`.

---

## Migration Order Summary

| Phase | Component(s) | Effort | Depends On |
|---|---|---|---|
| 0 | `LibraryStore`, `PlaylistStore` (eager refetch) | Low | — |
| 1 | `genre-details`, `artist-details` | Low | Phase 0 |
| 2 | `artists-view` | Low-Medium | Phase 0 |
| 3 | `genres-view` | High | Phase 0 |
| 4 | `track-list` | Medium | Phase 0 |
| 5 | `playlist-view` | High | Phase 0 |
| 6 | `cover-grid` | Very High | Phase 0 |
| 7 | Cleanup + docs | Low | Phases 1-6 |

Each phase after 0 is independent of the others and can be done in any order. The ordering above goes from easiest to hardest as a recommended sequence.

---

## Reactive Pattern: Reference Comparison

All component migrations use the same pattern to detect store data changes:

```typescript
private lastDataRef: T[] | null = null;

override updated() {
    const cached = this.controller.cachedData;
    if (cached !== null && cached !== this.lastDataRef) {
        this.lastDataRef = cached;
        this.loadData(); // existing imperative load method
    }
}
```

**Why reference comparison instead of a version counter or dirty flag:**
- **Simplicity:** No store API changes needed. `cachedTracks`/`cachedAlbums`/`cachedArtists`/`cachedPlaylists` are already exposed by controllers.
- **Correctness:** Each backend fetch creates a new array instance. `invalidate()` sets cache to `null`. The reference comparison catches both "new data arrived" and "data was cleared and refetched."
- **No infinite loops:** Setting `this.lastDataRef = cached` before calling `loadData()` prevents re-triggering. The `loadData()` call may set local `@state()` which triggers another `updated()`, but by then `lastDataRef` matches `cached` and the guard short-circuits.
- **No store changes needed:** The controllers already expose `cachedTracks`, `cachedAlbums`, `cachedArtists`, `cachedPlaylists`.

**Why not move everything into `render()`:** Components do significant local work beyond just displaying store data — filtering, sorting, scroll restoration, selection management. Keeping the imperative `loadData()` call but triggering it reactively is the minimal change that achieves the goal.

---

## Risk Assessment

| Risk | Mitigation |
|---|---|
| Double fetch on scan complete (eager + component listener during migration) | Store's `waitFor*()` helpers coalesce concurrent requests. Only one backend call actually fires. |
| Infinite `updated()` loop | Reference comparison with `lastDataRef` assignment prevents re-triggering. Each migration should be tested for this. |
| Stale selection after scan | `loadData()` methods already clear selection in most components. Verify for each migration. |
| Scroll position regression | `invalidate()` resets scroll to `0`. `loadData()` methods handle scroll restoration. The `0` position means "start from top", which is correct after a scan. |
| `externalAlbums`/`externalTracks` components don't refresh | Parent components (`artist-details`, `genre-details`) are migrated first. They re-fetch and update the `external*` property, which triggers the child's `willUpdate()` change detection. |
| `playlist-picker` left unmigrated | Intentional. It uses a different API (`GetAllPlaylists` vs `GetAllPlaylistsWithTracks`). See catalog item #19. |

---

## Testing Strategy

For each phase:
1. **Manual test:** Trigger a library scan while each affected view is visible. Verify data refreshes without stale content.
2. **Manual test:** Trigger a scan while a detail view is open (`genre-details`, `artist-details`). Verify child components (`track-list`, `cover-grid`) refresh via the parent's external data update.
3. **Manual test:** Verify scroll position resets to top after scan.
4. **Manual test:** Verify that adding/removing tracks from the library directory and scanning updates all views correctly.
5. **Verify no console errors** — especially no infinite loop warnings or unhandled promise rejections.
6. **Run `pnpm exec tsc --noEmit`** — ensure no TypeScript errors after each phase.

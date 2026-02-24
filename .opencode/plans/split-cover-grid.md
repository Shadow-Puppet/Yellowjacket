# Plan: Split `cover-grid.ts` + Extract Shared Context Menu

Addresses refactoring catalog #4 (split `cover-grid.ts`, 3774 lines) and partially addresses #8 (49× `as any` casts on popups).

## Current State

`frontend/src/components/cover-grid/cover-grid.ts` is the largest frontend file at 3774 lines. It contains a single `CoverGrid` LitElement that handles:

- Virtualized album grid rendering (single + split mode with inline dropdown)
- Album/track selection (custom inline logic with Ctrl/Shift/range)
- Context menus (album + track, with playlist submenu) — **duplicated across 6 components**
- Drag-and-drop source (albums + tracks)
- Sort controls (toolbar + dropdown)
- Ctrl+scroll zoom
- Scroll position save/restore (index-based + pixel-based resize-aware)
- Transition overlays (DOM snapshots during layout transitions)
- Album filtering/sorting (memoized)
- 319 lines of CSS

The context menu logic is copy-pasted into 6 components: `cover-grid.ts`, `track-list.ts`, `playlist-view.ts`, `queue-panel.ts`, `genres-view.ts`, `artists-view.ts`. Each duplicates ~200 lines of state, open/close methods, submenu timers, document event listeners, and render templates.

---

## Guiding Principles

1. **Extract logic modules, not sub-components.** The grid is one visual component. Splitting it into multiple custom elements would create artificial boundaries and state-forwarding complexity. Instead, extract plain TS files (classes/functions) that the component imports.

2. **Follow existing patterns.** The codebase has `SelectionController` in `utils/`, `drag-controller.ts`, `drag-image.ts`. New extractions follow these conventions.

3. **Shared context menu is the highest-value extraction.** Duplicated across 6 components, it benefits the whole codebase.

4. **Don't over-split.** Lifecycle methods, render methods, and data loading are inherently tied to component state and stay in the main file. Some code density is fine for orchestration.

---

## Part 1: Types and Constants → `cover-grid-types.ts`

**New file:** `frontend/src/components/cover-grid/cover-grid-types.ts` (~85 lines)

**Move from `cover-grid.ts` lines 49-132:**
- `ContextMenuTarget` discriminated union type
- `GridEntry` interface
- `SCROLL_DEBOUNCE_MS`, `ZOOM_STEP` constants
- `SORT_FIELD_KEY`, `SORT_DIR_KEY` localStorage key constants
- `AlbumSortField` type, `SortDirection` type
- `AlbumSortOption` interface
- `ALBUM_SORT_OPTIONS` array (3 sort options with comparator functions)

**Rationale:** Pure data definitions with zero component dependency. Multiple files in the directory will import these (scroll-manager needs `SCROLL_DEBOUNCE_MS`, main file needs sort options, etc.).

---

## Part 2: CSS Styles → `cover-grid-styles.ts`

**New file:** `frontend/src/components/cover-grid/cover-grid-styles.ts` (~270 lines)

**Move from `cover-grid.ts` lines 343-661**, minus the context-menu styles (~47 lines at 615-661) which move to the shared context menu utility in Part 4.

Export as a tagged template:
```typescript
import { css } from 'lit';
export const coverGridStyles = css`...`;
```

Main file uses:
```typescript
import { coverGridStyles } from './cover-grid-styles.js';
import { contextMenuStyles } from '@utils/context-menu-controller.js';
// ...
static override styles = [coverGridStyles, contextMenuStyles];
```

**Rationale:** Standard Lit pattern for large style blocks. Reduces visual noise. The style array composition pattern is idiomatic Lit.

---

## Part 3: Scroll Manager → `scroll-manager.ts`

**New file:** `frontend/src/components/cover-grid/scroll-manager.ts` (~450 lines)

**Move from `cover-grid.ts`:**
- Scroll position persistence: `restoreScrollPosition()` (line 1580), `onVisibilityChanged` (line 1607)
- Resize-aware scroll preservation: `setupResizeObserver()` (line 1668), `captureFocusPoint()` (line 1803)
- Layout helpers: `getColumnCount()` (line 1865), `getContainerWidth()` (line 1887), `getGridRowWidth()` (line 1901), `getCaratOffset()` (line 1916), `computeSplitIndex()` (line 1949)
- Transition overlay: `captureOverlay()` (line 2039), `removeOverlay()` (line 2090)
- Scroll positioning: `awaitBeforeLayout()` (line 2116), `computeAdjustedScrollTop()` (line 2135), `restoreScrollTop()` (line 2197), `scrollToShowDropdown()` (line 2265)
- Associated fields: `resizeObserver`, `resizeDebounceTimer`, `pendingFocus`, `currentColumnCount`, `isResizing`, `savedScrollTop`, `needsScrollRestore`, `showDropdownAfterRestore`, `scrollRestoreGeneration`, `scrollRestoreResolved`, `savedAlbumViewportOffset`, `transitionOverlay`, `scrollDebounceTimer`

**Shape:** Plain class with a host interface (not a ReactiveController — scroll management is imperative/async, not reactive).

```typescript
export interface ScrollManagerHost {
    readonly libraryCtrl: LibraryController;
    readonly cachedFilteredAlbums: library.Album[];
    readonly expandedAlbumId: number | null;
    readonly expandedTracks: library.Track[];
    readonly splitMode: boolean;
    readonly splitIndex: number;
    readonly cardWidth: number;
    readonly cardHeight: number;
    readonly cardTextHeight: number;
    shadowRoot: ShadowRoot | null;
    updateComplete: Promise<boolean>;
    requestUpdate(): void;
}

export class ScrollManager {
    constructor(host: ScrollManagerHost, gridConstants: GridConstants);
    
    // Called from component lifecycle
    setup(): void;          // from connectedCallback
    teardown(): void;       // from disconnectedCallback
    
    // Scroll save/restore
    onVisibilityChanged(e: VisibilityChangedEvent): void;
    restoreScrollPosition(): void;
    
    // Resize handling
    setupResizeObserver(): void;
    
    // Split/single mode transitions
    captureOverlay(): void;
    removeOverlay(): void;
    computeAdjustedScrollTop(): number;
    async restoreScrollTop(target: number): Promise<void>;
    async scrollToShowDropdown(): Promise<void>;
    awaitBeforeLayout(): Promise<void>;
    
    // Layout geometry
    getColumnCount(): number;
    getContainerWidth(): number;
    getGridRowWidth(): number;
    getCaratOffset(): number;
    computeSplitIndex(): number;
    
    // State exposed to component
    needsScrollRestore: boolean;
    showDropdownAfterRestore: boolean;
    savedScrollTop: number;
    savedAlbumViewportOffset: number | null;
    isResizing: boolean;
    splitIndex: number;
}
```

**Rationale:** Scroll management is the largest concern (~800 raw lines, consolidated to ~450 without the grid constants that stay on the component). It's completely self-contained — reads component state but doesn't modify selection, context menus, or rendering. The host interface decouples it from the concrete class. A plain class (not ReactiveController) is honest about the imperative nature of scroll management.

---

## Part 4: Shared Context Menu Controller → `utils/context-menu-controller.ts`

**New file:** `frontend/src/utils/context-menu-controller.ts` (~200 lines)

This is the highest cross-cutting value extraction. The same context menu pattern is duplicated in 6 components.

**Extract the common pattern from all 6 components:**

```typescript
import type { ReactiveController, ReactiveControllerHost } from 'lit';

export interface ContextMenuHost extends ReactiveControllerHost {
    // Query accessors — each component provides its own popup element refs
    getContextMenuPopup(): HTMLElement | undefined;
    getPlaylistSubmenuPopup(): HTMLElement | undefined;
    updateComplete: Promise<boolean>;
    shadowRoot: ShadowRoot | null;
}

export class ContextMenuController implements ReactiveController {
    // Reactive state (component reads these for rendering)
    contextMenuOpen = false;
    playlistSubmenuOpen = false;
    playlistFilePaths: string[] = [];

    constructor(host: ContextMenuHost);
    
    // Lifecycle — registers/removes document-level listeners
    hostConnected(): void;
    hostDisconnected(): void;
    
    // Actions
    openAt(clientX: number, clientY: number): void;
    close(): void;
    showPlaylistSubmenu(filePaths: string[]): Promise<void>;
    closePlaylistSubmenu(): void;
    onPlaylistActionComplete(): void;
}
```

**Also extract** shared context menu CSS styles as:
```typescript
export const contextMenuStyles = css`
    #context-menu { ... }
    .context-menu-panel { ... }
    wa-dropdown-item { ... }
    .submenu-item { ... }
    .submenu-arrow { ... }
    #playlist-submenu { ... }
`;
```

**What stays in each component:**
- The `renderContextMenu()` method — menu items differ per component (cover-grid has conditional "Track Details", queue-panel has "Remove" instead of "Add to Queue", etc.)
- The `onContextMenuAction(action)` handler — file path resolution differs per component
- The `@query` decorators for popup elements (passed to controller via host interface)

**Components to update (6):**
1. `cover-grid.ts` — Remove ~200 lines of inline context menu code
2. `track-list.ts` — Remove ~200 lines
3. `playlist-view.ts` — Remove ~200 lines (keep the second playlist-level context menu as-is or also migrate)
4. `queue-panel.ts` — Remove ~200 lines
5. `genres-view.ts` — Remove ~200 lines
6. `artists-view.ts` — Remove ~200 lines

**Bonus:** All 49× `(popup as any).anchor = ...` and `(popup as any).active = ...` casts are now centralized in one file. This partially addresses catalog item #8 — adding proper typing to the controller's internals eliminates the `as any` from all 6 components.

**Rationale:** ReactiveController is the right shape here (unlike ScrollManager) because it manages document-level event listeners tied to the component lifecycle via `hostConnected`/`hostDisconnected`. This matches the existing `SelectionController` pattern in `utils/`.

---

## Part 5: Album Selection Manager → `album-selection.ts`

**New file:** `frontend/src/components/cover-grid/album-selection.ts` (~250 lines)

**Move from `cover-grid.ts`:**
- Album selection: `selectAlbumRange()` (line 2378), `getSelectedAlbumFilePaths()` (line 2398), `getContextMenuAlbumFilePaths()` (line 2422), `getAlbumFilePaths()` (line 2449)
- Drag cache: `warmAlbumFilePathCache()` (line 2471), `getCachedSelectedAlbumFilePaths()` (line 2505), `albumFilePathCache` Map
- Track selection: `selectTrackRange()` (line 2528), `getSelectedTrackFilePaths()` (line 2547)
- Dropdown coupling: `closeDropdown()` (line 2561), `openDropdown()` (line 2575), `syncDropdownToSelection()` (line 2607)

**Shape:**
```typescript
export class AlbumSelectionManager {
    selectedAlbums = new Set<number>();
    selectedTracks = new Set<string>();
    expandedAlbumId: number | null = null;
    expandedTracks: library.Track[] = [];
    lastSelectedAlbumIndex: number | null = null;
    lastSelectedTrackIndex: number | null = null;
    
    private albumFilePathCache = new Map<number, string[]>();
    
    // Album selection
    selectAlbumRange(from: number, to: number, filteredAlbums: library.Album[]): Set<number>;
    async getSelectedAlbumFilePaths(albums: library.Album[]): Promise<string[]>;
    async getContextMenuAlbumFilePaths(contextMenuAlbumId: number | null, albums: library.Album[]): Promise<string[]>;
    
    // Drag cache
    async warmCache(albums: library.Album[]): Promise<void>;
    getCachedSelectedPaths(albums: library.Album[]): string[];
    
    // Track selection
    selectTrackRange(from: number, to: number): Set<string>;
    getSelectedTrackFilePaths(): string[];
    
    // Dropdown
    async openDropdown(album: library.Album): Promise<void>;
    closeDropdown(): void;
    syncDropdownToSelection(filteredAlbums: library.Album[]): void;
    
    // Reset
    clear(): void;
}
```

**Why not use the existing `SelectionController`?** The existing controller:
- Uses string keys only; album selection uses numeric IDs
- Manages a single selection set; cover-grid has separate album and track selections
- Has no concept of dropdown coupling (selecting 1 album → opens dropdown)
- Has no file path caching for drag

Retrofitting `SelectionController` to handle all of this would make it overly complex for its other consumers (`track-list.ts`, `playlist-view.ts`, `queue-panel.ts`). A dedicated manager for cover-grid's dual album/track model is cleaner.

**Rationale:** Selection state + file path resolution is a coherent concern (~250 lines) that doesn't need access to the DOM, making it easy to extract. The main component's event handlers become thin wrappers that call into this manager.

---

## What Stays in `cover-grid.ts`

After all extractions and improvements, the main file will be approximately **~1700 lines** (down from 3774):

| Section | ~Lines | Why it stays |
|---------|--------|-------------|
| Imports and class declaration | 60 | Structural |
| Properties, state, queries, controllers | 100 | Component-specific reactive state (fewer `@state` props) |
| Grid layout creation + memoization | 80 | Tightly coupled to virtualizer |
| Lifecycle (connectedCallback, disconnectedCallback, willUpdate, updated) | 350 | Orchestration — wires managers together (debug logs removed) |
| Dynamic size properties + zoom | 70 | Simple, component-specific |
| Data loading | 30 | Simple async fetch |
| Virtualizer item builders | 40 | Depends on component state (memoized) |
| Event handlers (album + track + drag) | 340 | Thin delegation to managers |
| Render methods | 430 | Templates reference component state |
| Sort toolbar logic | 120 | Small, self-contained |

~1700 lines is still substantial, but the *complexity* is dramatically reduced because the three hardest subsystems (scroll management, context menus, selection/file-path resolution) are encapsulated in dedicated modules. The remaining code is pure orchestration and rendering.

---

## What This Does NOT Do

- **Does not split into multiple custom elements** — Artificial component boundaries would add event-forwarding complexity for no UX benefit.
- **Does not refactor the split/single virtualizer architecture** — That's the core rendering strategy; changing it is a separate effort.
- **Does not retrofit `SelectionController` for albums** — The existing controller serves different consumers with simpler needs. See Part 5 rationale.
- **Does not touch `album-dropdown.ts`** — Already a well-scoped 410-line sub-component.
- **Does not extract drag handlers** — ~165 lines of glue code that delegates to existing `drag-controller.ts`/`drag-image.ts`. Diminishing returns.

---

## Part 6: Code Quality and Performance Improvements

These improvements are applied during the extraction steps that touch the relevant code. They don't change behavior — they make the same behavior more efficient and clean.

### 6a. Remove 13 `console.log` debug statements

**Lines:** 1133, 1151, 1192, 1226, 1303, 1320, 1328, 1390, 1432, 1437, 2158, 2176, 2345

The scroll restoration and transition overlay code contains 13 `console.log` calls that are clearly development debugging artifacts (e.g., `[willUpdate] exit split (tracks empty)`, `[updated] scroll restore start`, `[adjustScroll]`, `[restoreScrollTop] attempt ${i}`).

**Action:** Remove all 13 `console.log` calls. Keep the 3 `console.error` (actual failures) and 1 `console.warn` (retry exhaustion).

**Applied during:** Part 3 (scroll-manager extraction) and Part 5 lifecycle cleanup.

### 6b. Memoize `buildGridEntries()` — eliminates 3-5 redundant array allocations per render

**Problem:** `buildGridEntries()` allocates a new `GridEntry[]` array on every call. In split-mode rendering, it's called up to 5 times per render cycle:
- `getBeforeEntries()` → `buildGridEntries().slice(0, splitIndex)` (line 2003)
- `getAfterEntries()` called **twice** in `renderSplitGrid()` — once for `.length > 0` check (line 3612), once for `.items` (line 3616) — each rebuilding the full array
- `onVisibilityChanged` scroll handler also rebuilds it (line 1637)

There's even a placeholder comment on line 340: `// buildGridEntries() memoization cache.` — but no cache was ever implemented.

**Action:**
1. Cache the `GridEntry[]` result, keyed on `cachedFilteredAlbums` reference identity. Invalidate in `recomputeAlbumCache()`.
2. In `renderSplitGrid()`, compute `const afterEntries = this.getAfterEntries()` once and reuse for both the length check and the `.items` binding.

**Applied during:** Part 1 (types — `GridEntry` moves) and main file cleanup.

### 6c. Cache expanded album index — eliminates 6 redundant O(n) scans

**Problem:** `cachedFilteredAlbums.findIndex((a) => a.ID === this.expandedAlbumId)` appears at 6 call sites (lines 1077, 1364, 1813, 1919, 1959, 2276). Each is a linear scan of the album array for the same ID.

**Action:** Compute `expandedAlbumIndex` in `recomputeAlbumCache()` (or in `willUpdate` when `expandedAlbumId` changes). All 6 call sites become a direct property read. Invalidate when either `expandedAlbumId` or `cachedFilteredAlbums` changes.

**Applied during:** Part 3 (scroll-manager extraction — 4 of the 6 sites are in scroll code) and main file cleanup.

### 6d. Build `albumById` Map for O(1) selection lookups

**Problem:** `getSelectedAlbumFilePaths()` (line 2401) and `warmAlbumFilePathCache()` (line 2472) both call `this.albums.filter(a => selectedAlbums.has(a.ID))` to find selected albums — an O(n) scan of the full album list. `resolveTrackCoverArt()` (line 3181) does `this.albums.find(a => a.Name === albumName)` — an O(n) name-based scan that could also match the wrong album if names collide.

**Action:** Build a `Map<number, library.Album>` (keyed by album ID) when `albums` changes. Selection lookups iterate `selectedAlbums` and do O(1) map lookups. `resolveTrackCoverArt()` uses the map with `expandedAlbumId` instead of name-based search.

**Applied during:** Part 5 (album-selection extraction).

### 6e. Remove unnecessary `@state()` from 2 properties

**Problem:** 15 properties have `@state()`. Two don't need it:
- `playlistFilePaths` (line 695) — only rendered inside the playlist submenu, which is conditionally shown when `playlistSubmenuOpen` is true. Since `showPlaylistSubmenu()` sets `playlistFilePaths` before setting `playlistSubmenuOpen`, the reactive update from `playlistSubmenuOpen` will render with the correct paths. `playlistFilePaths` itself doesn't need to trigger a re-render.
- `splitIndex` (line 737) — only used to compute `getBeforeEntries()`/`getAfterEntries()`. It's always set before `splitMode` changes (which triggers the render), so it doesn't need independent reactivity.

**Action:** Remove `@state()` decorator from both. Make them plain private fields.

**Applied during:** Main file cleanup after extractions.

### 6f. Single-pass `onGridClick` path traversal

**Problem:** `onGridClick` (line 3071) calls `composedPath()` once, then iterates it twice with `.some()` — once for `.album-card` and once for `.album-dropdown`.

**Action:** Single loop checking both classes:
```typescript
for (const el of e.composedPath()) {
    if (!(el instanceof HTMLElement)) continue;
    if (el.classList.contains('album-card') ||
        el.classList.contains('album-dropdown')) return;
}
```

**Applied during:** Main file cleanup.

### 6g. Use expanded album directly for cover art resolution

**Problem:** `resolveTrackCoverArt(albumName)` (line 3176) does an O(n) `.find()` on `this.albums` by `Name` to get cover art URLs. But we already know which album is expanded (`expandedAlbumId`), and all tracks in the dropdown belong to that album. Name-based lookup has a theoretical collision risk if two albums share the same name.

**Action:** Replace the name-based search with a direct lookup using `expandedAlbumId` and the `albumById` map from improvement 6d. Falls back gracefully if the album isn't found.

**Applied during:** Part 5 (album-selection extraction) or main file cleanup.

### 6h. Prune `albumFilePathCache` to prevent unbounded growth

**Problem:** The `albumFilePathCache` (Map<number, string[]>) is warmed when albums are selected and read during dragstart, but entries are never removed. Over a session, it grows without bound.

**Action:**
1. Clear the entire cache when `albums` changes (library rescan).
2. After `warmAlbumFilePathCache()` completes, remove entries whose album ID is no longer in `selectedAlbums`.

**Applied during:** Part 5 (album-selection extraction — the cache moves to `AlbumSelectionManager`).

---

## Execution Order

| Step | File(s) | Risk | Notes |
|------|---------|------|-------|
| 1 | `cover-grid-types.ts` | Minimal | Pure move, no logic changes |
| 2 | `cover-grid-styles.ts` | Minimal | Pure move, verify `static styles` array works |
| 3 | `utils/context-menu-controller.ts` | Medium | Widest blast radius — update 6 components |
| 4 | `album-selection.ts` + improvements 6d, 6g, 6h | Low | Contained to cover-grid |
| 5 | `scroll-manager.ts` + improvements 6a, 6c | Medium | Largest extraction, deep state interaction |
| 6 | Main file cleanup: improvements 6b, 6e, 6f | Low | After extractions, clean up remaining code |
| 7 | Verify: `pnpm build` + `pnpm exec tsc --noEmit` | — | Ensure no type errors or build failures |

Steps 1-2 are safe warmups. Step 3 has the highest cross-cutting value. Steps 4-5 are the structural wins for cover-grid itself. Step 6 is polish. Each step should be independently verifiable with `tsc --noEmit`.

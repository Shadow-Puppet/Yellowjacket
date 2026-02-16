# Scroll Position Restore: Findings & Status

## Goal

When switching between views (track-list, cover-grid), restore the scroll position so the user doesn't lose their place. Data is already cached via `LibraryStore` so re-queries aren't needed.

## Architecture

- **LibraryStore** (`frontend/src/store/library-store.ts`): Singleton that caches track/album data and stores a per-view scroll position (stored as the first visible item index, not pixel offset).
- **LibraryController** (`frontend/src/store/controllers/library-controller.ts`): ReactiveController bridging LibraryStore to Lit components.
- **Save mechanism**: Both components listen for `visibilityChanged` events on `<lit-virtualizer>`. The event carries `{ first, last }` (indices of first/last visible items). We store `first` in the LibraryStore on every event.
- **Restore mechanism**: On first `visibilityChanged` after mount, call `scrollToIndex(savedIndex, 'start')` to jump to the saved item.
- **Backend**: `LibraryScanComplete` event emitted from Go after library scan; frontend store listens and invalidates caches.

## Key Technical Details

### lit-virtualizer internals (v2.1.1)

- `LitVirtualizer` extends `LitElement` but uses `createRenderRoot() { return this }` (no shadow DOM).
- The actual work is done by a `Virtualizer` class, created by the `virtualize()` directive during `LitVirtualizer.render()`.
- The `Virtualizer` instance is stored on the host element via `hostElement[virtualizerRef]`.
- `LitVirtualizer.layoutComplete` delegates to `this[virtualizerRef]?.layoutComplete` — returns `undefined` if the Virtualizer hasn't been created yet.

### Virtualizer layout cycle

1. `connected()` → `_schedule(_updateLayout)` (deferred via microtask)
2. `_updateLayout()` → `_updateView()` (reads viewport bounds via `getBoundingClientRect`) → `layout.reflowIfNeeded()`
3. Layout `_reflow()` → `_getActiveItems()` → `_updateVisibleIndices()` → `_sendStateChangedMessage()`
4. `_handleLayoutMessage('stateChanged')` → `_updateDOM()`:
   - **`_notifyVisibility()`** → dispatches `visibilityChanged` event
   - **`_notifyRange()`** → dispatches `rangeChanged` event
   - **`_finishDOMUpdate()`**:
     - `_positionChildren()` — positions child elements
     - `_sizeHostElement()` — updates the sizer element (creates scrollable area)
     - `_correctScrollError()` — calls native `scrollTo` if there's a scroll error

**Critical**: `visibilityChanged` fires BEFORE `_finishDOMUpdate`. This means when the event handler runs, the sizer hasn't been updated yet and children haven't been positioned yet.

### Sizer element

The virtualizer creates scrollable area using an absolutely positioned hidden div with `style.transform = translate(Wpx, Hpx)`. This transform creates overflow that establishes `scrollHeight`. For scroller mode (`scroller=true`), this is the mechanism for scroll area.

### `scrollToIndex` internals

`scrollToIndex(index, 'start')` → `element(index).scrollIntoView({ block: 'start' })` → `_scrollElementIntoView`:
- If item is in rendered range: calls native `scrollIntoView()` on the DOM element (works immediately)
- If item is NOT in range: sets `this._layout.pin = options` → triggers async reflow via `_triggerReflow()` → `Promise.resolve().then(() => this.reflowIfNeeded())`

The pin-triggered reflow: `_setPositionFromPin()` → calculates target scroll position → `_scrollError` → `_sendStateChangedMessage()` → `_updateDOM()` → `_finishDOMUpdate()` → `_sizeHostElement()` + `_correctScrollError()` → `_nativeScrollTo()`.

**The sizer update and scrollTo happen in the same synchronous chain.** If the browser hasn't laid out the sizer yet, `scrollTo` may be clamped to the (incorrect) current `scrollHeight`.

### `layoutComplete` internals

- **Lazily created**: accessing `layoutComplete` creates a promise if one doesn't exist
- **Resolved by `_scheduleLayoutComplete()`** which is called from `_childrenSizeChanged` (ResizeObserver callback)
- **Uses internal double-rAF**: `requestAnimationFrame(() => requestAnimationFrame(() => resolve()))`
- **After resolution**: `_resetLayoutCompleteState()` nulls out the promise (next access creates a fresh one)
- `_scheduleLayoutComplete` only resolves if `_layoutCompletePromise` is non-null AND `_pendingLayoutComplete` is null

### Flow layout vs Grid layout

**Flow layout** (`track-list`):
- Computes item positions as `index * delta` — doesn't need cross-axis viewport width
- Works immediately on first layout cycle
- First `visibilityChanged` has real item indices

**Grid layout** (`cover-grid`):
- Needs viewport width to compute number of columns (`rolumns`)
- Viewport width comes from `_updateView()` → `getBoundingClientRect()`, but on first cycle the element may have zero width
- When `_viewDim2 <= 0` (no width), `rolumns = 0`, `_first = -1`, `_last = -1`
- `_getItemPosition` divides by `rolumns` — division by 0 when columns=0 produces `Infinity`
- First `visibilityChanged` is premature: `first: 0, last: 0` (defaults from BaseLayout constructor, since `_updateVisibleIndices` returns early when `_first === -1`)
- Real layout happens after ResizeObserver reports viewport width → second reflow → second `visibilityChanged` with real indices

### Browser frame order

1. JavaScript execution (microtasks, macrotasks)
2. ResizeObserver callbacks
3. `requestAnimationFrame` callbacks
4. Style/Layout calculation
5. Paint

## What Has Been Tried

### Approach 1: `scrollTop` pixel offset with `await updateComplete` + double-rAF
**Result**: Failed for both components.
**Why**: `await this.updateComplete` only waits for the parent Lit component's render. The `LitVirtualizer` child element exists in the DOM but hasn't completed its own Lit render cycle — the `Virtualizer` instance doesn't exist yet. The double-rAF fires too early.

### Approach 2: `scrollTop` pixel offset with `await updateComplete` + `await layoutComplete`
**Result**: Failed for both components.
**Why**: After `await this.updateComplete`, `this.virtualizer.layoutComplete` returns `undefined` because `virtualizerRef` hasn't been set yet (LitVirtualizer hasn't rendered). `await undefined` resolves immediately.

### Approach 3: Index-based save/restore via `visibilityChanged` event + immediate `scrollToIndex`
**Result**: Track-list worked. Cover-grid did not.
**Why track-list worked**: Flow layout has real items on first `visibilityChanged`. `scrollToIndex` sets a pin, the reflow works correctly.
**Why cover-grid failed**: First `visibilityChanged` is premature (0 columns). `scrollToIndex` sets pin, but reflow with 0 columns produces garbage positions (division by 0).

### Approach 4: `visibilityChanged` + `scrollHeight > clientHeight` guard + `layoutComplete?.then` + `scrollToIndex`
**Result**: Cover-grid partially worked (scrolled to correct position after one manual scroll, not on initial load). Track-list worked but with a flash.
**Why cover-grid failed**: `visibilityChanged` fires BEFORE `_finishDOMUpdate` updates the sizer. So `scrollHeight` reflects the PREVIOUS state (premature layout with scrollSize=1). The guard `scrollHeight <= clientHeight` was always true during the visibilityChanged handler, causing every event to be skipped. Only after a manual scroll (which triggers a fresh `visibilityChanged` with updated DOM state) did it work.
**Why track-list flashed**: `layoutComplete` uses internal double-rAF, so the scroll happens 2 frames after the initial render at position 0.

### Approach 5: `visibilityChanged` + `scrollHeight > clientHeight` guard removed + `layoutComplete?.then` + `scrollToIndex`
**Result**: Cover-grid worked but with flash. Track-list worked but with flash.
**Why it flashed**: The double-rAF delay in `layoutComplete` means 2 frames render at position 0 before scrolling.

### Approach 6: `visibilityChanged` + `requestAnimationFrame` + `scrollToIndex` (no guard for cover-grid)
**Result**: Track-list worked without flash. Cover-grid did not work at all.
**Why track-list worked**: Flow layout has real items immediately. rAF fires after sizer is set. `scrollToIndex` works.
**Why cover-grid failed**: First `visibilityChanged` is premature (0 columns). `hasRestoredScroll` set to true. rAF fires, but grid still has 0 columns → pin fails. Restore opportunity consumed.

### Approach 7: `visibilityChanged` + `last <= 0` guard for cover-grid + `requestAnimationFrame` + `scrollToIndex`
**Result**: Track-list worked without flash. Cover-grid did not work.
**Why cover-grid failed**: The `last <= 0` guard correctly skips the premature event. The second `visibilityChanged` (real layout, `last > 0`) triggers the handler. `hasRestoredScroll = true`, schedules rAF. But in the rAF callback, `scrollToIndex` → pin → reflow → `_sizeHostElement` + `_correctScrollError` → `scrollTo`. The sizer was updated in `_finishDOMUpdate` (same JS execution context as the `visibilityChanged`), but the browser hasn't processed it into `scrollHeight` yet when `scrollTo` is called inside the pin-triggered reflow. The `scrollTo` is clamped to the old (small) scrollHeight.

**Key insight**: For cover-grid, even after waiting for the real `visibilityChanged`, the pin mechanism's synchronous reflow chain does `_sizeHostElement` + `scrollTo` atomically. The browser never gets a chance to process the sizer into `scrollHeight` between these two operations. This is why `requestAnimationFrame` alone isn't enough for cover-grid — the problem isn't WHEN we call `scrollToIndex`, it's that `scrollToIndex`'s internal reflow always does sizer+scroll atomically.

## Current State of Code

The current code has:
- `track-list.ts`: `visibilityChanged` handler with `requestAnimationFrame` + `scrollToIndex` (works without flash)
- `cover-grid.ts`: `visibilityChanged` handler with `last <= 0` guard + `requestAnimationFrame` + `scrollToIndex` (does NOT work)

## Untried Ideas

1. **Bypass `scrollToIndex` entirely for cover-grid**: After `layoutComplete` resolves (sizer is painted), directly set `el.scrollTop` instead of `scrollToIndex`. This avoids the pin mechanism's atomic sizer+scroll problem. The virtualizer will react to the scroll event and re-render items at the new position.

2. **Use `scrollToIndex` on the SECOND `visibilityChanged` after the real one**: The first real `visibilityChanged` triggers `_finishDOMUpdate` which sets the sizer. After the browser paints (next frame), `scrollHeight` is correct. If we could delay to the next `visibilityChanged`... but there might not be one without user interaction.

3. **Pre-set the pin before the virtualizer initializes**: If we could inject the pin into the layout before the first `_updateLayout` runs, the virtualizer would start at the correct position. But the layout's pin setter is internal.

4. **Use `element(index).scrollIntoView()` when the item IS in range**: After `layoutComplete`, if the target item happens to be in the rendered range, native `scrollIntoView` works. But for distant items it won't be in range.

5. **Two-phase for cover-grid**: Use `layoutComplete?.then` to wait for sizer to be painted, then set `scrollTop` directly (not `scrollToIndex`). Calculate pixel offset: `offset = padding + Math.floor(index / columns) * (itemHeight + gap)`. Grid config is known: `itemSize: 230px height, gap: 16px, padding: 16px`. Columns can be derived from viewport width: `columns = Math.floor((viewportWidth - padding*2 + gap) / (itemWidth + gap))`. This is fragile but would work.

6. **For cover-grid, after `layoutComplete` resolves, use `requestAnimationFrame` + direct `scrollTop`**: `layoutComplete` ensures sizer is painted → `scrollHeight` is correct. Then rAF + `el.scrollTop = computedOffset` avoids the pin mechanism entirely. The virtualizer reacts to the scroll event.

7. **Hybrid approach**: Track-list uses `requestAnimationFrame` + `scrollToIndex` (works). Cover-grid uses `layoutComplete?.then` + direct `scrollTop` (avoids pin, avoids flash since we set scrollTop before paint in the rAF following layoutComplete... actually layoutComplete already used double-rAF so there would still be a flash).

## File Locations

- `frontend/src/store/library-store.ts` — LibraryStore singleton
- `frontend/src/store/controllers/library-controller.ts` — LibraryController
- `frontend/src/components/track-list/track-list.ts` — Track list component
- `frontend/src/components/cover-grid/cover-grid.ts` — Cover grid component
- `frontend/src/events.ts` — Frontend event constants
- `backend/events/events.go` — Backend event constants
- `backend/library/library.go` — Emits LibraryScanComplete
- `frontend/node_modules/@lit-labs/virtualizer/` — Virtualizer source (v2.1.1)

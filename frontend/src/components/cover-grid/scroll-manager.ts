import type { LitElement } from 'lit';
import type { LitVirtualizer } from '@lit-labs/virtualizer';
import type { library } from '@go/models';
import type { LibraryController } from '@store/controllers/library-controller';

import type { GridEntry } from './cover-grid-types.js';

/**
 * Grid spacing constants shared between the scroll
 * manager and the host component.
 */
export interface GridConstants {
    readonly GRID_GAP: number;
    readonly GRID_PADDING: number;
}

/**
 * Read-only interface into the cover-grid component
 * that the scroll manager needs.
 */
export interface ScrollManagerHost extends LitElement {
    readonly libraryCtrl: LibraryController;
    readonly cachedFilteredAlbums: library.Album[];
    readonly expandedAlbumId: number | null;
    readonly expandedTracks: library.Track[];
    readonly splitMode: boolean;
    readonly splitIndex: number;
    readonly cardWidth: number;
    readonly cardHeight: number;
}

/**
 * Manages scroll position persistence, resize-aware
 * scroll preservation, transition overlays, and
 * split/single mode geometry for the cover grid.
 *
 * This is a plain class (not a ReactiveController)
 * because scroll management is imperative and async,
 * not reactive.
 */
export class ScrollManager {
    private host: ScrollManagerHost;
    private gc: GridConstants;

    // RAF-throttled scroll position saving.
    private scrollRAFId: number | null = null;

    // Resize-aware scroll preservation.
    private resizeObserver: ResizeObserver | null = null;
    private resizeDebounceTimer: ReturnType<
        typeof setTimeout
    > | null = null;
    private pendingFocus: {
        albumIndex: number;
        viewportOffset: number;
    } | null = null;
    private currentColumnCount = 0;

    /** True while a resize reflow is in progress. */
    isResizing = false;

    // Scroll restoration across single/split mode
    // transitions.
    savedScrollTop = 0;
    needsScrollRestore = false;
    showDropdownAfterRestore = false;

    /**
     * Monotonically increasing counter used to cancel
     * stale scroll-restore async blocks.
     */
    private scrollRestoreGeneration = 0;

    /**
     * Set to the generation value when an async
     * scroll-restore block finishes or is cancelled.
     */
    private scrollRestoreResolved = 0;

    /**
     * When switching albums, the pixel distance from
     * the newly-expanded album's top edge to the
     * viewport top.
     */
    savedAlbumViewportOffset: number | null = null;

    /** Overlay element showing the old grid state
     *  while a mode transition is in flight. */
    private transitionOverlay: HTMLDivElement | null =
        null;

    /** Cached index of the expanded album in the
     *  filtered list.  -1 when no album is expanded
     *  or the album isn't in the filtered list. */
    private expandedAlbumIndex = -1;

    /** The expanded album ID that corresponds to the
     *  cached index.  Used to detect invalidation. */
    private expandedAlbumIndexId: number | null = null;

    /** The filtered-albums reference used to compute
     *  the cached index.  Used to detect invalidation. */
    private expandedAlbumIndexAlbums:
        library.Album[] = [];

    constructor(
        host: ScrollManagerHost,
        gc: GridConstants,
    ) {
        this.host = host;
        this.gc = gc;
    }

    // ================================================================
    // Expanded album index cache (improvement 6c)
    // ================================================================

    /**
     * Return the index of the expanded album in the
     * filtered list.  Cached and invalidated when
     * `expandedAlbumId` or `cachedFilteredAlbums`
     * changes.
     */
    getExpandedAlbumIndex(): number {
        const id = this.host.expandedAlbumId;
        const albums = this.host.cachedFilteredAlbums;

        if (
            id === this.expandedAlbumIndexId &&
            albums === this.expandedAlbumIndexAlbums
        ) {
            return this.expandedAlbumIndex;
        }

        this.expandedAlbumIndexId = id;
        this.expandedAlbumIndexAlbums = albums;

        if (id === null) {
            this.expandedAlbumIndex = -1;
        } else {
            this.expandedAlbumIndex = albums.findIndex(
                (a) => a.ID === id,
            );
        }

        return this.expandedAlbumIndex;
    }

    // ================================================================
    // Lifecycle
    // ================================================================

    /** Clean up timers and observers. */
    teardown(): void {
        if (this.scrollRAFId !== null) {
            cancelAnimationFrame(this.scrollRAFId);
        }

        if (this.resizeDebounceTimer !== null) {
            clearTimeout(this.resizeDebounceTimer);
        }

        this.resizeObserver?.disconnect();
        this.resizeObserver = null;
        this.removeOverlay();
    }

    // ================================================================
    // Scroll position (index-based)
    // ================================================================

    /**
     * Restore scroll position from the library store
     * after initial album load.
     */
    restoreScrollPosition(
        virtualizer: LitVirtualizer | undefined,
    ): void {
        const saved =
            this.host.libraryCtrl.getScrollPosition(
                'albums',
            );

        if (saved <= 0 || !virtualizer) return;

        const safeIndex = Math.min(
            saved,
            this.host.cachedFilteredAlbums.length - 1,
        );

        if (safeIndex <= 0) return;

        virtualizer.scrollToIndex(safeIndex, 'start');
    }

    /**
     * Save scroll position from the first visible album.
     * In split mode we use the before-entries; in single
     * mode we use the full grid entries.
     *
     * Uses requestAnimationFrame throttling: saves at most
     * once per frame (~16ms at 60fps). Unlike debouncing,
     * this captures position continuously during scrolling
     * (not just after it stops) and naturally aligns with
     * the browser's paint cycle.
     */
    onVisibilityChanged(
        first: number,
        getEntries: () => GridEntry[],
    ): void {
        if (this.isResizing) return;

        if (this.scrollRAFId !== null) return;

        this.scrollRAFId = requestAnimationFrame(
            () => {
                this.scrollRAFId = null;

                const entries = getEntries();
                const entry = entries[first];

                if (entry) {
                    this.host.libraryCtrl.setScrollPosition(
                        'albums',
                        entry.albumIndex,
                    );
                }
            },
        );
    }

    // ================================================================
    // Resize-aware scroll preservation
    // ================================================================

    /**
     * Set up a ResizeObserver on the scroll container
     * to preserve scroll position across width changes.
     */
    setupResizeObserver(
        container: HTMLElement,
        onSplitResize: () => Promise<void>,
    ): void {
        // Guard against stacked observers.
        this.resizeObserver?.disconnect();

        // An empty library renders no scroll container, so the caller's
        // query returns undefined and observe() throws — asynchronously,
        // out of loadAlbums, where nothing catches it.
        if (!container) return;

        this.currentColumnCount =
            this.getColumnCount(container);

        const restoreScroll = () => {
            const pending = this.pendingFocus;

            this.pendingFocus = null;
            this.isResizing = false;

            if (!pending) return;

            const newColumns =
                this.getColumnCount(container);
            this.currentColumnCount = newColumns;

            // If a dropdown is open, delegate to the
            // host for split recomputation.
            if (
                this.host.splitMode &&
                this.host.expandedAlbumId !== null
            ) {
                void onSplitResize();

                return;
            }

            const gap = this.gc.GRID_GAP;
            const pad = this.gc.GRID_PADDING;
            const rowStep =
                this.host.cardHeight + gap;

            const newRow = Math.floor(
                pending.albumIndex / newColumns,
            );
            const newY = pad + newRow * rowStep;

            container.scrollTop =
                newY - pending.viewportOffset;
        };

        this.resizeObserver = new ResizeObserver(
            () => {
                const rowStep =
                    this.host.cardHeight +
                    this.gc.GRID_GAP;

                if (this.pendingFocus === null) {
                    this.isResizing = true;
                    this.captureFocusPoint(
                        container,
                        rowStep,
                    );
                }

                const newColumns =
                    this.getColumnCount(container);

                if (
                    newColumns !==
                    this.currentColumnCount
                ) {
                    if (
                        this.resizeDebounceTimer !==
                        null
                    ) {
                        clearTimeout(
                            this.resizeDebounceTimer,
                        );
                        this.resizeDebounceTimer =
                            null;
                    }

                    restoreScroll();

                    return;
                }

                if (
                    this.resizeDebounceTimer !== null
                ) {
                    clearTimeout(
                        this.resizeDebounceTimer,
                    );
                }

                this.resizeDebounceTimer = setTimeout(
                    restoreScroll,
                    100,
                );
            },
        );

        this.resizeObserver.observe(container);
    }

    /**
     * Determine the focus point for scroll restoration.
     */
    private captureFocusPoint(
        container: HTMLElement,
        rowStep: number,
    ): void {
        const pad = this.gc.GRID_PADDING;
        const cols = this.currentColumnCount;
        const filtered =
            this.host.cachedFilteredAlbums;

        // Prefer the expanded album as focus.
        if (this.host.expandedAlbumId !== null) {
            const idx = this.getExpandedAlbumIndex();

            if (idx >= 0) {
                const albumRow = Math.floor(
                    idx / cols,
                );
                const albumY =
                    pad + albumRow * rowStep;

                this.pendingFocus = {
                    albumIndex: idx,
                    viewportOffset:
                        albumY - container.scrollTop,
                };

                return;
            }
        }

        const centerY =
            container.scrollTop +
            container.clientHeight / 2;
        const centerRow = Math.floor(
            Math.max(0, centerY - pad) / rowStep,
        );
        const albumIndex = Math.min(
            centerRow * cols,
            Math.max(0, filtered.length - 1),
        );

        const albumY = pad + centerRow * rowStep;

        this.pendingFocus = {
            albumIndex,
            viewportOffset:
                albumY - container.scrollTop,
        };
    }

    // ================================================================
    // Column count / geometry helpers
    // ================================================================

    /**
     * Compute the number of columns that fit in the
     * given container.
     */
    getColumnCount(
        container?: HTMLElement,
    ): number {
        if (!container) return 1;

        const gap = this.gc.GRID_GAP;
        const pad = this.gc.GRID_PADDING;
        const availableWidth =
            container.clientWidth - pad * 2;

        return Math.max(
            1,
            Math.floor(
                (availableWidth + gap) /
                    (this.host.cardWidth + gap),
            ),
        );
    }

    /** Container width in pixels. */
    getContainerWidth(
        container?: HTMLElement,
    ): number {
        return container?.clientWidth ?? 800;
    }

    /**
     * Width of the album row (left of leftmost card to
     * right of rightmost card).
     */
    getGridRowWidth(
        container?: HTMLElement,
    ): number {
        const cols = this.getColumnCount(container);
        const gap = this.gc.GRID_GAP;

        return (
            cols * this.host.cardWidth +
            (cols - 1) * gap
        );
    }

    /**
     * Horizontal offset of the carat so it points at
     * the center of the expanded album card.
     */
    getCaratOffset(
        container?: HTMLElement,
    ): number {
        const idx = this.getExpandedAlbumIndex();

        if (idx < 0) return 0;

        const cols = this.getColumnCount(container);
        const colIndex = idx % cols;
        const gap = this.gc.GRID_GAP;

        return (
            colIndex *
                (this.host.cardWidth + gap) +
            this.host.cardWidth / 2
        );
    }

    // ================================================================
    // Split-mode helpers
    // ================================================================

    /**
     * Compute the split point and return it.  The
     * component assigns this to its `splitIndex` state.
     */
    computeSplitIndex(
        container?: HTMLElement,
    ): number {
        const filtered =
            this.host.cachedFilteredAlbums;

        const idx = this.getExpandedAlbumIndex();

        if (idx < 0) return filtered.length;

        const columns =
            this.getColumnCount(container);

        return Math.min(
            (Math.floor(idx / columns) + 1) * columns,
            filtered.length,
        );
    }

    // ================================================================
    // Transition overlay
    // ================================================================

    /**
     * Capture the current scroll container as a static
     * overlay.
     */
    captureOverlay(
        container: HTMLElement | undefined,
        shadowRoot: ShadowRoot | null,
    ): void {
        if (!container || this.transitionOverlay) {
            return;
        }

        const scrollY = container.scrollTop;
        const overlay = document.createElement('div');

        overlay.style.cssText =
            'position:absolute;inset:0;z-index:10;' +
            'overflow:hidden;pointer-events:none;';

        const inner = document.createElement('div');

        inner.style.cssText =
            'position:relative;height:100%;' +
            'pointer-events:none;';

        for (const child of Array.from(
            container.childNodes,
        )) {
            inner.appendChild(child.cloneNode(true));
        }

        inner.style.transform =
            `translateY(-${scrollY}px)`;

        overlay.appendChild(inner);
        shadowRoot?.appendChild(overlay);
        this.transitionOverlay = overlay;

        container.style.visibility = 'hidden';
    }

    /**
     * Remove the snapshot overlay and reveal the real
     * scroll container.
     */
    removeOverlay(): void {
        if (this.transitionOverlay) {
            this.transitionOverlay.remove();
            this.transitionOverlay = null;
        }
    }

    /**
     * Reveal the real scroll container (call separately
     * when the overlay has already been removed or was
     * never created).
     */
    revealContainer(
        container: HTMLElement | undefined,
    ): void {
        if (container) {
            container.style.visibility = '';
        }
    }

    // ================================================================
    // Dropdown scroll positioning
    // ================================================================

    /**
     * Wait for the "before" virtualizer to finish its
     * layout pass.
     */
    async awaitBeforeLayout(
        shadowRoot: ShadowRoot | null,
    ): Promise<void> {
        const virt = shadowRoot?.querySelector(
            '#grid-before',
        ) as LitVirtualizer | null;

        await virt?.layoutComplete;
    }

    /**
     * Return the current scrollTop converted to
     * single-mode (dropdown-free) coordinates.
     */
    computeAdjustedScrollTop(
        container: HTMLElement | undefined,
        shadowRoot: ShadowRoot | null,
    ): number {
        if (!container) return 0;

        const raw = container.scrollTop;

        if (!this.host.splitMode) return raw;

        const gap = this.gc.GRID_GAP;
        const pad = this.gc.GRID_PADDING;
        const columns =
            this.getColumnCount(container);
        const rowStep = this.host.cardHeight + gap;
        const beforeRows = Math.ceil(
            this.host.splitIndex / columns,
        );

        const dropdownTop =
            pad + beforeRows * rowStep;

        if (raw <= dropdownTop) return raw;

        const dropdown = shadowRoot?.querySelector(
            'album-dropdown',
        );
        const dropdownHeight =
            (dropdown as HTMLElement)?.offsetHeight ??
            0;

        return raw - dropdownHeight;
    }

    /**
     * Set scrollTop on the scroll container with
     * retry logic for virtualizer expansion.
     */
    async restoreScrollTop(
        container: HTMLElement | undefined,
        target: number,
    ): Promise<void> {
        if (!container) return;

        const maxAttempts = 10;

        for (let i = 0; i < maxAttempts; i++) {
            container.scrollTop = target;

            if (
                container.scrollTop >= target ||
                target <= 0
            ) {
                return;
            }

            await new Promise<void>((r) =>
                requestAnimationFrame(() => r()),
            );
        }

        console.warn(
            '[restoreScrollTop] gave up after max attempts',
            {
                target,
                actual: container.scrollTop,
                scrollHeight: container.scrollHeight,
            },
        );
    }

    /**
     * Scroll the container so the expanded album card
     * and its dropdown are visible with minimal movement.
     */
    async scrollToShowDropdown(
        container: HTMLElement | undefined,
        shadowRoot: ShadowRoot | null,
    ): Promise<void> {
        if (
            !container ||
            this.host.expandedAlbumId === null
        ) {
            return;
        }

        const expandedIndex =
            this.getExpandedAlbumIndex();

        if (expandedIndex < 0) return;

        const gap = this.gc.GRID_GAP;
        const pad = this.gc.GRID_PADDING;
        const columns =
            this.getColumnCount(container);
        const rowStep = this.host.cardHeight + gap;
        const albumRow = Math.floor(
            expandedIndex / columns,
        );

        const albumTop =
            pad + albumRow * rowStep - gap / 2;

        const dropdown = shadowRoot?.querySelector(
            'album-dropdown',
        );

        if (!dropdown) return;

        await (dropdown as LitElement).updateComplete;

        const beforeRows = Math.ceil(
            this.host.splitIndex / columns,
        );
        const dropdownTop =
            pad + beforeRows * rowStep;
        const dropdownBottom =
            dropdownTop +
            (dropdown as HTMLElement).offsetHeight;

        const viewTop = container.scrollTop;
        const viewHeight = container.clientHeight;

        const minScroll = dropdownBottom - viewHeight;
        const maxScroll = albumTop;

        let newScrollTop: number;

        if (minScroll <= maxScroll) {
            newScrollTop = Math.max(
                minScroll,
                Math.min(viewTop, maxScroll),
            );
        } else {
            newScrollTop = albumTop;
        }

        if (newScrollTop !== viewTop) {
            await this.restoreScrollTop(
                container,
                newScrollTop,
            );
        }
    }

    // ================================================================
    // willUpdate / updated helpers
    //
    // Called from the component's lifecycle methods to
    // compute scroll-related state transitions.
    // ================================================================

    /**
     * Check whether a scroll-restore async block is
     * currently in flight.
     */
    get restoreInFlight(): boolean {
        return (
            this.scrollRestoreGeneration >
            this.scrollRestoreResolved
        );
    }

    /**
     * Prepare the anchor capture for an exit-split
     * transition when switching albums (not closing).
     * Records the viewport offset of the newly-expanded
     * album in the old split layout.
     */
    captureAnchorOffset(
        container: HTMLElement | undefined,
        shadowRoot: ShadowRoot | null,
    ): void {
        if (this.host.expandedAlbumId === null) {
            this.savedAlbumViewportOffset = null;

            return;
        }

        const rawScrollTop =
            container?.scrollTop ?? 0;
        const idx = this.getExpandedAlbumIndex();

        if (idx < 0) return;

        const gap = this.gc.GRID_GAP;
        const pad = this.gc.GRID_PADDING;
        const cols =
            this.getColumnCount(container);
        const rowStep = this.host.cardHeight + gap;
        const row = Math.floor(idx / cols);

        const albumY = pad + row * rowStep;

        const oldBeforeRows = Math.ceil(
            this.host.splitIndex / cols,
        );
        const oldDropdownTop =
            pad + oldBeforeRows * rowStep;
        const dropdown = shadowRoot?.querySelector(
            'album-dropdown',
        );
        const oldDropdownHeight =
            (dropdown as HTMLElement)?.offsetHeight ??
            0;

        const albumYOldSplit =
            albumY >= oldDropdownTop
                ? albumY + oldDropdownHeight
                : albumY;

        this.savedAlbumViewportOffset =
            albumYOldSplit - rawScrollTop;
    }

    /**
     * Run the async scroll-restore sequence from the
     * component's `updated()` callback.
     */
    runScrollRestore(
        container: HTMLElement | undefined,
        shadowRoot: ShadowRoot | null,
        expandedAlbumId: number | null,
        updateComplete: Promise<boolean>,
    ): void {
        this.needsScrollRestore = false;

        const saved = this.savedScrollTop;
        const showDropdown =
            this.showDropdownAfterRestore;

        const switching =
            !showDropdown &&
            expandedAlbumId !== null;

        const gen = ++this.scrollRestoreGeneration;

        void (async () => {
            await updateComplete;

            if (gen !== this.scrollRestoreGeneration) {
                this.scrollRestoreResolved = gen;

                return;
            }

            await this.restoreScrollTop(
                container,
                saved,
            );

            if (gen !== this.scrollRestoreGeneration) {
                this.scrollRestoreResolved = gen;

                return;
            }

            if (showDropdown) {
                if (
                    this.savedAlbumViewportOffset !==
                        null &&
                    expandedAlbumId !== null
                ) {
                    const idx =
                        this.getExpandedAlbumIndex();

                    if (idx >= 0) {
                        const gap = this.gc.GRID_GAP;
                        const pad =
                            this.gc.GRID_PADDING;
                        const cols =
                            this.getColumnCount(
                                container,
                            );
                        const rowStep =
                            this.host.cardHeight +
                            gap;
                        const row = Math.floor(
                            idx / cols,
                        );
                        const albumY =
                            pad + row * rowStep;
                        const anchor =
                            albumY -
                            this
                                .savedAlbumViewportOffset!;

                        await this.restoreScrollTop(
                            container,
                            anchor,
                        );
                    }

                    this.savedAlbumViewportOffset =
                        null;
                }

                if (
                    gen !==
                    this.scrollRestoreGeneration
                ) {
                    this.scrollRestoreResolved = gen;

                    return;
                }

                await this.scrollToShowDropdown(
                    container,
                    shadowRoot,
                );
            }

            if (gen !== this.scrollRestoreGeneration) {
                this.scrollRestoreResolved = gen;

                return;
            }

            if (!switching) {
                this.removeOverlay();
                this.revealContainer(container);
            }

            this.scrollRestoreResolved = gen;
        })();
    }
}

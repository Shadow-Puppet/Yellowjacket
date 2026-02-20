import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';
import '@lit-labs/virtualizer';
import type {
    LitVirtualizer,
    VisibilityChangedEvent,
} from '@lit-labs/virtualizer';
import { grid } from '@lit-labs/virtualizer/layouts/grid.js';
import { GetAlbumTracks } from '@go/library/Library';
import { library } from '@go/models';
import { LibraryController } from '@store/controllers/library-controller';
import { queueStore } from '@store/queue-store';
import { Events } from '../../events';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';
import './album-dropdown.js';
import type {
    TrackClickDetail,
    TrackDblClickDetail,
    TrackContextMenuDetail,
    TrackDragStartDetail,
} from './album-dropdown.js';
import {
    DRAG_MIME,
    setDragPayload,
    emitDragActive,
} from '@utils/drag-controller';
import type { DragPayload } from '@utils/drag-controller';
import {
    createDragImage,
    removeDragImage,
} from '@utils/drag-image';

/**
 * Discriminated context menu target so we know whether the
 * context-menu is operating on albums or on tracks inside the
 * dropdown.
 */
type ContextMenuTarget =
    | { kind: 'album' }
    | { kind: 'track' };

/**
 * Item for the virtualized grid.
 * Carries the original album and its index in this.albums.
 */
interface GridEntry {
    album: library.Album;
    albumIndex: number;
}

/** Milliseconds to debounce visibility-changed saves. */
const SCROLL_DEBOUNCE_MS = 100;

/** Pixels to change card width per scroll tick. */
const ZOOM_STEP = 16;

@customElement('cover-grid')
export class CoverGrid extends LitElement {
    private libraryCtrl = new LibraryController(this);
    private cancelScanComplete?: () => void;

    // Fixed grid spacing constants.
    private static readonly GRID_GAP = 8;
    private static readonly GRID_PADDING = 8;
    private static readonly CARD_PADDING = 5;

    private lastSelectedAlbumIndex: number | null = null;
    private lastSelectedTrackIndex: number | null = null;
    private scrollDebounceTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    private closeHandler = () => this.closeContextMenu();

    /** Current card width — driven by the store. */
    private get cardWidth(): number {
        return this.libraryCtrl.coverSize;
    }

    /**
     * Height of the text area below the cover image.
     * Two lines: album name (+ year) and artist.
     */
    private get cardTextHeight(): number {
        const w = this.cardWidth;

        if (w < 160) return 36;

        return w > 250 ? 46 : 40;
    }

    /** Derived card height from card width. */
    private get cardHeight(): number {
        return this.cardWidth + this.cardTextHeight;
    }

    /** Image size inside the card (card minus padding). */
    private get imageSize(): number {
        return this.cardWidth - CoverGrid.CARD_PADDING * 2;
    }

    // Virtualizer grid layout instance — recreated when
    // the card size changes.
    private gridLayout = this.createGridLayout();
    private gridLayoutWidth = 0;

    /**
     * Secondary layout for the "after" virtualizer in
     * split mode.  Uses zero top padding so there is no
     * extra gap between the dropdown and the first row.
     */
    private gridLayoutAfter = this.createGridLayout(
        true,
    );

    private createGridLayout(noTopPad = false) {
        const w = this.libraryCtrl?.coverSize ?? 176;

        if (!noTopPad) {
            this.gridLayoutWidth = w;
        }

        const h = w + this.cardTextHeight;
        const gap = CoverGrid.GRID_GAP;
        const pad = CoverGrid.GRID_PADDING;

        return grid({
            itemSize: {
                width: `${w}px`,
                height: `${h}px`,
            },
            gap: `${gap}px`,
            padding: noTopPad
                ? `0 ${pad}px ${pad}px`
                : `${pad}px`,
            justify: 'center',
        });
    }

    private dragImageEl: HTMLElement | null = null;

    /**
     * Pre-resolved file paths for selected albums, keyed by album ID.
     * Populated asynchronously when albums are selected so that
     * dragstart can read them synchronously.
     */
    private albumFilePathCache = new Map<
        number,
        string[]
    >();

    /** Wheel event handler ref for manual add/remove. */
    private wheelHandler = (e: WheelEvent) => {
        this.onWheel(e);
    };

    private wheelListenerAttached = false;

    // buildGridEntries() memoization cache.
    private entriesCacheAlbums: library.Album[] = [];
    private entriesCache: GridEntry[] = [];

    static override styles = css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
            position: relative;
        }

        .grid-scroll-container {
            flex: 1;
            position: relative;
            overflow-y: auto;
        }

        /* ========================================
         * Album card
         * ======================================== */

        .album-card {
            display: flex;
            flex-direction: column;
            cursor: pointer;
            border-radius: 8px;
            padding: 5px;
            transition: background-color 0.2s ease;
            box-sizing: border-box;
            width: var(--card-width, 176px);
        }

        .album-card:hover {
            background-color: rgba(255, 255, 255, 0.1);
        }

        .album-card.selected {
            outline: 2px solid #ffd43b;
            outline-offset: 2px;
        }

        .album-card.expanded {
            outline: 2px solid #ffd43b;
            outline-offset: 2px;
        }

        .album-card:focus-visible {
            outline: 2px solid #ffd43b;
            outline-offset: 2px;
        }

        .cover-container {
            position: relative;
            width: 100%;
            aspect-ratio: 1;
            border-radius: 4px;
            overflow: hidden;
            background-color: #282828;
        }

        .cover-image {
            width: 100%;
            height: 100%;
            object-fit: cover;
        }

        .placeholder-cover {
            width: 100%;
            height: 100%;
            display: flex;
            align-items: center;
            justify-content: center;
            background: linear-gradient(
                135deg,
                #404040 0%,
                #282828 100%
            );
            color: #b3b3b3;
            font-size: var(--placeholder-font, 48px);
        }

        .album-info {
            margin-top: 4px;
            min-width: 0;
            text-align: center;
        }

        .album-name {
            font-size: var(--album-name-font, 14px);
            font-weight: 400;
            color: #fff;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .artist-name {
            font-size: var(--artist-name-font, 12px);
            color: #b3b3b3;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            margin-top: 2px;
        }

        .album-year {
            color: #888;
        }

        /* ========================================
         * Shared states
         * ======================================== */

        .loading {
            display: flex;
            justify-content: center;
            align-items: center;
            padding: 32px;
            color: #b3b3b3;
        }

        .empty-state {
            display: flex;
            flex-direction: column;
            justify-content: center;
            align-items: center;
            padding: 48px;
            color: #b3b3b3;
            text-align: center;
        }

        .empty-state p {
            margin: 8px 0;
        }

        /* ========================================
         * Context menu
         * ======================================== */

        #context-menu {
            z-index: 200;
        }

        .context-menu-panel {
            background-color: #343a40;
            border: 1px solid #444;
            border-radius: 6px;
            padding: 4px 0;
            box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
            min-width: 160px;
        }

        .context-menu-panel wa-dropdown-item {
            cursor: pointer;
        }

        .context-menu-panel wa-dropdown-item {
            --wa-color-text-normal: #fff;
            font-size: 13px;
        }

        .context-menu-panel wa-dropdown-item:hover {
            background-color: rgba(
                255,
                255,
                255,
                0.1
            );
        }

        .submenu-item {
            position: relative;
        }

        .submenu-arrow {
            font-size: 10px;
            margin-left: auto;
            padding-left: 12px;
        }

        #playlist-submenu {
            z-index: 210;
        }
    `;

    /* ====================================================================
     * Reactive state
     * ==================================================================== */

    @state()
    private albums: library.Album[] = [];

    @state()
    private loading = true;

    @state()
    private contextMenuOpen = false;

    @state()
    private contextMenuTarget: ContextMenuTarget = {
        kind: 'album',
    };

    @state()
    private selectedAlbums: Set<number> = new Set();

    @state()
    private playlistSubmenuOpen = false;

    @state()
    private playlistFilePaths: string[] = [];

    /** ID of the album whose dropdown is currently open, or null. */
    @state()
    private expandedAlbumId: number | null = null;

    /** Tracks loaded for the expanded album dropdown. */
    @state()
    private expandedTracks: library.Track[] = [];

    /** Set of file paths of selected tracks inside the dropdown. */
    @state()
    private selectedTracks: Set<string> = new Set();

    /**
     * True when using the dual-virtualizer layout
     * (dropdown sandwiched between two grids).
     */
    @state()
    private splitMode = false;

    /**
     * Index into this.albums where the split occurs.
     * Albums [0, splitIndex) go into the "before"
     * virtualizer; [splitIndex, length) go into "after".
     */
    @state()
    private splitIndex = 0;

    @query('#context-menu')
    private contextMenuPopup!: HTMLElement;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: HTMLElement;

    @query('#grid-single')
    private virtualizerSingle!: LitVirtualizer;

    @query('.grid-scroll-container')
    private scrollContainer!: HTMLElement;

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
    private isResizing = false;

    // Scroll restoration across single/split mode
    // transitions.
    private savedScrollTop = 0;
    private needsScrollRestore = false;
    private showDropdownAfterRestore = false;

    /**
     * Monotonically increasing counter used to
     * cancel stale scroll-restore async blocks.
     * Each new restore bumps the generation; the
     * async block bails out when it detects it is
     * no longer current.
     */
    private scrollRestoreGeneration = 0;

    /**
     * Set to the generation value when an async
     * scroll-restore block finishes or is cancelled.
     * When scrollRestoreGeneration >
     * scrollRestoreResolved, an async restore is
     * still in flight and the DOM scrollTop may be
     * unreliable.
     */
    private scrollRestoreResolved = 0;

    /**
     * When switching albums, the pixel distance from
     * the newly-expanded album's top edge to the
     * viewport top — computed in single-mode
     * coordinates during exit-split.  Used by the
     * enter-split restore to place the album at the
     * same visual position before scrollToShowDropdown
     * makes any further adjustments.
     */
    private savedAlbumViewportOffset: number | null =
        null;

    /** Overlay element showing the old grid state
     *  while a mode transition is in flight. */
    private transitionOverlay: HTMLDivElement | null =
        null;

    /* ====================================================================
     * Lifecycle
     * ==================================================================== */

    override connectedCallback() {
        super.connectedCallback();
        this.loadAlbums();
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            () => this.loadAlbums(),
        );
        document.addEventListener(
            'click',
            this.closeHandler,
        );
        document.addEventListener(
            'contextmenu',
            this.closeHandler,
        );

        // error events do not bubble — use capture
        // phase to catch <img> load failures.
        this.addEventListener(
            'error',
            this.onGridImageError,
            true,
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.cancelScanComplete?.();
        document.removeEventListener(
            'click',
            this.closeHandler,
        );
        document.removeEventListener(
            'contextmenu',
            this.closeHandler,
        );
        this.removeEventListener(
            'error',
            this.onGridImageError,
            true,
        );
        this.scrollContainer?.removeEventListener(
            'wheel',
            this.wheelHandler,
        );
        this.wheelListenerAttached = false;

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
        }

        if (this.resizeDebounceTimer !== null) {
            clearTimeout(this.resizeDebounceTimer);
        }

        this.resizeObserver?.disconnect();
        this.resizeObserver = null;
        this.removeOverlay();
    }

    override willUpdate(
        changed: Map<PropertyKey, unknown>,
    ) {
        super.willUpdate(changed);

        // When switching albums, expandedTracks
        // briefly goes to [].  Exit split mode so
        // the single virtualizer takes over while
        // loading.
        if (
            changed.has('expandedTracks') &&
            this.expandedTracks.length === 0 &&
            this.splitMode
        ) {
            // Capture the raw split-mode scrollTop
            // before converting to single-mode coords.
            const rawScrollTop =
                this.scrollContainer?.scrollTop ?? 0;

            // Convert to dropdown-free coordinates
            // before exiting split mode.
            this.savedScrollTop =
                this.computeAdjustedScrollTop();

            // If switching to a new album (not
            // closing), record the viewport offset
            // of the newly-expanded album in the
            // OLD split layout so the enter-split
            // restore can place it at the same
            // visual position.
            if (this.expandedAlbumId !== null) {
                const idx = this.albums.findIndex(
                    (a) =>
                        a.ID ===
                        this.expandedAlbumId,
                );

                if (idx >= 0) {
                    const gap = CoverGrid.GRID_GAP;
                    const pad =
                        CoverGrid.GRID_PADDING;
                    const cols =
                        this.getColumnCount();
                    const rowStep =
                        this.cardHeight + gap;
                    const row = Math.floor(
                        idx / cols,
                    );

                    // Album's Y in single-mode
                    // (no dropdown) coordinates.
                    const albumY =
                        pad + row * rowStep;

                    // In the old split layout the
                    // dropdown shifts everything
                    // below it.  Determine if
                    // album B sits below the old
                    // dropdown.
                    const oldBeforeRows = Math.ceil(
                        this.splitIndex / cols,
                    );
                    const oldDropdownTop =
                        pad + oldBeforeRows * rowStep;
                    const dropdown =
                        this.shadowRoot?.querySelector(
                            'album-dropdown',
                        );
                    const oldDropdownHeight =
                        (dropdown as HTMLElement)
                            ?.offsetHeight ?? 0;

                    const albumYOldSplit =
                        albumY >= oldDropdownTop
                            ? albumY +
                              oldDropdownHeight
                            : albumY;

                    // Viewport offset in old-split
                    // coordinates: use the raw
                    // (un-adjusted) scrollTop.
                    this.savedAlbumViewportOffset =
                        albumYOldSplit - rawScrollTop;

                    console.log(
                        '[willUpdate] anchor capture',
                        {
                            albumY,
                            oldDropdownTop,
                            oldDropdownHeight,
                            albumYOldSplit,
                            rawScrollTop,
                            offset: this
                                .savedAlbumViewportOffset,
                        },
                    );
                }
            } else {
                this.savedAlbumViewportOffset =
                    null;
            }

            console.log(
                '[willUpdate] exit split (tracks empty)',
                {
                    savedScrollTop:
                        this.savedScrollTop,
                    savedAlbumViewportOffset:
                        this.savedAlbumViewportOffset,
                    expandedAlbumId:
                        this.expandedAlbumId,
                },
            );

            this.captureOverlay();
            this.splitMode = false;
            this.needsScrollRestore = true;
            this.showDropdownAfterRestore = false;
        }

        // Enter split mode when tracks have loaded.
        if (
            changed.has('expandedTracks') &&
            this.expandedAlbumId !== null &&
            this.expandedTracks.length > 0
        ) {
            // If a restore is already in flight
            // (switching albums), keep the saved
            // value — the DOM scrollTop may still
            // be clamped to 0 because the previous
            // restore hasn't finished.
            const restoreInFlight =
                this.scrollRestoreGeneration >
                this.scrollRestoreResolved;

            if (!restoreInFlight) {
                this.savedScrollTop =
                    this.scrollContainer
                        ?.scrollTop ?? 0;
            }

            console.log(
                '[willUpdate] enter split',
                {
                    savedScrollTop:
                        this.savedScrollTop,
                    restoreInFlight,
                    expandedAlbumId:
                        this.expandedAlbumId,
                    splitIndex: this.splitIndex,
                    scrollHeight:
                        this.scrollContainer
                            ?.scrollHeight,
                },
            );

            this.captureOverlay();
            this.computeSplitIndex();
            this.splitMode = true;
            this.needsScrollRestore = true;
            this.showDropdownAfterRestore = true;
        }

        // Exit split mode when the dropdown closes.
        if (
            changed.has('expandedAlbumId') &&
            this.expandedAlbumId === null &&
            this.splitMode
        ) {
            // Convert to dropdown-free coordinates
            // before exiting split mode.
            this.savedScrollTop =
                this.computeAdjustedScrollTop();
            this.savedAlbumViewportOffset = null;

            console.log(
                '[willUpdate] exit split (close)',
                {
                    savedScrollTop:
                        this.savedScrollTop,
                },
            );

            this.captureOverlay();
            this.splitMode = false;
            this.needsScrollRestore = true;
            this.showDropdownAfterRestore = false;
        }
    }

    override updated(
        changed: Map<PropertyKey, unknown>,
    ) {
        super.updated(changed);

        // Ctrl+Scroll zoom — lazily attach to the
        // scroll container once it exists in the DOM.
        if (
            !this.wheelListenerAttached &&
            this.scrollContainer
        ) {
            this.scrollContainer.addEventListener(
                'wheel',
                this.wheelHandler,
                { passive: false },
            );
            this.wheelListenerAttached = true;
        }

        // Recreate the virtualizer grid layout when
        // the card size changes.
        const cardSizeChanged =
            this.gridLayoutWidth !== this.cardWidth;

        if (cardSizeChanged) {
            this.gridLayout = this.createGridLayout();
            this.gridLayoutAfter =
                this.createGridLayout(true);
        }

        // Apply CSS custom properties for dynamic sizing.
        this.updateSizeProperties();

        // Restore scroll after a single/split mode
        // transition (set in willUpdate).  Uses a
        // retry loop (restoreScrollTop) so the scroll
        // position is applied reliably even if the
        // virtualizer hasn't expanded its host height
        // yet.
        if (this.needsScrollRestore) {
            this.needsScrollRestore = false;

            const saved = this.savedScrollTop;
            const showDropdown =
                this.showDropdownAfterRestore;

            // Capture whether an album is still
            // selected — if so and !showDropdown, we
            // are in the brief split→single gap while
            // new tracks load (album switch).  Keep
            // the overlay visible until the new split
            // view is ready.
            const switching =
                !showDropdown &&
                this.expandedAlbumId !== null;

            // Bump the generation so any in-flight
            // async restore from a previous cycle
            // will bail out.
            const gen =
                ++this.scrollRestoreGeneration;

            console.log(
                '[updated] scroll restore start',
                {
                    saved,
                    showDropdown,
                    switching,
                    gen,
                },
            );

            void (async () => {
                await this.updateComplete;

                if (
                    gen !==
                    this.scrollRestoreGeneration
                ) {
                    console.log(
                        `[updated] gen ${gen} stale, aborting`,
                    );
                    this.scrollRestoreResolved = gen;

                    return;
                }

                console.log(
                    '[updated] after updateComplete',
                    {
                        scrollTop:
                            this.scrollContainer
                                ?.scrollTop,
                        scrollHeight:
                            this.scrollContainer
                                ?.scrollHeight,
                        gen,
                    },
                );

                await this.restoreScrollTop(saved);

                if (
                    gen !==
                    this.scrollRestoreGeneration
                ) {
                    this.scrollRestoreResolved = gen;

                    return;
                }

                if (showDropdown) {
                    // When switching albums, anchor
                    // the scroll so the newly-expanded
                    // album stays at the same viewport
                    // position it occupied before the
                    // old dropdown was removed.
                    if (
                        this.savedAlbumViewportOffset !==
                            null &&
                        this.expandedAlbumId !== null
                    ) {
                        const idx =
                            this.albums.findIndex(
                                (a) =>
                                    a.ID ===
                                    this
                                        .expandedAlbumId,
                            );

                        if (idx >= 0) {
                            const gap =
                                CoverGrid.GRID_GAP;
                            const pad =
                                CoverGrid.GRID_PADDING;
                            const cols =
                                this.getColumnCount();
                            const rowStep =
                                this.cardHeight + gap;
                            const row = Math.floor(
                                idx / cols,
                            );
                            const albumY =
                                pad + row * rowStep;
                            const anchor =
                                albumY -
                                this
                                    .savedAlbumViewportOffset;

                            console.log(
                                '[updated] anchor restore',
                                {
                                    albumY,
                                    offset: this
                                        .savedAlbumViewportOffset,
                                    anchor,
                                },
                            );

                            await this.restoreScrollTop(
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
                        this.scrollRestoreResolved =
                            gen;

                        return;
                    }

                    await this.scrollToShowDropdown();
                }

                if (
                    gen !==
                    this.scrollRestoreGeneration
                ) {
                    this.scrollRestoreResolved = gen;

                    return;
                }

                if (!switching) {
                    console.log(
                        '[updated] removing overlay',
                    );
                    this.removeOverlay();
                } else {
                    console.log(
                        '[updated] keeping overlay (switching)',
                    );
                }

                this.scrollRestoreResolved = gen;
            })();
        }

        // On zoom while dropdown is open, recompute
        // the split (column count may change) and
        // re-scroll after layout settles.
        if (
            cardSizeChanged &&
            this.splitMode &&
            this.expandedTracks.length > 0
        ) {
            this.computeSplitIndex();

            void (async () => {
                await this.updateComplete;
                await this.awaitBeforeLayout();
                await this.scrollToShowDropdown();
            })();
        }
    }

    /* ====================================================================
     * Dynamic size properties
     * ==================================================================== */

    private updateSizeProperties() {
        const w = this.cardWidth;

        this.style.setProperty(
            '--card-width',
            `${w}px`,
        );

        // Scale placeholder initial font.
        const placeholderFont =
            Math.max(16, Math.round(w * 0.3));
        this.style.setProperty(
            '--placeholder-font',
            `${placeholderFont}px`,
        );

        // Text sizing tiers.
        if (w < 160) {
            this.classList.add('size-small');
            this.style.setProperty(
                '--album-name-font',
                '11px',
            );
            this.style.setProperty(
                '--artist-name-font',
                '10px',
            );
        } else if (w > 250) {
            this.classList.remove('size-small');
            this.style.setProperty(
                '--album-name-font',
                '16px',
            );
            this.style.setProperty(
                '--artist-name-font',
                '13px',
            );
        } else {
            this.classList.remove('size-small');
            this.style.setProperty(
                '--album-name-font',
                '14px',
            );
            this.style.setProperty(
                '--artist-name-font',
                '12px',
            );
        }
    }

    /* ====================================================================
     * Ctrl+Scroll zoom
     * ==================================================================== */

    private onWheel(e: WheelEvent) {
        if (!e.ctrlKey) return;

        e.preventDefault();

        const delta = e.deltaY > 0 ? -ZOOM_STEP : ZOOM_STEP;

        this.libraryCtrl.coverSize =
            this.cardWidth + delta;
    }

    /* ====================================================================
     * Data loading
     * ==================================================================== */

    private async loadAlbums() {
        try {
            this.loading = true;

            const albums =
                await this.libraryCtrl.getAlbums();

            this.albums = albums ?? [];
            this.selectedAlbums = new Set();
            this.lastSelectedAlbumIndex = null;
        } catch (error) {
            console.error(
                'Error loading albums:',
                error,
            );
            this.albums = [];
        } finally {
            this.loading = false;
        }

        await this.updateComplete;
        this.restoreScrollPosition();
        this.setupResizeObserver();
    }

    /* ====================================================================
     * Scroll position (index-based)
     * ==================================================================== */

    private restoreScrollPosition() {
        const saved =
            this.libraryCtrl.getScrollPosition('albums');

        if (saved <= 0 || !this.virtualizerSingle) {
            return;
        }

        const safeIndex = Math.min(
            saved,
            this.albums.length - 1,
        );

        if (safeIndex <= 0) return;

        this.virtualizerSingle.scrollToIndex(
            safeIndex,
            'start',
        );
    }

    /**
     * Save scroll position from the first visible
     * album.  In split mode we compute the index from
     * scrollTop; in single mode we use the virtualizer
     * visibilityChanged event data.
     */
    private onVisibilityChanged = (
        e: VisibilityChangedEvent,
    ) => {
        // Skip saves while a resize reflow is in
        // progress — the virtualizer reports
        // intermediate positions that would overwrite
        // the real scroll position in the store.
        if (this.isResizing) return;

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
        }

        this.scrollDebounceTimer = setTimeout(() => {
            if (this.splitMode) {
                // In split mode the event indices are
                // relative to the before-virtualizer.
                // Save the album index directly.
                const entries =
                    this.getBeforeEntries();
                const first = entries[e.first];

                if (first) {
                    this.libraryCtrl.setScrollPosition(
                        'albums',
                        first.albumIndex,
                    );
                }
            } else {
                const entries =
                    this.buildGridEntries();
                const first = entries[e.first];

                if (first) {
                    this.libraryCtrl.setScrollPosition(
                        'albums',
                        first.albumIndex,
                    );
                }
            }
        }, SCROLL_DEBOUNCE_MS);
    };

    /* ====================================================================
     * Resize-aware scroll preservation
     *
     * When the container width changes (queue panel
     * open/close, window resize) the grid reflows and
     * the pixel scroll position becomes stale.
     *
     * We identify the album at the viewport center
     * before the resize, then after the reflow we
     * place that same album back at the same viewport
     * offset.  Integer album indices ensure zero
     * scroll creep across repeated open/close cycles.
     *
     * If a dropdown is open the expanded album is the
     * focus; otherwise the album at the viewport center
     * is used.
     * ==================================================================== */

    private setupResizeObserver() {
        const container = this.scrollContainer;

        if (!container) return;

        // Guard against stacked observers from
        // repeated calls (e.g. library re-scan).
        this.resizeObserver?.disconnect();

        this.currentColumnCount =
            this.getColumnCount();

        /** Restore scroll so the focus album stays
         *  at the same viewport offset after reflow. */
        const restoreScroll = () => {
            const pending = this.pendingFocus;

            this.pendingFocus = null;
            this.isResizing = false;

            if (!pending) return;

            const newColumns = this.getColumnCount();

            this.currentColumnCount = newColumns;

            // If a dropdown is open, recompute the
            // split and re-evaluate scroll.
            if (
                this.splitMode &&
                this.expandedAlbumId !== null
            ) {
                this.computeSplitIndex();
                this.requestUpdate();

                void (async () => {
                    await this.updateComplete;
                    await this.awaitBeforeLayout();
                    await this.scrollToShowDropdown();
                })();

                return;
            }

            const gap = CoverGrid.GRID_GAP;
            const pad = CoverGrid.GRID_PADDING;
            const rowStep =
                this.cardHeight + gap;

            // Derive the album's row under the new
            // column count.  Both albumIndex and
            // newColumns are integers, so newRow is
            // also an integer — no fractional drift.
            const newRow = Math.floor(
                pending.albumIndex / newColumns,
            );
            const newY =
                pad + newRow * rowStep;

            container.scrollTop =
                newY - pending.viewportOffset;
        };

        this.resizeObserver = new ResizeObserver(
            () => {
                const rowStep =
                    this.cardHeight + CoverGrid.GRID_GAP;

                // Capture on the first event using
                // the pre-resize column count.
                if (this.pendingFocus === null) {
                    this.isResizing = true;

                    this.captureFocusPoint(
                        container,
                        rowStep,
                    );
                }

                const newColumns =
                    this.getColumnCount();

                if (
                    newColumns !==
                    this.currentColumnCount
                ) {
                    // Column count changed — correct
                    // scroll immediately.
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

                // Same column count — debounce for a
                // final adjustment once resizing settles.
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
     * If a dropdown is open, the expanded album is the
     * focus and its current viewport offset is preserved.
     * Otherwise the album at the viewport center is used.
     *
     * Stores an integer album index and the pixel offset
     * from that album's top edge to the viewport top.
     * Integer indices ensure zero drift across repeated
     * open/close cycles (no fractional accumulation).
     */
    private captureFocusPoint(
        container: HTMLElement,
        rowStep: number,
    ) {
        const pad = CoverGrid.GRID_PADDING;
        const cols = this.currentColumnCount;

        // Prefer the expanded album as focus.
        if (this.expandedAlbumId !== null) {
            const idx = this.albums.findIndex(
                (a) => a.ID === this.expandedAlbumId,
            );

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

        // Fall back to the album whose row contains
        // the viewport center.
        const centerY =
            container.scrollTop +
            container.clientHeight / 2;
        const centerRow = Math.floor(
            Math.max(0, centerY - pad) /
            rowStep,
        );
        const albumIndex = Math.min(
            centerRow * cols,
            Math.max(0, this.albums.length - 1),
        );

        // Pixel offset from that album's top edge
        // to the viewport top — used exactly once in
        // restoreScroll, never fed back.
        const albumY =
            pad + centerRow * rowStep;

        this.pendingFocus = {
            albumIndex,
            viewportOffset:
                albumY - container.scrollTop,
        };
    }

    /* ====================================================================
     * Column count helper
     * ==================================================================== */

    private getColumnCount(): number {
        const el =
            this.scrollContainer ??
            this.virtualizerSingle;

        if (!el) return 1;

        const gap = CoverGrid.GRID_GAP;
        const pad = CoverGrid.GRID_PADDING;
        const availableWidth =
            el.clientWidth - pad * 2;

        return Math.max(
            1,
            Math.floor(
                (availableWidth + gap) /
                (this.cardWidth + gap),
            ),
        );
    }

    /** Container width in pixels for the dropdown. */
    private getContainerWidth(): number {
        const el =
            this.scrollContainer ??
            this.virtualizerSingle;

        return el?.clientWidth ?? 800;
    }

    /* ====================================================================
     * Split-mode helpers
     *
     * When the dropdown is open the album grid is split
     * into two virtualizers with the dropdown in between.
     * This avoids phantom rows and lets the dropdown size
     * itself to its content exactly.
     * ==================================================================== */

    /**
     * Compute the split point: all albums up to and
     * including the expanded album's row go into the
     * "before" virtualizer; the rest go into "after".
     */
    private computeSplitIndex() {
        if (this.expandedAlbumId === null) {
            this.splitIndex = this.albums.length;

            return;
        }

        const columns = this.getColumnCount();
        const expandedIndex = this.albums.findIndex(
            (a) => a.ID === this.expandedAlbumId,
        );

        if (expandedIndex < 0) {
            this.splitIndex = this.albums.length;

            return;
        }

        this.splitIndex = Math.min(
            (Math.floor(expandedIndex / columns) +
                1) *
                columns,
            this.albums.length,
        );
    }

    /* ====================================================================
     * Virtualizer items
     * ==================================================================== */

    /**
     * Build a flat GridEntry array for a given album
     * slice.  Used by both single and split modes.
     */
    private buildGridEntries(): GridEntry[] {
        // Return cached result when input unchanged.
        if (this.entriesCacheAlbums === this.albums) {
            return this.entriesCache;
        }

        const entries: GridEntry[] = [];

        for (let i = 0; i < this.albums.length; i++) {
            entries.push({
                album: this.albums[i]!,
                albumIndex: i,
            });
        }

        this.entriesCacheAlbums = this.albums;
        this.entriesCache = entries;

        return entries;
    }

    /** Entries for the "before" virtualizer. */
    private getBeforeEntries(): GridEntry[] {
        return this.buildGridEntries().slice(
            0,
            this.splitIndex,
        );
    }

    /** Entries for the "after" virtualizer. */
    private getAfterEntries(): GridEntry[] {
        return this.buildGridEntries().slice(
            this.splitIndex,
        );
    }

    private gridKeyFunction = (
        entry: GridEntry,
    ) => {
        return `a-${entry.album.ID}`;
    };

    /* ====================================================================
     * Transition overlay
     *
     * Before a single/split mode switch we clone the
     * scroll container into an absolutely-positioned
     * overlay so the old visual state stays on-screen
     * while the new layout computes underneath
     * (hidden).  Once the new layout is ready and
     * scroll is restored we remove the overlay and
     * reveal the real container in one paint frame.
     * ==================================================================== */

    /**
     * Capture the current scroll container as a
     * static overlay so the user keeps seeing the old
     * state while the DOM switches underneath.
     */
    private captureOverlay() {
        const container = this.scrollContainer;

        if (!container || this.transitionOverlay) {
            return;
        }

        const scrollY = container.scrollTop;
        const overlay = document.createElement('div');

        overlay.style.cssText =
            'position:absolute;inset:0;z-index:10;' +
            'overflow:hidden;pointer-events:none;';

        // Clone each child into a wrapper that
        // reproduces the scroll viewport.
        const inner = document.createElement('div');

        inner.style.cssText =
            'position:relative;height:100%;' +
            'pointer-events:none;';

        for (const child of Array.from(
            container.childNodes,
        )) {
            inner.appendChild(child.cloneNode(true));
        }

        // Shift content up to match the current
        // scroll offset.
        inner.style.transform =
            `translateY(-${scrollY}px)`;

        overlay.appendChild(inner);

        // Append to :host (shadow root), not inside
        // the scroll container, so Lit's diffing does
        // not touch it.
        this.shadowRoot?.appendChild(overlay);
        this.transitionOverlay = overlay;

        // Hide the real container while the new
        // layout settles.
        container.style.visibility = 'hidden';
    }

    /**
     * Remove the snapshot overlay and reveal the real
     * scroll container.  Both happen synchronously so
     * they land in the same paint frame.
     */
    private removeOverlay() {
        if (this.transitionOverlay) {
            this.transitionOverlay.remove();
            this.transitionOverlay = null;
        }

        if (this.scrollContainer) {
            this.scrollContainer.style.visibility = '';
        }
    }

    /* ====================================================================
     * Dropdown scroll positioning
     *
     * In split mode the dropdown is a normal-flow DOM
     * element between two virtualizers.  We query its
     * position from the DOM.
     * ==================================================================== */

    /**
     * Wait for the "before" virtualizer to finish its
     * layout pass so that its host element height
     * reflects the total content size.  Without this,
     * setting scrollTop can be silently clamped to 0
     * because the scroll container hasn't grown yet.
     */
    private async awaitBeforeLayout(): Promise<void> {
        const virt =
            this.shadowRoot?.querySelector(
                '#grid-before',
            ) as LitVirtualizer | null;

        await virt?.layoutComplete;
    }

    /**
     * Return the current scrollTop converted to
     * single-mode (dropdown-free) coordinates.
     *
     * In split mode the open dropdown shifts all
     * content below it downward.  When we save a
     * scroll position for later restoration in a
     * different layout we need to remove that shift
     * so the saved value is layout-agnostic.
     */
    private computeAdjustedScrollTop(): number {
        const container = this.scrollContainer;

        if (!container) return 0;

        const raw = container.scrollTop;

        if (!this.splitMode) return raw;

        const gap = CoverGrid.GRID_GAP;
        const pad = CoverGrid.GRID_PADDING;
        const columns = this.getColumnCount();
        const rowStep = this.cardHeight + gap;
        const beforeRows = Math.ceil(
            this.splitIndex / columns,
        );

        // Position where the dropdown starts in
        // the split layout (scroll-content coords).
        const dropdownTop =
            pad + beforeRows * rowStep;

        if (raw <= dropdownTop) {
            console.log(
                '[adjustScroll] raw <= dropdownTop, no adjust',
                { raw, dropdownTop },
            );

            return raw;
        }

        const dropdown =
            this.shadowRoot?.querySelector(
                'album-dropdown',
            );
        const dropdownHeight =
            (dropdown as HTMLElement)?.offsetHeight ??
            0;

        const adjusted = raw - dropdownHeight;

        console.log(
            '[adjustScroll]',
            {
                raw,
                dropdownTop,
                dropdownHeight,
                adjusted,
            },
        );

        return adjusted;
    }

    /**
     * Set scrollTop on the scroll container and verify
     * the browser didn't silently clamp it.  If the
     * virtualizer hasn't expanded its host height yet,
     * scrollTop will be clamped to a smaller value.
     * In that case, wait one animation frame (giving
     * the virtualizer time to size itself) and retry.
     */
    private async restoreScrollTop(
        target: number,
    ): Promise<void> {
        const container = this.scrollContainer;

        if (!container) return;

        const maxAttempts = 10;

        for (let i = 0; i < maxAttempts; i++) {
            container.scrollTop = target;

            console.log(
                `[restoreScrollTop] attempt ${i}`,
                {
                    target,
                    actual: container.scrollTop,
                    scrollHeight:
                        container.scrollHeight,
                    clientHeight:
                        container.clientHeight,
                },
            );

            // Success if the browser accepted the
            // value, or the target is at/below zero.
            if (
                container.scrollTop >= target ||
                target <= 0
            ) {
                return;
            }

            // Content hasn't expanded enough yet —
            // wait one frame and retry.
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
     * and its dropdown are visible, using minimal
     * movement:
     *
     * 1. If both fit in the viewport already, don't
     *    scroll.
     * 2. If the dropdown bottom overflows below the
     *    viewport, align it with the viewport bottom.
     * 3. If that would push the album card above the
     *    viewport, pin the album card top to the
     *    viewport top instead.
     *
     * Positions are computed from grid math rather
     * than DOM queries so that the method works
     * immediately after a single/split mode switch
     * (before the virtualizer has laid out).
     */
    private async scrollToShowDropdown() {
        const container = this.scrollContainer;

        if (
            !container ||
            this.expandedAlbumId === null
        ) {
            return;
        }

        const expandedIndex = this.albums.findIndex(
            (a) => a.ID === this.expandedAlbumId,
        );

        if (expandedIndex < 0) return;

        const gap = CoverGrid.GRID_GAP;
        const pad = CoverGrid.GRID_PADDING;
        const columns = this.getColumnCount();
        const rowStep = this.cardHeight + gap;
        const albumRow = Math.floor(
            expandedIndex / columns,
        );

        // Top of the album card (at the midpoint of
        // the gap above the row) in scroll-content
        // coordinates.
        const albumTop =
            pad + albumRow * rowStep - gap / 2;

        // Compute the dropdown bottom from grid math.
        // The dropdown sits right after the "before"
        // rows: ceil(splitIndex / columns) full rows.
        const dropdown =
            this.shadowRoot?.querySelector(
                'album-dropdown',
            );

        if (!dropdown) return;

        // Wait for the dropdown to finish rendering
        // its tracks so that offsetHeight is accurate.
        await (dropdown as LitElement).updateComplete;

        const beforeRows = Math.ceil(
            this.splitIndex / columns,
        );
        const dropdownTop = pad + beforeRows * rowStep;
        const dropdownBottom =
            dropdownTop +
            (dropdown as HTMLElement).offsetHeight;

        const viewTop = container.scrollTop;
        const viewHeight = container.clientHeight;

        // The valid scroll range where both the album
        // top and dropdown bottom are in view:
        //   scrollTop <= albumTop  (card visible)
        //   scrollTop >= dropdownBottom - viewHeight
        const minScroll =
            dropdownBottom - viewHeight;
        const maxScroll = albumTop;

        let newScrollTop: number;

        if (minScroll <= maxScroll) {
            // Both can fit — clamp to the valid
            // range, only scrolling if needed.
            newScrollTop = Math.max(
                minScroll,
                Math.min(viewTop, maxScroll),
            );
        } else {
            // Combined height exceeds the viewport.
            // Pin the album card top to the viewport
            // top so it stays visible.
            newScrollTop = albumTop;
        }

        console.log(
            '[scrollToShowDropdown]',
            {
                expandedIndex,
                albumRow,
                albumTop,
                beforeRows,
                dropdownTop,
                dropdownOffsetHeight:
                    (dropdown as HTMLElement)
                        .offsetHeight,
                dropdownBottom,
                viewTop,
                viewHeight,
                minScroll,
                maxScroll,
                newScrollTop,
                scrollHeight:
                    container.scrollHeight,
                willScroll:
                    newScrollTop !== viewTop,
            },
        );

        if (newScrollTop !== viewTop) {
            await this.restoreScrollTop(newScrollTop);
        }
    }

    /* ====================================================================
     * Album selection helpers
     * ==================================================================== */

    private selectAlbumRange(
        from: number,
        to: number,
    ): Set<number> {
        const start = Math.min(from, to);
        const end = Math.max(from, to);
        const ids = new Set<number>();

        for (let i = start; i <= end; i++) {
            const album = this.albums[i];

            if (album) {
                ids.add(album.ID);
            }
        }

        return ids;
    }

    private async getSelectedAlbumFilePaths(): Promise<
        string[]
    > {
        const selected = this.albums.filter((a) =>
            this.selectedAlbums.has(a.ID),
        );
        const allPaths: string[] = [];

        for (const album of selected) {
            const paths =
                await this.getAlbumFilePaths(album);
            allPaths.push(...paths);
        }

        return allPaths;
    }

    private async getAlbumFilePaths(
        album: library.Album,
    ): Promise<string[]> {
        try {
            const tracks = await GetAlbumTracks(album.ID);

            return tracks.map((t) => t.FilePath);
        } catch (error) {
            console.error(
                'Error loading album tracks:',
                error,
            );

            return [];
        }
    }

    /**
     * Pre-resolve file paths for all selected albums so
     * that dragstart can read them synchronously.  Called
     * fire-and-forget whenever the album selection changes.
     */
    private async warmAlbumFilePathCache(): Promise<void> {
        const selected = this.albums.filter((a) =>
            this.selectedAlbums.has(a.ID),
        );

        // Prune stale entries.
        for (const id of this.albumFilePathCache.keys()) {
            if (!this.selectedAlbums.has(id)) {
                this.albumFilePathCache.delete(id);
            }
        }

        // Fetch missing entries.
        for (const album of selected) {
            if (this.albumFilePathCache.has(album.ID)) {
                continue;
            }

            try {
                const tracks = await GetAlbumTracks(
                    album.ID,
                );
                // Only store if still selected.
                if (this.selectedAlbums.has(album.ID)) {
                    this.albumFilePathCache.set(
                        album.ID,
                        tracks.map((t) => t.FilePath),
                    );
                }
            } catch {
                // Silently skip — drag will just not
                // include this album's paths.
            }
        }
    }

    /**
     * Read cached file paths for the current album
     * selection.  Returns an empty array if any albums
     * haven't been cached yet.
     */
    private getCachedSelectedAlbumFilePaths(): string[] {
        const result: string[] = [];

        for (const album of this.albums) {
            if (!this.selectedAlbums.has(album.ID)) {
                continue;
            }

            const paths =
                this.albumFilePathCache.get(album.ID);

            if (paths) {
                result.push(...paths);
            }
        }

        return result;
    }

    /* ====================================================================
     * Track selection helpers
     * ==================================================================== */

    private selectTrackRange(
        from: number,
        to: number,
    ): Set<string> {
        const start = Math.min(from, to);
        const end = Math.max(from, to);
        const paths = new Set<string>();

        for (let i = start; i <= end; i++) {
            const track = this.expandedTracks[i];

            if (track) {
                paths.add(track.FilePath);
            }
        }

        return paths;
    }

    private getSelectedTrackFilePaths(): string[] {
        // Preserve the original track order
        return this.expandedTracks
            .filter((t) =>
                this.selectedTracks.has(t.FilePath),
            )
            .map((t) => t.FilePath);
    }

    /* ====================================================================
     * Dropdown (expand/collapse)
     * ==================================================================== */

    private async toggleDropdown(
        album: library.Album,
    ) {
        if (this.expandedAlbumId === album.ID) {
            // Close
            this.expandedAlbumId = null;
            this.expandedTracks = [];
            this.selectedTracks = new Set();
            this.lastSelectedTrackIndex = null;

            return;
        }

        // Open (or switch)
        this.expandedAlbumId = album.ID;
        this.expandedTracks = [];
        this.selectedTracks = new Set();
        this.lastSelectedTrackIndex = null;
        try {
            const tracks = await GetAlbumTracks(album.ID);

            // Only apply if still the same album
            if (this.expandedAlbumId === album.ID) {
                this.expandedTracks = tracks;
            }
        } catch (error) {
            console.error(
                'Error loading album tracks:',
                error,
            );
        }
    }

    /* ====================================================================
     * Event delegation helpers
     * ==================================================================== */

    /**
     * Walk up from the event target to find the nearest
     * `.album-card` and read its `data-index` attribute.
     * Returns `null` if the click was not on a card.
     */
    private resolveAlbumFromEvent(
        e: Event,
    ): { album: library.Album; index: number } | null {
        const path = e.composedPath();

        for (const el of path) {
            if (
                el instanceof HTMLElement &&
                el.classList.contains('album-card')
            ) {
                const raw = el.dataset['index'];

                if (raw === undefined) return null;

                const index = parseInt(raw, 10);
                const album = this.albums[index];

                if (!album) return null;

                return { album, index };
            }
        }

        return null;
    }

    /* ====================================================================
     * Delegated album event handlers
     * ==================================================================== */

    private onGridAlbumClick = (e: MouseEvent) => {
        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        const { album, index } = hit;
        const isCtrl = e.ctrlKey || e.metaKey;
        const isShift = e.shiftKey;

        if (
            isShift &&
            this.lastSelectedAlbumIndex !== null
        ) {
            const range = this.selectAlbumRange(
                this.lastSelectedAlbumIndex,
                index,
            );
            const next = new Set(this.selectedAlbums);

            for (const id of range) {
                next.add(id);
            }

            this.selectedAlbums = next;
            void this.warmAlbumFilePathCache();
        } else if (isCtrl) {
            const next = new Set(this.selectedAlbums);

            if (next.has(album.ID)) {
                next.delete(album.ID);
            } else {
                next.add(album.ID);
            }

            this.selectedAlbums = next;
            this.lastSelectedAlbumIndex = index;
            void this.warmAlbumFilePathCache();
        } else {
            void this.toggleDropdown(album);
            this.lastSelectedAlbumIndex = index;
        }
    };

    private onGridAlbumDblClick = async (
        e: MouseEvent,
    ) => {
        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        const filePaths = await this.getAlbumFilePaths(
            hit.album,
        );

        if (filePaths.length === 0) return;

        this.selectedAlbums = new Set();
        queueStore.setQueue(filePaths, 0);
    };

    private onGridAlbumKeydown = (
        e: KeyboardEvent,
    ) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;

        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        e.preventDefault();
        void this.toggleDropdown(hit.album);
        this.lastSelectedAlbumIndex = hit.index;
    };

    private onGridAlbumContextMenu = (
        e: MouseEvent,
    ) => {
        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        e.preventDefault();
        e.stopPropagation();

        if (!this.selectedAlbums.has(hit.album.ID)) {
            this.selectedAlbums = new Set([
                hit.album.ID,
            ]);
            void this.warmAlbumFilePathCache();
        }

        this.contextMenuTarget = { kind: 'album' };
        this.openContextMenuAt(e.clientX, e.clientY);
    };

    /**
     * Delegated image error handler — falls back from
     * thumbnail to full-size cover art.
     */
    private onGridImageError = (e: Event) => {
        const img = e.target;

        if (!(img instanceof HTMLImageElement)) return;

        if (!img.classList.contains('cover-image')) {
            return;
        }

        const card = img.closest('.album-card');

        if (!card) return;

        const raw = (card as HTMLElement).dataset[
            'index'
        ];

        if (raw === undefined) return;

        const index = parseInt(raw, 10);
        const album = this.albums[index];

        if (album && img.src !== album.CoverArtPath) {
            img.src = album.CoverArtPath;
        }
    };

    /* ====================================================================
     * Track event handlers (from album-dropdown)
     * ==================================================================== */

    private onTrackClick = (
        e: CustomEvent<TrackClickDetail>,
    ) => {
        const {
            track,
            index,
            ctrlKey,
            shiftKey,
            metaKey,
        } = e.detail;

        const isCtrl = ctrlKey || metaKey;

        if (
            shiftKey &&
            this.lastSelectedTrackIndex !== null
        ) {
            const range = this.selectTrackRange(
                this.lastSelectedTrackIndex,
                index,
            );
            const next = new Set(this.selectedTracks);

            for (const path of range) {
                next.add(path);
            }

            this.selectedTracks = next;
        } else if (isCtrl) {
            const next = new Set(this.selectedTracks);

            if (next.has(track.FilePath)) {
                next.delete(track.FilePath);
            } else {
                next.add(track.FilePath);
            }

            this.selectedTracks = next;
            this.lastSelectedTrackIndex = index;
        } else {
            this.selectedTracks = new Set([
                track.FilePath,
            ]);
            this.lastSelectedTrackIndex = index;
        }
    };

    private onTrackDblClick = (
        e: CustomEvent<TrackDblClickDetail>,
    ) => {
        const { index } = e.detail;

        // Play the full album starting from this track
        const filePaths = this.expandedTracks.map(
            (t) => t.FilePath,
        );

        if (filePaths.length === 0) return;

        this.selectedTracks = new Set();
        queueStore.setQueue(filePaths, index);
    };

    private onTrackContextMenu = (
        e: CustomEvent<TrackContextMenuDetail>,
    ) => {
        const { track, clientX, clientY } = e.detail;

        if (!this.selectedTracks.has(track.FilePath)) {
            this.selectedTracks = new Set([
                track.FilePath,
            ]);
        }

        this.contextMenuTarget = { kind: 'track' };
        this.openContextMenuAt(clientX, clientY);
    };

    /* ====================================================================
     * Drag source (dropdown tracks)
     * ==================================================================== */

    private onTrackDragStart = (
        e: CustomEvent<TrackDragStartDetail>,
    ) => {
        const { track, dataTransfer } = e.detail;

        let filePaths: string[];

        if (this.selectedTracks.has(track.FilePath)) {
            filePaths =
                this.getSelectedTrackFilePaths();
        } else {
            filePaths = [track.FilePath];
        }

        if (filePaths.length === 0) return;

        if (dataTransfer) {
            const payload: DragPayload = {
                filePaths,
                source: 'cover-grid',
            };

            dataTransfer.effectAllowed = 'copy';
            dataTransfer.setData(
                DRAG_MIME,
                JSON.stringify(payload),
            );

            this.dragImageEl = createDragImage(
                filePaths.length,
            );
            dataTransfer.setDragImage(
                this.dragImageEl,
                0,
                0,
            );
        }

        emitDragActive(true);
    };

    private onTrackDragEnd = () => {
        if (this.dragImageEl) {
            removeDragImage(this.dragImageEl);
            this.dragImageEl = null;
        }

        emitDragActive(false);
    };

    /* ====================================================================
     * Drag source (album cards)
     * ==================================================================== */

    private onAlbumDragStart = (e: DragEvent) => {
        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        // Read file paths synchronously from the
        // pre-warmed cache.  The cache is populated
        // asynchronously whenever the album selection
        // changes, so by the time the user drags, the
        // data is already available.
        let filePaths: string[];

        if (this.selectedAlbums.has(hit.album.ID)) {
            filePaths =
                this.getCachedSelectedAlbumFilePaths();
        } else {
            // Single unselected album — check cache.
            filePaths =
                this.albumFilePathCache.get(
                    hit.album.ID,
                ) ?? [];
        }

        if (filePaths.length === 0) {
            // Cache miss — cancel the drag.
            e.preventDefault();

            return;
        }

        setDragPayload(e, {
            filePaths,
            source: 'cover-grid',
        });

        this.dragImageEl = createDragImage(
            filePaths.length,
        );
        e.dataTransfer?.setDragImage(
            this.dragImageEl,
            0,
            0,
        );

        emitDragActive(true);
    };

    private onAlbumDragEnd = () => {
        if (this.dragImageEl) {
            removeDragImage(this.dragImageEl);
            this.dragImageEl = null;
        }

        emitDragActive(false);
    };

    /* ====================================================================
     * Grid click (empty area)
     * ==================================================================== */

    private onGridClick = (e: MouseEvent) => {
        const path = e.composedPath();

        const clickedCard = path.some(
            (el) =>
                el instanceof HTMLElement &&
                el.classList.contains('album-card'),
        );

        const clickedDropdown = path.some(
            (el) =>
                el instanceof HTMLElement &&
                el.classList.contains('album-dropdown'),
        );

        if (!clickedCard && !clickedDropdown) {
            this.selectedAlbums = new Set();
            this.lastSelectedAlbumIndex = null;
            this.expandedAlbumId = null;
            this.expandedTracks = [];
            this.selectedTracks = new Set();
            this.lastSelectedTrackIndex = null;
        }
    };

    /* ====================================================================
     * Context menu (shared between albums and tracks)
     * ==================================================================== */

    private openContextMenuAt(
        clientX: number,
        clientY: number,
    ) {
        this.contextMenuOpen = true;

        this.updateComplete.then(() => {
            const popup = this.contextMenuPopup;

            if (popup) {
                (popup as any).anchor = {
                    getBoundingClientRect() {
                        return {
                            width: 0,
                            height: 0,
                            x: clientX,
                            y: clientY,
                            top: clientY,
                            left: clientX,
                            right: clientX,
                            bottom: clientY,
                        };
                    },
                };
                (popup as any).active = true;
            }
        });
    }

    private async onContextMenuAction(action: string) {
        const filePaths =
            this.contextMenuTarget.kind === 'track'
                ? this.getSelectedTrackFilePaths()
                : await this.getSelectedAlbumFilePaths();

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                queueStore.setQueue(filePaths, 0);
                break;
            case 'add-to-queue':
                queueStore.addTracksToQueue(filePaths);
                break;
            case 'play-next':
                queueStore.playTracksNext(filePaths);
                break;
        }

        this.closeContextMenu(true);
    }

    private closeContextMenu(clearSelection = false) {
        if (!this.contextMenuOpen) return;

        this.closePlaylistSubmenu();
        this.contextMenuOpen = false;
        this.playlistFilePaths = [];

        if (clearSelection) {
            if (
                this.contextMenuTarget.kind === 'track'
            ) {
                this.selectedTracks = new Set();
            } else {
                this.selectedAlbums = new Set();
            }
        }

        const popup = this.contextMenuPopup;

        if (popup) {
            (popup as any).active = false;
        }
    }

    private async showPlaylistSubmenu() {
        if (this.playlistSubmenuOpen) return;

        if (this.contextMenuTarget.kind === 'track') {
            this.playlistFilePaths =
                this.getSelectedTrackFilePaths();
        } else if (this.selectedAlbums.size > 0) {
            this.playlistFilePaths =
                await this.getSelectedAlbumFilePaths();
        }

        this.playlistSubmenuOpen = true;

        await this.updateComplete;

        const submenu = this.playlistSubmenuPopup;
        const trigger =
            this.shadowRoot?.querySelector(
                '.submenu-item',
            );

        if (submenu && trigger) {
            (submenu as any).anchor = trigger;
            (submenu as any).active = true;
        }

        const picker =
            this.shadowRoot?.querySelector(
                'playlist-picker',
            ) as PlaylistPicker | null;

        picker?.reset();
    }

    private closePlaylistSubmenu() {
        if (!this.playlistSubmenuOpen) return;

        this.playlistSubmenuOpen = false;

        const submenu = this.playlistSubmenuPopup;

        if (submenu) {
            (submenu as any).active = false;
        }
    }

    private onPlaylistActionComplete = () => {
        this.closeContextMenu();
    };

    /* ====================================================================
     * Rendering helpers
     * ==================================================================== */

    private getAlbumInitial(name: string): string {
        return name.charAt(0).toUpperCase();
    }

    /**
     * Pick the appropriate cover art URL based on the
     * current card size and device pixel ratio.
     */
    private getCoverUrl(album: library.Album): string {
        const needed =
            this.imageSize * window.devicePixelRatio;

        if (needed <= 100) {
            return (
                album.CoverArtSmall ||
                album.CoverArtMedium ||
                album.CoverArtPath
            );
        }

        if (needed <= 200) {
            return (
                album.CoverArtMedium ||
                album.CoverArtLarge ||
                album.CoverArtPath
            );
        }

        if (needed <= 400) {
            return (
                album.CoverArtLarge ||
                album.CoverArtPath
            );
        }

        return album.CoverArtPath;
    }

    /* ====================================================================
     * Render: grid entry (virtualizer renderItem)
     * ==================================================================== */

    private renderGridEntry = (
        entry: GridEntry,
    ) => {
        return this.renderAlbumCard(
            entry.album,
            entry.albumIndex,
        );
    };

    /* ====================================================================
     * Render: album card
     *
     * No per-card event listeners — events are delegated
     * via data-index on the virtualizer.
     * ==================================================================== */

    private renderAlbumCard(
        album: library.Album,
        index: number,
    ) {
        const selected = this.selectedAlbums.has(
            album.ID,
        );
        const expanded =
            this.expandedAlbumId === album.ID;

        const classes = [
            'album-card',
            selected ? 'selected' : '',
            expanded ? 'expanded' : '',
        ]
            .filter(Boolean)
            .join(' ');

        const imgSize = this.imageSize;

        return html`
            <div
                class=${classes}
                tabindex="0"
                role="button"
                data-index=${index}
                aria-label="${album.Name} by ${album.ArtistName}"
                draggable="true"
                @dragstart=${this.onAlbumDragStart}
                @dragend=${this.onAlbumDragEnd}
            >
                <div class="cover-container">
                    ${album.CoverArtPath
                ? html`<img
                              class="cover-image"
                              src="${this.getCoverUrl(album)}"
                              alt="${album.Name} cover"
                              width="${imgSize}"
                              height="${imgSize}"
                              loading="lazy"
                              decoding="async"
                          />`
                : html`<div
                              class="placeholder-cover"
                          >
                              ${this.getAlbumInitial(album.Name)}
                          </div>`}
                </div>
                <div class="album-info">
                    <div
                        class="album-name"
                        title="${album.Name}"
                    >
                        ${album.Name}${album.Year
                ? html`
                              <span class="album-year">
                                  (${album.Year})</span
                              >`
                : nothing}
                    </div>
                    <div
                        class="artist-name"
                        title="${album.ArtistName}"
                    >
                        ${album.ArtistName}
                    </div>
                </div>
            </div>
        `;
    }

    /* ====================================================================
     * Render: main
     * ==================================================================== */

    override render() {
        if (this.loading) {
            return html`<div class="loading">
                Loading albums...
            </div>`;
        }

        if (this.albums.length === 0) {
            return html`
                <div class="empty-state">
                    <p>No albums found</p>
                    <p>
                        Add music to your library to see
                        album covers here.
                    </p>
                </div>
            `;
        }

        const gridContent = this.splitMode
            ? this.renderSplitGrid()
            : this.renderSingleGrid();

        return html`
            <div
                class="grid-scroll-container"
                @click=${this.onGridClick}
            >
                ${gridContent}
            </div>

            ${this.renderContextMenu()}
        `;
    }

    /** Single virtualizer — no dropdown open. */
    private renderSingleGrid() {
        return html`
            <lit-virtualizer
                id="grid-single"
                .items=${this.buildGridEntries()}
                .renderItem=${this.renderGridEntry}
                .keyFunction=${this.gridKeyFunction}
                .layout=${this.gridLayout}
                @click=${this.onGridAlbumClick}
                @dblclick=${this.onGridAlbumDblClick}
                @keydown=${this.onGridAlbumKeydown}
                @contextmenu=${this.onGridAlbumContextMenu}
                @visibilityChanged=${this.onVisibilityChanged}
            ></lit-virtualizer>
        `;
    }

    /**
     * Dual virtualizer — dropdown sandwiched between
     * "before" and "after" grids.
     */
    private renderSplitGrid() {
        return html`
            <lit-virtualizer
                id="grid-before"
                .items=${this.getBeforeEntries()}
                .renderItem=${this.renderGridEntry}
                .keyFunction=${this.gridKeyFunction}
                .layout=${this.gridLayout}
                @click=${this.onGridAlbumClick}
                @dblclick=${this.onGridAlbumDblClick}
                @keydown=${this.onGridAlbumKeydown}
                @contextmenu=${this.onGridAlbumContextMenu}
                @visibilityChanged=${this.onVisibilityChanged}
            ></lit-virtualizer>

            <album-dropdown
                .tracks=${this.expandedTracks}
                .selectedTracks=${this.selectedTracks}
                .containerWidth=${this.getContainerWidth()}
                @track-click=${this.onTrackClick}
                @track-dblclick=${this.onTrackDblClick}
                @track-contextmenu=${this.onTrackContextMenu}
                @track-dragstart=${this.onTrackDragStart}
                @track-dragend=${this.onTrackDragEnd}
            ></album-dropdown>

            ${this.getAfterEntries().length > 0
                ? html`
                      <lit-virtualizer
                          id="grid-after"
                          .items=${this.getAfterEntries()}
                          .renderItem=${this.renderGridEntry}
                          .keyFunction=${this.gridKeyFunction}
                          .layout=${this.gridLayoutAfter}
                          @click=${this.onGridAlbumClick}
                          @dblclick=${this.onGridAlbumDblClick}
                          @keydown=${this.onGridAlbumKeydown}
                          @contextmenu=${this.onGridAlbumContextMenu}
                      ></lit-virtualizer>
                  `
                : nothing}
        `;
    }

    /** Context menu + playlist submenu popups. */
    private renderContextMenu() {
        return html`
            <wa-popup
                id="context-menu"
                placement="bottom-start"
                flip
                shift
                .active=${this.contextMenuOpen}
            >
                ${this.contextMenuOpen
                ? html`
                          <div class="context-menu-panel">
                              <wa-dropdown-item
                                  @click=${() =>
                        this.onContextMenuAction(
                            'play',
                        )}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="play"
                                  ></wa-icon>
                                  Play
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  @click=${() =>
                        this.onContextMenuAction(
                            'add-to-queue',
                        )}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="plus"
                                  ></wa-icon>
                                  Add to Queue
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  @click=${() =>
                        this.onContextMenuAction(
                            'play-next',
                        )}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="forward-step"
                                  ></wa-icon>
                                  Play Next
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  class="submenu-item"
                                  @mouseenter=${() =>
                        this.showPlaylistSubmenu()}
                                  @click=${(e: Event) => {
                        e.stopPropagation();
                        void this.showPlaylistSubmenu();
                    }}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="plus"
                                  ></wa-icon>
                                  Add to Playlist
                                  <span
                                      class="submenu-arrow"
                                      >&#9654;</span
                                  >
                              </wa-dropdown-item>
                          </div>
                      `
                : nothing}
            </wa-popup>

            <wa-popup
                id="playlist-submenu"
                placement="right-start"
                flip
                shift
                .active=${this.playlistSubmenuOpen}
            >
                ${this.playlistSubmenuOpen
                ? html`
                          <playlist-picker
                              .filePaths=${this.playlistFilePaths}
                              @playlist-action-complete=${this.onPlaylistActionComplete}
                              @click=${(e: Event) =>
                        e.stopPropagation()}
                          ></playlist-picker>
                      `
                : nothing}
            </wa-popup>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'cover-grid': CoverGrid;
    }
}

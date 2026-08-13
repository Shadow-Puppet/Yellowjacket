import { LitElement, html, nothing } from 'lit';
import {
    customElement,
    property,
    state,
    query,
} from 'lit/decorators.js';
import '@lit-labs/virtualizer';
import type {
    LitVirtualizer,
    VisibilityChangedEvent,
} from '@lit-labs/virtualizer';
import { grid } from '@lit-labs/virtualizer/layouts/grid.js';
import {
    GetAlbumTracks,
    GetAlbumTracksByLibrary,
} from '@go/library/Library';
import { library } from '@go/models';
import { LibraryController } from '@store/controllers/library-controller';
import { SearchController } from '@store/controllers/search-controller';
import { ViewLifecycleMixin } from '@utils/view-lifecycle';
import { RovingGridController } from '@utils/roving-grid';
import { queueStore } from '@store/queue-store';
import type { QueueSource } from '@store/queue-store';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import type WaPopup from '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/playlist-picker/playlist-picker.js';
import { loadTrackDetails } from '@utils/lazy-track-details.js';
import { tracksByFilePath, tracksForPaths } from '@utils/track-index.js';
import type { TrackDetails } from '@components/track-details/track-details.js';
import type { CoverArtUrls } from '@components/track-details/track-details.js';
import { AlbumSelectionManager } from './album-selection.js';
import { ScrollManager } from './scroll-manager.js';
import type { ScrollManagerHost } from './scroll-manager.js';
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
    ContextMenuController,
    isContextMenuKey,
} from '@utils/context-menu-controller.js';
import type { ContextMenuHost } from '@utils/context-menu-controller.js';
import { FavoritesController } from '@store/controllers/favorites-controller';
import { artistLink, exploreLinkStyles } from '../../utils/explore-link';
import {
    createAlbumArtDragImage,
    createDragImage,
    createTrackCardDragImage,
    removeDragImage,
} from '@utils/drag-image';
import { coverGridStyles } from './cover-grid-styles.js';
import '@components/page-header/page-header';
import {
    ALBUM_SORT_OPTIONS,
    SORT_DIR_KEY,
    SORT_FIELD_KEY,
    ZOOM_STEP,
} from './cover-grid-types.js';
import type {
    AlbumSortField,
    ContextMenuTarget,
    GridEntry,
    SortDirection,
} from './cover-grid-types.js';

@customElement('cover-grid')
export class CoverGrid
    extends ViewLifecycleMixin(LitElement)
    implements ContextMenuHost, ScrollManagerHost
{
    /**
     * When set, the grid displays these albums instead of
     * fetching all albums from the library store.  The
     * parent is responsible for reloading when data changes.
     */
    @property({ type: Array, attribute: false })
    externalAlbums?: library.Album[];

    libraryCtrl = new LibraryController(this);
    private searchCtrl = new SearchController(this);
    private lastSearchTerm = '';

    /** Tracks the store's cached array reference to detect refreshes. */
    private lastAlbumsRef: library.Album[] | null =
        null;

    // Fixed grid spacing constants.
    private static readonly GRID_GAP = 8;
    private static readonly GRID_PADDING = 8;
    private static readonly CARD_PADDING = 5;

    private ctxMenu = new ContextMenuController(this);
    private favCtrl = new FavoritesController(this);
    private selMgr = new AlbumSelectionManager();
    private scrollMgr = new ScrollManager(this, {
        GRID_GAP: CoverGrid.GRID_GAP,
        GRID_PADDING: CoverGrid.GRID_PADDING,
    });

    private lastSelectedAlbumIndex: number | null = null;
    private lastSelectedTrackIndex: number | null = null;

    /**
     * When true, the next split→single transition
     * skips the expensive overlay capture and scroll
     * restore.
     */
    private skipOverlay = false;

    /** Current card width — driven by the store. */
    get cardWidth(): number {
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
    get cardHeight(): number {
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

    // -- Memoisation caches for filtered albums --
    cachedFilteredAlbums: library.Album[] = [];
    private prevFilterAlbums: library.Album[] = [];
    private prevFilterTerm = '';
    private prevSortField: AlbumSortField = 'name';
    private prevSortDir: SortDirection = 'asc';

    // =================================================================
    // Filtered albums (memoised)
    // =================================================================

    /**
     * Recompute the filtered-albums cache when its
     * inputs have changed.  Called from willUpdate()
     * so the cache is ready before render().
     */
    private recomputeAlbumCache() {
        const term = this.searchCtrl.term;

        if (
            this.albums !== this.prevFilterAlbums ||
            term !== this.prevFilterTerm ||
            this.sortField !== this.prevSortField ||
            this.sortDirection !== this.prevSortDir
        ) {
            this.prevFilterAlbums = this.albums;
            this.prevFilterTerm = term;
            this.prevSortField = this.sortField;
            this.prevSortDir = this.sortDirection;
            this.cachedFilteredAlbums =
                this.computeFilteredAlbums();
        }
    }

    private computeFilteredAlbums(): library.Album[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        let albums: library.Album[];

        if (!term) {
            albums = this.albums;
        } else {
            albums = this.albums.filter(
                (a) =>
                    a.Name.toLowerCase().includes(
                        term,
                    ) ||
                    a.ArtistName.toLowerCase().includes(
                        term,
                    ),
            );
        }

        // Apply sort.
        const opt = ALBUM_SORT_OPTIONS.find(
            (o) => o.id === this.sortField,
        );

        if (!opt) return albums;

        const dir =
            this.sortDirection === 'asc' ? 1 : -1;

        return [...albums].sort(
            (a, b) => dir * opt.comparator(a, b),
        );
    }

    /** Wheel event handler ref for manual add/remove. */
    private wheelHandler = (e: WheelEvent) => {
        this.onWheel(e);
    };

    private wheelListenerAttached = false;

    // buildGridEntries() memoization cache.
    private gridEntriesCache: GridEntry[] = [];
    private gridEntriesCacheKey: library.Album[] = [];

    // getBeforeEntries/getAfterEntries memoization — prevents .slice()
    // from creating new array refs that trigger virtualizer relayout.
    private beforeEntriesCache: GridEntry[] = [];
    private afterEntriesCache: GridEntry[] = [];
    private splitEntriesCacheKey: GridEntry[] | null = null;
    private splitEntriesCacheIndex = -1;

    static override styles = [coverGridStyles, exploreLinkStyles];

    /* ====================================================================
     * Reactive state
     * ==================================================================== */

    @state()
    private albums: library.Album[] = [];

    @state()
    private loading = true;

    /** One tab stop for the grid, moved with the arrow keys — a card per
     *  tab stop makes a library-length tab sequence (H-5). */
    private roving = new RovingGridController(this, {
        cardSelector: '.album-card',
        count: () => this.buildGridEntries().length,
        scrollToIndex: (index) => this.scrollGridToIndex(index),
    });

    private contextMenuTarget: ContextMenuTarget = {
        kind: 'album',
    };

    /**
     * Album ID that was right-clicked to open the
     * context menu.  Used as fallback when the
     * right-clicked album is not part of the current
     * visual selection.
     */
    private contextMenuAlbumId: number | null = null;

    @state()
    private selectedAlbums: Set<number> = new Set();

    /** Current album sort field. */
    @state()
    private sortField: AlbumSortField = 'name';

    /** Current sort direction. */
    @state()
    private sortDirection: SortDirection = 'asc';

    /** ID of the album whose dropdown is currently open, or null. */
    @state()
    expandedAlbumId: number | null = null;

    /** Tracks loaded for the expanded album dropdown. */
    @state()
    expandedTracks: library.Track[] = [];

    /** Set of file paths of selected tracks inside the dropdown. */
    @state()
    private selectedTracks: Set<string> = new Set();

    /**
     * True when using the dual-virtualizer layout
     * (dropdown sandwiched between two grids).
     */
    @state()
    splitMode = false;

    /**
     * Index into this.albums where the split occurs.
     * Albums [0, splitIndex) go into the "before"
     * virtualizer; [splitIndex, length) go into "after".
     * Not `@state()` — always set before `splitMode`
     * changes, which triggers the render.
     */
    splitIndex = 0;

    @query('#context-menu')
    private contextMenuPopup!: WaPopup;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: WaPopup;

    // ContextMenuHost interface.
    getContextMenuPopup(): WaPopup | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup(): WaPopup | undefined {
        return this.playlistSubmenuPopup;
    }

    onContextMenuClose(): void {
        this.contextMenuAlbumId = null;
    }

    @query('track-details')
    private trackDetailsDialog!: TrackDetails;

    @query('#grid-single')
    private virtualizerSingle!: LitVirtualizer;

    @query('.grid-scroll-container')
    private scrollContainer!: HTMLElement;



    /* ====================================================================
     * Sort controls
     * ==================================================================== */

    /** Restore sort preferences from localStorage. */
    private restoreSortPreferences() {
        try {
            const field = localStorage.getItem(
                SORT_FIELD_KEY,
            );
            const dir =
                localStorage.getItem(SORT_DIR_KEY);

            if (
                field &&
                ALBUM_SORT_OPTIONS.some(
                    (o) => o.id === field,
                )
            ) {
                this.sortField =
                    field as AlbumSortField;
            }

            if (dir === 'asc' || dir === 'desc') {
                this.sortDirection = dir;
            }
        } catch {
            // Ignore storage errors.
        }
    }

    /** Persist sort preferences to localStorage. */
    private saveSortPreferences() {
        try {
            localStorage.setItem(
                SORT_FIELD_KEY,
                this.sortField,
            );
            localStorage.setItem(
                SORT_DIR_KEY,
                this.sortDirection,
            );
        } catch {
            // Ignore storage errors.
        }
    }

    /* ====================================================================
     * Lifecycle
     * ==================================================================== */

    override connectedCallback() {
        super.connectedCallback();
        this.restoreSortPreferences();
        this.loadAlbums();

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

        this.scrollMgr.teardown();
        this.scrollMgr.revealContainer(
            this.scrollContainer,
        );
    }

    override willUpdate(
        changed: Map<PropertyKey, unknown>,
    ) {
        super.willUpdate(changed);
        this.recomputeAlbumCache();

        // When the parent provides a new external album
        // list, update local albums and reset selection.
        if (changed.has('externalAlbums') && this.externalAlbums) {
            this.albums = this.externalAlbums;
            this.selMgr.setAlbums(this.externalAlbums);
            this.selectedAlbums = new Set();
            this.lastSelectedAlbumIndex = null;
            this.loading = false;
        }

        // When switching albums, expandedTracks
        // briefly goes to [].  Exit split mode so
        // the single virtualizer takes over while
        // loading.
        if (
            changed.has('expandedTracks') &&
            this.expandedTracks.length === 0 &&
            this.splitMode
        ) {
            const sm = this.scrollMgr;

            if (this.skipOverlay) {
                this.skipOverlay = false;
                sm.savedScrollTop =
                    sm.computeAdjustedScrollTop(
                        this.scrollContainer,
                        this.shadowRoot,
                    );
                sm.savedAlbumViewportOffset = null;
                this.splitMode = false;
                sm.needsScrollRestore = true;
                sm.showDropdownAfterRestore = false;
            } else {
                sm.savedScrollTop =
                    sm.computeAdjustedScrollTop(
                        this.scrollContainer,
                        this.shadowRoot,
                    );

                sm.captureAnchorOffset(
                    this.scrollContainer,
                    this.shadowRoot,
                );

                sm.captureOverlay(
                    this.scrollContainer,
                    this.shadowRoot,
                );
                this.splitMode = false;
                sm.needsScrollRestore = true;
                sm.showDropdownAfterRestore = false;
            }
        }

        // Enter split mode when tracks have loaded.
        if (
            changed.has('expandedTracks') &&
            this.expandedAlbumId !== null &&
            this.expandedTracks.length > 0
        ) {
            const sm = this.scrollMgr;

            if (!sm.restoreInFlight) {
                sm.savedScrollTop =
                    this.scrollContainer
                        ?.scrollTop ?? 0;
            }

            sm.captureOverlay(
                this.scrollContainer,
                this.shadowRoot,
            );
            this.splitIndex =
                sm.computeSplitIndex(
                    this.scrollContainer,
                );
            this.splitMode = true;
            sm.needsScrollRestore = true;
            sm.showDropdownAfterRestore = true;
        }

        // Exit split mode when the dropdown closes.
        if (
            changed.has('expandedAlbumId') &&
            this.expandedAlbumId === null &&
            this.splitMode
        ) {
            const sm = this.scrollMgr;

            sm.savedScrollTop =
                sm.computeAdjustedScrollTop(
                    this.scrollContainer,
                    this.shadowRoot,
                );
            sm.savedAlbumViewportOffset = null;

            sm.captureOverlay(
                this.scrollContainer,
                this.shadowRoot,
            );
            this.splitMode = false;
            sm.needsScrollRestore = true;
            sm.showDropdownAfterRestore = false;
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
        // transition (set in willUpdate).
        if (this.scrollMgr.needsScrollRestore) {
            this.scrollMgr.runScrollRestore(
                this.scrollContainer,
                this.shadowRoot,
                this.expandedAlbumId,
                this.updateComplete,
            );
        }

        // Close dropdown and clear selection when
        // the search term changes.
        const currentTerm = this.searchCtrl.term;

        if (currentTerm !== this.lastSearchTerm) {
            this.lastSearchTerm = currentTerm;
            this.closeDropdown();
            this.selectedAlbums = new Set();
            this.lastSelectedAlbumIndex = null;
        }

        // On zoom while dropdown is open, recompute
        // the split (column count may change) and
        // re-scroll after layout settles.
        if (
            cardSizeChanged &&
            this.splitMode &&
            this.expandedTracks.length > 0
        ) {
            this.splitIndex =
                this.scrollMgr.computeSplitIndex(
                    this.scrollContainer,
                );

            const sm = this.scrollMgr;

            void (async () => {
                await this.updateComplete;

                await sm.awaitBeforeLayout(
                    this.shadowRoot,
                );

                await sm.scrollToShowDropdown(
                    this.scrollContainer,
                    this.shadowRoot,
                );
            })();
        }

        // Re-fetch when the store delivers fresh
        // data after eager refetch on invalidation.
        if (!this.externalAlbums) {
            const cached =
                this.libraryCtrl.cachedAlbums;

            if (
                cached !== null &&
                cached !== this.lastAlbumsRef
            ) {
                this.lastAlbumsRef = cached;
                this.loadAlbums();
            }
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

        // Text sizing tiers mapped to design tokens.
        if (w < 160) {
            this.classList.add('size-small');
            this.style.setProperty(
                '--album-name-font',
                'var(--yj-text-xs)',
            );
            this.style.setProperty(
                '--artist-name-font',
                '10px',
            );
        } else if (w > 250) {
            this.classList.remove('size-small');
            this.style.setProperty(
                '--album-name-font',
                'var(--yj-text-lg)',
            );
            this.style.setProperty(
                '--artist-name-font',
                'var(--yj-text-md)',
            );
        } else {
            this.classList.remove('size-small');
            this.style.setProperty(
                '--album-name-font',
                'var(--yj-text-lg)',
            );
            this.style.setProperty(
                '--artist-name-font',
                'var(--yj-text-sm)',
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

            // When driven by an external album list,
            // skip the backend fetch entirely.
            const albums = this.externalAlbums
                ?? (await this.libraryCtrl.getAlbums())
                ?? [];

            this.albums = albums;
            this.selMgr.setAlbums(albums);
            this.selectedAlbums = new Set();
            this.lastSelectedAlbumIndex = null;
        } catch (error) {
            console.error(
                'Error loading albums:',
                error,
            );
            this.albums = [];
            this.selMgr.setAlbums([]);
        } finally {
            this.loading = false;
        }

        await this.updateComplete;

        this.scrollMgr.restoreScrollPosition(
            this.virtualizerSingle,
        );
        this.scrollMgr.setupResizeObserver(
            this.scrollContainer,
            async () => {
                this.splitIndex =
                    this.scrollMgr.computeSplitIndex(
                        this.scrollContainer,
                    );
                this.requestUpdate();

                await this.updateComplete;

                await this.scrollMgr.awaitBeforeLayout(
                    this.shadowRoot,
                );

                await this.scrollMgr.scrollToShowDropdown(
                    this.scrollContainer,
                    this.shadowRoot,
                );
            },
        );
    }

    /* ====================================================================
     * Scroll event handler
     * ==================================================================== */

    private onVisibilityChanged = (
        e: VisibilityChangedEvent,
    ) => {
        const sm = this.scrollMgr;
        const isSplit = this.splitMode;

        sm.onVisibilityChanged(e.first, () =>
            isSplit
                ? this.getBeforeEntries()
                : this.buildGridEntries(),
        );
    };

    /* ====================================================================
     * Virtualizer items
     * ==================================================================== */

    /**
     * Build a flat GridEntry array for the filtered
     * albums.  Memoized on the cachedFilteredAlbums
     * reference — only allocates a new array when the
     * underlying album list changes.
     */
    private buildGridEntries(): GridEntry[] {
        const filtered = this.cachedFilteredAlbums;

        if (filtered === this.gridEntriesCacheKey) {
            return this.gridEntriesCache;
        }

        const entries: GridEntry[] = [];

        for (let i = 0; i < filtered.length; i++) {
            entries.push({
                album: filtered[i]!,
                albumIndex: i,
            });
        }

        this.gridEntriesCacheKey = filtered;
        this.gridEntriesCache = entries;

        return entries;
    }

    /**
     * Bring a card into view, in whichever virtualizer currently holds
     * it.
     *
     * The roving tab stop's index is an index into the whole filtered
     * album list, but a split grid draws that list across *two*
     * virtualizers, each of which indexes only its own slice. This used
     * to be `querySelector('lit-virtualizer')` — always `#grid-before`
     * — so with a dropdown open, End scrolled the before-grid to an
     * index it does not contain and the card that should have taken
     * focus was never rendered to take it.
     */
    private scrollGridToIndex(index: number): void {
        const split = this.splitMode && this.expandedTracks.length > 0;
        const after = split && index >= this.splitIndex;

        const id = !split
            ? '#grid-single'
            : after
              ? '#grid-after'
              : '#grid-before';

        this.shadowRoot
            ?.querySelector<LitVirtualizer>(id)
            ?.scrollToIndex(after ? index - this.splitIndex : index, 'nearest');
    }

    /** Rebuild before/after caches if the entries or splitIndex changed. */
    private ensureSplitCache(): void {
        const entries = this.buildGridEntries();
        if (
            entries === this.splitEntriesCacheKey &&
            this.splitIndex === this.splitEntriesCacheIndex
        ) {
            return;
        }
        this.splitEntriesCacheKey = entries;
        this.splitEntriesCacheIndex = this.splitIndex;
        this.beforeEntriesCache = entries.slice(0, this.splitIndex);
        this.afterEntriesCache = entries.slice(this.splitIndex);
    }

    /** Entries for the "before" virtualizer (memoized). */
    private getBeforeEntries(): GridEntry[] {
        this.ensureSplitCache();
        return this.beforeEntriesCache;
    }

    /** Entries for the "after" virtualizer (memoized). */
    private getAfterEntries(): GridEntry[] {
        this.ensureSplitCache();
        return this.afterEntriesCache;
    }

    /* ====================================================================
     * Dropdown (expand/collapse)
     * ==================================================================== */

    /** Close the dropdown if one is open. */
    private closeDropdown() {
        if (this.expandedAlbumId === null) return;

        this.expandedAlbumId = null;
        this.expandedTracks = [];
        this.selectedTracks = new Set();
        this.lastSelectedTrackIndex = null;
    }

    /**
     * Open (or switch to) the given album's
     * dropdown. If the dropdown is already open
     * for the same album this is a no-op.
     */
    private async openDropdown(
        album: library.Album,
    ) {
        if (this.expandedAlbumId === album.ID) return;

        this.expandedAlbumId = album.ID;
        this.expandedTracks = [];
        this.selectedTracks = new Set();
        this.lastSelectedTrackIndex = null;

        try {
            const libId =
                this.libraryCtrl.selectedLibraryId;

            const tracks = libId !== null
                ? await GetAlbumTracksByLibrary(
                      album.ID,
                      libId,
                  )
                : await GetAlbumTracks(album.ID);

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
        const filtered = this.cachedFilteredAlbums;

        for (const el of path) {
            if (
                el instanceof HTMLElement &&
                el.classList.contains('album-card')
            ) {
                const raw = el.dataset['index'];

                if (raw === undefined) return null;

                const index = parseInt(raw, 10);
                const album = filtered[index];

                if (!album) return null;

                return { album, index };
            }
        }

        return null;
    }

    /** The queue source recorded when a full album starts playing. */
    private albumSource(album: library.Album): QueueSource {
        return { type: 'album', id: album.ID, label: album.Name };
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
            const range = this.selMgr.selectAlbumRange(
                this.lastSelectedAlbumIndex,
                index,
                this.cachedFilteredAlbums,
            );
            const next = new Set(this.selectedAlbums);

            for (const id of range) {
                next.add(id);
            }

            this.selectedAlbums = next;
            void this.selMgr.warmCache(
                this.selectedAlbums,
            );
        } else if (isCtrl) {
            const next = new Set(this.selectedAlbums);

            if (next.has(album.ID)) {
                next.delete(album.ID);
            } else {
                next.add(album.ID);
            }

            this.selectedAlbums = next;
            this.lastSelectedAlbumIndex = index;
            void this.selMgr.warmCache(
                this.selectedAlbums,
            );
        } else {
            // Plain click: navigate to explore album page.
            this.selectedAlbums = new Set();
            this.dispatchEvent(
                new CustomEvent('navigate', {
                    bubbles: true,
                    composed: true,
                    detail: {
                        view: 'explore-album-details',
                        releaseGroupMBID: album.MBID || '',
                        albumName: album.Name,
                        localAlbumId: album.ID,
                    },
                }),
            );
        }
    };

    private onGridAlbumDblClick = async (
        e: MouseEvent,
    ) => {
        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        const filePaths =
            await this.selMgr.getAlbumFilePaths(
                hit.album,
            );

        if (filePaths.length === 0) return;

        this.selectedAlbums = new Set();
        this.closeDropdown();
        queueStore.setQueue(filePaths, 0, true, this.albumSource(hit.album));
    };

    private onGridAlbumKeydown = (
        e: KeyboardEvent,
    ) => {
        if (isContextMenuKey(e)) {
            const target = this.resolveAlbumFromEvent(e);
            const card =
                e.composedPath().find(
                    (el): el is HTMLElement =>
                        el instanceof HTMLElement &&
                        el.classList.contains('album-card'),
                );

            if (!target || !card) return;

            e.preventDefault();
            e.stopPropagation();
            this.contextMenuAlbumId = target.album.ID;
            this.contextMenuTarget = { kind: 'album' };
            this.ctxMenu.openFrom(card);

            return;
        }

        if (e.key !== 'Enter' && e.key !== ' ') return;

        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        e.preventDefault();

        const { album, index } = hit;

        // Mirror plain-click behaviour: toggle
        // sole selection, or select and open.
        if (
            this.selectedAlbums.size === 1 &&
            this.selectedAlbums.has(album.ID)
        ) {
            this.selectedAlbums = new Set();
            this.closeDropdown();
        } else {
            this.selectedAlbums = new Set([album.ID]);
            void this.openDropdown(album);
        }

        this.lastSelectedAlbumIndex = index;
        void this.selMgr.warmCache(
            this.selectedAlbums,
        );
    };

    private onGridAlbumContextMenu = (
        e: MouseEvent,
    ) => {
        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        e.preventDefault();
        e.stopPropagation();

        this.contextMenuAlbumId = hit.album.ID;
        this.contextMenuTarget = { kind: 'album' };
        this.ctxMenu.openAt(e.clientX, e.clientY);
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
        const album = this.cachedFilteredAlbums[index];

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
            const range = this.selMgr.selectTrackRange(
                this.lastSelectedTrackIndex,
                index,
                this.expandedTracks,
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

        const album = this.albums.find(
            (a) => a.ID === this.expandedAlbumId,
        );

        queueStore.setQueue(
            filePaths,
            index,
            false,
            album ? this.albumSource(album) : undefined,
        );
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
        this.ctxMenu.openAt(clientX, clientY);
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
                this.selMgr.getSelectedTrackFilePaths(
                    this.selectedTracks,
                    this.expandedTracks,
                );
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

            this.dragImageEl =
                filePaths.length === 1
                    ? createTrackCardDragImage(
                          track.TrackName,
                          track.ArtistName,
                          track.FilePath,
                      )
                    : createDragImage(
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

    /**
     * Pre-warm the file-path cache for the album under
     * the pointer so that a subsequent dragstart (which
     * is synchronous) can read the paths immediately.
     */
    private onAlbumPointerDown = (e: PointerEvent) => {
        // Only act on primary button (left-click).
        if (e.button !== 0) return;

        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        // Fire-and-forget: warm the cache entry.
        void this.selMgr.warmSingleAlbum(hit.album);
    };

    private onAlbumDragStart = (e: DragEvent) => {
        const hit = this.resolveAlbumFromEvent(e);

        if (!hit) return;

        // Read file paths synchronously from the
        // pre-warmed cache.  The cache is populated
        // via pointerdown or selection-change warming.
        let filePaths: string[];
        let isSingleAlbum: boolean;

        if (this.selectedAlbums.has(hit.album.ID)) {
            // Dragged album is part of the selection —
            // drag all selected albums' tracks.
            filePaths =
                this.selMgr.getCachedSelectedPaths(
                    this.selectedAlbums,
                );
            isSingleAlbum =
                this.selectedAlbums.size === 1;
        } else {
            // Dragged album is not selected — clear
            // selection and drag only this album.
            this.selectedAlbums = new Set();
            filePaths =
                this.selMgr.getCachedAlbumPaths(
                    hit.album.ID,
                ) ?? [];
            isSingleAlbum = true;
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

        // Single album: show cover art thumbnail.
        // Multiple albums: show track-count badge.
        if (isSingleAlbum && hit.album.CoverArtPath) {
            this.dragImageEl =
                createAlbumArtDragImage(
                    this.getCoverUrl(hit.album),
                );
        } else {
            this.dragImageEl = createDragImage(
                filePaths.length,
            );
        }

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
        for (const el of e.composedPath()) {
            if (!(el instanceof HTMLElement)) continue;

            if (
                el.classList.contains('album-card') ||
                el.classList.contains('album-dropdown')
            ) {
                return;
            }
        }

        this.selectedAlbums = new Set();
        this.lastSelectedAlbumIndex = null;
        this.expandedAlbumId = null;
        this.expandedTracks = [];
        this.selectedTracks = new Set();
        this.lastSelectedTrackIndex = null;
    };

    /* ====================================================================
     * Context menu actions
     * ==================================================================== */

    private async onContextMenuAction(action: string) {
        let filePaths: string[];
        let source: QueueSource | undefined;

        if (this.contextMenuTarget.kind === 'track') {
            filePaths =
                this.selMgr.getSelectedTrackFilePaths(
                    this.selectedTracks,
                    this.expandedTracks,
                );

            const album = this.albums.find(
                (a) => a.ID === this.expandedAlbumId,
            );

            source = album ? this.albumSource(album) : undefined;
        } else {
            filePaths =
                await this.selMgr.getContextMenuAlbumFilePaths(
                    this.contextMenuAlbumId,
                    this.selectedAlbums,
                );

            // A single targeted album has an unambiguous source; a
            // multi-album selection does not.
            if (this.selectedAlbums.size <= 1) {
                const album = this.albums.find(
                    (a) => a.ID === this.contextMenuAlbumId,
                );

                source = album ? this.albumSource(album) : undefined;
            }
        }

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                queueStore.setQueue(filePaths, 0, true, source);
                break;
            case 'add-to-queue':
                queueStore.addTracksToQueue(filePaths);
                break;
            case 'play-next':
                queueStore.playTracksNext(filePaths);
                break;
            case 'track-details':
                if (filePaths.length === 1) {
                    void this.openTrackDetails(filePaths[0]!);
                } else {
                    void this.openBatchTrackDetails(filePaths);
                }
                break;
        }

        this.clearContextMenuSelection();
        this.ctxMenu.close();
    }

    private async onContextMenuFavoriteToggle() {
        const filePaths =
            await this.getPlaylistSubmenuFilePaths();

        if (filePaths.length === 0) return;

        if (this.favCtrl.allFavorited(filePaths)) {
            void this.favCtrl.removeFromFavorites(
                filePaths,
            );
        } else {
            void this.favCtrl.addToFavorites(
                filePaths,
            );
        }

        this.clearContextMenuSelection();
        this.ctxMenu.close();
    }

    /** Clear the selection that was active for the context menu. */
    private clearContextMenuSelection() {
        if (this.contextMenuTarget.kind === 'track') {
            this.selectedTracks = new Set();
        } else {
            this.selectedAlbums = new Set();
        }
    }

    private async openTrackDetails(filePath: string) {
        const track = tracksByFilePath(
            this.expandedTracks,
        ).get(filePath);

        if (!track) return;

        const ready = await loadTrackDetails(
            () => void this.openTrackDetails(filePath),
        );

        if (!ready) return;

        const coverArt =
            this.selMgr.resolveTrackCoverArt(
                track.Album,
                this.expandedAlbumId,
            );

        this.trackDetailsDialog?.show(
            track,
            coverArt ?? undefined,
        );
    }

    private async openBatchTrackDetails(
        filePaths: string[],
    ) {
        const tracks = tracksForPaths(
            this.expandedTracks,
            filePaths,
        );

        if (tracks.length === 0) return;

        const ready = await loadTrackDetails(
            () => void this.openBatchTrackDetails(filePaths),
        );

        if (!ready) return;

        const albumNames = new Set(
            tracks.map((t) => t.Album),
        );
        let coverArt: CoverArtUrls | null = null;
        let coverArtMixed = false;

        if (albumNames.size === 1) {
            const albumName = [...albumNames][0]!;
            coverArt =
                this.selMgr.resolveTrackCoverArt(
                    albumName,
                    this.expandedAlbumId,
                );
        } else {
            coverArtMixed = true;
        }

        this.trackDetailsDialog?.showBatch(
            tracks,
            coverArt,
            coverArtMixed,
        );
    }

    /** Resolve file paths for the playlist submenu. */
    private async getPlaylistSubmenuFilePaths(): Promise<
        string[]
    > {
        if (this.contextMenuTarget.kind === 'track') {
            return this.selMgr.getSelectedTrackFilePaths(
                this.selectedTracks,
                this.expandedTracks,
            );
        }

        return this.selMgr.getContextMenuAlbumFilePaths(
            this.contextMenuAlbumId,
            this.selectedAlbums,
        );
    }

    /* ====================================================================
     * Render: sort toolbar
     * ==================================================================== */

    /** Render the sort toolbar above the grid. */
    /**
     * The page header: title, count, sort, and what the header search
     * box is filtering by.  This was a hand-rolled toolbar written out
     * twice — the other copy was in `track-list` — which is how Albums
     * and Tracks came to have a sort control while Artists and Genres
     * had none (H-19).
     *
     * `externalAlbums` means this grid is a section of the artist page,
     * which has a heading of its own; there it keeps the count and the
     * sort and drops the title.
     */
    private renderPageHeader() {
        return html`
            <page-header
                heading=${this.externalAlbums === undefined ? 'Albums' : ''}
                .count=${this.cachedFilteredAlbums.length}
                count-noun="album"
                .sortOptions=${ALBUM_SORT_OPTIONS.map((o) => ({
                    id: o.id,
                    label: o.label,
                }))}
                sort-field=${this.sortField}
                sort-direction=${this.sortDirection}
                search-term=${this.searchCtrl.term}
                @sort-change=${this.onPageHeaderSort}
            ></page-header>
        `;
    }

    private onPageHeaderSort = (
        e: CustomEvent<{ field: string; direction: SortDirection }>,
    ) => {
        this.sortField = e.detail.field as AlbumSortField;
        this.sortDirection = e.detail.direction;
        this.saveSortPreferences();
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
                tabindex=${this.roving.tabIndexFor(index)}
                @focus=${() => this.roving.noteFocus(index)}
                role="option"
                aria-selected=${this.selectedAlbums.has(album.ID)}
                data-index=${index}
                aria-label="${album.Name} by ${album.ArtistName}"
                draggable="true"
                @pointerdown=${this.onAlbumPointerDown}
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
                        ${artistLink(album.ArtistName, album.ArtistMBID ?? '')}
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

        if (this.cachedFilteredAlbums.length === 0) {
            return html`
                ${this.renderPageHeader()}
                <div class="empty-state">
                    <p>No albums match your search.</p>
                </div>
            `;
        }

        // The split path draws the dropdown between two grids. Until
        // this was wired up, `render()` ignored `splitMode` entirely:
        // pressing Enter on an album card fetched its tracks over the
        // IPC, ran the whole split state machine (`splitMode: true`,
        // `splitIndex: 6`, measured against the real container) and
        // then drew the single grid regardless, so the only route from
        // the albums grid to a track was the plain click that
        // navigates away to the catalog page.
        const gridContent =
            this.splitMode && this.expandedTracks.length > 0
                ? this.renderSplitGrid()
                : this.renderSingleGrid();

        return html`
            ${this.renderPageHeader()}
            <div
                class="grid-scroll-container"
                @click=${this.onGridClick}
                @keydown=${this.roving.handleKeydown}
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
                role="listbox"
                aria-label="Albums"
                aria-multiselectable="true"
                .items=${this.buildGridEntries()}
                .renderItem=${this.renderGridEntry}
                .keyFunction=${(entry: GridEntry) => entry.album.ID}
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
     * Dual virtualizer — dropdown sandwiched between the "before" and
     * "after" halves of the grid.
     *
     * Both halves carry the same listbox semantics as the single grid:
     * they are one control to the user, and a selection that spans the
     * dropdown must be announced the same way on either side of it.
     */
    private renderSplitGrid() {
        const sm = this.scrollMgr;
        const ctr = this.scrollContainer;
        const containerW = sm.getContainerWidth(ctr);
        const rowW = sm.getGridRowWidth(ctr);
        const afterEntries = this.getAfterEntries();

        return html`
            <lit-virtualizer
                id="grid-before"
                role="listbox"
                aria-label="Albums"
                aria-multiselectable="true"
                .items=${this.getBeforeEntries()}
                .renderItem=${this.renderGridEntry}
                .keyFunction=${(entry: GridEntry) => entry.album.ID}
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
                .containerWidth=${containerW}
                .gridRowWidth=${rowW}
                .caratOffset=${sm.getCaratOffset(ctr)}
                style="margin-left:${(containerW - rowW) / 2}px"
                @track-click=${this.onTrackClick}
                @track-dblclick=${this.onTrackDblClick}
                @track-contextmenu=${this.onTrackContextMenu}
                @track-dragstart=${this.onTrackDragStart}
                @track-dragend=${this.onTrackDragEnd}
            ></album-dropdown>

            ${afterEntries.length > 0
                ? html`
                      <lit-virtualizer
                          id="grid-after"
                          role="listbox"
                          aria-label="Albums, continued"
                          aria-multiselectable="true"
                          .items=${afterEntries}
                          .renderItem=${this.renderGridEntry}
                          .keyFunction=${(entry: GridEntry) => entry.album.ID}
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
    /** Trigger playlist submenu with resolved file paths. */
    private async handleShowPlaylistSubmenu() {
        const filePaths =
            await this.getPlaylistSubmenuFilePaths();

        void this.ctxMenu.showPlaylistSubmenu(
            filePaths,
        );
    }

    private renderContextMenu() {
        const { ctxMenu } = this;

        return html`
            <wa-popup
                id="context-menu"
                placement="bottom-start"
                flip
                shift
                .active=${ctxMenu.contextMenuOpen}
            >
                ${ctxMenu.contextMenuOpen
                ? html`
                          <div
                              class="context-menu-panel"
                              role="menu"
                              aria-label=${this.contextMenuTarget.kind === 'track'
                        ? 'Track actions'
                        : 'Album actions'}
                          >
                              <wa-dropdown-item
                                  @click=${() =>
                        this.onContextMenuAction(
                            'play',
                        )}
                                  @mouseenter=${() =>
                        ctxMenu.closePlaylistSubmenu()}
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
                                  @mouseenter=${() =>
                        ctxMenu.closePlaylistSubmenu()}
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
                                  @mouseenter=${() =>
                        ctxMenu.closePlaylistSubmenu()}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="forward-step"
                                  ></wa-icon>
                                  Play Next
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  class="submenu-item"
                                  @mouseenter=${() => {
                        ctxMenu.clearSubmenuCloseTimer();
                        void this.handleShowPlaylistSubmenu();
                    }}
                                  @mouseleave=${ctxMenu
                        .scheduleSubmenuClose}
                                  @click=${(e: Event) => {
                        e.stopPropagation();
                        void this.handleShowPlaylistSubmenu();
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
                              <wa-dropdown-item
                                  @click=${() =>
                                      this.onContextMenuFavoriteToggle()}
                                  @mouseenter=${() =>
                                      ctxMenu.closePlaylistSubmenu()}
                              >
                                   <wa-icon
                                       slot="icon"
                                       name=${this.favCtrl.iconName}
                                   ></wa-icon>
                                  ${this.favCtrl.allFavorited(this.ctxMenu.playlistFilePaths) ? `Remove from ${this.favCtrl.playlistName}` : `Add to ${this.favCtrl.playlistName}`}
                              </wa-dropdown-item>
                              ${this.contextMenuTarget
                                      .kind ===
                                  'track'
                                  ? html`
                                        <wa-dropdown-item
                                            @click=${() =>
                                                this.onContextMenuAction(
                                                    'track-details',
                                                )}
                                            @mouseenter=${() =>
                                                ctxMenu.closePlaylistSubmenu()}
                                        >
                                            <wa-icon
                                                slot="icon"
                                                name="circle-info"
                                            ></wa-icon>
                                            Track
                                            Details
                                        </wa-dropdown-item>
                                    `
                                  : nothing}
                          </div>
                      `
                : nothing}
            </wa-popup>

            <wa-popup
                id="playlist-submenu"
                placement="right-start"
                flip
                shift
                .active=${ctxMenu.playlistSubmenuOpen}
            >
                ${ctxMenu.playlistSubmenuOpen
                ? html`
                          <div
                              @mouseenter=${() =>
                        ctxMenu.clearSubmenuCloseTimer()}
                              @mouseleave=${ctxMenu
                        .scheduleSubmenuClose}
                          >
                              <playlist-picker
                                  .filePaths=${ctxMenu
                        .playlistFilePaths}
                                  @playlist-action-complete=${ctxMenu
                        .onPlaylistActionComplete}
                                  @click=${(e: Event) =>
                        e.stopPropagation()}
                              ></playlist-picker>
                          </div>
                      `
                : nothing}
            </wa-popup>

            <track-details></track-details>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'cover-grid': CoverGrid;
    }
}

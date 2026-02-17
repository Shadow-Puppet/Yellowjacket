import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
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
} from './album-dropdown.js';

/**
 * Discriminated context menu target so we know whether the
 * context-menu is operating on albums or on tracks inside the
 * dropdown.
 */
type ContextMenuTarget =
    | { kind: 'album' }
    | { kind: 'track' };

/**
 * Union item for the virtualized grid.
 * Album entries carry the original album and its index
 * in this.albums.  Phantom entries are invisible
 * placeholders that reserve space for the dropdown overlay.
 */
type GridEntry =
    | {
          kind: 'album';
          album: library.Album;
          albumIndex: number;
      }
    | { kind: 'phantom'; phantomIndex: number };

/** Milliseconds to debounce visibility-changed saves. */
const SCROLL_DEBOUNCE_MS = 100;

@customElement('cover-grid')
export class CoverGrid extends LitElement {
    private libraryCtrl = new LibraryController(this);

    // Grid layout constants — must match the virtualizer
    // grid config and the CSS card dimensions.
    private static readonly GRID_ITEM_WIDTH = 176;
    private static readonly GRID_ITEM_HEIGHT = 230;
    private static readonly GRID_GAP = 16;
    private static readonly GRID_PADDING = 16;

    private lastSelectedAlbumIndex: number | null = null;
    private lastSelectedTrackIndex: number | null = null;
    private scrollDebounceTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    private closeHandler = () => this.closeContextMenu();

    /**
     * Tracks per phantom row.  Each track row is ~28px,
     * the base content height is 202px, each column fits
     * ~7 tracks, and 3 columns = 21 tracks per row.
     */
    private static readonly TRACKS_PER_PHANTOM_ROW = 21;

    // Virtualizer grid layout instance.
    private gridLayout = grid({
        itemSize: {
            width: `${CoverGrid.GRID_ITEM_WIDTH}px`,
            height: `${CoverGrid.GRID_ITEM_HEIGHT}px`,
        },
        gap: `${CoverGrid.GRID_GAP}px`,
        padding: `${CoverGrid.GRID_PADDING}px`,
        justify: 'center',
    });

    // buildVirtualizerItems() memoization cache.
    private itemsCacheAlbums: library.Album[] = [];
    private itemsCacheExpandedId: number | null = null;
    private itemsCacheColumns = 0;
    private itemsCachePhantomRows = 0;
    private itemsCache: GridEntry[] = [];

    static override styles = css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
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
            padding: 8px;
            transition: background-color 0.2s ease;
            box-sizing: border-box;
            width: 176px;
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
            font-size: 48px;
        }

        .album-info {
            margin-top: 8px;
            min-width: 0;
        }

        .album-name {
            font-size: 14px;
            font-weight: 600;
            color: #fff;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .artist-name {
            font-size: 12px;
            color: #b3b3b3;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            margin-top: 4px;
        }

        /* ========================================
         * Phantom cards (dropdown spacer)
         * ======================================== */

        .phantom-card {
            visibility: hidden;
            pointer-events: none;
        }

        /* ========================================
         * Dropdown overlay
         * ======================================== */

        .dropdown-overlay {
            position: absolute;
            left: 0;
            right: 0;
            z-index: 10;
            pointer-events: auto;
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

    /** Whether we're currently loading tracks for the dropdown. */
    @state()
    private loadingTracks = false;

    /** Set of file paths of selected tracks inside the dropdown. */
    @state()
    private selectedTracks: Set<string> = new Set();

    /** Number of phantom rows reserved for the dropdown overlay. */
    @state()
    private phantomRowCount = 1;

    /** Pixel offset of the dropdown overlay from the top of the scroll content. */
    @state()
    private dropdownTopPx = 0;

    @query('#context-menu')
    private contextMenuPopup!: HTMLElement;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: HTMLElement;

    @query('lit-virtualizer')
    private virtualizer!: LitVirtualizer;

    @query('.grid-scroll-container')
    private scrollContainer!: HTMLElement;

    // Resize-aware scroll preservation.
    private resizeObserver: ResizeObserver | null = null;
    private resizeDebounceTimer: ReturnType<
        typeof setTimeout
    > | null = null;
    private pendingFocus: {
        row: number;
        viewportOffset: number;
    } | null = null;
    private currentColumnCount = 0;

    /* ====================================================================
     * Lifecycle
     * ==================================================================== */

    override connectedCallback() {
        super.connectedCallback();
        this.loadAlbums();
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

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
        }

        if (this.resizeDebounceTimer !== null) {
            clearTimeout(this.resizeDebounceTimer);
        }

        this.resizeObserver?.disconnect();
        this.resizeObserver = null;
    }

    override updated(
        changed: Map<PropertyKey, unknown>,
    ) {
        super.updated(changed);

        // When the dropdown opens or tracks change,
        // recompute the phantom row count and overlay
        // position.
        if (
            changed.has('expandedAlbumId') ||
            changed.has('expandedTracks')
        ) {
            this.phantomRowCount =
                this.expandedAlbumId !== null
                    ? Math.max(
                          1,
                          Math.ceil(
                              this.expandedTracks
                                  .length /
                                  CoverGrid.TRACKS_PER_PHANTOM_ROW,
                          ),
                      )
                    : 1;
            this.updateDropdownPosition();
        }

        // When a new album is expanded, scroll its
        // row to the top of the viewport after the
        // DOM reflects the new phantom rows.
        if (
            changed.has('expandedAlbumId') &&
            this.expandedAlbumId !== null
        ) {
            void this.updateComplete.then(() => {
                this.scrollToExpandedAlbum();
            });
        }
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

        if (saved <= 0 || !this.virtualizer) return;

        const safeIndex = Math.min(
            saved,
            this.albums.length - 1,
        );

        if (safeIndex <= 0) return;

        this.virtualizer.scrollToIndex(
            safeIndex,
            'start',
        );
    }

    private onVisibilityChanged = (
        e: VisibilityChangedEvent,
    ) => {
        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
        }

        this.scrollDebounceTimer = setTimeout(() => {
            const items = this.buildVirtualizerItems();
            const first = items[e.first];

            if (first?.kind === 'album') {
                this.libraryCtrl.setScrollPosition(
                    'albums',
                    first.albumIndex,
                );
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
     * We compute a fractional album index at a focus
     * point before the resize, then after the reflow we
     * place that same index back at the same viewport
     * offset.
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

        const {
            GRID_ITEM_HEIGHT,
            GRID_GAP,
            GRID_PADDING,
        } = CoverGrid;
        const rowStep = GRID_ITEM_HEIGHT + GRID_GAP;

        this.currentColumnCount =
            this.getColumnCount();

        /** Restore scroll so the focus album stays
         *  at the same viewport offset after reflow. */
        const restoreScroll = () => {
            const pending = this.pendingFocus;

            this.pendingFocus = null;

            if (!pending) return;

            const newColumns = this.getColumnCount();

            this.currentColumnCount = newColumns;

            // If a dropdown is open, snap the
            // expanded album's row to the top.
            if (this.expandedAlbumId !== null) {
                this.updateDropdownPosition();
                this.scrollToExpandedAlbum();

                return;
            }

            // No dropdown — restore the
            // center-of-viewport position.
            const newY =
                GRID_PADDING + pending.row * rowStep;

            container.scrollTop =
                newY - pending.viewportOffset;
        };

        this.resizeObserver = new ResizeObserver(
            () => {
                // Capture on the first event using
                // the pre-resize column count.
                if (this.pendingFocus === null) {
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
     */
    private captureFocusPoint(
        container: HTMLElement,
        rowStep: number,
    ) {
        const { GRID_PADDING } = CoverGrid;

        // Prefer the expanded album as focus.
        if (this.expandedAlbumId !== null) {
            const idx = this.albums.findIndex(
                (a) => a.ID === this.expandedAlbumId,
            );

            if (idx >= 0) {
                const albumRow = Math.floor(
                    idx / this.currentColumnCount,
                );
                const albumY =
                    GRID_PADDING + albumRow * rowStep;

                this.pendingFocus = {
                    row: albumRow,
                    viewportOffset:
                        albumY - container.scrollTop,
                };

                return;
            }
        }

        // Fall back to the viewport center.
        const halfViewport =
            container.clientHeight / 2;
        const centerY =
            container.scrollTop + halfViewport;
        const row =
            (centerY - GRID_PADDING) / rowStep;

        this.pendingFocus = {
            row,
            viewportOffset: halfViewport,
        };
    }

    /* ====================================================================
     * Column count helper
     * ==================================================================== */

    private getColumnCount(): number {
        const el =
            this.scrollContainer ?? this.virtualizer;

        if (!el) return 1;

        const { GRID_ITEM_WIDTH, GRID_GAP, GRID_PADDING } =
            CoverGrid;
        const availableWidth =
            el.clientWidth - GRID_PADDING * 2;

        return Math.max(
            1,
            Math.floor(
                (availableWidth + GRID_GAP) /
                    (GRID_ITEM_WIDTH + GRID_GAP),
            ),
        );
    }

    /* ====================================================================
     * Virtualizer items (albums + phantom rows)
     *
     * When a dropdown is open we inject phantom entries
     * after the expanded album's row.  The dropdown is
     * rendered as an absolutely-positioned overlay on top
     * of the phantom cards.
     * ==================================================================== */

    private buildVirtualizerItems(): GridEntry[] {
        const columns = this.getColumnCount();
        const phantomRows =
            this.expandedAlbumId !== null
                ? this.phantomRowCount
                : 0;

        // Return cached result when inputs unchanged.
        if (
            this.itemsCacheAlbums === this.albums &&
            this.itemsCacheExpandedId ===
                this.expandedAlbumId &&
            this.itemsCacheColumns === columns &&
            this.itemsCachePhantomRows === phantomRows
        ) {
            return this.itemsCache;
        }

        const items: GridEntry[] = [];

        if (this.expandedAlbumId === null) {
            for (let i = 0; i < this.albums.length; i++) {
                items.push({
                    kind: 'album',
                    album: this.albums[i]!,
                    albumIndex: i,
                });
            }
        } else {
            const expandedIndex = this.albums.findIndex(
                (a) => a.ID === this.expandedAlbumId,
            );

            // Position after the expanded album's row.
            const insertAfter =
                expandedIndex >= 0
                    ? Math.min(
                          (Math.floor(
                              expandedIndex / columns,
                          ) +
                              1) *
                              columns,
                          this.albums.length,
                      )
                    : this.albums.length;

            const phantomCount = columns * phantomRows;

            for (let i = 0; i < insertAfter; i++) {
                items.push({
                    kind: 'album',
                    album: this.albums[i]!,
                    albumIndex: i,
                });
            }

            for (let i = 0; i < phantomCount; i++) {
                items.push({
                    kind: 'phantom',
                    phantomIndex: i,
                });
            }

            for (
                let i = insertAfter;
                i < this.albums.length;
                i++
            ) {
                items.push({
                    kind: 'album',
                    album: this.albums[i]!,
                    albumIndex: i,
                });
            }
        }

        // Update cache.
        this.itemsCache = items;
        this.itemsCacheAlbums = this.albums;
        this.itemsCacheExpandedId = this.expandedAlbumId;
        this.itemsCacheColumns = columns;
        this.itemsCachePhantomRows = phantomRows;

        return items;
    }

    private gridKeyFunction = (entry: GridEntry) => {
        if (entry.kind === 'phantom') {
            return `phantom-${entry.phantomIndex}`;
        }

        return `a-${entry.album.ID}`;
    };

    /* ====================================================================
     * Dropdown overlay positioning
     *
     * The dropdown is absolutely positioned inside the
     * scroll container, overlaying the phantom cards.
     * We compute its top offset from the expanded album's
     * row position in the grid.
     * ==================================================================== */

    /**
     * Smooth-scroll the container so the expanded
     * album's row is positioned at the top of the view.
     */
    private scrollToExpandedAlbum() {
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

        const {
            GRID_ITEM_HEIGHT,
            GRID_GAP,
            GRID_PADDING,
        } = CoverGrid;
        const columns = this.getColumnCount();
        const rowStep = GRID_ITEM_HEIGHT + GRID_GAP;
        const albumRow = Math.floor(
            expandedIndex / columns,
        );

        container.scrollTo({
            top:
                GRID_PADDING +
                albumRow * rowStep -
                GRID_GAP / 2,
            behavior: 'instant',
        });
    }

    private updateDropdownPosition() {
        if (this.expandedAlbumId === null) return;

        const columns = this.getColumnCount();
        const expandedIndex = this.albums.findIndex(
            (a) => a.ID === this.expandedAlbumId,
        );

        if (expandedIndex < 0) return;

        const {
            GRID_ITEM_HEIGHT,
            GRID_GAP,
            GRID_PADDING,
        } = CoverGrid;
        const rowStep = GRID_ITEM_HEIGHT + GRID_GAP;
        const phantomStartRow =
            Math.floor(expandedIndex / columns) + 1;

        this.dropdownTopPx =
            GRID_PADDING + phantomStartRow * rowStep;
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
            this.phantomRowCount = 1;

            return;
        }

        // Open (or switch)
        this.expandedAlbumId = album.ID;
        this.expandedTracks = [];
        this.selectedTracks = new Set();
        this.lastSelectedTrackIndex = null;
        this.phantomRowCount = 1;
        this.loadingTracks = true;

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
        } finally {
            this.loadingTracks = false;
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
        } else if (isCtrl) {
            const next = new Set(this.selectedAlbums);

            if (next.has(album.ID)) {
                next.delete(album.ID);
            } else {
                next.add(album.ID);
            }

            this.selectedAlbums = next;
            this.lastSelectedAlbumIndex = index;
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

    /* ====================================================================
     * Render: grid entry (virtualizer renderItem)
     * ==================================================================== */

    private renderGridEntry = (
        entry: GridEntry,
    ) => {
        if (entry.kind === 'phantom') {
            return html`<div
                class="phantom-card"
            ></div>`;
        }

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

        return html`
            <div
                class=${classes}
                tabindex="0"
                role="button"
                data-index=${index}
                aria-label="${album.Name} by ${album.ArtistName}"
            >
                <div class="cover-container">
                    ${album.CoverArtPath
                ? html`<img
                              class="cover-image"
                              src="${album.CoverArtThumbnailPath || album.CoverArtPath}"
                              alt="${album.Name} cover"
                              width="160"
                              height="160"
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
                        ${album.Name}
                    </div>
                    <div
                        class="artist-name"
                        title="${album.ArtistName}"
                    >
                        ${album.ArtistName}${album.Year
                ? ` - ${album.Year}`
                : ''}
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

        return html`
            <div
                class="grid-scroll-container"
                @click=${this.onGridClick}
            >
                <lit-virtualizer
                    .items=${this.buildVirtualizerItems()}
                    .renderItem=${this.renderGridEntry}
                    .keyFunction=${this.gridKeyFunction}
                    .layout=${this.gridLayout}
                    @click=${this.onGridAlbumClick}
                    @dblclick=${this.onGridAlbumDblClick}
                    @keydown=${this.onGridAlbumKeydown}
                    @contextmenu=${this.onGridAlbumContextMenu}
                    @visibilityChanged=${this.onVisibilityChanged}
                ></lit-virtualizer>

                ${this.expandedAlbumId !== null
                ? html`
                          <album-dropdown
                              class="dropdown-overlay"
                              style="top:${this.dropdownTopPx}px"
                              .tracks=${this.expandedTracks}
                              ?loading-tracks=${this.loadingTracks}
                              .selectedTracks=${this.selectedTracks}
                              .phantomRows=${this.phantomRowCount}
                              @track-click=${this.onTrackClick}
                              @track-dblclick=${this.onTrackDblClick}
                              @track-contextmenu=${this.onTrackContextMenu}
                          ></album-dropdown>
                      `
                : nothing}
            </div>

            <wa-popup
                id="context-menu"
                placement="bottom-start"
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

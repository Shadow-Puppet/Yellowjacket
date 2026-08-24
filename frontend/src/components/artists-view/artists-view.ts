import { LitElement, html, css, nothing } from 'lit';
import {
    customElement,
    state,
    query,
} from 'lit/decorators.js';
import '@lit-labs/virtualizer';
import type {
    LitVirtualizer,
    RangeChangedEvent,
    VisibilityChangedEvent,
} from '@lit-labs/virtualizer';
import { grid } from '@lit-labs/virtualizer/layouts/grid.js';
import { gridSpacingFor } from '@utils/grid-spacing';
import {
    GetAlbumsByArtist,
    GetFilePathsByAlbums,
} from '@go/library/library.js';
import * as library from '@go/library/models.js';
import { LibraryController } from '@store/controllers/library-controller';
import { libraryStore } from '@store/library-store';
import { SearchController } from '@store/controllers/search-controller';
import '@components/page-header/page-header';
import { queueStore } from '@store/queue-store';
import {
    ContextMenuController,
    contextMenuStyles,
    isContextMenuKey,
} from '@utils/context-menu-controller.js';
import type { ContextMenuHost, MenuTarget } from '@utils/context-menu-controller.js';
import { FavoritesController } from '@store/controllers/favorites-controller';
import { ViewLifecycleMixin } from '@utils/view-lifecycle';
import { RovingGridController } from '@utils/roving-grid';
import { prefetchImageWindow } from '@utils/image-prefetch';

import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import type { MenuSurface } from '../menu-surface/menu-surface';
import '../menu-surface/menu-surface';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@components/playlist-picker/playlist-picker.js';
import { dict, list } from '@utils/binding';
import {
    ICON_PLAYLIST,
    ICON_QUEUE,
} from '@utils/icon-language';

/** Pixels to change card width per scroll tick. */
const ZOOM_STEP = 16;

/** localStorage key for persisted artist card size. */
const CARD_SIZE_KEY = 'artists-view-card-size';

/** Card size limits. */
const CARD_SIZE_MIN = 100;
const CARD_SIZE_MAX = 350;
const CARD_SIZE_DEFAULT = 176;

/** Debounce delay for saving scroll position. */
const SCROLL_DEBOUNCE_MS = 100;

/**
 * Grid entry for the virtualized artist grid.
 */
const ARTIST_SORT_DIR_KEY = 'artists-view-sort-dir';

/** One key, so the header renders a label and a direction button. */
const ARTIST_SORT_OPTIONS = [{ id: 'name', label: 'Name' }];

interface ArtistEntry {
    artist: library.Artist;
    index: number;
}

@customElement('artists-view')
export class ArtistsView
    extends ViewLifecycleMixin(LitElement)
    implements ContextMenuHost
{
    private libraryCtrl = new LibraryController(this);
    private searchCtrl = new SearchController(this);
    private ctxMenu = new ContextMenuController(this);
    private favCtrl = new FavoritesController(this);
    private wheelListenerAttached = false;
    private lastSearchTerm = '';

    /** One tab stop for the whole grid, moved with the arrows — a card
     *  per tab stop makes a library-length tab sequence (H-5). */
    private roving = new RovingGridController(this, {
        cardSelector: '.artist-card',
        count: () => this.cachedGridEntries.length,
        scrollToIndex: (index) => {
            this.shadowRoot
                ?.querySelector<LitVirtualizer>('lit-virtualizer')
                ?.scrollToIndex(index, 'nearest');
        },
    });

    /** Tracks the store's cached array reference to detect refreshes. */
    private lastArtistsRef: library.Artist[] | null =
        null;
    private scrollDebounceTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    @state()
    private artists: library.Artist[] = [];

    @state()
    private loading = true;

    @state()
    private restoringScroll = false;

    @state()
    private cardSize: number = CARD_SIZE_DEFAULT;

    // ----- Multi-select state -----

    @state()
    private selectedArtists: Set<number> = new Set();

    private lastSelectedArtistIndex: number | null =
        null;

    // ----- Context menu state -----

    /**
     * Artist ID that was right-clicked to open the
     * context menu.  Used as fallback when the
     * right-clicked artist is not in the current
     * visual selection.
     */
    private contextMenuArtistId: number | null = null;

    @query('#context-menu')
    private contextMenuPopup!: MenuSurface;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: MenuSurface;

    getContextMenuPopup(): MenuTarget | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup():
        | MenuTarget
        | undefined {
        return this.playlistSubmenuPopup;
    }

    onContextMenuClose(): void {
        this.contextMenuArtistId = null;
    }

    // ----- Grid spacing constants -----

    private static readonly CARD_PADDING = 5;

    private get imageSize(): number {
        return (
            this.cardSize -
            ArtistsView.CARD_PADDING * 2
        );
    }

    private get cardTextHeight(): number {
        const w = this.cardSize;

        if (w < 160) return 30;
        if (w > 250) return 42;

        return 36;
    }

    /** Wheel handler reference for add/remove. */
    private wheelHandler = (e: WheelEvent) => {
        this.onWheel(e);
    };

    private gridLayout = this.createGridLayout();

    private createGridLayout() {
        const w = this.cardSize ?? CARD_SIZE_DEFAULT;
        const h = w + this.cardTextHeight;

        // One number for the gap, the row gap and the padding: whatever
        // a row could not spend on another card, shared out equally, so
        // the outside is never wider than the inside.  See
        // `utils/grid-spacing.ts`.
        const spacing = this.spacingFor(this.containerWidth);

        this.lastLayoutSpacing = spacing;

        return grid({
            itemSize: {
                width: `${w}px`,
                height: `${h}px`,
            },
            gap: `${spacing}px`,
            padding: `${spacing}px`,
            justify: 'start',
        });
    }

    /** The width the grid lays itself out in. */
    private get containerWidth(): number {
        return (
            this.renderRoot?.querySelector<HTMLElement>(
                '.grid-scroll-container',
            )?.clientWidth ||
            this.clientWidth ||
            0
        );
    }

    private spacingFor(width: number): number {
        return gridSpacingFor(width, this.cardSize);
    }

    /** Sort direction for the artist grid.
     *
     *  There is only one key to sort by: `library.Artist` carries a
     *  name, an MBID and three image URLs, and nothing countable — so
     *  the header shows "Sort: Name" and this button, rather than a
     *  select with one option in it (H-19). */
    @state()
    private sortDirection: 'asc' | 'desc' = 'asc';

    // -- Memoisation caches for filtered artists --
    private cachedFilteredArtists: library.Artist[] =
        [];
    private cachedGridEntries: ArtistEntry[] = [];
    private prevFilterArtists: library.Artist[] = [];
    private prevFilterTerm = '';
    private prevFilterDir: 'asc' | 'desc' | '' = '';

    /**
     * Recompute the filtered-artists and grid-entries
     * caches when their inputs have changed.  Called
     * from willUpdate() so the caches are ready
     * before render().
     */
    private recomputeArtistCaches() {
        const term = this.searchCtrl.term;

        if (
            this.artists !== this.prevFilterArtists ||
            term !== this.prevFilterTerm ||
            this.sortDirection !== this.prevFilterDir
        ) {
            this.prevFilterArtists = this.artists;
            this.prevFilterTerm = term;
            this.prevFilterDir = this.sortDirection;
            this.cachedFilteredArtists =
                this.computeFilteredArtists();
            this.cachedGridEntries =
                this.cachedFilteredArtists.map(
                    (artist, index) => ({
                        artist,
                        index,
                    }),
                );
        }
    }

    private computeFilteredArtists(): library.Artist[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        const matching = term
            ? this.artists.filter((a) =>
                  a.Name.toLowerCase().includes(term),
              )
            : this.artists;

        // Descending is the only reordering available, so ascending
        // keeps the backend's order rather than re-sorting it: the
        // array's identity is what tells the virtualizer to repaint,
        // and copying it every pass would repaint on every keystroke.
        if (this.sortDirection === 'asc') return matching;

        return [...matching].sort((a, b) =>
            b.Name.localeCompare(a.Name),
        );
    }

    private onPageHeaderSort = (
        e: CustomEvent<{ direction: 'asc' | 'desc' }>,
    ) => {
        this.sortDirection = e.detail.direction;

        try {
            localStorage.setItem(
                ARTIST_SORT_DIR_KEY,
                this.sortDirection,
            );
        } catch {
            // Ignore storage errors.
        }
    };

    static override styles = [
        contextMenuStyles,
        css`
            :host {
                display: flex;
                flex-direction: column;
                overflow: hidden;
                height: 100%;
                position: relative;
                contain: layout style;
            }

            .grid-scroll-container {
                flex: 1;
                overflow-y: auto;
                overflow-x: hidden;
                contain: paint;
            }

            lit-virtualizer {
                width: 100%;
                min-height: 100%;
            }

            .artist-card {
                display: flex;
                flex-direction: column;
                align-items: center;
                padding: 5px;
                border-radius: 8px;
                cursor: pointer;
                /* transitions removed — software rendering repaints per frame */
                overflow: hidden;
            }

            .artist-card:hover {
                background-color: var(
                    --yj-bg-overlay,
                    rgba(255, 255, 255, 0.06)
                );
            }

            .artist-card:active {
                transform: scale(0.97);
            }

            .artist-card.selected {
                outline: 2px solid
                    var(--yj-accent, #ffd43b);
                outline-offset: 2px;
            }

            .artist-card.selected
                .avatar-container {
                scale: 0.95;
            }

            .artist-card.selected .artist-name {
                scale: 0.95;
            }

            .avatar-container {
                width: var(--avatar-size);
                height: var(--avatar-size);
                border-radius: 50%;
                overflow: hidden;
                background: linear-gradient(
                    135deg,
                    var(--yj-bg-overlay, #404040) 0%,
                    var(--yj-bg-surface, #282828)
                        100%
                );
                display: flex;
                align-items: center;
                justify-content: center;
                flex-shrink: 0;
            }

            .avatar-image {
                width: 100%;
                height: 100%;
                object-fit: cover;
                border-radius: 50%;
            }

            .avatar-placeholder {
                color: var(
                    --yj-text-secondary,
                    #b3b3b3
                );
                font-size: var(
                    --placeholder-font,
                    48px
                );
                font-weight: 600;
                text-transform: uppercase;
                user-select: none;
                line-height: 1;
            }

            .artist-name {
                width: 100%;
                text-align: center;
                font-size: var(
                    --artist-name-font,
                    14px
                );
                font-weight: 500;
                color: var(
                    --yj-text-primary,
                    #fff
                );
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                padding: var(--artist-name-pad, 6px)
                    2px 0;
                line-height: 1.3;
            }

            .search-bar-row {
                position: relative;
                display: flex;
                align-items: center;
                justify-content: center;
                min-height: 30px;
                border-bottom: 1px solid
                    var(--yj-border-subtle, #333);
                flex-shrink: 0;
                user-select: none;
            }

            .search-indicator {
                position: absolute;
                left: 50%;
                transform: translateX(-50%);
                pointer-events: none;
                background: var(
                    --yj-bg-overlay,
                    #495057
                );
                color: var(
                    --yj-text-secondary,
                    #b3b3b3
                );
                font-size: 12px;
                padding: 2px 14px;
                border-radius: 12px;
                border: 1px solid
                    var(--yj-border-subtle, #555);
                white-space: nowrap;
                opacity: 0.92;
            }

            .loading-message,
            .empty-message {
                display: flex;
                align-items: center;
                justify-content: center;
                height: 100%;
                color: var(
                    --yj-text-secondary,
                    #b3b3b3
                );
                font-size: 14px;
            }

        `,
    ];

    /* ================================================================
     * Lifecycle
     * ================================================================ */

    override willUpdate(
        changed: Map<PropertyKey, unknown>,
    ) {
        super.willUpdate(changed);
        this.recomputeArtistCaches();
    }

    override connectedCallback() {
        super.connectedCallback();
        this.loadCardSize();
        this.loadSortDirection();
        this.loadArtists();
    }

    private loadSortDirection() {
        try {
            const saved = localStorage.getItem(
                ARTIST_SORT_DIR_KEY,
            );

            if (saved === 'asc' || saved === 'desc') {
                this.sortDirection = saved;
            }
        } catch {
            // Ignore storage errors.
        }
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.detachWheelListener();
        this.gridResizeObserver?.disconnect();
        this.gridResizeObserver = null;
    }

    /** The wheel listener and the scroll debounce belong to the grid
     *  while it is on screen; off-screen it cannot be scrolled, and a
     *  cached view is never disconnected. */
    protected override onViewDeactivate(): void {
        this.detachWheelListener();

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
            this.scrollDebounceTimer = null;
        }
    }

    override updated() {
        this.updateSizeProperties();
        this.ensureWheelListener();
        this.updateGridLayout();

        // Clear selection when search term changes.
        const currentTerm = this.searchCtrl.term;

        if (currentTerm !== this.lastSearchTerm) {
            this.lastSearchTerm = currentTerm;
            this.clearSelection();
        }

        // Re-fetch when the store delivers fresh
        // data after eager refetch on invalidation.
        const cached =
            this.libraryCtrl.cachedArtists;

        if (
            cached !== null &&
            cached !== this.lastArtistsRef
        ) {
            this.lastArtistsRef = cached;
            this.loadArtists();
        }
    }

    /* ================================================================
     * Data loading
     * ================================================================ */

    private async loadArtists() {
        try {
            this.loading = true;

            const artists =
                await this.libraryCtrl.getArtists();

            this.artists = artists ?? [];
        } catch (error) {
            console.error(
                'Error loading artists:',
                error,
            );
            this.artists = [];
        } finally {
            const saved =
                this.libraryCtrl.getScrollPosition(
                    'artists',
                );

            this.restoringScroll = saved > 0;
            this.loading = false;
        }

        await this.updateComplete;
        this.restoreScrollPosition();
    }

    /* ================================================================
     * Scroll position persistence
     * ================================================================ */

    /**
     * Warm the avatars just past the rendered range (#65).
     *
     * `rangeChanged` is the rendered range and `visibilityChanged` is
     * what is on screen; the virtualizer has already drawn about
     * 1000px past the latter, so that is the wrong anchor to measure a
     * prefetch window from. It is deliberately outside the
     * `restoringScroll` guard below: a restored scroll lands in the
     * middle of the grid, which is exactly when nothing around it is
     * cached.
     */
    private onRangeChanged = (e: RangeChangedEvent) => {
        prefetchImageWindow(
            this.cachedGridEntries,
            e.first,
            e.last,
            (entry) => this.artistAvatarURL(entry.artist),
        );
    };

    /**
     * Save the first visible item index on scroll.
     */
    private onVisibilityChanged = (
        e: VisibilityChangedEvent,
    ) => {
        if (this.restoringScroll) return;

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
        }

        this.scrollDebounceTimer = setTimeout(
            () => {
                this.libraryCtrl.setScrollPosition(
                    'artists',
                    e.first,
                );
            },
            SCROLL_DEBOUNCE_MS,
        );
    };

    /**
     * Restore scroll position from the store.
     */
    private restoreScrollPosition(): void {
        const saved =
            this.libraryCtrl.getScrollPosition(
                'artists',
            );

        if (saved <= 0) {
            this.restoringScroll = false;

            return;
        }

        const virt =
            this.shadowRoot?.querySelector(
                'lit-virtualizer',
            ) as LitVirtualizer | null;

        if (!virt) {
            this.restoringScroll = false;

            return;
        }

        const safeIndex = Math.min(
            saved,
            this.cachedFilteredArtists.length - 1,
        );

        if (safeIndex <= 0) {
            this.restoringScroll = false;

            return;
        }

        virt.scrollToIndex(safeIndex, 'start');
        this.restoringScroll = false;
    }

    /* ================================================================
     * Card size (zoom)
     * ================================================================ */

    private loadCardSize(): void {
        try {
            const stored =
                localStorage.getItem(CARD_SIZE_KEY);

            if (stored !== null) {
                const parsed = parseInt(stored, 10);

                if (!Number.isNaN(parsed)) {
                    this.cardSize = Math.max(
                        CARD_SIZE_MIN,
                        Math.min(
                            CARD_SIZE_MAX,
                            parsed,
                        ),
                    );
                }
            }
        } catch {
            // localStorage may be unavailable.
        }
    }

    private saveCardSize(): void {
        try {
            localStorage.setItem(
                CARD_SIZE_KEY,
                String(this.cardSize),
            );
        } catch {
            // localStorage may be unavailable.
        }
    }

    private setCardSize(size: number): void {
        const clamped = Math.round(
            Math.max(
                CARD_SIZE_MIN,
                Math.min(CARD_SIZE_MAX, size),
            ),
        );

        if (clamped === this.cardSize) return;

        this.cardSize = clamped;
        this.saveCardSize();
    }

    /* ================================================================
     * Wheel zoom (Ctrl+scroll)
     * ================================================================ */

    private onWheel(e: WheelEvent) {
        if (!e.ctrlKey) return;

        e.preventDefault();

        const delta =
            e.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP;

        this.setCardSize(this.cardSize + delta);
    }

    private ensureWheelListener() {
        const container =
            this.shadowRoot?.querySelector(
                '.grid-scroll-container',
            );

        if (
            container &&
            !this.wheelListenerAttached
        ) {
            container.addEventListener(
                'wheel',
                this
                    .wheelHandler as EventListener,
                { passive: false },
            );
            this.wheelListenerAttached = true;
        }
    }

    private detachWheelListener() {
        const container =
            this.shadowRoot?.querySelector(
                '.grid-scroll-container',
            );

        if (
            container &&
            this.wheelListenerAttached
        ) {
            container.removeEventListener(
                'wheel',
                this
                    .wheelHandler as EventListener,
            );
            this.wheelListenerAttached = false;
        }
    }

    /* ================================================================
     * Grid layout
     * ================================================================ */

    private lastLayoutWidth = 0;
    private lastLayoutSpacing = 0;

    /** Watches the scroller so a window resize rebuilds the layout:
     *  the spacing is derived from its width, and nothing else asks
     *  this view to update when only that changes. */
    private gridResizeObserver: ResizeObserver | null = null;

    private observeGridWidth() {
        const container =
            this.renderRoot?.querySelector<HTMLElement>(
                '.grid-scroll-container',
            );

        if (!container || this.gridResizeObserver) return;

        this.gridResizeObserver = new ResizeObserver(() =>
            this.requestUpdate(),
        );
        this.gridResizeObserver.observe(container);
    }

    private updateGridLayout() {
        this.observeGridWidth();

        if (
            this.cardSize === this.lastLayoutWidth &&
            this.lastLayoutSpacing ===
                this.spacingFor(this.containerWidth)
        ) {
            return;
        }

        this.lastLayoutWidth = this.cardSize;
        this.gridLayout = this.createGridLayout();
    }

    /* ================================================================
     * Dynamic size properties
     * ================================================================ */

    private updateSizeProperties() {
        const w = this.cardSize;

        if (w < 160) {
            this.style.setProperty(
                '--artist-name-font',
                '12px',
            );
            this.style.setProperty(
                '--artist-name-pad',
                '4px',
            );
        } else if (w > 250) {
            this.style.setProperty(
                '--artist-name-font',
                '15px',
            );
            this.style.setProperty(
                '--artist-name-pad',
                '8px',
            );
        } else {
            this.style.setProperty(
                '--artist-name-font',
                '14px',
            );
            this.style.setProperty(
                '--artist-name-pad',
                '6px',
            );
        }
    }

    /* ================================================================
     * Artist selection helpers
     * ================================================================ */

    /**
     * Select a contiguous range of artist IDs
     * between two indices in filteredArtists.
     */
    private selectArtistRange(
        from: number,
        to: number,
    ): Set<number> {
        const filtered = this.cachedFilteredArtists;
        const start = Math.min(from, to);
        const end = Math.max(from, to);
        const ids = new Set<number>();

        for (let i = start; i <= end; i++) {
            const artist = filtered[i];

            if (artist) {
                ids.add(artist.ID);
            }
        }

        return ids;
    }

    /**
     * Fetches all file paths for every selected
     * artist.
     */
    private async getSelectedArtistFilePaths(): Promise<
        string[]
    > {
        const selected = this.artists.filter((a) =>
            this.selectedArtists.has(a.ID),
        );
        const allPaths: string[] = [];

        for (const artist of selected) {
            const paths =
                await this.getArtistFilePaths(
                    artist,
                );
            allPaths.push(...paths);
        }

        return allPaths;
    }

    /**
     * Return file paths for the context menu target.
     * If the right-clicked artist is part of the
     * current selection, return paths for all selected
     * artists.  Otherwise return paths for the
     * right-clicked artist only.
     */
    private async getContextMenuArtistFilePaths(): Promise<
        string[]
    > {
        if (
            this.contextMenuArtistId !== null &&
            !this.selectedArtists.has(
                this.contextMenuArtistId,
            )
        ) {
            const artist = this.artists.find(
                (a) =>
                    a.ID ===
                    this.contextMenuArtistId,
            );

            if (artist) {
                return this.getArtistFilePaths(
                    artist,
                );
            }

            return [];
        }

        return this.getSelectedArtistFilePaths();
    }

    /** Clear the current artist selection. */
    private clearSelection() {
        this.selectedArtists = new Set();
        this.lastSelectedArtistIndex = null;
    }

    /* ================================================================
     * Artist card click
     * ================================================================ */

    private onArtistClick(
        e: MouseEvent,
        artist: library.Artist,
        index: number,
    ) {
        const isCtrl = e.ctrlKey || e.metaKey;
        const isShift = e.shiftKey;

        if (
            isShift &&
            this.lastSelectedArtistIndex !== null
        ) {
            const range = this.selectArtistRange(
                this.lastSelectedArtistIndex,
                index,
            );
            const next = new Set(
                this.selectedArtists,
            );

            for (const id of range) {
                next.add(id);
            }

            this.selectedArtists = next;
        } else if (isCtrl) {
            const next = new Set(
                this.selectedArtists,
            );

            if (next.has(artist.ID)) {
                next.delete(artist.ID);
            } else {
                next.add(artist.ID);
            }

            this.selectedArtists = next;
            this.lastSelectedArtistIndex = index;
        } else {
            // Plain click: navigate to details.
            this.clearSelection();
            this.dispatchEvent(
                new CustomEvent('navigate', {
                    bubbles: true,
                    composed: true,
                    detail: {
                        view: 'explore-artist-details',
                        artistMBID: artist.MBID || '',
                        artistName: artist.Name,
                        localArtistId: artist.ID,
                    },
                }),
            );
        }
    }

    /* ================================================================
     * Context menu
     * ================================================================ */

    private onArtistContextMenu = (
        e: MouseEvent,
        artist: library.Artist,
    ) => {
        e.preventDefault();
        e.stopPropagation();

        this.contextMenuArtistId = artist.ID;

        this.ctxMenu.openAt(
            e.clientX,
            e.clientY,
        );
    };

    /** Shift+F10 / ContextMenu on a focused card, anchored to the card
     *  so the menu appears where the artist is and focus goes back
     *  there when it closes. */
    private openArtistMenuFromKey(
        e: KeyboardEvent,
        artist: library.Artist,
    ): void {
        const card = e.currentTarget as HTMLElement | null;

        if (!card) return;

        e.preventDefault();
        e.stopPropagation();
        this.contextMenuArtistId = artist.ID;
        this.ctxMenu.openFrom(card);
    }

    private async onContextMenuAction(
        action: string,
    ) {
        const filePaths =
            await this.getContextMenuArtistFilePaths();

        if (filePaths.length === 0) return;

        const artist = this.artists.find(
            (a) => a.ID === this.contextMenuArtistId,
        );

        switch (action) {
            case 'play':
                queueStore.setQueue(
                    filePaths,
                    0,
                    true,
                    artist
                        ? { type: 'artist', id: artist.ID, label: artist.Name }
                        : undefined,
                );
                break;
            case 'add-to-queue':
                queueStore.addTracksToQueue(
                    filePaths,
                );
                break;
            case 'play-next':
                queueStore.playTracksNext(
                    filePaths,
                );
                break;
        }

        this.ctxMenu.close();
    }

    /**
     * Resolve artist file paths and show the
     * playlist submenu.
     */
    private async handleShowPlaylistSubmenu() {
        const paths =
            await this.getContextMenuArtistFilePaths();

        void this.ctxMenu.showPlaylistSubmenu(paths);
    }

    private async onContextMenuFavoriteToggle() {
        const filePaths =
            await this.getContextMenuArtistFilePaths();

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

        this.ctxMenu.close();
    }

    /* ================================================================
     * File path resolution
     * ================================================================ */

    /**
     * Fetches all file paths for an artist by
     * loading their albums, then every album's paths
     * in one call.  Respects the active library filter.
     *
     * perf.m2: this was a `for await` over the albums,
     * so a 16-album artist was 17 sequential round
     * trips returning whole track rows to read one
     * field off each.
     */
    private async getArtistFilePaths(
        artist: library.Artist,
    ): Promise<string[]> {
        try {
            const libId =
                this.libraryCtrl.selectedLibraryId;

            const albums = await list(
                GetAlbumsByArtist(artist.Name, libId ?? 0),
            );

            const byAlbum = await dict(
                GetFilePathsByAlbums(
                    albums.map((a) => a.ID),
                    libId ?? 0,
                ),
            );

            const allPaths: string[] = [];

            // The album order is the caller's, which is
            // why the backend groups rather than
            // flattens.  Keys arrive as strings, JSON
            // having no integer keys.
            for (const album of albums) {
                const paths =
                    byAlbum[album.ID] ?? [];

                allPaths.push(...paths);
            }

            return allPaths;
        } catch (error) {
            console.error(
                'Error loading artist tracks:',
                error,
            );

            return [];
        }
    }

    /* ================================================================
     * Helpers
     * ================================================================ */

    /**
     * The image this artist's card will draw, or `''` for the initial
     * placeholder.
     *
     * Split out of `renderArtistAvatar` so the prefetch (#65) asks for
     * exactly what the card is going to ask for — a second copy of the
     * tier ladder would be a second thing to keep in step, and warming
     * the wrong tier is a download that buys nothing.
     */
    private artistAvatarURL(artist: library.Artist): string {
        const needed = (this.imageSize ?? 176) * window.devicePixelRatio;
        let imageURL = '';

        if (needed <= 100) {
            imageURL = artist.ImageSmall || artist.ImageMedium || artist.ImageLarge || '';
        } else if (needed <= 200) {
            imageURL = artist.ImageMedium || artist.ImageLarge || '';
        } else {
            imageURL = artist.ImageLarge || '';
        }

        // Fallback: use album cover art if no artist image.
        //
        // `perf.M4`. This was a linear scan of every cached album,
        // lowercasing two strings per comparison, run from inside the
        // virtualizer's `renderItem` — so per card, per frame. It is
        // also the *common* case rather than an edge one: a
        // locally-tagged library has no artist images at all (the bulk
        // measurement seed has 440 artists, 4 988 albums and zero
        // images), so every visible card paid it on every pass.
        if (!imageURL) {
            imageURL = this.albumArtByArtist().get(
                artist.Name.toLowerCase(),
            ) ?? '';
        }

        return imageURL;
    }

    private renderArtistAvatar(artist: library.Artist) {
        const imageURL = this.artistAvatarURL(artist);

        if (imageURL) {
            return html`<img
                class="avatar-image"
                src="${imageURL}"
                alt="${artist.Name}"
                loading="lazy"
            />`;
        }

        return html`<span class="avatar-placeholder">
            ${this.getArtistInitial(artist.Name)}
        </span>`;
    }

    /**
     * Lowercased artist name → that artist's cover art, built once per
     * identity of the album cache rather than per card per frame.
     *
     * Keyed on the array identity because that is exactly what
     * `library-store` gives it: the store replaces the array when its
     * contents change and shares the unchanged members, which is the
     * same signal every memoized cache in `track-list` keys on.  The
     * size hint is included so a *tier* change (the grid resizing past
     * one of `getCoverUrl`'s breakpoints) also rebuilds.
     */
    private albumArtCache?: {
        albums: readonly library.Album[];
        needed: number;
        map: Map<string, string>;
    };

    private albumArtByArtist(): Map<string, string> {
        const albums = libraryStore.cachedAlbums;
        const needed = (this.imageSize ?? 176) * window.devicePixelRatio;

        if (!albums) return new Map();

        if (
            this.albumArtCache
            && this.albumArtCache.albums === albums
            && this.albumArtCache.needed === needed
        ) {
            return this.albumArtCache.map;
        }

        const map = new Map<string, string>();

        for (const a of albums) {
            const key = a.ArtistName.toLowerCase();

            // First album wins, which is what the original scan did by
            // breaking on its first match.
            if (map.has(key)) continue;

            const url = needed <= 100
                ? a.CoverArtSmall || a.CoverArtMedium || ''
                : a.CoverArtMedium || a.CoverArtLarge || '';

            if (url) map.set(key, url);
        }

        this.albumArtCache = { albums, needed, map };

        return map;
    }

    private getArtistInitial(
        name: string,
    ): string {
        if (!name) return '?';

        return name.charAt(0).toUpperCase();
    }

    /* ================================================================
     * Rendering
     * ================================================================ */

    private renderArtistCard(entry: ArtistEntry) {
        const { artist, index } = entry;
        const imgSize = this.imageSize;
        const placeholderFont = Math.round(
            imgSize * 0.38,
        );
        const isSelected =
            this.selectedArtists.has(artist.ID);

        return html`
            <div
                class="artist-card${isSelected
                    ? ' selected'
                    : ''}"
                data-index=${index}
                tabindex=${this.roving.tabIndexFor(index)}
                @focus=${() => this.roving.noteFocus(index)}
                role="option"
                aria-label="${artist.Name}"
                aria-selected="${isSelected}"
                style="
                    --avatar-size: ${imgSize}px;
                    --placeholder-font: ${placeholderFont}px;
                "
                @click=${(e: MouseEvent) =>
                    this.onArtistClick(
                        e,
                        artist,
                        index,
                    )}
                @contextmenu=${(e: MouseEvent) =>
                    this.onArtistContextMenu(
                        e,
                        artist,
                    )}
                @keydown=${(e: KeyboardEvent) => {
                    if (isContextMenuKey(e)) {
                        this.openArtistMenuFromKey(
                            e,
                            artist,
                        );

                        return;
                    }

                    if (
                        e.key === 'Enter' ||
                        e.key === ' '
                    ) {
                        e.preventDefault();
                        this.clearSelection();
                        this.dispatchEvent(
                            new CustomEvent(
                                'navigate',
                                {
                                    bubbles: true,
                                    composed: true,
                                    detail: {
                                        view: 'explore-artist-details',
                                        artistMBID:
                                            artist.MBID || '',
                                        artistName:
                                            artist.Name,
                                        localArtistId:
                                            artist.ID,
                                    },
                                },
                            ),
                        );
                    }
                }}
            >
                <div class="avatar-container">
                    ${this.renderArtistAvatar(artist)}
                </div>
                <div
                    class="artist-name"
                    title="${artist.Name}"
                >
                    ${artist.Name}
                </div>
            </div>
        `;
    }

    private renderContextMenu() {
        return html`
            <menu-surface
                id="context-menu"
                .active=${this.ctxMenu
                    .contextMenuOpen}
            >
                ${this.ctxMenu.contextMenuOpen
                    ? html`
                          <div
                              class="context-menu-panel"
                              role="menu"
                              aria-label="Artist actions"
                          >
                              <wa-dropdown-item
                                  @click=${() =>
                                      this.onContextMenuAction(
                                          'play',
                                      )}
                                  @mouseenter=${() =>
                                      this.ctxMenu.closePlaylistSubmenu()}
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
                                      this.ctxMenu.closePlaylistSubmenu()}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name=${ICON_QUEUE}
                                  ></wa-icon>
                                  Add to Queue
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  @click=${() =>
                                      this.onContextMenuAction(
                                          'play-next',
                                      )}
                                  @mouseenter=${() =>
                                      this.ctxMenu.closePlaylistSubmenu()}
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
                                      this.ctxMenu.clearSubmenuCloseTimer();
                                      void this.handleShowPlaylistSubmenu();
                                  }}
                                  @mouseleave=${this
                                      .ctxMenu
                                      .scheduleSubmenuClose}
                                  @click=${(
                                      e: Event,
                                  ) => {
                                      e.stopPropagation();
                                      void this.handleShowPlaylistSubmenu();
                                  }}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name=${ICON_PLAYLIST}
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
                                      this.ctxMenu.closePlaylistSubmenu()}
                              >
                                   <wa-icon
                                       slot="icon"
                                       name=${this.favCtrl.iconName}
                                   ></wa-icon>
                                  ${this.favCtrl.allFavorited(this.ctxMenu.playlistFilePaths) ? `Remove from ${this.favCtrl.playlistName}` : `Add to ${this.favCtrl.playlistName}`}
                              </wa-dropdown-item>
                          </div>
                      `
                    : nothing}
            </menu-surface>

            <menu-surface
                id="playlist-submenu"
                label="Add to playlist"
                placement="right-start"
                .active=${this.ctxMenu
                    .playlistSubmenuOpen}
            >
                ${this.ctxMenu.playlistSubmenuOpen
                    ? html`
                          <div
                              @mouseenter=${() =>
                                  this.ctxMenu.clearSubmenuCloseTimer()}
                              @mouseleave=${this
                                  .ctxMenu
                                  .scheduleSubmenuClose}
                          >
                              <playlist-picker
                                  .filePaths=${this
                                      .ctxMenu
                                      .playlistFilePaths}
                                  @playlist-action-complete=${this
                                      .ctxMenu
                                      .onPlaylistActionComplete}
                                  @click=${(
                                      e: Event,
                                  ) =>
                                      e.stopPropagation()}
                              ></playlist-picker>
                          </div>
                      `
                    : nothing}
            </menu-surface>
        `;
    }

    override render() {
        if (this.loading) {
            // The header keeps its place while the view loads: a
            // heading that appears only once the data does is the
            // shifting layout this component exists to stop.
            return html`
                <page-header
                    heading="Artists"
                    .sortOptions=${ARTIST_SORT_OPTIONS}
                    sort-field="name"
                    sort-direction=${this.sortDirection}
                ></page-header>
                <div class="loading-message">
                    Loading artists...
                </div>
            `;
        }

        const entries = this.cachedGridEntries;
        const searchBar = html`
            <page-header
                heading="Artists"
                .count=${entries.length}
                count-noun="artist"
                .sortOptions=${ARTIST_SORT_OPTIONS}
                sort-field="name"
                sort-direction=${this.sortDirection}
                search-term=${this.searchCtrl.term}
                @sort-change=${this.onPageHeaderSort}
            ></page-header>
        `;

        if (entries.length === 0) {
            return html`
                ${searchBar}
                <div class="empty-message">
                    ${this.searchCtrl.term
                        ? 'No artists match your search.'
                        : 'No artists in library.'}
                </div>
            `;
        }

        return html`
            ${searchBar}
            <div
                class="grid-scroll-container"
                style=${this.restoringScroll
                    ? 'visibility: hidden'
                    : ''}
                @click=${this.onGridClick}
                @keydown=${this.roving.handleKeydown}
            >
                <lit-virtualizer
                    role="listbox"
                    aria-label="Artists"
                    aria-multiselectable="true"
                    .items=${entries}
                    .renderItem=${(entry: ArtistEntry) => this.renderArtistCard(entry)}
                    .keyFunction=${(entry: ArtistEntry) => entry.artist.ID}
                    .layout=${this.gridLayout}
                    @visibilityChanged=${this.onVisibilityChanged}
                    @rangeChanged=${this.onRangeChanged}
                ></lit-virtualizer>
            </div>
            ${this.renderContextMenu()}
        `;
    }

    /**
     * Click on empty area of the grid clears the
     * selection.
     */
    private onGridClick = (e: MouseEvent) => {
        const path = e.composedPath();

        const clickedCard = path.some(
            (el) =>
                el instanceof HTMLElement &&
                el.classList.contains('artist-card'),
        );

        if (!clickedCard) {
            this.clearSelection();
        }
    };
}

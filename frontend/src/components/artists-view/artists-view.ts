import { LitElement, html, css, nothing } from 'lit';
import {
    customElement,
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
    GetAlbumsByArtist,
    GetAlbumTracks,
} from '@go/library/Library';
import { library } from '@go/models';
import { LibraryController } from '@store/controllers/library-controller';
import { SearchController } from '@store/controllers/search-controller';
import { queueStore } from '@store/queue-store';
import {
    ContextMenuController,
    contextMenuStyles,
} from '@utils/context-menu-controller.js';
import type { ContextMenuHost } from '@utils/context-menu-controller.js';
import { FavoritesController } from '@store/controllers/favorites-controller';

import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import type WaPopup from '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@components/playlist-picker/playlist-picker.js';

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
interface ArtistEntry {
    artist: library.Artist;
    index: number;
}

@customElement('artists-view')
export class ArtistsView
    extends LitElement
    implements ContextMenuHost
{
    private libraryCtrl = new LibraryController(this);
    private searchCtrl = new SearchController(this);
    private ctxMenu = new ContextMenuController(this);
    private favCtrl = new FavoritesController(this);
    private wheelListenerAttached = false;
    private lastSearchTerm = '';

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
    private contextMenuPopup!: WaPopup;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: WaPopup;

    getContextMenuPopup(): WaPopup | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup():
        | WaPopup
        | undefined {
        return this.playlistSubmenuPopup;
    }

    onContextMenuClose(): void {
        this.contextMenuArtistId = null;
    }

    // ----- Grid spacing constants -----

    private static readonly GRID_GAP = 8;
    private static readonly GRID_PADDING = 8;
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
        const gap = ArtistsView.GRID_GAP;
        const pad = ArtistsView.GRID_PADDING;

        return grid({
            itemSize: {
                width: `${w}px`,
                height: `${h}px`,
            },
            gap: `${gap}px`,
            padding: `${pad}px`,
            justify: 'center',
        });
    }

    // -- Memoisation caches for filtered artists --
    private cachedFilteredArtists: library.Artist[] =
        [];
    private cachedGridEntries: ArtistEntry[] = [];
    private prevFilterArtists: library.Artist[] = [];
    private prevFilterTerm = '';

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
            term !== this.prevFilterTerm
        ) {
            this.prevFilterArtists = this.artists;
            this.prevFilterTerm = term;
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

        if (!term) {
            return this.artists;
        }

        return this.artists.filter((a) =>
            a.Name.toLowerCase().includes(term),
        );
    }

    static override styles = [
        contextMenuStyles,
        css`
            :host {
                display: flex;
                flex-direction: column;
                overflow: hidden;
                position: relative;
            }

            .grid-scroll-container {
                flex: 1;
                overflow-y: auto;
                overflow-x: hidden;
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
                transition:
                    background-color 0.15s ease,
                    transform 0.15s ease;
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

            .search-indicator {
                position: absolute;
                top: 8px;
                left: 50%;
                transform: translateX(-50%);
                z-index: 5;
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
                padding: 4px 14px;
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
        this.loadArtists();
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.detachWheelListener();

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
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

    private updateGridLayout() {
        if (
            this.cardSize === this.lastLayoutWidth
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
                        view: 'artist-details',
                        artistId: artist.ID,
                        artistName: artist.Name,
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

    private async onContextMenuAction(
        action: string,
    ) {
        const filePaths =
            await this.getContextMenuArtistFilePaths();

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                queueStore.setQueue(
                    filePaths,
                    0,
                    true,
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
     * loading their albums, then each album's
     * tracks.
     */
    private async getArtistFilePaths(
        artist: library.Artist,
    ): Promise<string[]> {
        try {
            const albums =
                await GetAlbumsByArtist(artist.ID);

            const allPaths: string[] = [];

            for (const album of albums) {
                const tracks =
                    await GetAlbumTracks(album.ID);

                for (const t of tracks) {
                    allPaths.push(t.FilePath);
                }
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
                tabindex="0"
                role="button"
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
                                        view: 'artist-details',
                                        artistId:
                                            artist.ID,
                                        artistName:
                                            artist.Name,
                                    },
                                },
                            ),
                        );
                    }
                }}
            >
                <div class="avatar-container">
                    <span class="avatar-placeholder">
                        ${this.getArtistInitial(
                            artist.Name,
                        )}
                    </span>
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
            <wa-popup
                id="context-menu"
                placement="bottom-start"
                flip
                shift
                .active=${this.ctxMenu
                    .contextMenuOpen}
            >
                ${this.ctxMenu.contextMenuOpen
                    ? html`
                          <div
                              class="context-menu-panel"
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
            </wa-popup>

            <wa-popup
                id="playlist-submenu"
                placement="right-start"
                flip
                shift
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
            </wa-popup>
        `;
    }

    override render() {
        if (this.loading) {
            return html`
                <div class="loading-message">
                    Loading artists...
                </div>
            `;
        }

        const entries = this.cachedGridEntries;

        if (entries.length === 0) {
            return html`
                <div class="empty-message">
                    ${this.searchCtrl.term
                        ? 'No artists match your search.'
                        : 'No artists in library.'}
                </div>
            `;
        }

        return html`
            ${this.searchCtrl.term
                ? html`<div
                      class="search-indicator"
                  >
                      Showing results for
                      &ldquo;${this.searchCtrl
                          .term}&rdquo;
                  </div>`
                : nothing}
            <div
                class="grid-scroll-container"
                style=${this.restoringScroll
                    ? 'visibility: hidden'
                    : ''}
                @click=${this.onGridClick}
            >
                <lit-virtualizer
                    .items=${entries}
                    .renderItem=${(
                        entry: ArtistEntry,
                    ) =>
                        this.renderArtistCard(
                            entry,
                        )}
                    .layout=${this.gridLayout}
                    @visibilityChanged=${this.onVisibilityChanged}
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

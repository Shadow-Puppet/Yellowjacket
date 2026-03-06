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
import { GetTracksByGenre } from '@go/library/Library';
import type { library } from '@go/models';
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

/** localStorage key for persisted genre card size. */
const CARD_SIZE_KEY = 'genres-view-card-size';

/** Card size limits. */
const CARD_SIZE_MIN = 100;
const CARD_SIZE_MAX = 350;
const CARD_SIZE_DEFAULT = 176;

/** Debounce delay for saving scroll position. */
const SCROLL_DEBOUNCE_MS = 100;

/** A genre extracted from the track library. */
interface Genre {
    name: string;
    trackCount: number;
}

/** Grid entry for the virtualized genre grid. */
interface GenreEntry {
    genre: Genre;
    index: number;
}

@customElement('genres-view')
export class GenresView
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
    private lastGenresRef:
        | library.GenreWithCount[]
        | null = null;

    private scrollDebounceTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    @state()
    private genres: Genre[] = [];

    @state()
    private loading = true;

    @state()
    private restoringScroll = false;

    @state()
    private cardSize: number = CARD_SIZE_DEFAULT;

    // ----- Multi-select state -----

    @state()
    private selectedGenres: Set<string> = new Set();

    private lastSelectedGenreIndex: number | null =
        null;

    // ----- Context menu state -----

    /**
     * Genre name that was right-clicked to open the
     * context menu.  Used as fallback when the
     * right-clicked genre is not in the current
     * visual selection.
     */
    private contextMenuGenreName: string | null = null;

    @query('#context-menu')
    private contextMenuPopup!: WaPopup;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: WaPopup;

    // ----- ContextMenuHost interface -----

    getContextMenuPopup(): WaPopup | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup():
        | WaPopup
        | undefined {
        return this.playlistSubmenuPopup;
    }

    onContextMenuClose(): void {
        this.contextMenuGenreName = null;
    }

    // ----- Grid spacing constants -----

    private static readonly GRID_GAP = 8;
    private static readonly GRID_PADDING = 8;
    private static readonly CARD_PADDING = 5;

    private get imageSize(): number {
        return (
            this.cardSize -
            GenresView.CARD_PADDING * 2
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
        const gap = GenresView.GRID_GAP;
        const pad = GenresView.GRID_PADDING;

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

    // -- Memoisation caches for filtered genres --
    private cachedFilteredGenres: Genre[] = [];
    private cachedGridEntries: GenreEntry[] = [];
    private prevFilterGenres: Genre[] = [];
    private prevFilterTerm = '';

    /**
     * Recompute the filtered-genres and grid-entries
     * caches when their inputs have changed.  Called
     * from willUpdate() so the caches are ready
     * before render().
     */
    private recomputeGenreCaches() {
        const term = this.searchCtrl.term;

        if (
            this.genres !== this.prevFilterGenres ||
            term !== this.prevFilterTerm
        ) {
            this.prevFilterGenres = this.genres;
            this.prevFilterTerm = term;
            this.cachedFilteredGenres =
                this.computeFilteredGenres();
            this.cachedGridEntries =
                this.cachedFilteredGenres.map(
                    (genre, index) => ({
                        genre,
                        index,
                    }),
                );
        }
    }

    private computeFilteredGenres(): Genre[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        if (!term) {
            return this.genres;
        }

        return this.genres.filter((g) =>
            g.name.toLowerCase().includes(term),
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

        .genre-card {
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

        .genre-card:hover {
            background-color: var(
                --yj-bg-overlay,
                rgba(255, 255, 255, 0.06)
            );
        }

        .genre-card:active {
            transform: scale(0.97);
        }

        .genre-card.selected {
            outline: 2px solid
                var(--yj-accent, #ffd43b);
            outline-offset: 2px;
        }

        .genre-card.selected .avatar-container {
            scale: 0.95;
        }

        .genre-card.selected .genre-name {
            scale: 0.95;
        }

        .avatar-container {
            width: var(--avatar-size);
            height: var(--avatar-size);
            border-radius: 8px;
            overflow: hidden;
            background: linear-gradient(
                135deg,
                var(--yj-bg-overlay, #404040) 0%,
                var(--yj-bg-surface, #282828) 100%
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

        .genre-name {
            width: 100%;
            text-align: center;
            font-size: var(
                --genre-name-font,
                14px
            );
            font-weight: 500;
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            padding: var(--genre-name-pad, 6px) 2px
                0;
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
        this.recomputeGenreCaches();
    }

    override connectedCallback() {
        super.connectedCallback();
        this.loadCardSize();
        this.loadGenres();
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
            this.libraryCtrl.cachedGenres;

        if (
            cached !== null &&
            cached !== this.lastGenresRef
        ) {
            this.lastGenresRef = cached;
            this.loadGenres();
        }
    }

    /* ================================================================
     * Data loading
     * ================================================================ */

    private async loadGenres() {
        try {
            this.loading = true;

            const rows =
                await this.libraryCtrl.getGenres();

            this.genres = (rows ?? []).map((r) => ({
                name: r.Name,
                trackCount: r.TrackCount,
            }));
        } catch (error) {
            console.error(
                'Error loading genres:',
                error,
            );
            this.genres = [];
        } finally {
            const saved =
                this.libraryCtrl.getScrollPosition(
                    'genres',
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
                    'genres',
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
                'genres',
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
            this.cachedFilteredGenres.length - 1,
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
                '--genre-name-font',
                '12px',
            );
            this.style.setProperty(
                '--genre-name-pad',
                '4px',
            );
        } else if (w > 250) {
            this.style.setProperty(
                '--genre-name-font',
                '15px',
            );
            this.style.setProperty(
                '--genre-name-pad',
                '8px',
            );
        } else {
            this.style.setProperty(
                '--genre-name-font',
                '14px',
            );
            this.style.setProperty(
                '--genre-name-pad',
                '6px',
            );
        }
    }

    /* ================================================================
     * Genre selection helpers
     * ================================================================ */

    /**
     * Select a contiguous range of genre names
     * between two indices in filteredGenres.
     */
    private selectGenreRange(
        from: number,
        to: number,
    ): Set<string> {
        const filtered = this.cachedFilteredGenres;
        const start = Math.min(from, to);
        const end = Math.max(from, to);
        const names = new Set<string>();

        for (let i = start; i <= end; i++) {
            const genre = filtered[i];

            if (genre) {
                names.add(genre.name);
            }
        }

        return names;
    }

    /**
     * Fetch file paths for a set of genre names by
     * querying the backend for each genre.
     */
    private async getFilePathsForGenres(
        genreNames: Iterable<string>,
    ): Promise<string[]> {
        const seen = new Set<string>();
        const allPaths: string[] = [];

        const promises = Array.from(
            genreNames,
            (name) => GetTracksByGenre(name),
        );

        const results = await Promise.all(promises);

        for (const tracks of results) {
            for (const track of tracks ?? []) {
                if (!seen.has(track.FilePath)) {
                    seen.add(track.FilePath);
                    allPaths.push(track.FilePath);
                }
            }
        }

        return allPaths;
    }

    /**
     * Return file paths for the context menu target.
     * If the right-clicked genre is part of the
     * current selection, return paths for all selected
     * genres.  Otherwise return paths for the
     * right-clicked genre only.
     */
    private async getContextMenuGenreFilePaths(): Promise<
        string[]
    > {
        if (
            this.contextMenuGenreName !== null &&
            !this.selectedGenres.has(
                this.contextMenuGenreName,
            )
        ) {
            return this.getFilePathsForGenres([
                this.contextMenuGenreName,
            ]);
        }

        return this.getFilePathsForGenres(
            this.selectedGenres,
        );
    }

    /** Clear the current genre selection. */
    private clearSelection() {
        this.selectedGenres = new Set();
        this.lastSelectedGenreIndex = null;
    }

    /* ================================================================
     * Genre card click
     * ================================================================ */

    private onGenreClick(
        e: MouseEvent,
        genre: Genre,
        index: number,
    ) {
        const isCtrl = e.ctrlKey || e.metaKey;
        const isShift = e.shiftKey;

        if (
            isShift &&
            this.lastSelectedGenreIndex !== null
        ) {
            const range = this.selectGenreRange(
                this.lastSelectedGenreIndex,
                index,
            );
            const next = new Set(
                this.selectedGenres,
            );

            for (const name of range) {
                next.add(name);
            }

            this.selectedGenres = next;
        } else if (isCtrl) {
            const next = new Set(
                this.selectedGenres,
            );

            if (next.has(genre.name)) {
                next.delete(genre.name);
            } else {
                next.add(genre.name);
            }

            this.selectedGenres = next;
            this.lastSelectedGenreIndex = index;
        } else {
            // Plain click: navigate to details.
            this.clearSelection();
            this.dispatchEvent(
                new CustomEvent('navigate', {
                    bubbles: true,
                    composed: true,
                    detail: {
                        view: 'genre-details',
                        genreName: genre.name,
                    },
                }),
            );
        }
    }

    /* ================================================================
     * Context menu
     * ================================================================ */

    private onGenreContextMenu = (
        e: MouseEvent,
        genre: Genre,
    ) => {
        e.preventDefault();
        e.stopPropagation();

        this.contextMenuGenreName = genre.name;

        this.ctxMenu.openAt(
            e.clientX,
            e.clientY,
        );
    };

    private async onContextMenuAction(
        action: string,
    ) {
        const filePaths =
            await this.getContextMenuGenreFilePaths();

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                queueStore.setQueue(filePaths, 0, true);
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

    private async onContextMenuFavoriteToggle() {
        const filePaths =
            await this.getContextMenuGenreFilePaths();

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
     * Helpers
     * ================================================================ */

    private getGenreInitial(name: string): string {
        if (!name) return '?';

        return name.charAt(0).toUpperCase();
    }

    /* ================================================================
     * Rendering
     * ================================================================ */

    private renderGenreCard(entry: GenreEntry) {
        const { genre, index } = entry;
        const imgSize = this.imageSize;
        const placeholderFont = Math.round(
            imgSize * 0.38,
        );
        const isSelected =
            this.selectedGenres.has(genre.name);

        return html`
            <div
                class="genre-card${isSelected
                    ? ' selected'
                    : ''}"
                tabindex="0"
                role="button"
                aria-label="${genre.name}"
                aria-selected="${isSelected}"
                style="
                    --avatar-size: ${imgSize}px;
                    --placeholder-font: ${placeholderFont}px;
                "
                @click=${(e: MouseEvent) =>
                    this.onGenreClick(
                        e,
                        genre,
                        index,
                    )}
                @contextmenu=${(e: MouseEvent) =>
                    this.onGenreContextMenu(
                        e,
                        genre,
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
                                        view: 'genre-details',
                                        genreName:
                                            genre.name,
                                    },
                                },
                            ),
                        );
                    }
                }}
            >
                <div class="avatar-container">
                    <span class="avatar-placeholder">
                        ${this.getGenreInitial(
                            genre.name,
                        )}
                    </span>
                </div>
                <div
                    class="genre-name"
                    title="${genre.name}"
                >
                    ${genre.name}
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
                                      void this.getContextMenuGenreFilePaths().then(
                                          (paths) =>
                                              this.ctxMenu.showPlaylistSubmenu(
                                                  paths,
                                              ),
                                      );
                                  }}
                                  @mouseleave=${this
                                      .ctxMenu
                                      .scheduleSubmenuClose}
                                  @click=${(
                                      e: Event,
                                  ) => {
                                      e.stopPropagation();
                                      void this.getContextMenuGenreFilePaths().then(
                                          (paths) =>
                                              this.ctxMenu.showPlaylistSubmenu(
                                                  paths,
                                              ),
                                      );
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
                    Loading genres...
                </div>
            `;
        }

        const entries = this.cachedGridEntries;
        const searchBar = this.searchCtrl.term
            ? html`<div class="search-bar-row">
                  <div class="search-indicator">
                      Showing results for
                      &ldquo;${this.searchCtrl
                          .term}&rdquo;
                  </div>
              </div>`
            : nothing;

        if (entries.length === 0) {
            return html`
                ${searchBar}
                <div class="empty-message">
                    ${this.searchCtrl.term
                        ? 'No genres match your search.'
                        : 'No genres in library.'}
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
            >
                <lit-virtualizer
                    .items=${entries}
                    .renderItem=${(entry: GenreEntry) => this.renderGenreCard(entry)}
                    .keyFunction=${(entry: GenreEntry) => entry.genre.name}
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
                el.classList.contains('genre-card'),
        );

        if (!clickedCard) {
            this.clearSelection();
        }
    };
}

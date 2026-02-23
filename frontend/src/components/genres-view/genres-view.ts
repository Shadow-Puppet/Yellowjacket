import { LitElement, html, css, nothing } from 'lit';
import {
    customElement,
    state,
    query,
} from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';
import '@lit-labs/virtualizer';
import { grid } from '@lit-labs/virtualizer/layouts/grid.js';
import { library } from '@go/models';
import { LibraryController } from '@store/controllers/library-controller';
import { SearchController } from '@store/controllers/search-controller';
import { queueStore } from '@store/queue-store';
import { Events } from '../../events';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';

/** Pixels to change card width per scroll tick. */
const ZOOM_STEP = 16;

/** localStorage key for persisted genre card size. */
const CARD_SIZE_KEY = 'genres-view-card-size';

/** Card size limits. */
const CARD_SIZE_MIN = 100;
const CARD_SIZE_MAX = 350;
const CARD_SIZE_DEFAULT = 176;

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
export class GenresView extends LitElement {
    private libraryCtrl = new LibraryController(this);
    private searchCtrl = new SearchController(this);
    private cancelScanComplete?: () => void;
    private wheelListenerAttached = false;

    /** All tracks from the library (used to derive genres). */
    private allTracks: library.Track[] = [];

    @state()
    private genres: Genre[] = [];

    @state()
    private loading = true;

    @state()
    private cardSize: number = CARD_SIZE_DEFAULT;

    // ----- Context menu state -----

    @state()
    private contextMenuOpen = false;

    @state()
    private playlistSubmenuOpen = false;

    @state()
    private playlistFilePaths: string[] = [];

    /** The genre targeted by the current context menu. */
    private contextMenuGenre: Genre | null = null;

    @query('#context-menu')
    private contextMenuPopup!: HTMLElement;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: HTMLElement;

    private submenuCloseTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    // ----- Close handlers -----

    private closeHandler = () =>
        this.closeContextMenu();

    private mousedownCloseHandler = (
        e: MouseEvent,
    ) => {
        const path = e.composedPath();
        const popup = this.contextMenuPopup;
        const submenu = this.playlistSubmenuPopup;

        if (popup && path.includes(popup)) return;
        if (submenu && path.includes(submenu)) return;

        this.closeContextMenu();
    };

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

    /** Filtered genres based on search term. */
    private get filteredGenres(): Genre[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        if (!term) {
            return this.genres;
        }

        return this.genres.filter((g) =>
            g.name.toLowerCase().includes(term),
        );
    }

    /** Build grid entries from filtered genres. */
    private get gridEntries(): GenreEntry[] {
        return this.filteredGenres.map(
            (genre, index) => ({
                genre,
                index,
            }),
        );
    }

    static override styles = css`
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
                transform 0.1s ease;
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

        /* ====================================
         * Context menu
         * ==================================== */

        #context-menu {
            z-index: 200;
        }

        .context-menu-panel {
            background-color: var(
                --yj-bg-elevated,
                #343a40
            );
            border: 1px solid
                var(--yj-border, #444);
            border-radius: 6px;
            padding: 4px 0;
            box-shadow: 0 8px 24px
                rgba(0, 0, 0, 0.5);
            min-width: 160px;
        }

        .context-menu-panel wa-dropdown-item {
            cursor: pointer;
            --wa-color-text-normal: var(
                --yj-text-primary,
                #fff
            );
            font-size: 13px;
        }

        .context-menu-panel
            wa-dropdown-item:hover {
            background-color: var(
                --yj-hover-overlay,
                rgba(255, 255, 255, 0.1)
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

    /* ================================================================
     * Lifecycle
     * ================================================================ */

    override connectedCallback() {
        super.connectedCallback();
        this.loadCardSize();
        this.loadGenres();
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            () => this.loadGenres(),
        );
        document.addEventListener(
            'click',
            this.closeHandler,
        );
        document.addEventListener(
            'contextmenu',
            this.closeHandler,
        );
        document.addEventListener(
            'mousedown',
            this.mousedownCloseHandler,
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.cancelScanComplete?.();
        this.detachWheelListener();
        document.removeEventListener(
            'click',
            this.closeHandler,
        );
        document.removeEventListener(
            'contextmenu',
            this.closeHandler,
        );
        document.removeEventListener(
            'mousedown',
            this.mousedownCloseHandler,
        );
    }

    override updated() {
        this.updateSizeProperties();
        this.ensureWheelListener();
        this.updateGridLayout();
    }

    /* ================================================================
     * Data loading
     * ================================================================ */

    private async loadGenres() {
        try {
            this.loading = true;

            const tracks =
                await this.libraryCtrl.getTracks();

            this.allTracks = tracks ?? [];
            this.genres =
                this.extractGenres(this.allTracks);
        } catch (error) {
            console.error(
                'Error loading genres:',
                error,
            );
            this.allTracks = [];
            this.genres = [];
        } finally {
            this.loading = false;
        }
    }

    /**
     * Extract unique genres from all tracks,
     * sorted alphabetically by name.
     */
    private extractGenres(
        tracks: library.Track[],
    ): Genre[] {
        const counts = new Map<string, number>();

        for (const track of tracks) {
            const genres = track.Genre ?? [];

            for (const name of genres) {
                if (!name) continue;

                counts.set(
                    name,
                    (counts.get(name) ?? 0) + 1,
                );
            }
        }

        const result: Genre[] = [];

        for (const [name, trackCount] of counts) {
            result.push({ name, trackCount });
        }

        result.sort((a, b) =>
            a.name.localeCompare(b.name),
        );

        return result;
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
     * Genre card click
     * ================================================================ */

    private onGenreClick(genre: Genre) {
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

    /* ================================================================
     * Context menu
     * ================================================================ */

    private onGenreContextMenu = (
        e: MouseEvent,
        genre: Genre,
    ) => {
        e.preventDefault();
        e.stopPropagation();

        this.contextMenuGenre = genre;
        this.openContextMenuAt(
            e.clientX,
            e.clientY,
        );
    };

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

    private closeContextMenu() {
        if (!this.contextMenuOpen) return;

        this.closePlaylistSubmenu();
        this.contextMenuOpen = false;
        this.playlistFilePaths = [];
        this.contextMenuGenre = null;

        const popup = this.contextMenuPopup;

        if (popup) {
            (popup as any).active = false;
        }
    }

    private onContextMenuAction(action: string) {
        const genre = this.contextMenuGenre;

        if (!genre) return;

        const filePaths =
            this.getGenreFilePaths(genre.name);

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                queueStore.setQueue(filePaths, 0);
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

        this.closeContextMenu();
    }

    /* ================================================================
     * Playlist submenu
     * ================================================================ */

    private clearSubmenuCloseTimer() {
        if (this.submenuCloseTimer !== null) {
            clearTimeout(this.submenuCloseTimer);
            this.submenuCloseTimer = null;
        }
    }

    private scheduleSubmenuClose = () => {
        this.clearSubmenuCloseTimer();
        this.submenuCloseTimer = setTimeout(() => {
            this.submenuCloseTimer = null;
            this.closePlaylistSubmenu();
        }, 150);
    };

    private showPlaylistSubmenu() {
        this.clearSubmenuCloseTimer();

        if (this.playlistSubmenuOpen) return;

        const genre = this.contextMenuGenre;

        if (!genre) return;

        this.playlistFilePaths =
            this.getGenreFilePaths(genre.name);

        this.playlistSubmenuOpen = true;

        void this.updateComplete.then(() => {
            const submenu =
                this.playlistSubmenuPopup;

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
        });
    }

    private closePlaylistSubmenu() {
        this.clearSubmenuCloseTimer();

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

    /* ================================================================
     * File path resolution
     * ================================================================ */

    /**
     * Returns all file paths for tracks that
     * contain the given genre name.
     */
    private getGenreFilePaths(
        genreName: string,
    ): string[] {
        return this.allTracks
            .filter((t) =>
                (t.Genre ?? []).includes(genreName),
            )
            .map((t) => t.FilePath);
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
        const { genre } = entry;
        const imgSize = this.imageSize;
        const placeholderFont = Math.round(
            imgSize * 0.38,
        );

        return html`
            <div
                class="genre-card"
                tabindex="0"
                role="button"
                aria-label="${genre.name}"
                style="
                    --avatar-size: ${imgSize}px;
                    --placeholder-font: ${placeholderFont}px;
                "
                @click=${() =>
                    this.onGenreClick(genre)}
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
                        this.onGenreClick(genre);
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
                .active=${this.contextMenuOpen}
            >
                ${this.contextMenuOpen
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
                                      this.closePlaylistSubmenu()}
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
                                      this.closePlaylistSubmenu()}
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
                                      this.closePlaylistSubmenu()}
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
                                      this.clearSubmenuCloseTimer();
                                      this.showPlaylistSubmenu();
                                  }}
                                  @mouseleave=${this
                                      .scheduleSubmenuClose}
                                  @click=${(
                                      e: Event,
                                  ) => {
                                      e.stopPropagation();
                                      this.showPlaylistSubmenu();
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
                .active=${this
                    .playlistSubmenuOpen}
            >
                ${this.playlistSubmenuOpen
                    ? html`
                          <div
                              @mouseenter=${() =>
                                  this.clearSubmenuCloseTimer()}
                              @mouseleave=${this
                                  .scheduleSubmenuClose}
                          >
                              <playlist-picker
                                  .filePaths=${this
                                      .playlistFilePaths}
                                  @playlist-action-complete=${this
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

        const entries = this.gridEntries;

        if (entries.length === 0) {
            return html`
                <div class="empty-message">
                    ${this.searchCtrl.term
                        ? 'No genres match your search.'
                        : 'No genres in library.'}
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
            <div class="grid-scroll-container">
                <lit-virtualizer
                    .items=${entries}
                    .renderItem=${(
                        entry: GenreEntry,
                    ) =>
                        this.renderGenreCard(entry)}
                    .layout=${this.gridLayout}
                ></lit-virtualizer>
            </div>
            ${this.renderContextMenu()}
        `;
    }
}

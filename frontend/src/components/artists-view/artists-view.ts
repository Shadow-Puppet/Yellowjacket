import { LitElement, html, css, nothing } from 'lit';
import {
    customElement,
    state,
    query,
} from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';
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
import { Events } from '../../events';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';

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
export class ArtistsView extends LitElement {
    private libraryCtrl = new LibraryController(this);
    private searchCtrl = new SearchController(this);
    private cancelScanComplete?: () => void;
    private wheelListenerAttached = false;
    private lastSearchTerm = '';
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

    @state()
    private contextMenuOpen = false;

    /**
     * Artist ID that was right-clicked to open the
     * context menu.  Used as fallback when the
     * right-clicked artist is not in the current
     * visual selection.
     */
    private contextMenuArtistId: number | null = null;

    @state()
    private playlistSubmenuOpen = false;

    @state()
    private playlistFilePaths: string[] = [];

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

    /** Filtered artists based on search term. */
    private get filteredArtists(): library.Artist[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        if (!term) {
            return this.artists;
        }

        return this.artists.filter((a) =>
            a.Name.toLowerCase().includes(term),
        );
    }

    /** Build grid entries from filtered artists. */
    private get gridEntries(): ArtistEntry[] {
        return this.filteredArtists.map(
            (artist, index) => ({
                artist,
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

        .artist-card {
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

        .artist-card.selected .avatar-container {
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
            font-size: var(--placeholder-font, 48px);
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
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            padding: var(--artist-name-pad, 6px) 2px
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
        this.loadArtists();
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            () => this.loadArtists(),
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

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
        }

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

        // Clear selection when search term changes.
        const currentTerm = this.searchCtrl.term;

        if (currentTerm !== this.lastSearchTerm) {
            this.lastSearchTerm = currentTerm;
            this.clearSelection();
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
            this.filteredArtists.length - 1,
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
        const filtered = this.filteredArtists;
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
        this.contextMenuArtistId = null;

        const popup = this.contextMenuPopup;

        if (popup) {
            (popup as any).active = false;
        }
    }

    private async onContextMenuAction(
        action: string,
    ) {
        const filePaths =
            await this.getContextMenuArtistFilePaths();

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

    private async showPlaylistSubmenu() {
        this.clearSubmenuCloseTimer();

        if (this.playlistSubmenuOpen) return;

        this.playlistFilePaths =
            await this.getContextMenuArtistFilePaths();

        if (this.playlistFilePaths.length === 0) {
            return;
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
                                      void this.showPlaylistSubmenu();
                                  }}
                                  @mouseleave=${this
                                      .scheduleSubmenuClose}
                                  @click=${(
                                      e: Event,
                                  ) => {
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
                    Loading artists...
                </div>
            `;
        }

        const entries = this.gridEntries;

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

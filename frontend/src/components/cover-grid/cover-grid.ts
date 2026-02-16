import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import { GetAlbumTracks } from '@go/library/Library';
import { library } from '@go/models';
import { QueueController } from '@store/controllers/queue-controller';
import { LibraryController } from '@store/controllers/library-controller';
import '@lit-labs/virtualizer';
import type {
    LitVirtualizer,
    VisibilityChangedEvent,
} from '@lit-labs/virtualizer';
import { grid } from '@lit-labs/virtualizer/layouts/grid.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';

@customElement('cover-grid')
export class CoverGrid extends LitElement {
    private queue = new QueueController(this);
    private libraryCtrl = new LibraryController(this);

    // Grid layout constants — must match the grid() config in render().
    private static readonly GRID_ITEM_WIDTH = 176;
    private static readonly GRID_ITEM_HEIGHT = 230;
    private static readonly GRID_GAP = 16;
    private static readonly GRID_PADDING = 16;

    private lastSelectedIndex: number | null = null;
    private hasRestoredScroll = false;

    private closeHandler = () => this.closeContextMenu();

    static override styles = css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }

        lit-virtualizer {
            flex: 1;
            overflow-y: auto;
        }

        lit-virtualizer.restoring {
            visibility: hidden;
        }

        .album-card {
            display: flex;
            flex-direction: column;
            cursor: pointer;
            border-radius: 8px;
            padding: 8px;
            transition: background-color 0.2s ease;
            box-sizing: border-box;
        }

        .album-card:hover {
            background-color: rgba(255, 255, 255, 0.1);
        }

        .album-card.selected {
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
            background: linear-gradient(135deg, #404040 0%, #282828 100%);
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
            background-color: rgba(255, 255, 255, 0.1);
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

    @state()
    private albums: library.Album[] = [];

    @state()
    private loading = true;

    @state()
    private contextMenuOpen = false;

    @state()
    private selectedAlbums: Set<number> = new Set();

    @state()
    private playlistSubmenuOpen = false;

    @state()
    private playlistFilePaths: string[] = [];

    @state()
    private hiddenForRestore = false;

    @query('#context-menu')
    private contextMenuPopup!: HTMLElement;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: HTMLElement;

    @query('lit-virtualizer')
    private virtualizer!: LitVirtualizer;

    override connectedCallback() {
        super.connectedCallback();
        this.loadAlbums();
        document.addEventListener('click', this.closeHandler);
        document.addEventListener('contextmenu', this.closeHandler);
    }

    override disconnectedCallback() {
        this.virtualizer?.removeEventListener(
            'visibilityChanged',
            this.onVisibilityChanged,
        );
        this.hasRestoredScroll = false;
        this.hiddenForRestore = false;
        super.disconnectedCallback();
        document.removeEventListener('click', this.closeHandler);
        document.removeEventListener('contextmenu', this.closeHandler);
    }

    private async loadAlbums() {
        try {
            this.loading = true;
            const albums = await this.libraryCtrl.getAlbums();
            this.albums = albums ?? [];
            this.selectedAlbums = new Set();
            this.lastSelectedIndex = null;
        } catch (error) {
            console.error("Error loading albums:", error);
            this.albums = [];
        } finally {
            this.loading = false;
        }

        const savedIndex =
            this.libraryCtrl.getScrollPosition('albums');

        if (savedIndex > 0) {
            this.hiddenForRestore = true;
        }

        await this.updateComplete;

        if (this.isConnected && this.virtualizer) {
            this.virtualizer.addEventListener(
                'visibilityChanged',
                this.onVisibilityChanged,
            );
        }
    }

    private onVisibilityChanged = (e: Event) => {
        const { first, last } = e as VisibilityChangedEvent;

        if (!this.hasRestoredScroll) {
            // The grid layout fires a premature visibilityChanged
            // before the viewport width is measured. Skip until
            // real items are visible.
            if (last <= 0) {
                return;
            }

            this.hasRestoredScroll = true;

            const savedIndex =
                this.libraryCtrl.getScrollPosition('albums');

            if (savedIndex > 0) {
                void this.restoreScrollPosition(savedIndex);

                return;
            }

            this.hiddenForRestore = false;
        }

        this.libraryCtrl.setScrollPosition('albums', first);
    };

    private async restoreScrollPosition(
        savedIndex: number,
    ) {
        // Wait for the virtualizer layout to settle. layoutComplete
        // resolves after ResizeObserver + double-rAF, so the sizer
        // transform has been painted and scrollHeight is correct.
        await this.virtualizer?.layoutComplete;

        // Compute pixel offset matching the grid layout internals:
        // offset = padding + row * (itemHeight + gap).
        const vw = this.virtualizer?.clientWidth ?? 0;
        const {
            GRID_ITEM_WIDTH,
            GRID_ITEM_HEIGHT,
            GRID_GAP,
            GRID_PADDING,
        } = CoverGrid;

        const availableWidth = vw - GRID_PADDING * 2;
        const columns = Math.max(
            1,
            Math.floor(
                (availableWidth + GRID_GAP) /
                    (GRID_ITEM_WIDTH + GRID_GAP),
            ),
        );
        const row = Math.floor(savedIndex / columns);
        const pixelOffset =
            GRID_PADDING +
            row * (GRID_ITEM_HEIGHT + GRID_GAP);

        if (this.virtualizer) {
            this.virtualizer.scrollTop = pixelOffset;
        }

        // Reveal now that the virtualizer is at the correct position.
        this.hiddenForRestore = false;
    }

    private selectRange(
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

    private async getSelectedFilePaths(): Promise<string[]> {
        const selected = this.albums.filter(
            (a) => this.selectedAlbums.has(a.ID),
        );
        const allPaths: string[] = [];

        for (const album of selected) {
            const paths = await this.getAlbumFilePaths(album);
            allPaths.push(...paths);
        }

        return allPaths;
    }

    private async getAlbumFilePaths(album: library.Album): Promise<string[]> {
        try {
            const tracks = await GetAlbumTracks(album.ID);

            return tracks.map((t) => t.FilePath);
        } catch (error) {
            console.error("Error loading album tracks:", error);

            return [];
        }
    }

    private async onAlbumDblClick(album: library.Album) {
        const filePaths = await this.getAlbumFilePaths(album);

        if (filePaths.length === 0) return;

        this.selectedAlbums = new Set();
        this.queue.setQueue(filePaths, 0);
    }

    private onAlbumContextMenu(e: MouseEvent, album: library.Album) {
        e.preventDefault();
        e.stopPropagation();

        if (!this.selectedAlbums.has(album.ID)) {
            this.selectedAlbums = new Set([album.ID]);
        }

        this.contextMenuOpen = true;

        this.updateComplete.then(() => {
            const popup = this.contextMenuPopup;

            if (popup) {
                (popup as any).anchor = {
                    getBoundingClientRect() {
                        return {
                            width: 0,
                            height: 0,
                            x: e.clientX,
                            y: e.clientY,
                            top: e.clientY,
                            left: e.clientX,
                            right: e.clientX,
                            bottom: e.clientY,
                        };
                    },
                };
                (popup as any).active = true;
            }
        });
    }

    private async onContextMenuAction(action: string) {
        if (this.selectedAlbums.size === 0) return;

        const filePaths = await this.getSelectedFilePaths();

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                this.queue.setQueue(filePaths, 0);
                break;
            case 'add-to-queue':
                this.queue.addTracksToQueue(filePaths);
                break;
            case 'play-next':
                this.queue.playTracksNext(filePaths);
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
            this.selectedAlbums = new Set();
        }

        const popup = this.contextMenuPopup;

        if (popup) {
            (popup as any).active = false;
        }
    }

    private async showPlaylistSubmenu() {
        if (this.playlistSubmenuOpen) return;

        if (this.selectedAlbums.size > 0) {
            this.playlistFilePaths =
                await this.getSelectedFilePaths();
        }

        this.playlistSubmenuOpen = true;

        await this.updateComplete;

        const submenu = this.playlistSubmenuPopup;
        const trigger = this.shadowRoot?.querySelector(
            '.submenu-item',
        );

        if (submenu && trigger) {
            (submenu as any).anchor = trigger;
            (submenu as any).active = true;
        }

        const picker = this.shadowRoot?.querySelector(
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

    private renderAlbumCard = (
        album: library.Album,
        index: number,
    ): unknown => {
        const selected = this.selectedAlbums.has(album.ID);

        const classes = [
            'album-card',
            selected ? 'selected' : '',
        ]
            .filter(Boolean)
            .join(' ');

        return html`
            <div
                class=${classes}
                tabindex="0"
                role="button"
                aria-label="${album.Name} by ${album.ArtistName}"
                @click=${(e: MouseEvent) =>
                    this.onAlbumClick(e, album, index)}
                @dblclick=${() => this.onAlbumDblClick(album)}
                @keydown=${(e: KeyboardEvent) =>
                    this.onAlbumKeydown(e, album, index)}
                @contextmenu=${(e: MouseEvent) =>
                    this.onAlbumContextMenu(e, album)}
            >
                <div class="cover-container">
                    ${album.CoverArtPath
                        ? html`<img
                              class="cover-image"
                              src="${album.CoverArtThumbnailPath || album.CoverArtPath}"
                              alt="${album.Name} cover"
                              loading="lazy"
                              @error=${(e: Event) => {
                                  const img = e.target as HTMLImageElement;
                                  if (img.src !== album.CoverArtPath) {
                                      img.src = album.CoverArtPath;
                                  }
                              }}
                          />`
                        : html`<div class="placeholder-cover">
                              ${this.getAlbumInitial(album.Name)}
                          </div>`}
                </div>
                <div class="album-info">
                    <div class="album-name" title="${album.Name}">${album.Name}</div>
                    <div class="artist-name" title="${album.ArtistName}">
                        ${album.ArtistName}${album.Year ? ` - ${album.Year}` : ''}
                    </div>
                </div>
            </div>
        `;
    };

    private getAlbumInitial(name: string): string {
        return name.charAt(0).toUpperCase();
    }

    private onGridClick(e: MouseEvent) {
        const clickedCard = e.composedPath().some(
            (el) =>
                el instanceof HTMLElement &&
                el.classList.contains('album-card'),
        );

        if (!clickedCard) {
            this.selectedAlbums = new Set();
            this.lastSelectedIndex = null;
        }
    }

    private onAlbumClick(
        e: MouseEvent,
        album: library.Album,
        index: number,
    ) {
        const isCtrl = e.ctrlKey || e.metaKey;
        const isShift = e.shiftKey;

        if (isShift && this.lastSelectedIndex !== null) {
            const range = this.selectRange(
                this.lastSelectedIndex,
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
            this.lastSelectedIndex = index;
        } else {
            this.selectedAlbums = new Set([album.ID]);
            this.lastSelectedIndex = index;
        }
    }

    private onAlbumKeydown(
        e: KeyboardEvent,
        album: library.Album,
        index: number,
    ) {
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            this.selectedAlbums = new Set([album.ID]);
            this.lastSelectedIndex = index;
        }
    }

    override render() {
        if (this.loading) {
            return html`<div class="loading">Loading albums...</div>`;
        }

        if (this.albums.length === 0) {
            return html`
                <div class="empty-state">
                    <p>No albums found</p>
                    <p>Add music to your library to see album covers here.</p>
                </div>
            `;
        }

        return html`
            <lit-virtualizer
                class=${this.hiddenForRestore ? 'restoring' : ''}
                scroller
                .items=${this.albums}
                .renderItem=${this.renderAlbumCard}
                @click=${(e: MouseEvent) => this.onGridClick(e)}
                .layout=${grid({
                    itemSize: { width: '176px', height: '230px' },
                    gap: '16px',
                    padding: '16px',
                })}
            ></lit-virtualizer>

            <wa-popup
                id="context-menu"
                placement="bottom-start"
                .active=${this.contextMenuOpen}
            >
                ${this.contextMenuOpen
                    ? html`
                        <div class="context-menu-panel">
                            <wa-dropdown-item
                                @click=${() => this.onContextMenuAction('play')}
                            >
                                <wa-icon slot="icon" name="play"></wa-icon>
                                Play
                            </wa-dropdown-item>
                            <wa-dropdown-item
                                @click=${() => this.onContextMenuAction('add-to-queue')}
                            >
                                <wa-icon slot="icon" name="plus"></wa-icon>
                                Add to Queue
                            </wa-dropdown-item>
                            <wa-dropdown-item
                                @click=${() => this.onContextMenuAction('play-next')}
                            >
                                <wa-icon slot="icon" name="forward-step"></wa-icon>
                                Play Next
                            </wa-dropdown-item>
                            <wa-dropdown-item
                                class="submenu-item"
                                @mouseenter=${() => this.showPlaylistSubmenu()}
                                @click=${(e: Event) => {
                                    e.stopPropagation();
                                    void this.showPlaylistSubmenu();
                                }}
                            >
                                <wa-icon slot="icon" name="plus"></wa-icon>
                                Add to Playlist
                                <span class="submenu-arrow">&#9654;</span>
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
                            @click=${(e: Event) => e.stopPropagation()}
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

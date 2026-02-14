import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import { EventsEmit } from '@runtime/runtime';
import { GetAllAlbums, GetAlbumTracks } from '@go/library/Library';
import { library } from '@go/models';
import { QueueController } from '@store/controllers/queue-controller';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

@customElement('cover-grid')
export class CoverGrid extends LitElement {
    private queue = new QueueController(this);

    private closeHandler = () => this.closeContextMenu();

    static override styles = css`
        :host {
            display: block;
        }

        .grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
            gap: 16px;
            padding: 16px;
        }

        .album-card {
            display: flex;
            flex-direction: column;
            cursor: pointer;
            border-radius: 8px;
            padding: 8px;
            transition: background-color 0.2s ease;
        }

        .album-card:hover {
            background-color: rgba(255, 255, 255, 0.1);
        }

        .album-card:focus {
            outline: 2px solid #1db954;
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
            background-color: #2a2a3e;
            border: 1px solid #444;
            border-radius: 6px;
            padding: 4px 0;
            box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
            min-width: 160px;
        }

        .context-menu-panel wa-dropdown-item {
            cursor: pointer;
        }

        .context-menu-panel wa-dropdown-item::part(base) {
            color: #e0e0e0;
            font-size: 13px;
        }

        .context-menu-panel wa-dropdown-item::part(base):hover {
            background-color: rgba(255, 255, 255, 0.1);
        }
    `;

    @state()
    private albums: library.Album[] = [];

    @state()
    private loading = true;

    @state()
    private contextMenuOpen = false;

    @state()
    private contextMenuAlbum: library.Album | null = null;

    @query('#context-menu')
    private contextMenuPopup!: HTMLElement;

    override connectedCallback() {
        super.connectedCallback();
        this.loadAlbums();
        document.addEventListener('click', this.closeHandler);
        document.addEventListener('contextmenu', this.closeHandler);
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        document.removeEventListener('click', this.closeHandler);
        document.removeEventListener('contextmenu', this.closeHandler);
    }

    private async loadAlbums() {
        try {
            this.loading = true;
            const albums = await GetAllAlbums();
            this.albums = albums ?? [];
        } catch (error) {
            console.error("Error loading albums:", error);
            this.albums = [];
        } finally {
            this.loading = false;
        }
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

    private onAlbumContextMenu(e: MouseEvent, album: library.Album) {
        e.preventDefault();
        e.stopPropagation();

        this.contextMenuAlbum = album;
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
        if (!this.contextMenuAlbum) return;

        const filePaths = await this.getAlbumFilePaths(this.contextMenuAlbum);

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

        this.closeContextMenu();
    }

    private closeContextMenu() {
        if (!this.contextMenuOpen) return;

        this.contextMenuOpen = false;
        this.contextMenuAlbum = null;

        const popup = this.contextMenuPopup;

        if (popup) {
            (popup as any).active = false;
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
            <div class="grid">
                ${this.albums.map(album => this.renderAlbumCard(album))}
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
                        </div>
                    `
                    : nothing}
            </wa-popup>
        `;
    }

    private renderAlbumCard(album: library.Album) {
        return html`
            <div
                class="album-card"
                tabindex="0"
                role="button"
                aria-label="${album.Name} by ${album.ArtistName}"
                @click=${() => this.onAlbumClick(album)}
                @keydown=${(e: KeyboardEvent) => this.onAlbumKeydown(e, album)}
                @contextmenu=${(e: MouseEvent) => this.onAlbumContextMenu(e, album)}
            >
                <div class="cover-container">
                    ${album.CoverArtPath
                        ? html`<img
                              class="cover-image"
                              src="${album.CoverArtPath}"
                              alt="${album.Name} cover"
                              loading="lazy"
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
    }

    private getAlbumInitial(name: string): string {
        return name.charAt(0).toUpperCase();
    }

    private onAlbumClick(album: library.Album) {
        EventsEmit('AlbumSelected', album);
        this.dispatchEvent(
            new CustomEvent('album-selected', {
                detail: album,
                bubbles: true,
                composed: true,
            })
        );
    }

    private onAlbumKeydown(e: KeyboardEvent, album: library.Album) {
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            this.onAlbumClick(album);
        }
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'cover-grid': CoverGrid;
    }
}

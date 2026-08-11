import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/button/button.js';
import { GetShelves } from '@go/home/Service';
import { GetAlbumTracks } from '@go/library/Library';
import type { home, library } from '@go/models';
import { queueStore } from '@store/queue-store';
import { libraryStore } from '@store/library-store';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import { designTokens } from '../../styles/tokens.css';

type Shelf = home.Shelf;

/** Icon per shelf kind — a row's reason, at a glance. */
const KIND_ICONS: Record<string, string> = {
    'recently-played': 'clock-rotate-left',
    'recently-added': 'star',
    'most-played': 'repeat',
    unplayed: 'box-open',
    stale: 'hourglass-half',
    artist: 'user',
    genre: 'masks-theater',
    random: 'shuffle',
};

/**
 * The home page: a set of ways *into* the library, rather than another
 * view of it.
 *
 * Everything here is computed by `backend/home`, including the reason
 * each row exists, so the rows can change with the user's listening
 * without the frontend holding a second opinion about what "on repeat"
 * means. This component's job is only to render them and to make a
 * cover do the two things a cover should: open the album, or play it.
 */
@customElement('home-view')
export class HomeView extends LitElement {
    @state() private shelves: Shelf[] = [];

    @state() private loading = true;

    @state() private failed = false;

    /** Generation of the library the shelves were built from. */
    private builtFromGeneration = -1;

    private unsubScan?: () => void;

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
                height: 100%;
                overflow-y: auto;
                padding: 24px 20px 40px;
                box-sizing: border-box;
            }

            header {
                display: flex;
                align-items: baseline;
                gap: 12px;
                margin-bottom: 4px;
            }

            h1 {
                margin: 0;
                font-size: 24px;
                font-weight: 700;
                color: var(--yj-text-primary, #fff);
                flex: 1;
            }

            .lede {
                margin: 0 0 24px;
                font-size: var(--yj-text-md, 13px);
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .shelf {
                margin-bottom: 28px;
            }

            .shelf-head {
                display: flex;
                align-items: center;
                gap: 8px;
                margin-bottom: 2px;
            }

            .shelf-title {
                font-size: var(--yj-text-xl, 18px);
                font-weight: 700;
                color: var(--yj-text-primary, #fff);
            }

            .shelf-sub {
                margin: 0 0 10px;
                font-size: var(--yj-text-sm, 12px);
                color: var(--yj-text-tertiary, #888);
            }

            .row {
                display: grid;
                grid-auto-flow: column;
                grid-auto-columns: 160px;
                gap: 14px;
                overflow-x: auto;
                padding-bottom: 6px;
                scrollbar-width: thin;
            }

            .card {
                background: none;
                border: none;
                padding: 0;
                text-align: left;
                cursor: pointer;
                color: inherit;
                display: block;
            }

            .art {
                position: relative;
                width: 160px;
                height: 160px;
                border-radius: 6px;
                overflow: hidden;
                background: var(--yj-bg-surface, #181818);
                display: flex;
                align-items: center;
                justify-content: center;
                color: var(--yj-text-tertiary, #888);
            }

            .art img {
                width: 100%;
                height: 100%;
                object-fit: cover;
                display: block;
            }

            .play {
                position: absolute;
                right: 8px;
                bottom: 8px;
                width: 38px;
                height: 38px;
                border: none;
                border-radius: 50%;
                background: var(--yj-accent, #ffd43b);
                color: #000;
                display: flex;
                align-items: center;
                justify-content: center;
                cursor: pointer;
                opacity: 0;
                transform: translateY(6px);
                transition: opacity 0.12s ease, transform 0.12s ease;
            }

            .card:hover .play,
            .card:focus-within .play {
                opacity: 1;
                transform: translateY(0);
            }

            .name {
                margin-top: 8px;
                font-size: var(--yj-text-md, 13px);
                color: var(--yj-text-primary, #fff);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .artist {
                font-size: var(--yj-text-sm, 12px);
                color: var(--yj-text-tertiary, #888);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .empty {
                padding: 48px 20px;
                text-align: center;
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-lg, 15px);
            }
        `,
    ];

    override connectedCallback(): void {
        super.connectedCallback();
        void this.load();

        // A finished scan changes what every shelf would say, and the
        // home page is the view most likely to be sitting open while
        // one runs.
        this.unsubScan = EventsOn(Events.LibraryScanComplete, () => {
            void this.load();
        });
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.unsubScan?.();
        this.unsubScan = undefined;
    }

    /**
     * Rebuild when the view is shown again after the library changed.
     * Navigation keeps this element alive (see `frontend/index.ts`), so
     * without this the shelves would be as old as the session.
     */
    override willUpdate(): void {
        if (
            !this.loading
            && this.builtFromGeneration !== libraryStore.changeGeneration
        ) {
            void this.load();
        }
    }

    private async load(): Promise<void> {
        this.builtFromGeneration = libraryStore.changeGeneration;
        this.loading = true;

        try {
            this.shelves = (await GetShelves()) ?? [];
            this.failed = false;
        } catch (err) {
            console.error('Could not build the home page:', err);
            this.failed = true;
        } finally {
            this.loading = false;
        }
    }

    override render() {
        return html`
            <header>
                <h1>Home</h1>
                <wa-button
                    size="small"
                    appearance="plain"
                    title="Reshuffle the suggestions"
                    @click=${() => void this.load()}
                >
                    <wa-icon slot="start" name="shuffle"></wa-icon>
                    Shuffle
                </wa-button>
            </header>
            <p class="lede">Somewhere to start listening.</p>
            ${this.renderBody()}
        `;
    }

    private renderBody() {
        if (this.loading && this.shelves.length === 0) {
            return html`<div class="empty">Looking through your library\u2026</div>`;
        }

        if (this.failed) {
            return html`<div class="empty">
                Could not read your library just now.
            </div>`;
        }

        if (this.shelves.length === 0) {
            return html`<div class="empty">
                Nothing to suggest yet \u2014 add a music folder under Settings
                and the shelves fill in once it has been scanned.
            </div>`;
        }

        return this.shelves.map((shelf) => this.renderShelf(shelf));
    }

    private renderShelf(shelf: Shelf) {
        return html`
            <section class="shelf" data-kind=${shelf.kind}>
                <div class="shelf-head">
                    <wa-icon name=${KIND_ICONS[shelf.kind] ?? 'compact-disc'}></wa-icon>
                    <span class="shelf-title">${shelf.title}</span>
                </div>
                <p class="shelf-sub">${shelf.subtitle}</p>
                <div class="row">
                    ${shelf.albums.map((album) => this.renderCard(album))}
                </div>
            </section>
        `;
    }

    private renderCard(album: library.Album) {
        const art = album.CoverArtMedium || album.CoverArtSmall || album.CoverArtPath;

        return html`
            <div
                class="card"
                role="button"
                tabindex="0"
                title="${album.Name}${album.ArtistName ? ` \u2014 ${album.ArtistName}` : ''}"
                @click=${() => this.openAlbum(album)}
                @keydown=${(e: KeyboardEvent) => this.onCardKey(e, album)}
            >
                <div class="art">
                    ${art
                        ? html`<img src=${art} alt="" loading="lazy" />`
                        : html`<wa-icon name="compact-disc"></wa-icon>`}
                    <button
                        class="play"
                        title="Play this album"
                        aria-label="Play ${album.Name}"
                        @click=${(e: Event) => {
                            e.stopPropagation();
                            void this.playAlbum(album);
                        }}
                    >
                        <wa-icon name="play"></wa-icon>
                    </button>
                </div>
                <div class="name">${album.Name}</div>
                ${album.ArtistName
                    ? html`<div class="artist">${album.ArtistName}</div>`
                    : nothing}
            </div>
        `;
    }

    private onCardKey(e: KeyboardEvent, album: library.Album): void {
        if (e.key !== 'Enter' && e.key !== ' ') return;

        e.preventDefault();
        this.openAlbum(album);
    }

    private openAlbum(album: library.Album): void {
        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: {
                    view: 'explore-album-details',
                    releaseGroupMBID: album.MBID || '',
                    albumName: album.Name,
                    artistName: album.ArtistName,
                    localAlbumId: album.ID,
                },
            }),
        );
    }

    private async playAlbum(album: library.Album): Promise<void> {
        try {
            const tracks = await GetAlbumTracks(album.ID);
            const paths = (tracks ?? []).map((t) => t.FilePath).filter(Boolean);

            if (paths.length === 0) return;

            queueStore.setQueue(paths, 0, true);
        } catch (err) {
            console.error('Could not play that album:', err);
        }
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'home-view': HomeView;
    }
}

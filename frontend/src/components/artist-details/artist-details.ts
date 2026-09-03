import { LitElement, html, css } from 'lit';
import {
    customElement,
    property,
    state,
} from 'lit/decorators.js';
import * as library from '@go/library/models.js';
import { LibraryController } from '@store/controllers/library-controller';
import {
    GetArtistImageURL,
    GetArtistImageCachedPath,
    GetArtistMBID,
} from '@go/explore/service.js';
import { GetFilePathsByAlbums } from '@go/library/library.js';
import { libraryStore } from '@store/library-store';
import { notificationStore } from '@store/notification-store';
import { dict } from '@utils/binding';
import { playAll } from '@utils/play-all';
import { describeError } from '@utils/describe-error';
import { ICON_PLAY, ICON_SHUFFLE } from '@utils/icon-language';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/cover-grid/cover-grid.js';
import '../notifications/inline-notice';
import { designTokens } from '../../styles/tokens.css';
import { backButton } from '../../styles/back-button.css';

/** The region the artist header's own failures are rendered in. */
const ArtistRegion = 'library-artist';

@customElement('artist-details')
export class ArtistDetails extends LitElement {
    @property({ type: Number, attribute: 'artist-id' })
    artistId = 0;

    @property({ type: String, attribute: 'artist-name' })
    artistName = '';

    @property({ type: String, attribute: 'artist-mbid' })
    artistMBID = '';

    @state()
    private albums: library.Album[] = [];

    @state()
    private loading = true;

    @state()
    private artistImageURL = '';

    private libraryCtrl = new LibraryController(this);

    /** Tracks the store's cached array reference to detect refreshes. */
    private lastAlbumsRef: library.Album[] | null = null;

    static override styles = [designTokens, backButton, css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
            height: 100%;
            box-sizing: border-box;
        }

        /* ====================================
         * Header
         * ==================================== */

        .artist-header {
            display: flex;
            align-items: center;
            gap: 20px;
            padding: 16px 20px;
            flex-shrink: 0;
            border-bottom: 1px solid
                var(
                    --yj-border-subtle,
                    rgba(255, 255, 255, 0.06)
                );
        }

        .back-button wa-icon {
            font-size: 16px; /* back button — outside type scale */
        }

        .artist-avatar {
            width: 80px;
            height: 80px;
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

        .artist-avatar img {
            width: 100%;
            height: 100%;
            object-fit: cover;
        }

        .artist-avatar .initial {
            color: var(
                --yj-text-secondary,
                #b3b3b3
            );
            font-size: 32px; /* large decorative initial */
            font-weight: 600;
            text-transform: uppercase;
            user-select: none;
            line-height: 1;
        }

        .artist-info {
            display: flex;
            flex-direction: column;
            gap: 4px;
            min-width: 0;
        }

        .artist-title {
            font-size: 24px; /* page title — outside type scale */
            font-weight: 700;
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            margin: 0;
            line-height: 1.2;
        }

        .album-count {
            font-size: var(--yj-text-md);
            color: var(
                --yj-text-secondary,
                #b3b3b3
            );
        }

        .header-actions {
            margin-left: auto;
            display: flex;
            align-items: center;
            gap: 8px;
            flex-shrink: 0;
        }

        .header-action {
            background: none;
            border: 1px solid var(--yj-border-subtle, #555);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 6px 12px;
            font-size: var(--yj-text-md, 13px);
            font-family: inherit;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 6px;
            white-space: nowrap;
        }

        .header-action:hover {
            border-color: var(--yj-accent, #ffd43b);
            color: var(--yj-accent-text, #ffd43b);
        }

        .header-action:disabled {
            opacity: 0.5;
            cursor: default;
        }

        /* ====================================
         * Content
         * ==================================== */

        .content {
            flex: 1;
            overflow: hidden;
        }

        cover-grid {
            width: 100%;
            height: 100%;
        }

        /* Phone widths: the header's flex row squeezed .artist-info to
         * nothing, so the title ellipsised away entirely and the
         * actions clipped against the host's own overflow — the album
         * page's fault one detail view over (#66). The pair takes its
         * own row instead. Written last, because a media query adds no
         * specificity and a rule placed above the plain ones it
         * overrides is silently dead. */
        @media (max-width: 599px) {
            .artist-header {
                flex-wrap: wrap;
            }

            .header-actions {
                flex-basis: 100%;
                margin-left: 0;
            }
        }
    `];

    override connectedCallback() {
        super.connectedCallback();
        this.loadAlbums();
        this.loadArtistImage();
    }

    override updated() {
        const cached = this.libraryCtrl.cachedAlbums;

        if (
            cached !== null &&
            cached !== this.lastAlbumsRef
        ) {
            this.lastAlbumsRef = cached;
            this.loadAlbums();
        }
    }

    /* ================================================================
     * Data loading
     * ================================================================ */

    private async loadArtistImage() {
        // Resolve MBID from tags if not provided via attribute.
        let mbid = this.artistMBID;

        if (!mbid && this.artistName) {
            try {
                mbid = await GetArtistMBID(this.artistName);
            } catch {
                return;
            }
        }

        if (!mbid) return;

        try {
            // Disk cache first — the resolving call below is MB →
            // Wikidata → Wikipedia → Wikimedia, and most artists this
            // page renders have been resolved once already.
            const cached = await GetArtistImageCachedPath(mbid);

            if (cached) {
                this.artistImageURL = cached;

                return;
            }

            const url = await GetArtistImageURL(mbid);

            if (url) {
                this.artistImageURL = url;
            }
        } catch {
            // No image — avatar stays as initial letter.
        }
    }

    private async loadAlbums() {
        if (!this.artistId) return;

        // Try to populate instantly from the
        // cached all-albums list if available.
        const cached =
            this.libraryCtrl.getAlbumsByArtistNameCached(
                this.artistName,
            );

        if (cached !== null && cached.length > 0) {
            this.albums = cached;
            this.loading = false;
        }

        // Always run the authoritative backend
        // query.  If we got a cache hit above,
        // this serves as a correction pass.
        try {
            const albums =
                await this.libraryCtrl.getAlbumsByArtist(
                    this.artistName,
                );

            const result = albums ?? [];

            // Skip update if the cached result
            // is identical (same IDs in same
            // order) to avoid a re-render.
            if (!this.albumsMatch(result)) {
                this.albums = result;
            }
        } catch (error) {
            console.error(
                'Error loading artist albums:',
                error,
            );

            // Only overwrite if we had no cached
            // result to fall back on.
            if (cached === null) {
                this.albums = [];
            }
        } finally {
            this.loading = false;
        }
    }

    /**
     * Compare two album lists by ID to avoid
     * unnecessary re-renders when the backend
     * result matches the cached approximation.
     */
    private albumsMatch(
        incoming: library.Album[],
    ): boolean {
        const current = this.albums;

        if (current.length !== incoming.length) {
            return false;
        }

        for (let i = 0; i < current.length; i++) {
            if (
                current[i]!.ID !== incoming[i]!.ID
            ) {
                return false;
            }
        }

        return true;
    }

    /* ================================================================
     * Navigation
     * ================================================================ */

    private navigateBack() {
        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: { view: 'artists' },
            }),
        );
    }

    /* ================================================================
     * Helpers
     * ================================================================ */

    private getInitial(name: string): string {
        if (!name) return '?';

        return name.charAt(0).toUpperCase();
    }

    /**
     * Play every track on this artist's albums, in album order.
     *
     * One `GetFilePathsByAlbums` call returns the paths grouped by
     * album id; the caller owns the ordering, so they are flattened in
     * `this.albums` order rather than by id.
     */
    private async playAllTracks(shuffle: boolean): Promise<void> {
        if (this.albums.length === 0) return;

        try {
            const libId = libraryStore.getSelectedLibraryId() ?? 0;
            const ids = this.albums.map((a) => a.ID);
            const byAlbum = await dict(
                GetFilePathsByAlbums(ids, libId),
            );
            const paths: string[] = [];

            for (const id of ids) {
                paths.push(...(byAlbum[id] ?? []));
            }

            playAll(
                paths,
                {
                    type: 'artist',
                    id: this.artistId,
                    label: this.artistName,
                },
                shuffle,
            );
        } catch (error) {
            console.error('Could not play artist:', error);
            notificationStore.inline(ArtistRegion, {
                text: describeError(error, 'Could not play this artist’s tracks.'),
            });
        }
    }

    /* ================================================================
     * Rendering
     * ================================================================ */

    override render() {
        const albumCount = this.albums.length;
        const albumLabel =
            albumCount === 1 ? 'album' : 'albums';

        return html`
            <div class="artist-header">
                <button
                    class="back-button"
                    @click=${this.navigateBack}
                    title="Back to artists"
                    aria-label="Back to artists"
                >
                    <wa-icon
                        name="arrow-left"
                    ></wa-icon>
                </button>
                <div class="artist-avatar">
                    ${this.artistImageURL
                        ? html`<img
                              src="${this.artistImageURL}"
                              alt="${this.artistName}"
                          />`
                        : html`<span class="initial">
                              ${this.getInitial(this.artistName)}
                          </span>`}
                </div>
                <div class="artist-info">
                    <h1
                        class="artist-title"
                        title="${this.artistName}"
                    >
                        ${this.artistName}
                    </h1>
                    ${!this.loading
                        ? html`
                              <span
                                  class="album-count"
                              >
                                  ${albumCount}
                                  ${albumLabel}
                              </span>
                          `
                        : ''}
                </div>
                <div class="header-actions">
                    <button
                        class="header-action"
                        data-testid="artist-play-all"
                        ?disabled=${this.albums.length === 0}
                        @click=${() =>
                            void this.playAllTracks(false)}
                    >
                        <wa-icon name=${ICON_PLAY}></wa-icon>
                        Play all
                    </button>
                    <button
                        class="header-action"
                        data-testid="artist-shuffle-all"
                        ?disabled=${this.albums.length === 0}
                        @click=${() =>
                            void this.playAllTracks(true)}
                    >
                        <wa-icon name=${ICON_SHUFFLE}></wa-icon>
                        Shuffle all
                    </button>
                </div>
            </div>
            <div class="content">
                <cover-grid
                    .externalAlbums=${this.albums}
                ></cover-grid>
            </div>
            <inline-notice
                region=${ArtistRegion}
                testid="artist-play-message"
            ></inline-notice>
        `;
    }
}

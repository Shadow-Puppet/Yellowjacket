import { LitElement, html, css } from 'lit';
import {
    customElement,
    property,
    state,
} from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';
import { library } from '@go/models';
import { LibraryController } from '@store/controllers/library-controller';
import { Events } from '../../events';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/cover-grid/cover-grid.js';

@customElement('artist-details')
export class ArtistDetails extends LitElement {
    @property({ type: Number, attribute: 'artist-id' })
    artistId = 0;

    @property({ type: String, attribute: 'artist-name' })
    artistName = '';

    @state()
    private albums: library.Album[] = [];

    @state()
    private loading = true;

    private libraryCtrl = new LibraryController(this);
    private cancelScanComplete?: () => void;

    static override styles = css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
            height: 100%;
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

        .back-button {
            display: flex;
            align-items: center;
            justify-content: center;
            width: 32px;
            height: 32px;
            border: none;
            border-radius: 50%;
            background: var(
                --yj-bg-overlay,
                rgba(255, 255, 255, 0.06)
            );
            color: var(--yj-text-primary, #fff);
            cursor: pointer;
            flex-shrink: 0;
            transition: background-color 0.15s ease;
        }

        .back-button:hover {
            background: var(
                --yj-bg-hover,
                rgba(255, 255, 255, 0.12)
            );
        }

        .back-button wa-icon {
            font-size: 16px;
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

        .artist-avatar .initial {
            color: var(
                --yj-text-secondary,
                #b3b3b3
            );
            font-size: 32px;
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
            font-size: 24px;
            font-weight: 700;
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            margin: 0;
            line-height: 1.2;
        }

        .album-count {
            font-size: 13px;
            color: var(
                --yj-text-secondary,
                #b3b3b3
            );
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
    `;

    override connectedCallback() {
        super.connectedCallback();
        this.loadAlbums();
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            () => this.loadAlbums(),
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.cancelScanComplete?.();
    }

    /* ================================================================
     * Data loading
     * ================================================================ */

    private async loadAlbums() {
        if (!this.artistId) return;

        try {
            this.loading = true;

            const albums =
                await this.libraryCtrl.getAlbumsByArtist(
                    this.artistId,
                );

            this.albums = albums ?? [];
        } catch (error) {
            console.error(
                'Error loading artist albums:',
                error,
            );
            this.albums = [];
        } finally {
            this.loading = false;
        }
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
                    <span class="initial">
                        ${this.getInitial(
                            this.artistName,
                        )}
                    </span>
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
            </div>
            <div class="content">
                ${this.loading
                    ? html`
                          <div
                              class="loading-message"
                          >
                              Loading albums...
                          </div>
                      `
                    : html`
                          <cover-grid
                              .externalAlbums=${this
                                  .albums}
                          ></cover-grid>
                      `}
            </div>
        `;
    }
}

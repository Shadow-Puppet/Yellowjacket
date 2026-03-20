import { LitElement, html, css } from 'lit';
import {
    customElement,
    property,
    state,
} from 'lit/decorators.js';
import { library } from '@go/models';
import {
    GetTracksByGenre,
    GetTracksByGenreByLibrary,
} from '@go/library/Library';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import { libraryStore } from '@store/library-store';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/track-list/track-list.js';
import { designTokens } from '../../styles/tokens.css';

@customElement('genre-details')
export class GenreDetails extends LitElement {
    @property({ type: String, attribute: 'genre-name' })
    genreName = '';

    @state()
    private tracks: library.Track[] = [];

    @state()
    private loading = true;

    private scanCompleteCleanup: (() => void) | null =
        null;

    static override styles = [designTokens, css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
            height: 100%;
        }

        /* ====================================
         * Header
         * ==================================== */

        .genre-header {
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
            font-size: 16px; /* back button — outside type scale */
        }

        .genre-avatar {
            width: 80px;
            height: 80px;
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

        .genre-avatar .initial {
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

        .genre-info {
            display: flex;
            flex-direction: column;
            gap: 4px;
            min-width: 0;
        }

        .genre-title {
            font-size: 24px; /* page title — outside type scale */
            font-weight: 700;
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            margin: 0;
            line-height: 1.2;
        }

        .track-count {
            font-size: var(--yj-text-md);
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

        track-list {
            width: 100%;
            height: 100%;
        }
    `];

    override connectedCallback() {
        super.connectedCallback();
        this.loadTracks();

        this.scanCompleteCleanup = EventsOn(
            Events.LibraryScanComplete,
            () => this.loadTracks(),
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();

        if (this.scanCompleteCleanup) {
            this.scanCompleteCleanup();
            this.scanCompleteCleanup = null;
        }
    }

    /* ================================================================
     * Data loading
     * ================================================================ */

    private async loadTracks() {
        if (!this.genreName) return;

        try {
            const libId =
                libraryStore.getSelectedLibraryId();

            this.tracks = libId !== null
                ? await GetTracksByGenreByLibrary(
                      this.genreName,
                      libId,
                  )
                : await GetTracksByGenre(
                      this.genreName,
                  );
        } catch (error) {
            console.error(
                'Error loading genre tracks:',
                error,
            );
            this.tracks = [];
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
                detail: { view: 'genres' },
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
        const trackCount = this.tracks.length;
        const trackLabel =
            trackCount === 1 ? 'track' : 'tracks';

        return html`
            <div class="genre-header">
                <button
                    class="back-button"
                    @click=${this.navigateBack}
                    title="Back to genres"
                    aria-label="Back to genres"
                >
                    <wa-icon
                        name="arrow-left"
                    ></wa-icon>
                </button>
                <div class="genre-avatar">
                    <span class="initial">
                        ${this.getInitial(
                            this.genreName,
                        )}
                    </span>
                </div>
                <div class="genre-info">
                    <h1
                        class="genre-title"
                        title="${this.genreName}"
                    >
                        ${this.genreName}
                    </h1>
                    ${!this.loading
                        ? html`
                              <span
                                  class="track-count"
                              >
                                  ${trackCount}
                                  ${trackLabel}
                              </span>
                          `
                        : ''}
                </div>
            </div>
            <div class="content">
                <track-list
                    .externalTracks=${this.tracks}
                ></track-list>
            </div>
        `;
    }
}

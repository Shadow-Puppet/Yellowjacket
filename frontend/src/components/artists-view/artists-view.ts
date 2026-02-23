import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';
import '@lit-labs/virtualizer';
import { grid } from '@lit-labs/virtualizer/layouts/grid.js';
import { library } from '@go/models';
import { LibraryController } from '@store/controllers/library-controller';
import { SearchController } from '@store/controllers/search-controller';
import { Events } from '../../events';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

/** Pixels to change card width per scroll tick. */
const ZOOM_STEP = 16;

/** localStorage key for persisted artist card size. */
const CARD_SIZE_KEY = 'artists-view-card-size';

/** Card size limits. */
const CARD_SIZE_MIN = 100;
const CARD_SIZE_MAX = 350;
const CARD_SIZE_DEFAULT = 176;

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

    @state()
    private artists: library.Artist[] = [];

    @state()
    private loading = true;

    @state()
    private cardSize: number = CARD_SIZE_DEFAULT;

    // Fixed grid spacing constants.
    private static readonly GRID_GAP = 8;
    private static readonly GRID_PADDING = 8;
    private static readonly CARD_PADDING = 5;

    private get imageSize(): number {
        return this.cardSize - ArtistsView.CARD_PADDING * 2;
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
            font-size: var(--artist-name-font, 14px);
            font-weight: 500;
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            padding: var(--artist-name-pad, 6px) 2px 0;
            line-height: 1.3;
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
        this.loadCardSize();
        this.loadArtists();
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            () => this.loadArtists(),
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.cancelScanComplete?.();
        this.detachWheelListener();
    }

    override updated() {
        this.updateSizeProperties();
        this.ensureWheelListener();
        this.updateGridLayout();
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
            this.loading = false;
        }
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
        const container = this.shadowRoot?.querySelector(
            '.grid-scroll-container',
        );

        if (container && !this.wheelListenerAttached) {
            container.addEventListener(
                'wheel',
                this.wheelHandler as EventListener,
                { passive: false },
            );
            this.wheelListenerAttached = true;
        }
    }

    private detachWheelListener() {
        const container = this.shadowRoot?.querySelector(
            '.grid-scroll-container',
        );

        if (container && this.wheelListenerAttached) {
            container.removeEventListener(
                'wheel',
                this.wheelHandler as EventListener,
            );
            this.wheelListenerAttached = false;
        }
    }

    /* ================================================================
     * Grid layout
     * ================================================================ */

    private lastLayoutWidth = 0;

    private updateGridLayout() {
        if (this.cardSize === this.lastLayoutWidth) {
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
     * Artist card click
     * ================================================================ */

    private onArtistClick(artist: library.Artist) {
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

    /* ================================================================
     * Helpers
     * ================================================================ */

    private getArtistInitial(name: string): string {
        if (!name) return '?';

        return name.charAt(0).toUpperCase();
    }

    /* ================================================================
     * Rendering
     * ================================================================ */

    private renderArtistCard(
        entry: ArtistEntry,
    ) {
        const { artist } = entry;
        const imgSize = this.imageSize;
        const placeholderFont = Math.round(
            imgSize * 0.38,
        );

        return html`
            <div
                class="artist-card"
                tabindex="0"
                role="button"
                aria-label="${artist.Name}"
                style="
                    --avatar-size: ${imgSize}px;
                    --placeholder-font: ${placeholderFont}px;
                "
                @click=${() =>
                    this.onArtistClick(artist)}
                @keydown=${(e: KeyboardEvent) => {
                    if (
                        e.key === 'Enter' ||
                        e.key === ' '
                    ) {
                        e.preventDefault();
                        this.onArtistClick(artist);
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
            <div class="grid-scroll-container">
                <lit-virtualizer
                    .items=${entries}
                    .renderItem=${(
                        entry: ArtistEntry,
                    ) =>
                        this.renderArtistCard(entry)}
                    .layout=${this.gridLayout}
                ></lit-virtualizer>
            </div>
        `;
    }
}

import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import type * as explore from '@go/explore/models.js';
import {
    GetArtistImageURL,
    GetThumbnail,
    RecordSearchClick,
} from '@go/explore/service.js';
import '../library-status-indicator/library-status-indicator.js';
import type { LibraryStatus } from '../library-status-indicator/library-status-indicator.js';
import { creditLink, exploreLinkStyles } from '../../utils/explore-link';
import { creditStore } from '@store/credit-store';
import { albumBadgeFor, libraryStatusFor } from '../../utils/library-status';
import {
    isOwned,
    ownershipLabel,
    unownedStyles,
    type OwnableKind,
} from '../../utils/ownership';
import { completenessStore } from '../../store/completeness-store';
import { downloadStore } from '../../store/download-store';
import { classMap } from 'lit/directives/class-map.js';

/** Format milliseconds as m:ss. */
function formatDuration(ms: number | undefined): string {
    if (!ms || ms <= 0) return '';
    const totalSeconds = Math.floor(ms / 1000);
    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;
    return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

/** Color for entity type badges. */
function badgeColor(type: string): string {
    switch (type) {
        case 'artist': return '#7c3aed';
        case 'release_group': return '#2563eb';
        case 'recording': return '#059669';
        default: return '#6b7280';
    }
}

function badgeLabel(type: string): string {
    switch (type) {
        case 'artist': return 'Artist';
        case 'release_group': return 'Album';
        case 'recording': return 'Track';
        default: return type;
    }
}

@customElement('top-results-row')
export class TopResultsRow extends LitElement {
    @property({ attribute: false })
    results: explore.TopResult[] = [];

    @property({ type: String })
    query = '';

    // Per-card state: cover images.
    private images = new Map<string, string>();

    private unsubRequests?: () => void;

    /**
     * The badges here say whether something is already requested, and
     * this row will not hear about a change from its host: `explore-view`
     * re-rendering sets the same `results` array back, so Lit stops at
     * the property and never updates this element. One subscription for
     * the row, not one per card.
     */
    /** Unsubscribes the credit-arrival repaint. */
    private creditsUnsub?: () => void;

    /** Unsubscribes the "how much of this album is here" repaint. */
    private unsubCompleteness?: () => void;

    override connectedCallback(): void {
        super.connectedCallback();

        this.creditsUnsub = creditStore.subscribe(() => {
            this.requestUpdate();
        });
        this.unsubRequests = downloadStore.subscribe(() =>
            this.requestUpdate(),
        );
        this.unsubCompleteness = completenessStore.subscribe(() =>
            this.requestUpdate(),
        );
    }

    override disconnectedCallback(): void {
        this.creditsUnsub?.();
        this.creditsUnsub = undefined;
        this.unsubRequests?.();
        this.unsubRequests = undefined;
        this.unsubCompleteness?.();
        this.unsubCompleteness = undefined;
        super.disconnectedCallback();
    }

    static override styles = [
        designTokens,
        exploreLinkStyles,
        unownedStyles,
        css`
            :host {
                display: block;
                margin-bottom: 16px;
            }

            .row {
                display: flex;
                gap: 12px;
                overflow-x: auto;
                padding-bottom: 4px;
            }

            .card {
                flex: 0 0 auto;
                width: 200px;
                background: var(--yj-bg-elevated, rgba(255, 255, 255, 0.06));
                border-radius: 10px;
                padding: 14px;
                cursor: pointer;
                transition: background 0.15s ease, transform 0.1s ease;
                display: flex;
                flex-direction: column;
                gap: 8px;
                position: relative;
            }

            .card > library-status-indicator {
                position: absolute;
                right: 10px;
                bottom: 10px;
            }

            .card:hover {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.1));
                transform: translateY(-1px);
            }

            .card:active {
                transform: scale(0.98);
            }

            .card-header {
                display: flex;
                align-items: center;
                gap: 10px;
            }

            .card-image {
                width: 48px;
                height: 48px;
                border-radius: 6px;
                object-fit: cover;
                flex-shrink: 0;
                background: var(--yj-bg-subtle, rgba(255, 255, 255, 0.04));
            }

            .card-image.artist {
                border-radius: 50%;
            }

            .card-image-placeholder {
                width: 48px;
                height: 48px;
                border-radius: 6px;
                flex-shrink: 0;
                background: var(--yj-bg-subtle, rgba(255, 255, 255, 0.08));
                display: flex;
                align-items: center;
                justify-content: center;
                font-size: 20px;
                color: var(--yj-text-secondary, #999);
            }

            .card-image-placeholder.artist {
                border-radius: 50%;
            }

            .card-info {
                flex: 1;
                min-width: 0;
                display: flex;
                flex-direction: column;
                gap: 2px;
            }

            .card-name {
                font-weight: 600;
                font-size: var(--yj-text-md);
                color: var(--yj-text-primary, #fff);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .card-subtitle {
                font-size: var(--yj-text-xs);
                color: var(--yj-text-secondary, #999);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .badge {
                display: inline-block;
                font-size: 10px;
                font-weight: 600;
                letter-spacing: 0.5px;
                text-transform: uppercase;
                padding: 2px 6px;
                border-radius: 4px;
                color: #fff;
                width: fit-content;
            }

            .section-label {
                font-size: var(--yj-text-xs);
                font-weight: 600;
                text-transform: uppercase;
                letter-spacing: 0.5px;
                color: var(--yj-text-secondary, #999);
                margin-bottom: 8px;
            }
        `,
    ];

    override updated(changed: Map<string, unknown>) {
        if (changed.has('results')) {
            this.loadCardData();
        }
    }

    private async loadCardData() {
        for (const r of this.results) {
            if (!r.mbid || this.images.has(r.mbid)) continue;

            if (r.entityType === 'artist') {
                // Load artist image.
                GetArtistImageURL(r.mbid)
                    .then((url) => {
                        if (url) {
                            this.images.set(r.mbid, url);
                            this.requestUpdate();
                        }
                    })
                    .catch(() => {});

                // Load preview tracks removed — cards are cleaner without them.
            } else if (r.entityType === 'release_group') {
                // Load album art.
                GetThumbnail(r.mbid, r.name, r.artistCredit || '')
                    .then((url) => {
                        if (url) {
                            this.images.set(r.mbid, url);
                            this.requestUpdate();
                        }
                    })
                    .catch(() => {});
            }
        }
    }

    private handleClick(r: explore.TopResult) {
        // Record the click for learning.
        RecordSearchClick(this.query, r.mbid, r.entityType).catch(() => {});

        // Navigate to the appropriate explore page.
        this.dispatchEvent(
            new CustomEvent('top-result-click', {
                detail: r,
                bubbles: true,
                composed: true,
            }),
        );
    }

    override render() {
        if (!this.results?.length) return nothing;

        return html`
            <div class="section-label">Top Results</div>
            <div class="row">
                ${this.results.map((r) => this.renderCard(r))}
            </div>
        `;
    }

    private renderCard(r: explore.TopResult) {
        const imgUrl = this.images.get(r.mbid);
        const isArtist = r.entityType === 'artist';

        // The artist portion of the subtitle links to the artist page;
        // the remaining metadata (type/country, year, duration) is plain
        // text.  Artist cards have no artist credit — their whole subtitle
        // is metadata.
        const artistPart = isArtist ? '' : r.artistCredit || '';
        const metaPart = isArtist
            ? [r.artistType, r.country].filter(Boolean).join(' · ')
            : r.entityType === 'release_group'
              ? r.year || ''
              : formatDuration(r.length) || '';

        const entityType: OwnableKind =
            r.entityType === 'artist'
                ? 'artist'
                : r.entityType === 'release_group'
                  ? 'album'
                  : 'track';

        // Ownership is the local row, not the catalog's flag — see
        // `utils/ownership.ts`. An album additionally says *how much*
        // of it is here, which is the one thing a tick cannot.
        const owned = isOwned(r);
        const badge =
            entityType === 'album'
                ? albumBadgeFor(r, r.mbid)
                : {
                      status: libraryStatusFor(owned, r.mbid) as LibraryStatus,
                      owned: 0,
                      expected: 0,
                  };

        // A card navigates whether or not the entity is owned, so it is
        // not `aria-disabled` the way an unplayable track row is — the
        // name is what carries the state to anyone not seeing the
        // dimming.
        return html`
            <div
                class=${classMap({ card: true, unowned: !owned })}
                role="button"
                tabindex="0"
                aria-label=${ownershipLabel(
                    owned,
                    `${badgeLabel(r.entityType)}:`,
                    r.name,
                    entityType,
                )}
                @click=${() => this.handleClick(r)}
                @keydown=${(e: KeyboardEvent) => {
                    if (e.key !== 'Enter' && e.key !== ' ') return;

                    e.preventDefault();
                    this.handleClick(r);
                }}
            >
                <span
                    class="badge"
                    style="background: ${badgeColor(r.entityType)}"
                    >${badgeLabel(r.entityType)}</span
                >
                <div class="card-header">
                    ${imgUrl
                        ? html`<img
                              class="card-image ${isArtist ? 'artist' : ''}"
                              src="${imgUrl}"
                              alt=""
                              loading="lazy"
                          />`
                        : html`<div
                              class="card-image-placeholder ${isArtist ? 'artist' : ''}"
                          >
                              ${r.name.charAt(0)}
                          </div>`}
                    <div class="card-info">
                        <span class="card-name">${r.name}</span>
                        ${artistPart || metaPart
                            ? html`<span class="card-subtitle"
                                  >${artistPart
                                      ? creditLink(creditStore.credits(r.mbid), artistPart, r.artistMbid ?? '')
                                      : nothing}${artistPart && metaPart
                                      ? ' · '
                                      : ''}${metaPart}</span
                              >`
                            : nothing}
                    </div>
                </div>
                ${isArtist || badge.status === 'in-library'
                    ? nothing
                    : html`<library-status-indicator
                        status=${badge.status}
                        owned=${badge.owned}
                        expected=${badge.expected}
                        entity-type=${entityType}
                        label=${r.name}
                        request-mbid=${r.mbid}
                        request-artist=${r.artistCredit ?? ''}
                        size="22"
                    ></library-status-indicator>`}
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'top-results-row': TopResultsRow;
    }
}

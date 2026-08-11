import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';

/**
 * How much of an album or artist page the user is actually looking at.
 *
 * The album and artist pages draw from two sources — the MusicBrainz
 * catalog, and the local library — and until now the page looked the
 * same either way.  That is the confusing part: a page showing one
 * track because that is all you own is indistinguishable from a page
 * showing one track because that is all the album has, and a page that
 * is still waiting on a background catalog fetch looks like a page that
 * has finished and found nothing.
 *
 *  - `catalog`   — full catalog data.  Nothing is rendered; the normal
 *                  case does not need a banner.
 *  - `loading`   — a catalog fetch is in flight; what is on screen is
 *                  the library copy, standing in.
 *  - `library`   — the entity carries no MusicBrainz ID, so the catalog
 *                  has nothing to say about it, now or later.
 *  - `unavailable` — the catalog was asked and did not answer (offline,
 *                  timeout, error).  Retrying is meaningful here, and
 *                  only here.
 */
export type CatalogScope = 'catalog' | 'loading' | 'library' | 'unavailable';

/**
 * One-line banner naming the source of what is on screen.
 *
 * Emits `catalog-retry` (bubbling, composed) when the user asks for
 * another attempt, which only appears for the `unavailable` scope.
 */
@customElement('catalog-scope-notice')
export class CatalogScopeNotice extends LitElement {
    @property({ type: String })
    scope: CatalogScope = 'catalog';

    /** What the page is about, so the copy can name it. */
    @property({ type: String, attribute: 'entity-type' })
    entityType: 'album' | 'artist' = 'album';

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
            }

            .notice {
                display: flex;
                align-items: center;
                gap: 8px;
                padding: 8px 12px;
                border-radius: 6px;
                font-size: var(--yj-text-sm, 12px);
                line-height: 1.4;
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .notice.unavailable {
                color: var(--yj-text-primary, #fff);
            }

            wa-icon {
                flex-shrink: 0;
            }

            .text {
                flex: 1;
                min-width: 0;
            }

            button {
                flex-shrink: 0;
                background: none;
                border: 1px solid var(--yj-border-subtle, #333);
                border-radius: 4px;
                color: inherit;
                cursor: pointer;
                font-size: var(--yj-text-sm, 12px);
                padding: 3px 10px;
            }

            button:hover {
                border-color: var(--yj-accent, #ffd43b);
            }

            .spin {
                animation: spin 1.4s linear infinite;
            }

            @keyframes spin {
                to {
                    transform: rotate(360deg);
                }
            }
        `,
    ];

    override render() {
        if (this.scope === 'catalog') return nothing;

        const entity = this.entityType;
        const copy = this.copyFor(entity);

        return html`
            <div class="notice ${this.scope}" role="status">
                <wa-icon
                    class=${this.scope === 'loading' ? 'spin' : ''}
                    name=${copy.icon}
                ></wa-icon>
                <span class="text">${copy.text}</span>
                ${this.scope === 'unavailable'
                    ? html`<button @click=${this.retry}>Retry</button>`
                    : nothing}
            </div>
        `;
    }

    private copyFor(entity: 'album' | 'artist'): {
        icon: string;
        text: string;
    } {
        const thing = entity === 'album' ? 'album' : 'artist';

        switch (this.scope) {
            case 'loading':
                return {
                    icon: 'rotate',
                    text: `Showing what your library has while the full ${thing} details load\u2026`,
                };
            case 'library':
                return {
                    icon: 'database',
                    text:
                        `Library only \u2014 this ${thing} isn't matched to MusicBrainz, `
                        + 'so only what you already have is shown. Tag it in Autotag '
                        + 'to see the rest.',
                };
            case 'unavailable':
                return {
                    icon: 'triangle-exclamation',
                    // Deliberately covers both "the fetch failed" and
                    // "the catalog has nothing for this one": the user
                    // cannot tell those apart and does not need to —
                    // what matters is that this page is their own copy.
                    text:
                        `No catalog details for this ${thing} right now, so this is your `
                        + 'library copy \u2014 anything you do not own is missing from this page.',
                };
            default:
                return { icon: 'circle-info', text: '' };
        }
    }

    private retry = () => {
        this.dispatchEvent(
            new CustomEvent('catalog-retry', { bubbles: true, composed: true }),
        );
    };
}

declare global {
    interface HTMLElementTagNameMap {
        'catalog-scope-notice': CatalogScopeNotice;
    }
}

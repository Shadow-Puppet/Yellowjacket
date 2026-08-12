import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

import { designTokens } from '../../styles/tokens.css';

/**
 * The one arrangement every primary view uses to say what it is.
 *
 * Before this, four views had a heading and four did not, two had sort
 * controls and none showed a count (`hands-on.md`, H-19) — so the page
 * shifted its shape as you moved through it, and the answer to "how
 * many albums do I have" was to count them. Each view had also written
 * its own arrangement, which is why they disagreed: the sort toolbar in
 * `track-list` and the one in `cover-grid` are the same twenty lines
 * twice, and the ninth view would have made a ninth.
 *
 * Title, count, sort, actions — in that order, in one component, so a
 * new view gets the shape by using it rather than by copying whichever
 * neighbour it happened to read.
 */

export interface SortOption {
    id: string;
    label: string;
}

export type SortDirection = 'asc' | 'desc';

@customElement('page-header')
export class PageHeader extends LitElement {
    /**
     * The page's name. Rendered as the view's only `h1`.
     *
     * Empty is a real mode, not a missing value: `cover-grid` and
     * `track-list` are also *embedded* in the artist and genre pages,
     * which already have a heading of their own. There they keep the
     * count and the sort and drop the title, rather than growing a
     * second arrangement for the same three controls.
     */
    @property({ type: String })
    heading = '';

    /**
     * How many things are on the page. `null` means "not applicable"
     * (Jobs, Settings) rather than zero, and renders nothing — an
     * empty page says so in its empty state, which has room for a
     * sentence.
     */
    @property({ type: Number })
    count: number | null = null;

    /** Singular noun for the count; pluralised with a trailing `s`. */
    @property({ type: String, attribute: 'count-noun' })
    countNoun = 'item';

    /** Irregular plural, where a trailing `s` will not do. */
    @property({ type: String, attribute: 'count-plural' })
    countPlural = '';

    /** Sort choices. Empty (the default) renders no sort control. */
    @property({ attribute: false })
    sortOptions: SortOption[] = [];

    @property({ type: String, attribute: 'sort-field' })
    sortField = '';

    @property({ type: String, attribute: 'sort-direction' })
    sortDirection: SortDirection = 'asc';

    /**
     * The search term the page is filtered by, if any. The header says
     * so, because the *scope* of the header search box is the view —
     * so a page showing three of forty albums has to admit why.
     */
    @property({ type: String, attribute: 'search-term' })
    searchTerm = '';

    /** Shown while the view is refetching, next to the heading. */
    @property({ type: Boolean })
    busy = false;

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
                flex-shrink: 0;
            }

            .page-header {
                display: flex;
                align-items: center;
                gap: 12px;
                padding: 12px 16px 10px;
                border-bottom: 1px solid var(--yj-border-subtle, #333);
            }

            h1 {
                margin: 0;
                font-size: var(--yj-text-xl, 18px);
                font-weight: 600;
                color: var(--yj-text-primary, #fff);
                white-space: nowrap;
            }

            .count {
                font-size: var(--yj-text-sm, 12px);
                color: var(--yj-text-tertiary, #888);
                white-space: nowrap;
            }

            .scope {
                font-size: var(--yj-text-sm, 12px);
                color: var(--yj-text-secondary, #b3b3b3);
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
                min-width: 0;
            }

            /* Actions sit at the right; everything before them is the
               page's identity and stays left. */
            .spacer {
                flex: 1 1 auto;
                min-width: 0;
            }

            .sort {
                display: inline-flex;
                align-items: center;
                gap: 6px;
                font-size: var(--yj-text-sm, 12px);
                color: var(--yj-text-secondary, #b3b3b3);
                flex-shrink: 0;
            }

            .sort select {
                font: inherit;
                color: inherit;
                background: var(--yj-bg-surface, #212529);
                border: 1px solid var(--yj-border, #444);
                border-radius: 4px;
                padding: 3px 6px;
                cursor: pointer;
            }

            .sort-dir {
                display: inline-flex;
                align-items: center;
                justify-content: center;
                background: transparent;
                border: 1px solid transparent;
                border-radius: 4px;
                color: inherit;
                cursor: pointer;
                padding: 3px 5px;
            }

            .sort-dir:hover {
                background: var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
            }

            .sort-dir:focus-visible,
            .sort select:focus-visible {
                outline: 2px solid var(--yj-accent, #ffd43b);
                outline-offset: -1px;
            }

            .spinner {
                width: 12px;
                height: 12px;
                border: 2px solid var(--yj-border, #444);
                border-top-color: var(--yj-accent, #ffd43b);
                border-radius: 50%;
                animation: spin 0.8s linear infinite;
            }

            @keyframes spin {
                to {
                    transform: rotate(360deg);
                }
            }

            @media (prefers-reduced-motion: reduce) {
                .spinner {
                    animation-duration: 3s;
                }
            }

            ::slotted(*) {
                flex-shrink: 0;
            }
        `,
    ];

    override render() {
        return html`
            <header class="page-header" part="header">
                ${this.heading === ''
                    ? nothing
                    : html`<h1 data-testid="page-heading">
                          ${this.heading}
                      </h1>`}
                ${this.busy
                    ? html`<span
                          class="spinner"
                          role="status"
                          aria-label="Refreshing"
                      ></span>`
                    : nothing}
                ${this.renderCount()}
                <div class="spacer"></div>
                ${this.renderScope()} ${this.renderSort()}
                <slot name="actions"></slot>
            </header>
        `;
    }

    private renderCount() {
        if (this.count === null) return nothing;

        const plural =
            this.countPlural !== ''
                ? this.countPlural
                : `${this.countNoun}s`;

        const noun = this.count === 1 ? this.countNoun : plural;

        return html`<span class="count" data-testid="page-count"
            >${this.count.toLocaleString()} ${noun}</span
        >`;
    }

    private renderScope() {
        if (this.searchTerm === '') return nothing;

        const what =
            this.heading === ''
                ? 'results'
                : this.heading.toLowerCase();

        return html`<span class="scope" data-testid="page-search-scope"
            >Showing ${what} matching &ldquo;${this.searchTerm}&rdquo;</span
        >`;
    }

    private renderSort() {
        if (this.sortOptions.length === 0) return nothing;

        const ascending = this.sortDirection === 'asc';

        // One option is not a choice: Artists can only be sorted by
        // name, because `library.Artist` carries no counts to sort by.
        // A select with a single option is a control that does
        // nothing, so it says what the order is and lets the direction
        // button do the work.
        if (this.sortOptions.length === 1) {
            return html`
                <div class="sort">
                    <span>Sort: ${this.sortOptions[0]?.label}</span>
                    ${this.renderDirectionButton(ascending)}
                </div>
            `;
        }

        return html`
            <div class="sort">
                <label>
                    Sort:
                    <select
                        data-testid="page-sort"
                        .value=${this.sortField}
                        @change=${this.onSortFieldChange}
                    >
                        ${this.sortOptions.map(
                            (o) => html`
                                <option
                                    value=${o.id}
                                    ?selected=${o.id === this.sortField}
                                >
                                    ${o.label}
                                </option>
                            `,
                        )}
                    </select>
                </label>
                ${this.renderDirectionButton(ascending)}
            </div>
        `;
    }

    private renderDirectionButton(ascending: boolean) {
        return html`
            <button
                class="sort-dir"
                type="button"
                data-testid="page-sort-direction"
                aria-label=${ascending ? 'Sort ascending' : 'Sort descending'}
                aria-pressed=${ascending ? 'false' : 'true'}
                title=${ascending ? 'Ascending' : 'Descending'}
                @click=${this.onDirectionClick}
            >
                <wa-icon
                    name=${ascending
                        ? 'arrow-up-short-wide'
                        : 'arrow-down-wide-short'}
                ></wa-icon>
            </button>
        `;
    }

    private onSortFieldChange = (e: Event) => {
        const field = (e.target as HTMLSelectElement).value;

        this.emitSort(field, this.sortDirection);
    };

    private onDirectionClick = () => {
        this.emitSort(
            this.sortField,
            this.sortDirection === 'asc' ? 'desc' : 'asc',
        );
    };

    /** The host owns the sort state and persists it; this only asks. */
    private emitSort(field: string, direction: SortDirection) {
        this.dispatchEvent(
            new CustomEvent('sort-change', {
                detail: { field, direction },
                bubbles: true,
                composed: true,
            }),
        );
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'page-header': PageHeader;
    }
}

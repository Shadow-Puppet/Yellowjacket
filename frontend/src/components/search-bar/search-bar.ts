import { LitElement, html, css, nothing } from 'lit';
import { customElement, query } from 'lit/decorators.js';
import { SearchController } from '@store/controllers/search-controller';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';

/**
 * The header search box.
 *
 * It is **view-scoped** (plan 007, Decisions 2) and used to look
 * global: placeheld "Search…", sitting in the app header, and silently
 * doing nothing on the pages that do not read the term. It now names
 * what it searches — "Search albums" — so "No playlists match your
 * search" arrives having already said it was only ever looking at
 * playlists (H-10).
 *
 * It also used to *hide* on those pages, which moved the library
 * filter and the job indicator every time the user navigated. It keeps
 * its slot now and is disabled, with the reason in its title and its
 * placeholder: either the page has a search of its own (Explore), or
 * there is nothing on it to search.
 */
@customElement('search-bar')
export class SearchBar extends LitElement {
    private searchCtrl = new SearchController(this);
    private searchDebounceTimer: ReturnType<typeof setTimeout> | null = null;

    @query('input')
    private inputEl!: HTMLInputElement;

    static override styles = [designTokens, css`
        :host {
            display: flex;
            align-items: center;
        }

        /* Kept, because the first-run wizard hides the whole header;
           navigation no longer sets it. */
        :host([hidden]) {
            display: none;
        }

        .search-container.disabled {
            opacity: 0.55;
        }

        .search-container.disabled:focus-within {
            border-color: var(--yj-border-subtle, #555);
        }

        input:disabled {
            cursor: not-allowed;
        }

        .search-container {
            display: flex;
            align-items: center;
            background: var(--yj-bg-surface, #212529);
            border: 1px solid var(--yj-border-subtle, #555);
            border-radius: 6px;
            padding: 0 10px;
            gap: 8px;
            height: 32px;
            min-width: 200px;
            max-width: 360px;
            width: 100%;
            transition: border-color 0.15s ease;
        }

        /* The 200px floor is a desktop floor. On a phone the header is
           the whole width there is, and a min-width in a flex row is a
           *hard* one -- it does not shrink, so the header stayed 580px
           wide inside a 360px viewport and the shell scrolled
           sideways. Measured at 360px: 580 -> 360. */
        @media (max-width: 599px) {
            :host {
                min-width: 0;
            }

            .search-container {
                min-width: 0;
            }
        }

        .search-container:focus-within {
            border-color: var(--yj-accent, #ffd43b);
        }

        .search-icon {
            color: var(--yj-text-tertiary, #888);
            font-size: var(--yj-icon-sm);
            flex-shrink: 0;
        }

        input {
            flex: 1;
            background: none;
            border: none;
            outline: none;
            color: var(--yj-text-primary, #fff);
            font-size: var(--yj-text-md);
            font-family: inherit;
            min-width: 0;
        }

        input::placeholder {
            color: var(--yj-text-tertiary, #888);
        }

        .clear-button {
            display: flex;
            align-items: center;
            justify-content: center;
            background: none;
            border: none;
            color: var(--yj-text-tertiary, #888);
            cursor: pointer;
            padding: 0;
            font-size: var(--yj-text-sm);
            flex-shrink: 0;
        }

        .clear-button:hover {
            color: var(--yj-text-primary, #fff);
        }
    `];

    /**
     * Focus the search input and select all text.
     * Called by the global Ctrl+F handler.
     */
    focusInput(): void {
        if (!this.inputEl) return;

        this.inputEl.focus();
        this.inputEl.select();
    }

    private handleInput = (e: Event) => {
        const input = e.target as HTMLInputElement;
        const value = input.value;

        if (this.searchDebounceTimer !== null) {
            clearTimeout(this.searchDebounceTimer);
            this.searchDebounceTimer = null;
        }

        if (value === '') {
            // Instant clear for responsive feedback.
            this.searchCtrl.term = '';
        } else {
            this.searchDebounceTimer = setTimeout(() => {
                this.searchDebounceTimer = null;
                this.searchCtrl.term = value;
            }, 150);
        }
    };

    private handleClear = () => {
        this.searchCtrl.term = '';

        if (this.inputEl) {
            this.inputEl.value = '';
            this.inputEl.focus();
        }
    };

    private handleKeydown = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
            this.searchCtrl.term = '';

            if (this.inputEl) {
                this.inputEl.value = '';
                this.inputEl.blur();
            }
        }
    };

    override render() {
        const term = this.searchCtrl.term;
        const scope = this.searchCtrl.scopeLabel;
        const disabledReason = this.searchCtrl.disabledReason;
        const enabled = disabledReason === '';

        const placeholder = enabled
            ? `Search ${scope}\u2026`
            : disabledReason;

        return html`
            <div class="search-container ${enabled ? '' : 'disabled'}">
                <wa-icon
                    class="search-icon"
                    name="magnifying-glass"
                ></wa-icon>
                <input
                    type="text"
                    data-testid="search-input"
                    aria-label=${enabled
                        ? `Search ${scope}`
                        : disabledReason}
                    title=${enabled ? '' : disabledReason}
                    placeholder=${placeholder}
                    ?disabled=${!enabled}
                    .value=${enabled ? term : ''}
                    @input=${this.handleInput}
                    @keydown=${this.handleKeydown}
                />
                ${enabled && term
                    ? html`
                          <button
                              class="clear-button"
                              aria-label="Clear search"
                              title="Clear search"
                              @click=${this.handleClear}
                          >
                              <wa-icon
                                  name="xmark"
                              ></wa-icon>
                          </button>
                      `
                    : nothing}
            </div>
        `;
    }
}

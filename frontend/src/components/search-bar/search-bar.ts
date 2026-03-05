import { LitElement, html, css, nothing } from 'lit';
import { customElement, query } from 'lit/decorators.js';
import { SearchController } from '@store/controllers/search-controller';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

/**
 * Global search bar displayed in the top bar.
 * Hides itself when the active view is not searchable.
 */
@customElement('search-bar')
export class SearchBar extends LitElement {
    private searchCtrl = new SearchController(this);
    private searchDebounceTimer: ReturnType<typeof setTimeout> | null = null;

    @query('input')
    private inputEl!: HTMLInputElement;

    static override styles = css`
        :host {
            display: flex;
            align-items: center;
        }

        :host([hidden]) {
            display: none;
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

        .search-container:focus-within {
            border-color: var(--yj-accent, #ffd43b);
        }

        .search-icon {
            color: var(--yj-text-tertiary, #888);
            font-size: 14px;
            flex-shrink: 0;
        }

        input {
            flex: 1;
            background: none;
            border: none;
            outline: none;
            color: var(--yj-text-primary, #fff);
            font-size: 13px;
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
            font-size: 12px;
            flex-shrink: 0;
        }

        .clear-button:hover {
            color: var(--yj-text-primary, #fff);
        }
    `;

    override updated() {
        // Toggle the hidden attribute based on whether the
        // current view supports searching.
        if (this.searchCtrl.isSearchableView) {
            this.removeAttribute('hidden');
        } else {
            this.setAttribute('hidden', '');
        }
    }

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

        return html`
            <div class="search-container">
                <wa-icon
                    class="search-icon"
                    name="magnifying-glass"
                ></wa-icon>
                <input
                    type="text"
                    placeholder="Search..."
                    .value=${term}
                    @input=${this.handleInput}
                    @keydown=${this.handleKeydown}
                />
                ${term
                    ? html`
                          <button
                              class="clear-button"
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

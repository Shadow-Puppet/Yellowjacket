import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { LibraryController } from '@store/controllers/library-controller';
import { libraryStore } from '@store/library-store';
import type { library } from '@go/models';
import { designTokens } from '../../styles/tokens.css';

/**
 * Compact dropdown in the top bar for filtering all
 * browse views to a specific library.  "All Libraries"
 * (value = null) shows the unified merged view.
 */
@customElement('library-filter')
export class LibraryFilter extends LitElement {
    private libraryCtrl = new LibraryController(this);

    @state()
    private libraries: library.Info[] = [];

    static override styles = [designTokens, css`
        :host {
            display: flex;
            align-items: center;
        }

        select {
            height: 32px;
            padding: 0 8px;
            border-radius: 6px;
            border: 1px solid
                var(--yj-border-subtle, #555);
            background: var(--yj-bg-surface, #212529);
            color: var(--yj-text-primary, #fff);
            font-size: var(--yj-text-md);
            font-family: inherit;
            cursor: pointer;
            outline: none;
            min-width: 120px;
            max-width: 200px;
        }

        select:focus {
            border-color: var(--yj-accent, #ffd43b);
        }

        option {
            background: var(--yj-bg-surface, #212529);
            color: var(--yj-text-primary, #fff);
        }
    `];

    private unsubscribeStore?: () => void;

    override connectedCallback() {
        super.connectedCallback();
        this.loadLibraries();

        // The LibraryController already wires a reactive subscription to
        // libraryStore so the host re-renders on change, but it doesn't
        // refresh our cached `this.libraries` array.  Subscribe directly
        // and re-fetch so LibraryAdded / Removed / Renamed events flow
        // into the dropdown without a restart.
        this.unsubscribeStore = libraryStore.subscribe(() => {
            this.loadLibraries();
        });
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.unsubscribeStore?.();
    }

    private async loadLibraries() {
        try {
            this.libraries =
                await this.libraryCtrl.getLibraries();
        } catch (error) {
            console.error(
                'Error loading libraries:',
                error,
            );
        }
    }

    private handleChange = (e: Event) => {
        const select = e.target as HTMLSelectElement;
        const value = select.value;
        const id = value === ''
            ? null
            : Number(value);

        this.libraryCtrl.setSelectedLibrary(id);
    };

    override render() {
        const selected =
            this.libraryCtrl.selectedLibraryId;
        const selectValue =
            selected === null ? '' : String(selected);

        return html`
            <select
                .value=${selectValue}
                @change=${this.handleChange}
                aria-label="Library filter"
            >
                <option value="">All Libraries</option>
                ${this.libraries.map(
                    (lib) => html`
                        <option value=${lib.id}>
                            ${lib.name}
                        </option>
                    `,
                )}
            </select>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'library-filter': LibraryFilter;
    }
}

/**
 * Choosing a directory, where the platform will not do it for us.
 *
 * Wails' file dialog can select directories on every desktop platform.
 * On Android it returns an error, because the Storage Access Framework
 * yields tree URIs rather than filesystem paths — and a path is what
 * this app's whole library model is keyed on. So the app browses the
 * filesystem itself, through `ListDirectories`, which it can do because
 * it holds all-files access.
 *
 * Deliberately not a general file browser: it lists directories only,
 * because the thing being chosen is a library root.
 *
 * The shape is `confirm-dialog`'s — a promise-returning `choose()` on a
 * `wa-dialog`, so callers `await` a path or `null` and there is no
 * second dialog pattern in the codebase.
 */
import { LitElement, css, html, nothing } from 'lit';
import { customElement, query, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';

import {
    DefaultBrowseRoot,
    ListDirectories,
} from '@go/frontendutil/frontendutil.js';

import { designTokens } from '../../styles/tokens.css';
import { srOnly } from '../../styles/sr-only.css';
import { describeError } from '../../utils/describe-error';
import { nameDialogsIn } from '../../utils/name-dialog';

interface Entry {
    name: string;
    path: string;
}

@customElement('folder-picker')
export class FolderPicker extends LitElement {
    @query('wa-dialog') private dialog?: HTMLElement & { open: boolean };

    @state() private path = '';
    @state() private parent = '';
    @state() private entries: Entry[] = [];
    @state() private loading = false;
    @state() private errorMessage = '';
    @state() private isOpen = false;

    private settle: ((path: string | null) => void) | null = null;

    static override styles = [
        designTokens,
        srOnly,
        css`
            :host {
                display: contents;
            }

            wa-dialog::part(dialog) {
                background: var(--yj-bg-surface, #212529);
                color: var(--yj-text-primary, #fff);
            }

            .current {
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: var(--yj-text-sm, 0.8125rem);
                margin-bottom: 8px;
                overflow-wrap: anywhere;
            }

            ul {
                border: 1px solid var(--yj-border, #495057);
                border-radius: 4px;
                list-style: none;
                margin: 0;
                max-height: 45vh;
                min-height: 8em;
                overflow-y: auto;
                padding: 0;
            }

            li button {
                align-items: center;
                background: none;
                border: 0;
                color: inherit;
                cursor: pointer;
                display: flex;
                font: inherit;
                gap: 8px;
                padding: 10px 12px;
                text-align: left;
                width: 100%;
            }

            li button:hover,
            li button:focus-visible {
                background: var(--yj-bg-elevated, #343a40);
            }

            .empty {
                color: var(--yj-text-secondary, #b3b3b3);
                padding: 12px;
            }

            .error {
                color: var(--yj-error-text, #ff8787);
                padding: 8px 0;
            }

            .actions {
                display: flex;
                gap: 8px;
                justify-content: flex-end;
                margin-top: 12px;
            }

            .actions button {
                background: var(--yj-bg-elevated, #343a40);
                border: 1px solid var(--yj-border, #495057);
                border-radius: 4px;
                color: inherit;
                cursor: pointer;
                font: inherit;
                padding: 6px 14px;
            }

            .actions button.primary {
                background: var(--yj-accent, #ffd43b);
                border-color: var(--yj-accent, #ffd43b);
                color: var(--yj-accent-fg, #000);
            }
        `,
    ];

    /** Browse. Resolves with an absolute path, or null if cancelled. */
    async choose(startAt?: string): Promise<string | null> {
        this.settle?.(null);
        this.settle = null;

        let start = startAt ?? '';

        if (!start) {
            try {
                start = await DefaultBrowseRoot();
            } catch {
                start = '';
            }
        }

        this.isOpen = true;
        await this.load(start);
        await this.updateComplete;

        if (this.dialog) this.dialog.open = true;

        return new Promise<string | null>((resolve) => {
            this.settle = resolve;
        });
    }

    private async load(path: string): Promise<void> {
        this.loading = true;
        this.errorMessage = '';

        try {
            const listing = await ListDirectories(path);

            this.path = listing.path;
            this.parent = listing.parent;
            this.entries = listing.entries ?? [];
        } catch (err) {
            // A directory that cannot be read is not a failed picker —
            // stay where we are and say so, or the user is stranded
            // with an empty dialog and no way back.
            this.errorMessage = describeError(
                err,
                'That folder could not be opened.',
            );
            console.error('folder-picker: listing failed:', err);
        } finally {
            this.loading = false;
        }
    }

    private close(path: string | null): void {
        const settle = this.settle;

        this.settle = null;

        if (this.dialog) this.dialog.open = false;
        this.isOpen = false;
        settle?.(path);
    }

    override updated(): void {
        nameDialogsIn(this.shadowRoot);
    }

    override render() {
        if (!this.isOpen) return nothing;

        return html`
            <wa-dialog
                label="Choose a folder"
                data-testid="folder-picker"
                @wa-hide=${() => this.close(null)}
            >
                <p class="current" data-testid="folder-picker-path">
                    ${this.path || '\u2026'}
                </p>

                ${this.errorMessage
                    ? html`<p class="error" role="alert">
                          ${this.errorMessage}
                      </p>`
                    : nothing}

                <ul data-testid="folder-picker-list">
                    ${this.parent
                        ? html`<li>
                              <button
                                  @click=${() => void this.load(this.parent)}
                                  data-testid="folder-picker-up"
                              >
                                  <span aria-hidden="true">\u2191</span> Up one
                                  level
                              </button>
                          </li>`
                        : nothing}
                    ${this.entries.map(
                        (entry) => html`
                            <li>
                                <button
                                    @click=${() => void this.load(entry.path)}
                                >
                                    <span aria-hidden="true">\u{1F4C1}</span>
                                    ${entry.name}
                                </button>
                            </li>
                        `,
                    )}
                    ${!this.loading && this.entries.length === 0
                        ? html`<li class="empty">No folders here.</li>`
                        : nothing}
                </ul>

                <!--
                  The live region is in the DOM before it has anything to
                  say, because most screen readers announce a change to a
                  region they are already watching and ignore one that
                  appears with its content already in it.
                -->
                <p class="sr-only" role="status" aria-live="polite">
                    ${this.loading
                        ? 'Loading folders'
                        : `${this.entries.length} folders in ${this.path}`}
                </p>

                <div class="actions">
                    <button @click=${() => this.close(null)}>Cancel</button>
                    <button
                        class="primary"
                        ?disabled=${!this.path}
                        @click=${() => this.close(this.path)}
                        data-testid="folder-picker-select"
                    >
                        Use this folder
                    </button>
                </div>
            </wa-dialog>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'folder-picker': FolderPicker;
    }
}

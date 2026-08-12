/**
 * The key story, told once.
 *
 * The single-key global bindings are staying (plan 007, Decision 1),
 * and until now Settings was the only place they were written down —
 * three of the four categories of them, at that, so the autotag keys
 * were written down nowhere. `?` opens this from anywhere the app owns
 * the keyboard.
 *
 * It reads `services/shortcut-meta` for the labels and the *store* for
 * the keys, so a rebound key shows its new binding rather than the
 * default it shipped with.
 *
 * It is a `wa-dialog` for the reason every dialog in this app is: the
 * focus trap, Escape and focus restore come with it. Being help rather
 * than a question, it is a dialog in a host template rather than a
 * `confirmAction()` call.
 */
import { LitElement, css, html, nothing } from 'lit';
import { customElement, query, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';

import { designTokens } from '../../styles/tokens.css';
import {
    SHORTCUT_CATEGORIES,
    SHORTCUT_META,
} from '../../services/shortcut-meta';
import { shortcutsStore } from '@store/shortcuts-store';

/** How a key string reads to a person: `Ctrl+F` is two keys. */
function keyParts(key: string): string[] {
    return key.split('+');
}

/** Where a binding applies, in the user's terms. A panel binding that
 *  does not say so reads as broken everywhere else. */
function scopeNote(scope: string): string {
    if (scope === 'panel:track-list') return 'in the track list';
    if (scope === 'panel:autotag') return 'on the Autotag page';

    return '';
}

@customElement('shortcuts-overlay')
export class ShortcutsOverlay extends LitElement {
    @query('wa-dialog') private dialog?: HTMLElement & { open: boolean };

    @state() private isOpen = false;

    private unsubscribe: (() => void) | null = null;

    static override styles = [
        designTokens,
        css`
            :host {
                display: contents;
            }

            wa-dialog::part(dialog) {
                background: var(--yj-bg-surface, #212529);
                color: var(--yj-text-primary, #fff);
            }

            .category {
                margin-bottom: 1.25em;
            }

            .category:last-of-type {
                margin-bottom: 0;
            }

            h3 {
                margin: 0 0 0.5em;
                font-size: var(--yj-text-sm, 0.8125rem);
                font-weight: 700;
                text-transform: uppercase;
                letter-spacing: 0.06em;
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .row {
                display: flex;
                align-items: baseline;
                justify-content: space-between;
                gap: 1em;
                padding: 0.25em 0;
            }

            .label {
                font-size: var(--yj-text-sm, 0.8125rem);
            }

            .scope {
                color: var(--yj-text-tertiary, #888);
            }

            .keys {
                display: flex;
                gap: 0.25em;
                flex-shrink: 0;
            }

            kbd {
                background: var(--yj-bg-elevated, #343a40);
                border: 1px solid var(--yj-border, #495057);
                border-radius: 4px;
                padding: 0.1em 0.45em;
                font-family: inherit;
                font-size: var(--yj-text-xs, 0.6875rem);
            }

            .footnote {
                margin: 1em 0 0;
                font-size: var(--yj-text-xs, 0.6875rem);
                color: var(--yj-text-tertiary, #888);
            }
        `,
    ];

    override connectedCallback(): void {
        super.connectedCallback();
        document.addEventListener('shortcut:app-shortcuts', this.open);
        // The keys come from the backend, so the first open can arrive
        // before they do.
        this.unsubscribe = shortcutsStore.subscribe(() =>
            this.requestUpdate(),
        );
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        document.removeEventListener('shortcut:app-shortcuts', this.open);
        this.unsubscribe?.();
        this.unsubscribe = null;
    }

    /**
     * `?` opens; Escape closes, as it does for every dialog here.
     *
     * It is deliberately not a toggle. A dialog owns the whole keyboard
     * while it is up — `focusedControlOwnsKey` yields every unmodified
     * key to anything inside one — so a second `?` never reaches the
     * service, and a toggle would be a promise the shortcut layer
     * cannot keep. Written after watching an e2e spec assert it and
     * fail.
     */
    private open = (): void => {
        if (this.isOpen) return;

        this.isOpen = true;
        void this.updateComplete.then(() => {
            if (this.dialog) this.dialog.open = true;
        });
    };

    private close(): void {
        if (this.dialog) this.dialog.open = false;
        this.isOpen = false;
    }

    override render() {
        if (!this.isOpen) return nothing;

        const bindings = shortcutsStore.getBindings();

        return html`
            <wa-dialog
                label="Keyboard Shortcuts"
                data-testid="shortcuts-overlay"
                @wa-hide=${() => this.close()}
            >
                ${SHORTCUT_CATEGORIES.map((category) => {
                    const rows = Object.entries(SHORTCUT_META).filter(
                        ([, meta]) => meta.category === category,
                    );

                    if (rows.length === 0) return nothing;

                    // A note every row in the category repeats is a
                    // note about the category. "— on the Autotag page"
                    // seven times is noise; once is the heading.
                    const first = scopeNote(rows[0]![1].scope);
                    const uniform = rows.every(
                        ([, meta]) => scopeNote(meta.scope) === first,
                    );
                    // …and a note the *heading* already says is not a
                    // note either: "Autotag — on the Autotag page".
                    const shared =
                        uniform &&
                        !first
                            .toLowerCase()
                            .includes(category.toLowerCase())
                            ? first
                            : '';

                    return html`
                        <div class="category">
                            <h3>
                                ${category}
                                ${shared
                                    ? html`<span class="scope"
                                          >— ${shared}</span
                                      >`
                                    : nothing}
                            </h3>
                            ${rows.map(([action, meta]) => {
                                const key =
                                    bindings.get(action) ?? meta.defaultKey;
                                const note =
                                    shared || uniform
                                        ? ''
                                        : scopeNote(meta.scope);

                                return html`
                                    <div class="row">
                                        <span class="label">
                                            ${meta.label}
                                            ${note
                                                ? html`<span class="scope"
                                                      >— ${note}</span
                                                  >`
                                                : nothing}
                                        </span>
                                        <span class="keys">
                                            ${keyParts(key).map(
                                                (part) =>
                                                    html`<kbd>${part}</kbd>`,
                                            )}
                                        </span>
                                    </div>
                                `;
                            })}
                        </div>
                    `;
                })}
                <p class="footnote">
                    Every one of these can be rebound in Settings →
                    Keyboard Shortcuts.
                </p>
            </wa-dialog>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'shortcuts-overlay': ShortcutsOverlay;
    }
}

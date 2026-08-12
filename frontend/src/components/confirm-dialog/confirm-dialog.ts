/**
 * "Are you sure?", once.
 *
 * Three destructive actions had no confirmation at all: deleting
 * playlists (including a multi-select loop that deleted N of them),
 * removing a durable download request, and removing a download client
 * with its stored credentials (errors.M6, M7, m4).
 *
 * The shape is the one the codebase already uses twice — say what will
 * happen *before* asking (`config-page`'s removal impact), then ask —
 * so this is that shape written once rather than a third pattern. It is
 * a `wa-dialog`, which brings the focus trap, Escape and focus restore
 * that the hand-rolled overlays do not have.
 */
import { LitElement, css, html, nothing } from 'lit';
import { customElement, query, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';

import { designTokens } from '../../styles/tokens.css';

export interface ConfirmRequest {
    title: string;
    /** What is about to happen, in the user's terms. */
    message: string;
    /** The consequence, when it is worth spelling out separately. */
    impact?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    /** Styles the confirm button as destructive. */
    danger?: boolean;
}

@customElement('confirm-dialog')
export class ConfirmDialog extends LitElement {
    @query('wa-dialog') private dialog?: HTMLElement & { open: boolean };

    @state() private request: ConfirmRequest | null = null;

    private settle: ((ok: boolean) => void) | null = null;

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

            .impact {
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: var(--yj-text-sm, 0.8125rem);
                margin-top: 8px;
            }

            .actions {
                display: flex;
                gap: 8px;
                justify-content: flex-end;
            }

            button {
                background: var(--yj-bg-elevated, #343a40);
                border: 1px solid var(--yj-border, #495057);
                border-radius: 4px;
                color: inherit;
                cursor: pointer;
                font: inherit;
                padding: 6px 14px;
            }

            button.danger {
                background: var(--yj-danger, #e03131);
                border-color: var(--yj-danger, #e03131);
                color: #fff;
            }
        `,
    ];

    /** Ask. Resolves true if the user went ahead. */
    ask(request: ConfirmRequest): Promise<boolean> {
        this.close(false);
        this.request = request;

        return new Promise<boolean>((resolve) => {
            this.settle = resolve;
            void this.updateComplete.then(() => {
                if (this.dialog) this.dialog.open = true;
            });
        });
    }

    private close(ok: boolean): void {
        const settle = this.settle;

        this.settle = null;

        if (this.dialog) this.dialog.open = false;
        this.request = null;
        settle?.(ok);
    }

    override render() {
        const request = this.request;

        if (!request) return nothing;

        return html`
            <wa-dialog
                label=${request.title}
                data-testid="confirm-dialog"
                @wa-hide=${() => this.close(false)}
            >
                <p>${request.message}</p>
                ${request.impact
                    ? html`<p class="impact">${request.impact}</p>`
                    : nothing}
                <div class="actions" slot="footer">
                    <button
                        type="button"
                        data-testid="confirm-cancel"
                        @click=${() => this.close(false)}
                    >
                        ${request.cancelLabel ?? 'Cancel'}
                    </button>
                    <button
                        type="button"
                        class=${request.danger ? 'danger' : ''}
                        data-testid="confirm-accept"
                        @click=${() => this.close(true)}
                    >
                        ${request.confirmLabel ?? 'Continue'}
                    </button>
                </div>
            </wa-dialog>
        `;
    }
}

/** The one instance, created on first use and reused after. */
let host: ConfirmDialog | null = null;

/**
 * Ask the user to confirm something destructive.
 *
 * Call sites do not mount anything: the dialog attaches itself to the
 * document the first time it is needed, which keeps a confirmation from
 * being skipped because the host forgot to render it.
 */
export function confirmAction(request: ConfirmRequest): Promise<boolean> {
    if (!host) {
        host = document.createElement('confirm-dialog') as ConfirmDialog;
        document.body.append(host);
    }

    return host.ask(request);
}

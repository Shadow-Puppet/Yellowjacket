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
import { nameDialogsIn } from '../../utils/name-dialog';

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

    /**
     * Which question is on screen.
     *
     * This is a singleton reused for every confirmation in the app,
     * and `wa-dialog` reports its close *asynchronously* — `open =
     * false` starts an animation and `wa-hide` arrives after it. So a
     * hide belonging to a question that has already been answered can
     * land after the *next* question has opened, and cancel it: the
     * user is asked something, the dialog vanishes on its own, and the
     * call site is told they said no.
     *
     * The counter is what tells one question from the next. Every
     * close bumps it, and the `wa-hide` handler carries the id its
     * template was rendered with.
     */
    private askSeq = 0;

    /** Ask. Resolves true if the user went ahead. */
    ask(request: ConfirmRequest): Promise<boolean> {
        this.close(false);

        const id = ++this.askSeq;

        this.request = request;

        return new Promise<boolean>((resolve) => {
            this.settle = resolve;
            void this.updateComplete.then(() => {
                // A third question could have arrived while this one
                // was waiting for its own render.
                if (this.askSeq === id && this.dialog) this.dialog.open = true;
            });
        });
    }

    /**
     * Settle the current question, if `id` still names it.
     *
     * The button handlers pass nothing and always mean the question on
     * screen; only `wa-hide` carries an id, because only `wa-hide` can
     * arrive late.
     */
    private close(ok: boolean, id = this.askSeq): void {
        if (id !== this.askSeq) return;

        const settle = this.settle;

        this.settle = null;
        this.askSeq++;

        if (this.dialog) this.dialog.open = false;
        this.request = null;
        settle?.(ok);
    }

    /**
     * Web Awesome renders `label` into a heading it never points the
     * `<dialog>` at, so the dialog has no accessible name until
     * something sets one. See `utils/name-dialog.ts`.
     */
    override updated() {
        nameDialogsIn(this.shadowRoot);
    }

    override render() {
        const request = this.request;

        if (!request) return nothing;

        // Captured at render time, so the handler answers the question
        // it was drawn for and not whichever one is up when it fires.
        const id = this.askSeq;

        return html`
            <wa-dialog
                label=${request.title}
                data-testid="confirm-dialog"
                @wa-hide=${() => this.close(false, id)}
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

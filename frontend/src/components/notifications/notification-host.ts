/**
 * Where the app speaks: the three levels that are not tied to a panel.
 *
 * Mounted once, in `index.html`, next to the first-run wizard — it has
 * to outlive every view, since the failure it reports is usually the
 * reason the user is about to navigate somewhere else.
 *
 * `inline` is the fourth level and is not here: it belongs to the
 * region that failed (`<inline-notice region="…">`).
 */
import { LitElement, css, html, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';

import { notificationStore } from '@store/notification-store';
import { designTokens } from '../../styles/tokens.css';

import { noticeStyles, renderNotice } from './notice';

@customElement('notification-host')
export class NotificationHost extends LitElement {
    @state() private version = 0;

    private unsubscribe?: () => void;

    static override styles = [
        designTokens,
        noticeStyles,
        css`
            :host {
                display: contents;
            }

            /* Below the header, not above the player bar: the bottom
               band belongs to the player, whose own inline notice
               floats there and grows upward by however many lines it
               needs — at 800×600 a two-line one reached straight into
               a bottom-anchored stack. */
            .stack {
                position: fixed;
                right: 16px;
                top: calc(4em + 12px);
                z-index: 60;
                display: flex;
                flex-direction: column;
                gap: 8px;
                width: min(30em, calc(100vw - 32px));
                pointer-events: none;
            }

            .stack .notice {
                pointer-events: auto;
            }

            wa-dialog::part(dialog) {
                background: var(--yj-bg-surface, #212529);
                color: var(--yj-text-primary, #fff);
            }

            .blocking-actions {
                display: flex;
                gap: 8px;
                justify-content: flex-end;
                margin-top: 16px;
            }

            .blocking-actions button {
                background: var(--yj-bg-elevated, #343a40);
                border: 1px solid var(--yj-border, #495057);
                border-radius: 4px;
                color: inherit;
                cursor: pointer;
                font: inherit;
                padding: 6px 14px;
            }

            .blocking-actions .primary {
                background: var(--yj-accent, #ffd43b);
                border-color: var(--yj-accent, #ffd43b);
                color: #000;
            }

            .blocking-detail {
                color: var(--yj-text-tertiary, #868e96);
                font-size: var(--yj-text-sm, 0.8125rem);
                margin-top: 12px;
                overflow-wrap: anywhere;
                user-select: text;
            }
        `,
    ];

    override connectedCallback(): void {
        super.connectedCallback();
        // Not a cached view: this element is mounted once and never
        // navigated away from, so connection really is its lifetime.
        this.unsubscribe = notificationStore.subscribe(() => {
            this.version += 1;
        });
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.unsubscribe?.();
    }

    private handlers = {
        dismiss: (id: number) => notificationStore.dismiss(id),
        act: (id: number) => notificationStore.runAction(id),
    };

    /** Blocking is one modal at a time, and it must be acknowledged. */
    private renderBlocking() {
        const notification = notificationStore.currentBlocking();

        if (!notification) return nothing;

        return html`
            <wa-dialog
                open
                label=${notification.title ?? 'Something needs your attention'}
                data-testid="notification-blocking"
                @wa-hide=${() => notificationStore.dismiss(notification.id)}
            >
                <p>${notification.text}</p>
                ${notification.detail
                    ? html`<p class="blocking-detail">${notification.detail}</p>`
                    : nothing}
                <div class="blocking-actions" slot="footer">
                    ${notification.action
                        ? html`
                              <button
                                  type="button"
                                  data-testid="notification-action"
                                  @click=${() =>
                                      notificationStore.runAction(notification.id)}
                              >
                                  ${notification.action.label}
                              </button>
                          `
                        : nothing}
                    <button
                        type="button"
                        class="primary"
                        @click=${() => notificationStore.dismiss(notification.id)}
                    >
                        OK
                    </button>
                </div>
            </wa-dialog>
        `;
    }

    override render() {
        const stacked = [
            ...notificationStore.byLevel('persistent'),
            ...notificationStore.byLevel('transient'),
        ];

        return html`
            ${this.renderBlocking()}
            ${stacked.length === 0
                ? nothing
                : html`
                      <div
                          class="stack"
                          role="status"
                          aria-live="polite"
                          data-testid="notification-stack"
                      >
                          ${stacked.map((n) => renderNotice(n, this.handlers))}
                      </div>
                  `}
        `;
    }
}

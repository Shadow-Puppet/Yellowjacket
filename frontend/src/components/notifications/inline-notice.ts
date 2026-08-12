/**
 * The fourth level: a failure rendered in the region that failed.
 *
 * A track that will not play, a search that did not answer, an index
 * whose status could not be read — these belong to one panel, and a
 * toast for them would be noise. The region names itself
 * (`<inline-notice region="player">`) and anything that raises
 * `notificationStore.inline('player', …)` lands here.
 *
 * `floating` is for a host with no room in its own layout: the bottom
 * bar is a fixed 4em grid row, so a message laid out inside it squeezes
 * the transport out of its own footer.
 */
import { LitElement, css, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import { notificationStore } from '@store/notification-store';
import { designTokens } from '../../styles/tokens.css';

import { noticeStyles, renderNotice } from './notice';

@customElement('inline-notice')
export class InlineNotice extends LitElement {
    /** Which region's messages to render. */
    @property({ type: String }) region = '';

    /** Render above the host instead of in its flow. */
    @property({ type: Boolean }) floating = false;

    /** Overrides `data-testid` on the notice, for hosts that already
     *  have a named message element in their specs. */
    @property({ type: String, attribute: 'testid' }) testid = '';

    @state() private version = 0;

    private unsubscribe?: () => void;

    static override styles = [
        designTokens,
        noticeStyles,
        css`
            :host {
                display: block;
            }

            /* Left-anchored and no wider than half the window: the
               app-level stack sits in the same band on the right, and
               a full-width strip overlapped it. */
            :host([floating]) {
                position: absolute;
                bottom: calc(100% + 4px);
                left: 16px;
                right: auto;
                max-width: min(36em, 48vw);
                z-index: 20;
            }

            :host(:not([floating])) .notice {
                margin: 8px 0;
            }
        `,
    ];

    override connectedCallback(): void {
        super.connectedCallback();
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

    override render() {
        const items = notificationStore.forRegion(this.region);

        if (items.length === 0) return nothing;

        return html`
            <div role="status" aria-live="polite">
                ${items.map((n) =>
                    renderNotice(n, this.handlers, this.testid || undefined),
                )}
            </div>
        `;
    }
}

/**
 * The one presentation, shared by the four levels.
 *
 * A notice looks the same wherever it appears — icon, sentence, an
 * optional action, a dismiss — and the level only decides *where* it is
 * rendered and how long it stays. Keeping the markup in one place is
 * what stops the next level from inventing a fifth look.
 */
import { css, html, nothing, type TemplateResult } from 'lit';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

import type { Notification, NotificationTone } from '@store/notification-store';

const TONE_ICONS: Record<NotificationTone, string> = {
    error: 'circle-exclamation',
    warning: 'triangle-exclamation',
    info: 'circle-info',
    success: 'circle-check',
};

export const noticeStyles = css`
    .notice {
        display: flex;
        align-items: flex-start;
        gap: 8px;
        padding: 8px 10px;
        border-radius: 4px;
        border: 1px solid var(--yj-border, #495057);
        border-left: 3px solid var(--yj-warning, #e0a800);
        background: var(--yj-bg-elevated, #343a40);
        color: var(--yj-text-primary, #fff);
        font-size: var(--yj-text-sm, 0.8125rem);
        box-shadow: 0 2px 8px rgb(0 0 0 / 40%);
        text-align: left;
    }

    .notice[data-tone='error'] {
        border-left-color: var(--yj-danger, #e03131);
    }

    .notice[data-tone='info'] {
        border-left-color: var(--yj-accent, #ffd43b);
    }

    .notice[data-tone='success'] {
        border-left-color: var(--yj-success, #37b24d);
    }

    .notice > wa-icon {
        flex-shrink: 0;
        margin-top: 1px;
        color: var(--yj-warning, #e0a800);
    }

    .notice[data-tone='error'] > wa-icon {
        color: var(--yj-danger, #e03131);
    }

    .notice-body {
        flex: 1;
        min-width: 0;
    }

    .notice-title {
        font-weight: 600;
        margin-bottom: 2px;
    }

    .notice-action {
        background: none;
        border: 1px solid var(--yj-border, #495057);
        border-radius: 3px;
        color: inherit;
        cursor: pointer;
        font: inherit;
        margin-top: 6px;
        padding: 2px 8px;
    }

    .notice-action:hover {
        background: var(--yj-bg-surface, #212529);
    }

    .notice-dismiss {
        background: none;
        border: none;
        color: inherit;
        cursor: pointer;
        font: inherit;
        line-height: 1;
        padding: 0 4px;
    }
`;

export interface NoticeHandlers {
    dismiss: (id: number) => void;
    act: (id: number) => void;
}

/**
 * One notice, in the shape every level shares.
 *
 * `testid` exists for hosts whose specs already name their message
 * element — the player bar's, which predates this surface.
 */
export function renderNotice(
    notification: Notification,
    handlers: NoticeHandlers,
    testid = 'notification',
): TemplateResult {
    return html`
        <div
            class="notice"
            data-tone=${notification.tone}
            data-testid=${testid}
            data-level=${notification.level}
            role=${notification.tone === 'error' ? 'alert' : 'status'}
        >
            <wa-icon name=${TONE_ICONS[notification.tone]}></wa-icon>
            <div class="notice-body">
                ${notification.title
                    ? html`<div class="notice-title">${notification.title}</div>`
                    : nothing}
                <div class="notice-text">${notification.text}</div>
                ${notification.action
                    ? html`
                          <button
                              type="button"
                              class="notice-action"
                              data-testid="notification-action"
                              @click=${() => handlers.act(notification.id)}
                          >
                              ${notification.action.label}
                          </button>
                      `
                    : nothing}
            </div>
            <button
                type="button"
                class="notice-dismiss"
                aria-label="Dismiss message"
                @click=${() => handlers.dismiss(notification.id)}
            >
                ×
            </button>
        </div>
    `;
}

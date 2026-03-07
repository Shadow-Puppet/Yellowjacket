import { LitElement, html, css } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { buildKeyString } from '../../services/keyboard-shortcut-service';

@customElement('shortcut-capture')
export class ShortcutCapture extends LitElement {
    @property() action = '';
    @property() currentKey = '';
    @property() defaultKey = '';

    @state() private recording = false;

    static override styles = css`
        :host {
            display: inline-block;
        }
        button {
            font-family: inherit;
            font-size: var(--yj-text-sm, 13px);
            padding: 4px 12px;
            border-radius: 4px;
            border: 1px solid var(--yj-border, #555);
            background: var(--yj-bg-input, #333);
            color: var(--yj-text-primary, #eee);
            cursor: pointer;
            min-width: 80px;
            text-align: center;
            transition:
                border-color 0.15s,
                background 0.15s;
        }
        button:hover {
            border-color: var(--yj-accent, #ffd43b);
        }
        button.recording {
            border-color: var(--yj-accent, #ffd43b);
            background: var(--yj-bg-active, #444);
            animation: pulse 1.2s ease-in-out infinite;
        }
        button.not-set {
            color: var(--yj-text-tertiary, #888);
            font-style: italic;
        }
        @keyframes pulse {
            0%,
            100% {
                opacity: 1;
            }
            50% {
                opacity: 0.7;
            }
        }
        .reset-btn {
            font-size: var(--yj-text-xs, 11px);
            padding: 2px 6px;
            margin-left: 4px;
            border: none;
            background: transparent;
            color: var(--yj-text-tertiary, #888);
            cursor: pointer;
            min-width: auto;
            opacity: 0;
            transition: opacity 0.15s;
        }
        :host(:hover) .reset-btn {
            opacity: 1;
        }
        .reset-btn:hover {
            color: var(--yj-accent, #ffd43b);
        }
    `;

    private handleClick = () => {
        this.recording = true;
        // Focus self so keydown events arrive
        this.shadowRoot?.querySelector('button')?.focus();
    };

    private handleKeydown = (e: KeyboardEvent) => {
        if (!this.recording) return;

        e.preventDefault();
        e.stopPropagation();

        const keyStr = buildKeyString(e);
        if (!keyStr) return; // bare modifier press — keep recording

        if (keyStr === 'Escape') {
            this.recording = false;
            return;
        }

        this.recording = false;

        this.dispatchEvent(
            new CustomEvent('shortcut-change', {
                detail: { action: this.action, key: keyStr },
                bubbles: true,
                composed: true,
            }),
        );
    };

    private handleBlur = () => {
        // Cancel recording if focus leaves
        if (this.recording) {
            this.recording = false;
        }
    };

    private handleReset = (e: Event) => {
        e.stopPropagation();
        if (this.defaultKey && this.currentKey !== this.defaultKey) {
            this.dispatchEvent(
                new CustomEvent('shortcut-change', {
                    detail: {
                        action: this.action,
                        key: this.defaultKey,
                    },
                    bubbles: true,
                    composed: true,
                }),
            );
        }
    };

    override render() {
        const showReset =
            this.defaultKey && this.currentKey !== this.defaultKey;
        return html`
            <button
                class=${this.recording
                    ? 'recording'
                    : this.currentKey
                      ? ''
                      : 'not-set'}
                @click=${this.handleClick}
                @keydown=${this.handleKeydown}
                @blur=${this.handleBlur}
            >
                ${this.recording
                    ? 'Press a key combo\u2026'
                    : this.currentKey || 'Not set'}
            </button>
            ${showReset
                ? html`
                      <button
                          class="reset-btn"
                          @click=${this.handleReset}
                          title="Reset to default (${this.defaultKey})"
                      >
                          \u21BA
                      </button>
                  `
                : ''}
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'shortcut-capture': ShortcutCapture;
    }
}

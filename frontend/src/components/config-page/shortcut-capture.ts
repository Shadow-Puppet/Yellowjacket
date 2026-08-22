import { LitElement, html, css } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { buildKeyString } from '../../services/keyboard-shortcut-service';

@customElement('shortcut-capture')
export class ShortcutCapture extends LitElement {
    @property() action = '';
    @property() currentKey = '';
    @property() defaultKey = '';

    /**
     * What the binding does, for the name.
     *
     * The button's text is the *key* — so the shortcuts list rendered
     * three buttons called "S", two called "Down" and one called "?",
     * beside a visible label that named none of them (it is a sibling,
     * in another shadow root, and nothing associated the two). The
     * label is what a control is for; the key is its value.
     */
    @property() label = '';

    @state() private recording = false;

    static override styles = css`
        :host {
            display: inline-block;
        }
        /* 80x25, twenty-six of them -- the most numerous control on
           the Settings page after the column lists (#186). The floor
           is a height here and nothing else: the width was already
           past it, and the type stays where it is so a shortcut still
           reads as a key rather than as a button. */
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
            min-height: 44px;
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
        /* Reset renders only for a rebound shortcut, so a sweep of a
           freshly-installed app never sees it -- it is not in #186's
           tables for that reason, and it is a touch target the moment
           anybody uses the feature. It also has no background, so the
           padding out to 44px is invisible. */
        .reset-btn {
            font-size: var(--yj-text-xs, 11px);
            padding: 2px 6px;
            margin-left: 4px;
            border: none;
            background: transparent;
            color: var(--yj-text-tertiary, #888);
            cursor: pointer;
            min-width: 44px;
            min-height: 44px;
            opacity: 0;
            transition: opacity 0.15s;
        }
        :host(:hover) .reset-btn {
            opacity: 1;
        }
        .reset-btn:hover {
            color: var(--yj-accent-text, #ffd43b);
        }
        /*
         * Reset is the only way to put a rebound shortcut back, so where
         * the device has no hover it is always visible rather than an
         * invisible button holding its hit area. The inverse of #68's
         * rule, which applies where the hover control is redundant.
         */
        @media not all and (hover: hover) {
            .reset-btn {
                opacity: 1;
            }
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
                aria-label=${this.label
                    ? `${this.label} shortcut: ${this.currentKey || 'not set'}`
                    : ''}
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
                          aria-label="Reset ${this.label ||
                              this.action} to ${this.defaultKey}"
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

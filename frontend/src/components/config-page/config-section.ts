import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

/**
 * A visual grouping wrapper for config fields.
 * Renders a collapsible heading with optional description,
 * and a slot for fields. Starts collapsed by default.
 */
@customElement('config-section')
export class ConfigSection extends LitElement {
    static override styles = css`
        :host {
            display: block;
            margin-bottom: 1.5em;
        }

        .section {
            background: var(--yj-bg-surface, #212529);
            border: 1px solid var(--yj-border-subtle, #333);
            border-radius: 6px;
        }

        .header {
            display: flex;
            align-items: flex-start;
            gap: 0.5em;
            padding: 1.25em;
            cursor: pointer;
            user-select: none;
        }

        .header:hover {
            background: var(--yj-bg-elevated, #343a40);
            border-radius: 6px;
        }

        .chevron {
            flex-shrink: 0;
            width: 16px;
            height: 16px;
            margin-top: 0.15em;
            transition: transform 150ms ease;
            color: var(--yj-text-tertiary, #888);
        }

        .chevron.open {
            transform: rotate(90deg);
        }

        .header-text {
            flex: 1;
            min-width: 0;
        }

        h3 {
            margin: 0;
            font-size: 1em;
            font-weight: 700;
            color: var(--yj-text-primary, #fff);
        }

        .description {
            font-size: 0.8em;
            color: var(--yj-text-tertiary, #888);
            margin: 0.25em 0 0;
        }

        .body {
            padding: 0 1.25em 1.25em;
        }

        .fields {
            display: flex;
            flex-direction: column;
        }
    `;

    @property({ type: String })
    heading = '';

    @property({ type: String })
    description = '';

    @property({ type: Boolean })
    open = false;

    @state()
    private expanded = false;

    override connectedCallback(): void {
        super.connectedCallback();
        this.expanded = this.open;
    }

    private toggle = (): void => {
        this.expanded = !this.expanded;
    };

    override render() {
        return html`
            <div class="section">
                <div
                    class="header"
                    @click=${this.toggle}
                >
                    <svg
                        class="chevron ${this.expanded ? 'open' : ''}"
                        viewBox="0 0 16 16"
                        fill="currentColor"
                    >
                        <path
                            d="M6 3l5 5-5 5z"
                        />
                    </svg>
                    <div class="header-text">
                        <h3>${this.heading}</h3>
                        ${this.description
                            ? html`<p class="description">
                                  ${this.description}
                              </p>`
                            : nothing}
                    </div>
                </div>
                ${this.expanded
                    ? html`
                          <div class="body">
                              <div class="fields">
                                  <slot></slot>
                              </div>
                          </div>
                      `
                    : nothing}
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'config-section': ConfigSection;
    }
}

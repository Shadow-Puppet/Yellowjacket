import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';

/**
 * A visual grouping wrapper for config fields.
 * Renders a heading, optional description, and a slot for fields.
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
            padding: 1.25em;
        }

        h3 {
            margin: 0 0 0.25em;
            font-size: 1em;
            font-weight: 700;
            color: var(--yj-text-primary, #fff);
        }

        .description {
            font-size: 0.8em;
            color: var(--yj-text-tertiary, #888);
            margin: 0 0 1em;
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

    override render() {
        return html`
            <div class="section">
                <h3>${this.heading}</h3>
                ${this.description
                    ? html`<p class="description">
                          ${this.description}
                      </p>`
                    : nothing}
                <div class="fields">
                    <slot></slot>
                </div>
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'config-section': ConfigSection;
    }
}

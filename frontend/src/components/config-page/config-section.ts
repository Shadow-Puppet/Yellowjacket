import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

/**
 * A visual grouping wrapper for config fields.
 * Renders a collapsible heading with optional description,
 * and a slot for fields. Starts collapsed by default.
 *
 * The header is a real `<button aria-expanded aria-controls>`, which
 * it was not: it was a bare `<div @click>` with no tabindex and no
 * role, and every section defaults to collapsed — so every setting in
 * the app sat behind a control that could not be tabbed to (a11y.1).
 * `explore-artist-details` has had the correct pattern in five places
 * the whole time.
 *
 * The body renders unconditionally and is toggled with `hidden`,
 * rather than being added and removed. `aria-controls` has to name an
 * element that exists, and the slot's light-DOM children exist either
 * way — a conditional `<slot>` only stops projecting them.
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
            width: 100%;
            padding: 1.25em;
            cursor: pointer;
            user-select: none;
            background: none;
            border: none;
            font: inherit;
            color: inherit;
            text-align: left;
        }

        .header:hover {
            background: var(--yj-bg-elevated, #343a40);
            border-radius: 6px;
        }

        .header:focus-visible {
            outline: 2px solid var(--yj-accent, #ffc107);
            outline-offset: -2px;
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

        .body[hidden] {
            display: none;
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
                <button
                    type="button"
                    class="header"
                    aria-expanded=${this.expanded ? 'true' : 'false'}
                    aria-controls="section-body"
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
                </button>
                <div class="body" id="section-body" ?hidden=${!this.expanded}>
                    <div class="fields">
                        <slot></slot>
                    </div>
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

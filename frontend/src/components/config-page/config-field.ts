import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';

/**
 * The id every field's control carries, so its `<label>` can name it.
 *
 * The label was a *sibling* of the control with no `for`, which names
 * nothing — so every select, toggle and text field in Settings computed
 * an empty accessible name. Measured on the expanded Settings page:
 * **24 of 93 controls unnamed**, six of them here and the rest the
 * column toggles in `config-page`. `a11y.6` is not wrong about this;
 * it says in the same line that it scanned every `<button>`, and none
 * of these is one.
 *
 * A fixed id is safe, and only because each `config-field` is its own
 * shadow root — the whole page renders a dozen elements with
 * `id="control"` and each `for` resolves within its own root. It is
 * preferred over `aria-label` for what it buys beyond the name: a real
 * label association also makes the label text a click target for the
 * control, which is behaviour, not annotation.
 */
const CONTROL_ID = 'control';

/**
 * Schema describing a single config field.
 *
 * The `type` controls which input widget is rendered:
 *  - `text`      – plain text input
 *  - `select`    – dropdown with `options`
 *  - `color`     – colour picker swatch
 *  - `directory` – readonly text + browse button
 *  - `number`    – numeric input
 *  - `toggle`    – on/off switch
 */
export interface ConfigFieldSchema {
    key: string;
    label: string;
    description?: string;
    type:
        | 'text'
        | 'select'
        | 'color'
        | 'directory'
        | 'number'
        | 'toggle';
    options?: { value: string; label: string }[];
    disabled?: boolean;
}

export interface ConfigFieldChangeEvent {
    key: string;
    value: unknown;
}

@customElement('config-field')
export class ConfigField extends LitElement {
    static override styles = css`
        :host {
            display: block;
            margin-bottom: 1em;
            color-scheme: inherit;
        }

        .field {
            display: flex;
            flex-direction: column;
            gap: 0.35em;
        }

        label {
            font-weight: 600;
            font-size: 0.85em;
            color: var(--yj-text-primary, #fff);
        }

        .description {
            font-size: 0.75em;
            color: var(--yj-text-tertiary, #888);
            margin: 0;
        }

        .input-row {
            display: flex;
            align-items: center;
            gap: 0.5em;
        }

        /* Every control here meets the app's 44px touch floor (#186).

           This is the shape every row in Settings uses, so it is the
           one rule that covers the most controls -- and it is the
           *cheapest* place to reach the floor, because there is no
           overflow fit on this page. The page header's had one (#69),
           which is why that pass had to grow padding and hand the
           width back with a negative margin; here the control is a
           block in a column and a taller box costs nothing but the
           height it takes.

           Measured on the reference device before this: the select
           335x30, the text and number inputs the same, the browse
           button 30 tall, the colour swatch 33x33 and the toggle
           **34x19**. */
        input[type='text'],
        input[type='number'] {
            background: var(--yj-bg-elevated, #343a40);
            color: var(--yj-text-primary, #fff);
            border: 1px solid var(--yj-border-subtle, #333);
            border-radius: 4px;
            padding: 0.4em 0.6em;
            font-size: 0.85em;
            font-family: inherit;
            min-width: 0;
            min-block-size: 44px;
            flex: 1;
        }

        input:focus {
            outline: 1px solid var(--yj-accent, #ffd43b);
            border-color: var(--yj-accent, #ffd43b);
        }

        input[readonly] {
            opacity: 0.8;
            cursor: default;
        }

        select {
            background: var(--yj-bg-elevated, #343a40);
            color: var(--yj-text-primary, #fff);
            border: 1px solid var(--yj-border-subtle, #333);
            border-radius: 4px;
            padding: 0.4em 0.6em;
            font-size: 0.85em;
            font-family: inherit;
            cursor: pointer;
            min-block-size: 44px;
            flex: 1;
        }

        select:focus {
            outline: 1px solid var(--yj-accent, #ffd43b);
            border-color: var(--yj-accent, #ffd43b);
        }

        select option {
            background: var(--yj-bg-elevated, #343a40);
            color: var(--yj-text-primary, #fff);
        }

        button {
            background: var(--yj-info, #4263eb);
            color: var(--yj-info-fg, #fff);
            border: none;
            border-radius: 4px;
            padding: 0.4em 0.8em;
            font-size: 0.85em;
            cursor: pointer;
            white-space: nowrap;
            min-block-size: 44px;
        }

        button:hover {
            background: var(--yj-info-hover, #3b5bdb);
        }

        button:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }

        /* Colour picker */
        .color-wrapper {
            display: flex;
            align-items: center;
            gap: 0.75em;
        }

        input[type='color'] {
            /* border-box, or the 2px border makes this 48 and the
               assertion below reads as passing by four pixels of
               border rather than by the rule. */
            box-sizing: border-box;
            width: 44px;
            height: 44px;
            border: 2px solid var(--yj-border, #444);
            border-radius: 4px;
            padding: 0;
            cursor: pointer;
            background: none;
        }

        input[type='color']::-webkit-color-swatch-wrapper {
            padding: 2px;
        }

        input[type='color']::-webkit-color-swatch {
            border: none;
            border-radius: 2px;
        }

        .color-hex {
            font-family: monospace;
            font-size: 0.85em;
            color: var(--yj-text-secondary, #b3b3b3);
        }

        /* Toggle */
        .toggle-row {
            display: flex;
            align-items: center;
            justify-content: space-between;
            min-block-size: 44px;
        }

        /* The toggle is the one control here whose target and paint
           must differ, and it is also the one no sweep can see.

           Its <input> is opacity: 0; width: 0; height: 0, so a
           walk of every input on the page skips it as a zero-sized
           node -- the thing a finger actually hits is this <label>,
           which measured **34x19**. That is smaller than anything in
           #186's original table and it is absent from it for exactly
           that reason.

           A 44px pill is not what a switch should look like, so the
           box is 44px and the paint is not: .toggle-slider is a
           2.5em x 1.4em child centred in it rather than an absolute
           fill. The negative inline margins hand the extra width back
           to the layout, so the pill stays flush with the right edge
           of the inputs in the rows above it -- the header pass's
           shape, used here for alignment rather than for a fit. */
        .toggle-switch {
            display: grid;
            place-items: center;
            inline-size: 44px;
            block-size: 44px;
            margin-inline: calc((2.5em - 44px) / 2);
        }

        .toggle-switch input {
            opacity: 0;
            width: 0;
            height: 0;
        }

        .toggle-slider {
            position: relative;
            cursor: pointer;
            inline-size: 2.5em;
            block-size: 1.4em;
            background: var(--yj-bg-overlay, #495057);
            border-radius: 1em;
            transition: background 0.2s;
        }

        .toggle-slider::before {
            content: '';
            position: absolute;
            height: 1em;
            width: 1em;
            left: 0.2em;
            bottom: 0.2em;
            background: white;
            border-radius: 50%;
            transition: transform 0.2s;
        }

        .toggle-switch input:checked + .toggle-slider {
            background: var(--yj-accent, #ffd43b);
        }

        .toggle-switch input:checked + .toggle-slider::before {
            transform: translateX(1.1em);
        }
    `;

    @property({ attribute: false })
    schema!: ConfigFieldSchema;

    @property({ attribute: false })
    value: unknown = '';

    override render() {
        if (!this.schema) return nothing;

        return html`
            <div class="field">
                ${this.schema.type === 'toggle'
                    ? this.renderToggle()
                    : html`
                          <label for=${CONTROL_ID}>
                              ${this.schema.label}
                          </label>
                          ${this.renderInput()}
                      `}
                ${this.schema.description
                    ? html`<p class="description">
                          ${this.schema.description}
                      </p>`
                    : nothing}
            </div>
        `;
    }

    private renderInput() {
        switch (this.schema.type) {
            case 'text':
                return this.renderText();
            case 'number':
                return this.renderNumber();
            case 'select':
                return this.renderSelect();
            case 'color':
                return this.renderColor();
            case 'directory':
                return this.renderDirectory();
            default:
                return html`<p>Unsupported field type</p>`;
        }
    }

    private renderText() {
        return html`
            <input
                id=${CONTROL_ID}
                type="text"
                .value=${String(this.value ?? '')}
                ?disabled=${this.schema.disabled}
                @change=${this.onTextChange}
            />
        `;
    }

    private renderNumber() {
        return html`
            <input
                id=${CONTROL_ID}
                type="number"
                .value=${String(this.value ?? '')}
                ?disabled=${this.schema.disabled}
                @change=${this.onTextChange}
            />
        `;
    }

    private renderSelect() {
        const current = String(this.value ?? '');

        return html`
            <select
                id=${CONTROL_ID}
                ?disabled=${this.schema.disabled}
                @change=${this.onSelectChange}
            >
                ${(this.schema.options ?? []).map(
                    (opt) => html`
                        <option
                            value=${opt.value}
                            ?selected=${opt.value === current}
                        >
                            ${opt.label}
                        </option>
                    `,
                )}
            </select>
        `;
    }

    private renderColor() {
        const hex = String(this.value ?? '#ffffff');

        return html`
            <div class="color-wrapper">
                <input
                    id=${CONTROL_ID}
                    type="color"
                    .value=${hex}
                    ?disabled=${this.schema.disabled}
                    @input=${this.onColorInput}
                />
                <span class="color-hex">${hex}</span>
            </div>
        `;
    }

    private renderDirectory() {
        return html`
            <div class="input-row">
                <input
                    id=${CONTROL_ID}
                    type="text"
                    .value=${String(this.value ?? '')}
                    readonly
                />
                <button
                    aria-label="Browse for ${this.schema.label}"
                    ?disabled=${this.schema.disabled}
                    @click=${this.onBrowseClick}
                >
                    Browse
                </button>
            </div>
        `;
    }

    private renderToggle() {
        const checked = Boolean(this.value);

        return html`
            <div class="toggle-row">
                <label for=${CONTROL_ID}>${this.schema.label}</label>
                <label class="toggle-switch">
                    <input
                        id=${CONTROL_ID}
                        type="checkbox"
                        ?checked=${checked}
                        ?disabled=${this.schema.disabled}
                        @change=${this.onToggleChange}
                    />
                    <span class="toggle-slider"></span>
                </label>
            </div>
        `;
    }

    // ===================================================================
    // EVENT DISPATCHERS
    // ===================================================================

    private emitChange(value: unknown): void {
        this.dispatchEvent(
            new CustomEvent<ConfigFieldChangeEvent>(
                'config-change',
                {
                    detail: {
                        key: this.schema.key,
                        value,
                    },
                    bubbles: true,
                    composed: true,
                },
            ),
        );
    }

    private onTextChange = (e: Event) => {
        const input = e.target as HTMLInputElement;
        this.emitChange(input.value);
    };

    private onSelectChange = (e: Event) => {
        const select = e.target as HTMLSelectElement;
        this.emitChange(select.value);
    };

    private onColorInput = (e: Event) => {
        const input = e.target as HTMLInputElement;
        this.value = input.value;
        this.emitChange(input.value);
    };

    private onBrowseClick = () => {
        this.dispatchEvent(
            new CustomEvent('config-browse', {
                detail: { key: this.schema.key },
                bubbles: true,
                composed: true,
            }),
        );
    };

    private onToggleChange = (e: Event) => {
        const input = e.target as HTMLInputElement;
        this.emitChange(input.checked);
    };
}

declare global {
    interface HTMLElementTagNameMap {
        'config-field': ConfigField;
    }
}

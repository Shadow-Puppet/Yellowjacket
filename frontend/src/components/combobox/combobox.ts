import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';

/**
 * `<yj-combobox>` — Typeable dropdown with autocomplete filtering and
 * keyboard navigation.  Accepts a flat `options` string array, filters as
 * the user types, and emits `combobox-change` when a value is selected.
 *
 * Key implementation detail: option `<li>` elements use `@mousedown` with
 * `e.preventDefault()` so that the input's `blur` event does not close the
 * dropdown before the click registers.
 *
 * `a11y.14`: the roles were right and the wiring between them was
 * missing, so arrowing through the list moved a visual highlight and
 * announced nothing — confirmed against the browser's own computation
 * (`Accessibility.getFullAXTree` reported no `activedescendant` and no
 * `controls` on any of the five comboboxes on the page). Three things
 * carry it now: ids on the listbox and every option, `aria-controls`,
 * and `aria-activedescendant` naming the highlighted option.
 *
 * `aria-selected` used to mean "highlighted", which is the one thing it
 * does not mean. It is the *chosen* value now; the highlight is what
 * `aria-activedescendant` points at, which is the distinction the whole
 * pattern rests on.
 *
 * Unlike `config-section`'s disclosure, this `aria-controls` IDREF is
 * allowed to dangle while the popup is closed: the listbox genuinely
 * does not exist then, and `aria-expanded="false"` says so. A
 * disclosure's body exists either way, which is why that one renders
 * unconditionally and hides with `hidden`.
 */
@customElement('yj-combobox')
export class YjCombobox extends LitElement {
    // ── Public reactive properties ──────────────────────────────────

    /** Full list of selectable options. */
    @property({ type: Array })
    options: string[] = [];

    /** Currently selected value (reflects to attribute for CSS hooks). */
    @property({ type: String, reflect: true })
    value = '';

    /** Placeholder text shown when the input is empty. */
    @property({ type: String })
    placeholder = '';

    /** Disables input and dropdown interaction. */
    @property({ type: Boolean })
    disabled = false;

    // ── Internal state ──────────────────────────────────────────────

    /** Text currently in the input — drives filtering. */
    @state()
    private filterText = '';

    /** Whether the dropdown is visible. */
    @state()
    private open = false;

    /** Index into `filteredOptions` for keyboard highlight (-1 = none). */
    @state()
    private highlightedIndex = -1;

    /**
     * Per-instance id prefix for the IDREFs below.
     *
     * The ids only have to be unique within this shadow root — an IDREF
     * does not cross one — but two comboboxes render side by side in a
     * smart-playlist rule row, so a counter costs nothing and keeps the
     * DOM readable when one of these is being debugged.
     */
    private readonly uid = `yj-combobox-${(YjCombobox.instances += 1)}`;

    private static instances = 0;

    private optionId(i: number): string {
        return `${this.uid}-opt-${i}`;
    }

    // ── Computed ────────────────────────────────────────────────────

    /** Options that match the current filterText (case-insensitive substring). */
    private get filteredOptions(): string[] {
        const opts = this.options ?? [];
        if (!this.filterText) return opts;
        const needle = this.filterText.toLowerCase();
        return opts.filter((o) => o.toLowerCase().includes(needle));
    }

    // ── Styles ──────────────────────────────────────────────────────

    static override styles = [
        designTokens,
        css`
            :host {
                display: inline-block;
                width: 100%;
                height: 100%;
            }

            .combobox-wrapper {
                position: relative;
                display: inline-block;
                width: 100%;
                height: 100%;
            }

            input {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                color: var(--yj-text-primary, #fff);
                border: 1px solid var(--yj-border-subtle, rgba(255, 255, 255, 0.1));
                border-radius: 4px;
                padding: 4px 8px;
                font-size: var(--yj-text-md);
                font-family: inherit;
                width: 100%;
                height: 100%;
                box-sizing: border-box;
            }

            input:focus {
                outline: none;
                border-color: var(--yj-accent, #ffd43b);
            }

            input:disabled {
                opacity: 0.4;
                cursor: not-allowed;
            }

            .dropdown {
                position: absolute;
                top: 100%;
                left: 0;
                right: 0;
                z-index: 10;
                max-height: 200px;
                overflow-y: auto;
                background: var(--yj-bg-surface, #282828);
                border: 1px solid var(--yj-border-subtle, rgba(255, 255, 255, 0.1));
                border-top: none;
                border-radius: 0 0 4px 4px;
                margin: 0;
                padding: 0;
                list-style: none;
            }

            .dropdown li {
                padding: 4px 8px;
                cursor: pointer;
                color: var(--yj-text-primary, #fff);
                font-size: var(--yj-text-md);
            }

            .dropdown li:hover,
            .dropdown li.highlighted {
                background: var(--yj-bg-hover, rgba(255, 255, 255, 0.12));
            }
        `,
    ];

    // ── Lifecycle ───────────────────────────────────────────────────

    override connectedCallback() {
        super.connectedCallback();
        // Initialise filterText from the external value so an existing
        // selection is visible immediately.
        this.filterText = this.value;
    }

    override updated(changed: Map<string, unknown>) {
        super.updated(changed);

        // Sync filterText when the parent sets `value` programmatically
        // (e.g. when pre-populating the editor with saved rules).
        if (changed.has('value') && !this.open) {
            this.filterText = this.value;
        }

        // Scroll the highlighted option into view.
        if (changed.has('highlightedIndex') && this.highlightedIndex >= 0) {
            const items = this.shadowRoot?.querySelectorAll('.dropdown li');
            items?.[this.highlightedIndex]?.scrollIntoView({
                block: 'nearest',
            });
        }
    }

    // ── Event handlers ──────────────────────────────────────────────

    private handleInput(e: Event) {
        const input = e.target as HTMLInputElement;
        this.filterText = input.value;
        this.open = true;
        this.highlightedIndex = -1;
    }

    private handleFocus() {
        // Clear filter so the full option list is visible on focus.
        this.filterText = '';
        this.open = true;
        this.highlightedIndex = -1;
    }

    private handleBlur() {
        // Use rAF as a safety net — mousedown on an option calls
        // preventDefault() which should keep focus, but some browsers are
        // inconsistent.  The tiny delay lets any pending mousedown handler
        // fire first.
        requestAnimationFrame(() => {
            this.open = false;

            // Commit free-form text: if the user typed something that
            // isn't in the option list, accept it as the value anyway.
            const typed = this.filterText.trim();
            if (typed && typed !== this.value) {
                this.value = typed;
                this.filterText = typed;
                this.dispatchEvent(
                    new CustomEvent('combobox-change', {
                        bubbles: true,
                        composed: true,
                        detail: { value: typed },
                    }),
                );
            } else {
                // Restore display text to the confirmed value.
                this.filterText = this.value;
            }
        });
    }

    private handleKeydown(e: KeyboardEvent) {
        const opts = this.filteredOptions;

        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                if (!this.open) {
                    this.open = true;
                    this.highlightedIndex = 0;
                } else if (opts.length > 0) {
                    this.highlightedIndex =
                        (this.highlightedIndex + 1) % opts.length;
                }
                break;

            case 'ArrowUp':
                e.preventDefault();
                if (opts.length > 0 && this.open) {
                    this.highlightedIndex =
                        (this.highlightedIndex - 1 + opts.length) %
                        opts.length;
                }
                break;

            case 'Enter':
                if (
                    this.open &&
                    this.highlightedIndex >= 0 &&
                    this.highlightedIndex < opts.length
                ) {
                    e.preventDefault();
                    this.selectOption(opts[this.highlightedIndex]!);
                } else if (this.filterText.trim()) {
                    // Commit free-form text on Enter even without a
                    // highlighted option.
                    e.preventDefault();
                    this.selectOption(this.filterText.trim());
                }
                break;

            case 'Escape':
                e.preventDefault();
                this.open = false;
                this.filterText = this.value;
                break;

            case 'Tab':
                // Close dropdown but let default Tab navigation proceed.
                this.open = false;
                this.filterText = this.value;
                break;

            default:
                break;
        }
    }

    // ── Selection ───────────────────────────────────────────────────

    private selectOption(opt: string) {
        this.value = opt;
        this.filterText = opt;
        this.open = false;
        this.highlightedIndex = -1;

        this.dispatchEvent(
            new CustomEvent('combobox-change', {
                bubbles: true,
                composed: true,
                detail: { value: opt },
            }),
        );
    }

    // ── Render ──────────────────────────────────────────────────────

    override render() {
        const opts = this.filteredOptions;

        return html`
            <div class="combobox-wrapper">
                <input
                    .value=${this.filterText}
                    @input=${this.handleInput}
                    @focus=${this.handleFocus}
                    @blur=${this.handleBlur}
                    @keydown=${this.handleKeydown}
                    ?disabled=${this.disabled}
                    placeholder=${this.placeholder}
                    autocomplete="off"
                    role="combobox"
                    aria-expanded=${this.open}
                    aria-autocomplete="list"
                    aria-controls=${`${this.uid}-listbox`}
                    aria-activedescendant=${this.open && this.highlightedIndex >= 0
                        ? this.optionId(this.highlightedIndex)
                        : nothing}
                />
                ${this.open && opts.length > 0
                    ? html`
                          <ul class="dropdown" role="listbox" id=${`${this.uid}-listbox`}>
                              ${opts.map(
                                  (opt, i) => html`
                                      <li
                                          role="option"
                                          id=${this.optionId(i)}
                                          aria-selected=${opt === this.value}
                                          class=${i === this.highlightedIndex
                                              ? 'highlighted'
                                              : ''}
                                          @mousedown=${(e: Event) => {
                                              e.preventDefault();
                                              this.selectOption(opt);
                                          }}
                                      >
                                          ${opt}
                                      </li>
                                  `,
                              )}
                          </ul>
                      `
                    : nothing}
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'yj-combobox': YjCombobox;
    }
}

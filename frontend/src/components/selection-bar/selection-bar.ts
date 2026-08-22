import { LitElement, html, css, nothing } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';
import { ICON_MORE_ACTIONS } from '@utils/icon-language';

/**
 * What a selection can have done to it, while a finger is holding one.
 *
 * This is the context menu (plan 019, #63). Not a second surface
 * beside it — the same actions, contextualised to whatever is
 * selected, in the shape Android puts them in.
 *
 * #63 asked for a double tap to open the menu instead. That mapping
 * costs the app's primary interaction 250ms on every play, because the
 * first tap of a double tap is indistinguishable from a single tap
 * until the interval expires, and playing a track is ~100ms end to end
 * on the reference device. So there is no double tap: a long press
 * selects, this says what can be done, and the sheet behind "More" is
 * the same `menu-surface` every other menu in the app opens.
 *
 * Four things about it are load-bearing.
 *
 * **It is presentational.** It takes a count and a list of actions and
 * emits `selection-action` / `selection-exit`; it holds no selection
 * and calls no store. The host already owns a `SelectionController`
 * and an action handler, and a bar that reached for either would be a
 * second definition of what "play the selection" means — the fault
 * `utils/library-status.ts` exists to have fixed one feature over.
 *
 * **The bar is only what fits, and "More" is the rest.** A context
 * menu can be nine items because it is a sheet; a bar is one row on a
 * 424px screen. So the host passes the two or three worth a thumb and
 * the overflow opens the menu it already renders, which is what keeps
 * every action reachable at every size — plan 018's promise, and the
 * reason this cannot simply drop the long tail.
 *
 * **Its controls are 44px** (#56, #186), and the count is a live
 * region: the number changes under the user's finger as they tap rows,
 * and nothing else on screen announces it.
 *
 * **It renders nothing at zero.** The mode ends when the last row is
 * deselected — `SelectionController.toggleInMode` is where that is
 * decided — so a bar with a count of none is a state this should never
 * be asked to draw, and drawing it anyway would hide the fact that it
 * has been.
 */
export interface SelectionAction {
    id: string;
    label: string;
    icon: string;
    danger?: boolean;
}

@customElement('selection-bar')
export class SelectionBar extends LitElement {
    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
            }

            .bar {
                display: flex;
                align-items: center;
                gap: 0.25em;
                padding: 0.25em 0.5em;
                background: var(--yj-bg-elevated, #343a40);
                border-top: 1px solid var(--yj-border-subtle, #333);
            }

            .count {
                flex: 1;
                min-width: 0;
                font-size: var(--yj-text-md);
                font-weight: 600;
                color: var(--yj-text-primary, #fff);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            /* 44px, from #56 and #186 -- this is a bar a thumb uses. */
            button {
                display: flex;
                align-items: center;
                justify-content: center;
                min-inline-size: 44px;
                min-block-size: 44px;
                padding: 0 0.5em;
                border: none;
                border-radius: 4px;
                background: none;
                color: var(--yj-text-primary, #fff);
                font-family: inherit;
                font-size: var(--yj-text-md);
                cursor: pointer;
            }

            button:hover {
                background: var(--yj-bg-overlay, #495057);
            }

            button.danger {
                color: var(--yj-error-text, #ff8787);
            }
        `,
    ];

    /** How many items are selected. Zero renders nothing. */
    @property({ type: Number }) count = 0;

    /** The actions worth a thumb. The rest live behind "More". */
    @property({ attribute: false }) actions: SelectionAction[] = [];

    private emit(name: string, detail?: unknown) {
        this.dispatchEvent(
            new CustomEvent(name, { detail, bubbles: true, composed: true }),
        );
    }

    override render() {
        if (this.count <= 0) return nothing;

        const noun = this.count === 1 ? 'track' : 'tracks';

        return html`
            <div class="bar" role="toolbar" aria-label="Selection actions">
                <button
                    aria-label="Leave selection"
                    @click=${() => this.emit('selection-exit')}
                >
                    <wa-icon name="xmark"></wa-icon>
                </button>
                <span class="count" role="status" aria-live="polite">
                    ${this.count.toLocaleString()} ${noun} selected
                </span>
                ${this.actions.map(
                    (action) => html`
                        <button
                            class=${action.danger ? 'danger' : ''}
                            aria-label=${action.label}
                            title=${action.label}
                            @click=${() =>
                                this.emit('selection-action', { id: action.id })}
                        >
                            <wa-icon name=${action.icon}></wa-icon>
                        </button>
                    `,
                )}
                <button
                    aria-label="More actions"
                    @click=${(e: MouseEvent) => {
                        const box = (
                            e.currentTarget as HTMLElement
                        ).getBoundingClientRect();

                        this.emit('selection-more', {
                            x: box.left,
                            y: box.top,
                        });
                    }}
                >
                    <wa-icon name=${ICON_MORE_ACTIONS}></wa-icon>
                </button>
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'selection-bar': SelectionBar;
    }
}

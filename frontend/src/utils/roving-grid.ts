/**
 * A roving tab stop for a grid of cards.
 *
 * Every card in the albums, artists and genres grids is
 * `role="button" tabindex="0"`, which is reachable but unusable: the tab
 * sequence is as long as the library, so getting past the grid means
 * pressing Tab a thousand times and getting *into* it lands on card one
 * with no way to move but Tab.  The convention for a grid is one tab
 * stop that the arrow keys move — which is what this is.
 *
 * The column count is measured from the rendered cards rather than
 * computed from the layout config, because all three grids are
 * virtualized with a centring `justify` and the arithmetic would be a
 * second description of a layout the DOM already knows.
 */
import type { ReactiveController, ReactiveControllerHost } from 'lit';

export interface RovingGridHost extends ReactiveControllerHost {
    shadowRoot: ShadowRoot | null;
}

export interface RovingGridOptions {
    /** CSS selector matching one card, inside the host's shadow root. */
    cardSelector: string;
    /** How many cards there are right now. */
    count: () => number;
    /** Bring an index into view before focusing it — the grids are
     *  virtualized, so a card outside the window does not exist yet. */
    scrollToIndex?: (index: number) => void;
    /** Called when the user activates the focused card. */
    activate?: (index: number) => void;
}

export class RovingGridController implements ReactiveController {
    private host: RovingGridHost;
    private opts: RovingGridOptions;

    /** The card holding the tab stop. */
    private focusedIndex = 0;

    constructor(host: RovingGridHost, opts: RovingGridOptions) {
        this.host = host;
        this.opts = opts;
        host.addController(this);
    }

    hostConnected(): void {}

    /** `tabindex` for the card at `index`. */
    tabIndexFor(index: number): number {
        return index === this.focusedIndex ? 0 : -1;
    }

    /** Remember where the user is, so tabbing back returns there. */
    noteFocus(index: number): void {
        if (index === this.focusedIndex) return;

        this.focusedIndex = index;
        this.host.requestUpdate();
    }

    /** Keydown handler for the scroll container. */
    handleKeydown = (e: KeyboardEvent): void => {
        const last = this.opts.count() - 1;

        if (last < 0) return;

        const columns = this.measureColumns();
        let next = this.focusedIndex;

        switch (e.key) {
            case 'ArrowRight':
                next = Math.min(this.focusedIndex + 1, last);
                break;
            case 'ArrowLeft':
                next = Math.max(this.focusedIndex - 1, 0);
                break;
            case 'ArrowDown':
                next = Math.min(this.focusedIndex + columns, last);
                break;
            case 'ArrowUp':
                next = Math.max(this.focusedIndex - columns, 0);
                break;
            case 'Home':
                next = 0;
                break;
            case 'End':
                next = last;
                break;
            default:
                return;
        }

        e.preventDefault();
        e.stopPropagation();
        this.focus(next);
    };

    /** Move the tab stop, scrolling and focusing the card. */
    focus(index: number): void {
        this.focusedIndex = index;
        this.opts.scrollToIndex?.(index);
        this.host.requestUpdate();

        void this.host.updateComplete.then(() => {
            const cards = this.cards();

            cards
                .find((card) => Number(card.dataset['index']) === index)
                ?.focus();
        });
    }

    private cards(): HTMLElement[] {
        return [
            ...(this.host.shadowRoot?.querySelectorAll<HTMLElement>(
                this.opts.cardSelector,
            ) ?? []),
        ];
    }

    /** Cards sharing a top offset are one row. */
    private measureColumns(): number {
        const cards = this.cards();

        if (cards.length === 0) return 1;

        const top = cards[0]!.offsetTop;
        const inRow = cards.filter((card) => card.offsetTop === top).length;

        return Math.max(1, inRow);
    }
}

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
 *
 * It has to be measured with `getBoundingClientRect()`, though, and
 * not with `offsetTop`: `lit-virtualizer` positions its children with
 * a `transform`, which `offsetTop` does not see, so **every** card in
 * every one of these grids reported `offsetTop === 0`. That made the
 * measured column count the number of rendered cards, which made
 * ArrowDown `min(i + everything, last)` and ArrowUp `max(i - everything,
 * 0)` — the vertical arrows were End and Home, in all three grids,
 * from the day this was written. Reproduced at 700×700 with three real
 * rows of 3/3/2: ArrowDown from card 0 landed on card 7.
 */
import type { ReactiveController, ReactiveControllerHost } from 'lit';

/**
 * How long to keep retrying the focus while a virtualizer catches up.
 *
 * A deadline rather than a frame count because the wait is a scroll and
 * a re-render, not a fixed number of paints: at 5 000 albums, End from
 * the top produced the card in under 500 ms and a ten-frame budget
 * (~160 ms) expired first — the index moved and nothing took focus,
 * which is indistinguishable from the key not being handled at all.
 */
const focusRetryBudgetMs = 1000;

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

    /**
     * Move the tab stop, scrolling and focusing the card.
     *
     * The focus is retried across a few frames because the host
     * finishing its update is not the virtualizer finishing its own: a
     * scroll of several thousand rows produces the card a frame or two
     * later, and a single query at `updateComplete` finds nothing and
     * silently leaves focus where it was. Measured at 5 000 albums with
     * a dropdown open, where End moved the index and focused nothing.
     */
    focus(index: number): void {
        this.focusedIndex = index;
        this.opts.scrollToIndex?.(index);
        this.host.requestUpdate();

        const deadline = performance.now() + focusRetryBudgetMs;

        void this.host.updateComplete.then(() => this.focusCard(index, deadline));
    }

    private focusCard(index: number, deadline: number): void {
        const card = this.cards().find(
            (c) => Number(c.dataset['index']) === index,
        );

        if (card) {
            card.focus();

            return;
        }

        // Give up rather than spin: the index can also simply be gone,
        // if a rescan shortened the list mid-keypress.
        if (performance.now() >= deadline) return;

        // Stop chasing an index the user has already moved away from.
        if (this.focusedIndex !== index) return;

        requestAnimationFrame(() => this.focusCard(index, deadline));
    }

    private cards(): HTMLElement[] {
        return [
            ...(this.host.shadowRoot?.querySelectorAll<HTMLElement>(
                this.opts.cardSelector,
            ) ?? []),
        ];
    }

    /**
     * Cards sharing a top edge are one row.
     *
     * Rounded because a virtualizer's transforms are fractional and two
     * cards in the same row routinely differ in the third decimal, and
     * measured against the *first* card's row because that row is the
     * only one guaranteed to be full — a short last row would
     * under-count the columns.
     */
    private measureColumns(): number {
        const cards = this.cards();

        if (cards.length === 0) return 1;

        const top = Math.round(cards[0]!.getBoundingClientRect().top);
        const inRow = cards.filter(
            (card) => Math.round(card.getBoundingClientRect().top) === top,
        ).length;

        return Math.max(1, inRow);
    }
}

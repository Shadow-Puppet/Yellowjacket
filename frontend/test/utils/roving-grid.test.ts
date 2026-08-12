/**
 * The roving tab stop for the card grids, and specifically the one
 * thing in it that was wrong from the day it was written: how it
 * measures a row.
 *
 * `measureColumns` used `offsetTop`, and every card in these grids is
 * produced by a `lit-virtualizer`, which positions its children with a
 * `transform`. `offsetTop` does not see a transform — so all of them
 * reported 0, every rendered card counted as one row, and ArrowDown
 * became `min(i + everything, last)` while ArrowUp became `max(i -
 * everything, 0)`. The vertical arrows were End and Home, in the
 * albums, artists and genres grids alike.
 *
 * These cards are positioned the same way the virtualizer positions
 * its own, which is the point: laid out with `top` the old code passes.
 */
import { describe, expect, it } from 'vitest';

import { RovingGridController } from '@utils/roving-grid';

const CARD = 100;
const ROW = 120;

/**
 * A grid of `n` cards in rows of `columns`, positioned with a
 * transform, inside a host that satisfies ReactiveControllerHost.
 */
function grid(n: number, columns: number) {
    const el = document.createElement('div');

    el.attachShadow({ mode: 'open' });
    document.body.append(el);

    for (let i = 0; i < n; i++) {
        const card = document.createElement('div');

        card.className = 'album-card';
        card.dataset['index'] = String(i);
        card.tabIndex = -1;
        card.style.cssText = `position:absolute;width:${CARD}px;height:${CARD}px;transform:translate(${
            (i % columns) * CARD
        }px, ${Math.floor(i / columns) * ROW}px)`;
        el.shadowRoot!.append(card);
    }

    const host = {
        shadowRoot: el.shadowRoot,
        addController: () => {},
        removeController: () => {},
        requestUpdate: () => {},
        updateComplete: Promise.resolve(true),
    };

    const controller = new RovingGridController(host, {
        cardSelector: '.album-card',
        count: () => n,
    });

    const press = (key: string) => {
        controller.handleKeydown(
            new KeyboardEvent('keydown', { key, bubbles: true }),
        );
    };

    /** Which card holds the tab stop. */
    const stop = () => {
        for (let i = 0; i < n; i++) {
            if (controller.tabIndexFor(i) === 0) return i;
        }

        return -1;
    };

    return { press, stop, cleanup: () => el.remove() };
}

describe('roving grid: moving by a row', () => {
    it('moves down by the number of columns, not by the whole grid', () => {
        const g = grid(12, 4);

        g.press('ArrowDown');

        // 4, not 11. With offsetTop this was 11 — the last card.
        expect(g.stop()).toBe(4);

        g.cleanup();
    });

    it('moves back up by the same amount', () => {
        const g = grid(12, 4);

        g.press('ArrowDown');
        g.press('ArrowDown');
        g.press('ArrowUp');

        expect(g.stop()).toBe(4);

        g.cleanup();
    });

    it('clamps at the last card rather than overshooting', () => {
        const g = grid(10, 4);

        for (let i = 0; i < 5; i++) g.press('ArrowDown');

        expect(g.stop()).toBe(9);

        g.cleanup();
    });

    it('counts the columns of a single-row grid', () => {
        const g = grid(3, 4);

        g.press('ArrowDown');

        expect(g.stop()).toBe(2);

        g.cleanup();
    });

    it('still moves one card at a time horizontally', () => {
        const g = grid(12, 4);

        g.press('ArrowRight');
        g.press('ArrowRight');

        expect(g.stop()).toBe(2);

        g.cleanup();
    });

    it('takes Home and End to the ends', () => {
        const g = grid(12, 4);

        g.press('End');
        expect(g.stop()).toBe(11);

        g.press('Home');
        expect(g.stop()).toBe(0);

        g.cleanup();
    });
});

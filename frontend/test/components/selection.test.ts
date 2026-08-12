/**
 * A refetch is not a deselection.
 *
 * Selecting forty tracks to drag into a playlist was impossible while
 * music was playing: every finished track invalidated the library
 * cache, `track-list` answered the new array by calling `loadTracks()`,
 * and that cleared the selection (audit perf.C2).
 *
 * Half of that is fixed in the backend — a play count no longer
 * invalidates anything (perf.C1) — but the other half has to hold on
 * its own, because a rescan, a retag or a library switch still deliver
 * a new array, and none of those should throw away a selection whose
 * items are still in the list.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { SelectionController } from '../../src/utils/selection-controller';

class FakeHost {
    items: string[] = [];

    updates = 0;

    changes = 0;

    // ReactiveControllerHost, minus the parts a selection never uses.
    addController(): void {
        /* registration; nothing to do here. */
    }

    removeController(): void {
        /* ditto. */
    }

    updateComplete = Promise.resolve(true);

    requestUpdate(): void {
        this.updates++;
    }

    onSelectionChanged(): void {
        this.changes++;
    }

    getItemCount(): number {
        return this.items.length;
    }

    getItemKey(index: number): string | undefined {
        return this.items[index];
    }
}

function click(
    selection: SelectionController,
    host: FakeHost,
    index: number,
    modifiers: MouseEventInit = {},
): void {
    selection.handleItemClick(
        new MouseEvent('click', modifiers),
        host.items[index] as string,
        index,
    );
}

describe('selection across a refetch', () => {
    let host: FakeHost;
    let selection: SelectionController;

    beforeEach(() => {
        host = new FakeHost();
        host.items = ['/a.mp3', '/b.mp3', '/c.mp3', '/d.mp3'];
        selection = new SelectionController(host);

        click(selection, host, 0);
        click(selection, host, 2, { ctrlKey: true });
        click(selection, host, 3, { ctrlKey: true });
    });

    it('keeps a selection whose items all survive', () => {
        const present = new Set(host.items);

        selection.retain((key) => present.has(key));

        expect([...selection.selectedItems].sort()).toEqual([
            '/a.mp3', '/c.mp3', '/d.mp3',
        ]);
    });

    it('drops only the items that are gone', () => {
        host.items = ['/a.mp3', '/b.mp3', '/c.mp3'];
        const present = new Set(host.items);

        selection.retain((key) => present.has(key));

        expect([...selection.selectedItems].sort()).toEqual([
            '/a.mp3', '/c.mp3',
        ]);
        expect(host.changes).toBeGreaterThan(0);
    });

    it('does not notify when nothing changed', () => {
        const present = new Set(host.items);
        const before = host.changes;

        selection.retain((key) => present.has(key));

        expect(host.changes).toBe(before);
    });

    it('forgets the shift-click anchor, which indexes the old list', () => {
        host.items = ['/d.mp3', '/a.mp3', '/c.mp3'];
        const present = new Set(host.items);

        selection.retain((key) => present.has(key));

        // A shift-click after a refetch extends from the clicked item
        // alone rather than from a row that has since moved.
        click(selection, host, 2, { shiftKey: true });

        expect(selection.selectedItems.has('/c.mp3')).toBe(true);
    });

    it('selects all even when the count matches the previous size', () => {
        // perf.p5: the guard compared cardinalities, so a selection
        // that happened to be the same size as the list short-circuited
        // Select All into a no-op.
        selection.clear();
        click(selection, host, 0);
        click(selection, host, 1, { ctrlKey: true });
        click(selection, host, 2, { ctrlKey: true });
        click(selection, host, 3, { ctrlKey: true });

        host.items = ['/w.mp3', '/x.mp3', '/y.mp3', '/z.mp3'];
        selection.selectAll();

        expect([...selection.selectedItems].sort()).toEqual([
            '/w.mp3', '/x.mp3', '/y.mp3', '/z.mp3',
        ]);
    });
});

/**
 * `getSelectedKeysOrdered()` / `getSelectedIndices()` walk the list
 * rather than the selection, which is `perf.m6`. Measured at 50 000
 * tracks that walk is 3 ms — a fifth of a frame — so it stays, and the
 * only change is that it stops once it has found everything.
 *
 * That early exit has one way to be wrong, and it is the case these
 * tests exist for: a selected key that is *not* in the list must not
 * end the walk early and truncate the answer. It is reachable —
 * `retain()` deliberately keeps keys across a list it has not
 * re-checked, and Ctrl-clicking builds a selection in any order.
 */
describe('ordered selection accessors', () => {
    let host: FakeHost;
    let selection: SelectionController;

    beforeEach(() => {
        host = new FakeHost();
        host.items = ['/a.mp3', '/b.mp3', '/c.mp3', '/d.mp3'];
        selection = new SelectionController(host);
    });

    it('returns keys in list order, not selection order', () => {
        click(selection, host, 3);
        click(selection, host, 1, { ctrlKey: true });
        click(selection, host, 0, { ctrlKey: true });

        expect(selection.getSelectedKeysOrdered())
            .toEqual(['/a.mp3', '/b.mp3', '/d.mp3']);
        expect(selection.getSelectedIndices()).toEqual([0, 1, 3]);
    });

    it('is empty for an empty selection', () => {
        expect(selection.getSelectedKeysOrdered()).toEqual([]);
        expect(selection.getSelectedIndices()).toEqual([]);
    });

    it('finds a selection at the end of the list', () => {
        click(selection, host, 3);

        expect(selection.getSelectedKeysOrdered()).toEqual(['/d.mp3']);
        expect(selection.getSelectedIndices()).toEqual([3]);
    });

    it('does not stop early when a selected key is missing', () => {
        click(selection, host, 0);
        click(selection, host, 3, { ctrlKey: true });

        // The list loses the first selected item; the selection still
        // holds its key, which is exactly what `retain()` allows.
        host.items = ['/b.mp3', '/c.mp3', '/d.mp3'];

        expect(selection.getSelectedKeysOrdered()).toEqual(['/d.mp3']);
        expect(selection.getSelectedIndices()).toEqual([2]);
    });

    it('returns every selected key when all are selected', () => {
        selection.selectAll();

        expect(selection.getSelectedKeysOrdered()).toEqual(host.items);
        expect(selection.getSelectedIndices()).toEqual([0, 1, 2, 3]);
    });
});

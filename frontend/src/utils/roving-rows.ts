/**
 * A roving tab stop over a virtualized list of rows.
 *
 * Three lists needed the same thing at once — the queue panel and both
 * playlist detail views — because a context menu opened with Shift+F10
 * needs a focused row to open *from*, and none of the three had one:
 * their rows were plain `<div>`s with no `tabindex` and no `role`.
 *
 * `track-list` deliberately does not use this. Its equivalent predates
 * it, carries selection semantics (shift-extend, ctrl-toggle) that the
 * other three do not have, and is pinned by its own tests; converting it
 * would be a rewrite of the one list that already worked.
 *
 * Two things here are not optional:
 *
 * - **The virtualizer is told the index changed.** Rows come from the
 *   `virtualize` directive, which re-renders on the virtualizer's *own*
 *   properties — a host re-render does not move a `tabindex`.
 * - **Focus is taken after the update.** The row for an index that was
 *   off-screen does not exist until the virtualizer has scrolled to it.
 */
import type { LitVirtualizer } from '@lit-labs/virtualizer';

export interface RovingRowsHost {
    requestUpdate(): void;
    updateComplete: Promise<boolean>;
}

/** The keys this handles, and what they mean given a row count. */
export function nextRovingIndex(
    key: string,
    current: number,
    count: number,
): number | null {
    switch (key) {
    case 'ArrowDown':
        return Math.min(current + 1, count - 1);
    case 'ArrowUp':
        return Math.max(current - 1, 0);
    case 'Home':
        return 0;
    case 'End':
        return count - 1;
    default:
        return null;
    }
}

/**
 * Move the tab stop to `index` and put focus on it.
 *
 * `rowSelector` receives the index and must return a selector matching
 * that row inside the virtualizer's light DOM.
 */
export async function focusRovingRow(
    host: RovingRowsHost,
    virtualizer: LitVirtualizer | undefined,
    index: number,
    rowSelector: (index: number) => string,
): Promise<void> {
    host.requestUpdate();
    virtualizer?.requestUpdate();
    virtualizer?.scrollToIndex(index, 'nearest');

    await host.updateComplete;

    virtualizer
        ?.querySelector<HTMLElement>(rowSelector(index))
        ?.focus();
}

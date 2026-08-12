import type { ReactiveController, ReactiveControllerHost } from 'lit';

/**
 * Host interface for components using the SelectionController.
 * The host must provide a way to look up item keys by index and
 * report the total item count.
 */
export interface SelectionHost extends ReactiveControllerHost {
    getItemKey(index: number): string | undefined;
    getItemCount(): number;
    onSelectionChanged?(): void;
}

/**
 * Reusable selection controller that manages multi-select state
 * with click, Ctrl+click, and Shift+click semantics.
 */
export class SelectionController implements ReactiveController {
    private host: SelectionHost;
    private _selectedItems: Set<string> = new Set();
    private lastSelectedIndex: number | null = null;

    constructor(host: SelectionHost) {
        this.host = host;
        host.addController(this);
    }

    hostConnected(): void {
        // No-op; state is component-local.
    }

    hostDisconnected(): void {
        // No-op.
    }

    // =================================================================
    // STATE ACCESSORS
    // =================================================================

    /** The current set of selected item keys. */
    get selectedItems(): ReadonlySet<string> {
        return this._selectedItems;
    }

    /** Whether any items are currently selected. */
    get hasSelection(): boolean {
        return this._selectedItems.size > 0;
    }

    /** Number of selected items. */
    get selectionCount(): number {
        return this._selectedItems.size;
    }

    /** Check whether a specific key is selected. */
    isSelected(key: string): boolean {
        return this._selectedItems.has(key);
    }

    // =================================================================
    // ACTIONS
    // =================================================================

    /**
     * Handle a click on an item row. Supports plain click (replace
     * selection), Ctrl/Cmd+click (toggle), Shift+click (range), and
     * Ctrl+Shift+click (add range to existing selection).
     */
    handleItemClick(
        e: MouseEvent,
        key: string,
        index: number,
    ): void {
        const isCtrl = e.ctrlKey || e.metaKey;
        const isShift = e.shiftKey;

        if (isShift && this.lastSelectedIndex !== null) {
            const range = this.selectRange(
                this.lastSelectedIndex,
                index,
            );

            // Both Shift and Ctrl+Shift add the range to the
            // existing selection.
            const next = new Set(this._selectedItems);

            for (const path of range) {
                next.add(path);
            }

            this._selectedItems = next;

            // Don't update anchor on shift-click so the user can
            // adjust the range endpoint with another shift-click.
        } else if (isCtrl) {
            const next = new Set(this._selectedItems);

            if (next.has(key)) {
                next.delete(key);
            } else {
                next.add(key);
            }

            this._selectedItems = next;
            this.lastSelectedIndex = index;
        } else {
            this._selectedItems = new Set([key]);
            this.lastSelectedIndex = index;
        }

        this.host.requestUpdate();
        this.host.onSelectionChanged?.();
    }

    /**
     * Handle a right-click (context menu) on an item. If the clicked
     * item is not already selected, replace the selection with just
     * that item. Otherwise preserve the existing multi-selection.
     */
    handleContextMenu(key: string): void {
        if (!this._selectedItems.has(key)) {
            this._selectedItems = new Set([key]);
            this.host.requestUpdate();
            this.host.onSelectionChanged?.();
        }
    }

    /** Whether `next` holds exactly the currently selected keys. */
    private sameMembership(next: ReadonlySet<string>): boolean {
        if (next.size !== this._selectedItems.size) return false;

        for (const key of next) {
            if (!this._selectedItems.has(key)) return false;
        }

        return true;
    }

    /** Clear the entire selection. */
    clear(): void {
        if (this._selectedItems.size === 0) return;

        this._selectedItems = new Set();
        this.lastSelectedIndex = null;
        this.host.requestUpdate();
        this.host.onSelectionChanged?.();
    }

    /**
     * Drop selected keys that are no longer in the list, keeping the
     * rest.
     *
     * The reason this exists rather than `clear()`: a refetch is not a
     * deselection. Every naturally finished track used to invalidate
     * the library cache, and `track-list` answered the new array by
     * clearing the selection — so selecting forty tracks to drag into a
     * playlist was impossible while music was playing (audit perf.C2).
     * Keys are file paths, which survive a refetch, so the selection
     * survives with them.
     *
     * `lastSelectedIndex` is dropped regardless: it is an index into a
     * list that has just been replaced, and a shift-click against a
     * stale one selects the wrong range.
     */
    retain(isStillPresent: (key: string) => boolean): void {
        if (this._selectedItems.size === 0) return;

        const next = new Set<string>();

        for (const key of this._selectedItems) {
            if (isStillPresent(key)) next.add(key);
        }

        this.lastSelectedIndex = null;

        if (next.size === this._selectedItems.size) return;

        this._selectedItems = next;
        this.host.requestUpdate();
        this.host.onSelectionChanged?.();
    }

    /** Select all items. */
    selectAll(): void {
        const count = this.host.getItemCount();
        const next = new Set<string>();

        for (let i = 0; i < count; i++) {
            const key = this.host.getItemKey(i);
            if (key !== undefined) next.add(key);
        }

        // Membership, not cardinality. The guard used to compare sizes
        // alone, so selecting four rows and then Select All over a
        // *different* four was a no-op (audit perf.p5) — reachable now
        // that `retain()` above carries a selection across a refetch
        // that replaced the list.
        if (this.sameMembership(next)) return;

        this._selectedItems = next;
        this.lastSelectedIndex = count > 0 ? count - 1 : null;
        this.host.requestUpdate();
        this.host.onSelectionChanged?.();
    }

    /**
     * Return the selected keys in the order they appear in the host's
     * item list. This preserves positional ordering for queue operations.
     *
     * This walks the *list*, not the selection, which audit `perf.m6`
     * calls out: every `dragstart`, every context-menu action and every
     * favourite toggle pays it. Measured at 50 000 tracks it is **3 ms**
     * — real, and a fifth of a frame, so the loop stays. What it does
     * not do any more is keep going after it has found everything.
     *
     * It walks the list rather than the selection *deliberately*: the
     * only way to order the selection directly is to store each key's
     * index with it, and an index goes stale whenever the list is
     * re-sorted, re-filtered or refetched, while the keys (file paths)
     * survive all three — which is exactly why `retain()` drops
     * `lastSelectedIndex` and keeps the keys. Trading 3 ms for a
     * silently mis-ordered queue insert is not a trade.
     */
    getSelectedKeysOrdered(): string[] {
        const wanted = this._selectedItems.size;

        if (wanted === 0) return [];

        const count = this.host.getItemCount();
        const result: string[] = [];

        for (let i = 0; i < count; i++) {
            const key = this.host.getItemKey(i);

            if (key !== undefined && this._selectedItems.has(key)) {
                result.push(key);
                if (result.length === wanted) break;
            }
        }

        return result;
    }

    /**
     * Return the selected indices in ascending order.
     *
     * Same shape, same reasoning, same early exit as
     * `getSelectedKeysOrdered()` above.
     */
    getSelectedIndices(): number[] {
        const wanted = this._selectedItems.size;

        if (wanted === 0) return [];

        const count = this.host.getItemCount();
        const result: number[] = [];

        for (let i = 0; i < count; i++) {
            const key = this.host.getItemKey(i);

            if (key !== undefined && this._selectedItems.has(key)) {
                result.push(i);
                if (result.length === wanted) break;
            }
        }

        return result;
    }

    // =================================================================
    // INTERNALS
    // =================================================================

    /**
     * Build a Set of keys for all items between two indices (inclusive),
     * handling either direction.
     */
    private selectRange(from: number, to: number): Set<string> {
        const start = Math.min(from, to);
        const end = Math.max(from, to);
        const keys = new Set<string>();

        for (let i = start; i <= end; i++) {
            const key = this.host.getItemKey(i);

            if (key !== undefined) {
                keys.add(key);
            }
        }

        return keys;
    }
}

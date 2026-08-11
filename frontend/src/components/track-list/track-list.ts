import { library } from '@go/models';
import { LitElement, html, svg, css, nothing } from 'lit';
import { designTokens } from '../../styles/tokens.css';
import {
    customElement,
    property,
    state,
    query,
} from 'lit/decorators.js';
import { SelectionController } from '@utils/selection-controller';
import type { SelectionHost } from '@utils/selection-controller';
import {
    ContextMenuController,
    contextMenuStyles,
} from '@utils/context-menu-controller.js';

import type { ContextMenuHost } from '@utils/context-menu-controller.js';
import { PlayerController } from '@store/controllers/player-controller';
import { SearchController } from '@store/controllers/search-controller';
import { TrackListController } from '@store/controllers/tracklist-controller';
import { FavoritesController } from '@store/controllers/favorites-controller';
import { queueStore } from '@store/queue-store';
import { LibraryController } from '@store/controllers/library-controller';
import {
    COLUMN_DEFS,
    DEFAULT_COLUMN_IDS,
} from './columns';
import type { ColumnDef } from './columns';
import { classMap } from 'lit/directives/class-map.js';
import {
    rankTracks,
    highlightText,
} from './search-ranking';
import {
    artistLink,
    albumLink,
    trackLink,
    exploreLinkStyles,
} from '@utils/explore-link';
import {
    setDragPayload,
    emitDragActive,
} from '@utils/drag-controller';
import {
    createDragImage,
    createTrackCardDragImage,
    removeDragImage,
} from '@utils/drag-image';
import '@lit-labs/virtualizer';
import type {
    LitVirtualizer,
    VisibilityChangedEvent,
} from '@lit-labs/virtualizer';
import { flow } from '@lit-labs/virtualizer/layouts/flow.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import type WaPopup from '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/playlist-picker/playlist-picker.js';
import '@components/track-details/track-details.js';
import type { TrackDetails } from '@components/track-details/track-details.js';
import type { CoverArtUrls } from '@components/track-details/track-details.js';

const COLUMN_STORAGE_KEY = 'track-list-column-widths';
const SORT_FIELD_KEY = 'track-list-sort-field';
const SORT_DIR_KEY = 'track-list-sort-direction';
const MIN_COLUMN_WIDTH = 50;
const DEFAULT_FIXED_WIDTH = 80;

// Inline SVG paths for favorite icons — eliminates wa-icon shadow DOM
// overhead (30-50 shadow roots during scroll).  Font Awesome 6 paths.
const FAV_ICONS = {
    heart: {
        viewBox: '0 0 512 512',
        regular: 'M225.8 468.2l-2.5-2.3L48.1 303.2C17.4 274.7 0 234.7 0 192.8v-3.3c0-70.4 50-130.8 119.2-144C158.6 37.9 198.9 47 231 69.6c9 6.3 17.3 13.5 25 21.5c7.7-8 16-15.2 25-21.5c32.1-22.6 72.4-31.7 111.8-24.2C461.5 59.6 512 124.2 512 192.8v3.3c0 41.9-17.4 81.9-48.1 110.4L288.7 465.9l-2.5 2.3c-8.2 7.6-19 11.9-30.2 11.9s-22-4.2-30.2-11.9z',
        solid: 'M47.6 300.4L228.3 469.1c7.5 7 17.4 10.9 27.7 10.9s20.2-3.9 27.7-10.9L464.4 300.4c30.4-28.3 47.6-68 47.6-109.5v-5.8c0-69.9-50.5-129.5-119.4-141C347 36.5 300.6 51.4 268 84L256 96 244 84c-32.6-32.6-79-47.5-124.6-39.9C50.5 55.6 0 115.2 0 185.1v5.8c0 41.5 17.2 81.2 47.6 109.5z',
    },
    star: {
        viewBox: '0 0 576 512',
        regular: 'M287.9 0c9.2 0 17.6 5.2 21.6 13.5l68.6 141.3 153.2 22.6c9 1.3 16.5 7.6 19.3 16.3s.5 18.1-5.9 24.5L434.8 326.7l26.2 155.6c1.5 9-2.2 18.1-9.7 23.5s-17.3 6-25.3 1.7L288 439.6 149.7 507.5c-8 4.3-17.8 3.7-25.3-1.7s-11.2-14.5-9.7-23.5l26.2-155.6L31.1 218.2c-6.5-6.4-8.7-15.9-5.9-24.5s10.3-14.9 19.3-16.3l153.2-22.6L266.3 13.5C270.4 5.2 278.7 0 287.9 0z',
        solid: 'M316.9 18C311.6 7 300.4 0 288.1 0s-23.4 7-28.8 18L195 150.3 51.4 171.5c-12 1.8-22 10.2-25.7 21.7s-.7 24.2 7.9 32.7L137.8 329 108.4 474.7c-2 12 3 24.2 12.9 31.3s23 8 33.8 2.3L288.1 439.8 420.9 508.3c10.8 5.7 23.9 4.9 33.8-2.3s14.9-19.3 12.9-31.3L437.7 329 542 225.9c8.6-8.4 11.7-21.2 7.9-32.7s-13.7-19.9-25.7-21.7L380.7 150.3 316.9 18z',
    },
} as const;

type SortDirection = 'asc' | 'desc';

@customElement('track-list')
export class TrackList extends LitElement implements SelectionHost, ContextMenuHost {
    /**
     * When set, the list displays these tracks instead of
     * fetching all tracks from the library store.  The
     * parent is responsible for reloading when data changes.
     */
    @property({ type: Array, attribute: false })
    externalTracks?: library.Track[];

    private player = new PlayerController(this);
    private libraryCtrl = new LibraryController(this);
    private searchCtrl = new SearchController(this);
    private trackListCtrl = new TrackListController(this);
    private favCtrl = new FavoritesController(this);
    private selection = new SelectionController(this);
    private ctxMenu = new ContextMenuController(this);
    private lastSearchTerm = '';

    /** Tracks the store's cached array reference to detect refreshes. */
    private lastTracksRef: library.Track[] | null =
        null;

    /**
     * Resolved column definitions for the currently configured
     * column IDs.  Falls back to defaults for any unknown ID.
     */
    private get activeColumns(): ColumnDef[] {
        const ids = this.trackListCtrl.columnIds;

        if (!ids || ids.length === 0) {
            return DEFAULT_COLUMN_IDS
                .map((id) => COLUMN_DEFS[id])
                .filter(
                    (d): d is ColumnDef =>
                        d !== undefined,
                );
        }

        return ids
            .map((id) => COLUMN_DEFS[id])
            .filter(
                (d): d is ColumnDef =>
                    d !== undefined,
            );
    }

    @state()
    private tracks: library.Track[] = [];

    @query('#context-menu')
    private contextMenuPopup!: WaPopup;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: WaPopup;

    // -- ContextMenuHost interface --

    getContextMenuPopup(): WaPopup | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup(): WaPopup | undefined {
        return this.playlistSubmenuPopup;
    }

    @query('track-details')
    private trackDetailsDialog!: TrackDetails;

    @query('lit-virtualizer')
    private virtualizer!: LitVirtualizer;

    private lastActiveTrackPath: string | null = null;

    // -- Memoisation caches for filtered / sorted tracks --
    private cachedFilteredTracks: library.Track[] = [];
    private cachedSortedTracks: library.Track[] = [];
    private cachedRelevanceScores = new Map<
        string,
        number
    >();
    private prevFilterTracks: library.Track[] = [];
    private prevFilterTerm = '';
    private prevFilterColIds = '';
    private prevSortFiltered: library.Track[] = [];
    private prevSortField: string | null = null;
    private prevSortDir: SortDirection = 'asc';

    private handleSelectAll = (): void => {
        this.selection.selectAll();
    };

    private clearSelectionHandler = (e: MouseEvent) => {
        const path = e.composedPath();
        const isTrackClick = path.some(
            (el) =>
                el instanceof HTMLElement &&
                el.classList.contains('track-row') &&
                this.shadowRoot?.contains(el),
        );

        if (!isTrackClick) {
            this.selection.clear();
        }
    };

    private dragImageEl: HTMLElement | null = null;

    @state()
    private columnWidths: number[] = [];

    /** Column ID to sort by, or null for default order. */
    @state()
    private sortField: string | null = null;

    /** Current sort direction. */
    @state()
    private sortDirection: SortDirection = 'asc';

    /** Whether the sort dropdown popup is open. */
    @state()
    private sortDropdownOpen = false;

    @query('#sort-dropdown')
    private sortDropdownPopup!: WaPopup;

    /** Whether delegated event handlers have been attached to the virtualizer. */
    private delegationAttached = false;

    private resizingColumn: number | null = null;
    private resizeStartX = 0;
    private resizeStartWidths: number[] = [];
    private resizeObserver: ResizeObserver | null =
        null;

    // _itemSize matches the fixed .track-row height (33px) so lit-virtualizer
    // doesn't need to measure items. Without this hint, the default 100px
    // estimate causes constant scroll error correction (scrollTo() calls)
    // that produce visible jumping/skipping during scroll.
    private flowLayout = flow({
        _itemSize: { width: 100, height: 33 },
    } as Parameters<typeof flow>[0]);
    private hasRestoredScroll = false;
    private scrollSaveRAFId: number | null = null;

    // =================================================================
    // Filtered / sorted tracks (memoised)
    // =================================================================

    /**
     * Recompute the filtered and sorted track caches when
     * their inputs have changed.  Called from willUpdate()
     * so the caches are ready before render().
     */
    private recomputeTrackCaches() {
        const term = this.searchCtrl.term;
        const colIds =
            this.trackListCtrl.columnIds.join(',');

        if (
            this.tracks !== this.prevFilterTracks ||
            term !== this.prevFilterTerm ||
            colIds !== this.prevFilterColIds
        ) {
            this.prevFilterTracks = this.tracks;
            this.prevFilterTerm = term;
            this.prevFilterColIds = colIds;
            this.cachedFilteredTracks =
                this.computeFilteredTracks();
        }

        if (
            this.cachedFilteredTracks !==
                this.prevSortFiltered ||
            this.sortField !== this.prevSortField ||
            this.sortDirection !== this.prevSortDir
        ) {
            this.prevSortFiltered =
                this.cachedFilteredTracks;
            this.prevSortField = this.sortField;
            this.prevSortDir = this.sortDirection;
            this.cachedSortedTracks =
                this.computeSortedTracks();
        }
    }

    private computeFilteredTracks(): library.Track[] {
        const term = this.searchCtrl.term;

        if (!term) {
            this.cachedRelevanceScores.clear();

            return this.tracks;
        }

        const result = rankTracks(
            this.tracks,
            term,
            this.activeColumns,
        );

        this.cachedRelevanceScores = result.scores;

        return result.tracks;
    }

    private computeSortedTracks(): library.Track[] {
        const tracks = this.cachedFilteredTracks;
        const hasSearch =
            this.cachedRelevanceScores.size > 0;
        const col = this.sortField
            ? COLUMN_DEFS[this.sortField]
            : undefined;
        const hasColSort = col?.comparator != null;

        // No search, no column sort — default order.
        if (!hasSearch && !hasColSort) return tracks;

        // No search, column sort only — sort by column.
        if (!hasSearch && hasColSort) {
            const dir =
                this.sortDirection === 'asc' ? 1 : -1;

            return [...tracks].sort(
                (a, b) =>
                    dir * col!.comparator!(a, b),
            );
        }

        // Search active — relevance is primary sort,
        // column sort (if any) is the tiebreaker.
        const scores = this.cachedRelevanceScores;
        const dir =
            this.sortDirection === 'asc' ? 1 : -1;

        return [...tracks].sort((a, b) => {
            const sa = scores.get(a.FilePath) ?? 0;
            const sb = scores.get(b.FilePath) ?? 0;

            if (sa !== sb) return sb - sa;

            if (hasColSort) {
                return dir * col!.comparator!(a, b);
            }

            return 0;
        });
    }

    // =================================================================
    // SelectionHost interface
    // =================================================================

    getItemKey(index: number): string | undefined {
        return this.cachedSortedTracks[index]?.FilePath;
    }

    getItemCount(): number {
        return this.cachedSortedTracks.length;
    }

    onSelectionChanged(): void {
        this.virtualizer?.requestUpdate();
    }

    private get gridTemplateColumns(): string {
        const cols = this.activeColumns;
        const favCol = '24px';

        if (this.columnWidths.length === 0 || this.columnWidths.length !== cols.length) {
            return (
                favCol +
                ' ' +
                cols
                    .map((c) => c.defaultWidth)
                    .join(' ')
            );
        }

        return (
            favCol +
            ' ' +
            this.columnWidths
                .map((w) => `${w}px`)
                .join(' ')
        );
    }

    private get colBoundaryPositions(): number[] {
        if (this.columnWidths.length === 0) return [];

        const padding = 8;
        const favColWidth = 24;
        const positions: number[] = [];
        let cumulative = padding + favColWidth;

        for (
            let i = 0;
            i < this.columnWidths.length - 1;
            i++
        ) {
            cumulative += this.columnWidths[i] ?? 0;
            positions.push(cumulative);
        }

        return positions;
    }

    private initColumnWidths() {
        const saved = this.loadColumnWidths();
        const cols = this.activeColumns;

        if (saved && saved.length === cols.length) {
            this.columnWidths = saved;

            return;
        }

        this.computeDefaultWidths();
    }

    private computeDefaultWidths() {
        const totalWidth = this.clientWidth;

        if (totalWidth <= 0) return;

        const cols = this.activeColumns;

        if (cols.length === 0) return;

        // Fixed-width columns use their pixel default;
        // flex columns share the remainder equally.
        const fixedTotal = cols.reduce((sum, c) => {
            if (c.defaultWidth.endsWith('px')) {
                return (
                    sum +
                    parseInt(c.defaultWidth, 10)
                );
            }

            return sum;
        }, 0);

        const flexCols = cols.filter(
            (c) => !c.defaultWidth.endsWith('px'),
        );

        const remaining = Math.max(
            0,
            totalWidth - fixedTotal,
        );

        const perFlex =
            flexCols.length > 0
                ? Math.floor(
                      remaining / flexCols.length,
                  )
                : DEFAULT_FIXED_WIDTH;

        const raw = cols.map((c) => {
            if (c.defaultWidth.endsWith('px')) {
                return parseInt(c.defaultWidth, 10);
            }

            return Math.max(
                MIN_COLUMN_WIDTH,
                perFlex,
            );
        });

        this.columnWidths = this.normalizeWidths(raw);
    }

    private loadColumnWidths(): number[] | null {
        try {
            const raw = localStorage.getItem(
                COLUMN_STORAGE_KEY,
            );

            if (!raw) return null;

            const parsed: unknown = JSON.parse(raw);

            // Support new id-keyed format: Record<string, number>.
            if (
                parsed !== null &&
                typeof parsed === 'object' &&
                !Array.isArray(parsed)
            ) {
                const map = parsed as Record<
                    string,
                    unknown
                >;
                const cols = this.activeColumns;

                const widths = cols.map((c) => {
                    const w = map[c.id];

                    if (
                        typeof w === 'number' &&
                        w >= MIN_COLUMN_WIDTH
                    ) {
                        return w;
                    }

                    // Fallback for columns without saved width.
                    if (
                        c.defaultWidth.endsWith('px')
                    ) {
                        return parseInt(
                            c.defaultWidth,
                            10,
                        );
                    }

                    return MIN_COLUMN_WIDTH;
                });

                return this.normalizeWidths(widths);
            }

            // Legacy array format — discard on column count mismatch.
            return null;
        } catch {
            return null;
        }
    }

    private saveColumnWidths() {
        try {
            const cols = this.activeColumns;

            const map: Record<string, number> = {};

            cols.forEach((c, i) => {
                map[c.id] =
                    this.columnWidths[i] ??
                    MIN_COLUMN_WIDTH;
            });

            localStorage.setItem(
                COLUMN_STORAGE_KEY,
                JSON.stringify(map),
            );
        } catch {
            // Ignore storage errors.
        }
    }

    /**
     * Scale widths so they sum to exactly the container width.
     * Every column is guaranteed at least MIN_COLUMN_WIDTH.
     */
    private normalizeWidths(
        widths: number[],
    ): number[] {
        const container = this.clientWidth;

        if (container <= 0 || widths.length === 0) {
            return widths;
        }

        const minTotal =
            widths.length * MIN_COLUMN_WIDTH;

        // If the container can't even fit minimums,
        // give every column the minimum.
        if (container <= minTotal) {
            return widths.map(
                () => MIN_COLUMN_WIDTH,
            );
        }

        const sum = widths.reduce(
            (a, b) => a + b,
            0,
        );

        if (sum <= 0) {
            const even = Math.floor(
                container / widths.length,
            );

            return widths.map(() =>
                Math.max(MIN_COLUMN_WIDTH, even),
            );
        }

        // Scale proportionally.
        const scale = container / sum;

        const scaled = widths.map((w) =>
            Math.max(
                MIN_COLUMN_WIDTH,
                Math.round(w * scale),
            ),
        );

        // Fix rounding remainder so the total is
        // exactly containerWidth.
        const scaledSum = scaled.reduce(
            (a, b) => a + b,
            0,
        );

        const diff = container - scaledSum;

        if (diff !== 0) {
            // Apply remainder to the widest column.
            let maxIdx = 0;

            for (let i = 1; i < scaled.length; i++) {
                if (
                    (scaled[i] ?? 0) >
                    (scaled[maxIdx] ?? 0)
                ) {
                    maxIdx = i;
                }
            }

            scaled[maxIdx] =
                (scaled[maxIdx] ?? 0) + diff;
        }

        return scaled;
    }

    private onColResizeStart = (e: MouseEvent, columnIndex: number) => {
        e.preventDefault();
        this.resizingColumn = columnIndex;
        this.resizeStartX = e.clientX;
        this.resizeStartWidths = [...this.columnWidths];
        this.requestUpdate();
    };

    private onColResizeMove = (e: MouseEvent) => {
        if (this.resizingColumn === null) return;

        const container = this.clientWidth;

        if (container <= 0) return;

        const delta = e.clientX - this.resizeStartX;
        const col = this.resizingColumn;
        const starts = this.resizeStartWidths;

        // Sum of columns to the left (unchanged).
        let leftSum = 0;

        for (let i = 0; i < col; i++) {
            leftSum += starts[i] ?? 0;
        }

        // Count and sum of columns to the right.
        const rightCount =
            starts.length - col - 1;

        let rightSum = 0;

        for (
            let i = col + 1;
            i < starts.length;
            i++
        ) {
            rightSum += starts[i] ?? 0;
        }

        // Clamp dragged column: leave at least
        // MIN_COLUMN_WIDTH for each right column.
        const maxWidth =
            container -
            leftSum -
            rightCount * MIN_COLUMN_WIDTH;

        let newWidth = Math.max(
            MIN_COLUMN_WIDTH,
            Math.min(
                maxWidth,
                (starts[col] ?? 0) + delta,
            ),
        );

        const updated: number[] = new Array(
            starts.length,
        );

        // Left columns keep starting widths.
        for (let i = 0; i < col; i++) {
            updated[i] = starts[i] ?? 0;
        }

        updated[col] = newWidth;

        // Right columns always fill remaining space
        // proportionally (handles both grow & shrink).
        const availableForRight =
            container - leftSum - newWidth;

        if (rightCount === 0 || rightSum <= 0) {
            // Nothing to distribute.
        } else {
            const scale =
                availableForRight / rightSum;

            let roundedSum = 0;
            let maxIdx = -1;
            let maxVal = 0;

            for (
                let i = col + 1;
                i < starts.length;
                i++
            ) {
                const scaled = Math.max(
                    MIN_COLUMN_WIDTH,
                    Math.round(
                        (starts[i] ?? 0) * scale,
                    ),
                );

                updated[i] = scaled;
                roundedSum += scaled;

                if (scaled > maxVal) {
                    maxVal = scaled;
                    maxIdx = i;
                }
            }

            // Fix rounding remainder on the widest
            // right column.
            const diff =
                availableForRight - roundedSum;

            if (diff !== 0 && maxIdx >= 0) {
                updated[maxIdx] =
                    (updated[maxIdx] ?? 0) + diff;
            }

            // Re-derive dragged width so total is
            // exactly container.
            newWidth =
                container -
                leftSum -
                roundedSum -
                diff;

            if (newWidth < MIN_COLUMN_WIDTH) {
                newWidth = MIN_COLUMN_WIDTH;
            }

            updated[col] = newWidth;
        }

        this.columnWidths = updated;
    };

    private onColResizeEnd = () => {
        if (this.resizingColumn === null) return;

        this.resizingColumn = null;
        this.saveColumnWidths();
        this.requestUpdate();
    };

    static override styles = [designTokens, contextMenuStyles, exploreLinkStyles, css`
    :host {
      display: flex;
      flex-direction: column;
      overflow: hidden;
      height: 100%;
      contain: layout style;
    }

    .table-container {
      position: relative;
      flex: 1;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }

    /* ---- Sort toolbar ---- */

    .sort-toolbar {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 4px 8px;
      font-size: var(--yj-text-sm);
      color: var(--yj-text-secondary, #b3b3b3);
      border-bottom: 1px solid
        var(--yj-border-subtle, #333);
      flex-shrink: 0;
      user-select: none;
    }


    .sort-anchor {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      cursor: pointer;
      padding: 2px 6px;
      border-radius: 4px;
      background: transparent;
      border: none;
      color: inherit;
      font: inherit;
    }

    .sort-anchor:hover {
      background: var(
        --yj-hover-overlay,
        rgba(255, 255, 255, 0.05)
      );
    }

    .sort-anchor .sort-label {
      color: var(--yj-text-primary, #fff);
    }

    .sort-dir-btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 24px;
      height: 24px;
      cursor: pointer;
      border: none;
      border-radius: 4px;
      background: transparent;
      color: var(--yj-text-secondary, #b3b3b3);
      font-size: var(--yj-text-sm);
      padding: 0;
    }

    .sort-dir-btn:hover {
      background: var(
        --yj-hover-overlay,
        rgba(255, 255, 255, 0.05)
      );
      color: var(--yj-text-primary, #fff);
    }

    .sort-dropdown-panel {
      background-color: var(
        --yj-bg-elevated,
        #343a40
      );
      border: 1px solid var(--yj-border, #444);
      border-radius: 6px;
      padding: 4px 0;
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
      min-width: 140px;
    }

    .sort-dropdown-panel wa-dropdown-item {
      cursor: pointer;
      --wa-color-text-normal: var(
        --yj-text-primary,
        #fff
      );
      font-size: var(--yj-text-md);
    }

    .sort-dropdown-panel wa-dropdown-item:hover {
      background-color: var(
        --yj-hover-overlay,
        rgba(255, 255, 255, 0.1)
      );
    }

    .sort-dropdown-panel wa-dropdown-item.active-sort {
      color: var(--yj-accent, #ffd43b);
      --wa-color-text-normal: var(
        --yj-accent,
        #ffd43b
      );
    }

    #sort-dropdown {
      z-index: 200;
    }

    /* ---- Header row ---- */

    .header-row {
      display: grid;
      grid-template-columns: var(--grid-cols);
      padding: 8px;
      font-weight: bold;
      color: var(--yj-text-primary, #fff);
      border-bottom: 1px solid
        var(--yj-text-tertiary, #666);
      flex-shrink: 0;
      overflow: hidden;
    }

    .header-cell {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      cursor: pointer;
      display: flex;
      align-items: center;
      gap: 4px;
    }

    .header-cell:hover {
      color: var(--yj-accent, #ffd43b);
    }

    .sort-arrow {
      font-size: 10px; /* intentionally sub-token: tiny sort indicator */
      flex-shrink: 0;
      color: var(--yj-accent, #ffd43b);
    }

    .resize-overlay {
      position: absolute;
      inset: 0;
      pointer-events: none;
      z-index: 2;
    }

    .col-resize-handle {
      position: absolute;
      top: 0;
      height: 100%;
      width: 1px;
      cursor: col-resize;
      pointer-events: auto;
      background-color: var(--yj-border, #444);
      transition: background-color 0.15s ease;
    }

    .col-resize-handle::before {
      content: '';
      position: absolute;
      top: 0;
      left: -3px;
      width: 7px;
      height: 100%;
    }

    .col-resize-handle:hover,
    .col-resize-handle.active {
      background-color: var(--yj-text-tertiary, #6c757d);
    }

    .sort-toolbar {
      position: relative;
    }

    .search-indicator {
      position: absolute;
      left: 50%;
      transform: translateX(-50%);
      pointer-events: none;
      background: var(--yj-bg-overlay, #495057);
      color: var(--yj-text-secondary, #b3b3b3);
      font-size: var(--yj-text-sm);
      padding: 2px 14px;
      border-radius: 12px;
      border: 1px solid var(--yj-border-subtle, #555);
      white-space: nowrap;
      opacity: 0.92;
    }

    .no-results {
      padding: 24px 16px;
      color: var(--yj-text-secondary, #b3b3b3);
      font-size: var(--yj-text-md);
    }

    lit-virtualizer {
      flex: 1;
      overflow-x: hidden;
      overflow-y: auto;
      user-select: none;
      contain: paint;
      overflow-anchor: none;
    }

    .track-row {
      display: grid;
      grid-template-columns: var(--grid-cols);
      font-size: var(--yj-text-sm);
      padding: 8px;
      border-bottom: 1px solid var(--yj-border-subtle, #333);
      align-items: center;
      width: 100%;
      cursor: default;
      user-select: none;
      overflow: hidden;
      height: 33px;
      box-sizing: border-box;
      contain: strict;
    }

    .track-row > * {
      min-width: 0;
    }

    .header-row > :not(:first-child),
    .track-row > :not(:first-child) {
      padding-left: 6px;
    }

    .track-row:hover {
      background-color: var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
    }

    .track-row.selected {
      background-color: var(--yj-selection-bg, rgba(100, 160, 255, 0.15));
    }

    .track-row.active {
      background-color: var(--yj-accent-bg, rgba(255, 212, 59, 0.1));
    }

    .track-row.active {
      color: var(--yj-accent, #ffd43b);
    }

    .track-row.selected.active {
      background-color: var(--yj-selection-bg, rgba(100, 160, 255, 0.15));
    }

    .cell {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      cursor: default;
      user-select: none;
    }

    .cell-right {
      text-align: right;
    }

    .cell-center {
      text-align: center;
    }

    .fav-icon {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 24px;
      flex-shrink: 0;
      cursor: pointer;
      color: var(--yj-text-tertiary, #666);
      font-size: var(--yj-text-sm);
    }

    .fav-icon:hover {
      color: var(--yj-text-primary, #fff);
    }

    .fav-icon.favorited {
      color: var(--yj-accent, #ffd43b);
    }

    .fav-icon.favorited:hover {
      color: var(--yj-accent, #ffd43b);
      opacity: 0.8;
    }

    .search-match {
      background-color: rgba(255, 212, 59, 0.15);
      border-radius: 2px;
    }

  `];

    override connectedCallback() {
        super.connectedCallback();
        this.restoreSortPreferences();

        if (this.externalTracks) {
            this.tracks = this.externalTracks;
        } else {
            this.loadTracks();
        }
        document.addEventListener('mousedown', this.sortDropdownCloseHandler);
        document.addEventListener('click', this.clearSelectionHandler);
        document.addEventListener('shortcut:select-all', this.handleSelectAll);
        document.addEventListener('mousemove', this.onColResizeMove);
        document.addEventListener('mouseup', this.onColResizeEnd);

        this.resizeObserver = new ResizeObserver(
            () => {
                this.onHostResize();
            },
        );

        this.resizeObserver.observe(this);
    }

    override disconnectedCallback() {
        // Remove delegated event handlers from virtualizer.
        const virt = this.virtualizer;
        if (virt) {
            virt.removeEventListener('click', this.onDelegatedClick);
            virt.removeEventListener('dblclick', this.onDelegatedDblClick);
            virt.removeEventListener('contextmenu', this.onDelegatedContextMenu);
            virt.removeEventListener('dragstart', this.onDelegatedDragStart);
            virt.removeEventListener('dragend', this.onTrackDragEnd);
        }
        this.delegationAttached = false;

        this.virtualizer?.removeEventListener(
            'visibilityChanged',
            this.onVisibilityChanged,
        );
        this.hasRestoredScroll = false;
        super.disconnectedCallback();
        document.removeEventListener('mousedown', this.sortDropdownCloseHandler);
        document.removeEventListener('click', this.clearSelectionHandler);
        document.removeEventListener('shortcut:select-all', this.handleSelectAll);
        document.removeEventListener('mousemove', this.onColResizeMove);
        document.removeEventListener('mouseup', this.onColResizeEnd);

        this.resizeObserver?.disconnect();
        this.resizeObserver = null;
    }

    override willUpdate(
        changed: Map<PropertyKey, unknown>,
    ) {
        super.willUpdate(changed);

        // When the parent provides a new external track
        // list, update local tracks and reset selection.
        if (
            changed.has('externalTracks') &&
            this.externalTracks
        ) {
            this.tracks = this.externalTracks;
            this.selection.clear();
        }

        this.recomputeTrackCaches();
    }

    override firstUpdated() {
        this.initColumnWidths();
        this.attachDelegation();
    }

    /**
     * Attach delegated event handlers to the virtualizer.
     * The virtualizer may not exist on first render (tracks
     * still loading), so this is called from both
     * firstUpdated() and updated() — guarded by a flag.
     */
    private attachDelegation() {
        if (this.delegationAttached) return;

        const virt = this.virtualizer;

        if (!virt) return;

        virt.addEventListener('click', this.onDelegatedClick);
        virt.addEventListener('dblclick', this.onDelegatedDblClick);
        virt.addEventListener('contextmenu', this.onDelegatedContextMenu);
        virt.addEventListener('dragstart', this.onDelegatedDragStart);
        virt.addEventListener('dragend', this.onTrackDragEnd);
        this.delegationAttached = true;
    }

    override updated(changed: Map<string, unknown>) {
        // The virtualizer may not exist on first render
        // (tracks still loading). Retry delegation here.
        this.attachDelegation();

        // Recompute widths when the column config changes.
        const colKey = this.trackListCtrl.columnIds.join(
            ',',
        );

        if (colKey !== this.previousColumnIds) {
            this.previousColumnIds = colKey;
            this.initColumnWidths();
        }

        if (changed.has('columnWidths')) {
            this.style.setProperty(
                '--grid-cols',
                this.gridTemplateColumns,
            );
        }

        const currentPath =
            this.player.currentTrack?.filePath ?? null;

        if (currentPath !== this.lastActiveTrackPath) {
            this.lastActiveTrackPath = currentPath;
            this.virtualizer?.requestUpdate();
        }

        // Clear selection when search term changes.
        const currentTerm = this.searchCtrl.term;

        if (currentTerm !== this.lastSearchTerm) {
            this.lastSearchTerm = currentTerm;
            this.selection.clear();
        }

        // Re-fetch when the store delivers fresh
        // data after eager refetch on invalidation.
        if (!this.externalTracks) {
            const cached =
                this.libraryCtrl.cachedTracks;

            if (
                cached !== null &&
                cached !== this.lastTracksRef
            ) {
                this.lastTracksRef = cached;
                this.loadTracks();
            }
        }
    }

    private previousHostWidth = 0;
    private previousColumnIds = '';

    private onHostResize() {
        const newWidth = this.clientWidth;

        if (
            newWidth <= 0 ||
            this.columnWidths.length === 0 ||
            this.resizingColumn !== null
        ) {
            return;
        }

        if (this.previousHostWidth === 0) {
            this.previousHostWidth = newWidth;

            return;
        }

        this.columnWidths = this.normalizeWidths(
            this.columnWidths,
        );

        this.previousHostWidth = newWidth;
        this.saveColumnWidths();
    }

    async loadTracks() {
        try {
            const tracks = await this.libraryCtrl.getTracks();
            this.tracks = tracks;
            this.selection.clear();
            await this.updateComplete;

            if (this.isConnected && this.virtualizer) {
                this.virtualizer.addEventListener(
                    'visibilityChanged',
                    this.onVisibilityChanged,
                );
            }
        } catch (error) {
            console.error('Error loading tracks:', error);
        }
    }

    private onVisibilityChanged = (e: Event) => {
        const { first } = e as VisibilityChangedEvent;

        if (!this.hasRestoredScroll) {
            this.hasRestoredScroll = true;

            const savedIndex =
                this.libraryCtrl.getScrollPosition('tracks');

            if (savedIndex > 0) {
                requestAnimationFrame(() => {
                    this.virtualizer?.scrollToIndex(
                        savedIndex,
                        'start',
                    );
                });

                return;
            }
        }

        // RAF-throttle scroll position saves — at most once per frame.
        // Without this, every visibilityChanged (fired per-item during
        // scroll) writes to the store synchronously, adding main-thread
        // work during the scroll hot path.
        if (this.scrollSaveRAFId === null) {
            this.scrollSaveRAFId = requestAnimationFrame(() => {
                this.scrollSaveRAFId = null;
                this.libraryCtrl.setScrollPosition('tracks', first);
            });
        }
    };

    // =================================================================
    // Delegated event handlers (stable references, zero per-item closures)
    // =================================================================

    /**
     * Walk up from the event target to find the nearest
     * `.track-row` and extract track + index via `data-index`.
     */
    private resolveTrackFromEvent(
        e: Event,
    ): { track: library.Track; index: number } | null {
        const row = (e.target as HTMLElement).closest(
            '.track-row',
        ) as HTMLElement | null;

        if (!row) return null;

        const idx = Number(row.dataset.index);
        const track = this.cachedSortedTracks[idx];

        if (!track) return null;

        return { track, index: idx };
    }

    private onDelegatedClick = (e: MouseEvent) => {
        const hit = this.resolveTrackFromEvent(e);

        if (!hit) return;

        // Check if click was on fav-icon
        const favEl = (e.target as HTMLElement).closest(
            '.fav-icon',
        );

        if (favEl) {
            e.stopPropagation();
            void this.favCtrl.toggleFavorite(
                hit.track.FilePath,
            );

            return;
        }

        this.onTrackRowClick(e, hit.track, hit.index);
    };

    private onDelegatedDblClick = (e: MouseEvent) => {
        const hit = this.resolveTrackFromEvent(e);

        if (hit) this.onTrackRowDblClick(hit.track);
    };

    private onDelegatedContextMenu = (e: MouseEvent) => {
        const hit = this.resolveTrackFromEvent(e);

        if (hit) this.onTrackContextMenu(e, hit.track);
    };

    private onDelegatedDragStart = (e: DragEvent) => {
        const hit = this.resolveTrackFromEvent(e);

        if (hit) this.onTrackDragStart(e, hit.track);
    };

    // =================================================================
    // Original handlers (still used internally)
    // =================================================================

    private onTrackRowClick(
        e: MouseEvent,
        track: library.Track,
        index: number,
    ) {
        this.selection.handleItemClick(e, track.FilePath, index);
    }

    private onTrackRowDblClick(track: library.Track) {
        this.selection.clear();
        queueStore.setQueue([track.FilePath], 0);
    }

    private onTrackContextMenu(e: MouseEvent, track: library.Track) {
        e.preventDefault();
        e.stopPropagation();

        this.selection.handleContextMenu(track.FilePath);
        this.ctxMenu.openAt(e.clientX, e.clientY);
    }

    // =================================================================
    // Drag source
    // =================================================================

    private onTrackDragStart = (
        e: DragEvent,
        track: library.Track,
    ) => {
        // Gather file paths: all selected if this track is selected,
        // otherwise just the dragged track.
        let filePaths: string[];

        if (this.selection.isSelected(track.FilePath)) {
            filePaths =
                this.selection.getSelectedKeysOrdered();
        } else {
            filePaths = [track.FilePath];
        }

        if (filePaths.length === 0) return;

        setDragPayload(e, {
            filePaths,
            source: 'track-list',
        });

        // Custom drag image.
        this.dragImageEl =
            filePaths.length === 1
                ? createTrackCardDragImage(
                      track.TrackName,
                      track.ArtistName,
                      track.FilePath,
                  )
                : createDragImage(filePaths.length);
        e.dataTransfer?.setDragImage(
            this.dragImageEl,
            0,
            0,
        );

        emitDragActive(true);
    };

    private onTrackDragEnd = () => {
        if (this.dragImageEl) {
            removeDragImage(this.dragImageEl);
            this.dragImageEl = null;
        }

        emitDragActive(false);
    };

    private onContextMenuAction(action: string) {
        const filePaths =
            this.selection.getSelectedKeysOrdered();

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                queueStore.setQueue(filePaths, 0, true);
                break;
            case 'add-to-queue':
                queueStore.addTracksToQueue(filePaths);
                break;
            case 'play-next':
                queueStore.playTracksNext(filePaths);
                break;
            case 'track-details':
                if (filePaths.length === 1) {
                    this.openTrackDetails(filePaths[0]!);
                } else {
                    this.openBatchTrackDetails(filePaths);
                }
                break;
        }

        this.selection.clear();
        this.ctxMenu.close();
    }

    private onContextMenuFavoriteToggle() {
        const filePaths =
            this.selection.getSelectedKeysOrdered();

        if (filePaths.length === 0) return;

        if (this.favCtrl.allFavorited(filePaths)) {
            void this.favCtrl.removeFromFavorites(
                filePaths,
            );
        } else {
            void this.favCtrl.addToFavorites(
                filePaths,
            );
        }

        this.selection.clear();
        this.ctxMenu.close();
    }

    private openTrackDetails(filePath: string) {
        const track = this.tracks.find(
            (t) => t.FilePath === filePath,
        );

        if (!track) return;

        const coverArt = track.CoverArtPath
            ? {
                coverArtPath: track.CoverArtPath,
                coverArtSmall: track.CoverArtSmall,
                coverArtMedium: track.CoverArtMedium,
                coverArtLarge: track.CoverArtLarge,
            }
            : undefined;

        this.trackDetailsDialog?.show(
            track,
            coverArt,
        );
    }

    private openBatchTrackDetails(
        filePaths: string[],
    ) {
        const tracks = filePaths
            .map((fp) =>
                this.tracks.find(
                    (t) => t.FilePath === fp,
                ),
            )
            .filter(
                (t): t is library.Track => t != null,
            );

        if (tracks.length === 0) return;

        // Use cover art from the first track. If all tracks share
        // the same album, they share the same art.
        const first = tracks[0]!;
        let coverArt: CoverArtUrls | null = null;
        let coverArtMixed = false;

        const albumNames = new Set(tracks.map((t) => t.Album));

        if (albumNames.size === 1 && first.CoverArtPath) {
            coverArt = {
                coverArtPath: first.CoverArtPath,
                coverArtSmall: first.CoverArtSmall,
                coverArtMedium: first.CoverArtMedium,
                coverArtLarge: first.CoverArtLarge,
            };
        } else if (albumNames.size > 1) {
            coverArtMixed = true;
        }

        this.trackDetailsDialog?.showBatch(
            tracks,
            coverArt,
            coverArtMixed,
        );
    }

    // =================================================================
    // Sort controls
    // =================================================================

    /** Restore sort preferences from localStorage. */
    private restoreSortPreferences() {
        try {
            const field =
                localStorage.getItem(SORT_FIELD_KEY);
            const dir =
                localStorage.getItem(SORT_DIR_KEY);

            if (
                field &&
                COLUMN_DEFS[field]?.comparator
            ) {
                this.sortField = field;
            }

            if (dir === 'asc' || dir === 'desc') {
                this.sortDirection = dir;
            }
        } catch {
            // Ignore storage errors.
        }
    }

    /** Persist sort preferences to localStorage. */
    private saveSortPreferences() {
        try {
            if (this.sortField) {
                localStorage.setItem(
                    SORT_FIELD_KEY,
                    this.sortField,
                );
            } else {
                localStorage.removeItem(
                    SORT_FIELD_KEY,
                );
            }

            localStorage.setItem(
                SORT_DIR_KEY,
                this.sortDirection,
            );
        } catch {
            // Ignore storage errors.
        }
    }

    /**
     * Handle a click on a column header to toggle sorting.
     * First click: sort ascending. Second: descending.
     * Third: clear sort (back to default order).
     */
    private onHeaderCellClick(colId: string) {
        const col = COLUMN_DEFS[colId];

        if (!col?.comparator) return;

        if (this.sortField === colId) {
            if (this.sortDirection === 'asc') {
                this.sortDirection = 'desc';
            } else {
                this.sortField = null;
                this.sortDirection = 'asc';
            }
        } else {
            this.sortField = colId;
            this.sortDirection = 'asc';
        }

        this.saveSortPreferences();
    }

    /** Set sort from the dropdown and close it. */
    private onSortDropdownSelect(
        colId: string | null,
    ) {
        if (colId === null) {
            this.sortField = null;
            this.sortDirection = 'asc';
        } else {
            this.sortField = colId;
        }

        this.saveSortPreferences();
        this.closeSortDropdown();
    }

    /** Toggle sort direction via the toolbar button. */
    private toggleSortDirection() {
        this.sortDirection =
            this.sortDirection === 'asc'
                ? 'desc'
                : 'asc';
        this.saveSortPreferences();
    }

    private toggleSortDropdown() {
        if (this.sortDropdownOpen) {
            this.closeSortDropdown();
        } else {
            this.openSortDropdown();
        }
    }

    private async openSortDropdown() {
        this.sortDropdownOpen = true;

        await this.updateComplete;

        const popup = this.sortDropdownPopup;
        const anchor = this.shadowRoot?.querySelector(
            '.sort-anchor',
        );

        if (popup && anchor) {
            popup.anchor = anchor;
            popup.active = true;
        }
    }

    private closeSortDropdown() {
        if (!this.sortDropdownOpen) return;

        this.sortDropdownOpen = false;

        const popup = this.sortDropdownPopup;

        if (popup) {
            popup.active = false;
        }
    }

    private sortDropdownCloseHandler = (
        e: MouseEvent,
    ) => {
        if (!this.sortDropdownOpen) return;

        const path = e.composedPath();
        const popup = this.sortDropdownPopup;

        if (popup && path.includes(popup)) return;

        const anchor =
            this.shadowRoot?.querySelector(
                '.sort-anchor',
            );

        if (anchor && path.includes(anchor)) return;

        this.closeSortDropdown();
    };

    private isActiveTrack(track: library.Track): boolean {
        const currentTrack = this.player.currentTrack;

        if (!currentTrack) return false;

        return currentTrack.filePath === track.FilePath;
    }

    private renderTrackRow = (
        track: library.Track,
        index: number,
    ): unknown => {
        const active = this.isActiveTrack(track);
        const selected = this.selection.isSelected(
            track.FilePath,
        );

        const cols = this.activeColumns;

        const isFav = this.favCtrl.isFavorited(
            track.FilePath,
        );

        // Inline SVG instead of wa-icon — eliminates a shadow DOM tree
        // per visible row (~30-50 during scroll).
        const iconDef = FAV_ICONS[this.favCtrl.iconStyle === 'star' ? 'star' : 'heart'];
        const iconPath = isFav ? iconDef.solid : iconDef.regular;

        // No inline closures — all events delegated via data-index
        // on the virtualizer element (see firstUpdated).
        const term = this.searchCtrl.term;
        return html`
      <div
        class=${classMap({
            'track-row': true,
            active,
            selected,
        })}
        draggable="true"
        data-index=${index}
        data-testid="track-row"
        data-file-path=${track.FilePath}
      >
        <div
          class=${classMap({
            'fav-icon': true,
            favorited: isFav,
        })}
        >
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${iconDef.viewBox.split(' ').slice(2).join(' ')}" width="14" height="14">
            ${svg`<path fill="currentColor" d="${iconPath}"/>`}
          </svg>
        </div>
        ${cols.map((col) => {
                const customCell = col.renderCell?.(track);
                if (customCell !== undefined && customCell !== nothing) {
                    return html`<div class="cell">${customCell}</div>`;
                }
                const val = col.accessor(track);
                const centered = val === '\u2014';
                let display: unknown = term
                    ? highlightText(val, term)
                    : val;

                // Wrap artist/album/track values in explore links.
                if (col.id === 'trackName') {
                    display = trackLink(track.TrackName, track.Album, track.ReleaseGroupMBID, track.RecordingMBID, display as any);
                } else if (col.id === 'artistName') {
                    display = artistLink(track.ArtistName, track.ArtistMBID, display as any);
                } else if (col.id === 'album') {
                    display = albumLink(track.Album, track.ReleaseGroupMBID, display as any);
                }

                return html`
                    <div class=${classMap({
                        cell: true,
                        'cell-center': centered,
                        'cell-right': !centered && col.align === 'right',
                    })}>
                        ${display}
                    </div>
                `;
            })}
      </div>
    `;
    };

    /** Render the sort toolbar above the header row. */
    private renderSortToolbar() {
        const activeCol = this.sortField
            ? COLUMN_DEFS[this.sortField]
            : null;

        const label = activeCol
            ? activeCol.label
            : 'Default';

        const dirIcon =
            this.sortDirection === 'asc'
                ? 'arrow-up-short-wide'
                : 'arrow-down-wide-short';

        return html`
            <div class="sort-toolbar">
                <span>Sort:</span>
                <button
                    class="sort-anchor"
                    @click=${() =>
                        this.toggleSortDropdown()}
                >
                    <span class="sort-label">
                        ${label}
                    </span>
                    <wa-icon
                        name="chevron-down"
                    ></wa-icon>
                </button>
                ${this.sortField
                    ? html`
                          <button
                              class="sort-dir-btn"
                              title="${this.sortDirection === 'asc' ? 'Ascending' : 'Descending'}"
                              @click=${() =>
                                  this.toggleSortDirection()}
                          >
                              <wa-icon
                                  name=${dirIcon}
                              ></wa-icon>
                          </button>
                      `
                    : nothing}
                ${this.searchCtrl.term
                    ? html`<div class="search-indicator">
                          Showing results for
                          &ldquo;${this.searchCtrl.term}&rdquo;
                      </div>`
                    : nothing}
            </div>
            ${this.renderSortDropdownPopup()}
        `;
    }

    /** Render the sort dropdown popup. */
    private renderSortDropdownPopup() {
        const cols = this.activeColumns;

        return html`
            <wa-popup
                id="sort-dropdown"
                placement="bottom-start"
                flip
                shift
                .active=${this.sortDropdownOpen}
            >
                ${this.sortDropdownOpen
                    ? html`
                          <div
                              class="sort-dropdown-panel"
                          >
                              <wa-dropdown-item
                                  class=${!this.sortField ? 'active-sort' : ''}
                                  @click=${() =>
                                      this.onSortDropdownSelect(
                                          null,
                                      )}
                              >
                                  Default
                              </wa-dropdown-item>
                              ${cols
                                  .filter(
                                      (c) =>
                                          c.comparator,
                                  )
                                  .map(
                                      (col) => html`
                                      <wa-dropdown-item
                                          class=${this.sortField === col.id ? 'active-sort' : ''}
                                          @click=${() =>
                                              this.onSortDropdownSelect(
                                                  col.id,
                                              )}
                                      >
                                          ${col.label}
                                      </wa-dropdown-item>
                                  `,
                                  )}
                          </div>
                      `
                    : nothing}
            </wa-popup>
        `;
    }

    override render() {
        const visibleTracks = this.cachedSortedTracks;
        const cols = this.activeColumns;

        return html`
      ${this.tracks.length === 0
                ? html`<p>Loading tracks...</p>`
                : html`
            ${this.renderSortToolbar()}
            <div class="table-container">
            <div class="header-row">
              <div></div>
              ${cols.map(
                    (col) => html`
                <div
                  class="header-cell ${col.align === 'right' ? 'cell-right' : ''}"
                  @click=${() =>
                          this.onHeaderCellClick(
                              col.id,
                          )}
                >
                  <span>${col.label}</span>
                  ${this.sortField === col.id
                              ? html`<span
                            class="sort-arrow"
                        >
                            ${this.sortDirection === 'asc' ? '\u25B2' : '\u25BC'}
                        </span>`
                              : nothing}
                </div>
              `,
                )}
            </div>
            ${visibleTracks.length === 0
                        ? html`<p class="no-results">
                      No tracks match your search.
                    </p>`
                        : html`
                      <lit-virtualizer
                        scroller
                        .items=${visibleTracks}
                        .renderItem=${this.renderTrackRow}
                        .keyFunction=${(track: library.Track) => track.FilePath}
                        .layout=${this.flowLayout}
                      ></lit-virtualizer>
                    `}

      <div class="resize-overlay">
        ${this.colBoundaryPositions.map(
                        (pos, i) => html`
            <div
              class="col-resize-handle ${this.resizingColumn === i ? 'active' : ''}"
              style="left: ${pos}px"
              @mousedown=${(e: MouseEvent) =>
                            this.onColResizeStart(e, i)}
            ></div>
          `,
                    )}
      </div>
            </div>
          `}

      <wa-popup
        id="context-menu"
        placement="bottom-start"
        flip
        shift
        .active=${this.ctxMenu.contextMenuOpen}
      >
        ${this.ctxMenu.contextMenuOpen
                ? html`
              <div class="context-menu-panel">
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('play')}
                   @mouseenter=${() => this.ctxMenu.closePlaylistSubmenu()}
                >
                  <wa-icon slot="icon" name="play"></wa-icon>
                  Play
                </wa-dropdown-item>
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('add-to-queue')}
                  @mouseenter=${() => this.ctxMenu.closePlaylistSubmenu()}
                >
                  <wa-icon slot="icon" name="plus"></wa-icon>
                  Add to Queue
                </wa-dropdown-item>
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('play-next')}
                  @mouseenter=${() => this.ctxMenu.closePlaylistSubmenu()}
                >
                  <wa-icon slot="icon" name="forward-step"></wa-icon>
                  Play Next
                </wa-dropdown-item>
                <wa-dropdown-item
                  class="submenu-item"
                  @mouseenter=${() => {
                        this.ctxMenu.clearSubmenuCloseTimer();
                        void this.ctxMenu.showPlaylistSubmenu(this.selection.getSelectedKeysOrdered());
                    }}
                  @mouseleave=${this.ctxMenu.scheduleSubmenuClose}
                  @click=${(e: Event) => {
                        e.stopPropagation();
                        void this.ctxMenu.showPlaylistSubmenu(this.selection.getSelectedKeysOrdered());
                    }}
                >
                  <wa-icon slot="icon" name="plus"></wa-icon>
                  Add to Playlist
                  <span class="submenu-arrow">&#9654;</span>
                </wa-dropdown-item>
                <wa-dropdown-item
                  @click=${() =>
                        this.onContextMenuFavoriteToggle()}
                  @mouseenter=${() =>
                        this.ctxMenu.closePlaylistSubmenu()}
                >
                  <wa-icon
                    slot="icon"
                    name=${this.favCtrl.iconName}
                  ></wa-icon>
                  ${this.favCtrl.allFavorited(this.selection.getSelectedKeysOrdered()) ? `Remove from ${this.favCtrl.playlistName}` : `Add to ${this.favCtrl.playlistName}`}
                </wa-dropdown-item>
                <wa-dropdown-item
                    @click=${() =>
                        this.onContextMenuAction(
                            'track-details',
                        )}
                     @mouseenter=${() =>
                         this.ctxMenu.closePlaylistSubmenu()}
                >
                    <wa-icon
                        slot="icon"
                        name="circle-info"
                    ></wa-icon>
                    Track Details
                </wa-dropdown-item>
              </div>
            `
                : nothing}
      </wa-popup>

      <wa-popup
        id="playlist-submenu"
        placement="right-start"
        flip
        shift
        .active=${this.ctxMenu.playlistSubmenuOpen}
      >
        ${this.ctxMenu.playlistSubmenuOpen && this.selection.hasSelection
                ? html`
              <div
                @mouseenter=${() =>
                    this.ctxMenu.clearSubmenuCloseTimer()}
                @mouseleave=${this.ctxMenu.scheduleSubmenuClose}
              >
                <playlist-picker
                  .filePaths=${this.ctxMenu.playlistFilePaths}
                  @playlist-action-complete=${this.ctxMenu.onPlaylistActionComplete}
                  @click=${(e: Event) => e.stopPropagation()}
                ></playlist-picker>
              </div>
            `
                : nothing}
      </wa-popup>

      <track-details></track-details>
    `;
    }
}

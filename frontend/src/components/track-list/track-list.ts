import { library } from '@go/models';
import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';
import { SelectionController } from '@utils/selection-controller';
import type { SelectionHost } from '@utils/selection-controller';
import { PlayerController } from '@store/controllers/player-controller';
import { SearchController } from '@store/controllers/search-controller';
import { TrackListController } from '@store/controllers/tracklist-controller';
import { queueStore } from '@store/queue-store';
import { LibraryController } from '@store/controllers/library-controller';
import { Events } from '../../events';
import {
    COLUMN_DEFS,
    DEFAULT_COLUMN_IDS,
} from './columns';
import type { ColumnDef } from './columns';
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
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';

const COLUMN_STORAGE_KEY = 'track-list-column-widths';
const SORT_FIELD_KEY = 'track-list-sort-field';
const SORT_DIR_KEY = 'track-list-sort-direction';
const MIN_COLUMN_WIDTH = 50;
const DEFAULT_FIXED_WIDTH = 80;

type SortDirection = 'asc' | 'desc';

@customElement('track-list')
export class TrackList extends LitElement implements SelectionHost {
    private player = new PlayerController(this);
    private libraryCtrl = new LibraryController(this);
    private searchCtrl = new SearchController(this);
    private trackListCtrl = new TrackListController(this);
    private selection = new SelectionController(this);
    private cancelScanComplete?: () => void;
    private lastSearchTerm = '';

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

    @state()
    private contextMenuOpen = false;

    @state()
    private playlistSubmenuOpen = false;

    @query('#context-menu')
    private contextMenuPopup!: HTMLElement;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: HTMLElement;

    @query('lit-virtualizer')
    private virtualizer!: LitVirtualizer;

    private lastActiveTrackPath: string | null = null;

    private closeHandler = () => this.closeContextMenu();

    private mousedownCloseHandler = (
        e: MouseEvent,
    ) => {
        const path = e.composedPath();
        const popup = this.contextMenuPopup;
        const submenu = this.playlistSubmenuPopup;

        if (popup && path.includes(popup)) return;
        if (submenu && path.includes(submenu)) return;

        this.closeContextMenu();
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
    private sortDropdownPopup!: HTMLElement;

    private resizingColumn: number | null = null;
    private resizeStartX = 0;
    private resizeStartWidths: number[] = [];
    private resizeObserver: ResizeObserver | null =
        null;

    private flowLayout = flow();
    private hasRestoredScroll = false;

    // =================================================================
    // Filtered tracks (search)
    // =================================================================

    private get filteredTracks(): library.Track[] {
        const term = this.searchCtrl.term.toLowerCase();

        if (!term) return this.tracks;

        const cols = this.activeColumns;

        return this.tracks.filter((t) =>
            cols.some((col) =>
                col
                    .accessor(t)
                    .toLowerCase()
                    .includes(term),
            ),
        );
    }

    // =================================================================
    // Sorted tracks
    // =================================================================

    private get sortedTracks(): library.Track[] {
        const tracks = this.filteredTracks;

        if (!this.sortField) return tracks;

        const col = COLUMN_DEFS[this.sortField];

        if (!col?.comparator) return tracks;

        const dir =
            this.sortDirection === 'asc' ? 1 : -1;

        return [...tracks].sort(
            (a, b) => dir * col.comparator!(a, b),
        );
    }

    // =================================================================
    // SelectionHost interface
    // =================================================================

    getItemKey(index: number): string | undefined {
        return this.sortedTracks[index]?.FilePath;
    }

    getItemCount(): number {
        return this.sortedTracks.length;
    }

    onSelectionChanged(): void {
        this.virtualizer?.requestUpdate();
    }

    private get gridTemplateColumns(): string {
        const cols = this.activeColumns;

        if (this.columnWidths.length === 0) {
            return cols
                .map((c) => c.defaultWidth)
                .join(' ');
        }

        return this.columnWidths
            .map((w) => `${w}px`)
            .join(' ');
    }

    private get colBoundaryPositions(): number[] {
        if (this.columnWidths.length === 0) return [];

        const padding = 8;
        const positions: number[] = [];
        let cumulative = padding;

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

        if (saved) {
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

    static override styles = css`
    :host {
      display: flex;
      flex-direction: column;
      overflow: hidden;
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
      font-size: 12px;
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
      font-size: 12px;
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
      font-size: 13px;
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
      font-size: 10px;
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

    .search-indicator {
      position: absolute;
      top: 8px;
      left: 50%;
      transform: translateX(-50%);
      z-index: 5;
      pointer-events: none;
      background: var(--yj-bg-overlay, #495057);
      color: var(--yj-text-secondary, #b3b3b3);
      font-size: 12px;
      padding: 4px 14px;
      border-radius: 12px;
      border: 1px solid var(--yj-border-subtle, #555);
      white-space: nowrap;
      opacity: 0.92;
    }

    .no-results {
      padding: 24px 16px;
      color: var(--yj-text-secondary, #b3b3b3);
      font-size: 13px;
    }

    lit-virtualizer {
      flex: 1;
      overflow-x: hidden;
      overflow-y: auto;
      user-select: none;
    }

    .track-row {
      display: grid;
      grid-template-columns: var(--grid-cols);
      font-size: 12px;
      padding: 8px;
      border-bottom: 1px solid var(--yj-border-subtle, #333);
      align-items: center;
      width: 100%;
      cursor: default;
      user-select: none;
      overflow: hidden;
    }

    .track-row > * {
      min-width: 0;
    }

    .header-cell + .header-cell,
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

    #context-menu {
      z-index: 200;
    }

    .context-menu-panel {
      background-color: var(--yj-bg-elevated, #343a40);
      border: 1px solid var(--yj-border, #444);
      border-radius: 6px;
      padding: 4px 0;
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
      min-width: 160px;
    }

    .context-menu-panel wa-dropdown-item {
      cursor: pointer;
    }

    .context-menu-panel wa-dropdown-item {
      --wa-color-text-normal: var(--yj-text-primary, #fff);
      font-size: 13px;
    }

    .context-menu-panel wa-dropdown-item:hover {
      background-color: var(--yj-hover-overlay, rgba(255, 255, 255, 0.1));
    }

    .submenu-item {
      position: relative;
    }

    .submenu-arrow {
      font-size: 10px;
      margin-left: auto;
      padding-left: 12px;
    }

    #playlist-submenu {
      z-index: 210;
    }
  `;

    override connectedCallback() {
        super.connectedCallback();
        this.restoreSortPreferences();
        this.loadTracks();
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            () => this.loadTracks(),
        );
        document.addEventListener('click', this.closeHandler);
        document.addEventListener('contextmenu', this.closeHandler);
        document.addEventListener('mousedown', this.mousedownCloseHandler);
        document.addEventListener('mousedown', this.sortDropdownCloseHandler);
        document.addEventListener('click', this.clearSelectionHandler);
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
        this.virtualizer?.removeEventListener(
            'visibilityChanged',
            this.onVisibilityChanged,
        );
        this.hasRestoredScroll = false;
        super.disconnectedCallback();
        this.cancelScanComplete?.();
        document.removeEventListener('click', this.closeHandler);
        document.removeEventListener('contextmenu', this.closeHandler);
        document.removeEventListener('mousedown', this.mousedownCloseHandler);
        document.removeEventListener('mousedown', this.sortDropdownCloseHandler);
        document.removeEventListener('click', this.clearSelectionHandler);
        document.removeEventListener('mousemove', this.onColResizeMove);
        document.removeEventListener('mouseup', this.onColResizeEnd);

        this.resizeObserver?.disconnect();
        this.resizeObserver = null;
    }

    override firstUpdated() {
        this.initColumnWidths();
    }

    override updated(changed: Map<string, unknown>) {
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

        this.libraryCtrl.setScrollPosition('tracks', first);
    };

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
        this.contextMenuOpen = true;

        // Position the popup at the mouse cursor using a virtual anchor.
        this.updateComplete.then(() => {
            const popup = this.contextMenuPopup;

            if (popup) {
                (popup as any).anchor = {
                    getBoundingClientRect() {
                        return {
                            width: 0,
                            height: 0,
                            x: e.clientX,
                            y: e.clientY,
                            top: e.clientY,
                            left: e.clientX,
                            right: e.clientX,
                            bottom: e.clientY,
                        };
                    },
                };
                (popup as any).active = true;
            }
        });
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
        const filePaths = this.selection.getSelectedKeysOrdered();

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                queueStore.setQueue(filePaths, 0);
                break;
            case 'add-to-queue':
                queueStore.addTracksToQueue(filePaths);
                break;
            case 'play-next':
                queueStore.playTracksNext(filePaths);
                break;
        }

        this.closeContextMenu(true);
    }

    private closeContextMenu(clearSelection = false) {
        if (!this.contextMenuOpen) return;

        this.closePlaylistSubmenu();
        this.contextMenuOpen = false;

        if (clearSelection) {
            this.selection.clear();
        }

        const popup = this.contextMenuPopup;

        if (popup) {
            (popup as any).active = false;
        }
    }

    private async showPlaylistSubmenu() {
        if (this.playlistSubmenuOpen) return;

        this.playlistSubmenuOpen = true;

        await this.updateComplete;

        const submenu = this.playlistSubmenuPopup;
        const trigger = this.shadowRoot?.querySelector('.submenu-item');

        if (submenu && trigger) {
            (submenu as any).anchor = trigger;
            (submenu as any).active = true;
        }

        const picker = this.shadowRoot?.querySelector(
            'playlist-picker',
        ) as PlaylistPicker | null;

        picker?.reset();
    }

    private closePlaylistSubmenu() {
        if (!this.playlistSubmenuOpen) return;

        this.playlistSubmenuOpen = false;

        const submenu = this.playlistSubmenuPopup;

        if (submenu) {
            (submenu as any).active = false;
        }
    }

    private onPlaylistActionComplete = () => {
        this.closeContextMenu(true);
    };

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
            (popup as any).anchor = anchor;
            (popup as any).active = true;
        }
    }

    private closeSortDropdown() {
        if (!this.sortDropdownOpen) return;

        this.sortDropdownOpen = false;

        const popup = this.sortDropdownPopup;

        if (popup) {
            (popup as any).active = false;
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

        const classes = [
            'track-row',
            active ? 'active' : '',
            selected ? 'selected' : '',
        ]
            .filter(Boolean)
            .join(' ');

        const cols = this.activeColumns;

        return html`
      <div
        class=${classes}
        draggable="true"
        @click=${(e: MouseEvent) =>
                this.onTrackRowClick(e, track, index)}
        @dblclick=${() =>
                this.onTrackRowDblClick(track)}
        @contextmenu=${(e: MouseEvent) =>
                this.onTrackContextMenu(e, track)}
        @dragstart=${(e: DragEvent) =>
                this.onTrackDragStart(e, track)}
        @dragend=${this.onTrackDragEnd}
      >
        ${cols.map(
                (col) => html`
            <div class="cell ${col.align === 'right' ? 'cell-right' : ''}">
              ${col.accessor(track)}
            </div>
          `,
            )}
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
        const visibleTracks = this.sortedTracks;
        const cols = this.activeColumns;

        return html`
      ${this.tracks.length === 0
                ? html`<p>Loading tracks...</p>`
                : html`
            ${this.renderSortToolbar()}
            <div class="table-container">
            <div class="header-row">
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
                        .layout=${this.flowLayout}
                      ></lit-virtualizer>
                    `}

      ${this.searchCtrl.term && visibleTracks.length > 0
                        ? html`<div class="search-indicator">
                Showing results for
                &ldquo;${this.searchCtrl.term}&rdquo;
            </div>`
                        : nothing}

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
        .active=${this.contextMenuOpen}
      >
        ${this.contextMenuOpen
                ? html`
              <div class="context-menu-panel">
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('play')}
                >
                  <wa-icon slot="icon" name="play"></wa-icon>
                  Play
                </wa-dropdown-item>
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('add-to-queue')}
                >
                  <wa-icon slot="icon" name="plus"></wa-icon>
                  Add to Queue
                </wa-dropdown-item>
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('play-next')}
                >
                  <wa-icon slot="icon" name="forward-step"></wa-icon>
                  Play Next
                </wa-dropdown-item>
                <wa-dropdown-item
                  class="submenu-item"
                  @mouseenter=${() => this.showPlaylistSubmenu()}
                  @click=${(e: Event) => {
                        e.stopPropagation();
                        void this.showPlaylistSubmenu();
                    }}
                >
                  <wa-icon slot="icon" name="plus"></wa-icon>
                  Add to Playlist
                  <span class="submenu-arrow">&#9654;</span>
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
        .active=${this.playlistSubmenuOpen}
      >
        ${this.playlistSubmenuOpen && this.selection.hasSelection
                ? html`
              <playlist-picker
                .filePaths=${this.selection.getSelectedKeysOrdered()}
                @playlist-action-complete=${this.onPlaylistActionComplete}
                @click=${(e: Event) => e.stopPropagation()}
              ></playlist-picker>
            `
                : nothing}
      </wa-popup>
    `;
    }
}

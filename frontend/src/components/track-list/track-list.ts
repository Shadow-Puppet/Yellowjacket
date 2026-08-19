import * as library from '@go/library/models.js';
import { LitElement, html, svg, css, nothing } from 'lit';
import { designTokens } from '../../styles/tokens.css';
import { srOnly } from '../../styles/sr-only.css';
import {
    customElement,
    property,
    state,
    query,
} from 'lit/decorators.js';
import { SelectionController } from '@utils/selection-controller';
import type { SelectionHost } from '@utils/selection-controller';
import { ViewLifecycleMixin } from '@utils/view-lifecycle';
import { PHONE_QUERY } from '@utils/breakpoints';
import {
    ContextMenuController,
    contextMenuStyles,
    isContextMenuKey,
} from '@utils/context-menu-controller.js';

import type { ContextMenuHost } from '@utils/context-menu-controller.js';
import { PlayerController } from '@store/controllers/player-controller';
import { SearchController } from '@store/controllers/search-controller';
import '@components/page-header/page-header';
import type { SortOption } from '@components/page-header/page-header';
import { TrackListController } from '@store/controllers/tracklist-controller';
import { FavoritesController } from '@store/controllers/favorites-controller';
import { queueStore } from '@store/queue-store';
import { creditStore } from '@store/credit-store';
import type { QueueSource } from '@store/queue-store';
import { LibraryController } from '@store/controllers/library-controller';
import {
    COLUMN_DEFS,
    DEFAULT_COLUMN_IDS,
    PHONE_COLUMN_IDS,
} from './columns';
import type { ColumnDef } from './columns';
import { classMap } from 'lit/directives/class-map.js';
import {
    rankTracks,
    highlightText,
} from './search-ranking';
import {
    artistLink,
    creditLink,
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
import { describeError } from '@utils/describe-error';
import { notificationStore } from '@store/notification-store';
import { confirmAction } from '@components/confirm-dialog/confirm-dialog';
import { RemoveFromLibrary } from '@go/library/library.js';
import { loadTrackDetails } from '@utils/lazy-track-details.js';
import { tracksByFilePath, tracksForPaths } from '@utils/track-index.js';
import '@components/playlist-picker/playlist-picker.js';
import type { TrackDetails } from '@components/track-details/track-details.js';
import type { CoverArtUrls } from '@components/track-details/track-details.js';
import {
    ICON_PLAYLIST,
    ICON_QUEUE,
} from '@utils/icon-language';

const COLUMN_STORAGE_KEY = 'track-list-column-widths';
const SORT_FIELD_KEY = 'track-list-sort-field';
const SORT_DIR_KEY = 'track-list-sort-direction';
const MIN_COLUMN_WIDTH = 50;
const DEFAULT_FIXED_WIDTH = 80;

/**
 * Chrome the resizable columns cannot use: the fixed favourite column
 * and the row's own horizontal padding.  Both numbers also appear in
 * the CSS below — the `24px` first track of `--grid-cols` and
 * `.track-row`/`.header-row`'s `padding: 8px` — so they live here and
 * are read from both places rather than written out four times.
 */
const FAV_COL_WIDTH = 24;
const ROW_PADDING_X = 8;
const ROW_CHROME_WIDTH =
    FAV_COL_WIDTH + ROW_PADDING_X * 2;

/**
 * Row heights, in the same relationship as the widths above: the number
 * is read by the CSS *and* by the virtualizer's layout, so they cannot
 * disagree. A phone row is two lines (title over artist).
 */
const ROW_HEIGHT = 33;
const PHONE_ROW_HEIGHT = 52;


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
export class TrackList
    extends ViewLifecycleMixin(LitElement)
    implements SelectionHost, ContextMenuHost
{
    /* Claimed while this list is the view on screen, which is what makes
     * the `tracklist.*` bindings resolve at all: `data-shortcut-scope`
     * was read by the shortcut service and set by nobody, so Enter and
     * Delete were dead shortcuts the Settings page still advertised. */
    protected override shortcutScope = 'tracklist';

    /**
     * When set, the list displays these tracks instead of
     * fetching all tracks from the library store.  The
     * parent is responsible for reloading when data changes.
     */
    @property({ type: Array, attribute: false })
    externalTracks?: library.Track[];

    /**
     * What a host embedding this list (e.g. `genre-details`) should say
     * a queue built from it came from. Unset when this list is showing
     * the whole library — the one case with no host to ask, and where
     * `effectiveQueueSource` supplies "All Tracks" itself.
     */
    @property({ attribute: false })
    queueSource?: QueueSource;

    /**
     * The library's own top-level Tracks view has no host to name a
     * source — it *is* the source. Anything embedding this list with
     * `externalTracks` is expected to set `queueSource` itself; if it
     * doesn't, the queue is left undescribed rather than mislabeled.
     */
    private get effectiveQueueSource(): QueueSource | undefined {
        if (this.queueSource) return this.queueSource;

        return this.externalTracks
            ? undefined
            : { type: 'tracks', id: 0, label: 'All Tracks' };
    }

    /**
     * Loading, empty and failed are three different things, and this
     * list used to render all three as a permanent “Loading tracks…”
     * — including on the first screen a new user ever sees, behind the
     * first-run wizard (errors.M2, H-12). `home-view` is the model.
     */
    @state() private loadingTracks = false;
    @state() private loadError = '';

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
    /**
     * The columns the user has chosen — what a desktop draws, and what
     * *anything* may be sorted by.
     *
     * This is deliberately separate from `activeColumns`: "which columns
     * are drawn" and "what can I sort by" are different questions, and
     * the phone is exactly where they diverge. Building the sort list
     * from the drawn columns would silently take sort-by-artist and
     * sort-by-album away from the phone, which has no other route to
     * them since it has no column headers either.
     */
    private get configuredColumns(): ColumnDef[] {
        const ids = this.trackListCtrl.columnIds;
        const chosen = !ids || ids.length === 0 ? DEFAULT_COLUMN_IDS : ids;

        return chosen
            .map((id) => COLUMN_DEFS[id])
            .filter(
                (d): d is ColumnDef =>
                    d !== undefined,
            );
    }

    /** The columns actually drawn: two stacked lines on a phone. */
    private get activeColumns(): ColumnDef[] {
        if (!this.phone) return this.configuredColumns;

        return PHONE_COLUMN_IDS
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

    /** The row that holds the list's single tab stop.
     *
     *  A grid of ten thousand rows must not be ten thousand tab stops,
     *  so one row is focusable at a time and the arrows move which — the
     *  standard roving tabindex.  Before this the list had no keyboard
     *  path into it at all (H-5). */
    @state() private focusedIndex = 0;

    private handleSelectAll = (): void => {
        this.selection.selectAll();
    };

    /** Arrow/Home/End move the focused row; Enter plays it.
     *
     *  Enter is not handled here — it is the `tracklist.play` binding,
     *  which resolves because the list claims the `tracklist` shortcut
     *  scope while it is on screen. */
    private onListKeydown = (e: KeyboardEvent): void => {
        const last = this.cachedSortedTracks.length - 1;

        if (last < 0) return;

        let next = this.focusedIndex;

        // The menu is most of what a row can do, and it was reachable
        // only by right-click (a11y.3).
        if (isContextMenuKey(e)) {
            const track = this.cachedSortedTracks[this.focusedIndex];
            const row = e.target instanceof HTMLElement
                ? e.target.closest<HTMLElement>('[role="row"]')
                : null;

            if (!track || !row) return;

            e.preventDefault();
            e.stopPropagation();
            this.selection.handleContextMenu(track.FilePath);
            this.ctxMenu.openFrom(row);

            return;
        }

        switch (e.key) {
            case 'ArrowDown':
                next = Math.min(this.focusedIndex + 1, last);
                break;
            case 'ArrowUp':
                next = Math.max(this.focusedIndex - 1, 0);
                break;
            case 'Home':
                next = 0;
                break;
            case 'End':
                next = last;
                break;
            case ' ':
            case 'Enter':
                return;
            default:
                return;
        }

        e.preventDefault();
        e.stopPropagation();
        this.focusRow(next, { select: e.shiftKey || !e.ctrlKey });
    };

    /** Move the roving tab stop, bringing the row into view and
     *  selecting it so Enter and the context menu have a subject. */
    private focusRow(
        index: number,
        opts: { select?: boolean } = {},
    ): void {
        const track = this.cachedSortedTracks[index];

        if (!track) return;

        this.focusedIndex = index;

        if (opts.select) {
            this.selection.clear();
            this.selection.handleItemClick(
                new MouseEvent('click'),
                track.FilePath,
                index,
            );
        }

        this.virtualizer?.scrollToIndex(index, 'nearest');
        void this.updateComplete.then(() => {
            this.shadowRoot
                ?.querySelector<HTMLElement>(
                    `.track-row[data-index="${index}"]`,
                )
                ?.focus();
        });
    }

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
    /**
     * The virtualizer's item size and the CSS row height are the same
     * number in two places, and they must agree: the layout positions
     * rows from this figure, so a row that is really taller overlaps its
     * neighbour and a shorter one leaves a gap. Both come from here.
     */
    private flowLayout = flow({
        _itemSize: { width: 100, height: ROW_HEIGHT },
    } as Parameters<typeof flow>[0]);

    private phoneFlowLayout = flow({
        _itemSize: { width: 100, height: PHONE_ROW_HEIGHT },
    } as Parameters<typeof flow>[0]);

    private get rowLayout(): Parameters<typeof flow>[0] {
        return this.phone ? this.phoneFlowLayout : this.flowLayout;
    }

    /**
     * Phone width, from the shell's own breakpoint.
     *
     * A media query *inside* a shadow root is answered by the viewport,
     * which is what lets every other component state what it drops at
     * phone width in its own stylesheet. This list cannot: its grid is
     * computed in JS from the host width, so the same threshold has to
     * be readable from JS as well. One breakpoint, two expressions of
     * it, and the reason is written here rather than inferred.
     */
    @state()
    private phone = matchMedia(PHONE_QUERY).matches;

    private phoneQuery = matchMedia(PHONE_QUERY);

    private onPhoneChange = (e: MediaQueryListEvent): void => {
        this.phone = e.matches;
    };
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
        const favCol = `${FAV_COL_WIDTH}px`;

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

        const positions: number[] = [];
        let cumulative =
            ROW_PADDING_X + FAV_COL_WIDTH;

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
        // A phone's widths are never the saved ones. `loadColumnWidths`
        // is keyed by column *id* and fills a gap with
        // `MIN_COLUMN_WIDTH`, so the phone's stacked column -- which
        // nothing has ever saved a width for, there being no handles to
        // drag -- came out at the minimum while the duration column
        // inherited a width saved for a four-column desktop row. Found
        // on the device: `24px 148px 236px`, the duration column with
        // 55% of a phone's row.
        if (this.phone) {
            this.computeDefaultWidths();

            return;
        }

        const saved = this.loadColumnWidths();
        const cols = this.activeColumns;

        if (saved && saved.length === cols.length) {
            this.columnWidths = saved;

            return;
        }

        this.computeDefaultWidths();
    }

    /**
     * Width the resizable columns may share.  H-7: this was the raw
     * `clientWidth`, which the columns then summed to exactly — so
     * every row was `ROW_CHROME_WIDTH` (40 px) wider than the box it
     * had to fit in and the last column was always clipped.
     */
    private get availableColumnWidth(): number {
        return this.clientWidth - ROW_CHROME_WIDTH;
    }

    private computeDefaultWidths() {
        const totalWidth = this.availableColumnWidth;

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
        // And a phone's widths are never *saved*: they are computed from
        // a column set the user did not choose, and writing them would
        // overwrite the width they dragged for the same column on a
        // desktop. Nothing on a phone can resize a column anyway, so
        // this is only reachable by a window crossing the breakpoint.
        if (this.phone) return;

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
     * Scale widths so they sum to exactly the width available to the
     * columns (the host minus the row chrome).
     * Every column is guaranteed at least MIN_COLUMN_WIDTH.
     */
    private normalizeWidths(
        widths: number[],
    ): number[] {
        const container = this.availableColumnWidth;

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

    /** perf.m4: the drag's document listeners exist while it is dragging
     *  and not before, which is the standard pattern and stops every
     *  pointer move in the app calling a handler that guards and
     *  returns. */
    private attachColResizeListeners(on: boolean): void {
        const fn = on ? 'addEventListener' : 'removeEventListener';

        document[fn]('mousemove', this.onColResizeMove as EventListener);
        document[fn]('mouseup', this.onColResizeEnd as EventListener);
    }

    private onColResizeStart = (e: MouseEvent, columnIndex: number) => {
        e.preventDefault();
        this.resizingColumn = columnIndex;
        this.resizeStartX = e.clientX;
        this.resizeStartWidths = [...this.columnWidths];
        this.attachColResizeListeners(true);
        this.requestUpdate();
    };

    private onColResizeMove = (e: MouseEvent) => {
        if (this.resizingColumn === null) return;

        const container = this.availableColumnWidth;

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
        this.attachColResizeListeners(false);

        if (this.resizingColumn === null) return;

        this.resizingColumn = null;
        this.saveColumnWidths();
        this.requestUpdate();
    };

    static override styles = [designTokens, srOnly, contextMenuStyles, exploreLinkStyles, css`
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
      color: var(--yj-accent-text, #ffd43b);
    }

    .sort-arrow {
      /* a11y.34. Was 10px, below the type scale's own floor, with a
         comment acknowledging it. The finding's stated harm — "the sort
         direction is a 10px glyph or nothing" — is half closed already:
         Phase 1 gave these cells an aria-sort, so it is announced. What
         is left is a sighted user reading it, and nothing in the header
         needs it to be smaller than the smallest text in the app. */
      font-size: var(--yj-text-xs, 11px);
      flex-shrink: 0;
      color: var(--yj-accent-text, #ffd43b);
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
      position: relative;
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

    /* A phone row is two lines, and this height must equal
       PHONE_ROW_HEIGHT: the virtualizer positions rows from that number,
       so a taller row overlaps its neighbour and a shorter one gaps. */
    @media (max-width: 599px) {
      .track-row {
        height: 52px;
      }
    }

    .stacked {
      display: flex;
      flex-direction: column;
      justify-content: center;
      gap: 2px;
      min-width: 0;
    }

    .stacked-title,
    .stacked-sub {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .stacked-sub {
      font-size: var(--yj-text-xs);
      color: var(--yj-text-secondary, #b3b3b3);
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
      color: var(--yj-accent-text, #ffd43b);
    }

    /* a11y.22: the playing row was a background tint and a text colour
       and nothing else, so a colour-blind user could not find it
       (WCAG 1.4.1). A triangle drawn in the row's own 8px left padding
       is a *shape* that is present or absent, and it costs no layout:
       the grid columns are computed from the host width and every one
       of them would have had to move for a marker in the flow. Sized
       to that padding rather than to the glyph a font would give. */
    .track-row.active::before {
      content: '';
      position: absolute;
      left: 1px;
      top: 50%;
      transform: translateY(-50%);
      border-left: 5px solid currentColor;
      border-top: 4px solid transparent;
      border-bottom: 4px solid transparent;
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
      color: var(--yj-accent-text, #ffd43b);
    }

    .fav-icon.favorited:hover {
      color: var(--yj-accent-text, #ffd43b);
      opacity: 0.8;
    }

    .search-match {
      background-color: rgba(255, 212, 59, 0.15);
      border-radius: 2px;
    }

    .list-placeholder {
      color: var(--yj-text-secondary, #b3b3b3);
      padding: 1em;
    }

    .placeholder-action {
      background: none;
      border: 1px solid var(--yj-border, #495057);
      border-radius: 4px;
      color: inherit;
      cursor: pointer;
      font: inherit;
      margin-top: 0.5em;
      padding: 4px 10px;
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
        this.resizeObserver = new ResizeObserver(
            () => {
                this.onHostResize();
            },
        );

        this.resizeObserver.observe(this);

        // Connection, not the view lifecycle: this only sets state, so
        // it is harmless (and wanted) while the list is off screen -- a
        // rotation on another view must not leave this one laid out for
        // the wrong width when the user comes back to it.
        this.phoneQuery.addEventListener('change', this.onPhoneChange);
    }

    override disconnectedCallback() {
        this.phoneQuery.removeEventListener('change', this.onPhoneChange);

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
        // A drag interrupted by the list going away still has to clean
        // up after itself; these are no longer registered with the view
        // lifecycle, so nothing else would.
        this.attachColResizeListeners(false);
        super.disconnectedCallback();

        this.resizeObserver?.disconnect();
        this.resizeObserver = null;
    }

    /** Document-level listeners belong to the *visible* list.  A cached
     *  list is never disconnected, so this is the only place they can be
     *  taken down again. */
    protected override onViewActivate(): void {
        this.listenWhileActive(document, 'click', this.clearSelectionHandler);
        this.listenWhileActive(
            document,
            'shortcut:select-all',
            this.handleSelectAll,
        );
        this.listenWhileActive(
            document,
            'shortcut:tracklist-play',
            this.handleShortcutPlay,
        );
        this.listenWhileActive(
            document,
            'shortcut:tracklist-delete',
            this.handleShortcutDelete,
        );

        // Credits arrive after the rows that asked for them.  The
        // virtualizer produces its rows from its *own* properties, so a
        // host re-render alone repaints nothing — the same reason a
        // selection change pushes requestUpdate() into it.
        this.whileActive(
            creditStore.subscribe(() => {
                this.requestUpdate();
                this.virtualizer?.requestUpdate();
            }),
        );
    }

    /**
     * Delete opens the confirmation and does nothing else.
     *
     * That is the whole design of the binding: one keystroke from a
     * focused row, a key that *asks* is defensible and a key that
     * *acts* is not — so this is the same dialog the menu command
     * opens, reached by a different route.
     */
    private handleShortcutDelete = (): void => {
        const filePaths = this.selection.getSelectedKeysOrdered();

        if (filePaths.length === 0) return;

        void this.removeFromLibrary(filePaths);
    };

    /** Enter plays the selection — the `tracklist.play` binding, which
     *  has existed in the defaults and in Settings since it was written
     *  and has never had anything on the other end of it. */
    private handleShortcutPlay = (): void => {
        this.playSelection(this.selection.getSelectedKeysOrdered());
    };

    /**
     * "Play" means the same thing from the menu and from Enter, and it
     * asks how much the user selected. One row is a position in the
     * list - it queues the list from there, exactly as double-clicking
     * does. Several rows are an explicit choice of *those* tracks, so
     * they become the queue on their own (and `shuffleStart` applies,
     * since no one row was named as the place to start).
     */
    private playSelection(filePaths: string[]): void {
        if (filePaths.length === 0) return;

        if (filePaths.length === 1) {
            const index = this.displayIndexOf(filePaths[0]!);

            if (index >= 0) {
                this.playFromRow(index);

                return;
            }
        }

        queueStore.setQueue(filePaths, 0, true, this.effectiveQueueSource);
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
        this.loadingTracks = this.tracks.length === 0;
        this.loadError = '';

        try {
            const tracks = await this.libraryCtrl.getTracks();
            this.tracks = tracks;

            // Keep what is still there. A refetch is not a
            // deselection: this used to clear, so every finished track
            // wiped the user's selection while music played (perf.C2).
            // The keys are file paths, which survive a refetch.
            const present = new Set(tracks.map((t) => t.FilePath));
            this.selection.retain((key) => present.has(key));

            await this.updateComplete;

            if (this.isConnected && this.virtualizer) {
                this.virtualizer.addEventListener(
                    'visibilityChanged',
                    this.onVisibilityChanged,
                );
            }
        } catch (error) {
            console.error('Error loading tracks:', error);
            this.loadError = describeError(
                error,
                'Your tracks could not be loaded.',
            );
        } finally {
            this.loadingTracks = false;
        }
    }

    /**
     * What a screen reader is told about this list, in a sentence.
     *
     * Loading, failed, empty and "n results for a search" were all
     * silent — the list said them in text nobody was watching (a11y.12).
     */
    private liveStatus(visible: number): string {
        if (this.loadError) return this.loadError;

        if (this.loadingTracks) return 'Loading tracks…';

        if (this.tracks.length === 0) return 'No tracks.';

        const term = this.searchCtrl.term.trim();

        if (term === '') return '';

        return visible === 0
            ? `No tracks match “${term}”.`
            : `${visible} track${visible === 1 ? '' : 's'} match “${term}”.`;
    }

    /** Loading / failed / genuinely empty, said apart. */
    private renderPlaceholder() {
        if (this.loadError) {
            return html`
                <div class="list-placeholder" data-testid="track-list-error">
                    <p>${this.loadError}</p>
                    <button
                        type="button"
                        class="placeholder-action"
                        @click=${() => void this.loadTracks()}
                    >
                        Try again
                    </button>
                </div>
            `;
        }

        if (this.loadingTracks) {
            return html`<p class="list-placeholder" data-testid="track-list-loading">
                Loading tracks…
            </p>`;
        }

        return html`<p class="list-placeholder" data-testid="track-list-empty">
            ${this.externalTracks
                ? 'Nothing here yet.'
                : 'No tracks yet — add a folder in Settings to get started.'}
        </p>`;
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

        if (hit) this.onTrackRowDblClick(hit.track, hit.index);
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
        // Clicking is also how the keyboard's starting point is chosen:
        // tabbing back into the list should land where the user was.
        this.focusedIndex = index;
        this.selection.handleItemClick(e, track.FilePath, index);
    }

    private onTrackRowDblClick(_track: library.Track, index: number) {
        this.selection.clear();
        this.playFromRow(index);
    }

    /**
     * Activating one row plays the list that row is in, from that row -
     * the library, the artist or the genre the user is looking at, not
     * a queue of one. The paths come from `cachedSortedTracks`, so it
     * is the list as *displayed*: whatever the current sort, search and
     * library filter have made of it, which is the only order the user
     * can see and therefore the only one they can mean.
     */
    private playFromRow(index: number) {
        const filePaths = this.cachedSortedTracks.map(
            (t) => t.FilePath,
        );

        if (filePaths.length === 0 || index < 0) return;

        queueStore.setQueue(
            filePaths,
            index,
            false,
            this.effectiveQueueSource,
        );
    }

    /**
     * Where a "play this" command lands in the displayed list, or -1.
     *
     * Selection keys are file paths, which survive the re-sorts and
     * refetches an index does not - so the index is looked up at the
     * moment it is used rather than remembered.
     */
    private displayIndexOf(filePath: string): number {
        return this.cachedSortedTracks.findIndex(
            (t) => t.FilePath === filePath,
        );
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
                this.playSelection(filePaths);
                break;
            case 'add-to-queue':
                queueStore.addTracksToQueue(filePaths);
                break;
            case 'play-next':
                queueStore.playTracksNext(filePaths);
                break;
            case 'track-details':
                if (filePaths.length === 1) {
                    void this.openTrackDetails(filePaths[0]!);
                } else {
                    void this.openBatchTrackDetails(filePaths);
                }
                break;
            case 'remove-from-library':
                // The only destructive command in this menu: it asks
                // first, and it keeps the selection until the user has
                // answered — the dialog names a count, and clearing the
                // selection under it would make that count a claim
                // about nothing.
                this.ctxMenu.close();
                void this.removeFromLibrary(filePaths);

                return;
        }

        this.selection.clear();
        this.ctxMenu.close();
    }

    /**
     * "Remove from library", behind a confirmation that says what it
     * does *and* what it does not.
     *
     * The second half is the point. This deletes the database rows and
     * stops the scanner importing those paths again; the audio files
     * are left exactly where they are. A user who reads "remove" as
     * "delete" and finds their music gone would have been failed by the
     * copy, not by the operation — so the copy says so in the impact
     * line, where the consequence of every other destructive action in
     * the app is written.
     */
    private async removeFromLibrary(filePaths: string[]) {
        const count = filePaths.length;
        const only =
            count === 1
                ? tracksByFilePath(this.tracks).get(filePaths[0]!)
                : undefined;

        const ok = await confirmAction({
            title:
                count === 1
                    ? `Remove “${only?.TrackName ?? filePaths[0]!}” from the library?`
                    : `Remove ${count.toLocaleString()} tracks from the library?`,
            message:
                count === 1
                    ? 'It is removed from YellowJacket and will not be added' +
                      ' back by a future scan.'
                    : 'They are removed from YellowJacket and will not be' +
                      ' added back by a future scan.',
            impact:
                count === 1
                    ? 'The file is not deleted — it stays on disk exactly' +
                      ' where it is. A full rescan brings it back.'
                    : 'The files are not deleted — they stay on disk exactly' +
                      ' where they are. A full rescan brings them back.',
            confirmLabel:
                count === 1
                    ? 'Remove track'
                    : `Remove ${count.toLocaleString()} tracks`,
            danger: true,
        });

        if (!ok) return;

        try {
            await RemoveFromLibrary(filePaths);
            this.selection.clear();
        } catch (error) {
            console.error('Error removing tracks from library:', error);
            notificationStore.persistent({
                title: 'Could not remove from library',
                text: `${count === 1 ? 'That track is' : `Those ${count.toLocaleString()} tracks are`} still in your library. ${describeError(error)}`,
            });
        }
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

    private async openTrackDetails(filePath: string) {
        const track = tracksByFilePath(this.tracks).get(
            filePath,
        );

        if (!track) return;

        const ready = await loadTrackDetails(
            () => void this.openTrackDetails(filePath),
        );

        if (!ready) return;

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

    private async openBatchTrackDetails(
        filePaths: string[],
    ) {
        const tracks = tracksForPaths(
            this.tracks,
            filePaths,
        );

        if (tracks.length === 0) return;

        const ready = await loadTrackDetails(
            () => void this.openBatchTrackDetails(filePaths),
        );

        if (!ready) return;

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
        role="row"
        aria-rowindex=${index + 1}
        aria-selected=${selected}
        aria-current=${active ? 'true' : 'false'}
        tabindex=${index === this.focusedIndex ? 0 : -1}
        draggable="true"
        data-index=${index}
        data-testid="track-row"
        data-file-path=${track.FilePath}
      >
        <div
          role="gridcell"
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
                const customCell = col.renderCell?.(track, term);
                if (customCell !== undefined && customCell !== nothing) {
                    return html`<div role="gridcell" class="cell">${customCell}</div>`;
                }
                const val = col.accessor(track);
                const centered = val === '\u2014';
                let display: unknown = term
                    ? highlightText(val, term)
                    : val;

                // Wrap artist/album/track values in explore links.
                if (col.id === 'trackName') {
                    display = trackLink(track.TrackName, track.Album, track.ReleaseGroupMBID, track.RecordingMBID, display as any, track.ArtistName);
                } else if (col.id === 'artistName') {
                    // A search term highlights the *flat* credit string,
                    // and mapping those spans onto decomposed parts is a
                    // different problem from rendering the credit.  While
                    // filtering, the single link is the honest answer.
                    creditStore.request(track.RecordingMBID);
                    const parts = term ? undefined : creditStore.get(track.RecordingMBID);
                    display = parts && parts.length > 1
                        ? creditLink(parts, track.ArtistName, track.ArtistMBID)
                        : artistLink(track.ArtistName, track.ArtistMBID, display as any);
                } else if (col.id === 'album') {
                    display = albumLink(track.Album, track.ReleaseGroupMBID, display as any, track.ArtistName);
                }

                // `title` on the cell rather than on whatever is inside
                // it: the value may be a link, a highlighted match or
                // plain text, and a tooltip is inherited by descendants
                // either way (a11y.24).
                return html`
                    <div role="gridcell" title=${val} class=${classMap({
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

    /**
     * The page header, which carries what used to be a hand-rolled
     * sort toolbar written out twice (here and in `cover-grid`).
     *
     * `externalTracks` means this list is a section of some other page
     * — the genre and playlist details — which has a heading already,
     * so the header keeps the count and the sort and drops the title.
     */
    private renderPageHeader() {
        const options: SortOption[] = [
            { id: '', label: 'Default' },
            ...this.configuredColumns
                .filter((c) => c.comparator)
                .map((c) => ({ id: c.id, label: c.label })),
        ];

        return html`
            <page-header
                heading=${this.externalTracks === undefined ? 'Tracks' : ''}
                .count=${this.loadingTracks
                    ? null
                    : this.cachedSortedTracks.length}
                count-noun="track"
                .sortOptions=${options}
                sort-field=${this.sortField ?? ''}
                sort-direction=${this.sortDirection}
                search-term=${this.searchCtrl.term}
                @sort-change=${this.onPageHeaderSort}
            ></page-header>
        `;
    }

    private onPageHeaderSort = (
        e: CustomEvent<{ field: string; direction: 'asc' | 'desc' }>,
    ) => {
        this.sortField =
            e.detail.field === '' ? null : e.detail.field;
        this.sortDirection = e.detail.direction;
        this.saveSortPreferences();
    };

    override render() {
        const visibleTracks = this.cachedSortedTracks;
        const cols = this.activeColumns;

        return html`
      ${this.renderPageHeader()}
      <div class="sr-only" role="status" aria-live="polite">
        ${this.liveStatus(visibleTracks.length)}
      </div>
      ${this.tracks.length === 0
                ? this.renderPlaceholder()
                : html`
            <div
              class="table-container"
              role="grid"
              aria-label="Tracks"
              aria-rowcount=${visibleTracks.length}
              aria-busy=${this.loadingTracks}
              @keydown=${this.onListKeydown}
            >
            ${this.phone ? nothing : html`<div class="header-row" role="row">
              <div role="columnheader" aria-label="Favourite"></div>
              ${cols.map(
                    (col) => html`
                <div
                  role="columnheader"
                  tabindex="0"
                  aria-sort=${this.sortField === col.id
                          ? (this.sortDirection === 'asc' ? 'ascending' : 'descending')
                          : 'none'}
                  class="header-cell ${col.align === 'right' ? 'cell-right' : ''}"
                  @click=${() =>
                          this.onHeaderCellClick(
                              col.id,
                          )}
                  @keydown=${(e: KeyboardEvent) => {
                          if (e.key !== 'Enter' && e.key !== ' ') return;

                          e.preventDefault();
                          e.stopPropagation();
                          this.onHeaderCellClick(col.id);
                      }}
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
            </div>`}
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
                        .layout=${this.rowLayout}
                      ></lit-virtualizer>
                    `}

      <!-- Resizing is a pointer gesture with no touch equivalent, and
           the phone's two columns are not the user's to arrange. -->
      <div class="resize-overlay">
        ${(this.phone ? [] : this.colBoundaryPositions).map(
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
              <div class="context-menu-panel" role="menu" aria-label="Track actions">
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
                  <wa-icon slot="icon" name=${ICON_QUEUE}></wa-icon>
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
                  <wa-icon slot="icon" name=${ICON_PLAYLIST}></wa-icon>
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
                <wa-dropdown-item
                    @click=${() =>
                        this.onContextMenuAction(
                            'remove-from-library',
                        )}
                     @mouseenter=${() =>
                         this.ctxMenu.closePlaylistSubmenu()}
                >
                    <wa-icon
                        slot="icon"
                        name="trash"
                    ></wa-icon>
                    Remove from Library
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

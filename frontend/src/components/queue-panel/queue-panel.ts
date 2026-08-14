import { LitElement, html, svg, css, nothing, unsafeCSS } from 'lit';
import { designTokens } from '../../styles/tokens.css';
import { srOnly } from '../../styles/sr-only.css';
import {
    customElement,
    property,
    state,
    query,
} from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import type WaPopup from '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import { QueueController } from '@store/controllers/queue-controller';
import {
    describeQueueSource,
    isQueueSourceNavigable,
    navigateToQueueSource,
} from '@utils/queue-source-link';
import { confirmAction } from '@components/confirm-dialog/confirm-dialog';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';
import '@lit-labs/virtualizer';
import type { LitVirtualizer } from '@lit-labs/virtualizer';
import { virtualizerRef } from '@lit-labs/virtualizer/virtualize.js';
import { flow } from '@lit-labs/virtualizer/layouts/flow.js';
import { classMap } from 'lit/directives/class-map.js';
import type { QueueTrack } from '@store/queue-store';
import { SelectionController } from '@utils/selection-controller';
import type { SelectionHost } from '@utils/selection-controller';
import {
    ContextMenuController,
    contextMenuStyles,
    isContextMenuKey,
} from '@utils/context-menu-controller.js';
import type { ContextMenuHost } from '@utils/context-menu-controller.js';
import { focusRovingRow, nextRovingIndex } from '@utils/roving-rows';
import { FavoritesController } from '@store/controllers/favorites-controller';
import {
    hasTrackPayload,
    getDragPayload,
    setDragPayload,
    emitDragActive,
    getActiveDragSource,
} from '@utils/drag-controller';
import {
    createDragImage,
    createTrackCardDragImage,
    removeDragImage,
} from '@utils/drag-image';
import { libraryStore } from '@store/library-store';
import type * as library from '@go/library/models.js';
import { loadTrackDetails } from '@utils/lazy-track-details.js';
import { tracksByFilePath } from '@utils/track-index.js';
import type { TrackDetails } from '@components/track-details/track-details.js';
import type { CoverArtUrls } from '@components/track-details/track-details.js';
import {
    artistLink,
    trackLink,
    exploreLinkStyles,
} from '@utils/explore-link';
/** Above this many tracks, clearing the queue asks first. */
const CLEAR_CONFIRM_THRESHOLD = 20;

const MIN_WIDTH = 200;
const MAX_WIDTH = 500;
const DEFAULT_WIDTH = 320;

@customElement('queue-panel')
export class QueuePanel
    extends LitElement
    implements SelectionHost, ContextMenuHost
{
    private queue = new QueueController(this);
    private selection = new SelectionController(this);
    private ctxMenu = new ContextMenuController(this);
    private favCtrl = new FavoritesController(this);

    @property({ type: Boolean, reflect: true })
    open = false;

    @state()
    private isDragging = false;

    @state()
    private playlistPickerOpen = false;

    private dragOver = false;
    private dragEnterCount = 0;

    private dropTargetIndex = -1;
    private dropTargetRafId = 0;

    private autoScrollRafId = 0;
    private autoScrollDelta = 0;

    private dragImageEl: HTMLElement | null = null;

    /** Whether delegated event handlers have been attached to the virtualizer. */
    private delegationAttached = false;

    @query('#add-to-playlist-popup')
    private addToPlaylistPopup!: WaPopup;

    @query('#context-menu')
    private contextMenuPopup!: WaPopup;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: WaPopup;

    @query('lit-virtualizer')
    private virtualizer!: LitVirtualizer;

    @query('track-details')
    private trackDetailsDialog!: TrackDetails;

    private handleSelectAll = (): void => {
        this.selection.selectAll();
    };

    // Detect native scrollbar thumb drag to suppress lit-virtualizer's
    // scroll error corrections that fight the browser's drag gesture.
    private onVirtualizerMouseDown = (e: MouseEvent) => {
        const el = this.virtualizer;
        if (!el) return;
        const rect = el.getBoundingClientRect();
        // Click is in the scrollbar gutter if it's beyond the content area.
        // clientWidth excludes scrollbar; offsetWidth includes it.
        if (e.clientX > rect.left + el.clientWidth) {
            this.scrollbarDragging = true;
        }
    };

    private onScrollbarDragEnd = () => {
        this.scrollbarDragging = false;
    };

    private closePickerHandler = (e: MouseEvent) => {
        const path = e.composedPath();
        const popup = this.addToPlaylistPopup;
        const btn = this.shadowRoot?.querySelector(
            '.add-to-playlist-button',
        );

        if (
            popup &&
            !path.includes(popup) &&
            (!btn || !path.includes(btn))
        ) {
            this.closePlaylistPicker();
        }
    };

    private clearSelectionHandler = (e: MouseEvent) => {
        const path = e.composedPath();
        const isTrackClick = path.some(
            (el) =>
                el instanceof HTMLElement &&
                el.classList.contains('track-item') &&
                this.shadowRoot?.contains(el),
        );

        if (!isTrackClick) {
            this.selection.clear();
        }
    };

    /** The row holding the roving tab stop. */
    @state() private focusedIndex = 0;

    /**
     * What the live region says about the last keyboard reorder.
     *
     * Empty until there has been one — the region itself renders
     * unconditionally, because a screen reader announces a *change* to a
     * region it is already watching and ignores one that appears with
     * its text already in it.
     */
    @state() private moveAnnouncement = '';

    private panelWidth = DEFAULT_WIDTH;
    private scrollbarDragging = false;

    // _itemSize is an internal property applied via Object.assign in BaseLayout's
    // config setter. Setting it to match the actual fixed .track-item height (49px)
    // prevents lit-virtualizer's scroll error correction from fighting the native
    // scrollbar during drag on large lists (20k+ items). Without this, the default
    // estimate of 100px causes massive scroll height recalculation as items get
    // measured, which calls scrollTo() and desynchronizes the scrollbar thumb.
    private flowLayout = flow({
        _itemSize: { width: 100, height: 49 },
    } as Parameters<typeof flow>[0]);

    /**
     * Track the last currentIndex so we only auto-scroll
     * on actual track changes.
     */
    private lastScrolledIndex = -1;

    /**
     * Tracks the last currentIndex for which the virtualizer was
     * told to re-render, so the active-track highlight stays in sync.
     */
    private lastRenderedIndex = -1;



    // =================================================================
    // SelectionHost interface
    // =================================================================

    getItemKey(index: number): string | undefined {
        if (index < 0 || index >= this.queue.tracks.length) {
            return undefined;
        }

        return String(index);
    }

    getItemCount(): number {
        return this.queue.tracks.length;
    }

    onSelectionChanged(): void {
        this.virtualizer?.requestUpdate();
    }

    // =================================================================
    // ContextMenuHost interface
    // =================================================================

    getContextMenuPopup(): WaPopup | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup(): WaPopup | undefined {
        return this.playlistSubmenuPopup;
    }

    static override styles = [designTokens, srOnly, contextMenuStyles, exploreLinkStyles, css`
        :host {
            flex-shrink: 0;
            width: 0;
            overflow: hidden;
            background-color: var(--yj-bg-surface, #212529);
            display: flex;
            flex-direction: row;
            contain: layout style paint;
        }

        :host([open]) {
            width: var(
                --queue-width,
                ${unsafeCSS(DEFAULT_WIDTH)}px
            );
            border-left: 1px solid var(--yj-border-subtle, #333);
        }

        .resize-handle {
            position: absolute;
            top: 0;
            left: 0;
            width: 4px;
            height: 100%;
            cursor: col-resize;
            background-color: transparent;
            transition: background-color 0.15s ease;
            z-index: 10;
        }

        .resize-handle:hover,
        .resize-handle.dragging {
            background-color: var(--yj-text-tertiary, #6c757d);
        }

        .panel-content {
            position: relative;
            display: flex;
            flex-direction: column;
            min-width: ${unsafeCSS(MIN_WIDTH)}px;
            flex: 1;
        }

        .header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 12px 16px;
            border-bottom: 1px solid var(--yj-border-subtle, #333);
            flex-shrink: 0;
        }

        .header-title {
            display: flex;
            flex-direction: column;
            gap: 2px;
            min-width: 0;
        }

        .header h3 {
            margin: 0;
            font-size: var(--yj-text-lg);
            font-weight: 600;
        }

        .queue-source {
            font-size: var(--yj-text-xs, 0.75rem);
            color: var(--yj-text-tertiary, #666);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .queue-source.navigable {
            cursor: pointer;
        }

        .queue-source.navigable:hover {
            text-decoration: underline;
        }

        .header-actions {
            display: flex;
            align-items: center;
            gap: 4px;
        }

        .header-action-button {
            background: none;
            border: none;
            color: inherit;
            cursor: pointer;
            padding: 4px;
            display: flex;
            align-items: center;
        }

        .header-action-button:hover {
            color: var(--yj-accent-text, #ffd43b);
        }

        .header-action-button:disabled {
            color: var(--yj-border-subtle, #555);
            cursor: not-allowed;
        }

        #add-to-playlist-popup {
            z-index: 210;
        }

        .list-area {
            flex: 1;
            display: flex;
            flex-direction: column;
            min-height: 0;
        }

        lit-virtualizer {
            flex: 1;
            overflow-y: auto;
            contain: paint;
            overflow-anchor: none;
        }

        .track-item {
            position: relative;
            display: flex;
            align-items: center;
            padding: 8px 16px;
            gap: 12px;
            border-bottom: 1px solid
                var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
            cursor: default;
            user-select: none;
            width: 100%;
            box-sizing: border-box;
            height: 49px;
            overflow: hidden;
            contain: strict;
        }

        .track-item:hover {
            background-color: var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
        }

        .track-item.selected {
            background-color: var(--yj-selection-bg, rgba(100, 160, 255, 0.15));
        }

        .track-item.active {
            background-color: var(--yj-accent-bg, rgba(255, 212, 59, 0.1));
        }

        .track-item.selected.active {
            background-color: var(--yj-selection-bg, rgba(100, 160, 255, 0.15));
        }

        .track-position {
            font-size: var(--yj-text-sm);
            color: var(--yj-text-tertiary, #888);
            min-width: 20px;
            text-align: right;
        }

        .track-item.active .track-position {
            color: var(--yj-accent-text, #ffd43b);
        }

        /* a11y.22, the same rule as track-list one panel over: the
           playing row was a tint and a text colour and nothing else.
           The triangle lives in the row's own 16px left padding, so it
           is a shape that is present or absent and the flex row does
           not move. */
        .track-item.active::before {
            content: '';
            position: absolute;
            left: 5px;
            top: 50%;
            transform: translateY(-50%);
            border-left: 5px solid var(--yj-accent-text, #ffd43b);
            border-top: 4px solid transparent;
            border-bottom: 4px solid transparent;
        }

        .track-art {
            width: 32px;
            height: 32px;
            border-radius: 4px;
            overflow: hidden;
            flex-shrink: 0;
            background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
        }

        .track-art img {
            width: 100%;
            height: 100%;
            object-fit: cover;
            display: block;
        }

        .track-details {
            flex: 1;
            min-width: 0;
            display: flex;
            flex-direction: column;
            gap: 2px;
            overflow: hidden;
        }

        .track-title {
            font-size: var(--yj-text-md);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .track-item.active .track-title {
            color: var(--yj-accent-text, #ffd43b);
        }

        .track-artist {
            font-size: var(--yj-text-xs);
            color: var(--yj-text-secondary, #b3b3b3);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .remove-button {
            background: none;
            border: none;
            color: var(--yj-text-tertiary, #888);
            cursor: pointer;
            padding: 4px;
            display: flex;
            align-items: center;
            visibility: hidden;
        }

        .track-item:hover .remove-button {
            visibility: visible;
        }

        .remove-button:hover {
            color: var(--yj-error-text, #ff8787);
        }

        .list-area.drag-over {
            outline: 2px dashed var(--yj-accent, #ffd43b);
            outline-offset: -2px;
        }

        .track-item.drop-before::before {
            content: '';
            position: absolute;
            top: -1px;
            left: 8px;
            right: 8px;
            height: 2px;
            background: var(--yj-accent, #ffd43b);
            border-radius: 1px;
            z-index: 5;
            pointer-events: none;
        }

        .track-item.drop-after::after {
            content: '';
            position: absolute;
            bottom: -1px;
            left: 8px;
            right: 8px;
            height: 2px;
            background: var(--yj-accent, #ffd43b);
            border-radius: 1px;
            z-index: 5;
            pointer-events: none;
        }

        .empty-state {
            flex: 1;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            padding: 48px 20px;
            color: var(--yj-text-secondary, #b3b3b3);
            text-align: center;
            gap: 8px;
        }

        .empty-state wa-icon {
            font-size: 32px; /* intentionally large decorative icon */
        }

        .empty-state p {
            margin: 4px 0;
        }

        .drop-zone-icon {
            display: none;
            align-items: center;
            justify-content: center;
            width: 56px;
            height: 56px;
            border-radius: 12px;
            background: var(
                --yj-accent-bg-strong,
                rgba(255, 212, 59, 0.18)
            );
            color: var(--yj-accent-text, #ffd43b);
            font-size: 28px;
            pointer-events: none;
        }

        .list-area.drag-over.empty-drag
            .empty-state {
            background-color: var(
                --yj-accent-bg-strong,
                rgba(255, 212, 59, 0.15)
            );
        }

        .list-area.drag-over.empty-drag
            .drop-zone-icon {
            display: flex;
        }

        .list-area.drag-over.empty-drag
            .empty-state
            > :not(.drop-zone-icon) {
            display: none;
        }

    `];

    override firstUpdated() {
        this.attachVirtualizerHooks();
    }

    /**
     * Attach scroll-error monkey-patch, mousedown listener,
     * and delegated event handlers to the virtualizer.
     * The virtualizer may not exist on first render (queue
     * empty), so this is called from both firstUpdated()
     * and updated() — guarded by a flag.
     */
    private attachVirtualizerHooks() {
        if (this.delegationAttached) return;

        const virtEl = this.virtualizer;

        if (!virtEl) return;

        // Monkey-patch lit-virtualizer's scroll error correction to suppress it
        // during native scrollbar drag. Without this, the virtualizer calls
        // scrollTo() to "correct" sub-pixel estimation errors, which fights the
        // browser's native scrollbar drag gesture and causes the thumb to
        // desync from the mouse on large lists (20k+ items).
        //
        // NOTE: CSS `overflow-anchor: none` (set on lit-virtualizer above)
        // disables the *browser's* native scroll anchoring, but does NOT
        // affect lit-virtualizer's own _correctScrollError() method which
        // calls scrollTo() internally. This monkey-patch is still needed
        // to suppress that internal correction during scrollbar drag.
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const virt = (virtEl as any)?.[virtualizerRef];
        if (virt) {
            const origCorrect = virt._correctScrollError.bind(virt);
            virt._correctScrollError = () => {
                if (this.scrollbarDragging) {
                    // Discard the error instead of applying it via scrollTo().
                    // This prevents stale corrections from accumulating and
                    // being applied in a burst when the drag ends.
                    virt._scrollError = null;
                    return;
                }
                origCorrect();
            };
        }
        virtEl.addEventListener(
            'mousedown',
            this.onVirtualizerMouseDown,
        );

        // Event delegation: attach stable handlers to the virtualizer
        // so renderTrackItem creates zero per-item closures.
        virtEl.addEventListener('click', this.onDelegatedClick);
        virtEl.addEventListener('dblclick', this.onDelegatedDblClick);
        virtEl.addEventListener('contextmenu', this.onDelegatedContextMenu);
        virtEl.addEventListener('dragstart', this.onDelegatedDragStart);
        virtEl.addEventListener('dragend', this.onTrackDragEnd);
        virtEl.addEventListener('keydown', this.onDelegatedKeydown);
        this.delegationAttached = true;
    }

    override connectedCallback() {
        super.connectedCallback();
        this.style.setProperty(
            '--queue-width',
            `${this.panelWidth}px`,
        );
        document.addEventListener(
            'mousemove',
            this.handleMouseMove,
        );
        document.addEventListener(
            'mouseup',
            this.handleMouseUp,
        );
        document.addEventListener(
            'click',
            this.closePickerHandler,
        );
        document.addEventListener(
            'click',
            this.clearSelectionHandler,
        );
        document.addEventListener(
            'dragend',
            this.onDocumentDragEnd,
        );
        document.addEventListener(
            'shortcut:select-all',
            this.handleSelectAll,
        );
        document.addEventListener(
            'mouseup',
            this.onScrollbarDragEnd,
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        document.removeEventListener(
            'mousemove',
            this.handleMouseMove,
        );
        document.removeEventListener(
            'mouseup',
            this.handleMouseUp,
        );
        document.removeEventListener(
            'click',
            this.closePickerHandler,
        );
        document.removeEventListener(
            'click',
            this.clearSelectionHandler,
        );
        document.removeEventListener(
            'dragend',
            this.onDocumentDragEnd,
        );
        document.removeEventListener(
            'shortcut:select-all',
            this.handleSelectAll,
        );
        document.removeEventListener(
            'mouseup',
            this.onScrollbarDragEnd,
        );
        this.virtualizer?.removeEventListener(
            'mousedown',
            this.onVirtualizerMouseDown,
        );

        // Remove delegated event handlers from virtualizer.
        const virtEl = this.virtualizer;
        if (virtEl) {
            virtEl.removeEventListener('click', this.onDelegatedClick);
            virtEl.removeEventListener('dblclick', this.onDelegatedDblClick);
            virtEl.removeEventListener('contextmenu', this.onDelegatedContextMenu);
            virtEl.removeEventListener('dragstart', this.onDelegatedDragStart);
            virtEl.removeEventListener('dragend', this.onTrackDragEnd);
            virtEl.removeEventListener('keydown', this.onDelegatedKeydown);
        }
        this.delegationAttached = false;
    }

    override updated() {
        // Closed, the panel is `width: 0` — which hides it from the eye
        // and from nobody else: its Clear and Add buttons still took tab
        // stops at x=1440 and were still read out (H-5).  `inert` is the
        // one attribute that means "not there" to both.
        this.inert = !this.open;

        // ...and a panel that is not there does not render a list
        // either (perf.m7).  `width: 0` and `contain: layout style
        // paint` bound the damage but do not stop the work: the
        // virtualizer inside still had a real height and a
        // `min-width: 300px`, so it measured its visible window on
        // every queue change, and `scrollToIndex` below called
        // `scrollIntoView()` on a laid-out but invisible element every
        // time the current track changed.  The list body renders
        // `nothing` while closed; these indices reset so opening the
        // panel re-syncs the highlight and the scroll position rather
        // than inheriting stale ones.
        if (!this.open) {
            this.lastRenderedIndex = -1;
            this.lastScrolledIndex = -1;

            return;
        }

        // The virtualizer may not exist on first render
        // (queue empty). Retry hooks here when it appears.
        this.attachVirtualizerHooks();

        const currentIndex = this.queue.currentIndex;

        // Force virtualizer to re-render visible items when the
        // active track changes so the highlight stays in sync.
        if (currentIndex !== this.lastRenderedIndex) {
            this.lastRenderedIndex = currentIndex;
            this.virtualizer?.requestUpdate();
        }

        // Auto-scroll to the active track when it changes.
        if (
            currentIndex >= 0 &&
            currentIndex !== this.lastScrolledIndex &&
            this.virtualizer
        ) {
            this.lastScrolledIndex = currentIndex;
            requestAnimationFrame(() => {
                this.virtualizer?.scrollToIndex(
                    currentIndex,
                    'center',
                );
            });
        }
    }

    /**
     * Clearing stops playback and discards the list, and it is the one
     * mutation in this panel with no way back (errors.m3). A queue you
     * could rebuild in a second is not worth a prompt; one you spent
     * the evening on is.
     */
    private handleClearQueue = async () => {
        const count = this.queue.tracks.length;

        if (count > CLEAR_CONFIRM_THRESHOLD) {
            const ok = await confirmAction({
                title: 'Clear the queue?',
                message: `${count} tracks will be removed and playback will stop.`,
                impact: 'The queue cannot be brought back.',
                confirmLabel: 'Clear queue',
                danger: true,
            });

            if (!ok) return;
        }

        this.queue.clearQueue();
    };

    private async handleAddToPlaylist() {
        if (this.queue.tracks.length === 0) return;

        this.playlistPickerOpen = !this.playlistPickerOpen;

        await this.updateComplete;

        const popup = this.addToPlaylistPopup;
        const btn = this.shadowRoot?.querySelector(
            '.add-to-playlist-button',
        );

        if (popup && btn) {
            popup.anchor = btn;
            popup.active = this.playlistPickerOpen;
        }

        if (this.playlistPickerOpen) {
            const picker = this.shadowRoot?.querySelector(
                'playlist-picker',
            ) as PlaylistPicker | null;

            picker?.reset();
        }
    }

    private closePlaylistPicker() {
        if (!this.playlistPickerOpen) return;

        this.playlistPickerOpen = false;

        const popup = this.addToPlaylistPopup;

        if (popup) {
            popup.active = false;
        }
    }

    private onPlaylistActionComplete = () => {
        this.closePlaylistPicker();
    };

    // =================================================================
    // Delegated event handlers (stable references, zero per-item closures)
    // =================================================================

    /**
     * Walk up from the event target to find the nearest
     * `.track-item` and extract the index via `data-index`.
     */
    private resolveTrackIndexFromEvent(
        e: Event,
    ): number | null {
        const row = (e.target as HTMLElement).closest(
            '.track-item',
        ) as HTMLElement | null;

        if (!row) return null;

        const idx = Number(row.dataset.index);

        if (Number.isNaN(idx)) return null;

        return idx;
    }

    private onDelegatedClick = (e: MouseEvent) => {
        const idx = this.resolveTrackIndexFromEvent(e);

        if (idx === null) return;

        // Check if click was on the remove button
        const removeBtn = (e.target as HTMLElement).closest(
            '.remove-button',
        );

        if (removeBtn) {
            e.stopPropagation();
            this.queue.removeFromQueue(idx);

            return;
        }

        const track = this.queue.tracks[idx];

        if (track) this.handleTrackClick(e, track, idx);
    };

    private onDelegatedDblClick = (e: MouseEvent) => {
        const idx = this.resolveTrackIndexFromEvent(e);

        if (idx !== null) this.handleTrackDblClick(idx);
    };

    private onDelegatedContextMenu = (e: MouseEvent) => {
        const idx = this.resolveTrackIndexFromEvent(e);

        if (idx !== null) this.handleTrackContextMenu(e, idx);
    };

    private onDelegatedDragStart = (e: DragEvent) => {
        const idx = this.resolveTrackIndexFromEvent(e);

        if (idx !== null) this.onTrackDragStart(e, idx);
    };

    // =================================================================
    // Selection & click handlers
    // =================================================================

    private handleTrackClick(
        e: MouseEvent,
        _track: QueueTrack,
        index: number,
    ) {
        this.selection.handleItemClick(
            e,
            String(index),
            index,
        );
    }

    private handleTrackDblClick(index: number) {
        this.selection.clear();
        this.queue.playAtIndex(index);
    }

    private handleTrackContextMenu(
        e: MouseEvent,
        index: number,
    ) {
        e.preventDefault();
        e.stopPropagation();

        this.selection.handleContextMenu(String(index));
        this.ctxMenu.openAt(e.clientX, e.clientY);
    }

    /**
     * The queue's rows had no keyboard path at all: not focusable, so
     * neither Enter nor Shift+F10 had anywhere to fire from, and the
     * menu — which is the only way to reach most of what a queue row can
     * do — was right-click only (a11y.3).
     */
    private onDelegatedKeydown = (e: KeyboardEvent): void => {
        const count = this.queue.tracks.length;

        if (count === 0) return;

        const row = (e.target as HTMLElement | null)?.closest<HTMLElement>(
            '.track-item',
        );

        // `focusedIndex` is the roving tab stop, and until now only the
        // arrow keys moved it — so a row focused by a click or by Tab
        // left it saying 0, and every key below acted on the wrong row.
        // Enter played the first track in the queue from any focused
        // row, which is a pre-existing bug that Alt+Arrow made visible
        // by moving something. The key event knows which row it came
        // from; use that.
        const rowIndex = Number(row?.dataset.index ?? NaN);

        if (Number.isInteger(rowIndex) && rowIndex !== this.focusedIndex) {
            this.focusedIndex = rowIndex;
        }

        if (isContextMenuKey(e) && row) {
            e.preventDefault();
            e.stopPropagation();
            this.selection.handleContextMenu(String(this.focusedIndex));
            this.ctxMenu.openFrom(row);

            return;
        }

        if (e.key === 'Enter') {
            e.preventDefault();
            e.stopPropagation();
            this.selection.clear();
            this.queue.playAtIndex(this.focusedIndex);

            return;
        }

        // a11y.11: reordering the queue was drag-only, so its order
        // could not be changed without a mouse at all.
        //
        // This has to come before `nextRovingIndex`, which switches on
        // `e.key` and does not look at the modifiers — so Alt+ArrowUp
        // already moved the roving focus, and would have gone on doing
        // that *as well* as moving the row.
        if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
            e.preventDefault();
            e.stopPropagation();
            this.moveFocusedRow(e.key === 'ArrowUp' ? -1 : 1, count);

            return;
        }

        const next = nextRovingIndex(e.key, this.focusedIndex, count);

        if (next === null) return;

        e.preventDefault();
        e.stopPropagation();
        this.focusedIndex = next;
        this.selection.handleContextMenu(String(next));
        void focusRovingRow(
            this,
            this.virtualizer,
            next,
            (i) => `.track-item[data-index="${i}"]`,
        );
    };

    /**
     * Move the focused row one position, and say where it went.
     *
     * It moves the *focused* row rather than the selection, which the
     * drag path uses: the keyboard model already keeps those in step
     * (every roving move re-selects the row it lands on), and "Alt+Down
     * moved four rows you cannot see" is not a thing to do without an
     * undo.
     *
     * The asymmetry in the target index is `MoveQueueTracks`'s, not
     * ours. `toIndex` is an index into the array *before* the move, so
     * moving down by one has to ask for `i + 2`: `i + 1` is where the
     * row already is once you account for its own removal, and the
     * backend's contiguous-block guard correctly treats it as a no-op.
     */
    private moveFocusedRow(delta: -1 | 1, count: number): void {
        const from = this.focusedIndex;
        const to = from + delta;

        if (to < 0 || to >= count) {
            this.moveAnnouncement =
                delta < 0
                    ? 'Already first in the queue'
                    : 'Already last in the queue';

            return;
        }

        this.queue.moveTracksInQueue([from], delta < 0 ? to : from + 2);

        this.focusedIndex = to;
        this.selection.handleContextMenu(String(to));
        this.moveAnnouncement = `Moved to position ${to + 1} of ${count}`;

        void focusRovingRow(
            this,
            this.virtualizer,
            to,
            (i) => `.track-item[data-index="${i}"]`,
        );
    }

    private onContextMenuAction(action: string) {
        const indices =
            this.selection.getSelectedIndices();

        if (indices.length === 0) return;

        switch (action) {
            case 'play':
                this.queue.playAtIndex(indices[0]!);
                break;
            case 'remove':
                this.queue.removeTracksFromQueue(
                    indices,
                );
                break;
            case 'track-details':
                if (indices.length === 1) {
                    void this.openTrackDetails(indices[0]!);
                } else {
                    void this.openBatchTrackDetails(indices);
                }
                break;
        }

        this.selection.clear();
        this.ctxMenu.close();
    }

    private onContextMenuFavoriteToggle() {
        const filePaths =
            this.getSelectedFilePaths();

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

    private async openTrackDetails(index: number) {
        const queueTrack =
            this.queue.tracks[index];

        if (!queueTrack) return;

        const tracks =
            libraryStore.getCachedTracks();
        const track = tracks
            ? tracksByFilePath(tracks).get(
                queueTrack.filePath,
            )
            : undefined;

        if (!track) return;

        const ready = await loadTrackDetails(
            () => void this.openTrackDetails(index),
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
        indices: number[],
    ) {
        const queueTracks = this.queue.tracks;
        const cachedTracks =
            libraryStore.getCachedTracks();

        if (!cachedTracks) return;

        const byPath = tracksByFilePath(cachedTracks);
        const tracks = indices
            .map((i) => queueTracks[i])
            .filter((qt) => qt != null)
            .map((qt) => byPath.get(qt.filePath))
            .filter(
                (t): t is library.Track =>
                    t != null,
            );

        if (tracks.length === 0) return;

        const ready = await loadTrackDetails(
            () => void this.openBatchTrackDetails(indices),
        );

        if (!ready) return;

        const first = tracks[0]!;
        const albumNames = new Set(tracks.map((t) => t.Album));
        let coverArt: CoverArtUrls | null = null;
        let coverArtMixed = false;

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

    private onContextPlaylistActionComplete = () => {
        this.selection.clear();
        this.ctxMenu.close();
    };

    /**
     * Derive file paths from selected indices for
     * operations that need file paths (e.g. Add to Playlist).
     */
    private getSelectedFilePaths(): string[] {
        const tracks = this.queue.tracks;

        return this.selection
            .getSelectedIndices()
            .map((i) => tracks[i]!.filePath);
    }

    // =================================================================
    // Drop target (tracks dropped into queue)
    // =================================================================

    /**
     * Toggle the drag-over CSS classes directly on the
     * DOM element. This avoids Lit re-renders which
     * cause DOM mutations that break the browser's
     * drag event stream.
     */
    private updateDragOverClass() {
        const panel =
            this.shadowRoot?.querySelector(
                '.list-area',
            );

        if (!panel) return;

        const isEmpty = this.queue.tracks.length === 0;

        panel.classList.toggle(
            'drag-over',
            this.dragOver,
        );
        panel.classList.toggle(
            'empty-drag',
            this.dragOver && isEmpty,
        );
    }

    private onPanelDragEnter = (e: DragEvent) => {
        if (!hasTrackPayload(e)) return;

        e.preventDefault();
        this.dragEnterCount++;

        if (e.dataTransfer) {
            const isInternal =
                getActiveDragSource() === 'queue';
            e.dataTransfer.dropEffect = isInternal
                ? 'move'
                : 'copy';
        }

        if (!this.dragOver) {
            this.dragOver = true;
            this.updateDragOverClass();
            this.startAutoScroll();
        }
    };

    private onPanelDragOver = (e: DragEvent) => {
        if (!hasTrackPayload(e)) return;

        e.preventDefault();

        const isInternal =
            getActiveDragSource() === 'queue';

        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = isInternal
                ? 'move'
                : 'copy';
        }

        this.updateDropTargetIndex(e.clientY);
        this.updateAutoScrollDelta(e.clientY);
    };

    private onPanelDragLeave = (_e: DragEvent) => {
        this.dragEnterCount--;

        // Each child-boundary crossing fires a
        // paired dragenter/dragleave.  The counter
        // only reaches 0 when the cursor truly
        // leaves the panel.
        if (this.dragEnterCount <= 0) {
            this.dragEnterCount = 0;
            this.cleanupDragState();
        }
    };

    private onPanelDrop = (e: DragEvent) => {
        e.preventDefault();

        const targetIndex = this.dropTargetIndex;

        this.cleanupDragState();

        const payload = getDragPayload(e);

        if (
            !payload ||
            payload.filePaths.length === 0
        ) {
            return;
        }

        if (payload.source === 'queue') {
            // Internal reorder.
            const fromIndices = this.selection
                .getSelectedIndices();

            if (fromIndices.length > 0) {
                this.queue.moveTracksInQueue(
                    fromIndices,
                    targetIndex >= 0
                        ? targetIndex
                        : this.queue.tracks.length,
                );
            }
        } else {
            // External insert at position.
            const idx =
                targetIndex >= 0
                    ? targetIndex
                    : this.queue.tracks.length;
            this.queue.insertTracksAtIndex(
                payload.filePaths,
                idx,
            );
        }
    };

    /**
     * Calculate the drop target index from cursor Y
     * position relative to the virtualizer's children.
     */
    private updateDropTargetIndex(clientY: number) {
        const newIdx =
            this.computeDropTargetIndex(clientY);

        if (newIdx !== this.dropTargetIndex) {
            this.dropTargetIndex = newIdx;

            // Debounce via RAF to avoid layout thrashing
            // that interrupts the browser drag stream.
            if (!this.dropTargetRafId) {
                this.dropTargetRafId =
                    requestAnimationFrame(() => {
                        this.dropTargetRafId = 0;
                        this.virtualizer?.requestUpdate();
                    });
            }
        }
    }

    private computeDropTargetIndex(
        clientY: number,
    ): number {
        const tracks = this.queue.tracks;

        if (tracks.length === 0) return 0;

        const virt = this.virtualizer;

        if (!virt) return tracks.length;

        const items =
            virt.querySelectorAll('.track-item');

        if (items.length === 0) return tracks.length;

        // Check each visible item to find the drop
        // position.
        for (const item of items) {
            const rect = item.getBoundingClientRect();
            const midY = rect.top + rect.height / 2;

            if (clientY < midY) {
                const idx = Number(
                    (item as HTMLElement).dataset.index,
                );

                if (!Number.isNaN(idx)) return idx;
            }
        }

        // Cursor is below all visible items — append
        // at end.
        const lastItem = items[items.length - 1];

        if (lastItem) {
            const idx = Number(
                (lastItem as HTMLElement).dataset
                    .index,
            );

            if (!Number.isNaN(idx)) return idx + 1;
        }

        return tracks.length;
    }

    // =================================================================
    // Auto-scroll during drag
    // =================================================================

    private static readonly SCROLL_ZONE = 60;
    private static readonly SCROLL_SPEED = 12;

    /**
     * Update the scroll delta based on cursor proximity
     * to the top/bottom edges. The RAF loop (started in
     * onPanelDragEnter) reads this value each frame.
     * Setting delta to 0 means no scrolling; the loop
     * stays running until the drag ends.
     */
    private updateAutoScrollDelta(clientY: number) {
        const virt = this.virtualizer;

        if (!virt) return;

        const rect = virt.getBoundingClientRect();
        const zone = QueuePanel.SCROLL_ZONE;

        const distTop = clientY - rect.top;
        const distBottom = rect.bottom - clientY;

        if (distTop < zone && distTop >= 0) {
            this.autoScrollDelta =
                -QueuePanel.SCROLL_SPEED *
                (1 - distTop / zone);
        } else if (
            distBottom < zone &&
            distBottom >= 0
        ) {
            this.autoScrollDelta =
                QueuePanel.SCROLL_SPEED *
                (1 - distBottom / zone);
        } else {
            this.autoScrollDelta = 0;
        }
    }

    private startAutoScroll() {
        if (this.autoScrollRafId) return;

        const step = () => {
            const virt = this.virtualizer;

            if (!virt) {
                this.autoScrollRafId = 0;

                return;
            }

            if (this.autoScrollDelta !== 0) {
                virt.scrollTop += this.autoScrollDelta;
            }

            this.autoScrollRafId =
                requestAnimationFrame(step);
        };

        this.autoScrollRafId =
            requestAnimationFrame(step);
    }

    private stopAutoScroll() {
        if (this.autoScrollRafId) {
            cancelAnimationFrame(this.autoScrollRafId);
            this.autoScrollRafId = 0;
        }

        this.autoScrollDelta = 0;
    }

    /**
     * Reset all drag-related state. Called from drop,
     * dragend, and the global dragend fallback.
     */
    private cleanupDragState() {
        if (!this.dragOver) return;

        this.dragOver = false;
        this.dragEnterCount = 0;
        this.dropTargetIndex = -1;

        if (this.dropTargetRafId) {
            cancelAnimationFrame(this.dropTargetRafId);
            this.dropTargetRafId = 0;
        }

        this.updateDragOverClass();
        this.stopAutoScroll();
        this.virtualizer?.requestUpdate();
    }

    /**
     * Global dragend handler catches external drags
     * (from track-list / cover-grid) that end outside
     * the queue panel without a drop event.
     */
    private onDocumentDragEnd = () => {
        this.cleanupDragState();
    };

    // =================================================================
    // Drag source (queue tracks to playlist)
    // =================================================================

    private onTrackDragStart = (
        e: DragEvent,
        index: number,
    ) => {
        const tracks = this.queue.tracks;

        let filePaths: string[];

        if (this.selection.isSelected(String(index))) {
            // Drag the entire multi-selection.
            filePaths = this.selection
                .getSelectedIndices()
                .map((i) => tracks[i]!.filePath);
        } else {
            // Dragging an unselected track — select
            // only it so internal reorder works.
            this.selection.handleContextMenu(
                String(index),
            );

            const track = tracks[index];

            if (!track) return;

            filePaths = [track.filePath];
        }

        if (filePaths.length === 0) return;

        setDragPayload(e, {
            filePaths,
            source: 'queue',
        });

        if (filePaths.length === 1) {
            const t = tracks[index]!;

            this.dragImageEl =
                createTrackCardDragImage(
                    t.title,
                    t.artist,
                    t.filePath,
                );
        } else {
            this.dragImageEl = createDragImage(
                filePaths.length,
            );
        }

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

        this.cleanupDragState();
        emitDragActive(false);
    };

    // =================================================================
    // Other handlers
    // =================================================================

    private getDisplayTitle(track: {
        title: string;
        filePath: string;
    }): string {
        if (track.title) return track.title;

        // Fall back to filename without extension.
        const parts = track.filePath.split(/[\\/]/);
        const filename =
            parts[parts.length - 1] ?? track.filePath;

        return filename.replace(/\.[^.]+$/, '');
    }

    private handleMouseDown = (e: MouseEvent) => {
        e.preventDefault();
        this.isDragging = true;
    };

    private handleMouseMove = (e: MouseEvent) => {
        if (!this.isDragging) return;

        const rect = this.getBoundingClientRect();
        const newWidth = rect.right - e.clientX;
        const clampedWidth = Math.min(
            Math.max(newWidth, MIN_WIDTH),
            MAX_WIDTH,
        );

        this.panelWidth = clampedWidth;
        this.style.setProperty(
            '--queue-width',
            `${clampedWidth}px`,
        );
    };

    private handleMouseUp = () => {
        if (!this.isDragging) return;

        this.isDragging = false;
    };

    private renderTrackItem = (
        track: QueueTrack,
        index: number,
    ) => {
        const currentIndex = this.queue.currentIndex;
        const active = index === currentIndex;
        const selected = this.selection.isSelected(
            String(index),
        );

        const dropIdx = this.dropTargetIndex;
        const trackCount = this.queue.tracks.length;
        const showBefore = dropIdx === index;
        const showAfter =
            dropIdx === trackCount &&
            index === trackCount - 1;

        const artUrl = track.coverArtPath || '';

        // The panel's width is user-resizable down to MIN_WIDTH, so
        // both of these are routinely clipped (a11y.24) — and the
        // remove button is one of every row, named identically
        // (a11y.32).
        const title = this.getDisplayTitle(track);
        const artist = track.artist || 'Unknown Artist';

        // No inline closures — all events delegated via data-index
        // on the virtualizer element (see firstUpdated).
        return html`
            <div
                class=${classMap({
                    'track-item': true,
                    active,
                    selected,
                    'drop-before': showBefore,
                    'drop-after': showAfter,
                })}
                data-index=${index}
                data-testid="queue-row"
                data-file-path=${track.filePath}
                draggable="true"
                role="option"
                aria-selected=${selected}
                aria-current=${active ? 'true' : 'false'}
                tabindex=${index === this.focusedIndex ? 0 : -1}
            >
                <span class="track-position">
                    ${index + 1}
                </span>
                ${artUrl ? html`<div class="track-art"><img src="${artUrl}" alt="" loading="lazy" /></div>` : nothing}
                <div class="track-details">
                    <span class="track-title" title=${title}>
                        ${trackLink(title, track.album, track.releaseGroupMbid, track.recordingMbid, undefined, track.artist)}
                    </span>
                    <span class="track-artist" title=${artist}>
                        ${artistLink(track.artist, track.artistMbid) || 'Unknown Artist'}
                    </span>
                </div>
                <button
                    class="remove-button"
                    title="Remove from queue"
                    aria-label="Remove ${title} from queue"
                >
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 384 512" width="14" height="14">
                        ${svg`<path fill="currentColor" d="M342.6 150.6c12.5-12.5 12.5-32.8 0-45.3s-32.8-12.5-45.3 0L192 210.7 86.6 105.4c-12.5-12.5-32.8-12.5-45.3 0s-12.5 32.8 0 45.3L146.7 256 41.4 361.4c-12.5 12.5-12.5 32.8 0 45.3s32.8 12.5 45.3 0L192 301.3 297.4 406.6c12.5 12.5 32.8 12.5 45.3 0s12.5-32.8 0-45.3L237.3 256 342.6 150.6z"/>`}
                    </svg>
                </button>
            </div>
        `;
    };

    override render() {
        const tracks = this.queue.tracks;

        return html`
            <div class="panel-content">
                <div
                    class="resize-handle ${this.isDragging
                        ? 'dragging'
                        : ''}"
                    @mousedown=${this.handleMouseDown}
                ></div>
                <div class="sr-only" role="status" aria-live="polite">
                    ${this.moveAnnouncement}
                </div>
                <div class="header">
                    <div class="header-title">
                        <h3>Queue</h3>
                        ${describeQueueSource(this.queue.source)
                            ? html`
                              <span
                                  class="queue-source ${isQueueSourceNavigable(this.queue.source) ? 'navigable' : ''}"
                                  @click=${(e: MouseEvent) => {
                                      if (!isQueueSourceNavigable(this.queue.source)) return;
                                      navigateToQueueSource(
                                          e.currentTarget as EventTarget,
                                          this.queue.source,
                                      );
                                  }}
                              >${describeQueueSource(this.queue.source)}</span>
                            `
                            : nothing}
                    </div>
                    <div class="header-actions">
                        <button
                            class="header-action-button"
                            @click=${() => void this.handleClearQueue()}
                            ?disabled=${tracks.length === 0}
                            title="Clear queue"
                        >
                            <wa-icon
                                name="trash"
                            ></wa-icon>
                        </button>
                        <button
                            class="header-action-button add-to-playlist-button"
                            @click=${this.handleAddToPlaylist}
                            ?disabled=${tracks.length === 0}
                            title="Add queue to playlist"
                        >
                            <wa-icon
                                name="plus"
                            ></wa-icon>
                        </button>
                    </div>
                </div>

                <wa-popup
                    id="add-to-playlist-popup"
                    placement="bottom-end"
                    .active=${this.playlistPickerOpen}
                >
                    ${this.playlistPickerOpen
                        ? html`
                              <playlist-picker
                                  .filePaths=${tracks.map(
                                      (t) => t.filePath,
                                  )}
                                  @playlist-action-complete=${this
                                      .onPlaylistActionComplete}
                                  @click=${(e: Event) =>
                                      e.stopPropagation()}
                              ></playlist-picker>
                          `
                        : nothing}
                </wa-popup>

                <div
                    class="list-area"
                    @dragenter=${this.onPanelDragEnter}
                    @dragover=${this.onPanelDragOver}
                    @dragleave=${this.onPanelDragLeave}
                    @drop=${this.onPanelDrop}
                >
                    ${!this.open
                        ? nothing
                        : tracks.length === 0
                        ? html`<div class="empty-state">
                              <div class="drop-zone-icon">
                                  <wa-icon
                                      name="plus"
                                  ></wa-icon>
                              </div>
                              <wa-icon
                                  name="list"
                              ></wa-icon>
                              <p>Queue is empty</p>
                              <p style="font-size: 12px;">
                                  Add tracks from your
                                  library or drop them
                                  here.
                              </p>
                          </div>`
                        : html`
                              <lit-virtualizer
                                  scroller
                                  role="listbox"
                                  aria-label="Queue"
                                  aria-multiselectable="true"
                                  .items=${tracks}
                                  .renderItem=${this.renderTrackItem}
                                  .keyFunction=${(track: QueueTrack) => track.id}
                                  .layout=${this.flowLayout}
                              ></lit-virtualizer>
                          `}
                </div>
            </div>

            <wa-popup
                id="context-menu"
                placement="bottom-start"
                flip
                shift
                .active=${this.ctxMenu.contextMenuOpen}
            >
                ${this.ctxMenu.contextMenuOpen
                    ? html`
                          <div class="context-menu-panel" role="menu" aria-label="Queue track actions">
                              <wa-dropdown-item
                                  @click=${() =>
                                      this.onContextMenuAction(
                                          'play',
                                      )}
                                  @mouseenter=${() =>
                                      this.ctxMenu.closePlaylistSubmenu()}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="play"
                                  ></wa-icon>
                                  Play
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  @click=${() =>
                                      this.onContextMenuAction(
                                          'remove',
                                      )}
                                  @mouseenter=${() =>
                                      this.ctxMenu.closePlaylistSubmenu()}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="trash"
                                  ></wa-icon>
                                  Remove from Queue
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  class="submenu-item"
                                  @mouseenter=${() => {
                                      this.ctxMenu.clearSubmenuCloseTimer();
                                      void this.ctxMenu.showPlaylistSubmenu(this.getSelectedFilePaths());
                                  }}
                                  @mouseleave=${this
                                      .ctxMenu.scheduleSubmenuClose}
                                  @click=${(e: Event) => {
                                      e.stopPropagation();
                                      void this.ctxMenu.showPlaylistSubmenu(this.getSelectedFilePaths());
                                  }}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="plus"
                                  ></wa-icon>
                                  Add to Playlist
                                  <span
                                      class="submenu-arrow"
                                  >
                                      &#9654;
                                  </span>
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
                                  ${this.favCtrl.allFavorited(this.getSelectedFilePaths()) ? `Remove from ${this.favCtrl.playlistName}` : `Add to ${this.favCtrl.playlistName}`}
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
                                  Track
                                  Details
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
                ${this.ctxMenu.playlistSubmenuOpen &&
                this.selection.hasSelection
                    ? html`
                          <div
                              @mouseenter=${() =>
                                  this.ctxMenu.clearSubmenuCloseTimer()}
                              @mouseleave=${this
                                  .ctxMenu.scheduleSubmenuClose}
                          >
                              <playlist-picker
                                  id="context-playlist-picker"
                                  .filePaths=${this.ctxMenu.playlistFilePaths}
                                  @playlist-action-complete=${this
                                      .onContextPlaylistActionComplete}
                                  @click=${(e: Event) =>
                                      e.stopPropagation()}
                              ></playlist-picker>
                          </div>
                      `
                    : nothing}
            </wa-popup>

            <track-details></track-details>
        `;
    }
}

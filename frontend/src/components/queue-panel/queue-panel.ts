import { LitElement, html, css, nothing, unsafeCSS } from 'lit';
import {
    customElement,
    property,
    state,
    query,
} from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import { QueueController } from '@store/controllers/queue-controller';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';
import '@lit-labs/virtualizer';
import type { LitVirtualizer } from '@lit-labs/virtualizer';
import { flow } from '@lit-labs/virtualizer/layouts/flow.js';
import type { QueueTrack } from '@store/queue-store';
import { SelectionController } from '@utils/selection-controller';
import type { SelectionHost } from '@utils/selection-controller';
import {
    hasTrackPayload,
    getDragPayload,
    setDragPayload,
    emitDragActive,
    getActiveDragSource,
} from '@utils/drag-controller';
import {
    createDragImage,
    removeDragImage,
} from '@utils/drag-image';

const MIN_WIDTH = 200;
const MAX_WIDTH = 500;
const DEFAULT_WIDTH = 320;

@customElement('queue-panel')
export class QueuePanel
    extends LitElement
    implements SelectionHost
{
    private queue = new QueueController(this);
    private selection = new SelectionController(this);

    @property({ type: Boolean, reflect: true })
    open = false;

    @state()
    private isDragging = false;

    @state()
    private playlistPickerOpen = false;

    @state()
    private contextMenuOpen = false;

    @state()
    private playlistSubmenuOpen = false;

    private dragOver = false;
    private dragEnterCount = 0;

    private dropTargetIndex = -1;
    private dropTargetRafId = 0;

    private autoScrollRafId = 0;
    private autoScrollDelta = 0;

    private dragImageEl: HTMLElement | null = null;

    @query('#add-to-playlist-popup')
    private addToPlaylistPopup!: HTMLElement;

    @query('#context-menu')
    private contextMenuPopup!: HTMLElement;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: HTMLElement;

    @query('lit-virtualizer')
    private virtualizer!: LitVirtualizer;

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

    private closeContextMenuHandler = () =>
        this.closeContextMenu();

    private mousedownCloseHandler = (e: MouseEvent) => {
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
                el.classList.contains('track-item') &&
                this.shadowRoot?.contains(el),
        );

        if (!isTrackClick) {
            this.selection.clear();
        }
    };

    private panelWidth = DEFAULT_WIDTH;
    private flowLayout = flow();

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

    static override styles = css`
        :host {
            flex-shrink: 0;
            width: 0;
            overflow: hidden;
            background-color: var(--yj-bg-surface, #212529);
            display: flex;
            flex-direction: row;
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

        .header h3 {
            margin: 0;
            font-size: 14px;
            font-weight: 600;
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
            color: var(--yj-accent, #ffd43b);
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
            font-size: 12px;
            color: var(--yj-text-tertiary, #888);
            min-width: 20px;
            text-align: right;
        }

        .track-item.active .track-position {
            color: var(--yj-accent, #ffd43b);
        }

        .track-details {
            flex: 1;
            min-width: 0;
            display: flex;
            flex-direction: column;
            gap: 2px;
        }

        .track-title {
            font-size: 13px;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .track-item.active .track-title {
            color: var(--yj-accent, #ffd43b);
        }

        .track-artist {
            font-size: 11px;
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
            opacity: 0;
            transition: opacity 0.15s;
        }

        .track-item:hover .remove-button {
            opacity: 1;
        }

        .remove-button:hover {
            color: var(--yj-error, #ff6b6b);
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
            font-size: 32px;
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
            color: var(--yj-accent, #ffd43b);
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
            --wa-color-text-normal: var(--yj-text-primary, #fff);
            font-size: 13px;
        }

        .context-menu-panel wa-dropdown-item:hover {
            background-color: rgba(255, 255, 255, 0.1);
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
            this.closeContextMenuHandler,
        );
        document.addEventListener(
            'contextmenu',
            this.closeContextMenuHandler,
        );
        document.addEventListener(
            'mousedown',
            this.mousedownCloseHandler,
        );
        document.addEventListener(
            'click',
            this.clearSelectionHandler,
        );
        document.addEventListener(
            'dragend',
            this.onDocumentDragEnd,
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
            this.closeContextMenuHandler,
        );
        document.removeEventListener(
            'contextmenu',
            this.closeContextMenuHandler,
        );
        document.removeEventListener(
            'mousedown',
            this.mousedownCloseHandler,
        );
        document.removeEventListener(
            'click',
            this.clearSelectionHandler,
        );
        document.removeEventListener(
            'dragend',
            this.onDocumentDragEnd,
        );
    }

    override updated() {
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

    private handleClearQueue = () => {
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
            (popup as any).anchor = btn;
            (popup as any).active = this.playlistPickerOpen;
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
            (popup as any).active = false;
        }
    }

    private onPlaylistActionComplete = () => {
        this.closePlaylistPicker();
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
        this.contextMenuOpen = true;

        // Position at mouse cursor using a virtual anchor.
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

    private onContextMenuAction(action: string) {
        const indices =
            this.selection.getSelectedIndices();

        if (indices.length === 0) return;

        switch (action) {
            case 'play':
                this.queue.playAtIndex(indices[0]!);
                break;
            case 'remove':
                this.queue.removeTracksFromQueue(indices);
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
        const trigger =
            this.shadowRoot?.querySelector('.submenu-item');

        if (submenu && trigger) {
            (submenu as any).anchor = trigger;
            (submenu as any).active = true;
        }

        const picker = this.shadowRoot?.querySelector(
            '#context-playlist-picker',
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

    private onContextPlaylistActionComplete = () => {
        this.closeContextMenu(true);
    };

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

        this.dragImageEl = createDragImage(
            filePaths.length,
        );
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

    private handleRemoveTrack(e: Event, position: number) {
        e.stopPropagation();
        this.queue.removeFromQueue(position);
    }

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

        const classes = [
            'track-item',
            active ? 'active' : '',
            selected ? 'selected' : '',
            showBefore ? 'drop-before' : '',
            showAfter ? 'drop-after' : '',
        ]
            .filter(Boolean)
            .join(' ');

        return html`
            <div
                class=${classes}
                data-index=${index}
                draggable="true"
                @click=${(e: MouseEvent) =>
                    this.handleTrackClick(e, track, index)}
                @dblclick=${() =>
                    this.handleTrackDblClick(index)}
                @contextmenu=${(e: MouseEvent) =>
                    this.handleTrackContextMenu(e, index)}
                @dragstart=${(e: DragEvent) =>
                    this.onTrackDragStart(e, index)}
                @dragend=${this.onTrackDragEnd}
            >
                <span class="track-position">
                    ${index + 1}
                </span>
                <div class="track-details">
                    <span class="track-title">
                        ${this.getDisplayTitle(track)}
                    </span>
                    <span class="track-artist">
                        ${track.artist || 'Unknown Artist'}
                    </span>
                </div>
                <button
                    class="remove-button"
                    @click=${(e: Event) =>
                        this.handleRemoveTrack(e, index)}
                    title="Remove from queue"
                >
                    <wa-icon name="xmark"></wa-icon>
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
                <div class="header">
                    <h3>Queue</h3>
                    <div class="header-actions">
                        <button
                            class="header-action-button"
                            @click=${this.handleClearQueue}
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
                    ${tracks.length === 0
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
                                  .items=${tracks}
                                  .renderItem=${this
                                      .renderTrackItem}
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
                .active=${this.contextMenuOpen}
            >
                ${this.contextMenuOpen
                    ? html`
                          <div class="context-menu-panel">
                              <wa-dropdown-item
                                  @click=${() =>
                                      this.onContextMenuAction(
                                          'play',
                                      )}
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
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="trash"
                                  ></wa-icon>
                                  Remove from Queue
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  class="submenu-item"
                                  @mouseenter=${() =>
                                      this.showPlaylistSubmenu()}
                                  @click=${(e: Event) => {
                                      e.stopPropagation();
                                      void this.showPlaylistSubmenu();
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
                ${this.playlistSubmenuOpen &&
                this.selection.hasSelection
                    ? html`
                          <playlist-picker
                              id="context-playlist-picker"
                              .filePaths=${this.getSelectedFilePaths()}
                              @playlist-action-complete=${this
                                  .onContextPlaylistActionComplete}
                              @click=${(e: Event) =>
                                  e.stopPropagation()}
                          ></playlist-picker>
                      `
                    : nothing}
            </wa-popup>
        `;
    }
}

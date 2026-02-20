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
            background-color: #212529;
            display: flex;
            flex-direction: row;
        }

        :host([open]) {
            width: var(
                --queue-width,
                ${unsafeCSS(DEFAULT_WIDTH)}px
            );
            border-left: 1px solid #333;
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
            background-color: #6c757d;
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
            border-bottom: 1px solid #333;
            flex-shrink: 0;
        }

        .header h3 {
            margin: 0;
            font-size: 14px;
            font-weight: 600;
        }

        .add-to-playlist-button {
            background: none;
            border: none;
            color: inherit;
            cursor: pointer;
            padding: 4px;
            display: flex;
            align-items: center;
        }

        .add-to-playlist-button:hover {
            color: #ffd43b;
        }

        .add-to-playlist-button:disabled {
            color: #555;
            cursor: not-allowed;
        }

        #add-to-playlist-popup {
            z-index: 210;
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
                rgba(255, 255, 255, 0.05);
            cursor: default;
            user-select: none;
            width: 100%;
            box-sizing: border-box;
        }

        .track-item:hover {
            background-color: rgba(255, 255, 255, 0.05);
        }

        .track-item.selected {
            background-color: rgba(100, 160, 255, 0.15);
        }

        .track-item.active {
            background-color: rgba(255, 212, 59, 0.1);
        }

        .track-item.selected.active {
            background-color: rgba(100, 160, 255, 0.15);
        }

        .track-position {
            font-size: 12px;
            color: #888;
            min-width: 20px;
            text-align: right;
        }

        .track-item.active .track-position {
            color: #ffd43b;
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
            color: #ffd43b;
        }

        .track-artist {
            font-size: 11px;
            color: #b3b3b3;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .remove-button {
            background: none;
            border: none;
            color: #888;
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
            color: #ff6b6b;
        }

        .panel-content.drag-over {
            outline: 2px dashed #ffd43b;
            outline-offset: -2px;
        }

        .drop-indicator {
            display: none;
            padding: 12px 16px;
            text-align: center;
            font-size: 12px;
            color: #ffd43b;
            border-bottom: 1px solid
                rgba(255, 212, 59, 0.2);
        }

        .panel-content.drag-over.empty-drag
            .drop-indicator {
            display: block;
        }

        .track-item.drop-before::before {
            content: '';
            position: absolute;
            top: -1px;
            left: 8px;
            right: 8px;
            height: 2px;
            background: #ffd43b;
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
            background: #ffd43b;
            border-radius: 1px;
            z-index: 5;
            pointer-events: none;
        }

        .empty-state {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            padding: 40px 20px;
            color: #b3b3b3;
            text-align: center;
            gap: 8px;
        }

        .empty-state wa-icon {
            font-size: 32px;
        }

        #context-menu {
            z-index: 200;
        }

        .context-menu-panel {
            background-color: #343a40;
            border: 1px solid #444;
            border-radius: 6px;
            padding: 4px 0;
            box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
            min-width: 160px;
        }

        .context-menu-panel wa-dropdown-item {
            cursor: pointer;
            --wa-color-text-normal: #fff;
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
                '.panel-content',
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
        // No-op: cleanup is handled by dragend / drop.
        // Firing here would break due to child-boundary
        // and virtualizer re-render events.
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
            <div
                class="panel-content"
                @dragenter=${this.onPanelDragEnter}
                @dragover=${this.onPanelDragOver}
                @dragleave=${this.onPanelDragLeave}
                @drop=${this.onPanelDrop}
            >
                <div
                    class="resize-handle ${this.isDragging
                        ? 'dragging'
                        : ''}"
                    @mousedown=${this.handleMouseDown}
                ></div>
                <div class="header">
                    <h3>Queue</h3>
                    <button
                        class="add-to-playlist-button"
                        @click=${this.handleAddToPlaylist}
                        ?disabled=${tracks.length === 0}
                        title="Add queue to playlist"
                    >
                        <wa-icon name="plus"></wa-icon>
                    </button>
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

                <div class="drop-indicator">
                    Drop tracks here to add to queue
                </div>

                ${tracks.length === 0
                    ? html`
                          <div class="empty-state">
                              <wa-icon name="list"></wa-icon>
                              <p>Queue is empty</p>
                              <p style="font-size: 12px;">
                                  Click a track to start
                                  playing
                              </p>
                          </div>
                      `
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

import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';

import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';

import {
    CreatePlaylist,
    AddTracksToPlaylist,
    RemoveTracksFromPlaylist,
    DeletePlaylist,
    RenamePlaylist,
    ImportPlaylist,
} from '@go/playlist/Service';
import { PlaylistFilePicker } from '@go/frontendutil/FrontendUtil';
import { Events } from '../../events';
import type { playlist } from '@go/models';
import { queueStore } from '@store/queue-store';
import { PlayerController } from '@store/controllers/player-controller';
import { PlaylistController } from '@store/controllers/playlist-controller';
import '@components/track-info/track-info';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';
import { SelectionController } from '@utils/selection-controller';
import type { SelectionHost } from '@utils/selection-controller';
import {
    hasTrackPayload,
    getDragPayload,
    setDragPayload,
    emitDragActive,
    getActiveDragSource,
    getActiveDragPlaylistId,
} from '@utils/drag-controller';
import {
    createDragImage,
    removeDragImage,
} from '@utils/drag-image';

const SCROLL_DEBOUNCE_MS = 100;

interface PlaylistEntry {
    summary: playlist.Summary;
    expanded: boolean;
    tracks: playlist.Track[];
}

@customElement('playlist-view')
export class PlaylistView
    extends LitElement
    implements SelectionHost
{
    private player = new PlayerController(this);
    private playlistCtrl = new PlaylistController(this);
    private selection = new SelectionController(this);
    private cancelScanComplete?: () => void;
    private scrollDebounceTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    /**
     * Index of the playlist whose tracks are currently
     * selectable. -1 means no active selection scope.
     */
    private activePlaylistIndex = -1;

    @state() private entries: PlaylistEntry[] = [];
    @state() private loading = true;
    @state() private creating = false;
    @state() private newPlaylistName = '';
    @state() private contextMenuOpen = false;
    @state() private playlistSubmenuOpen = false;
    @state() private playlistContextMenuOpen = false;
    @state() private playlistContextMenuIndex = -1;
    @state() private renamingPlaylistIndex = -1;
    @state() private renameValue = '';

    /** Index of the playlist currently hovered during a drag. */
    @state() private dragOverPlaylistIndex = -1;

    private dragImageEl: HTMLElement | null = null;

    @query('#context-menu')
    private contextMenuPopup!: HTMLElement;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: HTMLElement;

    @query('#playlist-context-menu')
    private playlistContextMenuPopup!: HTMLElement;

    private closeContextMenuHandler = () => {
        this.closeContextMenu();
        this.closePlaylistContextMenu();
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

    // =================================================================
    // SelectionHost interface
    // =================================================================

    getItemKey(index: number): string | undefined {
        if (this.activePlaylistIndex < 0) return undefined;

        const entry =
            this.entries[this.activePlaylistIndex];

        if (
            !entry ||
            index < 0 ||
            index >= entry.tracks.length
        ) {
            return undefined;
        }

        return String(index);
    }

    getItemCount(): number {
        if (this.activePlaylistIndex < 0) return 0;

        const entry =
            this.entries[this.activePlaylistIndex];

        return entry?.tracks.length ?? 0;
    }

    onSelectionChanged(): void {
        this.requestUpdate();
    }

    /**
     * Return the selected playlist track IDs (database IDs)
     * in order, for removal operations.
     */
    private getSelectedTrackIDs(): number[] {
        if (this.activePlaylistIndex < 0) return [];

        const entry =
            this.entries[this.activePlaylistIndex];

        if (!entry) return [];

        return this.selection
            .getSelectedIndices()
            .map((i) => entry.tracks[i]!.ID);
    }

    /**
     * Derive file paths from selected indices for
     * operations that need file paths.
     */
    private getSelectedFilePaths(): string[] {
        if (this.activePlaylistIndex < 0) return [];

        const entry =
            this.entries[this.activePlaylistIndex];

        if (!entry) return [];

        return this.selection
            .getSelectedIndices()
            .map((i) => entry.tracks[i]!.FilePath);
    }

    /**
     * Ensure the selection scope matches the given playlist
     * index. If switching playlists, clear the old selection.
     */
    private ensureSelectionScope(
        playlistIndex: number,
    ): void {
        if (
            this.activePlaylistIndex !== playlistIndex
        ) {
            this.selection.clear();
            this.activePlaylistIndex = playlistIndex;
        }
    }

    static override styles = css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }

        .header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 16px;
            flex-shrink: 0;
            border-bottom: 1px solid #333;
        }

        .header h2 {
            margin: 0;
            font-size: 18px;
            font-weight: 600;
            color: #fff;
        }

        .new-playlist-button {
            background: none;
            border: 1px solid #555;
            border-radius: 4px;
            color: #fff;
            padding: 6px 12px;
            font-size: 13px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 6px;
            font-family: inherit;
        }

        .new-playlist-button:hover {
            border-color: #ffd43b;
            color: #ffd43b;
        }

        .create-form {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 12px 16px;
            border-bottom: 1px solid #333;
            flex-shrink: 0;
        }

        .create-form input {
            flex: 1;
            background: #2a2d30;
            border: 1px solid #555;
            border-radius: 4px;
            color: #fff;
            padding: 6px 10px;
            font-size: 13px;
            outline: none;
            font-family: inherit;
        }

        .create-form input:focus {
            border-color: #ffd43b;
        }

        .create-form input::placeholder {
            color: #888;
        }

        .create-form button {
            background: #495057;
            border: none;
            border-radius: 4px;
            color: #fff;
            padding: 6px 12px;
            font-size: 13px;
            cursor: pointer;
            font-family: inherit;
        }

        .create-form button:hover {
            background: #5a6268;
        }

        .create-form button.primary {
            background: #ffd43b;
            color: #000;
        }

        .create-form button.primary:hover {
            background: #ffe066;
        }

        .create-form button.primary:disabled {
            background: #665a1e;
            color: #888;
            cursor: not-allowed;
        }

        .playlist-list {
            flex: 1;
            overflow-y: auto;
            padding: 0;
            margin: 0;
            list-style: none;
        }

        .playlist-item {
            border-bottom: 1px solid
                rgba(255, 255, 255, 0.05);
        }

        .playlist-header {
            display: flex;
            align-items: center;
            padding: 12px 16px;
            gap: 10px;
            cursor: pointer;
            user-select: none;
        }

        .playlist-header:hover {
            background-color: rgba(255, 255, 255, 0.05);
        }

        .playlist-item.drag-over > .playlist-header {
            background-color: rgba(255, 212, 59, 0.15);
            outline: 1px dashed #ffd43b;
            outline-offset: -1px;
        }

        .chevron {
            font-size: 14px;
            color: #888;
            flex-shrink: 0;
            transition: transform 0.15s ease;
        }

        .chevron.expanded {
            transform: rotate(90deg);
        }

        .playlist-icon {
            font-size: 18px;
            color: #888;
            flex-shrink: 0;
        }

        .playlist-name {
            font-size: 14px;
            color: #fff;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            flex: 1;
        }

        .track-count {
            font-size: 11px;
            color: #666;
            flex-shrink: 0;
        }

        .playlist-body {
            padding: 0 16px 12px 42px;
        }

        .playlist-actions {
            display: flex;
            align-items: center;
            gap: 8px;
            padding-bottom: 8px;
        }

        .play-all-button {
            background: none;
            border: 1px solid #555;
            border-radius: 4px;
            color: #fff;
            padding: 4px 10px;
            font-size: 12px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 5px;
            font-family: inherit;
        }

        .play-all-button:hover {
            border-color: #ffd43b;
            color: #ffd43b;
        }

        .track-item {
            padding: 6px 0;
            border-bottom: 1px solid
                rgba(255, 255, 255, 0.03);
            cursor: default;
            user-select: none;
        }

        .track-item:hover {
            background-color: rgba(255, 255, 255, 0.05);
        }

        .track-item.selected {
            background-color: rgba(100, 160, 255, 0.15);
        }

        .track-item.active {
            background-color: rgba(255, 212, 59, 0.1);
            color: #ffd43b;
        }

        .track-item.selected.active {
            background-color: rgba(100, 160, 255, 0.15);
        }

        .track-item.phantom {
            opacity: 0.45;
            cursor: not-allowed;
        }

        .track-item.phantom:hover {
            background-color: transparent;
        }

        .phantom-badge {
            display: inline-block;
            font-size: 10px;
            color: #e67700;
            background: rgba(230, 119, 0, 0.15);
            padding: 1px 6px;
            border-radius: 3px;
            margin-left: 8px;
            vertical-align: middle;
        }

        .track-item:last-child {
            border-bottom: none;
        }

        .tracks-empty {
            padding: 12px 0;
            color: #666;
            font-size: 12px;
        }

        .loading {
            display: flex;
            justify-content: center;
            align-items: center;
            padding: 32px;
            color: #b3b3b3;
        }

        .empty-state {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            padding: 48px 20px;
            color: #b3b3b3;
            text-align: center;
            gap: 8px;
        }

        .empty-state wa-icon {
            font-size: 32px;
        }

        .empty-state p {
            margin: 4px 0;
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

        #playlist-context-menu {
            z-index: 200;
        }

        .rename-input {
            flex: 1;
            background: #2a2d30;
            border: 1px solid #ffd43b;
            border-radius: 4px;
            color: #fff;
            padding: 4px 8px;
            font-size: 14px;
            outline: none;
            font-family: inherit;
            min-width: 0;
        }

        .import-button {
            background: none;
            border: 1px solid #555;
            border-radius: 4px;
            color: #fff;
            padding: 6px 12px;
            font-size: 13px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 6px;
            font-family: inherit;
        }

        .import-button:hover {
            border-color: #ffd43b;
            color: #ffd43b;
        }
    `;

    override connectedCallback() {
        super.connectedCallback();
        this.loadPlaylists();
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            () => this.loadPlaylists(),
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
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.cancelScanComplete?.();

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
            this.scrollDebounceTimer = null;
        }

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
    }

    private get scrollContainer(): HTMLElement | null {
        return (
            this.shadowRoot?.querySelector(
                '.playlist-list',
            ) ?? null
        );
    }

    private restoreScrollPosition() {
        const saved =
            this.playlistCtrl.getScrollPosition();

        if (saved > 0 && this.scrollContainer) {
            this.scrollContainer.scrollTop = saved;
        }
    }

    private onScroll = () => {
        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
        }

        this.scrollDebounceTimer = setTimeout(() => {
            if (this.scrollContainer) {
                this.playlistCtrl.setScrollPosition(
                    this.scrollContainer.scrollTop,
                );
            }
        }, SCROLL_DEBOUNCE_MS);
    };

    private async loadPlaylists() {
        try {
            this.loading = true;

            const playlists =
                await this.playlistCtrl.getPlaylists();

            this.entries = playlists.map((p) => ({
                summary: p.Summary,
                expanded: false,
                tracks: p.Tracks ?? [],
            }));
        } catch (err) {
            console.error(
                'Failed to load playlists:',
                err,
            );
            this.entries = [];
        } finally {
            this.loading = false;
        }

        await this.updateComplete;
        this.restoreScrollPosition();
    }

    private handleToggle = (index: number) => {
        const entry = this.entries[index];

        if (!entry) return;

        // If collapsing the active playlist, clear selection.
        if (
            entry.expanded &&
            this.activePlaylistIndex === index
        ) {
            this.selection.clear();
            this.activePlaylistIndex = -1;
        }

        this.entries = this.entries.map((e, i) =>
            i === index
                ? { ...e, expanded: !e.expanded }
                : e,
        );
    };

    private handlePlayAll = (index: number) => {
        const entry = this.entries[index];

        if (!entry || entry.tracks.length === 0)
            return;

        const filePaths = entry.tracks
            .filter((t) => !t.Phantom)
            .map((t) => t.FilePath);

        if (filePaths.length === 0) return;

        queueStore.setQueue(filePaths, 0);
    };

    // =================================================================
    // Track selection & context menu
    // =================================================================

    private handleTrackClick(
        e: MouseEvent,
        _track: playlist.Track,
        trackIndex: number,
        playlistIndex: number,
    ) {
        this.ensureSelectionScope(playlistIndex);
        this.selection.handleItemClick(
            e,
            String(trackIndex),
            trackIndex,
        );
    }

    private handleTrackDblClick(
        _track: playlist.Track,
        trackIndex: number,
        playlistIndex: number,
    ) {
        const entry = this.entries[playlistIndex];

        if (!entry) return;

        this.selection.clear();

        const filePaths = entry.tracks.map(
            (t) => t.FilePath,
        );

        queueStore.setQueue(filePaths, trackIndex);
    }

    private handleTrackContextMenu(
        e: MouseEvent,
        trackIndex: number,
        playlistIndex: number,
    ) {
        e.preventDefault();
        e.stopPropagation();

        this.ensureSelectionScope(playlistIndex);
        this.selection.handleContextMenu(
            String(trackIndex),
        );
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
        const filePaths =
            this.getSelectedFilePaths();

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
            case 'remove':
                void this.removeSelectedTracks();
                break;
        }

        this.closeContextMenu(true);
    }

    private async removeSelectedTracks() {
        if (this.activePlaylistIndex < 0) return;

        const entry =
            this.entries[this.activePlaylistIndex];

        if (!entry) return;

        const trackIDs = this.getSelectedTrackIDs();

        if (trackIDs.length === 0) return;

        try {
            await RemoveTracksFromPlaylist(
                entry.summary.ID,
                trackIDs,
            );

            this.playlistCtrl.invalidate();
            await this.loadPlaylists();
        } catch (err) {
            console.error(
                'Failed to remove tracks:',
                err,
            );
        }
    }

    // =================================================================
    // Drag source (playlist tracks → queue or other playlist)
    // =================================================================

    private onTrackDragStart = (
        e: DragEvent,
        track: playlist.Track,
        trackIndex: number,
        playlistIndex: number,
    ) => {
        this.ensureSelectionScope(playlistIndex);

        const entry = this.entries[playlistIndex];

        if (!entry) return;

        let filePaths: string[];

        if (
            this.activePlaylistIndex ===
                playlistIndex &&
            this.selection.isSelected(
                String(trackIndex),
            )
        ) {
            filePaths = this.getSelectedFilePaths();
        } else {
            filePaths = [track.FilePath];
        }

        if (filePaths.length === 0) return;

        setDragPayload(e, {
            filePaths,
            source: 'playlist',
            sourcePlaylistId: entry.summary.ID,
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

        emitDragActive(false);
    };

    // =================================================================
    // Drop target (tracks dropped onto a specific playlist)
    // =================================================================

    private onPlaylistDragOver = (
        e: DragEvent,
        index: number,
    ) => {
        if (!hasTrackPayload(e)) return;

        // Don't allow dropping tracks back onto
        // the same playlist.
        const entry = this.entries[index];

        if (
            entry &&
            getActiveDragSource() === 'playlist' &&
            getActiveDragPlaylistId() ===
                entry.summary.ID
        ) {
            return;
        }

        e.preventDefault();

        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = 'copy';
        }

        if (this.dragOverPlaylistIndex !== index) {
            this.dragOverPlaylistIndex = index;
        }
    };

    private onPlaylistDragLeave = (
        e: DragEvent,
        index: number,
    ) => {
        // Only clear if we're actually leaving this
        // playlist item (not entering a child).
        const related = e.relatedTarget as Node | null;
        const items =
            this.shadowRoot?.querySelectorAll(
                '.playlist-item',
            );
        const item = items?.[index];

        if (item && !item.contains(related)) {
            if (this.dragOverPlaylistIndex === index) {
                this.dragOverPlaylistIndex = -1;
            }
        }
    };

    private onPlaylistDrop = async (
        e: DragEvent,
        index: number,
    ) => {
        e.preventDefault();
        this.dragOverPlaylistIndex = -1;

        const payload = getDragPayload(e);

        if (
            !payload ||
            payload.filePaths.length === 0
        ) {
            return;
        }

        const entry = this.entries[index];

        if (!entry) return;

        // Don't allow dropping tracks back onto
        // the same playlist.
        if (
            payload.source === 'playlist' &&
            payload.sourcePlaylistId ===
                entry.summary.ID
        ) {
            return;
        }

        try {
            await AddTracksToPlaylist(
                entry.summary.ID,
                payload.filePaths,
            );
            this.playlistCtrl.invalidate();
            await this.loadPlaylists();
        } catch (err) {
            console.error(
                'Failed to add tracks to playlist:',
                err,
            );
        }
    };

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
            this.shadowRoot?.querySelector(
                '.submenu-item',
            );

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

    private isActiveTrack(
        track: playlist.Track,
    ): boolean {
        const currentTrack = this.player.currentTrack;

        if (!currentTrack) return false;

        return currentTrack.filePath === track.FilePath;
    }

    // =================================================================
    // Playlist-level context menu (rename, delete)
    // =================================================================

    private handlePlaylistContextMenu = (
        e: MouseEvent,
        index: number,
    ) => {
        e.preventDefault();
        e.stopPropagation();

        this.closeContextMenu();
        this.playlistContextMenuIndex = index;
        this.playlistContextMenuOpen = true;

        this.updateComplete.then(() => {
            const popup =
                this.playlistContextMenuPopup;

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
    };

    private closePlaylistContextMenu() {
        if (!this.playlistContextMenuOpen) return;

        this.playlistContextMenuOpen = false;
        this.playlistContextMenuIndex = -1;

        const popup =
            this.playlistContextMenuPopup;

        if (popup) {
            (popup as any).active = false;
        }
    }

    private onPlaylistContextAction(
        action: string,
    ) {
        const index =
            this.playlistContextMenuIndex;
        const entry = this.entries[index];

        if (!entry) return;

        switch (action) {
            case 'rename':
                this.renamingPlaylistIndex = index;
                this.renameValue =
                    entry.summary.Name;

                void this.updateComplete.then(
                    () => {
                        const input =
                            this.shadowRoot?.querySelector<HTMLInputElement>(
                                '.rename-input',
                            );

                        input?.focus();
                        input?.select();
                    },
                );
                break;
            case 'delete':
                void this.handleDeletePlaylist(
                    entry.summary.ID,
                );
                break;
        }

        this.closePlaylistContextMenu();
    }

    private async handleDeletePlaylist(
        playlistID: number,
    ) {
        try {
            await DeletePlaylist(playlistID);
            this.playlistCtrl.invalidate();
            await this.loadPlaylists();
        } catch (err) {
            console.error(
                'Failed to delete playlist:',
                err,
            );
        }
    }

    private handleRenameKeydown = async (
        e: KeyboardEvent,
    ) => {
        if (e.key === 'Enter') {
            await this.submitRename();
        } else if (e.key === 'Escape') {
            this.renamingPlaylistIndex = -1;
            this.renameValue = '';
        }
    };

    private handleRenameBlur = async () => {
        await this.submitRename();
    };

    private handleRenameInput = (e: Event) => {
        const input = e.target as HTMLInputElement;
        this.renameValue = input.value;
    };

    private async submitRename() {
        const index = this.renamingPlaylistIndex;

        if (index < 0) return;

        const entry = this.entries[index];

        if (!entry) return;

        const trimmed = this.renameValue.trim();

        this.renamingPlaylistIndex = -1;
        this.renameValue = '';

        if (
            !trimmed ||
            trimmed === entry.summary.Name
        ) {
            return;
        }

        try {
            await RenamePlaylist(
                entry.summary.ID,
                trimmed,
            );
            this.playlistCtrl.invalidate();
            await this.loadPlaylists();
        } catch (err) {
            console.error(
                'Failed to rename playlist:',
                err,
            );
        }
    }

    // =================================================================
    // Import playlist
    // =================================================================

    private handleImportPlaylist = async () => {
        try {
            const filePath =
                await PlaylistFilePicker();

            if (!filePath) return;

            await ImportPlaylist(filePath);
            this.playlistCtrl.invalidate();
            await this.loadPlaylists();
        } catch (err) {
            console.error(
                'Failed to import playlist:',
                err,
            );
        }
    };

    // =================================================================
    // Create playlist
    // =================================================================

    private handleNewPlaylistClick = () => {
        this.creating = true;
        this.newPlaylistName = '';

        void this.updateComplete.then(() => {
            const input =
                this.shadowRoot?.querySelector<HTMLInputElement>(
                    '.create-form input',
                );

            input?.focus();
        });
    };

    private handleCancelCreate = () => {
        this.creating = false;
        this.newPlaylistName = '';
    };

    private handleCreatePlaylist = async () => {
        const name = this.newPlaylistName.trim();
        if (!name) return;

        try {
            await CreatePlaylist(name);
            this.creating = false;
            this.newPlaylistName = '';
            this.playlistCtrl.invalidate();
            await this.loadPlaylists();
        } catch (err) {
            console.error(
                'Failed to create playlist:',
                err,
            );
        }
    };

    private handleInputChange = (e: Event) => {
        const input = e.target as HTMLInputElement;
        this.newPlaylistName = input.value;
    };

    private handleInputKeydown = (e: KeyboardEvent) => {
        if (e.key === 'Enter') {
            void this.handleCreatePlaylist();
        } else if (e.key === 'Escape') {
            this.handleCancelCreate();
        }
    };

    // =================================================================
    // Render
    // =================================================================

    override render() {
        return html`
            <div class="header">
                <h2>Playlists</h2>
                <div
                    style="display: flex; gap: 8px;"
                >
                    <button
                        class="import-button"
                        @click=${this
                            .handleImportPlaylist}
                    >
                        <wa-icon
                            name="file-import"
                        ></wa-icon>
                        Import
                    </button>
                    <button
                        class="new-playlist-button"
                        @click=${this
                            .handleNewPlaylistClick}
                    >
                        <wa-icon
                            name="plus"
                        ></wa-icon>
                        New Playlist
                    </button>
                </div>
            </div>

            ${this.creating
                ? this.renderCreateForm()
                : nothing}
            ${this.loading
                ? html`<div class="loading">
                      Loading playlists...
                  </div>`
                : this.renderPlaylistList()}

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
                                          'add-to-queue',
                                      )}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="plus"
                                  ></wa-icon>
                                  Add to Queue
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  @click=${() =>
                                      this.onContextMenuAction(
                                          'play-next',
                                      )}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="forward-step"
                                  ></wa-icon>
                                  Play Next
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
                                  Remove from Playlist
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
                              .filePaths=${this.getSelectedFilePaths()}
                              @playlist-action-complete=${this
                                  .onPlaylistActionComplete}
                              @click=${(e: Event) =>
                                  e.stopPropagation()}
                          ></playlist-picker>
                      `
                    : nothing}
            </wa-popup>

            <wa-popup
                id="playlist-context-menu"
                placement="bottom-start"
                flip
                shift
                .active=${this
                    .playlistContextMenuOpen}
            >
                ${this.playlistContextMenuOpen
                    ? html`
                          <div
                              class="context-menu-panel"
                          >
                              <wa-dropdown-item
                                  @click=${() =>
                                      this.onPlaylistContextAction(
                                          'rename',
                                      )}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="pen"
                                  ></wa-icon>
                                  Rename
                              </wa-dropdown-item>
                              <wa-dropdown-item
                                  @click=${() =>
                                      this.onPlaylistContextAction(
                                          'delete',
                                      )}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="trash"
                                  ></wa-icon>
                                  Delete Playlist
                              </wa-dropdown-item>
                          </div>
                      `
                    : nothing}
            </wa-popup>
        `;
    }

    private renderCreateForm() {
        const canCreate =
            this.newPlaylistName.trim().length > 0;

        return html`
            <div class="create-form">
                <input
                    type="text"
                    placeholder="Playlist name"
                    .value=${this.newPlaylistName}
                    @input=${this.handleInputChange}
                    @keydown=${this.handleInputKeydown}
                />
                <button
                    @click=${this.handleCancelCreate}
                >
                    Cancel
                </button>
                <button
                    class="primary"
                    ?disabled=${!canCreate}
                    @click=${this.handleCreatePlaylist}
                >
                    Create
                </button>
            </div>
        `;
    }

    private renderPlaylistList() {
        if (this.entries.length === 0) {
            return html`
                <div class="empty-state">
                    <wa-icon name="list"></wa-icon>
                    <p>No playlists yet</p>
                    <p style="font-size: 12px;">
                        Create a playlist to get started.
                    </p>
                </div>
            `;
        }

        return html`
            <ul
                class="playlist-list"
                @scroll=${this.onScroll}
            >
                ${this.entries.map((entry, i) =>
                    this.renderPlaylistItem(entry, i),
                )}
            </ul>
        `;
    }

    private renderPlaylistItem(
        entry: PlaylistEntry,
        index: number,
    ) {
        const trackCount = entry.tracks.length;
        const countLabel = `${trackCount} track${trackCount !== 1 ? 's' : ''}`;
        const isDragOver =
            this.dragOverPlaylistIndex === index;

        const isRenaming =
            this.renamingPlaylistIndex === index;

        return html`
            <li
                class="playlist-item ${isDragOver
                    ? 'drag-over'
                    : ''}"
                @dragover=${(e: DragEvent) =>
                    this.onPlaylistDragOver(e, index)}
                @dragleave=${(e: DragEvent) =>
                    this.onPlaylistDragLeave(e, index)}
                @drop=${(e: DragEvent) =>
                    this.onPlaylistDrop(e, index)}
            >
                <div
                    class="playlist-header"
                    @click=${() =>
                        this.handleToggle(index)}
                    @contextmenu=${(e: MouseEvent) =>
                        this.handlePlaylistContextMenu(
                            e,
                            index,
                        )}
                >
                    <wa-icon
                        class="chevron ${entry.expanded
                            ? 'expanded'
                            : ''}"
                        name="chevron-right"
                    ></wa-icon>
                    <wa-icon
                        class="playlist-icon"
                        name="list"
                    ></wa-icon>
                    ${isRenaming
                        ? html`
                              <input
                                  class="rename-input"
                                  type="text"
                                  .value=${this
                                      .renameValue}
                                  @input=${this
                                      .handleRenameInput}
                                  @keydown=${this
                                      .handleRenameKeydown}
                                  @blur=${this
                                      .handleRenameBlur}
                                  @click=${(
                                      e: Event,
                                  ) =>
                                      e.stopPropagation()}
                              />
                          `
                        : html`
                              <span
                                  class="playlist-name"
                              >
                                  ${entry.summary
                                      .Name}
                              </span>
                          `}
                    <span class="track-count">
                        ${countLabel}
                    </span>
                </div>
                ${entry.expanded
                    ? this.renderPlaylistBody(
                          entry,
                          index,
                      )
                    : nothing}
            </li>
        `;
    }

    private renderPlaylistBody(
        entry: PlaylistEntry,
        playlistIndex: number,
    ) {
        if (entry.tracks.length === 0) {
            return html`
                <div class="playlist-body">
                    <div class="tracks-empty">
                        This playlist is empty.
                    </div>
                </div>
            `;
        }

        return html`
            <div class="playlist-body">
                <div class="playlist-actions">
                    <button
                        class="play-all-button"
                        @click=${(e: Event) => {
                            e.stopPropagation();
                            this.handlePlayAll(
                                playlistIndex,
                            );
                        }}
                    >
                        <wa-icon name="play"></wa-icon>
                        Play All
                    </button>
                </div>
                ${entry.tracks.map(
                    (track, trackIndex) => {
                        const isPhantom =
                            track.Phantom;
                        const active =
                            !isPhantom &&
                            this.isActiveTrack(
                                track,
                            );
                        const selected =
                            !isPhantom &&
                            this.activePlaylistIndex ===
                                playlistIndex &&
                            this.selection.isSelected(
                                String(trackIndex),
                            );

                        const classes = [
                            'track-item',
                            active ? 'active' : '',
                            selected
                                ? 'selected'
                                : '',
                            isPhantom
                                ? 'phantom'
                                : '',
                        ]
                            .filter(Boolean)
                            .join(' ');

                        return html`
                            <div
                                class=${classes}
                                draggable=${isPhantom
                                    ? 'false'
                                    : 'true'}
                                @click=${isPhantom
                                    ? nothing
                                    : (
                                          e: MouseEvent,
                                      ) =>
                                          this.handleTrackClick(
                                              e,
                                              track,
                                              trackIndex,
                                              playlistIndex,
                                          )}
                                @dblclick=${isPhantom
                                    ? nothing
                                    : () =>
                                          this.handleTrackDblClick(
                                              track,
                                              trackIndex,
                                              playlistIndex,
                                          )}
                                @contextmenu=${isPhantom
                                    ? nothing
                                    : (
                                          e: MouseEvent,
                                      ) =>
                                          this.handleTrackContextMenu(
                                              e,
                                              trackIndex,
                                              playlistIndex,
                                          )}
                                @dragstart=${isPhantom
                                    ? nothing
                                    : (
                                          e: DragEvent,
                                      ) =>
                                          this.onTrackDragStart(
                                              e,
                                              track,
                                              trackIndex,
                                              playlistIndex,
                                          )}
                                @dragend=${isPhantom
                                    ? nothing
                                    : this
                                          .onTrackDragEnd}
                            >
                                <track-info
                                    .trackTitle=${track.Title ||
                                    track.FilePath}
                                    .artist=${track.Artist}
                                    .duration=${track.Duration}
                                    .filePath=${track.FilePath}
                                ></track-info>
                                ${isPhantom
                                    ? html`<span
                                          class="phantom-badge"
                                          >File not
                                          found</span
                                      >`
                                    : nothing}
                            </div>
                        `;
                    },
                )}
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'playlist-view': PlaylistView;
    }
}

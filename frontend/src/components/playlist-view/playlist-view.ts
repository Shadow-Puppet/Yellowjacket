import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import type WaPopup from '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';

import {
    CreatePlaylist,
    CreatePlaylistWithTracks,
    AddTracksToPlaylist,
    DeletePlaylist,
    RenamePlaylist,
    ImportPlaylists,
    FindDuplicateTracksInPlaylist,
} from '@go/playlist/Service';
import { PlaylistFilePicker } from '@go/frontendutil/FrontendUtil';
import type { playlist } from '@go/models';
import { PlaylistController } from '@store/controllers/playlist-controller';
import { SearchController } from '@store/controllers/search-controller';
import {
    hasTrackPayload,
    getDragPayload,
    getActiveDragSource,
    getActiveDragPlaylistId,
} from '@utils/drag-controller';
import { contextMenuStyles } from '@utils/context-menu-controller.js';
import { FavoritesController } from '@store/controllers/favorites-controller';
import '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';
import type { DuplicateTracksDialog } from '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';

const SCROLL_DEBOUNCE_MS = 100;

type PlaylistSortField = 'name' | 'created' | 'modified' | 'tracks';
type SortDirection = 'asc' | 'desc';

const PLAYLIST_SORT_KEY = 'playlist-view-sort-field';
const PLAYLIST_SORT_DIR_KEY = 'playlist-view-sort-direction';

const SORT_OPTIONS: { id: PlaylistSortField; label: string }[] = [
    { id: 'modified', label: 'Recent' },
    { id: 'name', label: 'Name' },
    { id: 'created', label: 'Date Created' },
    { id: 'tracks', label: 'Track Count' },
];

interface PlaylistEntry {
    summary: playlist.Summary;
    tracks: playlist.Track[];
}

@customElement('playlist-view')
export class PlaylistView extends LitElement {
    private playlistCtrl = new PlaylistController(this);
    private searchCtrl = new SearchController(this);
    private favCtrl = new FavoritesController(this);

    /** Tracks the store's cached array reference to detect refreshes. */
    private lastPlaylistsRef:
        | playlist.WithTracks[]
        | null = null;

    private scrollDebounceTimer: ReturnType<
        typeof setTimeout
    > | null = null;
    private lastSearchTerm = '';

    // =================================================================
    // Filtered entries (search — playlist name only)
    // =================================================================

    private get filteredEntries(): PlaylistEntry[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        if (!term) return this.entries;

        return this.entries.filter((e) =>
            e.summary.Name.toLowerCase().includes(
                term,
            ),
        );
    }

    @state() private entries: PlaylistEntry[] = [];
    @state() private loading = true;
    @state() private refreshing = false;
    @state() private creating = false;
    @state() private newPlaylistName = '';
    @state() private playlistContextMenuOpen = false;
    @state() private playlistContextMenuIndex = -1;
    @state() private renamingPlaylistIndex = -1;
    @state() private renameValue = '';

    /** Indices of playlists selected via Ctrl/Shift+Click. */
    @state() private selectedPlaylists: Set<number> = new Set();

    /** Anchor index for Shift+Click range selection on playlists. */
    private lastSelectedPlaylistIndex: number | null = null;

    /** Index of the playlist currently hovered during a drag. */
    @state() private dragOverPlaylistIndex = -1;

    /** True when dragging over empty space in the playlist list. */
    @state() private dragOverEmptyZone = false;

    /** True when dragging over the "New Playlist" button. */
    @state() private dragOverNewButton = false;

    /** Error message from the last failed import, auto-clears. */
    @state() private importError = '';

    /** Active sort field for playlists. */
    @state() private sortField: PlaylistSortField = 'modified';

    /** Sort direction. */
    @state() private sortDirection: SortDirection = 'desc';

    /** Whether the sort dropdown is open. */
    @state() private sortDropdownOpen = false;

    @query('#sort-dropdown')
    private sortDropdownPopup!: WaPopup;

    /**
     * File paths from a drop that landed outside any playlist.
     * When non-empty the create form is in "create-and-add" mode.
     */
    private pendingDropPaths: string[] = [];

    @query('#playlist-context-menu')
    private playlistContextMenuPopup!: WaPopup;

    @query('duplicate-tracks-dialog')
    private duplicateDialog!: DuplicateTracksDialog;

    private closePlaylistCtxMenuHandler =
        () => this.closePlaylistContextMenu();

    private playlistCtxMenuMousedownHandler =
        (e: MouseEvent) => {
            const plPopup =
                this.playlistContextMenuPopup;

            if (
                plPopup &&
                e.composedPath().includes(plPopup)
            ) {
                return;
            }

            this.closePlaylistContextMenu();
        };

    private clearSelectionHandler = (e: MouseEvent) => {
        const path = e.composedPath();
        const isPlaylistHeaderClick = path.some(
            (el) =>
                el instanceof HTMLElement &&
                el.classList.contains('playlist-header') &&
                this.shadowRoot?.contains(el),
        );

        if (!isPlaylistHeaderClick) {
            this.selectedPlaylists = new Set();
            this.lastSelectedPlaylistIndex = null;
        }
    };

    static override styles = [
        contextMenuStyles,
        css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
            position: relative;
            contain: layout style;
        }

        .header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 16px;
            flex-shrink: 0;
            border-bottom: 1px solid var(--yj-border-subtle, #333);
        }

        .header h2 {
            margin: 0;
            font-size: 18px;
            font-weight: 600;
            color: var(--yj-text-primary, #fff);
            display: flex;
            align-items: center;
            gap: 10px;
        }

        @keyframes spin {
            to {
                transform: rotate(360deg);
            }
        }

        .header-spinner {
            display: inline-block;
            width: 14px;
            height: 14px;
            border: 2px solid var(--yj-border-subtle, #555);
            border-top-color: var(--yj-text-primary, #fff);
            border-radius: 50%;
            animation: spin 0.6s linear infinite;
        }

        .new-playlist-button {
            background: none;
            border: 1px solid var(--yj-border-subtle, #555);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 6px 12px;
            font-size: 13px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 6px;
            font-family: inherit;
        }

        .new-playlist-button:hover,
        .new-playlist-button.drag-over {
            border-color: var(--yj-accent, #ffd43b);
            color: var(--yj-accent, #ffd43b);
        }

        .new-playlist-button.drag-over {
            background-color: var(
                --yj-accent-bg-strong,
                rgba(255, 212, 59, 0.15)
            );
        }

        .create-form {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 12px 16px;
            border-bottom: 1px solid var(--yj-border-subtle, #333);
            flex-shrink: 0;
        }

        .create-form input {
            flex: 1;
            background: var(--yj-bg-surface, #2b3035);
            border: 1px solid var(--yj-border-subtle, #555);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 6px 10px;
            font-size: 13px;
            outline: none;
            font-family: inherit;
        }

        .create-form input:focus {
            border-color: var(--yj-accent, #ffd43b);
        }

        .create-form input::placeholder {
            color: var(--yj-text-tertiary, #888);
        }

        .create-form button {
            background: var(--yj-bg-overlay, #495057);
            border: none;
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 6px 12px;
            font-size: 13px;
            cursor: pointer;
            font-family: inherit;
        }

        .create-form button:hover {
            background: var(--yj-bg-overlay, #5a6268);
        }

        .create-form button.primary {
            background: var(--yj-accent, #ffd43b);
            color: #000;
        }

        .create-form button.primary:hover {
            background: var(--yj-accent-hover, #ffe066);
        }

        .create-form button.primary:disabled {
            background: var(--yj-accent-muted, #665a1e);
            color: var(--yj-text-tertiary, #888);
            cursor: not-allowed;
        }

        .playlist-list {
            flex: 1;
            overflow-y: auto;
            padding: 0;
            margin: 0;
            list-style: none;
            display: flex;
            flex-direction: column;
            contain: paint;
        }

        .playlist-item {
            border-bottom: 1px solid
                var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
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
            background-color: var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
        }

        .playlist-header.selected {
            background-color: var(--yj-selection-bg, rgba(100, 160, 255, 0.15));
        }

        .playlist-item.drag-over > .playlist-header {
            background-color: var(--yj-accent-bg-strong, rgba(255, 212, 59, 0.15));
            outline: 1px dashed var(--yj-accent, #ffd43b);
            outline-offset: -1px;
        }

        .playlist-icon {
            font-size: 18px;
            color: var(--yj-text-tertiary, #888);
            flex-shrink: 0;
        }

        .playlist-name {
            font-size: 14px;
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            flex: 1;
        }

        .track-count {
            font-size: 11px;
            color: var(--yj-text-tertiary, #666);
            flex-shrink: 0;
        }

        .loading {
            display: flex;
            justify-content: center;
            align-items: center;
            padding: 32px;
            color: var(--yj-text-secondary, #b3b3b3);
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
            color: var(
                --yj-text-secondary,
                #b3b3b3
            );
            font-size: 12px;
            padding: 2px 14px;
            border-radius: 12px;
            border: 1px solid
                var(--yj-border-subtle, #555);
            white-space: nowrap;
            opacity: 0.92;
        }

        .empty-state {
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

        .empty-state.drag-over {
            background-color: var(
                --yj-accent-bg-strong,
                rgba(255, 212, 59, 0.15)
            );
            outline: 2px dashed
                var(--yj-accent, #ffd43b);
            outline-offset: -4px;
        }

        .empty-state.drag-over .drop-zone-icon {
            display: flex;
        }

        .empty-state.drag-over > :not(.drop-zone-icon) {
            display: none;
        }

        .drop-zone {
            flex: 1;
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 80px;
        }

        .drop-zone.drag-over {
            background-color: var(
                --yj-accent-bg-strong,
                rgba(255, 212, 59, 0.15)
            );
            outline: 2px dashed
                var(--yj-accent, #ffd43b);
            outline-offset: -4px;
        }

        .drop-zone.drag-over .drop-zone-icon {
            display: flex;
        }

        #playlist-context-menu {
            z-index: 200;
        }

        .rename-input {
            flex: 1;
            background: var(--yj-bg-surface, #2b3035);
            border: 1px solid var(--yj-accent, #ffd43b);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 4px 8px;
            font-size: 14px;
            outline: none;
            font-family: inherit;
            min-width: 0;
        }

        .import-button {
            background: none;
            border: 1px solid var(--yj-border-subtle, #555);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 6px 12px;
            font-size: 13px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 6px;
            font-family: inherit;
        }

        .import-button:hover {
            border-color: var(--yj-accent, #ffd43b);
            color: var(--yj-accent, #ffd43b);
        }

        .import-error {
            padding: 0.5em 0.75em;
            margin: 0.5em 16px 0;
            font-size: 0.8em;
            color: var(--yj-error, #e03131);
            background: color-mix(
                in srgb,
                var(--yj-error, #e03131) 10%,
                var(--yj-bg-elevated, #343a40)
            );
            border-radius: 4px;
            border-left: 3px solid
                var(--yj-error, #e03131);
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

    `];

    // =================================================================
    // Sort controls
    // =================================================================

    private restoreSortPreferences() {
        try {
            const field =
                localStorage.getItem(PLAYLIST_SORT_KEY);

            if (
                field &&
                SORT_OPTIONS.some(
                    (o) => o.id === field,
                )
            ) {
                this.sortField =
                    field as PlaylistSortField;
            }

            const dir = localStorage.getItem(
                PLAYLIST_SORT_DIR_KEY,
            );

            if (dir === 'asc' || dir === 'desc') {
                this.sortDirection = dir;
            }
        } catch {
            /* localStorage unavailable */
        }
    }

    private saveSortPreferences() {
        try {
            localStorage.setItem(
                PLAYLIST_SORT_KEY,
                this.sortField,
            );
            localStorage.setItem(
                PLAYLIST_SORT_DIR_KEY,
                this.sortDirection,
            );
        } catch {
            /* localStorage unavailable */
        }
    }

    private get sortedEntries(): PlaylistEntry[] {
        const entries = this.filteredEntries;
        const dir =
            this.sortDirection === 'asc' ? 1 : -1;

        return [...entries].sort((a, b) => {
            // Pin default playlist to top when enabled.
            if (this.favCtrl.pinDefault) {
                const aIsDefault =
                    a.summary.ID ===
                    this.favCtrl.playlistId;
                const bIsDefault =
                    b.summary.ID ===
                    this.favCtrl.playlistId;

                if (aIsDefault && !bIsDefault)
                    return -1;

                if (!aIsDefault && bIsDefault)
                    return 1;
            }

            let cmp = 0;

            switch (this.sortField) {
                case 'name':
                    cmp = a.summary.Name.localeCompare(
                        b.summary.Name,
                    );
                    break;
                case 'created':
                    cmp = (
                        a.summary.CreatedAt || ''
                    ).localeCompare(
                        b.summary.CreatedAt || '',
                    );
                    break;
                case 'modified':
                    cmp = (
                        a.summary.UpdatedAt || ''
                    ).localeCompare(
                        b.summary.UpdatedAt || '',
                    );
                    break;
                case 'tracks':
                    cmp =
                        a.tracks.length -
                        b.tracks.length;
                    break;
            }

            return cmp * dir;
        });
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
        const anchor =
            this.shadowRoot?.querySelector(
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

    private onSortDropdownSelect(
        field: PlaylistSortField,
    ) {
        this.sortField = field;
        this.saveSortPreferences();
        this.closeSortDropdown();
    }

    private toggleSortDirection() {
        this.sortDirection =
            this.sortDirection === 'asc'
                ? 'desc'
                : 'asc';
        this.saveSortPreferences();
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

    override connectedCallback() {
        super.connectedCallback();
        this.restoreSortPreferences();
        this.loadPlaylists();
        document.addEventListener(
            'click',
            this.closePlaylistCtxMenuHandler,
        );
        document.addEventListener(
            'contextmenu',
            this.closePlaylistCtxMenuHandler,
        );
        document.addEventListener(
            'mousedown',
            this.playlistCtxMenuMousedownHandler,
        );
        document.addEventListener(
            'click',
            this.clearSelectionHandler,
        );
        document.addEventListener(
            'mousedown',
            this.sortDropdownCloseHandler,
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
            this.scrollDebounceTimer = null;
        }

        document.removeEventListener(
            'click',
            this.closePlaylistCtxMenuHandler,
        );
        document.removeEventListener(
            'contextmenu',
            this.closePlaylistCtxMenuHandler,
        );
        document.removeEventListener(
            'mousedown',
            this.playlistCtxMenuMousedownHandler,
        );
        document.removeEventListener(
            'click',
            this.clearSelectionHandler,
        );
        document.removeEventListener(
            'mousedown',
            this.sortDropdownCloseHandler,
        );
    }

    override updated() {
        const currentTerm = this.searchCtrl.term;

        if (currentTerm !== this.lastSearchTerm) {
            this.lastSearchTerm = currentTerm;
            this.selectedPlaylists = new Set();
            this.lastSelectedPlaylistIndex = null;
        }

        // Re-fetch when the store delivers fresh
        // data after eager refetch on invalidation.
        const cached =
            this.playlistCtrl.cachedPlaylists;

        if (
            cached !== null &&
            cached !== this.lastPlaylistsRef
        ) {
            this.lastPlaylistsRef = cached;
            this.loadPlaylists();
        }
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

    /**
     * Re-fetches playlists without clearing the current view.
     * Shows a spinner in the header while the fetch is in-flight.
     */
    private async refreshPlaylists() {
        this.refreshing = true;

        try {
            const playlists =
                await this.playlistCtrl.refetch();

            this.entries = playlists.map((p) => ({
                summary: p.Summary,
                tracks: p.Tracks ?? [],
            }));
        } catch (err) {
            console.error(
                'Failed to refresh playlists:',
                err,
            );
        } finally {
            this.refreshing = false;
        }
    }

    private handlePlaylistHeaderClick = (
        e: MouseEvent,
        index: number,
    ) => {
        const entry = this.entries[index];

        if (!entry) return;

        const isCtrl = e.ctrlKey || e.metaKey;
        const isShift = e.shiftKey;

        if (isCtrl) {
            // Ctrl/Cmd+Click: toggle playlist in selection
            const next = new Set(this.selectedPlaylists);

            if (next.has(index)) {
                next.delete(index);
            } else {
                next.add(index);
            }

            this.selectedPlaylists = next;
            this.lastSelectedPlaylistIndex = index;
            return;
        }

        if (isShift && this.lastSelectedPlaylistIndex !== null) {
            // Shift+Click: range-select playlists
            const start = Math.min(this.lastSelectedPlaylistIndex, index);
            const end = Math.max(this.lastSelectedPlaylistIndex, index);
            const next = new Set(this.selectedPlaylists);

            for (let i = start; i <= end; i++) {
                next.add(i);
            }

            this.selectedPlaylists = next;
            return;
        }

        // Plain click: navigate to playlist details
        this.selectedPlaylists = new Set();
        this.lastSelectedPlaylistIndex = null;

        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: {
                    view: 'playlist-details',
                    playlistId: entry.summary.ID,
                    playlistName: entry.summary.Name,
                },
            }),
        );
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

        // A specific playlist is targeted — hide the
        // "new playlist" drop zone highlights.
        if (this.dragOverEmptyZone) {
            this.dragOverEmptyZone = false;
        }

        if (this.dragOverNewButton) {
            this.dragOverNewButton = false;
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
        e.stopPropagation();
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
            const result = await FindDuplicateTracksInPlaylist(
                entry.summary.ID,
                payload.filePaths,
            );
            const duplicates = result.Duplicates ?? [];
            const unique = result.Unique ?? [];

            if (duplicates.length > 0) {
                await this.updateComplete;
                this.duplicateDialog.show(
                    entry.summary.ID,
                    duplicates,
                    unique,
                );

                return;
            }

            await AddTracksToPlaylist(
                entry.summary.ID,
                payload.filePaths,
            );
            await this.refreshPlaylists();
        } catch (err) {
            console.error(
                'Failed to add tracks to playlist:',
                err,
            );
        }
    };

    // =================================================================
    // Drop target (empty space → create new playlist)
    // =================================================================

    private onEmptyZoneDragOver = (e: DragEvent) => {
        if (!hasTrackPayload(e)) return;

        e.preventDefault();

        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = 'copy';
        }

        // Only show the "new playlist" drop zone when
        // not hovering a specific playlist item.
        if (
            this.dragOverPlaylistIndex === -1 &&
            !this.dragOverEmptyZone
        ) {
            this.dragOverEmptyZone = true;
        }

        if (this.dragOverNewButton) {
            this.dragOverNewButton = false;
        }
    };

    private onEmptyZoneDragLeave = (e: DragEvent) => {
        const related =
            e.relatedTarget as Node | null;

        if (!related || !this.contains(related)) {
            this.dragOverEmptyZone = false;
        }
    };

    private onEmptyZoneDrop = (e: DragEvent) => {
        e.preventDefault();
        this.dragOverEmptyZone = false;

        const payload = getDragPayload(e);

        if (
            !payload ||
            payload.filePaths.length === 0
        ) {
            return;
        }

        this.pendingDropPaths = payload.filePaths;
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

    // =================================================================
    // Drop target ("New Playlist" button)
    // =================================================================

    private onNewButtonDragOver = (e: DragEvent) => {
        if (!hasTrackPayload(e)) return;

        e.preventDefault();
        e.stopPropagation();

        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = 'copy';
        }

        if (!this.dragOverNewButton) {
            this.dragOverNewButton = true;
        }

        // Hide the empty-zone highlight while
        // hovering the button.
        if (this.dragOverEmptyZone) {
            this.dragOverEmptyZone = false;
        }
    };

    private onNewButtonDragLeave = (
        e: DragEvent,
    ) => {
        const related =
            e.relatedTarget as Node | null;
        const btn =
            this.shadowRoot?.querySelector(
                '.new-playlist-button',
            );

        if (btn && !btn.contains(related)) {
            this.dragOverNewButton = false;
        }
    };

    private onNewButtonDrop = (e: DragEvent) => {
        e.preventDefault();
        e.stopPropagation();
        this.dragOverNewButton = false;
        this.onEmptyZoneDrop(e);
    };

    // =================================================================
    // Playlist-level context menu (rename, delete)
    // =================================================================

    private handlePlaylistContextMenu = (
        e: MouseEvent,
        index: number,
    ) => {
        e.preventDefault();
        e.stopPropagation();

        // If the right-clicked playlist is NOT in the current
        // multi-selection, replace the selection with just that one.
        if (!this.selectedPlaylists.has(index)) {
            this.selectedPlaylists = new Set([index]);
            this.lastSelectedPlaylistIndex = index;
        }

        this.playlistContextMenuIndex = index;
        this.playlistContextMenuOpen = true;

        this.updateComplete.then(() => {
            const popup =
                this.playlistContextMenuPopup;

            if (popup) {
                popup.anchor = {
                    getBoundingClientRect() {
                        return new DOMRect(
                            e.clientX,
                            e.clientY,
                            0,
                            0,
                        );
                    },
                };
                popup.active = true;
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
            popup.active = false;
        }
    }

    private async onPlaylistContextAction(
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
            case 'set-default':
                void this.favCtrl
                    .setDefaultPlaylist(entry.summary.ID)
                    .catch((err: unknown) => {
                        console.error(
                            'Failed to set default playlist:',
                            err,
                        );
                    });
                break;
            case 'delete': {
                if (this.selectedPlaylists.size > 1) {
                    const ids = [...this.selectedPlaylists]
                        .map(i => this.entries[i])
                        .filter((e): e is PlaylistEntry => e !== undefined)
                        .map(e => e.summary.ID);

                    for (const id of ids) {
                        await DeletePlaylist(id);
                    }

                    this.selectedPlaylists = new Set();
                    this.lastSelectedPlaylistIndex = null;
                    await this.refreshPlaylists();
                } else {
                    await this.handleDeletePlaylist(
                        entry.summary.ID,
                    );
                }

                break;
            }
        }

        this.selectedPlaylists = new Set();
        this.lastSelectedPlaylistIndex = null;
        this.closePlaylistContextMenu();
    }

    private async handleDeletePlaylist(
        playlistID: number,
    ) {
        try {
            await DeletePlaylist(playlistID);
            await this.refreshPlaylists();
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
            await this.refreshPlaylists();
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
            const filePaths =
                await PlaylistFilePicker();

            if (!filePaths || filePaths.length === 0)
                return;

            this.importError = '';
            await ImportPlaylists(filePaths);
        } catch (err) {
            console.error(
                'Failed to import playlist:',
                err,
            );
            this.importError =
                err instanceof Error
                    ? err.message
                    : String(err);
            setTimeout(() => {
                this.importError = '';
            }, 6000);
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
        this.pendingDropPaths = [];
    };

    private handleCreatePlaylist = async () => {
        const name = this.newPlaylistName.trim();
        if (!name) return;

        const paths = this.pendingDropPaths;

        try {
            if (paths.length > 0) {
                await CreatePlaylistWithTracks(
                    name,
                    paths,
                );
            } else {
                await CreatePlaylist(name);
            }

            this.creating = false;
            this.newPlaylistName = '';
            this.pendingDropPaths = [];
            await this.refreshPlaylists();
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

    private renderSortToolbar() {
        const activeOption = SORT_OPTIONS.find(
            (o) => o.id === this.sortField,
        );
        const label = activeOption?.label ?? 'Recent';
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

    private renderSortDropdownPopup() {
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
                              ${SORT_OPTIONS.map(
                                  (opt) => html`
                                  <wa-dropdown-item
                                      class=${this.sortField === opt.id ? 'active-sort' : ''}
                                      @click=${() =>
                                          this.onSortDropdownSelect(
                                              opt.id,
                                          )}
                                  >
                                      ${opt.label}
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
        return html`
            <div class="header">
                <h2>
                    Playlists
                    ${this.refreshing
                        ? html`<span
                              class="header-spinner"
                          ></span>`
                        : nothing}
                </h2>
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
                        class="new-playlist-button ${this.dragOverNewButton ? 'drag-over' : ''}"
                        @click=${this
                            .handleNewPlaylistClick}
                        @dragover=${this
                            .onNewButtonDragOver}
                        @dragleave=${this
                            .onNewButtonDragLeave}
                        @drop=${this
                            .onNewButtonDrop}
                    >
                        <wa-icon
                            name="plus"
                        ></wa-icon>
                        New Playlist
                    </button>
                </div>
            </div>

            ${this.importError
                ? html`<div class="import-error">
                      ${this.importError}
                  </div>`
                : nothing}

            ${this.renderSortToolbar()}

            ${this.creating
                ? this.renderCreateForm()
                : nothing}
            ${this.loading &&
            this.entries.length === 0
                ? html`<div class="loading">
                      Loading playlists...
                  </div>`
                : this.renderPlaylistList()}

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
                              ${this.selectedPlaylists.size <= 1
                                  ? html`
                                        <wa-dropdown-item
                                            @click=${() =>
                                                void this.onPlaylistContextAction(
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
                                                void this.onPlaylistContextAction(
                                                    'set-default',
                                                )}
                                        >
                                            <wa-icon
                                                slot="icon"
                                                name="star"
                                            ></wa-icon>
                                            Set as Default Playlist
                                        </wa-dropdown-item>
                                    `
                                  : nothing}
                              <wa-dropdown-item
                                  @click=${() =>
                                      void this.onPlaylistContextAction(
                                          'delete',
                                      )}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name="trash"
                                  ></wa-icon>
                                  ${this.selectedPlaylists.size > 1
                                      ? `Delete ${this.selectedPlaylists.size} Playlists`
                                      : 'Delete Playlist'}
                              </wa-dropdown-item>
                          </div>
                      `
                    : nothing}
            </wa-popup>

            <duplicate-tracks-dialog
                @playlist-action-complete=${() =>
                    this.refreshPlaylists()}
            ></duplicate-tracks-dialog>
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
                <div
                    class="empty-state ${this.dragOverEmptyZone ? 'drag-over' : ''}"
                    @dragover=${this.onEmptyZoneDragOver}
                    @dragleave=${this.onEmptyZoneDragLeave}
                    @drop=${this.onEmptyZoneDrop}
                >
                    <div class="drop-zone-icon">
                        <wa-icon
                            name="plus"
                        ></wa-icon>
                    </div>
                    <wa-icon name="list"></wa-icon>
                    <p>No playlists yet</p>
                    <p style="font-size: 12px;">
                        Create a playlist or drop
                        tracks here.
                    </p>
                </div>
            `;
        }

        const visible = this.sortedEntries;

        if (visible.length === 0) {
            return html`
                <div
                    class="empty-state ${this.dragOverEmptyZone ? 'drag-over' : ''}"
                    @dragover=${this.onEmptyZoneDragOver}
                    @dragleave=${this.onEmptyZoneDragLeave}
                    @drop=${this.onEmptyZoneDrop}
                >
                    <div class="drop-zone-icon">
                        <wa-icon
                            name="plus"
                        ></wa-icon>
                    </div>
                    <p>
                        No playlists match your
                        search.
                    </p>
                </div>
            `;
        }

        return html`
            <ul
                class="playlist-list"
                @scroll=${this.onScroll}
            >
                ${visible.map((entry) => {
                    const originalIndex =
                        this.entries.indexOf(entry);

                    return this.renderPlaylistItem(
                        entry,
                        originalIndex,
                    );
                })}
                <li
                    class="drop-zone ${this.dragOverEmptyZone ? 'drag-over' : ''}"
                    @dragover=${this
                        .onEmptyZoneDragOver}
                    @dragleave=${this
                        .onEmptyZoneDragLeave}
                    @drop=${this.onEmptyZoneDrop}
                >
                    <div class="drop-zone-icon">
                        <wa-icon
                            name="plus"
                        ></wa-icon>
                    </div>
                </li>
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
                    class="playlist-header ${this.selectedPlaylists.has(index) ? 'selected' : ''}"
                    @click=${(e: MouseEvent) =>
                        this.handlePlaylistHeaderClick(e, index)}
                    @contextmenu=${(e: MouseEvent) =>
                        this.handlePlaylistContextMenu(
                            e,
                            index,
                        )}
                >
                    ${entry.summary.ID === this.favCtrl.playlistId
                        ? html`<wa-icon
                              class="playlist-icon"
                              name=${this.favCtrl.iconName}
                          ></wa-icon>`
                        : nothing}
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
            </li>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'playlist-view': PlaylistView;
    }
}

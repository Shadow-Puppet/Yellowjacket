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
    RemoveTracksFromPlaylist,
    DeletePlaylist,
    RenamePlaylist,
    ImportPlaylists,
    RemovePhantomTracks,
    FindDuplicateTracksInPlaylist,
} from '@go/playlist/Service';
import { PlaylistFilePicker } from '@go/frontendutil/FrontendUtil';
import type { playlist } from '@go/models';
import { queueStore } from '@store/queue-store';
import { PlayerController } from '@store/controllers/player-controller';
import { PlaylistController } from '@store/controllers/playlist-controller';
import { SearchController } from '@store/controllers/search-controller';
import '@components/track-info/track-info';
import '@components/playlist-picker/playlist-picker.js';
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
    createTrackCardDragImage,
    removeDragImage,
} from '@utils/drag-image';
import { libraryStore } from '@store/library-store';
import { ContextMenuController } from '@utils/context-menu-controller.js';
import type { ContextMenuHost } from '@utils/context-menu-controller.js';
import { contextMenuStyles } from '@utils/context-menu-controller.js';
import { FavoritesController } from '@store/controllers/favorites-controller';
import '@components/track-details/track-details.js';
import type { TrackDetails } from '@components/track-details/track-details.js';
import type { CoverArtUrls } from '@components/track-details/track-details.js';
import '@components/phantom-resolver/phantom-resolver.js';
import type { PhantomResolver } from '@components/phantom-resolver/phantom-resolver.js';
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
    expanded: boolean;
    tracks: playlist.Track[];
}

@customElement('playlist-view')
export class PlaylistView
    extends LitElement
    implements SelectionHost, ContextMenuHost
{
    private player = new PlayerController(this);
    private playlistCtrl = new PlaylistController(this);
    private searchCtrl = new SearchController(this);
    private selection = new SelectionController(this);
    private ctxMenu = new ContextMenuController(this);
    private favCtrl = new FavoritesController(this);

    getContextMenuPopup(): WaPopup | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup():
        | WaPopup
        | undefined {
        return this.playlistSubmenuPopup;
    }
    /** Tracks the store's cached array reference to detect refreshes. */
    private lastPlaylistsRef:
        | playlist.WithTracks[]
        | null = null;

    private scrollDebounceTimer: ReturnType<
        typeof setTimeout
    > | null = null;
    private lastSearchTerm = '';

    /**
     * Index of the playlist whose tracks are currently
     * selectable. -1 means no active selection scope.
     */
    private activePlaylistIndex = -1;

    // =================================================================
    // Filtered entries (search)
    // =================================================================

    private get filteredEntries(): PlaylistEntry[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        if (!term) return this.entries;

        return this.entries.filter(
            (e) =>
                e.summary.Name.toLowerCase().includes(
                    term,
                ) ||
                e.tracks.some(
                    (t) =>
                        t.Title.toLowerCase().includes(
                            term,
                        ) ||
                        t.Artist.toLowerCase().includes(
                            term,
                        ),
                ),
        );
    }

    /**
     * Return the tracks to display for a playlist entry,
     * preserving original indices for event handlers.
     * When a search term is active, only tracks matching
     * the term are shown.  When there is no search term
     * (or the playlist matched by name), all tracks are
     * returned.
     */
    private getVisibleTracks(
        entry: PlaylistEntry,
    ): { track: playlist.Track; trackIndex: number }[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        if (!term) {
            return entry.tracks.map(
                (track, trackIndex) => ({
                    track,
                    trackIndex,
                }),
            );
        }

        // If the playlist name itself matches, show
        // all tracks — the whole playlist is relevant.
        if (
            entry.summary.Name.toLowerCase().includes(
                term,
            )
        ) {
            return entry.tracks.map(
                (track, trackIndex) => ({
                    track,
                    trackIndex,
                }),
            );
        }

        // Otherwise only show tracks whose metadata
        // matches.
        return entry.tracks
            .map((track, trackIndex) => ({
                track,
                trackIndex,
            }))
            .filter(
                ({ track }) =>
                    track.Title.toLowerCase().includes(
                        term,
                    ) ||
                    track.Artist.toLowerCase().includes(
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

    private dragImageEl: HTMLElement | null = null;

    @query('#context-menu')
    private contextMenuPopup!: WaPopup;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: WaPopup;

    @query('#playlist-context-menu')
    private playlistContextMenuPopup!: WaPopup;

    @query('track-details')
    private trackDetailsDialog!: TrackDetails;

    @query('phantom-resolver')
    private phantomResolver!: PhantomResolver;

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

    private handleSelectAll = (): void => {
        this.selection.selectAll();
    };

    private clearSelectionHandler = (e: MouseEvent) => {
        const path = e.composedPath();
        const isTrackClick = path.some(
            (el) =>
                el instanceof HTMLElement &&
                el.classList.contains('track-item') &&
                this.shadowRoot?.contains(el),
        );
        const isPlaylistHeaderClick = path.some(
            (el) =>
                el instanceof HTMLElement &&
                el.classList.contains('playlist-header') &&
                this.shadowRoot?.contains(el),
        );

        if (!isTrackClick) {
            this.selection.clear();
        }

        if (!isPlaylistHeaderClick && !isTrackClick) {
            this.selectedPlaylists = new Set();
            this.lastSelectedPlaylistIndex = null;
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
        // Clear playlist-level selection when entering track selection
        if (this.selectedPlaylists.size > 0) {
            this.selectedPlaylists = new Set();
            this.lastSelectedPlaylistIndex = null;
        }

        if (
            this.activePlaylistIndex !== playlistIndex
        ) {
            this.selection.clear();
            this.activePlaylistIndex = playlistIndex;
        }
    }

    static override styles = [
        contextMenuStyles,
        css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
            position: relative;
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

        .chevron {
            font-size: 14px;
            color: var(--yj-text-tertiary, #888);
            flex-shrink: 0;
            transition: transform 0.15s ease;
        }

        .chevron.expanded {
            transform: rotate(90deg);
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

        .playlist-body {
            padding: 0 16px 12px 32px;
        }

        .playlist-actions {
            display: flex;
            align-items: center;
            gap: 8px;
            padding-bottom: 8px;
        }

        .play-all-button {
            background: none;
            border: 1px solid var(--yj-border-subtle, #555);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 4px 10px;
            font-size: 12px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 5px;
            font-family: inherit;
        }

        .play-all-button:hover {
            border-color: var(--yj-accent, #ffd43b);
            color: var(--yj-accent, #ffd43b);
        }

        .track-item {
            padding: 6px 0;
            border-bottom: 1px solid
                rgba(255, 255, 255, 0.03);
            cursor: default;
            user-select: none;
        }

        .track-item:hover {
            background-color: var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
        }

        .track-item.selected {
            background-color: var(--yj-selection-bg, rgba(100, 160, 255, 0.15));
        }

        .track-item.active {
            background-color: var(--yj-accent-bg, rgba(255, 212, 59, 0.1));
            color: var(--yj-accent, #ffd43b);
        }

        .track-item.selected.active {
            background-color: var(--yj-selection-bg, rgba(100, 160, 255, 0.15));
        }

        .track-item.phantom {
            cursor: pointer;
        }

        .track-item.phantom:hover {
            background-color: var(
                --yj-hover-overlay,
                rgba(255, 255, 255, 0.05)
            );
        }

        .track-item.phantom.selected {
            background-color: var(
                --yj-selection-bg,
                rgba(100, 160, 255, 0.15)
            );
        }

        .phantom-row {
            display: flex;
            align-items: center;
            gap: 8px;
            min-width: 0;
            width: 100%;
        }

        .phantom-caution {
            flex-shrink: 0;
            font-size: 14px;
            color: var(--yj-warning, #e67700);
        }

        .phantom-path {
            flex: 1;
            min-width: 0;
            font-size: 12px;
            color: var(--yj-text-tertiary, #888);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .phantom-actions {
            display: flex;
            align-items: center;
            gap: 2px;
            flex-shrink: 0;
        }

        .phantom-icon-btn {
            display: flex;
            align-items: center;
            justify-content: center;
            background: none;
            border: none;
            color: var(--yj-text-tertiary, #888);
            cursor: pointer;
            padding: 4px;
            border-radius: 3px;
            font-size: 13px;
        }

        .phantom-icon-btn:hover {
            color: var(
                --yj-text-primary,
                #fff
            );
            background: rgba(
                255,
                255,
                255,
                0.08
            );
        }

        .phantom-icon-btn.phantom-icon-remove:hover {
            color: var(--yj-error, #e03131);
            background: rgba(224, 49, 49, 0.12);
        }

        .track-item:last-child {
            border-bottom: none;
        }

        .tracks-empty {
            padding: 12px 0;
            color: var(--yj-text-tertiary, #666);
            font-size: 12px;
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
        document.addEventListener(
            'shortcut:select-all',
            this.handleSelectAll,
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
        document.removeEventListener(
            'shortcut:select-all',
            this.handleSelectAll,
        );
    }

    override updated() {
        const currentTerm = this.searchCtrl.term;

        if (currentTerm !== this.lastSearchTerm) {
            this.lastSearchTerm = currentTerm;
            this.selection.clear();
            this.activePlaylistIndex = -1;

            const term = currentTerm.toLowerCase();

            this.entries = this.entries.map((e) => {
                if (!term) {
                    return { ...e, expanded: false };
                }

                const hasTrackMatch = e.tracks.some(
                    (t) =>
                        t.Title.toLowerCase().includes(
                            term,
                        ) ||
                        t.Artist.toLowerCase().includes(
                            term,
                        ),
                );

                return {
                    ...e,
                    expanded: hasTrackMatch,
                };
            });
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

    /**
     * Re-fetches playlists without clearing the current view.
     * Shows a spinner in the header while the fetch is in-flight
     * and preserves the expanded/collapsed state of each playlist.
     */
    private async refreshPlaylists() {
        this.refreshing = true;

        try {
            const playlists =
                await this.playlistCtrl.refetch();

            const expandedIDs = new Set(
                this.entries
                    .filter((e) => e.expanded)
                    .map((e) => e.summary.ID),
            );

            this.entries = playlists.map((p) => ({
                summary: p.Summary,
                expanded: expandedIDs.has(
                    p.Summary.ID,
                ),
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
            // Clear track-level selection
            this.selection.clear();
            this.activePlaylistIndex = -1;
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
            // Clear track-level selection
            this.selection.clear();
            this.activePlaylistIndex = -1;
            return;
        }

        // Plain click: clear playlist selection, toggle expand/collapse
        this.selectedPlaylists = new Set();
        this.lastSelectedPlaylistIndex = null;

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

        queueStore.setQueue(filePaths, 0, true);
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
        this.ctxMenu.openAt(e.clientX, e.clientY);
    }

    private onContextMenuAction(action: string) {
        const filePaths =
            this.getSelectedFilePaths();

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                queueStore.setQueue(filePaths, 0, true);
                break;
            case 'add-to-queue':
                queueStore.addTracksToQueue(
                    filePaths,
                );
                break;
            case 'play-next':
                queueStore.playTracksNext(filePaths);
                break;
            case 'remove':
                void this.removeSelectedTracks();
                break;
            case 'track-details':
                this.openTrackDetails(filePaths[0]!);
                break;
            case 'phantom-locate':
                if (this.activePlaylistIndex >= 0) {
                    this.openPhantomResolver(
                        this.activePlaylistIndex,
                    );
                }

                break;
            case 'phantom-remove':
                void this.removeSelectedPhantoms();
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

    private async removeSelectedPhantoms(): Promise<void> {
        if (this.activePlaylistIndex < 0) return;

        const entry =
            this.entries[
                this.activePlaylistIndex
            ];

        if (!entry) return;

        const selectedIndices =
            this.selection.getSelectedIndices();
        const phantomPaths = selectedIndices
            .map((i) => entry.tracks[i])
            .filter(
                (t): t is playlist.Track =>
                    t !== undefined &&
                    t.Phantom,
            )
            .map((t) => t.FilePath);

        if (phantomPaths.length === 0) return;

        try {
            await RemovePhantomTracks(
                entry.summary.ID,
                phantomPaths,
            );
            await this.refreshPlaylists();
        } catch (err) {
            console.error(
                'Failed to remove phantom tracks:',
                err,
            );
        }
    }

    private openTrackDetails(filePath: string) {
        const tracks =
            libraryStore.getCachedTracks();
        const track = tracks?.find(
            (t) => t.FilePath === filePath,
        );

        if (!track) return;

        const coverArt =
            this.resolvePlaylistCoverArt(
                track.Album,
            );

        this.trackDetailsDialog?.show(
            track,
            coverArt ?? undefined,
        );
    }

    private resolvePlaylistCoverArt(
        albumName: string,
    ): CoverArtUrls | null {
        if (!albumName) return null;

        const albums =
            libraryStore.getCachedAlbums();

        if (!albums) return null;

        const album = albums.find(
            (a) => a.Name === albumName,
        );

        if (!album || !album.CoverArtPath) {
            return null;
        }

        return {
            coverArtPath: album.CoverArtPath,
            coverArtSmall: album.CoverArtSmall,
            coverArtMedium: album.CoverArtMedium,
            coverArtLarge: album.CoverArtLarge,
        };
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

            await this.refreshPlaylists();
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

        this.dragImageEl =
            filePaths.length === 1
                ? createTrackCardDragImage(
                      track.Title,
                      track.Artist,
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

        this.ctxMenu.close();

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

    /**
     * Check whether all currently selected tracks are phantoms.
     * Returns false if nothing is selected or the active playlist
     * index is unset.
     */
    private isPhantomSelection(): boolean {
        if (this.activePlaylistIndex < 0) return false;

        const entry =
            this.entries[this.activePlaylistIndex];

        if (!entry) return false;

        const indices =
            this.selection.getSelectedIndices();

        if (indices.length === 0) return false;

        return indices.every((i) => {
            const t = entry.tracks[i];

            return t !== undefined && t.Phantom;
        });
    }

    // =================================================================
    // Phantom track interactions
    // =================================================================

    private handlePhantomClick(
        e: MouseEvent,
        trackIndex: number,
        playlistIndex: number,
    ): void {
        this.ensureSelectionScope(playlistIndex);
        this.selection.handleItemClick(
            e,
            String(trackIndex),
            trackIndex,
        );
    }

    private handlePhantomContextMenu(
        e: MouseEvent,
        trackIndex: number,
        playlistIndex: number,
    ): void {
        e.preventDefault();
        e.stopPropagation();
        this.ensureSelectionScope(playlistIndex);
        this.selection.handleContextMenu(
            String(trackIndex),
        );
        this.ctxMenu.openAt(e.clientX, e.clientY);
    }

    private openPhantomResolver(
        playlistIndex: number,
        trackIndex?: number,
    ): void {
        const entry =
            this.entries[playlistIndex];

        if (!entry) return;

        // Collect selected phantom tracks, or just the
        // one that was clicked.
        let phantoms: playlist.Track[];

        if (
            this.activePlaylistIndex ===
            playlistIndex
        ) {
            const selectedIndices =
                this.selection.getSelectedIndices();
            phantoms = selectedIndices
                .map(
                    (i) => entry.tracks[i],
                )
                .filter(
                    (t): t is playlist.Track =>
                        t !== undefined &&
                        t.Phantom,
                );
        } else {
            phantoms = [];
        }

        // Fall back to the clicked track.
        if (
            phantoms.length === 0 &&
            trackIndex !== undefined
        ) {
            const track =
                entry.tracks[trackIndex];

            if (track?.Phantom) {
                phantoms = [track];
            }
        }

        if (phantoms.length === 0) return;

        this.phantomResolver.show(
            entry.summary.ID,
            phantoms,
        );
    }

    private async removePhantomTrack(
        playlistIndex: number,
        trackIndex: number,
    ): Promise<void> {
        const entry =
            this.entries[playlistIndex];

        if (!entry) return;

        const track =
            entry.tracks[trackIndex];

        if (!track?.Phantom) return;

        try {
            await RemovePhantomTracks(
                entry.summary.ID,
                [track.FilePath],
            );
            await this.refreshPlaylists();
        } catch (err) {
            console.error(
                'Failed to remove phantom track:',
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
                id="context-menu"
                placement="bottom-start"
                flip
                shift
                .active=${this.ctxMenu
                    .contextMenuOpen}
            >
                ${this.ctxMenu.contextMenuOpen
                    ? this.isPhantomSelection()
                        ? html`
                              <div class="context-menu-panel">
                                  <wa-dropdown-item
                                      @click=${() =>
                                          this.onContextMenuAction(
                                              'phantom-locate',
                                          )}
                                  >
                                      <wa-icon
                                          slot="icon"
                                          name="magnifying-glass"
                                      ></wa-icon>
                                      Locate in
                                      Library
                                  </wa-dropdown-item>
                                  <wa-dropdown-item
                                      @click=${() =>
                                          this.onContextMenuAction(
                                              'phantom-remove',
                                          )}
                                  >
                                      <wa-icon
                                          slot="icon"
                                          name="trash"
                                      ></wa-icon>
                                      Remove from
                                      Playlist
                                  </wa-dropdown-item>
                              </div>
                          `
                        : html`
                              <div class="context-menu-panel">
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
                                              'add-to-queue',
                                          )}
                                      @mouseenter=${() =>
                                          this.ctxMenu.closePlaylistSubmenu()}
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
                                      @mouseenter=${() =>
                                          this.ctxMenu.closePlaylistSubmenu()}
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
                                      @mouseenter=${() =>
                                          this.ctxMenu.closePlaylistSubmenu()}
                                  >
                                      <wa-icon
                                          slot="icon"
                                          name="trash"
                                      ></wa-icon>
                                      Remove from
                                      Playlist
                                  </wa-dropdown-item>
                                  <wa-dropdown-item
                                      class="submenu-item"
                                      @mouseenter=${() => {
                                          this.ctxMenu.clearSubmenuCloseTimer();
                                          void this.ctxMenu.showPlaylistSubmenu(this.getSelectedFilePaths());
                                      }}
                                      @mouseleave=${this
                                          .ctxMenu
                                          .scheduleSubmenuClose}
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
                                  ${this.selection
                                      .selectionCount ===
                                  1
                                      ? html`
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
                                        `
                                      : nothing}
                              </div>
                          `
                    : nothing}
            </wa-popup>

            <wa-popup
                id="playlist-submenu"
                placement="right-start"
                flip
                shift
                .active=${this.ctxMenu
                    .playlistSubmenuOpen}
            >
                ${this.ctxMenu.playlistSubmenuOpen &&
                this.selection.hasSelection
                    ? html`
                          <div
                              @mouseenter=${() =>
                                  this.ctxMenu.clearSubmenuCloseTimer()}
                              @mouseleave=${this
                                  .ctxMenu
                                  .scheduleSubmenuClose}
                          >
                              <playlist-picker
                                  .filePaths=${this.getSelectedFilePaths()}
                                  @playlist-action-complete=${this
                                      .ctxMenu
                                      .onPlaylistActionComplete}
                                  @click=${(e: Event) =>
                                      e.stopPropagation()}
                              ></playlist-picker>
                          </div>
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

            <track-details></track-details>
            <phantom-resolver
                @phantom-resolved=${() =>
                    this.refreshPlaylists()}
            ></phantom-resolver>
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
                    <wa-icon
                        class="chevron ${entry.expanded
                            ? 'expanded'
                            : ''}"
                        name="chevron-right"
                    ></wa-icon>
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
                ${this.getVisibleTracks(entry).map(
                    ({ track, trackIndex }) => {
                        const isPhantom =
                            track.Phantom;
                        const active =
                            !isPhantom &&
                            this.isActiveTrack(
                                track,
                            );
                        const selected =
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
                                    ? (
                                          e: MouseEvent,
                                      ) =>
                                          this.handlePhantomClick(
                                              e,
                                              trackIndex,
                                              playlistIndex,
                                          )
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
                                    ? (
                                          e: MouseEvent,
                                      ) =>
                                          this.handlePhantomContextMenu(
                                              e,
                                              trackIndex,
                                              playlistIndex,
                                          )
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
                                ${isPhantom
                                    ? html`<div
                                          class="phantom-row"
                                      >
                                          <wa-icon
                                              class="phantom-caution"
                                              name="triangle-exclamation"
                                              title="File not found"
                                          ></wa-icon>
                                          <span
                                              class="phantom-path"
                                              title=${track.FilePath}
                                          >
                                              ${track.FilePath}
                                          </span>
                                          <div
                                              class="phantom-actions"
                                          >
                                              <button
                                                  class="phantom-icon-btn"
                                                  title="Locate in library"
                                                  @click=${(
                                                      e: Event,
                                                  ) => {
                                                      e.stopPropagation();
                                                      this.openPhantomResolver(
                                                          playlistIndex,
                                                          trackIndex,
                                                      );
                                                  }}
                                              >
                                                  <wa-icon
                                                      name="magnifying-glass"
                                                  ></wa-icon>
                                              </button>
                                              <button
                                                  class="phantom-icon-btn phantom-icon-remove"
                                                  title="Remove from playlist"
                                                  @click=${(
                                                      e: Event,
                                                  ) => {
                                                      e.stopPropagation();
                                                      void this.removePhantomTrack(
                                                          playlistIndex,
                                                          trackIndex,
                                                      );
                                                  }}
                                              >
                                                  <wa-icon
                                                      name="xmark"
                                                  ></wa-icon>
                                              </button>
                                          </div>
                                      </div>`
                                    : html`<track-info
                                          .trackTitle=${track.Title ||
                                          track.FilePath}
                                          .artist=${track.Artist}
                                          .duration=${track.Duration}
                                          .filePath=${track.FilePath}
                                      ></track-info>`}
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

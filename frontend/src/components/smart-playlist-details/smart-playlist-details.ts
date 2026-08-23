import { LitElement, html, css, nothing } from 'lit';
import {
    customElement,
    property,
    state,
    query,
} from 'lit/decorators.js';
import type * as playlist from '@go/playlist/models.js';
import {
    GetSmartPlaylistTracks,
    RefreshSmartPlaylist,
    GetSmartPlaylistRules,
    UpdateSmartPlaylistRules,
} from '@go/playlist/service.js';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import { queueStore } from '@store/queue-store';
import { creditStore } from '@store/credit-store';
import { PlayerController } from '@store/controllers/player-controller';
import { SearchController } from '@store/controllers/search-controller';
import '../search-dialog/search-trigger';
import { SelectionController } from '@utils/selection-controller';
import type { SelectionHost } from '@utils/selection-controller';
import {
    ContextMenuController,
    contextMenuStyles,
    isContextMenuKey,
} from '@utils/context-menu-controller.js';
import type { ContextMenuHost, MenuTarget } from '@utils/context-menu-controller.js';
import { focusRovingRow, nextRovingIndex } from '@utils/roving-rows';
import type { GestureEvent } from '@utils/touch-gestures';
import { SwipeToQueue, swipeRevealStyles } from '@utils/swipe-to-queue';
import '@components/selection-bar/selection-bar';
import type { SelectionAction } from '@components/selection-bar/selection-bar';
import { FavoritesController } from '@store/controllers/favorites-controller';
import {
    setDragPayload,
    emitDragActive,
} from '@utils/drag-controller';
import {
    createDragImage,
    createTrackCardDragImage,
    removeDragImage,
} from '@utils/drag-image';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import type { MenuSurface } from '../menu-surface/menu-surface';
import '../menu-surface/menu-surface';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@lit-labs/virtualizer';
import type { LitVirtualizer } from '@lit-labs/virtualizer';
import { flow } from '@lit-labs/virtualizer/layouts/flow.js';
import '@components/playlist-picker/playlist-picker.js';
import { loadTrackDetails } from '@utils/lazy-track-details.js';
import { tracksByFilePath, tracksForPaths } from '@utils/track-index.js';
import type { TrackDetails } from '@components/track-details/track-details.js';
import type { CoverArtUrls } from '@components/track-details/track-details.js';
import { libraryStore } from '@store/library-store';
import { formatMilliseconds } from '@utils/time';
import {
    creditLink,
    creditText,
    albumLink,
    trackLink,
    exploreLinkStyles,
} from '@utils/explore-link';
import { goToMenuItems } from '@utils/go-to-menu';
import type { GoToTarget } from '@utils/go-to-menu';
import '@components/smart-playlist-editor/smart-playlist-editor.js';
import { designTokens } from '../../styles/tokens.css';
import { backButton } from '../../styles/back-button.css';
import { srOnly } from '../../styles/sr-only.css';
import { list } from '@utils/binding';
import {
    ICON_PLAY,
    ICON_PLAY_NEXT,
    ICON_PLAYLIST,
    ICON_QUEUE,
    ICON_SMART_PLAYLIST,
} from '@utils/icon-language';


/**
 * Format total milliseconds as a human-readable duration.
 * e.g. 8_100_000 → "2h 15m", 180_000 → "3m 0s", 45_000 → "0m 45s"
 */
function formatTotalDuration(totalMs: number): string {
    if (totalMs <= 0) return '0m';

    const totalSeconds = Math.floor(totalMs / 1000);
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);

    if (hours > 0) {
        return `${hours}h ${minutes}m`;
    }

    const seconds = totalSeconds % 60;

    if (minutes > 0) {
        return `${minutes}m ${seconds}s`;
    }

    return `${seconds}s`;
}

/** One row: the track and its position in the *playlist*, which is not
 *  its position in the filtered view. */
interface VisibleTrack {
    track: playlist.Track;
    trackIndex: number;
}

@customElement('smart-playlist-details')
export class SmartPlaylistDetails
    extends LitElement
    implements SelectionHost, ContextMenuHost
{
    @property({ type: Number, attribute: 'playlist-id' })
    playlistId = 0;

    @property({ type: String, attribute: 'playlist-name' })
    playlistName = '';

    @property({ type: Boolean, attribute: 'auto-edit' })
    autoEdit = false;

    @state()
    private tracks: playlist.Track[] = [];

    @state()
    private loading = true;

    @state()
    private editing = false;

    @state()
    private currentRulesJSON = '';

    @state()
    private pendingRulesJSON = '';

    @state()
    private saving = false;

    @state()
    private refreshing = false;

    private player = new PlayerController(this);
    private searchCtrl = new SearchController(this);
    private selection = new SelectionController(this);
    private ctxMenu = new ContextMenuController(this);

    /* _itemSize matches the fixed 45 px .track-item height, measured
     * rather than guessed: without the hint the flow layout's 100 px
     * default drives constant scroll-error correction, which reads as
     * the list jumping under the pointer. */
    /** Unsubscribes the credit-arrival repaint. */
    private creditsUnsub?: () => void;

    @query('lit-virtualizer')
    private virtualizer?: LitVirtualizer;

    /* The row templates live inside the virtualizer, not in this
     * component's own template, so a host re-render alone does not
     * repaint them: the virtualizer re-renders when its `items` change,
     * and `items` is now memoised precisely so it does not change when
     * nothing has. Selection and the playing-track highlight are
     * therefore pushed to it explicitly, which is what `track-list` has
     * always done. Costing ~36 rows, not the whole playlist. */
    private lastActiveTrackPath: string | null = null;

    private flowLayout = flow({
        _itemSize: { width: 100, height: 45 },
    } as Parameters<typeof flow>[0]);

    private visibleCache: VisibleTrack[] | null = null;
    private visibleCacheKey: {
        tracks: playlist.Track[];
        term: string;
    } | null = null;
    private favCtrl = new FavoritesController(this);

    private playlistDeletedCleanup: (() => void) | null = null;
    private playlistRenamedCleanup: (() => void) | null = null;

    private dragImageEl: HTMLElement | null = null;

    @query('#context-menu')
    private contextMenuPopup!: MenuSurface;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: MenuSurface;

    @query('track-details')
    private trackDetailsDialog!: TrackDetails;

    // =================================================================
    // ContextMenuHost interface
    // =================================================================

    getContextMenuPopup(): MenuTarget | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup(): MenuTarget | undefined {
        return this.playlistSubmenuPopup;
    }

    onContextMenuClose(): void {
        // No-op.
    }

    // =================================================================
    // SelectionHost interface
    // =================================================================

    getItemKey(index: number): string | undefined {
        if (index < 0 || index >= this.tracks.length) {
            return undefined;
        }

        return String(index);
    }

    getItemCount(): number {
        return this.tracks.length;
    }

    onSelectionChanged(): void {
        this.requestUpdate();
        this.virtualizer?.requestUpdate();
    }

    protected override updated(
        changed: Map<string, unknown>,
    ): void {
        super.updated(changed);

        const currentPath =
            this.player.currentTrack?.filePath ?? null;

        if (currentPath !== this.lastActiveTrackPath) {
            this.lastActiveTrackPath = currentPath;
            this.virtualizer?.requestUpdate();
        }
    }

    // =================================================================
    // Styles
    // =================================================================

    static override styles = [
        designTokens,
        srOnly,
        backButton,
        contextMenuStyles,
        exploreLinkStyles,
        swipeRevealStyles,
        css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
            height: 100%;
        }

        /* ====================================
         * Header
         * ==================================== */

        .smart-playlist-header {
            display: flex;
            align-items: center;
            gap: 20px;
            padding: 16px 20px;
            flex-shrink: 0;
            border-bottom: 1px solid
                var(
                    --yj-border-subtle,
                    rgba(255, 255, 255, 0.06)
                );
        }

        .back-button wa-icon {
            font-size: 16px;
        }

        .playlist-avatar {
            width: 80px;
            height: 80px;
            border-radius: 8px;
            overflow: hidden;
            background: linear-gradient(
                135deg,
                var(--yj-bg-overlay, #404040) 0%,
                var(--yj-bg-surface, #282828) 100%
            );
            display: flex;
            align-items: center;
            justify-content: center;
            flex-shrink: 0;
        }

        .playlist-avatar wa-icon {
            font-size: 32px;
            color: var(
                --yj-text-secondary,
                #b3b3b3
            );
        }

        .playlist-info {
            display: flex;
            flex-direction: column;
            gap: 4px;
            min-width: 0;
            flex: 1;
        }

        .playlist-title {
            font-size: 24px;
            font-weight: 700;
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            margin: 0;
            line-height: 1.2;
        }

        .track-count {
            font-size: var(--yj-text-md);
            color: var(
                --yj-text-secondary,
                #b3b3b3
            );
        }

        /* ====================================
         * Actions
         * ==================================== */

        .playlist-actions {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 12px 20px 8px;
            flex-shrink: 0;
        }

        /* #57. Like playlist-details, this view filters on the search
           term and has no page-header to carry the phone's search
           button, so the action row does. */
        .actions-end {
            margin-left: auto;
            display: flex;
            align-items: center;
        }

        .action-button {
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
            transition: border-color 0.15s ease, color 0.15s ease;
        }

        .action-button:hover {
            border-color: var(--yj-accent, #ffd43b);
            color: var(--yj-accent-text, #ffd43b);
        }

        .action-button:disabled {
            opacity: 0.4;
            cursor: not-allowed;
        }

        .action-button:disabled:hover {
            border-color: var(--yj-border-subtle, #555);
            color: var(--yj-text-primary, #fff);
        }

        .action-button wa-icon {
            font-size: 12px;
        }

        /* ====================================
         * Content
         * ==================================== */

        .content {
            flex: 1;
            overflow-y: auto;
            padding: 0 16px 12px 16px;
        }

        .search-bar-row {
            position: relative;
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 30px;
            border-bottom: 1px solid
                var(--yj-border-subtle, #333);
            flex-shrink: 0;
            user-select: none;
        }

        .search-indicator {
            position: absolute;
            left: 50%;
            transform: translateX(-50%);
            pointer-events: none;
            background: var(
                --yj-bg-overlay,
                #495057
            );
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

        /* Column grid layout */
        .track-header,
        .track-item {
            display: grid;
            grid-template-columns: 40px 36px 1fr 1fr 1fr 80px;
            align-items: center;
            gap: 0;
        }

        /* The virtualizer positions its children absolutely, so a row
         * shrinks to fit its content and its columns stop lining up
         * with the header above them. track-list has always carried
         * the same declaration for the same reason.
         *
         * No backticks in here: this is inside a css tagged template
         * literal, and one ends it. */
        .track-item {
            width: 100%;
            box-sizing: border-box;
            /* The swipe reveal is absolute inside the row. */
            position: relative;
            overflow: hidden;
        }

        .track-header {
            padding: 6px 8px;
            font-size: 11px;
            font-weight: 600;
            color: var(--yj-text-secondary, #b3b3b3);
            text-transform: uppercase;
            letter-spacing: 0.03em;
            border-bottom: 1px solid var(--yj-text-tertiary, #666);
            user-select: none;
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

        .header-cell,
        .cell {
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            min-width: 0;
            padding: 0 4px;
        }

        .col-number {
            text-align: center;
            color: var(--yj-text-tertiary, #888);
            font-variant-numeric: tabular-nums;
        }

        .col-duration {
            text-align: right;
            color: var(--yj-text-tertiary, #888);
            font-variant-numeric: tabular-nums;
        }

        .track-item {
            padding: 6px 8px;
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
            color: var(--yj-accent-text, #ffd43b);
        }

        .track-item.selected.active {
            background-color: var(--yj-selection-bg, rgba(100, 160, 255, 0.15));
        }

        /* Phantom rows span the full grid */
        .track-item.phantom {
            display: grid;
            grid-template-columns: 40px 36px 1fr 1fr 1fr 80px;
            cursor: default;
        }

        .phantom-row {
            grid-column: 1 / -1;
            display: flex;
            align-items: center;
            gap: 8px;
            min-width: 0;
            width: 100%;
        }

        .phantom-caution {
            flex-shrink: 0;
            font-size: 14px;
            color: var(--yj-warning-text, #ffa94d);
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

        .empty-state {
            padding: 32px 20px;
            color: var(--yj-text-tertiary, #666);
            font-size: 13px;
            text-align: center;
        }

        .editor-container {
            flex: 1;
            overflow: hidden;
            padding: 0 20px 20px;
            display: flex;
            flex-direction: column;
            min-height: 0;
        }
    `];

    // =================================================================
    // Lifecycle
    // =================================================================

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

        if (!isTrackClick) {
            this.selection.clear();
        }
    };

    override connectedCallback() {
        super.connectedCallback();

        // Credits arrive after the rows that asked for them, and a
        // virtualizer repaints from its *own* properties — a host
        // update alone leaves the rows exactly as they were.
        this.creditsUnsub = creditStore.subscribe(() => {
            this.requestUpdate();
            this.virtualizer?.requestUpdate();
        });

        if (this.autoEdit) {
            // Skip evaluation for new playlists — go straight to editor.
            this.autoEdit = false;
            this.loading = false;
            this.tracks = [];
            this.handleEditRules();
        } else {
            void this.loadTracks();
        }

        this.playlistDeletedCleanup = EventsOn(
            Events.PlaylistDeleted,
            (deletedId: number) => {
                if (deletedId === this.playlistId) {
                    this.navigateBack();
                }
            },
        );

        this.playlistRenamedCleanup = EventsOn(
            Events.PlaylistRenamed,
            (summary: { ID: number; Name: string }) => {
                if (summary.ID === this.playlistId) {
                    this.playlistName = summary.Name;
                }
            },
        );

        document.addEventListener(
            'click',
            this.clearSelectionHandler,
        );
        document.addEventListener(
            'shortcut:select-all',
            this.handleSelectAll,
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.creditsUnsub?.();
        this.creditsUnsub = undefined;

        if (this.playlistDeletedCleanup) {
            this.playlistDeletedCleanup();
            this.playlistDeletedCleanup = null;
        }

        if (this.playlistRenamedCleanup) {
            this.playlistRenamedCleanup();
            this.playlistRenamedCleanup = null;
        }

        document.removeEventListener(
            'click',
            this.clearSelectionHandler,
        );
        document.removeEventListener(
            'shortcut:select-all',
            this.handleSelectAll,
        );
    }

    // =================================================================
    // Data loading
    // =================================================================

    private async loadTracks() {
        if (!this.playlistId) return;

        this.loading = true;

        try {
            const result = await GetSmartPlaylistTracks(
                this.playlistId,
            );

            this.tracks = result ?? [];
            this.selection.clear();
        } catch (error) {
            console.error(
                'Failed to load smart playlist tracks:',
                error,
            );
            this.tracks = [];
        } finally {
            this.loading = false;
        }
    }

    private async refreshTracks() {
        if (!this.playlistId) return;

        try {
            this.tracks = await list(
                GetSmartPlaylistTracks(this.playlistId),
            );
        } catch (error) {
            console.error(
                'Failed to refresh smart playlist tracks:',
                error,
            );
        }
    }

    // =================================================================
    // Navigation
    // =================================================================

    private navigateBack() {
        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: { view: 'playlists' },
            }),
        );
    }

    // =================================================================
    // Actions
    // =================================================================

    private handlePlay() {
        const filePaths = this.tracks
            .filter((t) => !t.Phantom)
            .map((t) => t.FilePath);

        if (filePaths.length === 0) return;

        queueStore.setQueue(filePaths, 0, false, { type: 'smartPlaylist', id: this.playlistId, label: this.playlistName });
    }

    private handleShuffle() {
        const filePaths = this.tracks
            .filter((t) => !t.Phantom)
            .map((t) => t.FilePath);

        if (filePaths.length === 0) return;

        queueStore.setQueue(filePaths, 0, true, { type: 'smartPlaylist', id: this.playlistId, label: this.playlistName });
    }

    private async handleRefresh() {
        this.refreshing = true;

        try {
            await RefreshSmartPlaylist(this.playlistId);
            await this.refreshTracks();
        } catch (error) {
            console.error(
                'Failed to refresh smart playlist:',
                error,
            );
        } finally {
            this.refreshing = false;
        }
    }

    private async handleEditRules() {
        try {
            const result = await GetSmartPlaylistRules(this.playlistId);

            this.currentRulesJSON = result;
            this.pendingRulesJSON = result;
            this.editing = true;
        } catch (error) {
            console.error('Failed to load smart playlist rules:', error);
        }
    }

    private async handleSaveRules() {
        this.saving = true;

        try {
            await UpdateSmartPlaylistRules(
                this.playlistId,
                this.pendingRulesJSON,
            );

            this.editing = false;
            this.loadTracks();
        } catch (error) {
            console.error('Failed to save smart playlist rules:', error);
        } finally {
            this.saving = false;
        }
    }

    private handleCancelEdit() {
        this.editing = false;
        this.pendingRulesJSON = '';
    }

    private handleRulesChanged(e: CustomEvent) {
        this.pendingRulesJSON = e.detail.json;
    }

    // =================================================================
    // Track interactions
    // =================================================================

    private handleTrackClick(
        e: MouseEvent,
        trackIndex: number,
    ) {
        this.selection.handleItemClick(
            e,
            String(trackIndex),
            trackIndex,
        );
    }


    /** The row holding the roving tab stop. Rows had no keyboard path at
     *  all before this: not focusable, so Shift+F10 had nowhere to fire
     *  from and the menu was right-click only (a11y.3). */
    @state() private focusedIndex = 0;

    private onRowKeydown(e: KeyboardEvent, trackIndex: number): void {
        const count = this.tracks.length;

        if (count === 0) return;

        const row = e.currentTarget as HTMLElement | null;

        if (isContextMenuKey(e) && row) {
            e.preventDefault();
            e.stopPropagation();
            this.selection.handleContextMenu(String(trackIndex));
            this.ctxMenu.openFrom(row);

            return;
        }

        if (e.key === 'Enter') {
            e.preventDefault();
            e.stopPropagation();
            this.handleTrackDblClick(trackIndex);

            return;
        }

        const next = nextRovingIndex(e.key, this.focusedIndex, count);

        if (next === null) return;

        e.preventDefault();
        e.stopPropagation();
        this.focusedIndex = next;
        void focusRovingRow(
            this,
            this.virtualizer,
            next,
            (i) => `.track-item[data-index="${i}"]`,
        );
    }

    private handleTrackDblClick(trackIndex: number) {
        this.selection.clear();

        const filePaths = this.tracks.map(
            (t) => t.FilePath,
        );

        queueStore.setQueue(filePaths, trackIndex, false, { type: 'smartPlaylist', id: this.playlistId, label: this.playlistName });
    }

    private handleTrackContextMenu(
        e: MouseEvent,
        trackIndex: number,
    ) {
        e.preventDefault();
        e.stopPropagation();

        this.selection.handleContextMenu(
            String(trackIndex),
        );
        this.ctxMenu.openAt(e.clientX, e.clientY);
    }

    private isActiveTrack(
        track: playlist.Track,
    ): boolean {
        const currentTrack = this.player.currentTrack;

        if (!currentTrack) return false;

        return currentTrack.filePath === track.FilePath;
    }

    // =================================================================
    // Selection helpers
    // =================================================================

    private getSelectedFilePaths(): string[] {
        return this.selection
            .getSelectedIndices()
            .map((i) => this.tracks[i]!.FilePath);
    }

    /**
     * The row "Go to Artist" / "Go to Album" navigate from — one row
     * or none, and only below the phone breakpoint, where the row's
     * own names stopped being links (#67).
     */
    private get goToTarget(): GoToTarget | undefined {
        const indices = this.selection.getSelectedIndices();

        if (indices.length !== 1) return undefined;

        const track = this.tracks[indices[0]!];

        if (!track) return undefined;

        return {
            artistName: track.Artist,
            artistMBID: track.ArtistMBID,
            albumName: track.Album,
            albumMBID: track.ReleaseGroupMBID,
        };
    }

    // =================================================================
    // Context menu actions
    // =================================================================

    // =================================================================
    // A finger on a smart playlist row (plan 019 phase 3, #63)
    // =================================================================

    /** The row an announced gesture is on, with its track. */
    private rowFromGesture(
        e: Event,
    ): { index: number; track: playlist.Track } | null {
        const row = (e.target as HTMLElement).closest(
            '.track-item',
        ) as HTMLElement | null;

        if (!row) return null;

        const index = Number(row.dataset.index);
        const track = this.tracks[index];

        if (Number.isNaN(index) || !track) return null;

        return { index, track };
    }

    /** A tap plays the playlist from that row -- `playlist-details`'
     *  rule, and the app's: activating a row plays the list it is in. */
    private onRowTap = (e: GestureEvent) => {
        const hit = this.rowFromGesture(e);

        if (!hit) return;

        if (this.selection.selectionMode) {
            e.preventDefault();
            this.focusedIndex = hit.index;
            this.selection.toggleInMode(String(hit.index), hit.index);
            this.virtualizer?.requestUpdate();

            return;
        }

        // A missing file has nothing to play, so the tap falls through
        // to the click that selects it.
        if (hit.track.Phantom) return;

        e.preventDefault();
        this.focusedIndex = hit.index;
        this.handleTrackDblClick(hit.index);
    };

    private onRowLongPress = (e: GestureEvent) => {
        const hit = this.rowFromGesture(e);

        if (!hit) return;

        e.preventDefault();
        this.focusedIndex = hit.index;
        this.selection.enterSelectionMode(String(hit.index), hit.index);
        this.virtualizer?.requestUpdate();
    };

    private swipe = new SwipeToQueue(this, {
        resolve: (e) => {
            const hit = this.rowFromGesture(e);

            if (!hit || hit.track.Phantom) return null;

            const selected = this.selection.getSelectedIndices();
            const many =
                selected.length > 1 && selected.includes(hit.index);
            const filePaths = many
                ? this.getSelectedFilePaths()
                : [hit.track.FilePath];

            return { index: hit.index, filePaths, label: hit.track.Title };
        },
        repaint: () => this.virtualizer?.requestUpdate(),
    });

    /** The three worth a thumb; the sheet behind "More" is the rest. */
    private static readonly SELECTION_ACTIONS: SelectionAction[] = [
        { id: 'play', label: 'Play', icon: ICON_PLAY },
        { id: 'add-to-queue', label: 'Add to queue', icon: ICON_QUEUE },
        { id: 'play-next', label: 'Play next', icon: ICON_PLAY_NEXT },
    ];

    private renderSelectionBar() {
        if (!this.selection.selectionMode) return nothing;

        return html`
            <selection-bar
                .count=${this.selection.selectionCount}
                .actions=${SmartPlaylistDetails.SELECTION_ACTIONS}
                @selection-exit=${this.onSelectionExit}
                @selection-action=${(e: CustomEvent<{ id: string }>) =>
                this.onContextMenuAction(e.detail.id)}
                @selection-more=${(e: CustomEvent<{ x: number; y: number }>) =>
                this.ctxMenu.openAt(e.detail.x, e.detail.y)}
            ></selection-bar>
        `;
    }

    private onSelectionExit = () => {
        this.selection.exitSelectionMode();
        this.virtualizer?.requestUpdate();
    };

    private onContextMenuAction(action: string) {
        const filePaths = this.getSelectedFilePaths();

        if (filePaths.length === 0) return;

        switch (action) {
            case 'play':
                // One row is a position in the playlist, so it queues
                // the playlist from there - the same thing
                // double-clicking the row does. Several rows are an
                // explicit choice of *those* tracks and become the
                // queue on their own.
                if (filePaths.length === 1) {
                    this.handleTrackDblClick(
                        this.selection.getSelectedIndices()[0]!,
                    );
                } else {
                    queueStore.setQueue(filePaths, 0, true, { type: 'smartPlaylist', id: this.playlistId, label: this.playlistName });
                }

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
        }

        this.selection.clear();
        this.ctxMenu.close();
    }

    private onContextMenuFavoriteToggle() {
        const filePaths = this.getSelectedFilePaths();

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
        const tracks = libraryStore.getCachedTracks();
        const track = tracks
            ? tracksByFilePath(tracks).get(filePath)
            : undefined;

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
        const cachedTracks =
            libraryStore.getCachedTracks();

        if (!cachedTracks) return;

        const tracks = tracksForPaths(
            cachedTracks,
            filePaths,
        );

        if (tracks.length === 0) return;

        const ready = await loadTrackDetails(
            () => void this.openBatchTrackDetails(filePaths),
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

    // =================================================================
    // Drag source (smart-playlist tracks -> queue or playlist)
    // =================================================================

    private onTrackDragStart = (
        e: DragEvent,
        track: playlist.Track,
        trackIndex: number,
    ) => {
        let filePaths: string[];

        if (
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
            source: 'track-list',
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
    // Search filtering
    // =================================================================

    private getVisibleTracks(): VisibleTrack[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        // Keyed on the identity of the tracks array and the term. The
        // wrappers used to be rebuilt every render, so the array
        // identity always changed — which is the one thing a
        // virtualizer keys its work on (perf.M5).
        if (
            this.visibleCache &&
            this.visibleCacheKey &&
            this.visibleCacheKey.tracks === this.tracks &&
            this.visibleCacheKey.term === term
        ) {
            return this.visibleCache;
        }

        const all = this.tracks.map(
            (track, trackIndex) => ({
                track,
                trackIndex,
            }),
        );

        const visible = term
            ? all.filter(
                  ({ track }) =>
                      track.Title.toLowerCase().includes(
                          term,
                      ) ||
                      track.Artist.toLowerCase().includes(
                          term,
                      ),
              )
            : all;

        this.visibleCache = visible;
        this.visibleCacheKey = { tracks: this.tracks, term };

        return visible;
    }

    /** Stable across renders: lit-virtualizer declares renderItem and
     *  keyFunction as plain properties, so a fresh arrow function marks
     *  them dirty and forces its own render pass every host update
     *  (perf.m1). */
    private renderRow = (entry: VisibleTrack) =>
        this.renderTrackRow(entry);

    private rowKey = (entry: VisibleTrack) => entry.trackIndex;

    // =================================================================
    // Helpers
    // =================================================================

    private getTotalDuration(): string {
        const totalMs = this.tracks.reduce(
            (sum, t) => sum + Number(t.Duration || 0),
            0,
        );

        return formatTotalDuration(totalMs);
    }

    // =================================================================
    // Render
    // =================================================================

    override render() {
        const trackCount = this.tracks.length;
        const trackLabel = trackCount === 1 ? 'track' : 'tracks';
        const hasPlayableTracks = this.tracks.some((t) => !t.Phantom);

        const searchBar = this.searchCtrl.term
            ? html`<div class="search-bar-row">
                  <div class="search-indicator">
                      Showing results for
                      &ldquo;${this.searchCtrl.term}&rdquo;
                  </div>
              </div>`
            : nothing;

        return html`
            <div class="smart-playlist-header">
                <button
                    class="back-button"
                    @click=${this.navigateBack}
                    title="Back to playlists"
                    aria-label="Back to playlists"
                >
                    <wa-icon name="arrow-left"></wa-icon>
                </button>
                <div class="playlist-avatar">
                    <wa-icon name=${ICON_SMART_PLAYLIST}></wa-icon>
                </div>
                <div class="playlist-info">
                    <h1
                        class="playlist-title"
                        title="${this.playlistName}"
                    >
                        ${this.playlistName}
                    </h1>
                    ${!this.loading
                        ? html`
                              <span class="track-count">
                                  ${trackCount}
                                  ${trackLabel}
                                  · ${this.getTotalDuration()}
                              </span>
                          `
                        : nothing}
                </div>
            </div>

            ${this.loading
                ? html`<div class="loading">
                      Loading tracks…
                  </div>`
                : html`
                      <div class="playlist-actions">
                          ${this.editing
                              ? html`
                                    <button
                                        class="action-button"
                                        @click=${this.handleSaveRules}
                                        ?disabled=${this.saving}
                                        title="Save rules"
                                    >
                                        <wa-icon name="floppy-disk"></wa-icon>
                                        ${this.saving ? 'Saving…' : 'Save Rules'}
                                    </button>
                                    <button
                                        class="action-button"
                                        @click=${this.handleCancelEdit}
                                        ?disabled=${this.saving}
                                        title="Cancel editing"
                                    >
                                        <wa-icon name="xmark"></wa-icon>
                                        Cancel
                                    </button>
                                `
                              : html`
                                    <button
                                        class="action-button"
                                        @click=${this.handlePlay}
                                        ?disabled=${!hasPlayableTracks}
                                        title="Play all tracks"
                                    >
                                        <wa-icon name="play"></wa-icon>
                                        Play
                                    </button>
                                    <button
                                        class="action-button"
                                        @click=${this.handleShuffle}
                                        ?disabled=${!hasPlayableTracks}
                                        title="Shuffle all tracks"
                                    >
                                        <wa-icon name="shuffle"></wa-icon>
                                        Shuffle
                                    </button>
                                    <button
                                        class="action-button"
                                        @click=${this.handleRefresh}
                                        ?disabled=${this.refreshing}
                                        title="Re-evaluate smart playlist rules against the library"
                                    >
                                        <wa-icon name="arrow-rotate-right"></wa-icon>
                                        ${this.refreshing ? 'Refreshing…' : 'Refresh'}
                                    </button>
                                    <button
                                        class="action-button"
                                        @click=${this.handleEditRules}
                                        title="Edit smart playlist rules"
                                    >
                                        <wa-icon name="pen-to-square"></wa-icon>
                                        Edit Rules
                                    </button>
                                `}
                          <div class="actions-end">
                              <search-trigger></search-trigger>
                          </div>
                      </div>
                      ${this.editing
                          ? html`
                                <div class="editor-container">
                                    <smart-playlist-editor
                                        .rules=${this.currentRulesJSON}
                                        @rules-changed=${this.handleRulesChanged}
                                    ></smart-playlist-editor>
                                </div>
                            `
                          : trackCount > 0
                            ? html`
                                  ${searchBar}
                                  <div class="content">
                                      ${this.renderTrackList()}
                                  </div>
                              `
                            : html`
                                  <div class="empty-state">
                                      No tracks match the current rules.
                                      Configure rules and click Refresh.
                                  </div>
                              `}
                  `}

            ${this.renderContextMenu()}

            <track-details></track-details>
        `;
    }

    private renderTrackList() {
        const visibleTracks = this.getVisibleTracks();

        return html`
            <div class="track-header">
                <div class="header-cell col-number">#</div>
                <div class="header-cell col-art"></div>
                <div class="header-cell col-title">Title</div>
                <div class="header-cell col-artist">Artist</div>
                <div class="header-cell col-album">Album</div>
                <div class="header-cell col-duration">Duration</div>
            </div>
            <div class="sr-only" role="status" aria-live="polite">
                ${this.swipe.announcement}
            </div>
            <lit-virtualizer
                role="listbox"
                aria-label="Smart playlist tracks"
                aria-multiselectable="true"
                .items=${visibleTracks}
                .renderItem=${this.renderRow}
                .keyFunction=${this.rowKey}
                .layout=${this.flowLayout}
                @yj-tap=${this.onRowTap}
                @yj-long-press=${this.onRowLongPress}
                @yj-swipe-start=${this.swipe.onSwipeStart}
                @yj-swipe-move=${this.swipe.onSwipeMove}
                @yj-swipe-end=${this.swipe.onSwipeEnd}
            ></lit-virtualizer>
            ${this.renderSelectionBar()}
        `;
    }

    /* One row. Extracted from renderTrackList so it can be a stable
     * bound field rather than a closure the virtualizer sees as new on
     * every pass. */
    private renderTrackRow({ track, trackIndex }: VisibleTrack) {
        const isPhantom = track.Phantom;
                    const active =
                        !isPhantom &&
                        this.isActiveTrack(track);
                    const selected =
                        this.selection.isSelected(
                            String(trackIndex),
                        );

                    const classes = [
                        'track-item',
                        active ? 'active' : '',
                        selected ? 'selected' : '',
                        isPhantom ? 'phantom' : '',
                        this.swipe.isSwiping(trackIndex) ? 'swiping' : '',
                    ]
                        .filter(Boolean)
                        .join(' ');

                    return html`
                        <div
                            class=${classes}
                            role="option"
                            aria-selected=${selected}
                            data-index=${trackIndex}
                            data-swipe
                            tabindex=${trackIndex === this.focusedIndex ? 0 : -1}
                            @keydown=${(e: KeyboardEvent) =>
                                this.onRowKeydown(e, trackIndex)}
                            draggable=${isPhantom ? 'false' : 'true'}
                            @click=${(e: MouseEvent) =>
                                this.handleTrackClick(
                                    e,
                                    trackIndex,
                                )}
                            @dblclick=${isPhantom
                                ? nothing
                                : () =>
                                      this.handleTrackDblClick(
                                          trackIndex,
                                      )}
                            @contextmenu=${isPhantom
                                ? nothing
                                : (e: MouseEvent) =>
                                      this.handleTrackContextMenu(
                                          e,
                                          trackIndex,
                                      )}
                            @dragstart=${isPhantom
                                ? nothing
                                : (e: DragEvent) =>
                                      this.onTrackDragStart(
                                          e,
                                          track,
                                          trackIndex,
                                      )}
                            @dragend=${isPhantom
                                ? nothing
                                : this.onTrackDragEnd}
                        >
                            ${this.swipe.renderReveal(trackIndex)}
                            ${isPhantom
                                ? html`<div class="phantom-row">
                                      <wa-icon
                                          class="phantom-caution"
                                          name="triangle-exclamation"
                                          title="File not found — will resolve on next refresh"
                                      ></wa-icon>
                                      <span
                                          class="phantom-path"
                                          title=${track.FilePath}
                                      >${track.FilePath}</span>
                                  </div>`
                                : html`<span class="cell col-number">${trackIndex + 1}</span>
                                  <div class="track-art">
                                      ${track.CoverArtSmall || track.CoverArtMedium
                                          ? html`<img
                                                src="${track.CoverArtSmall || track.CoverArtMedium}"
                                                alt=""
                                                loading="lazy"
                                                decoding="async"
                                                width="32"
                                                height="32"
                                            />`
                                          : nothing}
                                  </div>
                                  <span class="cell col-title" title="${track.Title || track.FilePath}">${trackLink(track.Title, track.Album, track.ReleaseGroupMBID, track.RecordingMBID, undefined, track.Artist) || track.FilePath}</span>
                                  <span class="cell col-artist" title="${creditText(creditStore.credits(track.RecordingMBID), track.Artist)}">${creditLink(creditStore.credits(track.RecordingMBID), track.Artist, track.ArtistMBID)}</span>
                                  <span class="cell col-album" title="${track.Album}">${albumLink(track.Album, track.ReleaseGroupMBID, undefined, track.Artist)}</span>
                                  <span class="cell col-duration">${formatMilliseconds(track.Duration)}</span>`}
                        </div>
                    `;
    }

    private renderContextMenu() {
        return html`
            <menu-surface
                id="context-menu"
                .active=${this.ctxMenu.contextMenuOpen}
            >
                ${this.ctxMenu.contextMenuOpen
                    ? html`
                          <div class="context-menu-panel" role="menu" aria-label="Track actions">
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
                                      name=${ICON_QUEUE}
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
                                  class="submenu-item"
                                  @mouseenter=${() => {
                                      this.ctxMenu.clearSubmenuCloseTimer();
                                      void this.ctxMenu.showPlaylistSubmenu(
                                          this.getSelectedFilePaths(),
                                      );
                                  }}
                                  @mouseleave=${this
                                      .ctxMenu
                                      .scheduleSubmenuClose}
                                  @click=${(
                                      e: Event,
                                  ) => {
                                      e.stopPropagation();
                                      void this.ctxMenu.showPlaylistSubmenu(
                                          this.getSelectedFilePaths(),
                                      );
                                  }}
                              >
                                  <wa-icon
                                      slot="icon"
                                      name=${ICON_PLAYLIST}
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
                                      name=${this
                                          .favCtrl
                                          .iconName}
                                  ></wa-icon>
                                  ${this.favCtrl.allFavorited(
                                      this.getSelectedFilePaths(),
                                  )
                                      ? `Remove from ${this.favCtrl.playlistName}`
                                      : `Add to ${this.favCtrl.playlistName}`}
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
                              ${goToMenuItems(this.goToTarget, {
                                  onSelect: () => {
                                      this.selection.clear();
                                      this.ctxMenu.close();
                                  },
                                  onHover: () =>
                                      this.ctxMenu.closePlaylistSubmenu(),
                              })}
                          </div>
                      `
                    : nothing}
            </menu-surface>

            <menu-surface
                id="playlist-submenu"
                label="Add to playlist"
                placement="right-start"
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
                                  @click=${(
                                      e: Event,
                                  ) =>
                                      e.stopPropagation()}
                              ></playlist-picker>
                          </div>
                      `
                    : nothing}
            </menu-surface>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'smart-playlist-details': SmartPlaylistDetails;
    }
}

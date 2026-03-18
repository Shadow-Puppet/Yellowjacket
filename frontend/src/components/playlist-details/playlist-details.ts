import { LitElement, html, css, nothing } from 'lit';
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

import {
    GetPlaylistTracks,
    AddTracksToPlaylist,
    RemoveTracksFromPlaylist,
    RemovePhantomTracks,
    FindDuplicateTracksInPlaylist,
} from '@go/playlist/Service';
import type { playlist, library } from '@go/models';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import { queueStore } from '@store/queue-store';
import { PlayerController } from '@store/controllers/player-controller';
import { SearchController } from '@store/controllers/search-controller';
import { SelectionController } from '@utils/selection-controller';
import type { SelectionHost } from '@utils/selection-controller';
import {
    ContextMenuController,
    contextMenuStyles,
} from '@utils/context-menu-controller.js';
import type { ContextMenuHost } from '@utils/context-menu-controller.js';
import { FavoritesController } from '@store/controllers/favorites-controller';
import {
    hasTrackPayload,
    getDragPayload,
    setDragPayload,
    emitDragActive,
} from '@utils/drag-controller';
import {
    createDragImage,
    createTrackCardDragImage,
    removeDragImage,
} from '@utils/drag-image';
import { libraryStore } from '@store/library-store';
import '@components/playlist-picker/playlist-picker.js';
import '@components/track-details/track-details.js';
import type { TrackDetails } from '@components/track-details/track-details.js';
import type { CoverArtUrls } from '@components/track-details/track-details.js';
import '@components/phantom-resolver/phantom-resolver.js';
import type { PhantomResolver } from '@components/phantom-resolver/phantom-resolver.js';
import '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';
import type { DuplicateTracksDialog } from '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';
import { formatMilliseconds } from '@utils/time';
import { designTokens } from '../../styles/tokens.css';

@customElement('playlist-details')
export class PlaylistDetails
    extends LitElement
    implements SelectionHost, ContextMenuHost
{
    @property({ type: Number, attribute: 'playlist-id' })
    playlistId = 0;

    @property({ type: String, attribute: 'playlist-name' })
    playlistName = '';

    @state()
    private tracks: playlist.Track[] = [];

    @state()
    private loading = true;

    private player = new PlayerController(this);
    private searchCtrl = new SearchController(this);
    private selection = new SelectionController(this);
    private ctxMenu = new ContextMenuController(this);
    private favCtrl = new FavoritesController(this);

    private tracksChangedCleanup: (() => void) | null = null;
    private playlistDeletedCleanup: (() => void) | null = null;

    private dragImageEl: HTMLElement | null = null;

    @query('#context-menu')
    private contextMenuPopup!: WaPopup;

    @query('#playlist-submenu')
    private playlistSubmenuPopup!: WaPopup;

    @query('track-details')
    private trackDetailsDialog!: TrackDetails;

    @query('phantom-resolver')
    private phantomResolver!: PhantomResolver;

    @query('duplicate-tracks-dialog')
    private duplicateDialog!: DuplicateTracksDialog;

    // =================================================================
    // ContextMenuHost interface
    // =================================================================

    getContextMenuPopup(): WaPopup | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup(): WaPopup | undefined {
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
    }

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
        this.loadTracks();

        this.tracksChangedCleanup = EventsOn(
            Events.PlaylistTracksChanged,
            () => this.refreshTracks(),
        );

        this.playlistDeletedCleanup = EventsOn(
            Events.PlaylistDeleted,
            () => this.navigateBack(),
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

        if (this.tracksChangedCleanup) {
            this.tracksChangedCleanup();
            this.tracksChangedCleanup = null;
        }

        if (this.playlistDeletedCleanup) {
            this.playlistDeletedCleanup();
            this.playlistDeletedCleanup = null;
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

        try {
            this.tracks = await GetPlaylistTracks(
                this.playlistId,
            );
        } catch (error) {
            console.error(
                'Error loading playlist tracks:',
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
            this.tracks = await GetPlaylistTracks(
                this.playlistId,
            );
        } catch (error) {
            console.error(
                'Error refreshing playlist tracks:',
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
    // Track interactions
    // =================================================================

    private handlePlayAll() {
        const filePaths = this.tracks
            .filter((t) => !t.Phantom)
            .map((t) => t.FilePath);

        if (filePaths.length === 0) return;

        queueStore.setQueue(filePaths, 0, true);
    }

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

    private handleTrackDblClick(trackIndex: number) {
        this.selection.clear();

        const filePaths = this.tracks.map(
            (t) => t.FilePath,
        );

        queueStore.setQueue(filePaths, trackIndex);
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

    private getSelectedTrackIDs(): number[] {
        return this.selection
            .getSelectedIndices()
            .map((i) => this.tracks[i]!.ID);
    }

    private getSelectedFilePaths(): string[] {
        return this.selection
            .getSelectedIndices()
            .map((i) => this.tracks[i]!.FilePath);
    }

    // =================================================================
    // Context menu actions
    // =================================================================

    private onContextMenuAction(action: string) {
        const filePaths = this.getSelectedFilePaths();

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
            case 'remove':
                void this.removeSelectedTracks();
                break;
            case 'track-details':
                if (filePaths.length === 1) {
                    this.openTrackDetails(filePaths[0]!);
                } else {
                    this.openBatchTrackDetails(filePaths);
                }
                break;
            case 'phantom-locate':
                this.openPhantomResolver();
                break;
            case 'phantom-remove':
                void this.removeSelectedPhantoms();
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

    private async removeSelectedTracks() {
        const trackIDs = this.getSelectedTrackIDs();

        if (trackIDs.length === 0) return;

        try {
            await RemoveTracksFromPlaylist(
                this.playlistId,
                trackIDs,
            );
            await this.refreshTracks();
        } catch (err) {
            console.error(
                'Failed to remove tracks:',
                err,
            );
        }
    }

    private async removeSelectedPhantoms(): Promise<void> {
        const selectedIndices =
            this.selection.getSelectedIndices();
        const phantomPaths = selectedIndices
            .map((i) => this.tracks[i])
            .filter(
                (t): t is playlist.Track =>
                    t !== undefined && t.Phantom,
            )
            .map((t) => t.FilePath);

        if (phantomPaths.length === 0) return;

        try {
            await RemovePhantomTracks(
                this.playlistId,
                phantomPaths,
            );
            await this.refreshTracks();
        } catch (err) {
            console.error(
                'Failed to remove phantom tracks:',
                err,
            );
        }
    }

    private openTrackDetails(filePath: string) {
        const tracks = libraryStore.getCachedTracks();
        const track = tracks?.find(
            (t) => t.FilePath === filePath,
        );

        if (!track) return;

        const coverArt =
            this.resolvePlaylistCoverArt(track.Album);

        this.trackDetailsDialog?.show(
            track,
            coverArt ?? undefined,
        );
    }

    private openBatchTrackDetails(
        filePaths: string[],
    ) {
        const cachedTracks =
            libraryStore.getCachedTracks();

        if (!cachedTracks) return;

        const tracks = filePaths
            .map((fp) =>
                cachedTracks.find(
                    (t) => t.FilePath === fp,
                ),
            )
            .filter(
                (t): t is library.Track =>
                    t != null,
            );

        if (tracks.length === 0) return;

        const albumNames = new Set(
            tracks.map((t) => t.Album),
        );
        let coverArt: CoverArtUrls | null = null;
        let coverArtMixed = false;

        if (albumNames.size === 1) {
            const albumName = [...albumNames][0]!;
            coverArt =
                this.resolvePlaylistCoverArt(
                    albumName,
                );
        } else {
            coverArtMixed = true;
        }

        this.trackDetailsDialog?.showBatch(
            tracks,
            coverArt,
            coverArtMixed,
        );
    }

    private resolvePlaylistCoverArt(
        albumName: string,
    ): CoverArtUrls | null {
        if (!albumName) return null;

        const albums = libraryStore.getCachedAlbums();

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

    /**
     * Check whether all currently selected tracks are phantoms.
     */
    private isPhantomSelection(): boolean {
        const indices =
            this.selection.getSelectedIndices();

        if (indices.length === 0) return false;

        return indices.every((i) => {
            const t = this.tracks[i];

            return t !== undefined && t.Phantom;
        });
    }

    // =================================================================
    // Phantom track interactions
    // =================================================================

    private handlePhantomClick(
        e: MouseEvent,
        trackIndex: number,
    ): void {
        this.selection.handleItemClick(
            e,
            String(trackIndex),
            trackIndex,
        );
    }

    private handlePhantomContextMenu(
        e: MouseEvent,
        trackIndex: number,
    ): void {
        e.preventDefault();
        e.stopPropagation();
        this.selection.handleContextMenu(
            String(trackIndex),
        );
        this.ctxMenu.openAt(e.clientX, e.clientY);
    }

    private openPhantomResolver(
        trackIndex?: number,
    ): void {
        const selectedIndices =
            this.selection.getSelectedIndices();
        let phantoms = selectedIndices
            .map((i) => this.tracks[i])
            .filter(
                (t): t is playlist.Track =>
                    t !== undefined && t.Phantom,
            );

        // Fall back to the clicked track.
        if (
            phantoms.length === 0 &&
            trackIndex !== undefined
        ) {
            const track = this.tracks[trackIndex];

            if (track?.Phantom) {
                phantoms = [track];
            }
        }

        if (phantoms.length === 0) return;

        this.phantomResolver.show(
            this.playlistId,
            phantoms,
        );
    }

    private async removePhantomTrack(
        trackIndex: number,
    ): Promise<void> {
        const track = this.tracks[trackIndex];

        if (!track?.Phantom) return;

        try {
            await RemovePhantomTracks(
                this.playlistId,
                [track.FilePath],
            );
            await this.refreshTracks();
        } catch (err) {
            console.error(
                'Failed to remove phantom track:',
                err,
            );
        }
    }

    // =================================================================
    // Drag source (playlist tracks -> queue or other playlist)
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
            source: 'playlist',
            sourcePlaylistId: this.playlistId,
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
    // Drop target (tracks dropped onto this playlist)
    // =================================================================

    @state()
    private dragOver = false;

    private onDragOver = (e: DragEvent) => {
        if (!hasTrackPayload(e)) return;

        e.preventDefault();

        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = 'copy';
        }

        if (!this.dragOver) {
            this.dragOver = true;
        }
    };

    private onDragLeave = (e: DragEvent) => {
        const related =
            e.relatedTarget as Node | null;

        if (!related || !this.contains(related)) {
            this.dragOver = false;
        }
    };

    private onDrop = async (e: DragEvent) => {
        e.preventDefault();
        this.dragOver = false;

        const payload = getDragPayload(e);

        if (
            !payload ||
            payload.filePaths.length === 0
        ) {
            return;
        }

        // Don't allow dropping tracks back onto the
        // same playlist.
        if (
            payload.source === 'playlist' &&
            payload.sourcePlaylistId === this.playlistId
        ) {
            return;
        }

        try {
            const result =
                await FindDuplicateTracksInPlaylist(
                    this.playlistId,
                    payload.filePaths,
                );
            const duplicates =
                result.Duplicates ?? [];
            const unique = result.Unique ?? [];

            if (duplicates.length > 0) {
                await this.updateComplete;
                this.duplicateDialog.show(
                    this.playlistId,
                    duplicates,
                    unique,
                );

                return;
            }

            await AddTracksToPlaylist(
                this.playlistId,
                payload.filePaths,
            );
            await this.refreshTracks();
        } catch (err) {
            console.error(
                'Failed to add tracks to playlist:',
                err,
            );
        }
    };

    // =================================================================
    // Search filtering
    // =================================================================

    private getVisibleTracks(): {
        track: playlist.Track;
        trackIndex: number;
    }[] {
        const term =
            this.searchCtrl.term.toLowerCase();

        if (!term) {
            return this.tracks.map(
                (track, trackIndex) => ({
                    track,
                    trackIndex,
                }),
            );
        }

        return this.tracks
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

    // =================================================================
    // Styles
    // =================================================================

    static override styles = [
        designTokens,
        contextMenuStyles,
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

        .playlist-header {
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

        .back-button {
            display: flex;
            align-items: center;
            justify-content: center;
            width: 32px;
            height: 32px;
            border: none;
            border-radius: 50%;
            background: var(
                --yj-bg-overlay,
                rgba(255, 255, 255, 0.06)
            );
            color: var(--yj-text-primary, #fff);
            cursor: pointer;
            flex-shrink: 0;
            transition: background-color 0.15s ease;
        }

        .back-button:hover {
            background: var(
                --yj-bg-hover,
                rgba(255, 255, 255, 0.12)
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

        .playlist-actions {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 12px 0 8px;
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

        /* Column grid layout */
        .track-header,
        .track-item {
            display: grid;
            grid-template-columns: 40px 1fr 1fr 1fr 80px;
            align-items: center;
            gap: 0;
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

        /* Phantom rows span full grid */
        .track-item.phantom {
            display: grid;
            grid-template-columns: 40px 1fr 1fr 1fr 80px;
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
    `];

    // =================================================================
    // Render
    // =================================================================

    override render() {
        const trackCount = this.tracks.length;
        const trackLabel =
            trackCount === 1 ? 'track' : 'tracks';

        const searchBar = this.searchCtrl.term
            ? html`<div class="search-bar-row">
                  <div class="search-indicator">
                      Showing results for
                      &ldquo;${this.searchCtrl
                          .term}&rdquo;
                  </div>
              </div>`
            : nothing;

        return html`
            <div
                class="playlist-header"
                @dragover=${this.onDragOver}
                @dragleave=${this.onDragLeave}
                @drop=${this.onDrop}
            >
                <button
                    class="back-button"
                    @click=${() => this.navigateBack()}
                    title="Back to playlists"
                    aria-label="Back to playlists"
                >
                    <wa-icon
                        name="arrow-left"
                    ></wa-icon>
                </button>
                <div class="playlist-avatar">
                    <wa-icon
                        name="list"
                    ></wa-icon>
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
                              <span
                                  class="track-count"
                              >
                                  ${trackCount}
                                  ${trackLabel}
                              </span>
                          `
                        : ''}
                </div>
            </div>
            ${searchBar}
            <div
                class="content"
                @dragover=${this.onDragOver}
                @dragleave=${this.onDragLeave}
                @drop=${this.onDrop}
            >
                ${this.loading
                    ? html`<div class="loading">
                          Loading tracks...
                      </div>`
                    : this.renderTrackList()}
            </div>

            ${this.renderContextMenu()}

            <track-details></track-details>
            <phantom-resolver
                @phantom-resolved=${() =>
                    this.refreshTracks()}
            ></phantom-resolver>
            <duplicate-tracks-dialog
                @playlist-action-complete=${() =>
                    this.refreshTracks()}
            ></duplicate-tracks-dialog>
        `;
    }

    private renderTrackList() {
        if (this.tracks.length === 0) {
            return html`
                <div class="tracks-empty">
                    This playlist is empty.
                </div>
            `;
        }

        const visibleTracks = this.getVisibleTracks();

        return html`
            <div class="playlist-actions">
                <button
                    class="play-all-button"
                    @click=${() => this.handlePlayAll()}
                >
                    <wa-icon name="play"></wa-icon>
                    Play All
                </button>
            </div>
            <div class="track-header">
                <div class="header-cell col-number">#</div>
                <div class="header-cell col-title">Title</div>
                <div class="header-cell col-artist">Artist</div>
                <div class="header-cell col-album">Album</div>
                <div class="header-cell col-duration">Duration</div>
            </div>
            ${visibleTracks.map(
                ({ track, trackIndex }) => {
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
                                ? (e: MouseEvent) =>
                                      this.handlePhantomClick(
                                          e,
                                          trackIndex,
                                      )
                                : (e: MouseEvent) =>
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
                                ? (e: MouseEvent) =>
                                      this.handlePhantomContextMenu(
                                          e,
                                          trackIndex,
                                      )
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
                                : html`<span class="cell col-number">${trackIndex + 1}</span>
                                  <span class="cell col-title" title="${track.Title || track.FilePath}">${track.Title || track.FilePath}</span>
                                  <span class="cell col-artist" title="${track.Artist}">${track.Artist}</span>
                                  <span class="cell col-album" title="${track.Album}">${track.Album}</span>
                                  <span class="cell col-duration">${formatMilliseconds(track.Duration)}</span>`}
                        </div>
                    `;
                },
            )}
        `;
    }

    private renderContextMenu() {
        return html`
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
            </wa-popup>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'playlist-details': PlaylistDetails;
    }
}

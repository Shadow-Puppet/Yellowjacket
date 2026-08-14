import type { ReactiveController, ReactiveControllerHost } from 'lit';
import type * as playlist from '@go/playlist/models.js';
import { playlistStore } from '../playlist-store';

/**
 * PlaylistController connects a Lit component to the PlaylistStore.
 *
 * Usage in a component:
 *
 *   private playlistCtrl = new PlaylistController(this);
 *
 *   async connectedCallback() {
 *     super.connectedCallback();
 *     const playlists = await this.playlistCtrl.getPlaylists();
 *   }
 */
export class PlaylistController implements ReactiveController {
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    // ===================================================================
    // LIFECYCLE HOOKS
    // ===================================================================

    hostConnected(): void {
        this.unsubscribe = playlistStore.subscribe(() => {
            this.host.requestUpdate();
        });
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    // ===================================================================
    // DATA ACCESS
    // ===================================================================

    async getPlaylists(): Promise<playlist.WithTracks[]> {
        return playlistStore.getPlaylists();
    }

    get cachedPlaylists(): playlist.WithTracks[] | null {
        return playlistStore.getCachedPlaylists();
    }

    get isLoading(): boolean {
        return playlistStore.isLoading();
    }

    // ===================================================================
    // SCROLL POSITION
    // ===================================================================

    getScrollPosition(): number {
        return playlistStore.getScrollPosition();
    }

    setScrollPosition(offset: number): void {
        playlistStore.setScrollPosition(offset);
    }

    // ===================================================================
    // REFETCH
    // ===================================================================

    async refetch(): Promise<playlist.WithTracks[]> {
        return playlistStore.refetch();
    }

    // ===================================================================
    // INVALIDATION
    // ===================================================================

    invalidate(): void {
        playlistStore.invalidate();
    }
}

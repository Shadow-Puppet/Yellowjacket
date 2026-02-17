import { GetAllPlaylistsWithTracks } from '@go/playlist/Service';
import type { playlist } from '@go/models';

type Subscriber = () => void;

class PlaylistStore {
    private playlists: playlist.WithTracks[] | null = null;
    private playlistsLoading = false;
    private scrollPosition = 0;
    private subscribers = new Set<Subscriber>();

    // ===================================================================
    // DATA ACCESS
    // Returns cached data or fetches from backend on first access.
    // ===================================================================

    async getPlaylists(): Promise<playlist.WithTracks[]> {
        if (this.playlists !== null) {
            return this.playlists;
        }

        if (this.playlistsLoading) {
            return this.waitForPlaylists();
        }

        this.playlistsLoading = true;
        this.notify();

        try {
            const result = await GetAllPlaylistsWithTracks();
            this.playlists = result ?? [];

            return this.playlists;
        } finally {
            this.playlistsLoading = false;
            this.notify();
        }
    }

    // ===================================================================
    // STATE ACCESSORS
    // Synchronous access for controllers that need current cached values.
    // ===================================================================

    getCachedPlaylists(): playlist.WithTracks[] | null {
        return this.playlists;
    }

    isLoading(): boolean {
        return this.playlistsLoading;
    }

    // ===================================================================
    // SCROLL POSITION
    // ===================================================================

    getScrollPosition(): number {
        return this.scrollPosition;
    }

    setScrollPosition(offset: number): void {
        this.scrollPosition = offset;
    }

    // ===================================================================
    // INVALIDATION
    // ===================================================================

    invalidate(): void {
        this.playlists = null;
        this.scrollPosition = 0;
        this.notify();
    }

    // ===================================================================
    // SUBSCRIPTION SYSTEM
    // ===================================================================

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    private notify(): void {
        this.subscribers.forEach((callback) => callback());
    }

    // ===================================================================
    // HELPERS
    // Wait for an in-flight fetch to complete.
    // ===================================================================

    private waitForPlaylists(): Promise<playlist.WithTracks[]> {
        return new Promise((resolve) => {
            const unsub = this.subscribe(() => {
                if (
                    !this.playlistsLoading &&
                    this.playlists !== null
                ) {
                    unsub();
                    resolve(this.playlists);
                }
            });
        });
    }
}

// Singleton instance.
export const playlistStore = new PlaylistStore();

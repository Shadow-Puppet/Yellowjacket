import { EventsOn } from '@runtime/runtime';
import { GetAllPlaylistsWithTracks } from '@go/playlist/Service';
import type { playlist } from '@go/models';
import { Events } from '../events';

type Subscriber = () => void;

class PlaylistStore {
    private playlists: playlist.WithTracks[] | null = null;
    private playlistsLoading = false;
    private scrollPosition = 0;
    private subscribers = new Set<Subscriber>();

    constructor() {
        EventsOn(Events.LibraryScanComplete, () => {
            this.invalidate();
        });

        EventsOn(Events.PlaylistCreated, () => {
            this.invalidate();
        });

        EventsOn(Events.PlaylistDeleted, () => {
            this.invalidate();
        });

        EventsOn(Events.PlaylistRenamed, () => {
            this.invalidate();
        });

        EventsOn(Events.PlaylistTracksChanged, () => {
            this.invalidate();
        });

        EventsOn(Events.PlaylistsRestored, () => {
            this.invalidate();
        });

        void this.getPlaylists();
    }

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
    // REFETCH
    // Fetches fresh data without clearing the cache first, so existing
    // consumers keep rendering stale data until the new data arrives.
    // ===================================================================

    async refetch(): Promise<playlist.WithTracks[]> {
        if (this.playlistsLoading) {
            return this.waitForPlaylists();
        }

        this.playlistsLoading = true;

        try {
            const result =
                await GetAllPlaylistsWithTracks();
            this.playlists = result ?? [];

            return this.playlists;
        } finally {
            this.playlistsLoading = false;
            this.notify();
        }
    }

    // ===================================================================
    // INVALIDATION
    // ===================================================================

    invalidate(): void {
        this.playlists = null;
        this.scrollPosition = 0;
        this.notify();
        void this.getPlaylists();
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

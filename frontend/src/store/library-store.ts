import { EventsOn } from '@runtime/runtime';
import { GetAllTracks, GetAllAlbums } from '@go/library/Library';
import type { library } from '@go/models';
import { Events } from '../events';

type ViewName = 'tracks' | 'albums';

type Subscriber = () => void;

class LibraryStore {
    private tracks: library.Track[] | null = null;
    private albums: library.Album[] | null = null;

    private tracksLoading = false;
    private albumsLoading = false;

    private scrollPositions: Record<ViewName, number> = {
        tracks: 0,
        albums: 0,
    };

    private subscribers = new Set<Subscriber>();

    constructor() {
        EventsOn(Events.LibraryScanComplete, () => {
            this.invalidate();
        });
    }

    // ===================================================================
    // DATA ACCESS
    // Returns cached data or fetches from backend on first access.
    // ===================================================================

    async getTracks(): Promise<library.Track[]> {
        if (this.tracks !== null) {
            return this.tracks;
        }

        if (this.tracksLoading) {
            return this.waitForTracks();
        }

        this.tracksLoading = true;
        this.notify();

        try {
            const tracks = await GetAllTracks();
            this.tracks = tracks;

            return tracks;
        } finally {
            this.tracksLoading = false;
            this.notify();
        }
    }

    async getAlbums(): Promise<library.Album[]> {
        if (this.albums !== null) {
            return this.albums;
        }

        if (this.albumsLoading) {
            return this.waitForAlbums();
        }

        this.albumsLoading = true;
        this.notify();

        try {
            const albums = await GetAllAlbums();
            this.albums = albums;

            return albums;
        } finally {
            this.albumsLoading = false;
            this.notify();
        }
    }

    // ===================================================================
    // STATE ACCESSORS
    // Synchronous access for controllers that need current cached values.
    // ===================================================================

    getCachedTracks(): library.Track[] | null {
        return this.tracks;
    }

    getCachedAlbums(): library.Album[] | null {
        return this.albums;
    }

    isTracksLoading(): boolean {
        return this.tracksLoading;
    }

    isAlbumsLoading(): boolean {
        return this.albumsLoading;
    }

    // ===================================================================
    // SCROLL POSITION
    // ===================================================================

    getScrollPosition(view: ViewName): number {
        return this.scrollPositions[view];
    }

    setScrollPosition(view: ViewName, offset: number): void {
        this.scrollPositions[view] = offset;
    }

    // ===================================================================
    // INVALIDATION
    // ===================================================================

    private invalidate(): void {
        this.tracks = null;
        this.albums = null;
        this.scrollPositions = { tracks: 0, albums: 0 };
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

    private waitForTracks(): Promise<library.Track[]> {
        return new Promise((resolve) => {
            const unsub = this.subscribe(() => {
                if (!this.tracksLoading && this.tracks !== null) {
                    unsub();
                    resolve(this.tracks);
                }
            });
        });
    }

    private waitForAlbums(): Promise<library.Album[]> {
        return new Promise((resolve) => {
            const unsub = this.subscribe(() => {
                if (!this.albumsLoading && this.albums !== null) {
                    unsub();
                    resolve(this.albums);
                }
            });
        });
    }
}

// Singleton instance.
export const libraryStore = new LibraryStore();

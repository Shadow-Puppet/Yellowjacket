import { EventsOn } from '@runtime/runtime';
import {
    GetAllTracks,
    GetAllAlbums,
    GetAllArtists,
    GetAllGenresWithCounts,
    GetAlbumsByArtist,
    GetAllTracksByLibrary,
    GetAllAlbumsByLibrary,
    GetAllArtistsByLibrary,
    GetAllGenresWithCountsByLibrary,
    GetAlbumsByArtistByLibrary,
    GetAllLibrariesWithTrackCounts,
} from '@go/library/Library';
import type { library } from '@go/models';
import { Events } from '../events';

type ViewName = 'tracks' | 'albums' | 'artists' | 'genres';

type Subscriber = () => void;

/** Minimum album card width in CSS pixels. */
const COVER_SIZE_MIN = 100;

/** Maximum album card width in CSS pixels. */
const COVER_SIZE_MAX = 350;

/** Default album card width in CSS pixels. */
const COVER_SIZE_DEFAULT = 176;

/** localStorage key for persisted cover size. */
const COVER_SIZE_KEY = 'cover-grid-size';

class LibraryStore {
    private tracks: library.Track[] | null = null;
    private albums: library.Album[] | null = null;
    private artists: library.Artist[] | null = null;
    private genres: library.GenreWithCount[] | null = null;
    private libraries: library.Info[] | null = null;

    private selectedLibraryIdValue: number | null = null;

    private tracksLoading = false;
    private albumsLoading = false;
    private artistsLoading = false;
    private genresLoading = false;

    private coverSizeValue: number = COVER_SIZE_DEFAULT;

    private scrollPositions: Record<ViewName, number> = {
        tracks: 0,
        albums: 0,
        artists: 0,
        genres: 0,
    };

    private subscribers = new Set<Subscriber>();
    private notifyScheduled = false;

    /**
     * Monotonic counter incremented only when actual data changes
     * (not loading flag transitions). Subscribers can compare against
     * a saved value to skip requestUpdate when only loading state toggled.
     */
    private changeGen = 0;

    constructor() {
        EventsOn(Events.LibraryScanComplete, () => {
            this.invalidate();
        });
        EventsOn(Events.LibraryRemoved, () => {
            this.libraries = null;
            this.invalidate();
        });
        EventsOn(Events.LibraryAdded, () => {
            this.libraries = null;
            this.changeGen++;
            this.notify();
        });
        EventsOn(Events.LibraryRenamed, () => {
            this.libraries = null;
            this.changeGen++;
            this.notify();
        });
        EventsOn(Events.TrackMetadataChanged, () => {
            this.invalidate();
        });

        this.loadCoverSize();
        this.deferEagerFetch();
    }

    /**
     * Schedules eagerFetch() to run after the DOM is ready.
     * The LibraryStore singleton is instantiated during ES module
     * evaluation (import time), so calling eagerFetch() in the
     * constructor would fire 4 backend roundtrips before the app
     * shell has rendered.  Deferring to the 'DOMContentLoaded'
     * event (or calling immediately if the DOM is already parsed)
     * lets the shell paint first, then begins data loading.
     */
    private deferEagerFetch(): void {
        if (document.readyState === 'loading') {
            window.addEventListener(
                'DOMContentLoaded',
                () => {
                    this.eagerFetch();
                },
                { once: true },
            );
        } else {
            // DOM already parsed (shouldn't happen during module
            // eval, but handles dynamic instantiation safely).
            this.eagerFetch();
        }
    }

    // ===================================================================
    // DATA ACCESS
    // Returns cached data or fetches from backend on first access.
    // ===================================================================

    /** Synchronous access to cached artists (null if not yet loaded). */
    get cachedArtists(): library.Artist[] | null {
        return this.artists;
    }

    /** Synchronous access to cached albums (null if not yet loaded). */
    get cachedAlbums(): library.Album[] | null {
        return this.albums;
    }

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
            const id = this.selectedLibraryIdValue;
            const tracks = id !== null
                ? await GetAllTracksByLibrary(id)
                : await GetAllTracks();

            this.tracks = tracks;
            this.changeGen++;

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
            const id = this.selectedLibraryIdValue;
            const albums = id !== null
                ? await GetAllAlbumsByLibrary(id)
                : await GetAllAlbums();

            this.albums = albums;
            this.changeGen++;

            return albums;
        } finally {
            this.albumsLoading = false;
            this.notify();
        }
    }

    async getArtists(): Promise<library.Artist[]> {
        if (this.artists !== null) {
            return this.artists;
        }

        if (this.artistsLoading) {
            return this.waitForArtists();
        }

        this.artistsLoading = true;
        this.notify();

        try {
            const id = this.selectedLibraryIdValue;
            const artists = id !== null
                ? await GetAllArtistsByLibrary(id)
                : await GetAllArtists();

            this.artists = artists;
            this.changeGen++;

            return artists;
        } finally {
            this.artistsLoading = false;
            this.notify();
        }
    }

    async getGenres(): Promise<library.GenreWithCount[]> {
        if (this.genres !== null) {
            return this.genres;
        }

        if (this.genresLoading) {
            return this.waitForGenres();
        }

        this.genresLoading = true;
        this.notify();

        try {
            const id = this.selectedLibraryIdValue;
            const genres = id !== null
                ? await GetAllGenresWithCountsByLibrary(id)
                : await GetAllGenresWithCounts();

            this.genres = genres;
            this.changeGen++;

            return genres;
        } finally {
            this.genresLoading = false;
            this.notify();
        }
    }

    async getAlbumsByArtist(
        artistID: number,
    ): Promise<library.Album[]> {
        const id = this.selectedLibraryIdValue;

        return id !== null
            ? GetAlbumsByArtistByLibrary(artistID, id)
            : GetAlbumsByArtist(artistID);
    }

    /**
     * Returns albums filtered by artist name from
     * the in-memory cache, or null if the cache is
     * not populated.  This provides an instant
     * result when the all-albums list has already
     * been loaded (e.g. the user visited the albums
     * view first).
     *
     * Returns null when a library filter is active
     * because the cached albums are already filtered
     * by library — the client-side ArtistName match
     * is correct but forcing a backend query ensures
     * consistency.
     */
    getAlbumsByArtistNameCached(
        artistName: string,
    ): library.Album[] | null {
        if (this.selectedLibraryIdValue !== null) {
            return null;
        }

        if (this.albums === null) return null;

        return this.albums.filter(
            (a) => a.ArtistName === artistName,
        );
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

    getCachedArtists(): library.Artist[] | null {
        return this.artists;
    }

    isArtistsLoading(): boolean {
        return this.artistsLoading;
    }

    getCachedGenres(): library.GenreWithCount[] | null {
        return this.genres;
    }

    isGenresLoading(): boolean {
        return this.genresLoading;
    }

    /**
     * Monotonic counter that increments only when cached data
     * actually changes (tracks, albums, artists, genres, coverSize,
     * or invalidation).  Loading flag transitions do NOT increment.
     * Controllers can compare against a saved value to skip
     * unnecessary requestUpdate() calls.
     */
    get changeGeneration(): number {
        return this.changeGen;
    }

    // ===================================================================
    // LIBRARY FILTER
    // ===================================================================

    getSelectedLibraryId(): number | null {
        return this.selectedLibraryIdValue;
    }

    setSelectedLibrary(id: number | null): void {
        if (id === this.selectedLibraryIdValue) return;

        this.selectedLibraryIdValue = id;
        this.invalidate();
    }

    async getLibraries(): Promise<library.Info[]> {
        if (this.libraries !== null) {
            return this.libraries;
        }

        const libs =
            await GetAllLibrariesWithTrackCounts();

        this.libraries = libs;

        return libs;
    }

    /**
     * Resolves a library id to attach a download/want to: the selected
     * filter if one is set, otherwise the first known library. Callers
     * that need a library id (rather than "all libraries", which is
     * what `getSelectedLibraryId()` returning null means for browsing)
     * should use this instead of defaulting to 0 — id 0 never exists
     * and trips the library_id foreign key on download_requests /
     * download_wants.
     */
    async getDefaultLibraryId(): Promise<number | null> {
        if (this.selectedLibraryIdValue !== null) {
            return this.selectedLibraryIdValue;
        }

        const libraries = await this.getLibraries();

        return libraries[0]?.id ?? null;
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
    // COVER SIZE
    // ===================================================================

    getCoverSize(): number {
        return this.coverSizeValue;
    }

    setCoverSize(size: number): void {
        const clamped = Math.round(
            Math.max(COVER_SIZE_MIN, Math.min(COVER_SIZE_MAX, size)),
        );

        if (clamped === this.coverSizeValue) return;

        this.coverSizeValue = clamped;
        this.changeGen++;
        this.saveCoverSize();
        this.notify();
    }

    private loadCoverSize(): void {
        try {
            const stored = localStorage.getItem(COVER_SIZE_KEY);

            if (stored !== null) {
                const parsed = parseInt(stored, 10);

                if (!Number.isNaN(parsed)) {
                    this.coverSizeValue = Math.max(
                        COVER_SIZE_MIN,
                        Math.min(COVER_SIZE_MAX, parsed),
                    );
                }
            }
        } catch {
            // localStorage may be unavailable.
        }
    }

    private saveCoverSize(): void {
        try {
            localStorage.setItem(
                COVER_SIZE_KEY,
                String(this.coverSizeValue),
            );
        } catch {
            // localStorage may be unavailable.
        }
    }

    // ===================================================================
    // INVALIDATION
    // ===================================================================

    private invalidate(): void {
        this.tracks = null;
        this.albums = null;
        this.artists = null;
        this.genres = null;
        this.changeGen++;
        this.scrollPositions = { tracks: 0, albums: 0, artists: 0, genres: 0 };
        this.notify();
        this.eagerFetch();
    }

    /**
     * Fetches all library data.  Called after DOM ready
     * (initial load, via deferEagerFetch) and after cache
     * invalidation so that controller subscribers receive
     * fresh data on the next requestUpdate() cycle without
     * needing their own LibraryScanComplete listener.
     */
    private eagerFetch(): void {
        void this.getTracks();
        void this.getAlbums();
        void this.getArtists();
        void this.getGenres();
    }

    // ===================================================================
    // SUBSCRIPTION SYSTEM
    // ===================================================================

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    private notify(): void {
        if (this.notifyScheduled) return;
        this.notifyScheduled = true;
        queueMicrotask(() => {
            this.notifyScheduled = false;
            this.subscribers.forEach((callback) => callback());
        });
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

    private waitForArtists(): Promise<library.Artist[]> {
        return new Promise((resolve) => {
            const unsub = this.subscribe(() => {
                if (!this.artistsLoading && this.artists !== null) {
                    unsub();
                    resolve(this.artists);
                }
            });
        });
    }

    private waitForGenres(): Promise<library.GenreWithCount[]> {
        return new Promise((resolve) => {
            const unsub = this.subscribe(() => {
                if (!this.genresLoading && this.genres !== null) {
                    unsub();
                    resolve(this.genres);
                }
            });
        });
    }
}

// Singleton instance.
export const libraryStore = new LibraryStore();

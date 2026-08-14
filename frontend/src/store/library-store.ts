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
} from '@go/library/library.js';
import type * as library from '@go/library/models.js';
import { list } from '@utils/binding';
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
     * The one in-flight request per collection.
     *
     * Concurrent readers used to be deduplicated by deriving a promise
     * from subscriber notifications, which never settled when the fetch
     * *failed* (errors.M1) — the waiter tested for "loaded and not
     * loading", and a failed fetch is neither. Holding the request
     * itself makes every waiter settle exactly as the fetch did.
     */
    private inFlight = new Map<ViewName, Promise<unknown>>();

    /**
     * Monotonic counter incremented only when actual data changes
     * (not loading flag transitions). Subscribers can compare against
     * a saved value to skip requestUpdate when only loading state toggled.
     */
    private changeGen = 0;

    /**
     * Incremented only by invalidation — a scan, a retag, or a library
     * filter switch. A fetch captures it at request time and discards
     * its answer if it no longer matches, which is what stops library
     * A's tracks being cached while library B is selected (errors.C4).
     * Separate from `changeGen`, which any collection landing bumps.
     */
    private cacheGen = 0;

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
        EventsOn(Events.TrackPlayCountChanged, (payload: unknown) => {
            this.applyPlayCount(payload);
        });
        EventsOn(Events.TracksRemovedFromLibrary, (payload: unknown) => {
            this.applyTracksRemoved(payload);
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

    /**
     * Track a fetch: guard its answer against an invalidation that
     * happened while it was in flight, hold it as *the* in-flight
     * request for its collection, and clear the loading flag when it
     * settles either way.
     *
     * A stale answer is not cached and not returned — the caller is
     * chained onto whatever the current selection is fetching instead,
     * so "give me the tracks" always answers with the tracks of the
     * library that is selected when it answers.
     */
    private track<T>(
        slot: ViewName,
        request: Promise<T>,
        commit: (value: T) => void,
        current: () => Promise<T>,
    ): Promise<T> {
        const gen = this.cacheGen;
        const guarded = request.then((value) => {
            if (gen !== this.cacheGen) return current();

            commit(value);
            this.changeGen++;

            return value;
        });
        const tracked: Promise<T> = guarded.finally(() => {
            if (this.inFlight.get(slot) !== tracked) return;

            this.inFlight.delete(slot);
            this.setLoading(slot, false);
            this.notify();
        });

        this.inFlight.set(slot, tracked);
        this.setLoading(slot, true);
        this.notify();

        return tracked;
    }

    private pending<T>(slot: ViewName): Promise<T> | undefined {
        return this.inFlight.get(slot) as Promise<T> | undefined;
    }

    private setLoading(slot: ViewName, loading: boolean): void {
        switch (slot) {
            case 'tracks':
                this.tracksLoading = loading;
                break;
            case 'albums':
                this.albumsLoading = loading;
                break;
            case 'artists':
                this.artistsLoading = loading;
                break;
            case 'genres':
                this.genresLoading = loading;
                break;
        }
    }

    async getTracks(): Promise<library.Track[]> {
        if (this.tracks !== null) {
            return this.tracks;
        }

        const pending = this.pending<library.Track[]>('tracks');

        if (pending) return pending;

        const id = this.selectedLibraryIdValue;

        return this.track(
            'tracks',
            list(id !== null ? GetAllTracksByLibrary(id) : GetAllTracks()),
            (tracks) => {
                this.tracks = tracks;
            },
            () => this.getTracks(),
        );
    }

    async getAlbums(): Promise<library.Album[]> {
        if (this.albums !== null) {
            return this.albums;
        }

        const pending = this.pending<library.Album[]>('albums');

        if (pending) return pending;

        const id = this.selectedLibraryIdValue;

        return this.track(
            'albums',
            list(id !== null ? GetAllAlbumsByLibrary(id) : GetAllAlbums()),
            (albums) => {
                this.albums = albums;
            },
            () => this.getAlbums(),
        );
    }

    async getArtists(): Promise<library.Artist[]> {
        if (this.artists !== null) {
            return this.artists;
        }

        const pending = this.pending<library.Artist[]>('artists');

        if (pending) return pending;

        const id = this.selectedLibraryIdValue;

        return this.track(
            'artists',
            list(id !== null ? GetAllArtistsByLibrary(id) : GetAllArtists()),
            (artists) => {
                this.artists = artists;
            },
            () => this.getArtists(),
        );
    }

    async getGenres(): Promise<library.GenreWithCount[]> {
        if (this.genres !== null) {
            return this.genres;
        }

        const pending = this.pending<library.GenreWithCount[]>('genres');

        if (pending) return pending;

        const id = this.selectedLibraryIdValue;

        return this.track(
            'genres',
            list(
                id !== null
                    ? GetAllGenresWithCountsByLibrary(id)
                    : GetAllGenresWithCounts(),
            ),
            (genres) => {
                this.genres = genres;
            },
            () => this.getGenres(),
        );
    }

    async getAlbumsByArtist(
        artistID: number,
    ): Promise<library.Album[]> {
        const id = this.selectedLibraryIdValue;

        return list(
            id !== null
                ? GetAlbumsByArtistByLibrary(artistID, id)
                : GetAlbumsByArtist(artistID),
        );
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

        const libs = await list(GetAllLibrariesWithTrackCounts());

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

    /**
     * Patch one track's play statistics in place.
     *
     * Finishing a track used to arrive as TrackMetadataChanged, so every
     * song invalidated the whole cache and refetched tracks, albums,
     * artists and genres — ~37 MB across the IPC and ~0.8 s of blocked
     * main thread per song at 50 000 tracks (perf.C1), and it cleared
     * the user's track-list selection while it did (perf.C2).
     *
     * A play count changes one integer on one row. The backend sends
     * everything needed to write it, so nothing is refetched and no
     * collection identity changes except the tracks array itself.
     *
     * The array *is* replaced rather than mutated: consumers key their
     * memoized filter/sort caches on its identity (`track-list`'s
     * `cachedTracks` check is the load-bearing one), so an in-place
     * mutation would be invisible to every one of them. The individual
     * Track objects other than the patched one are shared, which is
     * what keeps this cheap.
     */
    private applyPlayCount(payload: unknown): void {
        if (this.tracks === null) return;

        const p = payload as {
            filePath?: string;
            playCount?: number;
            lastPlayed?: string;
        } | null;

        if (!p?.filePath) return;

        const idx = this.tracks.findIndex((t) => t.FilePath === p.filePath);

        if (idx === -1) return;

        const existing = this.tracks[idx];

        if (existing === undefined) return;

        const patched = Object.assign(
            Object.create(Object.getPrototypeOf(existing) as object),
            existing,
            {
                PlayCount: p.playCount ?? existing.PlayCount,
                LastPlayed: p.lastPlayed ?? existing.LastPlayed,
            },
        ) as library.Track;

        this.tracks = [
            ...this.tracks.slice(0, idx),
            patched,
            ...this.tracks.slice(idx + 1),
        ];

        this.changeGen++;
        this.notify();
    }

    /**
     * Splice removed tracks out in place, and refetch only the
     * summaries whose counts changed.
     *
     * `invalidate()` would be correct and is the expensive answer: it
     * nulls `tracks` and eagerly refetches it, which is ~37 MB across
     * the IPC at 50 000 tracks for an operation that removed three
     * rows. The event carries the paths precisely so this does not have
     * to happen — the same bargain `TrackPlayCountChanged` makes.
     *
     * The album, artist and genre lists really do change (their track
     * counts, and the row itself when its last track goes), so they are
     * dropped and refetched. They are the small collections.
     */
    private applyTracksRemoved(payload: unknown): void {
        const p = payload as { filePaths?: string[] } | null;
        const removed = p?.filePaths;

        if (!removed || removed.length === 0) return;

        // A tracks fetch already in flight would land holding the rows
        // that were just deleted, and it captured the cache generation
        // this patch is about to leave behind. There is no patch that
        // is equivalent to that, so fall back.
        if (this.inFlight.has('tracks')) {
            this.invalidate();

            return;
        }

        if (this.tracks !== null) {
            const gone = new Set(removed);
            const kept = this.tracks.filter((t) => !gone.has(t.FilePath));

            // A new array identity even when nothing matched would
            // invalidate every memoized filter/sort cache keyed on it
            // for no reason.
            if (kept.length !== this.tracks.length) {
                this.tracks = kept;
            }
        }

        this.albums = null;
        this.artists = null;
        this.genres = null;
        // Bumping the cache generation is what stops an album fetch
        // issued before the removal from committing its pre-removal
        // answer. Safe for the tracks slot precisely because the guard
        // above established there is nothing in flight for it.
        this.cacheGen++;
        this.inFlight.delete('albums');
        this.inFlight.delete('artists');
        this.inFlight.delete('genres');

        this.changeGen++;
        this.notify();

        const logged = (what: string) => (err: unknown) =>
            console.error(`library: could not reload ${what}`, err);

        void this.getAlbums().catch(logged('albums'));
        void this.getArtists().catch(logged('artists'));
        void this.getGenres().catch(logged('genres'));
    }

    private invalidate(): void {
        this.tracks = null;
        this.albums = null;
        this.artists = null;
        this.genres = null;
        // Anything still in flight was asked for on behalf of a
        // selection that no longer applies: forget it, so the eager
        // refetch below starts a request for the current one rather
        // than adopting the old one's answer.
        this.inFlight.clear();
        this.cacheGen++;
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
        // A failed fetch is reported by whichever view asked for the
        // data (it is that panel's failure, not the app's), but the
        // eager refetch has no caller to reject to — without a catch it
        // is an unhandled rejection.
        const logged = (what: string) => (err: unknown) =>
            console.error(`library: could not load ${what}`, err);

        void this.getTracks().catch(logged('tracks'));
        void this.getAlbums().catch(logged('albums'));
        void this.getArtists().catch(logged('artists'));
        void this.getGenres().catch(logged('genres'));
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

}

// Singleton instance.
export const libraryStore = new LibraryStore();

import type { ReactiveController, ReactiveControllerHost } from 'lit';
import type * as library from '@go/library/models.js';
import { libraryStore } from '../library-store';

type ViewName = 'tracks' | 'albums' | 'artists' | 'genres';

/**
 * LibraryController connects a Lit component to the LibraryStore.
 *
 * Usage in a component:
 *
 *   private library = new LibraryController(this);
 *
 *   async connectedCallback() {
 *     super.connectedCallback();
 *     this.tracks = await this.library.getTracks();
 *   }
 */
export class LibraryController implements ReactiveController {
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    // ===================================================================
    // LIFECYCLE HOOKS
    // ===================================================================

    /** Last observed changeGeneration from the store. */
    private lastChangeGen = libraryStore.changeGeneration;

    hostConnected(): void {
        this.lastChangeGen = libraryStore.changeGeneration;

        this.unsubscribe = libraryStore.subscribe(() => {
            const gen = libraryStore.changeGeneration;

            // Skip requestUpdate when only loading flags toggled
            // (changeGeneration unchanged means no actual data changed).
            if (gen !== this.lastChangeGen) {
                this.lastChangeGen = gen;
                this.host.requestUpdate();
            }
        });
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    // ===================================================================
    // DATA ACCESS
    // ===================================================================

    async getTracks(): Promise<library.Track[]> {
        return libraryStore.getTracks();
    }

    async getAlbums(): Promise<library.Album[]> {
        return libraryStore.getAlbums();
    }

    async getArtists(): Promise<library.Artist[]> {
        return libraryStore.getArtists();
    }

    async getGenres(): Promise<library.GenreWithCount[]> {
        return libraryStore.getGenres();
    }

    async getAlbumsByArtist(
        artist: string,
    ): Promise<library.Album[]> {
        return libraryStore.getAlbumsByArtist(artist);
    }

    getAlbumsByArtistNameCached(
        artistName: string,
    ): library.Album[] | null {
        return libraryStore.getAlbumsByArtistNameCached(
            artistName,
        );
    }

    get cachedTracks(): library.Track[] | null {
        return libraryStore.getCachedTracks();
    }

    get cachedAlbums(): library.Album[] | null {
        return libraryStore.getCachedAlbums();
    }

    get cachedArtists(): library.Artist[] | null {
        return libraryStore.getCachedArtists();
    }

    get cachedGenres(): library.GenreWithCount[] | null {
        return libraryStore.getCachedGenres();
    }

    get tracksLoading(): boolean {
        return libraryStore.isTracksLoading();
    }

    get albumsLoading(): boolean {
        return libraryStore.isAlbumsLoading();
    }

    get artistsLoading(): boolean {
        return libraryStore.isArtistsLoading();
    }

    get genresLoading(): boolean {
        return libraryStore.isGenresLoading();
    }

    // ===================================================================
    // SCROLL POSITION
    // ===================================================================

    getScrollPosition(view: ViewName): number {
        return libraryStore.getScrollPosition(view);
    }

    setScrollPosition(view: ViewName, offset: number): void {
        libraryStore.setScrollPosition(view, offset);
    }

    // ===================================================================
    // LIBRARY FILTER
    // ===================================================================

    get selectedLibraryId(): number | null {
        return libraryStore.getSelectedLibraryId();
    }

    setSelectedLibrary(id: number | null): void {
        libraryStore.setSelectedLibrary(id);
    }

    async getLibraries(): Promise<library.Info[]> {
        return libraryStore.getLibraries();
    }

    // ===================================================================
    // COVER SIZE
    // ===================================================================

    get coverSize(): number {
        return libraryStore.getCoverSize();
    }

    set coverSize(size: number) {
        libraryStore.setCoverSize(size);
    }
}

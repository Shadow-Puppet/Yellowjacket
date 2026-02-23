import type { ReactiveController, ReactiveControllerHost } from 'lit';
import type { library } from '@go/models';
import { libraryStore } from '../library-store';

type ViewName = 'tracks' | 'albums';

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

    hostConnected(): void {
        this.unsubscribe = libraryStore.subscribe(() => {
            this.host.requestUpdate();
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

    async getAlbumsByArtist(
        artistID: number,
    ): Promise<library.Album[]> {
        return libraryStore.getAlbumsByArtist(artistID);
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

    get tracksLoading(): boolean {
        return libraryStore.isTracksLoading();
    }

    get albumsLoading(): boolean {
        return libraryStore.isAlbumsLoading();
    }

    get artistsLoading(): boolean {
        return libraryStore.isArtistsLoading();
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
    // COVER SIZE
    // ===================================================================

    get coverSize(): number {
        return libraryStore.getCoverSize();
    }

    set coverSize(size: number) {
        libraryStore.setCoverSize(size);
    }
}

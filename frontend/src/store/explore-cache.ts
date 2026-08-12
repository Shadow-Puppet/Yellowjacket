/**
 * ExploreCache — a simple in-memory cache for explore data that
 * persists across page navigations within a session.  Populated by
 * search results and consumed by detail pages to avoid redundant
 * API calls.
 *
 * Data flows:
 *   search results → cache artist images, album art, release groups
 *   artist detail page → check cache before API calls
 *   album detail page → check cache before API calls
 */

import { registerCacheProbe } from '../utils/cache-stats';
import { LRUMap } from '../utils/lru-map';

/**
 * Cap for artist entries (`perf.M8`).
 *
 * An entry's `imageURL` is the artist photo's base64 data URL — ~128 kB
 * measured — and it is the *same string* `explore-view`'s own
 * `artistImageCache` holds.  Two unbounded maps referencing one string
 * means bounding either alone frees nothing, so this constant is
 * exported and both use it.  Changing it here changes both.
 */
export const ARTIST_IMAGE_CACHE_LIMIT = 32;

/**
 * Cap for album entries.  These are small — measured at 61 chars each,
 * since `coverArt` is a local `/coverart/…` path rather than a data URL
 * — so the cap is generous and exists to make the map bounded at all
 * rather than because it costs anything today.
 */
const ALBUM_CACHE_LIMIT = 512;

/** Cached artist data from search results. */
export interface CachedArtist {
    mbid: string;
    name: string;
    imageURL?: string;        // resolved artist image
    imageSmall?: string;      // library small image
    imageMedium?: string;     // library medium image
}

/** Cached album data from search results. */
export interface CachedAlbum {
    mbid: string;
    title: string;
    artistName: string;
    coverArt?: string;        // local cover art URL
    year?: string;
}

class ExploreCacheStore {
    private artists = new LRUMap<string, CachedArtist>(ARTIST_IMAGE_CACHE_LIMIT);
    private albums = new LRUMap<string, CachedAlbum>(ALBUM_CACHE_LIMIT);

    // -- Artists --

    setArtist(mbid: string, data: CachedArtist) {
        if (mbid) this.artists.set(mbid, data);
    }

    getArtist(mbid: string): CachedArtist | undefined {
        return this.artists.get(mbid);
    }

    // -- Albums --

    setAlbum(mbid: string, data: CachedAlbum) {
        if (mbid) this.albums.set(mbid, data);
    }

    getAlbum(mbid: string): CachedAlbum | undefined {
        return this.albums.get(mbid);
    }

    // -- Bulk populate from search results --

    populateFromSearch(artists: any[], releaseGroups: any[]) {
        for (const a of artists) {
            if (a.mbid) {
                this.setArtist(a.mbid, {
                    mbid: a.mbid,
                    name: a.name,
                    imageSmall: a._imageSmall,
                    imageMedium: a._imageMedium,
                });
            }
        }

        for (const rg of releaseGroups) {
            if (rg.mbid) {
                this.setAlbum(rg.mbid, {
                    mbid: rg.mbid,
                    title: rg.title,
                    artistName: rg.artistCredit || '',
                    coverArt: rg._coverArt,
                    year: rg.firstReleaseDate,
                });
            }
        }
    }

    /** Entries and retained string length, for `window.__yjCacheStats()`. */
    stats() {
        const size = (o: object) => {
            let n = 0;

            for (const v of Object.values(o)) {
                if (typeof v === 'string') n += v.length;
            }

            return n;
        };
        const of = (m: LRUMap<string, object>) => {
            let chars = 0;

            for (const v of m.values()) chars += size(v);

            return { entries: m.size, chars, limit: m.limit };
        };

        return {
            artists: of(this.artists as LRUMap<string, object>),
            albums: of(this.albums as LRUMap<string, object>),
        };
    }
}

export const exploreCache = new ExploreCacheStore();

registerCacheProbe('exploreCache.artists', () => exploreCache.stats().artists);
registerCacheProbe('exploreCache.albums', () => exploreCache.stats().albums);

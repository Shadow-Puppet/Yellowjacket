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

import type { explore } from '@go/models';
type MBReleaseGroup = explore.MBReleaseGroup;
type LBTopRecording = explore.LBTopRecording;

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
    private artists = new Map<string, CachedArtist>();
    private albums = new Map<string, CachedAlbum>();
    private artistAlbums = new Map<string, MBReleaseGroup[]>();
    private artistTopTracks = new Map<string, LBTopRecording[]>();

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

    // -- Artist → Albums (release groups) --

    setArtistAlbums(artistMBID: string, albums: MBReleaseGroup[]) {
        if (artistMBID) this.artistAlbums.set(artistMBID, albums);
    }

    getArtistAlbums(artistMBID: string): MBReleaseGroup[] | undefined {
        return this.artistAlbums.get(artistMBID);
    }

    // -- Artist → Top tracks --

    setArtistTopTracks(artistMBID: string, tracks: LBTopRecording[]) {
        if (artistMBID) this.artistTopTracks.set(artistMBID, tracks);
    }

    getArtistTopTracks(artistMBID: string): LBTopRecording[] | undefined {
        return this.artistTopTracks.get(artistMBID);
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
}

export const exploreCache = new ExploreCacheStore();

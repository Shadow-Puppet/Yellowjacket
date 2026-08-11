/**
 * The library store is the frontend's read cache for the whole
 * catalogue: four collections, each fetched once and held until
 * something invalidates them. The interesting behaviour is not the
 * fetching but the caching — deduplicating concurrent readers, throwing
 * everything away on a rescan, and swapping to the by-library bindings
 * when a filter is active.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { libraryStore } from '@store/library-store';
import { Events } from '../../src/events';
import {
  emit,
  calls,
  stub,
  flush,
  lastArgs,
  resetHarness,
} from '@test/support/harness';

const TRACKS = [{ ID: 1, Title: 'One' }];
const ALBUMS = [{ ID: 1, Name: 'Album', ArtistName: 'Artist' }];
const OTHER_ALBUMS = [{ ID: 2, Name: 'Other', ArtistName: 'Other Artist' }];
const ARTISTS = [{ ID: 1, Name: 'Artist' }];
const GENRES = [{ Name: 'Ambient', Count: 3 }];
const LIBRARIES = [{ id: 7, name: 'Music' }, { id: 8, name: 'Field' }];

/** Stub every read binding the store can reach. Unstubbed bindings
 *  resolve undefined, which the store would cache as if it were data. */
function stubReads(): void {
  stub('library.Library.GetAllTracks', TRACKS);
  stub('library.Library.GetAllAlbums', ALBUMS);
  stub('library.Library.GetAllArtists', ARTISTS);
  stub('library.Library.GetAllGenresWithCounts', GENRES);
  stub('library.Library.GetAllTracksByLibrary', TRACKS);
  stub('library.Library.GetAllAlbumsByLibrary', ALBUMS);
  stub('library.Library.GetAllArtistsByLibrary', ARTISTS);
  stub('library.Library.GetAllGenresWithCountsByLibrary', GENRES);
  stub('library.Library.GetAlbumsByArtist', ALBUMS);
  stub('library.Library.GetAlbumsByArtistByLibrary', ALBUMS);
  stub('library.Library.GetAllLibrariesWithTrackCounts', LIBRARIES);
}

/**
 * Drop the cache and let the eager refetch settle, so each test starts
 * from the same place. The store has no reset of its own; a scan
 * completing is how the app itself clears it.
 */
async function reload(): Promise<void> {
  stubReads();
  emit(Events.LibraryScanComplete);
  await flush();
  // The eager refetch the invalidation kicks off is recorded like any
  // other call; clear it, or every count in every test is off by one.
  resetHarness();
  stubReads();
}

describe('library store: caching', () => {
  beforeEach(async () => {
    await reload();
  });

  it('serves a second read from cache without touching the backend', async () => {
    await libraryStore.getTracks();

    expect(calls('library.Library.GetAllTracks')).toHaveLength(0);
  });

  it('deduplicates concurrent first reads into one backend call', async () => {
    emit(Events.LibraryScanComplete);
    const [a, b] = await Promise.all([
      libraryStore.getArtists(),
      libraryStore.getArtists(),
    ]);

    expect([a, b, calls('library.Library.GetAllArtists').length]).toEqual([
      ARTISTS,
      ARTISTS,
      1,
    ]);
  });

  it('exposes cached collections synchronously once loaded', () => {
    expect([
      libraryStore.getCachedTracks(),
      libraryStore.getCachedAlbums(),
      libraryStore.cachedArtists,
      libraryStore.getCachedGenres(),
    ]).toEqual([TRACKS, ALBUMS, ARTISTS, GENRES]);
  });

  it('refetches everything when a scan completes', async () => {
    emit(Events.LibraryScanComplete);
    await flush();

    expect(calls().map((c) => c.path).sort()).toEqual([
      'library.Library.GetAllAlbums',
      'library.Library.GetAllArtists',
      'library.Library.GetAllGenresWithCounts',
      'library.Library.GetAllTracks',
    ]);
  });

  it('refetches when a track is retagged', async () => {
    emit(Events.TrackMetadataChanged, { filePath: '/a.mp3' });
    await flush();

    expect(calls('library.Library.GetAllTracks')).toHaveLength(1);
  });

  it('resets scroll positions on invalidation, so a shorter list is not scrolled past its end', async () => {
    libraryStore.setScrollPosition('albums', 4200);
    emit(Events.LibraryScanComplete);
    await flush();

    expect(libraryStore.getScrollPosition('albums')).toBe(0);
  });

  it('bumps the change generation for data, not for loading flags', async () => {
    const before = libraryStore.changeGeneration;

    await libraryStore.getTracks(); // cached: no change
    const afterCachedRead = libraryStore.changeGeneration;

    emit(Events.LibraryScanComplete);
    await flush();

    expect([
      afterCachedRead === before,
      libraryStore.changeGeneration > before,
    ]).toEqual([true, true]);
  });
});

describe('library store: library filter', () => {
  beforeEach(async () => {
    libraryStore.setSelectedLibrary(null);
    await reload();
  });

  it('switches to the by-library bindings when a filter is set', async () => {
    libraryStore.setSelectedLibrary(7);
    await flush();

    expect(lastArgs('library.Library.GetAllTracksByLibrary')).toEqual([7]);
  });

  it('ignores a redundant selection instead of invalidating', async () => {
    libraryStore.setSelectedLibrary(null);
    await flush();

    expect(calls()).toEqual([]);
  });

  it('answers the cached artist-albums query only when unfiltered', async () => {
    const unfiltered = libraryStore.getAlbumsByArtistNameCached('Artist');

    libraryStore.setSelectedLibrary(7);
    await flush();

    // With a filter active the cache is already library-scoped, so the
    // store declines to answer and forces a backend query instead.
    expect([
      unfiltered,
      libraryStore.getAlbumsByArtistNameCached('Artist'),
    ]).toEqual([ALBUMS, null]);
  });

  it('scopes an artist drill-down to the selected library', async () => {
    libraryStore.setSelectedLibrary(8);
    await libraryStore.getAlbumsByArtist(3);

    expect(lastArgs('library.Library.GetAlbumsByArtistByLibrary')).toEqual([
      3, 8,
    ]);
  });
});

describe('library store: default library id', () => {
  beforeEach(async () => {
    libraryStore.setSelectedLibrary(null);
    await reload();
  });

  it('prefers the active filter', async () => {
    libraryStore.setSelectedLibrary(8);

    await expect(libraryStore.getDefaultLibraryId()).resolves.toBe(8);
  });

  it('falls back to the first known library, never to zero', async () => {
    // id 0 never exists and trips the download_requests foreign key.
    await expect(libraryStore.getDefaultLibraryId()).resolves.toBe(7);
  });

  it('returns null when there are no libraries at all', async () => {
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    emit(Events.LibraryRemoved, { id: 7 });

    await expect(libraryStore.getDefaultLibraryId()).resolves.toBeNull();
  });

  it('caches the library list until a library is added or renamed', async () => {
    const fetched = (): number =>
      calls('library.Library.GetAllLibrariesWithTrackCounts').length;

    // The list survives a rescan — only library CRUD changes it.
    emit(Events.LibraryAdded, { id: 9 });
    await libraryStore.getLibraries();
    const afterAdd = fetched();

    await libraryStore.getLibraries();
    const afterCachedRead = fetched();

    emit(Events.LibraryRenamed, { id: 7, name: 'Renamed' });
    await libraryStore.getLibraries();

    expect([afterAdd, afterCachedRead, fetched()]).toEqual([1, 1, 2]);
  });
});

describe('library store: cover size', () => {
  beforeEach(() => {
    localStorage.removeItem('cover-grid-size');
    libraryStore.setCoverSize(176);
  });

  it('clamps below the minimum card width', () => {
    libraryStore.setCoverSize(10);

    expect(libraryStore.getCoverSize()).toBe(100);
  });

  it('clamps above the maximum card width', () => {
    libraryStore.setCoverSize(9000);

    expect(libraryStore.getCoverSize()).toBe(350);
  });

  it('rounds a fractional size, since it becomes a CSS pixel value', () => {
    libraryStore.setCoverSize(180.6);

    expect(libraryStore.getCoverSize()).toBe(181);
  });

  it('persists the size for the next session', () => {
    libraryStore.setCoverSize(200);

    expect(localStorage.getItem('cover-grid-size')).toBe('200');
  });

  it('does not notify when the clamped size is unchanged', async () => {
    libraryStore.setCoverSize(300);
    await flush();

    let notifications = 0;
    const off = libraryStore.subscribe(() => {
      notifications += 1;
    });

    libraryStore.setCoverSize(400); // clamps back to 350 ≠ 300
    libraryStore.setCoverSize(9999); // clamps to 350, unchanged
    await flush();
    off();

    expect(notifications).toBe(1);
  });
});

describe('library store: albums by artist name', () => {
  beforeEach(async () => {
    libraryStore.setSelectedLibrary(null);
    await reload();
  });

  it('filters the album cache by artist name', () => {
    stub('library.Library.GetAllAlbums', [...ALBUMS, ...OTHER_ALBUMS]);

    expect(libraryStore.getAlbumsByArtistNameCached('Artist')).toEqual(ALBUMS);
  });

  it('returns an empty list, not null, for an artist with no albums', () => {
    expect(libraryStore.getAlbumsByArtistNameCached('Nobody')).toEqual([]);
  });
});

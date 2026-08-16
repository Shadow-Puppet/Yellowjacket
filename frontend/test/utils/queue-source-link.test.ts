import { describe, expect, it, vi, beforeEach } from 'vitest';

import {
  describeQueueSource,
  isQueueSourceNavigable,
  navigateToQueueSource,
} from '@utils/queue-source-link';
import { libraryStore } from '@store/library-store';
import type { QueueSource } from '@store/queue-store';
import { Events } from '../../src/events';
import { emit, flush, stub } from '@test/support/harness';

/**
 * The albums the library cache holds for these tests: one tagged, one
 * not, since which of the two an album is decides whether the album
 * page opens on the catalog or says it is library-only.
 */
const ALBUMS = [
  { ID: 7, Name: 'Scary Monsters', ArtistName: 'David Bowie', MBID: 'rg-7' },
  { ID: 8, Name: 'Untagged', ArtistName: 'Nobody', MBID: '' },
];

/** Drop the cache and refill it, the way a scan completing does. */
async function warmAlbumCache(): Promise<void> {
  stub('library.Library.GetAlbums', ALBUMS);
  emit(Events.LibraryScanComplete);
  await flush();
  await libraryStore.getAlbums();
}

describe('describeQueueSource', () => {
  it('returns null for an empty source', () => {
    expect(describeQueueSource({ type: '', id: 0, label: '' })).toBeNull();
  });

  it('describes a navigable source', () => {
    expect(
      describeQueueSource({ type: 'album', id: 1, label: 'Kid A' }),
    ).toBe('Playing from Kid A');
  });

  it('describes a dynamic mix, which has no page to navigate to', () => {
    expect(
      describeQueueSource({ type: 'dynamicMix', id: 0, label: 'a dynamic mix' }),
    ).toBe('Playing from a dynamic mix');
  });
});

describe('isQueueSourceNavigable', () => {
  it.each([
    ['album', true],
    ['playlist', true],
    ['smartPlaylist', true],
    ['genre', true],
    ['artist', true],
    ['dynamicMix', false],
    ['', false],
  ] as const)('%s -> %s', (type, expected) => {
    expect(isQueueSourceNavigable({ type, id: 0, label: '' })).toBe(expected);
  });
});

describe('navigateToQueueSource', () => {
  beforeEach(async () => {
    await warmAlbumCache();
  });

  function fireOn(source: QueueSource): unknown {
    const target = document.createElement('div');
    let detail: unknown;

    target.addEventListener('navigate', (e) => {
      detail = (e as CustomEvent).detail;
    });

    navigateToQueueSource(target, source);

    return detail;
  }

  it('builds the album navigate detail, carrying the release group MBID', () => {
    expect(fireOn({ type: 'album', id: 7, label: 'Scary Monsters' })).toEqual({
      view: 'explore-album-details',
      localAlbumId: 7,
      albumName: 'Scary Monsters',
      releaseGroupMBID: 'rg-7',
    });
  });

  it('omits the MBID for an untagged album, which really is library-only', () => {
    expect(fireOn({ type: 'album', id: 8, label: 'Untagged' })).toEqual({
      view: 'explore-album-details',
      localAlbumId: 8,
      albumName: 'Untagged',
    });
  });

  it('omits the MBID for an album the cache does not know', () => {
    expect(fireOn({ type: 'album', id: 99, label: 'Gone' })).toEqual({
      view: 'explore-album-details',
      localAlbumId: 99,
      albumName: 'Gone',
    });
  });

  it('builds the playlist navigate detail', () => {
    expect(fireOn({ type: 'playlist', id: 3, label: 'Road Trip' })).toEqual({
      view: 'playlist-details',
      playlistId: 3,
      playlistName: 'Road Trip',
    });
  });

  it('builds the smart playlist navigate detail', () => {
    expect(
      fireOn({ type: 'smartPlaylist', id: 4, label: 'Recently Added' }),
    ).toEqual({
      view: 'smart-playlist-details',
      playlistId: 4,
      playlistName: 'Recently Added',
    });
  });

  it('builds the genre navigate detail, which has no numeric id', () => {
    expect(fireOn({ type: 'genre', id: 0, label: 'Jazz' })).toEqual({
      view: 'genre-details',
      genreName: 'Jazz',
    });
  });

  it('builds the artist navigate detail', () => {
    expect(fireOn({ type: 'artist', id: 5, label: 'Björk' })).toEqual({
      view: 'artist-details',
      artistId: 5,
      artistName: 'Björk',
    });
  });

  it('does nothing for a source with no destination', () => {
    const dispatch = vi.fn();
    const target = { dispatchEvent: dispatch } as unknown as EventTarget;

    navigateToQueueSource(target, { type: 'dynamicMix', id: 0, label: 'a mix' });

    expect(dispatch).not.toHaveBeenCalled();
  });
});

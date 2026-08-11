/**
 * The three small stores behind view chrome: the global search term,
 * the track list's column set, and the explore cache that keeps detail
 * pages from re-fetching what a search already returned.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { searchStore } from '@store/search-store';
import { trackListStore } from '@store/tracklist-store';
import { exploreCache } from '@store/explore-cache';
import { Events } from '../../src/events';
import { emit, lastCall, flush } from '@test/support/harness';

describe('search store', () => {
  beforeEach(() => {
    searchStore.setTerm('');
    searchStore.setCurrentView('tracks');
  });

  it('holds the term', () => {
    searchStore.setTerm('bowie');

    expect(searchStore.getTerm()).toBe('bowie');
  });

  it('does not notify when the term is unchanged', () => {
    let notifications = 0;
    const off = searchStore.subscribe(() => {
      notifications += 1;
    });

    searchStore.setTerm('bowie');
    searchStore.setTerm('bowie');
    off();

    expect(notifications).toBe(1);
  });

  it('notifies synchronously — the search box has no batching to hide behind', () => {
    let notified = false;
    const off = searchStore.subscribe(() => {
      notified = true;
    });

    searchStore.setTerm('x');
    off();

    expect(notified).toBe(true);
  });

  it('knows which views the search box applies to', () => {
    const searchable = [
      'tracks',
      'albums',
      'playlists',
      'playlist-details',
      'artists',
      'genres',
    ].map((view) => {
      searchStore.setCurrentView(view);

      return searchStore.isSearchableView();
    });

    expect(searchable.every(Boolean)).toBe(true);
  });

  it('hides the search box on views it cannot filter', () => {
    const results = ['settings', 'explore', 'jobs', 'downloads'].map((view) => {
      searchStore.setCurrentView(view);

      return searchStore.isSearchableView();
    });

    expect(results).toEqual([false, false, false, false]);
  });
});

describe('track list store', () => {
  it('starts from the default column set', () => {
    expect(trackListStore.getState().columnIds.length).toBeGreaterThan(0);
  });

  it('adopts the column order the backend pushes', () => {
    emit(Events.TrackListConfigChanged, {
      columns: [{ id: 'title' }, { id: 'artist' }],
    });

    expect(trackListStore.getState().columnIds).toEqual(['title', 'artist']);
  });

  it('sends columns back as objects, the shape the Go binding expects', async () => {
    await trackListStore.setColumns(['title', 'album']);

    // A bare string array would be a type mismatch, and a Wails binding
    // called with wrong argument types never settles its callback.
    expect(lastCall('config.Config.SetTrackListColumns')?.args).toEqual([
      [{ id: 'title' }, { id: 'album' }],
    ]);
  });

  it('does not apply a column change until the backend confirms it', async () => {
    emit(Events.TrackListConfigChanged, { columns: [{ id: 'title' }] });
    await trackListStore.setColumns(['title', 'album', 'year']);
    await flush();

    expect(trackListStore.getState().columnIds).toEqual(['title']);
  });
});

describe('explore cache', () => {
  it('round-trips an artist by mbid', () => {
    exploreCache.setArtist('mbid-1', { mbid: 'mbid-1', name: 'Bowie' });

    expect(exploreCache.getArtist('mbid-1')?.name).toBe('Bowie');
  });

  it('misses cleanly for an unknown mbid', () => {
    expect(exploreCache.getArtist('nothing-here')).toBeUndefined();
  });

  it('refuses to key anything under an empty mbid', () => {
    // An empty key would collide across every unidentified entity.
    exploreCache.setArtist('', { mbid: '', name: 'Unknown' });
    exploreCache.setAlbum('', { mbid: '', title: 'X', artistName: 'Y' });

    expect([exploreCache.getArtist(''), exploreCache.getAlbum('')]).toEqual([
      undefined,
      undefined,
    ]);
  });

  it('overwrites an entry with richer data from a later fetch', () => {
    exploreCache.setArtist('mbid-2', { mbid: 'mbid-2', name: 'Eno' });
    exploreCache.setArtist('mbid-2', {
      mbid: 'mbid-2',
      name: 'Eno',
      imageURL: 'http://x/eno.jpg',
    });

    expect(exploreCache.getArtist('mbid-2')?.imageURL).toBe('http://x/eno.jpg');
  });

  it('caches an artist’s release groups and top tracks separately', () => {
    exploreCache.setArtistAlbums('mbid-3', [{ mbid: 'rg-1' }] as never);
    exploreCache.setArtistTopTracks('mbid-3', [{ mbid: 'rec-1' }] as never);

    expect([
      exploreCache.getArtistAlbums('mbid-3')?.length,
      exploreCache.getArtistTopTracks('mbid-3')?.length,
    ]).toEqual([1, 1]);
  });

  it('populates artists and albums from one search result', () => {
    exploreCache.populateFromSearch(
      [{ mbid: 'a-1', name: 'Artist', _imageSmall: 's.jpg' }],
      [
        {
          mbid: 'rg-1',
          title: 'Album',
          artistCredit: 'Artist',
          _coverArt: 'c.jpg',
          firstReleaseDate: '1977',
        },
      ],
    );

    expect([
      exploreCache.getArtist('a-1')?.imageSmall,
      exploreCache.getAlbum('rg-1')?.year,
      exploreCache.getAlbum('rg-1')?.artistName,
    ]).toEqual(['s.jpg', '1977', 'Artist']);
  });

  it('defaults a missing artist credit to empty rather than undefined', () => {
    exploreCache.populateFromSearch([], [{ mbid: 'rg-2', title: 'Untitled' }]);

    expect(exploreCache.getAlbum('rg-2')?.artistName).toBe('');
  });

  it('skips search entries that carry no mbid', () => {
    exploreCache.populateFromSearch(
      [{ name: 'Nameless' }],
      [{ title: 'Nameless' }],
    );

    expect(exploreCache.getArtist('')).toBeUndefined();
  });
});

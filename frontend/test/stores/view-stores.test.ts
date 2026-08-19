/**
 * The small stores behind view chrome: the global search term, the
 * active view both navs highlight, the track list's column set, and
 * the explore cache that keeps detail pages from re-fetching what a
 * search already returned.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { searchStore } from '@store/search-store';
import { activeViewStore } from '@store/active-view-store';
import { trackListStore } from '@store/tracklist-store';
import { exploreCache, ARTIST_IMAGE_CACHE_LIMIT } from '@store/explore-cache';
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
      // A detail view that filters on the term has to be in the map:
      // this one was not, so the box was disabled and unlabelled on a
      // page that narrowed its list as you typed.
      'smart-playlist-details',
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

describe('active view store', () => {
  beforeEach(() => {
    activeViewStore.setView('home', true);
  });

  it('holds the primary view the shell navigated to', () => {
    activeViewStore.setView('albums', true);

    expect(activeViewStore.get()).toBe('albums');
    expect(activeViewStore.isActive('albums')).toBe(true);
    expect(activeViewStore.isActive('tracks')).toBe(false);
  });

  it('leaves the primary view lit while a detail view is open', () => {
    activeViewStore.setView('albums', true);
    activeViewStore.setView('explore-album-details', false);

    // #72's third finding, made deliberate: a detail view is not a
    // destination in either nav, and the tab it was opened from is
    // where the user still is. `app-sidebar` did this by accident (it
    // guarded on its own item list) and `bottom-nav` did not do it at
    // all, which is why one looked right and the other looked broken.
    expect(activeViewStore.get()).toBe('albums');
  });

  it('does not notify when the view is unchanged', () => {
    let notifications = 0;
    const off = activeViewStore.subscribe(() => {
      notifications += 1;
    });

    activeViewStore.setView('albums', true);
    activeViewStore.setView('albums', true);
    activeViewStore.setView('explore-album-details', false);
    off();

    expect(notifications).toBe(1);
  });

  it('lights nothing for a view with no name', () => {
    // The store starts empty rather than defaulting to a view, because
    // a written-down default is right only while `GetDefaultPage()`
    // agrees with it. That is only safe if the empty value matches
    // nothing: `isActive` compares strings, and a component asking
    // about an id it does not have must not light up.
    activeViewStore.setView('', true);

    expect(activeViewStore.isActive('')).toBe(false);
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

  // `perf.M8`. An artist entry holds the artist photo's base64 data URL
  // — ~128 kB measured — so an unbounded map on a view that never
  // unmounts grows for the life of the process.
  it('evicts the least recently used artist past its cap', () => {
    const over = ARTIST_IMAGE_CACHE_LIMIT + 10;

    for (let i = 0; i < over; i++) {
      exploreCache.setArtist(`cap-${i}`, { mbid: `cap-${i}`, name: `A${i}` });
    }

    expect(exploreCache.stats().artists.entries).toBe(ARTIST_IMAGE_CACHE_LIMIT);
    // The first inserted is gone; the last is not.
    expect(exploreCache.getArtist('cap-0')).toBeUndefined();
    expect(exploreCache.getArtist(`cap-${over - 1}`)?.name).toBe(`A${over - 1}`);
  });

  it('keeps an artist alive by reading it', () => {
    // Recency is what makes the cap safe: the entry being rendered must
    // not be the one evicted, or the render refetches it immediately.
    for (let i = 0; i < ARTIST_IMAGE_CACHE_LIMIT; i++) {
      exploreCache.setArtist(`lru-${i}`, { mbid: `lru-${i}`, name: `A${i}` });
    }

    exploreCache.getArtist('lru-0');
    exploreCache.setArtist('lru-new', { mbid: 'lru-new', name: 'New' });

    expect(exploreCache.getArtist('lru-0')?.name).toBe('A0');
    expect(exploreCache.getArtist('lru-1')).toBeUndefined();
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

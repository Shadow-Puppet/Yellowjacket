/**
 * The play-all/shuffle-all pair on every page that lists tracks.
 *
 * The pair is driven by one helper (`utils/play-all`) that owns the
 * one rule the album page already carried: `shuffleStart` does not turn
 * shuffle on, it only picks a random first track once the mode is on —
 * so the mode has to be set *before* the queue, not after.  The hosts
 * differ only in where their paths come from and what `Source` they
 * hand over.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/track-list/track-list';
import '@components/artist-details/artist-details';
import '@components/playlist-details/playlist-details';
import '@components/smart-playlist-details/smart-playlist-details';
import {
  stub,
  flush,
  resetHarness,
  calls,
  lastArgs,
  emit,
} from '@test/support/harness';
import {
  fixture,
  shadowAll,
  deepShadow,
} from '@test/support/render';

/** The action button rendered by `<page-header>`, through the nested
 *  shadow roots (track-list → page-header). */
function pageAction(
  host: LitElement,
  id: string,
): HTMLElement | null {
  return deepShadow<HTMLElement>(host, `[data-testid="page-action-${id}"]`);
}

function setShuffleMode(on: boolean): void {
  emit('QueueModeChanged', { shuffleMode: on, repeatMode: 'off' });
}

/** The queue's `SetQueue` args, with the shuffle flag and source. */
function queued(): {
  paths: string[];
  startIndex: number;
  shuffleStart: boolean;
  source: unknown;
} {
  const args = lastArgs('queue.Queue.SetQueue');

  if (!args) throw new Error('nothing was queued');

  return {
    paths: args[0] as string[],
    startIndex: args[1] as number,
    shuffleStart: args[2] as boolean,
    source: args[3],
  };
}

// =====================================================================
// The track list (Tracks, and every embedding detail view)
// =====================================================================

const PATHS = Array.from({ length: 12 }, (_, i) => `/music/track-${i}.mp3`);

const LIST = PATHS.map((FilePath, i) => ({
  FilePath,
  TrackName: `Track ${i}`,
  ArtistName: 'An Artist',
  Album: 'An Album',
  Duration: 180,
}));

const GENRE_SOURCE = { type: 'genre', id: 0, label: 'Dream Pop' };

async function embeddedTrackList(): Promise<LitElement> {
  resetHarness();
  localStorage.removeItem('track-list-column-widths');

  const el = await fixture<LitElement>('track-list', {
    externalTracks: LIST,
    queueSource: GENRE_SOURCE,
  });

  // Say which order is being asserted rather than inheriting a
  // persisted sort. See play-in-context.test.ts for the same trap.
  (el as unknown as { sortField: string | null }).sortField = null;

  el.style.display = 'block';
  el.style.height = '600px';
  await flush();
  await el.updateComplete;
  await new Promise((r) => setTimeout(r, 60));

  return el;
}

describe('the track-list play-all/shuffle-all pair', () => {
  beforeEach(() => {
    resetHarness();
    localStorage.removeItem('track-list-column-widths');
  });

  it('renders in the primary Tracks header and is disabled while empty', async () => {
    const el = await fixture<LitElement>('track-list', {});

    const play = pageAction(el, 'play-all');
    const shuffle = pageAction(el, 'shuffle-all');

    expect(play).not.toBeNull();
    expect(shuffle).not.toBeNull();
    expect(play?.hasAttribute('disabled')).toBe(true);
    expect(shuffle?.hasAttribute('disabled')).toBe(true);
  });

  it('renders in an embedded track-list header too', async () => {
    const el = await embeddedTrackList();

    expect(pageAction(el, 'play-all')).not.toBeNull();
    expect(pageAction(el, 'shuffle-all')).not.toBeNull();
    expect(pageAction(el, 'play-all')?.hasAttribute('disabled')).toBe(false);
    expect(pageAction(el, 'shuffle-all')?.hasAttribute('disabled')).toBe(false);
  });

  it('Play all queues the displayed list in order, unshuffled', async () => {
    const el = await embeddedTrackList();

    pageAction(el, 'play-all')!.click();
    await flush();

    expect(queued()).toEqual({
      paths: PATHS,
      startIndex: 0,
      shuffleStart: false,
      source: GENRE_SOURCE,
    });
  });

  it('Shuffle all turns shuffle on before queueing when the mode is off', async () => {
    const el = await embeddedTrackList();

    setShuffleMode(false);
    await flush();

    pageAction(el, 'shuffle-all')!.click();
    await flush();

    const all = calls();
    const toggle = all.findLastIndex(
      (c) => c.path === 'queue.Queue.ToggleShuffle',
    );
    const setQueue = all.findLastIndex(
      (c) => c.path === 'queue.Queue.SetQueue',
    );

    expect(toggle).toBeGreaterThanOrEqual(0);
    expect(toggle).toBeLessThan(setQueue);

    expect(queued()).toEqual({
      paths: PATHS,
      startIndex: 0,
      shuffleStart: true,
      source: GENRE_SOURCE,
    });
  });

  it('Shuffle all does not toggle when the mode is already on', async () => {
    const el = await embeddedTrackList();

    setShuffleMode(true);
    await flush();

    pageAction(el, 'shuffle-all')!.click();
    await flush();

    expect(calls('queue.Queue.ToggleShuffle')).toHaveLength(0);

    expect(queued()).toEqual({
      paths: PATHS,
      startIndex: 0,
      shuffleStart: true,
      source: GENRE_SOURCE,
    });
  });
});

// =====================================================================
// The library artist page
// =====================================================================

const ALBUMS = [
  { ID: 3, Name: 'Third', ArtistName: 'Aurora Fields', ArtistMBID: '', MBID: '', CoverArtPath: '', CoverArtSmall: '', CoverArtMedium: '', CoverArtLarge: '', Year: 0, ReleaseYear: 0 },
  { ID: 1, Name: 'First', ArtistName: 'Aurora Fields', ArtistMBID: '', MBID: '', CoverArtPath: '', CoverArtSmall: '', CoverArtMedium: '', CoverArtLarge: '', Year: 0, ReleaseYear: 0 },
  { ID: 2, Name: 'Second', ArtistName: 'Aurora Fields', ArtistMBID: '', MBID: '', CoverArtPath: '', CoverArtSmall: '', CoverArtMedium: '', CoverArtLarge: '', Year: 0, ReleaseYear: 0 },
];

describe('the artist page play-all pair', () => {
  beforeEach(() => {
    resetHarness();
    stub('explore.Service.GetArtistMBID', '');
    stub('library.Library.GetAlbumsByArtist', ALBUMS);
    stub('library.Library.GetFilePathsByAlbums', {
      '3': ['/a3-1', '/a3-2'],
      '1': ['/a1'],
      '2': ['/a2-1', '/a2-2', '/a2-3'],
    });
  });

  it('flattens album paths in the album list order', async () => {
    const el = await fixture<LitElement>('artist-details', {
      artistId: 7,
      artistName: 'Aurora Fields',
      artistMBID: '',
    });

    await flush();
    await el.updateComplete;

    const play = shadowAll<HTMLElement>(el, '[data-testid="artist-play-all"]')[0];

    play!.click();
    await flush();

    expect(queued()).toEqual({
      paths: ['/a3-1', '/a3-2', '/a1', '/a2-1', '/a2-2', '/a2-3'],
      startIndex: 0,
      shuffleStart: false,
      source: { type: 'artist', id: 7, label: 'Aurora Fields' },
    });
  });
});

// =====================================================================
// A smart playlist
// =====================================================================

function smartPlaylistTracks(n: number) {
  return Array.from({ length: n }, (_, i) => ({
    ID: i + 1,
    FilePath: `/music/track-${i}.mp3`,
    Title: `Track ${i}`,
    Artist: 'An Artist',
    Album: 'An Album',
    Duration: 180000,
    CoverArtSmall: `/covers/${i}_sm.jpg`,
    CoverArtMedium: `/covers/${i}_md.jpg`,
    CoverArtPath: `/covers/${i}.jpg`,
    Phantom: false,
  }));
}

describe('the smart-playlist play-all pair', () => {
  beforeEach(() => {
    resetHarness();
    stub('playlist.Service.GetSmartPlaylistTracks', smartPlaylistTracks(8));
    stub('playlist.Service.GetSmartPlaylistRules', '{"rules":[]}');
    stub('playlist.Service.GetAllPlaylists', []);
  });

  /** The details header's own action row, not a page-header action. */
  function actionButton(el: LitElement, label: string): HTMLElement {
    const button = shadowAll<HTMLElement>(el, '.action-button').find(
      (b) => b.textContent?.trim() === label,
    );

    if (!button) throw new Error(`no "${label}" action button rendered`);

    return button;
  }

  it('Shuffle turns the mode on before queueing, so the queue starts shuffled', async () => {
    const el = await fixture<LitElement>('smart-playlist-details', {
      playlistId: 1,
      playlistName: 'A smart playlist',
    });

    el.style.display = 'block';
    el.style.height = '600px';
    await flush();
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));

    setShuffleMode(false);
    await flush();

    actionButton(el, 'Shuffle').click();
    await flush();

    // The issue's headline fix. `SetQueue`'s `shuffleStart` only picks
    // a random first track when shuffle mode is already on — it does
    // not turn it on — so reverting the mode toggle puts the queue back
    // to track 1 in order while every assertion about the queue's
    // contents still passes. Order of the calls is the assertion.
    const all = calls();
    const toggle = all.findLastIndex(
      (c) => c.path === 'queue.Queue.ToggleShuffle',
    );
    const setQueue = all.findLastIndex(
      (c) => c.path === 'queue.Queue.SetQueue',
    );

    expect(toggle).toBeGreaterThanOrEqual(0);
    expect(toggle).toBeLessThan(setQueue);

    expect(queued()).toEqual({
      paths: Array.from({ length: 8 }, (_, i) => `/music/track-${i}.mp3`),
      startIndex: 0,
      shuffleStart: true,
      source: { type: 'smartPlaylist', id: 1, label: 'A smart playlist' },
    });
  });
});

// =====================================================================
// A regular playlist
// =====================================================================

function playlistTracks(n: number) {
  return Array.from({ length: n }, (_, i) => ({
    ID: i + 1,
    FilePath: `/music/track-${i}.mp3`,
    Title: `Track ${i}`,
    Artist: 'An Artist',
    Album: 'An Album',
    Duration: 180000,
    Phantom: false,
  }));
}

describe('the playlist play-all pair', () => {
  beforeEach(() => {
    resetHarness();
    stub('playlist.Service.GetPlaylistTracks', playlistTracks(8));
    stub('playlist.Service.GetAllPlaylists', []);
  });

  it('offers Shuffle All beside Play All, both through the helper', async () => {
    const el = await fixture<LitElement>('playlist-details', {
      playlistId: 1,
      playlistName: 'A playlist',
    });

    el.style.display = 'block';
    el.style.height = '600px';
    await flush();
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));

    const buttons = shadowAll<HTMLElement>(el, '.play-all-button');

    expect(buttons.map((b) => b.textContent?.trim())).toEqual([
      'Play All',
      'Shuffle All',
    ]);

    setShuffleMode(false);
    await flush();

    buttons[1]!.click();
    await flush();

    expect(queued()).toEqual({
      paths: Array.from({ length: 8 }, (_, i) => `/music/track-${i}.mp3`),
      startIndex: 0,
      shuffleStart: true,
      source: { type: 'playlist', id: 1, label: 'A playlist' },
    });
  });
});

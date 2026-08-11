/**
 * `<now-playing>` and `<queue-panel>` are the two components driven
 * entirely by store state rather than by their own properties: feeding
 * them backend events is how they are made to render anything at all.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/now-playing/now-playing';
import '@components/queue-panel/queue-panel';
import { Events } from '../../src/events';
import { emit, calls, stub, flush, lastArgs } from '@test/support/harness';
import {
  fixture,
  shadow,
  shadowAll,
  text,
  visual,
} from '@test/support/render';
import type { TrackInfo } from '@store/player-store';
import type { QueueTrack } from '@store/queue-store';

const TRACK: TrackInfo = {
  fileName: 'ashes.mp3',
  filePath: '/music/ashes.mp3',
  trackLength: 215,
  seekPosition: 0,
  state: 'playing',
  title: 'Ashes to Ashes',
  artist: 'David Bowie',
  album: 'Scary Monsters',
  coverArt: '',
  coverArtSmall: '',
  coverArtMedium: '',
  coverArtLarge: '',
  trackChangeId: 1,
  artistMbid: '',
  releaseGroupMbid: '',
  recordingMbid: '',
};

function queueTrack(n: number, title: string): QueueTrack {
  return {
    id: n,
    audioFileId: n,
    filePath: `/music/${n}.mp3`,
    position: n,
    title,
    artist: 'Artist',
    album: 'Album',
    coverArtPath: '',
    artistMbid: '',
    releaseGroupMbid: '',
    recordingMbid: '',
  };
}

function setQueue(tracks: QueueTrack[], currentIndex = 0): void {
  emit(Events.QueueChanged, {
    tracks,
    currentIndex,
    shuffleMode: false,
    repeatMode: 'off',
    sourcePlaylistId: 0,
  });
}

describe('<now-playing>', () => {
  beforeEach(() => {
    emit(Events.TrackChanged, null);
  });

  it('shows only a placeholder when nothing is loaded', async () => {
    const el = await fixture('now-playing');

    expect([
      shadow(el, '.cover-placeholder'),
      shadow(el, '[data-testid="now-playing-title"]'),
    ]).toEqual([expect.anything(), null]);
  });

  it('renders the title and artist of the loaded track', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, TRACK);
    await flush();
    await el.updateComplete;

    expect([
      text(el, '[data-testid="now-playing-title"]'),
      text(el, '[data-testid="now-playing-artist"]'),
    ]).toEqual(['Ashes to Ashes', 'David Bowie']);
  });

  it('names an artistless track rather than leaving the line blank', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, artist: '', trackChangeId: 2 });
    await flush();
    await el.updateComplete;

    expect(text(el, '[data-testid="now-playing-artist"]')).toBe(
      'Unknown Artist',
    );
  });

  it('prefers the small cover variant and falls back on error', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, {
      ...TRACK,
      coverArt: '/covers/big.jpg',
      coverArtSmall: '/covers/missing.jpg',
      trackChangeId: 3,
    });
    await flush();
    await el.updateComplete;

    const img = shadow<HTMLImageElement>(el, '.cover-art img');
    const initial = img?.getAttribute('src');

    img?.dispatchEvent(new Event('error'));

    expect([initial, img?.src.endsWith('/covers/big.jpg')]).toEqual([
      '/covers/missing.jpg',
      true,
    ]);
  });

  it('offers a favourite toggle that names the target playlist', async () => {
    stub('playlist.Service.GetDefaultPlaylistTrackPaths', []);
    stub('playlist.Service.GetDefaultPlaylistInfo', { Name: 'Loved' });
    emit(Events.PlaylistRenamed, 1);
    await flush();

    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 4 });
    await flush();
    await el.updateComplete;

    expect(shadow(el, '.fav-btn')?.getAttribute('title')).toBe('Add to Loved');
  });

  it('toggles the favourite through the backend', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 5 });
    await flush();
    await el.updateComplete;

    shadow<HTMLElement>(el, '.fav-btn')?.click();
    await flush();

    expect(lastArgs('playlist.Service.ToggleDefaultPlaylistTrack')).toEqual([
      '/music/ashes.mp3',
    ]);
  });

  it('looks the way it did last time', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 6 });
    await flush();
    await el.updateComplete;

    await visual(el, 'now-playing');
    expect(text(el, '[data-testid="now-playing-title"]')).toBe(
      'Ashes to Ashes',
    );
  });
});

describe('<queue-panel>', () => {
  beforeEach(() => {
    setQueue([]);
  });

  it('says so when the queue is empty', async () => {
    const el = await fixture('queue-panel');

    expect(text(el, '.empty-state p')).toBe('Queue is empty');
  });

  it('renders a row per queued track, tagged with its file path', async () => {
    const el = await fixture('queue-panel');

    setQueue([queueTrack(1, 'First'), queueTrack(2, 'Second')]);
    await flush();
    await el.updateComplete;
    await new Promise((r) => {
      requestAnimationFrame(() => r(null));
    });

    const rows = shadowAll(el, '[data-testid="queue-row"]');

    expect(rows.map((r) => r.getAttribute('data-file-path'))).toEqual([
      '/music/1.mp3',
      '/music/2.mp3',
    ]);
  });

  it('marks the playing row as active', async () => {
    const el = await fixture('queue-panel');

    setQueue([queueTrack(1, 'First'), queueTrack(2, 'Second')], 1);
    await flush();
    await el.updateComplete;
    await new Promise((r) => {
      requestAnimationFrame(() => r(null));
    });

    const active = shadowAll(el, '[data-testid="queue-row"].active');

    expect(active.map((r) => r.getAttribute('data-index'))).toEqual(['1']);
  });

  it('disables the clear button on an empty queue', async () => {
    const el = await fixture('queue-panel');

    const button = shadow<HTMLButtonElement>(el, '.header-action-button');

    expect(button?.disabled).toBe(true);
  });

  it('clears through the backend, not locally', async () => {
    const el = await fixture('queue-panel');

    setQueue([queueTrack(1, 'First')]);
    await flush();
    await el.updateComplete;

    shadow<HTMLButtonElement>(el, '.header-action-button')?.click();
    await flush();

    expect(calls().map((c) => c.path)).toContain('queue.Queue.Clear');
  });

  // No screenshot for the queue panel: its list is a
  // @lit-labs/virtualizer, which keeps re-measuring, so
  // toMatchScreenshot never gets two identical frames and fails with
  // "could not capture a stable screenshot" rather than a real diff.
  it('keeps rendering rows after the virtualizer settles', async () => {
    const el = await fixture('queue-panel');

    setQueue([queueTrack(1, 'First'), queueTrack(2, 'Second')], 0);
    await flush();
    await el.updateComplete;
    await new Promise((r) => {
      requestAnimationFrame(() => r(null));
    });

    expect(shadowAll(el, '[data-testid="queue-row"]').length).toBe(2);
  });
});

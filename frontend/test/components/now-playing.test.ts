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

function setQueue(
  tracks: QueueTrack[],
  currentIndex = 0,
  source = { type: '', id: 0, label: '' },
): void {
  emit(Events.QueueChanged, {
    tracks,
    currentIndex,
    shuffleMode: false,
    repeatMode: 'off',
    source,
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

  // perf.m5. `updated()` used to measure and rewrite the text geometry
  // on every pass, and the player store notifies while playing — so a
  // component whose DOM had not changed did six querySelectors and a
  // read/write interleave several times a second. These two pin the
  // guard from both sides: it has to skip the work when nothing it
  // measures changed, and it has to *not* skip it when something did.
  async function settle(el: HTMLElement & { updateComplete: Promise<unknown> }) {
    for (let i = 0; i < 3; i++) {
      await new Promise((r) => {
        requestAnimationFrame(() => r(null));
      });
      await flush();
      await el.updateComplete;
    }
  }

  function countShadowQueries(el: HTMLElement): () => number {
    const root = el.shadowRoot!;
    const orig = root.querySelector.bind(root);
    let n = 0;

    root.querySelector = ((...args: [string]) => {
      n++;

      return orig(...args);
    }) as typeof root.querySelector;

    return () => n;
  }

  it('does not touch the DOM again when a position report changes nothing', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 7 });
    await flush();
    await el.updateComplete;
    // `observe()` delivers an initial callback of its own, which is one
    // more legitimate re-measure. Let it land before counting, or the
    // straggler reads as the thing this is asserting is gone.
    await settle(el);

    const queries = countShadowQueries(el);

    for (let i = 0; i < 3; i++) {
      emit(Events.PlaybackPositionChanged, {
        positionSeconds: i + 1,
        trackChangeId: 7,
        seq: i,
      });
      await flush();
      await el.updateComplete;
    }

    expect(queries()).toBe(0);
  });

  it('re-measures when the track changes', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 8 });
    await flush();
    await el.updateComplete;

    const queries = countShadowQueries(el);

    emit(Events.TrackChanged, {
      ...TRACK,
      title: 'Teenage Wildlife',
      trackChangeId: 9,
    });
    await flush();
    await el.updateComplete;

    expect(queries()).toBeGreaterThan(0);
  });

  // a11y.15 (WCAG 2.2.2). Reproduced in the running app first: under an
  // emulated `prefers-reduced-motion: reduce` the title still carried
  // `will-scroll` with a 15s transition and the transform was still
  // moving — the read landed in the *snap-back* half of the cycle,
  // which is why suppressing the transition alone is not the fix.
  //
  // Both directions are asserted because a guard that suppresses
  // everything passes the negative case for free, and a component that
  // never scrolls at this width would too.
  const LONG =
    'An Exhaustively Overlong Track Title That Exists Solely To Find Out ' +
    'Whether The Bottom Bar Truncates Or Overflows';

  async function mountScrolling(reduce: boolean) {
    const real = window.matchMedia.bind(window);

    window.matchMedia = ((q: string) =>
      q.includes('prefers-reduced-motion')
        ? {
            matches: reduce,
            media: q,
            addEventListener() {},
            removeEventListener() {},
          }
        : real(q)) as typeof window.matchMedia;

    try {
      localStorage.setItem('yj-now-playing-scroll-mode', 'always');

      const el = await fixture('now-playing');

      // The real host is sized by `--now-playing-width` on `.bottom-bar`,
      // which the fixture does not have — so it is document-width here
      // and nothing overflows, which made the positive case fail first.
      el.style.width = '320px';

      emit(Events.TrackChanged, { ...TRACK, title: LONG, trackChangeId: 10 });
      await flush();
      await settle(el);

      return shadow(el, '.track-title')?.className ?? '';
    } finally {
      window.matchMedia = real;
      localStorage.removeItem('yj-now-playing-scroll-mode');
    }
  }

  it('scrolls an overflowing title when motion is not a problem', async () => {
    expect(await mountScrolling(false)).toContain('will-scroll');
  });

  it('does not scroll at all under prefers-reduced-motion', async () => {
    expect(await mountScrolling(true)).not.toContain('will-scroll');
  });

  it('shows no source line when the queue has no known source', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 10 });
    setQueue([queueTrack(1, 'Ashes to Ashes')]);
    await flush();
    await el.updateComplete;

    expect(shadow(el, '[data-testid="now-playing-source"]')).toBeNull();
  });

  it('names where the queue came from, and navigates back to it', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 11 });
    setQueue([queueTrack(1, 'Ashes to Ashes')], 0, {
      type: 'album',
      id: 7,
      label: 'Scary Monsters',
    });
    await flush();
    await el.updateComplete;

    expect(text(el, '[data-testid="now-playing-source"]')).toBe(
      'Playing from Scary Monsters',
    );

    let detail: unknown;
    el.addEventListener('navigate', (e) => {
      detail = (e as CustomEvent).detail;
    });

    shadow<HTMLElement>(el, '[data-testid="now-playing-source"]')?.click();

    expect(detail).toEqual({
      view: 'explore-album-details',
      localAlbumId: 7,
      albumName: 'Scary Monsters',
    });
  });

  it('names a dynamic mix as text, not a dead link', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 12 });
    setQueue([queueTrack(1, 'Ashes to Ashes')], 0, {
      type: 'dynamicMix',
      id: 0,
      label: 'a dynamic mix',
    });
    await flush();
    await el.updateComplete;

    const sourceEl = shadow<HTMLElement>(
      el,
      '[data-testid="now-playing-source"]',
    );

    expect(sourceEl?.textContent).toBe('Playing from a dynamic mix');
    expect(sourceEl?.classList.contains('navigable')).toBe(false);

    let navigated = false;
    el.addEventListener('navigate', () => {
      navigated = true;
    });

    sourceEl?.click();

    expect(navigated).toBe(false);
  });

  it('looks the way it did last time', async () => {
    const el = await fixture('now-playing');

    emit(Events.TrackChanged, { ...TRACK, trackChangeId: 6 });
    // Stated rather than inherited: the queue store is a singleton, so
    // without this the shot records whichever source the *previous*
    // case left in it and the reference moves when the file is
    // reordered. Three lines is what the bar renders while playing
    // from somewhere, which is the arrangement worth recording.
    setQueue([queueTrack(1, 'Ashes to Ashes')], 0, {
      type: 'album',
      id: 7,
      label: 'Scary Monsters',
    });
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

  // Every case here mounts the panel *open*. A closed panel renders no
  // list at all (perf.m7) — `width: 0` used to hide a virtualizer that
  // was still measuring its window on every queue change and still
  // calling `scrollIntoView()` on an invisible element. These tests
  // passed against a closed panel before that, which is the finding
  // rather than a detail of the fixture.

  it('says so when the queue is empty', async () => {
    const el = await fixture('queue-panel', { open: true });

    expect(text(el, '.empty-state p')).toBe('Queue is empty');
  });

  it('renders a row per queued track, tagged with its file path', async () => {
    const el = await fixture('queue-panel', { open: true });

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
    const el = await fixture('queue-panel', { open: true });

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
    const el = await fixture('queue-panel', { open: true });

    const button = shadow<HTMLButtonElement>(el, '.header-action-button');

    expect(button?.disabled).toBe(true);
  });

  it('clears through the backend, not locally', async () => {
    const el = await fixture('queue-panel', { open: true });

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
  it('names where the queue came from, and navigates back to it', async () => {
    const el = await fixture('queue-panel', { open: true });

    setQueue([queueTrack(1, 'First')], 0, {
      type: 'playlist',
      id: 3,
      label: 'Road Trip',
    });
    await flush();
    await el.updateComplete;

    expect(text(el, '.queue-source')).toBe('Playing from Road Trip');

    let detail: unknown;
    el.addEventListener('navigate', (e) => {
      detail = (e as CustomEvent).detail;
    });

    shadow<HTMLElement>(el, '.queue-source')?.click();

    expect(detail).toEqual({
      view: 'playlist-details',
      playlistId: 3,
      playlistName: 'Road Trip',
    });
  });

  it('names a dynamic mix as text, not a dead link', async () => {
    const el = await fixture('queue-panel', { open: true });

    setQueue([queueTrack(1, 'First')], 0, {
      type: 'dynamicMix',
      id: 0,
      label: 'a dynamic mix',
    });
    await flush();
    await el.updateComplete;

    const sourceEl = shadow<HTMLElement>(el, '.queue-source');

    expect(sourceEl?.textContent).toBe('Playing from a dynamic mix');
    expect(sourceEl?.classList.contains('navigable')).toBe(false);

    let navigated = false;
    el.addEventListener('navigate', () => {
      navigated = true;
    });

    sourceEl?.click();

    expect(navigated).toBe(false);
  });

  it('keeps rendering rows after the virtualizer settles', async () => {
    const el = await fixture('queue-panel', { open: true });

    setQueue([queueTrack(1, 'First'), queueTrack(2, 'Second')], 0);
    await flush();
    await el.updateComplete;
    await new Promise((r) => {
      requestAnimationFrame(() => r(null));
    });

    expect(shadowAll(el, '[data-testid="queue-row"]').length).toBe(2);
  });
});

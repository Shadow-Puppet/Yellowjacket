/**
 * The full-screen now-playing view (plan 016 B2, phase 2).
 *
 * What is worth pinning here is not the layout but the *composition*:
 * it renders the same `<seek-bar>`, `<player-controls>` and
 * `<volume-control>` the desktop transport does, rather than its own.
 * A phone layout that reimplements the transport is a second transport
 * to fix every bug in — and the seek bar in particular carries
 * interpolation rules that took a plan of their own to get right.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/now-playing-view/now-playing-view';
import { Events } from '../../src/events';
import { emit, resetHarness, stub } from '@test/support/harness';
import { fixture, shadow, text } from '@test/support/render';
import type { TrackInfo } from '@store/player-store';

const TRACK: TrackInfo = {
  fileName: 'tideline.mp3',
  filePath: '/music/tideline.mp3',
  trackLength: 245,
  seekPosition: 0,
  state: 'playing',
  title: 'Tideline',
  artist: 'Sea Change',
  album: 'Ebb',
  coverArt: '/covers/ebb.jpg',
  coverArtSmall: '/covers/ebb_sm.jpg',
  coverArtMedium: '/covers/ebb_md.jpg',
  coverArtLarge: '/covers/ebb_lg.jpg',
  trackChangeId: 1,
  artistMbid: '',
  releaseGroupMbid: '',
  recordingMbid: '',
};

describe('now-playing-view', () => {
  beforeEach(() => {
    resetHarness();
  });

  it('reuses the real transport components', async () => {
    emit(Events.TrackChanged, TRACK);

    const el = await fixture('now-playing-view');

    for (const tag of ['seek-bar', 'player-controls', 'volume-control']) {
      expect(shadow(el, tag), `${tag} is not rendered`).not.toBeNull();
    }
  });

  it('shows the track, and the largest cover tier that is kept', async () => {
    emit(Events.TrackChanged, TRACK);

    const el = await fixture('now-playing-view');

    expect(text(el, '[data-testid="npv-title"]')).toBe('Tideline');

    // `saveCoverArt` records the largest *tier* as the path; there is
    // no full-resolution original on disk to reach for.
    expect(
      shadow<HTMLImageElement>(el, '[data-testid="npv-art"]')?.getAttribute('src'),
    ).toBe('/covers/ebb_lg.jpg');
  });

  it('says so when nothing is playing, rather than rendering an empty frame', async () => {
    // The player store is a singleton and outlives a test, so "no
    // track" has to be stated rather than assumed from a fresh mount.
    emit(Events.TrackChanged, null);

    const el = await fixture('now-playing-view');

    expect(shadow(el, '[data-testid="npv-empty"]')).not.toBeNull();
    expect(shadow(el, '[data-testid="npv-art"]')).toBeNull();

    // …and the way out is still there, which is the whole point of
    // rendering the header in both branches.
    expect(shadow(el, '[data-testid="npv-back"]')).not.toBeNull();
  });

  it('leaves by the nav stack, not by guessing where it came from', async () => {
    emit(Events.TrackChanged, TRACK);

    const el = await fixture('now-playing-view');
    let backs = 0;

    document.addEventListener('navigate-back', () => {
      backs += 1;
    });

    shadow<HTMLButtonElement>(el, '[data-testid="npv-back"]')?.click();

    // `navigate-back` pops what index.ts pushed. Dispatching a
    // `navigate` to a hardcoded view would strand anyone who arrived
    // here from a detail page.
    expect(backs).toBe(1);
  });

  it('gives the favourite button a target and a state', async () => {
    stub('playlist.Service.ToggleFavorite', undefined);
    emit(Events.TrackChanged, TRACK);

    const el = await fixture('now-playing-view');
    const fav = shadow<HTMLButtonElement>(el, '[data-testid="npv-favorite"]');

    expect(fav).not.toBeNull();
    expect(fav?.getAttribute('aria-pressed')).toBe('false');

    // A button that says only "heart" says nothing; the name carries
    // the track and the playlist it goes to.
    expect(fav?.getAttribute('aria-label')).toContain('Tideline');

    expect(fav!.getBoundingClientRect().height).toBeGreaterThanOrEqual(48);
  });

  it('gives the way out a thumb-sized target', async () => {
    emit(Events.TrackChanged, TRACK);

    const el = await fixture('now-playing-view');
    const back = shadow<HTMLButtonElement>(el, '[data-testid="npv-back"]');

    expect(back!.getBoundingClientRect().height).toBeGreaterThanOrEqual(48);
    expect(back?.getAttribute('aria-label')).toBe('Back');
  });
});

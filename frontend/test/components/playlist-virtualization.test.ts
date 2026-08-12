/**
 * `perf.M5`: the two playlist detail views rendered every track with a
 * plain `.map()`. Measured at 2 000 tracks: 22 090 elements retained in
 * the shadow root and 2 000 eager cover requests on open, against the
 * 487 and 0 a virtualizer costs.
 *
 * The magnitude belongs to `e2e/perf/measure.mjs` and a 50 000-track
 * library. These are the cheap guards for the *mechanism*, and in
 * particular for the one thing that broke while fixing it: the row
 * templates now live inside the virtualizer, so a host re-render alone
 * no longer repaints them. Selection went silently dead — the
 * controller held the right keys and no row ever showed it. Nothing but
 * a click in the real app caught that, which is precisely why it is
 * pinned here.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/playlist-details/playlist-details';
import '@components/smart-playlist-details/smart-playlist-details';
import { stub, flush, resetHarness } from '@test/support/harness';
import { fixture, shadowAll } from '@test/support/render';

const TRACKS = 500;

function tracks(n: number) {
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

/** Give the virtualizer a viewport; a zero-height host renders no rows. */
function sized(el: HTMLElement): void {
  el.style.display = 'block';
  el.style.height = '600px';
}

describe('<playlist-details> at length', () => {
  let el: LitElement;

  beforeEach(async () => {
    resetHarness();
    stub('playlist.Service.GetPlaylistTracks', tracks(TRACKS));
    stub('playlist.Service.GetAllPlaylists', []);

    el = await fixture<LitElement>('playlist-details', {
      playlistId: 1,
      playlistName: 'A playlist',
    });
    sized(el);
    await flush();
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));
  });

  it('renders through a virtualizer, not one row per track', () => {
    const rows = shadowAll(el, '.track-item');

    // A screenful and its overscan, not the playlist. The exact number
    // depends on the viewport, so the assertion is the *order of
    // magnitude* — 500 rows would be the bug.
    expect([
      shadowAll(el, 'lit-virtualizer').length,
      rows.length > 0,
      rows.length < TRACKS / 4,
    ]).toEqual([1, true, true]);
  });

  it('never asks for a cover it has not scrolled to', () => {
    const imgs = shadowAll<HTMLImageElement>(el, '.track-item img');

    expect(imgs.every((i) => i.getAttribute('loading') === 'lazy')).toBe(true);
  });

  it('asks for the small tier, not the original', () => {
    const img = shadowAll<HTMLImageElement>(el, '.track-item img')[0];

    expect(img?.getAttribute('src')).toBe('/covers/0_sm.jpg');
  });

  it('still shows a selection, though the rows are the virtualizer\u2019s', async () => {
    const row = shadowAll(el, '.track-item')[0]!;

    row.dispatchEvent(
      new MouseEvent('click', { bubbles: true, composed: true }),
    );
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));

    expect(
      shadowAll(el, '.track-item').filter((r) =>
        r.classList.contains('selected'),
      ).length,
    ).toBe(1);
  });
});

describe('<smart-playlist-details> at length', () => {
  let el: LitElement;

  beforeEach(async () => {
    resetHarness();
    stub('playlist.Service.GetSmartPlaylistTracks', tracks(TRACKS));
    stub('playlist.Service.GetSmartPlaylistRules', '{"rules":[]}');
    stub('playlist.Service.GetAllPlaylists', []);

    el = await fixture<LitElement>('smart-playlist-details', {
      playlistId: 1,
      playlistName: 'A smart playlist',
    });
    sized(el);
    await flush();
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));
  });

  it('renders through a virtualizer, not one row per track', () => {
    const rows = shadowAll(el, '.track-item');

    expect([
      shadowAll(el, 'lit-virtualizer').length,
      rows.length > 0,
      rows.length < TRACKS / 4,
    ]).toEqual([1, true, true]);
  });

  it('never asks for a cover it has not scrolled to', () => {
    const imgs = shadowAll<HTMLImageElement>(el, '.track-item img');

    expect(imgs.every((i) => i.getAttribute('loading') === 'lazy')).toBe(true);
  });
});

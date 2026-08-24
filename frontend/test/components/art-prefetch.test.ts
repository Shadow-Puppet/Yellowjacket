/**
 * The grids ask for the art below the fold before the card exists
 * (#65).
 *
 * Reported as "scrolling through albums, the art pops in". The cards
 * already draw the smallest adequate tier and are already
 * `loading="lazy"`, so what was left is *when*: `<lit-virtualizer>`
 * renders about 1000px past the viewport and the `<img>` — and
 * therefore the request — does not exist until it does. On the
 * reference device that is about two screens.
 *
 * These assert the mechanism, since no tier here can photograph a
 * pop-in: that the rows past the rendered range are requested, that
 * the request is for the same tier the card will draw, and that the
 * window has an end — an unbounded prefetch of a 5 000-album library
 * is the failure this trades against.
 *
 * What is *not* asserted here is that a rendered card was never
 * prefetched. It often was, honestly: the grid lays out more than once
 * on mount, so a row warmed by the first pass is drawn by the second,
 * which is the whole point. The rule that a single pass skips its own
 * rendered range is `image-prefetch.test.ts`'s, where one call can be
 * looked at on its own.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/cover-grid/cover-grid';
import '@components/artists-view/artists-view';
import { emit, stub, flush, resetHarness } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadowAll } from '@test/support/render';
import {
  PREFETCH_AHEAD,
  imagePrefetched,
  resetImagePrefetch,
} from '@utils/image-prefetch';

/** Enough albums that the virtualizer's own window is nowhere near the end. */
const ALBUMS = Array.from({ length: 400 }, (_, i) => {
  const n = String(i + 1).padStart(4, '0');

  return {
    ID: i + 1,
    Name: `Album ${n}`,
    ArtistName: 'Aurora Fields',
    Year: 2020,
    CoverArtPath: `/covers/${n}.jpg`,
    CoverArtSmall: `/covers/${n}_sm.jpg`,
    CoverArtMedium: `/covers/${n}_md.jpg`,
    CoverArtLarge: `/covers/${n}_lg.jpg`,
  };
});

const ARTISTS = Array.from({ length: 400 }, (_, i) => {
  const n = String(i + 1).padStart(4, '0');

  return {
    ID: i + 1,
    Name: `Artist ${n}`,
    AlbumCount: 2,
    TrackCount: 9,
    ImageSmall: `/artists/${n}_sm.jpg`,
    ImageMedium: `/artists/${n}_md.jpg`,
    ImageLarge: `/artists/${n}_lg.jpg`,
  };
});

/** Give the virtualizer a viewport; a zero-height host renders nothing. */
function sized(el: HTMLElement): void {
  el.style.display = 'block';
  el.style.height = '600px';
  el.style.width = '900px';
}

async function settle(el: LitElement): Promise<void> {
  await flush();
  await el.updateComplete;
  await new Promise((r) => setTimeout(r, 200));
}

/** The `src` of every card the grid actually rendered. */
function renderedSources(el: LitElement, selector: string): string[] {
  return shadowAll(el, selector)
    .map((img) => (img as HTMLImageElement).getAttribute('src') ?? '')
    .filter(Boolean);
}

/**
 * The last index the virtualizer has rendered, read off the cards
 * rather than counted: the rendered range is what the prefetch window
 * is measured from, and a count assumes it starts at 0 and has no
 * gaps.
 */
function lastRenderedIndex(el: LitElement, selector: string): number {
  const indices = shadowAll(el, selector).map((card) =>
    Number(card.getAttribute('data-index')),
  );

  return Math.max(...indices);
}

/**
 * The tier the cards chose, read off a rendered card rather than
 * recomputed — the point of the assertion is that the prefetch and the
 * card agree, so deriving both from the same ladder here would prove
 * nothing.
 */
function tierSuffix(src: string): string {
  const m = /_(sm|md|lg)\.jpg$/.exec(src);

  return m ? `_${m[1]}` : '';
}

beforeEach(() => {
  resetHarness();
  resetImagePrefetch();
  localStorage.clear();
  stub('library.Library.GetAlbums', ALBUMS);
  stub('library.Library.GetArtists', ARTISTS);
  stub('library.Library.GetTracks', []);
  stub('library.Library.GetGenres', []);
  emit(Events.LibraryScanComplete);
});

describe('the albums grid warms the covers below the fold', () => {
  it('asks for the covers past the rendered range, in the tier the card draws', async () => {
    const el = await fixture<LitElement>('cover-grid');

    sized(el);
    await settle(el);

    const rendered = renderedSources(el, 'img.cover-image');

    expect(rendered.length).toBeGreaterThan(0);

    const tier = tierSuffix(rendered[0]!);
    const url = (index: number) =>
      `/covers/${String(index + 1).padStart(4, '0')}${tier}.jpg`;

    // The grid starts at the top and never scrolls here, so the whole
    // window lies past the last card drawn.
    const last = lastRenderedIndex(el, '.album-card');

    expect(imagePrefetched(url(last + 1))).toBe(true);
    expect(imagePrefetched(url(last + PREFETCH_AHEAD))).toBe(true);
  });

  it('stops at the end of the window rather than warming the library', async () => {
    const el = await fixture<LitElement>('cover-grid');

    sized(el);
    await settle(el);

    const rendered = renderedSources(el, 'img.cover-image');
    const tier = tierSuffix(rendered[0]!);
    const url = (index: number) =>
      `/covers/${String(index + 1).padStart(4, '0')}${tier}.jpg`;

    // Not "exactly `last + PREFETCH_AHEAD`": the grid lays out more
    // than once on mount and each pass warms a window from wherever
    // the rendered range was then, so the reachable set is a few
    // windows wide. The property that matters is that it is a window
    // at all rather than the library.
    expect(imagePrefetched(url(399))).toBe(false);
    expect(window.__yjCacheStats?.()['imagePrefetch']?.entries ?? 0)
      .toBeLessThan(ALBUMS.length / 2);
  });
});

describe('the artists grid warms its avatars the same way', () => {
  it('asks for the avatars past the rendered range', async () => {
    const el = await fixture<LitElement>('artists-view');

    sized(el);
    await settle(el);

    const rendered = renderedSources(el, 'img.avatar-image');

    expect(rendered.length).toBeGreaterThan(0);

    const tier = tierSuffix(rendered[0]!);
    const last = lastRenderedIndex(el, '.artist-card');
    const url = (index: number) =>
      `/artists/${String(index + 1).padStart(4, '0')}${tier}.jpg`;

    expect(imagePrefetched(url(last + 1))).toBe(true);
    expect(imagePrefetched(url(399))).toBe(false);
  });
});

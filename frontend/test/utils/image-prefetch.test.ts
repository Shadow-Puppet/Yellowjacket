/**
 * What the grids ask for ahead of the scroll (#65).
 *
 * The virtualizer renders about 1000px past its viewport and nothing
 * else can be asked for, because the `<img>` does not exist until the
 * card does — two screens on the reference device, which is a fraction
 * of a second at speed. `prefetchImageWindow` issues the request
 * before the element, so the assertions here are about *which* rows
 * are asked for, that none is asked for twice, and that a request is
 * really made rather than merely recorded.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import {
  PREFETCH_MEMORY,
  imagePrefetched,
  prefetchImage,
  prefetchImageWindow,
  resetImagePrefetch,
} from '@utils/image-prefetch';

/** A hundred cards, each with its own cover URL. */
const CARDS = Array.from({ length: 100 }, (_, i) => ({ url: `/covers/${i}_sm.jpg` }));

const urlOf = (card: { url: string }) => card.url;

beforeEach(() => {
  resetImagePrefetch();
});

describe('warming the images a scroll is about to reach', () => {
  it('asks for the rows just past the rendered range, and no further', () => {
    const issued = prefetchImageWindow(CARDS, 40, 50, urlOf, 3);

    // Three past each edge: 51-53 and 37-39.
    expect(issued).toBe(6);
    expect(imagePrefetched('/covers/51_sm.jpg')).toBe(true);
    expect(imagePrefetched('/covers/53_sm.jpg')).toBe(true);
    expect(imagePrefetched('/covers/54_sm.jpg')).toBe(false);
    expect(imagePrefetched('/covers/39_sm.jpg')).toBe(true);
    expect(imagePrefetched('/covers/37_sm.jpg')).toBe(true);
    expect(imagePrefetched('/covers/36_sm.jpg')).toBe(false);
  });

  it('leaves the rendered rows alone — they have their own <img>', () => {
    prefetchImageWindow(CARDS, 40, 50, urlOf, 3);

    expect(imagePrefetched('/covers/45_sm.jpg')).toBe(false);
  });

  it('asks for nothing twice, so a scroll back over the same rows is free', () => {
    prefetchImageWindow(CARDS, 40, 50, urlOf, 3);

    expect(prefetchImageWindow(CARDS, 40, 50, urlOf, 3)).toBe(0);
  });

  it('clamps at both ends of the list', () => {
    // At the top of a five-item list nothing precedes the range, and
    // the tail runs out after two.
    expect(prefetchImageWindow(CARDS.slice(0, 5), 0, 2, urlOf, 10)).toBe(2);
  });

  it('asks for nothing when the virtualizer reports an empty range', () => {
    // `visibilityChanged` reports -1/-1 before anything is laid out.
    expect(prefetchImageWindow(CARDS, -1, -1, urlOf)).toBe(0);
  });

  it('skips a card that draws a placeholder rather than an image', () => {
    expect(prefetchImageWindow(CARDS, 40, 50, () => '', 3)).toBe(0);
  });

  it('really issues the request, rather than only recording it', async () => {
    // A served file, so the load succeeds and the resource timing entry
    // is unambiguous; the query string keeps it distinct per run.
    const url = `/test/support/pixel.svg?prefetch=${Date.now()}`;
    const href = new URL(url, location.href).href;

    expect(prefetchImage(url)).toBe(true);

    for (let i = 0; i < 100; i++) {
      if (performance.getEntriesByName(href).length > 0) break;

      await new Promise((r) => setTimeout(r, 20));
    }

    expect(performance.getEntriesByName(href)).toHaveLength(1);
    expect(prefetchImage(url)).toBe(false);
    expect(performance.getEntriesByName(href)).toHaveLength(1);
  });

  it('reports what it is holding, with its cap, to the cache stats', () => {
    prefetchImageWindow(CARDS, 40, 50, urlOf, 3);

    const stat = window.__yjCacheStats?.()['imagePrefetch'];

    expect(stat).toBeTruthy();
    expect(stat!.entries).toBe(6);
    expect(stat!.limit).toBe(PREFETCH_MEMORY);
    // It holds URLs, not images — the bytes are the browser's cache.
    expect(stat!.chars).toBe(6 * '/covers/51_sm.jpg'.length);
  });

  it('keeps its record bounded, so a 50 000-album scroll cannot grow it', () => {
    const many = Array.from(
      { length: PREFETCH_MEMORY * 2 },
      (_, i) => ({ url: `/covers/bulk-${i}_sm.jpg` }),
    );

    prefetchImageWindow(many, 0, 0, urlOf, many.length);

    expect(window.__yjCacheStats?.()['imagePrefetch']?.entries).toBe(PREFETCH_MEMORY);
  });
});

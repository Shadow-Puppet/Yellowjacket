/**
 * Warm the browser's image cache for the cards a scroll is about to
 * reach.
 *
 * #65: album art pops in while scrolling. The rule this app already
 * follows is that a row image is `loading="lazy" decoding="async"` and
 * draws the smallest adequate tier, and both halves are in place —
 * `cover-grid.getCoverUrl()` and `artists-view`'s avatar both pick
 * `_sm`/`_md`/`_lg` from the card size and the device pixel ratio. What
 * is left is *when* the fetch starts: the grids are virtualized, so the
 * `<img>` does not exist at all until the virtualizer decides to render
 * its card, and only then can the browser ask for anything.
 *
 * The issue's Direction asks for a larger overscan, and that is not
 * available: `@lit-labs/virtualizer`'s `_overhang` is a hard-coded
 * 1000px `protected` field on `BaseLayout` with no configuration
 * surface, so raising it means monkey-patching a private. 1000px is
 * about two screens on the reference device's 439px viewport, which is
 * a fraction of a second at speed.
 *
 * So the request is issued ahead of the element instead. Cover art and
 * artist images are plain URLs served by `coverart.Handler` /
 * `explore`'s image handler under `Cache-Control: public,
 * max-age=31536000, immutable` — the filenames are content hashes — so
 * a prefetched image is a cache hit by the time the card is drawn, and
 * a second pass over the same rows costs nothing at all.
 *
 * Three things about it are load-bearing.
 *
 * **This is not the `LRUMap` path the issue's Findings warn about.**
 * That ceiling (`ARTIST_IMAGE_CACHE_LIMIT` and friends) bounds
 * Explore's base64 data URLs, which are held in JS. A library cover is
 * a URL, and what retains the bytes is the browser's own HTTP cache,
 * which evicts on its own terms. What this module retains is the *set
 * of URLs already asked for*, which is why that set has a cap and
 * reports itself to `window.__yjCacheStats()` — the measurement the
 * issue asks for.
 *
 * **A window is warmed on both sides of the rendered range.** The
 * event carries no direction, and scrolling back up needs the same
 * treatment; the rows behind are already in `requested` from the pass
 * that rendered them, so the backward half issues nothing in the
 * common case and is free.
 *
 * **An in-flight image is held.** `new Image().src = url` and drop it
 * is the usual idiom and usually survives, but "usually" is an engine
 * detail and the engine that matters here is a two-year-old WebView.
 * The element is kept until it loads or fails, and no longer — nothing
 * here holds a decoded bitmap on purpose.
 */

import { registerCacheProbe } from './cache-stats.js';
import { LRUMap } from './lru-map.js';

/**
 * How many entries past each edge of the rendered range to warm.
 *
 * Entries rather than pixels, because that is what the event reports
 * and what the caller has an array of. Twelve rows on the phone's
 * two-column grid and four on a desktop's six, on top of the
 * virtualizer's own 1000px — enough to cover a flick, and bounded so a
 * fast scroll through 5 000 albums cannot ask for 5 000 covers.
 */
export const PREFETCH_AHEAD = 24;

/** Ceiling on the record of what has already been asked for. */
export const PREFETCH_MEMORY = 512;

/** URLs already requested; the value is a placeholder, the key is the record. */
const requested = new LRUMap<string, true>(PREFETCH_MEMORY);

/** Images still loading, held so the request cannot be collected. */
const inFlight = new Set<HTMLImageElement>();

registerCacheProbe('imagePrefetch', () => {
    let chars = 0;

    for (const url of requested.keys()) chars += url.length;

    return { entries: requested.size, chars, limit: PREFETCH_MEMORY };
});

/** Whether this URL has already been asked for. */
export function imagePrefetched(url: string): boolean {
    return requested.has(url);
}

/**
 * Ask the browser for `url` unless it has already been asked for.
 * Returns whether a request was issued.
 */
export function prefetchImage(url: string): boolean {
    if (!url || requested.has(url)) return false;

    requested.set(url, true);

    const img = new Image();

    inFlight.add(img);

    const done = () => {
        inFlight.delete(img);
    };

    img.addEventListener('load', done, { once: true });
    img.addEventListener('error', done, { once: true });
    img.decoding = 'async';
    img.src = url;

    return true;
}

/**
 * Warm the images either side of a virtualizer's rendered range.
 *
 * `first`/`last` are the indices the `visibilityChanged` event
 * reported; `urlOf` returns the image the card at that index will
 * draw, or `''` where it draws a placeholder. Returns how many
 * requests were issued, which is what a test can assert on and what
 * makes "a second run does approximately nothing" checkable.
 */
export function prefetchImageWindow<T>(
    items: readonly T[],
    first: number,
    last: number,
    urlOf: (item: T) => string,
    ahead: number = PREFETCH_AHEAD,
): number {
    if (items.length === 0 || first < 0 || last < first) return 0;

    const from = Math.max(0, first - ahead);
    const to = Math.min(items.length - 1, last + ahead);
    let issued = 0;

    // Forward first: it is the direction a scroll is usually going, so
    // it is the half that has to win the race.
    for (let i = last + 1; i <= to; i++) {
        const item = items[i];

        if (item !== undefined && prefetchImage(urlOf(item))) issued++;
    }

    for (let i = from; i < first; i++) {
        const item = items[i];

        if (item !== undefined && prefetchImage(urlOf(item))) issued++;
    }

    return issued;
}

/** Forget what has been asked for. For tests; the app never needs it. */
export function resetImagePrefetch(): void {
    requested.clear();
    inFlight.clear();
}

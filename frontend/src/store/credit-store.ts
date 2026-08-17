/**
 * Multi-artist credits, keyed by recording MBID.
 *
 * A credit is ordered parts and the credit *string* is derived from
 * them.  This store holds the parts for entities that have more than
 * one credited artist; everything else renders the single link it
 * always did.
 *
 * Three things about it are load-bearing.
 *
 * **Absence is an answer, and it is cached as one.**  The backend
 * returns nothing for a single-artist credit, which is the common case
 * by a wide margin — measured on a real library, 13% of tracks are
 * multi-artist.  Caching only the hits would re-request the other 87%
 * on every render, forever, which is the same shape as the bug that
 * made `explore-album-details` ask the backend on hover.  A miss is
 * stored as an empty array: *asked*, not *answered*.
 *
 * **The lookup is batched, and coalesced across callers.**  Every row
 * of every tracklist asks this question, and one IPC round trip per row
 * is how a 5,000-row list becomes unusable.  A virtualized list cannot
 * hand over "the whole list" either — 50,000 rows is 100 queries for
 * the ~30 on screen.  So `request()` is per-row and cheap: it collects
 * into a pending set and flushes once on the next frame, which turns a
 * screenful of rows into exactly one call.  `ensure()` remains for a
 * caller that genuinely has a bounded list in hand.
 *
 * **It is bounded.**  A cache that grows with use is a leak with a
 * schedule; a browsing afternoon touches far more credits than a
 * screenful.  The cap is entries rather than bytes because a credit is
 * a handful of short strings, unlike the art caches next door.
 */

import { GetCredits } from '@go/explore/service.js';
import type { CreditPart } from '../utils/explore-link';
import { LRUMap } from '../utils/lru-map';
import { compact } from '../utils/binding';
import { registerCacheProbe } from '../utils/cache-stats';

/**
 * Entries retained.  A credit is ~4 short strings, so this is well
 * under a megabyte — sized to comfortably exceed any single list the
 * app renders, because a cap below the visible count evicts rows that
 * are still on screen and the re-render fetches them straight back.
 */
export const CREDIT_CACHE_LIMIT = 20_000;

/** An empty parts array is the negative marker: asked, no decomposition. */
type CachedParts = readonly CreditPart[];

class CreditStore {
    private cache = new LRUMap<string, CachedParts>(CREDIT_CACHE_LIMIT);

    /** MBIDs with a request in flight, so a re-render does not refetch. */
    private inFlight = new Set<string>();

    private listeners = new Set<() => void>();

    /** Collected by request(), flushed as one batch on the next frame. */
    private pending = new Set<string>();

    private flushHandle: number | null = null;

    constructor() {
        registerCacheProbe('credits', () => ({
            entries: this.cache.size,
            chars: this.retainedChars(),
            limit: CREDIT_CACHE_LIMIT,
        }));
    }

    /**
     * The strings actually retained, counted rather than estimated —
     * a bound that is only checkable against a guess is not checkable.
     */
    private retainedChars(): number {
        let total = 0;

        for (const parts of this.cache.values()) {
            for (const part of parts) {
                total +=
                    part.creditedName.length +
                    part.joinPhrase.length +
                    part.artistMbid.length;
            }
        }

        return total;
    }

    /**
     * Subscribe to "some credits arrived".
     *
     * Deliberately not per-MBID: a list fetches its rows in one call and
     * re-renders once, so a fine-grained signal would buy nothing and
     * cost a listener per row.
     */
    subscribe(fn: () => void): () => void {
        this.listeners.add(fn);

        return () => this.listeners.delete(fn);
    }

    /**
     * The parts for one entity, or undefined when it has not been asked
     * about yet.
     *
     * An entity with a single-artist credit returns an empty array, and
     * `creditLink` treats fewer than two parts as the fallback — so a
     * caller does not have to distinguish "not asked" from "one artist"
     * to render correctly, only to decide whether to ask.
     */
    get(mbid: string | undefined): readonly CreditPart[] | undefined {
        if (!mbid) return undefined;

        return this.cache.get(mbid);
    }

    /**
     * Ask about one entity, joining whatever batch is forming.
     *
     * Safe to call from a render: it is a set insert and a scheduled
     * flush, and an entity already cached or in flight is dropped.  The
     * loop it looks like it might cause does not happen — after a flush
     * every requested MBID is cached, so the re-render's requests are
     * all dropped and nothing notifies again.
     */
    request(mbid: string | undefined): void {
        if (!mbid) return;
        if (this.cache.has(mbid)) return;
        if (this.inFlight.has(mbid)) return;
        if (this.pending.has(mbid)) return;

        this.pending.add(mbid);

        if (this.flushHandle !== null) return;

        // A frame, not a microtask: the point is to collect every row a
        // virtualizer renders in this pass, and those happen across the
        // whole update, not within one microtask checkpoint.
        this.flushHandle = requestAnimationFrame(() => {
            this.flushHandle = null;

            const batch = [...this.pending];

            this.pending.clear();

            void this.ensure(batch);
        });
    }

    /**
     * Ask and read in one call, for use inside a template.
     *
     * A getter with a side effect, deliberately: the alternative is
     * every call site writing `request(x)` beside `get(x)` and one of
     * them eventually forgetting, which renders a permanently
     * single-artist credit that looks exactly like an entity with one
     * artist.  Making the request the same act as the read is what
     * stops the two drifting apart.
     */
    credits(mbid: string | undefined): readonly CreditPart[] | undefined {
        this.request(mbid);

        return this.get(mbid);
    }

    /**
     * Fetch the credits for a list, skipping anything already known or
     * already being fetched.
     *
     * `has` rather than `get` for the membership test: probing must not
     * mark an entry recently-used, or scrolling past a row would keep
     * it alive ahead of one actually being rendered.
     */
    async ensure(mbids: readonly (string | undefined)[]): Promise<void> {
        const wanted = new Set<string>();

        for (const mbid of mbids) {
            if (!mbid) continue;
            if (this.cache.has(mbid)) continue;
            if (this.inFlight.has(mbid)) continue;

            wanted.add(mbid);
        }

        if (wanted.size === 0) return;

        const batch = [...wanted];

        for (const mbid of batch) this.inFlight.add(mbid);

        try {
            const found = compact(await GetCredits(batch));

            for (const mbid of batch) {
                // Every MBID asked for gets an entry, present or not:
                // the absent ones are the answer "one artist", and not
                // recording that is what would re-ask forever.
                this.cache.set(mbid, found[mbid] ?? []);
            }

            this.notify();
        } catch (err) {
            // A credit is an enrichment: without it every name renders
            // as the single link it did before, which is a worse answer
            // rather than a broken one.  Nothing user-facing is worth
            // interrupting for, so this stays in the console.
            console.error('Failed to load artist credits', err);
        } finally {
            for (const mbid of batch) this.inFlight.delete(mbid);
        }
    }

    /** Drop everything. The tags on disk changed, so credits may have. */
    invalidate(): void {
        this.cache = new LRUMap<string, CachedParts>(CREDIT_CACHE_LIMIT);
        this.notify();
    }

    private notify(): void {
        for (const fn of this.listeners) fn();
    }
}

export const creditStore = new CreditStore();

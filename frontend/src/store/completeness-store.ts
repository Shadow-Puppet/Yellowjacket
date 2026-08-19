/**
 * How much of an album is here, keyed by local album id.
 *
 * The album page has always been able to say "9 of 12"; a card could
 * not, so an album held two tracks of ten wore a plain green tick on
 * every grid in the app — which is the complaint the badge-accuracy
 * issue was filed about, one surface over. A ring with a count needs a
 * numerator and a denominator per album, and one `GetAlbumCompleteness`
 * per card is fifty round trips for a grid of fifty.
 *
 * So this is `credit-store` one question over, and for the same three
 * reasons.
 *
 * **The lookup is batched and coalesced across callers.** `request()`
 * is per-card, cheap and safe to call from a render: it collects into a
 * pending set and flushes once on the next frame, which turns a
 * screenful into exactly one `GetAlbumsCompleteness`. A frame rather
 * than a microtask because the point is to collect every card an update
 * pass renders, and those do not land inside one microtask checkpoint.
 *
 * **Absence is cached as an answer.** An album with no files at all is
 * omitted by the backend deliberately — "I have none of this" and "I
 * have no idea" are the third state `known` exists to keep apart — so a
 * miss stored as a hit would re-ask about it forever. What is cached is
 * *asked*, and the unknown answer is a first-class value.
 *
 * **It is invalidated rather than aged.** Completeness is a fact about
 * files on disk, and the two events that change it — a scan finishing
 * and tags being rewritten — are exactly the ones `library-store`
 * already discards everything on. `TracksRemovedFromLibrary` is the
 * third: it changes a numerator without a rescan.
 */

import { GetAlbumsCompleteness } from '@go/library/library.js';
import type * as library from '@go/library/models.js';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../events';
import { LRUMap } from '../utils/lru-map';
import { compact } from '../utils/binding';
import { registerCacheProbe } from '../utils/cache-stats';

/**
 * Entries retained. Four small numbers each, so the bound is about
 * unbounded growth rather than bytes — and it is set well above any one
 * grid, since a cap below the visible count evicts cards that are still
 * on screen and the re-render fetches them straight back.
 */
export const COMPLETENESS_CACHE_LIMIT = 20_000;

/** What the app asks of a cached entry. */
export type Completeness = library.AlbumCompleteness;

/**
 * The answer for an album nobody has a file of.
 *
 * Not a zero-of-zero: `known` false is what every consumer already
 * reads as "say nothing", and it is the same value the album page's
 * `completenessAnswer()` treats as unknowable.
 */
const NOTHING_HERE: Completeness = {
    owned: 0,
    expected: 0,
    known: false,
    complete: false,
} as Completeness;

class CompletenessStore {
    private cache = new LRUMap<number, Completeness>(COMPLETENESS_CACHE_LIMIT);

    /** Ids with a request in flight, so a re-render does not refetch. */
    private inFlight = new Set<number>();

    private listeners = new Set<() => void>();

    /** Collected by request(), flushed as one batch on the next frame. */
    private pending = new Set<number>();

    private flushHandle: number | null = null;

    constructor() {
        registerCacheProbe('albumCompleteness', () => ({
            entries: this.cache.size,
            chars: this.cache.size * 4,
            limit: COMPLETENESS_CACHE_LIMIT,
        }));

        EventsOn(Events.LibraryScanComplete, () => this.invalidate());
        EventsOn(Events.TrackMetadataChanged, () => this.invalidate());
        EventsOn(Events.TracksRemovedFromLibrary, () => this.invalidate());
    }

    /**
     * Subscribe to "some answers arrived".
     *
     * Deliberately not per-album, for `credit-store`'s reason: a grid
     * fetches its cards in one call and re-renders once, so a
     * fine-grained signal would buy nothing and cost a listener a card.
     */
    subscribe(fn: () => void): () => void {
        this.listeners.add(fn);

        return () => this.listeners.delete(fn);
    }

    /** The answer for one album, or undefined until it has been asked. */
    get(albumID: number | undefined | null): Completeness | undefined {
        if (!albumID || albumID <= 0) return undefined;

        return this.cache.get(albumID);
    }

    /**
     * Ask about one album, joining whatever batch is forming.
     *
     * Safe from inside a render: a set insert and a scheduled flush,
     * with anything cached or in flight dropped. It does not loop —
     * after a flush every id asked for is cached, so the re-render's
     * requests are all dropped and nothing notifies again.
     */
    request(albumID: number | undefined | null): void {
        if (!albumID || albumID <= 0) return;
        if (this.cache.has(albumID)) return;
        if (this.inFlight.has(albumID)) return;
        if (this.pending.has(albumID)) return;

        this.pending.add(albumID);

        if (this.flushHandle !== null) return;

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
     * A getter with a side effect, deliberately — `credit-store` makes
     * the same trade and for the same reason: the alternative is every
     * call site writing `request(x)` beside `get(x)` and one of them
     * eventually forgetting, which renders a permanently unknown
     * completeness that looks exactly like an album with no totals.
     */
    completeness(albumID: number | undefined | null): Completeness | undefined {
        this.request(albumID);

        return this.get(albumID);
    }

    /** Fetch for a list, skipping anything known or already in flight. */
    async ensure(albumIDs: readonly number[]): Promise<void> {
        const wanted = new Set<number>();

        for (const id of albumIDs) {
            if (!id || id <= 0) continue;
            // `has` rather than `get`: probing must not mark an entry
            // recently-used, or scrolling past a card would keep it
            // alive ahead of one actually being rendered.
            if (this.cache.has(id)) continue;
            if (this.inFlight.has(id)) continue;

            wanted.add(id);
        }

        if (wanted.size === 0) return;

        const batch = [...wanted];

        for (const id of batch) this.inFlight.add(id);

        try {
            const found = compact(await GetAlbumsCompleteness(batch));

            for (const id of batch) {
                this.cache.set(id, found[String(id)] ?? NOTHING_HERE);
            }

            this.notify();
        } catch (err) {
            // A count is an enrichment: without it a card shows the
            // plain "you have this", which is what it showed before and
            // is a weaker answer rather than a broken one.
            console.error('Failed to load album completeness', err);
        } finally {
            for (const id of batch) this.inFlight.delete(id);
        }
    }

    /** Drop everything: the files on disk changed. */
    invalidate(): void {
        this.cache = new LRUMap<number, Completeness>(
            COMPLETENESS_CACHE_LIMIT,
        );
        this.notify();
    }

    private notify(): void {
        for (const fn of this.listeners) fn();
    }
}

export const completenessStore = new CompletenessStore();

/**
 * A `Map` with a ceiling.
 *
 * `perf.M7`/`M8`: the Explore caches were never evicted, and Explore is
 * a cached primary view that never unmounts — so a desktop player left
 * open for days grew monotonically.  Measured at twelve searches:
 * **+8.48 MB of retained heap**, climbing 0.7 MB per search with no
 * sign of levelling off, because a cover thumbnail is a ~27 kB base64
 * data URL and an artist photo is a ~128 kB one.
 *
 * JS `Map` already iterates in insertion order, so the whole LRU is:
 * re-insert on read, and drop from the front when over the cap.  That
 * is deliberately all this is — a dependency, or a generic cache with
 * TTLs and weak refs, would be more machinery than the two call sites
 * justify.
 *
 * One rule for callers, learned by measuring: **two caches holding the
 * same string must have the same cap.** `artistImageCache` and
 * `exploreCache.artists` both hold the artist photo's data URL, so
 * bounding either one alone frees nothing at all — the other still
 * pins every string. A bound is only a bound if it covers every
 * reference.
 */
export class LRUMap<K, V> {
    private map = new Map<K, V>();

    constructor(readonly limit: number) {
        if (limit < 1) throw new Error('LRUMap: limit must be at least 1');
    }

    get size(): number {
        return this.map.size;
    }

    /** Read, and mark the entry most-recently-used. */
    get(key: K): V | undefined {
        if (!this.map.has(key)) return undefined;

        const value = this.map.get(key) as V;
        // Re-insertion moves it to the end of the iteration order, which
        // is what makes the front of the map the eviction candidate.
        this.map.delete(key);
        this.map.set(key, value);

        return value;
    }

    /**
     * Membership, *without* marking the entry used.
     *
     * Both Explore caches store `''` to mean "already attempted, no art"
     * — a negative marker that also prevents a duplicate in-flight
     * fetch. Those probes should not keep a dead entry alive ahead of
     * one that is actually being rendered.
     */
    has(key: K): boolean {
        return this.map.has(key);
    }

    set(key: K, value: V): this {
        this.map.delete(key);
        this.map.set(key, value);

        while (this.map.size > this.limit) {
            const oldest = this.map.keys().next();

            if (oldest.done) break;

            this.map.delete(oldest.value);
        }

        return this;
    }

    delete(key: K): boolean {
        return this.map.delete(key);
    }

    clear(): void {
        this.map.clear();
    }

    values(): IterableIterator<V> {
        return this.map.values();
    }

    keys(): IterableIterator<K> {
        return this.map.keys();
    }

    entries(): IterableIterator<[K, V]> {
        return this.map.entries();
    }

    [Symbol.iterator](): IterableIterator<[K, V]> {
        return this.map[Symbol.iterator]();
    }
}

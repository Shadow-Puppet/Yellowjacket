/**
 * A registry of the app's in-memory caches, for measurement.
 *
 * `perf.M7`/`M8` are retention findings, and retention is only ever
 * credible as a number: the two Explore caches did not show as heap
 * growth in two separate sessions of a ten-view browse script, because
 * that script *visits* Explore and never types in it, so the caches
 * stayed empty.  What made them real was a session that searched twelve
 * times and watched the heap climb 9 MB monotonically.
 *
 * The lesson is that a bound needs a way to be *checked*, not just
 * written — otherwise the next session has to rediscover the same
 * reproduction before it can tell whether the LRU still holds.  A cache
 * registers itself here and `window.__yjCacheStats()` reports every one
 * in a single eval, which is what `e2e/perf/measure.mjs` reads.
 *
 * This follows `src/icons/index.ts`'s `__yjIconMisses`: an unconditional
 * measurement surface, costing one function and one Map, rather than a
 * dev-only build branch that is therefore never exercised in the build
 * that ships.
 */

/** What one cache reports about itself. */
export interface CacheStat {
    /** Number of live entries. */
    entries: number;
    /** Total length of the strings retained, where the cache holds strings. */
    chars: number;
    /** The cap, so a reading can be read against its bound. */
    limit: number;
}

const probes = new Map<string, () => CacheStat>();

declare global {
    interface Window {
        __yjCacheStats?: () => Record<string, CacheStat>;
    }
}

/**
 * Register a cache under a stable name.  Re-registering the same name
 * replaces the probe, which is what a cached view remounting in a test
 * needs.
 */
export function registerCacheProbe(name: string, probe: () => CacheStat): void {
    probes.set(name, probe);
}

/** Drop a probe — a per-instance cache going away with its host. */
export function unregisterCacheProbe(name: string): void {
    probes.delete(name);
}

if (typeof window !== 'undefined') {
    window.__yjCacheStats = () => {
        const out: Record<string, CacheStat> = {};

        for (const [name, probe] of probes) {
            try {
                out[name] = probe();
            } catch {
                // A probe must never be able to break a measurement run.
            }
        }

        return out;
    };
}

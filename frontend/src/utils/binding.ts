/**
 * The boundary between a Go return value and the app's own types.
 *
 * v2's binding generator typed a `[]T` return as `T[]`, which was a
 * lie: a nil slice marshals to JSON `null`, and every list in this app
 * has always been able to arrive that way.  v3 types it honestly as
 * `T[] | null`, which surfaced ~50 sites that were relying on the lie.
 *
 * The app's contract is the one it has always behaved as if it had —
 * *an absent list is an empty list* — so it is stated once here rather
 * than as `?? []` at every call site, and stated at the only place it
 * is true: the moment a value crosses from Go.
 *
 * These helpers also return a plain `Promise`.  v3 bindings return a
 * `CancellablePromise`, and nothing in this app cancels one; letting
 * that type leak inward would put a Wails type in the signature of
 * every store method for a capability none of them use.
 */

/**
 * list awaits a binding returning a Go slice and yields `[]` for nil.
 */
export async function list<T>(
    request: PromiseLike<T[] | null>,
): Promise<T[]> {
    return (await request) ?? [];
}

/**
 * dict awaits a binding returning a Go map and yields `{}` for nil.
 *
 * Two shapes of the same lie are undone here.  The generator types a
 * `map[int64]T` with a template-literal key (`` `${number}` ``), which
 * cannot be indexed by a `number` even though every such key is one;
 * and it types each value as nullable, because a map of slices can
 * hold a nil one.  A null-valued key is dropped rather than kept,
 * which loses nothing: `noUncheckedIndexedAccess` already makes every
 * read `V | undefined`, so an absent key and a nil value are
 * indistinguishable to every consumer.
 */
export async function dict<V>(
    request: PromiseLike<Record<string, V | null | undefined> | null>,
): Promise<Record<number, V>> {
    const raw = (await request) ?? {};
    const out: Record<number, V> = {};

    for (const [key, val] of Object.entries(raw)) {
        if (val != null) out[Number(key)] = val;
    }

    return out;
}

/**
 * dictByName is dict for a Go map keyed by something that is already a
 * string — an MBID, a file path, a genre name.
 */
export async function dictByName<V>(
    request: PromiseLike<Record<string, V | null | undefined> | null>,
): Promise<Record<string, V>> {
    return compact(await request);
}

/**
 * compact is dictByName for a map that arrived as a *field* rather
 * than as a return value — a nested `map[string]string`, which the
 * generator types with optional values because a JSON object need not
 * carry every key.
 */
export function compact<V>(
    map: Record<string, V | null | undefined> | null | undefined,
): Record<string, V> {
    const out: Record<string, V> = {};

    for (const [key, val] of Object.entries(map ?? {})) {
        if (val != null) out[key] = val;
    }

    return out;
}

/**
 * value awaits a binding whose result is used as-is, dropping only the
 * cancellation the app never asks for.
 */
export async function value<T>(request: PromiseLike<T>): Promise<T> {
    return await request;
}

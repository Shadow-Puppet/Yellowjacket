// Shared helpers for grouping a tracklist by disc number — used by
// both the explore album view and the autotag review UI so the
// rendering rules stay consistent (single-disc albums skip the
// "Disc 1" separator, multi-disc albums show one per disc).

export interface Disced {
    discNumber?: number;
    position?: number;
}

/**
 * Returns true when any track has a discNumber > 1.  A list with
 * only disc 1 (or no discNumber set) renders without "Disc N"
 * headers.
 */
export function isMultiDisc<T extends Disced>(tracks: T[]): boolean {
    return tracks.some((t) => (t.discNumber ?? 1) > 1);
}

/**
 * Group tracks by disc number, sorting tracks within each disc
 * by position.  Discs with no number default to disc 1, which
 * matches MusicBrainz behaviour for releases that omit the field.
 */
export function groupByDisc<T extends Disced>(tracks: T[]): Map<number, T[]> {
    const discMap = new Map<number, T[]>();

    for (const track of tracks) {
        const disc = track.discNumber ?? 1;
        const bucket = discMap.get(disc);
        if (bucket) {
            bucket.push(track);
        } else {
            discMap.set(disc, [track]);
        }
    }

    for (const bucket of discMap.values()) {
        bucket.sort((a, b) => (a.position ?? 0) - (b.position ?? 0));
    }

    return discMap;
}

/** Returns disc numbers in ascending order from a grouped map. */
export function discNumbers<T>(discMap: Map<number, T[]>): number[] {
    return [...discMap.keys()].sort((a, b) => a - b);
}

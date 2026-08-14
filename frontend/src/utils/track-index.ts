/**
 * A `FilePath → Track` lookup over an array the store owns.
 *
 * Five components turn a list of selected file paths back into tracks
 * with `filePaths.map(fp => tracks.find(t => t.FilePath === fp))`
 * (audit `perf.m6`). That is O(selection × total), and at 50 000
 * tracks "Select all → Edit tags" blocked the main thread for **six
 * seconds** — measured, through the real opener, before this existed.
 *
 * The cache is a `WeakMap` keyed on the **identity of the array**,
 * which is the signal this app already uses for exactly this: the
 * stores replace the array when its contents change and share every
 * unchanged member (see `library-store`'s `TrackPlayCountChanged`
 * patch, and `track-list`'s memoized filter/sort caches). So a stale
 * map is not reachable — a changed list is a different array and gets
 * a different map — and an array nobody holds any more takes its map
 * with it.
 *
 * Build cost is one O(total) pass, paid on the first lookup against a
 * given array and never again.
 */

import type * as library from '@go/library/models.js';

const byArray = new WeakMap<
    readonly library.Track[],
    Map<string, library.Track>
>();

/** The lookup for `tracks`, built once per array identity. */
export function tracksByFilePath(
    tracks: readonly library.Track[],
): Map<string, library.Track> {
    let map = byArray.get(tracks);

    if (map) return map;

    map = new Map<string, library.Track>();

    for (const track of tracks) {
        // First wins: a duplicate path would be the same file, and
        // `find` returned the first too.
        if (!map.has(track.FilePath)) map.set(track.FilePath, track);
    }

    byArray.set(tracks, map);

    return map;
}

/** Resolve file paths to tracks, dropping any that are not present. */
export function tracksForPaths(
    tracks: readonly library.Track[],
    filePaths: readonly string[],
): library.Track[] {
    const byPath = tracksByFilePath(tracks);
    const result: library.Track[] = [];

    for (const filePath of filePaths) {
        const track = byPath.get(filePath);

        if (track) result.push(track);
    }

    return result;
}

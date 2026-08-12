/**
 * `tracksByFilePath` is a cache keyed on an array's identity, which is
 * only safe because the stores replace the array whenever its contents
 * change. These tests pin both halves of that: the cache is reused for
 * the same array, and a new array gets a new map.
 *
 * The reason it exists is `perf.m6`: five components resolved selected
 * file paths back to tracks with `filePaths.map(fp => tracks.find(…))`,
 * O(selection × total). Measured through the real opener at 50 000
 * tracks, "Select all → Edit tags" blocked the main thread for
 * 3.0–6.3 s; with the map, 68 ms.
 */
import { describe, expect, it } from 'vitest';

import { tracksByFilePath, tracksForPaths } from '@utils/track-index';

type Track = { FilePath: string; Title: string };

const track = (n: number): Track => ({
    FilePath: `/music/${n}.mp3`,
    Title: `Track ${n}`,
});

// The util is typed against the generated `library.Track`; these
// fixtures carry only the fields it reads.
const asTracks = (t: Track[]) => t as unknown as Parameters<
    typeof tracksByFilePath
>[0];

describe('tracksByFilePath', () => {
    it('indexes by file path', () => {
        const tracks = asTracks([track(1), track(2), track(3)]);
        const map = tracksByFilePath(tracks);

        expect(map.size).toBe(3);
        expect(map.get('/music/2.mp3')).toBe(tracks[1]);
        expect(map.get('/music/nope.mp3')).toBeUndefined();
    });

    it('reuses the map for the same array', () => {
        const tracks = asTracks([track(1), track(2)]);

        expect(tracksByFilePath(tracks)).toBe(tracksByFilePath(tracks));
    });

    it('builds a fresh map for a replaced array', () => {
        // The store shares unchanged members and replaces the array,
        // so identity is the invalidation signal.
        const first = asTracks([track(1)]);
        const second = asTracks([track(1), track(2)]);

        expect(tracksByFilePath(second)).not.toBe(tracksByFilePath(first));
        expect(tracksByFilePath(second).size).toBe(2);
    });

    it('keeps the first of a duplicated path, as find() did', () => {
        const a = { FilePath: '/music/1.mp3', Title: 'first' };
        const b = { FilePath: '/music/1.mp3', Title: 'second' };

        expect(tracksByFilePath(asTracks([a, b])).get('/music/1.mp3'))
            .toBe(a);
    });
});

describe('tracksForPaths', () => {
    it('resolves in the order asked for, not list order', () => {
        const tracks = asTracks([track(1), track(2), track(3)]);
        const got = tracksForPaths(
            tracks,
            ['/music/3.mp3', '/music/1.mp3'],
        );

        expect(got.map((t) => t.FilePath))
            .toEqual(['/music/3.mp3', '/music/1.mp3']);
    });

    it('drops paths that are not in the list', () => {
        const tracks = asTracks([track(1)]);

        expect(tracksForPaths(tracks, ['/music/1.mp3', '/gone.mp3']))
            .toHaveLength(1);
    });

    it('is empty for an empty selection', () => {
        expect(tracksForPaths(asTracks([track(1)]), [])).toEqual([]);
    });
});

/**
 * `track-details` is 42 kB and must not ride in the startup chunk.
 *
 * Five components open it — `track-list`, `cover-grid`, `queue-panel`,
 * `playlist-details` and `smart-playlist-details` — and all five used
 * to `import` it for side effect, so it was eagerly evaluated before
 * first paint however `index.ts` split the routes. Measured: 814.5 kB
 * of JS evaluated before first paint, against 772.9 kB after.
 *
 * What keeps it out is the *absence* of those imports, which is
 * invisible: adding one back costs nothing anybody would notice, and
 * the dialog carries on working because the chunk is also warmed on
 * idle. So the first half of this file reads the five sources and
 * fails if one of them reaches for it statically again. A `import
 * type` is fine — types are erased and pull in no chunk.
 *
 * The second half is the reason that is safe: `loadTrackDetails()`
 * really does define the element, so an opener that awaits it can then
 * use the `<track-details>` its template already rendered. Before the
 * chunk lands that element exists but is not upgraded — an inert
 * `HTMLElement` with no `show()` — which is the same trap `index.ts`'s
 * `VIEW_LOADERS` exists for, and why every opener awaits.
 */
import { describe, expect, it } from 'vitest';

import trackListSource from '@components/track-list/track-list.ts?raw';
import coverGridSource from '@components/cover-grid/cover-grid.ts?raw';
import queuePanelSource from '@components/queue-panel/queue-panel.ts?raw';
import playlistDetailsSource from '@components/playlist-details/playlist-details.ts?raw';
import smartPlaylistDetailsSource from '@components/smart-playlist-details/smart-playlist-details.ts?raw';

import { loadTrackDetails } from '@utils/lazy-track-details';

const OPENERS: Array<[string, string]> = [
    ['track-list', trackListSource],
    ['cover-grid', coverGridSource],
    ['queue-panel', queuePanelSource],
    ['playlist-details', playlistDetailsSource],
    ['smart-playlist-details', smartPlaylistDetailsSource],
];

/** A side-effect import: `import '…track-details…'`, no bindings. */
const SIDE_EFFECT_IMPORT =
    /^\s*import\s+['"][^'"]*track-details[^'"]*['"]\s*;?\s*$/m;

describe('track-details stays out of the startup chunk', () => {
    it.each(OPENERS)(
        '%s does not import track-details for side effect',
        (_name, source) => {
            expect(SIDE_EFFECT_IMPORT.test(source)).toBe(false);
        },
    );

    it.each(OPENERS)('%s loads it at the point of use', (_name, source) => {
        expect(source).toContain('loadTrackDetails');
    });
});

describe('loadTrackDetails', () => {
    it('defines the element and reports success', async () => {
        await expect(loadTrackDetails()).resolves.toBe(true);
        expect(customElements.get('track-details')).toBeDefined();
    });

    it('leaves a rendered element usable', async () => {
        // The invariant every opener depends on: after the await, the
        // `<track-details>` its template already rendered has a
        // `show()` on it. (This cannot observe the *un*-upgraded state
        // — the suite above has already defined the element, and a
        // custom element cannot be undefined again — so it asserts the
        // postcondition rather than the transition.)
        const el = document.createElement('track-details');

        document.body.appendChild(el);

        try {
            await loadTrackDetails();
            expect(typeof (el as { show?: unknown }).show).toBe('function');
        } finally {
            el.remove();
        }
    });

    it('is memoised, so a second open does not refetch', async () => {
        const first = loadTrackDetails();

        expect(loadTrackDetails()).toBe(first);
        await first;
    });
});

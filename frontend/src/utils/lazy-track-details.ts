/**
 * Load the `<track-details>` chunk at the point of use.
 *
 * `track-details` is 42 kB and is opened from a context menu in five
 * components — `track-list`, `cover-grid`, `queue-panel`,
 * `playlist-details` and `smart-playlist-details`. All five imported it
 * statically, so it rode in the startup chunk however `index.ts` split
 * the routes: a dialog nobody may open, parsed before first paint.
 *
 * The awkward part is that `document.createElement` (and lit rendering
 * a tag) on an *undefined* custom element yields an inert
 * `HTMLElement` rather than throwing — the same trap `index.ts`'s
 * `VIEW_LOADERS` exists for. All five hosts render
 * `<track-details></track-details>` in their template and reach it with
 * `@query`, so before the chunk lands that query returns a real
 * element with no `show()` on it: an optional-chained call would throw,
 * and a truthiness guard would silently do nothing. So every opener
 * awaits this first. Once `define()` runs, the already-rendered element
 * upgrades in place, which is why the hosts need no render guard.
 *
 * The promise is memoised, so the second open is free. A *rejected*
 * one is not: a chunk that failed to arrive once (a dropped
 * connection mid-session) must be retryable, so the failure clears the
 * memo and says so at the level the plan's rule picks — the user asked
 * for a dialog, it did not happen, and asking again is meaningful.
 */

import { notificationStore } from '@store/notification-store.js';
import { describeError } from '@utils/describe-error.js';

let pending: Promise<boolean> | null = null;

/**
 * Resolve once `<track-details>` is defined and its element upgraded.
 *
 * Returns `false` if the chunk could not be loaded, having already
 * told the user; the caller should simply return.
 *
 * @param retry Re-runs the action that wanted the dialog, offered to
 *              the user as the notification's action.
 */
export function loadTrackDetails(retry?: () => void): Promise<boolean> {
    pending ??= import('@components/track-details/track-details.js')
        .then(() => customElements.whenDefined('track-details'))
        .then(() => true)
        .catch((err: unknown) => {
            // Let the next attempt try again rather than caching the
            // failure for the life of the session.
            pending = null;
            console.error('Failed to load track-details chunk:', err);

            notificationStore.persistent({
                title: 'Could not open track details',
                text: describeError(err),
                key: 'track-details-chunk',
                action: retry
                    ? { label: 'Try again', run: retry }
                    : undefined,
            });

            return false;
        });

    return pending;
}

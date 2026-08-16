/**
 * Open `<track-details>` for a file path.
 *
 * The five library-side hosts already hold the `library.Track` the
 * dialog wants — they render it. Explore's rows do not: a tracklist row
 * is an `MBTrack`/`LBTopRecording` from the catalog, and all it can say
 * about the library is *which file is behind it*. So the path is the
 * one key both sides share, and turning it back into a track is the
 * work this does.
 *
 * `libraryStore.getTracks()` is awaited rather than
 * `getCachedTracks()`-and-bail (which is what `queue-panel` does):
 * Explore is reachable without ever opening the library views, so a
 * cold cache is ordinary here rather than a symptom, and silently doing
 * nothing on a menu item the user just clicked is not an option. The
 * fetch is the store's own, shared with every other reader.
 */

import type * as library from '@go/library/models.js';
import type {
    CoverArtUrls,
    TrackDetails,
} from '@components/track-details/track-details.js';
import { libraryStore } from '@store/library-store.js';
import { loadTrackDetails } from '@utils/lazy-track-details.js';
import { tracksByFilePath } from '@utils/track-index.js';

/** The cover art the dialog shows, or nothing when the track has none. */
function coverArtOf(track: library.Track): CoverArtUrls | undefined {
    return track.CoverArtPath
        ? {
            coverArtPath: track.CoverArtPath,
            coverArtSmall: track.CoverArtSmall,
            coverArtMedium: track.CoverArtMedium,
            coverArtLarge: track.CoverArtLarge,
        }
        : undefined;
}

/**
 * What became of the attempt.
 *
 * `chunk-failed` is separate from `not-in-library` because
 * `loadTrackDetails` has already told the user about it — a caller that
 * treated the two alike would report a missing track over the top of a
 * notification saying the dialog itself could not be fetched.
 */
export type TrackDetailsOutcome = 'shown' | 'not-in-library' | 'chunk-failed';

/**
 * Show the details dialog for the library track at `filePath`.
 *
 * @param dialog A getter, not the element: `@query` resolves an
 *               un-upgraded `<track-details>` before the chunk lands,
 *               and only the read *after* `loadTrackDetails` is
 *               guaranteed to have `show()` on it.
 * @param retry  Re-runs the action that wanted the dialog, offered to
 *               the user if the chunk could not be fetched.
 */
export async function showTrackDetailsForPath(
    dialog: () => TrackDetails | undefined,
    filePath: string,
    retry: () => void,
): Promise<TrackDetailsOutcome> {
    const tracks = await libraryStore.getTracks();
    const track = tracksByFilePath(tracks).get(filePath);

    if (!track) return 'not-in-library';

    const ready = await loadTrackDetails(retry);

    if (!ready) return 'chunk-failed';

    dialog()?.show(track, coverArtOf(track));

    return 'shown';
}

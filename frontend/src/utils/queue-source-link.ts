/**
 * Turns a queue's `Source` into a "Playing from: X" link that navigates
 * back to the album, playlist, smart playlist, genre, artist or the
 * library's own Tracks view that a queue was built from — dispatching
 * the same `navigate` CustomEvent `explore-link.ts` uses, since every
 * primary/detail view already listens for it (see `frontend/index.ts`).
 * Kept separate from `explore-link.ts` rather than reusing its helpers:
 * the destinations and attributes differ per source type, and there is
 * no MBID/local-id fallback dance to share — a queue source always
 * carries a local id (`tracks` is the one exception, needing none).
 */

import type { QueueSource } from '../store/queue-store';

/** Fire a navigate event from the clicked element. */
function navigate(target: EventTarget, detail: Record<string, unknown>): void {
    target.dispatchEvent(
        new CustomEvent('navigate', {
            bubbles: true,
            composed: true,
            detail,
        }),
    );
}

/** Builds the `navigate` event detail for each source type. */
const SOURCE_NAVIGATE_DETAIL: Record<
    string,
    (source: QueueSource) => Record<string, unknown>
> = {
    tracks: () => ({ view: 'tracks' }),
    album: (source) => ({
        view: 'explore-album-details',
        localAlbumId: source.id,
        albumName: source.label,
    }),
    playlist: (source) => ({
        view: 'playlist-details',
        playlistId: source.id,
        playlistName: source.label,
    }),
    smartPlaylist: (source) => ({
        view: 'smart-playlist-details',
        playlistId: source.id,
        playlistName: source.label,
    }),
    genre: (source) => ({
        view: 'genre-details',
        genreName: source.label,
    }),
    artist: (source) => ({
        view: 'artist-details',
        artistId: source.id,
        artistName: source.label,
    }),
};

/**
 * Whether a source has somewhere to navigate back to. A dynamic mix
 * does not — it was synthesized, not fetched from a real page — so it
 * still describes itself (below) but should render as plain text
 * rather than a dead link.
 */
export function isQueueSourceNavigable(source: QueueSource): boolean {
    return source.type in SOURCE_NAVIGATE_DETAIL;
}

/**
 * The text to show for a queue's source, or null when there is none —
 * so callers can conditionally render without duplicating that check.
 */
export function describeQueueSource(source: QueueSource): string | null {
    if (source.type === '' || !source.label) return null;

    return `Playing from ${source.label}`;
}

/** Navigate to the collection a queue was built from. */
export function navigateToQueueSource(
    target: EventTarget,
    source: QueueSource,
): void {
    const buildDetail = SOURCE_NAVIGATE_DETAIL[source.type];
    if (!buildDetail) return;

    navigate(target, buildDetail(source));
}

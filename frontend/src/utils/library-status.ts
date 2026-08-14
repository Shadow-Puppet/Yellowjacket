import { downloadStore } from '@store/download-store';
import { libraryStore } from '@store/library-store';
import type { download } from '@go/models';
import type { LibraryStatus } from '../components/library-status-indicator/library-status-indicator';

/**
 * What the tick/hourglass/plus badge should say about one entity.
 *
 * This exists because the rule was written at all eight call sites and
 * so none of them had the whole of it: every one was a two-way ternary
 * between `in-library` and `not-in-library`, and the badge's third
 * state — `queued`, styled and labelled since it was written — was
 * produced by nothing. An album the user had already asked for through
 * the "Want this" button showed a plus and said it was not in their
 * library, on the same page, forty pixels from a filled button reading
 * "Wanted".
 *
 * Two rules decide the answer, and both are about honesty rather than
 * precedence for its own sake:
 *
 *  - **Owning outranks wanting.** A request that has been satisfied by
 *    any route — downloaded here, ripped, bought elsewhere — is not
 *    news; what the user has is.
 *  - **A request is by MBID, and the badge answers about the entity it
 *    is on.** A track inside a requested album is not itself requested,
 *    so it stays a plus. Saying otherwise would promise that clicking
 *    it later would find *that* recording.
 *
 * A `satisfied` request is deliberately not `queued`: nothing is coming.
 * A `paused` one is, because the user did ask for it and it is still on
 * the list — "queued" is a slight overstatement of a paused request and
 * a much smaller one than "not in your library".
 */
export function libraryStatusFor(
    owned: boolean,
    mbid?: string | null,
): LibraryStatus {
    if (owned) return 'in-library';

    if (!mbid) return 'not-in-library';

    const request = downloadStore.requestFor(mbid);

    if (request && request.state !== 'satisfied') return 'queued';

    return 'not-in-library';
}

/** What a badge can ask for. Artists are deliberately absent: a
 *  discography subscription is `explore-artist-details`'s Follow
 *  button, which can say what it is committing to. */
export type RequestableEntity = 'album' | 'track';

const ENTITY: Record<RequestableEntity, string> = {
    album: 'release-group',
    track: 'recording',
};

/**
 * Add or drop a request for one entity, and report which way it went.
 *
 * The counterpart to `libraryStatusFor`, here rather than in the badge
 * because the badge is one of several things that can ask —
 * `explore-album-details`'s "Want this" button is the other, and two
 * implementations of "what does wanting something mean" is exactly what
 * phase 1 was about.
 *
 * Returns `'wanted'` or `'cancelled'` so a caller can announce what
 * happened; throws if the backend refused, because a badge that
 * silently does nothing is what this whole plan is about.
 */
export async function toggleRequest(input: {
    mbid: string;
    entity: RequestableEntity;
    title: string;
    artist?: string;
}): Promise<'wanted' | 'cancelled'> {
    const existing = downloadStore.requestFor(input.mbid);

    if (existing) {
        await downloadStore.removeRequest(existing.id);

        return 'cancelled';
    }

    // A request belongs to a library because that is where its files
    // will land. There is always at least one by the time anything is
    // on screen — the first-run wizard blocks every pointer event until
    // there is — but an explicit failure beats a request filed against
    // library 0, which no import would ever match.
    const libraryId = await libraryStore.getDefaultLibraryId();

    if (!libraryId) throw new Error('no library to add this to');

    await downloadStore.addRequest({
        mbid: input.mbid,
        entity: ENTITY[input.entity],
        libraryId,
        artist: input.artist ?? '',
        title: input.title,
        scope: 'future',
        secondary: false,
    } as download.RequestInput);

    return 'wanted';
}

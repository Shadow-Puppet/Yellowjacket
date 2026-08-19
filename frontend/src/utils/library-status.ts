import { completenessStore } from '@store/completeness-store';
import { downloadStore } from '@store/download-store';
import { libraryStore } from '@store/library-store';
import type * as download from '@go/download/models.js';
import type { LibraryStatus } from '../components/library-status-indicator/library-status-indicator';
import { isOwned, type Ownable } from './ownership';

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

/**
 * Everything a badge needs about one entity, decided in one place.
 *
 * `status` is the state; `owned`/`expected` are the counts behind
 * `partial` and are zero for every other state, which is what the badge
 * requires — it documents that a caller with no total must not pass a
 * ring at 0%.
 */
export interface BadgeState {
    status: LibraryStatus;
    owned: number;
    expected: number;
}

/**
 * What the badge on an album card should say.
 *
 * Three rules, and the middle one is the whole point of this issue.
 *
 * **Ownership is the local album id**, per `utils/ownership.ts` — a
 * file, not the catalog's `inLibrary` ratchet.
 *
 * **A partly-held album says how partly.** The count comes from
 * `completenessStore`, which batches a screenful into one query;
 * reading it is what asks for it. Before this, an album held 2 tracks
 * of 10 wore the same green tick as one held whole on every grid in
 * the app.
 *
 * **A total that was never declared is not a total of zero.** Where
 * `known` is false — most of an untagged library, and every album until
 * a rescan repopulates `audio_files.total_tracks` — this is a plain
 * `in-library` and says nothing, which is the rule the badge's own
 * documentation states and the reason `Known` exists at all.
 */
export function albumBadgeFor(
    album: Ownable | null | undefined,
    mbid?: string | null,
): BadgeState {
    if (!isOwned(album)) {
        return { status: libraryStatusFor(false, mbid), owned: 0, expected: 0 };
    }

    const held = completenessStore.completeness(album?.localId);

    if (held?.known && !held.complete) {
        return {
            status: 'partial',
            owned: held.owned,
            expected: held.expected,
        };
    }

    return { status: 'in-library', owned: 0, expected: 0 };
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

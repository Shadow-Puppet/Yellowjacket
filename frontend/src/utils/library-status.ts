import { downloadStore } from '@store/download-store';
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

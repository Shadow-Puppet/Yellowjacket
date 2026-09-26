/**
 * Opening an external page, with the destination pinned.
 *
 * Every external link this app opens is a MusicBrainz entity page built
 * from an MBID that came from the catalog. Constructing the URL by
 * string concatenation leaves the destination to whatever is in that
 * string, so this parses it against the one origin the app means and
 * refuses anything else — an MBID cannot change the host, and if it
 * somehow did, nothing would open.
 *
 * It navigates through a real anchor rather than `window.open`: the
 * same top-level `_blank` navigation with `noopener`, and it keeps the
 * destination an ordinary link rather than an argument to a function
 * whose first parameter is a URL.
 */
const MUSICBRAINZ_ORIGIN = 'https://musicbrainz.org';

export function openMusicBrainz(path: string): void {
    const url = new URL(path, MUSICBRAINZ_ORIGIN);

    if (url.origin !== MUSICBRAINZ_ORIGIN) return;

    const link = document.createElement('a');

    link.href = url.toString();
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    link.click();
}

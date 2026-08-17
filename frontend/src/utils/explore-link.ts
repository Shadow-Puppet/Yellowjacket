/**
 * Utility for rendering track/album/artist names as clickable links.
 *
 * A name links to its MusicBrainz page when the entity is tagged, and
 * to the local library page for the same thing when it is not.  Both
 * destinations are the same two components — `explore-album-details`
 * and `explore-artist-details` both accept a local id instead of an
 * MBID — so an untagged album is not a dead end, it is just a page with
 * less on it.
 *
 * Falling back rather than rendering plain text is deliberate: a list
 * where some rows are clickable and others silently are not reads as a
 * bug, not as a statement about metadata.  The only case that still
 * renders as text is one we genuinely cannot route (no name at all, or
 * nothing in the library by that name).
 */

import { html, css } from 'lit';
import type { TemplateResult } from 'lit';
import { libraryStore } from '../store/library-store';

/** Shared CSS for explore link styling. Import into component styles. */
export const exploreLinkStyles = css`
    .explore-link {
        color: inherit;
        text-decoration: none;
        cursor: pointer;
    }

    .explore-link:hover {
        text-decoration: underline;
    }
`;

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

/** Case-insensitive compare that tolerates undefined. */
function sameName(a: string | undefined, b: string | undefined): boolean {
    return (a ?? '').toLowerCase() === (b ?? '').toLowerCase();
}

/**
 * Find the library album row for a name, loading the album cache first
 * if a view that populates it has not been opened yet.
 */
async function findLocalAlbum(
    albumName: string,
    artistName?: string,
): Promise<{ ID: number; Name: string; ArtistName: string } | null> {
    let albums = libraryStore.cachedAlbums;

    if (!albums) {
        try {
            albums = await libraryStore.getAlbums();
        } catch {
            return null;
        }
    }

    let fallback: (typeof albums)[0] | null = null;

    for (const album of albums ?? []) {
        if (!sameName(album.Name, albumName)) continue;
        if (artistName && sameName(album.ArtistName, artistName)) return album;
        fallback ??= album;
    }

    return fallback;
}

/** Find the library artist row for a name, loading the cache if needed. */
async function findLocalArtist(
    artistName: string,
): Promise<{ ID: number; Name: string; MBID: string } | null> {
    let artists = libraryStore.cachedArtists;

    if (!artists) {
        try {
            artists = await libraryStore.getArtists();
        } catch {
            return null;
        }
    }

    for (const artist of artists ?? []) {
        if (sameName(artist.Name, artistName)) return artist;
    }

    return null;
}

/**
 * How long a link waits before navigating.
 *
 * Every list these links appear in also plays a row on double-click,
 * and the title is the widest thing in the row — so the same gesture
 * that plays a track starts with a click on its name.  Navigating on
 * the first of those two clicks means double-clicking a track title
 * opens a page instead of playing it.  Holding the navigation for one
 * double-click interval, and dropping it if the second click arrives,
 * lets one element serve both without the row having to know links
 * exist.
 */
const DOUBLE_CLICK_GRACE_MS = 250;

/**
 * Wrap a link action so it fires on a genuine single click only.
 *
 * The click's propagation is stopped (the row must not also treat it as
 * a selection) but the *double*-click is left alone, so it still
 * reaches the row and plays the track.
 */
function singleClick(
    run: (target: EventTarget) => void,
): (e: MouseEvent) => void {
    return (e: MouseEvent) => {
        e.stopPropagation();
        e.preventDefault();

        // detail > 1 is the second click of a double click; the first
        // one already scheduled and is about to be cancelled.
        if (e.detail > 1) return;

        const target = (e.currentTarget ?? e.target) as EventTarget;

        const timer = window.setTimeout(() => {
            target.removeEventListener('dblclick', cancel);
            run(target);
        }, DOUBLE_CLICK_GRACE_MS);

        function cancel(): void {
            window.clearTimeout(timer);
        }

        target.addEventListener('dblclick', cancel, { once: true });
    };
}

/**
 * Render an artist name as a link to the artist page — the
 * MusicBrainz one when tagged, the library one when not.
 *
 * @param artistName - The artist name to display.
 * @param mbid - The MusicBrainz artist ID.  Empty string = local only.
 * @param content - Optional custom content to render inside the link
 *                  (e.g. highlighted search result).  Defaults to artistName.
 */
export function artistLink(
    artistName: string,
    mbid: string,
    content?: TemplateResult | string,
): TemplateResult | string {
    if (!artistName) return artistName;

    const onClick = singleClick((target) => {
        void (async () => {
            if (mbid) {
                navigate(target, {
                    view: 'explore-artist-details',
                    artistMBID: mbid,
                    artistName,
                });

                return;
            }

            const local = await findLocalArtist(artistName);
            if (!local) return;

            // The caller's row had no MBID, but the library row for the
            // same artist may — the grid routes by exactly this field,
            // so reading it here is what keeps the two paths agreeing.
            navigate(target, {
                view: 'explore-artist-details',
                artistMBID: local.MBID || '',
                artistName,
                localArtistId: local.ID,
            });
        })();
    });

    return html`<a
        class="explore-link"
        @click=${onClick}
        title=${mbid ? 'View artist on Explore' : 'View artist in your library'}
    >${content ?? artistName}</a>`;
}

/**
 * Render an album name as a link to the album page — the MusicBrainz
 * one when tagged, the library one when not.
 *
 * @param albumName - The album name to display.
 * @param mbid - The MusicBrainz release group ID.  Empty = local only.
 * @param content - Optional custom content to render inside the link.
 * @param artistName - Disambiguates same-named albums in the library.
 */
export function albumLink(
    albumName: string,
    mbid: string,
    content?: TemplateResult | string,
    artistName?: string,
): TemplateResult | string {
    if (!albumName) return albumName;

    return html`<a
        class="explore-link"
        @click=${singleClick((target) => {
            void openAlbum(target, albumName, mbid, artistName);
        })}
        title=${mbid ? 'View album on Explore' : 'View album in your library'}
    >${content ?? albumName}</a>`;
}

/**
 * Render a track name as a link that opens the track's album with the
 * track highlighted.  An untagged track highlights by title on the
 * library album page instead, so every row in a list behaves the same.
 *
 * @param trackName - The track name to display.
 * @param albumName - The album name (for the page title).
 * @param releaseGroupMBID - The album's MusicBrainz release group ID.
 * @param recordingMBID - The track's MusicBrainz recording ID.
 * @param content - Optional custom content (e.g. highlighted text).
 * @param artistName - Disambiguates same-named albums in the library.
 */
export function trackLink(
    trackName: string,
    albumName: string,
    releaseGroupMBID: string,
    recordingMBID: string,
    content?: TemplateResult | string,
    artistName?: string,
): TemplateResult | string {
    if (!trackName) return trackName;
    if (!albumName) return content ?? trackName;

    return html`<a
        class="explore-link"
        @click=${singleClick((target) => {
            void openAlbum(
                target,
                albumName,
                releaseGroupMBID,
                artistName,
                recordingMBID,
                trackName,
            );
        })}
        title=${releaseGroupMBID
            ? 'View track on the album page'
            : 'View track on the album page in your library'}
    >${content ?? trackName}</a>`;
}

/**
 * Route to an album page, preferring the catalog and falling back to
 * the library copy.  `highlight*` marks one track on arrival.
 */
async function openAlbum(
    target: EventTarget,
    albumName: string,
    releaseGroupMBID: string,
    artistName?: string,
    highlightTrackMBID?: string,
    highlightTrackTitle?: string,
): Promise<void> {
    const detail: Record<string, unknown> = {
        view: 'explore-album-details',
        releaseGroupMBID,
        albumName,
        artistName: artistName ?? '',
    };

    if (highlightTrackMBID) detail.highlightTrackMBID = highlightTrackMBID;
    if (highlightTrackTitle) detail.highlightTrackTitle = highlightTrackTitle;

    if (!releaseGroupMBID) {
        const local = await findLocalAlbum(albumName, artistName);
        if (!local) return;

        detail.localAlbumId = local.ID;
        detail.artistName = local.ArtistName;
    }

    navigate(target, detail);
}

/**
 * One credited artist within a multi-artist credit.
 *
 * Mirrors `artist_credit_part` / `file_artists`: the name **as
 * credited** (which is not the artist's own name — MusicBrainz credits
 * "Snoop Dogg" on a track by the artist called "Snoop Doggy Dogg"), the
 * MBID to navigate to, and the literal connector that follows this
 * part.
 */
export interface CreditPart {
    /** The name as credited. Display uses this. */
    creditedName: string;
    /** The artist's MusicBrainz ID. Navigation uses this. */
    artistMbid: string;
    /** The connector following this part: " feat. ", " & ", ", ", "". */
    joinPhrase: string;
}

/**
 * Render a credit as links, one per credited artist, with the join
 * phrases as plain text between them.
 *
 * Join phrases are **assembly instructions, not disassembly
 * instructions**.  This concatenates parts; it never searches for a
 * name inside a credit string.  That distinction is the whole point:
 * the stored credit text may have come from a file's tags while the
 * parts come from the catalog, and measured on a real library those
 * disagree for about one in three multi-artist credits ("Skrillex
 * feat. Swae Lee" tagged against "Skrillex & Swae Lee" upstream).  A
 * search would miss, or match the wrong span.  Building from parts,
 * the link boundaries are known by construction.
 *
 * Falls back to `artistLink(fallbackName, fallbackMbid)` — today's
 * behaviour exactly — when there are no parts.  That is the common
 * case and not a degraded one: a single-artist credit *is* one link,
 * and a file with no recording MBID or no catalog row has nothing to
 * decompose.  Do not try to split the fallback string; there is
 * genuinely no information in it to split on.
 *
 * @param parts - The credit's parts in position order, if known.
 * @param fallbackName - The credit as a single string.
 * @param fallbackMbid - The primary artist's MBID.
 */
export function creditLink(
    parts: readonly CreditPart[] | undefined,
    fallbackName: string,
    fallbackMbid: string,
): TemplateResult | string {
    // One part is one link, so it is the fallback rather than a special
    // case — and a zero-part credit reaching here would otherwise
    // render as nothing at all, which is worse than the single-artist
    // answer it replaced.
    if (!parts || parts.length < 2) {
        return artistLink(fallbackName, fallbackMbid);
    }

    return html`${parts.map(
        (part) =>
            html`${artistLink(part.creditedName, part.artistMbid)}${part.joinPhrase}`,
    )}`;
}

/**
 * The plain-text form of a credit, for `title=` attributes and any
 * other place that needs a string rather than a template.
 *
 * Rendered from the same parts by the same concatenation, so the
 * tooltip cannot disagree with the links beneath it.
 */
export function creditText(
    parts: readonly CreditPart[] | undefined,
    fallbackName: string,
): string {
    if (!parts || parts.length < 2) return fallbackName;

    return parts.map((p) => p.creditedName + p.joinPhrase).join('');
}

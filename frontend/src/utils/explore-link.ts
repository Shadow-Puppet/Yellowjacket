/**
 * Utility for rendering artist/album names as clickable links
 * that navigate to their MusicBrainz explore detail pages.
 *
 * Links are rendered only when an MBID is provided.  If the MBID
 * is empty (entity not tagged), the name renders as plain text.
 */

import { html, css } from 'lit';
import type { TemplateResult } from 'lit';

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

/**
 * Dispatch a navigate event to the explore-artist-details page.
 * The event bubbles through shadow DOM boundaries.
 */
function navigateToArtist(artistName: string, mbid: string, e: Event): void {
    e.stopPropagation();
    e.preventDefault();

    const target = e.currentTarget as HTMLElement;

    target.dispatchEvent(
        new CustomEvent('navigate', {
            bubbles: true,
            composed: true,
            detail: {
                view: 'explore-artist-details',
                artistMBID: mbid,
                artistName,
            },
        }),
    );
}

/**
 * Dispatch a navigate event to the explore-album-details page.
 * The event bubbles through shadow DOM boundaries.
 */
function navigateToAlbum(albumName: string, mbid: string, e: Event): void {
    e.stopPropagation();
    e.preventDefault();

    const target = e.currentTarget as HTMLElement;

    target.dispatchEvent(
        new CustomEvent('navigate', {
            bubbles: true,
            composed: true,
            detail: {
                view: 'explore-album-details',
                releaseGroupMBID: mbid,
                albumName,
            },
        }),
    );
}

/**
 * Render an artist name as a clickable link if an MBID is provided,
 * or as plain text if not.
 *
 * @param artistName - The artist name to display.
 * @param mbid - The MusicBrainz artist ID.  Empty string = no link.
 * @param content - Optional custom content to render inside the link
 *                  (e.g. highlighted search result).  Defaults to artistName.
 */
export function artistLink(
    artistName: string,
    mbid: string,
    content?: TemplateResult | string,
): TemplateResult | string {
    if (!artistName) return artistName;
    if (!mbid) return content ?? artistName;

    return html`<a
        class="explore-link"
        @click=${(e: Event) => navigateToArtist(artistName, mbid, e)}
        title="View artist on Explore"
    >${content ?? artistName}</a>`;
}

/**
 * Dispatch a navigate event to the explore-album-details page
 * with a highlight on a specific track.
 */
function navigateToTrack(
    albumName: string,
    releaseGroupMBID: string,
    recordingMBID: string,
    e: Event,
): void {
    e.stopPropagation();
    e.preventDefault();

    const target = e.currentTarget as HTMLElement;

    target.dispatchEvent(
        new CustomEvent('navigate', {
            bubbles: true,
            composed: true,
            detail: {
                view: 'explore-album-details',
                releaseGroupMBID,
                albumName,
                highlightTrackMBID: recordingMBID,
            },
        }),
    );
}

/**
 * Render an album name as a clickable link if an MBID is provided,
 * or as plain text if not.
 *
 * @param albumName - The album name to display.
 * @param mbid - The MusicBrainz release group ID.  Empty string = no link.
 * @param content - Optional custom content to render inside the link
 *                  (e.g. highlighted search result).  Defaults to albumName.
 */
export function albumLink(
    albumName: string,
    mbid: string,
    content?: TemplateResult | string,
): TemplateResult | string {
    if (!albumName) return albumName;
    if (!mbid) return content ?? albumName;

    return html`<a
        class="explore-link"
        @click=${(e: Event) => navigateToAlbum(albumName, mbid, e)}
        title="View album on Explore"
    >${content ?? albumName}</a>`;
}

/**
 * Render a track name as a clickable link that opens the album's
 * explore page with the track highlighted.  Requires both a
 * release group MBID (album) and a recording MBID (track).
 *
 * @param trackName - The track name to display.
 * @param albumName - The album name (for the page title).
 * @param releaseGroupMBID - The album's MusicBrainz release group ID.
 * @param recordingMBID - The track's MusicBrainz recording ID.
 * @param content - Optional custom content (e.g. highlighted text).
 */
export function trackLink(
    trackName: string,
    albumName: string,
    releaseGroupMBID: string,
    recordingMBID: string,
    content?: TemplateResult | string,
): TemplateResult | string {
    if (!trackName) return trackName;
    if (!releaseGroupMBID || !recordingMBID) return content ?? trackName;

    return html`<a
        class="explore-link"
        @click=${(e: Event) => navigateToTrack(albumName, releaseGroupMBID, recordingMBID, e)}
        title="View track on album page"
    >${content ?? trackName}</a>`;
}

/**
 * "Go to Artist" / "Go to Album", for the menus that carry a name the
 * phone stopped drawing as a link (#67).
 *
 * `utils/explore-link.ts` renders a plain string below the phone
 * breakpoint, because a few characters inside a row is not a touch
 * target and the row's own tap already means "play".  That takes a
 * destination away, so the row's context menu gives it back — which is
 * the whole of this issue: the navigation moves, it does not go.
 *
 * Three things about it are load-bearing.
 *
 * **It is drawn under exactly the condition the link is not.**
 * `inlineLinksSuppressed()` answers both, so a desktop menu is
 * untouched (the name beside it is still a link, and a menu that
 * repeats what the row already offers is furniture) and a phone menu
 * cannot be missing what the row lost.
 *
 * **It goes where the name went.** `openArtistPage` / `openAlbumPage`
 * are `explore-link`'s own routing, exported rather than reimplemented,
 * so an untagged artist reaches the library page here for the same
 * reason and by the same lookup it does from a link.
 *
 * **The host says when it is over**, through `onSelect` — every menu in
 * this app closes itself and most clear their selection, and both are
 * the host's bookkeeping rather than something a shared item may do on
 * its behalf.  `onHover` is for the four hosts with a playlist submenu,
 * which closes on any other item being pointed at.
 */

import { html, nothing } from 'lit';
import type { TemplateResult } from 'lit';

import { inlineLinksSuppressed, openArtistPage, openAlbumPage } from './explore-link';

/**
 * The entities one row or card can send you to.
 *
 * Everything is optional because the hosts differ: a track row knows
 * both, an album card knows only its artist, and an artist page's own
 * tracklist knows only the album.
 */
export interface GoToTarget {
    artistName?: string;
    artistMBID?: string;
    albumName?: string;
    albumMBID?: string;
}

export interface GoToHandlers {
    /** Called before navigating: close the menu, clear the selection. */
    onSelect?: () => void;
    /** Called on hover: close a playlist submenu, where the host has one. */
    onHover?: () => void;
}

/**
 * The menu items for a target, or nothing at all where the name beside
 * them is still a link.
 */
export function goToMenuItems(
    target: GoToTarget | undefined,
    handlers: GoToHandlers = {},
): TemplateResult | typeof nothing {
    if (!target || !inlineLinksSuppressed()) return nothing;

    const artist = target.artistName?.trim();
    const album = target.albumName?.trim();

    if (!artist && !album) return nothing;

    return html`
        ${artist
            ? html`<wa-dropdown-item
                  data-testid="go-to-artist"
                  @click=${(e: Event) => {
                      handlers.onSelect?.();
                      void openArtistPage(
                          e.currentTarget as EventTarget,
                          artist,
                          target.artistMBID ?? '',
                      );
                  }}
                  @mouseenter=${() => handlers.onHover?.()}
              >
                  <wa-icon slot="icon" name="user-group"></wa-icon>
                  Go to Artist
              </wa-dropdown-item>`
            : nothing}
        ${album
            ? html`<wa-dropdown-item
                  data-testid="go-to-album"
                  @click=${(e: Event) => {
                      handlers.onSelect?.();
                      void openAlbumPage(
                          e.currentTarget as EventTarget,
                          album,
                          target.albumMBID ?? '',
                          artist,
                      );
                  }}
                  @mouseenter=${() => handlers.onHover?.()}
              >
                  <wa-icon slot="icon" name="compact-disc"></wa-icon>
                  Go to Album
              </wa-dropdown-item>`
            : nothing}
    `;
}

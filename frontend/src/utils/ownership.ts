/**
 * What "I do not own this" looks like, and how the app decides it.
 *
 * The rule the user asked for, in their words: *owned content is the
 * default, normal, unadorned presentation; unowned content is what gets
 * marked*. `explore-album-details` implemented it for one tracklist —
 * dimmed in place, `aria-disabled` because dimming is a colour and
 * cannot be the only signal, and nothing at all drawn on the owned rows
 * — and every other catalog surface still mixed the two with a small
 * badge as the only difference. This is that rule, written once, so
 * eight surfaces cannot each keep their own version of it.
 *
 * ## Ownership is a file, and `localId` is the flag that says so
 *
 * The album page answers "do I own this row" with `filePaths`, a map
 * from a displayed track to a real file. A card grid cannot afford a
 * lookup per card — and does not need one, because the answer is
 * already on every model.
 *
 * `explore_index.local_artist_id` / `local_release_group_id` /
 * `local_recording_id` are built by `collectLibraryEntities` from
 * queries that every one join `audio_files`, and cleared by
 * `pruneStaleLocalCrossReferences` whose existence test is a file test
 * in all three cases. That is the same "ownership is a file" rule,
 * computed once per scan instead of once per screenful.
 *
 * **`inLibrary` is the weaker one and is deliberately not consulted.**
 * It is written by the same pass, so today the two agree — but it is a
 * one-way ratchet (`in_library = MAX(in_library, excluded.in_library)`)
 * whose only clearing pass is gated on a non-null `local_*_id`, so it
 * cannot be un-set on its own. One of the two is a fact with an owner;
 * the other is a flag that happens to agree with it.
 *
 * The divergence was observable before this: both `explore-view` and
 * `explore-artist-details` kept a `libraryMBIDs` set that accumulated
 * every MBID ever seen with `inLibrary` and cleared it never, in a view
 * that never unmounts. And on one artist-detail card the two answers
 * were used side by side — the context menu gated Play on
 * `localId > 0` while the badge said "in your library" from
 * `inLibrary`, so a card could claim to be owned, offer no Play, and
 * (the request item being gated on *not* owned) offer no way to ask for
 * it either.
 */

import { css } from 'lit';

/** Anything a card or row can be drawn from, as far as this is concerned. */
export interface Ownable {
    /** The local row id behind this entity: an album, a file, an artist. */
    localId?: number | null;
}

/**
 * Whether there is something of the user's behind this entity.
 *
 * Deliberately narrow: a local id and nothing else. Passing the model
 * straight in is the point — a call site that has to remember which of
 * two fields to read is a call site that will eventually read the other
 * one, which is exactly how the two answers came to sit on one card.
 */
export function isOwned(entity: Ownable | null | undefined): boolean {
    return (entity?.localId ?? 0) > 0;
}

/** The kinds of thing a catalog surface can draw. */
export type OwnableKind = 'album' | 'track' | 'artist';

/**
 * The sentence an unowned thing says, once.
 *
 * It reaches whoever is not seeing the dimming, so it has to name the
 * thing as well as the state — "not in your library" alone, repeated
 * down a grid, identifies nothing. The em dash matches the album
 * tracklist's existing phrasing, which is where this came from.
 */
export function unownedLabel(name: string, kind: OwnableKind): string {
    return `${name} — not in your library, ${
        kind === 'artist' ? 'browsing the catalog' : 'available to request'
    }`;
}

/**
 * The accessible name for a card or row, owned or not.
 *
 * `activates` is what the thing does when it is yours: "Play", "Album",
 * whatever the surface's own verb is. An unowned one does not get that
 * verb, because it cannot do it.
 */
export function ownershipLabel(
    owned: boolean,
    activates: string,
    name: string,
    kind: OwnableKind,
): string {
    return owned ? `${activates} ${name}` : unownedLabel(name, kind);
}

/**
 * The dimming, shared so it cannot drift across surfaces.
 *
 * Two things about it are load-bearing.
 *
 * **The text dims to a token, not with `opacity`.** `theme-store`'s
 * ramps are checked by `theme-contrast.test.ts` against every surface
 * text can sit on; an opacity multiplier is outside that check and
 * would quietly drop a dimmed title under 4.5:1 on the light ramps.
 * Secondary rather than tertiary for the reason the album tracklist
 * gives: these rows and cards have a `bgOverlay` hover background,
 * which tertiary does not clear.
 *
 * **Only the artwork takes an `opacity`.** A cover is not text, so it
 * is outside the contrast rule entirely, and it is the part of a card
 * that carries the most weight — dimming it is what makes a grid read
 * as catalog at a glance rather than needing the badge to be found.
 */
export const unownedStyles = css`
    .unowned .album-title,
    .unowned .track-title,
    .unowned .card-name,
    .unowned .top-release-title,
    .unowned .artist-name {
        color: var(--yj-text-secondary, #b3b3b3);
        font-weight: 400;
    }

    .unowned .album-art-container,
    .unowned .top-release-art,
    .unowned .track-art,
    .unowned .card-image,
    .unowned .card-image-placeholder,
    .unowned .artist-avatar {
        opacity: 0.55;
    }
`;

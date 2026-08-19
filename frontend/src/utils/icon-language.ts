/**
 * What each icon in this app means, once.
 *
 * The set was a mix: `plus` meant "add to the queue", "add to a
 * playlist", "make a new playlist" and "you do not own this" — the
 * first two *adjacent in the same context menu* — while `list` meant
 * the queue, the Playlists destination, and (in `queue-panel` alone)
 * adding to the queue. Two icons carrying seven meanings between them
 * is not a vocabulary, and a user cannot learn one that says four
 * things.
 *
 * The rule these are chosen by: **an icon names the noun it acts on,
 * not the verb.** "Add to queue" and "add to playlist" are the same
 * verb on different nouns, so the noun is what has to differ — which is
 * also why adding to a playlist wears the Playlists destination's own
 * icon rather than a generic plus. `plus` survives for exactly the one
 * thing it is unambiguous about, making something that did not exist.
 *
 * Import these rather than writing a name inline. A literal string is
 * how the last set drifted, and nothing catches it: a wrong-but-real
 * icon renders perfectly.
 */

/** Start playing this now. */
export const ICON_PLAY = 'play';

/** Start playing this now, in a shuffled order. */
export const ICON_SHUFFLE = 'shuffle';

/**
 * The queue, and putting something into it.
 *
 * One glyph for the noun and the action, so the button that opens the
 * queue and the menu item that adds to it are visibly the same subject.
 * The queue used to wear `list`, which is the Playlists destination.
 */
export const ICON_QUEUE = 'bars-staggered';

/** Put this next in the queue rather than at the end. */
export const ICON_PLAY_NEXT = 'forward-step';

/**
 * A playlist, and adding something to one.
 *
 * The same icon as the Playlists destination in the sidebar, which is
 * the point: the menu item says where the thing is going.
 */
export const ICON_PLAYLIST = 'list';

/**
 * Make a new thing that did not exist — a playlist, a rule, a library.
 *
 * This is the only meaning `plus` keeps. It used to carry four.
 */
export const ICON_NEW = 'plus';

/**
 * A smart playlist — the rule, and the thing the rule makes.
 *
 * Governed for the reason `ICON_AUTOTAG` states: it was already at
 * three call sites (the Playlists header, the row marker beside a smart
 * playlist's name, and `smart-playlist-details`'s avatar), and a name
 * stops being a detail of one component the moment there are two. It is
 * deliberately *not* `ICON_NEW`, even on the button that makes one:
 * an icon names the noun it acts on, and the noun here is the rule.
 */
export const ICON_SMART_PLAYLIST = 'filter';

/**
 * The request ("want") toggle, as an outline/solid pair.
 *
 * Two states of one control have to read as each other's opposite,
 * which a plus and a bookmark do not. The pair was already in the app
 * and already correct — `explore-album-details`'s "Want this" button
 * has used it since it was written, and `favorites-controller` uses the
 * same shape for `regular/heart` → `heart` — while the badge forty
 * pixels away showed a plus for the same state.
 *
 * That is `utils/library-status.ts`'s fault one layer down: it made the
 * two surfaces agree on *what wanting means* and left them disagreeing
 * on what it looks like.
 */
export const ICON_CAN_REQUEST = 'regular/bookmark';
export const ICON_REQUESTED = 'solid/bookmark';

/**
 * You have this.
 *
 * Deliberately not drawn on the common case — see the tracklist, where
 * absence is what gets marked. This is for the places that answer the
 * question directly, like the badge on a catalog card.
 */
export const ICON_IN_LIBRARY = 'check';

/**
 * The autotagger, and a match it is offering.
 *
 * The same icon as the Autotag destination in the sidebar, on the rule
 * `ICON_PLAYLIST` was chosen by: an icon names the noun it acts on, so
 * a suggestion on the album page wears the mark of the page it would
 * send you to. Governed from the moment there were two call sites,
 * which is when a name stops being a detail of one component.
 */
export const ICON_AUTOTAG = 'tag';

/**
 * Something is being fetched right now.
 *
 * Distinct from `ICON_REQUESTED`: a request may sit on the list
 * forever without anything happening, which is exactly why the badge's
 * "queued" state stopped being an hourglass.
 */
export const ICON_DOWNLOADING = 'download';

/**
 * The rest of what this thing can do.
 *
 * `page-header` collapses the actions that do not fit into one menu
 * behind this, so the glyph has to name *more of the same nouns* rather
 * than any one of them — which is what an ellipsis is and what `bars`
 * (the navigation drawer, one component over in `bottom-nav`) is not.
 * It is deliberately the only meaning it carries: an overflow menu that
 * shared an icon with a destination would be the `list` problem again.
 */
export const ICON_MORE_ACTIONS = 'ellipsis';

/**
 * Take this away.
 *
 * One icon for removing from a playlist, from the queue and from the
 * library, because the difference that matters is stated in the words
 * beside it and in the confirmation — "Remove from Library" says in its
 * impact line that the files are not deleted.
 */
export const ICON_REMOVE = 'trash';

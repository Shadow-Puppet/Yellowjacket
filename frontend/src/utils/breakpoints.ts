/**
 * The shell's breakpoints, where JavaScript has to agree with CSS.
 *
 * A media query inside a shadow root is answered by the viewport, so a
 * component normally states what it drops at phone width in its own
 * stylesheet and needs nothing from here. This exists for the cases
 * where the decision is not a style: `track-list` computes its grid in
 * JS from the host width, and `now-playing` renders *different content*
 * on a phone — a plain string instead of a link — which no stylesheet
 * can express.
 *
 * One breakpoint, several expressions of it. It was a private const in
 * track-list.ts when there was one; a second reader is where a copy
 * would start drifting from index.css.
 */

/**
 * Phone width. 600px rather than the sidebar's 900px because 900 is a
 * laptop: the answer there is a narrower sidebar, which is still a
 * sidebar. Below this the shell drops the sidebar column entirely and
 * bottom-nav takes over.
 */
export const PHONE_QUERY = '(max-width: 599px)';

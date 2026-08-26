import { css } from 'lit';

/**
 * A bottom sheet whose body scrolls says so, in one rule both sheets
 * read.
 *
 * The app has two sheets — `menu-surface`'s context menu (#60) and
 * `bottom-nav`'s "More" navigation (#71) — and both are capped at 85vh,
 * because a surface covering the whole screen is a page rather than a
 * sheet. So both overflow, and both used to overflow *silently*: the
 * menu at 424x439 with eight items ending at y=470 (#207), the nav
 * sheet at the same viewport with `scrollHeight` 412 against
 * `clientHeight` 373 (#210). Where the cut lands on a row boundary the
 * sheet ends in a clean edge that reads as the end of the list.
 *
 * The mechanism is #207's and is unchanged by being shared: two
 * background layers on the scrolling box, whose *attachments* are the
 * conditionality. A cover of the sheet's own colour is painted at the
 * end of the *content* (`local`) over a shadow pinned to the box
 * (`scroll`), so the cover scrolls up over the shadow exactly when
 * there is nothing more to see. The fade is therefore absent on a sheet
 * that fits, present the moment one does not, and gone again at the end
 * of the list — with no scroll listener, no measurement and nothing
 * reaching into another component's shadow root for the scroller.
 * `background-attachment` is Chrome 4; the reference device is
 * Chrome 113.
 *
 * Three things about it are load-bearing.
 *
 * **The cover takes the sheet's own colour, from a custom property.**
 * The two sheets are different greys — the nav sheet paints
 * `--yj-bg-surface`, because it holds the sidebar and two greys in one
 * sheet is a seam across the middle of it, while the context sheet
 * paints the menus' `--yj-bg-elevated`. A shared rule that hard-coded
 * either would put that seam back on the other one, so the host sets
 * `--yj-sheet-surface` on the same box and this reads it.
 *
 * **The curve is steep because the rows under it stay live.** A scrim
 * over a menu item is that item's text surface, and this app's rule is
 * that text clears 4.5:1 on every surface it can sit on — which the
 * light ramp, whose `bgElevated` is `#e9ecef`, makes non-theoretical. A
 * row is 48px with its label centred, so 32px of scrim already down to
 * a quarter strength at 14px spends its weight on the strip below the
 * last legible label: measured at 9.9:1 on that label on the light ramp,
 * against 5.0:1 for a linear 48px draft at 0.8. The dark-ramp pixel
 * table is in `.planning/NOTES.md` (2026-08-23).
 *
 * **The box is declared a scroller here too.** `overflow-y: auto` is
 * part of the same statement rather than left to each host: a fade over
 * a box that is not the scroller is a fade that never moves, and the
 * component tier asserts the pair together for that reason.
 */
export const sheetScrollFade = css`
    overflow-y: auto;
    background:
        linear-gradient(
                var(--yj-sheet-surface, #343a40),
                var(--yj-sheet-surface, #343a40)
            )
            bottom / 100% 32px no-repeat local,
        linear-gradient(
                to top,
                rgba(0, 0, 0, 0.6) 0%,
                rgba(0, 0, 0, 0.25) 45%,
                rgba(0, 0, 0, 0) 100%
            )
            bottom / 100% 32px no-repeat scroll;
`;

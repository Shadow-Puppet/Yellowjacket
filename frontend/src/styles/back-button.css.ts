import { css } from 'lit';

/**
 * The way out of a detail view, at the app's 44px touch floor.
 *
 * #186's second table names `artist-details`' back button at
 * **32x32**. It is the same declaration in **six** components —
 * `artist-details`, `genre-details`, `playlist-details`,
 * `smart-playlist-details`, `explore-artist-details` and
 * `explore-album-details` — byte-identical, 32px in all six, and the
 * sweep that filed the issue visited one of them.
 *
 * That is the argument for this file rather than six edits. A device
 * sweep walks the views somebody thought to open, so six copies of a
 * control is six chances for the next pass to miss five; the arrows
 * and the toggles were each one declaration covering 36 and 29
 * controls, and this is the same shape stated the other way round.
 *
 * **It is a real 44px box, not padding with the width handed back.**
 * The header pass had to grow a hit area past its own layout box
 * because `page-header` measures itself for #69's overflow fit; a
 * detail view's header does not, so the control can simply be the
 * target. It also *should* be — this button has a visible background,
 * so a hit area larger than the circle would be a control that is
 * bigger than it looks, which is the thing #187 accepts only where a
 * thin painted track is the point.
 *
 * The size is #55's, arrived at for the same reason one component
 * over: "the way out is 44px on a phone", when the queue panel's close
 * button was 25x21 and, at phone width, the only pointer route off a
 * full-screen surface. A detail view has the platform's back gesture
 * as well, so this is less severe than the queue was — it is the same
 * control wearing the same mistake.
 */
export const backButton = css`
    .back-button {
        display: flex;
        align-items: center;
        justify-content: center;
        width: 44px;
        height: 44px;
        border: none;
        border-radius: 50%;
        background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
        color: var(--yj-text-primary, #fff);
        cursor: pointer;
        flex-shrink: 0;
        transition: background-color 0.15s ease;
    }

    .back-button:hover {
        background: var(--yj-bg-hover, rgba(255, 255, 255, 0.12));
    }
`;

import { css } from 'lit';

/**
 * A Web Awesome form control is at least the app's 44px touch floor.
 *
 * #56 named 44px and #186 found nothing but the transport had reached
 * it. Web Awesome's form controls are the part of Settings this app
 * does not draw: measured on the reference device (TLP301, 424x439),
 * `wa-input`'s control is **204x20** and `wa-button` **185x21** — the
 * shortest controls on the page, and the only ones whose height is
 * decided inside somebody else's shadow root.
 *
 * `--wa-form-control-height` is that decision, and it is the library's
 * own theming variable rather than a part or an internal — the default
 * theme sets it at `:root` and every control that has a height reads
 * it (button, input, select, radio). So this is `wa-slider-label`'s
 * better half: the API first, and no reach into a shadow root at all.
 *
 * Three things about it are load-bearing.
 *
 * **A custom property inherits through a shadow boundary**, which is
 * what lets a `:host` declaration reach a `wa-input` the host renders.
 * That is also why it is a stylesheet a component adopts rather than a
 * `:root` rule in `index.css`: a `:root` rule would cover every wa
 * control in the app in one line and be invisible to the component
 * tier, which renders a component and no page stylesheet. Here the
 * floor is measurable where it is applied.
 *
 * **It is a flat 44px rather than a floor over the library's own
 * expression.** The default is `round(calc(2 * padding-block + 1em *
 * line-height), 1px)` — em-based, so `size="small"` is what produced
 * the 20px above — and a `max(44px, …)` would have to restate that
 * formula here, which is a copy of somebody else's arithmetic that
 * goes stale silently. A flat value is safe because this app uses
 * exactly two sizes, `small` and the default, and both are under the
 * floor; a `size="large"` added later would be pinned down to 44 and
 * should take that as the prompt to revisit this.
 *
 * **Only the height is pinned.** The font size still comes from
 * `size="small"`, so a control grows its hit area without growing its
 * visual weight — which is what #186's Direction asks for and what the
 * page header's second pass had to be corrected to do.
 */
export const waTouchFloor = css`
    :host {
        --wa-form-control-height: 44px;
    }
`;

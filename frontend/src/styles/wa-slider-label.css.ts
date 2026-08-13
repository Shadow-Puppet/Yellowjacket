import { css } from 'lit';

/**
 * A `wa-slider` is named by its `label`, and its `label` is visible.
 *
 * The same trap as `utils/name-dialog.ts`, one component over, and this
 * one the audit filed under "what is already correct": `a11y.md` lists
 * `seek-bar` and `volume-control` as exemplary because they pass
 * `aria-label`. The role is not on the host. Web Awesome renders a
 * `<div id="slider" role="slider" aria-labelledby="label">` inside its
 * own shadow root, pointing at an internal `<label id="label">` that is
 * empty unless the `label` property is set — and `aria-labelledby`
 * outranks the host's `aria-label`, which the AX tree never sees. Both
 * sliders in this app computed a name of `""`. Measured with
 * `Accessibility.getFullAXTree` against the running app, on all eleven
 * views; `volume-control` did not even have the `aria-label` the audit
 * credits it with.
 *
 * So the name comes from `label`, which is the library's own API, and
 * this hides it. That is preferred over reaching into the shadow root
 * the way `name-dialog.ts` has to, for the failure mode: if Web Awesome
 * renames these parts the label becomes *visible* — wrong-looking and
 * correctly named — rather than silently nameless again.
 *
 * The second rule is not decoration. `#slider` takes an 8px
 * `margin-block-start` as soon as a label exists, so hiding the label
 * alone still grows the control from 6px to 14px and moves the transport
 * bar. `display: none` on the label is deliberate and safe: an element
 * referenced by `aria-labelledby` contributes its text even when it is
 * hidden, which is exactly the accname rule this relies on (verified —
 * the slider reports "Seek" with the label displaying nothing).
 */
export const waSliderLabel = css`
    wa-slider::part(label) {
        display: none;
    }

    wa-slider::part(slider) {
        margin-block-start: 0;
    }
`;

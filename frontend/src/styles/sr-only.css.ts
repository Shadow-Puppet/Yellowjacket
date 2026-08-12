import { css } from 'lit';

/**
 * The visually-hidden class, and with it the rule for using one.
 *
 * A live region has to be **in the DOM before the text it announces
 * is**: most screen readers announce a *change* to a region they are
 * already watching, and ignore a region that appears with its content
 * already in it. So these regions render unconditionally and empty, and
 * only their text changes — which is why they are a class rather than a
 * component that mounts on demand.
 *
 * `clip-path` rather than `display: none` or `visibility: hidden`, both
 * of which take the element out of the accessibility tree along with the
 * layout, which would defeat the point.
 */
export const srOnly = css`
    .sr-only {
        position: absolute;
        width: 1px;
        height: 1px;
        margin: -1px;
        padding: 0;
        overflow: hidden;
        clip-path: inset(50%);
        white-space: nowrap;
        border: 0;
    }
`;

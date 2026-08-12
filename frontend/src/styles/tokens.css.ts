import { css } from 'lit';

/**
 * Design tokens for consistent sizing across all components.
 * Import and include in a component's static styles array:
 *
 *   import { designTokens } from '../../styles/tokens.css';
 *   static styles = [designTokens, css`...`];
 */
export const designTokens = css`
    :host {
        /* ── Icon sizes ── */
        --yj-icon-sm: 14px;
        --yj-icon-md: 18px;
        --yj-icon-lg: 24px;

        /* ── Type scale ──

           In rem, so it tracks the root font size: the scale was
           hardcoded px and consumed by essentially every component, so
           raising the OS or browser font size changed nothing anywhere
           (WCAG 1.4.4, a11y.19). The values are the same at the default
           16px root — 11/16, 12/16, 13/16, 15/16, 18/16 — so nothing
           moves until someone asks it to.

           The scale is coupled to four virtualized lists and does not
           reach them (a11y.20). track-list's rows are 33px, queue-panel's
           49px and both playlist detail views' 45px, each duplicated as
           the layout's _itemSize hint so the scroll maths agrees with
           the DOM; all of them also carry contain: strict, which clips
           overflow rather than growing the row. So larger text reflows
           the app but crops those rows, and fixing that means deriving
           _itemSize from a measured row rather than from a constant.
           Deliberately not done here: it is a change to the scroll maths
           of four lists, not a change to a type scale. */
        --yj-text-xs: 0.6875rem;
        --yj-text-sm: 0.75rem;
        --yj-text-md: 0.8125rem;
        --yj-text-lg: 0.9375rem;
        --yj-text-xl: 1.125rem;
    }
`;

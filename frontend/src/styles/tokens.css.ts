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

        /* ── Type scale ── */
        --yj-text-xs: 11px;
        --yj-text-sm: 12px;
        --yj-text-md: 13px;
        --yj-text-lg: 15px;
        --yj-text-xl: 18px;
    }
`;

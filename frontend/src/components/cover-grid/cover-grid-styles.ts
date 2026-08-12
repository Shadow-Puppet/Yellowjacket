import { css } from 'lit';
import { contextMenuStyles } from '@utils/context-menu-controller.js';
import { designTokens } from '../../styles/tokens.css';

/** Component-specific styles for the cover grid. */
const gridStyles = css`
    :host {
        display: flex;
        flex-direction: column;
        overflow: hidden;
        height: 100%;
        position: relative;
        contain: layout style;
    }

    /* ========================================
     * Album card
     * ======================================== */

    .album-card {
        display: flex;
        flex-direction: column;
        cursor: pointer;
        padding: 5px;
        /* border-radius removed — forces anti-aliased path clipping on
           every paint of every visible card; cover image retains its own
           4px radius via .cover-container */
        box-sizing: border-box;
        width: var(--card-width, 176px);
    }

    .album-card:hover {
        background-color: var(--yj-hover-overlay, rgba(255, 255, 255, 0.1));
    }

    .album-card.selected {
        outline: 2px solid var(--yj-accent, #ffd43b);
        outline-offset: 2px;
    }

    .album-card:focus-visible {
        outline: 2px solid var(--yj-accent, #ffd43b);
        outline-offset: 2px;
    }

    .cover-container {
        position: relative;
        width: 100%;
        aspect-ratio: 1;
        border-radius: 4px;
        overflow: hidden;
        background-color: var(--yj-bg-surface, #282828);
        /* transition removed — software rendering repaints per frame */
    }

    .album-card.selected .cover-container {
        scale: 0.95;
    }

    .cover-image {
        width: 100%;
        height: 100%;
        object-fit: cover;
        -webkit-user-drag: none;
    }

    .placeholder-cover {
        width: 100%;
        height: 100%;
        display: flex;
        align-items: center;
        justify-content: center;
        background: linear-gradient(
            135deg,
            var(--yj-bg-overlay, #404040) 0%,
            var(--yj-bg-surface, #282828) 100%
        );
        color: var(--yj-text-secondary, #b3b3b3);
        font-size: var(--placeholder-font, 48px);
    }

    .album-info {
        margin-top: 4px;
        min-width: 0;
        text-align: center;
        /* transition removed — software rendering repaints per frame */
    }

    .album-card.selected .album-info {
        scale: 0.95;
    }

    .album-name {
        font-size: var(--album-name-font, 14px);
        font-weight: 400;
        color: var(--yj-text-primary, #fff);
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .artist-name {
        font-size: var(--artist-name-font, 12px);
        color: var(--yj-text-secondary, #b3b3b3);
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        margin-top: 2px;
    }

    .album-year {
        color: var(--yj-text-tertiary, #888);
    }

    /* ========================================
     * Shared states
     * ======================================== */

    .loading {
        display: flex;
        justify-content: center;
        align-items: center;
        padding: 32px;
        color: var(--yj-text-secondary, #b3b3b3);
    }

    .empty-state {
        display: flex;
        flex-direction: column;
        justify-content: center;
        align-items: center;
        padding: 48px;
        color: var(--yj-text-secondary, #b3b3b3);
        text-align: center;
    }

    .empty-state p {
        margin: 8px 0;
    }
`;

/** Combined styles for the cover grid component. */
export const coverGridStyles = [
    designTokens,
    gridStyles,
    contextMenuStyles,
];

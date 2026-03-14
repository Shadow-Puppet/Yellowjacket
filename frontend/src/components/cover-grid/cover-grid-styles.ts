import { css } from 'lit';
import { contextMenuStyles } from '@utils/context-menu-controller.js';
import { designTokens } from '../../styles/tokens.css';

/** Component-specific styles for the cover grid. */
const gridStyles = css`
    :host {
        display: flex;
        flex-direction: column;
        overflow: hidden;
        position: relative;
        contain: layout style;
    }

    /* ========================================
     * Sort toolbar
     * ======================================== */

    .sort-toolbar {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 4px 8px;
        font-size: var(--yj-text-sm);
        color: var(
            --yj-text-secondary,
            #b3b3b3
        );
        border-bottom: 1px solid
            var(--yj-border-subtle, #333);
        flex-shrink: 0;
        user-select: none;
    }

    .sort-anchor {
        display: inline-flex;
        align-items: center;
        gap: 4px;
        cursor: pointer;
        padding: 2px 6px;
        border-radius: 4px;
        background: transparent;
        border: none;
        color: inherit;
        font: inherit;
    }

    .sort-anchor:hover {
        background: var(
            --yj-hover-overlay,
            rgba(255, 255, 255, 0.05)
        );
    }

    .sort-anchor .sort-label {
        color: var(--yj-text-primary, #fff);
    }

    .sort-dir-btn {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        width: 24px;
        height: 24px;
        cursor: pointer;
        border: none;
        border-radius: 4px;
        background: transparent;
        color: var(
            --yj-text-secondary,
            #b3b3b3
        );
        font-size: var(--yj-text-sm);
        padding: 0;
    }

    .sort-dir-btn:hover {
        background: var(
            --yj-hover-overlay,
            rgba(255, 255, 255, 0.05)
        );
        color: var(--yj-text-primary, #fff);
    }

    .sort-dropdown-panel {
        background-color: var(
            --yj-bg-elevated,
            #343a40
        );
        border: 1px solid
            var(--yj-border, #444);
        border-radius: 6px;
        padding: 4px 0;
        box-shadow: 0 8px 24px
            rgba(0, 0, 0, 0.5);
        min-width: 140px;
    }

    .sort-dropdown-panel wa-dropdown-item {
        cursor: pointer;
        --wa-color-text-normal: var(
            --yj-text-primary,
            #fff
        );
        font-size: var(--yj-text-md);
    }

    .sort-dropdown-panel
        wa-dropdown-item:hover {
        background-color: var(
            --yj-hover-overlay,
            rgba(255, 255, 255, 0.1)
        );
    }

    .sort-dropdown-panel
        wa-dropdown-item.active-sort {
        color: var(--yj-accent, #ffd43b);
        --wa-color-text-normal: var(
            --yj-accent,
            #ffd43b
        );
    }

    #sort-dropdown {
        z-index: 200;
    }

    .grid-scroll-container {
        flex: 1;
        position: relative;
        overflow-y: auto;
        contain: paint;
        will-change: transform;
    }

    /* ========================================
     * Album card
     * ======================================== */

    .album-card {
        display: flex;
        flex-direction: column;
        cursor: pointer;
        border-radius: 8px;
        padding: 5px;
        transition:
            background-color 0.2s ease,
            transform 0.15s ease;
        box-sizing: border-box;
        width: var(--card-width, 176px);
        content-visibility: auto;
        contain-intrinsic-size: auto var(--card-width, 176px) auto calc(var(--card-width, 176px) + 40px);
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
        transition: scale 0.15s ease;
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
        transition: scale 0.15s ease;
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

    .sort-toolbar {
        position: relative;
    }

    .search-indicator {
        position: absolute;
        left: 50%;
        transform: translateX(-50%);
        pointer-events: none;
        background: var(--yj-bg-overlay, #495057);
        color: var(--yj-text-secondary, #b3b3b3);
        font-size: var(--yj-text-sm);
        padding: 2px 14px;
        border-radius: 12px;
        border: 1px solid
            var(--yj-border-subtle, #555);
        white-space: nowrap;
        opacity: 0.92;
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

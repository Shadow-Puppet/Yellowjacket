import { css } from 'lit';

/**
 * The Explore album card, once.
 *
 * Two components draw one — `explore-view`'s shelves and search
 * results, and `explore-artist-details`'s discography — and they had
 * grown two copies of the same rules. That is how the size came apart:
 * `explore-view` clamped its cards to a 130–150px range so two cards in
 * one row could be different widths, and since the artwork is square
 * that made them different *heights* as well. A row of covers with
 * ragged bottoms is the whole complaint.
 *
 * So the width is a fixed `--yj-album-card-width` and the lines below
 * the art each reserve their own space, which is what makes every card
 * the same size no matter what a given album happens to carry —
 * `album-card-size.test.ts` measures that rather than trusting it.
 *
 * Three rules here are the parts that changed rather than moved.
 *
 * **The artwork is inset in the square, not cropped to it.** The
 * container was already `aspect-ratio: 1` but the image was
 * `object-fit: cover`, so a non-square cover lost its edges. It is
 * `contain` now and the container's own background is transparent, so
 * a tall or wide cover sits in the middle of the square with the page
 * showing through beside it.
 *
 * **The badge lives on the artwork, top-left, and only under the
 * pointer.** It used to sit in the metadata line and only for the
 * unowned case. It draws for every card now — an owned album's tick is
 * the answer to the same question — and it is revealed by hover on a
 * pointer device. Where there is no hover it is *always* visible rather
 * than never, because on those devices it is the only route to its
 * action: `explore-view`'s card menu carries no request item, so a
 * phone with the badge hidden could not ask for an album at all.
 *
 * **Nothing dims an unowned card.** `unownedStyles` was removed from
 * the catalog surfaces on the rule that the badge is the mark; the
 * album page's *tracklist* still dims unowned rows, which is a
 * different statement about a different thing.
 */
export const albumCardStyles = css`
    .album-card {
        width: var(--yj-album-card-width, 150px);
        display: flex;
        flex-direction: column;
        gap: 6px;
        padding: 8px;
        border-radius: 8px;
        box-sizing: border-box;
        flex-shrink: 0;
        cursor: pointer;
        transition: background 0.15s ease;
    }

    .album-card:hover {
        background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
    }

    .album-card:active {
        transform: scale(0.97);
    }

    .album-card:focus-visible {
        outline: 2px solid var(--yj-accent-text, #ffd43b);
        outline-offset: -2px;
    }

    .album-art-container {
        position: relative;
        width: 100%;
        aspect-ratio: 1;
        border-radius: 4px;
        overflow: hidden;
        background: transparent;
        display: flex;
        align-items: center;
        justify-content: center;
    }

    .album-art-container img {
        width: 100%;
        height: 100%;
        object-fit: contain;
        display: block;
    }

    /* The placeholder is the one case that *is* a full square, so it
       carries the background the container gave up. */
    .album-art-fallback {
        display: flex;
        align-items: center;
        justify-content: center;
        width: 100%;
        height: 100%;
        position: absolute;
        inset: 0;
        background: linear-gradient(
            135deg,
            var(--yj-bg-overlay, #404040) 0%,
            var(--yj-bg-surface, #282828) 100%
        );
    }

    .album-art-fallback wa-icon {
        color: var(--yj-text-tertiary, #888);
        font-size: 24px;
        opacity: 0.5;
    }

    .album-card-badge {
        position: absolute;
        top: 6px;
        left: 6px;
        z-index: 1;
        display: flex;
        visibility: hidden;
        opacity: 0;
        transition: opacity 0.15s ease, visibility 0.15s ease;
    }

    @media (hover: hover) and (pointer: fine) {
        .album-card:hover .album-card-badge,
        .album-card:focus-within .album-card-badge {
            visibility: visible;
            opacity: 1;
        }
    }

    @media not all and (hover: hover) {
        .album-card-badge {
            visibility: visible;
            opacity: 1;
        }
    }

    .album-title {
        font-weight: 500;
        color: var(--yj-text-primary, #fff);
        font-size: var(--yj-text-sm);
        line-height: 1.3;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    /* Reserved even where a surface has no artist to draw, so a card
       in a row is never shorter than its neighbour. */
    .album-artist {
        color: var(--yj-text-tertiary, #888);
        font-size: var(--yj-text-xs);
        line-height: 1.3;
        min-height: 1.3em;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .album-meta {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 6px;
        color: var(--yj-text-tertiary, #888);
        font-size: var(--yj-text-xs);
        height: 20px;
    }

    .album-meta-text {
        display: flex;
        align-items: center;
        gap: 6px;
        min-width: 0;
        overflow: hidden;
    }

    .type-badge {
        background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
        padding: 1px 6px;
        border-radius: 3px;
        font-size: 10px;
        white-space: nowrap;
    }
`;

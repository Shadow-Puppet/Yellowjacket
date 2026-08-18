/**
 * Even spacing for the three card grids — albums, artists, genres.
 *
 * All three used `justify: 'center'` with a fixed 8px gap and 8px
 * padding, which gives the row a fixed width and pushes everything left
 * over to the two margins: on a 1440px window the albums grid drew its
 * cards 16px apart inside 78px of nothing down each side. The outside
 * was five times the inside.
 *
 * The fix is to spend the leftover on the spacing instead, so there is
 * one number: between two cards, between two rows, and down each edge.
 * The virtualizer has a word for that — `justify: 'space-evenly'` with
 * `gap: 'auto'` — and it cannot be used, because it fits
 * `floor(width / cardWidth)` columns without reserving the gap it is
 * about to need: a width one card short of exact fits seven cards a
 * pixel apart. Deciding the column count here is what puts a floor
 * under the spacing, and the grid is then given plain numbers.
 */

/** The narrowest the spacing is allowed to get. */
export const MIN_GRID_SPACING = 8;

/**
 * How many cards of `cardWidth` fit across `width`.
 *
 * A row of c cards spends c×cardWidth on cards and (c+1)×spacing on the
 * spaces between and beside them, so c is bounded by
 * (width − spacing) / (cardWidth + spacing) at the minimum spacing.
 */
export function gridColumnsFor(
    width: number,
    cardWidth: number,
): number {
    if (cardWidth <= 0) return 1;

    const fit = Math.floor(
        (width - MIN_GRID_SPACING) / (cardWidth + MIN_GRID_SPACING),
    );

    return Math.max(1, fit);
}

/**
 * The spacing `width` produces — the gap, the row gap and the padding,
 * which are all the same number.
 */
export function gridSpacingFor(
    width: number,
    cardWidth: number,
): number {
    const columns = gridColumnsFor(width, cardWidth);
    const leftover = width - columns * cardWidth;

    return Math.max(
        MIN_GRID_SPACING,
        Math.floor(leftover / (columns + 1)),
    );
}

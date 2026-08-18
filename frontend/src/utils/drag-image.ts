/**
 * Creates a custom drag image element showing a track count badge.
 * The element is appended to the document body (required by the
 * setDragImage API) and removed after the drag ends.
 */
export function createDragImage(count: number): HTMLElement {
    const el = document.createElement('div');

    el.textContent = `${count} track${count !== 1 ? 's' : ''}`;
    el.style.cssText = [
        'position: fixed',
        'top: -1000px',
        'left: -1000px',
        'padding: 6px 14px',
        'border-radius: 6px',
        'background: #ffd43b',
        'color: #000',
        'font-size: 13px',
        'font-weight: 600',
        'font-family: inherit',
        'white-space: nowrap',
        'pointer-events: none',
        'z-index: 9999',
    ].join(';');

    document.body.appendChild(el);

    return el;
}

/**
 * Creates a drag image showing an album cover art thumbnail, with a
 * corner badge saying how many tracks are on the way.
 *
 * The count is not decoration. The cover says *what* is being dragged
 * and nothing said *how much* — an album is 1 track or 30 and the
 * thumbnail is identical either way, so the one number the drop is
 * about was the one thing the drag did not show. Every other drag in
 * the app says it (`createDragImage` is a count and nothing else);
 * this one was the exception because it had a picture to show instead.
 *
 * A count of 1 draws no badge: "1" over a single album cover is noise,
 * and the absence is unambiguous next to a badge that only ever
 * appears when there is more than one.
 */
export function createAlbumArtDragImage(
    coverUrl: string,
    count = 1,
): HTMLElement {
    const size = 64;
    const wrapper = document.createElement('div');

    wrapper.style.cssText = [
        'position: fixed',
        'top: -1000px',
        'left: -1000px',
        'pointer-events: none',
        'z-index: 9999',
        // The badge is positioned against this box, and the box stays
        // exactly the cover's size: anything outside it risks being
        // clipped out of the snapshot the browser takes, and padding
        // it instead would move the cover away from the cursor.
        `width: ${size}px`,
        `height: ${size}px`,
    ].join(';');

    const img = document.createElement('img');

    img.src = coverUrl;
    img.width = size;
    img.height = size;
    img.style.cssText = [
        'display: block',
        `width: ${size}px`,
        `height: ${size}px`,
        'border-radius: 6px',
        'object-fit: cover',
        'box-shadow: 0 2px 8px rgba(0,0,0,0.4)',
    ].join(';');

    wrapper.appendChild(img);

    if (count > 1) {
        wrapper.appendChild(countBadge(count));
    }

    document.body.appendChild(wrapper);

    return wrapper;
}

/** The corner badge on a multi-track drag image. */
function countBadge(count: number): HTMLElement {
    const badge = document.createElement('span');

    badge.className = 'drag-count-badge';
    badge.textContent = String(count);
    badge.style.cssText = [
        'position: absolute',
        'top: 3px',
        'right: 3px',
        'min-width: 20px',
        'height: 20px',
        'padding: 0 5px',
        'box-sizing: border-box',
        'border-radius: 10px',
        'background: #ffd43b',
        'color: #000',
        'font-size: 12px',
        'font-weight: 600',
        'font-family: inherit',
        'line-height: 20px',
        'text-align: center',
        'box-shadow: 0 1px 4px rgba(0,0,0,0.5)',
    ].join(';');

    return badge;
}

/**
 * Creates a drag image styled like a queue track card showing the
 * track title and artist.  Used when dragging a single track.
 *
 * If `title` is empty and `filePath` is provided the filename
 * (without extension) is used as a fallback.  An empty `artist`
 * falls back to "Unknown Artist".
 */
export function createTrackCardDragImage(
    title: string,
    artist: string,
    filePath?: string,
): HTMLElement {
    let displayTitle = title;

    if (!displayTitle && filePath) {
        const parts = filePath.split(/[\\/]/);
        const filename =
            parts[parts.length - 1] ?? filePath;

        displayTitle = filename.replace(/\.[^.]+$/, '');
    }

    if (!displayTitle) {
        displayTitle = 'Unknown Title';
    }

    const displayArtist = artist || 'Unknown Artist';

    const card = document.createElement('div');

    card.style.cssText = [
        'position: fixed',
        'top: -1000px',
        'left: -1000px',
        'max-width: 220px',
        'padding: 8px 14px',
        'border-radius: 6px',
        'background: #2a2a2a',
        'box-shadow: 0 2px 8px rgba(0,0,0,0.4)',
        'pointer-events: none',
        'z-index: 9999',
        'display: flex',
        'flex-direction: column',
        'gap: 2px',
        'font-family: inherit',
    ].join(';');

    const titleEl = document.createElement('span');

    titleEl.textContent = displayTitle;
    titleEl.style.cssText = [
        'font-size: 13px',
        'color: #fff',
        'white-space: nowrap',
        'overflow: hidden',
        'text-overflow: ellipsis',
    ].join(';');

    const artistEl = document.createElement('span');

    artistEl.textContent = displayArtist;
    artistEl.style.cssText = [
        'font-size: 11px',
        'color: #b3b3b3',
        'white-space: nowrap',
        'overflow: hidden',
        'text-overflow: ellipsis',
    ].join(';');

    card.appendChild(titleEl);
    card.appendChild(artistEl);
    document.body.appendChild(card);

    return card;
}

/** Remove a drag image element created by createDragImage. */
export function removeDragImage(el: HTMLElement): void {
    el.remove();
}

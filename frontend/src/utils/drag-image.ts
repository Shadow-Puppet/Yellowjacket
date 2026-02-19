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

/** Remove a drag image element created by createDragImage. */
export function removeDragImage(el: HTMLElement): void {
    el.remove();
}

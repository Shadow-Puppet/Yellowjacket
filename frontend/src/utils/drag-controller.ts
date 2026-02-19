/**
 * Shared drag-and-drop coordination for track items.
 *
 * Uses the HTML5 Drag and Drop API with a custom MIME type so that
 * drag sources and drop targets across different shadow roots can
 * communicate.  A global custom event ("yj-drag-active") is
 * dispatched on `document` so that non-participating components
 * (sidebar, queue button) can react to the drag lifecycle.
 */

/** MIME type used in dataTransfer for in-app track drags. */
export const DRAG_MIME = 'application/x-yj-tracks';

/** Sources that can originate a drag. */
export type DragSource =
    | 'track-list'
    | 'cover-grid'
    | 'queue'
    | 'playlist';

/** Serialized payload stored in dataTransfer. */
export interface DragPayload {
    filePaths: string[];
    source: DragSource;
    sourcePlaylistId?: number;
}

// =====================================================================
// Global drag-active event
// =====================================================================

export interface DragActiveDetail {
    active: boolean;
}

/**
 * Notify the entire document that a track drag has started or ended.
 * Non-participating components listen for this to show/hide drop
 * affordances (e.g. sidebar hover-to-navigate, queue button glow).
 */
export function emitDragActive(active: boolean): void {
    document.dispatchEvent(
        new CustomEvent<DragActiveDetail>(
            'yj-drag-active',
            {
                bubbles: true,
                composed: true,
                detail: { active },
            },
        ),
    );
}

// =====================================================================
// Helpers for drag sources
// =====================================================================

/**
 * Populate a DragEvent's dataTransfer with the standard payload.
 * Returns false if dataTransfer is unavailable.
 */
export function setDragPayload(
    e: DragEvent,
    payload: DragPayload,
): boolean {
    if (!e.dataTransfer) return false;

    e.dataTransfer.effectAllowed = 'copy';
    e.dataTransfer.setData(
        DRAG_MIME,
        JSON.stringify(payload),
    );

    return true;
}

// =====================================================================
// Helpers for drop targets
// =====================================================================

/** Check whether a dragover event carries our custom MIME type. */
export function hasTrackPayload(e: DragEvent): boolean {
    return (
        e.dataTransfer?.types.includes(DRAG_MIME) ?? false
    );
}

/**
 * Extract the DragPayload from a drop event.
 * Returns null if the data is missing or malformed.
 */
export function getDragPayload(
    e: DragEvent,
): DragPayload | null {
    const raw = e.dataTransfer?.getData(DRAG_MIME);

    if (!raw) return null;

    try {
        const parsed: unknown = JSON.parse(raw);

        if (
            typeof parsed === 'object' &&
            parsed !== null &&
            'filePaths' in parsed &&
            Array.isArray(
                (parsed as DragPayload).filePaths,
            )
        ) {
            return parsed as DragPayload;
        }

        return null;
    } catch {
        return null;
    }
}

import type { library } from '@go/models';

/**
 * Discriminated context menu target so we know whether the
 * context-menu is operating on albums or on tracks inside the
 * dropdown.
 */
export type ContextMenuTarget =
    | { kind: 'album' }
    | { kind: 'track' };

/**
 * Item for the virtualized grid.
 * Carries the original album and its index in the filtered
 * album list.
 */
export interface GridEntry {
    album: library.Album;
    albumIndex: number;
}

/** Milliseconds to debounce visibility-changed saves. */
export const SCROLL_DEBOUNCE_MS = 100;

/** Pixels to change card width per scroll tick. */
export const ZOOM_STEP = 16;

/** localStorage keys for sort preferences. */
export const SORT_FIELD_KEY = 'cover-grid-sort-field';
export const SORT_DIR_KEY = 'cover-grid-sort-direction';

/** Available sort fields for the album grid. */
export type AlbumSortField = 'name' | 'artist' | 'year';

/** Sort option definition for the dropdown. */
export interface AlbumSortOption {
    id: AlbumSortField;
    label: string;
    comparator: (
        a: library.Album,
        b: library.Album,
    ) => number;
}

/** All available sort options for albums. */
export const ALBUM_SORT_OPTIONS: AlbumSortOption[] = [
    {
        id: 'name',
        label: 'Name',
        comparator: (a, b) =>
            a.Name.localeCompare(b.Name),
    },
    {
        id: 'artist',
        label: 'Artist',
        comparator: (a, b) => {
            const cmp = a.ArtistName.localeCompare(
                b.ArtistName,
            );

            if (cmp !== 0) return cmp;

            return a.Name.localeCompare(b.Name);
        },
    },
    {
        id: 'year',
        label: 'Year',
        comparator: (a, b) => {
            // Albums without a year sort last.
            if (!a.Year && !b.Year) {
                return a.Name.localeCompare(b.Name);
            }

            if (!a.Year) return 1;
            if (!b.Year) return -1;

            const cmp = a.Year - b.Year;

            if (cmp !== 0) return cmp;

            return a.Name.localeCompare(b.Name);
        },
    },
];

/** Sort direction for the album grid. */
export type SortDirection = 'asc' | 'desc';

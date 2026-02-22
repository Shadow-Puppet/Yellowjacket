import type { library } from '@go/models';
import { formatMilliseconds } from '@utils/time';

/** Compares two strings using locale-aware ordering. */
const compareStr = (
    a: string,
    b: string,
): number => a.localeCompare(b);

/** Compares two numbers, treating 0 as "empty" (sorted last). */
const compareNum = (a: number, b: number): number => {
    if (!a && !b) return 0;
    if (!a) return 1;
    if (!b) return -1;

    return a - b;
};

/** Definition for a single displayable column. */
export interface ColumnDef {
    /** Unique identifier matching the backend ColumnID. */
    id: string;
    /** Human-readable header label. */
    label: string;
    /** Extracts the display value from a track. */
    accessor: (track: library.Track) => string;
    /** Default CSS width (used when no saved width exists). */
    defaultWidth: string;
    /** Text alignment. Defaults to left. */
    align?: 'left' | 'right';
    /**
     * Comparison function for sorting two tracks by this column.
     * Returns negative if a < b, positive if a > b, zero if equal.
     * If omitted the column is not sortable.
     */
    comparator?: (
        a: library.Track,
        b: library.Track,
    ) => number;
}

/** Registry of every available column keyed by ID. */
export const COLUMN_DEFS: Record<string, ColumnDef> = {
    trackName: {
        id: 'trackName',
        label: 'Track Name',
        accessor: (t) => t.TrackName,
        defaultWidth: '1fr',
        comparator: (a, b) =>
            compareStr(a.TrackName, b.TrackName),
    },
    artistName: {
        id: 'artistName',
        label: 'Artist',
        accessor: (t) => t.ArtistName,
        defaultWidth: '1fr',
        comparator: (a, b) =>
            compareStr(a.ArtistName, b.ArtistName),
    },
    trackLength: {
        id: 'trackLength',
        label: 'Duration',
        accessor: (t) => formatMilliseconds(t.TrackLength),
        defaultWidth: '80px',
        comparator: (a, b) =>
            Number(a.TrackLength) - Number(b.TrackLength),
    },
    album: {
        id: 'album',
        label: 'Album',
        accessor: (t) => t.Album,
        defaultWidth: '1fr',
        comparator: (a, b) =>
            compareStr(a.Album, b.Album),
    },
    genre: {
        id: 'genre',
        label: 'Genre',
        accessor: (t) => (t.Genre ?? []).join(', '),
        defaultWidth: '120px',
        comparator: (a, b) =>
            compareStr(
                (a.Genre ?? []).join(', '),
                (b.Genre ?? []).join(', '),
            ),
    },
    year: {
        id: 'year',
        label: 'Year',
        accessor: (t) =>
            t.Year ? String(t.Year) : '',
        defaultWidth: '60px',
        comparator: (a, b) =>
            compareNum(a.Year, b.Year),
    },
    composer: {
        id: 'composer',
        label: 'Composer',
        accessor: (t) => t.Composer,
        defaultWidth: '1fr',
        comparator: (a, b) =>
            compareStr(a.Composer, b.Composer),
    },
    trackNumber: {
        id: 'trackNumber',
        label: 'Track #',
        accessor: (t) =>
            t.TrackNumber ? String(t.TrackNumber) : '',
        defaultWidth: '60px',
        comparator: (a, b) =>
            compareNum(a.TrackNumber, b.TrackNumber),
    },
    discNumber: {
        id: 'discNumber',
        label: 'Disc #',
        accessor: (t) =>
            t.DiscNumber ? String(t.DiscNumber) : '',
        defaultWidth: '60px',
        comparator: (a, b) =>
            compareNum(a.DiscNumber, b.DiscNumber),
    },
    filePath: {
        id: 'filePath',
        label: 'File Path',
        accessor: (t) => t.FilePath,
        defaultWidth: '1fr',
        comparator: (a, b) =>
            compareStr(a.FilePath, b.FilePath),
    },
    fileType: {
        id: 'fileType',
        label: 'File Type',
        accessor: (t) => t.FileType,
        defaultWidth: '80px',
        comparator: (a, b) =>
            compareStr(a.FileType, b.FileType),
    },
};

/**
 * All column IDs in default display order.
 * Used by the settings UI to list available columns.
 */
export const ALL_COLUMN_IDS: string[] = Object.keys(COLUMN_DEFS);

/** Default column IDs matching the original hardcoded layout. */
export const DEFAULT_COLUMN_IDS: string[] = [
    'trackName',
    'artistName',
    'trackLength',
];

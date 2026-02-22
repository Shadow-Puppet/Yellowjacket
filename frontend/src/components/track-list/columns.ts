import type { library } from '@go/models';
import { formatMilliseconds } from '@utils/time';

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
}

/** Registry of every available column keyed by ID. */
export const COLUMN_DEFS: Record<string, ColumnDef> = {
    trackName: {
        id: 'trackName',
        label: 'Track Name',
        accessor: (t) => t.TrackName,
        defaultWidth: '1fr',
    },
    artistName: {
        id: 'artistName',
        label: 'Artist',
        accessor: (t) => t.ArtistName,
        defaultWidth: '1fr',
    },
    trackLength: {
        id: 'trackLength',
        label: 'Duration',
        accessor: (t) => formatMilliseconds(t.TrackLength),
        defaultWidth: '80px',
    },
    album: {
        id: 'album',
        label: 'Album',
        accessor: (t) => t.Album,
        defaultWidth: '1fr',
    },
    genre: {
        id: 'genre',
        label: 'Genre',
        accessor: (t) => (t.Genre ?? []).join(', '),
        defaultWidth: '120px',
    },
    year: {
        id: 'year',
        label: 'Year',
        accessor: (t) =>
            t.Year ? String(t.Year) : '',
        defaultWidth: '60px',
    },
    composer: {
        id: 'composer',
        label: 'Composer',
        accessor: (t) => t.Composer,
        defaultWidth: '1fr',
    },
    trackNumber: {
        id: 'trackNumber',
        label: 'Track #',
        accessor: (t) =>
            t.TrackNumber ? String(t.TrackNumber) : '',
        defaultWidth: '60px',
    },
    discNumber: {
        id: 'discNumber',
        label: 'Disc #',
        accessor: (t) =>
            t.DiscNumber ? String(t.DiscNumber) : '',
        defaultWidth: '60px',
    },
    filePath: {
        id: 'filePath',
        label: 'File Path',
        accessor: (t) => t.FilePath,
        defaultWidth: '1fr',
    },
    fileType: {
        id: 'fileType',
        label: 'File Type',
        accessor: (t) => t.FileType,
        defaultWidth: '80px',
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

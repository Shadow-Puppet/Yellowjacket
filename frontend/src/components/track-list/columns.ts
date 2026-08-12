import type { library } from '@go/models';
import {
    formatSampleRate,
    formatBitDepth,
    formatChannels,
    formatBitrate,
    formatFileSize,
} from '@utils/format';
import { formatMilliseconds } from '@utils/time';
import { html, nothing } from 'lit';

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
    /** Optional custom render function returning an HTML template. */
    renderCell?: (track: library.Track) => unknown;
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
    albumArt: {
        id: 'albumArt',
        label: 'Art',
        accessor: () => '',
        defaultWidth: '36px',
        renderCell: (track: library.Track) => {
            // `perf.M3`. This rendered `CoverArtPath` — the *original*
            // embedded artwork, commonly 1500×1500 and several hundred
            // kB — scaled by CSS into a 24 px box, while the 100 px
            // `CoverArtSmall` sat unused on the same model. Every row
            // the virtualizer recycled into view decoded a full
            // resolution JPEG on the main thread to draw 576 pixels.
            //
            // `cover-grid.getCoverUrl()` has picked the right tier all
            // along; this is the same rule for a much smaller box, with
            // the two attributes that keep the decode off the scroll
            // path.
            const src = track.CoverArtSmall
                || track.CoverArtMedium
                || track.CoverArtPath;

            if (!src) return nothing;

            return html`<img
                src="${src}"
                alt=""
                loading="lazy"
                decoding="async"
                width="24"
                height="24"
                style="width:24px;height:24px;border-radius:3px;object-fit:cover;display:block;"
            />`;
        },
    },
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
    sampleRate: {
        id: 'sampleRate',
        label: 'Sample Rate',
        accessor: (t) => formatSampleRate(t.SampleRate),
        defaultWidth: '100px',
        align: 'right',
        comparator: (a, b) =>
            compareNum(a.SampleRate, b.SampleRate),
    },
    bitDepth: {
        id: 'bitDepth',
        label: 'Bit Depth',
        accessor: (t) => formatBitDepth(t.BitDepth),
        defaultWidth: '80px',
        align: 'right',
        comparator: (a, b) =>
            compareNum(a.BitDepth, b.BitDepth),
    },
    channels: {
        id: 'channels',
        label: 'Channels',
        accessor: (t) => formatChannels(t.Channels),
        defaultWidth: '80px',
        comparator: (a, b) =>
            compareNum(a.Channels, b.Channels),
    },
    bitrate: {
        id: 'bitrate',
        label: 'Bitrate',
        accessor: (t) => formatBitrate(t.Bitrate),
        defaultWidth: '100px',
        align: 'right',
        comparator: (a, b) =>
            compareNum(a.Bitrate, b.Bitrate),
    },
    fileSize: {
        id: 'fileSize',
        label: 'File Size',
        accessor: (t) => formatFileSize(t.FileSize),
        defaultWidth: '80px',
        align: 'right',
        comparator: (a, b) =>
            compareNum(a.FileSize, b.FileSize),
    },
    playCount: {
        id: 'playCount',
        label: 'Play Count',
        accessor: (t) => t.PlayCount > 0 ? `${t.PlayCount}` : '0',
        defaultWidth: '80px',
        comparator: (a, b) =>
            compareNum(a.PlayCount, b.PlayCount),
    },
};

/**
 * All column IDs in default display order.
 * Used by the settings UI to list available columns.
 */
export const ALL_COLUMN_IDS: string[] = Object.keys(COLUMN_DEFS);

/**
 * Column IDs that are always searched regardless of visibility.
 * These represent the most common search targets.
 */
export const CORE_SEARCH_COLUMN_IDS: string[] = [
    'trackName',
    'artistName',
    'album',
];

/** Default column IDs matching the original hardcoded layout. */
export const DEFAULT_COLUMN_IDS: string[] = [
    'trackName',
    'artistName',
    'trackLength',
];

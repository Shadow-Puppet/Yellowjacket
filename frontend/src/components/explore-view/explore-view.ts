import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query as litQuery } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import { Search, GetThumbnail, GetThumbnails, GetArtistImageURL, GetPopularityBatch, RecordSearchClick } from '@go/explore/Service';
import type { ThumbnailRequest } from '@go/explore/Service';
import type {
    MBSearchResult,
    MBArtist,
    MBReleaseGroup,
    MBRecording,
} from '@go/explore/Service';
import { libraryStore } from '../../store/library-store';
import { exploreCache } from '../../store/explore-cache';
import { exploreSettings } from '../../store/explore-settings';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '../library-status-indicator/library-status-indicator.js';
import '../top-results-row/top-results-row.js';
import type { explore } from '@go/models';

/* ── Constants ── */
const DEBOUNCE_MS = 300;
const MIN_QUERY_LENGTH = 2;
const FUZZY_MAX_DISTANCE = 2;

/* ── Fuzzy matching ── */

/** Levenshtein edit distance between two strings. */
function editDistance(a: string, b: string): number {
    if (a.length === 0) return b.length;
    if (b.length === 0) return a.length;

    const matrix: number[][] = [];

    for (let i = 0; i <= a.length; i++) matrix[i] = [i];
    for (let j = 0; j <= b.length; j++) matrix[0][j] = j;

    for (let i = 1; i <= a.length; i++) {
        for (let j = 1; j <= b.length; j++) {
            const cost = a[i - 1] === b[j - 1] ? 0 : 1;
            matrix[i][j] = Math.min(
                matrix[i - 1][j] + 1,
                matrix[i][j - 1] + 1,
                matrix[i - 1][j - 1] + cost,
            );
        }
    }

    return matrix[a.length][b.length];
}

/**
 * Check if a name fuzzy-matches a query.  Returns true if:
 * - the name contains the query as a substring (exact), OR
 * - any word-aligned segment of the name is within edit distance
 *   FUZZY_MAX_DISTANCE of the query
 */
function fuzzyMatch(query: string, name: string): boolean {
    if (name.includes(query)) return true;

    // Split both into words and check if all query words match
    // a name word within edit distance (handles per-word typos).
    const qWords = query.split(/\s+/);
    const nWords = name.split(/\s+/);

    return qWords.every((qw) =>
        nWords.some(
            (nw) =>
                nw.includes(qw) ||
                (nw.length >= 3 && qw.includes(nw)) ||
                (qw.length >= 4 && nw.length >= 4 && editDistance(qw, nw) <= FUZZY_MAX_DISTANCE),
        ),
    );
}
const MAX_SECTION_RESULTS = 10;
const CAA_GROUP_BASE = 'https://coverartarchive.org/release-group';

/**
 * Build a Cover Art Archive URL for a release-group front cover.
 * Mirrors the Wails CoverArtGroupURL binding but runs synchronously
 * on the frontend — avoids N async round-trips per render cycle.
 */
function CoverArtGroupURL(releaseGroupMBID: string): string {
    return `${CAA_GROUP_BASE}/${releaseGroupMBID}/front-250`;
}

/** Hash a string to a hue value 0–360 for avatar coloring. */
function nameToHue(name: string): number {
    let hash = 0;
    for (let i = 0; i < name.length; i++) {
        hash = name.charCodeAt(i) + ((hash << 5) - hash);
    }
    return Math.abs(hash) % 360;
}

/** Format milliseconds as mm:ss. */
function formatDuration(ms: number): string {
    if (!ms || ms <= 0) return '';
    const totalSeconds = Math.floor(ms / 1000);
    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;
    return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

/** Format a raw listen count as a compact human-readable string. */
function formatPopularity(count: number): string {
    if (!count || count <= 0) return '';
    if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M plays`;
    if (count >= 1_000) return `${(count / 1_000).toFixed(count >= 10_000 ? 0 : 1)}K plays`;
    return `${count} plays`;
}

/**
 * Check if `text` contains `word` as a whole word, bounded by
 * spaces, hyphens, or string boundaries.  Both args must be
 * pre-lowercased.
 */
function containsWord(text: string, word: string): boolean {
    const re = new RegExp(`(?:^|[\\s\\-])${escapeRegExp(word)}(?:$|[\\s\\-])`, 'i');
    return re.test(text);
}

function escapeRegExp(s: string): string {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** Parse a TrackLength string (milliseconds as string) to number. */
function parseDuration(s: string): number {
    if (!s) return 0;
    const n = Number(s);
    return isNaN(n) ? 0 : n;
}

/** Extract the year from a date string like "2005-03-29" or "2005". */
function extractYear(dateStr: string): string {
    if (!dateStr) return '';
    return dateStr.substring(0, 4);
}

/**
 * Find album cover art for an artist from the library store.
 * Returns the best available cover art URL, or '' if none.
 */
function getArtistAlbumArt(artistName: string): string {
    const cachedAlbums = libraryStore.cachedAlbums;
    if (!cachedAlbums) return '';

    const name = artistName.toLowerCase();

    for (const a of cachedAlbums) {
        if (a.ArtistName.toLowerCase() === name) {
            const art = a.CoverArtMedium || a.CoverArtSmall || a.CoverArtPath;
            if (art) return art;
        }
    }

    return '';
}

@customElement('explore-view')
export class ExploreView extends LitElement {
    /* ── State ── */

    @state() private searchQuery = '';
    @state() private results: MBSearchResult | null = null;
    @state() private loading = false;
    @state() private error = '';
    @state() private queryTooShort = false;

    /** Monotonic counter to discard stale responses. */
    private searchVersion = 0;
    private debounceTimer: ReturnType<typeof setTimeout> | null = null;
    private thumbnailCache = new Map<string, string>();
    private artistImageCache = new Map<string, string>();
    private libraryMBIDs = new Set<string>();

    @litQuery('input') private inputEl!: HTMLInputElement;

    /* ── Styles ── */

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
                padding: 24px;
                overflow-y: auto;
                height: 100%;
                box-sizing: border-box;
            }

            /* ── Search input ── */
            .search-container {
                display: flex;
                align-items: center;
                background: var(--yj-bg-surface, #212529);
                border: 1px solid var(--yj-border-subtle, #555);
                border-radius: 6px;
                padding: 0 12px;
                gap: 8px;
                height: 36px;
                max-width: 520px;
                transition: border-color 0.15s ease;
            }

            .search-container:focus-within {
                border-color: var(--yj-accent, #ffd43b);
            }

            .search-icon {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-icon-sm);
                flex-shrink: 0;
            }

            input {
                flex: 1;
                background: none;
                border: none;
                outline: none;
                color: var(--yj-text-primary, #fff);
                font-size: var(--yj-text-md);
                font-family: inherit;
                min-width: 0;
            }

            input::placeholder {
                color: var(--yj-text-tertiary, #888);
            }

            .clear-button {
                display: flex;
                align-items: center;
                justify-content: center;
                background: none;
                border: none;
                color: var(--yj-text-tertiary, #888);
                cursor: pointer;
                padding: 0;
                font-size: var(--yj-text-sm);
                flex-shrink: 0;
            }

            .clear-button:hover {
                color: var(--yj-text-primary, #fff);
            }

            /* ── Status messages ── */
            .status-message {
                margin-top: 32px;
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: var(--yj-text-md);
            }

            .error-message {
                margin-top: 16px;
                display: flex;
                align-items: center;
                gap: 6px;
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: var(--yj-text-sm);
            }

            .error-message wa-icon {
                color: #e5534b;
                font-size: var(--yj-icon-sm);
                flex-shrink: 0;
            }

            .loading-indicator {
                margin-top: 24px;
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: var(--yj-text-md);
                animation: pulse 1.5s ease-in-out infinite;
            }

            @keyframes pulse {
                0%, 100% { opacity: 1; }
                50% { opacity: 0.5; }
            }

            /* ── Sections ── */
            .results-container {
                margin-top: 24px;
                display: flex;
                flex-direction: column;
                gap: 28px;
            }

            .section-header {
                font-size: var(--yj-text-lg);
                font-weight: 600;
                color: var(--yj-text-secondary, #b3b3b3);
                margin: 0 0 12px;
                text-transform: uppercase;
                letter-spacing: 0.05em;
                font-size: 11px;
            }

            /* ── Horizontal scroll rows ── */
            .horizontal-row {
                display: flex;
                gap: 12px;
                overflow-x: auto;
                padding-bottom: 4px;
                scrollbar-width: none;
            }

            .horizontal-row::-webkit-scrollbar {
                display: none;
            }

            /* ── Top result cards ── */
            /* ── Artist cards ── */
            .artist-card {
                display: flex;
                flex-direction: column;
                align-items: center;
                gap: 8px;
                padding: 10px;
                border-radius: 8px;
                cursor: pointer;
                min-width: 100px;
                max-width: 120px;
                flex-shrink: 0;
                text-align: center;
                transition: background 0.15s ease;
            }

            .artist-card:hover {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
            }

            .artist-card:active {
                transform: scale(0.97);
            }

            .artist-avatar {
                width: 48px;
                height: 48px;
                border-radius: 50%;
                display: flex;
                align-items: center;
                justify-content: center;
                color: #fff;
                font-weight: 600;
                font-size: 20px;
                text-transform: uppercase;
                user-select: none;
                flex-shrink: 0;
                overflow: hidden;
            }

            .artist-avatar img {
                width: 100%;
                height: 100%;
                object-fit: cover;
            }

            .artist-name {
                font-weight: 500;
                color: var(--yj-text-primary, #fff);
                font-size: var(--yj-text-sm);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                width: 100%;
            }

            .artist-native-name {
                color: var(--yj-text-secondary, #aaa);
                font-size: var(--yj-text-xs);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                width: 100%;
                margin-top: -4px;
            }

            .artist-disambiguation {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-xs);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                width: 100%;
                margin-top: -4px;
            }

            .artist-country {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-xs);
            }

            /* ── Album cards ── */
            .album-card {
                display: flex;
                flex-direction: column;
                gap: 6px;
                padding: 8px;
                border-radius: 8px;
                cursor: pointer;
                min-width: 130px;
                max-width: 150px;
                flex-shrink: 0;
                transition: background 0.15s ease;
            }

            .album-card:hover {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
            }

            .album-card:active {
                transform: scale(0.97);
            }

            .album-art-container {
                width: 100%;
                aspect-ratio: 1;
                border-radius: 4px;
                overflow: hidden;
                background: linear-gradient(
                    135deg,
                    var(--yj-bg-overlay, #404040) 0%,
                    var(--yj-bg-surface, #282828) 100%
                );
                display: flex;
                align-items: center;
                justify-content: center;
                position: relative;
            }

            .album-art-container img {
                width: 100%;
                height: 100%;
                object-fit: cover;
                display: block;
            }

            .album-art-fallback {
                display: flex;
                align-items: center;
                justify-content: center;
                width: 100%;
                height: 100%;
                position: absolute;
                inset: 0;
            }

            .album-art-fallback wa-icon {
                color: var(--yj-text-tertiary, #888);
                font-size: 24px;
                opacity: 0.5;
            }

            .album-title {
                font-weight: 500;
                color: var(--yj-text-primary, #fff);
                font-size: var(--yj-text-sm);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .album-artist {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-xs);
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
                min-height: 20px;
            }

            .album-meta-text {
                display: flex;
                align-items: center;
                gap: 6px;
                min-width: 0;
                overflow: hidden;
            }

            .album-meta library-status-indicator {
                flex-shrink: 0;
                margin-left: auto;
            }

            .type-badge {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
                padding: 1px 6px;
                border-radius: 3px;
                font-size: 10px;
                white-space: nowrap;
            }

            /* ── Track list ── */
            .track-list {
                display: flex;
                flex-direction: column;
            }

            .track-item {
                display: flex;
                align-items: center;
                gap: 12px;
                padding: 8px 12px;
                border-radius: 6px;
                cursor: default;
                transition: background 0.1s ease;
            }

            .track-item:hover {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.04));
            }

            .track-info {
                flex: 1;
                min-width: 0;
            }

            .track-title {
                font-weight: 500;
                color: var(--yj-text-primary, #fff);
                font-size: var(--yj-text-md);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .track-artist {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-sm);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .track-duration {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-sm);
                flex-shrink: 0;
                font-variant-numeric: tabular-nums;
            }

            .track-meta {
                display: flex;
                align-items: center;
                gap: 10px;
                flex-shrink: 0;
            }

            .track-item library-status-indicator {
                flex-shrink: 0;
            }

            .track-popularity {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-xs, 11px);
                white-space: nowrap;
                opacity: 0.7;
            }
        `,
    ];

    /* ── Lifecycle ── */

    private unsubSettings?: () => void;

    override connectedCallback() {
        super.connectedCallback();
        // Re-render and re-search when library-only mode toggles.
        this.unsubSettings = exploreSettings.subscribe(() => {
            this.requestUpdate();
            // Re-run the current search with the new mode.
            if (this.searchQuery.trim().length >= MIN_QUERY_LENGTH) {
                void this.executeSearch();
            }
        });
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.unsubSettings?.();
        if (this.debounceTimer !== null) {
            clearTimeout(this.debounceTimer);
            this.debounceTimer = null;
        }
    }

    /* ── Search Logic ── */

    private handleInput(e: Event) {
        const input = e.target as HTMLInputElement;
        this.searchQuery = input.value;

        if (this.debounceTimer !== null) {
            clearTimeout(this.debounceTimer);
            this.debounceTimer = null;
        }

        const trimmed = this.searchQuery.trim();

        if (!trimmed) {
            this.results = null;
            this.error = '';
            this.loading = false;
            this.queryTooShort = false;
            return;
        }

        if (trimmed.length < MIN_QUERY_LENGTH) {
            this.results = null;
            this.error = '';
            this.loading = false;
            this.queryTooShort = true;
            return;
        }

        this.queryTooShort = false;

        this.debounceTimer = setTimeout(() => {
            this.debounceTimer = null;
            void this.executeSearch();
        }, DEBOUNCE_MS);
    }

    private handleClear() {
        this.searchQuery = '';
        this.results = null;
        this.error = '';
        this.loading = false;
        this.queryTooShort = false;
        if (this.debounceTimer !== null) {
            clearTimeout(this.debounceTimer);
            this.debounceTimer = null;
        }
        if (this.inputEl) {
            this.inputEl.value = '';
            this.inputEl.focus();
        }
    }

    private handleKeydown(e: KeyboardEvent) {
        if (e.key === 'Escape') {
            this.handleClear();
        }
    }

    private async executeSearch() {
        const version = ++this.searchVersion;
        const query = this.searchQuery.trim();
        if (!query) return;

        this.loading = true;
        this.error = '';

        const startTime = performance.now();
        console.log(`[explore] search started: "${query}"`);

        // Phase 1: instant library search — pure frontend, no Go calls.
        const localResults = this.searchLibraryCache(query);
        if (localResults && (localResults.artists?.length || localResults.releaseGroups?.length)) {
            this.results = localResults;
            exploreCache.populateFromSearch(
                localResults.artists || [],
                localResults.releaseGroups || [],
            );

            // Seed artist image cache from library data.
            for (const a of localResults.artists || []) {
                const img = (a as any)._imageMedium || (a as any)._imageSmall;
                if (img && a.mbid) {
                    this.artistImageCache.set(a.mbid, img);
                }
            }

            // Fallback: album art for artists without images.
            for (const a of localResults.artists || []) {
                if (a.mbid && !this.artistImageCache.get(a.mbid)) {
                    const albumArt = getArtistAlbumArt(a.name);
                    if (albumArt) {
                        this.artistImageCache.set(a.mbid, albumArt);
                    }
                }
            }

            // In library-only mode, local results already have cover art
            // and artist images from the library store — seed both caches
            // from library data without making any API calls.
            if (exploreSettings.libraryOnly) {
                this.seedThumbnailsFromLibrary();
                this.seedArtistImagesFromLibrary();
            } else {
                this.loadThumbnails();
                this.loadArtistImages();
            }
            const elapsed = (performance.now() - startTime).toFixed(0);
            console.log(
                `[explore] library results: "${query}" in ${elapsed}ms — ` +
                    `artists=${localResults.artists?.length ?? 0}, ` +
                    `albums=${localResults.releaseGroups?.length ?? 0}`,
            );
        }

        // Phase 2: full pipeline (MB + LB + reranking) via Wails RPC.
        // Skip entirely in library-only mode — local results are final.
        if (!exploreSettings.libraryOnly) {
            void this.executeFullSearch(version, query, startTime);
        } else {
            // Rerank with popularity from the explore index, then finalize.
            void this.rerankWithPopularity(localResults).then(() => {
                this.loading = false;
            });
        }
    }

    /**
     * Search the frontend library cache for matching artists and albums.
     * Pure JS — no Go calls, guaranteed instant.  Returns results with
     * MBIDs and local cover art so they can navigate to explore pages.
     */
    private searchLibraryCache(query: string): MBSearchResult | null {
        const q = query.toLowerCase();

        // Collect all matching artists with match-quality scores.
        const artistMatches: Array<{ artist: any; score: number }> = [];
        const cachedArtists = libraryStore.cachedArtists;
        if (cachedArtists) {
            for (const a of cachedArtists) {
                const name = a.Name.toLowerCase();
                if (!fuzzyMatch(q, name)) continue;

                // Score by match quality.
                let score: number;
                if (name === q) {
                    score = 100; // exact
                } else if (name.startsWith(q)) {
                    score = 90;  // starts with
                } else if (containsWord(name, q)) {
                    score = 75;  // contains word
                } else if (name.includes(q)) {
                    score = 60;  // substring
                } else {
                    score = 40;  // fuzzy/word match
                }

                artistMatches.push({ artist: a, score });
            }
        }

        // Sort by score descending, then alphabetically.
        artistMatches.sort((a, b) => b.score - a.score || a.artist.Name.localeCompare(b.artist.Name));

        const artists: MBArtist[] = artistMatches.slice(0, 10).map((m) => ({
            mbid: m.artist.MBID || '',
            name: m.artist.Name,
            sortName: '',
            type: '',
            country: '',
            disambiguation: '',
            score: m.score,
            inLibrary: true,
            localId: m.artist.ID,
            _imageSmall: m.artist.ImageSmall || '',
            _imageMedium: m.artist.ImageMedium || '',
            _inLibrary: true,
        } as MBArtist & { _imageSmall: string; _imageMedium: string; _inLibrary: boolean }));

        // Collect all matching albums with match-quality scores.
        const albumMatches: Array<{ album: any; score: number }> = [];
        const cachedAlbums = libraryStore.cachedAlbums;
        if (cachedAlbums) {
            for (const a of cachedAlbums) {
                const name = a.Name.toLowerCase();
                const artist = a.ArtistName.toLowerCase();
                const matchesName = fuzzyMatch(q, name);
                const matchesArtist = fuzzyMatch(q, artist);
                if (!matchesName && !matchesArtist) continue;

                let score: number;
                if (artist === q) {
                    score = 100;
                } else if (name === q) {
                    score = 95;
                } else if (artist.startsWith(q)) {
                    score = 88;
                } else if (name.startsWith(q)) {
                    score = 85;
                } else if (containsWord(artist, q)) {
                    score = 78;
                } else if (containsWord(name, q)) {
                    score = 75;
                } else if (artist.includes(q)) {
                    score = 65;
                } else if (name.includes(q)) {
                    score = 60;
                } else {
                    score = 40;
                }

                albumMatches.push({ album: a, score });
            }
        }

        albumMatches.sort((a, b) => b.score - a.score || a.album.Name.localeCompare(b.album.Name));

        const releaseGroups: MBReleaseGroup[] = albumMatches.slice(0, 10).map((m) => ({
            mbid: m.album.MBID || '',
            title: m.album.Name,
            primaryType: 'Album',
            artistCredit: m.album.ArtistName,
            firstReleaseDate: m.album.Year ? String(m.album.Year) : '',
            _coverArt: m.album.CoverArtMedium || m.album.CoverArtSmall || '',
            _inLibrary: true,
        } as MBReleaseGroup & { _coverArt: string; _inLibrary: boolean }));

        // Collect matching tracks by title or artist name.
        const trackMatches: Array<{ track: any; score: number }> = [];
        const cachedTracks = libraryStore.getCachedTracks();
        if (cachedTracks) {
            for (const t of cachedTracks) {
                const title = t.TrackName.toLowerCase();
                const artist = t.ArtistName.toLowerCase();
                const matchesTitle = fuzzyMatch(q, title);
                const matchesArtist = fuzzyMatch(q, artist);
                if (!matchesTitle && !matchesArtist) continue;

                let score: number;
                if (title === q) {
                    score = 100;
                } else if (artist === q) {
                    score = 95;
                } else if (title.startsWith(q)) {
                    score = 88;
                } else if (artist.startsWith(q)) {
                    score = 85;
                } else if (containsWord(title, q)) {
                    score = 78;
                } else if (containsWord(artist, q)) {
                    score = 75;
                } else if (title.includes(q)) {
                    score = 65;
                } else if (artist.includes(q)) {
                    score = 60;
                } else {
                    score = 40;
                }

                trackMatches.push({ track: t, score });
            }
        }

        trackMatches.sort((a, b) => b.score - a.score || a.track.TrackName.localeCompare(b.track.TrackName));

        // Deduplicate by recording MBID (keep highest score).
        const seenRecMBIDs = new Set<string>();
        const recordings: MBRecording[] = [];
        for (const m of trackMatches) {
            if (recordings.length >= 15) break;
            const mbid = m.track.RecordingMBID || '';
            if (mbid && seenRecMBIDs.has(mbid)) continue;
            if (mbid) seenRecMBIDs.add(mbid);

            recordings.push({
                mbid,
                title: m.track.TrackName,
                length: parseDuration(m.track.TrackLength),
                artistCredit: m.track.ArtistName,
                score: m.score,
            } as MBRecording);
        }

        if (artists.length === 0 && releaseGroups.length === 0 && recordings.length === 0) {
            return null;
        }

        return { artists, releaseGroups, recordings } as MBSearchResult;
    }

    /**
     * Fetch LB popularity for all MBIDs in the result and re-sort
     * each category using a blended score: match quality + log-scaled
     * popularity.  Same approach as the backend reranker.
     */
    private async rerankWithPopularity(result: MBSearchResult | null): Promise<void> {
        if (!result) return;

        // Collect all non-empty MBIDs.
        const mbids: string[] = [];
        for (const a of result.artists) if (a.mbid) mbids.push(a.mbid);
        for (const rg of result.releaseGroups) if (rg.mbid) mbids.push(rg.mbid);
        for (const r of result.recordings) if (r.mbid) mbids.push(r.mbid);

        if (mbids.length === 0) return;

        let batch: Record<string, {popularity: number; inLibrary: boolean; similarityScore: number}>;
        try {
            batch = await GetPopularityBatch(mbids);
        } catch {
            return; // degrade gracefully — keep match-quality order
        }

        if (!batch || Object.keys(batch).length === 0) return;

        // Weights matching backend: 0.35 relevance + 0.50 popularity + 0.15 personalization
        const maxPop = Math.max(1, ...Object.values(batch).map(b => b.popularity));
        const logMax = Math.log10(maxPop + 1);
        const maxSim = Math.max(1, ...Object.values(batch).map(b => b.similarityScore || 0));

        const blendedScore = (mbid: string, matchScore: number): number => {
            const b = batch[mbid];
            const relevance = matchScore / 100;
            const logPop = b ? Math.log10(b.popularity + 1) / logMax : 0;
            let personal = 0;
            if (b?.inLibrary) {
                personal = 1.0;
            } else if (b?.similarityScore && maxSim > 0) {
                personal = 0.5 * (b.similarityScore / maxSim);
            }
            return 0.35 * relevance + 0.50 * logPop + 0.15 * personal;
        };

        // Re-sort each category.
        result.artists.sort((a, b) =>
            blendedScore(b.mbid, b.score) - blendedScore(a.mbid, a.score));
        result.releaseGroups.sort((a, b) =>
            blendedScore(b.mbid, b.score) - blendedScore(a.mbid, a.score));
        result.recordings.sort((a, b) =>
            blendedScore(b.mbid, b.score) - blendedScore(a.mbid, a.score));

        // Trigger re-render.
        this.results = { ...result };
    }

    /**
     * Merge full search results with library data: library entries
     * take priority (local art, "In Library" badge).  MB-only results
     * are appended after library matches.
     */
    private mergeWithLibrary(result: MBSearchResult): MBSearchResult {
        const cachedArtists = libraryStore.cachedArtists;
        const cachedAlbums = libraryStore.cachedAlbums;

        // Build MBID→library lookups.
        const libArtistsByMBID = new Map<string, typeof cachedArtists extends (infer T)[] | null ? T : never>();
        const libArtistsByName = new Map<string, typeof cachedArtists extends (infer T)[] | null ? T : never>();
        if (cachedArtists) {
            for (const a of cachedArtists) {
                if (a.MBID) libArtistsByMBID.set(a.MBID, a);
                libArtistsByName.set(a.Name.toLowerCase(), a);
            }
        }

        const libAlbumsByMBID = new Map<string, typeof cachedAlbums extends (infer T)[] | null ? T : never>();
        if (cachedAlbums) {
            for (const a of cachedAlbums) {
                if (a.MBID) libAlbumsByMBID.set(a.MBID, a);
            }
        }

        // Enrich artists: if MB result matches a library artist, add local images.
        if (result.artists) {
            for (let i = 0; i < result.artists.length; i++) {
                const a = result.artists[i];
                const lib = (a.mbid && libArtistsByMBID.get(a.mbid)) ||
                    libArtistsByName.get(a.name.toLowerCase());
                if (lib) {
                    (a as any)._imageSmall = lib.ImageSmall || '';
                    (a as any)._imageMedium = lib.ImageMedium || '';
                    (a as any)._inLibrary = true;
                }
            }
        }

        // Enrich release groups: if MB result matches a library album, use local art.
        if (result.releaseGroups) {
            for (let i = 0; i < result.releaseGroups.length; i++) {
                const rg = result.releaseGroups[i];
                const lib = rg.mbid ? libAlbumsByMBID.get(rg.mbid) : undefined;
                if (lib) {
                    (rg as any)._coverArt = lib.CoverArtMedium || lib.CoverArtSmall || '';
                    (rg as any)._inLibrary = true;
                }
            }
        }

        return result;
    }

    /**
     * Merge library-only results from this.results into the full
     * search result.  Adds local artists/albums that the MB search
     * didn't find (by name dedup) so they aren't lost.
     */
    private mergeLocalIntoFull(full: MBSearchResult) {
        const prev = this.results;
        if (!prev) return;

        // Dedup artists by name (case-insensitive).
        if (prev.artists?.length) {
            const existing = new Set(
                (full.artists || []).map((a) => a.name.toLowerCase()),
            );
            for (const a of prev.artists) {
                if (!existing.has(a.name.toLowerCase())) {
                    full.artists = full.artists || [];
                    full.artists.push(a);
                }
            }
        }

        // Dedup albums by title + artist (case-insensitive).
        if (prev.releaseGroups?.length) {
            const existing = new Set(
                (full.releaseGroups || []).map(
                    (rg) => `${rg.title}|${rg.artistCredit}`.toLowerCase(),
                ),
            );
            for (const rg of prev.releaseGroups) {
                const key = `${rg.title}|${rg.artistCredit}`.toLowerCase();
                if (!existing.has(key)) {
                    full.releaseGroups = full.releaseGroups || [];
                    full.releaseGroups.push(rg);
                }
            }
        }
    }

    private async executeFullSearch(version: number, query: string, startTime: number) {
        try {
            const result = await Search(query);

            // Discard stale response
            if (version !== this.searchVersion) {
                console.debug(
                    `[explore] discarded stale response for "${query}" (v${version} != v${this.searchVersion})`,
                );
                return;
            }

            const merged = this.mergeWithLibrary(result);

            // If the full search returned results, use them.
            // If it returned nothing but we had local results, keep those.
            const hasFullResults =
                (merged.artists?.length ?? 0) > 0 ||
                (merged.releaseGroups?.length ?? 0) > 0 ||
                (merged.recordings?.length ?? 0) > 0;
            const hadLocalResults = this.results &&
                ((this.results.artists?.length ?? 0) > 0 ||
                 (this.results.releaseGroups?.length ?? 0) > 0 ||
                 (this.results.recordings?.length ?? 0) > 0);

            if (hasFullResults) {
                // Preserve any library-only artists/albums that the MB
                // search didn't find (no MBID, or MB didn't match).
                if (hadLocalResults) {
                    this.mergeLocalIntoFull(merged);
                }

                this.results = merged;
            } else if (!hadLocalResults) {
                // Both local and full are empty — show empty state.
                this.results = merged;
            }
            // else: keep existing local results as-is.

            exploreCache.populateFromSearch(
                this.results?.artists || [],
                this.results?.releaseGroups || [],
            );
            this.loadThumbnails();
            this.loadArtistImages();
            this.checkLibrary();

            const elapsed = (performance.now() - startTime).toFixed(0);
            console.log(
                `[explore] search completed: "${query}" in ${elapsed}ms — ` +
                    `artists=${result.artists?.length ?? 0}, ` +
                    `albums=${result.releaseGroups?.length ?? 0}, ` +
                    `tracks=${result.recordings?.length ?? 0}`,
            );
        } catch (err) {
            if (version !== this.searchVersion) return;
            const message = err instanceof Error ? err.message : String(err);
            this.error = message;
            console.error(`[explore] search error: "${query}" — ${message}`);
        } finally {
            if (version === this.searchVersion) {
                this.loading = false;
            }
        }
    }

    /* ── Thumbnail Loading ── */

    private thumbnailBatchPending = false;

    /**
     * Load thumbnails for all visible album cards in one batched
     * Wails call.  Called after search results are set.
     */
    /**
     * Seed the thumbnail cache from local library data only.  Safe
     * to call in library-only mode — does no API calls.  Reads from
     * cachedAlbums (by MBID) and from any `_coverArt` underscore
     * field that searchLibraryCache stamped on the release group.
     */
    private seedThumbnailsFromLibrary() {
        if (!this.results?.releaseGroups?.length) return;

        const cachedAlbums = libraryStore.cachedAlbums;
        const libAlbumsByMBID = new Map<string, string>();

        if (cachedAlbums) {
            for (const a of cachedAlbums) {
                if (a.MBID && (a.CoverArtMedium || a.CoverArtSmall)) {
                    libAlbumsByMBID.set(a.MBID, a.CoverArtMedium || a.CoverArtSmall);
                }
            }
        }

        let updated = false;

        for (const rg of this.results.releaseGroups) {
            if (this.thumbnailCache.has(rg.mbid)) continue;

            const localArt = libAlbumsByMBID.get(rg.mbid) || (rg as any)._coverArt;
            if (localArt) {
                this.thumbnailCache.set(rg.mbid, localArt);
                updated = true;
            }
        }

        if (updated) {
            this.requestUpdate();
        }
    }

    /**
     * Seed the artist image cache from local library data only.
     * Safe to call in library-only mode.  Reads from cachedArtists
     * by MBID and from any `_imageMedium`/`_imageSmall` underscore
     * field that searchLibraryCache stamped on the artist.  Falls
     * back to library album art when an artist has no portrait.
     */
    private seedArtistImagesFromLibrary() {
        if (!this.results?.artists?.length) return;

        const cachedArtists = libraryStore.cachedArtists;
        const libByMBID = new Map<string, string>();

        if (cachedArtists) {
            for (const a of cachedArtists) {
                if (a.MBID && (a.ImageMedium || a.ImageSmall)) {
                    libByMBID.set(a.MBID, a.ImageMedium || a.ImageSmall);
                }
            }
        }

        let updated = false;

        for (const a of this.results.artists) {
            if (!a.mbid || this.artistImageCache.has(a.mbid)) continue;

            const local = libByMBID.get(a.mbid) || (a as any)._imageMedium || (a as any)._imageSmall;
            if (local) {
                this.artistImageCache.set(a.mbid, local);
                updated = true;
                continue;
            }

            // Fallback: album art for artists without a portrait.
            const albumArt = getArtistAlbumArt(a.name);
            if (albumArt) {
                this.artistImageCache.set(a.mbid, albumArt);
                updated = true;
            }
        }

        if (updated) {
            this.requestUpdate();
        }
    }

    private loadThumbnails() {
        if (this.thumbnailBatchPending || !this.results?.releaseGroups?.length) {
            return;
        }

        // Always seed from local library first.
        this.seedThumbnailsFromLibrary();

        // Collect MBIDs that still need fetching from the API.
        const requests: ThumbnailRequest[] = [];

        for (const rg of this.results.releaseGroups) {
            if (!this.thumbnailCache.has(rg.mbid)) {
                requests.push({
                    mbid: rg.mbid,
                    albumName: rg.title || '',
                    artistName: rg.artistCredit || '',
                });
            }
        }

        if (requests.length === 0) return;

        this.thumbnailBatchPending = true;

        // Phase 1: batch cached lookup (instant, backend returns only cached items).
        GetThumbnails(requests)
            .then((results) => {
                let updated = false;
                const cached = results || {};

                for (const [mbid, dataUrl] of Object.entries(cached)) {
                    if (dataUrl) {
                        this.thumbnailCache.set(mbid, dataUrl);
                        updated = true;
                    }
                }

                if (updated) {
                    this.requestUpdate();
                }

                // Phase 2: fire individual fetches for uncached items —
                // each runs in its own Go goroutine and streams in as
                // the CAA fetch completes.
                const uncached = requests.filter((req) => !cached[req.mbid]);
                for (const req of uncached) {
                    // Mark as in-flight to prevent duplicates.
                    if (this.thumbnailCache.has(req.mbid)) continue;
                    this.thumbnailCache.set(req.mbid, '');

                    GetThumbnail(req.mbid, req.albumName, req.artistName)
                        .then((url) => {
                            if (url) {
                                this.thumbnailCache.set(req.mbid, url);
                                this.requestUpdate();
                            }
                        })
                        .catch(() => {});
                }
            })
            .catch(() => {
                // Batch failed — mark all as attempted so we don't retry.
                for (const req of requests) {
                    if (!this.thumbnailCache.has(req.mbid)) {
                        this.thumbnailCache.set(req.mbid, '');
                    }
                }
            })
            .finally(() => {
                this.thumbnailBatchPending = false;
            });
    }

    /**
     * Load artist images for all visible artist cards.  Each call
     * is async and updates the cache + re-renders on success.
     */
    private async loadArtistImages() {
        if (!this.results?.artists?.length) return;

        // Seed from library store first (instant, no API).
        this.seedArtistImagesFromLibrary();

        // Fetch remaining from API (only artists not yet resolved).
        for (const a of this.results.artists) {
            if (this.artistImageCache.has(a.mbid)) continue;

            this.artistImageCache.set(a.mbid, '');

            try {
                const url = await GetArtistImageURL(a.mbid);

                if (url) {
                    this.artistImageCache.set(a.mbid, url);
                    this.requestUpdate();
                }
            } catch {
                // No image — leave empty string.
            }
        }

        // Final fallback: album art for artists the API couldn't resolve.
        // Try library store first, then search-result release groups.
        let fallbackUpdated = false;

        for (const a of this.results.artists) {
            if (a.mbid && !this.artistImageCache.get(a.mbid)) {
                // 1) Library album art
                const albumArt = getArtistAlbumArt(a.name);
                if (albumArt) {
                    this.artistImageCache.set(a.mbid, albumArt);
                    fallbackUpdated = true;
                    continue;
                }

                // 2) Cover art from a search-result release group by this artist
                if (this.results.releaseGroups) {
                    const name = a.name.toLowerCase();
                    for (const rg of this.results.releaseGroups) {
                        if (rg.mbid && rg.artistCredit?.toLowerCase().includes(name)) {
                            const url = this.thumbnailCache.get(rg.mbid);
                            if (url) {
                                this.artistImageCache.set(a.mbid, url);
                                fallbackUpdated = true;
                                break;
                            }
                        }
                    }
                }
            }
        }

        if (fallbackUpdated) this.requestUpdate();

        // Sync resolved images into the explore cache so detail pages
        // pick them up without redundant API calls.
        for (const a of this.results.artists) {
            const url = this.artistImageCache.get(a.mbid);
            if (url) {
                const cached = exploreCache.getArtist(a.mbid);
                if (cached) {
                    cached.imageURL = url;
                } else {
                    exploreCache.setArtist(a.mbid, {
                        mbid: a.mbid,
                        name: a.name,
                        imageURL: url,
                    });
                }
            }
        }
    }

    /**
     * Check which result MBIDs exist in the local library.
     */
    private checkLibrary() {
        if (!this.results) return;

        // Backend now populates `inLibrary` directly on each MB result
        // via the local_*_id cross-reference columns.  Just read those.
        let updated = false;

        for (const a of this.results.artists ?? []) {
            if (a.mbid && a.inLibrary && !this.libraryMBIDs.has(a.mbid)) {
                this.libraryMBIDs.add(a.mbid);
                updated = true;
            }
        }

        for (const rg of this.results.releaseGroups ?? []) {
            if (rg.mbid && rg.inLibrary && !this.libraryMBIDs.has(rg.mbid)) {
                this.libraryMBIDs.add(rg.mbid);
                updated = true;
            }
        }

        for (const r of this.results.recordings ?? []) {
            if (r.mbid && r.inLibrary && !this.libraryMBIDs.has(r.mbid)) {
                this.libraryMBIDs.add(r.mbid);
                updated = true;
            }
        }

        if (updated) {
            this.requestUpdate();
        }
    }

    /* ── Navigation ── */

    private navigateToArtist(artist: MBArtist) {
        RecordSearchClick(this.searchQuery, artist.mbid, 'artist').catch(() => {});

        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: {
                    view: 'explore-artist-details',
                    artistMBID: artist.mbid,
                    artistName: artist.name,
                    localArtistId: (artist as MBArtist & { localId?: number }).localId || 0,
                },
            }),
        );
    }

    private navigateToAlbum(rg: MBReleaseGroup) {
        RecordSearchClick(this.searchQuery, rg.mbid, 'release_group').catch(() => {});

        const isLocal = typeof rg.mbid === 'string' && rg.mbid.startsWith('local:');
        const realMBID = isLocal ? '' : (rg.mbid || '');
        const localId = (rg as MBReleaseGroup & { localId?: number }).localId
            || (isLocal ? Number(rg.mbid.slice('local:'.length)) : 0);

        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: {
                    view: 'explore-album-details',
                    releaseGroupMBID: realMBID,
                    albumName: rg.title,
                    artistName: rg.artistCredit || '',
                    localAlbumId: localId,
                },
            }),
        );
    }

    private handleTopResultClick(e: CustomEvent<explore.TopResult>) {
        const r = e.detail;

        switch (r.entityType) {
            case 'artist':
                this.dispatchEvent(
                    new CustomEvent('navigate', {
                        bubbles: true,
                        composed: true,
                        detail: {
                            view: 'explore-artist-details',
                            artistMBID: r.mbid,
                            artistName: r.name,
                        },
                    }),
                );
                break;
            case 'release_group':
                this.dispatchEvent(
                    new CustomEvent('navigate', {
                        bubbles: true,
                        composed: true,
                        detail: {
                            view: 'explore-album-details',
                            releaseGroupMBID: r.mbid,
                            albumName: r.name,
                        },
                    }),
                );
                break;
            case 'recording':
                // Navigate to the album page if we can resolve it,
                // otherwise navigate to the artist.
                if (r.artistCredit) {
                    this.dispatchEvent(
                        new CustomEvent('navigate', {
                            bubbles: true,
                            composed: true,
                            detail: {
                                view: 'explore-artist-details',
                                artistMBID: '',
                                artistName: r.artistCredit,
                            },
                        }),
                    );
                }
                break;
        }
    }

    /* ── Image Error Handling ── */

    private handleImageError(e: Event) {
        const img = e.target as HTMLImageElement;
        img.style.display = 'none';
        // Reveal the fallback placeholder behind the image
        const fallback = img.nextElementSibling as HTMLElement | null;
        if (fallback) {
            fallback.style.display = 'flex';
        }
    }

    /* ── Render ── */

    override render() {
        return html`
            ${this.renderSearchInput()}
            ${this.loading
                ? html`<div class="loading-indicator">Searching\u2026</div>`
                : nothing}
            ${this.error
                ? html`<div class="error-message">
                      <wa-icon name="triangle-exclamation"></wa-icon>
                      ${this.error}
                  </div>`
                : nothing}
            ${this.renderBody()}
        `;
    }

    private renderSearchInput() {
        return html`
            <div class="search-container">
                <wa-icon class="search-icon" name="magnifying-glass"></wa-icon>
                <input
                    type="text"
                    placeholder="Search MusicBrainz\u2026"
                    .value=${this.searchQuery}
                    @input=${this.handleInput}
                    @keydown=${this.handleKeydown}
                />
                ${this.searchQuery
                    ? html`
                          <button
                              class="clear-button"
                              @click=${this.handleClear}
                          >
                              <wa-icon name="xmark"></wa-icon>
                          </button>
                      `
                    : nothing}
            </div>
        `;
    }

    private renderBody() {
        // Query too short
        if (this.queryTooShort) {
            return html`<div class="status-message">
                Keep typing\u2026
            </div>`;
        }

        // No query entered yet
        if (!this.searchQuery.trim() && !this.results) {
            return html`<div class="status-message">
                Search MusicBrainz to discover artists, albums, and tracks.
            </div>`;
        }

        // Loading state already shown above
        if (this.loading && !this.results) return nothing;

        // Results exist
        if (this.results) {
            const hasArtists = (this.results.artists?.length ?? 0) > 0;
            const hasAlbums = (this.results.releaseGroups?.length ?? 0) > 0;
            const hasTracks = (this.results.recordings?.length ?? 0) > 0;

            if (!hasArtists && !hasAlbums && !hasTracks) {
                return html`<div class="status-message">
                    No results found for \u201c${this.searchQuery}\u201d
                </div>`;
            }

            return html`
                <div class="results-container">
                    ${this.results.topResults?.length
                        ? html`<top-results-row
                            .results=${this.results.topResults}
                            .query=${this.searchQuery}
                            @top-result-click=${this.handleTopResultClick}
                        ></top-results-row>`
                        : nothing}
                    ${hasArtists
                        ? this.renderArtistsSection(this.results.artists!.slice(0, MAX_SECTION_RESULTS))
                        : nothing}
                    ${hasAlbums
                        ? this.renderAlbumsSection(this.results.releaseGroups!.slice(0, MAX_SECTION_RESULTS))
                        : nothing}
                    ${hasTracks
                        ? this.renderTracksSection(this.results.recordings!.slice(0, MAX_SECTION_RESULTS))
                        : nothing}
                </div>
            `;
        }

        return nothing;
    }

    /* ── Section Renderers ── */

    private renderArtistsSection(artists: MBArtist[]) {
        return html`
            <section>
                <h3 class="section-header">Artists</h3>
                <div class="horizontal-row">
                    ${artists.map((a) => {
                        const hue = nameToHue(a.name);
                        return html`
                            <div
                                class="artist-card"
                                @click=${() => this.navigateToArtist(a)}
                                role="button"
                                tabindex="0"
                                @keydown=${(e: KeyboardEvent) => {
                                    if (e.key === 'Enter' || e.key === ' ') {
                                        e.preventDefault();
                                        this.navigateToArtist(a);
                                    }
                                }}
                            >
                                <div
                                    class="artist-avatar"
                                    style="background: hsl(${hue}, 45%, 35%)"
                                >
                                    ${this.artistImageCache.get(a.mbid)
                                        ? html`<img
                                              src="${this.artistImageCache.get(a.mbid)}"
                                              alt="${a.englishName || a.name}"
                                          />`
                                        : (a.englishName || a.name).charAt(0).toUpperCase()}
                                </div>
                                <div class="artist-name" title="${a.englishName || a.name}">
                                    ${a.englishName || a.name}
                                </div>
                                ${a.englishName
                                    ? html`<div class="artist-native-name">${a.name}</div>`
                                    : nothing}
                                ${a.disambiguation
                                    ? html`<div class="artist-disambiguation">
                                          ${a.disambiguation}
                                      </div>`
                                    : nothing}
                                ${a.country
                                    ? html`<div class="artist-country">
                                          ${a.country}
                                      </div>`
                                    : nothing}
                            </div>
                        `;
                    })}
                </div>
            </section>
        `;
    }

    private renderAlbumsSection(releaseGroups: MBReleaseGroup[]) {
        return html`
            <section>
                <h3 class="section-header">Albums</h3>
                <div class="horizontal-row">
                    ${releaseGroups.map((rg) => {
                        const artURL = this.thumbnailCache.get(rg.mbid) || '';
                        const year = extractYear(rg.firstReleaseDate);

                        return html`
                            <div
                                class="album-card"
                                @click=${() => this.navigateToAlbum(rg)}
                                role="button"
                                tabindex="0"
                                @keydown=${(e: KeyboardEvent) => {
                                    if (e.key === 'Enter' || e.key === ' ') {
                                        e.preventDefault();
                                        this.navigateToAlbum(rg);
                                    }
                                }}
                            >
                                <div class="album-art-container">
                                    ${artURL
                                        ? html`<img
                                            src="${artURL}"
                                            alt="${rg.title}"
                                            loading="lazy"
                                            @error=${this.handleImageError}
                                        />`
                                        : nothing}
                                    <div
                                        class="album-art-fallback"
                                        style="${artURL ? 'display: none' : ''}"
                                    >
                                        <wa-icon name="compact-disc"></wa-icon>
                                    </div>
                                </div>
                                <div class="album-title" title="${rg.title}">
                                    ${rg.title}
                                </div>
                                <div class="album-artist">${rg.artistCredit}</div>
                                <div class="album-meta">
                                    <div class="album-meta-text">
                                        ${rg.primaryType
                                            ? html`<span class="type-badge"
                                                  >${rg.primaryType}</span
                                              >`
                                            : nothing}
                                        ${year ? html`<span>${year}</span>` : nothing}
                                    </div>
                                    <library-status-indicator
                                        status=${this.libraryMBIDs.has(rg.mbid) || rg.inLibrary ? 'in-library' : 'not-in-library'}
                                        entity-type="album"
                                        label=${rg.title}
                                    ></library-status-indicator>
                                </div>
                            </div>
                        `;
                    })}
                </div>
            </section>
        `;
    }

    private renderTracksSection(recordings: MBRecording[]) {
        return html`
            <section>
                <h3 class="section-header">Tracks</h3>
                <div class="track-list">
                    ${recordings.map(
                        (r) => html`
                            <div class="track-item">
                                <div class="track-info">
                                    <div class="track-title">${r.title}</div>
                                    <div class="track-artist">
                                        ${r.artistCredit}
                                    </div>
                                </div>
                                <div class="track-meta">
                                    ${r.popularity > 0
                                        ? html`<span class="track-popularity">${formatPopularity(r.popularity)}</span>`
                                        : nothing}
                                    ${r.length > 0
                                        ? html`<span class="track-duration">${formatDuration(r.length)}</span>`
                                        : nothing}
                                </div>
                                <library-status-indicator
                                    status=${this.libraryMBIDs.has(r.mbid) || r.inLibrary ? 'in-library' : 'not-in-library'}
                                    entity-type="track"
                                    label=${r.title}
                                ></library-status-indicator>
                            </div>
                        `,
                    )}
                </div>
            </section>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'explore-view': ExploreView;
    }
}

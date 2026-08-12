import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query as litQuery } from 'lit/decorators.js';
import '@components/page-header/page-header';
import { designTokens } from '../../styles/tokens.css';
import { SearchLocal, SearchLyrics, GetThumbnail, GetThumbnails, GetArtistImageURL, RecordSearchClick } from '@go/explore/Service';
import { libraryStore } from '../../store/library-store';
import { exploreCache, ARTIST_IMAGE_CACHE_LIMIT } from '../../store/explore-cache';
import { queueStore } from '../../store/queue-store';
import { artistLink, trackLink, exploreLinkStyles } from '../../utils/explore-link';
import { describeError } from '../../utils/describe-error';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '../library-status-indicator/library-status-indicator.js';
import '../top-results-row/top-results-row.js';
import { explore } from '@go/models';
import { ViewLifecycleMixin } from '../../utils/view-lifecycle';
import { registerCacheProbe } from '../../utils/cache-stats';
import { LRUMap } from '../../utils/lru-map';
type ThumbnailRequest = explore.ThumbnailRequest;
type MBSearchResult = explore.MBSearchResult;
type LyricsResult = explore.LyricsResult;
type MBArtist = explore.MBArtist;
type MBReleaseGroup = explore.MBReleaseGroup;
type MBRecording = explore.MBRecording;

/* ── Constants ── */
const MIN_QUERY_LENGTH = 2;
// Debounce window for live search-as-you-type.  The index query is
// local (no network), so this only coalesces rapid keystrokes.
const SEARCH_DEBOUNCE_MS = 180;

const MAX_SECTION_RESULTS = 10;

/*
 * Caps for the two art caches (`perf.M7`).
 *
 * Both were unbounded, on a view that never unmounts: twelve searches
 * retained 8.48 MB and were still climbing 0.7 MB per search.  The caps
 * come from the measured cost of an entry — a cover thumbnail is ~27 kB
 * of base64, an artist photo ~128 kB — so each is set to hold roughly a
 * dozen searches' worth and then stop:
 *
 *   thumbnails     96 × ~27 kB  ≈ 2.6 MB ceiling
 *   artist images  32 × ~128 kB ≈ 4.1 MB ceiling
 *
 * Both are comfortably larger than one screen of results, which matters:
 * a cap below the visible count would evict art that is still rendered,
 * and the re-render would fetch it again immediately.  A search shows at
 * most `MAX_SECTION_RESULTS` artists and ~15 release groups, so these
 * are ~3× and ~6× a screenful — six searches of history for covers,
 * which is far more than the "go back to the previous search" the cache
 * exists to serve.
 *
 * `ARTIST_IMAGE_CACHE_LIMIT` is shared with `explore-cache.ts`, which
 * holds the *same* data URLs for the detail pages.  Bounding one and
 * not the other frees nothing.
 */
const THUMBNAIL_CACHE_LIMIT = 96;


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
export class ExploreView extends ViewLifecycleMixin(LitElement) {
    /* ── State ── */

    @state() private searchQuery = '';
    @state() private results: MBSearchResult | null = null;
    @state() private loading = false;
    @state() private error = '';
    @state() private queryTooShort = false;
    /** Which search surface is active: catalog (index) or lyrics. */
    @state() private searchMode: 'catalog' | 'lyrics' = 'catalog';
    /** Lyric-search hits (library tracks matched by lyric fragment). */
    @state() private lyricsResults: LyricsResult[] | null = null;

    /** Monotonic counter to discard stale responses. */
    private searchVersion = 0;
    /** Debounce timer for live search-as-you-type. */
    private searchDebounceTimer?: ReturnType<typeof setTimeout>;
    private thumbnailCache = new LRUMap<string, string>(THUMBNAIL_CACHE_LIMIT);
    private artistImageCache = new LRUMap<string, string>(ARTIST_IMAGE_CACHE_LIMIT);
    private libraryMBIDs = new Set<string>();

    constructor() {
        super();

        // Registered from the constructor rather than on connect: this is
        // a cached primary view, so it connects once, and a measurement
        // must be able to read the caches whether or not Explore is the
        // view currently on screen.
        const stat = (m: LRUMap<string, string>) => () => {
            let chars = 0;

            for (const v of m.values()) chars += v.length;

            return { entries: m.size, chars, limit: m.limit };
        };

        registerCacheProbe('explore.thumbnails', stat(this.thumbnailCache));
        registerCacheProbe('explore.artistImages', stat(this.artistImageCache));
    }

    @litQuery('input') private inputEl!: HTMLInputElement;

    /* ── Styles ── */

    static override styles = [
        designTokens,
        exploreLinkStyles,
        css`
            :host {
                display: block;
                padding: 24px;
                overflow-y: auto;
                height: 100%;
                box-sizing: border-box;
            }

            /* The header supplies its own padding and rule, so it runs
               to the edge of a host that pads its own content. */
            page-header {
                margin: -24px -24px 1em;
            }

            /* ── Search mode tabs ── */
            .search-mode-tabs {
                display: flex;
                gap: 4px;
                margin-bottom: 10px;
            }

            .search-mode-tab {
                display: inline-flex;
                align-items: center;
                gap: 6px;
                background: none;
                border: 1px solid transparent;
                border-radius: 6px;
                color: var(--yj-text-tertiary, #888);
                cursor: pointer;
                padding: 5px 12px;
                font-size: var(--yj-text-sm);
                font-family: inherit;
                transition: color 0.15s ease, background 0.15s ease;
            }

            .search-mode-tab:hover {
                color: var(--yj-text-primary, #fff);
            }

            .search-mode-tab.active {
                color: var(--yj-bg-base, #1a1a1a);
                background: var(--yj-accent, #ffd43b);
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

            /* ── Lyrics results ── */
            .lyrics-results {
                margin-top: 20px;
                display: flex;
                flex-direction: column;
                gap: 2px;
                max-width: 640px;
            }

            .lyrics-hit {
                display: flex;
                align-items: center;
                gap: 12px;
                width: 100%;
                text-align: left;
                background: none;
                border: none;
                border-radius: 6px;
                color: var(--yj-text-primary, #fff);
                cursor: pointer;
                padding: 8px 10px;
                font-family: inherit;
                transition: background 0.12s ease;
            }

            .lyrics-hit:hover {
                background: var(--yj-bg-surface, #212529);
            }

            .lyrics-hit-play {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-icon-sm);
                flex-shrink: 0;
            }

            .lyrics-hit:hover .lyrics-hit-play {
                color: var(--yj-accent, #ffd43b);
            }

            .lyrics-hit-main {
                display: flex;
                flex-direction: column;
                min-width: 0;
            }

            .lyrics-hit-title {
                font-size: var(--yj-text-md);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .lyrics-hit-meta {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-secondary, #b3b3b3);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
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

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.cancelPendingSearch();
    }

    /** A debounced search that lands after the user has left the page is
     *  a query nobody asked for, against a 1.1 M-row index. */
    protected override onViewDeactivate(): void {
        this.cancelPendingSearch();
    }

    /* ── Search Logic ── */

    private handleInput(e: Event) {
        const input = e.target as HTMLInputElement;
        this.searchQuery = input.value;

        const trimmed = this.searchQuery.trim();

        if (!trimmed) {
            this.cancelPendingSearch();
            this.results = null;
            this.error = '';
            this.loading = false;
            this.queryTooShort = false;
            return;
        }

        if (trimmed.length < MIN_QUERY_LENGTH) {
            this.cancelPendingSearch();
            this.results = null;
            this.error = '';
            this.loading = false;
            this.queryTooShort = true;
            return;
        }

        this.queryTooShort = false;

        // Both modes debounce straight to their backend search — catalog
        // to the offline index (SearchLocal), lyrics to the FTS lyric
        // search.  No owned-library seed: the index is the sole source of
        // catalog results, so we never paint temporary library matches.
        this.scheduleSearch();
    }

    /** Switch between catalog and lyric search, resetting results. */
    private setSearchMode(mode: 'catalog' | 'lyrics') {
        if (this.searchMode === mode) return;

        this.cancelPendingSearch();
        this.searchMode = mode;
        this.results = null;
        this.lyricsResults = null;
        this.error = '';
        this.loading = false;

        if (this.searchQuery.trim().length >= MIN_QUERY_LENGTH) {
            void this.executeSearch();
        }

        this.inputEl?.focus();
    }

    /** Debounce a live index search after the latest keystroke. */
    private scheduleSearch() {
        this.cancelPendingSearch();
        this.searchDebounceTimer = setTimeout(() => {
            this.searchDebounceTimer = undefined;
            if (this.searchQuery.trim().length >= MIN_QUERY_LENGTH) {
                void this.executeSearch();
            }
        }, SEARCH_DEBOUNCE_MS);
    }

    private cancelPendingSearch() {
        if (this.searchDebounceTimer) {
            clearTimeout(this.searchDebounceTimer);
            this.searchDebounceTimer = undefined;
        }
    }

    private handleClear() {
        this.cancelPendingSearch();
        this.searchQuery = '';
        this.results = null;
        this.lyricsResults = null;
        this.error = '';
        this.loading = false;
        this.queryTooShort = false;
        if (this.inputEl) {
            this.inputEl.value = '';
            this.inputEl.focus();
        }
    }

    private handleKeydown(e: KeyboardEvent) {
        if (e.key === 'Escape') {
            this.handleClear();
            return;
        }

        // Enter is optional now — search runs live as you type — but it
        // still fires an immediate search, skipping the debounce wait.
        if (e.key === 'Enter') {
            e.preventDefault();
            if (this.searchQuery.trim().length >= MIN_QUERY_LENGTH) {
                this.cancelPendingSearch();
                void this.executeSearch();
            }
        }
    }

    private async executeSearch() {
        const version = ++this.searchVersion;
        const query = this.searchQuery.trim();
        if (!query) return;

        this.loading = true;
        this.error = '';

        // Lyrics mode: a single FTS lyric search over the library.
        if (this.searchMode === 'lyrics') {
            void this.executeLyricsSearch(version, query);
            return;
        }

        // Offline search over the local popularity index via Wails RPC.
        // No network, and no owned-library seed — the index is the sole
        // source of catalog results.
        void this.executeIndexSearch(version, query);
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
            for (const a of result.artists) {
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
            for (const rg of result.releaseGroups) {
                const lib = rg.mbid ? libAlbumsByMBID.get(rg.mbid) : undefined;
                if (lib) {
                    (rg as any)._coverArt = lib.CoverArtMedium || lib.CoverArtSmall || '';
                    (rg as any)._inLibrary = true;
                }
            }
        }

        return result;
    }

    private async executeIndexSearch(version: number, query: string) {
        try {
            // Local FTS index only — no network.  Returns null when the
            // index has no hits, in which case we keep the owned-library
            // matches already displayed.
            const result =
                (await SearchLocal(query)) ??
                explore.MBSearchResult.createFrom({
                    artists: [],
                    releaseGroups: [],
                    recordings: [],
                    topResults: [],
                });

            // Discard stale response
            if (version !== this.searchVersion) {
                console.debug(
                    `[explore] discarded stale response for "${query}" (v${version} != v${this.searchVersion})`,
                );
                return;
            }

            // Enrich index results with local cover art / "In Library"
            // badges, but never inject library-only entries — the index
            // is the sole source of results shown.
            this.results = this.mergeWithLibrary(result);

            exploreCache.populateFromSearch(
                this.results?.artists || [],
                this.results?.releaseGroups || [],
            );
            this.loadThumbnails();
            this.loadArtistImages();
            this.checkLibrary();
        } catch (err) {
            if (version !== this.searchVersion) return;

            console.error(`[explore] search error: "${query}"`, err);
            // Inline: this failure belongs to the results panel, and
            // the panel is what the user is looking at (errors.M9).
            this.error = describeError(
                err,
                'The catalog search did not answer.',
            );
        } finally {
            if (version === this.searchVersion) {
                this.loading = false;
            }
        }
    }

    /* ── Lyrics Search ── */

    private async executeLyricsSearch(version: number, query: string) {
        try {
            const hits = await SearchLyrics(query);
            if (version !== this.searchVersion) return;

            this.lyricsResults = hits ?? [];
        } catch (err) {
            if (version !== this.searchVersion) return;

            console.error(`[explore] lyrics search error: "${query}"`, err);
            this.error = describeError(
                err,
                'The lyric search did not answer.',
            );
        } finally {
            if (version === this.searchVersion) {
                this.loading = false;
            }
        }
    }

    /** Play a lyric-search hit immediately (replaces the queue). */
    private playLyricHit(hit: LyricsResult) {
        if (!hit.filePath) return;
        queueStore.setQueue([hit.filePath], 0);
    }

    /* ── Thumbnail Loading ── */

    private thumbnailBatchPending = false;

    /**
     * Load thumbnails for all visible album cards in one batched
     * Wails call.  Called after search results are set.
     */
    /**
     * Seed the thumbnail cache from local library data only — does
     * no API calls.  Reads from cachedAlbums (by MBID) and from any
     * `_coverArt` underscore field that searchLibraryCache stamped
     * on the release group.
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
     * Seed the artist image cache from local library data only —
     * does no API calls.  Reads from cachedArtists
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
                // Standard track-click behaviour (matches the library list,
                // queue, playlists, etc.): open the track's album page with
                // the track highlighted.  The backend resolves the parent
                // release group from the local index, so this is an instant,
                // index-backed load rather than a name-only artist lookup.
                if (r.releaseGroupMbid) {
                    this.dispatchEvent(
                        new CustomEvent('navigate', {
                            bubbles: true,
                            composed: true,
                            detail: {
                                view: 'explore-album-details',
                                releaseGroupMBID: r.releaseGroupMbid,
                                albumName: r.releaseName || '',
                                highlightTrackMBID: r.mbid,
                            },
                        }),
                    );
                } else if (r.artistCredit) {
                    // Fallback only when the album can't be resolved locally.
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
            <page-header heading="Explore"></page-header>
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
        const placeholder =
            this.searchMode === 'lyrics'
                ? 'Search by a lyric\u2026'
                : 'Search artists, albums, and tracks\u2026';

        return html`
            <div class="search-mode-tabs">
                <button
                    class="search-mode-tab ${this.searchMode === 'catalog' ? 'active' : ''}"
                    @click=${() => this.setSearchMode('catalog')}
                >
                    <wa-icon name="magnifying-glass"></wa-icon>
                    Catalog
                </button>
                <button
                    class="search-mode-tab ${this.searchMode === 'lyrics' ? 'active' : ''}"
                    @click=${() => this.setSearchMode('lyrics')}
                >
                    <wa-icon name="quote-left"></wa-icon>
                    Lyrics
                </button>
            </div>
            <div class="search-container">
                <wa-icon class="search-icon" name="magnifying-glass"></wa-icon>
                <input
                    type="text"
                    placeholder=${placeholder}
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

    private renderLyricsBody() {
        if (this.queryTooShort) {
            return html`<div class="status-message">Keep typing…</div>`;
        }

        if (!this.searchQuery.trim() && !this.lyricsResults) {
            return html`<div class="status-message">
                Type a line of lyrics to find the track in your library.
            </div>`;
        }

        if (this.loading && !this.lyricsResults) return nothing;

        if (this.lyricsResults && this.lyricsResults.length === 0) {
            return html`<div class="status-message">
                No tracks with lyrics matching “${this.searchQuery}”.
            </div>`;
        }

        if (!this.lyricsResults) return nothing;

        return html`
            <div class="lyrics-results">
                ${this.lyricsResults.map(
                    (hit) => html`
                        <button
                            class="lyrics-hit"
                            @click=${() => this.playLyricHit(hit)}
                            title="Play ${hit.title}"
                        >
                            <wa-icon class="lyrics-hit-play" name="play"></wa-icon>
                            <span class="lyrics-hit-main">
                                <span class="lyrics-hit-title">${hit.title || 'Unknown title'}</span>
                                <span class="lyrics-hit-meta">
                                    ${hit.artist || 'Unknown artist'}${hit.album
                                        ? html` · ${hit.album}`
                                        : nothing}
                                </span>
                            </span>
                        </button>
                    `,
                )}
            </div>
        `;
    }

    private renderBody() {
        if (this.searchMode === 'lyrics') {
            return this.renderLyricsBody();
        }

        // Query too short
        if (this.queryTooShort) {
            return html`<div class="status-message">
                Keep typing\u2026
            </div>`;
        }

        // No query entered yet
        if (!this.searchQuery.trim() && !this.results) {
            return html`<div class="status-message">
                Search to discover artists, albums, and tracks.
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
                                <div class="album-artist">${artistLink(rg.artistCredit, rg.artistMbid ?? '')}</div>
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
                                    <div class="track-title">
                                        ${trackLink(r.title, r.releaseName ?? '', r.releaseGroupMbid ?? '', r.mbid)}
                                    </div>
                                    <div class="track-artist">
                                        ${artistLink(r.artistCredit, r.artistMbid ?? '')}
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

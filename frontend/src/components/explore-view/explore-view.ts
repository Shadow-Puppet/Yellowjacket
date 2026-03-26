import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query as litQuery } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import { Search, GetThumbnails, GetArtistImageURL, CheckLibraryMBIDs } from '@go/explore/Service';
import type { ThumbnailRequest } from '@go/explore/Service';
import type {
    MBSearchResult,
    MBArtist,
    MBReleaseGroup,
    MBRecording,
} from '@go/explore/Service';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

/* ── Constants ── */
const DEBOUNCE_MS = 300;
const MIN_QUERY_LENGTH = 2;
const MAX_SECTION_RESULTS = 10;
const CAA_GROUP_BASE = 'https://coverartarchive.org/release-group';
const TOP_RESULTS_COUNT = 3;

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
    if (!ms || ms <= 0) return '0:00';
    const totalSeconds = Math.floor(ms / 1000);
    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;
    return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

/** Extract the year from a date string like "2005-03-29" or "2005". */
function extractYear(dateStr: string): string {
    if (!dateStr) return '';
    return dateStr.substring(0, 4);
}

/**
 * Union type for top-results ranking. We can only rank items with
 * a score field — MBReleaseGroup lacks one in the Go struct.
 */
interface ScoredItem {
    type: 'artist' | 'recording';
    score: number;
    artist?: MBArtist;
    recording?: MBRecording;
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
            .top-card {
                display: flex;
                align-items: center;
                gap: 12px;
                padding: 10px 14px;
                background: var(--yj-bg-surface, #212529);
                border-radius: 8px;
                cursor: pointer;
                min-width: 200px;
                max-width: 280px;
                flex-shrink: 0;
                transition: background 0.15s ease;
            }

            .top-card:hover {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
            }

            .top-card:active {
                transform: scale(0.98);
            }

            .top-card-info {
                flex: 1;
                min-width: 0;
            }

            .top-card-name {
                font-weight: 500;
                color: var(--yj-text-primary, #fff);
                font-size: var(--yj-text-md);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .top-card-meta {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-sm);
                margin-top: 2px;
            }

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
                gap: 6px;
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-xs);
            }

            .type-badge {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
                padding: 1px 6px;
                border-radius: 3px;
                font-size: 10px;
                white-space: nowrap;
            }

            .library-badge {
                background: var(--yj-accent, #1db954);
                color: #000;
                padding: 1px 6px;
                border-radius: 3px;
                font-size: 10px;
                font-weight: 600;
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
        `,
    ];

    /* ── Lifecycle ── */

    override disconnectedCallback() {
        super.disconnectedCallback();
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

        try {
            const result = await Search(query);

            // Discard stale response
            if (version !== this.searchVersion) {
                console.debug(
                    `[explore] discarded stale response for "${query}" (v${version} != v${this.searchVersion})`,
                );
                return;
            }

            this.results = result;
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
    private loadThumbnails() {
        if (this.thumbnailBatchPending || !this.results?.releaseGroups?.length) {
            return;
        }

        // Collect MBIDs that need fetching.
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

        GetThumbnails(requests)
            .then((results) => {
                let updated = false;

                for (const [mbid, dataUrl] of Object.entries(results)) {
                    if (dataUrl) {
                        this.thumbnailCache.set(mbid, dataUrl);
                        updated = true;
                    }
                }

                // Mark MBIDs with no art so we don't re-request.
                for (const req of requests) {
                    if (!this.thumbnailCache.has(req.mbid)) {
                        this.thumbnailCache.set(req.mbid, '');
                    }
                }

                if (updated) {
                    this.requestUpdate();
                }
            })
            .catch(() => {
                // Batch failed — mark all as attempted.
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

        // Load sequentially to avoid hammering the MB rate limiter.
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
    }

    /**
     * Check which result MBIDs exist in the local library.
     */
    private async checkLibrary() {
        if (!this.results) return;

        const mbids: string[] = [];

        for (const a of this.results.artists ?? []) {
            if (a.mbid) mbids.push(a.mbid);
        }

        for (const rg of this.results.releaseGroups ?? []) {
            if (rg.mbid) mbids.push(rg.mbid);
        }

        if (mbids.length === 0) return;

        try {
            const found = await CheckLibraryMBIDs(mbids);

            if (found && Object.keys(found).length > 0) {
                for (const mbid of Object.keys(found)) {
                    this.libraryMBIDs.add(mbid);
                }

                this.requestUpdate();
            }
        } catch {
            // Library check is non-critical.
        }
    }

    /* ── Top Results ── */

    private getTopResults(): ScoredItem[] {
        if (!this.results) return [];

        const items: ScoredItem[] = [];

        if (this.results.artists) {
            for (const a of this.results.artists) {
                items.push({ type: 'artist', score: a.score, artist: a });
            }
        }

        if (this.results.recordings) {
            for (const r of this.results.recordings) {
                items.push({ type: 'recording', score: r.score, recording: r });
            }
        }

        items.sort((a, b) => b.score - a.score);
        return items.slice(0, TOP_RESULTS_COUNT);
    }

    /* ── Navigation ── */

    private navigateToArtist(artist: MBArtist) {
        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: {
                    view: 'explore-artist-details',
                    artistMBID: artist.mbid,
                    artistName: artist.name,
                },
            }),
        );
    }

    private navigateToAlbum(rg: MBReleaseGroup) {
        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: {
                    view: 'explore-album-details',
                    releaseGroupMBID: rg.mbid,
                    albumName: rg.title,
                },
            }),
        );
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

            const topResults = this.getTopResults();

            return html`
                <div class="results-container">
                    ${topResults.length > 0
                        ? this.renderTopResults(topResults)
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

    private renderTopResults(items: ScoredItem[]) {
        return html`
            <section>
                <h3 class="section-header">Top Results</h3>
                <div class="horizontal-row">
                    ${items.map((item) => this.renderTopCard(item))}
                </div>
            </section>
        `;
    }

    private renderTopCard(item: ScoredItem) {
        if (item.type === 'artist' && item.artist) {
            const a = item.artist;
            const hue = nameToHue(a.name);
            const imgURL = this.artistImageCache.get(a.mbid);
            return html`
                <div
                    class="top-card"
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
                        ${imgURL
                            ? html`<img src="${imgURL}" alt="${a.englishName || a.name}" />`
                            : (a.englishName || a.name).charAt(0).toUpperCase()}
                    </div>
                    <div class="top-card-info">
                        <div class="top-card-name">${a.englishName || a.name}</div>
                        <div class="top-card-meta">Artist${a.country ? ` · ${a.country}` : ''}</div>
                    </div>
                </div>
            `;
        }

        if (item.type === 'recording' && item.recording) {
            const r = item.recording;
            return html`
                <div class="top-card" role="listitem">
                    <wa-icon
                        style="color: var(--yj-text-tertiary, #888); font-size: 18px; flex-shrink: 0;"
                        name="music"
                    ></wa-icon>
                    <div class="top-card-info">
                        <div class="top-card-name">${r.title}</div>
                        <div class="top-card-meta">
                            ${r.artistCredit}${r.length
                                ? ` · ${formatDuration(r.length)}`
                                : ''}
                        </div>
                    </div>
                </div>
            `;
        }

        return nothing;
    }

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
                                ${this.libraryMBIDs.has(a.mbid)
                                    ? html`<div class="library-badge">In Library</div>`
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
                        const cachedArt = this.thumbnailCache.get(rg.mbid);
                        const artURL = cachedArt || CoverArtGroupURL(rg.mbid);
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
                                    <img
                                        src="${artURL}"
                                        alt="${rg.title}"
                                        loading="lazy"
                                        @error=${this.handleImageError}
                                    />
                                    <div
                                        class="album-art-fallback"
                                        style="display: none"
                                    >
                                        <wa-icon name="compact-disc"></wa-icon>
                                    </div>
                                </div>
                                <div class="album-title" title="${rg.title}">
                                    ${rg.title}
                                </div>
                                <div class="album-artist">${rg.artistCredit}</div>
                                <div class="album-meta">
                                    ${this.libraryMBIDs.has(rg.mbid)
                                        ? html`<span class="library-badge">In Library</span>`
                                        : nothing}
                                    ${rg.primaryType
                                        ? html`<span class="type-badge"
                                              >${rg.primaryType}</span
                                          >`
                                        : nothing}
                                    ${year ? html`<span>${year}</span>` : nothing}
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
                                <div class="track-duration">
                                    ${formatDuration(r.length)}
                                </div>
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

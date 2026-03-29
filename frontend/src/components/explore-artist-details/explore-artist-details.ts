import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import {
    LookupArtist,
    BrowseReleaseGroups,
    TopRecordingsForArtist,
    SimilarArtists,
    GetArtistImageURL,
    CheckLibraryMBIDs,
} from '@go/explore/Service';
import type {
    MBArtist,
    MBReleaseGroup,
    LBTopRecording,
    LBSimilarArtist,
} from '@go/explore/Service';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

/* ── Constants ── */
const CAA_GROUP_BASE = 'https://coverartarchive.org/release-group';

/** Desired section order for grouping release types. */
const TYPE_ORDER = ['Albums', 'EP', 'Single', 'Other Albums'];

/**
 * Secondary types that move an "Album" out of the studio albums
 * bucket and into "Other Albums".
 */
const NON_STUDIO_SECONDARY_TYPES = new Set([
    'Compilation',
    'Soundtrack',
    'Live',
    'Remix',
    'Spokenword',
    'Interview',
    'DJ-mix',
    'Mixtape/Street',
    'Demo',
    'Audio drama',
]);

/* ── Utility functions (duplicated from explore-view per design decision) ── */

function CoverArtGroupURL(releaseGroupMBID: string): string {
    return `${CAA_GROUP_BASE}/${releaseGroupMBID}/front-250`;
}

function nameToHue(name: string): number {
    let hash = 0;
    for (let i = 0; i < name.length; i++) {
        hash = name.charCodeAt(i) + ((hash << 5) - hash);
    }
    return Math.abs(hash) % 360;
}

function extractYear(dateStr: string): string {
    if (!dateStr) return '';
    return dateStr.substring(0, 4);
}

function formatListenCount(count: number): string {
    if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`;
    if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`;
    return String(count);
}

/* ── Component ── */

@customElement('explore-artist-details')
export class ExploreArtistDetails extends LitElement {
    /* ── Public attributes ── */

    @property({ type: String, attribute: 'artist-mbid' })
    artistMBID = '';

    @property({ type: String, attribute: 'artist-name' })
    artistName = '';

    /* ── Internal state ── */

    @state() private artist: MBArtist | null = null;
    @state() private topTracks: LBTopRecording[] = [];
    @state() private releaseGroups: MBReleaseGroup[] = [];
    @state() private loadingArtist = true;
    @state() private loadingTracks = true;
    @state() private loadingReleases = true;
    @state() private errorArtist = '';
    @state() private errorTracks = '';
    @state() private errorReleases = '';
    @state() private similarArtists: LBSimilarArtist[] = [];
    @state() private loadingSimilar = true;
    @state() private artistImageURL = '';
    @state() private similarImageURLs = new Map<string, string>();
    private libraryMBIDs = new Set<string>();

    /* ── Styles ── */

    static override styles = [
        designTokens,
        css`
            :host {
                display: flex;
                flex-direction: column;
                overflow: hidden;
                height: 100%;
            }

            /* ── Header ── */
            .artist-header {
                display: flex;
                align-items: center;
                gap: 20px;
                padding: 16px 20px;
                flex-shrink: 0;
                border-bottom: 1px solid
                    var(--yj-border-subtle, rgba(255, 255, 255, 0.06));
            }

            .back-button {
                display: flex;
                align-items: center;
                justify-content: center;
                width: 32px;
                height: 32px;
                border: none;
                border-radius: 50%;
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                color: var(--yj-text-primary, #fff);
                cursor: pointer;
                flex-shrink: 0;
                transition: background-color 0.15s ease;
            }

            .back-button:hover {
                background: var(--yj-bg-hover, rgba(255, 255, 255, 0.12));
            }

            .back-button wa-icon {
                font-size: 16px;
            }

            .artist-avatar {
                width: 80px;
                height: 80px;
                border-radius: 50%;
                display: flex;
                align-items: center;
                justify-content: center;
                color: #fff;
                font-weight: 600;
                font-size: 32px;
                text-transform: uppercase;
                user-select: none;
                flex-shrink: 0;
                line-height: 1;
                overflow: hidden;
            }

            .artist-avatar img {
                width: 100%;
                height: 100%;
                object-fit: cover;
            }

            .artist-info {
                display: flex;
                flex-direction: column;
                gap: 4px;
                min-width: 0;
            }

            .artist-title {
                font-size: 24px;
                font-weight: 700;
                color: var(--yj-text-primary, #fff);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                margin: 0;
                line-height: 1.2;
            }

            .artist-native-name {
                font-size: var(--yj-text-md);
                color: var(--yj-text-secondary, #b3b3b3);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                margin-top: 2px;
            }

            .artist-meta {
                font-size: var(--yj-text-md);
                color: var(--yj-text-secondary, #b3b3b3);
                display: flex;
                align-items: center;
                gap: 6px;
                flex-wrap: wrap;
            }

            .meta-separator {
                opacity: 0.4;
            }

            /* ── Scrollable content ── */
            .content {
                flex: 1;
                overflow-y: auto;
                padding: 20px 24px 32px;
                display: flex;
                flex-direction: column;
                gap: 32px;
            }

            /* ── Section headers ── */
            .section-header {
                font-size: 11px;
                font-weight: 600;
                color: var(--yj-text-secondary, #b3b3b3);
                text-transform: uppercase;
                letter-spacing: 0.05em;
                margin: 0 0 12px;
            }

            /* ── Loading / error states ── */
            .section-loading {
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: var(--yj-text-md);
                animation: pulse 1.5s ease-in-out infinite;
            }

            @keyframes pulse {
                0%,
                100% {
                    opacity: 1;
                }
                50% {
                    opacity: 0.5;
                }
            }

            .section-error {
                display: flex;
                align-items: center;
                gap: 6px;
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: var(--yj-text-sm);
            }

            .section-error wa-icon {
                color: #e5534b;
                font-size: var(--yj-icon-sm);
                flex-shrink: 0;
            }

            /* ── Top tracks ── */
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
                background: var(
                    --yj-bg-overlay,
                    rgba(255, 255, 255, 0.04)
                );
            }

            .track-rank {
                width: 24px;
                text-align: right;
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-md);
                font-variant-numeric: tabular-nums;
                flex-shrink: 0;
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

            .track-listens {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-sm);
                flex-shrink: 0;
                font-variant-numeric: tabular-nums;
                white-space: nowrap;
            }

            /* ── Discography grid ── */
            .disco-group {
                display: flex;
                flex-direction: column;
                gap: 12px;
            }

            .disco-type-header {
                font-size: var(--yj-text-md);
                font-weight: 600;
                color: var(--yj-text-primary, #fff);
                margin: 0;
            }

            .album-grid {
                display: grid;
                grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
                gap: 16px;
            }

            .album-card {
                display: flex;
                flex-direction: column;
                gap: 6px;
                padding: 8px;
                border-radius: 8px;
                cursor: pointer;
                transition: background 0.15s ease;
            }

            .album-card:hover {
                background: var(
                    --yj-bg-overlay,
                    rgba(255, 255, 255, 0.06)
                );
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

            .album-meta {
                display: flex;
                align-items: center;
                gap: 6px;
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-xs);
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

            /* ── Similar artists ── */
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

            .similar-artist-card {
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

            .similar-artist-card:hover {
                background: var(
                    --yj-bg-overlay,
                    rgba(255, 255, 255, 0.06)
                );
            }

            .similar-artist-card:active {
                transform: scale(0.97);
            }

            .similar-avatar {
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

            .similar-avatar img {
                width: 100%;
                height: 100%;
                object-fit: cover;
            }

            .similar-name {
                font-weight: 500;
                color: var(--yj-text-primary, #fff);
                font-size: var(--yj-text-sm);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                width: 100%;
            }
        `,
    ];

    /* ── Lifecycle ── */

    override connectedCallback() {
        super.connectedCallback();
        if (this.artistMBID) {
            void this.loadAllData();
        }
    }

    /* ── Data Loading ── */

    private async loadAllData() {
        const mbid = this.artistMBID;
        console.log(
            `[explore-artist] loading: "${this.artistName}" (${mbid})`,
        );

        // Fire all five requests in parallel — each section is independent.
        const [artistResult, tracksResult, releasesResult, similarResult] =
            await Promise.allSettled([
                this.fetchArtist(mbid),
                this.fetchTopTracks(mbid),
                this.fetchReleaseGroups(mbid),
                this.fetchSimilarArtists(mbid),
            ]);

        // Artist image is fire-and-forget — doesn't block the page.
        this.fetchArtistImage(mbid);

        // Check which release groups are in the local library.
        this.checkLibrary();

        const summary = [
            `artist=${artistResult.status}`,
            `tracks=${tracksResult.status}`,
            `releases=${releasesResult.status}`,
            `similar=${similarResult.status}`,
        ].join(', ');
        console.log(
            `[explore-artist] loaded: "${this.artistName}" (${summary})`,
        );
    }

    private async fetchArtist(mbid: string) {
        try {
            this.artist = await LookupArtist(mbid);
        } catch (err) {
            const msg = err instanceof Error ? err.message : String(err);
            this.errorArtist = msg;
            console.error(`[explore-artist] LookupArtist error: ${msg}`);
        } finally {
            this.loadingArtist = false;
        }
    }

    private async fetchTopTracks(mbid: string) {
        try {
            const tracks = await TopRecordingsForArtist(mbid);
            this.topTracks = tracks ?? [];
        } catch (err) {
            const msg = err instanceof Error ? err.message : String(err);
            this.errorTracks = msg;
            console.error(
                `[explore-artist] TopRecordingsForArtist error: ${msg}`,
            );
        } finally {
            this.loadingTracks = false;
        }
    }

    private async fetchReleaseGroups(mbid: string) {
        try {
            const rgs = await BrowseReleaseGroups(mbid);
            this.releaseGroups = rgs ?? [];
        } catch (err) {
            const msg = err instanceof Error ? err.message : String(err);
            this.errorReleases = msg;
            console.error(
                `[explore-artist] BrowseReleaseGroups error: ${msg}`,
            );
        } finally {
            this.loadingReleases = false;
        }
    }

    private async fetchSimilarArtists(mbid: string) {
        try {
            const artists = await SimilarArtists(mbid);
            this.similarArtists = artists ?? [];
        } catch (err) {
            // D024: graceful degradation — silently omit similar artists on failure.
            const msg = err instanceof Error ? err.message : String(err);
            console.error(
                `[explore-artist] SimilarArtists error: ${msg}`,
            );
            this.similarArtists = [];
        } finally {
            this.loadingSimilar = false;
        }

        // Fire-and-forget: resolve images for similar artists in parallel.
        if (this.similarArtists.length > 0) {
            void this.fetchSimilarArtistImages();
        }
    }

    private async fetchSimilarArtistImages() {
        const artists = this.similarArtists;
        // Fetch in parallel — each call is cached after first resolution.
        await Promise.allSettled(
            artists.map(async (a) => {
                try {
                    const url = await GetArtistImageURL(a.artistMbid);
                    if (url) {
                        this.similarImageURLs = new Map(this.similarImageURLs).set(
                            a.artistMbid,
                            url,
                        );
                    }
                } catch {
                    // No image — letter avatar stays.
                }
            }),
        );
    }

    private async fetchArtistImage(mbid: string) {
        try {
            const url = await GetArtistImageURL(mbid);
            if (url) {
                this.artistImageURL = url;
            }
        } catch {
            // No image available — avatar stays as initial letter.
        }
    }

    private async checkLibrary() {
        const mbids: string[] = [];

        for (const rg of this.releaseGroups) {
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
            // Non-critical.
        }
    }

    /* ── Navigation ── */

    private navigateBack() {
        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: { view: 'explore' },
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

    private navigateToSimilarArtist(artist: LBSimilarArtist) {
        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: {
                    view: 'explore-artist-details',
                    artistMBID: artist.artistMbid,
                    artistName: artist.name,
                },
            }),
        );
    }

    /* ── Image Error Handling ── */

    private handleImageError(e: Event) {
        const img = e.target as HTMLImageElement;
        img.style.display = 'none';
        const fallback = img.nextElementSibling as HTMLElement | null;
        if (fallback) {
            fallback.style.display = 'flex';
        }
    }

    /** On similar-artist image error, remove the img so the letter initial shows. */
    private handleSimilarImageError(e: Event) {
        const img = e.target as HTMLImageElement;
        img.remove();
    }

    /* ── Helpers ── */

    private getInitial(name: string): string {
        if (!name) return '?';
        return name.charAt(0).toUpperCase();
    }

    /** English name if available, otherwise the native name. */
    private get displayName(): string {
        return this.artist?.englishName || this.artistName;
    }

    /**
     * Group release groups by type, returning entries in the
     * canonical order: Albums → EP → Single → Other Albums → ...rest.
     *
     * "Albums" contains release groups with primaryType "Album" and
     * no non-studio secondary types.  "Other Albums" collects
     * compilations, soundtracks, live albums, etc.
     */
    private groupByType(): Array<{ type: string; items: MBReleaseGroup[] }> {
        const map = new Map<string, MBReleaseGroup[]>();

        for (const rg of this.releaseGroups) {
            let key = rg.primaryType || 'Other';

            // Split "Album" into studio vs other based on secondary types.
            if (key === 'Album') {
                const hasNonStudio = rg.secondaryTypes?.some((t) =>
                    NON_STUDIO_SECONDARY_TYPES.has(t),
                );
                key = hasNonStudio ? 'Other Albums' : 'Albums';
            }

            let bucket = map.get(key);
            if (!bucket) {
                bucket = [];
                map.set(key, bucket);
            }
            bucket.push(rg);
        }

        // Sort each bucket by firstReleaseDate descending (newest first).
        for (const bucket of map.values()) {
            bucket.sort((a, b) => {
                const da = a.firstReleaseDate || '';
                const db = b.firstReleaseDate || '';
                return db.localeCompare(da);
            });
        }

        // Build ordered result following TYPE_ORDER, then any remaining types.
        const result: Array<{ type: string; items: MBReleaseGroup[] }> = [];
        const seen = new Set<string>();

        for (const type of TYPE_ORDER) {
            const items = map.get(type);
            if (items && items.length > 0) {
                result.push({ type, items });
                seen.add(type);
            }
        }

        // Remaining types not in TYPE_ORDER (alphabetical).
        const remaining = [...map.keys()]
            .filter((k) => !seen.has(k))
            .sort();
        for (const type of remaining) {
            const items = map.get(type);
            if (items && items.length > 0) {
                result.push({ type, items });
            }
        }

        return result;
    }

    /* ── Render ── */

    override render() {
        const hue = nameToHue(this.artistName);

        return html`
            <div class="artist-header">
                <button
                    class="back-button"
                    @click=${this.navigateBack}
                    title="Back to explore"
                    aria-label="Back to explore"
                >
                    <wa-icon name="arrow-left"></wa-icon>
                </button>
                <div
                    class="artist-avatar"
                    style="background: hsl(${hue}, 45%, 35%)"
                >
                    ${this.artistImageURL
                        ? html`<img
                              src="${this.artistImageURL}"
                              alt="${this.displayName}"
                          />`
                        : this.getInitial(this.displayName)}
                </div>
                <div class="artist-info">
                    <h1 class="artist-title" title="${this.displayName}">
                        ${this.displayName}
                    </h1>
                    ${this.artist?.englishName
                        ? html`<div class="artist-native-name">${this.artist.name}</div>`
                        : nothing}
                    ${this.renderArtistMeta()}
                </div>
            </div>
            <div class="content">
                ${this.renderTopTracks()} ${this.renderDiscography()}
                ${this.renderSimilarArtists()}
            </div>
        `;
    }

    private renderArtistMeta() {
        if (this.loadingArtist) {
            return html`<span class="artist-meta section-loading"
                >Loading\u2026</span
            >`;
        }
        if (this.errorArtist) {
            return html`<span class="artist-meta"
                >${this.errorArtist}</span
            >`;
        }
        if (!this.artist) return nothing;

        const parts: string[] = [];
        if (this.artist.type) parts.push(this.artist.type);
        if (this.artist.country) parts.push(this.artist.country);
        if (this.artist.disambiguation) parts.push(this.artist.disambiguation);

        if (parts.length === 0) return nothing;

        return html`
            <span class="artist-meta">
                ${parts.map(
                    (p, i) =>
                        html`${i > 0
                            ? html`<span class="meta-separator">\u00B7</span>`
                            : nothing}${p}`,
                )}
            </span>
        `;
    }

    /* ── Top Tracks Section ── */

    private renderTopTracks() {
        if (this.loadingTracks) {
            return html`
                <section>
                    <h3 class="section-header">Top Tracks</h3>
                    <div class="section-loading">Loading\u2026</div>
                </section>
            `;
        }
        if (this.errorTracks) {
            return html`
                <section>
                    <h3 class="section-header">Top Tracks</h3>
                    <div class="section-error">
                        <wa-icon name="triangle-exclamation"></wa-icon>
                        ${this.errorTracks}
                    </div>
                </section>
            `;
        }
        if (this.topTracks.length === 0) return nothing;

        return html`
            <section>
                <h3 class="section-header">Top Tracks</h3>
                <div class="track-list">
                    ${this.topTracks.map(
                        (t, i) => html`
                            <div class="track-item">
                                <span class="track-rank">${i + 1}</span>
                                <div class="track-info">
                                    <div class="track-title">
                                        ${t.trackName}
                                    </div>
                                    <div class="track-artist">
                                        ${t.artistName}
                                    </div>
                                </div>
                                <span class="track-listens">
                                    ${formatListenCount(t.totalListenCount)}
                                    plays
                                </span>
                            </div>
                        `,
                    )}
                </div>
            </section>
        `;
    }

    /* ── Discography Section ── */

    private renderDiscography() {
        if (this.loadingReleases) {
            return html`
                <section>
                    <h3 class="section-header">Discography</h3>
                    <div class="section-loading">Loading\u2026</div>
                </section>
            `;
        }
        if (this.errorReleases) {
            return html`
                <section>
                    <h3 class="section-header">Discography</h3>
                    <div class="section-error">
                        <wa-icon name="triangle-exclamation"></wa-icon>
                        ${this.errorReleases}
                    </div>
                </section>
            `;
        }
        if (this.releaseGroups.length === 0) return nothing;

        const groups = this.groupByType();

        return html`
            <section>
                <h3 class="section-header">Discography</h3>
                ${groups.map(
                    (g) => html`
                        <div class="disco-group">
                            <h4 class="disco-type-header">
                                ${g.type === 'Other' ? 'Other Releases' : g.type.endsWith('s') ? g.type : `${g.type}s`}
                            </h4>
                            <div class="album-grid">
                                ${g.items.map((rg) => this.renderAlbumCard(rg))}
                            </div>
                        </div>
                    `,
                )}
            </section>
        `;
    }

    private renderAlbumCard(rg: MBReleaseGroup) {
        const artURL = CoverArtGroupURL(rg.mbid);
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
                    <div class="album-art-fallback" style="display: none">
                        <wa-icon name="compact-disc"></wa-icon>
                    </div>
                </div>
                <div class="album-title" title="${rg.title}">${rg.title}</div>
                <div class="album-meta">
                    ${this.libraryMBIDs.has(rg.mbid)
                        ? html`<span class="library-badge">In Library</span>`
                        : nothing}
                    ${year ? html`<span>${year}</span>` : nothing}
                </div>
            </div>
        `;
    }

    /* ── Similar Artists Section ── */

    private renderSimilarArtists() {
        // D024: when loading or empty/null, simply omit the section.
        if (this.loadingSimilar || this.similarArtists.length === 0) {
            return nothing;
        }

        return html`
            <section>
                <h3 class="section-header">Similar Artists</h3>
                <div class="horizontal-row">
                    ${this.similarArtists.map((a) => {
                        const hue = nameToHue(a.name);
                        const imgURL = this.similarImageURLs.get(a.artistMbid);
                        return html`
                            <div
                                class="similar-artist-card"
                                @click=${() => this.navigateToSimilarArtist(a)}
                                role="button"
                                tabindex="0"
                                @keydown=${(e: KeyboardEvent) => {
                                    if (
                                        e.key === 'Enter' ||
                                        e.key === ' '
                                    ) {
                                        e.preventDefault();
                                        this.navigateToSimilarArtist(a);
                                    }
                                }}
                            >
                                <div
                                    class="similar-avatar"
                                    style="background: hsl(${hue}, 45%, 35%)"
                                >
                                    ${imgURL
                                        ? html`<img
                                              src="${imgURL}"
                                              alt="${a.name}"
                                              @error=${this.handleSimilarImageError}
                                          />`
                                        : a.name.charAt(0).toUpperCase()}
                                </div>
                                <div
                                    class="similar-name"
                                    title="${a.name}"
                                >
                                    ${a.name}
                                </div>
                            </div>
                        `;
                    })}
                </div>
            </section>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'explore-artist-details': ExploreArtistDetails;
    }
}

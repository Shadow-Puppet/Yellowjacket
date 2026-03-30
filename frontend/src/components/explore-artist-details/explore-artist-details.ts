import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import {
    LookupArtist,
    BrowseReleaseGroups,
    TopRecordingsForArtist,
    TopReleaseGroupsForArtist,
    SimilarArtists,
    GetArtistImageURL,
    GetArtistPlayCount,
    GetLibrarySimilarArtists,
    CheckLibraryMBIDs,
} from '@go/explore/Service';
import type {
    MBArtist,
    MBReleaseGroup,
    LBTopRecording,
    LBTopReleaseGroup,
    LBSimilarArtist,
} from '@go/explore/Service';
import { exploreCache } from '../../store/explore-cache';
import { exploreSettings } from '../../store/explore-settings';
import { libraryStore } from '../../store/library-store';
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
    @state() private topReleaseGroups: LBTopReleaseGroup[] = [];
    @state() private releaseGroups: MBReleaseGroup[] = [];
    @state() private loadingArtist = true;
    @state() private loadingTracks = true;
    @state() private loadingTopReleases = true;
    @state() private loadingReleases = true;
    @state() private errorArtist = '';
    @state() private errorTracks = '';
    @state() private errorReleases = '';
    @state() private similarArtists: LBSimilarArtist[] = [];
    @state() private loadingSimilar = true;
    @state() private artistImageURL = '';
    @state() private similarImageURLs = new Map<string, string>();
    @state() private topSectionExpanded = false;
    @state() private artistPlayCount = 0;
    @state() private expandedDiscoGroups = new Set<string>();
    @state() private similarExpanded = false;
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
                box-sizing: border-box;
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

            /* ── Top section (tracks + releases side-by-side) ── */
            .top-section-columns {
                display: grid;
                grid-template-columns: 1fr 1fr;
                gap: 24px;
            }

            .top-section-column {
                min-width: 0;
                display: flex;
                flex-direction: column;
            }

            .top-releases-grid {
                display: grid;
                grid-template-columns: 1fr 1fr;
                gap: 10px;
                flex: 1;
            }

            .top-release-card {
                display: flex;
                flex-direction: column;
                gap: 4px;
                cursor: pointer;
                transition: background 0.15s ease;
                padding: 4px;
                border-radius: 6px;
                min-width: 0;
            }

            .top-release-card:hover {
                background: var(
                    --yj-bg-overlay,
                    rgba(255, 255, 255, 0.06)
                );
            }

            .top-release-card:active {
                transform: scale(0.97);
            }

            .top-release-art {
                width: 100%;
                aspect-ratio: 1;
                border-radius: 4px;
                overflow: hidden;
                background: linear-gradient(
                    135deg,
                    var(--yj-bg-overlay, #404040) 0%,
                    var(--yj-bg-surface, #282828) 100%
                );
                position: relative;
            }

            .top-release-art img {
                width: 100%;
                height: 100%;
                object-fit: cover;
                display: block;
            }

            .top-release-art .album-art-fallback {
                position: absolute;
                inset: 0;
                display: flex;
                align-items: center;
                justify-content: center;
            }

            .top-release-art .album-art-fallback wa-icon {
                font-size: 20px;
                color: var(--yj-text-tertiary, #888);
                opacity: 0.5;
            }

            .top-release-text {
                min-width: 0;
                text-align: center;
            }

            .top-release-title {
                font-weight: 500;
                color: var(--yj-text-primary, #fff);
                font-size: var(--yj-text-sm);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .top-release-meta {
                display: flex;
                align-items: center;
                justify-content: center;
                gap: 6px;
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-xs);
            }

            .top-section-toggle {
                display: flex;
                align-items: center;
                justify-content: center;
                gap: 6px;
                padding: 6px 12px;
                margin-top: 12px;
                border: none;
                border-radius: 6px;
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: var(--yj-text-sm);
                cursor: pointer;
                transition: background 0.15s ease, color 0.15s ease;
                width: 100%;
            }

            .top-section-toggle:hover {
                background: var(--yj-bg-hover, rgba(255, 255, 255, 0.1));
                color: var(--yj-text-primary, #fff);
            }

            .top-section-toggle wa-icon {
                font-size: 12px;
                transition: transform 0.2s ease;
            }

            .top-section-toggle[aria-expanded='true'] wa-icon {
                transform: rotate(180deg);
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

            .album-grid.collapsed {
                grid-template-rows: 1fr;
                overflow: hidden;
            }

            .disco-toggle {
                display: flex;
                align-items: center;
                justify-content: center;
                gap: 6px;
                padding: 4px 10px;
                margin-top: 4px;
                border: none;
                border-radius: 6px;
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: var(--yj-text-xs);
                cursor: pointer;
                transition: background 0.15s ease, color 0.15s ease;
                width: 100%;
            }

            .disco-toggle:hover {
                background: var(--yj-bg-hover, rgba(255, 255, 255, 0.1));
                color: var(--yj-text-primary, #fff);
            }

            .disco-toggle wa-icon {
                font-size: 11px;
                transition: transform 0.2s ease;
            }

            .disco-toggle[aria-expanded='true'] wa-icon {
                transform: rotate(180deg);
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
            .similar-row {
                display: flex;
                flex-wrap: wrap;
                gap: 12px;
                overflow: hidden;
            }

            .similar-row.collapsed {
                max-height: 130px;
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

    private unsubSettings?: () => void;

    override connectedCallback() {
        super.connectedCallback();
        if (this.artistMBID) {
            void this.loadAllData();
        }

        // Re-render when library-only mode toggles.
        this.unsubSettings = exploreSettings.subscribe(() => {
            this.requestUpdate();
            if (this.artistMBID) {
                void this.loadAllData();
            }
        });
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.unsubSettings?.();
    }

    /* ── Data Loading ── */

    private async loadAllData() {
        const mbid = this.artistMBID;
        console.log(
            `[explore-artist] loading: "${this.artistName}" (${mbid})`,
        );

        // Phase 0: hydrate from caches (instant, no Go calls).
        this.hydrateFromCache(mbid);

        if (exploreSettings.libraryOnly) {
            // Library-only mode: no external API calls.
            // Discography comes from library store (already hydrated).
            // Similar artists from pre-computed DB table.
            this.loadingArtist = false;
            this.loadingTracks = false;
            this.loadingTopReleases = false;
            this.loadingReleases = false;
            this.loadingSimilar = false;

            // Fetch library-only similar artists (single Go call, no external API).
            try {
                const similar = await GetLibrarySimilarArtists(mbid);
                this.similarArtists = similar ?? [];
            } catch {
                this.similarArtists = [];
            }

            console.log(
                `[explore-artist] loaded (library-only): "${this.artistName}"`,
            );

            return;
        }

        // Phase 1: fire all API requests in parallel.
        const [artistResult, tracksResult, topReleasesResult, releasesResult, similarResult] =
            await Promise.allSettled([
                this.fetchArtist(mbid),
                this.fetchTopTracks(mbid),
                this.fetchTopReleaseGroups(mbid),
                this.fetchReleaseGroups(mbid),
                this.fetchSimilarArtists(mbid),
            ]);

        // Artist image is fire-and-forget — doesn't block the page.
        if (!this.artistImageURL) {
            this.fetchArtistImage(mbid);
        }

        // Play count is fire-and-forget.
        this.fetchArtistPlayCount(mbid);

        // Check which release groups are in the local library.
        this.checkLibrary();

        const summary = [
            `artist=${artistResult.status}`,
            `tracks=${tracksResult.status}`,
            `topReleases=${topReleasesResult.status}`,
            `releases=${releasesResult.status}`,
            `similar=${similarResult.status}`,
        ].join(', ');
        console.log(
            `[explore-artist] loaded: "${this.artistName}" (${summary})`,
        );
    }

    /**
     * Hydrate state from the explore cache and library store.
     * Shows cached data instantly before API calls complete.
     */
    private hydrateFromCache(mbid: string) {
        // Artist image: check explore cache first, then library store.
        if (!this.artistImageURL) {
            const cachedArtist = exploreCache.getArtist(mbid);
            if (cachedArtist) {
                this.artistImageURL = cachedArtist.imageURL
                    || cachedArtist.imageMedium
                    || cachedArtist.imageSmall
                    || '';
            }
        }

        if (!this.artistImageURL) {
            const cachedArtists = libraryStore.cachedArtists;
            if (cachedArtists) {
                for (const a of cachedArtists) {
                    if (a.MBID === mbid) {
                        this.artistImageURL = a.ImageMedium || a.ImageSmall || '';
                        break;
                    }
                }
            }
        }

        // Library albums by this artist — show as discography instantly.
        const cachedAlbums = libraryStore.cachedAlbums;
        if (cachedAlbums) {
            const artistName = this.artistName.toLowerCase();
            const libraryAlbums: MBReleaseGroup[] = [];
            for (const a of cachedAlbums) {
                if (a.ArtistName.toLowerCase() === artistName) {
                    libraryAlbums.push({
                        mbid: a.MBID || '',
                        title: a.Name,
                        primaryType: 'Album',
                        artistCredit: a.ArtistName,
                        firstReleaseDate: a.Year ? String(a.Year) : '',
                    } as MBReleaseGroup);
                }
            }

            if (libraryAlbums.length > 0) {
                this.releaseGroups = libraryAlbums;
                this.loadingReleases = false;
            }
        }
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

    private async fetchTopReleaseGroups(mbid: string) {
        try {
            const rgs = await TopReleaseGroupsForArtist(mbid);
            this.topReleaseGroups = rgs ?? [];
        } catch (err) {
            const msg = err instanceof Error ? err.message : String(err);
            console.error(
                `[explore-artist] TopReleaseGroupsForArtist error: ${msg}`,
            );
            this.topReleaseGroups = [];
        } finally {
            this.loadingTopReleases = false;
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

    private async fetchArtistPlayCount(mbid: string) {
        try {
            const count = await GetArtistPlayCount(mbid);
            if (count > 0) {
                this.artistPlayCount = count;
            }
        } catch {
            // Non-critical.
        }
    }

    private async checkLibrary() {
        // Check frontend-side first.
        const cachedAlbums = libraryStore.cachedAlbums;
        if (cachedAlbums) {
            const localMBIDs = new Set<string>();
            for (const a of cachedAlbums) {
                if (a.MBID) localMBIDs.add(a.MBID);
            }

            let updated = false;

            for (const rg of this.releaseGroups) {
                if (rg.mbid && localMBIDs.has(rg.mbid)) {
                    this.libraryMBIDs.add(rg.mbid);
                    updated = true;
                }
            }

            if (updated) {
                this.requestUpdate();
                return;
            }
        }

        // Fallback to backend.
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
                    ${this.artistPlayCount > 0 && !exploreSettings.libraryOnly
                        ? html`<span class="artist-meta">${formatListenCount(this.artistPlayCount)} plays on ListenBrainz</span>`
                        : nothing}
                </div>
            </div>
            <div class="content">
                ${this.renderTopSection()} ${this.renderDiscography()}
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

    /* ── Top Section (tracks + releases side-by-side) ── */

    private toggleTopSection() {
        this.topSectionExpanded = !this.topSectionExpanded;
    }

    private toggleDiscoGroup(type: string) {
        const next = new Set(this.expandedDiscoGroups);
        if (next.has(type)) {
            next.delete(type);
        } else {
            next.add(type);
        }
        this.expandedDiscoGroups = next;
    }

    private renderTopSection() {
        // Library-only mode: no top tracks/releases from LB.
        if (exploreSettings.libraryOnly) return nothing;

        const hasTracks = !this.loadingTracks && this.topTracks.length > 0;
        const hasReleases = !this.loadingTopReleases && this.topReleaseGroups.length > 0;
        const tracksLoading = this.loadingTracks;
        const releasesLoading = this.loadingTopReleases;

        // Both still loading — show single loading state.
        if (tracksLoading && releasesLoading) {
            return html`
                <section>
                    <h3 class="section-header">Popular</h3>
                    <div class="section-loading">Loading\u2026</div>
                </section>
            `;
        }

        // Both done, neither has data.
        if (!tracksLoading && !releasesLoading && !hasTracks && !hasReleases) {
            return nothing;
        }

        const expanded = this.topSectionExpanded;
        const trackLimit = expanded ? 10 : 5;
        const releaseLimit = expanded ? 4 : 2;

        const tracks = this.topTracks.slice(0, trackLimit);
        const releases = this.topReleaseGroups.slice(0, releaseLimit);

        const canExpand =
            this.topTracks.length > 5 || this.topReleaseGroups.length > 2;

        return html`
            <section>
                <div class="top-section-columns">
                    ${hasTracks
                        ? html`
                              <div class="top-section-column">
                                  <h3 class="section-header">Top Tracks</h3>
                                  <div class="track-list">
                                      ${tracks.map(
                                          (t, i) => html`
                                              <div class="track-item">
                                                  <span class="track-rank"
                                                      >${i + 1}</span
                                                  >
                                                  <div class="track-info">
                                                      <div
                                                          class="track-title"
                                                      >
                                                          ${t.trackName}
                                                      </div>
                                                      <div
                                                          class="track-artist"
                                                      >
                                                          ${t.artistName}
                                                      </div>
                                                  </div>
                                                  <span class="track-listens">
                                                      ${formatListenCount(
                                                          t.totalListenCount,
                                                      )}
                                                      plays
                                                  </span>
                                              </div>
                                          `,
                                      )}
                                  </div>
                              </div>
                          `
                        : nothing}
                    ${hasReleases
                        ? html`
                              <div class="top-section-column">
                                  <h3 class="section-header">Top Releases</h3>
                                  <div class="top-releases-grid">
                                      ${releases.map((rg) =>
                                          this.renderTopReleaseCard(rg),
                                      )}
                                  </div>
                              </div>
                          `
                        : releasesLoading
                          ? html`
                                <div class="top-section-column">
                                    <h3 class="section-header">Top Releases</h3>
                                    <div class="section-loading">Loading\u2026</div>
                                </div>
                            `
                          : nothing}
                </div>
                ${canExpand
                    ? html`
                          <button
                              class="top-section-toggle"
                              @click=${this.toggleTopSection}
                              aria-expanded="${expanded}"
                          >
                              ${expanded ? 'Show less' : 'Show more'}
                              <wa-icon
                                  name="chevron-down"
                              ></wa-icon>
                          </button>
                      `
                    : nothing}
            </section>
        `;
    }

    private renderTopReleaseCard(rg: LBTopReleaseGroup) {
        const artURL = CoverArtGroupURL(rg.releaseGroupMbid);

        return html`
            <div
                class="top-release-card"
                @click=${() => this.navigateToTopRelease(rg)}
                role="button"
                tabindex="0"
                @keydown=${(e: KeyboardEvent) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        this.navigateToTopRelease(rg);
                    }
                }}
            >
                <div class="top-release-art">
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
                <div class="top-release-text">
                    <div class="top-release-title" title="${rg.title}">
                        ${rg.title}
                    </div>
                    <div class="top-release-meta">
                        ${rg.date ? html`<span>${extractYear(rg.date)}</span>` : nothing}
                    </div>
                </div>
            </div>
        `;
    }

    private navigateToTopRelease(rg: LBTopReleaseGroup) {
        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: {
                    view: 'explore-album-details',
                    releaseGroupMBID: rg.releaseGroupMbid,
                    albumName: rg.title,
                },
            }),
        );
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
                    (g) => {
                        const isExpanded = this.expandedDiscoGroups.has(g.type);
                        const rowSize = 5;
                        const showToggle = g.items.length > rowSize;
                        const visibleItems = isExpanded ? g.items : g.items.slice(0, rowSize);

                        return html`
                            <div class="disco-group">
                                <h4 class="disco-type-header">
                                    ${g.type === 'Other' ? 'Other Releases' : g.type.endsWith('s') ? g.type : `${g.type}s`}
                                </h4>
                                <div class="album-grid">
                                    ${visibleItems.map((rg) => this.renderAlbumCard(rg))}
                                </div>
                                ${showToggle
                                    ? html`
                                          <button
                                              class="disco-toggle"
                                              aria-expanded="${isExpanded}"
                                              @click=${() => this.toggleDiscoGroup(g.type)}
                                          >
                                              ${isExpanded
                                                  ? 'Show less'
                                                  : `Show all ${g.items.length}`}
                                              <wa-icon name="chevron-down"></wa-icon>
                                          </button>
                                      `
                                    : nothing}
                            </div>
                        `;
                    },
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
        if (this.loadingSimilar || this.similarArtists.length === 0) {
            return nothing;
        }

        const showToggle = this.similarArtists.length > 6;
        const collapsed = !this.similarExpanded && showToggle;

        return html`
            <section>
                <h3 class="section-header">Similar Artists</h3>
                <div class="similar-row ${collapsed ? 'collapsed' : ''}">
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
                ${showToggle
                    ? html`
                          <button
                              class="disco-toggle"
                              aria-expanded="${this.similarExpanded}"
                              @click=${() => { this.similarExpanded = !this.similarExpanded; }}
                          >
                              ${this.similarExpanded
                                  ? 'Show less'
                                  : `Show all ${this.similarArtists.length}`}
                              <wa-icon name="chevron-down"></wa-icon>
                          </button>
                      `
                    : nothing}
            </section>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'explore-artist-details': ExploreArtistDetails;
    }
}

import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import {
    LookupReleaseGroup,
    BrowseReleases,
    GetThumbnail,
} from '@go/explore/Service';
import {
    GetAlbumTracks,
    GetFilePathsByAlbums,
    GetFilePathsByRecordingMBIDs,
} from '@go/library/Library';
import { library } from '@go/models';
import type { download, explore } from '@go/models';
type MBReleaseGroup = explore.MBReleaseGroup;
type MBRelease = explore.MBRelease;
type MBTrack = explore.MBTrack;
import { exploreCache } from '../../store/explore-cache';
import { libraryStore } from '../../store/library-store';
import { artistLink, exploreLinkStyles } from '../../utils/explore-link';
import { describeError } from '../../utils/describe-error';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '../library-status-indicator/library-status-indicator.js';
import { libraryStatusFor } from '@utils/library-status';
import type { LibraryStatus } from '../library-status-indicator/library-status-indicator';
import '../catalog-scope-notice/catalog-scope-notice.js';
import type { CatalogScope } from '../catalog-scope-notice/catalog-scope-notice.js';
import '@awesome.me/webawesome/dist/components/button/button.js';
import '../download-picker/download-picker';
import { downloadStore } from '../../store/download-store';
import { queueStore } from '../../store/queue-store';
import { notificationStore } from '../../store/notification-store';
import '../notifications/inline-notice';

/**
 * The region the album header's own failures are rendered in.
 *
 * "Inline" says *not global*, not *where* — so the region is named
 * once, here, rather than spelled at each call site.
 */
export const ExploreAlbumRegion = 'explore-album';

/* ── Utility functions (duplicated per Knowledge Pattern #9 — no cross-component imports) ── */

function extractYear(dateStr: string): string {
    if (!dateStr) return '';
    return dateStr.substring(0, 4);
}

/**
 * Convert a duration in milliseconds to a human-readable "m:ss" string.
 * Returns "0:00" for zero/negative/NaN values.
 */
function formatDuration(ms: number): string {
    if (!ms || ms <= 0) return '0:00';
    const totalSeconds = Math.round(ms / 1000);
    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;
    return `${minutes}:${String(seconds).padStart(2, '0')}`;
}

/* ── Types ── */

interface ReleaseCluster {
    representative: MBRelease;
    allReleases: MBRelease[];
    fingerprint: string;
    /** Standard-version score, computed by buildClusters. */
    score: number;
}

/** Synthetic kinds — virtual entries pinned at the top of the dropdown. */
type SyntheticKind = 'standard' | 'comprehensive' | 'library';

/** Unified version-entry shape covering both synthetic and real clusters. */
interface VersionEntry {
    /** Stable identifier for the dropdown <option value="...">. */
    key: string;
    /** Display label for the dropdown option. */
    label: string;
    /** Tooltip / sub-label. */
    sublabel: string;
    /** Visible group: "Aggregate" (synthetics) vs "Versions" (real clusters). */
    group: 'aggregate' | 'cluster';
    /** Synthetic identifier when group is aggregate. */
    syntheticKind?: SyntheticKind;
    /** Track set to render when this entry is selected. */
    tracks: MBTrack[];
    /** Underlying cluster for real entries; undefined for synthetics. */
    cluster?: ReleaseCluster;
    /**
     * For synthetics: the cluster (or synthetic source) that this entry
     * was *built from*, used for the metadata header.  For real clusters,
     * always equal to `cluster`.
     */
    backingCluster?: ReleaseCluster;
}

/* ── Component ── */

@customElement('explore-album-details')
export class ExploreAlbumDetails extends LitElement {
    /* ── Public attributes ── */

    @property({ type: String, attribute: 'release-group-mbid' })
    releaseGroupMBID = '';

    @property({ type: String, attribute: 'album-name' })
    albumName = '';

    @property({ type: String, attribute: 'artist-name' })
    artistName = '';

    @property({ type: String, attribute: 'highlight-track-mbid' })
    highlightTrackMBID = '';

    /** Highlight target for a track with no recording MBID — the only
     * handle an untagged track has is its title. */
    @property({ type: String, attribute: 'highlight-track-title' })
    highlightTrackTitle = '';

    @property({ type: Number, attribute: 'local-album-id' })
    localAlbumId = 0;

    /* ── Internal state ── */

    @state() private releaseGroup: MBReleaseGroup | null = null;
    @state() private releases: MBRelease[] = [];
    @state() private loadingInfo = true;
    @state() private loadingReleases = true;
    @state() private errorInfo = '';
    @state() private errorReleases = '';
    /** True once the versions/tracklist on screen came from the catalog
     * rather than standing in from the local library. */
    @state() private catalogReleasesLoaded = false;
    /** True while a catalog fetch (foreground or background) may still
     * land.  Distinct from loadingReleases, which goes false as soon as
     * *something* is renderable — including a library stand-in. */
    @state() private catalogPending = false;
    /** Unified entries shown in the dropdown — synthetics first, then real clusters. */
    @state() private versionEntries: VersionEntry[] = [];
    /** Currently-selected dropdown entry (by VersionEntry.key). */
    @state() private selectedVersionKey: string = '';
    @state() private coverArtURL = '';

    /** Open state of the "find this album" dialog. */
    @state() private pickerOpen = false;

    /** True once a download client is configured and enabled. */
    @state() private canDownload = false;

    /** True when this album already has a request. */
    @state() private isRequested = false;

    /**
     * Library to attach downloads/requests to.  The library-filter UI that
     * would normally set libraryStore's selection isn't mounted anywhere
     * currently, so that selection is always null here — falling back to
     * `?? 0` would send a library id that doesn't exist and fail the
     * download_requests/download_wants foreign key.  Resolved to the
     * selected library, or the first one if none is selected.
     */
    @state() private targetLibraryId: number | null = null;

    /** Unsubscribe handle for the download store. */
    private downloadUnsub: (() => void) | null = null;

    /* ── Styles ── */

    static override styles = [
        designTokens,
        exploreLinkStyles,
        css`
            :host {
                display: flex;
                flex-direction: column;
                overflow: hidden;
                height: 100%;
                box-sizing: border-box;
            }

            /* ── Header ── */
            .album-header {
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

            .cover-art-container {
                width: 200px;
                height: 200px;
                border-radius: 6px;
                overflow: hidden;
                background: linear-gradient(
                    135deg,
                    var(--yj-bg-overlay, #404040) 0%,
                    var(--yj-bg-surface, #282828) 100%
                );
                display: flex;
                align-items: center;
                justify-content: center;
                flex-shrink: 0;
                position: relative;
            }

            .cover-art-container img {
                width: 100%;
                height: 100%;
                object-fit: cover;
                display: block;
            }

            .cover-art-fallback {
                display: flex;
                align-items: center;
                justify-content: center;
                width: 100%;
                height: 100%;
                position: absolute;
                inset: 0;
            }

            .cover-art-fallback wa-icon {
                color: var(--yj-text-tertiary, #888);
                font-size: 48px;
                opacity: 0.5;
            }

            .album-info {
                display: flex;
                flex-direction: column;
                gap: 4px;
                min-width: 0;
            }

            .album-title {
                font-size: 24px;
                font-weight: 700;
                color: var(--yj-text-primary, #fff);
                margin: 0;
                line-height: 1.2;
                display: flex;
                align-items: center;
                gap: 12px;
                min-width: 0;
            }

            .album-title-text {
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                min-width: 0;
            }

            .album-title library-status-indicator {
                flex-shrink: 0;
            }

            .album-actions {
                display: flex;
                flex-wrap: wrap;
                align-items: center;
                gap: 8px;
                margin-top: 8px;
            }

            /*
             * The sentence under a partial Play button. It repeats the
             * count on the button on purpose: the button has to be
             * short and the claim has to be unambiguous, and "Play 7 of
             * 12" alone does not say whether the other five are missing
             * or merely unselected.
             */
            .album-owned-note {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-secondary, #aaa);
            }

            .album-artist {
                font-size: var(--yj-text-lg);
                color: var(--yj-text-secondary, #b3b3b3);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .album-meta {
                font-size: var(--yj-text-md);
                color: var(--yj-text-tertiary, #888);
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
                gap: 24px;
            }

            /* ── Section headers ── */
            .tracklist-legend {
                display: inline-flex;
                align-items: center;
                gap: 4px;
                margin-left: 10px;
                font-weight: 400;
                text-transform: none;
                letter-spacing: 0;
                color: var(--yj-text-tertiary, #888);
            }

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

            /* ── Version selector ── */
            .version-selector {
                display: flex;
                flex-direction: column;
                gap: 8px;
                padding: 10px 16px;
                background: var(--yj-surface-1, rgba(255, 255, 255, 0.04));
                border-radius: 8px;
            }

            .version-selector-row {
                display: flex;
                align-items: center;
                gap: 10px;
            }

            .version-selector label {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-secondary, #b3b3b3);
                font-weight: 600;
                text-transform: uppercase;
                letter-spacing: 0.04em;
                white-space: nowrap;
                flex-shrink: 0;
            }

            .version-selector select {
                flex: 1;
                min-width: 0;
                padding: 6px 10px;
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                color: var(--yj-text-primary, #fff);
                border: 1px solid var(--yj-border-subtle, rgba(255, 255, 255, 0.08));
                border-radius: 6px;
                font-size: var(--yj-text-md);
                font-family: inherit;
                cursor: pointer;
                appearance: none;
                -webkit-appearance: none;
                background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%23888' d='M3 5l3 3 3-3'/%3E%3C/svg%3E");
                background-repeat: no-repeat;
                background-position: right 8px center;
                padding-right: 28px;
            }

            .version-selector select:focus {
                outline: none;
                border-color: var(--yj-border-focus, rgba(255, 255, 255, 0.2));
            }

            .version-selector select option {
                background: var(--yj-bg-surface, #1e1e1e);
                color: var(--yj-text-primary, #fff);
            }

            .version-meta {
                font-size: var(--yj-text-xs);
                color: var(--yj-text-tertiary, #888);
                line-height: 1.4;
                padding-left: 2px;
            }

            /* ── Tracklist ── */
            .tracklist {
                display: flex;
                flex-direction: column;
            }

            .disc-separator {
                display: flex;
                align-items: center;
                gap: 12px;
                padding: 12px 0 8px;
            }

            .disc-separator::before,
            .disc-separator::after {
                content: '';
                flex: 1;
                height: 1px;
                background: var(--yj-border-subtle, rgba(255, 255, 255, 0.06));
            }

            .disc-label {
                font-size: var(--yj-text-xs);
                font-weight: 600;
                color: var(--yj-text-tertiary, #888);
                text-transform: uppercase;
                letter-spacing: 0.05em;
                white-space: nowrap;
            }

            .track-row {
                display: flex;
                align-items: center;
                gap: 12px;
                padding: 7px 12px;
                border-radius: 6px;
                cursor: default;
                transition: background 0.1s ease;
            }

            .track-row:hover {
                background: var(
                    --yj-bg-overlay,
                    rgba(255, 255, 255, 0.04)
                );
            }

            .track-row.highlighted {
                background: var(
                    --yj-bg-overlay,
                    rgba(255, 255, 255, 0.04)
                );
            }

            .track-position {
                width: 28px;
                text-align: right;
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-md);
                font-variant-numeric: tabular-nums;
                flex-shrink: 0;
            }

            .track-title {
                flex: 1;
                min-width: 0;
                font-weight: 500;
                color: var(--yj-text-primary, #fff);
                font-size: var(--yj-text-md);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .track-duration {
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-sm);
                font-variant-numeric: tabular-nums;
                flex-shrink: 0;
                white-space: nowrap;
            }

            .track-row library-status-indicator {
                flex-shrink: 0;
            }
        `,
    ];

    /* ── Lifecycle ── */

    private unsubReleasesReady?: () => void;
    /** Release-group MBIDs whose AlbumReleasesReady event we've handled,
     * so a background BrowseReleases fetch re-hydrates versions once. */
    private releasesReloaded = new Set<string>();
    /** Fallback timer that stops the versions spinner if the background
     * BrowseReleases fetch never signals readiness. */
    private releasesFallbackTimer?: number;

    override connectedCallback() {
        super.connectedCallback();
        if (this.releaseGroupMBID || this.localAlbumId) {
            void this.loadAllData();
        }

        // The download button only appears once a client is connected,
        // so this tracks the provider list rather than assuming.
        this.downloadUnsub = downloadStore.subscribe(() => {
            this.canDownload = downloadStore.available;
            this.syncRequested();
        });

        void downloadStore.init().then(() => {
            this.canDownload = downloadStore.available;
            this.syncRequested();
        });

        void this.resolveTargetLibraryId();

        // A background BrowseReleases fetch (cold album, versions +
        // tracklist not cached yet) finished — re-fetch the versions once
        // per release group so they fill in without the initial request
        // having blocked on a live MusicBrainz browse.
        this.unsubReleasesReady = EventsOn(
            Events.AlbumReleasesReady,
            (mbid: string) => {
                if (mbid !== this.releaseGroupMBID) return;
                if (this.releasesReloaded.has(mbid)) return;

                if (this.releasesFallbackTimer) clearTimeout(this.releasesFallbackTimer);
                this.releasesReloaded.add(mbid);
                void this.fetchReleases(mbid);
            },
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.downloadUnsub?.();
        this.downloadUnsub = null;
        this.unsubReleasesReady?.();
        if (this.releasesFallbackTimer) clearTimeout(this.releasesFallbackTimer);
    }

    /**
     * Arm a one-shot fallback that stops the versions spinner if
     * AlbumReleasesReady never arrives (e.g. the background browse stalled
     * or the release group genuinely has no releases).
     */
    private armReleasesFallback(mbid: string) {
        if (this.releasesFallbackTimer) clearTimeout(this.releasesFallbackTimer);

        this.releasesFallbackTimer = window.setTimeout(() => {
            if (this.releasesReloaded.has(mbid)) return;

            this.releasesReloaded.add(mbid);
            this.catalogPending = false;
            if (this.releases.length === 0) this.loadingReleases = false;
        }, 12000);
    }

    /** Whether we've already scrolled to the highlight target. */
    private hasScrolledToHighlight = false;

    override updated() {
        if (
            (this.highlightTrackMBID || this.highlightTrackTitle) &&
            !this.hasScrolledToHighlight &&
            !this.loadingReleases
        ) {
            const el = this.highlightTrackMBID
                ? this.shadowRoot?.querySelector<HTMLElement>(
                      `[data-track-mbid="${this.highlightTrackMBID}"]`,
                  )
                : this.findRowByTitle(this.highlightTrackTitle);

            if (el) {
                this.hasScrolledToHighlight = true;

                // Apply highlight imperatively — avoids CSS
                // transition/animation conflicts with the hover
                // transition on .track-row.
                const hoverBg = 'rgba(255, 255, 255, 0.04)';

                requestAnimationFrame(() => {
                    el.scrollIntoView({
                        behavior: 'smooth',
                        block: 'center',
                    });

                    // Two quick pulses after scroll settles.
                    setTimeout(() => {
                        el.style.transition = 'background 0.22s ease-in';
                        el.style.background = hoverBg;

                        setTimeout(() => {
                            el.style.transition = 'background 0.22s ease-out';
                            el.style.background = '';

                            setTimeout(() => {
                                el.style.transition = 'background 0.22s ease-in';
                                el.style.background = hoverBg;

                                setTimeout(() => {
                                    el.style.transition = 'background 0.22s ease-out';
                                    el.style.background = '';

                                    setTimeout(() => {
                                        el.style.transition = 'background 0.22s ease-in';
                                        el.style.background = hoverBg;

                                        setTimeout(() => {
                                            el.style.transition = 'background 0.22s ease-out';
                                            el.style.background = '';
                                        }, 840);
                                    }, 112);
                                }, 560);
                            }, 112);
                        }, 560);
                    }, 300);
                });
            }
        }
    }

    /**
     * Locate a track row by title, for tracks with no recording MBID.
     * Matched in JS rather than by attribute selector because titles
     * contain quotes and other things a selector cannot carry.
     */
    private findRowByTitle(title: string): HTMLElement | null {
        const wanted = title.trim().toLowerCase();
        const rows = this.shadowRoot?.querySelectorAll<HTMLElement>(
            '.track-row[data-track-title]',
        );

        for (const row of rows ?? []) {
            if ((row.dataset.trackTitle ?? '').toLowerCase() === wanted) {
                return row;
            }
        }

        return null;
    }

    /* ── Data Loading ── */

    private async loadAllData() {
        const mbid = this.releaseGroupMBID;

        // Reset state for a clean reload (handles toggle switches).
        this.errorInfo = '';
        this.errorReleases = '';
        this.loadingInfo = true;
        this.loadingReleases = true;
        this.catalogReleasesLoaded = false;
        this.catalogPending = Boolean(this.releaseGroupMBID);
        this.releases = [];
        this.versionEntries = [];
        this.selectedVersionKey = '';

        // Local-only album (no MBID) — populate entirely from library.
        if (!mbid && this.localAlbumId) {

            await this.hydrateLocalOnly();


            return;
        }


        // Phase 0: hydrate from explore cache (instant).
        const cached = exploreCache.getAlbum(mbid);
        if (cached) {
            this.releaseGroup = {
                mbid: cached.mbid,
                title: cached.title,
                artistCredit: cached.artistName,
                firstReleaseDate: cached.year || '',
                primaryType: 'Album',
            } as MBReleaseGroup;
            this.loadingInfo = false;
        }

        // Phase 1: hydrate tracklist from local library if available.
        // Awaited for the side effect of populating the local
        // tracklist; the return value isn't currently consumed.
        await this.hydrateFromLibrary(mbid);

        // Phase 2: fire API calls independently so each section
        // renders as its data arrives.  Allow the versions section one
        // background-fetch re-fetch and arm a fallback so it can't spin
        // forever if AlbumReleasesReady never arrives.
        this.releasesReloaded.delete(mbid);
        this.armReleasesFallback(mbid);
        void this.fetchReleaseGroup(mbid);
        void this.fetchReleases(mbid);

        // Fire cover art resolution immediately — doesn't wait for release group.
        this.resolveCoverArt();

    }

    /**
     * Hydrate album info and tracklist from the local library store.
     * Uses GetAlbumTracks(album.ID) — same local DB call as cover-grid.
     * Returns true if a tracklist was populated from local data.
     */
    /**
     * Populate the view entirely from local library data when the
     * album has no MusicBrainz release group ID.  Uses the local
     * album ID to fetch tracks and resolve metadata.
     */
    private async hydrateLocalOnly() {
        const cachedAlbums = libraryStore.cachedAlbums;
        let libraryAlbum: library.Album | null = null;

        if (cachedAlbums) {
            for (const a of cachedAlbums) {
                if (a.ID === this.localAlbumId) {
                    libraryAlbum = a;
                    break;
                }
            }
        }

        if (libraryAlbum) {
            this.releaseGroup = {
                mbid: '',
                title: libraryAlbum.Name,
                artistCredit: libraryAlbum.ArtistName,
                firstReleaseDate: libraryAlbum.Year ? String(libraryAlbum.Year) : '',
                primaryType: 'Album',
            } as MBReleaseGroup;
        }

        this.loadingInfo = false;

        // Fetch tracks via the same local DB call the cover-grid uses.
        let tracks: Awaited<ReturnType<typeof GetAlbumTracks>>;
        try {
            tracks = await GetAlbumTracks(this.localAlbumId);
        } catch {
            this.loadingReleases = false;
            return;
        }

        if (!tracks || tracks.length === 0) {
            this.loadingReleases = false;
            return;
        }

        const localTracks: MBTrack[] = tracks.map((t) => ({
            mbid: t.RecordingMBID || '',
            title: t.TrackName,
            position: t.TrackNumber || 0,
            length: this.parseDurationMs(t.TrackLength),
            discNumber: t.DiscNumber || 1,
            inLibrary: true,
        } as MBTrack));

        localTracks.sort((a, b) => {
            const d = (a.discNumber || 1) - (b.discNumber || 1);
            return d !== 0 ? d : a.position - b.position;
        });

        const localRelease: MBRelease = {
            mbid: '',
            title: libraryAlbum?.Name || this.albumName,
            date: libraryAlbum?.Year ? String(libraryAlbum.Year) : '',
            country: '',
            tracks: localTracks,
        } as MBRelease;

        this.releases = [localRelease];
        this.buildClusters();
        this.loadingReleases = false;
    }

    private async hydrateFromLibrary(mbid: string): Promise<boolean> {
        const cachedAlbums = libraryStore.cachedAlbums;
        if (!cachedAlbums) return false;

        // Find the library album: prefer MBID match, fall back to
        // name+artist match for albums without MBID tags.
        let libraryAlbum: typeof cachedAlbums[0] | null = null;

        if (mbid) {
            for (const a of cachedAlbums) {
                if (a.MBID === mbid) {
                    libraryAlbum = a;
                    break;
                }
            }
        }

        if (!libraryAlbum && this.albumName) {
            const targetName = this.albumName.toLowerCase();
            const targetArtist = this.artistName.toLowerCase();

            for (const a of cachedAlbums) {
                if (a.Name.toLowerCase() !== targetName) continue;
                if (targetArtist && a.ArtistName.toLowerCase() !== targetArtist) continue;
                libraryAlbum = a;
                break;
            }
        }

        if (!libraryAlbum) return false;

        // Populate release group info from library if not already set.
        if (!this.releaseGroup) {
            this.releaseGroup = {
                mbid,
                title: libraryAlbum.Name,
                artistCredit: libraryAlbum.ArtistName,
                firstReleaseDate: libraryAlbum.Year ? String(libraryAlbum.Year) : '',
                primaryType: 'Album',
            } as MBReleaseGroup;
            this.loadingInfo = false;
        }

        // Fetch tracks via the same local DB call the cover-grid uses.
        let tracks: Awaited<ReturnType<typeof GetAlbumTracks>>;
        try {
            tracks = await GetAlbumTracks(libraryAlbum.ID);
        } catch {
            return false;
        }

        if (!tracks || tracks.length === 0) return false;

        const localTracks: MBTrack[] = tracks.map((t) => ({
            mbid: t.RecordingMBID || '',
            title: t.TrackName,
            position: t.TrackNumber || 0,
            length: this.parseDurationMs(t.TrackLength),
            discNumber: t.DiscNumber || 1,
            inLibrary: true,
        } as MBTrack));

        // Sort by disc, then position.
        localTracks.sort((a, b) => {
            const d = (a.discNumber || 1) - (b.discNumber || 1);
            return d !== 0 ? d : a.position - b.position;
        });

        // Wrap into a single MBRelease.
        const localRelease: MBRelease = {
            mbid: '',
            title: libraryAlbum.Name,
            date: libraryAlbum.Year ? String(libraryAlbum.Year) : '',
            country: '',
            tracks: localTracks,
        } as MBRelease;

        this.releases = [localRelease];
        this.buildClusters();
        this.loadingReleases = false;

        return true;
    }

    /**
     * Parse a TrackLength value into milliseconds.
     * Library stores TrackLength as a string of milliseconds (e.g. "180000").
     * Also handles "m:ss" format as a fallback.
     */
    private parseDurationMs(duration: string): number {
        if (!duration) return 0;
        // Check for "m:ss" format
        if (duration.includes(':')) {
            const parts = duration.split(':');
            if (parts.length === 2) {
                const mins = parseInt(parts[0]!, 10);
                const secs = parseInt(parts[1]!, 10);
                if (!isNaN(mins) && !isNaN(secs)) {
                    return (mins * 60 + secs) * 1000;
                }
            }
        }
        // Raw milliseconds string (the library format)
        const n = Number(duration);
        return isNaN(n) ? 0 : n;
    }

    private async fetchReleaseGroup(mbid: string) {
        try {
            this.releaseGroup = await LookupReleaseGroup(mbid);
            this.resolveCoverArt();
        } catch (err) {
            console.error('[explore-album] LookupReleaseGroup error', err);
            this.errorInfo = describeError(
                err,
                'The catalog did not answer for this album.',
            );
        } finally {
            this.loadingInfo = false;
        }
    }

    /**
     * Resolve the album cover art through the backend proxy
     * (local library → disk cache → CAA).
     */
    private resolveCoverArt() {
        if (this.coverArtURL || !this.releaseGroupMBID) return;

        const albumName = this.releaseGroup?.title || this.albumName || '';
        const artistName = this.releaseGroup?.artistCredit || this.artistName || '';

        GetThumbnail(this.releaseGroupMBID, albumName, artistName)
            .then((url) => {
                if (url) {
                    this.coverArtURL = url;
                }
            })
            .catch(() => {});
    }

    private async fetchReleases(mbid: string) {
        try {
            const releases = await BrowseReleases(mbid);

            if (releases && releases.length > 0) {
                // Warm cache hit (or the background re-fetch landed):
                // authoritative MB versions replace any local placeholder.
                this.catalogReleasesLoaded = true;
                this.catalogPending = false;
                this.releases = releases;
                this.buildClusters();
                this.loadingReleases = false;
                return;
            }

            // Cold miss: BrowseReleases is cache-first + async and the
            // versions/tracklist are still being fetched in the background.
            // Don't clobber a tracklist already hydrated from the library —
            // keep showing it.  Hold the spinner only when there's nothing
            // on screen yet; AlbumReleasesReady (or the fallback) resolves it.
            if (this.releases.length > 0 || this.releasesReloaded.has(mbid)) {
                this.loadingReleases = false;
            }

            // A cold miss after the background fetch already signalled
            // ready is as far as the catalog is going to get.
            if (this.releasesReloaded.has(mbid)) {
                this.catalogPending = false;
            }
        } catch (err) {
            console.error('[explore-album] BrowseReleases error', err);
            this.errorReleases = describeError(
                err,
                'The catalog did not answer for this album\u2019s versions.',
            );
            this.loadingReleases = false;
            this.catalogPending = false;
        }
    }

    /* ── Release Clustering (R026) + Synthetic Versions ── */

    /**
     * Groups releases by identical tracklist fingerprints (ordered
     * recording MBIDs), scores each cluster, then builds the unified
     * version-entry list (synthetics + clusters) for the dropdown.
     */
    private buildClusters() {
        const clusterMap = new Map<string, MBRelease[]>();

        for (const release of this.releases) {
            const tracks = release.tracks ?? [];

            // Fingerprint: sort tracks by (discNumber, position), join MBIDs.
            // Releases whose tracks have no MBIDs all collapse together
            // under an "empty" fingerprint, which is intentional —
            // unidentifiable releases shouldn't multiply dropdown noise.
            const sorted = [...tracks].sort((a, b) => {
                const discDiff = (a.discNumber || 1) - (b.discNumber || 1);
                if (discDiff !== 0) return discDiff;
                return a.position - b.position;
            });
            const fingerprint = sorted.map((t) => t.mbid).join('|');

            const existing = clusterMap.get(fingerprint);
            if (existing) {
                existing.push(release);
            } else {
                clusterMap.set(fingerprint, [release]);
            }
        }

        // Build clusters with earliest-dated representative per group.
        const clusters: ReleaseCluster[] = [];
        for (const [fingerprint, releases] of clusterMap) {
            // Sort releases inside the cluster by date ascending so the
            // earliest serves as the cluster's representative — this
            // also gives us a stable "earliest official date" for
            // scoring later.
            const sorted = [...releases].sort((a, b) => {
                const da = a.date || '\uffff';
                const db = b.date || '\uffff';
                return da.localeCompare(db);
            });

            const cluster: ReleaseCluster = {
                representative: sorted[0]!,
                allReleases: sorted,
                fingerprint,
                score: 0, // filled in below
            };
            cluster.score = this.scoreCluster(cluster);
            clusters.push(cluster);
        }

        // Sort clusters by score descending so the highest-scoring
        // (best "standard" candidate) sits first.  Tiebreak by
        // earliest representative date.
        clusters.sort((a, b) => {
            if (b.score !== a.score) return b.score - a.score;
            const da = a.representative.date || '\uffff';
            const db = b.representative.date || '\uffff';
            return da.localeCompare(db);
        });

        // Build the unified version-entry list (synthetics + clusters).
        this.versionEntries = this.buildVersionEntries(clusters);

        // Default selection: prefer the user's library version when it
        // exists, otherwise the standard version, otherwise the first
        // entry available.
        const libraryEntry = this.versionEntries.find(
            (e) => e.syntheticKind === 'library',
        );
        const standardEntry = this.versionEntries.find(
            (e) => e.syntheticKind === 'standard',
        );
        this.selectedVersionKey =
            libraryEntry?.key
            || standardEntry?.key
            || this.versionEntries[0]?.key
            || '';
    }

    /**
     * Score a cluster as a "standard version" candidate.  Higher is
     * better.  Heuristic: prefer official, non-compilation, earliest
     * tracklists with broad consensus across releases.
     */
    private scoreCluster(cluster: ReleaseCluster): number {
        let score = 0;

        // Earliest-date base score: invert the ISO date so older = higher.
        // Use year as the integer score component — the rest is fine-grained
        // tiebreaking handled by the sort comparator, not the score itself.
        const earliestDate = cluster.representative.date || '';
        if (earliestDate.length >= 4) {
            const year = parseInt(earliestDate.slice(0, 4), 10);
            if (Number.isFinite(year)) {
                // Newer albums have larger year values, but we want
                // older = higher score, so invert against a future
                // ceiling.  Anything older than ~3000 ranks higher.
                score += 3000 - year;
            }
        }

        // Status filter: any official release in the cluster is a big
        // positive; pseudo / promo / bootleg pull the score down hard.
        let hasOfficial = false;
        let hasBadStatus = false;
        for (const r of cluster.allReleases) {
            const status = (r.status || '').toLowerCase();
            if (status === 'official') {
                hasOfficial = true;
            } else if (
                status === 'bootleg'
                || status === 'promotion'
                || status === 'pseudo-release'
                || status === 'withdrawn'
            ) {
                hasBadStatus = true;
            }
        }
        if (hasOfficial) score += 1000;
        if (hasBadStatus && !hasOfficial) score -= 1000;

        // Release group secondary type penalty: compilations, live
        // albums, remix collections, demos, and DJ mixes are not the
        // "standard" album.  Applies to every cluster equally since
        // it's a release-group property, but we still subtract so the
        // score values are comparable across release groups.
        const secondary = (this.releaseGroup?.secondaryTypes || []).map(
            (s: string) => s.toLowerCase(),
        );
        const badTypes = [
            'compilation',
            'live',
            'remix',
            'demo',
            'dj-mix',
            'soundtrack',
            'mixtape/street',
        ];
        for (const t of secondary) {
            if (badTypes.includes(t)) {
                score -= 500;
                break;
            }
        }

        // Cluster size bonus: log-scaled so a cluster of 10 isn't 10x
        // a cluster of 1, but a cluster of 30 still beats a cluster of
        // 5 by a meaningful amount.
        score += Math.log2(1 + cluster.allReleases.length) * 50;

        return score;
    }

    /**
     * Build the unified version-entry list: synthetic Standard /
     * Comprehensive / Library entries pinned at the top, followed by
     * the real clusters in score order.
     */
    private buildVersionEntries(clusters: ReleaseCluster[]): VersionEntry[] {
        const entries: VersionEntry[] = [];

        if (clusters.length === 0) return entries;

        // ── Standard ────────────────────────────────────────────────
        // Highest-scoring cluster is the standard.  We point at the
        // cluster directly so the standard entry is just a labeled
        // alias — selecting it shows the same tracks as selecting that
        // cluster from the bottom list.
        const standardCluster = clusters[0]!;
        entries.push({
            key: 'synthetic:standard',
            label: 'Standard',
            sublabel: this.standardSublabel(standardCluster),
            group: 'aggregate',
            syntheticKind: 'standard',
            tracks: standardCluster.representative.tracks ?? [],
            backingCluster: standardCluster,
        });

        // ── Comprehensive ───────────────────────────────────────────
        // Union of every distinct recording across all clusters.
        // Skip when only one cluster exists or when it would be
        // identical to standard (no extras to show).
        if (clusters.length > 1) {
            const comprehensiveTracks = this.buildComprehensiveTracklist(
                clusters,
                standardCluster,
            );
            const standardSize = (standardCluster.representative.tracks ?? [])
                .length;
            if (comprehensiveTracks.length > standardSize) {
                entries.push({
                    key: 'synthetic:comprehensive',
                    label: 'Comprehensive',
                    sublabel: `${comprehensiveTracks.length} tracks · all versions combined`,
                    group: 'aggregate',
                    syntheticKind: 'comprehensive',
                    tracks: comprehensiveTracks,
                    backingCluster: standardCluster,
                });
            }
        }

        // ── Library ─────────────────────────────────────────────────
        // If the user owns a copy of this album, find the cluster that
        // best matches their tracklist (by recording-MBID overlap) and
        // expose it as a synthetic alias.  Falls back to the standard
        // cluster's track set with whatever inLibrary flags the
        // backend has populated, which is "show me what I have."
        const libraryCluster = this.findLibraryCluster(clusters);
        if (libraryCluster) {
            entries.push({
                key: 'synthetic:library',
                label: 'Your Library',
                sublabel: this.librarySublabel(libraryCluster),
                group: 'aggregate',
                syntheticKind: 'library',
                tracks: libraryCluster.representative.tracks ?? [],
                backingCluster: libraryCluster,
            });
        }

        // ── Real clusters ───────────────────────────────────────────
        // Every distinct cluster gets its own entry under "Versions".
        // Already sorted by score descending from buildClusters.
        for (const cluster of clusters) {
            entries.push({
                key: `cluster:${cluster.representative.mbid}`,
                label: this.clusterLabel(cluster),
                sublabel: this.clusterSublabel(cluster),
                group: 'cluster',
                tracks: cluster.representative.tracks ?? [],
                cluster,
                backingCluster: cluster,
            });
        }

        return entries;
    }

    /**
     * Build the comprehensive (union) tracklist: every distinct
     * recording across all clusters, in a stable order.  Anchor on
     * the standard cluster's positions when possible, then append
     * any extras from other clusters in earliest-cluster order.
     */
    private buildComprehensiveTracklist(
        clusters: ReleaseCluster[],
        anchor: ReleaseCluster,
    ): MBTrack[] {
        const seen = new Set<string>();
        const result: MBTrack[] = [];

        // First pass: take the anchor cluster's tracks in order.
        const anchorTracks = anchor.representative.tracks ?? [];
        for (const t of anchorTracks) {
            if (!t.mbid || seen.has(t.mbid)) continue;
            seen.add(t.mbid);
            result.push(t);
        }

        // Second pass: walk other clusters in score order (which
        // sorts to "best to worst" via the existing comparator) and
        // append any track whose recording MBID hasn't been seen.
        // This biases extras toward versions that are themselves
        // closer to standard, which feels more natural than
        // pulling from random late-period anniversary editions.
        for (const cluster of clusters) {
            if (cluster === anchor) continue;
            const tracks = cluster.representative.tracks ?? [];
            for (const t of tracks) {
                if (!t.mbid || seen.has(t.mbid)) continue;
                seen.add(t.mbid);
                // Re-number positions sequentially so the renderer
                // doesn't show duplicate "track 5" rows from
                // different clusters.  The original disc/position
                // metadata is mostly meaningless once we've blended
                // versions anyway.
                result.push({
                    ...t,
                    position: result.length + 1,
                    discNumber: 1,
                });
            }
        }

        return result;
    }

    /**
     * Find the cluster that best matches the user's library tracks
     * for this album.  Returns the cluster with the highest fraction
     * of tracks marked inLibrary (i.e. the cluster the user actually
     * owns).  Returns null when the user has no tracks for this album.
     */
    private findLibraryCluster(
        clusters: ReleaseCluster[],
    ): ReleaseCluster | null {
        let best: ReleaseCluster | null = null;
        let bestScore = 0;

        for (const cluster of clusters) {
            const tracks = cluster.representative.tracks ?? [];
            if (tracks.length === 0) continue;

            const inLibCount = tracks.filter((t: MBTrack) => t.inLibrary).length;
            // Require at least half the cluster's tracks to be owned
            // before we'd label it "your library version" — otherwise
            // a single matching recording would falsely promote a
            // huge anniversary edition.
            const fraction = inLibCount / tracks.length;
            if (fraction < 0.5) continue;

            // Tiebreak: prefer the smaller (closer-fit) cluster when
            // multiple clusters meet the threshold.  E.g., if the
            // user owns the standard 12-track edition and we also see
            // a 14-track deluxe with the same 12 + 2 extras, the
            // standard fits better.
            const score = fraction * 1000 - tracks.length;
            if (score > bestScore) {
                best = cluster;
                bestScore = score;
            }
        }

        return best;
    }

    /* ── Synthetic version labelling ── */

    private standardSublabel(cluster: ReleaseCluster): string {
        const tracks = cluster.representative.tracks ?? [];
        const count = `${tracks.length} track${tracks.length === 1 ? '' : 's'}`;
        const year = (cluster.representative.date || '').slice(0, 4);
        const releases = cluster.allReleases.length;
        const releaseLabel = `${releases} release${releases === 1 ? '' : 's'}`;
        const parts = [count, releaseLabel];
        if (year) parts.push(year);
        return parts.join(' · ');
    }

    private librarySublabel(cluster: ReleaseCluster): string {
        const tracks = cluster.representative.tracks ?? [];
        const owned = tracks.filter((t: MBTrack) => t.inLibrary).length;
        return `${owned} of ${tracks.length} tracks owned`;
    }

    private clusterLabel(cluster: ReleaseCluster): string {
        // Prefer a distinguishing date.  Fall back to title when no
        // date is known.
        const date = cluster.representative.date;
        if (date) {
            return date;
        }
        return cluster.representative.title || 'Untitled release';
    }

    private clusterSublabel(cluster: ReleaseCluster): string {
        const tracks = cluster.representative.tracks ?? [];
        const count = `${tracks.length} track${tracks.length === 1 ? '' : 's'}`;
        const releases = cluster.allReleases.length;
        const releaseLabel
            = releases === 1
                ? '1 release'
                : `${releases} releases`;

        // Build "country list" from up to 3 distinct countries.
        const countries = new Set<string>();
        for (const r of cluster.allReleases) {
            if (r.country) countries.add(r.country);
            if (countries.size >= 3) break;
        }
        const countryStr = countries.size > 0
            ? Array.from(countries).join(', ')
            : '';

        const parts = [count, releaseLabel];
        if (countryStr) parts.push(countryStr);
        return parts.join(' · ');
    }

    /* ── Navigation ── */

    private navigateBack() {
        this.dispatchEvent(
            new CustomEvent('navigate-back', {
                bubbles: true,
                composed: true,
            }),
        );
    }

    /* ── Image Error Handling ── */

    private handleCoverError(e: Event) {
        const img = e.target as HTMLImageElement;
        img.style.display = 'none';
        const fallback = img.nextElementSibling as HTMLElement | null;
        if (fallback) {
            fallback.style.display = 'flex';
        }
    }

    /* ── Helpers ── */

    private handleVersionChange(e: Event) {
        const select = e.target as HTMLSelectElement;
        this.selectedVersionKey = select.value;
    }

    /** Resolve the currently-selected version entry, if any. */
    private currentVersion(): VersionEntry | null {
        if (!this.selectedVersionKey) return null;
        return (
            this.versionEntries.find(
                (e) => e.key === this.selectedVersionKey,
            ) ?? null
        );
    }

    /**
     * Resolve the album's overall library status for the header
     * indicator.  Cheap O(1)/O(n) lookups against data we already
     * have in memory — no Wails calls.
     *
     *   - localAlbumId set            → owned
     *   - releaseGroup.inLibrary set  → owned (backend cross-ref)
     *   - cachedAlbums has MBID match → owned
     *   - any selected version has    → owned (covers local-only albums
     *     a track marked inLibrary       where releaseGroup may be null)
     *   - else                        → whatever the request list says
     *
     * Four different claims of decreasing confidence, OR'd together and
     * reported as one tick — the last of which fires when a *single*
     * recording of a forty-track release matches.  `ownership()` is the
     * honest version of the same question and is what the header's
     * actions key off; this stays as it was, because the indicator's
     * job is "is any of this yours" and that is what it answers.
     *
     * When none of them hold the answer is not automatically "no":
     * the album may be on the request list, which the button directly
     * below this badge has reported as "Wanted" all along.
     */
    private albumLibraryStatus(): LibraryStatus {
        if (this.localAlbumId > 0) return 'in-library';

        if (this.releaseGroup?.inLibrary) return 'in-library';

        const mbid = this.releaseGroupMBID;
        if (mbid) {
            const cachedAlbums = libraryStore.cachedAlbums;
            if (cachedAlbums) {
                for (const a of cachedAlbums) {
                    if (a.MBID === mbid) return 'in-library';
                }
            }
        }

        const current = this.currentVersion();
        if (current) {
            for (const t of current.tracks) {
                if (t.inLibrary) return 'in-library';
            }
        }

        // None of the five ownership claims held, so the badge falls
        // through to the one thing this page already knew and never
        // said: whether the album is on the request list. The button
        // below it has read "Wanted" all along.
        return libraryStatusFor(false, this.releaseGroupMBID);
    }

    /**
     * How much of the shown release the user actually has.
     *
     * The tick beside the title is a yes/no answer to "is any of this
     * mine", and four of its five branches can be true when one track
     * of forty matches. That is fine for a badge and useless for a
     * button: "Play" that plays one track of a forty-track release is
     * worse than no Play button, so the header asks this instead.
     *
     * It is counted off the *tracklist being displayed*, which is the
     * one thing on this page that is not an inference — each track's
     * `inLibrary` is set by the backend from its recording MBID
     * (`markReleasesInLibrary`), the same key the file paths are
     * fetched by.
     */
    private ownership(): { owned: number; total: number } {
        const tracks = this.currentVersion()?.tracks ?? [];

        return {
            owned: tracks.filter((t) => t.inLibrary).length,
            total: tracks.length,
        };
    }

    /**
     * Say what the green tick against a track means.
     *
     * `H-13` calls the ticks "unexplained". Half of that has aged: the
     * indicator carries a `title` *and* an `aria-label` reading
     * "Track \u201cX\u201d is in your library", so a screen reader and a hover
     * both get a full sentence. What a sighted user scanning the page
     * gets is a column of green circles and no key, which is what this
     * is — rendered only when at least one track is actually ticked, so
     * it never explains a symbol that is not on screen.
     */
    private renderTracklistLegend() {
        const { owned } = this.ownership();

        if (owned === 0) return nothing;

        return html`<span class="tracklist-legend">
            <library-status-indicator
                status="in-library"
                entity-type="track"
                size="14"
            ></library-status-indicator>
            in your library
        </span>`;
    }

    /**
     * Check whether the selected release's tracklist contains any track
     * with discNumber > 1, indicating a multi-disc release.
     */
    private isMultiDisc(tracks: MBTrack[]): boolean {
        return tracks.some((t) => (t.discNumber || 1) > 1);
    }

    /**
     * Group tracks by disc number, returning them in disc order.
     */
    private groupByDisc(tracks: MBTrack[]): Map<number, MBTrack[]> {
        const discMap = new Map<number, MBTrack[]>();
        for (const track of tracks) {
            const disc = track.discNumber || 1;
            const bucket = discMap.get(disc);
            if (bucket) {
                bucket.push(track);
            } else {
                discMap.set(disc, [track]);
            }
        }
        // Sort tracks within each disc by position
        for (const bucket of discMap.values()) {
            bucket.sort((a, b) => a.position - b.position);
        }
        return discMap;
    }

    /* ── Render ── */

    override render() {
        return html`
            ${this.renderHeader()}
            <div class="content">
                <catalog-scope-notice
                    scope=${this.catalogScope()}
                    entity-type="album"
                    @catalog-retry=${this.retryCatalog}
                ></catalog-scope-notice>
                ${this.renderVersionSelector()}
                ${this.renderTracklist()}
            </div>
        `;
    }

    private renderHeader() {
        let artURL = this.coverArtURL;

        if (!artURL) {
            // Local fallback: try to find a library album that matches
            // by ID, MBID, or name+artist (in that priority).  Library
            // store has /covers/* paths that the dev server serves
            // directly without any backend round-trip.
            const cachedAlbums = libraryStore.cachedAlbums;
            if (cachedAlbums) {
                const targetName = (this.albumName || this.releaseGroup?.title || '').toLowerCase();
                const targetArtist = (this.artistName || this.releaseGroup?.artistCredit || '').toLowerCase();

                for (const a of cachedAlbums) {
                    let match = false;
                    if (this.localAlbumId && a.ID === this.localAlbumId) {
                        match = true;
                    } else if (this.releaseGroupMBID && a.MBID && a.MBID === this.releaseGroupMBID) {
                        match = true;
                    } else if (
                        targetName
                        && targetArtist
                        && a.Name.toLowerCase() === targetName
                        && a.ArtistName.toLowerCase() === targetArtist
                    ) {
                        match = true;
                    }

                    if (match) {
                        artURL = a.CoverArtMedium || a.CoverArtLarge || a.CoverArtPath || '';
                        if (artURL) break;
                    }
                }
            }
        }

        return html`
            <div class="album-header">
                <button
                    class="back-button"
                    @click=${this.navigateBack}
                    title="Back"
                    aria-label="Back to previous view"
                >
                    <wa-icon name="arrow-left"></wa-icon>
                </button>
                <div class="cover-art-container">
                    ${artURL
                        ? html`<img
                            src="${artURL}"
                            alt="${this.albumName}"
                            @error=${this.handleCoverError}
                        />`
                        : nothing}
                    <div class="cover-art-fallback" style="${artURL ? 'display: none' : ''}">
                        <wa-icon name="compact-disc"></wa-icon>
                    </div>
                </div>
                <div class="album-info">
                    <h1 class="album-title" title="${this.albumName}">
                        <span class="album-title-text">${this.albumName}</span>
                        <library-status-indicator
                            status=${this.albumLibraryStatus()}
                            entity-type="album"
                            label=${this.albumName}
                            size="22"
                        ></library-status-indicator>
                    </h1>
                    ${this.renderAlbumMeta()}
                    ${this.renderPlayActions()}
                    ${this.renderDownloadAction()}
                </div>
            </div>
            ${this.renderPicker()}
        `;
    }

    /**
     * The primary action, and the sentence that says what it will do.
     *
     * `H-13`: the album page had no Play, no Shuffle and no Add to
     * queue. What it could not have is a Play button that means the
     * same thing in every case — this is a *catalog* page, and the
     * album on it may be entirely yours, partly yours, or not yours at
     * all. So the button says which:
     *
     *   - all of it       → "Play", and the count is in the meta line
     *   - some of it      → "Play 7 of 12", because playing seven
     *                       tracks under a button that says "Play" is
     *                       the page lying about what you own
     *   - none of it      → no play button at all; the download and
     *                       want actions below are the whole answer
     *
     * The count is the tracklist's own `inLibrary` flags, which the
     * backend sets from each recording's MBID — the same key
     * `GetFilePathsByRecordingMBIDs` resolves the files by, so the
     * number on the button is the number of tracks that will play.
     */
    private renderPlayActions() {
        const { owned, total } = this.ownership();

        if (owned === 0 || total === 0) return nothing;

        const partial = owned < total;
        const playLabel = partial ? `Play ${owned} of ${total}` : 'Play';

        return html`
            <div class="album-actions">
                <wa-button
                    size="small"
                    appearance="filled"
                    data-testid="album-play"
                    @click=${() => void this.playOwned(false)}
                >
                    <wa-icon slot="start" name="play"></wa-icon>
                    ${playLabel}
                </wa-button>
                <wa-button
                    size="small"
                    appearance="outlined"
                    data-testid="album-shuffle"
                    @click=${() => void this.playOwned(true)}
                >
                    <wa-icon slot="start" name="shuffle"></wa-icon>
                    Shuffle album
                </wa-button>
                <wa-button
                    size="small"
                    appearance="outlined"
                    data-testid="album-queue"
                    @click=${() => void this.queueOwned()}
                >
                    <wa-icon slot="start" name="list"></wa-icon>
                    Add to queue
                </wa-button>
                ${partial
                    ? html`<span class="album-owned-note">
                          You have ${owned} of these ${total} tracks.
                      </span>`
                    : nothing}
            </div>
            <inline-notice
                region=${ExploreAlbumRegion}
                testid="album-action-message"
            ></inline-notice>
        `;
    }

    /**
     * File paths for the tracks of this release the user actually owns,
     * in the tracklist's order.
     *
     * One call. The obvious alternative — ask for the album's tracks
     * and read `FilePath` off them — is the shape `perf.m2` was about,
     * and it is not even available here: this page's model is
     * `MBTrack`, which carries a recording MBID and a `localId` that
     * nothing in the backend ever writes.
     */
    private async ownedFilePaths(): Promise<string[]> {
        const libraryID = libraryStore.getSelectedLibraryId() ?? 0;

        // The local album id is the better key whenever there is one:
        // it needs no MBIDs at all, and a library-only album has none —
        // its tracks are synthesised from `GetAlbumTracks` with
        // `mbid: RecordingMBID || ''`, so an untagged library resolves
        // to an empty set and the Play button silently does nothing.
        // That is exactly what the first version of this did.
        if (this.localAlbumId > 0) {
            const byAlbum = await GetFilePathsByAlbums(
                [this.localAlbumId],
                libraryID,
            );

            return byAlbum[this.localAlbumId] ?? [];
        }

        // Catalog-only: the page knows what is owned as recording MBIDs
        // and nothing else — which is how the backend decided each
        // track's `inLibrary` in the first place.
        const tracks = this.currentVersion()?.tracks ?? [];
        const mbids = tracks
            .filter((t) => t.inLibrary && t.mbid)
            .map((t) => t.mbid);

        if (mbids.length === 0) return [];

        const byMBID = await GetFilePathsByRecordingMBIDs(mbids, libraryID);

        // Walked in tracklist order rather than flattened, because the
        // grouping is what lets the caller keep its own order. A
        // recording with more than one file is a duplicate; play the
        // first and leave the rest to the feature that exists for them.
        const paths: string[] = [];

        for (const mbid of mbids) {
            const first = byMBID[mbid]?.[0];

            if (first) paths.push(first);
        }

        return paths;
    }

    /** Play what the user owns of this release, optionally shuffled. */
    private async playOwned(shuffle: boolean): Promise<void> {
        try {
            const paths = await this.ownedFilePaths();

            if (paths.length === 0) {
                notificationStore.inline(ExploreAlbumRegion, {
                    text: 'None of these tracks could be found in your library.',
                });

                return;
            }

            // `shuffleStart` only picks a random first track when
            // shuffle mode is *already* on — it does not turn it on —
            // so the mode has to be set before the queue, not after.
            if (shuffle && !queueStore.getState().shuffleMode) {
                queueStore.toggleShuffle();
            }

            queueStore.setQueue(paths, 0, shuffle);
        } catch (error) {
            console.error('Could not play album:', error);
            notificationStore.inline(ExploreAlbumRegion, {
                text: describeError(error, 'Could not play this album.'),
            });
        }
    }

    /** Append what the user owns of this release to the queue. */
    private async queueOwned(): Promise<void> {
        try {
            const paths = await this.ownedFilePaths();

            if (paths.length === 0) {
                notificationStore.inline(ExploreAlbumRegion, {
                    text: 'None of these tracks could be found in your library.',
                });

                return;
            }

            queueStore.addTracksToQueue(paths);
        } catch (error) {
            console.error('Could not queue album:', error);
            notificationStore.inline(ExploreAlbumRegion, {
                text: describeError(
                    error,
                    'Could not add this album to the queue.',
                ),
            });
        }
    }

    /**
     * Offers to acquire the album, but only when the user has actually
     * connected a download client and does not already own it. Showing
     * the button otherwise would advertise a feature that cannot work.
     */
    private renderDownloadAction() {
        if (this.albumLibraryStatus() === 'in-library') return nothing;

        return html`
            <div class="album-download">
                ${this.canDownload
                    ? html`
                          <wa-button
                              size="small"
                              appearance="outlined"
                              @click=${() => {
                                  void this.openPicker();
                              }}
                          >
                              <wa-icon slot="start" name="download"></wa-icon>
                              Find this album
                          </wa-button>
                      `
                    : nothing}
                ${this.renderWantAction()}
            </div>
        `;
    }

    /**
     * Adds the album to the requests list, which is the answer to "look
     * for it, but not right now".
     *
     * Unlike the download button this shows whether or not a client is
     * connected: requesting something is a durable statement about the
     * library, and it stays true — and stays queued — until a client
     * exists to act on it.
     */
    private renderWantAction() {
        if (!this.releaseGroupMBID) return nothing;

        const request = downloadStore.requestFor(this.releaseGroupMBID);

        return html`
            <wa-button
                size="small"
                appearance=${this.isRequested ? 'filled' : 'outlined'}
                @click=${() => void this.toggleRequested(request?.id)}
            >
                <!-- The requested state used to ask for bookmark-check,
                     which is a Font Awesome *Pro* name: never bundled,
                     so this button has rendered the missing-icon
                     fallback in that state ever since. Outline and solid
                     of the same Free glyph carry the toggle instead. -->
                <wa-icon
                    slot="start"
                    name=${this.isRequested ? 'solid/bookmark' : 'regular/bookmark'}
                ></wa-icon>
                ${this.isRequested ? 'Wanted' : 'Want this'}
            </wa-button>
        `;
    }

    /** Resolves the library to attach downloads/requests to. */
    private async resolveTargetLibraryId(): Promise<void> {
        this.targetLibraryId = await libraryStore.getDefaultLibraryId();
    }

    /** Reflects the store's view of whether this album is requested. */
    private syncRequested(): void {
        this.isRequested = this.releaseGroupMBID
            ? downloadStore.isRequested(this.releaseGroupMBID)
            : false;
    }

    private async toggleRequested(requestId: number | undefined): Promise<void> {
        if (!this.releaseGroupMBID) return;

        try {
            if (requestId) {
                await downloadStore.removeRequest(requestId);
            } else {
                if (!this.targetLibraryId) {
                    await this.resolveTargetLibraryId();
                }
                if (!this.targetLibraryId) {
                    console.error('Could not update the requests list: no library available');
                    return;
                }

                await downloadStore.addRequest({
                    mbid: this.releaseGroupMBID,
                    entity: 'release-group',
                    libraryId: this.targetLibraryId,
                    artist: this.releaseGroup?.artistCredit ?? '',
                    title: this.albumName,
                    scope: 'future',
                    secondary: false,
                } as download.RequestInput);
            }
        } catch (err) {
            console.error('Could not update the requests list:', err);
        }

        this.syncRequested();
    }

    private async openPicker(): Promise<void> {
        if (!this.targetLibraryId) {
            await this.resolveTargetLibraryId();
        }

        if (!this.targetLibraryId) {
            console.error('Cannot search for downloads: no library available');
            return;
        }

        this.pickerOpen = true;
    }

    private renderPicker() {
        if (!this.pickerOpen || !this.targetLibraryId) return nothing;

        const tracks = this.currentTracks();

        return html`
            <download-picker
                ?open=${this.pickerOpen}
                library-id=${this.targetLibraryId}
                artist=${this.releaseGroup?.artistCredit ?? ''}
                album=${this.albumName}
                release-group-mbid=${this.releaseGroupMBID ?? ''}
                .expected=${tracks.map((t, index) => ({
                    position: t.position || index + 1,
                    discNumber: t.discNumber ?? 0,
                    title: t.title,
                    artist: '',
                    lengthMillis: t.length ?? 0,
                }))}
                @picker-close=${() => {
                    this.pickerOpen = false;
                }}
            ></download-picker>
        `;
    }

    /**
     * Where the tracklist on screen came from.  The distinction the
     * user cares about is not "did a fetch fail" but "is what I am
     * looking at everything, or only my own copy" — so an album with
     * no MBID is `library` permanently, while one whose catalog fetch
     * has not landed is `loading` and then either resolves or degrades
     * to `unavailable`.
     */
    private catalogScope(): CatalogScope {
        if (!this.releaseGroupMBID) return 'library';
        if (this.catalogReleasesLoaded) return 'catalog';
        if (this.catalogPending) return 'loading';

        // Not pending and no catalog data: the browse errored, came
        // back empty, or the fallback timer gave up.  Whatever is on
        // screen is the library copy, and retrying is worth offering.
        return 'unavailable';
    }

    /** Ask the catalog again after a failed or empty fetch. */
    private retryCatalog = () => {
        const mbid = this.releaseGroupMBID;
        if (!mbid) return;

        this.errorReleases = '';
        this.loadingReleases = this.releases.length === 0;
        this.catalogPending = true;
        this.releasesReloaded.delete(mbid);
        this.armReleasesFallback(mbid);
        void this.fetchReleases(mbid);
    };

    /** Tracks of the version currently selected in the dropdown. */
    private currentTracks(): MBTrack[] {
        const entry = this.versionEntries.find(
            (e) => e.key === this.selectedVersionKey,
        );

        return entry?.tracks ?? [];
    }

    private renderAlbumMeta() {
        if (this.loadingInfo) {
            return html`<span class="album-artist section-loading"
                >Loading\u2026</span
            >`;
        }
        if (this.errorInfo) {
            return html`
                <div class="section-error">
                    <wa-icon name="triangle-exclamation"></wa-icon>
                    ${this.errorInfo}
                </div>
            `;
        }
        if (!this.releaseGroup) return nothing;

        const rg = this.releaseGroup;
        const artist = rg.artistCredit || '';
        const artistMbid = rg.artistMbid ?? '';
        const year = extractYear(rg.firstReleaseDate);
        const type = rg.primaryType || '';

        const metaParts: string[] = [];
        if (type) metaParts.push(type);
        if (year) metaParts.push(year);

        return html`
            ${artist
                ? html`<div class="album-artist">
                      ${artistLink(artist, artistMbid)}
                  </div>`
                : nothing}
            ${metaParts.length > 0
                ? html`
                      <span class="album-meta">
                          ${metaParts.map(
                              (p, i) =>
                                  html`${i > 0
                                      ? html`<span class="meta-separator"
                                            >\u00B7</span
                                        >`
                                      : nothing}${p}`,
                          )}
                      </span>
                  `
                : nothing}
        `;
    }

    /* ── Version Selector (R025, R026, R027) ── */

    private renderVersionSelector() {
        if (this.loadingReleases) {
            return html`
                <section>
                    <h3 class="section-header">Versions</h3>
                    <div class="section-loading">Loading releases\u2026</div>
                </section>
            `;
        }
        if (this.errorReleases) {
            return html`
                <section>
                    <h3 class="section-header">Versions</h3>
                    <div class="section-error">
                        <wa-icon name="triangle-exclamation"></wa-icon>
                        ${this.errorReleases}
                    </div>
                </section>
            `;
        }

        // No selector to show when there are no version entries at all,
        // or when the only entry is a single real cluster (no synthetics
        // were added because there's only one tracklist).
        if (this.versionEntries.length <= 1) return nothing;

        const aggregateEntries = this.versionEntries.filter(
            (e) => e.group === 'aggregate',
        );
        const clusterEntries = this.versionEntries.filter(
            (e) => e.group === 'cluster',
        );

        return html`
            <div class="version-selector">
                <div class="version-selector-row">
                    <label for="version-select">Version</label>
                    <select
                        id="version-select"
                        @change=${this.handleVersionChange}
                        aria-label="Select release version"
                    >
                        ${aggregateEntries.length > 0
                            ? html`
                                  <optgroup label="Aggregate">
                                      ${aggregateEntries.map(
                                          (e) => html`
                                              <option
                                                  value=${e.key}
                                                  ?selected=${e.key
                                                  === this.selectedVersionKey}
                                              >
                                                  ${e.label} — ${e.sublabel}
                                              </option>
                                          `,
                                      )}
                                  </optgroup>
                              `
                            : nothing}
                        <optgroup label="Versions">
                            ${clusterEntries.map(
                                (e) => html`
                                    <option
                                        value=${e.key}
                                        ?selected=${e.key
                                        === this.selectedVersionKey}
                                    >
                                        ${e.label} — ${e.sublabel}
                                    </option>
                                `,
                            )}
                        </optgroup>
                    </select>
                </div>
                ${this.renderVersionMeta()}
            </div>
        `;
    }

    /**
     * Render a small status line under the version selector explaining
     * what the user is currently viewing.  Helps disambiguate the
     * synthetic entries from real clusters.
     */
    private renderVersionMeta() {
        const current = this.currentVersion();
        if (!current) return nothing;

        switch (current.syntheticKind) {
            case 'standard':
                return html`
                    <div class="version-meta">
                        Showing the standard version — the most likely
                        canonical tracklist for this album, picked by
                        weighing release date, status, and how many
                        physical releases share this exact tracklist.
                    </div>
                `;
            case 'comprehensive':
                return html`
                    <div class="version-meta">
                        Showing every distinct track that appears on any
                        version of this album combined into a single
                        list.  Not a real release.
                    </div>
                `;
            case 'library':
                return html`
                    <div class="version-meta">
                        Showing the version that best matches your
                        library copy.
                    </div>
                `;
            default:
                if (current.cluster) {
                    const releases = current.cluster.allReleases.length;
                    if (releases > 1) {
                        return html`
                            <div class="version-meta">
                                ${releases} physical releases share this
                                tracklist.
                            </div>
                        `;
                    }
                }
                return nothing;
        }
    }

    /* ── Tracklist ── */

    private renderTracklist() {
        if (this.loadingReleases) {
            return html`
                <section>
                    <h3 class="section-header">Tracklist</h3>
                    <div class="section-loading">Loading tracks\u2026</div>
                </section>
            `;
        }
        if (this.errorReleases) {
            // Error already shown in version selector section
            return nothing;
        }
        const current = this.currentVersion();
        if (!current) {
            return html`
                <section>
                    <h3 class="section-header">Tracklist</h3>
                    <div class="section-error">
                        <wa-icon name="triangle-exclamation"></wa-icon>
                        No release data available.
                    </div>
                </section>
            `;
        }

        const tracks = current.tracks;
        if (tracks.length === 0) {
            return html`
                <section>
                    <h3 class="section-header">Tracklist</h3>
                    <div
                        style="color: var(--yj-text-tertiary, #888); font-size: var(--yj-text-md)"
                    >
                        No tracks available for this release.
                    </div>
                </section>
            `;
        }

        const multiDisc = this.isMultiDisc(tracks);
        const discMap = this.groupByDisc(tracks);
        const discNumbers = [...discMap.keys()].sort((a, b) => a - b);

        return html`
            <section>
                <h3 class="section-header">
                    Tracklist ${this.renderTracklistLegend()}
                </h3>
                <div class="tracklist">
                    ${discNumbers.map((discNum) => {
                        const discTracks = discMap.get(discNum) ?? [];
                        return html`
                            ${multiDisc
                                ? html`
                                      <div class="disc-separator">
                                          <span class="disc-label"
                                              >Disc ${discNum}</span
                                          >
                                      </div>
                                  `
                                : nothing}
                            ${discTracks.map(
                                (track) => html`
                                    <div
                                        class="track-row"
                                        data-track-mbid="${track.mbid}"
                                        data-track-title="${track.title}"
                                    >
                                        <span class="track-position"
                                            >${track.position}</span
                                        >
                                        <span
                                            class="track-title"
                                            title="${track.title}"
                                            >${track.title}</span
                                        >
                                        <span class="track-duration"
                                            >${formatDuration(
                                                track.length,
                                            )}</span
                                        >
                                        <library-status-indicator
                                            status=${libraryStatusFor(Boolean(track.inLibrary), track.mbid)}
                                            entity-type="track"
                                            label=${track.title}
                                        ></library-status-indicator>
                                    </div>
                                `,
                            )}
                        `;
                    })}
                </div>
            </section>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'explore-album-details': ExploreAlbumDetails;
    }
}

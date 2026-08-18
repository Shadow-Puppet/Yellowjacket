import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state, query } from 'lit/decorators.js';
import { classMap } from 'lit/directives/class-map.js';
import { designTokens } from '../../styles/tokens.css';
import {
    LookupReleaseGroup,
    BrowseReleases,
    GetThumbnail,
} from '@go/explore/service.js';
import {
    GetAlbumTracks,
    GetAlbumCompleteness,
    GetFilePathsByRecordingMBIDs,
} from '@go/library/library.js';
import * as library from '@go/library/models.js';
import type * as download from '@go/download/models.js';
import type * as explore from '@go/explore/models.js';
type MBReleaseGroup = explore.MBReleaseGroup;
type MBRelease = explore.MBRelease;
type MBTrack = explore.MBTrack;
import { exploreCache } from '../../store/explore-cache';
import { libraryStore } from '../../store/library-store';
import { creditLink, exploreLinkStyles } from '../../utils/explore-link';
import { creditStore } from '@store/credit-store';
import { describeError } from '../../utils/describe-error';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '../library-status-indicator/library-status-indicator.js';
import { libraryStatusFor } from '@utils/library-status';
import type { LibraryStatus } from '../library-status-indicator/library-status-indicator.js';
import '../catalog-scope-notice/catalog-scope-notice.js';
import type { CatalogScope } from '../catalog-scope-notice/catalog-scope-notice.js';
import '@awesome.me/webawesome/dist/components/button/button.js';
import '../download-picker/download-picker';
import { downloadStore } from '../../store/download-store';
import { queueStore } from '../../store/queue-store';
import type { QueueSource } from '../../store/queue-store';
import { notificationStore } from '../../store/notification-store';
import '../notifications/inline-notice';
import {
    ContextMenuController,
    contextMenuStyles,
    isContextMenuKey,
} from '@utils/context-menu-controller.js';
import type { ContextMenuHost } from '@utils/context-menu-controller.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import type WaPopup from '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import { dictByName } from '@utils/binding';
import type { TrackDetails } from '@components/track-details/track-details.js';
import { showTrackDetailsForPath } from '@utils/track-details-opener.js';
import '@components/playlist-picker/playlist-picker.js';

/**
 * The region the album header's own failures are rendered in.
 *
 * "Inline" says *not global*, not *where* — so the region is named
 * once, here, rather than spelled at each call site.
 */
export const ExploreAlbumRegion = 'explore-album';

/**
 * How long to wait on a background release browse that has reported
 * neither success nor failure before treating it as gone.  See
 * `armReleasesFallback` for why this is generous.
 */
const RELEASES_FALLBACK_MS = 60000;

/* ── Utility functions (duplicated per Knowledge Pattern #9 — no cross-component imports) ── */

/** The earliest release date in a group, for tie-breaking. */
function earliestDate(releases: MBRelease[]): string {
    let earliest = '\uffff';

    for (const r of releases) {
        const d = r.date || '\uffff';
        if (d < earliest) earliest = d;
    }

    return earliest;
}

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
type SyntheticKind = 'standard' | 'library';

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
     * This real version is the one on disk.  It carries a marker rather
     * than being replaced by a synthetic "Your Library" entry, so the
     * user can still see *which* release they own.
     */
    inLibrary?: boolean;
    /**
     * For synthetics: the cluster (or synthetic source) that this entry
     * was *built from*, used for the metadata header.  For real clusters,
     * always equal to `cluster`.
     */
    backingCluster?: ReleaseCluster;
}

/* ── Component ── */

@customElement('explore-album-details')
export class ExploreAlbumDetails extends LitElement implements ContextMenuHost {
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
    /** True once the catalog has been *shown* not to answer for this
     * release group — the browse errored, or it came back empty after
     * the background fetch reported itself done.  Only this puts the
     * page in `unavailable`; a fetch that is merely slow does not. */
    @state() private catalogFailed = false;
    /** How much of this album the local files say is here, or null when
     * the album is not in the library at all. */
    @state() private completeness: library.AlbumCompleteness | null = null;
    /** Unified entries shown in the dropdown — synthetics first, then real clusters. */
    @state() private versionEntries: VersionEntry[] = [];
    /** Currently-selected dropdown entry (by VersionEntry.key). */
    @state() private selectedVersionKey: string = '';
    @state() private coverArtURL = '';

    /**
     * The local album's own tracks — the authoritative answer to "what
     * is actually on disk," independent of `this.releases`, which
     * `fetchReleases()` fully replaces with catalog data as soon as it
     * lands. "Your Library" is built from this, so a slow or wrong
     * catalog match can never make it show a different release's
     * tracklist than what the files are actually tagged to.
     */
    @state() private localTracks: MBTrack[] = [];

    /**
     * The file behind each displayed track, resolved once when the
     * tracklist settles rather than per click.
     *
     * This is the page's one answer to "do I own this". It used to be
     * asked three different ways — a local album id, the backend's
     * cross-reference, a cached MBID match, or *any* track flagged
     * inLibrary — none of which is "there is a file", and then answered
     * a fourth way at the moment the user clicked something. So a row
     * could render owned, offer Play, and fail; on a real library 129
     * catalog rows were in exactly that state.
     *
     * A path here means the track plays. Nothing else on this page is
     * allowed to mean it.
     */
    @state() private filePaths = new Map<string, string>();

    /**
     * Which MBIDs have been *asked* about, which is not the same as
     * which resolved.
     *
     * A track the library does not have never lands in `filePaths`, so
     * a guard keyed on the answer asks about it again on every render —
     * an unbounded query loop for exactly the tracks the user does not
     * own. This is not `@state`: it records work done, and changing it
     * must not schedule a render.
     */
    private askedFor = new Set<string>();

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

    /* ── Track context menu ── */

    private ctxMenu = new ContextMenuController(this);

    /** The track the open context menu applies to. */
    @state() private ctxMenuTrack: MBTrack | null = null;

    @query('#track-context-menu')
    private contextMenuPopup!: WaPopup;

    @query('#playlist-submenu')
    private playlistSubmenuPopup?: WaPopup;

    @query('track-details')
    private trackDetailsDialog?: TrackDetails;

    // -- ContextMenuHost interface --

    getContextMenuPopup(): WaPopup | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup(): WaPopup | undefined {
        return this.playlistSubmenuPopup;
    }

    onContextMenuClose(): void {
        this.ctxMenuTrack = null;
    }

    /* ── Styles ── */

    static override styles = [
        designTokens,
        exploreLinkStyles,
        contextMenuStyles,
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

            .track-row.owned {
                cursor: pointer;
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

            .track-row:focus-visible {
                outline: 2px solid var(--yj-accent-text, #ffd43b);
                outline-offset: -2px;
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

            /* A track the library does not have, on the pattern a
             * streaming service uses for something it cannot play: the
             * row stays, dimmed, so the album reads as the album rather
             * than as the subset that happens to be here.
             *
             * The dimming is a colour, so it cannot be the only signal
             * — the row also carries aria-disabled, which is what
             * reaches anyone not seeing it.  Secondary rather than
             * tertiary because the row's hover background is
             * bgOverlay, which tertiary does not clear. */
            .track-row.unowned .track-title {
                color: var(--yj-text-secondary, #b3b3b3);
                font-weight: 400;
            }

            /* The request control is offered on every row that has
             * something to request, and is not revealed on hover.
             *
             * It used to be transparent until the row was hovered or
             * focused, on the reasoning that a column of plus signs
             * down a mostly-owned album is clutter. That reasoning was
             * inherited from the green ticks it replaced and does not
             * survive the rule those were removed for: a tick marked
             * the *common* case, while this marks the rows that are
             * **not** here. A mark on the exception is the information
             * on this page — and one that appears only under the
             * pointer cannot be seen, counted, or reached by anyone
             * driving this with a finger. */
            .track-row .track-request {
                flex-shrink: 0;
            }
        `,
    ];

    /* ── Lifecycle ── */

    private unsubReleasesReady?: () => void;
    private unsubReleasesFailed?: () => void;
    /** Release-group MBIDs whose AlbumReleasesReady event we've handled,
     * so a background BrowseReleases fetch re-hydrates versions once. */
    private releasesReloaded = new Set<string>();
    /** Fallback timer that stops the versions spinner if the background
     * BrowseReleases fetch never signals readiness. */
    private releasesFallbackTimer?: number;

    /** Unsubscribes the credit-arrival repaint. */
    private creditsUnsub?: () => void;

    override connectedCallback() {
        super.connectedCallback();

        this.creditsUnsub = creditStore.subscribe(() => {
            this.requestUpdate();
        });
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

        // The same browse reporting that it failed.  This is the only
        // prompt reason to say the catalog is unavailable — before it,
        // the page had to infer a failure from a deadline, which a
        // browse queued behind PrefetchReleases on a 1 req/s limiter
        // misses routinely while succeeding.
        this.unsubReleasesFailed = EventsOn(
            Events.AlbumReleasesFailed,
            (mbid: string) => {
                if (mbid !== this.releaseGroupMBID) return;

                if (this.releasesFallbackTimer) clearTimeout(this.releasesFallbackTimer);
                this.releasesReloaded.add(mbid);
                this.catalogFailed = true;
                this.loadingReleases = false;
            },
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.creditsUnsub?.();
        this.creditsUnsub = undefined;
        this.downloadUnsub?.();
        this.downloadUnsub = null;
        this.unsubReleasesReady?.();
        this.unsubReleasesFailed?.();
        if (this.releasesFallbackTimer) clearTimeout(this.releasesFallbackTimer);
    }

    /**
     * Arm a one-shot fallback for the case neither event covers: the
     * background browse neither finished nor reported an error, because
     * it hung.  Both outcomes now emit, so this is a backstop rather
     * than the primary verdict — and it is long, because the thing it
     * used to cut short was a browse that was going to succeed.
     *
     * A cold browse waits on a 1 req/s limiter shared with
     * PrefetchReleases, which fires up to eight of them when an artist
     * page renders — and an album is almost always opened from one.  So
     * eight seconds of queueing before the request starts is the normal
     * path, not the tail, and the old 12 s deadline was reporting a
     * healthy fetch as a catalog failure.
     */
    private armReleasesFallback(mbid: string) {
        if (this.releasesFallbackTimer) clearTimeout(this.releasesFallbackTimer);

        this.releasesFallbackTimer = window.setTimeout(() => {
            if (this.releasesReloaded.has(mbid)) return;

            this.releasesReloaded.add(mbid);
            this.catalogFailed = true;
            if (this.releases.length === 0) this.loadingReleases = false;
        }, RELEASES_FALLBACK_MS);
    }

    /** Whether we've already scrolled to the highlight target. */
    private hasScrolledToHighlight = false;

    override updated() {
        // Whatever is displayed needs its files known, and the
        // tracklist can change from four directions - the local
        // hydrate, the catalog browse, the cluster build, the version
        // dropdown. Asking here covers all of them; resolveFilePaths
        // returns immediately once every displayed MBID is in the map,
        // so this settles after one pass.
        void this.resolveFilePaths();

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
        this.catalogFailed = false;
        this.completeness = null;
        this.releases = [];
        this.versionEntries = [];
        this.selectedVersionKey = '';
        this.localTracks = [];
        this.filePaths = new Map();
        this.askedFor = new Set();

        // Local-only album (no MBID) — populate entirely from library.
        if (!mbid && this.localAlbumId) {
            await this.hydrateLocalOnly();
            await this.loadCompleteness();

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

        // `hydrateFromLibrary` finds the library album by MBID or by a
        // name+artist match — if the caller already knows the local
        // album id (the ordinary case, navigated here from the
        // library) and that lookup still missed it, fetch by id
        // directly rather than leaving "Your Library" with nothing to
        // go on.
        if (this.localTracks.length === 0 && this.localAlbumId > 0) {
            await this.loadLocalTracks(this.localAlbumId);
        }

        // Phase 1b: ask the files how much of this album is here.
        const completeness = await this.loadCompleteness();

        // A complete album needs nothing from the catalog, and this is
        // the whole point of asking: the release group is MBID-matched
        // (so the identity is right) and the files' own tags account
        // for every track (so the tracklist is right), which between
        // them are the two things a browse was being spent on.  Opening
        // an owned album is now offline.
        //
        // The catalog still has *versions* — other pressings, editions —
        // and those are not derivable from tags. They stay a click away
        // rather than a page load away.
        if (completeness?.complete) {
            this.loadingReleases = false;
            void this.fetchReleaseGroup(mbid);
            this.resolveCoverArt();

            return;
        }

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
     * Uses GetAlbumTracks(album.ID, libraryStore.libraryFilter()) — same local DB call as cover-grid.
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
            tracks = await GetAlbumTracks(this.localAlbumId, libraryStore.libraryFilter());
        } catch {
            this.loadingReleases = false;
            return;
        }

        if (!tracks || tracks.length === 0) {
            this.loadingReleases = false;
            return;
        }

        const localTracks = this.mapLocalTracks(tracks);

        this.localTracks = localTracks;

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

    /**
     * Map local track rows to `MBTrack`s and sort them into disc/track
     * order — the shape every "built from local data" release/entry on
     * this page is made of.
     */
    private mapLocalTracks(
        tracks: Awaited<ReturnType<typeof GetAlbumTracks>>,
    ): MBTrack[] {
        // The rows carry the file paths; this is where they stop being
        // thrown away.
        this.rememberLocalPaths(tracks);

        const mapped: MBTrack[] = (tracks ?? []).map((t) => ({
            mbid: t.RecordingMBID || '',
            title: t.TrackName,
            position: t.TrackNumber || 0,
            length: this.parseDurationMs(t.TrackLength),
            discNumber: t.DiscNumber || 1,
            inLibrary: true,
        } as MBTrack));

        mapped.sort((a, b) => {
            const d = (a.discNumber || 1) - (b.discNumber || 1);
            return d !== 0 ? d : a.position - b.position;
        });

        return mapped;
    }

    /**
     * Read how much of this album is present, from the tags the scan
     * already stored.  No network, one query.
     *
     * Silent on failure: this decides whether to *skip* work and how to
     * draw a badge, so an unanswered question falls back to the old
     * behaviour rather than surfacing an error the user cannot act on.
     */
    private async loadCompleteness(): Promise<library.AlbumCompleteness | null> {
        if (this.localAlbumId <= 0) {
            this.completeness = null;

            return null;
        }

        try {
            this.completeness = await GetAlbumCompleteness(this.localAlbumId);
        } catch {
            this.completeness = null;
        }

        return this.completeness;
    }

    /**
     * Fetch and set `localTracks` directly by local album id — the
     * definite source of truth, used when nothing else has already
     * populated it (see the call site in `loadAllData`).
     */
    private async loadLocalTracks(albumId: number): Promise<void> {
        try {
            const tracks = await GetAlbumTracks(albumId, libraryStore.libraryFilter());

            this.localTracks = this.mapLocalTracks(tracks);
        } catch {
            this.localTracks = [];
        }

        // A catalog fetch may have already built the version list
        // without a "Your Library" entry to point at — rebuild now
        // that there's local data to match against it.
        //
        // Unconditionally, including when the catalog returned nothing:
        // `buildVersionEntries` synthesises the library entry *from*
        // these tracks, so the no-releases case is exactly the one that
        // needs this. Guarded on `releases.length` before, an album the
        // catalog could not answer for showed "No release data
        // available" over a tracklist it was holding in memory.
        this.buildClusters();
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
            tracks = await GetAlbumTracks(libraryAlbum.ID, libraryStore.libraryFilter());
        } catch {
            return false;
        }

        if (!tracks || tracks.length === 0) return false;

        const localTracks = this.mapLocalTracks(tracks);

        this.localTracks = localTracks;

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
                this.catalogFailed = false;
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
            // ready is as far as the catalog is going to get: it
            // answered, and the answer was nothing.
            if (this.releasesReloaded.has(mbid)) {
                this.catalogFailed = true;
            }
        } catch (err) {
            console.error('[explore-album] BrowseReleases error', err);
            this.errorReleases = describeError(
                err,
                'The catalog did not answer for this album\u2019s versions.',
            );
            this.loadingReleases = false;
            this.catalogFailed = true;
        }
    }

    /* ── Release Clustering (R026) + Synthetic Versions ── */

    /**
     * Groups releases by identical tracklist fingerprints (ordered
     * recording MBIDs), scores each cluster, then builds the unified
     * version-entry list (synthetics + clusters) for the dropdown.
     */
    /**
     * Fingerprint a tracklist: tracks sorted by (discNumber, position),
     * recording MBIDs joined. Two tracklists with the same fingerprint
     * are the same edition for display purposes — this is also how a
     * local album's own tracks are matched against a catalog cluster
     * for "Your Library" (`buildLibraryEntry`), so a match here is a
     * real one, not a guess.
     */
    private static fingerprint(tracks: MBTrack[]): string {
        const sorted = [...tracks].sort((a, b) => {
            const discDiff = (a.discNumber || 1) - (b.discNumber || 1);
            if (discDiff !== 0) return discDiff;
            return a.position - b.position;
        });

        return sorted.map((t) => t.mbid).join('|');
    }

    /**
     * How many genuinely different tracklists the version entries hold.
     *
     * Deliberately *not* `fingerprint()`, which keys on recording MBIDs
     * alone: a library entry built from untagged files has none, so
     * every such tracklist fingerprints to the same run of empty
     * strings and would compare equal to any other. Falling back to the
     * title lets a local copy be recognised as the same tracklist as
     * the catalog's when that is what it is — which is exactly the case
     * that decides whether the dropdown is a choice or a decoration.
     */
    private distinctTracklistCount(): number {
        // The basis is chosen for the whole comparison, not per track.
        // A per-track `mbid || title` fallback is asymmetric: it only
        // helps when *both* sides lack ids, so an untagged library copy
        // and the catalog's identical tracklist keyed one by title and
        // one by MBID and never compared equal — which put a dropdown
        // on every owned album the moment its catalog data landed, with
        // two options showing the same songs.
        const allTagged = this.versionEntries.every((e) =>
            e.tracks.every((t) => !!t.mbid),
        );

        const seen = new Set<string>();

        for (const entry of this.versionEntries) {
            const key = [...entry.tracks]
                .sort((a, b) => {
                    const discDiff = (a.discNumber || 1) - (b.discNumber || 1);

                    return discDiff !== 0 ? discDiff : a.position - b.position;
                })
                .map(
                    (t) =>
                        `${t.discNumber || 1}:${t.position}:${
                            allTagged ? t.mbid : t.title.trim().toLowerCase()
                        }`,
                )
                .join('|');

            seen.add(key);
        }

        return seen.size;
    }

    /** The set of recording MBIDs on a tracklist, order ignored. */
    private static trackMBIDSet(tracks: MBTrack[]): Set<string> {
        const set = new Set<string>();

        for (const t of tracks) if (t.mbid) set.add(t.mbid);

        return set;
    }

    /** How many recordings differ between two MBID sets. */
    private static symmetricDifferenceSize(a: Set<string>, b: Set<string>): number {
        let diff = 0;

        for (const x of a) if (!b.has(x)) diff++;
        for (const x of b) if (!a.has(x)) diff++;

        return diff;
    }

    /**
     * How many tracks two clusters may differ by and still count as
     * the same edition for display purposes. A bonus track present on
     * one regional pressing and missing from another — or a tagging
     * gap that drops one recording's MBID — produces a different exact
     * fingerprint but isn't a meaningfully different "version" to a
     * listener choosing between them.
     */
    private static readonly NEAR_DUPLICATE_TOLERANCE = 2;

    /**
     * Fold clusters that differ from a larger one by only a couple of
     * tracks into it, so the "Versions" list reflects meaningfully
     * different editions rather than every minor pressing variation.
     * This is the fix for the dropdown getting so long it stopped
     * being useful — trying to give every pressing its own row was the
     * over-categorization problem, not a missing feature.
     *
     * Largest-tracklist-first, so a cluster only ever merges into
     * something at least as big as itself: the result keeps the
     * fuller tracklist and folds the combined release counts into it,
     * rather than the reverse (which would silently drop tracks from
     * what's shown). A cluster with no recording MBIDs at all
     * (unidentifiable) is left alone — there's nothing to compare.
     */
    private mergeNearDuplicateClusters(
        clusters: ReleaseCluster[],
    ): ReleaseCluster[] {
        const sets = new Map<ReleaseCluster, Set<string>>(
            clusters.map((c) => [
                c,
                ExploreAlbumDetails.trackMBIDSet(c.representative.tracks ?? []),
            ]),
        );
        const sorted = [...clusters].sort(
            (a, b) =>
                (b.representative.tracks?.length ?? 0)
                - (a.representative.tracks?.length ?? 0),
        );

        const kept: ReleaseCluster[] = [];
        const keptSets: Set<string>[] = [];

        candidateLoop: for (const candidate of sorted) {
            const candSet = sets.get(candidate)!;

            if (candSet.size > 0) {
                for (let i = 0; i < kept.length; i++) {
                    const diff = ExploreAlbumDetails.symmetricDifferenceSize(
                        candSet,
                        keptSets[i]!,
                    );

                    if (diff <= ExploreAlbumDetails.NEAR_DUPLICATE_TOLERANCE) {
                        kept[i]!.allReleases.push(...candidate.allReleases);
                        continue candidateLoop;
                    }
                }
            }

            kept.push(candidate);
            keptSets.push(candSet);
        }

        return kept.map((c) => ExploreAlbumDetails.withConsensusRepresentative(c));
    }

    /**
     * Re-pick a merged cluster's representative by consensus.
     *
     * Merging compares track *sets*, so a resequenced pressing — same
     * songs, different running order — folds in correctly. But the
     * survivor was whichever release happened to be encountered first,
     * which is browse order and means nothing: one 2021 pressing
     * arriving ahead of eleven 2013 ones made the cluster wear the 2021
     * running order. The user's own files then matched no cluster
     * fingerprint, so the page said their copy "isn't linked to a
     * MusicBrainz release yet" while showing a tracklist in an order
     * almost nothing was pressed in.
     *
     * The representative is the ordering the most releases agree on,
     * earliest release breaking the tie — and the cluster's fingerprint
     * has to move with it, since that is what the library match is
     * tested against.
     */
    private static withConsensusRepresentative(
        cluster: ReleaseCluster,
    ): ReleaseCluster {
        if (cluster.allReleases.length < 2) return cluster;

        const byOrder = new Map<string, MBRelease[]>();

        for (const release of cluster.allReleases) {
            const fp = ExploreAlbumDetails.fingerprint(release.tracks ?? []);
            const group = byOrder.get(fp);

            if (group) {
                group.push(release);
            } else {
                byOrder.set(fp, [release]);
            }
        }

        let bestFingerprint = cluster.fingerprint;
        let best: MBRelease[] | null = null;

        for (const [fp, group] of byOrder) {
            if (
                best === null
                || group.length > best.length
                || (group.length === best.length
                    && earliestDate(group) < earliestDate(best))
            ) {
                best = group;
                bestFingerprint = fp;
            }
        }

        if (!best) return cluster;

        const representative = [...best].sort(
            (a, b) => (a.date || '￿').localeCompare(b.date || '￿'),
        )[0]!;

        return { ...cluster, representative, fingerprint: bestFingerprint };
    }

    private buildClusters() {
        const clusterMap = new Map<string, MBRelease[]>();

        for (const release of this.releases) {
            const tracks = release.tracks ?? [];

            // Releases whose tracks have no MBIDs all collapse together
            // under an "empty" fingerprint, which is intentional —
            // unidentifiable releases shouldn't multiply dropdown noise.
            const fingerprint = ExploreAlbumDetails.fingerprint(tracks);

            const existing = clusterMap.get(fingerprint);
            if (existing) {
                existing.push(release);
            } else {
                clusterMap.set(fingerprint, [release]);
            }
        }

        // Build clusters with earliest-dated representative per group.
        let clusters: ReleaseCluster[] = [];
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

            clusters.push({
                representative: sorted[0]!,
                allReleases: sorted,
                fingerprint,
                score: 0, // filled in below, after merging
            });
        }

        // Fold near-duplicate clusters (one bonus track, a tagging
        // gap) into the closest larger cluster before scoring —
        // otherwise every minor pressing difference gets counted as
        // its own "version" and the size bonus below undercounts how
        // many releases actually agree with each other.
        clusters = this.mergeNearDuplicateClusters(clusters);

        for (const cluster of clusters) {
            cluster.score = this.scoreCluster(cluster);
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

        // Default selection: prefer the user's own version — whether
        // that is a marked real release or the synthetic fallback —
        // otherwise the standard version, otherwise the first entry.
        const ownedEntry = this.versionEntries.find((e) => e.inLibrary);
        const libraryEntry = this.versionEntries.find(
            (e) => e.syntheticKind === 'library',
        );
        const standardEntry = this.versionEntries.find(
            (e) => e.syntheticKind === 'standard',
        );
        this.selectedVersionKey =
            ownedEntry?.key
            || libraryEntry?.key
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
        // A secondary signal now — dampened to a ~26-point range across
        // a century, so it breaks ties among similarly-agreed-upon
        // clusters rather than outweighing the size bonus below. It
        // used to be the dominant term (roughly 1 point per year,
        // undamped), which meant "released a few years earlier" could
        // beat "the edition 20 physical releases actually agree on" —
        // backwards for guessing which release someone means.
        const earliestDate = cluster.representative.date || '';
        if (earliestDate.length >= 4) {
            const year = parseInt(earliestDate.slice(0, 4), 10);
            if (Number.isFinite(year)) {
                // Newer albums have larger year values, but we want
                // older = higher score, so invert against a future
                // ceiling.  Anything older than ~3000 ranks higher.
                score += (3000 - year) / 5;
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

        // Cluster size bonus: the primary signal among same-status
        // clusters.  "How many releases agree on this tracklist" is a
        // much better guess at "the version people mean" than which
        // one happened to ship first — a reissue with dozens of
        // regional pressings sharing a tracklist is the canonical
        // edition even when an obscure early pressing is a few years
        // older.  Log-scaled so a cluster of 10 isn't 10x a cluster of
        // 1, but a cluster of 30 still beats a cluster of 5 by a
        // meaningful amount.
        score += Math.log2(1 + cluster.allReleases.length) * 150;

        return score;
    }

    /**
     * Build the unified version-entry list: a synthetic Library or
     * Standard entry pinned at the top (never both — see
     * `buildLibraryEntry`), followed by the real clusters in score
     * order.
     */
    private buildVersionEntries(clusters: ReleaseCluster[]): VersionEntry[] {
        const entries: VersionEntry[] = [];

        // No catalog clusters yet (still loading, or none exist) — the
        // only thing that can possibly be shown is the local tracklist
        // itself, if there is one.
        if (clusters.length === 0) {
            const libraryOnly = this.buildLibraryEntry(clusters);

            if (libraryOnly) entries.push(libraryOnly);

            return entries;
        }

        // ── Library ─────────────────────────────────────────────────
        // When the files on disk *are* one of these releases, that
        // release is marked rather than copied into a separate "Your
        // Library" entry. The synthetic hid the thing worth knowing:
        // the user could see that they owned a version but not which
        // one, and the real release — with its date, country and
        // release count — was sitting right underneath under a
        // different name.
        const owned = this.exactLibraryCluster(clusters);

        // Only when the local tracklist matches no release at all is a
        // synthetic still needed: there is no version name to mark.
        const libraryEntry = owned ? null : this.buildLibraryEntry(clusters);
        if (libraryEntry) entries.push(libraryEntry);

        // ── Standard ────────────────────────────────────────────────
        // Highest-scoring cluster is the standard.  We point at the
        // cluster directly so the standard entry is just a labeled
        // alias — selecting it shows the same tracks as selecting that
        // cluster from the bottom list.  Skipped once the user's own
        // version is identified, by either route: a guess next to a
        // known-correct answer is noise, not a choice.
        if (!libraryEntry && !owned) {
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
                inLibrary: !!owned && cluster === owned,
            });
        }

        return entries;
    }

    /**
     * Build the "Your Library" entry: the release the local files are
     * actually tagged to, not a guess.
     *
     * `localTracks` (when there is any — the local album's own tracks,
     * fetched straight from the DB) is fingerprinted the same way a
     * catalog cluster is, and matched against the clusters on screen.
     * An exact match means a real MusicBrainz release describes what's
     * on disk, so that cluster — with its real metadata — is shown. No
     * match (untagged files, or tags that don't line up with any
     * single release) falls back to the local tracks themselves, which
     * is always correct because it came straight from the files
     * rather than from a description of them.
     *
     * Only when there's no local album at all (the user owns some of
     * this release's tracks without a local album match — e.g.
     * scattered across a compilation) does this fall back to the old
     * fractional-overlap heuristic over the catalog's own `inLibrary`
     * flags, which is a guess by construction and stays one.
     */
    private exactLibraryCluster(
        clusters: ReleaseCluster[],
    ): ReleaseCluster | undefined {
        if (this.localTracks.length === 0) return undefined;

        const localFingerprint = ExploreAlbumDetails.fingerprint(this.localTracks);
        if (!localFingerprint) return undefined;

        return clusters.find((c) => c.fingerprint === localFingerprint);
    }

    private buildLibraryEntry(clusters: ReleaseCluster[]): VersionEntry | null {
        if (this.localTracks.length > 0) {

            // Callers reach this only when no release matches what is
            // on disk (`exactLibraryCluster` is checked first), so there
            // is no exact-match branch here — the version list marks
            // that case instead of synthesising an entry for it.
            //
            // A known-incomplete album should show the *release*, not
            // the subset of it that happens to be on disk — the tracks
            // that are missing are the useful information, and a
            // tracklist trimmed to what is owned cannot show them at
            // all.  So the local files stand in only while the album is
            // complete or unjudged; once the tags say nine of twelve,
            // the catalog's twelve are what the page draws, with three
            // of them dimmed.
            //
            // Guarded on `known` rather than on "fewer tracks than the
            // cluster", which would swap in a catalog tracklist for
            // every album whose tags simply never declared a total.
            const answer = this.completenessAnswer();
            const incomplete = answer?.known && !answer.complete;

            if (incomplete) {
                const fullRelease = this.findLibraryCluster(clusters);

                if (fullRelease) {
                    return {
                        key: 'synthetic:library',
                        label: 'Your Library',
                        sublabel: `${answer?.owned ?? 0} of ${answer?.expected ?? 0} tracks · ${this.clusterLabel(fullRelease)}`,
                        group: 'aggregate',
                        syntheticKind: 'library',
                        tracks: fullRelease.representative.tracks ?? [],
                        backingCluster: fullRelease,
                    };
                }
            }

            return {
                key: 'synthetic:library',
                label: 'Your Library',
                sublabel: `${this.localTracks.length} track${this.localTracks.length === 1 ? '' : 's'} · from your files`,
                group: 'aggregate',
                syntheticKind: 'library',
                tracks: this.localTracks,
            };
        }

        const libraryCluster = this.findLibraryCluster(clusters);

        if (!libraryCluster) return null;

        return {
            key: 'synthetic:library',
            label: 'Your Library',
            sublabel: this.librarySublabel(libraryCluster),
            group: 'aggregate',
            syntheticKind: 'library',
            tracks: libraryCluster.representative.tracks ?? [],
            backingCluster: libraryCluster,
        };
    }

    /**
     * Fallback only: used when there's no local album to anchor on
     * (see `buildLibraryEntry`). Finds the cluster with the highest
     * fraction of tracks the *library-wide* `inLibrary` cross-reference
     * marked owned — a guess, since that flag isn't scoped to this
     * album and a recording can appear on more than one release.
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
    /**
     * How much of this album is here, from whichever side can say.
     *
     * The files answer first: `GetAlbumCompleteness` reads the "5/12"
     * totals off the tags, which is exact and costs no network. A great
     * deal of any library declares no total at all, and for those the
     * *catalog* carries one — a per-release-group track count in
     * `explore_index`, shipped in the artifact for the price of about
     * two bytes a row.
     *
     * The numerator stays the local one either way: how many distinct
     * track numbers are on disk. Only the denominator is borrowed, and
     * only when the tags have none — a catalog total is a statement
     * about the canonical release, and the files' own total, where they
     * declare one, is a statement about the release the user actually
     * has.
     *
     * Zero still means "the catalog does not say", so an album neither
     * side can total stays `known: false` and wears no ring.
     */
    private completenessAnswer(): library.AlbumCompleteness | null {
        const local = this.completeness;

        if (local?.known) return local;

        const expected = this.releaseGroup?.totalTracks ?? 0;
        if (expected <= 0 || !local) return local;

        return {
            ...local,
            expected,
            known: true,
            complete: local.owned >= expected,
        };
    }

    /**
     * What the badge beside the album title shows.
     *
     * `albumLibraryStatus()` answers "is any of this yours", which is
     * the right question for a tick and the wrong one for a ring. The
     * ring needs a denominator, and an album neither the files nor the
     * catalog can total keeps the plain tick rather than wearing an arc
     * drawn from a guess.
     */
    private albumBadgeStatus(): LibraryStatus {
        const owned = this.albumLibraryStatus();

        if (owned !== 'in-library') return owned;

        const c = this.completenessAnswer();
        if (c?.known && !c.complete) return 'partial';

        return 'in-library';
    }

    /**
     * How a displayed track is identified in `filePaths`.
     *
     * A recording MBID where there is one, and disc/track/title where
     * there is not — a library-only album's tracks are synthesised from
     * the files' own tags and may carry no MBID at all, which is the
     * case an MBID-keyed lookup silently misses.
     */
    private static trackKey(t: MBTrack): string {
        if (t.mbid) return t.mbid;

        return `${t.discNumber || 1}:${t.position}:${t.title.toLowerCase()}`;
    }

    /** The file behind a displayed track, or '' if the user has none. */
    private filePathFor(t: MBTrack): string {
        return this.filePaths.get(ExploreAlbumDetails.trackKey(t)) ?? '';
    }

    /**
     * Record the files behind the local album's own tracks.
     *
     * These cost nothing: `GetAlbumTracks` already returned the paths,
     * and this is the one place they were being thrown away.
     */
    private rememberLocalPaths(
        rows: Awaited<ReturnType<typeof GetAlbumTracks>>,
    ): void {
        const paths = new Map(this.filePaths);

        for (const row of rows ?? []) {
            if (!row.FilePath) continue;

            const key = ExploreAlbumDetails.trackKey({
                mbid: row.RecordingMBID || '',
                title: row.TrackName,
                position: row.TrackNumber || 0,
                discNumber: row.DiscNumber || 1,
            } as MBTrack);

            paths.set(key, row.FilePath);
        }

        this.filePaths = paths;
    }

    /**
     * Resolve the catalog tracklist's files in one query.
     *
     * Called when the displayed tracklist changes rather than when a
     * user clicks: the answer decides what the rows look like and which
     * menu items exist, so it has to be known before either is drawn.
     */
    private async resolveFilePaths(): Promise<void> {
        const tracks = this.currentVersion()?.tracks ?? [];
        const wanted = tracks
            .map((t) => t.mbid)
            .filter((mbid) => mbid && !this.askedFor.has(mbid));

        if (wanted.length === 0) return;

        for (const mbid of wanted) this.askedFor.add(mbid);

        try {
            const byMBID = await dictByName(
                GetFilePathsByRecordingMBIDs(wanted, libraryStore.libraryFilter()),
            );

            const paths = new Map(this.filePaths);

            for (const [mbid, forMBID] of Object.entries(byMBID)) {
                const first = forMBID?.[0];
                if (first) paths.set(mbid, first);
            }

            this.filePaths = paths;
        } catch (error) {
            // A failure here means the page cannot say what is owned, so
            // it says nothing rather than guessing: rows stay dimmed and
            // the actions that need a file stay absent.
            console.error('Could not resolve library files for this album:', error);
        }
    }

    /**
     * Whether any of this album is the user's.
     *
     * One question, asked once: does any displayed track have a file.
     * It used to be four claims of decreasing confidence OR'd into a
     * single tick — a local album id, the backend's cross-reference, a
     * cached MBID match, and finally *any* track flagged `inLibrary` —
     * none of which is "there is a file", which is why the badge could
     * say yes about an album whose every action failed.
     *
     * When it is not owned the answer is not automatically "no": the
     * album may be on the request list, which the button below the
     * badge has reported as "Wanted" all along.
     */
    private albumLibraryStatus(): LibraryStatus {
        if (this.ownership().owned > 0) return 'in-library';

        return libraryStatusFor(false, this.releaseGroupMBID);
    }

    /**
     * How much of the shown release the user actually has.
     *
     * Counted off the tracklist being displayed, by how many of its
     * tracks resolved to a file. That is the only claim on this page
     * that is not an inference: a path means the track plays.
     *
     * It is what the header's buttons key off, because "Play" that
     * plays one track of a forty-track release is worse than no Play
     * button — and it is now also what the badge above them uses, so
     * the two can no longer disagree.
     */
    private ownership(): { owned: number; total: number } {
        const tracks = this.currentVersion()?.tracks ?? [];

        return {
            owned: tracks.filter((t) => this.filePathFor(t) !== '').length,
            total: tracks.length,
        };
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
            <track-details></track-details>
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
                            status=${this.albumBadgeStatus()}
                            .owned=${this.completenessAnswer()?.owned ?? 0}
                            .expected=${this.completenessAnswer()?.expected ?? 0}
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
                    @click=${() => this.playOwned(false)}
                >
                    <wa-icon slot="start" name="play"></wa-icon>
                    ${playLabel}
                </wa-button>
                <wa-button
                    size="small"
                    appearance="outlined"
                    data-testid="album-shuffle"
                    @click=${() => this.playOwned(true)}
                >
                    <wa-icon slot="start" name="shuffle"></wa-icon>
                    Shuffle album
                </wa-button>
                <wa-button
                    size="small"
                    appearance="outlined"
                    data-testid="album-queue"
                    @click=${() => this.queueOwned()}
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
    /**
     * What "Playing from" should say for a queue built here — only when
     * there's a local album to link back to. A catalog-only album (no
     * `localAlbumId`) has nowhere in the library for the queue panel's
     * link to point, so the queue is left undescribed rather than
     * pointed at a page that doesn't exist for it.
     */
    private queueSource(): QueueSource | undefined {
        if (!(this.localAlbumId > 0)) return undefined;

        return { type: 'album', id: this.localAlbumId, label: this.albumName };
    }

    /**
     * The files behind the displayed tracklist, in its order.
     *
     * No query: the paths were resolved when the tracklist settled.
     * This used to be two different lookups chosen by a branch - by
     * local album id, or by recording MBID for a catalog-only album -
     * and the second silently returned nothing for an untagged library,
     * because those tracks carry no MBID at all.
     */
    private ownedFilePaths(): string[] {
        const paths: string[] = [];

        for (const track of this.currentVersion()?.tracks ?? []) {
            const path = this.filePathFor(track);

            // A recording with more than one file is a duplicate; the
            // map holds the first and the rest are the duplicate
            // feature's business.
            if (path) paths.push(path);
        }

        return paths;
    }

    /** Play what the user owns of this release, optionally shuffled. */
    private playOwned(shuffle: boolean): void {
        const paths = this.ownedFilePaths();

        // The button is only rendered when there is something to play,
        // so an empty set here is not a state the user can reach.
        if (paths.length === 0) return;

        // `shuffleStart` only picks a random first track when shuffle
        // mode is *already* on — it does not turn it on — so the mode
        // has to be set before the queue, not after.
        if (shuffle && !queueStore.getState().shuffleMode) {
            queueStore.toggleShuffle();
        }

        queueStore.setQueue(paths, 0, shuffle, this.queueSource());
    }

    /** Append what the user owns of this release to the queue. */
    private queueOwned(): void {
        const paths = this.ownedFilePaths();

        if (paths.length === 0) return;

        queueStore.addTracksToQueue(paths);
    }

    /**
     * Play an owned track *in the context of the release it is on*:
     * the whole owned tracklist is queued and playback starts at that
     * track. Activating a row is a position in an album, not a request
     * to throw the album away - "Add to Queue" and "Play Next" are what
     * a caller reaches for when it wants the one track.
     */
    private playTrack(track: MBTrack): void {
        const path = this.filePathFor(track);

        // Every path into this is gated on the row having a file: the
        // row is not activatable without one and the menu offers
        // nothing that needs one. There is no "could not be found in
        // your library" any more, because the page no longer offers an
        // action it cannot perform.
        if (!path) return;

        const paths = this.ownedFilePaths();
        const start = paths.indexOf(path);

        // `start` is only -1 if the row is not in the version currently
        // displayed, which no gesture on this page can produce; playing
        // the one track is the honest answer to it either way.
        if (start < 0) {
            queueStore.setQueue([path], 0, false, this.queueSource());

            return;
        }

        queueStore.setQueue(paths, start, false, this.queueSource());
    }

    private queueTrackNext(track: MBTrack): void {
        const path = this.filePathFor(track);

        if (path) queueStore.playNext(path);
    }

    private addTrackToQueue(track: MBTrack): void {
        const path = this.filePathFor(track);

        if (path) queueStore.addToQueue(path);
    }

    private onTrackRowDblClick(track: MBTrack): void {
        this.playTrack(track);
    }

    private onTrackRowKeydown(e: KeyboardEvent, track: MBTrack): void {
        if (isContextMenuKey(e)) {
            e.preventDefault();
            this.ctxMenuTrack = track;
            this.ctxMenu.openFrom(e.currentTarget as HTMLElement);

            return;
        }

        if ((e.key === 'Enter' || e.key === ' ') && this.filePathFor(track)) {
            e.preventDefault();
            this.playTrack(track);
        }
    }

    private onTrackContextMenu(e: MouseEvent, track: MBTrack): void {
        e.preventDefault();
        e.stopPropagation();

        this.ctxMenuTrack = track;
        this.ctxMenu.openAt(e.clientX, e.clientY);
    }

    private onContextMenuAction(
        action: 'play' | 'add-to-queue' | 'play-next' | 'track-details',
    ): void {
        const track = this.ctxMenuTrack;

        this.ctxMenu.close();

        if (!track || !this.filePathFor(track)) return;

        switch (action) {
            case 'play':
                this.playTrack(track);
                break;
            case 'add-to-queue':
                this.addTrackToQueue(track);
                break;
            case 'play-next':
                this.queueTrackNext(track);
                break;
            case 'track-details':
                void this.openTrackDetails(track);
                break;
        }
    }

    /**
     * Open the "Add to Playlist" submenu for the track the menu is on.
     *
     * No await and no guards: the file was resolved when the tracklist
     * settled, so the submenu opens or the item was never rendered.
     * This used to resolve on demand, which meant a hover could report
     * a failure for a menu the user was passing through.
     */
    private openPlaylistSubmenu(): void {
        const track = this.ctxMenuTrack;
        if (!track) return;

        const path = this.filePathFor(track);
        if (!path) return;

        this.ctxMenu.clearSubmenuCloseTimer();
        void this.ctxMenu.showPlaylistSubmenu([path]);
    }

    /**
     * The details dialog for an owned track.
     *
     * It needs the library's own `Track`, which this page never has —
     * its rows are the catalog's — so the file path is the way in, and
     * the shared opener turns it back into a track.
     */
    private async openTrackDetails(track: MBTrack): Promise<void> {
        const path = this.filePathFor(track);
        if (!path) return;

        const outcome = await showTrackDetailsForPath(
            () => this.trackDetailsDialog,
            path,
            () => void this.openTrackDetails(track),
        );

        // The file exists but the library store does not know it: a
        // rescan removed it since the page loaded, which is the one
        // case the resolved map cannot rule out.
        if (outcome === 'not-in-library') {
            notificationStore.inline(ExploreAlbumRegion, {
                text: 'This track is no longer in your library.',
            });
        }
    }

    /**
     * Explore's tracks carry a recording MBID whether or not the user
     * owns them — this is the one context-menu action that works on a
     * track the library does not have, since it needs no file at all.
     */
    private viewTrackOnMusicBrainz(): void {
        const track = this.ctxMenuTrack;

        this.ctxMenu.close();

        if (!track?.mbid) return;

        window.open(`https://musicbrainz.org/recording/${track.mbid}`, '_blank', 'noopener');
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
                ${this.isRequested ? 'Requested' : 'Request this'}
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
        // A library-only album says nothing: the header names it, the
        // badge says it is yours, and the tracklist is the files'
        // own — there is nothing absent for a notice to warn about.
        // The artist page keeps its 'library' state because there a
        // missing catalog means missing *sections*.
        if (!this.releaseGroupMBID) return 'catalog';
        if (this.catalogReleasesLoaded) return 'catalog';

        // A complete, MBID-matched album is not missing anything the
        // notice could warn about, so there is nothing to say — and
        // saying it silently is the point: no browse was made, and a
        // banner about absent catalog data would be describing a
        // request the page deliberately did not send.
        if (this.completeness?.complete) return 'catalog';

        if (this.catalogFailed) return 'unavailable';

        // A fetch is still out there, and the page says nothing about
        // it.  A banner announcing that details are loading is a line
        // of text about the page's own plumbing, and it earns its space
        // only if the alternative is the user misreading what is on
        // screen — which the tracklist now prevents by itself: rows
        // that are not in the library are dimmed, so tracks arriving
        // dimmed reads as the album filling in rather than as anything
        // needing explanation.
        //
        // `unavailable` survives because it is not about plumbing: it
        // says rows may be missing from the page altogether, which
        // nothing on screen can show, and it carries a retry.
        return 'catalog';
    }

    /** Ask the catalog again after a failed or empty fetch. */
    private retryCatalog = () => {
        const mbid = this.releaseGroupMBID;
        if (!mbid) return;

        this.errorReleases = '';
        this.loadingReleases = this.releases.length === 0;
        this.catalogFailed = false;
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
                      ${creditLink(
                          creditStore.credits(this.releaseGroupMBID),
                          artist,
                          artistMbid,
                      )}
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

        // A dropdown is only a choice if the choices differ. Counting
        // *entries* is the wrong test: a release group routinely has
        // several releases — reissues, regional pressings, a remaster —
        // whose tracklists are identical, and the synthetic "Your
        // Library" entry is often a third name for the same one. That
        // offered a control whose every option showed the same rows.
        //
        // Distinct *tracklists* is the real question, and it is already
        // computed: clusters are keyed by tracklist fingerprint.
        if (this.distinctTracklistCount() <= 1) return nothing;

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
                                      ${aggregateEntries.map((e) =>
                                          this.renderVersionOption(e),
                                      )}
                                  </optgroup>
                              `
                            : nothing}
                        <optgroup label="Versions">
                            ${clusterEntries.map((e) =>
                                this.renderVersionOption(e),
                            )}
                        </optgroup>
                    </select>
                </div>
                ${this.renderVersionMeta()}
            </div>
        `;
    }

    /**
     * One option in the version list.
     *
     * The marker is a star *and* the words "in your library": a `<select>`
     * cannot be styled per option, so the only way to say this is in the
     * text — and a bare glyph would be a symbol with no key, which is
     * exactly what the tracklist's green ticks were.
     */
    private renderVersionOption(entry: VersionEntry) {
        const label = entry.inLibrary ? `\u2605 ${entry.label}` : entry.label;
        const sublabel = entry.inLibrary
            ? `${entry.sublabel} \u00b7 in your library`
            : entry.sublabel;

        return html`
            <option
                value=${entry.key}
                ?selected=${entry.key === this.selectedVersionKey}
            >
                ${label} — ${sublabel}
            </option>
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
                        weighing how many physical releases share it,
                        release status, and release date.
                    </div>
                `;
            case 'library': {
                // The exact-match case no longer lands here — it is a
                // marked real version now, handled below — so what is
                // left is the two ways of *not* being able to name the
                // release.
                const text = this.localTracks.length === 0
                    ? 'Showing the version that best matches your library, going by which tracks you own — not a specific local album.'
                    : 'Showing your library copy, built from your local files. It isn’t linked to a MusicBrainz release yet.';

                return html`<div class="version-meta">${text}</div>`;
            }
            default: {
                const releases = current.cluster?.allReleases.length ?? 0;
                const shared = releases > 1
                    ? `${releases} physical releases share this tracklist.`
                    : '';

                if (current.inLibrary) {
                    return html`
                        <div class="version-meta">
                            \u2605 This is the version in your library.
                            ${shared}
                        </div>
                    `;
                }

                return shared
                    ? html`<div class="version-meta">${shared}</div>`
                    : nothing;
            }
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
                <h3 class="section-header">Tracklist</h3>
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
                            ${discTracks.map((track) => {
                                // A row is owned if a file is behind it.
                                // It used to be the backend's inLibrary
                                // flag, which was set from a metadata
                                // row and could be true for a track
                                // that could not be played.
                                const owned = this.filePathFor(track) !== '';

                                return html`
                                    <div
                                        class=${classMap({
                                            'track-row': true,
                                            owned,
                                            unowned: !owned,
                                        })}
                                        data-track-mbid="${track.mbid}"
                                        data-track-title="${track.title}"
                                        tabindex="0"
                                        role="button"
                                        aria-disabled=${owned ? 'false' : 'true'}
                                        aria-label=${owned
                                            ? `Play “${track.title}”`
                                            : `${track.title} — not in your library`}
                                        @dblclick=${() => this.onTrackRowDblClick(track)}
                                        @contextmenu=${(e: MouseEvent) => this.onTrackContextMenu(e, track)}
                                        @keydown=${(e: KeyboardEvent) => this.onTrackRowKeydown(e, track)}
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
                                        ${owned
                                            ? nothing
                                            : html`
                                                  <library-status-indicator
                                                      class="track-request"
                                                      status=${libraryStatusFor(
                                                          false,
                                                          track.mbid,
                                                      )}
                                                      entity-type="track"
                                                      label=${track.title}
                                                      request-mbid=${track.mbid}
                                                      request-artist=${this.artistName}
                                                  ></library-status-indicator>
                                              `}
                                    </div>
                                `;
                            })}
                        `;
                    })}
                </div>
            </section>
            ${this.renderTrackContextMenu()}
        `;
    }

    private renderTrackContextMenu() {
        const track = this.ctxMenuTrack;

        return html`
            <wa-popup
                id="track-context-menu"
                placement="bottom-start"
                flip
                shift
                .active=${this.ctxMenu.contextMenuOpen}
            >
                ${this.ctxMenu.contextMenuOpen && track
                    ? html`
                          <div class="context-menu-panel" role="menu" aria-label="Track actions">
                              ${this.filePathFor(track) !== ''
                                  ? html`
                                        <wa-dropdown-item
                                            @click=${() => this.onContextMenuAction('play')}
                                            @mouseenter=${() => this.ctxMenu.closePlaylistSubmenu()}
                                        >
                                            <wa-icon slot="icon" name="play"></wa-icon>
                                            Play
                                        </wa-dropdown-item>
                                        <wa-dropdown-item
                                            @click=${() => this.onContextMenuAction('add-to-queue')}
                                            @mouseenter=${() => this.ctxMenu.closePlaylistSubmenu()}
                                        >
                                            <wa-icon slot="icon" name="plus"></wa-icon>
                                            Add to Queue
                                        </wa-dropdown-item>
                                        <wa-dropdown-item
                                            @click=${() => this.onContextMenuAction('play-next')}
                                            @mouseenter=${() => this.ctxMenu.closePlaylistSubmenu()}
                                        >
                                            <wa-icon slot="icon" name="forward-step"></wa-icon>
                                            Play Next
                                        </wa-dropdown-item>
                                        <wa-dropdown-item
                                            class="submenu-item"
                                            @mouseenter=${() => this.openPlaylistSubmenu()}
                                            @mouseleave=${this.ctxMenu.scheduleSubmenuClose}
                                            @click=${(e: Event) => {
                                                e.stopPropagation();
                                                this.openPlaylistSubmenu();
                                            }}
                                        >
                                            <wa-icon slot="icon" name="plus"></wa-icon>
                                            Add to Playlist
                                            <span class="submenu-arrow">&#9654;</span>
                                        </wa-dropdown-item>
                                        <wa-dropdown-item
                                            @click=${() => this.onContextMenuAction('track-details')}
                                            @mouseenter=${() => this.ctxMenu.closePlaylistSubmenu()}
                                        >
                                            <wa-icon slot="icon" name="circle-info"></wa-icon>
                                            Track Details
                                        </wa-dropdown-item>
                                    `
                                  : nothing}
                              <wa-dropdown-item
                                  @click=${() => this.viewTrackOnMusicBrainz()}
                                  @mouseenter=${() => this.ctxMenu.closePlaylistSubmenu()}
                              >
                                  <wa-icon slot="icon" name="globe"></wa-icon>
                                  View on MusicBrainz
                              </wa-dropdown-item>
                          </div>
                      `
                    : nothing}
            </wa-popup>

            <wa-popup
                id="playlist-submenu"
                placement="right-start"
                flip
                shift
                .active=${this.ctxMenu.playlistSubmenuOpen}
            >
                ${this.ctxMenu.playlistSubmenuOpen
                    ? html`
                          <div
                              @mouseenter=${() => this.ctxMenu.clearSubmenuCloseTimer()}
                              @mouseleave=${this.ctxMenu.scheduleSubmenuClose}
                          >
                              <playlist-picker
                                  .filePaths=${this.ctxMenu.playlistFilePaths}
                                  @playlist-action-complete=${this.ctxMenu.onPlaylistActionComplete}
                                  @click=${(e: Event) => e.stopPropagation()}
                              ></playlist-picker>
                          </div>
                      `
                    : nothing}
            </wa-popup>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'explore-album-details': ExploreAlbumDetails;
    }
}

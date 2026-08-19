import { avatarBackground } from '@utils/avatar-color';
import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state, query } from 'lit/decorators.js';
import { classMap } from 'lit/directives/class-map.js';
import { designTokens } from '../../styles/tokens.css';
import {
    LookupArtist,
    BrowseReleaseGroups,
    TopRecordingsForArtist,
    TopReleaseGroupsForArtist,
    SimilarArtists,
    GetArtistImageURL,
    GetArtistImageCachedPath,
    GetThumbnail,
    GetThumbnails,
    GetTrackThumbnail,
    GetTrackThumbnails,
    ResolveReleaseGroupMBIDs,
    PrefetchReleases,
} from '@go/explore/service.js';
import type * as explore from '@go/explore/models.js';
type MBArtist = explore.MBArtist;
type MBReleaseGroup = explore.MBReleaseGroup;
type LBTopRecording = explore.LBTopRecording;
type LBTopReleaseGroup = explore.LBTopReleaseGroup;
type LBSimilarArtist = explore.LBSimilarArtist;
import { exploreCache } from '../../store/explore-cache';
import { libraryStore } from '../../store/library-store';
import { downloadStore } from '../../store/download-store';
import '@awesome.me/webawesome/dist/components/button/button.js';
import { trackLink, exploreLinkStyles } from '../../utils/explore-link';
import { describeError } from '../../utils/describe-error';
import {
    GetAlbumsByArtist,
    GetFilePathsByAlbums,
    GetFilePathsByRecordingMBIDs,
} from '@go/library/library.js';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '../library-status-indicator/library-status-indicator.js';
import {
    albumBadgeFor,
    libraryStatusFor,
    toggleRequest,
} from '@utils/library-status';
import {
    isOwned,
    ownershipLabel,
    unownedStyles,
} from '@utils/ownership';
import { completenessStore } from '@store/completeness-store';
import '../catalog-scope-notice/catalog-scope-notice.js';
import type { CatalogScope } from '../catalog-scope-notice/catalog-scope-notice.js';
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
import { dict, dictByName } from '@utils/binding';
import type { TrackDetails } from '@components/track-details/track-details.js';
import { showTrackDetailsForPath } from '@utils/track-details-opener.js';
import '@components/playlist-picker/playlist-picker.js';
import {
    ICON_CAN_REQUEST,
    ICON_PLAYLIST,
    ICON_QUEUE,
    ICON_REQUESTED,
} from '@utils/icon-language';

/* ── Constants ── */

/** The region the artist header's own failures are rendered in. */
export const ExploreArtistRegion = 'explore-artist';

/**
 * A release the context menu can act on, whichever shape it came from.
 *
 * The page shows release groups in two forms — the top section's
 * `LBTopReleaseGroup` and the discography's `MBReleaseGroup` — and the
 * three questions the menu asks are not the same question: playback
 * needs a *local album id*, a request needs a *catalog MBID*, and
 * "owned" is neither on its own.
 */
interface ReleaseMenuTarget {
    /** The catalog release-group MBID, or '' for a library-only release. */
    mbid: string;
    /** The local album id, or 0 when nothing local backs it. */
    localId: number;
    title: string;
    owned: boolean;
}

/** What the shared context menu panel is currently about. */
type ContextMenuTarget =
    | { kind: 'track'; track: LBTopRecording }
    | { kind: 'release'; release: ReleaseMenuTarget };

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
export class ExploreArtistDetails extends LitElement implements ContextMenuHost {
    /* ── Public attributes ── */

    @property({ type: String, attribute: 'artist-mbid' })
    artistMBID = '';

    @property({ type: String, attribute: 'artist-name' })
    artistName = '';

    @property({ type: Number, attribute: 'local-artist-id' })
    localArtistId = 0;

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
    @state() private errorReleases = '';
    /** True once the discography on screen came from the catalog rather
     * than standing in from the local library. */
    @state() private catalogLoaded = false;
    /** True while a catalog fetch — foreground or the background
     * discography build — may still land. */
    @state() private catalogPending = false;
    @state() private similarArtists: LBSimilarArtist[] = [];
    @state() private loadingSimilar = true;
    @state() private artistImageURL = '';
    @state() private similarImageURLs = new Map<string, string>();
    /** Resolved cover art data URLs keyed by release group MBID. */
    @state() private thumbnailURLs = new Map<string, string>();
    /** Track thumbnails keyed by recording MBID — resolved via RG
     * when possible, or the release MBID as fallback. */
    @state() private trackThumbnails = new Map<string, string>();
    @state() private topTracksExpanded = false;
    @state() private topReleasesExpanded = false;
    private topSectionStacked = false;
    private topSectionObserver?: ResizeObserver;
    @state() private expandedDiscoGroups = new Set<string>();
    /** Number of album cards that fit in one row of the discography grid. */
    @state() private discoRowSize = 5;
    private discoObserver?: ResizeObserver;
    @state() private similarExpanded = false;

    /* ── Release prefetch ── */

    /** Top-section mbids awaiting the next coalesced prefetch. */
    private pendingTopPrefetch = new Set<string>();
    /** Discography mbids awaiting the next coalesced prefetch. */
    private pendingPrefetch = new Set<string>();
    private prefetchScheduled = false;
    /** Every mbid already sent, so a refetch does not re-ask. */
    private prefetchRequested = new Set<string>();

    /* ── Context menu ── */

    private ctxMenu = new ContextMenuController(this);

    /**
     * What the open context menu applies to.
     *
     * A discriminated union rather than one nullable field per kind,
     * because the panel is shared between the top-tracks list and the
     * release cards: that is what keeps `aria-label` moving with the
     * target, which is the fault `cover-grid` shipped — every menu
     * announced as "Album actions".
     */
    @state() private ctxMenuTarget: ContextMenuTarget | null = null;

    @query('#context-menu')
    private contextMenuPopup!: WaPopup;

    @query('#playlist-submenu')
    private playlistSubmenuPopup?: WaPopup;

    @query('track-details')
    private trackDetailsDialog?: TrackDetails;

    /**
     * The open menu's file paths, resolved once per open — see the same
     * field on the album page. Only a track menu ever has any: a
     * release's tracks are a different question, and adding a whole
     * album to a playlist from here is not what this item says.
     */
    private ctxMenuPaths: Promise<string[]> | null = null;

    // -- ContextMenuHost interface --

    getContextMenuPopup(): WaPopup | undefined {
        return this.contextMenuPopup;
    }

    getPlaylistSubmenuPopup(): WaPopup | undefined {
        return this.playlistSubmenuPopup;
    }

    onContextMenuClose(): void {
        this.ctxMenuTarget = null;
        this.ctxMenuPaths = null;
    }

    /** The open menu's track, or null when it is not a track menu. */
    private get ctxMenuTrack(): LBTopRecording | null {
        return this.ctxMenuTarget?.kind === 'track'
            ? this.ctxMenuTarget.track
            : null;
    }

    /** The open menu's release, or null when it is not a release menu. */
    private get ctxMenuRelease(): ReleaseMenuTarget | null {
        return this.ctxMenuTarget?.kind === 'release'
            ? this.ctxMenuTarget.release
            : null;
    }

    /* ── Styles ── */

    static override styles = [
        designTokens,
        exploreLinkStyles,
        contextMenuStyles,
        unownedStyles,
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

            .artist-follow {
                margin-top: 10px;
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

            .track-item.owned {
                cursor: pointer;
            }

            .track-item:hover {
                background: var(
                    --yj-bg-overlay,
                    rgba(255, 255, 255, 0.04)
                );
            }

            .track-item:focus-visible {
                outline: 2px solid var(--yj-accent-text, #ffd43b);
                outline-offset: -2px;
            }

            .artist-play-actions {
                margin-top: 10px;
                display: flex;
                gap: 8px;
                align-items: center;
                flex-wrap: wrap;
            }

            .track-rank {
                width: 24px;
                text-align: right;
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-md);
                font-variant-numeric: tabular-nums;
                flex-shrink: 0;
            }

            .track-art {
                width: 32px;
                height: 32px;
                border-radius: 4px;
                overflow: hidden;
                flex-shrink: 0;
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                position: relative;
                display: flex;
                align-items: center;
                justify-content: center;
            }

            .track-art img {
                width: 100%;
                height: 100%;
                object-fit: cover;
                display: block;
            }

            .track-art wa-icon {
                font-size: 16px;
                color: var(--yj-text-tertiary, #888);
                opacity: 0.5;
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

            .track-item library-status-indicator {
                flex-shrink: 0;
            }

            /* ── Top section (tracks + releases side-by-side) ── */
            .top-section-wrapper {
                container-type: inline-size;
            }

            .top-section-columns {
                display: grid;
                grid-template-columns: 1fr minmax(300px, 1fr);
                gap: 24px;
            }

            .top-section-col {
                min-width: 0;
            }

            /* Wide: releases column uses absolute positioning so
               tracks alone determine the row height. */
            .top-section-col-releases {
                position: relative;
            }

            .top-releases-content {
                position: absolute;
                inset: 0;
                display: flex;
                flex-direction: column;
            }

            .top-releases-grid {
                flex: 1;
                min-height: 0;
            }

            .top-releases-inner {
                height: 100%;
                display: grid;
                grid-template-columns: 1fr 1fr;
                gap: 8px;
            }

            /* Per-column toggles: hidden in wide mode */
            .column-toggle {
                display: none;
            }

            /* Shared toggle: visible in wide mode */
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

            /* ── Narrow / stacked layout ── */
            @container (max-width: 650px) {
                .top-section-columns {
                    display: flex;
                    flex-direction: column;
                    gap: 16px;
                }

                /* Releases column goes back to normal flow */
                .top-section-col-releases {
                    position: static;
                }

                .top-releases-content {
                    position: static;
                    display: flex;
                    flex-direction: column;
                }

                .top-releases-inner {
                    height: auto;
                }

                /* Per-column toggles visible */
                .column-toggle {
                    display: flex;
                    align-items: center;
                    justify-content: center;
                    gap: 6px;
                    padding: 6px 12px;
                    margin-top: 8px;
                    border: none;
                    border-radius: 6px;
                    background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                    color: var(--yj-text-secondary, #b3b3b3);
                    font-size: var(--yj-text-sm);
                    cursor: pointer;
                    transition: background 0.15s ease, color 0.15s ease;
                    width: 100%;
                }

                .column-toggle:hover {
                    background: var(--yj-bg-hover, rgba(255, 255, 255, 0.1));
                    color: var(--yj-text-primary, #fff);
                }

                .column-toggle wa-icon {
                    font-size: 12px;
                    transition: transform 0.2s ease;
                }

                .column-toggle[aria-expanded='true'] wa-icon {
                    transform: rotate(180deg);
                }

                /* Shared toggle hidden */
                .top-section-toggle {
                    display: none;
                }
            }

            .top-release-card {
                display: flex;
                flex-direction: column;
                align-items: center;
                gap: 4px;
                cursor: pointer;
                transition: background 0.15s ease;
                padding: 4px;
                border-radius: 6px;
                min-width: 0;
                min-height: 0;
                overflow: hidden;
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
                flex: 1;
                min-height: 0;
                border-radius: 4px;
                overflow: hidden;
                position: relative;
                display: flex;
                align-items: center;
                justify-content: center;
            }

            .top-release-art img {
                max-width: 100%;
                max-height: 100%;
                object-fit: contain;
                display: block;
                margin: auto;
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
                font-size: var(--yj-text-xs);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .top-release-meta {
                display: flex;
                align-items: center;
                justify-content: space-between;
                gap: 6px;
                color: var(--yj-text-tertiary, #888);
                font-size: var(--yj-text-xs);
                min-height: 18px;
            }

            .top-release-meta-text {
                display: flex;
                align-items: center;
                gap: 6px;
                min-width: 0;
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
            }

            .top-release-meta library-status-indicator {
                flex-shrink: 0;
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
                grid-template-columns: repeat(auto-fill, 140px);
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
                flex-shrink: 0;
                position: relative;
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
            }

            .album-art-container img {
                width: 100%;
                height: 100%;
                object-fit: cover;
                display: block;
                border-radius: 4px;
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
                text-overflow: ellipsis;
                white-space: nowrap;
            }

            .album-meta library-status-indicator {
                flex-shrink: 0;
                margin-left: auto;
            }

            /* ── Similar artists ── */
            .similar-row {
                display: grid;
                grid-template-columns: repeat(auto-fill, 140px);
                gap: 16px;
                overflow: hidden;
            }

            .similar-row.collapsed {
                grid-template-rows: 1fr;
                overflow: hidden;
            }

            .similar-artist-card {
                display: flex;
                flex-direction: column;
                align-items: center;
                gap: 8px;
                padding: 10px;
                border-radius: 8px;
                cursor: pointer;
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
                width: 67px;
                height: 67px;
                border-radius: 50%;
                display: flex;
                align-items: center;
                justify-content: center;
                color: #fff;
                font-weight: 600;
                font-size: 28px;
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
                font-size: var(--yj-text-md);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                width: 100%;
            }
        `,
    ];

    /* ── Lifecycle ── */

    private unsubDiscogReady?: () => void;
    private unsubSimilarReady?: () => void;
    /** MBIDs whose ArtistSimilarReady event we've already handled, so a
     * background similar-artists fetch re-hydrates that section once. */
    private similarReloaded = new Set<string>();
    /** MBIDs whose ArtistDiscographyReady event we've already handled,
     * so an artist with no discography can't trigger a re-fetch loop. */
    private discogReloaded = new Set<string>();
    /** Fallback timer that stops the top-section spinners if the
     * background discography fetch never signals readiness. */
    private discogFallbackTimer?: number;

    /** Unsubscribe handle for the requests list. */
    private unsubRequests: (() => void) | null = null;

    /** Unsubscribes the "how much of this album is here" repaint. */
    private unsubCompleteness: (() => void) | null = null;

    override connectedCallback() {
        super.connectedCallback();
        if (this.artistMBID || this.localArtistId) {
            void this.loadAllData();
        }

        // Keep the follow button in step with the requests list, which a
        // background reconcile pass can change without this page doing
        // anything.
        this.unsubRequests = downloadStore.subscribe(() => this.requestUpdate());
        void downloadStore.init().then(() => this.requestUpdate());

        // The count behind a partly-held album lands a frame after the
        // cards do, since the store batches a screenful into one query.
        this.unsubCompleteness = completenessStore.subscribe(() =>
            this.requestUpdate(),
        );

        // A background discography fetch (top tracks / top releases for an
        // artist that wasn't indexed yet) finished — re-fetch those two
        // sections, once per artist, so they fill in without the initial
        // request having blocked.
        this.unsubDiscogReady = EventsOn(
            Events.ArtistDiscographyReady,
            (mbid: string) => {
                if (mbid !== this.artistMBID) return;
                if (this.discogReloaded.has(mbid)) return;

                if (this.discogFallbackTimer) clearTimeout(this.discogFallbackTimer);
                this.discogReloaded.add(mbid);
                void this.fetchTopTracks(mbid);
                void this.fetchTopReleaseGroups(mbid);
                // Full discography section is also index-first + async now,
                // so re-read it from the freshly-populated index too.
                void this.fetchReleaseGroups(mbid);
            },
        );

        // A background similar-artists fetch (LB labs, first view of an
        // artist) finished — re-fetch that section once per artist.
        this.unsubSimilarReady = EventsOn(
            Events.ArtistSimilarReady,
            (mbid: string) => {
                if (mbid !== this.artistMBID) return;
                if (this.similarReloaded.has(mbid)) return;

                this.similarReloaded.add(mbid);
                void this.fetchSimilarArtists(mbid);
            },
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.unsubRequests?.();
        this.unsubRequests = null;
        this.unsubCompleteness?.();
        this.unsubCompleteness = null;
        this.unsubDiscogReady?.();
        this.unsubSimilarReady?.();
        if (this.discogFallbackTimer) clearTimeout(this.discogFallbackTimer);
        this.topSectionObserver?.disconnect();
        this.discoObserver?.disconnect();
    }

    /**
     * Arm a one-shot fallback that clears the top-section loading state
     * if ArtistDiscographyReady never arrives (e.g. the artist genuinely
     * has no discography, or the background fetch stalled).  Treated as a
     * "reload happened" so the finally blocks resolve to empty state.
     */
    private armDiscogFallback(mbid: string) {
        if (this.discogFallbackTimer) clearTimeout(this.discogFallbackTimer);

        this.discogFallbackTimer = window.setTimeout(() => {
            if (this.discogReloaded.has(mbid)) return;

            this.discogReloaded.add(mbid);
            this.similarReloaded.add(mbid);
            this.catalogPending = false;
            if (this.topTracks.length === 0) this.loadingTracks = false;
            if (this.topReleaseGroups.length === 0) this.loadingTopReleases = false;
            if (this.releaseGroups.length === 0) this.loadingReleases = false;
            if (this.similarArtists.length === 0) this.loadingSimilar = false;
        }, 12000);
    }

    protected override firstUpdated() {
        this.observeTopSectionWidth();
        this.observeDiscoWidth();
    }

    protected override updated() {
        // Re-attach observers if elements appeared after initial render.
        if (!this.topSectionObserver) {
            this.observeTopSectionWidth();
        }
        if (!this.discoObserver) {
            this.observeDiscoWidth();
        }
    }

    /**
     * Watch the top-section-wrapper width. When transitioning from
     * stacked (≤650px) to side-by-side (>650px), sync expand states
     * so both columns match.
     */
    private observeTopSectionWidth() {
        const wrapper = this.renderRoot.querySelector('.top-section-wrapper');
        if (!wrapper) return;

        this.topSectionObserver = new ResizeObserver((entries) => {
            for (const entry of entries) {
                const width = entry.contentBoxSize?.[0]?.inlineSize ?? entry.contentRect.width;
                const nowStacked = width <= 650;

                if (this.topSectionStacked && !nowStacked) {
                    // Transitioning stacked → side-by-side: sync states.
                    // If either is expanded, expand both.
                    if (this.topTracksExpanded || this.topReleasesExpanded) {
                        this.topTracksExpanded = true;
                        this.topReleasesExpanded = true;
                    }
                }

                this.topSectionStacked = nowStacked;
            }
        });

        this.topSectionObserver.observe(wrapper);
    }

    /**
     * Watch the .content width and compute how many album cards
     * fit in one row of the discography grid.
     * Grid uses: repeat(auto-fill, minmax(140px, 1fr)) with 16px gap
     * and album-card has 8px padding on each side.
     */
    private observeDiscoWidth() {
        const content = this.renderRoot.querySelector('.content');
        if (!content) return;

        const CARD_MIN = 140;
        const GAP = 16;

        this.discoObserver = new ResizeObserver((entries) => {
            for (const entry of entries) {
                const width = entry.contentBoxSize?.[0]?.inlineSize ?? entry.contentRect.width;
                const cols = Math.max(1, Math.floor((width + GAP) / (CARD_MIN + GAP)));
                if (cols !== this.discoRowSize) {
                    this.discoRowSize = cols;
                }
            }
        });

        this.discoObserver.observe(content);
    }

    /* ── Data Loading ── */

    private async loadAllData() {
        const mbid = this.artistMBID;

        // Local-only artist (no MBID) — populate from library store.
        if (!mbid && this.localArtistId) {

            this.loadingArtist = false;
            this.loadingTracks = false;
            this.loadingTopReleases = false;
            this.loadingReleases = false;
            this.loadingSimilar = false;
            this.similarArtists = [];

            this.hydrateLocalOnly();


            return;
        }


        // Phase 0: hydrate from caches (instant, no Go calls).
        this.hydrateFromCache(mbid);

        // Fresh load for this artist: allow the top sections one
        // background-fetch re-fetch, and arm a fallback so they can't spin
        // forever if ArtistDiscographyReady never arrives.
        this.discogReloaded.delete(mbid);
        this.similarReloaded.delete(mbid);
        this.catalogLoaded = false;
        this.catalogPending = true;
        this.armDiscogFallback(mbid);

        // Phase 1: fire all API requests independently so the UI
        // renders each section as its data arrives, rather than
        // waiting for the slowest call to finish.
        void this.fetchArtist(mbid);
        void this.fetchTopTracks(mbid);
        void this.fetchTopReleaseGroups(mbid);
        void this.fetchReleaseGroups(mbid);
        void this.fetchSimilarArtists(mbid);

        // Artist image is fire-and-forget — doesn't block the page.
        if (!this.artistImageURL) {
            this.fetchArtistImage(mbid);
        }

        // Play count comes from LookupArtist (already populated from index).

        // checkLibrary now runs from fetchReleaseGroups when data arrives.

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
            const thumbUpdates = new Map(this.thumbnailURLs);
            let thumbsChanged = false;

            for (const a of cachedAlbums) {
                if (a.ArtistName.toLowerCase() === artistName) {
                    const mbid = a.MBID || `local:${a.ID}`;
                    libraryAlbums.push({
                        mbid,
                        title: a.Name,
                        primaryType: 'Album',
                        artistCredit: a.ArtistName,
                        firstReleaseDate: a.Year ? String(a.Year) : '',
                        inLibrary: true,
                        localId: a.ID,
                    } as MBReleaseGroup);

                    // Seed the thumbnail map directly from the library
                    // cover art so the renderer doesn't need to call
                    // GetThumbnail.  Album cover art lives at
                    // /covers/... served by the Wails dev server.
                    const art = a.CoverArtMedium || a.CoverArtSmall || a.CoverArtPath;
                    if (art && !thumbUpdates.has(mbid)) {
                        thumbUpdates.set(mbid, art);
                        thumbsChanged = true;
                    }
                }
            }

            if (libraryAlbums.length > 0) {
                this.releaseGroups = libraryAlbums;
                this.loadingReleases = false;
            }

            if (thumbsChanged) {
                this.thumbnailURLs = thumbUpdates;
            }
        }

        // Fallback: use album cover art if no artist image found.
        if (!this.artistImageURL && cachedAlbums) {
            const artistName = this.artistName.toLowerCase();

            for (const a of cachedAlbums) {
                if (a.ArtistName.toLowerCase() === artistName) {
                    const art = a.CoverArtMedium || a.CoverArtSmall || a.CoverArtPath;

                    if (art) {
                        this.artistImageURL = art;

                        break;
                    }
                }
            }
        }
    }

    /**
     * Populate the view entirely from library data when the artist
     * has no MusicBrainz ID.  Uses the local artist ID to fetch
     * albums and resolves the artist image from album cover art.
     */
    private async hydrateLocalOnly() {
        // Ensure library store is populated before reading.
        if (!libraryStore.cachedArtists || !libraryStore.cachedAlbums) {
            try {
                const pending: Promise<unknown>[] = [];
                if (!libraryStore.cachedArtists) {
                    pending.push(libraryStore.getArtists());
                }
                if (!libraryStore.cachedAlbums) {
                    pending.push(libraryStore.getAlbums());
                }
                await Promise.all(pending);
            } catch {
                // Non-fatal — continue with whatever's loaded.
            }
        }

        // Artist image: use library store cached artist, then try
        // the on-disk image cache by MBID if available, then fall
        // back to album art.
        const cachedArtists = libraryStore.cachedArtists;
        let artistMbidForImage = '';

        if (cachedArtists) {
            for (const a of cachedArtists) {
                if (a.ID === this.localArtistId) {
                    this.artistImageURL = a.ImageMedium || a.ImageSmall || '';
                    artistMbidForImage = a.MBID || '';
                    break;
                }
            }
        }

        if (!this.artistImageURL && artistMbidForImage) {
            try {
                const url = await GetArtistImageCachedPath(artistMbidForImage);
                if (url) this.artistImageURL = url;
            } catch {
                // Ignore.
            }
        }

        // Discography: fetch the artist's albums and seed thumbnails
        // directly from library cover art.
        try {
            const albums = await GetAlbumsByArtist(
                this.artistName,
                libraryStore.libraryFilter(),
            );
            const thumbUpdates = new Map(this.thumbnailURLs);
            let thumbsChanged = false;

            this.releaseGroups = (albums ?? []).map((a) => {
                const mbid = a.MBID || `local:${a.ID}`;
                const art = a.CoverArtMedium || a.CoverArtSmall || a.CoverArtPath;

                if (art && !thumbUpdates.has(mbid)) {
                    thumbUpdates.set(mbid, art);
                    thumbsChanged = true;
                }

                return {
                    mbid,
                    title: a.Name,
                    primaryType: 'Album',
                    artistCredit: a.ArtistName,
                    firstReleaseDate: a.Year ? String(a.Year) : '',
                    inLibrary: true,
                    localId: a.ID,
                    _coverArt: art || '',
                    _inLibrary: true,
                } as MBReleaseGroup & { _coverArt: string; _inLibrary: boolean };
            });

            if (thumbsChanged) {
                this.thumbnailURLs = thumbUpdates;
            }
        } catch {
            this.releaseGroups = [];
        }

        // Final fallback: artist image from first album's cover art.
        if (!this.artistImageURL) {
            for (const rg of this.releaseGroups) {
                const art = (rg as MBReleaseGroup & { _coverArt?: string })._coverArt;
                if (art) {
                    this.artistImageURL = art;
                    break;
                }
            }
        }
    }

    /**
     * Where this page's discography came from.  An artist with no MBID
     * can only ever show what the library holds; one whose catalog
     * fetch is still in flight says so rather than looking finished.
     */
    private catalogScope(): CatalogScope {
        if (!this.artistMBID) return 'library';
        if (this.catalogLoaded) return 'catalog';
        if (this.catalogPending) return 'loading';

        return 'unavailable';
    }

    /** Ask the catalog again after a failed or empty discography fetch. */
    private retryCatalog = () => {
        const mbid = this.artistMBID;
        if (!mbid) return;

        this.errorReleases = '';
        this.catalogPending = true;
        this.discogReloaded.delete(mbid);
        this.similarReloaded.delete(mbid);
        this.armDiscogFallback(mbid);
        void this.fetchTopTracks(mbid);
        void this.fetchTopReleaseGroups(mbid);
        void this.fetchReleaseGroups(mbid);
        void this.fetchSimilarArtists(mbid);
    };

    private async fetchArtist(mbid: string) {
        try {
            this.artist = await LookupArtist(mbid);
        } catch (err) {
            console.error('[explore-artist] LookupArtist error', err);
            this.errorArtist = describeError(
                err,
                'The catalog did not answer.',
            );
        } finally {
            this.loadingArtist = false;
        }
    }

    private async fetchTopTracks(mbid: string) {
        try {
            const tracks = await TopRecordingsForArtist(mbid);
            this.topTracks = tracks ?? [];

            if (!tracks || tracks.length === 0) return;

            // Resolve track CAA release MBIDs to their parent release
            // group MBIDs where possible.  When a track's CAA release
            // belongs to a release group in the index, we reuse the
            // shared RG cache (discography, top releases).  When it
            // doesn't, we fall back to fetching by the release MBID
            // directly via the backend's GetTrackThumbnail.
            const caaMbids = tracks
                .map((t) => t.caaReleaseMbid)
                .filter((m): m is string => !!m);

            let mapping: Record<string, string> = {};
            if (caaMbids.length > 0) {
                try {
                    mapping = await dictByName(ResolveReleaseGroupMBIDs(caaMbids));
                } catch (err) {
                    console.warn('[explore-artist] failed to resolve release groups for tracks:', err);
                }
            }

            void this.batchResolveTrackThumbnails(tracks, mapping);
        } catch (err) {
            console.error('[explore-artist] TopRecordingsForArtist error', err);
        } finally {
            // An empty first pass may mean a background discography fetch
            // is still in flight (the artist wasn't indexed yet).  Keep
            // the loading state up until the ArtistDiscographyReady
            // re-fetch runs, so the section doesn't flash empty.
            if (this.topTracks.length > 0 || this.discogReloaded.has(mbid)) {
                this.loadingTracks = false;
            }
        }
    }

    /**
     * Two-phase track thumbnail loader: checks the backend cache in
     * one batch call, then fires individual network fetches for any
     * that are missing.  Each track's thumbnail is keyed by the
     * track's recording MBID so the renderer doesn't have to know
     * whether the art came from RG or release fallback.
     */
    private async batchResolveTrackThumbnails(
        tracks: LBTopRecording[],
        releaseToRG: Record<string, string>,
    ) {
        const needed = tracks.filter(
            (t) => t.recordingMbid && !this.trackThumbnails.has(t.recordingMbid),
        );
        if (needed.length === 0) return;

        // Mark all as in-flight.
        for (const t of needed) {
            this.trackThumbnails.set(t.recordingMbid, '');
        }

        const requests = needed.map((t) => ({
            key: t.recordingMbid,
            releaseMbid: t.caaReleaseMbid || '',
            releaseGroupMbid: releaseToRG[t.caaReleaseMbid] || '',
            albumName: t.releaseName || '',
            artistName: t.artistName || '',
        }));

        // Phase 1: batched cached lookup.
        let cached: Record<string, string> = {};
        try {
            cached = await dictByName(GetTrackThumbnails(requests));
            if (Object.keys(cached).length > 0) {
                const updated = new Map(this.trackThumbnails);
                for (const [key, url] of Object.entries(cached)) {
                    if (url) updated.set(key, url);
                }

                this.trackThumbnails = updated;
            }
        } catch {
            // Best-effort.
        }

        // Phase 2: per-track network fetches for anything still missing.
        const uncached = requests.filter((r) => !cached[r.key]);
        for (const req of uncached) {
            GetTrackThumbnail(
                req.releaseMbid,
                req.releaseGroupMbid,
                req.albumName,
                req.artistName,
            )
                .then((url) => {
                    if (url) {
                        this.trackThumbnails = new Map(this.trackThumbnails).set(req.key, url);
                    }
                })
                .catch(() => {});
        }
    }

    private async fetchTopReleaseGroups(mbid: string) {
        try {
            const rgs = await TopReleaseGroupsForArtist(mbid);
            this.topReleaseGroups = rgs ?? [];

            // Batch-resolve cover art for top releases.
            void this.batchResolveThumbnails(
                rgs?.map((r) => ({ mbid: r.releaseGroupMbid, albumName: r.title, artistName: r.artistName }))
                    ?? [],
            );

            // Warm the release/tracklist cache for the top albums — these
            // are the most likely to be clicked from the artist page.
            this.prefetchReleases(rgs?.map((r) => r.releaseGroupMbid) ?? [], true);
        } catch (err) {
            console.error('[explore-artist] TopReleaseGroupsForArtist error', err);
            this.topReleaseGroups = [];
        } finally {
            // See fetchTopTracks: hold the spinner while a background
            // discography fetch may still populate this section.
            if (this.topReleaseGroups.length > 0 || this.discogReloaded.has(mbid)) {
                this.loadingTopReleases = false;
            }
        }
    }

    private async fetchReleaseGroups(mbid: string) {
        try {
            const rgs = await BrowseReleaseGroups(mbid);

            // An empty result is "the index has not built this artist
            // yet", not "this artist released nothing" — so it must not
            // wipe the library albums hydrateFromCache put on screen.
            if (rgs && rgs.length > 0) {
                this.releaseGroups = rgs;
                this.catalogLoaded = true;
                this.catalogPending = false;
            } else if (this.discogReloaded.has(mbid)) {
                this.catalogPending = false;
            }

            // Batch-resolve cover art for discography (lower priority — loaded after top sections).
            void this.batchResolveThumbnails(
                rgs?.map((r) => ({ mbid: r.mbid, albumName: r.title, artistName: r.artistCredit }))
                    ?? [],
            );

            // Warm the release/tracklist cache for these albums so opening
            // one from here is instant instead of a cold MB browse.
            this.prefetchReleases(rgs?.map((r) => r.mbid) ?? []);
        } catch (err) {
            console.error('[explore-artist] BrowseReleaseGroups error', err);
            this.errorReleases = describeError(
                err,
                'The catalog did not answer for this artist\u2019s albums.',
            );
            this.catalogPending = false;
        } finally {
            // BrowseReleaseGroups is index-first + async: an empty result on
            // a cold artist means the discography is still being fetched in
            // the background.  Hold the spinner until it arrives (via
            // ArtistDiscographyReady) or the fallback fires.
            if (this.releaseGroups.length > 0 || this.discogReloaded.has(mbid)) {
                this.loadingReleases = false;
            }
        }
    }

    /**
     * Warm the backend's release/tracklist cache for a set of release
     * groups so opening an album from this page is instant.
     * Fire-and-forget.
     *
     * The page's two sections resolve independently and both want this,
     * so the mbids are collected and sent once on a microtask rather
     * than once per section — `BrowseReleases` is the most expensive
     * call the app makes, on a 1 req/s limiter, and asking twice for an
     * overlapping set spends that limiter on nothing.
     *
     * `prefetchRequested` is what stops the cold-artist refetch — which
     * re-runs every fetch on `ArtistDiscographyReady` — asking again for
     * everything it already asked for.
     */
    private prefetchReleases(mbids: string[], top = false) {
        const pending = top ? this.pendingTopPrefetch : this.pendingPrefetch;

        for (const mbid of mbids) {
            if (mbid) pending.add(mbid);
        }

        if (this.pendingTopPrefetch.size + this.pendingPrefetch.size === 0) {
            return;
        }

        if (this.prefetchScheduled) return;

        this.prefetchScheduled = true;

        queueMicrotask(() => {
            this.prefetchScheduled = false;

            // Top releases lead: they are what the page shows first, and
            // the backend takes the list in order.
            const batch = [
                ...new Set([
                    ...this.pendingTopPrefetch,
                    ...this.pendingPrefetch,
                ]),
            ].filter((mbid) => !this.prefetchRequested.has(mbid));

            this.pendingTopPrefetch.clear();
            this.pendingPrefetch.clear();

            if (batch.length === 0) return;

            for (const mbid of batch) this.prefetchRequested.add(mbid);

            void PrefetchReleases(batch).catch(() => {
                /* best-effort cache warming — ignore failures */
            });
        });
    }

    private async fetchSimilarArtists(mbid: string) {
        try {
            const artists = await SimilarArtists(mbid);
            this.similarArtists = artists ?? [];
        } catch (err) {
            // D024: graceful degradation — silently omit similar artists on failure.
            console.error('[explore-artist] SimilarArtists error', err);
            this.similarArtists = [];
        } finally {
            // SimilarArtists is DB-first + async: an empty result on the
            // first view means the LB labs fetch is still running.  Hold the
            // spinner until ArtistSimilarReady re-fetches (or the fallback
            // fires); once reloaded, an empty list is genuinely "none".
            if (this.similarArtists.length > 0 || this.similarReloaded.has(mbid)) {
                this.loadingSimilar = false;
            }
        }

        // Fire-and-forget: resolve images for similar artists in parallel.
        if (this.similarArtists.length > 0) {
            void this.fetchSimilarArtistImages();
        }
    }

    /**
     * Batch-resolve thumbnails in two phases:
     * 1. Batch call for cached/local art — instant.
     * 2. Individual async calls for uncached items — stream in one by one.
     * This prevents a slow CAA fetch from blocking cached items from rendering.
     */
    private async batchResolveThumbnails(
        items: Array<{ mbid: string; albumName: string; artistName: string }>,
    ) {
        // Filter out already-resolved or in-flight items.
        const needed = items.filter((i) => i.mbid && !this.thumbnailURLs.has(i.mbid));
        if (needed.length === 0) return;

        // Mark all as in-flight.
        for (const item of needed) {
            this.thumbnailURLs.set(item.mbid, '');
        }

        // Phase 1: batch cached lookup — instant return.
        let cached: Record<string, string> = {};
        try {
            const requests = needed.map((i) => ({
                mbid: i.mbid,
                albumName: i.albumName,
                artistName: i.artistName,
            }));

            cached = await dictByName(GetThumbnails(requests));
            if (Object.keys(cached).length > 0) {
                const updated = new Map(this.thumbnailURLs);
                for (const [mbid, url] of Object.entries(cached)) {
                    if (url) updated.set(mbid, url);
                }

                this.thumbnailURLs = updated;
            }
        } catch {
            // Best-effort.
        }

        // Phase 2: fire individual fetches for uncached items so they
        // stream in as CAA responds, rather than blocking on the slowest.
        const uncached = needed.filter((i) => !cached[i.mbid]);
        for (const item of uncached) {
            GetThumbnail(item.mbid, item.albumName, item.artistName)
                .then((url) => {
                    if (url) {
                        this.thumbnailURLs = new Map(this.thumbnailURLs).set(item.mbid, url);
                    }
                })
                .catch(() => {});
        }
    }

    /**
     * Populate similarImageURLs for the current similarArtists list
     * using only library-store data and disk-cached artist images.
     * Makes ZERO network calls.
     *
     * Resolution priority per artist:
     *   1. libraryStore.cachedArtists[mbid].ImageMedium (in-memory)
     *   2. GetArtistImageCachedPath(mbid) (disk-only Wails call)
     *   3. First library album cover art by that artist name (fallback)
     */
    private async seedSimilarArtistImagesFromLibrary() {
        if (this.similarArtists.length === 0) return;

        const cachedArtists = libraryStore.cachedArtists;
        const cachedAlbums = libraryStore.cachedAlbums;

        // Build an MBID → ImageMedium map from library store.
        const byMbid = new Map<string, string>();
        if (cachedArtists) {
            for (const a of cachedArtists) {
                if (!a.MBID) continue;
                const img = a.ImageMedium || a.ImageSmall;
                if (img) byMbid.set(a.MBID, img);
            }
        }

        // Seed synchronous hits first.
        let updates = new Map(this.similarImageURLs);
        let changed = false;

        for (const s of this.similarArtists) {
            if (updates.has(s.artistMbid)) continue;
            const img = byMbid.get(s.artistMbid);
            if (img) {
                updates.set(s.artistMbid, img);
                changed = true;
            }
        }

        if (changed) {
            this.similarImageURLs = updates;
            changed = false;
            updates = new Map(this.similarImageURLs);
        }

        // Disk-only Wails calls for any similars that weren't in the
        // library store cache (e.g. artist row exists but ImageMedium
        // is empty because primary_md.jpg was added after the eager
        // fetch).  Fire in parallel, update the map as each returns.
        const pending: Promise<void>[] = [];

        for (const s of this.similarArtists) {
            if (updates.has(s.artistMbid) && updates.get(s.artistMbid)) continue;
            if (!s.artistMbid) continue;

            pending.push(
                GetArtistImageCachedPath(s.artistMbid)
                    .then((url) => {
                        if (url) {
                            this.similarImageURLs = new Map(this.similarImageURLs).set(
                                s.artistMbid,
                                url,
                            );
                            return;
                        }

                        // Last-resort fallback: first library album by
                        // this artist name (covers artists without
                        // cached portraits).
                        if (!cachedAlbums) return;
                        const targetName = (s.name || '').toLowerCase();
                        for (const album of cachedAlbums) {
                            if (album.ArtistName.toLowerCase() !== targetName) continue;
                            const art = album.CoverArtMedium || album.CoverArtSmall || album.CoverArtPath;
                            if (art) {
                                this.similarImageURLs = new Map(this.similarImageURLs).set(
                                    s.artistMbid,
                                    art,
                                );
                                return;
                            }
                        }
                    })
                    .catch(() => {
                        // Ignore — letter avatar stays.
                    }),
            );
        }

        await Promise.allSettled(pending);
    }

    private async fetchSimilarArtistImages() {
        // Phase 1: instant seed from library store + disk cache, so any
        // card whose image is already on disk appears immediately,
        // without waiting for a network-enabled GetArtistImageURL
        // round-trip.
        await this.seedSimilarArtistImagesFromLibrary();

        // Phase 2: network fetch for any similars still without
        // an image.  GetArtistImageURL triggers MB → Wikidata →
        // Wikimedia resolution which is slow (~1-5s per artist),
        // so we only hit it for cards that weren't resolved
        // instantly from local sources above.
        const artists = this.similarArtists;

        await Promise.allSettled(
            artists.map(async (a) => {
                if (!a.artistMbid) return;
                if (this.similarImageURLs.get(a.artistMbid)) return;

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
            // No image available.
        }

        // Fallback: album cover art if no artist image resolved.
        if (!this.artistImageURL) {
            this.fallbackArtistImageFromAlbumArt();
        }
    }

    /**
     * Populate artistImageURL from the first matching library album's
     * cover art.  Used as a last-resort fallback when no portrait is
     * available and we don't want to make a network call.
     */
    private fallbackArtistImageFromAlbumArt() {
        if (this.artistImageURL) return;

        const cachedAlbums = libraryStore.cachedAlbums;
        if (!cachedAlbums) return;

        const artistName = this.artistName.toLowerCase();

        for (const a of cachedAlbums) {
            if (a.ArtistName.toLowerCase() !== artistName) continue;

            const art = a.CoverArtMedium || a.CoverArtSmall || a.CoverArtPath;
            if (art) {
                this.artistImageURL = art;
                return;
            }
        }
    }

    /* ── Playback ── */

    /**
     * Local album ids for every release group this page already knows
     * is owned. `releaseGroups` holds the artist's full discography, so
     * this covers everything the "Play library tracks" button promises
     * — not just what is currently expanded on screen.
     */
    private ownedLocalAlbumIds(): number[] {
        const ids = new Set<number>();

        for (const rg of this.releaseGroups) {
            if (rg.localId && rg.localId > 0) ids.add(rg.localId);
        }

        return [...ids];
    }

    /**
     * File paths for every track this page can show is owned, across
     * the whole discography. Each id came from a release group the
     * backend or the library cache already cross-referenced, and
     * `GetFilePathsByAlbums` only ever returns files that actually
     * exist for that local album — so this cannot pull in a track the
     * user does not have, even when the catalog release itself is only
     * partially owned.
     */
    private async libraryFilePaths(): Promise<string[]> {
        const albumIds = this.ownedLocalAlbumIds();

        if (albumIds.length === 0) return [];

        const libraryID = libraryStore.getSelectedLibraryId() ?? 0;
        const byAlbum = await dict(GetFilePathsByAlbums(albumIds, libraryID));
        const paths: string[] = [];

        for (const id of albumIds) paths.push(...(byAlbum[id] ?? []));

        return paths;
    }

    /**
     * The local artist id for "Playing from" purposes — the `local-
     * artist-id` navigation attribute when the caller had one, else a
     * lookup by MBID against the cached library artists (the same
     * cross-reference `hydrateFromCache`/`checkLibrary` already use).
     */
    private resolveLocalArtistId(): number {
        if (this.localArtistId > 0) return this.localArtistId;

        if (!this.artistMBID) return 0;

        for (const a of libraryStore.cachedArtists ?? []) {
            if (a.MBID === this.artistMBID) return a.ID;
        }

        return 0;
    }

    private queueSource(): QueueSource | undefined {
        const id = this.resolveLocalArtistId();

        if (id === 0) return undefined;

        return { type: 'artist', id, label: this.displayName };
    }

    /** Play every owned track by this artist, optionally shuffled. */
    private async playLibraryTracks(shuffle: boolean): Promise<void> {
        try {
            const paths = await this.libraryFilePaths();

            if (paths.length === 0) {
                notificationStore.inline(ExploreArtistRegion, {
                    text: 'None of this artist’s tracks could be found in your library.',
                });

                return;
            }

            if (shuffle && !queueStore.getState().shuffleMode) {
                queueStore.toggleShuffle();
            }

            queueStore.setQueue(paths, 0, shuffle, this.queueSource());
        } catch (error) {
            console.error('Could not play artist:', error);
            notificationStore.inline(ExploreArtistRegion, {
                text: describeError(error, 'Could not play this artist’s library tracks.'),
            });
        }
    }

    /**
     * File path for one top track, resolved by recording MBID — the
     * same key `inLibrary`/`localId` were set from. Works whether or
     * not the containing release itself matched a local album.
     */
    private async trackFilePath(track: LBTopRecording): Promise<string | null> {
        if (!(track.inLibrary || track.localId) || !track.recordingMbid) return null;

        const libraryID = libraryStore.getSelectedLibraryId() ?? 0;
        const byMBID = await dictByName(
            GetFilePathsByRecordingMBIDs([track.recordingMbid], libraryID),
        );

        return byMBID[track.recordingMbid]?.[0] ?? null;
    }

    private async playTrack(track: LBTopRecording): Promise<void> {
        try {
            const path = await this.trackFilePath(track);

            if (!path) {
                notificationStore.inline(ExploreArtistRegion, {
                    text: 'This track could not be found in your library.',
                });

                return;
            }

            queueStore.setQueue([path], 0, false, this.queueSource());
        } catch (error) {
            console.error('Could not play track:', error);
            notificationStore.inline(ExploreArtistRegion, {
                text: describeError(error, 'Could not play this track.'),
            });
        }
    }

    private async queueTrackNext(track: LBTopRecording): Promise<void> {
        const path = await this.trackFilePath(track);

        if (path) queueStore.playNext(path);
    }

    private async addTrackToQueue(track: LBTopRecording): Promise<void> {
        const path = await this.trackFilePath(track);

        if (path) queueStore.addToQueue(path);
    }

    private isTrackOwned(track: LBTopRecording): boolean {
        return isOwned(track);
    }

    private onTrackRowDblClick(track: LBTopRecording): void {
        if (!this.isTrackOwned(track)) return;

        void this.playTrack(track);
    }

    private onTrackRowKeydown(e: KeyboardEvent, track: LBTopRecording): void {
        if (isContextMenuKey(e)) {
            e.preventDefault();
            this.ctxMenuTarget = { kind: 'track', track };
            this.ctxMenu.openFrom(e.currentTarget as HTMLElement);

            return;
        }

        if ((e.key === 'Enter' || e.key === ' ') && this.isTrackOwned(track)) {
            e.preventDefault();
            void this.playTrack(track);
        }
    }

    private onTrackContextMenu(e: MouseEvent, track: LBTopRecording): void {
        e.preventDefault();
        e.stopPropagation();

        this.ctxMenuTarget = { kind: 'track', track };
        this.ctxMenu.openAt(e.clientX, e.clientY);
    }

    /* ── Release context menu ── */

    // The two release shapes on this page are normalised at the moment
    // the menu opens, so the union does not reach the action handlers.

    /** Normalise a top-section release group. */
    private topReleaseTarget(rg: LBTopReleaseGroup): ReleaseMenuTarget {
        return {
            mbid: rg.releaseGroupMbid || '',
            localId: rg.localId ?? 0,
            title: rg.title,
            owned: isOwned(rg),
        };
    }

    /**
     * Normalise a discography release group.
     *
     * A library-only release arrives as `local:<n>`, which names nothing
     * upstream — so its catalog MBID is empty and the local id is
     * unwrapped from it, the same way `navigateToAlbum` does.
     */
    private albumTarget(rg: MBReleaseGroup): ReleaseMenuTarget {
        const isLocal =
            typeof rg.mbid === 'string' && rg.mbid.startsWith('local:');
        const localId = rg.localId || (isLocal ? Number(rg.mbid.slice(6)) : 0);

        return {
            mbid: isLocal ? '' : rg.mbid || '',
            localId: Number.isFinite(localId) ? localId : 0,
            title: rg.title,
            // The same answer the menu gates Play on, which is the
            // point: this used to be `inLibrary` too, so a card could
            // report itself owned, be offered no Play (that item is
            // gated on the local id) and be offered no request either
            // (that one is gated on *not* owned).
            owned: localId > 0,
        };
    }

    private onReleaseContextMenu(e: MouseEvent, release: ReleaseMenuTarget): void {
        e.preventDefault();
        e.stopPropagation();

        this.ctxMenuTarget = { kind: 'release', release };
        this.ctxMenu.openAt(e.clientX, e.clientY);
    }

    private onReleaseKeydown(
        e: KeyboardEvent,
        release: ReleaseMenuTarget,
        activate: () => void,
    ): void {
        if (isContextMenuKey(e)) {
            e.preventDefault();
            this.ctxMenuTarget = { kind: 'release', release };
            this.ctxMenu.openFrom(e.currentTarget as HTMLElement);

            return;
        }

        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            activate();
        }
    }

    /**
     * The release's files, keyed on the local album id.
     *
     * Keyed on the id rather than the MBID for the reason the album page
     * is: an owned but untagged release has no recording MBIDs, so an
     * MBID-keyed lookup returns nothing while looking entirely correct.
     */
    private async releaseFilePaths(
        release: ReleaseMenuTarget,
    ): Promise<string[]> {
        if (release.localId <= 0) return [];

        const libraryID = libraryStore.getSelectedLibraryId() ?? 0;
        const byAlbum = await dict(
            GetFilePathsByAlbums([release.localId], libraryID),
        );

        return byAlbum[release.localId] ?? [];
    }

    private async onReleaseAction(
        action: 'play' | 'add-to-queue' | 'play-next',
    ): Promise<void> {
        const release = this.ctxMenuRelease;

        this.ctxMenu.close();

        if (!release) return;

        try {
            const paths = await this.releaseFilePaths(release);

            if (paths.length === 0) {
                notificationStore.inline(ExploreArtistRegion, {
                    text: `No files for ${release.title} were found in your library.`,
                });

                return;
            }

            switch (action) {
                case 'play':
                    queueStore.setQueue(paths, 0, false, this.queueSource());
                    break;
                case 'add-to-queue':
                    for (const path of paths) queueStore.addToQueue(path);
                    break;
                case 'play-next':
                    for (const path of [...paths].reverse())
                        queueStore.playNext(path);
                    break;
            }
        } catch (err) {
            console.error('Could not queue release:', err);
            notificationStore.inline(ExploreArtistRegion, {
                text: describeError(err, `Could not play ${release.title}.`),
            });
        }
    }

    private async onReleaseRequestToggle(): Promise<void> {
        const release = this.ctxMenuRelease;

        this.ctxMenu.close();

        if (!release?.mbid) return;

        try {
            await toggleRequest({
                mbid: release.mbid,
                entity: 'album',
                title: release.title,
                artist: this.artist?.name ?? this.artistName,
            });
        } catch (err) {
            console.error('Could not change the request:', err);
            notificationStore.inline(ExploreArtistRegion, {
                text: describeError(err, `Could not request ${release.title}.`),
            });
        }
    }

    private viewReleaseOnMusicBrainz(): void {
        const release = this.ctxMenuRelease;

        this.ctxMenu.close();

        if (!release?.mbid) return;

        window.open(
            `https://musicbrainz.org/release-group/${release.mbid}`,
            '_blank',
            'noopener',
        );
    }

    private onContextMenuAction(
        action: 'play' | 'add-to-queue' | 'play-next' | 'track-details',
    ): void {
        const track = this.ctxMenuTrack;

        this.ctxMenu.close();

        if (!track || !this.isTrackOwned(track)) return;

        switch (action) {
            case 'play':
                void this.playTrack(track);
                break;
            case 'add-to-queue':
                void this.addTrackToQueue(track);
                break;
            case 'play-next':
                void this.queueTrackNext(track);
                break;
            case 'track-details':
                void this.openTrackDetails(track);
                break;
        }
    }

    /**
     * Open the "Add to Playlist" submenu for the track the menu is on.
     *
     * The path is resolved on demand rather than at menu-open time —
     * the release menus that share this panel never need one, and a
     * right-click on a track is not a statement that a playlist is
     * coming. A menu closed while the lookup was in flight must not
     * sprout a submenu afterwards.
     */
    private async openPlaylistSubmenu(explicit: boolean): Promise<void> {
        const track = this.ctxMenuTrack;

        if (!track || !this.isTrackOwned(track)) return;

        this.ctxMenu.clearSubmenuCloseTimer();
        this.ctxMenuPaths ??= this.trackFilePath(track).then((p) =>
            p ? [p] : []);

        let paths: string[] = [];

        try {
            paths = await this.ctxMenuPaths;
        } catch (error) {
            console.error('Could not resolve the track’s file:', error);
            this.ctxMenuPaths = null;
        }

        if (!this.ctxMenu.contextMenuOpen) return;

        if (paths.length === 0) {
            // Only an explicit activation gets an answer. A hover is how
            // a submenu is *reached*, including on the way to the item
            // below it — reporting a failure from one would put an error
            // on screen for a menu the user was only passing through,
            // and closing the menu under the pointer is worse still.
            if (!explicit) return;

            this.ctxMenu.close();
            notificationStore.inline(ExploreArtistRegion, {
                text: 'This track could not be found in your library.',
            });

            return;
        }

        await this.ctxMenu.showPlaylistSubmenu(paths);
    }

    /**
     * The details dialog for an owned top track.
     *
     * The dialog wants the library's `Track` and this page has the
     * catalog's recording, so the route in is the same MBID → file path
     * resolution the playback actions use.
     */
    private async openTrackDetails(track: LBTopRecording): Promise<void> {
        try {
            const path = await this.trackFilePath(track);
            const outcome = path
                ? await showTrackDetailsForPath(
                    () => this.trackDetailsDialog,
                    path,
                    () => void this.openTrackDetails(track),
                )
                : 'not-in-library';

            if (outcome === 'not-in-library') {
                notificationStore.inline(ExploreArtistRegion, {
                    text: 'This track could not be found in your library.',
                });
            }
        } catch (error) {
            console.error('Could not open track details:', error);
            notificationStore.inline(ExploreArtistRegion, {
                text: describeError(error, 'Could not open track details.'),
            });
        }
    }

    private viewTrackOnMusicBrainz(): void {
        const track = this.ctxMenuTrack;

        this.ctxMenu.close();

        if (!track?.recordingMbid) return;

        window.open(`https://musicbrainz.org/recording/${track.recordingMbid}`, '_blank', 'noopener');
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

    private navigateToAlbum(rg: MBReleaseGroup) {
        // Strip the synthetic local: prefix that hydrateFromCache uses
        // for library-only albums without an MBID — those navigate by
        // local ID instead.
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
                    artistName: rg.artistCredit || this.artistName,
                    localAlbumId: localId,
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
                    style="background: ${avatarBackground(this.artistName)}"
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
                    ${this.artist?.popularity && this.artist.popularity > 0
                        ? html`<span class="artist-meta">${formatListenCount(this.artist.popularity)} plays on ListenBrainz</span>`
                        : nothing}
                    ${this.renderPlayLibraryAction()}
                    ${this.renderFollowAction()}
                </div>
            </div>
            <div class="content">
                <catalog-scope-notice
                    scope=${this.catalogScope()}
                    entity-type="artist"
                    @catalog-retry=${this.retryCatalog}
                ></catalog-scope-notice>
                ${this.renderTopSection()} ${this.renderDiscography()}
                ${this.renderSimilarArtists()}
            </div>
            <inline-notice
                region=${ExploreArtistRegion}
                testid="artist-action-message"
            ></inline-notice>
            ${this.renderContextMenu()}
            <track-details></track-details>
        `;
    }

    /**
     * The artist-page equivalent of the album page's Play button: play
     * everything by this artist that is actually in the library. Only
     * rendered when at least one release group is owned — an artist
     * page with nothing local has nothing for this button to do.
     */
    private renderPlayLibraryAction() {
        if (this.ownedLocalAlbumIds().length === 0) return nothing;

        return html`
            <div class="artist-play-actions">
                <wa-button
                    size="small"
                    appearance="filled"
                    data-testid="artist-play-library"
                    @click=${() => void this.playLibraryTracks(false)}
                >
                    <wa-icon slot="start" name="play"></wa-icon>
                    Play library tracks
                </wa-button>
                <wa-button
                    size="small"
                    appearance="outlined"
                    data-testid="artist-shuffle-library"
                    @click=${() => void this.playLibraryTracks(true)}
                >
                    <wa-icon slot="start" name="shuffle"></wa-icon>
                    Shuffle
                </wa-button>
            </div>
        `;
    }

    /**
     * The one context menu panel, shared by the top tracks and the
     * release cards.
     *
     * The label is computed from the target rather than written down,
     * which is the fault `cover-grid` shipped — a shared panel that
     * announced every menu as "Album actions".
     */
    private renderContextMenu() {
        const target = this.ctxMenuTarget;

        return html`
            <wa-popup
                id="context-menu"
                placement="bottom-start"
                flip
                shift
                .active=${this.ctxMenu.contextMenuOpen}
            >
                ${this.ctxMenu.contextMenuOpen && target
                    ? html`
                          <div
                              class="context-menu-panel"
                              role="menu"
                              aria-label=${target.kind === 'track'
                                  ? 'Track actions'
                                  : 'Release actions'}
                          >
                              ${target.kind === 'track'
                                  ? this.renderTrackMenuItems(target.track)
                                  : this.renderReleaseMenuItems(target.release)}
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

    private renderTrackMenuItems(track: LBTopRecording) {
        return html`
            ${this.isTrackOwned(track)
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
                          <wa-icon slot="icon" name=${ICON_QUEUE}></wa-icon>
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
                          @mouseenter=${() => void this.openPlaylistSubmenu(false)}
                          @mouseleave=${this.ctxMenu.scheduleSubmenuClose}
                          @click=${(e: Event) => {
                              e.stopPropagation();
                              void this.openPlaylistSubmenu(true);
                          }}
                      >
                          <wa-icon slot="icon" name=${ICON_PLAYLIST}></wa-icon>
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
        `;
    }

    /**
     * Which items a release gets, decided by what it can actually do.
     *
     * Playback is gated on a local album id rather than on "owned": a
     * release matched by MBID with no local album behind it has nothing
     * to queue. The request needs the opposite — a catalog MBID — so it
     * is absent for a library-only release, which is also the one case
     * where wanting it makes no sense.
     */
    private renderReleaseMenuItems(release: ReleaseMenuTarget) {
        const requested =
            libraryStatusFor(release.owned, release.mbid) === 'queued';

        return html`
            ${release.localId > 0
                ? html`
                      <wa-dropdown-item @click=${() => void this.onReleaseAction('play')}>
                          <wa-icon slot="icon" name="play"></wa-icon>
                          Play
                      </wa-dropdown-item>
                      <wa-dropdown-item @click=${() => void this.onReleaseAction('add-to-queue')}>
                          <wa-icon slot="icon" name=${ICON_QUEUE}></wa-icon>
                          Add to Queue
                      </wa-dropdown-item>
                      <wa-dropdown-item @click=${() => void this.onReleaseAction('play-next')}>
                          <wa-icon slot="icon" name="forward-step"></wa-icon>
                          Play Next
                      </wa-dropdown-item>
                  `
                : nothing}
            ${!release.owned && release.mbid
                ? html`
                      <wa-dropdown-item @click=${() => void this.onReleaseRequestToggle()}>
                          <wa-icon
                              slot="icon"
                              name=${requested ? ICON_REQUESTED : ICON_CAN_REQUEST}
                          ></wa-icon>
                          ${requested ? 'Cancel Request' : 'Request This'}
                      </wa-dropdown-item>
                  `
                : nothing}
            ${release.mbid
                ? html`
                      <wa-dropdown-item @click=${() => this.viewReleaseOnMusicBrainz()}>
                          <wa-icon slot="icon" name="globe"></wa-icon>
                          View on MusicBrainz
                      </wa-dropdown-item>
                  `
                : nothing}
        `;
    }

    /**
     * Subscribes to an artist: their new releases go on the requests
     * list as they come out.
     *
     * The default is new releases only. Following an artist should not
     * silently queue forty albums — someone who wants the back
     * catalogue can widen it from the requests list, and will not be
     * surprised by having done so.
     */
    private renderFollowAction() {
        if (!this.artistMBID) return nothing;

        const request = downloadStore.requestFor(this.artistMBID);

        return html`
            <div class="artist-follow">
                <wa-button
                    size="small"
                    appearance=${request ? 'filled' : 'outlined'}
                    @click=${() => void this.toggleFollow(request?.id)}
                >
                    <!-- This was bookmark-check, which is not in
                         names.txt and so has rendered the missing-icon
                         fallback — a circled question mark — on every
                         followed artist since it was written. A
                         backtick around that name would end this
                         template literal, which is why there is none. -->
                    <wa-icon
                        slot="start"
                        name=${request ? ICON_REQUESTED : ICON_CAN_REQUEST}
                    ></wa-icon>
                    ${request ? 'Following' : 'Follow for new releases'}
                </wa-button>
            </div>
        `;
    }

    private async toggleFollow(requestId: number | undefined): Promise<void> {
        if (!this.artistMBID) return;

        try {
            if (requestId) {
                await downloadStore.removeRequest(requestId);
            } else {
                const libraryId = await libraryStore.getDefaultLibraryId();
                if (!libraryId) {
                    console.error('Could not update the requests list: no library available');
                    return;
                }

                await downloadStore.addRequest({
                    mbid: this.artistMBID,
                    entity: 'artist',
                    libraryId,
                    artist: this.displayName,
                    title: this.displayName,
                    scope: 'future',
                    secondary: false,
                } as never);
            }
        } catch (err) {
            console.error('Could not update the requests list:', err);
        }

        this.requestUpdate();
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

    /** Shared toggle (wide mode) — expands both at once. */
    private toggleTopSection() {
        const next = !(this.topTracksExpanded && this.topReleasesExpanded);
        this.topTracksExpanded = next;
        this.topReleasesExpanded = next;
    }

    /** Per-column toggle (narrow/stacked mode). */
    private toggleTopTracks() {
        this.topTracksExpanded = !this.topTracksExpanded;
    }

    private toggleTopReleases() {
        this.topReleasesExpanded = !this.topReleasesExpanded;
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

        const tracksExpanded = this.topTracksExpanded;
        const releasesExpanded = this.topReleasesExpanded;
        const bothExpanded = tracksExpanded && releasesExpanded;
        const trackLimit = tracksExpanded ? 10 : 5;
        const releaseLimit = releasesExpanded ? 4 : 2;

        const tracks = this.topTracks.slice(0, trackLimit);
        const releases = this.topReleaseGroups.slice(0, releaseLimit);

        const canExpandTracks = this.topTracks.length > 5;
        const canExpandReleases = this.topReleaseGroups.length > 2;
        const canExpand = canExpandTracks || canExpandReleases;

        return html`
            <section>
                <div class="top-section-wrapper">
                    <div class="top-section-columns">
                        ${hasTracks
                            ? html`
                                <div class="top-section-col top-section-col-tracks">
                                    <h3 class="section-header">Top Tracks</h3>
                                    <div class="track-list">
                                        ${tracks.map((t, i) => {
                                            const owned = this.isTrackOwned(t);

                                            return html`
                                                <div
                                                    class=${classMap({
                                                        'track-item': true,
                                                        owned,
                                                        unowned: !owned,
                                                    })}
                                                    tabindex="0"
                                                    role="button"
                                                    aria-disabled=${owned ? 'false' : 'true'}
                                                    aria-label=${ownershipLabel(owned, 'Play', t.trackName, 'track')}
                                                    @dblclick=${() => this.onTrackRowDblClick(t)}
                                                    @contextmenu=${(e: MouseEvent) => this.onTrackContextMenu(e, t)}
                                                    @keydown=${(e: KeyboardEvent) => this.onTrackRowKeydown(e, t)}
                                                >
                                                    <span class="track-rank">${i + 1}</span>
                                                    <div class="track-art">
                                                        ${(() => {
                                                            const art = this.trackThumbnails.get(t.recordingMbid) || '';
                                                            return art
                                                                ? html`<img src="${art}" alt="" loading="lazy"
                                                                       @error=${(e: Event) => {
                                                                           const img = e.target as HTMLImageElement;
                                                                           img.style.display = 'none';
                                                                       }} />`
                                                                : html`<wa-icon name="compact-disc"></wa-icon>`;
                                                        })()}
                                                    </div>
                                                    <div class="track-info">
                                                        <div class="track-title">${trackLink(t.trackName, t.releaseName, t.releaseGroupMbid ?? '', t.recordingMbid)}</div>
                                                        <div class="track-artist">${t.artistName}</div>
                                                    </div>
                                                    <span class="track-listens">
                                                        ${formatListenCount(t.totalListenCount)} plays
                                                    </span>
                                                    ${owned
                                                        ? nothing
                                                        : html`<library-status-indicator
                                                              status=${libraryStatusFor(false, t.recordingMbid)}
                                                              entity-type="track"
                                                              label=${t.trackName}
                                                              request-mbid=${t.recordingMbid}
                                                              request-artist=${t.artistName ?? ''}
                                                          ></library-status-indicator>`}
                                                </div>
                                            `;
                                        })}
                                    </div>
                                    ${canExpandTracks
                                        ? html`
                                            <button
                                                class="column-toggle"
                                                @click=${this.toggleTopTracks}
                                                aria-expanded="${tracksExpanded}"
                                            >
                                                ${tracksExpanded ? 'Show less' : 'Show more'}
                                                <wa-icon name="chevron-down"></wa-icon>
                                            </button>
                                          `
                                        : nothing}
                                </div>
                              `
                            : nothing}
                        ${hasReleases
                            ? html`
                                <div class="top-section-col top-section-col-releases">
                                    <div class="top-releases-content">
                                        <h3 class="section-header">Top Releases</h3>
                                        <div class="top-releases-grid">
                                            <div class="top-releases-inner">
                                                ${releases.map((rg) => this.renderTopReleaseCard(rg))}
                                            </div>
                                        </div>
                                    </div>
                                    ${canExpandReleases
                                        ? html`
                                            <button
                                                class="column-toggle"
                                                @click=${this.toggleTopReleases}
                                                aria-expanded="${releasesExpanded}"
                                            >
                                                ${releasesExpanded ? 'Show less' : 'Show more'}
                                                <wa-icon name="chevron-down"></wa-icon>
                                            </button>
                                          `
                                        : nothing}
                                </div>
                              `
                            : releasesLoading
                              ? html`
                                    <div class="top-section-col top-section-col-releases">
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
                                  aria-expanded="${bothExpanded}"
                              >
                                  ${bothExpanded ? 'Show less' : 'Show more'}
                                  <wa-icon name="chevron-down"></wa-icon>
                              </button>
                          `
                        : nothing}
                </div>
            </section>
        `;
    }

    private renderTopReleaseCard(rg: LBTopReleaseGroup) {
        const artURL = this.thumbnailURLs.get(rg.releaseGroupMbid) || '';
        const target = this.topReleaseTarget(rg);
        const owned = target.owned;
        const badge = albumBadgeFor(
            { localId: target.localId },
            rg.releaseGroupMbid,
        );

        return html`
            <div
                class=${classMap({ 'top-release-card': true, unowned: !owned })}
                aria-label=${ownershipLabel(owned, 'Album', rg.title, 'album')}
                @click=${() => this.navigateToTopRelease(rg)}
                role="button"
                tabindex="0"
                @contextmenu=${(e: MouseEvent) =>
                    this.onReleaseContextMenu(e, target)}
                @keydown=${(e: KeyboardEvent) =>
                    this.onReleaseKeydown(e, target, () =>
                        this.navigateToTopRelease(rg),
                    )}
            >
                <div class="top-release-art">
                    ${artURL
                        ? html`<img
                            src="${artURL}"
                            alt="${rg.title}"
                            loading="lazy"
                            @error=${this.handleImageError}
                        />`
                        : nothing}
                    <div class="album-art-fallback" style="${artURL ? 'display: none' : ''}">
                        <wa-icon name="compact-disc"></wa-icon>
                    </div>
                </div>
                <div class="top-release-text">
                    <div class="top-release-title" title="${rg.title}">
                        ${rg.title}
                    </div>
                    <div class="top-release-meta">
                        <div class="top-release-meta-text">
                            ${rg.date ? html`<span>${extractYear(rg.date)}</span>` : nothing}
                        </div>
                        ${badge.status === 'in-library'
                            ? nothing
                            : html`<library-status-indicator
                                  status=${badge.status}
                                  owned=${badge.owned}
                                  expected=${badge.expected}
                                  entity-type="album"
                                  label=${rg.title}
                                  request-mbid=${rg.releaseGroupMbid}
                                  request-artist=${this.artist?.name ?? ''}
                                  size="18"
                              ></library-status-indicator>`}
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
                        const rowSize = this.discoRowSize;
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
        const artURL = this.thumbnailURLs.get(rg.mbid) || '';
        const year = extractYear(rg.firstReleaseDate);

        const target = this.albumTarget(rg);
        const owned = target.owned;
        const badge = albumBadgeFor({ localId: target.localId }, target.mbid);

        return html`
            <div
                class=${classMap({ 'album-card': true, unowned: !owned })}
                aria-label=${ownershipLabel(owned, 'Album', rg.title, 'album')}
                @click=${() => this.navigateToAlbum(rg)}
                role="button"
                tabindex="0"
                @contextmenu=${(e: MouseEvent) =>
                    this.onReleaseContextMenu(e, target)}
                @keydown=${(e: KeyboardEvent) =>
                    this.onReleaseKeydown(e, target, () =>
                        this.navigateToAlbum(rg),
                    )}
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
                    <div class="album-art-fallback" style="${artURL ? 'display: none' : ''}">
                        <wa-icon name="compact-disc"></wa-icon>
                    </div>
                </div>
                <div class="album-title" title="${rg.title}">${rg.title}</div>
                <div class="album-meta">
                    <div class="album-meta-text">
                        ${year ? html`<span>${year}</span>` : nothing}
                    </div>
                    ${badge.status === 'in-library'
                        ? nothing
                        : html`<library-status-indicator
                              status=${badge.status}
                              owned=${badge.owned}
                              expected=${badge.expected}
                              entity-type="album"
                              label=${rg.title}
                              request-mbid=${rg.mbid}
                              request-artist=${this.artist?.name ?? ''}
                          ></library-status-indicator>`}
                </div>
            </div>
        `;
    }

    /* ── Similar Artists Section ── */

    private renderSimilarArtists() {
        if (this.loadingSimilar || this.similarArtists.length === 0) {
            return nothing;
        }

        // Cap the similar-artists list at 10 to avoid a very long list.
        const maxSimilar = 10;
        const artists = this.similarArtists.slice(0, maxSimilar);
        const showToggle = artists.length > this.discoRowSize;
        const collapsed = !this.similarExpanded && showToggle;
        const visible = collapsed ? artists.slice(0, this.discoRowSize) : artists;

        return html`
            <section>
                <h3 class="section-header">Similar Artists</h3>
                <div class="similar-row ${collapsed ? 'collapsed' : ''}">
                    ${visible.map((a) => {
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
                                    style="background: ${avatarBackground(a.name)}"
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
                                  : `Show all ${artists.length}`}
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

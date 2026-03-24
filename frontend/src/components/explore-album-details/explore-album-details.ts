import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import {
    LookupReleaseGroup,
    BrowseReleases,
} from '@go/explore/Service';
import type {
    MBReleaseGroup,
    MBRelease,
    MBTrack,
} from '@go/explore/Service';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

/* ── Constants ── */
const CAA_GROUP_BASE = 'https://coverartarchive.org/release-group';

/* ── Utility functions (duplicated per Knowledge Pattern #9 — no cross-component imports) ── */

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
}

/* ── Component ── */

@customElement('explore-album-details')
export class ExploreAlbumDetails extends LitElement {
    /* ── Public attributes ── */

    @property({ type: String, attribute: 'release-group-mbid' })
    releaseGroupMBID = '';

    @property({ type: String, attribute: 'album-name' })
    albumName = '';

    /* ── Internal state ── */

    @state() private releaseGroup: MBReleaseGroup | null = null;
    @state() private releases: MBRelease[] = [];
    @state() private loadingInfo = true;
    @state() private loadingReleases = true;
    @state() private errorInfo = '';
    @state() private errorReleases = '';
    @state() private clusteredReleases: ReleaseCluster[] = [];
    @state() private selectedRelease: MBRelease | null = null;
    @state() private minTrackCount = 0;
    @state() private maxTrackCount = 0;

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
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                margin: 0;
                line-height: 1.2;
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
                align-items: center;
                gap: 10px;
                padding: 10px 16px;
                background: var(--yj-surface-1, rgba(255, 255, 255, 0.04));
                border-radius: 8px;
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
        `,
    ];

    /* ── Lifecycle ── */

    override connectedCallback() {
        super.connectedCallback();
        if (this.releaseGroupMBID) {
            void this.loadAllData();
        }
    }

    /* ── Data Loading ── */

    private async loadAllData() {
        const mbid = this.releaseGroupMBID;
        console.log(
            `[explore-album] loading: "${this.albumName}" (${mbid})`,
        );

        const [infoResult, releasesResult] = await Promise.allSettled([
            this.fetchReleaseGroup(mbid),
            this.fetchReleases(mbid),
        ]);

        const summary = [
            `info=${infoResult.status}`,
            `releases=${releasesResult.status}`,
        ].join(', ');
        console.log(
            `[explore-album] loaded: "${this.albumName}" (${summary})`,
        );
    }

    private async fetchReleaseGroup(mbid: string) {
        try {
            this.releaseGroup = await LookupReleaseGroup(mbid);
        } catch (err) {
            const msg = err instanceof Error ? err.message : String(err);
            this.errorInfo = msg;
            console.error(`[explore-album] LookupReleaseGroup error: ${msg}`);
        } finally {
            this.loadingInfo = false;
        }
    }

    private async fetchReleases(mbid: string) {
        try {
            const releases = await BrowseReleases(mbid);
            this.releases = releases ?? [];
            this.buildClusters();
        } catch (err) {
            const msg = err instanceof Error ? err.message : String(err);
            this.errorReleases = msg;
            console.error(`[explore-album] BrowseReleases error: ${msg}`);
        } finally {
            this.loadingReleases = false;
        }
    }

    /* ── Release Clustering (R026) ── */

    /**
     * Groups releases by identical tracklist fingerprints (ordered track MBIDs).
     * Picks the earliest-dated representative per cluster (R028).
     * Computes min/max track counts across all releases (R027).
     */
    private buildClusters() {
        const clusterMap = new Map<string, MBRelease[]>();

        // Compute track counts across all releases for R027
        let minCount = Infinity;
        let maxCount = 0;

        for (const release of this.releases) {
            const tracks = release.tracks ?? [];
            const trackCount = tracks.length;
            if (trackCount < minCount) minCount = trackCount;
            if (trackCount > maxCount) maxCount = trackCount;

            // Fingerprint: sort tracks by (discNumber, position), join MBIDs
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

        if (this.releases.length === 0) {
            minCount = 0;
        }

        this.minTrackCount = minCount;
        this.maxTrackCount = maxCount;

        // Build clusters with earliest-dated representative per group
        const clusters: ReleaseCluster[] = [];
        for (const [fingerprint, releases] of clusterMap) {
            // Sort by date ascending — earliest first (ISO date strings sort lexicographically)
            const sorted = [...releases].sort((a, b) => {
                const da = a.date || '\uffff'; // releases without dates sort last
                const db = b.date || '\uffff';
                return da.localeCompare(db);
            });
            clusters.push({
                representative: sorted[0],
                allReleases: sorted,
                fingerprint,
            });
        }

        // Sort clusters themselves by their representative's date (earliest first)
        clusters.sort((a, b) => {
            const da = a.representative.date || '\uffff';
            const db = b.representative.date || '\uffff';
            return da.localeCompare(db);
        });

        this.clusteredReleases = clusters;

        // Default to earliest-dated representative overall (R028)
        if (clusters.length > 0) {
            this.selectedRelease = clusters[0].representative;
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
        const mbid = select.value;
        for (const cluster of this.clusteredReleases) {
            if (cluster.representative.mbid === mbid) {
                this.selectedRelease = cluster.representative;
                return;
            }
        }
    }

    /**
     * Build a human-readable label for a release in the version selector dropdown.
     * Includes title, country, date, and optionally track count if editions differ (R027).
     */
    private releaseLabel(release: MBRelease): string {
        const parts: string[] = [release.title];
        if (release.country) parts.push(`(${release.country})`);
        if (release.date) parts.push(`— ${release.date}`);
        if (this.minTrackCount !== this.maxTrackCount) {
            const trackCount = (release.tracks ?? []).length;
            parts.push(`(${trackCount} tracks)`);
        }
        return parts.join(' ');
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
                ${this.renderVersionSelector()}
                ${this.renderTracklist()}
            </div>
        `;
    }

    private renderHeader() {
        const artURL = CoverArtGroupURL(this.releaseGroupMBID);

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
                    <img
                        src="${artURL}"
                        alt="${this.albumName}"
                        @error=${this.handleCoverError}
                    />
                    <div class="cover-art-fallback" style="display: none">
                        <wa-icon name="compact-disc"></wa-icon>
                    </div>
                </div>
                <div class="album-info">
                    <h1 class="album-title" title="${this.albumName}">
                        ${this.albumName}
                    </h1>
                    ${this.renderAlbumMeta()}
                </div>
            </div>
        `;
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
        const year = extractYear(rg.firstReleaseDate);
        const type = rg.primaryType || '';

        const metaParts: string[] = [];
        if (type) metaParts.push(type);
        if (year) metaParts.push(year);

        return html`
            ${artist
                ? html`<div class="album-artist">${artist}</div>`
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

        // Only show the selector when there are multiple distinct editions
        if (this.clusteredReleases.length <= 1) return nothing;

        return html`
            <div class="version-selector">
                <label for="version-select">Version</label>
                <select
                    id="version-select"
                    @change=${this.handleVersionChange}
                    aria-label="Select release version"
                >
                    ${this.clusteredReleases.map(
                        (cluster) => html`
                            <option
                                value="${cluster.representative.mbid}"
                                ?selected=${this.selectedRelease?.mbid ===
                                cluster.representative.mbid}
                            >
                                ${this.releaseLabel(cluster.representative)}
                            </option>
                        `,
                    )}
                </select>
            </div>
        `;
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
        if (!this.selectedRelease) {
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

        const tracks = this.selectedRelease.tracks ?? [];
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
                            ${discTracks.map(
                                (track) => html`
                                    <div class="track-row">
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

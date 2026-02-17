import { LitElement, html, css } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import type { library } from '@go/models';
import { PlayerController } from '@store/controllers/player-controller';
import { formatMilliseconds } from '@utils/time';

/** Detail payload for the track-click custom event. */
export interface TrackClickDetail {
    track: library.Track;
    index: number;
    ctrlKey: boolean;
    shiftKey: boolean;
    metaKey: boolean;
}

/** Detail payload for the track-dblclick custom event. */
export interface TrackDblClickDetail {
    track: library.Track;
    index: number;
}

/** Detail payload for the track-contextmenu custom event. */
export interface TrackContextMenuDetail {
    track: library.Track;
    clientX: number;
    clientY: number;
}

/**
 * Self-contained dropdown that renders an album's track list.
 *
 * Owns a PlayerController so that active-track highlighting
 * only re-renders this component, not the parent grid.
 */
@customElement('album-dropdown')
export class AlbumDropdown extends LitElement {
    private player = new PlayerController(this);

    @property({ attribute: false })
    tracks: library.Track[] = [];

    @property({ type: Boolean, attribute: 'loading-tracks' })
    loadingTracks = false;

    @property({ attribute: false })
    selectedTracks: Set<string> = new Set();

    static override styles = css`
        :host {
            display: block;
            grid-column: 1 / -1;
        }

        .album-dropdown {
            background-color: #1a1a2e;
            border-top: 2px solid #ffd43b;
            border-bottom: 2px solid #ffd43b;
            border-radius: 4px;
            padding: 12px 16px;
            box-sizing: border-box;
            min-height: 230px;
        }

        .dropdown-loading {
            display: flex;
            align-items: center;
            justify-content: center;
            height: 206px;
            color: #b3b3b3;
            font-size: 13px;
        }

        .dropdown-tracks {
            column-count: 3;
            column-fill: auto;
            column-gap: 24px;
            height: 206px;
        }

        .dropdown-tracks.overflow {
            height: auto;
            min-height: 206px;
        }

        .track-row {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 4px 8px;
            border-radius: 4px;
            cursor: default;
            user-select: none;
            font-size: 12px;
            break-inside: avoid;
        }

        .track-row:hover {
            background-color: rgba(
                255,
                255,
                255,
                0.05
            );
        }

        .track-row.selected {
            background-color: rgba(
                100,
                160,
                255,
                0.15
            );
        }

        .track-row.active {
            background-color: rgba(
                255,
                212,
                59,
                0.1
            );
            color: #ffd43b;
        }

        .track-row.selected.active {
            background-color: rgba(
                100,
                160,
                255,
                0.15
            );
        }

        .track-number {
            color: #888;
            min-width: 22px;
            text-align: right;
            flex-shrink: 0;
        }

        .track-title {
            color: #fff;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            flex: 1;
            min-width: 0;
        }

        .track-duration {
            color: #888;
            flex-shrink: 0;
            margin-left: auto;
        }
    `;

    /* ================================================================
     * Rendering helpers
     * ================================================================ */

    private isActiveTrack(
        track: library.Track,
    ): boolean {
        const currentTrack = this.player.currentTrack;

        if (!currentTrack) return false;

        return currentTrack.filePath === track.FilePath;
    }

    /**
     * Determine whether the multi-column track layout
     * overflows 3 columns at the base height.
     *
     * Heuristic: each track row is ~28px tall, the base
     * dropdown content height is 206px, each column fits
     * ~7 tracks, and with 3 columns that is ~21 tracks.
     */
    private tracksOverflow(): boolean {
        const rowHeight = 28;
        const containerHeight = 206;
        const perColumn = Math.floor(
            containerHeight / rowHeight,
        );
        const maxTracks = perColumn * 3;

        return this.tracks.length > maxTracks;
    }

    /* ================================================================
     * Event dispatching
     * ================================================================ */

    private onTrackClick(
        e: MouseEvent,
        track: library.Track,
        index: number,
    ) {
        e.stopPropagation();

        this.dispatchEvent(
            new CustomEvent<TrackClickDetail>(
                'track-click',
                {
                    bubbles: true,
                    composed: true,
                    detail: {
                        track,
                        index,
                        ctrlKey: e.ctrlKey,
                        shiftKey: e.shiftKey,
                        metaKey: e.metaKey,
                    },
                },
            ),
        );
    }

    private onTrackDblClick(
        e: MouseEvent,
        track: library.Track,
        index: number,
    ) {
        e.stopPropagation();

        this.dispatchEvent(
            new CustomEvent<TrackDblClickDetail>(
                'track-dblclick',
                {
                    bubbles: true,
                    composed: true,
                    detail: { track, index },
                },
            ),
        );
    }

    private onTrackContextMenu(
        e: MouseEvent,
        track: library.Track,
    ) {
        e.preventDefault();
        e.stopPropagation();

        this.dispatchEvent(
            new CustomEvent<TrackContextMenuDetail>(
                'track-contextmenu',
                {
                    bubbles: true,
                    composed: true,
                    detail: {
                        track,
                        clientX: e.clientX,
                        clientY: e.clientY,
                    },
                },
            ),
        );
    }

    /* ================================================================
     * Render
     * ================================================================ */

    private renderTrackRow(
        track: library.Track,
        index: number,
    ) {
        const active = this.isActiveTrack(track);
        const selected = this.selectedTracks.has(
            track.FilePath,
        );

        const classes = [
            'track-row',
            active ? 'active' : '',
            selected ? 'selected' : '',
        ]
            .filter(Boolean)
            .join(' ');

        const displayNumber =
            track.TrackNumber > 0
                ? track.TrackNumber
                : index + 1;

        return html`
            <div
                class=${classes}
                @click=${(e: MouseEvent) =>
                    this.onTrackClick(e, track, index)}
                @dblclick=${(e: MouseEvent) =>
                    this.onTrackDblClick(
                        e,
                        track,
                        index,
                    )}
                @contextmenu=${(e: MouseEvent) =>
                    this.onTrackContextMenu(e, track)}
            >
                <span class="track-number">
                    ${displayNumber}
                </span>
                <span
                    class="track-title"
                    title="${track.TrackName}"
                >
                    ${track.TrackName}
                </span>
                <span class="track-duration">
                    ${formatMilliseconds(
                        track.TrackLength,
                    )}
                </span>
            </div>
        `;
    }

    override render() {
        if (this.loadingTracks) {
            return html`
                <div class="album-dropdown">
                    <div class="dropdown-loading">
                        Loading tracks...
                    </div>
                </div>
            `;
        }

        const overflow = this.tracksOverflow();

        const tracksClass = overflow
            ? 'dropdown-tracks overflow'
            : 'dropdown-tracks';

        return html`
            <div class="album-dropdown">
                <div class=${tracksClass}>
                    ${this.tracks.map(
                        (track, i) =>
                            this.renderTrackRow(track, i),
                    )}
                </div>
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'album-dropdown': AlbumDropdown;
    }
}

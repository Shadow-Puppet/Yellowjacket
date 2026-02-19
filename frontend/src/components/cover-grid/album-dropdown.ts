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

/** Detail payload for the track-dragstart custom event. */
export interface TrackDragStartDetail {
    track: library.Track;
    index: number;
    dataTransfer: DataTransfer | null;
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

    @property({ attribute: false })
    selectedTracks: Set<string> = new Set();

    /** Width of the grid container in pixels (passed from parent). */
    @property({ type: Number })
    containerWidth = 800;

    static override styles = css`
        :host {
            display: block;
        }

        .album-dropdown {
            background-color: #212529;
            border: 2px solid #ffd43b;
            border-radius: 4px;
            padding: 12px 16px;
            box-sizing: border-box;
        }

        .dropdown-tracks {
            column-fill: auto;
            column-gap: 24px;
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
            line-height: 16px;
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
     * Layout helpers
     * ================================================================ */

    /**
     * Derive the number of track-list columns from the
     * grid container width.
     */
    get columnCount(): number {
        const w = this.containerWidth;

        if (w < 500) return 1;
        if (w < 800) return 2;
        if (w < 1200) return 3;

        return 4;
    }

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

    /** Height of each track row: 16px line-height + 4+4px padding. */
    private static readonly TRACK_ROW_HEIGHT = 24;

    /**
     * Height of the inner .dropdown-tracks container.
     * Sized so that column-fill:auto fills each column
     * completely before moving to the next.
     */
    private get tracksHeight(): number {
        const cols = this.columnCount;
        const rowsPerCol = Math.ceil(
            this.tracks.length / cols,
        );

        return (
            rowsPerCol * AlbumDropdown.TRACK_ROW_HEIGHT
        );
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

    private onTrackDragStart(
        e: DragEvent,
        track: library.Track,
        index: number,
    ) {
        // Delegate to the parent cover-grid which
        // owns the selection state and drag-image.
        this.dispatchEvent(
            new CustomEvent<TrackDragStartDetail>(
                'track-dragstart',
                {
                    bubbles: true,
                    composed: true,
                    detail: {
                        track,
                        index,
                        dataTransfer: e.dataTransfer,
                    },
                },
            ),
        );
    }

    private onTrackDragEnd() {
        this.dispatchEvent(
            new CustomEvent('track-dragend', {
                bubbles: true,
                composed: true,
            }),
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
                draggable=${selected ? 'true' : 'false'}
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
                @dragstart=${(e: DragEvent) =>
                    this.onTrackDragStart(
                        e,
                        track,
                        index,
                    )}
                @dragend=${() => this.onTrackDragEnd()}
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
        return html`
            <div class="album-dropdown">
                <div
                    class="dropdown-tracks"
                    style="height:${this.tracksHeight}px;column-count:${this.columnCount}"
                >
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

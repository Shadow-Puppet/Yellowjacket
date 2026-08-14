import { LitElement, html, svg, css } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import { classMap } from 'lit/directives/class-map.js';
import type * as library from '@go/library/models.js';
import { PlayerController } from '@store/controllers/player-controller';
import { FavoritesController } from '@store/controllers/favorites-controller';
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

// Inline SVG paths for favorite icons — eliminates wa-icon shadow DOM
// overhead per visible track row.  Font Awesome 6 paths.
const FAV_ICONS = {
    heart: {
        viewBox: '0 0 512 512',
        regular: 'M225.8 468.2l-2.5-2.3L48.1 303.2C17.4 274.7 0 234.7 0 192.8v-3.3c0-70.4 50-130.8 119.2-144C158.6 37.9 198.9 47 231 69.6c9 6.3 17.3 13.5 25 21.5c7.7-8 16-15.2 25-21.5c32.1-22.6 72.4-31.7 111.8-24.2C461.5 59.6 512 124.2 512 192.8v3.3c0 41.9-17.4 81.9-48.1 110.4L288.7 465.9l-2.5 2.3c-8.2 7.6-19 11.9-30.2 11.9s-22-4.2-30.2-11.9z',
        solid: 'M47.6 300.4L228.3 469.1c7.5 7 17.4 10.9 27.7 10.9s20.2-3.9 27.7-10.9L464.4 300.4c30.4-28.3 47.6-68 47.6-109.5v-5.8c0-69.9-50.5-129.5-119.4-141C347 36.5 300.6 51.4 268 84L256 96 244 84c-32.6-32.6-79-47.5-124.6-39.9C50.5 55.6 0 115.2 0 185.1v5.8c0 41.5 17.2 81.2 47.6 109.5z',
    },
    star: {
        viewBox: '0 0 576 512',
        regular: 'M287.9 0c9.2 0 17.6 5.2 21.6 13.5l68.6 141.3 153.2 22.6c9 1.3 16.5 7.6 19.3 16.3s.5 18.1-5.9 24.5L434.8 326.7l26.2 155.6c1.5 9-2.2 18.1-9.7 23.5s-17.3 6-25.3 1.7L288 439.6 149.7 507.5c-8 4.3-17.8 3.7-25.3-1.7s-11.2-14.5-9.7-23.5l26.2-155.6L31.1 218.2c-6.5-6.4-8.7-15.9-5.9-24.5s10.3-14.9 19.3-16.3l153.2-22.6L266.3 13.5C270.4 5.2 278.7 0 287.9 0z',
        solid: 'M316.9 18C311.6 7 300.4 0 288.1 0s-23.4 7-28.8 18L195 150.3 51.4 171.5c-12 1.8-22 10.2-25.7 21.7s-.7 24.2 7.9 32.7L137.8 329 108.4 474.7c-2 12 3 24.2 12.9 31.3s23 8 33.8 2.3L288.1 439.8 420.9 508.3c10.8 5.7 23.9 4.9 33.8-2.3s14.9-19.3 12.9-31.3L437.7 329 542 225.9c8.6-8.4 11.7-21.2 7.9-32.7s-13.7-19.9-25.7-21.7L380.7 150.3 316.9 18z',
    },
} as const;

/**
 * Self-contained dropdown that renders an album's track list.
 *
 * Owns a PlayerController so that active-track highlighting
 * only re-renders this component, not the parent grid.
 */
@customElement('album-dropdown')
export class AlbumDropdown extends LitElement {
    private player = new PlayerController(this);
    private favCtrl = new FavoritesController(this);

    @property({ attribute: false })
    tracks: library.Track[] = [];

    @property({ attribute: false })
    selectedTracks: Set<string> = new Set();

    /** Width of the grid container in pixels (passed from parent). */
    @property({ type: Number })
    containerWidth = 800;

    /** Width of the album row in pixels (cards + gaps, no outer padding). */
    @property({ type: Number })
    gridRowWidth = 800;

    /** Horizontal offset of the carat from the dropdown's left edge. */
    @property({ type: Number })
    caratOffset = 0;

    static override styles = css`
        :host {
            display: block;
            margin-top: 14px;
        }

        .album-dropdown {
            background-color: var(--yj-bg-elevated, #343a40);
            border-radius: 0 0 4px 4px;
            padding: 12px 16px;
            box-sizing: border-box;
            position: relative;
        }

        .carat {
            position: absolute;
            top: -10px;
            width: 0;
            height: 0;
            border-left: 11px solid transparent;
            border-right: 11px solid transparent;
            border-bottom: 10px solid var(--yj-bg-elevated, #343a40);
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
            background-color: var(
                --yj-hover-overlay,
                rgba(255, 255, 255, 0.05)
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
            background-color: var(
                --yj-accent-bg,
                rgba(255, 212, 59, 0.1)
            );
            color: var(--yj-accent-text, #ffd43b);
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
            color: var(--yj-text-tertiary, #888);
            min-width: 22px;
            text-align: right;
            flex-shrink: 0;
        }

        .track-title {
            color: var(--yj-text-primary, #fff);
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            flex: 1;
            min-width: 0;
        }

        .track-duration {
            color: var(--yj-text-tertiary, #888);
            flex-shrink: 0;
            margin-left: auto;
        }

        .fav-icon {
            display: flex;
            align-items: center;
            justify-content: center;
            width: 18px;
            flex-shrink: 0;
            cursor: pointer;
            color: var(--yj-text-tertiary, #666);
            font-size: 11px;
            transition: color 0.1s ease;
        }
        .fav-icon:hover {
            color: var(--yj-text-primary, #fff);
        }
        .fav-icon.favorited {
            color: var(--yj-accent-text, #ffd43b);
        }
        .fav-icon.favorited:hover {
            color: var(--yj-accent-text, #ffd43b);
            opacity: 0.8;
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
        const isFav = this.favCtrl.isFavorited(track.FilePath);
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
                draggable="true"
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
                <div
                    class=${classMap({ 'fav-icon': true, favorited: isFav })}
                    @click=${(e: MouseEvent) => {
                        e.stopPropagation();
                        void this.favCtrl.toggleFavorite(track.FilePath);
                    }}
                >
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${FAV_ICONS[this.favCtrl.iconStyle === 'star' ? 'star' : 'heart'].viewBox.split(' ').slice(2).join(' ')}" width="14" height="14">
                        ${svg`<path fill="currentColor" d="${FAV_ICONS[this.favCtrl.iconStyle === 'star' ? 'star' : 'heart'][isFav ? 'solid' : 'regular']}"/>`}
                    </svg>
                </div>
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
            <div
                class="album-dropdown"
                style="width:${this.gridRowWidth}px"
            >
                <div
                    class="carat"
                    style="left:${this.caratOffset}px;transform:translateX(-50%)"
                ></div>
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

import { LitElement, html, css, nothing } from 'lit';
import {
    customElement,
    state,
    query,
} from 'lit/decorators.js';
import type { library } from '@go/models';
import {
    formatSampleRate,
    formatBitDepth,
    formatChannels,
    formatBitrate,
    formatFileSize,
} from '@utils/format';
import { formatMilliseconds } from '@utils/time';

import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

/** Cover art URLs resolved from the album cache. */
export interface CoverArtUrls {
    coverArtPath: string;
    coverArtSmall: string;
    coverArtMedium: string;
    coverArtLarge: string;
}

/** Editable field definition. */
interface MetadataField {
    key: string;
    label: string;
    value: string;
    editable: boolean;
    type: 'text' | 'number';
}

/**
 * Modal dialog displaying detailed metadata for a single track.
 *
 * Call `show(track, coverArt?)` to open and `close()` to dismiss.
 * Includes an edit toggle for future tag-writing support.
 */
@customElement('track-details')
export class TrackDetails extends LitElement {
    @state() private track: library.Track | null = null;
    @state() private coverArt: CoverArtUrls | null = null;
    @state() private editing = false;
    @state() private editValues: Record<string, string> = {};

    @query('wa-dialog')
    private dialog!: HTMLElement & {
        show: () => void;
        hide: () => void;
    };

    // =================================================================
    // PUBLIC API
    // =================================================================

    /** Open the dialog for the given track. */
    show(
        track: library.Track,
        coverArt?: CoverArtUrls,
    ): void {
        this.track = track;
        this.coverArt = coverArt ?? null;
        this.editing = false;
        this.editValues = {};

        this.updateComplete.then(() => {
            this.dialog?.show();
        });
    }

    /** Close the dialog. */
    close(): void {
        this.dialog?.hide();
        this.editing = false;
        this.editValues = {};
    }

    // =================================================================
    // STYLES
    // =================================================================

    static override styles = css`
        wa-dialog {
            --width: 640px;
        }

        wa-dialog::part(dialog) {
            background: var(--yj-bg-surface, #212529);
            color: var(--yj-text-primary, #fff);
            border: 1px solid var(--yj-border, #444);
            border-radius: 8px;
        }

        wa-dialog::part(title) {
            font-size: 16px;
            font-weight: 600;
            color: var(--yj-text-primary, #fff);
            padding: 16px 20px 8px;
        }

        wa-dialog::part(header-actions) {
            padding: 16px 20px 8px;
        }

        wa-dialog::part(close-button__base) {
            color: var(--yj-text-tertiary, #888);
        }

        wa-dialog::part(body) {
            padding: 0 20px 20px;
        }

        .top-section {
            display: flex;
            gap: 20px;
            margin-bottom: 20px;
        }

        .cover-art {
            width: 200px;
            height: 200px;
            flex-shrink: 0;
            border-radius: 6px;
            overflow: hidden;
        }

        .cover-art img {
            width: 100%;
            height: 100%;
            object-fit: cover;
        }

        .cover-placeholder {
            width: 100%;
            height: 100%;
            background-color: var(
                --yj-bg-elevated,
                #343a40
            );
            display: flex;
            align-items: center;
            justify-content: center;
        }

        .cover-placeholder wa-icon {
            color: var(--yj-text-tertiary, #888);
            font-size: 64px;
        }

        .main-meta {
            display: flex;
            flex-direction: column;
            gap: 8px;
            min-width: 0;
            flex: 1;
            justify-content: center;
        }

        .main-meta .title {
            font-size: 22px;
            font-weight: 600;
            color: var(--yj-text-primary, #fff);
            word-break: break-word;
        }

        .main-meta .artist {
            font-size: 15px;
            color: var(--yj-text-secondary, #b3b3b3);
        }

        .main-meta .album {
            font-size: 14px;
            color: var(--yj-text-tertiary, #888);
        }

        .main-meta .duration {
            font-size: 13px;
            color: var(--yj-text-tertiary, #888);
            font-variant-numeric: tabular-nums;
        }

        .divider {
            height: 1px;
            background: var(--yj-border-subtle, #333);
            margin-bottom: 16px;
        }

        .section-label {
            font-size: 11px;
            font-weight: 600;
            color: var(--yj-text-tertiary, #888);
            text-transform: uppercase;
            letter-spacing: 0.8px;
            margin-bottom: 10px;
        }

        .metadata-grid {
            display: grid;
            grid-template-columns: 120px 1fr;
            gap: 8px 12px;
            align-items: baseline;
        }

        .meta-label {
            font-size: 12px;
            font-weight: 500;
            color: var(--yj-text-tertiary, #888);
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }

        .meta-value {
            font-size: 13px;
            color: var(--yj-text-secondary, #b3b3b3);
            word-break: break-word;
        }

        .meta-value.empty {
            color: var(--yj-text-tertiary, #888);
            font-style: italic;
        }

        /* Edit mode inputs */
        .meta-input {
            width: 100%;
            box-sizing: border-box;
            background: var(--yj-bg-elevated, #343a40);
            border: 1px solid
                var(--yj-border-subtle, #333);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            font-size: 13px;
            padding: 4px 8px;
            font-family: inherit;
        }

        .meta-input:focus {
            outline: none;
            border-color: var(--yj-accent, #ffd43b);
        }

        .main-input {
            background: var(--yj-bg-elevated, #343a40);
            border: 1px solid
                var(--yj-border-subtle, #333);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            font-family: inherit;
            padding: 4px 8px;
            width: 100%;
            box-sizing: border-box;
        }

        .main-input:focus {
            outline: none;
            border-color: var(--yj-accent, #ffd43b);
        }

        .main-input.title-input {
            font-size: 20px;
            font-weight: 600;
        }

        .main-input.artist-input {
            font-size: 14px;
        }

        .main-input.album-input {
            font-size: 13px;
        }

        /* Action bar */
        .action-bar {
            display: flex;
            justify-content: flex-end;
            gap: 8px;
            margin-top: 16px;
        }

        .btn {
            padding: 6px 16px;
            border-radius: 4px;
            border: 1px solid var(--yj-border, #444);
            background: var(
                --yj-bg-elevated,
                #343a40
            );
            color: var(--yj-text-primary, #fff);
            font-size: 13px;
            cursor: pointer;
            font-family: inherit;
            transition: background-color 0.15s ease;
        }

        .btn:hover {
            background: var(--yj-bg-overlay, #495057);
        }

        .btn-primary {
            background: var(--yj-accent, #ffd43b);
            color: #000;
            border-color: var(--yj-accent, #ffd43b);
        }

        .btn-primary:hover {
            background: var(
                --yj-accent-hover,
                #ffe066
            );
            border-color: var(
                --yj-accent-hover,
                #ffe066
            );
        }
    `;

    // =================================================================
    // RENDER
    // =================================================================

    override render() {
        return html`
            <wa-dialog label="Track Details">
                ${this.track
                    ? this.renderContent()
                    : nothing}
            </wa-dialog>
        `;
    }

    private renderContent() {
        const t = this.track!;

        return html`
            <div class="top-section">
                ${this.renderCoverArt()}
                <div class="main-meta">
                    ${this.renderMainFields(t)}
                </div>
            </div>
            <div class="divider"></div>
            <div class="metadata-grid">
                ${this.renderDetailFields(t)}
            </div>
            <div class="divider"></div>
            <div class="section-label">
                Audio Properties
            </div>
            <div class="metadata-grid">
                ${this.renderAudioProperties(t)}
            </div>
            <div class="action-bar">
                ${this.renderActions()}
            </div>
        `;
    }

    private renderCoverArt() {
        const src =
            this.coverArt?.coverArtLarge ??
            this.coverArt?.coverArtMedium ??
            this.coverArt?.coverArtPath;

        if (!src) {
            return html`
                <div class="cover-art">
                    <div class="cover-placeholder">
                        <wa-icon
                            name="music"
                        ></wa-icon>
                    </div>
                </div>
            `;
        }

        return html`
            <div class="cover-art">
                <img
                    src="${src}"
                    alt="Album cover"
                    @error=${this.handleImageError}
                />
            </div>
        `;
    }

    private handleImageError = (e: Event) => {
        const img = e.target as HTMLImageElement;
        const fallback = this.coverArt?.coverArtPath;

        if (fallback && img.src !== fallback) {
            img.src = fallback;

            return;
        }

        const container = img.parentElement;

        if (container) {
            container.innerHTML =
                '<div class="cover-placeholder">' +
                '<wa-icon name="music"></wa-icon>' +
                '</div>';
        }
    };

    private renderMainFields(t: library.Track) {
        if (this.editing) {
            return html`
                <input
                    class="main-input title-input"
                    .value=${this.getEditValue(
                        'title',
                        t.TrackName,
                    )}
                    @input=${(e: Event) =>
                        this.onEditInput(
                            'title',
                            e,
                        )}
                    placeholder="Title"
                />
                <input
                    class="main-input artist-input"
                    .value=${this.getEditValue(
                        'artist',
                        t.ArtistName,
                    )}
                    @input=${(e: Event) =>
                        this.onEditInput(
                            'artist',
                            e,
                        )}
                    placeholder="Artist"
                />
                <input
                    class="main-input album-input"
                    .value=${this.getEditValue(
                        'album',
                        t.Album,
                    )}
                    @input=${(e: Event) =>
                        this.onEditInput(
                            'album',
                            e,
                        )}
                    placeholder="Album"
                />
                <span class="duration">
                    ${formatMilliseconds(t.TrackLength)}
                </span>
            `;
        }

        return html`
            <span class="title">
                ${t.TrackName || this.fileNameFromPath(t.FilePath)}
            </span>
            <span class="artist">
                ${t.ArtistName || 'Unknown Artist'}
            </span>
            ${t.Album
                ? html`<span class="album"
                      >${t.Album}</span
                  >`
                : nothing}
            <span class="duration">
                ${formatMilliseconds(t.TrackLength)}
            </span>
        `;
    }

    private renderDetailFields(t: library.Track) {
        const fields: MetadataField[] = [
            {
                key: 'genre',
                label: 'Genre',
                value: (t.Genre ?? []).join(', '),
                editable: true,
                type: 'text',
            },
            {
                key: 'year',
                label: 'Year',
                value: t.Year ? String(t.Year) : '',
                editable: true,
                type: 'number',
            },
            {
                key: 'composer',
                label: 'Composer',
                value: t.Composer ?? '',
                editable: true,
                type: 'text',
            },
            {
                key: 'trackNumber',
                label: 'Track #',
                value: t.TrackNumber
                    ? String(t.TrackNumber)
                    : '',
                editable: true,
                type: 'number',
            },
            {
                key: 'discNumber',
                label: 'Disc #',
                value: t.DiscNumber
                    ? String(t.DiscNumber)
                    : '',
                editable: true,
                type: 'number',
            },
            {
                key: 'fileType',
                label: 'File Type',
                value: t.FileType ?? '',
                editable: false,
                type: 'text',
            },
            {
                key: 'filePath',
                label: 'File Path',
                value: t.FilePath ?? '',
                editable: false,
                type: 'text',
            },
        ];

        return fields.map((f) => this.renderField(f));
    }

    private renderAudioProperties(t: library.Track) {
        const props: { label: string; value: string }[] = [
            {
                label: 'Sample Rate',
                value: formatSampleRate(t.SampleRate),
            },
            {
                label: 'Bit Depth',
                value: formatBitDepth(t.BitDepth),
            },
            {
                label: 'Channels',
                value: formatChannels(t.Channels),
            },
            {
                label: 'Bitrate',
                value: formatBitrate(t.Bitrate),
            },
            {
                label: 'File Size',
                value: formatFileSize(t.FileSize),
            },
        ];

        return props.map(
            (p) => html`
                <span class="meta-label">${p.label}</span>
                <span
                    class="meta-value ${p.value ===
                    '\u2014'
                        ? 'empty'
                        : ''}"
                >
                    ${p.value}
                </span>
            `,
        );
    }

    private renderField(f: MetadataField) {
        const display =
            this.getEditValue(f.key, f.value) || f.value;

        return html`
            <span class="meta-label">${f.label}</span>
            ${this.editing && f.editable
                ? html`
                      <input
                          class="meta-input"
                          type=${f.type}
                          .value=${this.getEditValue(
                              f.key,
                              f.value,
                          )}
                          @input=${(e: Event) =>
                              this.onEditInput(
                                  f.key,
                                  e,
                              )}
                      />
                  `
                : html`
                      <span
                          class="meta-value ${display
                              ? ''
                              : 'empty'}"
                      >
                          ${display || 'None'}
                      </span>
                  `}
        `;
    }

    private renderActions() {
        if (this.editing) {
            return html`
                <button
                    class="btn"
                    @click=${this.cancelEdit}
                >
                    Cancel
                </button>
                <button
                    class="btn btn-primary"
                    @click=${this.saveEdit}
                >
                    Save
                </button>
            `;
        }

        return html`
            <button
                class="btn"
                @click=${this.startEdit}
            >
                <wa-icon name="pen-to-square"></wa-icon>
                Edit
            </button>
        `;
    }

    // =================================================================
    // EDIT LOGIC
    // =================================================================

    private startEdit = () => {
        this.editing = true;
        this.editValues = {};
    };

    private cancelEdit = () => {
        this.editing = false;
        this.editValues = {};
    };

    private saveEdit = () => {
        // TODO: implement tag writing when backend support is added.
        // For now, just exit edit mode.
        this.editing = false;
        this.editValues = {};
    };

    private getEditValue(
        key: string,
        fallback: string,
    ): string {
        return key in this.editValues
            ? this.editValues[key]!
            : fallback;
    }

    private onEditInput(key: string, e: Event) {
        const input = e.target as HTMLInputElement;

        this.editValues = {
            ...this.editValues,
            [key]: input.value,
        };
    }

    // =================================================================
    // HELPERS
    // =================================================================

    private fileNameFromPath(filePath: string): string {
        const parts = filePath.split(/[\\/]/);
        const filename =
            parts[parts.length - 1] ?? filePath;

        return filename.replace(/\.[^.]+$/, '');
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'track-details': TrackDetails;
    }
}

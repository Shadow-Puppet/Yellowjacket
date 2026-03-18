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
import { WriteTrackTagsByPath } from '@go/tagwriter/TagWriter';
import {
    BatchWriteTrackTags,
    CancelBatchWrite,
} from '@go/tagwriter/TagWriter';
import { ImageFilePicker, ReadFile } from '@go/frontendutil/FrontendUtil';
import { libraryStore } from '../../store/library-store';
import { EventsOn, EventsOff } from '@runtime/runtime';
import { Events } from '../../events';

import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';

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
    mixed?: boolean;
}

/** Human-readable label map for fields shown in confirmations. */
const FIELD_LABELS: Record<string, string> = {
    title: 'Title',
    artist: 'Artist',
    album: 'Album',
    genre: 'Genre',
    year: 'Year',
    composer: 'Composer',
    trackNumber: 'Track #',
    discNumber: 'Disc #',
};

/**
 * Modal dialog displaying detailed metadata for a single track
 * or a batch of selected tracks.
 *
 * Call `show(track, coverArt?)` for single-track mode.
 * Call `showBatch(tracks, coverArt, coverArtMixed)` for batch mode.
 * Call `close()` to dismiss.
 */
@customElement('track-details')
export class TrackDetails extends LitElement {
    // -- Single-track state --
    @state() private track: library.Track | null = null;
    @state() private coverArt: CoverArtUrls | null = null;

    // -- Shared edit state --
    @state() private editing = false;
    @state() private editValues: Record<string, string> = {};
    @state() private saving = false;
    @state() private errorMessage = '';
    @state() private pendingCoverArt: {
        data: ArrayBuffer;
        previewUrl: string;
    } | null = null;
    @state() private clearCoverArt = false;

    // -- Batch-specific state --
    @state() private batchMode = false;
    @state() private batchTracks: library.Track[] = [];
    @state() private batchFilePaths: string[] = [];
    @state() private batchCoverArtMixed = false;
    @state() private batchProgress: {
        current: number;
        total: number;
    } | null = null;
    @state() private batchResult: {
        succeeded: number;
        failed: number;
        cancelled: boolean;
        failures: Array<{ filePath: string; error: string }>;
    } | null = null;
    @state() private showConfirmation = false;

    @query('wa-dialog')
    private dialog!: HTMLElement & { open: boolean };

    // =================================================================
    // PUBLIC API
    // =================================================================

    /** Open the dialog for a single track. */
    show(
        track: library.Track,
        coverArt?: CoverArtUrls,
    ): void {
        this.track = track;
        this.coverArt = coverArt ?? null;
        this.batchMode = false;
        this.editing = false;
        this.editValues = {};
        this.errorMessage = '';
        this.cleanupPendingCoverArt();
        this.resetBatchState();

        this.updateComplete.then(() => {
            if (this.dialog) this.dialog.open = true;
        });
    }

    /** Open the dialog for batch editing multiple tracks. */
    showBatch(
        tracks: library.Track[],
        coverArt: CoverArtUrls | null,
        coverArtMixed: boolean,
    ): void {
        this.batchMode = true;
        this.batchTracks = tracks;
        this.batchFilePaths = tracks.map(
            (t) => t.FilePath,
        );
        this.batchCoverArtMixed = coverArtMixed;
        this.coverArt = coverArt;

        // Clear single-track state.
        this.track = null;

        // Reset edit state.
        this.editing = false;
        this.editValues = {};
        this.errorMessage = '';
        this.batchProgress = null;
        this.batchResult = null;
        this.showConfirmation = false;
        this.cleanupPendingCoverArt();

        this.updateComplete.then(() => {
            if (this.dialog) this.dialog.open = true;
        });
    }

    /** Close the dialog. */
    close(): void {
        // Cancel in-flight batch write if active.
        if (this.batchProgress) {
            void CancelBatchWrite();
        }

        if (this.dialog) this.dialog.open = false;
        this.editing = false;
        this.editValues = {};
        this.errorMessage = '';
        this.cleanupPendingCoverArt();
        this.resetBatchState();
    }

    // =================================================================
    // STYLES
    // =================================================================

    static override styles = [designTokens, css`
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
            font-size: 16px; /* dialog header — outside type scale */
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
            font-size: 64px; /* large decorative placeholder */
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
            font-size: 22px; /* dialog title — outside type scale */
            font-weight: 600;
            color: var(--yj-text-primary, #fff);
            word-break: break-word;
        }

        .main-meta .artist {
            font-size: var(--yj-text-lg);
            color: var(--yj-text-secondary, #b3b3b3);
        }

        .main-meta .album {
            font-size: var(--yj-text-lg);
            color: var(--yj-text-tertiary, #888);
        }

        .main-meta .duration {
            font-size: var(--yj-text-md);
            color: var(--yj-text-tertiary, #888);
            font-variant-numeric: tabular-nums;
        }

        .divider {
            height: 1px;
            background: var(--yj-border-subtle, #333);
            margin-bottom: 16px;
        }

        .section-label {
            font-size: var(--yj-text-xs);
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
            font-size: var(--yj-text-sm);
            font-weight: 500;
            color: var(--yj-text-tertiary, #888);
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }

        .meta-value {
            font-size: var(--yj-text-md);
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
            font-size: var(--yj-text-md);
            padding: 4px 8px;
            font-family: inherit;
        }

        .meta-input:focus {
            outline: none;
            border-color: var(--yj-accent, #ffd43b);
        }

        .meta-input::placeholder {
            color: var(--yj-text-tertiary, #888);
            font-style: italic;
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

        .main-input::placeholder {
            color: var(--yj-text-tertiary, #888);
            font-style: italic;
        }

        .main-input.title-input {
            font-size: 20px; /* edit mode title — outside type scale */
            font-weight: 600;
        }

        .main-input.artist-input {
            font-size: var(--yj-text-lg);
        }

        .main-input.album-input {
            font-size: var(--yj-text-md);
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
            font-size: var(--yj-text-md);
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

        .btn:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }

        .btn-danger {
            color: var(--yj-error, #e03131);
            border-color: var(--yj-error, #e03131);
        }

        .btn-danger:hover {
            background: var(--yj-error, #e03131);
            color: #fff;
        }

        /* Cover art edit mode */
        .cover-art-edit {
            cursor: pointer;
            position: relative;
        }

        .cover-art-overlay {
            position: absolute;
            inset: 0;
            background: rgba(0, 0, 0, 0.5);
            display: flex;
            align-items: center;
            justify-content: center;
            opacity: 0;
            transition: opacity 0.15s ease;
            border-radius: 6px;
        }

        .cover-art-edit:hover .cover-art-overlay {
            opacity: 1;
        }

        .cover-art-overlay wa-icon {
            color: #fff;
            font-size: 32px;
        }

        .cover-art-remove {
            position: absolute;
            top: 4px;
            right: 4px;
            width: 24px;
            height: 24px;
            border-radius: 50%;
            border: none;
            background: rgba(0, 0, 0, 0.7);
            color: #fff;
            font-size: 14px;
            cursor: pointer;
            display: flex;
            align-items: center;
            justify-content: center;
            opacity: 0;
            transition: opacity 0.15s ease;
        }

        .cover-art-edit:hover .cover-art-remove {
            opacity: 1;
        }

        .cover-art-remove:hover {
            background: var(--yj-error, #e03131);
        }

        /* Error message */
        .error-message {
            flex: 1;
            color: var(--yj-error, #e03131);
            font-size: var(--yj-text-sm);
            padding: 4px 0;
            word-break: break-word;
        }

        /* ---- Batch mode styles ---- */

        .batch-header {
            font-size: 22px;
            font-weight: 600;
            color: var(--yj-text-primary, #fff);
        }

        .batch-subheader {
            font-size: var(--yj-text-md);
            color: var(--yj-text-tertiary, #888);
        }

        .mixed-value {
            color: var(--yj-text-tertiary, #888);
            font-style: italic;
        }

        .cover-art-mixed {
            width: 100%;
            height: 100%;
            background-color: var(--yj-bg-elevated, #343a40);
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            gap: 8px;
        }

        .cover-art-mixed wa-icon {
            color: var(--yj-text-tertiary, #888);
            font-size: 48px;
        }

        .cover-art-mixed span {
            color: var(--yj-text-tertiary, #888);
            font-size: var(--yj-text-xs);
            font-style: italic;
        }

        /* Confirmation overlay */
        .confirmation-overlay {
            position: absolute;
            inset: 0;
            background: rgba(0, 0, 0, 0.6);
            display: flex;
            align-items: center;
            justify-content: center;
            z-index: 10;
            border-radius: 8px;
        }

        .confirmation-content {
            background: var(--yj-bg-surface, #212529);
            border: 1px solid var(--yj-border, #444);
            border-radius: 8px;
            padding: 20px;
            max-width: 420px;
            width: 90%;
        }

        .confirmation-content h3 {
            margin: 0 0 16px;
            font-size: var(--yj-text-lg);
            color: var(--yj-text-primary, #fff);
        }

        .confirmation-summary {
            margin-bottom: 16px;
            font-size: var(--yj-text-md);
            color: var(--yj-text-secondary, #b3b3b3);
        }

        .confirmation-summary .change-item {
            padding: 4px 0;
            border-bottom: 1px solid var(--yj-border-subtle, #333);
        }

        .confirmation-summary .change-item:last-child {
            border-bottom: none;
        }

        .confirmation-actions {
            display: flex;
            justify-content: flex-end;
            gap: 8px;
        }

        /* Progress bar */
        .batch-progress {
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 12px;
            padding: 40px 20px;
        }

        .progress-text {
            font-size: var(--yj-text-lg);
            color: var(--yj-text-primary, #fff);
            font-variant-numeric: tabular-nums;
        }

        .progress-bar-track {
            width: 100%;
            height: 8px;
            background: var(--yj-bg-elevated, #343a40);
            border-radius: 4px;
            overflow: hidden;
        }

        .progress-bar-fill {
            height: 100%;
            background: var(--yj-accent, #4a9eff);
            border-radius: 4px;
            transition: width 0.3s ease;
        }

        /* Batch results */
        .batch-result {
            padding: 20px 0;
        }

        .batch-result .result-title {
            font-size: var(--yj-text-lg);
            font-weight: 600;
            color: var(--yj-text-primary, #fff);
            margin-bottom: 12px;
        }

        .batch-result .result-success {
            color: var(--yj-success, #40c057);
        }

        .batch-result .result-partial {
            color: var(--yj-warning, #fab005);
        }

        .batch-result .result-cancelled {
            color: var(--yj-text-tertiary, #888);
        }

        .failure-list {
            margin-top: 12px;
            font-size: var(--yj-text-sm);
        }

        .failure-list summary {
            cursor: pointer;
            color: var(--yj-error, #e03131);
            font-weight: 500;
            margin-bottom: 8px;
        }

        .failure-item {
            padding: 4px 0;
            color: var(--yj-text-secondary, #b3b3b3);
            word-break: break-all;
        }

        .failure-item .failure-path {
            font-weight: 500;
            color: var(--yj-text-primary, #fff);
        }

        .failure-item .failure-error {
            color: var(--yj-error, #e03131);
            font-style: italic;
        }
    `];

    // =================================================================
    // RENDER
    // =================================================================

    override render() {
        const label = this.batchMode
            ? 'Batch Edit'
            : 'Track Details';

        return html`
            <wa-dialog label="${label}">
                ${this.batchMode
                    ? this.renderBatchContent()
                    : this.track
                        ? this.renderContent()
                        : nothing}
            </wa-dialog>
        `;
    }

    // -- Single-track render (unchanged) --

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

    // -- Batch render --

    private renderBatchContent() {
        if (this.batchProgress) {
            return this.renderBatchProgress();
        }

        if (this.batchResult) {
            return this.renderBatchResult();
        }

        const fields = this.getMergedFields();

        return html`
            <div class="top-section">
                ${this.renderBatchCoverArt()}
                <div class="main-meta">
                    ${this.renderBatchMainFields(fields)}
                </div>
            </div>
            <div class="divider"></div>
            <div class="metadata-grid">
                ${this.renderBatchDetailFields(fields)}
            </div>
            <div class="action-bar">
                ${this.renderBatchActions()}
            </div>
            ${this.showConfirmation
                ? this.renderConfirmation()
                : nothing}
        `;
    }

    private renderBatchCoverArt() {
        if (this.editing) {
            return this.renderCoverArtEditable();
        }

        if (this.batchCoverArtMixed && !this.coverArt) {
            return html`
                <div class="cover-art">
                    <div class="cover-art-mixed">
                        <wa-icon name="images"></wa-icon>
                        <span>Multiple cover arts</span>
                    </div>
                </div>
            `;
        }

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

    private renderBatchMainFields(
        fields: MetadataField[],
    ) {
        const titleField = fields.find(
            (f) => f.key === 'title',
        );
        const artistField = fields.find(
            (f) => f.key === 'artist',
        );
        const albumField = fields.find(
            (f) => f.key === 'album',
        );

        if (this.editing) {
            return html`
                <span class="batch-header">
                    Editing ${this.batchTracks.length} tracks
                </span>
                <input
                    class="main-input title-input"
                    .value=${this.getEditValue(
                        'title',
                        titleField?.value ?? '',
                    )}
                    @input=${(e: Event) =>
                        this.onEditInput('title', e)}
                    placeholder=${titleField?.mixed
                        ? 'Multiple values'
                        : 'Title'}
                />
                <input
                    class="main-input artist-input"
                    .value=${this.getEditValue(
                        'artist',
                        artistField?.value ?? '',
                    )}
                    @input=${(e: Event) =>
                        this.onEditInput('artist', e)}
                    placeholder=${artistField?.mixed
                        ? 'Multiple values'
                        : 'Artist'}
                />
                <input
                    class="main-input album-input"
                    .value=${this.getEditValue(
                        'album',
                        albumField?.value ?? '',
                    )}
                    @input=${(e: Event) =>
                        this.onEditInput('album', e)}
                    placeholder=${albumField?.mixed
                        ? 'Multiple values'
                        : 'Album'}
                />
            `;
        }

        return html`
            <span class="batch-header">
                ${this.batchTracks.length} tracks selected
            </span>
            ${titleField?.mixed
                ? html`<span class="mixed-value">
                      ${this.countDistinctValues('title')} different titles
                  </span>`
                : titleField?.value
                    ? html`<span class="title">
                          ${titleField.value}
                      </span>`
                    : nothing}
            ${artistField?.mixed
                ? html`<span class="mixed-value">
                      ${this.countDistinctValues('artist')} different artists
                  </span>`
                : artistField?.value
                    ? html`<span class="artist">
                          ${artistField.value}
                      </span>`
                    : nothing}
            ${albumField?.mixed
                ? html`<span class="mixed-value">
                      ${this.countDistinctValues('album')} different albums
                  </span>`
                : albumField?.value
                    ? html`<span class="album">
                          ${albumField.value}
                      </span>`
                    : nothing}
        `;
    }

    private renderBatchDetailFields(
        fields: MetadataField[],
    ) {
        // Only show editable detail fields in batch mode
        // (genre, year, composer, track#, disc#).
        const detailFields = fields.filter(
            (f) =>
                f.editable &&
                f.key !== 'title' &&
                f.key !== 'artist' &&
                f.key !== 'album',
        );

        return detailFields.map((f) =>
            this.renderBatchField(f),
        );
    }

    private renderBatchField(f: MetadataField) {
        if (this.editing) {
            return html`
                <span class="meta-label">${f.label}</span>
                <input
                    class="meta-input"
                    type=${f.type}
                    .value=${this.getEditValue(
                        f.key,
                        f.value,
                    )}
                    @input=${(e: Event) =>
                        this.onEditInput(f.key, e)}
                    placeholder=${f.mixed
                        ? 'Multiple values'
                        : ''}
                />
            `;
        }

        if (f.mixed) {
            return html`
                <span class="meta-label">${f.label}</span>
                <span class="meta-value mixed-value">
                    ${this.countDistinctValues(f.key)} different values
                </span>
            `;
        }

        const display = f.value;

        return html`
            <span class="meta-label">${f.label}</span>
            <span
                class="meta-value ${display
                    ? ''
                    : 'empty'}"
            >
                ${display || 'None'}
            </span>
        `;
    }

    private renderBatchActions() {
        if (this.editing) {
            return html`
                ${this.errorMessage
                    ? html`<div class="error-message">
                          ${this.errorMessage}
                      </div>`
                    : nothing}
                <button
                    class="btn"
                    @click=${this.cancelEdit}
                    ?disabled=${this.saving}
                >
                    Cancel
                </button>
                <button
                    class="btn btn-primary"
                    @click=${this.saveBatchEdit}
                    ?disabled=${this.saving}
                >
                    ${this.saving ? 'Saving…' : 'Save'}
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

    private renderConfirmation() {
        const changes = this.getConfirmationSummary();

        return html`
            <div class="confirmation-overlay">
                <div class="confirmation-content">
                    <h3>Apply changes to ${this.batchTracks.length} tracks?</h3>
                    <div class="confirmation-summary">
                        ${changes.map(
                            (c) => html`
                                <div class="change-item">
                                    ${c}
                                </div>
                            `,
                        )}
                    </div>
                    <div class="confirmation-actions">
                        <button
                            class="btn"
                            @click=${this.cancelConfirmation}
                        >
                            Cancel
                        </button>
                        <button
                            class="btn btn-primary"
                            @click=${this.confirmSave}
                        >
                            Apply
                        </button>
                    </div>
                </div>
            </div>
        `;
    }

    private renderBatchProgress() {
        const progress = this.batchProgress!;
        const pct =
            progress.total > 0
                ? (progress.current / progress.total) * 100
                : 0;

        return html`
            <div class="batch-progress">
                <div class="progress-text">
                    ${progress.current} of ${progress.total} tracks
                </div>
                <div class="progress-bar-track">
                    <div
                        class="progress-bar-fill"
                        style="width: ${pct}%"
                    ></div>
                </div>
                <button
                    class="btn btn-danger"
                    @click=${this.cancelBatchWrite}
                >
                    Cancel
                </button>
            </div>
        `;
    }

    private renderBatchResult() {
        const r = this.batchResult!;

        let titleClass = 'result-success';
        let titleText = `All ${r.succeeded} tracks updated successfully`;

        if (r.cancelled) {
            titleClass = 'result-cancelled';
            titleText = `Batch cancelled \u2014 ${r.succeeded} of ${r.succeeded + r.failed + (this.batchFilePaths.length - r.succeeded - r.failed)} tracks updated`;
        } else if (r.failed > 0) {
            titleClass = 'result-partial';
            titleText = `${r.succeeded} tracks updated, ${r.failed} failed`;
        }

        return html`
            <div class="batch-result">
                <div class="result-title ${titleClass}">
                    ${titleText}
                </div>
                ${r.failures.length > 0
                    ? html`
                          <details class="failure-list">
                              <summary>
                                  ${r.failures.length} failure${r.failures.length > 1 ? 's' : ''}
                              </summary>
                              ${r.failures.map(
                                  (f) => html`
                                      <div class="failure-item">
                                          <span class="failure-path">
                                              ${this.fileNameFromPath(f.filePath)}
                                          </span>
                                          \u2014
                                          <span class="failure-error">
                                              ${f.error}
                                          </span>
                                      </div>
                                  `,
                              )}
                          </details>
                      `
                    : nothing}
                <div class="action-bar">
                    <button
                        class="btn"
                        @click=${this.closeBatchResult}
                    >
                        Close
                    </button>
                </div>
            </div>
        `;
    }

    // -- Single-track render helpers (unchanged) --

    private renderCoverArt() {
        if (this.editing) {
            return this.renderCoverArtEditable();
        }

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

    private renderCoverArtEditable() {
        const showRemove =
            !this.clearCoverArt &&
            (this.pendingCoverArt || this.coverArt);

        // Determine which image to show.
        let src: string | undefined;

        if (this.clearCoverArt) {
            src = undefined; // Show placeholder.
        } else if (this.pendingCoverArt) {
            src = this.pendingCoverArt.previewUrl;
        } else {
            src =
                this.coverArt?.coverArtLarge ??
                this.coverArt?.coverArtMedium ??
                this.coverArt?.coverArtPath;
        }

        return html`
            <div
                class="cover-art cover-art-edit"
                @click=${this.selectCoverArt}
            >
                ${src
                    ? html`<img
                          src="${src}"
                          alt="Album cover"
                          @error=${this.handleImageError}
                      />`
                    : html`<div class="cover-placeholder">
                          <wa-icon
                              name="music"
                          ></wa-icon>
                      </div>`}
                <div class="cover-art-overlay">
                    <wa-icon
                        name="pen-to-square"
                    ></wa-icon>
                </div>
                ${showRemove
                    ? html`<button
                          class="cover-art-remove"
                          @click=${(e: Event) => {
                              e.stopPropagation();
                              this.removeCoverArt();
                          }}
                          title="Remove cover art"
                      >
                          ×
                      </button>`
                    : nothing}
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
                ${this.errorMessage
                    ? html`<div class="error-message">
                          ${this.errorMessage}
                      </div>`
                    : nothing}
                <button
                    class="btn"
                    @click=${this.cancelEdit}
                    ?disabled=${this.saving}
                >
                    Cancel
                </button>
                <button
                    class="btn btn-primary"
                    @click=${this.saveEdit}
                    ?disabled=${this.saving}
                >
                    ${this.saving ? 'Saving…' : 'Save'}
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
        this.errorMessage = '';
        this.cleanupPendingCoverArt();
    };

    private cancelEdit = () => {
        this.exitEditMode();
    };

    // -- Single-track save --

    private saveEdit = async () => {
        if (this.batchMode) {
            this.saveBatchEdit();

            return;
        }

        if (!this.track || this.saving) return;

        this.saving = true;
        this.errorMessage = '';

        try {
            const changes = this.buildChanges();

            if (Object.keys(changes).length === 0) {
                // No actual changes — just exit edit mode.
                this.exitEditMode();

                return;
            }

            const filePath = this.track.FilePath;

            await WriteTrackTagsByPath(filePath, changes);

            // Success — switch to read-only view. The
            // TrackMetadataChanged event triggers library store
            // invalidation, which refreshes all other views.
            this.exitEditMode();

            // Re-fetch track and album data so the dialog
            // shows updated values and cover art.  The store
            // invalidation is already in-flight from the event;
            // these calls await the pending fetch or start one.
            const [tracks, albums] = await Promise.all([
                libraryStore.getTracks(),
                libraryStore.getAlbums(),
            ]);

            const updated = tracks.find(
                (t) => t.FilePath === filePath,
            );

            if (updated) {
                this.track = updated;

                // Re-resolve cover art from the refreshed
                // album data (URLs change on new content hash).
                const album = albums.find(
                    (a) => a.Name === updated.Album,
                );

                if (album?.CoverArtPath) {
                    this.coverArt = {
                        coverArtPath: album.CoverArtPath,
                        coverArtSmall: album.CoverArtSmall,
                        coverArtMedium: album.CoverArtMedium,
                        coverArtLarge: album.CoverArtLarge,
                    };
                } else {
                    this.coverArt = null;
                }
            }
        } catch (err: unknown) {
            const msg =
                err instanceof Error
                    ? err.message
                    : String(err);
            this.errorMessage = msg;
        } finally {
            this.saving = false;
        }
    };

    // -- Batch save --

    private saveBatchEdit = () => {
        if (!this.batchMode) return;

        const changes = this.buildBatchChanges();

        if (Object.keys(changes).length === 0) {
            this.exitEditMode();

            return;
        }

        // Show confirmation before executing.
        this.showConfirmation = true;
    };

    private cancelConfirmation = () => {
        this.showConfirmation = false;
    };

    private confirmSave = async () => {
        this.showConfirmation = false;
        this.batchProgress = {
            current: 0,
            total: this.batchFilePaths.length,
        };
        this.editing = false;

        const changes = this.buildBatchChanges();

        // Listen for progress events.
        EventsOn(
            Events.BatchWriteProgress,
            (data: {
                current: number;
                total: number;
            }) => {
                this.batchProgress = {
                    current: data.current,
                    total: data.total,
                };
            },
        );

        try {
            const result = await BatchWriteTrackTags(
                this.batchFilePaths,
                changes,
            );

            this.batchResult = {
                succeeded: result.succeeded,
                failed: result.failed,
                cancelled: result.cancelled,
                failures: (result.failures ?? []).map(
                    (f) => ({
                        filePath: f.filePath,
                        error: f.error,
                    }),
                ),
            };
        } catch (err: unknown) {
            const msg =
                err instanceof Error
                    ? err.message
                    : String(err);
            this.batchResult = {
                succeeded: 0,
                failed: this.batchFilePaths.length,
                cancelled: false,
                failures: [
                    {
                        filePath: '(batch)',
                        error: msg,
                    },
                ],
            };
        } finally {
            EventsOff(Events.BatchWriteProgress);
            this.batchProgress = null;
        }
    };

    private cancelBatchWrite = () => {
        void CancelBatchWrite();
    };

    private closeBatchResult = async () => {
        this.batchResult = null;
        this.batchProgress = null;
        this.editValues = {};
        this.errorMessage = '';
        this.cleanupPendingCoverArt();

        // Refresh data from library store.
        const [tracks, albums] = await Promise.all([
            libraryStore.getTracks(),
            libraryStore.getAlbums(),
        ]);

        // Re-resolve batch tracks.
        const pathSet = new Set(this.batchFilePaths);
        const refreshed = tracks.filter((t) =>
            pathSet.has(t.FilePath),
        );
        this.batchTracks = refreshed;

        // Re-resolve cover art state.
        const albumNames = new Set(
            refreshed.map((t) => t.Album),
        );

        if (albumNames.size === 1) {
            const albumName = [...albumNames][0]!;
            const album = albums.find(
                (a) => a.Name === albumName,
            );

            if (album?.CoverArtPath) {
                this.coverArt = {
                    coverArtPath: album.CoverArtPath,
                    coverArtSmall: album.CoverArtSmall,
                    coverArtMedium: album.CoverArtMedium,
                    coverArtLarge: album.CoverArtLarge,
                };
                this.batchCoverArtMixed = false;
            } else {
                this.coverArt = null;
                this.batchCoverArtMixed = false;
            }
        } else {
            this.coverArt = null;
            this.batchCoverArtMixed = albumNames.size > 1;
        }
    };

    // -- Shared edit logic --

    private buildChanges(): Record<string, unknown> {
        const t = this.track!;
        const changes: Record<string, unknown> = {};

        // Map frontend edit keys to backend field constants
        // and original values.
        const fieldMap: Array<{
            editKey: string;
            backendKey: string;
            original: string;
            transform?: (v: string) => unknown;
        }> = [
            {
                editKey: 'title',
                backendKey: 'title',
                original: t.TrackName,
            },
            {
                editKey: 'artist',
                backendKey: 'artist',
                original: t.ArtistName,
            },
            {
                editKey: 'album',
                backendKey: 'album',
                original: t.Album,
            },
            {
                editKey: 'genre',
                backendKey: 'genre',
                original: (t.Genre ?? []).join(', '),
            },
            {
                editKey: 'year',
                backendKey: 'year',
                original: t.Year ? String(t.Year) : '',
                transform: (v) =>
                    v ? parseInt(v, 10) : 0,
            },
            {
                editKey: 'composer',
                backendKey: 'composer',
                original: t.Composer ?? '',
            },
            {
                editKey: 'trackNumber',
                backendKey: 'track_number',
                original: t.TrackNumber
                    ? String(t.TrackNumber)
                    : '',
                transform: (v) =>
                    v ? parseInt(v, 10) : 0,
            },
            {
                editKey: 'discNumber',
                backendKey: 'disc_number',
                original: t.DiscNumber
                    ? String(t.DiscNumber)
                    : '',
                transform: (v) =>
                    v ? parseInt(v, 10) : 0,
            },
        ];

        for (const {
            editKey,
            backendKey,
            original,
            transform,
        } of fieldMap) {
            if (editKey in this.editValues) {
                const newVal = this.editValues[editKey]!;

                if (newVal !== original) {
                    changes[backendKey] = transform
                        ? transform(newVal)
                        : newVal;
                }
            }
        }

        // Cover art changes.
        if (this.pendingCoverArt) {
            // Convert ArrayBuffer to number[] for JSON
            // serialization (Wails passes as []byte on Go side).
            changes['cover_art'] = Array.from(
                new Uint8Array(this.pendingCoverArt.data),
            );
        } else if (this.clearCoverArt) {
            changes['cover_art'] = null;
        }

        return changes;
    }

    private buildBatchChanges(): Record<string, unknown> {
        const changes: Record<string, unknown> = {};

        const fieldMap: Array<{
            editKey: string;
            backendKey: string;
            transform?: (v: string) => unknown;
        }> = [
            { editKey: 'title', backendKey: 'title' },
            { editKey: 'artist', backendKey: 'artist' },
            { editKey: 'album', backendKey: 'album' },
            { editKey: 'genre', backendKey: 'genre' },
            {
                editKey: 'year',
                backendKey: 'year',
                transform: (v) =>
                    v ? parseInt(v, 10) : 0,
            },
            {
                editKey: 'composer',
                backendKey: 'composer',
            },
            {
                editKey: 'trackNumber',
                backendKey: 'track_number',
                transform: (v) =>
                    v ? parseInt(v, 10) : 0,
            },
            {
                editKey: 'discNumber',
                backendKey: 'disc_number',
                transform: (v) =>
                    v ? parseInt(v, 10) : 0,
            },
        ];

        for (const {
            editKey,
            backendKey,
            transform,
        } of fieldMap) {
            if (editKey in this.editValues) {
                const val = this.editValues[editKey]!;
                changes[backendKey] = transform
                    ? transform(val)
                    : val;
            }
        }

        // Cover art.
        if (this.pendingCoverArt) {
            changes['cover_art'] = Array.from(
                new Uint8Array(this.pendingCoverArt.data),
            );
        } else if (this.clearCoverArt) {
            changes['cover_art'] = null;
        }

        return changes;
    }

    private exitEditMode(): void {
        this.editing = false;
        this.editValues = {};
        this.errorMessage = '';
        this.showConfirmation = false;
        this.cleanupPendingCoverArt();
    }

    private cleanupPendingCoverArt(): void {
        if (this.pendingCoverArt?.previewUrl) {
            URL.revokeObjectURL(
                this.pendingCoverArt.previewUrl,
            );
        }
        this.pendingCoverArt = null;
        this.clearCoverArt = false;
    }

    private resetBatchState(): void {
        this.batchMode = false;
        this.batchTracks = [];
        this.batchFilePaths = [];
        this.batchCoverArtMixed = false;
        this.batchProgress = null;
        this.batchResult = null;
        this.showConfirmation = false;
    }

    private selectCoverArt = async () => {
        try {
            const filePath = await ImageFilePicker();

            if (!filePath) return; // User cancelled.

            const bytes =
                await this.readCoverArtFile(filePath);

            if (!bytes) return;

            // Create preview URL from bytes.
            const buffer = (bytes.buffer as ArrayBuffer).slice(
                bytes.byteOffset,
                bytes.byteOffset + bytes.byteLength,
            );
            const blob = new Blob([buffer]);
            const previewUrl =
                URL.createObjectURL(blob);

            this.cleanupPendingCoverArt();
            this.pendingCoverArt = {
                data: buffer,
                previewUrl,
            };
            this.clearCoverArt = false;
        } catch (err) {
            console.error(
                'Failed to select cover art:',
                err,
            );
        }
    };

    private async readCoverArtFile(
        filePath: string,
    ): Promise<Uint8Array | null> {
        try {
            // ReadFile returns Go []byte which Wails serializes
            // as a base64-encoded string (standard encoding/json
            // behaviour for []byte).
            const result = await ReadFile(filePath);
            const b64 = result as unknown as string;
            const binary = atob(b64);
            const bytes = new Uint8Array(binary.length);

            for (let i = 0; i < binary.length; i++) {
                bytes[i] = binary.charCodeAt(i);
            }

            return bytes;
        } catch (err) {
            console.error(
                'Failed to read cover art file:',
                err,
            );

            return null;
        }
    }

    private removeCoverArt = () => {
        this.cleanupPendingCoverArt();
        this.clearCoverArt = true;
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
    // BATCH HELPERS
    // =================================================================

    /** Compute merged field values across all batch tracks. */
    private getMergedFields(): MetadataField[] {
        const tracks = this.batchTracks;

        const extractors: Array<{
            key: string;
            label: string;
            type: 'text' | 'number';
            extract: (t: library.Track) => string;
        }> = [
            {
                key: 'title',
                label: 'Title',
                type: 'text',
                extract: (t) => t.TrackName ?? '',
            },
            {
                key: 'artist',
                label: 'Artist',
                type: 'text',
                extract: (t) => t.ArtistName ?? '',
            },
            {
                key: 'album',
                label: 'Album',
                type: 'text',
                extract: (t) => t.Album ?? '',
            },
            {
                key: 'genre',
                label: 'Genre',
                type: 'text',
                extract: (t) =>
                    (t.Genre ?? []).join(', '),
            },
            {
                key: 'year',
                label: 'Year',
                type: 'number',
                extract: (t) =>
                    t.Year ? String(t.Year) : '',
            },
            {
                key: 'composer',
                label: 'Composer',
                type: 'text',
                extract: (t) => t.Composer ?? '',
            },
            {
                key: 'trackNumber',
                label: 'Track #',
                type: 'number',
                extract: (t) =>
                    t.TrackNumber
                        ? String(t.TrackNumber)
                        : '',
            },
            {
                key: 'discNumber',
                label: 'Disc #',
                type: 'number',
                extract: (t) =>
                    t.DiscNumber
                        ? String(t.DiscNumber)
                        : '',
            },
        ];

        return extractors.map(
            ({ key, label, type, extract }) => {
                const values = tracks.map(extract);
                const unique = new Set(values);
                const allSame = unique.size <= 1;

                return {
                    key,
                    label,
                    value: allSame
                        ? (values[0] ?? '')
                        : '',
                    editable: true,
                    type,
                    mixed: !allSame,
                };
            },
        );
    }

    /** Count distinct values for a field across batch tracks. */
    private countDistinctValues(key: string): number {
        const extractMap: Record<
            string,
            (t: library.Track) => string
        > = {
            title: (t) => t.TrackName ?? '',
            artist: (t) => t.ArtistName ?? '',
            album: (t) => t.Album ?? '',
            genre: (t) => (t.Genre ?? []).join(', '),
            year: (t) =>
                t.Year ? String(t.Year) : '',
            composer: (t) => t.Composer ?? '',
            trackNumber: (t) =>
                t.TrackNumber
                    ? String(t.TrackNumber)
                    : '',
            discNumber: (t) =>
                t.DiscNumber
                    ? String(t.DiscNumber)
                    : '',
        };

        const extract = extractMap[key];

        if (!extract) return 0;

        const unique = new Set(
            this.batchTracks.map(extract),
        );

        return unique.size;
    }

    /** Build human-readable list of changes for confirmation. */
    private getConfirmationSummary(): string[] {
        const summary: string[] = [];

        for (const [key, value] of Object.entries(
            this.editValues,
        )) {
            const label = FIELD_LABELS[key] ?? key;

            if (value === '') {
                summary.push(`Clear ${label}`);
            } else {
                summary.push(
                    `Set ${label} to \u201c${value}\u201d`,
                );
            }
        }

        if (this.pendingCoverArt) {
            summary.push('Set cover art');
        } else if (this.clearCoverArt) {
            summary.push('Remove cover art');
        }

        return summary;
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

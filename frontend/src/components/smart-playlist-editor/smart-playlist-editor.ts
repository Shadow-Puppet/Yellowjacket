import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { library } from '@go/models';
import { PreviewSmartPlaylist } from '@go/playlist/Service';
import { libraryStore } from '@store/library-store';
import { designTokens } from '../../styles/tokens.css';
import '@components/combobox/combobox.ts';

// ── Field / Operator constants ──────────────────────────────────────

/** All 16 fields matching the backend `fieldMap` keys. */
const FIELDS: string[] = [
    'title',
    'artist',
    'album',
    'genre',
    'year',
    'composer',
    'file_type',
    'duration',
    'sample_rate',
    'bit_depth',
    'channels',
    'bitrate',
    'file_size',
    'library',
    'track_number',
    'disc_number',
];

const NUMERIC_FIELDS = new Set([
    'year',
    'duration',
    'sample_rate',
    'bit_depth',
    'channels',
    'bitrate',
    'file_size',
    'library',
    'track_number',
    'disc_number',
]);

const TEXT_OPERATORS = [
    'is',
    'is_not',
    'contains',
    'does_not_contain',
    'starts_with',
    'ends_with',
    'is_any_of',
];

const NUMERIC_OPERATORS = [
    'is',
    'is_not',
    'greater_than',
    'less_than',
    'between',
];

const SORT_FIELDS = ['title', 'artist', 'album', 'year', 'duration', 'random'];

// ── Helpers ─────────────────────────────────────────────────────────

function getOperatorsForField(field: string): string[] {
    return NUMERIC_FIELDS.has(field) ? NUMERIC_OPERATORS : TEXT_OPERATORS;
}

/**
 * Human-readable labels for operator values.
 * `is_not` → "is not", `does_not_contain` → "does not contain", etc.
 */
function formatOperatorLabel(op: string): string {
    return op.replace(/_/g, ' ');
}

/** Returns autocomplete suggestions for a given field from libraryStore. */
function getAutocompleteOptions(field: string): string[] {
    switch (field) {
        case 'artist':
            return libraryStore.getCachedArtists()?.map((a) => a.Name) ?? [];
        case 'genre':
            return libraryStore.getCachedGenres()?.map((g) => g.Name) ?? [];
        case 'album':
            return libraryStore.getCachedAlbums()?.map((a) => a.Name) ?? [];
        case 'title': {
            const tracks = libraryStore.getCachedTracks();
            if (!tracks) return [];
            return [...new Set(tracks.map((t) => t.TrackName).filter(Boolean))];
        }
        case 'composer': {
            const tracks = libraryStore.getCachedTracks();
            if (!tracks) return [];
            return [...new Set(tracks.map((t) => t.Composer).filter(Boolean))];
        }
        case 'file_type': {
            const tracks = libraryStore.getCachedTracks();
            if (!tracks) return [];
            return [...new Set(tracks.map((t) => t.FileType).filter(Boolean))];
        }
        case 'year': {
            const tracks = libraryStore.getCachedTracks();
            if (!tracks) return [];
            return [
                ...new Set(
                    tracks
                        .map((t) => t.Year)
                        .filter((y) => y > 0)
                        .map(String),
                ),
            ].sort();
        }
        default:
            return [];
    }
}

/** Format a field name for display: `file_type` → "File Type". */
function formatFieldLabel(field: string): string {
    return field
        .split('_')
        .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
        .join(' ');
}

// ── Rule row type ───────────────────────────────────────────────────

interface RuleRow {
    field: string;
    operator: string;
    value: string;
    /** Second value for `between` operator (max). */
    value2: string;
}

function emptyRule(): RuleRow {
    return { field: '', operator: '', value: '', value2: '' };
}

// ── Component ───────────────────────────────────────────────────────

/**
 * `<smart-playlist-editor>` — Row-based rule builder with live preview.
 *
 * Accepts an initial `rules` JSON attribute (matching the backend RuleSet
 * schema) and emits `rules-changed` CustomEvent whenever the user edits
 * any row, limit, or sort control. A live preview panel calls
 * `PreviewSmartPlaylist` with 300ms debounce and displays matching tracks.
 */
@customElement('smart-playlist-editor')
export class SmartPlaylistEditor extends LitElement {
    // ── Public property ─────────────────────────────────────────────

    /** Initial rules JSON (attribute). Parsed in connectedCallback. */
    @property({ type: String })
    rules = '';

    // ── Internal state ──────────────────────────────────────────────

    @state() private ruleRows: RuleRow[] = [emptyRule()];
    @state() private limit = 0;
    @state() private sortField = '';
    @state() private sortDir = '';
    @state() private previewTracks: library.Track[] = [];
    @state() private previewLoading = false;
    @state() private previewError = '';

    private previewTimer: ReturnType<typeof setTimeout> | null = null;

    // ── Styles ──────────────────────────────────────────────────────

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
            }

            /* ── Rule rows ────────────────────────── */

            .rule-rows {
                display: flex;
                flex-direction: column;
                gap: 6px;
                padding: 12px 0 8px;
            }

            .rule-row {
                display: grid;
                grid-template-columns: 1fr 140px 1fr 28px;
                gap: 6px;
                align-items: start;
            }

            .rule-row.between-row {
                grid-template-columns: 1fr 140px 1fr 1fr 28px;
            }

            /* ── Form controls ────────────────────── */

            select,
            input[type='number'] {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                color: var(--yj-text-primary, #fff);
                border: 1px solid
                    var(--yj-border-subtle, rgba(255, 255, 255, 0.1));
                border-radius: 4px;
                padding: 4px 8px;
                font-size: var(--yj-text-md);
                font-family: inherit;
                width: 100%;
                box-sizing: border-box;
            }

            select:focus,
            input[type='number']:focus {
                outline: none;
                border-color: var(--yj-accent, #ffd43b);
            }

            input[type='number'] {
                -moz-appearance: textfield;
            }

            input[type='number']::-webkit-inner-spin-button,
            input[type='number']::-webkit-outer-spin-button {
                -webkit-appearance: none;
                margin: 0;
            }

            /* ── Remove button ────────────────────── */

            .remove-btn {
                display: flex;
                align-items: center;
                justify-content: center;
                width: 24px;
                height: 24px;
                border: none;
                border-radius: 4px;
                background: transparent;
                color: var(--yj-text-secondary, #b3b3b3);
                cursor: pointer;
                font-size: 14px;
                padding: 0;
                margin-top: 2px;
                transition: color 0.15s ease, background-color 0.15s ease;
            }

            .remove-btn:hover {
                color: #ff6b6b;
                background: rgba(255, 107, 107, 0.1);
            }

            .remove-btn.hidden {
                visibility: hidden;
            }

            /* ── Add rule button ──────────────────── */

            .add-rule-btn {
                background: none;
                border: 1px dashed
                    var(--yj-border-subtle, rgba(255, 255, 255, 0.15));
                border-radius: 4px;
                color: var(--yj-text-secondary, #b3b3b3);
                padding: 4px 12px;
                font-size: var(--yj-text-sm);
                font-family: inherit;
                cursor: pointer;
                transition: border-color 0.15s ease, color 0.15s ease;
                align-self: flex-start;
            }

            .add-rule-btn:hover {
                border-color: var(--yj-accent, #ffd43b);
                color: var(--yj-accent, #ffd43b);
            }

            /* ── Options row (limit, sort) ────────── */

            .options-row {
                display: flex;
                align-items: center;
                gap: 12px;
                padding: 10px 0 4px;
                border-top: 1px solid
                    var(--yj-border-subtle, rgba(255, 255, 255, 0.06));
                margin-top: 4px;
                flex-wrap: wrap;
            }

            .option-group {
                display: flex;
                align-items: center;
                gap: 6px;
            }

            .option-label {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-secondary, #b3b3b3);
                white-space: nowrap;
            }

            .limit-input {
                width: 64px;
            }

            .sort-select {
                min-width: 90px;
            }

            .sort-dir-btn {
                background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                border: 1px solid
                    var(--yj-border-subtle, rgba(255, 255, 255, 0.1));
                border-radius: 4px;
                color: var(--yj-text-primary, #fff);
                padding: 3px 8px;
                font-size: var(--yj-text-sm);
                font-family: inherit;
                cursor: pointer;
                min-width: 40px;
                text-align: center;
                transition: border-color 0.15s ease;
            }

            .sort-dir-btn:hover {
                border-color: var(--yj-accent, #ffd43b);
            }

            /* ── Preview section ──────────────────── */

            .preview-section {
                border-top: 1px solid
                    var(--yj-border-subtle, rgba(255, 255, 255, 0.06));
                margin-top: 8px;
                padding-top: 10px;
            }

            .preview-header {
                display: flex;
                align-items: center;
                gap: 8px;
                margin-bottom: 6px;
            }

            .preview-title {
                font-size: var(--yj-text-sm);
                font-weight: 600;
                color: var(--yj-text-secondary, #b3b3b3);
                text-transform: uppercase;
                letter-spacing: 0.05em;
            }

            .preview-count {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .preview-loading {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-secondary, #b3b3b3);
                padding: 8px 0;
            }

            .preview-error {
                font-size: var(--yj-text-sm);
                color: #ff6b6b;
                padding: 6px 0;
            }

            .preview-list {
                max-height: 200px;
                overflow-y: auto;
                display: flex;
                flex-direction: column;
            }

            .preview-track {
                display: grid;
                grid-template-columns: 1fr 1fr 1fr;
                gap: 8px;
                padding: 3px 0;
                font-size: var(--yj-text-sm);
                color: var(--yj-text-primary, #fff);
                border-bottom: 1px solid
                    var(--yj-border-subtle, rgba(255, 255, 255, 0.03));
            }

            .preview-track:last-child {
                border-bottom: none;
            }

            .preview-track span {
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
            }

            .preview-track .artist,
            .preview-track .album {
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .preview-empty {
                font-size: var(--yj-text-sm);
                color: var(--yj-text-tertiary, #666);
                padding: 8px 0;
            }
        `,
    ];

    // ── Lifecycle ───────────────────────────────────────────────────

    override connectedCallback() {
        super.connectedCallback();
        this.parseInitialRules();
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        if (this.previewTimer !== null) {
            clearTimeout(this.previewTimer);
            this.previewTimer = null;
        }
    }

    // ── Parse initial rules ─────────────────────────────────────────

    private parseInitialRules() {
        if (!this.rules) {
            this.ruleRows = [emptyRule()];
            return;
        }

        try {
            const parsed = JSON.parse(this.rules);
            const rows: RuleRow[] = (parsed.rules ?? []).map(
                (r: { field?: string; operator?: string; value?: string }) => {
                    const field = r.field ?? '';
                    const operator = r.operator ?? '';
                    let value = r.value ?? '';
                    let value2 = '';

                    // Deserialize `is_any_of` JSON array back to comma string
                    if (operator === 'is_any_of' && value.startsWith('[')) {
                        try {
                            const arr = JSON.parse(value) as string[];
                            value = arr.join(', ');
                        } catch {
                            // keep raw value
                        }
                    }

                    // Deserialize `between` "min,max" into two fields
                    if (operator === 'between' && value.includes(',')) {
                        const parts = value.split(',');
                        value = parts[0]?.trim() ?? '';
                        value2 = parts[1]?.trim() ?? '';
                    }

                    return { field, operator, value, value2 };
                },
            );

            this.ruleRows = rows.length > 0 ? rows : [emptyRule()];
            this.limit = parsed.limit ?? 0;
            this.sortField = parsed.sort_field ?? '';
            this.sortDir = parsed.sort_dir ?? '';
        } catch {
            this.ruleRows = [emptyRule()];
        }

        // Trigger initial preview if rules are complete.
        this.schedulePreview();
    }

    // ── Build JSON from current state ───────────────────────────────

    private buildRulesJSON(): string {
        const rules = this.ruleRows.map((row) => {
            let value = row.value;

            // Serialize is_any_of comma-separated values to JSON array
            if (row.operator === 'is_any_of' && value) {
                const parts = value
                    .split(',')
                    .map((v) => v.trim())
                    .filter(Boolean);
                value = JSON.stringify(parts);
            }

            // Serialize between as "min,max"
            if (row.operator === 'between') {
                value = `${row.value},${row.value2}`;
            }

            return {
                field: row.field,
                operator: row.operator,
                value,
            };
        });

        return JSON.stringify({
            rules,
            limit: this.limit || 0,
            sort_field: this.sortField || '',
            sort_dir: this.sortDir || '',
        });
    }

    // ── Rule mutation methods ───────────────────────────────────────

    private updateField(index: number, newField: string) {
        const row = this.ruleRows[index];
        if (!row) return;

        const wasNumeric = NUMERIC_FIELDS.has(row.field);
        const isNumeric = NUMERIC_FIELDS.has(newField);

        row.field = newField;

        // Reset operator when field type changes (text↔numeric)
        if (wasNumeric !== isNumeric || !row.operator) {
            const ops = getOperatorsForField(newField);
            row.operator = ops[0] ?? '';
        }

        // Reset value when field changes to avoid stale autocomplete data
        row.value = '';
        row.value2 = '';

        this.ruleRows = [...this.ruleRows];
        this.onRulesChanged();
    }

    private updateOperator(index: number, newOp: string) {
        const row = this.ruleRows[index];
        if (!row) return;

        row.operator = newOp;

        // Clear value2 if no longer between
        if (newOp !== 'between') {
            row.value2 = '';
        }

        this.ruleRows = [...this.ruleRows];
        this.onRulesChanged();
    }

    private updateValue(index: number, newValue: string) {
        const row = this.ruleRows[index];
        if (!row) return;

        row.value = newValue;
        this.ruleRows = [...this.ruleRows];
        this.onRulesChanged();
    }

    private updateValue2(index: number, newValue: string) {
        const row = this.ruleRows[index];
        if (!row) return;

        row.value2 = newValue;
        this.ruleRows = [...this.ruleRows];
        this.onRulesChanged();
    }

    private addRule() {
        this.ruleRows = [...this.ruleRows, emptyRule()];
    }

    private removeRule(index: number) {
        if (this.ruleRows.length <= 1) return;
        this.ruleRows = this.ruleRows.filter((_, i) => i !== index);
        this.onRulesChanged();
    }

    private updateLimit(value: string) {
        this.limit = Math.max(0, parseInt(value, 10) || 0);
        this.onRulesChanged();
    }

    private updateSortField(value: string) {
        this.sortField = value;
        if (!value) this.sortDir = '';
        this.onRulesChanged();
    }

    private toggleSortDir() {
        if (!this.sortDir) {
            this.sortDir = 'ASC';
        } else if (this.sortDir === 'ASC') {
            this.sortDir = 'DESC';
        } else {
            this.sortDir = '';
        }
        this.onRulesChanged();
    }

    // ── Change notification ─────────────────────────────────────────

    private onRulesChanged() {
        const json = this.buildRulesJSON();

        this.dispatchEvent(
            new CustomEvent('rules-changed', {
                bubbles: true,
                composed: true,
                detail: { json },
            }),
        );

        this.schedulePreview();
    }

    // ── Live preview ────────────────────────────────────────────────

    private schedulePreview() {
        if (this.previewTimer !== null) {
            clearTimeout(this.previewTimer);
        }

        this.previewTimer = setTimeout(() => {
            this.previewTimer = null;
            void this.runPreview();
        }, 300);
    }

    private async runPreview() {
        // Skip preview if any rule is incomplete
        const incomplete = this.ruleRows.some(
            (r) =>
                !r.field ||
                !r.value ||
                (r.operator === 'between' && !r.value2),
        );
        if (incomplete) {
            this.previewTracks = [];
            this.previewError = '';
            return;
        }

        const json = this.buildRulesJSON();
        this.previewLoading = true;
        this.previewError = '';

        try {
            const tracks = await PreviewSmartPlaylist(json);
            this.previewTracks = tracks ?? [];
        } catch (error) {
            console.error('Smart playlist preview failed:', error);
            this.previewError =
                error instanceof Error ? error.message : String(error);
            this.previewTracks = [];
        } finally {
            this.previewLoading = false;
        }
    }

    // ── Render ──────────────────────────────────────────────────────

    override render() {
        return html`
            <div class="rule-rows">
                ${this.ruleRows.map((row, index) =>
                    this.renderRuleRow(row, index),
                )}
                <button class="add-rule-btn" @click=${this.addRule}>
                    + Add Rule
                </button>
            </div>

            ${this.renderSortOptions()} ${this.renderPreview()}
        `;
    }

    private renderRuleRow(row: RuleRow, index: number) {
        const isBetween = row.operator === 'between';
        const operators = row.field ? getOperatorsForField(row.field) : [];
        const isNumeric = NUMERIC_FIELDS.has(row.field);
        const isAnyOf = row.operator === 'is_any_of';

        return html`
            <div class="rule-row ${isBetween ? 'between-row' : ''}">
                <!-- Field combobox -->
                <yj-combobox
                    .options=${FIELDS}
                    .value=${row.field}
                    placeholder="Select field…"
                    @combobox-change=${(e: CustomEvent) =>
                        this.updateField(index, e.detail.value)}
                ></yj-combobox>

                <!-- Operator select -->
                <select
                    @change=${(e: Event) =>
                        this.updateOperator(
                            index,
                            (e.target as HTMLSelectElement).value,
                        )}
                    ?disabled=${!row.field}
                >
                    ${!row.field
                        ? html`<option value="">—</option>`
                        : nothing}
                    ${operators.map(
                        (op) => html`
                            <option
                                value=${op}
                                ?selected=${op === row.operator}
                            >
                                ${formatOperatorLabel(op)}
                            </option>
                        `,
                    )}
                </select>

                <!-- Value input -->
                ${isNumeric && !isBetween
                    ? html`
                          <input
                              type="number"
                              .value=${row.value}
                              placeholder="Value"
                              ?disabled=${!row.field}
                              @input=${(e: Event) =>
                                  this.updateValue(
                                      index,
                                      (e.target as HTMLInputElement).value,
                                  )}
                          />
                      `
                    : isBetween
                      ? html`
                            <input
                                type="number"
                                .value=${row.value}
                                placeholder="Min"
                                @input=${(e: Event) =>
                                    this.updateValue(
                                        index,
                                        (e.target as HTMLInputElement).value,
                                    )}
                            />
                            <input
                                type="number"
                                .value=${row.value2}
                                placeholder="Max"
                                @input=${(e: Event) =>
                                    this.updateValue2(
                                        index,
                                        (e.target as HTMLInputElement).value,
                                    )}
                            />
                        `
                      : html`
                            <yj-combobox
                                .options=${getAutocompleteOptions(row.field)}
                                .value=${row.value}
                                placeholder=${isAnyOf
                                    ? 'Comma-separated values'
                                    : 'Value'}
                                ?disabled=${!row.field}
                                @combobox-change=${(e: CustomEvent) =>
                                    this.updateValue(
                                        index,
                                        e.detail.value,
                                    )}
                            ></yj-combobox>
                        `}

                <!-- Remove button -->
                <button
                    class="remove-btn ${this.ruleRows.length <= 1 ? 'hidden' : ''}"
                    @click=${() => this.removeRule(index)}
                    title="Remove rule"
                    aria-label="Remove rule"
                >
                    ✕
                </button>
            </div>
        `;
    }

    private renderSortOptions() {
        const sortDirLabel = !this.sortDir
            ? '—'
            : this.sortDir === 'ASC'
              ? '↑'
              : '↓';

        return html`
            <div class="options-row">
                <div class="option-group">
                    <span class="option-label">Limit</span>
                    <input
                        type="number"
                        class="limit-input"
                        min="0"
                        .value=${String(this.limit || '')}
                        placeholder="∞"
                        @input=${(e: Event) =>
                            this.updateLimit(
                                (e.target as HTMLInputElement).value,
                            )}
                    />
                </div>
                <div class="option-group">
                    <span class="option-label">Sort by</span>
                    <select
                        class="sort-select"
                        @change=${(e: Event) =>
                            this.updateSortField(
                                (e.target as HTMLSelectElement).value,
                            )}
                    >
                        <option value="" ?selected=${!this.sortField}>
                            None
                        </option>
                        ${SORT_FIELDS.map(
                            (f) => html`
                                <option
                                    value=${f}
                                    ?selected=${f === this.sortField}
                                >
                                    ${formatFieldLabel(f)}
                                </option>
                            `,
                        )}
                    </select>
                    ${this.sortField
                        ? html`
                              <button
                                  class="sort-dir-btn"
                                  @click=${this.toggleSortDir}
                                  title=${this.sortDir === 'ASC'
                                      ? 'Ascending'
                                      : this.sortDir === 'DESC'
                                        ? 'Descending'
                                        : 'No direction'}
                              >
                                  ${sortDirLabel}
                              </button>
                          `
                        : nothing}
                </div>
            </div>
        `;
    }

    private renderPreview() {
        return html`
            <div class="preview-section">
                <div class="preview-header">
                    <span class="preview-title">Preview</span>
                    ${this.previewTracks.length > 0 && !this.previewLoading
                        ? html`<span class="preview-count"
                              >${this.previewTracks.length} tracks</span
                          >`
                        : nothing}
                </div>

                ${this.previewLoading
                    ? html`<div class="preview-loading">
                          Evaluating rules…
                      </div>`
                    : this.previewError
                      ? html`<div class="preview-error">
                            ${this.previewError}
                        </div>`
                      : this.previewTracks.length > 0
                        ? html`
                              <div class="preview-list">
                                  ${this.previewTracks.map(
                                      (t) => html`
                                          <div class="preview-track">
                                              <span class="title"
                                                  >${t.TrackName}</span
                                              >
                                              <span class="artist"
                                                  >${t.ArtistName}</span
                                              >
                                              <span class="album"
                                                  >${t.Album}</span
                                              >
                                          </div>
                                      `,
                                  )}
                              </div>
                          `
                        : html`<div class="preview-empty">
                              Complete all rule fields to see a
                              preview.
                          </div>`}
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'smart-playlist-editor': SmartPlaylistEditor;
    }
}

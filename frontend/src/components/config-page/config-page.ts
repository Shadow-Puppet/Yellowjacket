import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';
import { Scan, FullRescan } from '@go/library/Library';
import {
    GetLibraryDirectory,
    SetLibraryDirectory,
    GetScanConcurrency,
    SetScanConcurrency,
} from '@go/config/Config';
import { DirectoryPicker } from '@go/frontendutil/FrontendUtil';
import { ThemeController } from '@store/controllers/theme-controller';
import { Events } from '../../events';
import type { ConfigFieldChangeEvent } from './config-field';
import type { BackgroundShade } from '@store/theme-store';

import './config-field';
import './config-section';

// ===================================================================
// Scan metrics types and helpers (carried over from library-manager)
// ===================================================================

const NS_PER_MS = 1_000_000;

interface ScanMetrics {
    total: number;
    loadExisting: number;
    walkDuration: number;
    extractionWallClock: number;
    dbWritesWallClock: number;
    orphanCleanup: number;
    postScanVariants: number;
    formatExtraction: Record<string, number>;
    formatCount: Record<string, number>;
    tagExtraction: number;
    durationExtraction: number;
    batchCommits: number;
    coverArtSave: number;
    thumbnailWallClock: number;
    thumbnailGeneration: number;
    thumbnailSmall: number;
    thumbnailMedium: number;
    thumbnailLarge: number;
    clearQueue: number;
    clearDatabase: number;
    clearCoverFiles: number;
    added: number;
    updated: number;
    skipped: number;
    removed: number;
}

function fmtNs(ns: number): string {
    if (ns <= 0) return '<1ms';

    const ms = ns / NS_PER_MS;

    if (ms < 1) return '<1ms';
    if (ms < 1000) return `${ms.toFixed(0)}ms`;

    const s = ms / 1000;

    if (s < 60) return `${s.toFixed(2)}s`;

    const m = Math.floor(s / 60);
    const rem = s % 60;

    return `${m}m ${rem.toFixed(1)}s`;
}

function fmtMs(ms: number): string {
    if (ms <= 0) return '<1ms';
    if (ms < 1000) return `${ms.toFixed(0)}ms`;

    const s = ms / 1000;

    if (s < 60) return `${s.toFixed(2)}s`;

    const m = Math.floor(s / 60);
    const rem = s % 60;

    return `${m}m ${rem.toFixed(1)}s`;
}

function formatMetricsText(m: ScanMetrics): string {
    const line = (
        label: string,
        value: string,
        indent = 0,
    ) => `${'  '.repeat(indent)}${label}: ${value}`;

    const lines: string[] = [
        '=== YellowJacket Scan Results ===',
        '',
        line('Total', fmtNs(m.total)),
        '',
        '-- File Counts --',
        line('Added', String(m.added), 1),
        line('Updated', String(m.updated), 1),
        line('Skipped', String(m.skipped), 1),
        line('Removed', String(m.removed), 1),
    ];

    if (
        m.clearQueue > 0 ||
        m.clearDatabase > 0 ||
        m.clearCoverFiles > 0
    ) {
        lines.push(
            '',
            '-- Clear Phases --',
            line('Clear Queue', fmtNs(m.clearQueue), 1),
            line(
                'Clear Database',
                fmtNs(m.clearDatabase),
                1,
            ),
            line(
                'Clear Cover Files',
                fmtNs(m.clearCoverFiles),
                1,
            ),
        );
    }

    lines.push(
        '',
        '-- Scan Phases --',
        line(
            'Load Existing',
            fmtNs(m.loadExisting),
            1,
        ),
        line(
            'Directory Walk',
            fmtNs(m.walkDuration),
            1,
        ),
        line(
            'Metadata Extraction (wall)',
            fmtNs(m.extractionWallClock),
            1,
        ),
        line(
            'DB Writes (wall)',
            fmtNs(m.dbWritesWallClock),
            1,
        ),
        line(
            'Thumbnail Generation (wall)',
            fmtNs(m.thumbnailWallClock),
            1,
        ),
        line(
            'Orphan Cleanup',
            fmtNs(m.orphanCleanup),
            1,
        ),
        line(
            'Post-Scan Variants',
            fmtNs(m.postScanVariants),
            1,
        ),
    );

    return lines.join('\n');
}

// ===================================================================
// Config page component
// ===================================================================

@customElement('config-page')
export class ConfigPage extends LitElement {
    // --- Theme controller for reading/writing theme state ---
    private themeCtrl = new ThemeController(this);

    // --- Library state ---
    @state() private libraryDirectory = '';
    @state() private selectedDirectory = '';
    @state() private scanning = false;
    @state() private statusMessage = '';
    @state() private metrics: ScanMetrics | null = null;
    @state() private copied = false;
    @state() private concurrencyMode = 'auto';

    private cancelScanStarted?: () => void;
    private cancelScanComplete?: () => void;

    static override styles = css`
        :host {
            display: block;
            padding: 1.5em;
            color: var(--yj-text-primary, #fff);
            font-family: system-ui, -apple-system, sans-serif;
            overflow-y: auto;
        }

        h2 {
            margin: 0 0 1em;
            font-size: 1.4em;
            font-weight: 600;
        }

        /* Button styles */
        button {
            padding: 0.5em 1.25em;
            border: none;
            border-radius: 4px;
            font-size: 0.85em;
            font-weight: 500;
            cursor: pointer;
            transition: background-color 0.15s ease;
            white-space: nowrap;
        }

        button:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }

        .btn-warning {
            background: var(--yj-warning, #e8590c);
            color: #fff;
        }

        .btn-warning:hover:not(:disabled) {
            background: var(--yj-warning-hover, #d9480f);
        }

        .btn-danger {
            background: var(--yj-error, #e03131);
            color: #fff;
        }

        .btn-danger:hover:not(:disabled) {
            background: var(--yj-error-hover, #c92a2a);
        }

        .btn-success {
            background: var(--yj-success, #2f9e44);
            color: #fff;
        }

        .btn-success:hover:not(:disabled) {
            background: var(--yj-success-hover, #2b8a3e);
        }

        .btn-ghost {
            background: transparent;
            color: var(--yj-text-tertiary, #868e96);
            padding: 0.3em 0.75em;
            font-size: 0.75em;
            border: 1px solid var(--yj-border, #444);
        }

        .btn-ghost:hover:not(:disabled) {
            background: var(--yj-bg-overlay, #495057);
            color: var(--yj-text-primary, #fff);
        }

        .btn-ghost.copied {
            border-color: var(--yj-success, #2f9e44);
            color: var(--yj-success, #2f9e44);
        }

        /* Scan actions */
        .scan-actions {
            display: flex;
            gap: 0.75em;
            flex-wrap: wrap;
        }

        .save-row {
            display: flex;
            gap: 0.5em;
            margin-top: 0.5em;
        }

        /* Status bar */
        .status-bar {
            margin-top: 1em;
            padding: 0.75em 1em;
            background: var(--yj-bg-elevated, #343a40);
            border-radius: 4px;
            font-size: 0.85em;
            color: var(--yj-text-tertiary, #868e96);
            min-height: 1.2em;
        }

        .status-bar.active {
            color: var(--yj-accent, #ffd43b);
        }

        /* Metrics tree */
        .metrics-wrapper {
            margin-top: 1em;
        }

        .metrics-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-bottom: 0.5em;
        }

        .metrics-title {
            margin: 0;
            font-size: 0.95em;
            font-weight: 600;
            color: var(--yj-text-primary, #fff);
        }

        details {
            margin-left: 1em;
        }

        details.root {
            margin-left: 0;
        }

        summary {
            cursor: pointer;
            padding: 0.25em 0;
            font-size: 0.85em;
            color: var(--yj-text-secondary, #b3b3b3);
            list-style: none;
        }

        summary::-webkit-details-marker {
            display: none;
        }

        summary::before {
            content: '\\25B6';
            display: inline-block;
            width: 1em;
            font-size: 0.6em;
            vertical-align: middle;
            transition: transform 0.15s ease;
            margin-right: 0.35em;
        }

        details[open] > summary::before {
            transform: rotate(90deg);
        }

        .metric-row {
            display: flex;
            justify-content: space-between;
            padding: 0.2em 0;
            padding-left: 1.35em;
            font-size: 0.85em;
        }

        .metric-label {
            color: var(--yj-text-secondary, #adb5bd);
        }

        .metric-value {
            color: var(--yj-text-primary, #e9ecef);
            font-family: monospace;
            font-weight: 500;
        }

        .metric-value.highlight {
            color: var(--yj-accent, #ffd43b);
        }

        .metric-note {
            color: var(--yj-text-tertiary, #868e96);
            font-size: 0.75em;
            font-style: italic;
            padding-left: 1.35em;
        }

        .counts-grid {
            display: grid;
            grid-template-columns: repeat(4, auto);
            gap: 0.25em 1.5em;
            padding-left: 1.35em;
            font-size: 0.85em;
        }

        .count-label {
            color: var(--yj-text-secondary, #adb5bd);
        }

        .count-value {
            color: var(--yj-text-primary, #e9ecef);
            font-family: monospace;
        }

        /* Theme preview */
        .color-preview {
            display: flex;
            gap: 0.5em;
            margin-top: 0.75em;
            flex-wrap: wrap;
        }

        .swatch {
            width: 2em;
            height: 2em;
            border-radius: 4px;
            border: 1px solid var(--yj-border, #444);
        }

        .swatch-label {
            font-size: 0.7em;
            text-align: center;
            color: var(--yj-text-tertiary, #888);
            margin-top: 0.2em;
        }

        .swatch-group {
            display: flex;
            flex-direction: column;
            align-items: center;
        }
    `;

    // ===================================================================
    // LIFECYCLE
    // ===================================================================

    override connectedCallback(): void {
        super.connectedCallback();
        this.loadLibraryConfig();

        this.cancelScanStarted = EventsOn(
            Events.LibraryScanStarted,
            this.handleScanStarted,
        );
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            this.handleScanComplete,
        );
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.cancelScanStarted?.();
        this.cancelScanComplete?.();
    }

    private async loadLibraryConfig(): Promise<void> {
        try {
            const [dir, mode] = await Promise.all([
                GetLibraryDirectory(),
                GetScanConcurrency(),
            ]);

            this.libraryDirectory = dir;
            this.selectedDirectory = dir;
            this.concurrencyMode = mode;
        } catch (err) {
            console.error(
                'Failed to load library config:',
                err,
            );
        }
    }

    // ===================================================================
    // LIBRARY HANDLERS
    // ===================================================================

    private handleScanStarted = (): void => {
        this.scanning = true;
        this.statusMessage = 'Scanning...';
        this.metrics = null;
        this.copied = false;
    };

    private handleScanComplete = (
        metrics?: ScanMetrics,
    ): void => {
        this.scanning = false;
        this.statusMessage = 'Scan complete.';

        if (metrics) {
            this.metrics = metrics;
        }
    };

    private handleDirectoryBrowse = async (): Promise<void> => {
        try {
            const dir = await DirectoryPicker();

            if (dir) {
                this.selectedDirectory = dir;
            }
        } catch (err) {
            console.error(
                'Directory picker failed:',
                err,
            );
        }
    };

    private handleSaveDirectory = async (): Promise<void> => {
        if (!this.selectedDirectory) return;

        try {
            await SetLibraryDirectory(
                this.selectedDirectory,
            );
            this.libraryDirectory =
                this.selectedDirectory;
            this.statusMessage =
                'Library directory saved. A scan will start automatically if the directory changed.';
        } catch (err) {
            this.statusMessage = `Failed to save directory: ${err}`;
        }
    };

    private handleConcurrencyChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        const mode = String(e.detail.value);

        SetScanConcurrency(mode)
            .then(() => {
                this.concurrencyMode = mode;
                this.statusMessage =
                    'Storage type saved. Takes effect on next scan.';
            })
            .catch((err: unknown) => {
                this.statusMessage = `Failed to save storage type: ${err}`;
            });
    };

    private handleSoftScan = async (): Promise<void> => {
        try {
            await Scan();
        } catch (err) {
            this.statusMessage = `Scan failed: ${err}`;
        }
    };

    private handleFullRescan = async (): Promise<void> => {
        const confirmed = confirm(
            'This will delete ALL library data including cover art and re-scan from scratch. Continue?',
        );

        if (!confirmed) return;

        try {
            await FullRescan();
        } catch (err) {
            this.statusMessage = `Full rescan failed: ${err}`;
        }
    };

    private handleCopyMetrics = async (): Promise<void> => {
        if (!this.metrics) return;

        try {
            await navigator.clipboard.writeText(
                formatMetricsText(this.metrics),
            );
            this.copied = true;
            setTimeout(() => {
                this.copied = false;
            }, 2000);
        } catch (err) {
            console.error(
                'Failed to copy metrics:',
                err,
            );
        }
    };

    // ===================================================================
    // THEME HANDLERS
    // ===================================================================

    private handleAccentChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        this.themeCtrl
            .setAccentColor(String(e.detail.value))
            .catch((err: unknown) => {
                console.error(
                    'Failed to set accent color:',
                    err,
                );
            });
    };

    private handleShadeChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        this.themeCtrl
            .setBackgroundShade(
                String(
                    e.detail.value,
                ) as BackgroundShade,
            )
            .catch((err: unknown) => {
                console.error(
                    'Failed to set background shade:',
                    err,
                );
            });
    };

    // ===================================================================
    // COMPUTED
    // ===================================================================

    private get directoryChanged(): boolean {
        return (
            this.selectedDirectory !==
            this.libraryDirectory
        );
    }

    private get hasRescanPhases(): boolean {
        if (!this.metrics) return false;

        const m = this.metrics;

        return (
            m.clearQueue > 0 ||
            m.clearDatabase > 0 ||
            m.clearCoverFiles > 0
        );
    }

    // ===================================================================
    // RENDER
    // ===================================================================

    override render() {
        return html`
            <h2>Settings</h2>

            ${this.renderThemeSection()}
            ${this.renderLibrarySection()}
        `;
    }

    // --- Theme section ---

    private renderThemeSection() {
        return html`
            <config-section
                heading="Theme"
                description="Customise the app's colour scheme."
            >
                <config-field
                    .schema=${{
                        key: 'accentColor',
                        label: 'Accent Colour',
                        description:
                            'The primary highlight colour used for active items, buttons, and indicators.',
                        type: 'color' as const,
                    }}
                    .value=${this.themeCtrl.accentColor}
                    @config-change=${this.handleAccentChange}
                ></config-field>

                <config-field
                    .schema=${{
                        key: 'backgroundShade',
                        label: 'Background Shade',
                        description:
                            'Controls the overall brightness of the interface.',
                        type: 'select' as const,
                        options: [
                            {
                                value: 'darker',
                                label: 'Darker (OLED)',
                            },
                            {
                                value: 'dark',
                                label: 'Dark (Default)',
                            },
                            {
                                value: 'light',
                                label: 'Light',
                            },
                        ],
                    }}
                    .value=${this.themeCtrl
                        .backgroundShade}
                    @config-change=${this.handleShadeChange}
                ></config-field>

                <div class="color-preview">
                    ${this.renderSwatches()}
                </div>
            </config-section>
        `;
    }

    private renderSwatches() {
        const swatches = [
            { label: 'Base', var: '--yj-bg-base' },
            {
                label: 'Surface',
                var: '--yj-bg-surface',
            },
            {
                label: 'Elevated',
                var: '--yj-bg-elevated',
            },
            {
                label: 'Overlay',
                var: '--yj-bg-overlay',
            },
            { label: 'Accent', var: '--yj-accent' },
        ];

        return swatches.map(
            (s) => html`
                <div class="swatch-group">
                    <div
                        class="swatch"
                        style="background: var(${s.var})"
                    ></div>
                    <span class="swatch-label">
                        ${s.label}
                    </span>
                </div>
            `,
        );
    }

    // --- Library section ---

    private renderLibrarySection() {
        return html`
            <config-section
                heading="Library"
                description="Configure your music library location and scanning behaviour."
            >
                <config-field
                    .schema=${{
                        key: 'libraryDirectory',
                        label: 'Library Directory',
                        description:
                            'The root directory containing your music files.',
                        type: 'directory' as const,
                    }}
                    .value=${this.selectedDirectory}
                    @config-browse=${this.handleDirectoryBrowse}
                ></config-field>

                ${this.directoryChanged
                    ? html`
                          <div class="save-row">
                              <button
                                  class="btn-success"
                                  ?disabled=${this.scanning}
                                  @click=${this.handleSaveDirectory}
                              >
                                  Save Directory
                              </button>
                          </div>
                      `
                    : nothing}

                <config-field
                    .schema=${{
                        key: 'concurrencyMode',
                        label: 'Storage Type',
                        description:
                            'Controls parallel workers during scanning. Auto-detect reads the disk type automatically.',
                        type: 'select' as const,
                        options: [
                            {
                                value: 'auto',
                                label: 'Auto-detect',
                            },
                            {
                                value: 'ssd',
                                label: 'SSD (max parallelism)',
                            },
                            {
                                value: 'hdd',
                                label: 'HDD (reduced I/O)',
                            },
                        ],
                    }}
                    .value=${this.concurrencyMode}
                    @config-change=${this.handleConcurrencyChange}
                ></config-field>

                <div class="scan-actions">
                    <button
                        class="btn-warning"
                        ?disabled=${this.scanning}
                        @click=${this.handleSoftScan}
                    >
                        ${this.scanning
                            ? 'Scanning...'
                            : 'Soft Scan'}
                    </button>
                    <button
                        class="btn-danger"
                        ?disabled=${this.scanning}
                        @click=${this.handleFullRescan}
                    >
                        ${this.scanning
                            ? 'Scanning...'
                            : 'Full Rescan'}
                    </button>
                </div>

                <div
                    class="status-bar ${this.scanning ? 'active' : ''}"
                >
                    ${this.statusMessage || 'Ready.'}
                </div>

                ${this.renderMetrics()}
            </config-section>
        `;
    }

    // --- Metrics ---

    private renderMetricRow(
        label: string,
        value: string,
        highlight = false,
    ) {
        return html`
            <div class="metric-row">
                <span class="metric-label">${label}</span>
                <span
                    class="metric-value ${highlight ? 'highlight' : ''}"
                    >${value}</span
                >
            </div>
        `;
    }

    private renderMetrics() {
        const m = this.metrics;

        if (!m) return nothing;

        const formatEntries = Object.entries(
            m.formatExtraction ?? {},
        ).sort(([, a], [, b]) => b - a);

        const pureDb = Math.max(
            0,
            m.batchCommits - m.coverArtSave,
        );

        return html`
            <div class="metrics-wrapper">
                <div class="metrics-header">
                    <p class="metrics-title">
                        Scan Results
                    </p>
                    <button
                        class="btn-ghost ${this.copied ? 'copied' : ''}"
                        @click=${this.handleCopyMetrics}
                    >
                        ${this.copied
                            ? 'Copied!'
                            : 'Copy'}
                    </button>
                </div>

                ${this.renderMetricRow('Total', fmtNs(m.total), true)}

                <details class="root" open>
                    <summary>File Counts</summary>
                    <div class="counts-grid">
                        <span class="count-label"
                            >Added</span
                        >
                        <span class="count-value"
                            >${m.added}</span
                        >
                        <span class="count-label"
                            >Updated</span
                        >
                        <span class="count-value"
                            >${m.updated}</span
                        >
                        <span class="count-label"
                            >Skipped</span
                        >
                        <span class="count-value"
                            >${m.skipped}</span
                        >
                        <span class="count-label"
                            >Removed</span
                        >
                        <span class="count-value"
                            >${m.removed}</span
                        >
                    </div>
                </details>

                ${this.hasRescanPhases
                    ? html`
                          <details class="root" open>
                              <summary>
                                  Clear Phases
                              </summary>
                              ${this.renderMetricRow('Clear Queue', fmtNs(m.clearQueue))}
                              ${this.renderMetricRow('Clear Database', fmtNs(m.clearDatabase))}
                              ${this.renderMetricRow('Clear Cover Files', fmtNs(m.clearCoverFiles))}
                          </details>
                      `
                    : nothing}

                ${this.renderMetricRow('Load Existing Files', fmtNs(m.loadExisting))}
                ${this.renderMetricRow('Directory Walk', fmtNs(m.walkDuration))}

                <details class="root" open>
                    <summary>
                        Metadata Extraction &mdash;
                        ${fmtNs(m.extractionWallClock)}
                        wall-clock
                    </summary>
                    <p class="metric-note">
                        Per-format and per-operation times
                        are cumulative across
                        ${Object.values(
                            m.formatCount ?? {},
                        ).reduce((a, b) => a + b, 0)}
                        files
                    </p>

                    ${formatEntries.length > 0
                        ? html`
                              <details open>
                                  <summary>
                                      By Format
                                  </summary>
                                  ${formatEntries.map(
                                      ([ext, ms]) =>
                                          this.renderMetricRow(
                                              `${ext} (${m.formatCount?.[ext] ?? 0} files)`,
                                              fmtMs(
                                                  ms,
                                              ),
                                          ),
                                  )}
                              </details>
                          `
                        : nothing}

                    <details>
                        <summary>By Operation</summary>
                        ${this.renderMetricRow('Tag Extraction', fmtNs(m.tagExtraction))}
                        ${this.renderMetricRow('Duration Extraction', fmtNs(m.durationExtraction))}
                    </details>
                </details>

                <details class="root" open>
                    <summary>
                        Database Writes &mdash;
                        ${fmtNs(m.dbWritesWallClock)}
                        wall-clock
                    </summary>
                    ${this.renderMetricRow('Batch Commits', fmtNs(m.batchCommits))}
                    ${this.renderMetricRow('Pure DB Operations', fmtNs(pureDb))}
                    ${this.renderMetricRow('Save Cover Originals', fmtNs(m.coverArtSave))}
                </details>

                <details class="root" open>
                    <summary>
                        Thumbnail Generation &mdash;
                        ${fmtNs(m.thumbnailWallClock)}
                        wall-clock
                    </summary>
                    <p class="metric-note">
                        Generated concurrently; cumulative
                        CPU time may exceed wall-clock
                    </p>
                    ${this.renderMetricRow('Cumulative CPU Time', fmtNs(m.thumbnailGeneration))}
                    ${this.renderMetricRow('Small (_sm)', fmtNs(m.thumbnailSmall))}
                    ${this.renderMetricRow('Medium (_md)', fmtNs(m.thumbnailMedium))}
                    ${this.renderMetricRow('Large (_lg)', fmtNs(m.thumbnailLarge))}
                </details>

                ${this.renderMetricRow('Orphan Cleanup', fmtNs(m.orphanCleanup))}
                ${this.renderMetricRow('Post-Scan Variants', fmtNs(m.postScanVariants))}
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'config-page': ConfigPage;
    }
}

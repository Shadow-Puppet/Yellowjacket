import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { EventsOn, EventsOff } from '@runtime/runtime';
import { Scan, FullRescan } from '@go/library/Library';
import {
    GetLibraryDirectory,
    SetLibraryDirectory,
    GetScanConcurrency,
    SetScanConcurrency,
} from '@go/config/Config';
import { DirectoryPicker } from '@go/frontendutil/FrontendUtil';
import { Events } from '../../events';

/** Go time.Duration serialises as nanoseconds. */
const NS_PER_MS = 1_000_000;

/**
 * Shape of the ScanMetrics struct emitted by the backend.
 * All duration fields are nanoseconds (Go time.Duration JSON).
 * FormatExtraction values are milliseconds (int64 set from Go).
 */
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

/** Format nanoseconds into a human-readable duration. */
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

/** Format milliseconds (used for formatExtraction which stores ms). */
function fmtMs(ms: number): string {
    if (ms <= 0) return '<1ms';
    if (ms < 1000) return `${ms.toFixed(0)}ms`;

    const s = ms / 1000;

    if (s < 60) return `${s.toFixed(2)}s`;

    const m = Math.floor(s / 60);
    const rem = s % 60;

    return `${m}m ${rem.toFixed(1)}s`;
}

/**
 * Build a plain-text representation of scan metrics suitable for
 * pasting into a chat, issue tracker, or notes.
 */
function formatMetricsText(m: ScanMetrics): string {
    const lines: string[] = [];
    const pad = (label: string, value: string) =>
        `  ${label.padEnd(28)} ${value}`;

    lines.push(`Scan Results`);
    lines.push(`${'='.repeat(42)}`);
    lines.push(pad('Total', fmtNs(m.total)));
    lines.push('');

    // File counts.
    lines.push('File Counts');
    lines.push(
        pad('Added', String(m.added)),
        pad('Updated', String(m.updated)),
        pad('Skipped', String(m.skipped)),
        pad('Removed', String(m.removed)),
    );
    lines.push('');

    // Clear phases (full rescan only).
    if (
        m.clearQueue > 0 ||
        m.clearDatabase > 0 ||
        m.clearCoverFiles > 0
    ) {
        lines.push('Clear Phases');
        lines.push(
            pad('Clear Queue', fmtNs(m.clearQueue)),
            pad('Clear Database', fmtNs(m.clearDatabase)),
            pad(
                'Clear Cover Files',
                fmtNs(m.clearCoverFiles),
            ),
        );
        lines.push('');
    }

    lines.push(
        pad('Load Existing Files', fmtNs(m.loadExisting)),
    );
    lines.push(
        pad('Directory Walk', fmtNs(m.walkDuration)),
    );
    lines.push('');

    // Metadata extraction.
    const totalFiles = Object.values(
        m.formatCount ?? {},
    ).reduce((a, b) => a + b, 0);

    lines.push(
        `Metadata Extraction -- ${fmtNs(m.extractionWallClock)} wall-clock`,
    );
    lines.push(
        `  (cumulative across ${totalFiles} files)`,
    );

    const formatEntries = Object.entries(
        m.formatExtraction ?? {},
    ).sort(([, a], [, b]) => b - a);

    if (formatEntries.length > 0) {
        lines.push('  By Format');

        for (const [ext, ms] of formatEntries) {
            const count = m.formatCount?.[ext] ?? 0;

            lines.push(
                pad(
                    `${ext} (${count} files)`,
                    fmtMs(ms),
                ),
            );
        }
    }

    lines.push('  By Operation');
    lines.push(
        pad('Tag Extraction', fmtNs(m.tagExtraction)),
    );
    lines.push(
        pad(
            'Duration Extraction',
            fmtNs(m.durationExtraction),
        ),
    );
    lines.push('');

    // Database writes.
    const pureDb = Math.max(
        0,
        m.batchCommits - m.coverArtSave,
    );

    lines.push(
        `Database Writes -- ${fmtNs(m.dbWritesWallClock)} wall-clock`,
    );
    lines.push(
        pad('Batch Commits', fmtNs(m.batchCommits)),
    );
    lines.push(pad('Pure DB Operations', fmtNs(pureDb)));
    lines.push(
        pad('Save Cover Originals', fmtNs(m.coverArtSave)),
    );
    lines.push('');

    // Thumbnails.
    lines.push(
        `Thumbnail Generation -- ${fmtNs(m.thumbnailWallClock)} wall-clock`,
    );
    lines.push(
        pad(
            'Cumulative CPU Time',
            fmtNs(m.thumbnailGeneration),
        ),
    );
    lines.push(
        pad('Small (_sm)', fmtNs(m.thumbnailSmall)),
    );
    lines.push(
        pad('Medium (_md)', fmtNs(m.thumbnailMedium)),
    );
    lines.push(
        pad('Large (_lg)', fmtNs(m.thumbnailLarge)),
    );
    lines.push('');

    lines.push(
        pad('Orphan Cleanup', fmtNs(m.orphanCleanup)),
    );
    lines.push(
        pad(
            'Post-Scan Variants',
            fmtNs(m.postScanVariants),
        ),
    );

    return lines.join('\n');
}

@customElement('library-manager')
export class LibraryManager extends LitElement {
    @state() private libraryDirectory = '';
    @state() private selectedDirectory = '';
    @state() private scanning = false;
    @state() private statusMessage = '';
    @state() private metrics: ScanMetrics | null = null;
    @state() private copied = false;
    @state() private concurrencyMode = 'auto';

    static override styles = css`
        :host {
            display: block;
            padding: 1.5em;
            color: #e9ecef;
            font-family: system-ui, -apple-system, sans-serif;
            overflow-y: auto;
        }

        h2 {
            margin: 0 0 1em 0;
            font-size: 1.4em;
            font-weight: 600;
            color: #f8f9fa;
        }

        .section {
            margin-bottom: 2em;
            padding: 1.25em;
            background: #2b3035;
            border-radius: 8px;
        }

        .section-title {
            margin: 0 0 0.75em 0;
            font-size: 1em;
            font-weight: 600;
            color: #dee2e6;
        }

        .section-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin: 0 0 0.75em 0;
        }

        .section-header .section-title {
            margin: 0;
        }

        .section-description {
            margin: 0 0 1em 0;
            font-size: 0.85em;
            color: #868e96;
            line-height: 1.4;
        }

        .directory-row {
            display: flex;
            align-items: center;
            gap: 0.75em;
            margin-bottom: 1em;
        }

        .directory-path {
            flex: 1;
            padding: 0.5em 0.75em;
            background: #1a1d20;
            border: 1px solid #495057;
            border-radius: 4px;
            color: #adb5bd;
            font-size: 0.85em;
            font-family: monospace;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            min-height: 1.2em;
        }

        .directory-path.has-value {
            color: #e9ecef;
        }

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

        .btn-primary {
            background: #4263eb;
            color: white;
        }

        .btn-primary:hover:not(:disabled) {
            background: #3b5bdb;
        }

        .btn-success {
            background: #2f9e44;
            color: white;
        }

        .btn-success:hover:not(:disabled) {
            background: #2b8a3e;
        }

        .btn-warning {
            background: #e8590c;
            color: white;
        }

        .btn-warning:hover:not(:disabled) {
            background: #d9480f;
        }

        .btn-danger {
            background: #e03131;
            color: white;
        }

        .btn-danger:hover:not(:disabled) {
            background: #c92a2a;
        }

        .btn-ghost {
            background: transparent;
            color: #868e96;
            padding: 0.3em 0.75em;
            font-size: 0.75em;
            border: 1px solid #495057;
        }

        .btn-ghost:hover:not(:disabled) {
            background: #495057;
            color: #e9ecef;
        }

        .btn-ghost.copied {
            border-color: #2f9e44;
            color: #2f9e44;
        }

        .setting-row {
            display: flex;
            align-items: center;
            gap: 1em;
            font-size: 0.85em;
        }

        .setting-row label {
            color: #adb5bd;
            min-width: 8em;
        }

        .setting-row select {
            padding: 0.4em 0.6em;
            background: #1a1d20;
            border: 1px solid #495057;
            border-radius: 4px;
            color: #e9ecef;
            font-size: 1em;
            font-family: inherit;
            cursor: pointer;
            color-scheme: dark;
        }

        .setting-row select:focus {
            outline: none;
            border-color: #4263eb;
        }

        .setting-row select option {
            background: #2b3035;
            color: #e9ecef;
        }

        .scan-actions {
            display: flex;
            gap: 0.75em;
            flex-wrap: wrap;
        }

        .status-bar {
            margin-top: 1.5em;
            padding: 0.75em 1em;
            background: #1a1d20;
            border-radius: 4px;
            font-size: 0.85em;
            color: #868e96;
            min-height: 1.2em;
        }

        .status-bar.active {
            color: #ffd43b;
        }

        /* --- Metrics tree --- */
        .metrics-section {
            margin-top: 1.5em;
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
            color: #ced4da;
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
            color: #adb5bd;
        }

        .metric-value {
            color: #e9ecef;
            font-family: monospace;
            font-weight: 500;
        }

        .metric-value.highlight {
            color: #ffd43b;
        }

        .metric-note {
            color: #868e96;
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
            color: #adb5bd;
        }

        .count-value {
            color: #e9ecef;
            font-family: monospace;
        }
    `;

    override connectedCallback(): void {
        super.connectedCallback();
        this.loadCurrentDirectory();
        this.loadConcurrencyMode();

        EventsOn(
            Events.LibraryScanStarted,
            this.handleScanStarted,
        );
        EventsOn(
            Events.LibraryScanComplete,
            this.handleScanComplete,
        );
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        EventsOff(Events.LibraryScanStarted);
        EventsOff(Events.LibraryScanComplete);
    }

    private async loadCurrentDirectory(): Promise<void> {
        try {
            const dir = await GetLibraryDirectory();
            this.libraryDirectory = dir;
            this.selectedDirectory = dir;
        } catch (err) {
            console.error(
                'Failed to load library directory:',
                err,
            );
        }
    }

    private async loadConcurrencyMode(): Promise<void> {
        try {
            this.concurrencyMode =
                await GetScanConcurrency();
        } catch (err) {
            console.error(
                'Failed to load scan concurrency:',
                err,
            );
        }
    }

    private handleConcurrencyChange = async (
        e: Event,
    ): Promise<void> => {
        const select = e.target as HTMLSelectElement;
        const mode = select.value;

        try {
            await SetScanConcurrency(mode);
            this.concurrencyMode = mode;
            this.statusMessage =
                'Storage type saved. Takes effect on next scan.';
        } catch (err) {
            this.statusMessage = `Failed to save storage type: ${err}`;
            console.error(
                'Failed to set scan concurrency:',
                err,
            );
        }
    };

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

    private handleSelectDirectory =
        async (): Promise<void> => {
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

    private handleSaveDirectory =
        async (): Promise<void> => {
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
                console.error(
                    'Failed to save directory:',
                    err,
                );
            }
        };

    private handleSoftScan = async (): Promise<void> => {
        try {
            await Scan();
        } catch (err) {
            this.statusMessage = `Scan failed: ${err}`;
            console.error('Soft scan failed:', err);
        }
    };

    private handleFullRescan =
        async (): Promise<void> => {
            const confirmed = confirm(
                'This will delete ALL library data including cover art and re-scan from scratch. Continue?',
            );

            if (!confirmed) return;

            try {
                await FullRescan();
            } catch (err) {
                this.statusMessage = `Full rescan failed: ${err}`;
                console.error(
                    'Full rescan failed:',
                    err,
                );
            }
        };

    private handleCopyMetrics =
        async (): Promise<void> => {
            if (!this.metrics) return;

            try {
                const text = formatMetricsText(
                    this.metrics,
                );

                await navigator.clipboard.writeText(text);
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

    // --- Render helpers ---

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
            <div class="metrics-section section">
                <div class="section-header">
                    <p class="section-title">
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

                <!-- File counts -->
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

                <!-- Full rescan phases -->
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

                <!-- Scan phases -->
                ${this.renderMetricRow('Load Existing Files', fmtNs(m.loadExisting))}
                ${this.renderMetricRow('Directory Walk', fmtNs(m.walkDuration))}

                <!-- Metadata extraction -->
                <details class="root" open>
                    <summary>
                        Metadata Extraction
                        &mdash;
                        ${fmtNs(m.extractionWallClock)}
                        wall-clock
                    </summary>
                    <p class="metric-note">
                        Per-format and per-operation times
                        are cumulative across
                        ${Object.values(
                            m.formatCount ?? {},
                        ).reduce(
                            (a, b) => a + b,
                            0,
                        )}
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
                                              fmtMs(ms),
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

                <!-- Database writes -->
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

                <!-- Thumbnail generation (async) -->
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

    override render() {
        return html`
            <h2>Library Manager</h2>

            <div class="section">
                <p class="section-title">
                    Library Directory
                </p>
                <p class="section-description">
                    Select the root directory containing your
                    music files. Changing this will
                    automatically trigger a scan.
                </p>
                <div class="directory-row">
                    <div
                        class="directory-path ${this.selectedDirectory ? 'has-value' : ''}"
                    >
                        ${this.selectedDirectory ||
                        'No directory selected'}
                    </div>
                    <button
                        class="btn-primary"
                        @click=${this.handleSelectDirectory}
                    >
                        Browse
                    </button>
                    <button
                        class="btn-success"
                        ?disabled=${!this.directoryChanged ||
                        this.scanning}
                        @click=${this.handleSaveDirectory}
                    >
                        Save
                    </button>
                </div>
            </div>

            <div class="section">
                <p class="section-title">
                    Scan Settings
                </p>
                <p class="section-description">
                    Choose how the scanner reads files.
                    Auto-detect reads the disk type
                    automatically. Select HDD if your music
                    is on a spinning disk, or SSD for
                    solid-state storage.
                </p>
                <div class="setting-row">
                    <label for="concurrency-select"
                        >Storage Type</label
                    >
                    <select
                        id="concurrency-select"
                        @change=${this.handleConcurrencyChange}
                    >
                        <option
                            value="auto"
                            ?selected=${this.concurrencyMode === 'auto'}
                        >
                            Auto-detect
                        </option>
                        <option
                            value="ssd"
                            ?selected=${this.concurrencyMode === 'ssd'}
                        >
                            SSD (max parallelism)
                        </option>
                        <option
                            value="hdd"
                            ?selected=${this.concurrencyMode === 'hdd'}
                        >
                            HDD (reduced I/O)
                        </option>
                    </select>
                </div>
            </div>

            <div class="section">
                <p class="section-title">Scan Actions</p>
                <p class="section-description">
                    Soft scan finds new files, skips existing
                    ones, and removes orphaned entries. Full
                    rescan clears the entire database and
                    cover art cache, then re-imports
                    everything.
                </p>
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
            </div>

            <div
                class="status-bar ${this.scanning ? 'active' : ''}"
            >
                ${this.statusMessage || 'Ready.'}
            </div>

            ${this.renderMetrics()}
        `;
    }
}

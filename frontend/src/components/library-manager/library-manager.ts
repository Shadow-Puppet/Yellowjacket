import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';
import {
    FullRescan,
    ScanAllLibraries,
} from '@go/library/Library';
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
interface ScanProgress {
    phase: 'counting' | 'scanning' | 'orphans' | 'thumbnails';
    total: number;
    processed: number;
    added: number;
    skipped: number;
    updated: number;
    libraryId: number;
    libraryName: string;
    queuedCount: number;
}

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
    @state() private scanProgress: ScanProgress | null = null;
    @state() private metrics: ScanMetrics | null = null;
    @state() private copied = false;
    @state() private errorsCopied = false;
    @state() private scanErrors = '';
    @state() private concurrencyMode = 'auto';
    @state() private scanQueuedCount = 0;
    private cancelScanStarted?: () => void;
    private cancelScanProgress?: () => void;
    private cancelScanComplete?: () => void;
    private cancelScanQueued?: () => void;
    private cancelScanQueueDrained?: () => void;

    static override styles = css`
        :host {
            display: block;
            padding: 1.5em;
            color: var(--yj-text-primary, #e9ecef);
            font-family: system-ui, -apple-system, sans-serif;
            overflow-y: auto;
        }

        h2 {
            margin: 0 0 1em 0;
            font-size: 1.4em;
            font-weight: 600;
            color: var(--yj-text-primary, #f8f9fa);
        }

        .section {
            margin-bottom: 2em;
            padding: 1.25em;
            background: var(--yj-bg-surface, #2b3035);
            border-radius: 8px;
        }

        .section-title {
            margin: 0 0 0.75em 0;
            font-size: 1em;
            font-weight: 600;
            color: var(--yj-text-primary, #dee2e6);
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
            color: var(--yj-text-tertiary, #868e96);
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
            background: var(--yj-bg-elevated, #1a1d20);
            border: 1px solid var(--yj-bg-overlay, #495057);
            border-radius: 4px;
            color: var(--yj-text-secondary, #adb5bd);
            font-size: 0.85em;
            font-family: monospace;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            min-height: 1.2em;
        }

        .directory-path.has-value {
            color: var(--yj-text-primary, #e9ecef);
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
            background: var(--yj-info, #4263eb);
            color: white;
        }

        .btn-primary:hover:not(:disabled) {
            background: var(--yj-info-hover, #3b5bdb);
        }

        .btn-success {
            background: var(--yj-success, #2f9e44);
            color: white;
        }

        .btn-success:hover:not(:disabled) {
            background: var(--yj-success-hover, #2b8a3e);
        }

        .btn-warning {
            background: var(--yj-warning, #e8590c);
            color: white;
        }

        .btn-warning:hover:not(:disabled) {
            background: var(--yj-warning-hover, #d9480f);
        }

        .btn-danger {
            background: var(--yj-error, #e03131);
            color: white;
        }

        .btn-danger:hover:not(:disabled) {
            background: var(--yj-error-hover, #c92a2a);
        }

        .btn-ghost {
            background: transparent;
            color: var(--yj-text-tertiary, #868e96);
            padding: 0.3em 0.75em;
            font-size: 0.75em;
            border: 1px solid var(--yj-bg-overlay, #495057);
        }

        .btn-ghost:hover:not(:disabled) {
            background: var(--yj-bg-overlay, #495057);
            color: var(--yj-text-primary, #e9ecef);
        }

        .btn-ghost.copied {
            border-color: var(--yj-success, #2f9e44);
            color: var(--yj-success, #2f9e44);
        }

        .setting-row {
            display: flex;
            align-items: center;
            gap: 1em;
            font-size: 0.85em;
        }

        .setting-row label {
            color: var(--yj-text-secondary, #adb5bd);
            min-width: 8em;
        }

        .setting-row select {
            padding: 0.4em 0.6em;
            background: var(--yj-bg-elevated, #1a1d20);
            border: 1px solid var(--yj-bg-overlay, #495057);
            border-radius: 4px;
            color: var(--yj-text-primary, #e9ecef);
            font-size: 1em;
            font-family: inherit;
            cursor: pointer;
            color-scheme: dark;
        }

        .setting-row select:focus {
            outline: none;
            border-color: var(--yj-info, #4263eb);
        }

        .setting-row select option {
            background: var(--yj-bg-surface, #2b3035);
            color: var(--yj-text-primary, #e9ecef);
        }

        .scan-actions {
            display: flex;
            gap: 0.75em;
            flex-wrap: wrap;
        }

        .status-bar {
            margin-top: 1.5em;
            padding: 0.75em 1em;
            background: var(--yj-bg-elevated, #1a1d20);
            border-radius: 4px;
            font-size: 0.85em;
            color: var(--yj-text-tertiary, #868e96);
            min-height: 1.2em;
        }

        .status-bar.active {
            color: var(--yj-accent, #ffd43b);
        }

        /* Progress bar */
        .progress-info {
            display: flex;
            align-items: baseline;
            gap: 0.5em;
            margin-bottom: 0.5em;
        }

        .progress-label {
            font-weight: 500;
        }

        .progress-detail {
            color: var(--yj-text-tertiary, #868e96);
            font-size: 0.95em;
        }

        .progress-percent {
            margin-left: auto;
            font-variant-numeric: tabular-nums;
        }

        .progress-phase {
            font-weight: 500;
        }

        .progress-track {
            height: 6px;
            background: var(--yj-bg-base, #1a1b1e);
            border-radius: 3px;
            overflow: hidden;
        }

        .progress-fill {
            height: 100%;
            background: var(--yj-accent, #ffd43b);
            border-radius: 3px;
            transition: width 300ms ease;
        }

        /* --- Error block --- */
        .error-block {
            margin-top: 1em;
            border: 1px solid var(--yj-error, #e03131);
            border-radius: 4px;
            overflow: hidden;
        }

        .error-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 0.5em 1em;
            background: color-mix(
                in srgb,
                var(--yj-error, #e03131) 15%,
                var(--yj-bg-elevated, #1a1d20)
            );
        }

        .error-title {
            font-size: 0.8em;
            font-weight: 600;
            color: var(--yj-error, #e03131);
        }

        .error-body {
            max-height: 200px;
            overflow-y: auto;
            padding: 0.75em 1em;
            background: var(--yj-bg-elevated, #1a1d20);
        }

        .error-body pre {
            margin: 0;
            font-size: 0.8em;
            font-family: inherit;
            white-space: pre-wrap;
            word-break: break-word;
            color: var(--yj-text-secondary, #adb5bd);
            line-height: 1.6;
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
            color: var(--yj-text-secondary, #ced4da);
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
    `;

    override connectedCallback(): void {
        super.connectedCallback();
        this.loadCurrentDirectory();
        this.loadConcurrencyMode();

        this.cancelScanStarted = EventsOn(
            Events.LibraryScanStarted,
            this.handleScanStarted,
        );
        this.cancelScanProgress = EventsOn(
            Events.LibraryScanProgress,
            this.handleScanProgress,
        );
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            this.handleScanComplete,
        );
        this.cancelScanQueued = EventsOn(
            Events.LibraryScanQueued,
            this.handleScanQueued,
        );
        this.cancelScanQueueDrained = EventsOn(
            Events.LibraryScanQueueDrained,
            this.handleScanQueueDrained,
        );
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.cancelScanStarted?.();
        this.cancelScanProgress?.();
        this.cancelScanComplete?.();
        this.cancelScanQueued?.();
        this.cancelScanQueueDrained?.();
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
        this.statusMessage = '';
        this.scanProgress = null;
        this.metrics = null;
        this.copied = false;
        this.scanErrors = '';
        this.errorsCopied = false;
    };

    private handleScanProgress = (
        progress?: ScanProgress,
    ): void => {
        if (progress) {
            this.scanProgress = progress;
            this.scanQueuedCount =
                progress.queuedCount ?? 0;
        }
    };

    private handleScanComplete = (
        metrics?: ScanMetrics,
    ): void => {
        this.scanProgress = null;

        // If queue still has entries, don't fully reset — next scan will fire ScanStarted
        if (this.scanQueuedCount > 0) {
            if (metrics) {
                this.metrics = metrics;
            }
            return;
        }

        this.scanning = false;
        this.statusMessage = 'Scan complete.';

        if (metrics) {
            this.metrics = metrics;
        }
    };

    private handleScanQueued = (): void => {
        this.scanQueuedCount++;
    };

    private handleScanQueueDrained = (): void => {
        this.scanQueuedCount = 0;
        this.scanning = false;
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

    private handleScanAll = async (): Promise<void> => {
        try {
            await ScanAllLibraries();
        } catch (err) {
            this.statusMessage =
                'Scan all libraries completed with errors.';
            this.scanErrors = String(err);
            console.error(
                'Scan all libraries failed:',
                err,
            );
        }
    };

    private handleSoftScan = async (): Promise<void> => {
        try {
            await ScanAllLibraries();
        } catch (err) {
            this.statusMessage =
                'Scan completed with errors.';
            this.scanErrors = String(err);
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
                this.statusMessage =
                    'Full rescan completed with errors.';
                this.scanErrors = String(err);
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

    private handleCopyErrors =
        async (): Promise<void> => {
            if (!this.scanErrors) return;

            try {
                await navigator.clipboard.writeText(
                    this.scanErrors,
                );
                this.errorsCopied = true;

                setTimeout(() => {
                    this.errorsCopied = false;
                }, 2000);
            } catch (err) {
                console.error(
                    'Failed to copy errors:',
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

    private renderScanProgress() {
        const p = this.scanProgress;

        if (!p) return nothing;

        const libraryPrefix = p.libraryName
            ? `Scanning: ${p.libraryName}`
            : '';

        if (p.phase === 'counting') {
            return html`
                <div class="progress-phase">
                    ${libraryPrefix
                        ? html`<strong>${libraryPrefix}</strong> \u2014 `
                        : ''}
                    Counting files\u2026
                </div>
                ${p.queuedCount > 0
                    ? html`<div class="progress-detail">
                          ${p.queuedCount}
                          ${p.queuedCount === 1 ? 'library' : 'libraries'}
                          queued
                      </div>`
                    : nothing}
            `;
        }

        const percent =
            p.total > 0
                ? Math.min(
                      100,
                      Math.round(
                          (p.processed / p.total) * 100,
                      ),
                  )
                : 0;

        const phaseLabel: Record<string, string> = {
            scanning: 'Scanning',
            orphans: 'Cleaning up',
            thumbnails: 'Generating thumbnails',
        };

        const baseLabel =
            phaseLabel[p.phase] ?? 'Scanning';
        const label = p.libraryName
            ? `${baseLabel}: ${p.libraryName}`
            : baseLabel;

        const parts: string[] = [];

        if (p.added > 0)
            parts.push(`${p.added.toLocaleString()} new`);
        if (p.updated > 0)
            parts.push(
                `${p.updated.toLocaleString()} updated`,
            );
        if (p.skipped > 0)
            parts.push(
                `${p.skipped.toLocaleString()} skipped`,
            );

        const detail =
            p.phase === 'scanning' && p.total > 0
                ? html`<span class="progress-detail">
                      ${p.processed.toLocaleString()} /
                      ${p.total.toLocaleString()} files${parts.length
                          ? ` (${parts.join(', ')})`
                          : ''}
                  </span>`
                : nothing;

        return html`
            <div class="progress-info">
                <span class="progress-label">
                    ${label}\u2026
                </span>
                ${detail}
                <span class="progress-percent">
                    ${percent}%
                </span>
            </div>
            <div class="progress-track">
                <div
                    class="progress-fill"
                    style="width: ${percent}%"
                ></div>
            </div>
            ${p.queuedCount > 0
                ? html`<div class="progress-detail">
                      ${p.queuedCount}
                      ${p.queuedCount === 1 ? 'library' : 'libraries'}
                      queued
                  </div>`
                : nothing}
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
                    <button
                        class="btn-primary"
                        ?disabled=${this.scanning}
                        @click=${this.handleScanAll}
                    >
                        ${this.scanning
                            ? 'Scanning...'
                            : 'Scan All Libraries'}
                    </button>
                </div>
            </div>

            <div
                class="status-bar ${this.scanning ? 'active' : ''}"
            >
                ${this.scanProgress
                    ? this.renderScanProgress()
                    : this.statusMessage || 'Ready.'}
            </div>

            ${this.scanErrors
                ? html`
                      <div class="error-block">
                          <div class="error-header">
                              <span class="error-title">
                                  Scan Errors
                              </span>
                              <button
                                  class="btn-ghost ${this.errorsCopied ? 'copied' : ''}"
                                  @click=${this.handleCopyErrors}
                              >
                                  ${this.errorsCopied
                                      ? 'Copied!'
                                      : 'Copy'}
                              </button>
                          </div>
                          <div class="error-body">
                              <pre>${this.scanErrors}</pre>
                          </div>
                      </div>
                  `
                : ''}

            ${this.renderMetrics()}
        `;
    }
}

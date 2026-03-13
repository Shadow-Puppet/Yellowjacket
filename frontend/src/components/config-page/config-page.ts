import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { repeat } from 'lit/directives/repeat.js';
import { EventsOn } from '@runtime/runtime';
import {
    FullRescan,
    CancelCurrentScan,
    CancelAllScans,
    ScanAllLibraries,
    ScanLibrary,
    PauseScan,
    ResumeScan,
    AddLibrary,
    RenameLibrary,
    RemoveLibrary,
    GetRemovalImpact,
    GetAllLibrariesWithTrackCounts,
} from '@go/library/Library';
import {
    GetScanConcurrency,
    SetScanConcurrency,
} from '@go/config/Config';
import { DirectoryPicker } from '@go/frontendutil/FrontendUtil';
import type { library } from '@go/models';
import { ThemeController } from '@store/controllers/theme-controller';
import { TrackListController } from '@store/controllers/tracklist-controller';
import { FavoritesController } from '@store/controllers/favorites-controller';
import { GetAllPlaylists } from '@go/playlist/Service';
import type { playlist } from '@go/models';
import { Events } from '../../events';
import type { ConfigFieldChangeEvent } from './config-field';
import type { BackgroundShade } from '@store/theme-store';
import type { IconStyle } from '@store/favorites-store';
import {
    COLUMN_DEFS,
    ALL_COLUMN_IDS,
} from '@components/track-list/columns';

import './config-field';
import './config-section';
import './shortcut-capture';
import { shortcutsStore } from '../../store/shortcuts-store';
import { ShortcutsController } from '../../store/controllers/shortcuts-controller';

const SCROLL_STORAGE_KEY = 'yj-now-playing-scroll-mode';
const SCROLL_CHANGE_EVENT = 'yj-scroll-mode-changed';

// ===================================================================
// Scan metrics types and helpers (carried over from library-manager)
// ===================================================================

const NS_PER_MS = 1_000_000;

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
    cancelled: boolean;
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

    // --- Track-list column config controller ---
    private trackListCtrl = new TrackListController(this);

    // --- Favorites controller ---
    private favCtrl = new FavoritesController(this);

    // --- Shortcuts controller ---
    private shortcutsCtrl = new ShortcutsController(this);

    // --- Shortcut metadata for UI grouping ---
    private static readonly SHORTCUT_META: Record<
        string,
        {
            label: string;
            category: string;
            scope: string;
            defaultKey: string;
        }
    > = {
        'player.playPause': {
            label: 'Play / Pause',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Space',
        },
        'player.next': {
            label: 'Next Track',
            category: 'Player',
            scope: 'global',
            defaultKey: 'N',
        },
        'player.previous': {
            label: 'Previous Track',
            category: 'Player',
            scope: 'global',
            defaultKey: 'P',
        },
        'player.volumeUp': {
            label: 'Volume Up',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Up',
        },
        'player.volumeDown': {
            label: 'Volume Down',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Down',
        },
        'player.seekForward': {
            label: 'Seek Forward',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Right',
        },
        'player.seekBack': {
            label: 'Seek Back',
            category: 'Player',
            scope: 'global',
            defaultKey: 'Left',
        },
        'player.shuffle': {
            label: 'Toggle Shuffle',
            category: 'Player',
            scope: 'global',
            defaultKey: 'S',
        },
        'player.repeat': {
            label: 'Cycle Repeat',
            category: 'Player',
            scope: 'global',
            defaultKey: 'R',
        },
        'player.mute': {
            label: 'Toggle Mute',
            category: 'Player',
            scope: 'global',
            defaultKey: 'M',
        },
        'nav.search': {
            label: 'Focus Search',
            category: 'Navigation',
            scope: 'global',
            defaultKey: '/',
        },
        'nav.searchAlt': {
            label: 'Focus Search (Alt)',
            category: 'Navigation',
            scope: 'global',
            defaultKey: 'Ctrl+F',
        },
        'nav.queue': {
            label: 'Toggle Queue',
            category: 'Navigation',
            scope: 'global',
            defaultKey: 'Q',
        },
        'app.selectAll': {
            label: 'Select All',
            category: 'App',
            scope: 'global',
            defaultKey: 'Ctrl+A',
        },
        'tracklist.play': {
            label: 'Play Selected',
            category: 'Navigation',
            scope: 'panel:track-list',
            defaultKey: 'Enter',
        },
        'tracklist.delete': {
            label: 'Remove Selected',
            category: 'Navigation',
            scope: 'panel:track-list',
            defaultKey: 'Delete',
        },
    };

    // --- Now Playing state ---
    @state() private scrollMode = 'hover';

    // --- Favorites state ---
    @state() private playlists: playlist.Summary[] = [];

    // --- Library state ---
    @state() private libraries: library.Info[] = [];
    @state() private editingLibraryId: number | null = null;
    @state() private editingName = '';
    @state() private removingLibraryId: number | null = null;
    @state() private removalImpact: library.RemovalImpact | null = null;
    @state() private isRemoving = false;
    @state() private toastMessage = '';
    @state() private toastVisible = false;
    @state() private activeMenuId: number | null = null;
    @state() private scanning = false;
    @state() private statusMessage = '';
    @state() private scanProgress: ScanProgress | null = null;
    @state() private metrics: ScanMetrics | null = null;
    @state() private copied = false;
    @state() private errorsCopied = false;
    @state() private scanErrors = '';
    @state() private concurrencyMode = 'auto';
    @state() private scanPaused = false;
    @state() private showCancelDialog = false;
    @state() private cancelMetrics: { added: number } | null = null;
    @state() private scanQueuedCount = 0;
    @state() private shortcutConflict: {
        newAction: string;
        newKey: string;
        existingAction: string;
    } | null = null;

    private toastTimer?: ReturnType<typeof setTimeout>;
    private cancelScanStarted?: () => void;
    private cancelScanProgress?: () => void;
    private cancelScanComplete?: () => void;
    private cancelScanPaused?: () => void;
    private cancelScanResumed?: () => void;
    private cancelScanCancelled?: () => void;
    private cancelScanQueued?: () => void;
    private cancelScanQueueDrained?: () => void;
    private cancelLibraryAdded?: () => void;
    private cancelLibraryRenamed?: () => void;
    private cancelLibraryRemoved?: () => void;

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

        /* Error block */
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
                var(--yj-bg-elevated, #343a40)
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
            background: var(--yj-bg-elevated, #343a40);
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

        /* Track list column configurator */
        .column-list {
            list-style: none;
            padding: 0;
            margin: 0;
        }

        .column-item {
            display: flex;
            align-items: center;
            gap: 0.5em;
            padding: 0.5em 0.75em;
            border-bottom: 1px solid
                var(--yj-border-subtle, #333);
            font-size: 0.85em;
        }

        .column-item:last-child {
            border-bottom: none;
        }

        .column-item.enabled {
            color: var(--yj-text-primary, #fff);
        }

        .column-item.disabled {
            color: var(--yj-text-tertiary, #888);
        }

        .column-toggle {
            cursor: pointer;
            accent-color: var(
                --yj-accent,
                #ffd43b
            );
        }

        .column-label {
            flex: 1;
        }

        .column-arrows {
            display: flex;
            gap: 0.15em;
            margin-left: auto;
        }

        .column-arrow-btn {
            background: none;
            border: 1px solid transparent;
            border-radius: 3px;
            color: var(--yj-text-tertiary, #888);
            cursor: pointer;
            font-size: 0.65em;
            line-height: 1;
            padding: 0.2em 0.35em;
            transition:
                color 0.15s,
                border-color 0.15s;
        }

        .column-arrow-btn:hover {
            color: var(--yj-text-primary, #fff);
            border-color: var(
                --yj-border-subtle,
                #333
            );
        }

        /* Cancel confirmation dialog */
        .cancel-dialog-overlay {
            position: fixed;
            inset: 0;
            background: rgba(0, 0, 0, 0.6);
            display: flex;
            align-items: center;
            justify-content: center;
            z-index: 1000;
        }

        .cancel-dialog {
            background: var(
                --yj-bg-surface,
                #2a2a2a
            );
            border: 1px solid
                var(--yj-border, #444);
            border-radius: 8px;
            padding: 24px;
            max-width: 420px;
            width: 90%;
        }

        .cancel-dialog-title {
            font-size: var(--yj-text-lg, 18px);
            font-weight: 600;
            margin-bottom: 12px;
        }

        .cancel-dialog-message {
            font-size: var(--yj-text-sm, 14px);
            color: var(
                --yj-text-secondary,
                #aaa
            );
            margin-bottom: 20px;
        }

        .cancel-dialog-actions {
            display: flex;
            gap: 8px;
            justify-content: flex-end;
        }

        .btn-primary {
            background: var(
                --yj-accent,
                #ffd43b
            );
            color: var(--yj-bg-base, #1a1b1e);
            font-weight: 600;
        }

        .btn-primary:hover:not(:disabled) {
            filter: brightness(1.1);
        }

        .status-bar.paused {
            color: var(--yj-accent, #ffd43b);
        }

        /* Library management */
        .library-list {
            display: flex;
            flex-direction: column;
            gap: 0;
        }

        .library-row {
            display: flex;
            align-items: center;
            gap: 0.75em;
            padding: 0.6em 0.5em;
            border-bottom: 1px solid var(--yj-border-subtle, #333);
        }

        .library-row:last-child {
            border-bottom: none;
        }

        .library-row:hover {
            background: var(--yj-bg-elevated, #343a40);
            border-radius: 4px;
        }

        .library-name {
            flex: 1;
            cursor: pointer;
            font-size: 0.9em;
            font-weight: 500;
            min-width: 0;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }

        .library-name:hover {
            color: var(--yj-accent, #ffd43b);
        }

        .library-path {
            color: var(--yj-text-tertiary, #868e96);
            font-size: var(--yj-text-sm, 13px);
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            max-width: 300px;
        }

        .library-count {
            color: var(--yj-text-tertiary, #868e96);
            font-size: var(--yj-text-sm, 13px);
            white-space: nowrap;
            flex-shrink: 0;
        }

        .edit-input {
            flex: 1;
            padding: 0.3em 0.5em;
            background: var(--yj-bg-elevated, #343a40);
            border: 1px solid var(--yj-accent, #ffd43b);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            font-size: 0.9em;
            font-family: inherit;
            outline: none;
        }

        .overflow-wrapper {
            position: relative;
            flex-shrink: 0;
        }

        .overflow-btn {
            cursor: pointer;
            border: none;
            background: transparent;
            color: var(--yj-text-tertiary, #868e96);
            font-size: 1.1em;
            padding: 0.2em 0.4em;
            letter-spacing: 2px;
            border-radius: 4px;
        }

        .overflow-btn:hover {
            background: var(--yj-bg-overlay, #495057);
            color: var(--yj-text-primary, #fff);
        }

        .overflow-menu {
            position: absolute;
            top: 100%;
            right: 0;
            background: var(--yj-bg-surface, #2a2a2a);
            border: 1px solid var(--yj-border, #444);
            border-radius: 6px;
            box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
            z-index: 100;
            min-width: 120px;
            padding: 4px 0;
        }

        .overflow-item {
            padding: 0.5em 1em;
            font-size: var(--yj-text-sm, 13px);
            cursor: pointer;
            white-space: nowrap;
        }

        .overflow-item:hover {
            background: var(--yj-bg-elevated, #343a40);
        }

        .overflow-item--danger {
            color: var(--yj-error, #e03131);
        }

        .overflow-item--danger:hover {
            background: color-mix(
                in srgb,
                var(--yj-error, #e03131) 10%,
                var(--yj-bg-elevated, #343a40)
            );
        }

        .add-library-btn {
            margin-top: 0.75em;
            margin-bottom: 1em;
        }

        .toast {
            position: fixed;
            bottom: 80px;
            left: 50%;
            transform: translateX(-50%);
            background: var(--yj-bg-surface, #2a2a2a);
            color: var(--yj-text-primary, #fff);
            padding: 0.75em 1.5em;
            border-radius: 8px;
            box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
            font-size: var(--yj-text-sm, 13px);
            z-index: 2000;
            border: 1px solid var(--yj-border, #444);
            animation: toast-in 0.2s ease-out;
        }

        @keyframes toast-in {
            from {
                opacity: 0;
                transform: translateX(-50%)
                    translateY(10px);
            }
            to {
                opacity: 1;
                transform: translateX(-50%)
                    translateY(0);
            }
        }

        .spinner {
            display: inline-block;
            width: 14px;
            height: 14px;
            border: 2px solid rgba(255, 255, 255, 0.3);
            border-top-color: #fff;
            border-radius: 50%;
            animation: spin 0.6s linear infinite;
            vertical-align: middle;
            margin-right: 4px;
        }

        @keyframes spin {
            to {
                transform: rotate(360deg);
            }
        }

        /* Keyboard shortcuts section */
        .shortcut-category {
            margin-bottom: 16px;
        }
        .shortcut-category-header {
            font-size: var(--yj-text-sm, 13px);
            font-weight: 600;
            color: var(--yj-text-secondary, #aaa);
            text-transform: uppercase;
            letter-spacing: 0.5px;
            margin-bottom: 8px;
            padding-bottom: 4px;
            border-bottom: 1px solid
                var(--yj-border, #444);
        }
        .shortcut-row {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 6px 0;
            gap: 16px;
        }
        .shortcut-label {
            font-size: var(--yj-text-sm, 13px);
            color: var(--yj-text-primary, #eee);
        }
        .shortcut-scope {
            font-size: var(--yj-text-xs, 11px);
            color: var(
                --yj-text-tertiary,
                #888
            );
            margin-left: 4px;
        }
        .shortcut-actions {
            margin-top: 16px;
            display: flex;
            justify-content: flex-end;
        }
        .conflict-banner {
            margin-top: 12px;
            padding: 12px;
            background: rgba(255, 165, 0, 0.1);
            border: 1px solid
                rgba(255, 165, 0, 0.4);
            border-radius: 6px;
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 12px;
        }
        .conflict-text {
            font-size: var(--yj-text-sm, 13px);
        }
        .conflict-actions {
            display: flex;
            gap: 8px;
            flex-shrink: 0;
        }

    `;

    // ===================================================================
    // LIFECYCLE
    // ===================================================================

    override connectedCallback(): void {
        super.connectedCallback();
        void this.loadLibraries();
        void this.loadPlaylists();
        this.scrollMode =
            localStorage.getItem(SCROLL_STORAGE_KEY) || 'hover';

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
        this.cancelScanPaused = EventsOn(
            Events.LibraryScanPaused,
            this.handleScanPaused,
        );
        this.cancelScanResumed = EventsOn(
            Events.LibraryScanResumed,
            this.handleScanResumed,
        );
        this.cancelScanCancelled = EventsOn(
            Events.LibraryScanCancelled,
            this.handleScanCancelled,
        );
        this.cancelScanQueued = EventsOn(
            Events.LibraryScanQueued,
            this.handleScanQueued,
        );
        this.cancelScanQueueDrained = EventsOn(
            Events.LibraryScanQueueDrained,
            this.handleScanQueueDrained,
        );
        this.cancelLibraryAdded = EventsOn(
            Events.LibraryAdded,
            () => void this.loadLibraries(),
        );
        this.cancelLibraryRenamed = EventsOn(
            Events.LibraryRenamed,
            () => void this.loadLibraries(),
        );
        this.cancelLibraryRemoved = EventsOn(
            Events.LibraryRemoved,
            () => void this.loadLibraries(),
        );

        document.addEventListener('click', this.handleDocumentClick);
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.cancelScanStarted?.();
        this.cancelScanProgress?.();
        this.cancelScanComplete?.();
        this.cancelScanPaused?.();
        this.cancelScanResumed?.();
        this.cancelScanCancelled?.();
        this.cancelScanQueued?.();
        this.cancelScanQueueDrained?.();
        this.cancelLibraryAdded?.();
        this.cancelLibraryRenamed?.();
        this.cancelLibraryRemoved?.();

        document.removeEventListener('click', this.handleDocumentClick);

        if (this.toastTimer) clearTimeout(this.toastTimer);
    }

    private async loadLibraries(): Promise<void> {
        try {
            const [libs, mode] = await Promise.all([
                GetAllLibrariesWithTrackCounts(),
                GetScanConcurrency(),
            ]);

            this.libraries = libs ?? [];
            this.concurrencyMode = mode;
        } catch (err) {
            console.error(
                'Failed to load libraries:',
                err,
            );
        }
    }

    // ===================================================================
    // LIBRARY HANDLERS
    // ===================================================================

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
        this.scanPaused = false;
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
        this.scanPaused = false;
    };

    private handleScanPaused = (): void => {
        this.scanPaused = true;
    };

    private handleScanResumed = (): void => {
        this.scanPaused = false;
    };

    private handleScanCancelled = (
        metrics?: ScanMetrics,
    ): void => {
        this.scanning = false;
        this.scanPaused = false;
        this.scanProgress = null;

        if (metrics) {
            this.metrics = metrics;
            this.statusMessage =
                metrics.cancelled
                    ? 'Scan cancelled.'
                    : 'Scan complete.';
        } else {
            this.statusMessage = 'Scan cancelled.';
        }
    };

    private handlePauseScan = (): void => {
        PauseScan();
    };

    private handleResumeScan = (): void => {
        ResumeScan();
    };

    private handleCancelScan = (): void => {
        const added =
            this.scanProgress?.added ?? 0;
        this.cancelMetrics = { added };
        this.showCancelDialog = true;
    };

    private handleCancelKeep = (): void => {
        this.showCancelDialog = false;
        this.cancelMetrics = null;
        CancelCurrentScan();
    };

    private handleCancelDiscard = (): void => {
        this.showCancelDialog = false;
        this.cancelMetrics = null;
        CancelCurrentScan();
        this.statusMessage =
            'Scan cancelled. Partial results discarded \u2014 run Full Rescan for a clean library.';
    };

    private handleCancelAll = (): void => {
        this.showCancelDialog = false;
        this.cancelMetrics = null;
        CancelAllScans();
        this.statusMessage = 'All scanning cancelled.';
    };

    private handleCancelDialogDismiss = (): void => {
        this.showCancelDialog = false;
        this.cancelMetrics = null;
    };

    private handleAddLibrary = async (): Promise<void> => {
        try {
            const dir = await DirectoryPicker();

            if (dir) {
                await AddLibrary(dir);
            }
        } catch (err) {
            console.error(
                'Failed to add library:',
                err,
            );
        }
    };

    private handleStartRename = (id: number, name: string): void => {
        this.editingLibraryId = id;
        this.editingName = name;
        this.activeMenuId = null;
    };

    private handleRenameKeyDown = async (e: KeyboardEvent): Promise<void> => {
        if (e.key === 'Enter') {
            e.preventDefault();

            if (this.editingLibraryId !== null && this.editingName.trim()) {
                try {
                    await RenameLibrary(this.editingLibraryId, this.editingName.trim());
                } catch (err) {
                    console.error('Failed to rename library:', err);
                }
            }

            this.editingLibraryId = null;
            this.editingName = '';
        } else if (e.key === 'Escape') {
            this.editingLibraryId = null;
            this.editingName = '';
        }
    };

    private handleRenameInput = (e: InputEvent): void => {
        this.editingName = (e.target as HTMLInputElement).value;
    };

    private handleRescanLibrary = (id: number): void => {
        this.activeMenuId = null;
        void ScanLibrary(id);
    };

    private handleRemoveClick = async (id: number): Promise<void> => {
        this.activeMenuId = null;

        try {
            const impact = await GetRemovalImpact(id);

            this.removalImpact = impact;
            this.removingLibraryId = id;
        } catch (err) {
            console.error('Failed to get removal impact:', err);
        }
    };

    private handleConfirmRemove = async (): Promise<void> => {
        if (this.removingLibraryId === null) return;

        const id = this.removingLibraryId;
        const lib = this.libraries.find((l) => l.id === id);
        const libName = lib?.name ?? 'Library';

        this.isRemoving = true;

        try {
            const summary = await RemoveLibrary(id);

            this.removingLibraryId = null;
            this.removalImpact = null;
            this.isRemoving = false;
            this.showToast(
                `Removed '${libName}' (${summary?.tracksDeleted ?? 0} tracks deleted)`,
            );
        } catch (err) {
            this.isRemoving = false;
            console.error('Failed to remove library:', err);
        }
    };

    private handleCancelRemove = (): void => {
        this.removingLibraryId = null;
        this.removalImpact = null;
        this.isRemoving = false;
    };

    private toggleOverflowMenu = (id: number, e: Event): void => {
        e.stopPropagation();
        this.activeMenuId = this.activeMenuId === id ? null : id;
    };

    private handleDocumentClick = (): void => {
        if (this.activeMenuId !== null) {
            this.activeMenuId = null;
        }
    };

    private showToast(message: string): void {
        this.toastMessage = message;
        this.toastVisible = true;

        if (this.toastTimer) clearTimeout(this.toastTimer);

        this.toastTimer = setTimeout(() => {
            this.toastVisible = false;
        }, 4000);
    }

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

    private handleScanAll = async (): Promise<void> => {
        try {
            await ScanAllLibraries();
        } catch (err) {
            this.statusMessage =
                'Scan all libraries completed with errors.';
            this.scanErrors = String(err);
        }
    };

    private handleSoftScan = async (): Promise<void> => {
        try {
            await ScanAllLibraries();
        } catch (err) {
            this.statusMessage =
                'Scan completed with errors.';
            this.scanErrors = String(err);
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
            this.statusMessage =
                'Full rescan completed with errors.';
            this.scanErrors = String(err);
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
    // FAVORITES HANDLERS
    // ===================================================================

    private async loadPlaylists(): Promise<void> {
        try {
            this.playlists =
                await GetAllPlaylists();
        } catch (err) {
            console.error(
                'Failed to load playlists:',
                err,
            );
        }
    }

    private handleFavIconStyleChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        const style = String(
            e.detail.value,
        ) as IconStyle;

        this.favCtrl
            .setIconStyle(style)
            .catch((err: unknown) => {
                console.error(
                    'Failed to set icon style:',
                    err,
                );
            });
    };

    private handleFavPlaylistChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        const id = Number(e.detail.value);

        if (Number.isNaN(id)) return;

        this.favCtrl
            .setDefaultPlaylist(id)
            .catch((err: unknown) => {
                console.error(
                    'Failed to set default playlist:',
                    err,
                );
            });
    };

    private handlePinDefaultChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        const pin = Boolean(e.detail.value);

        this.favCtrl
            .setPinDefault(pin)
            .catch((err: unknown) => {
                console.error(
                    'Failed to set pin default:',
                    err,
                );
            });
    };

    // ===================================================================
    // KEYBOARD SHORTCUTS HANDLERS
    // ===================================================================

    private async handleShortcutChange(
        e: CustomEvent<{ action: string; key: string }>,
    ) {
        const { action, key } = e.detail;

        // Check for conflict — find any other action with the same key in the same or overlapping scope
        const meta = ConfigPage.SHORTCUT_META[action];
        const conflict = shortcutsStore.findConflict(
            key,
            meta?.scope ?? 'global',
            action,
        );

        if (conflict) {
            // Show conflict warning
            this.shortcutConflict = {
                newAction: action,
                newKey: key,
                existingAction: conflict.action,
            };
            return;
        }

        // No conflict — save directly
        await shortcutsStore.updateBinding(action, key);
    }

    private async handleConflictOverwrite() {
        if (!this.shortcutConflict) return;
        const { newAction, newKey, existingAction } =
            this.shortcutConflict;
        // Unbind the existing action
        await shortcutsStore.updateBinding(
            existingAction,
            '',
        );
        // Set the new binding
        await shortcutsStore.updateBinding(
            newAction,
            newKey,
        );
        this.shortcutConflict = null;
    }

    private handleConflictCancel() {
        this.shortcutConflict = null;
    }

    private async handleResetAllShortcuts() {
        await shortcutsStore.resetAll();
    }

    // ===================================================================
    // NOW PLAYING HANDLERS
    // ===================================================================

    private handleScrollModeChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        const mode = String(e.detail.value);
        this.scrollMode = mode;
        localStorage.setItem(SCROLL_STORAGE_KEY, mode);
        window.dispatchEvent(
            new CustomEvent(SCROLL_CHANGE_EVENT),
        );
    };

    // ===================================================================
    // TRACK LIST COLUMN HANDLERS
    // ===================================================================

    private handleColumnToggle = (
        columnId: string,
    ): void => {
        const current = [
            ...this.trackListCtrl.columnIds,
        ];
        const idx = current.indexOf(columnId);

        if (idx >= 0) {
            // Don't allow removing the last column.
            if (current.length <= 1) return;

            current.splice(idx, 1);
        } else {
            current.push(columnId);
        }

        this.trackListCtrl
            .setColumns(current)
            .catch((err: unknown) => {
                console.error(
                    'Failed to update columns:',
                    err,
                );
            });
    };

    /**
     * Builds the merged column order: enabled IDs first
     * (in their configured display order), then disabled
     * IDs (in default static order).
     */
    private getMergedColumnOrder(): string[] {
        const enabledIds = [
            ...this.trackListCtrl.columnIds,
        ];

        const disabledIds = ALL_COLUMN_IDS.filter(
            (id) => !enabledIds.includes(id),
        );

        return [...enabledIds, ...disabledIds];
    }

    private handleColumnMove = (
        columnId: string,
        direction: 'up' | 'down',
    ): void => {
        const order = this.getMergedColumnOrder();
        const idx = order.indexOf(columnId);

        if (idx < 0) return;

        const targetIdx =
            direction === 'up' ? idx - 1 : idx + 1;

        if (targetIdx < 0 || targetIdx >= order.length)
            return;

        // Swap adjacent items in the full list.
        const tmp = order[targetIdx]!;
        order[targetIdx] = order[idx]!;
        order[idx] = tmp;

        // Keep only the enabled columns, preserving
        // the new order.
        const enabledSet = new Set(
            this.trackListCtrl.columnIds,
        );
        const newEnabled = order.filter((id) =>
            enabledSet.has(id),
        );

        this.trackListCtrl
            .setColumns(newEnabled)
            .catch((err: unknown) => {
                console.error(
                    'Failed to reorder columns:',
                    err,
                );
            });
    };

    // ===================================================================
    // COMPUTED
    // ===================================================================

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

            ${this.renderNowPlayingSection()}
            ${this.renderThemeSection()}
            ${this.renderFavoritesSection()}
            ${this.renderTrackListSection()}
            ${this.renderShortcutsSection()}
            ${this.renderLibrarySection()}
        `;
    }

    // --- Now Playing section ---

    private renderNowPlayingSection() {
        return html`
            <config-section
                heading="Now Playing"
                description="Configure the now-playing display in the bottom bar."
            >
                <config-field
                    .schema=${{
                        key: 'scrollMode',
                        label: 'Text Scroll Behaviour',
                        description:
                            'How overflowing track title and artist text is handled when it exceeds the available width.',
                        type: 'select' as const,
                        options: [
                            {
                                value: 'hover',
                                label: 'Scroll on Hover (Default)',
                            },
                            {
                                value: 'always',
                                label: 'Always Scroll',
                            },
                            {
                                value: 'never',
                                label: 'Never (Ellipsis)',
                            },
                        ],
                    }}
                    .value=${this.scrollMode}
                    @config-change=${this.handleScrollModeChange}
                ></config-field>
            </config-section>
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

    // --- Favorites section ---

    private renderFavoritesSection() {
        const playlistOptions =
            this.playlists.map((p) => ({
                value: String(p.ID),
                label: p.Name,
            }));

        return html`
            <config-section
                heading="Favorites"
                description="Configure the default playlist used for quick-favouriting tracks."
            >
                <config-field
                    .schema=${{
                        key: 'favoritesPlaylist',
                        label: 'Default Playlist',
                        description:
                            'The playlist used when toggling the favourite icon on tracks.',
                        type: 'select' as const,
                        options: playlistOptions,
                    }}
                    .value=${String(
                        this.favCtrl.playlistId,
                    )}
                    @config-change=${this.handleFavPlaylistChange}
                ></config-field>

                <config-field
                    .schema=${{
                        key: 'favoritesIcon',
                        label: 'Icon Style',
                        description:
                            'Choose heart or star for the favourite indicator.',
                        type: 'select' as const,
                        options: [
                            {
                                value: 'heart',
                                label: '\u2665 Heart',
                            },
                            {
                                value: 'star',
                                label: '\u2605 Star',
                            },
                        ],
                    }}
                    .value=${this.favCtrl
                        .iconStyle}
                    @config-change=${this.handleFavIconStyleChange}
                ></config-field>

                <config-field
                    .schema=${{
                        key: 'pinDefaultPlaylist',
                        label: 'Pin to Top',
                        description:
                            'Always show the default playlist first, regardless of sort order.',
                        type: 'toggle' as const,
                    }}
                    .value=${this.favCtrl.pinDefault}
                    @config-change=${this.handlePinDefaultChange}
                ></config-field>
            </config-section>
        `;
    }

    // --- Track list section ---

    private renderTrackListSection() {
        const enabledIds = this.trackListCtrl.columnIds;
        const order = this.getMergedColumnOrder();

        return html`
            <config-section
                heading="Track List Columns"
                description="Choose which columns are visible and set their display order."
            >
                <ul class="column-list">
                    ${repeat(order, (id) => id, (id, idx) => {
                        const checked =
                            enabledIds.includes(id);
                        const onlyOne =
                            checked &&
                            enabledIds.length <= 1;
                        const isFirst = idx === 0;
                        const isLast =
                            idx === order.length - 1;

                        return html`
                            <li
                                class="column-item ${checked ? 'enabled' : 'disabled'}"
                            >
                                <input
                                    type="checkbox"
                                    class="column-toggle"
                                    .checked=${checked}
                                    ?disabled=${onlyOne}
                                    @change=${() =>
                                        this.handleColumnToggle(
                                            id,
                                        )}
                                />
                                <span
                                    class="column-label"
                                >
                                    ${COLUMN_DEFS[id]
                                        ?.label ??
                                    id}
                                </span>
                                <span
                                    class="column-arrows"
                                >
                                    ${isFirst
                                        ? nothing
                                        : html`
                                              <button
                                                  class="column-arrow-btn"
                                                  title="Move up"
                                                  @click=${() =>
                                                      this.handleColumnMove(
                                                          id,
                                                          'up',
                                                      )}
                                              >
                                                  ▲
                                              </button>
                                          `}
                                    ${isLast
                                        ? nothing
                                        : html`
                                              <button
                                                  class="column-arrow-btn"
                                                  title="Move down"
                                                  @click=${() =>
                                                      this.handleColumnMove(
                                                          id,
                                                          'down',
                                                      )}
                                              >
                                                  ▼
                                              </button>
                                          `}
                                </span>
                            </li>
                        `;
                    })}
                </ul>
            </config-section>
        `;
    }

    // --- Keyboard Shortcuts section ---

    private renderShortcutsSection() {
        const bindings =
            this.shortcutsCtrl.state.bindings;
        const categories = [
            'Player',
            'Navigation',
            'App',
        ];

        return html`
            <config-section
                heading="Keyboard Shortcuts"
                description="Customise key bindings for player controls, navigation, and app actions."
            >
                ${categories.map((cat) => {
                    const actions = Object.entries(
                        ConfigPage.SHORTCUT_META,
                    ).filter(
                        ([, meta]) =>
                            meta.category === cat,
                    );

                    if (actions.length === 0) return '';

                    return html`
                        <div class="shortcut-category">
                            <div
                                class="shortcut-category-header"
                            >
                                ${cat}
                            </div>
                            ${actions.map(
                                ([action, meta]) => html`
                                    <div
                                        class="shortcut-row"
                                    >
                                        <span
                                            class="shortcut-label"
                                        >
                                            ${meta.label}
                                            ${meta.scope !==
                                            'global'
                                                ? html`
                                                      <span
                                                          class="shortcut-scope"
                                                          >(${meta.scope.replace(
                                                              'panel:',
                                                              '',
                                                          )})</span
                                                      >
                                                  `
                                                : ''}
                                        </span>
                                        <shortcut-capture
                                            .action=${action}
                                            .currentKey=${bindings.get(
                                                action,
                                            ) ?? ''}
                                            .defaultKey=${meta.defaultKey}
                                            @shortcut-change=${this
                                                .handleShortcutChange}
                                        ></shortcut-capture>
                                    </div>
                                `,
                            )}
                        </div>
                    `;
                })}

                <div class="shortcut-actions">
                    <button
                        class="btn-ghost"
                        @click=${this
                            .handleResetAllShortcuts}
                    >
                        Reset All to Defaults
                    </button>
                </div>

                ${this.shortcutConflict
                    ? html`
                          <div class="conflict-banner">
                              <span
                                  class="conflict-text"
                              >
                                  <strong
                                      >${this
                                          .shortcutConflict
                                          .newKey}</strong
                                  >
                                  is already bound to
                                  <strong
                                      >${ConfigPage
                                          .SHORTCUT_META[
                                          this
                                              .shortcutConflict
                                              .existingAction
                                      ]?.label ??
                                      this
                                          .shortcutConflict
                                          .existingAction}</strong
                                  >.
                              </span>
                              <div
                                  class="conflict-actions"
                              >
                                  <button
                                      class="btn-warning"
                                      @click=${this
                                          .handleConflictOverwrite}
                                  >
                                      Overwrite
                                  </button>
                                  <button
                                      class="btn-ghost"
                                      @click=${this
                                          .handleConflictCancel}
                                  >
                                      Cancel
                                  </button>
                              </div>
                          </div>
                      `
                    : ''}
            </config-section>
        `;
    }

    // --- Library section ---

    private renderLibrarySection() {
        const removingLib = this.libraries.find(
            (l) => l.id === this.removingLibraryId,
        );

        return html`
            <config-section
                heading="Libraries"
                description="Manage your music library folders. Each library is scanned independently."
            >
                <div class="library-list">
                    ${this.libraries.map(
                        (lib) => html`
                            <div class="library-row">
                                ${this.editingLibraryId === lib.id
                                    ? html`
                                          <input
                                              class="edit-input"
                                              type="text"
                                              .value=${this.editingName}
                                              @input=${this.handleRenameInput}
                                              @keydown=${this.handleRenameKeyDown}
                                              @click=${(e: Event) => e.stopPropagation()}
                                          />
                                      `
                                    : html`
                                          <span
                                              class="library-name"
                                              @click=${() => this.handleStartRename(lib.id, lib.name)}
                                          >
                                              ${lib.name}
                                          </span>
                                      `}
                                <span class="library-path">${lib.path}</span>
                                <span class="library-count">
                                    ${lib.trackCount} tracks
                                </span>
                                <div class="overflow-wrapper">
                                    <button
                                        class="overflow-btn"
                                        @click=${(e: Event) => this.toggleOverflowMenu(lib.id, e)}
                                    >
                                        \u22EF
                                    </button>
                                    ${this.activeMenuId === lib.id
                                        ? html`
                                              <div
                                                  class="overflow-menu"
                                                  @click=${(e: Event) => e.stopPropagation()}
                                              >
                                                  <div
                                                      class="overflow-item"
                                                      @click=${() => this.handleStartRename(lib.id, lib.name)}
                                                  >
                                                      Rename
                                                  </div>
                                                  <div
                                                      class="overflow-item"
                                                      @click=${() => this.handleRescanLibrary(lib.id)}
                                                  >
                                                      Rescan
                                                  </div>
                                                  <div
                                                      class="overflow-item overflow-item--danger"
                                                      @click=${() => void this.handleRemoveClick(lib.id)}
                                                  >
                                                      Remove
                                                  </div>
                                              </div>
                                          `
                                        : nothing}
                                </div>
                            </div>
                        `,
                    )}
                </div>

                <button
                    class="btn-primary add-library-btn"
                    @click=${this.handleAddLibrary}
                >
                    Add Library
                </button>

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
                    ${this.scanning
                        ? html`
                              ${this.scanPaused
                                  ? html`<button
                                        class="btn-warning"
                                        @click=${this.handleResumeScan}
                                    >
                                        Resume
                                    </button>`
                                  : html`<button
                                        class="btn-warning"
                                        @click=${this.handlePauseScan}
                                    >
                                        Pause
                                    </button>`}
                              <button
                                  class="btn-danger"
                                  @click=${this.handleCancelScan}
                              >
                                  Cancel Scan
                              </button>
                          `
                        : html`
                              <button
                                  class="btn-warning"
                                  @click=${this.handleSoftScan}
                              >
                                  Soft Scan
                              </button>
                              <button
                                  class="btn-danger"
                                  @click=${this.handleFullRescan}
                              >
                                  Full Rescan
                              </button>
                              <button
                                  class="btn-primary"
                                  @click=${this.handleScanAll}
                              >
                                  Scan All Libraries
                              </button>
                          `}
                </div>

                <div
                    class="status-bar ${this.scanning ? 'active' : ''} ${this.scanPaused ? 'paused' : ''}"
                >
                    ${this.scanPaused
                        ? 'Scan paused.'
                        : this.scanProgress
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
                ${this.showCancelDialog
                    ? html`
                          <div
                              class="cancel-dialog-overlay"
                              @click=${this.handleCancelDialogDismiss}
                          >
                              <div
                                  class="cancel-dialog"
                                  @click=${(e: Event) => e.stopPropagation()}
                              >
                                  ${this.scanQueuedCount > 0
                                      ? html`
                                            <div
                                                class="cancel-dialog-title"
                                            >
                                                Cancel Scanning
                                            </div>
                                            <div
                                                class="cancel-dialog-message"
                                            >
                                                A scan is in
                                                progress with
                                                ${this
                                                    .scanQueuedCount}
                                                ${this
                                                    .scanQueuedCount ===
                                                1
                                                    ? 'library'
                                                    : 'libraries'}
                                                still queued.
                                            </div>
                                            <div
                                                class="cancel-dialog-actions"
                                            >
                                                <button
                                                    class="btn-warning"
                                                    @click=${this.handleCancelKeep}
                                                >
                                                    Cancel
                                                    This
                                                    Library
                                                </button>
                                                <button
                                                    class="btn-danger"
                                                    @click=${this.handleCancelAll}
                                                >
                                                    Cancel
                                                    All
                                                    Scanning
                                                </button>
                                                <button
                                                    class="btn-ghost"
                                                    @click=${this.handleCancelDialogDismiss}
                                                >
                                                    Continue
                                                    Scanning
                                                </button>
                                            </div>
                                        `
                                      : html`
                                            <div
                                                class="cancel-dialog-title"
                                            >
                                                Cancel Scan
                                            </div>
                                            <div
                                                class="cancel-dialog-message"
                                            >
                                                ${this
                                                    .cancelMetrics
                                                    ?.added
                                                    ? `Keep ${this.cancelMetrics.added} tracks found so far, or discard?`
                                                    : 'Cancel the current scan?'}
                                            </div>
                                            <div
                                                class="cancel-dialog-actions"
                                            >
                                                <button
                                                    class="btn-primary"
                                                    @click=${this.handleCancelKeep}
                                                >
                                                    ${this
                                                        .cancelMetrics
                                                        ?.added
                                                        ? `Keep ${this.cancelMetrics.added} tracks`
                                                        : 'Cancel Scan'}
                                                </button>
                                                <button
                                                    class="btn-danger"
                                                    @click=${this.handleCancelDiscard}
                                                >
                                                    Discard
                                                </button>
                                                <button
                                                    class="btn-ghost"
                                                    @click=${this.handleCancelDialogDismiss}
                                                >
                                                    Continue
                                                    Scanning
                                                </button>
                                            </div>
                                        `}
                              </div>
                          </div>
                      `
                    : nothing}

                ${this.removingLibraryId !== null && this.removalImpact
                    ? html`
                          <div
                              class="cancel-dialog-overlay"
                              @click=${this.handleCancelRemove}
                          >
                              <div
                                  class="cancel-dialog"
                                  @click=${(e: Event) => e.stopPropagation()}
                              >
                                  <div class="cancel-dialog-title">
                                      Remove Library
                                  </div>
                                  <div class="cancel-dialog-message">
                                      Remove '${removingLib?.name}'?
                                      This will delete
                                      ${this.removalImpact.trackCount}
                                      tracks, affect
                                      ${this.removalImpact.playlistsAffected}
                                      playlists, and remove
                                      ${this.removalImpact.queueItemCount}
                                      queue items.
                                  </div>
                                  <div class="cancel-dialog-actions">
                                      <button
                                          class="btn-ghost"
                                          ?disabled=${this.isRemoving}
                                          @click=${this.handleCancelRemove}
                                      >
                                          Cancel
                                      </button>
                                      <button
                                          class="btn-danger"
                                          ?disabled=${this.isRemoving}
                                          @click=${this.handleConfirmRemove}
                                      >
                                          ${this.isRemoving
                                              ? html`<span class="spinner"></span> Removing\u2026`
                                              : 'Remove'}
                                      </button>
                                  </div>
                              </div>
                          </div>
                      `
                    : nothing}
            </config-section>

            ${this.toastVisible
                ? html`<div class="toast">${this.toastMessage}</div>`
                : nothing}
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

        // Build detail string: "1,247 / 2,013 files (891 new, 23 updated, 356 skipped)"
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

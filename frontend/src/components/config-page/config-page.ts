import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { repeat } from 'lit/directives/repeat.js';
import { EventsOn } from '@runtime/runtime';
import type { explore } from '@go/models';
import {
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
import './download-clients';
import './shortcut-capture';
import { shortcutsStore } from '../../store/shortcuts-store';
import { ShortcutsController } from '../../store/controllers/shortcuts-controller';

const SCROLL_STORAGE_KEY = 'yj-now-playing-scroll-mode';
const SCROLL_CHANGE_EVENT = 'yj-scroll-mode-changed';

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
    @state() private concurrencyMode = 'auto';
    @state() private indexStatus: explore.IndexStatus | null = null;
    @state() private shortcutConflict: {
        newAction: string;
        newKey: string;
        existingAction: string;
    } | null = null;

    private toastTimer?: ReturnType<typeof setTimeout>;
    private cancelIndexStatus?: () => void;
    private indexPollTimer?: ReturnType<typeof setInterval>;
    private cancelLibraryAdded?: () => void;
    private cancelLibraryRenamed?: () => void;
    private cancelLibraryRemoved?: () => void;

    static override styles = css`
        :host {
            display: block;
            padding: 1.5em;
            height: 100%;
            box-sizing: border-box;
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

        .library-header {
            display: flex;
            align-items: center;
            gap: 0.75em;
            padding: 0.4em 0.5em;
            border-bottom: 1px solid var(--yj-border-subtle, #333);
            font-size: var(--yj-text-sm, 0.8em);
            color: var(--yj-text-muted, #999);
        }

        .library-header-label {
            user-select: none;
        }

        .library-checkbox {
            cursor: pointer;
            flex-shrink: 0;
        }

        .library-row {
            display: flex;
            flex-wrap: wrap;
            align-items: center;
            gap: 0.5em 0.75em;
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

        .library-row .inline-progress {
            flex-basis: 100%;
            padding-left: 1.75em;
        }

        .library-row .inline-progress .progress-track {
            margin-top: 0.25em;
        }

        .library-scan-status {
            font-size: 0.75em;
            font-weight: 400;
            color: var(--yj-accent, #ffd43b);
            white-space: nowrap;
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

        /* Search index status */
        .index-status {
            padding: 0 0.25em 0.5em;
        }

        .index-stats {
            display: flex;
            align-items: center;
            gap: 0.5em;
            font-size: var(--yj-text-sm);
            color: var(--yj-text-secondary, #aaa);
            margin-bottom: 1em;
            font-variant-numeric: tabular-nums;
        }

        .index-stat-sep {
            opacity: 0.4;
        }

        .index-tiers {
            display: flex;
            flex-direction: column;
            gap: 0.5em;
        }

        .index-tier {
            display: flex;
            align-items: center;
            gap: 0.6em;
            font-size: var(--yj-text-sm);
        }

        .tier-icon {
            width: 1.2em;
            text-align: center;
            flex-shrink: 0;
        }

        .tier-name {
            color: var(--yj-text-primary, #fff);
        }

        .tier-progress {
            color: var(--yj-text-tertiary, #888);
            font-size: var(--yj-text-xs, 11px);
            font-variant-numeric: tabular-nums;
        }

        .tier-detail {
            color: var(--yj-text-tertiary, #888);
            font-size: var(--yj-text-xs, 11px);
            font-variant-numeric: tabular-nums;
        }

        .tier-error {
            color: var(--yj-accent-error, #f44);
            font-size: var(--yj-text-xs, 11px);
        }

        .index-ready {
            margin-top: 1em;
            font-size: var(--yj-text-sm);
            color: var(--yj-accent-success, #4a4);
        }

        .index-waiting, .index-loading {
            font-size: var(--yj-text-sm);
            color: var(--yj-text-tertiary, #888);
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

        // Scan progress lives in the jobs panel now — this page only
        // needs to know when the library list itself changes.
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

        // Listen for index status events (pushed from Go, no binding calls).
        this.cancelIndexStatus = EventsOn(
            Events.IndexStatusChanged,
            (status: explore.IndexStatus) => {
                console.log('IndexStatusChanged event received', status);
                this.indexStatus = status;
            },
        );
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.cancelLibraryAdded?.();
        this.cancelLibraryRenamed?.();
        this.cancelLibraryRemoved?.();

        document.removeEventListener('click', this.handleDocumentClick);

        if (this.toastTimer) clearTimeout(this.toastTimer);
        if (this.indexPollTimer) clearInterval(this.indexPollTimer);
        this.cancelIndexStatus?.();
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
            void this.loadLibraries();
        } catch (err) {
            this.isRemoving = false;
            this.removingLibraryId = null;
            this.removalImpact = null;
            console.error('Failed to remove library:', err);
            this.showToast(`Failed to remove '${libName}': ${String(err)}`);
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

        if (this.editingLibraryId !== null) {
            this.editingLibraryId = null;
            this.editingName = '';
        }
    };

    private showToast(message: string): void {
        this.toastMessage = message;
        this.toastVisible = true;

        if (this.toastTimer) clearTimeout(this.toastTimer);

        this.toastTimer = setTimeout(() => {
            this.toastVisible = false;
        }, 8000);
    }

    private handleConcurrencyChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        const mode = String(e.detail.value);

        SetScanConcurrency(mode)
            .then(() => {
                this.concurrencyMode = mode;
                this.showToast(
                    'Storage type saved. Takes effect on next scan.',
                );
            })
            .catch((err: unknown) => {
                this.showToast(`Failed to save storage type: ${err}`);
            });
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
    // RENDER
    // ===================================================================

    override render() {
        return html`
            <h2>Settings</h2>

            ${this.renderSearchSection()}
            ${this.renderNowPlayingSection()}
            ${this.renderThemeSection()}
            ${this.renderFavoritesSection()}
            ${this.renderTrackListSection()}
            ${this.renderShortcutsSection()}
            <download-clients></download-clients>
            ${this.renderLibrarySection()}
        `;
    }

    // --- Search / Index section ---

    private renderSearchSection() {
        const s = this.indexStatus;

        return html`
            <config-section
                heading="Search Index"
                description="The explore search index is built from the MusicBrainz/ListenBrainz data dumps — popular artists, albums, and tracks with listen counts — for fast offline search."
                .open=${true}
            >
                <div class="index-status">
                    ${s
                        ? html`
                            <div class="index-stats">
                                <span class="index-stat">${this.formatCount(s.artists)} artists</span>
                                <span class="index-stat-sep">·</span>
                                <span class="index-stat">${this.formatCount(s.recordings)} recordings</span>
                                <span class="index-stat-sep">·</span>
                                <span class="index-stat">${this.formatCount(s.releaseGroups)} albums</span>
                                <span class="index-stat-sep">·</span>
                                <span class="index-stat">${this.formatCount(s.totalRows)} total</span>
                                ${s.lastBuilt
                                    ? html`<span class="index-stat-sep">·</span>
                                           <span class="index-stat">updated ${this.timeAgo(s.lastBuilt)}</span>`
                                    : nothing}
                            </div>
                            ${s.tiers?.length > 0 && s.tiers.some((t) => t.state === 'running' || t.state === 'pending' || t.state === 'error')
                                ? html`
                                    <div class="index-tiers">
                                        ${s.tiers.map(
                                            (t) => html`
                                                <div class="index-tier">
                                                    <span class="tier-icon">${this.tierIcon(t.state)}</span>
                                                    <span class="tier-name">${t.name}</span>
                                                    ${t.state === 'running' && t.total > 0
                                                        ? html`<span class="tier-progress">${t.completed}/${t.total}</span>`
                                                        : nothing}
                                                    ${t.state === 'running' && t.detail
                                                        ? html`<span class="tier-detail">${t.detail}</span>`
                                                        : nothing}
                                                    ${t.state === 'error'
                                                        ? html`<span class="tier-error">${t.error}</span>`
                                                        : nothing}
                                                </div>
                                            `,
                                        )}
                                    </div>
                                `
                                : nothing}
                            ${!s.building && s.ready
                                ? html`<div class="index-ready">Index ready</div>`
                                : !s.building && !s.ready && s.totalRows === 0
                                    ? html`<div class="index-waiting">Index empty — build will start after library scan</div>`
                                    : !s.building && !s.ready
                                        ? html`<div class="index-waiting">Waiting for index build…</div>`
                                        : nothing}
                        `
                        : html`<div class="index-loading">Loading status…</div>`}
                </div>
            </config-section>
        `;
    }

    private tierIcon(state: string): string {
        switch (state) {
            case 'complete':
            case 'skipped':
                return '✅';
            case 'running':
                return '🔄';
            case 'error':
                return '❌';
            case 'pending':
            default:
                return '⏳';
        }
    }

    private formatCount(n: number): string {
        if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
        if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
        return `${n}`;
    }

    private timeAgo(iso: string): string {
        const then = new Date(iso).getTime();
        if (!then) return '';
        const seconds = Math.floor((Date.now() - then) / 1000);
        if (seconds < 60) return 'just now';
        const minutes = Math.floor(seconds / 60);
        if (minutes < 60) return `${minutes}m ago`;
        const hours = Math.floor(minutes / 60);
        if (hours < 24) return `${hours}h ago`;
        const days = Math.floor(hours / 24);
        if (days === 1) return 'yesterday';
        return `${days}d ago`;
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
                description="Manage your music library folders. Scanning and its
                    progress live in the Jobs panel."
            >
                <div class="scan-actions">
                    <button
                        class="btn-primary"
                        @click=${this.handleAddLibrary}
                    >
                        Add Library
                    </button>
                </div>

                ${this.libraries.length > 0
                    ? html`
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

}

declare global {
    interface HTMLElementTagNameMap {
        'config-page': ConfigPage;
    }
}

import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { repeat } from 'lit/directives/repeat.js';
import { EventsOn } from '@runtime/runtime';
import type * as explore from '@go/explore/models.js';
import {
    AddLibrary,
    RenameLibrary,
    RemoveLibrary,
    GetRemovalImpact,
    GetAllLibrariesWithTrackCounts,
} from '@go/library/library.js';
import {
    GetScanConcurrency,
    SetScanConcurrency,
    GetDefaultPage,
    SetDefaultPage,
    GetQueueFallback,
    SetQueueFallback,
} from '@go/config/config.js';
import { GetIndexStatus } from '@go/explore/service.js';
import { DirectoryPicker } from '@go/frontendutil/frontendutil.js';
import { notificationStore } from '@store/notification-store';
import { describeError, explainError } from '@utils/describe-error';
import type * as library from '@go/library/models.js';
import { ThemeController } from '@store/controllers/theme-controller';
import { TrackListController } from '@store/controllers/tracklist-controller';
import { FavoritesController } from '@store/controllers/favorites-controller';
import { GetAllPlaylists } from '@go/playlist/service.js';
import type * as playlist from '@go/playlist/models.js';
import { Events } from '../../events';
import {
    SHORTCUT_CATEGORIES,
    SHORTCUT_META,
} from '../../services/shortcut-meta';
import { ViewLifecycleMixin } from '../../utils/view-lifecycle';
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
import { confirmAction } from '../confirm-dialog/confirm-dialog';
import { shortcutsStore } from '../../store/shortcuts-store';
import { ShortcutsController } from '../../store/controllers/shortcuts-controller';
import { list } from '@utils/binding';

const SCROLL_STORAGE_KEY = 'yj-now-playing-scroll-mode';
const SCROLL_CHANGE_EVENT = 'yj-scroll-mode-changed';

// ===================================================================
// Config page component
// ===================================================================

@customElement('config-page')
export class ConfigPage extends ViewLifecycleMixin(LitElement) {
    // --- Theme controller for reading/writing theme state ---
    private themeCtrl = new ThemeController(this);

    // --- Track-list column config controller ---
    private trackListCtrl = new TrackListController(this);

    // --- Favorites controller ---
    private favCtrl = new FavoritesController(this);

    // --- Shortcuts controller ---
    private shortcutsCtrl = new ShortcutsController(this);

    // --- Now Playing state ---
    @state() private scrollMode = 'hover';

    // --- Favorites state ---
    @state() private playlists: playlist.Summary[] = [];

    // --- Library state ---
    @state() private libraries: library.Info[] = [];
    @state() private editingLibraryId: number | null = null;
    @state() private editingName = '';
    /** The library a removal is in flight for. The impact is computed
     *  before the confirmation rather than held here, so this is now
     *  "which row is busy" and nothing else. */
    @state() private removingLibraryId: number | null = null;
    @state() private activeMenuId: number | null = null;
    @state() private concurrencyMode = 'auto';
    @state() private defaultPage = 'home';
    @state() private queueFallback = 'favorites';
    @state() private indexStatus: explore.IndexStatus | null = null;
    /** Three states, not one: the panel used to say "Loading status…"
     *  for the entire session, because the only thing that ever set
     *  `indexStatus` was an event that fires on *change* (errors.M3). */
    @state() private indexStatusFailed = false;
    @state() private shortcutConflict: {
        newAction: string;
        newKey: string;
        existingAction: string;
    } | null = null;

    private cancelIndexStatus?: () => void;
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
            color: var(--yj-warning-fg, #fff);
        }

        .btn-warning:hover:not(:disabled) {
            background: var(--yj-warning-hover, #d9480f);
        }

        .btn-danger {
            background: var(--yj-error, #e03131);
            color: var(--yj-error-fg, #fff);
        }

        .btn-danger:hover:not(:disabled) {
            background: var(--yj-error-hover, #c92a2a);
        }

        .btn-success {
            background: var(--yj-success, #2f9e44);
            color: var(--yj-success-fg, #fff);
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
            color: var(--yj-success-text, #51cf66);
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
            color: var(--yj-accent-text, #ffd43b);
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
            color: var(--yj-error-text, #ff8787);
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
            color: var(--yj-accent-text, #ffd43b);
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

        .btn-primary {
            background: var(
                --yj-accent,
                #ffd43b
            );
            color: var(--yj-accent-fg, #000);
            font-weight: 600;
        }

        .btn-primary:hover:not(:disabled) {
            filter: brightness(1.1);
        }

        .status-bar.paused {
            color: var(--yj-accent-text, #ffd43b);
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
            color: var(--yj-accent-text, #ffd43b);
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
            color: var(--yj-accent-text, #ffd43b);
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
            color: var(--yj-error-text, #ff8787);
        }

        .overflow-item--danger:hover {
            background: color-mix(
                in srgb,
                var(--yj-error, #e03131) 10%,
                var(--yj-bg-elevated, #343a40)
            );
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

        .index-status-failed {
            align-items: center;
            color: var(--yj-text-secondary, #b3b3b3);
            display: flex;
            font-size: var(--yj-text-sm);
            gap: 0.5em;
        }

        .index-status-failed .link {
            background: none;
            border: none;
            color: var(--yj-accent-text, #ffd43b);
            cursor: pointer;
            font: inherit;
            padding: 0;
            text-decoration: underline;
        }

    `;

    // ===================================================================
    // LIFECYCLE
    // ===================================================================

    protected override onViewActivate(): void {
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

        this.listenWhileActive(document, 'click', this.handleDocumentClick);

        // Listen for index status events (pushed from Go, no binding calls).
        this.cancelIndexStatus = EventsOn(
            Events.IndexStatusChanged,
            (status: explore.IndexStatus) => {
                this.indexStatus = status;
                this.indexStatusFailed = false;
            },
        );

        // …and pull the current one, because that event only fires when
        // a build *changes* state, and the steady state is no build.
        void this.loadIndexStatus();
    }

    protected override onViewDeactivate(): void {
        this.cancelLibraryAdded?.();
        this.cancelLibraryRenamed?.();
        this.cancelLibraryRemoved?.();

        this.cancelIndexStatus?.();
    }

    private async loadLibraries(): Promise<void> {
        try {
            const [libs, mode, defaultPage, queueFallback] = await Promise.all([
                GetAllLibrariesWithTrackCounts(),
                GetScanConcurrency(),
                GetDefaultPage(),
                GetQueueFallback(),
            ]);

            this.libraries = libs ?? [];
            this.concurrencyMode = mode;
            this.defaultPage = defaultPage;
            this.queueFallback = queueFallback;

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
        let dir = '';

        try {
            dir = await DirectoryPicker();

            if (!dir) return;

            await AddLibrary(dir);
        } catch (err) {
            console.error('Failed to add library:', err);
            notificationStore.persistent({
                key: 'library-add',
                title: 'Library not added',
                text: explainError(
                    err,
                    'That folder could not be added as a library.',
                ),
                detail: dir,
                action: {
                    label: 'Try again',
                    run: () => void this.handleAddLibrary(),
                },
            });
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
                const name = this.editingName.trim();

                try {
                    await RenameLibrary(this.editingLibraryId, name);
                } catch (err) {
                    console.error('Failed to rename library:', err);
                    notificationStore.persistent({
                        key: 'library-rename',
                        title: 'Library not renamed',
                        text: explainError(
                            err,
                            `“${name}” could not be used as a library name.`,
                        ),
                        detail: String(err),
                    });
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

    /**
     * Say what will happen, then ask — through the one confirmation the
     * app has, which is a `wa-dialog` and so brings the focus trap, the
     * Escape handler and the focus restore the hand-rolled overlay this
     * replaces had none of (a11y.16).
     */
    private handleRemoveClick = async (id: number): Promise<void> => {
        this.activeMenuId = null;

        const lib = this.libraries.find((l) => l.id === id);
        const libName = lib?.name ?? 'Library';
        let impact: library.RemovalImpact | null = null;

        try {
            impact = await GetRemovalImpact(id);
        } catch (err) {
            // Asking without the impact is worse than not asking at all,
            // so this is a failure the user has to see rather than a
            // confirmation with a blank consequence.
            console.error('Failed to get removal impact:', err);
            notificationStore.persistent({
                key: 'library-remove',
                title: 'Library not removed',
                text: `Could not work out what removing “${libName}” would delete. ${describeError(err)}`,
                detail: String(err),
            });

            return;
        }

        const ok = await confirmAction({
            title: 'Remove library',
            message: `Remove “${libName}”?`,
            impact: `This deletes ${impact?.trackCount ?? 0} tracks, affects `
                + `${impact?.playlistsAffected ?? 0} playlists and removes `
                + `${impact?.queueItemCount ?? 0} queue items.`,
            confirmLabel: 'Remove',
            danger: true,
        });

        if (!ok) return;

        await this.removeLibrary(id, libName);
    };

    private async removeLibrary(id: number, libName: string): Promise<void> {
        this.removingLibraryId = id;

        try {
            const summary = await RemoveLibrary(id);

            this.removingLibraryId = null;
            notificationStore.transient({
                tone: 'success',
                key: 'library-remove',
                text: `Removed “${libName}” — ${summary?.tracksDeleted ?? 0} tracks deleted.`,
            });
            void this.loadLibraries();
        } catch (err) {
            this.removingLibraryId = null;
            console.error('Failed to remove library:', err);
            notificationStore.persistent({
                key: 'library-remove',
                title: 'Library not removed',
                text: `Could not remove “${libName}”. ${describeError(err)}`,
                detail: String(err),
            });
        }
    }

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

    private async loadIndexStatus(): Promise<void> {
        try {
            this.indexStatus = await GetIndexStatus();
            this.indexStatusFailed = false;
        } catch (err) {
            console.error('Failed to read index status:', err);
            this.indexStatusFailed = true;
        }
    }

    private handleDefaultPageChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        const page = String(e.detail.value);

        SetDefaultPage(page)
            .then(() => {
                this.defaultPage = page;
                notificationStore.transient({
                    tone: 'success',
                    key: 'default-page',
                    text: 'Launch page saved.',
                });
            })
            .catch((err: unknown) => {
                console.error('Failed to save launch page:', err);
                notificationStore.transient({
                    key: 'default-page',
                    text: `Could not save the launch page. ${describeError(err)}`,
                    detail: String(err),
                });
            });
    };

    private handleQueueFallbackChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        const mode = String(e.detail.value);

        SetQueueFallback(mode)
            .then(() => {
                this.queueFallback = mode;
                notificationStore.transient({
                    tone: 'success',
                    key: 'queue-fallback',
                    text: 'Queue fallback saved.',
                });
            })
            .catch((err: unknown) => {
                console.error('Failed to save queue fallback:', err);
                notificationStore.transient({
                    key: 'queue-fallback',
                    text: `Could not save the queue fallback. ${describeError(err)}`,
                    detail: String(err),
                });
            });
    };

    private handleConcurrencyChange = (
        e: CustomEvent<ConfigFieldChangeEvent>,
    ): void => {
        const mode = String(e.detail.value);

        SetScanConcurrency(mode)
            .then(() => {
                this.concurrencyMode = mode;
                notificationStore.transient({
                    tone: 'success',
                    key: 'storage-type',
                    text: 'Storage type saved. Takes effect on next scan.',
                });
            })
            .catch((err: unknown) => {
                console.error('Failed to save storage type:', err);
                notificationStore.transient({
                    key: 'storage-type',
                    text: `Could not save the storage type. ${describeError(err)}`,
                    detail: String(err),
                });
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
            this.playlists = await list(GetAllPlaylists());
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
        const meta = SHORTCUT_META[action];
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

            <!--
              Ordered by how often a setting is *reached*, not by how
              the sections were written (H-22).  Libraries was last and
              below the fold while Search Index — which is configured
              once, if ever — was first and the only expanded one.
            -->
            ${this.renderLibrarySection()}
            ${this.renderGeneralSection()}
            ${this.renderNowPlayingSection()}
            ${this.renderThemeSection()}
            ${this.renderTrackListSection()}
            ${this.renderFavoritesSection()}
            ${this.renderShortcutsSection()}
            ${this.renderSearchSection()}
            <download-clients></download-clients>
        `;
    }

    // --- Search / Index section ---

    private renderSearchSection() {
        const s = this.indexStatus;

        return html`
            <config-section
                heading="Search Index"
                description="The explore search index is built from the MusicBrainz/ListenBrainz data dumps — popular artists, albums, and tracks with listen counts — for fast offline search."
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
                            ${(s.tiers?.length ?? 0) > 0 && (s.tiers ?? []).some((t) => t.state === 'running' || t.state === 'pending' || t.state === 'error')
                                ? html`
                                    <div class="index-tiers">
                                        ${(s.tiers ?? []).map(
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
                                                        ? html`<span class="tier-error">${describeError(t.error, 'This part of the index could not be built.')}</span>`
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
                        : this.indexStatusFailed
                            ? html`<div class="index-status-failed">
                                  <span>The index status could not be read.</span>
                                  <button
                                      type="button"
                                      class="link"
                                      @click=${() => void this.loadIndexStatus()}
                                  >
                                      Retry
                                  </button>
                              </div>`
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

    // --- General section ---

    private renderGeneralSection() {
        return html`
            <config-section
                heading="General"
                description="General app behaviour."
            >
                <config-field
                    .schema=${{
                        key: 'defaultPage',
                        label: 'Launch Page',
                        description:
                            'The page the app opens to on launch.',
                        type: 'select' as const,
                        options: [
                            { value: 'home', label: 'Home' },
                            { value: 'tracks', label: 'Tracks' },
                            { value: 'albums', label: 'Albums' },
                            { value: 'artists', label: 'Artists' },
                            { value: 'genres', label: 'Genres' },
                            { value: 'playlists', label: 'Playlists' },
                            { value: 'explore', label: 'Explore' },
                            { value: 'downloads', label: 'Downloads' },
                            { value: 'autotag', label: 'Autotag' },
                            { value: 'jobs', label: 'Jobs' },
                        ],
                    }}
                    .value=${this.defaultPage}
                    @config-change=${this.handleDefaultPageChange}
                ></config-field>
                <config-field
                    .schema=${{
                        key: 'queueFallback',
                        label: 'When the Queue Ends',
                        description:
                            'What plays, if anything, once the queue runs out.',
                        type: 'select' as const,
                        options: [
                            { value: 'favorites', label: 'Play Favorites' },
                            { value: 'dynamicMix', label: 'Start a Dynamic Mix' },
                            { value: 'stop', label: 'Stop' },
                        ],
                    }}
                    .value=${this.queueFallback}
                    @config-change=${this.handleQueueFallbackChange}
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

                        const columnLabel =
                            COLUMN_DEFS[id]?.label ?? id;

                        return html`
                            <li
                                class="column-item ${checked ? 'enabled' : 'disabled'}"
                            >
                                <input
                                    type="checkbox"
                                    class="column-toggle"
                                    aria-label="Show the ${columnLabel} column"
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
                                    ${columnLabel}
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
                                                  aria-label="Move ${columnLabel} up"
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
                                                  aria-label="Move ${columnLabel} down"
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
        // All four, from the shared table: the Autotag bindings are
        // persisted and rebindable like any other, and listing three of
        // four categories is how they came to be written down nowhere.
        const categories = SHORTCUT_CATEGORIES;

        return html`
            <config-section
                heading="Keyboard Shortcuts"
                description="Customise key bindings for player controls, navigation, and app actions."
            >
                ${categories.map((cat) => {
                    const actions = Object.entries(
                        SHORTCUT_META,
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
                                            .label=${meta.label}
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
                                      >${SHORTCUT_META[
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
        return html`
            <config-section
                heading="Libraries"
                description="Manage your music library folders. Scanning and its
                    progress live in the Jobs panel."
                .open=${true}
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
                                                      @click=${(e: Event) => {
                                                          // The document handler closes the
                                                          // editor, and without this it closes
                                                          // the one this very click opened.
                                                          e.stopPropagation();
                                                          this.handleStartRename(lib.id, lib.name);
                                                      }}
                                                  >
                                                      ${lib.name}
                                                  </span>
                                              `}
                                        <span class="library-path">${lib.path}</span>
                                        <span class="library-count">
                                            ${this.removingLibraryId === lib.id
                                                ? 'Removing…'
                                                : html`${lib.trackCount} tracks`}
                                        </span>
                                        <div class="overflow-wrapper">
                                            <button
                                                class="overflow-btn"
                                                aria-label=${`Actions for ${lib.name}`}
                                                ?disabled=${this.removingLibraryId === lib.id}
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

            </config-section>
        `;
    }

}

declare global {
    interface HTMLElementTagNameMap {
        'config-page': ConfigPage;
    }
}

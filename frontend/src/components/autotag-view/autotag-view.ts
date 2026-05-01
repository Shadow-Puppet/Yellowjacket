import { LitElement, html, css, nothing } from 'lit';
import type { TemplateResult } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import {
    StartAutotagQueue,
    GetCandidates,
    GetCandidatesForPasteURL,
    GetCandidateCoverArt,
    GetPendingFolder,
    ListPendingFolders,
    ApplyAsync,
    Skip,
    LeaveAsIs,
    AckLibraryWarning,
    ClearCompletedEntries,
} from '@go/autotagservice/Service';
import type { autotagservice } from '@go/models';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import { isMultiDisc, groupByDisc, discNumbers } from '../../utils/disc-grouping';
import { inlineDiff } from '../../utils/text-diff';
import { libraryStore } from '../../store/library-store';

type PendingItem = autotagservice.PendingItem;
type ScoreView = autotagservice.ScoreView;
type CandidateView = autotagservice.CandidateView;
type AlignmentView = autotagservice.AlignmentView;
type LocalTrackView = autotagservice.LocalTrackView;

interface ApplyJobState {
    state: 'running' | 'completed' | 'failed';
    current: number;
    total: number;
    error?: string;
}

const LOCAL_STORAGE_WARNING_KEY = 'yj-autotag-warning-acked-libraries';

// A candidate with a score >= this is considered "confident"; the
// "Leave as-is" shortcut skips its confirmation prompt when such a
// candidate exists, since the user is knowingly rejecting a good
// match.  Below this, we force a confirmation so fat-finger L on a
// still-scrambled album doesn't silently permanent the bad tags.
const CONFIDENT_SCORE = 0.75;

// Show a "low confidence — pick a version" banner when the top
// candidate scores below this AND there are alternative versions
// to choose from.  Empirically, scores in [0.5, 0.7] are where the
// scorer is split between two clusters.
const LOW_CONFIDENCE_THRESHOLD = 0.7;

// A version cluster groups candidates that share a release-group
// MBID (or, for paste-only candidates without one, share a normalised
// album title + artist).  Within a cluster the candidates differ in
// release-level details (date, country, label) but share the
// recording set the user actually owns — there's no point making the
// user pick between them by default.
interface VersionCluster {
    key: string;
    label: string;
    sublabel: string;
    candidates: CandidateView[];
    bestIdx: number; // index into candidates[] of the highest-scoring one
}

/** autotag-view is the /autotag page: pick-apply-skip workflow for
 *  pending tagging items.  The left sidebar lists every folder
 *  awaiting review; the main pane shows a beets-style single-view
 *  diff between the folder's local files and the top candidate
 *  release; the right sidebar offers alternative versions when the
 *  match is ambiguous.  Keyboard shortcuts: A apply · S skip · L
 *  leave · U paste URL · ↑↓ navigate folders · Esc close dialogs.
 */
@customElement('autotag-view')
export class AutotagView extends LitElement {
    static override readonly styles = [
        designTokens,
        css`
            :host {
                display: block;
                color: var(--yj-text-primary, #fff);
                height: 100%;
                box-sizing: border-box;
                overflow: hidden;
            }

            .root {
                display: grid;
                grid-template-columns: 240px 1fr 220px;
                grid-template-rows: auto 1fr;
                grid-template-areas:
                    "header header header"
                    "folders main versions";
                gap: 0.75rem;
                height: 100%;
                box-sizing: border-box;
                padding: 0.75rem 1rem;
            }

            .header {
                grid-area: header;
                display: flex;
                align-items: center;
                gap: 0.75rem;
                flex-wrap: wrap;
            }

            .header h2 {
                margin: 0;
                font-size: 1.15rem;
                line-height: 1.2;
            }

            .header .meta {
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: 0.8rem;
            }

            .header .meta .folder-path {
                font-family: var(--yj-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
                color: var(--yj-text-tertiary, #888);
                margin-left: 0.25rem;
                opacity: 0.85;
            }

            .actions {
                margin-left: auto;
                display: flex;
                gap: 0.4rem;
            }

            button {
                background: var(--yj-accent, #ffd43b);
                color: var(--yj-bg-base, #000);
                border: 1px solid transparent;
                padding: 0.35rem 0.7rem;
                border-radius: 4px;
                font: inherit;
                font-size: 0.85rem;
                cursor: pointer;
            }

            button.secondary {
                background: transparent;
                color: var(--yj-text-primary, #fff);
                border-color: var(--yj-bg-overlay, rgba(255, 255, 255, 0.15));
            }

            button:disabled {
                opacity: 0.5;
                cursor: not-allowed;
            }

            /* ── Folder queue sidebar ── */

            .folders {
                grid-area: folders;
                background: var(--yj-bg-surface, #222);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.1));
                border-radius: 6px;
                overflow: auto;
                display: flex;
                flex-direction: column;
            }

            .folders-header {
                position: relative;
                display: flex;
                align-items: center;
                justify-content: space-between;
                padding: 0.55rem 0.5rem 0.55rem 0.75rem;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
                font-size: 0.78rem;
                color: var(--yj-text-secondary, #b3b3b3);
                text-transform: uppercase;
                letter-spacing: 0.4px;
            }

            .folders-section-header {
                padding: 0.55rem 0.75rem 0.3rem;
                font-size: 0.72rem;
                color: var(--yj-text-tertiary, #888);
                text-transform: uppercase;
                letter-spacing: 0.5px;
                margin-top: 0.4rem;
                border-top: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
            }

            .folders-menu-trigger {
                background: transparent;
                border: 0;
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: 1.1rem;
                line-height: 1;
                padding: 0.1rem 0.4rem;
                border-radius: 3px;
                cursor: pointer;
            }

            .folders-menu-trigger:hover {
                background: var(--yj-bg-elevated, #343a40);
                color: var(--yj-text-primary, #fff);
            }

            .folders-menu {
                position: absolute;
                top: calc(100% - 2px);
                right: 0.5rem;
                z-index: 50;
                min-width: 200px;
                background: var(--yj-bg-surface, #222);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.15));
                border-radius: 4px;
                box-shadow: 0 6px 20px rgba(0, 0, 0, 0.45);
                padding: 0.25rem;
                text-transform: none;
                letter-spacing: 0;
            }

            .folders-menu button {
                display: block;
                width: 100%;
                text-align: left;
                background: transparent;
                color: var(--yj-text-primary, #fff);
                border: 0;
                padding: 0.4rem 0.6rem;
                border-radius: 3px;
                font: inherit;
                font-size: 0.85rem;
                cursor: pointer;
            }

            .folders-menu button:hover {
                background: var(--yj-bg-elevated, #343a40);
            }

            .folder-row {
                padding: 0.45rem 0.75rem 1.4rem;
                cursor: pointer;
                border-left: 3px solid transparent;
                font-size: 0.83rem;
                line-height: 1.25;
                display: grid;
                grid-template-columns: 22px 1fr;
                grid-template-areas:
                    "icon album"
                    "icon artist"
                    "icon count";
                column-gap: 0.55rem;
                row-gap: 0.05rem;
                position: relative;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.04));
            }

            .folder-row:hover {
                background: var(--yj-bg-elevated, #343a40);
            }

            .folder-row.current {
                background: var(--yj-bg-elevated, #343a40);
                border-left-color: var(--yj-accent, #ffd43b);
            }

            /* Skipped + completed sections render visually muted so
             * the eye lands on the actionable Pending section first. */
            .folder-row.skipped,
            .folder-row.completed {
                opacity: 0.7;
            }

            .folder-row.skipped:hover,
            .folder-row.completed:hover,
            .folder-row.skipped.current,
            .folder-row.completed.current {
                opacity: 1;
            }

            .folder-row .status-icon {
                grid-area: icon;
                align-self: center;
                display: flex;
                align-items: center;
                justify-content: center;
                width: 22px;
                height: 22px;
            }

            .folder-row .album {
                grid-area: album;
                font-weight: 500;
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .folder-row .artist {
                grid-area: artist;
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: 0.74rem;
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .folder-row .count {
                grid-area: count;
                color: var(--yj-text-tertiary, #888);
                font-size: 0.7rem;
            }

            .folder-row .match-pill {
                position: absolute;
                right: 0.55rem;
                bottom: 0.4rem;
                font-size: 0.7rem;
                padding: 0.05rem 0.4rem;
                border-radius: 10px;
                background: var(--yj-bg-elevated, #343a40);
                color: var(--yj-text-secondary, #b3b3b3);
                font-variant-numeric: tabular-nums;
            }

            .folder-row .match-pill.high {
                background: var(--yj-accent, #ffd43b);
                color: var(--yj-bg-base, #000);
            }

            .folder-row .match-pill.pending {
                background: transparent;
                color: var(--yj-text-tertiary, #888);
                border: 1px dashed var(--yj-bg-overlay, rgba(255, 255, 255, 0.18));
                padding: 0 0.4rem;
            }

            /* ── Main pane (album header + tracks) ── */

            .main {
                grid-area: main;
                overflow: auto;
                display: flex;
                flex-direction: column;
                gap: 0.75rem;
            }

            .album-card {
                display: grid;
                grid-template-columns: 180px 1fr;
                gap: 1rem;
                background: var(--yj-bg-surface, #222);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.1));
                border-radius: 6px;
                padding: 0.9rem;
            }

            .cover {
                width: 180px;
                height: 180px;
                border-radius: 4px;
                background: var(--yj-bg-elevated, #343a40);
                object-fit: cover;
                display: block;
            }

            .cover.placeholder {
                display: flex;
                align-items: center;
                justify-content: center;
                color: var(--yj-text-tertiary, #888);
                font-size: 0.75rem;
            }

            .album-meta {
                display: flex;
                flex-direction: column;
                gap: 0.35rem;
                min-width: 0;
            }

            .album-title { font-size: 1.4rem; line-height: 1.2; font-weight: 600; }
            .album-artist { font-size: 1rem; color: var(--yj-text-secondary, #b3b3b3); }
            .album-line { font-size: 0.8rem; color: var(--yj-text-secondary, #b3b3b3); }

            .banner {
                background: rgba(255, 200, 90, 0.12);
                border: 1px solid rgba(255, 200, 90, 0.4);
                color: #ffd089;
                padding: 0.5rem 0.75rem;
                border-radius: 4px;
                font-size: 0.85rem;
                display: flex;
                align-items: center;
                gap: 0.5rem;
            }

            /* ── Match-details panel ── */

            .match-details {
                background: var(--yj-bg-surface, #222);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.1));
                border-radius: 6px;
                padding: 0.6rem 0.9rem 0.5rem;
                font-size: 0.85rem;
            }

            .md-header {
                display: flex;
                align-items: center;
                gap: 0.5rem;
                margin-bottom: 0.4rem;
                color: var(--yj-text-secondary, #b3b3b3);
                text-transform: uppercase;
                font-size: 0.72rem;
                letter-spacing: 0.4px;
            }

            .md-items {
                list-style: none;
                margin: 0;
                padding: 0;
                display: flex;
                flex-direction: column;
                gap: 0.2rem;
            }

            .md-items li {
                display: flex;
                align-items: baseline;
                gap: 0.45rem;
                line-height: 1.35;
            }

            .md-items li::before {
                content: '';
                display: inline-block;
                width: 0.65em;
                flex-shrink: 0;
            }

            .md-items li.ok::before    { content: '✓'; color: #9be09b; }
            .md-items li.warn::before  { content: '⚠'; color: #ffd089; }
            .md-items li.info::before  { content: '○'; color: var(--yj-text-tertiary, #888); }

            .md-items li.ok    { color: var(--yj-text-secondary, #b3b3b3); }
            .md-items li.warn  { color: var(--yj-text-primary, #fff); }
            .md-items li.info  { color: var(--yj-text-secondary, #b3b3b3); }

            .breakdown-line {
                margin-top: 0.45rem;
                padding-top: 0.4rem;
                border-top: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.06));
                font-size: 0.75rem;
                color: var(--yj-text-tertiary, #888);
                font-variant-numeric: tabular-nums;
                display: flex;
                gap: 0.75rem;
                flex-wrap: wrap;
            }

            .breakdown-line .b-pair {
                display: inline-flex;
                gap: 0.3rem;
            }

            .breakdown-line .b-val {
                color: var(--yj-text-secondary, #b3b3b3);
            }

            /* ── Tracklist ── */

            .tracklist {
                background: var(--yj-bg-surface, #222);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.1));
                border-radius: 6px;
                padding: 0.5rem 0;
            }

            .section-header {
                font-size: 0.78rem;
                color: var(--yj-text-secondary, #b3b3b3);
                text-transform: uppercase;
                letter-spacing: 0.4px;
                padding: 0.4rem 0.9rem 0.3rem;
            }

            .disc-separator {
                padding: 0.45rem 0.9rem 0.25rem;
                color: var(--yj-text-tertiary, #888);
                font-size: 0.78rem;
                font-weight: 500;
                border-top: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.05));
            }

            .disc-separator:first-child { border-top: 0; }

            .track-row {
                display: grid;
                grid-template-columns: 3rem 1fr 5rem;
                gap: 0.5rem;
                align-items: center;
                padding: 0.3rem 0.9rem;
                font-size: 0.86rem;
                line-height: 1.3;
            }

            .track-row.matched { background: rgba(120, 200, 120, 0.04); }
            .track-row.mismatched { background: rgba(255, 200, 90, 0.08); }
            .track-row.missing { background: rgba(220, 110, 110, 0.06); }
            .track-row.unmatched { background: rgba(220, 110, 110, 0.10); font-style: italic; }

            .track-pos {
                color: var(--yj-text-secondary, #b3b3b3);
                font-variant-numeric: tabular-nums;
                text-align: right;
            }

            .track-len {
                color: var(--yj-text-secondary, #b3b3b3);
                font-variant-numeric: tabular-nums;
                font-size: 0.8rem;
                text-align: right;
            }

            .track-title {
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            /* Diff coloring — applied per inline span */

            /* Inline word/punct diff segments — sit flush against
             * each other so a curly-vs-straight apostrophe swap
             * renders as one strikethrough char immediately
             * followed by one green char. */

            .diff-old {
                color: #f08080;
                text-decoration: line-through;
                text-decoration-thickness: 1.5px;
            }

            .diff-new {
                color: #9be09b;
            }

            .diff-changed {
                color: #ffd089;
            }

            /* ── Versions sidebar ── */

            .versions {
                grid-area: versions;
                background: var(--yj-bg-surface, #222);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.1));
                border-radius: 6px;
                overflow: auto;
                display: flex;
                flex-direction: column;
            }

            .versions-header {
                padding: 0.55rem 0.75rem;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
                font-size: 0.78rem;
                color: var(--yj-text-secondary, #b3b3b3);
                text-transform: uppercase;
                letter-spacing: 0.4px;
            }

            .version-row {
                padding: 0.4rem 0.75rem;
                cursor: pointer;
                border-left: 3px solid transparent;
                display: flex;
                flex-direction: column;
                gap: 0.15rem;
                font-size: 0.82rem;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.04));
            }

            .version-row:hover { background: var(--yj-bg-elevated, #343a40); }

            .version-row.selected {
                background: var(--yj-bg-elevated, #343a40);
                border-left-color: var(--yj-accent, #ffd43b);
            }

            .version-row .label {
                font-weight: 500;
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .version-row .sub {
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: 0.72rem;
                display: flex;
                align-items: center;
                gap: 0.4rem;
                flex-wrap: wrap;
            }

            .score-badge {
                display: inline-block;
                padding: 0.05rem 0.35rem;
                border-radius: 10px;
                font-size: 0.68rem;
                background: var(--yj-bg-elevated, #343a40);
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .score-badge.high {
                background: var(--yj-accent, #ffd43b);
                color: var(--yj-bg-base, #000);
            }

            .provenance-badge {
                display: inline-block;
                padding: 0 0.3rem;
                border-radius: 8px;
                font-size: 0.62rem;
                text-transform: uppercase;
                letter-spacing: 0.3px;
                color: var(--yj-bg-base, #000);
            }

            .provenance-local    { background: #9c7; }
            .provenance-strict   { background: #7bf; }
            .provenance-no-track-count,
            .provenance-title-only { background: #fb7; }
            .provenance-fuzzy-title { background: #e9d; }
            .provenance-paste    { background: var(--yj-accent, #ffd43b); }
            .provenance-unknown  { background: #777; color: #fff; }

            /* ── Generic states ── */

            .empty {
                text-align: center;
                padding: 2.5rem 1rem;
                color: var(--yj-text-secondary, #b3b3b3);
                font-size: 0.9rem;
            }

            .error {
                background: rgba(200, 90, 90, 0.15);
                color: #f99;
                padding: 0.5rem 0.75rem;
                border-radius: 4px;
                display: flex;
                align-items: center;
                gap: 0.75rem;
            }

            .error button {
                margin-left: auto;
                background: transparent;
                color: inherit;
                border: 1px solid currentColor;
            }

            /* ── Dialogs (carried over) ── */

            .dialog-overlay {
                position: fixed;
                inset: 0;
                background: rgba(0, 0, 0, 0.5);
                display: flex;
                align-items: center;
                justify-content: center;
                z-index: 100;
            }

            .dialog {
                background: var(--yj-bg-surface, #222);
                padding: 1.25rem;
                border-radius: 6px;
                min-width: 420px;
                max-width: 560px;
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.1));
            }

            .dialog-header { display: flex; align-items: center; margin-bottom: 0.75rem; }
            .dialog-header h3 { margin: 0; }

            .dialog-close {
                margin-left: auto;
                background: transparent;
                color: var(--yj-text-secondary, #b3b3b3);
                border: 0;
                font-size: 1.2rem;
                cursor: pointer;
                padding: 0 0.25rem;
            }

            .dialog-close:hover { color: var(--yj-text-primary, #fff); }
            .dialog p { margin: 0 0 1rem 0; font-size: 0.9rem; }

            .dialog input[type="url"] {
                width: 100%;
                padding: 0.4rem;
                font: inherit;
                background: var(--yj-bg-elevated, #343a40);
                color: var(--yj-text-primary, #fff);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.15));
                border-radius: 4px;
                box-sizing: border-box;
            }

            .dialog .row {
                display: flex;
                gap: 0.5rem;
                justify-content: flex-end;
                margin-top: 1rem;
            }
        `,
    ];

    @state() private folders: PendingItem[] = [];
    @state() private current: PendingItem | null = null;
    @state() private score: ScoreView | null = null;
    @state() private selectedCandidateIdx = 0;
    @state() private loading = false;
    @state() private errorMessage = '';
    @state() private dialog: 'none' | 'paste' | 'warning' | 'leave' = 'none';
    @state() private pasteURL = '';
    @state() private queueMenuOpen = false;
    // Per-folder apply-job state, keyed by groupKey.  Updated from
    // AutotagApply{Started,Progress,Finished} events emitted by the
    // backend.  Drives the sidebar status icons (running ring,
    // green check, yellow warning).  Folders without an entry
    // render the default gray question-mark icon.
    @state() private applyJobs: Map<string, ApplyJobState> = new Map();

    private unsubscribeLibraryStore?: () => void;
    private unsubscribeApplyEvents: Array<() => void> = [];
    private currentLibraryFilter: number | null = null;

    override connectedCallback(): void {
        super.connectedCallback();
        document.addEventListener('keydown', this.onKeydown);
        document.addEventListener('mousedown', this.onDocumentClickForMenu);
        // Track the global library filter so the autotag queue
        // shows only folders from the currently-selected library.
        // null means "all libraries", which the backend treats as
        // libraryID=0.
        this.currentLibraryFilter = libraryStore.getSelectedLibraryId();
        this.unsubscribeLibraryStore = libraryStore.subscribe(() => {
            const next = libraryStore.getSelectedLibraryId();
            if (next === this.currentLibraryFilter) return;
            this.currentLibraryFilter = next;
            void this.startQueue();
        });

        // Listen for per-job progress events so the sidebar can
        // render running/completed/failed indicators.  EventsOn
        // returns an unsubscribe; collected and called on disconnect.
        this.unsubscribeApplyEvents.push(
            EventsOn(Events.AutotagApplyStarted, (data: { groupKey: string; total: number }) => {
                this.onApplyStarted(data);
            }),
            EventsOn(Events.AutotagApplyProgress, (data: {
                groupKey: string; current: number; total: number;
                succeeded: number; failed: number;
            }) => {
                this.onApplyProgress(data);
            }),
            EventsOn(Events.AutotagApplyFinished, (data: {
                groupKey: string; succeeded: number; failed: number; error: string;
            }) => {
                void this.onApplyFinished(data);
            }),
            // Prefetch worker drives sidebar pill population in the
            // background.  Reload the folder list (debounced) so
            // newly-scored rows pick up their pill text.
            EventsOn(Events.AutotagPrefetchProgress, () => {
                this.schedulePrefetchRefresh();
            }),
            EventsOn(Events.AutotagPrefetchFinished, () => {
                this.schedulePrefetchRefresh();
            }),
        );

        void this.startQueue();
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        document.removeEventListener('keydown', this.onKeydown);
        document.removeEventListener('mousedown', this.onDocumentClickForMenu);
        this.unsubscribeLibraryStore?.();
        for (const fn of this.unsubscribeApplyEvents) fn();
        this.unsubscribeApplyEvents = [];
        if (this.prefetchRefreshTimer !== undefined) {
            clearTimeout(this.prefetchRefreshTimer);
            this.prefetchRefreshTimer = undefined;
        }
    }

    /** Debounced reload of the folder list — called from prefetch
     *  events.  Coalesces a flurry of per-group score updates
     *  into one DB query + render pass every ~500ms. */
    private prefetchRefreshTimer: number | undefined;

    private schedulePrefetchRefresh(): void {
        if (this.prefetchRefreshTimer !== undefined) return;
        this.prefetchRefreshTimer = window.setTimeout(() => {
            this.prefetchRefreshTimer = undefined;
            void this.loadFolders();
        }, 500);
    }

    /* ── Apply-job event handlers ── */

    private updateApplyJob(groupKey: string, patch: Partial<ApplyJobState>): void {
        const next = new Map(this.applyJobs);
        const prev = next.get(groupKey) ?? { state: 'running', current: 0, total: 0 };
        next.set(groupKey, { ...prev, ...patch });
        this.applyJobs = next;
    }

    private onApplyStarted({ groupKey, total }: { groupKey: string; total: number }): void {
        this.updateApplyJob(groupKey, { state: 'running', current: 0, total });
    }

    private onApplyProgress({ groupKey, current, total }:
        { groupKey: string; current: number; total: number;
          succeeded: number; failed: number }): void {
        this.updateApplyJob(groupKey, { state: 'running', current, total });
    }

    private async onApplyFinished({ groupKey, succeeded, failed, error }:
        { groupKey: string; succeeded: number; failed: number; error: string }): Promise<void> {
        const allFailed = error !== '' || (succeeded === 0 && failed > 0);
        if (allFailed) {
            this.updateApplyJob(groupKey, {
                state: 'failed',
                error: error || `${failed} of ${failed} tracks failed`,
            });
            // Toast the error so the user sees it.
            this.errorMessage = error
                ? `Apply failed: ${error}`
                : `Apply failed: ${failed} of ${failed} tracks could not be written.`;
            // Refresh so the row picks up any DB state change.
            await this.loadFolders();
            return;
        }

        // Success path: mark completed for one render so the green
        // check shows briefly, then refresh — the now-confirmed
        // folder drops out of the list.
        this.updateApplyJob(groupKey, { state: 'completed' });
        await this.loadFolders();
        const next = new Map(this.applyJobs);
        next.delete(groupKey);
        this.applyJobs = next;
    }

    /** Returns the library ID to send to the backend.  Wails
     *  passes 0 to mean "all libraries"; the store uses null. */
    private libraryFilterID(): number {
        return this.currentLibraryFilter ?? 0;
    }

    /* ── Stable callback refs ── */

    private onApplyClick = () => { void this.onApply(); };
    private onSkipClick = () => { void this.onSkip(); };
    private onLeaveClick = () => { void this.onLeave(); };
    private onOpenPaste = () => { this.dialog = 'paste'; };
    private onDismissError = () => { this.errorMessage = ''; };
    private onPasteCancel = () => { this.dialog = 'none'; this.pasteURL = ''; };
    private onPasteSubmit = () => { void this.submitPasteURL(); };
    private onPasteInput = (e: Event) => {
        this.pasteURL = (e.target as HTMLInputElement).value;
    };
    private onPasteKeydown = (e: KeyboardEvent) => {
        if (e.key === 'Enter') this.onPasteSubmit();
    };
    private onWarningCancel = () => { this.dialog = 'none'; };
    private onWarningContinue = async () => {
        if (!this.current) return;
        this.markWarningAcked(this.current.libraryId);
        await AckLibraryWarning(this.current.libraryId);
        this.dialog = 'none';
        await this.executeApply();
    };
    private onLeaveCancel = () => { this.dialog = 'none'; };
    private onLeaveConfirm = async () => {
        if (!this.current) return;
        this.dialog = 'none';
        await LeaveAsIs(this.current.groupKey);
        await this.refreshAfterAction();
    };

    private toggleQueueMenu = (e: Event) => {
        e.stopPropagation();
        this.queueMenuOpen = !this.queueMenuOpen;
    };

    private onClearCompleted = async (e: Event) => {
        e.stopPropagation();
        this.queueMenuOpen = false;
        try {
            await ClearCompletedEntries(this.libraryFilterID());
            await this.loadFolders();
        } catch (err) {
            this.errorMessage = `Clear completed failed: ${(err as Error).message}`;
        }
    };

    /** Click-outside handler that closes the queue menu when the
     *  user clicks anywhere except the menu itself.  Bound at
     *  connect/disconnect alongside the keyboard listener. */
    private onDocumentClickForMenu = (e: MouseEvent) => {
        if (!this.queueMenuOpen) return;
        const path = e.composedPath();
        for (const el of path) {
            if (!(el instanceof HTMLElement)) continue;
            if (el.classList?.contains('folders-menu')) return;
            if (el.classList?.contains('folders-menu-trigger')) return;
        }
        this.queueMenuOpen = false;
    };

    /* ── Queue ops ── */

    private async startQueue(): Promise<void> {
        this.loading = true;
        // Reset state so a library-filter change doesn't leave a
        // stale folder selected from the previous library.
        this.current = null;
        this.score = null;
        this.selectedCandidateIdx = 0;
        try {
            await StartAutotagQueue(this.libraryFilterID());
            await this.loadFolders();
            // Auto-select the first folder so the user lands on
            // something to review.
            const first = this.folders[0];
            if (first) {
                await this.selectFolder(first.groupKey);
            }
        } finally {
            this.loading = false;
        }
    }

    private async loadFolders(): Promise<void> {
        try {
            const list = await ListPendingFolders(this.libraryFilterID());
            this.folders = list ?? [];
        } catch (e) {
            this.errorMessage = `Failed to load pending folders: ${(e as Error).message}`;
        }
    }

    private async selectFolder(groupKey: string): Promise<void> {
        this.errorMessage = '';
        this.score = null;
        this.selectedCandidateIdx = 0;
        // Show what we already know about this folder while
        // candidates load — keeps the header from blanking.
        const inList = this.folders.find((f) => f.groupKey === groupKey);
        this.current = inList ?? null;
        try {
            if (!inList) {
                const fetched = await GetPendingFolder(groupKey);
                this.current = fetched ?? null;
            }
            if (this.current) {
                await this.loadCandidates(this.current.groupKey);
            }
        } catch (e) {
            this.errorMessage = `Failed to load folder: ${(e as Error).message}`;
        }
    }

    private async loadCandidates(groupKey: string): Promise<void> {
        this.loading = true;
        try {
            const result = await GetCandidates(groupKey);
            // The user may have clicked a different folder while
            // GetCandidates was in flight — drop the result
            // silently if the selected folder has changed,
            // otherwise we'd render the *previous* folder's
            // candidates against the *new* folder's header.
            if (this.current?.groupKey !== groupKey) return;
            this.score = result;
        } catch (e) {
            if (this.current?.groupKey !== groupKey) return;
            this.errorMessage = `Failed to score candidates: ${(e as Error).message}`;
        } finally {
            this.loading = false;
        }
    }

    private async refreshAfterAction(): Promise<void> {
        const previousKey = this.current?.groupKey ?? '';
        await this.loadFolders();

        // Skipped + completed entries now stay visible at the
        // bottom of the sidebar, so "next folder" is the next
        // *pending* one — not the next any-status row.  Otherwise
        // applying every album would land the user in the
        // Completed section staring at folders they can no longer
        // act on.
        const pending = this.folders.filter(
            (f) => f.status !== 'confirmed' && f.status !== 'skipped',
        );

        if (pending.length === 0) {
            this.current = null;
            this.score = null;
            return;
        }

        const prevIdx = this.folders.findIndex((f) => f.groupKey === previousKey);
        let next: PendingItem | undefined;
        if (prevIdx >= 0) {
            for (let i = prevIdx + 1; i < this.folders.length; i++) {
                const f = this.folders[i];
                if (f && f.status !== 'confirmed' && f.status !== 'skipped') {
                    next = f;
                    break;
                }
            }
        }
        // Fall back to the first pending row when there's nothing
        // forward (or we lost our place).
        next ??= pending[0];

        if (next) {
            await this.selectFolder(next.groupKey);
        }
    }

    private hasLibraryWarningBeenAcked(libraryId: number): boolean {
        const acked = JSON.parse(localStorage.getItem(LOCAL_STORAGE_WARNING_KEY) || '[]') as number[];
        return acked.includes(libraryId);
    }

    private markWarningAcked(libraryId: number): void {
        const acked = JSON.parse(localStorage.getItem(LOCAL_STORAGE_WARNING_KEY) || '[]') as number[];
        if (!acked.includes(libraryId)) {
            acked.push(libraryId);
            localStorage.setItem(LOCAL_STORAGE_WARNING_KEY, JSON.stringify(acked));
        }
    }

    private topScore(): number {
        if (!this.score || this.score.candidates.length === 0) return 0;
        return this.score.candidates[0]?.score ?? 0;
    }

    private async onApply(): Promise<void> {
        if (!this.current || !this.score || this.score.candidates.length === 0) return;
        if (!this.hasLibraryWarningBeenAcked(this.current.libraryId)) {
            this.dialog = 'warning';
            return;
        }
        await this.executeApply();
    }

    private async executeApply(): Promise<void> {
        if (!this.current || !this.score) return;

        const cand = this.score.candidates[this.selectedCandidateIdx];
        if (!cand) {
            this.errorMessage = 'No candidate selected.';
            return;
        }

        const groupKey = this.current.groupKey;
        const mbid = cand.releaseMbid || cand.releaseGroupMbid;
        const total = this.score.localTracks.length;

        // Pre-mark the folder as running so the sidebar icon flips
        // to the progress ring immediately — the Started event will
        // fill in the authoritative total moments later.
        this.updateApplyJob(groupKey, { state: 'running', current: 0, total });

        // Fire-and-forget.  The backend emits Started/Progress/
        // Finished events that drive the sidebar from here on.
        ApplyAsync(groupKey, mbid).catch((e) => {
            const msg = (e as Error).message ?? String(e);
            // ErrApplyInFlight is a soft no-op (shouldn't happen
            // from the UI, but defensive); other errors clear the
            // running state and surface a toast.
            if (!msg.includes('apply already in flight')) {
                this.updateApplyJob(groupKey, { state: 'failed', error: msg });
                this.errorMessage = `Apply failed: ${msg}`;
            }
        });

        // Advance to the next folder right away so the user can
        // keep reviewing while the background job runs.
        await this.refreshAfterAction();
    }

    private async onSkip(): Promise<void> {
        if (!this.current) return;
        await Skip(this.current.groupKey);
        await this.refreshAfterAction();
    }

    private async onLeave(): Promise<void> {
        if (!this.current) return;
        if (this.topScore() < CONFIDENT_SCORE) {
            this.dialog = 'leave';
            return;
        }
        await LeaveAsIs(this.current.groupKey);
        await this.refreshAfterAction();
    }

    private async submitPasteURL(): Promise<void> {
        if (!this.current || !this.pasteURL.trim()) return;
        this.loading = true;
        try {
            this.score = await GetCandidatesForPasteURL(
                this.current.groupKey, this.pasteURL.trim(),
            );
            this.selectedCandidateIdx = 0;
            this.dialog = 'none';
            this.pasteURL = '';
        } catch (e) {
            this.errorMessage = `Paste URL failed: ${(e as Error).message}`;
        } finally {
            this.loading = false;
        }
    }

    /* ── Folder navigation ── */

    private async navigateFolder(delta: -1 | 1): Promise<void> {
        if (this.folders.length === 0) return;
        const currentKey = this.current?.groupKey ?? '';
        const idx = this.folders.findIndex((f) => f.groupKey === currentKey);
        const next = idx < 0 ? 0 : Math.max(0, Math.min(this.folders.length - 1, idx + delta));
        const target = this.folders[next];
        if (target && target.groupKey !== currentKey) {
            await this.selectFolder(target.groupKey);
        }
    }

    private onKeydown = (e: KeyboardEvent): void => {
        const target = e.target as HTMLElement;
        if (target && (
            target.tagName === 'INPUT' ||
            target.tagName === 'TEXTAREA' ||
            target.isContentEditable
        )) {
            if (e.key === 'Escape' && this.dialog !== 'none') {
                e.preventDefault();
                this.dialog = 'none';
                this.pasteURL = '';
            }
            return;
        }

        if (e.key === 'Escape') {
            if (this.dialog !== 'none') {
                e.preventDefault();
                this.dialog = 'none';
                this.pasteURL = '';
            }
            return;
        }

        if (this.dialog !== 'none') return;

        switch (e.key.toLowerCase()) {
            case 'a': e.preventDefault(); void this.onApply(); break;
            case 's': e.preventDefault(); void this.onSkip(); break;
            case 'l': e.preventDefault(); void this.onLeave(); break;
            case 'u': e.preventDefault(); this.dialog = 'paste'; break;
            case 'arrowdown':
                e.preventDefault();
                void this.navigateFolder(1);
                break;
            case 'arrowup':
                e.preventDefault();
                void this.navigateFolder(-1);
                break;
        }
    };

    /* ── Version clustering ── */

    /**
     * Cluster candidates by release-group MBID.  Candidates with no
     * RG MBID (paste-only) bucket by normalised title+artist.  Each
     * cluster is collapsed to its highest-scoring candidate by
     * default; the user can drill into the alternatives via the
     * version-row click handler.
     */
    private clusterVersions(candidates: CandidateView[]): VersionCluster[] {
        const clusters = new Map<string, VersionCluster>();
        for (let i = 0; i < candidates.length; i++) {
            const c = candidates[i]!;
            const key = c.releaseGroupMbid
                || `paste:${(c.title || '').toLowerCase()}|${(c.artistCredit || '').toLowerCase()}`;
            const existing = clusters.get(key);
            if (existing) {
                existing.candidates.push(c);
                if (c.score > existing.candidates[existing.bestIdx]!.score) {
                    existing.bestIdx = existing.candidates.length - 1;
                }
            } else {
                clusters.set(key, {
                    key,
                    label: c.title || '(untitled)',
                    sublabel: this.versionSublabel(c),
                    candidates: [c],
                    bestIdx: 0,
                });
            }
        }
        // Sort clusters by their best candidate's score, descending.
        return [...clusters.values()].sort((a, b) => {
            const sa = a.candidates[a.bestIdx]!.score;
            const sb = b.candidates[b.bestIdx]!.score;
            return sb - sa;
        });
    }

    private versionSublabel(c: CandidateView): string {
        const parts: string[] = [];
        if (c.date) parts.push(c.date.slice(0, 4));
        if (c.country) parts.push(c.country);
        if (c.trackCount) parts.push(`${c.trackCount} tracks`);
        return parts.join(' · ');
    }

    /**
     * Resolve the index into score.candidates of the currently-
     * selected candidate.  Driven by selectedCandidateIdx; clamped
     * to the available range.
     */
    private currentCandidate(): CandidateView | null {
        if (!this.score) return null;
        return this.score.candidates[this.selectedCandidateIdx] ?? null;
    }

    private async selectCandidateByIdx(idx: number): Promise<void> {
        if (!this.score || idx < 0 || idx >= this.score.candidates.length) return;
        this.selectedCandidateIdx = idx;
        // Lazy-load cover art for non-top candidates.  The backend
        // populates art for the top one eagerly; everything else
        // arrives empty until the user picks it.
        const cand = this.score.candidates[idx]!;
        if (!cand.coverArtUrl) {
            try {
                const url = await GetCandidateCoverArt(
                    cand.releaseMbid, cand.releaseGroupMbid,
                );
                if (url && this.score) {
                    // ScoreView is a Wails-generated class (with
                    // bound methods) so we mutate in place and
                    // request a re-render rather than spreading,
                    // which would drop convertValues().
                    cand.coverArtUrl = url;
                    this.requestUpdate();
                }
            } catch {
                // Network failure on cover art shouldn't break the
                // review; just leave the placeholder.
            }
        }
    }

    /* ── Diff helpers ── */

    private formatLength(ms: number): string {
        if (!ms || ms <= 0) return '';
        const total = Math.round(ms / 1000);
        const m = Math.floor(total / 60);
        const s = total % 60;
        return `${m}:${s.toString().padStart(2, '0')}`;
    }

    /** Render an inline word/punct-level diff for a string field.
     *  Equal segments render plain; removed segments render with
     *  red strikethrough; added segments render in green.  This is
     *  the git-style word-diff rendering — apostrophe-type swaps
     *  and case differences highlight just the changed glyphs
     *  instead of the whole title. */
    private diffText(local: string, candidate: string): TemplateResult | string {
        if (local === candidate || candidate === '') return candidate || local || '';
        const segments = inlineDiff(local, candidate);
        return html`${segments.map((seg) => {
            if (seg.type === 'equal') return seg.text;
            if (seg.type === 'remove') return html`<span class="diff-old">${seg.text}</span>`;
            return html`<span class="diff-new">${seg.text}</span>`;
        })}`;
    }

    /** Render an inline diff for a number-valued field (track #).
     *  Renders nothing for matched values. */
    private diffNumber(local: number, candidate: number): TemplateResult | string {
        if (local === candidate || candidate === 0) return String(candidate || local || '');
        if (!local) return html`<span class="diff-new">${candidate}</span>`;
        return html`<span class="diff-old">${local}</span><span class="diff-new">${candidate}</span>`;
    }

    /* ── Render ── */

    private renderHeader() {
        if (!this.current) return nothing;
        return html`
            <div class="header">
                <div>
                    <h2>${this.current.albumName || '(no album)'}</h2>
                    <div class="meta">
                        ${this.current.albumArtist || 'Unknown artist'}
                        · ${this.current.trackCount} tracks
                        · library: ${this.current.libraryName || `#${this.current.libraryId}`}
                        ${this.current.folderSubPath
                            ? html`<span class="folder-path"
                                title=${this.current.folderSubPath}
                            >· ${this.current.folderSubPath}</span>`
                            : nothing}
                    </div>
                </div>
                <div class="actions">
                    <button @click=${this.onApplyClick}
                            ?disabled=${this.applyJobs.get(this.current.groupKey)?.state === 'running'}>
                        Apply (A)
                    </button>
                    <button class="secondary" @click=${this.onSkipClick}>Skip (S)</button>
                    <button class="secondary" @click=${this.onLeaveClick}>Leave as-is (L)</button>
                    <button class="secondary" @click=${this.onOpenPaste}>Paste URL (U)</button>
                </div>
            </div>
        `;
    }

    private renderFolderSidebar() {
        // Group folders by review state.  The backend now returns
        // pending + skipped + confirmed in a single list (sorted by
        // score desc), so we re-bucket here for the section
        // headers.  'matched' is treated as pending — it's an old
        // status that means "scorer found a top candidate but the
        // user hasn't acted yet", same UX as pending.
        const pending: PendingItem[] = [];
        const skipped: PendingItem[] = [];
        const completed: PendingItem[] = [];

        for (const f of this.folders) {
            switch (f.status) {
                case 'skipped':
                    skipped.push(f);
                    break;
                case 'confirmed':
                    completed.push(f);
                    break;
                default:
                    pending.push(f);
            }
        }

        return html`
            <div class="folders">
                <div class="folders-header">
                    <span>Pending (${pending.length})</span>
                    ${completed.length > 0 ? html`
                        <button class="folders-menu-trigger"
                                title="Queue actions"
                                @click=${this.toggleQueueMenu}>⋯</button>
                        ${this.queueMenuOpen ? html`
                            <div class="folders-menu"
                                 @click=${(e: Event) => e.stopPropagation()}>
                                <button @click=${this.onClearCompleted}>
                                    Clear ${completed.length} completed
                                    ${completed.length === 1 ? 'entry' : 'entries'}
                                </button>
                            </div>
                        ` : nothing}
                    ` : nothing}
                </div>
                ${this.folders.length === 0
                    ? html`<div class="empty">Queue is empty.</div>`
                    : html`
                        ${pending.map((f) => this.renderFolderRow(f))}
                        ${skipped.length > 0 ? html`
                            <div class="folders-section-header">
                                Skipped (${skipped.length})
                            </div>
                            ${skipped.map((f) => this.renderFolderRow(f))}
                        ` : nothing}
                        ${completed.length > 0 ? html`
                            <div class="folders-section-header">
                                Completed (${completed.length})
                            </div>
                            ${completed.map((f) => this.renderFolderRow(f))}
                        ` : nothing}
                    `}
            </div>
        `;
    }

    private renderFolderRow(f: PendingItem): TemplateResult {
        const sectionClass = f.status === 'skipped' ? 'skipped'
            : f.status === 'confirmed' ? 'completed'
            : '';
        const currentClass = f.groupKey === this.current?.groupKey ? 'current' : '';
        return html`
            <div class="folder-row ${sectionClass} ${currentClass}"
                 @click=${() => { void this.selectFolder(f.groupKey); }}>
                <div class="status-icon">${this.renderStatusIcon(f)}</div>
                <div class="album">${f.albumName || '(no album)'}</div>
                <div class="artist">${f.albumArtist || 'Unknown artist'}</div>
                <div class="count">${f.trackCount} tracks</div>
                ${f.score > 0 ? html`
                    <div class="match-pill ${f.score >= 0.85 ? 'high' : ''}">
                        ${Math.round(f.score * 100)}%
                    </div>
                ` : html`
                    <div class="match-pill pending"
                         title="Match score being computed in the background">
                        …
                    </div>
                `}
            </div>
        `;
    }

    /** Renders the left-side status indicator for one folder row.
     *  Priority: in-flight apply job state first (running ring,
     *  green check, yellow warning), then the row's persisted
     *  status (skipped → muted minus circle, confirmed → green
     *  check, anything else → gray question-mark). */
    private renderStatusIcon(f: PendingItem): TemplateResult {
        const job = this.applyJobs.get(f.groupKey);
        if (job) {
            if (job.state === 'running') {
                return this.renderProgressRing(job.current, job.total);
            }
            if (job.state === 'completed') {
                return html`<wa-icon name="circle-check"
                    style="color: #5cb85c; font-size: 1.1rem;"></wa-icon>`;
            }
            if (job.state === 'failed') {
                return html`<wa-icon name="triangle-exclamation"
                    title=${job.error ?? 'apply failed'}
                    style="color: #f0ad4e; font-size: 1.1rem;"></wa-icon>`;
            }
        }
        if (f.status === 'confirmed') {
            return html`<wa-icon name="circle-check"
                style="color: #5cb85c; font-size: 1.1rem;"></wa-icon>`;
        }
        if (f.status === 'skipped') {
            return html`<wa-icon name="circle-minus"
                style="color: var(--yj-text-tertiary, #888); font-size: 1.1rem;"></wa-icon>`;
        }
        return html`<wa-icon name="circle-question"
            style="color: var(--yj-text-tertiary, #888); font-size: 1.1rem;"></wa-icon>`;
    }

    /** Inline circular progress ring sized to match the icon set
     *  (~18 px).  Foreground stroke length proportional to
     *  current/total; rotated -90° so the arc starts at 12 o'clock. */
    private renderProgressRing(current: number, total: number): TemplateResult {
        const r = 7;
        const c = 2 * Math.PI * r;
        const fraction = total > 0 ? Math.min(1, Math.max(0, current / total)) : 0;
        const dash = (fraction * c).toFixed(2);
        return html`
            <svg width="18" height="18" viewBox="0 0 18 18"
                 style="transform: rotate(-90deg); display: block;">
                <circle cx="9" cy="9" r="${r}" fill="none"
                        stroke="rgba(255,255,255,0.18)" stroke-width="2"></circle>
                <circle cx="9" cy="9" r="${r}" fill="none"
                        stroke="var(--yj-accent, #ffd43b)" stroke-width="2"
                        stroke-linecap="round"
                        stroke-dasharray="${dash} ${c.toFixed(2)}"></circle>
            </svg>
        `;
    }

    private renderAlbumCard(cand: CandidateView) {
        const localAlbum = this.current?.albumName ?? '';
        const localArtist = this.current?.albumArtist ?? '';
        const releaseYear = (cand.date ?? '').slice(0, 4);
        const originalYear = (cand.originalDate ?? '').slice(0, 4);
        const subParts: string[] = [];
        if (originalYear && releaseYear && originalYear !== releaseYear) {
            // Remaster/reissue: show the original year prominently,
            // mark the technical re-release year as such.
            subParts.push(`${originalYear} (${releaseYear} reissue)`);
        } else if (releaseYear) {
            subParts.push(releaseYear);
        }
        if (cand.country) subParts.push(cand.country);
        if (cand.status) subParts.push(cand.status);
        if (cand.trackCount) subParts.push(`${cand.trackCount} tracks`);

        return html`
            <div class="album-card">
                ${cand.coverArtUrl
                    ? html`<img class="cover" src=${cand.coverArtUrl} alt="cover art">`
                    : html`<div class="cover placeholder">No cover</div>`}
                <div class="album-meta">
                    <div class="album-title">${this.diffText(localAlbum, cand.title)}</div>
                    <div class="album-artist">${this.diffText(localArtist, cand.artistCredit)}</div>
                    <div class="album-line">${subParts.join(' · ') || '\u00a0'}</div>
                    <div class="album-line">
                        <span class="score-badge ${cand.score >= 0.85 ? 'high' : ''}">
                            ${(cand.score * 100).toFixed(0)}% match
                        </span>
                        ${this.renderProvenanceBadge(cand)}
                    </div>
                </div>
            </div>
        `;
    }

    private renderProvenanceBadge(cand: CandidateView) {
        const prov = cand.provenance || 'unknown';
        const label = prov === 'local' ? 'local'
            : prov === 'paste' ? 'paste'
            : prov === 'strict' ? 'exact'
            : prov === 'no-track-count' ? 'any-tracks'
            : prov === 'title-only' ? 'title-only'
            : prov === 'fuzzy-title' ? 'fuzzy'
            : prov;
        return html`<span class="provenance-badge provenance-${prov}">${label}</span>`;
    }

    /**
     * Compute the album-level "what would change" summary for the
     * given candidate.  This pairs with renderMatchDetails to give
     * the user a concrete answer to "why isn't this 100%?" — the
     * tracklist already shows per-track title/length/# diffs, but
     * subtle length drift (<2s) is suppressed there and album-
     * level metadata factors aren't visible at all.
     */
    private computeMatchSummary(cand: CandidateView) {
        let paired = 0;
        let missingFromFolder = 0;
        let unmatchedInFolder = 0;
        let titleDiffs = 0;
        let trackNumDiffs = 0;
        let visibleLengthDiffs = 0;
        let subtleLengthDiffs = 0;
        let driftSum = 0;
        let driftCount = 0;

        const SUBTLE_LENGTH_MAX_MS = 2000;

        for (const a of cand.alignments) {
            if (a.status === 'matched' || a.status === 'mismatched') {
                paired++;
                const local = a.localIndex >= 0
                    ? this.score?.localTracks[a.localIndex] ?? null
                    : null;
                if (local) {
                    if (local.title !== a.candidateTitle) titleDiffs++;
                    if (
                        a.candidatePosition > 0
                        && local.trackNumber > 0
                        && local.trackNumber !== a.candidatePosition
                    ) {
                        trackNumDiffs++;
                    }
                }
                if (a.lengthDeltaMs > SUBTLE_LENGTH_MAX_MS) {
                    visibleLengthDiffs++;
                } else if (a.lengthDeltaMs > 0) {
                    subtleLengthDiffs++;
                    driftSum += a.lengthDeltaMs;
                    driftCount++;
                }
            } else if (a.status === 'missing') {
                missingFromFolder++;
            } else if (a.status === 'unmatched') {
                unmatchedInFolder++;
            }
        }

        const localAlbum = (this.current?.albumName ?? '').trim();
        const candAlbum = (cand.title ?? '').trim();
        const localArtist = (this.current?.albumArtist ?? '').trim();
        const candArtist = (cand.artistCredit ?? '').trim();

        return {
            paired,
            missingFromFolder,
            unmatchedInFolder,
            titleDiffs,
            trackNumDiffs,
            visibleLengthDiffs,
            subtleLengthDiffs,
            avgDriftMs: driftCount > 0 ? driftSum / driftCount : 0,
            albumTitleMatches: localAlbum === '' || localAlbum === candAlbum,
            albumArtistMatches: localArtist === '' || localArtist === candArtist,
        };
    }

    private renderMatchDetails(cand: CandidateView) {
        const s = this.computeMatchSummary(cand);
        const totalCand = s.paired + s.missingFromFolder;
        const items: TemplateResult[] = [];

        // ── Track pairing ──────────────────────────────────────
        if (s.missingFromFolder === 0 && s.unmatchedInFolder === 0) {
            items.push(html`
                <li class="ok">
                    All ${s.paired} ${s.paired === 1 ? 'track' : 'tracks'} paired
                </li>
            `);
        } else {
            if (s.missingFromFolder > 0) {
                items.push(html`
                    <li class="warn">
                        ${s.missingFromFolder} ${s.missingFromFolder === 1 ? 'track' : 'tracks'}
                        on candidate but not in folder
                    </li>
                `);
            }
            if (s.unmatchedInFolder > 0) {
                items.push(html`
                    <li class="warn">
                        ${s.unmatchedInFolder} ${s.unmatchedInFolder === 1 ? 'track' : 'tracks'}
                        in folder but not on candidate
                    </li>
                `);
            }
        }

        // ── Title diffs (pointer to tracklist) ─────────────────
        if (s.titleDiffs > 0) {
            items.push(html`
                <li class="warn">
                    ${s.titleDiffs} of ${s.paired} ${s.titleDiffs === 1 ? 'title differs' : 'titles differ'}
                    — see tracklist
                </li>
            `);
        } else if (s.paired > 0) {
            items.push(html`<li class="ok">All track titles match</li>`);
        }

        // ── Track-number diffs ─────────────────────────────────
        if (s.trackNumDiffs > 0) {
            items.push(html`
                <li class="warn">
                    ${s.trackNumDiffs} track ${s.trackNumDiffs === 1 ? 'number' : 'numbers'} would change
                </li>
            `);
        }

        // ── Length diffs: visible (>2s) vs subtle (<=2s) ───────
        if (s.visibleLengthDiffs > 0) {
            items.push(html`
                <li class="warn">
                    ${s.visibleLengthDiffs}
                    ${s.visibleLengthDiffs === 1 ? 'track length differs' : 'track lengths differ'}
                    by more than 2s — see tracklist
                </li>
            `);
        }
        if (s.subtleLengthDiffs > 0) {
            const avg = (s.avgDriftMs / 1000).toFixed(1);
            items.push(html`
                <li class="info">
                    ${s.subtleLengthDiffs}
                    ${s.subtleLengthDiffs === 1 ? 'track' : 'tracks'} drift by ~${avg}s
                    (under 2s, suppressed in tracklist)
                </li>
            `);
        }

        // ── Album header diffs (mirror what the header card shows) ──
        if (!s.albumTitleMatches) {
            items.push(html`<li class="warn">Album name would change</li>`);
        }
        if (!s.albumArtistMatches) {
            items.push(html`<li class="warn">Album artist would change</li>`);
        }

        // ── Release-info line: not a "diff" per se but a useful
        // signal since older / non-Official releases score lower
        // even when every track lines up.
        const releaseParts: string[] = [];
        const releaseYear = (cand.date ?? '').slice(0, 4);
        const originalYear = (cand.originalDate ?? '').slice(0, 4);
        if (originalYear && releaseYear && originalYear !== releaseYear) {
            releaseParts.push(`${originalYear} (${releaseYear} reissue)`);
        } else if (releaseYear) {
            releaseParts.push(releaseYear);
        }
        if (cand.country) releaseParts.push(cand.country);
        if (cand.status) releaseParts.push(cand.status);
        if (releaseParts.length) {
            items.push(html`
                <li class="info">Release: ${releaseParts.join(' · ')}</li>
            `);
        }

        if (totalCand > 0 && cand.trackCount > 0 && cand.trackCount !== totalCand) {
            items.push(html`
                <li class="info">
                    Candidate has ${cand.trackCount} tracks total
                </li>
            `);
        }

        const b = cand.breakdown;
        const pct = (v: number) => `${Math.round(v * 100)}%`;

        return html`
            <div class="match-details">
                <div class="md-header">
                    <span>Match details</span>
                </div>
                <ul class="md-items">${items}</ul>
                ${b ? html`
                    <div class="breakdown-line">
                        <span class="b-pair">Match <span class="b-val">${pct(cand.score)}</span></span>
                        <span class="b-pair">Title <span class="b-val">${pct(b.titleAvg)}</span></span>
                        <span class="b-pair">Length <span class="b-val">${pct(b.lengthAvg)}</span></span>
                        <span class="b-pair">Tracks <span class="b-val">${pct(b.trackCountFit)}</span></span>
                        <span class="b-pair">Release <span class="b-val">${pct(b.releaseMeta)}</span></span>
                    </div>
                ` : nothing}
            </div>
        `;
    }

    private renderLowConfidenceBanner(clusters: VersionCluster[]) {
        const top = this.score?.candidates[0];
        if (!top) return nothing;
        if (top.score >= LOW_CONFIDENCE_THRESHOLD) return nothing;
        if (clusters.length < 2) return nothing;

        const altCount = clusters.length - 1;
        return html`
            <div class="banner">
                Low confidence pick (${(top.score * 100).toFixed(0)}%).
                ${altCount} alternative ${altCount === 1 ? 'version' : 'versions'} available — review on the right.
            </div>
        `;
    }

    /**
     * Render the tracklist as a single beets-style diff: candidate
     * positions on top (grouped by disc), with each row showing the
     * field-level diff between the aligned local file and the
     * candidate track.  Folder tracks that didn't pair with any
     * candidate slot fall into a separate "Unmatched" group below;
     * candidate slots with no local file fall into a "Missing"
     * group at the same level so the user can see what's expected
     * but absent on disk.
     */
    private renderTracklist(cand: CandidateView) {
        const locals = new Map<number, LocalTrackView>();
        this.score?.localTracks.forEach((t, i) => locals.set(i, t));

        // Bucket alignments: paired (matched/mismatched) and
        // candidate-side missing get sorted into the candidate
        // tracklist by candidate position; folder-side unmatched
        // is rendered separately.
        const paired: AlignmentView[] = [];
        const missing: AlignmentView[] = [];
        const unmatched: AlignmentView[] = [];

        for (const a of cand.alignments) {
            switch (a.status) {
                case 'matched':
                case 'mismatched':
                    paired.push(a);
                    break;
                case 'missing':
                    missing.push(a);
                    break;
                case 'unmatched':
                    unmatched.push(a);
                    break;
                default:
                    paired.push(a);
            }
        }

        const candTracks = [...paired, ...missing].map((a) => ({
            ...a,
            discNumber: a.candidateDiscNumber || 1,
            position: a.candidatePosition || 0,
        }));

        const multi = isMultiDisc(candTracks);
        const grouped = groupByDisc(candTracks);
        const discs = discNumbers(grouped);

        return html`
            <div class="tracklist">
                <div class="section-header">Tracks</div>
                ${discs.map((discNum) => {
                    const rows = grouped.get(discNum) ?? [];
                    return html`
                        ${multi
                            ? html`<div class="disc-separator">Disc ${discNum}</div>`
                            : nothing}
                        ${rows.map((a) => this.renderTrackRow(a, locals))}
                    `;
                })}
                ${unmatched.length > 0 ? html`
                    <div class="section-header" style="margin-top:0.5rem;">
                        Unmatched (in folder, not in candidate)
                    </div>
                    ${unmatched.map((a) => this.renderUnmatchedRow(a, locals))}
                ` : nothing}
            </div>
        `;
    }

    private renderTrackRow(a: AlignmentView, locals: Map<number, LocalTrackView>) {
        const local = a.localIndex >= 0 ? locals.get(a.localIndex) ?? null : null;
        const cls = `track-row ${a.status}`;

        if (a.status === 'missing') {
            // Candidate has this track, folder doesn't.
            return html`
                <div class="${cls}">
                    <span class="track-pos">${a.candidatePosition}</span>
                    <span class="track-title">
                        <span class="diff-new">${a.candidateTitle}</span>
                    </span>
                    <span class="track-len">${this.formatLength(a.candidateLength)}</span>
                </div>
            `;
        }

        // Paired (matched / mismatched).  Show track-#, title,
        // length — each as a diff against the local file.
        const localTitle = local?.title ?? '';
        const localLen = local?.lengthMillis ?? 0;
        const localPos = local?.trackNumber ?? 0;

        return html`
            <div class="${cls}">
                <span class="track-pos">
                    ${this.diffNumber(localPos, a.candidatePosition)}
                </span>
                <span class="track-title">
                    ${this.diffText(localTitle, a.candidateTitle)}
                </span>
                <span class="track-len">
                    ${this.renderLengthDiff(localLen, a.candidateLength)}
                </span>
            </div>
        `;
    }

    private renderLengthDiff(localMs: number, candMs: number): TemplateResult | string {
        const candStr = this.formatLength(candMs);
        const localStr = this.formatLength(localMs);
        if (!candStr) return localStr;
        if (!localStr || localStr === candStr) return candStr;
        // Show only the candidate length when the difference is
        // tiny (<= 2s) — disk seek noise, not a real diff.
        if (Math.abs(localMs - candMs) <= 2000) return candStr;
        return html`<span class="diff-old">${localStr}</span><span class="diff-new">${candStr}</span>`;
    }

    private renderUnmatchedRow(a: AlignmentView, locals: Map<number, LocalTrackView>) {
        const local = a.localIndex >= 0 ? locals.get(a.localIndex) ?? null : null;
        const title = a.localTitle || local?.title || '(untitled)';
        const len = a.localLengthMillis || local?.lengthMillis || 0;
        const pos = local?.trackNumber ?? 0;
        return html`
            <div class="track-row unmatched">
                <span class="track-pos">${pos || '-'}</span>
                <span class="track-title">${title}</span>
                <span class="track-len">${this.formatLength(len)}</span>
            </div>
        `;
    }

    private renderVersionsSidebar(clusters: VersionCluster[]) {
        const selectedCand = this.currentCandidate();
        return html`
            <div class="versions">
                <div class="versions-header">Versions (${clusters.length})</div>
                ${clusters.length === 0
                    ? html`<div class="empty">No candidates.<br>Hit <kbd>U</kbd> to paste a URL.</div>`
                    : clusters.map((cluster) => {
                        const best = cluster.candidates[cluster.bestIdx]!;
                        const isSelected = best === selectedCand;
                        const idx = this.score?.candidates.indexOf(best) ?? -1;
                        return html`
                            <div class="version-row ${isSelected ? 'selected' : ''}"
                                 @click=${() => { void this.selectCandidateByIdx(idx); }}>
                                <div class="label">${cluster.label}</div>
                                <div class="sub">
                                    <span>${cluster.sublabel || '\u00a0'}</span>
                                    <span class="score-badge ${best.score >= 0.85 ? 'high' : ''}">
                                        ${(best.score * 100).toFixed(0)}%
                                    </span>
                                </div>
                            </div>
                        `;
                    })}
            </div>
        `;
    }

    private renderMain() {
        if (this.loading && !this.score) {
            return html`<div class="main"><div class="empty">Loading candidates\u2026</div></div>`;
        }

        if (!this.current) {
            return html`
                <div class="main">
                    <div class="empty">
                        ${this.folders.length === 0
                            ? (this.currentLibraryFilter !== null
                                ? 'No pending folders in the selected library. Switch the library filter or scan to find untagged albums.'
                                : 'No pending folders. Untagged albums appear here after a library scan.')
                            : 'Pick a folder from the list on the left to review.'}
                    </div>
                </div>
            `;
        }

        if (!this.score || this.score.candidates.length === 0) {
            return html`
                <div class="main">
                    <div class="empty">
                        No candidates found for this folder.<br>
                        Hit <kbd>U</kbd> to paste a MusicBrainz URL.
                    </div>
                </div>
            `;
        }

        const cand = this.currentCandidate();
        if (!cand) {
            return html`<div class="main"><div class="empty">Candidate index out of range.</div></div>`;
        }

        const clusters = this.clusterVersions(this.score.candidates);

        return html`
            <div class="main">
                ${this.errorMessage ? html`
                    <div class="error">
                        <span>${this.errorMessage}</span>
                        <button @click=${this.onDismissError}>Dismiss</button>
                    </div>
                ` : nothing}
                ${this.renderLowConfidenceBanner(clusters)}
                ${this.renderAlbumCard(cand)}
                ${this.renderMatchDetails(cand)}
                ${this.renderTracklist(cand)}
            </div>
        `;
    }

    /* ── Dialogs ── */

    private renderPasteDialog() {
        return html`
            <div class="dialog-overlay" @click=${(e: MouseEvent) => {
                if (e.target === e.currentTarget) this.onPasteCancel();
            }}>
                <div class="dialog">
                    <div class="dialog-header">
                        <h3>Paste MusicBrainz URL</h3>
                        <button class="dialog-close" aria-label="Close"
                                @click=${this.onPasteCancel}>×</button>
                    </div>
                    <p>
                        Paste a MusicBrainz release or release-group URL.
                        Tracks are aligned automatically when it loads.
                    </p>
                    <input type="url"
                           placeholder="https://musicbrainz.org/release/..."
                           .value=${this.pasteURL}
                           @input=${this.onPasteInput}
                           @keydown=${this.onPasteKeydown}
                           autofocus>
                    <div class="row">
                        <button class="secondary" @click=${this.onPasteCancel}>Cancel</button>
                        <button @click=${this.onPasteSubmit}>Load</button>
                    </div>
                </div>
            </div>
        `;
    }

    private renderWarningDialog() {
        if (!this.current) return nothing;
        return html`
            <div class="dialog-overlay" @click=${(e: MouseEvent) => {
                if (e.target === e.currentTarget) this.onWarningCancel();
            }}>
                <div class="dialog">
                    <div class="dialog-header">
                        <h3>Heads up: this rewrites audio files</h3>
                        <button class="dialog-close" aria-label="Close"
                                @click=${this.onWarningCancel}>×</button>
                    </div>
                    <p>
                        Applying autotag writes new metadata directly to every
                        track in <strong>${this.current.albumName}</strong>.
                        The change lands on disk and is not automatically
                        reversible.  Files outside this library are not
                        touched.
                    </p>
                    <p>
                        This warning shows once per library. Click Continue to
                        acknowledge for
                        <strong>${this.current.libraryName || `library #${this.current.libraryId}`}</strong>.
                    </p>
                    <div class="row">
                        <button class="secondary" @click=${this.onWarningCancel}>Cancel</button>
                        <button @click=${this.onWarningContinue}>Continue</button>
                    </div>
                </div>
            </div>
        `;
    }

    private renderLeaveDialog() {
        if (!this.current) return nothing;
        const top = this.score?.candidates[0];
        const topPct = top ? Math.round(top.score * 100) : 0;
        return html`
            <div class="dialog-overlay" @click=${(e: MouseEvent) => {
                if (e.target === e.currentTarget) this.onLeaveCancel();
            }}>
                <div class="dialog">
                    <div class="dialog-header">
                        <h3>Keep the current tags?</h3>
                        <button class="dialog-close" aria-label="Close"
                                @click=${this.onLeaveCancel}>×</button>
                    </div>
                    <p>
                        No candidate scored high enough to trust automatically
                        (top is ${topPct}%).  "Leave as-is" marks the local
                        tags on <strong>${this.current.albumName}</strong> as
                        correct and removes this folder from the review queue.
                    </p>
                    <p>
                        If the local tags are wrong, prefer <kbd>S</kbd> (Skip)
                        instead — that leaves the folder pending for later
                        review.
                    </p>
                    <div class="row">
                        <button class="secondary" @click=${this.onLeaveCancel}>Cancel</button>
                        <button @click=${this.onLeaveConfirm}>Mark as correct</button>
                    </div>
                </div>
            </div>
        `;
    }

    private renderDialog() {
        switch (this.dialog) {
            case 'paste':   return this.renderPasteDialog();
            case 'warning': return this.renderWarningDialog();
            case 'leave':   return this.renderLeaveDialog();
            default:        return nothing;
        }
    }

    override render() {
        if (this.loading && !this.current && this.folders.length === 0) {
            return html`<div class="empty">Loading pending folders\u2026</div>`;
        }

        const clusters = this.score
            ? this.clusterVersions(this.score.candidates)
            : [];

        return html`
            <div class="root">
                ${this.renderHeader()}
                ${this.renderFolderSidebar()}
                ${this.renderMain()}
                ${this.renderVersionsSidebar(clusters)}
            </div>
            ${this.renderDialog()}
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'autotag-view': AutotagView;
    }
}

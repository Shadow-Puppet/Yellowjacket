import { LitElement, html, css, nothing } from 'lit';
import type { TemplateResult } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { designTokens } from '../../styles/tokens.css';
import {
    StartAutotagQueue,
    GetCandidates,
    GetCandidatesForPasteURL,
    GetCandidateCoverArt,
    GetLocalCoverArt,
    GetPendingFolder,
    ListPendingFolders,
    ApplyAsync,
    Skip,
    LeaveAsIs,
    AckLibraryWarning,
    ClearCompletedEntries,
    SearchCandidates,
    SelectSearchCandidate,
} from '@go/autotagservice/Service';
import type { autotagservice } from '@go/models';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import { inlineDiff, normalizeStrict, isCosmeticDiff } from '../../utils/text-diff';
import { libraryStore } from '../../store/library-store';
import { notificationStore } from '../../store/notification-store';
import { describeError, explainError } from '../../utils/describe-error';
import { ViewLifecycleMixin } from '../../utils/view-lifecycle';
import { confirmAction } from '../confirm-dialog/confirm-dialog';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';

type PendingItem = autotagservice.PendingItem;
type ScoreView = autotagservice.ScoreView;
type CandidateView = autotagservice.CandidateView;
type AlignmentView = autotagservice.AlignmentView;
type SearchHitView = autotagservice.SearchHitView;

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

// Per-track detail rows behind the expandable match-detail lines.
// Each carries both sides so the dropdown can render an inline diff.
interface TitleDiffDetail {
    pos: number;
    local: string;
    candidate: string;
}

interface NumDiffDetail {
    title: string;
    local: number;
    candidate: number;
}

interface LengthDiffDetail {
    pos: number;
    title: string;
    localMs: number;
    candidateMs: number;
    deltaMs: number;
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
export class AutotagView extends ViewLifecycleMixin(LitElement) {
    /* The A/S/L/U/F and arrow keys are registered shortcuts in the
     * `autotag` panel scope, not a document listener of this view's own.
     * Two document keydown handlers with no arbitration is finding H-2:
     * on this page `s` both skipped the album and toggled shuffle. */
    protected override shortcutScope = 'autotag';

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
                grid-template-columns: 240px 1fr;
                grid-template-rows: auto 1fr;
                grid-template-areas:
                    "header header"
                    "folders main";
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
                display: flex;
                align-items: center;
                gap: 0.35rem;
                width: 100%;
                padding: 0.55rem 0.5rem 0.55rem 0.75rem;
                font-size: 0.78rem;
                font-family: inherit;
                color: var(--yj-text-secondary, #b3b3b3);
                text-transform: uppercase;
                letter-spacing: 0.4px;
                background: transparent;
                border: 0;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
                cursor: pointer;
                text-align: left;
            }

            .folders-section-header:hover {
                color: var(--yj-text-primary, #fff);
            }

            /* Collapsible-section toggle used in the Pending header —
               transparent button that inherits the header's type. */
            .section-toggle {
                display: flex;
                align-items: center;
                gap: 0.35rem;
                flex: 1;
                min-width: 0;
                padding: 0;
                background: transparent;
                border: 0;
                font: inherit;
                letter-spacing: inherit;
                text-transform: inherit;
                color: inherit;
                cursor: pointer;
                text-align: left;
            }

            .section-chevron {
                font-size: 0.85rem;
                flex-shrink: 0;
                color: var(--yj-text-tertiary, #888);
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

            /* Skeleton placeholders — the layout (header + panes) paints
               immediately and these shimmer blocks stand in for data
               that's still loading (folder list, candidate scoring),
               instead of a blank full-screen "Loading…". */
            .skeleton {
                position: relative;
                overflow: hidden;
                background: var(--yj-bg-elevated, #343a40);
                border-radius: 4px;
            }

            .skeleton::after {
                content: '';
                position: absolute;
                inset: 0;
                transform: translateX(-100%);
                background: linear-gradient(
                    90deg,
                    transparent,
                    rgba(255, 255, 255, 0.06),
                    transparent
                );
                animation: yj-shimmer 1.2s ease-in-out infinite;
            }

            @media (prefers-reduced-motion: reduce) {
                .skeleton::after { animation: none; }
            }

            @keyframes yj-shimmer {
                100% { transform: translateX(100%); }
            }

            .sk-line { height: 0.85rem; }
            .sk-title { height: 1.4rem; width: 65%; }
            .sk-artist { height: 1rem; width: 45%; }
            .sk-cover { width: 180px; height: 180px; border-radius: 4px; }
            .sk-pill { height: 1rem; width: 3.5rem; border-radius: 999px; }

            .sk-folder-row {
                display: grid;
                grid-template-columns: 22px 1fr;
                column-gap: 0.55rem;
                row-gap: 0.35rem;
                padding: 0.55rem 0.75rem 1.1rem;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.04));
            }

            .sk-folder-row .sk-icon {
                grid-column: 1;
                grid-row: 1 / span 2;
                width: 18px;
                height: 18px;
                border-radius: 50%;
                align-self: center;
            }

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
                margin: 0;
                padding: 0;
                display: flex;
                flex-direction: column;
                gap: 0.1rem;
            }

            .md-item {
                line-height: 1.35;
            }

            .md-item > summary,
            .md-item:not(.expandable) {
                display: flex;
                align-items: baseline;
                gap: 0.45rem;
                padding: 0.15rem 0;
            }

            .md-item.expandable > summary {
                cursor: pointer;
                list-style: none;
                border-radius: 3px;
            }

            .md-item.expandable > summary::-webkit-details-marker { display: none; }
            .md-item.expandable > summary:hover {
                background: var(--yj-bg-elevated, #343a40);
            }

            .md-mark {
                display: inline-block;
                width: 0.9em;
                flex-shrink: 0;
                text-align: center;
            }

            .md-mark-ok::before   { content: '✓'; color: #9be09b; }
            .md-mark-warn::before { content: '⚠'; color: #ffd089; }
            .md-mark-info::before { content: '○'; color: var(--yj-text-tertiary, #888); }

            .md-text { flex: 1; min-width: 0; }

            .md-count {
                font-variant-numeric: tabular-nums;
                color: #9be09b;
                font-weight: 500;
            }

            .md-count.bad { color: #ffd089; }

            .md-item.ok   { color: var(--yj-text-secondary, #b3b3b3); }
            .md-item.warn { color: var(--yj-text-primary, #fff); }
            .md-item.info { color: var(--yj-text-secondary, #b3b3b3); }

            .md-chevron {
                flex-shrink: 0;
                font-size: 0.8rem;
                color: var(--yj-text-tertiary, #888);
                transition: transform 0.15s ease;
            }

            @media (prefers-reduced-motion: reduce) {
                .md-chevron { transition: none; }
            }

            .md-item.expandable[open] > summary .md-chevron {
                transform: rotate(90deg);
            }

            .md-body {
                margin: 0.15rem 0 0.35rem 1.35rem;
                padding: 0.35rem 0.5rem;
                border-left: 2px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.12));
                background: var(--yj-bg-base, rgba(0, 0, 0, 0.18));
                border-radius: 0 4px 4px 0;
                display: flex;
                flex-direction: column;
                gap: 0.2rem;
                font-size: 0.82rem;
            }

            .md-diff-row {
                display: flex;
                align-items: baseline;
                gap: 0.5rem;
                line-height: 1.3;
            }

            .md-diff-pos {
                flex-shrink: 0;
                min-width: 1.4rem;
                text-align: right;
                color: var(--yj-text-tertiary, #888);
                font-variant-numeric: tabular-nums;
                font-size: 0.76rem;
            }

            .md-diff-text {
                flex: 1;
                min-width: 0;
                color: var(--yj-text-primary, #fff);
            }

            .md-diff-nums {
                flex-shrink: 0;
                display: inline-flex;
                align-items: baseline;
                gap: 0.3rem;
                font-variant-numeric: tabular-nums;
            }

            .md-arrow, .md-delta {
                color: var(--yj-text-tertiary, #888);
            }

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

            /* Cosmetic-only difference (case / punctuation): the
             * normalized strings match, so the score is unaffected.
             * Render muted + dotted rather than the alarming
             * red-strike / green so it reads as "formatting, not a
             * real change". */
            .diff-cosmetic-old {
                color: var(--yj-text-tertiary, #888);
                text-decoration: line-through dotted;
                text-decoration-thickness: 1px;
                opacity: 0.7;
            }

            .diff-cosmetic-new {
                color: var(--yj-text-secondary, #b3b3b3);
                border-bottom: 1px dotted var(--yj-text-tertiary, #888);
            }

            /* ── Two-column folder-vs-candidate comparison ── */

            .compare {
                display: grid;
                grid-template-columns: 1fr 1fr;
                gap: 0.6rem;
                align-items: start;
            }

            .compare-col {
                background: var(--yj-bg-surface, #222);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.1));
                border-radius: 6px;
                padding: 0.5rem 0;
                min-width: 0;
            }

            .compare-col-header {
                display: flex;
                align-items: baseline;
                justify-content: space-between;
                padding: 0.2rem 0.9rem 0.45rem;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
                margin-bottom: 0.3rem;
            }

            .compare-col-header .title {
                font-size: 0.78rem;
                text-transform: uppercase;
                letter-spacing: 0.4px;
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .compare-col-header .count {
                font-size: 0.72rem;
                color: var(--yj-text-tertiary, #888);
                font-variant-numeric: tabular-nums;
            }

            /* Per-column album header — each comparison column shows
             * its own artwork + album/artist so the local (embedded)
             * and candidate (fetched) identities sit side by side
             * without any diff coloring. */
            .cc-album {
                display: grid;
                grid-template-columns: 84px 1fr;
                gap: 0.75rem;
                padding: 0.6rem 0.9rem 0.7rem;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
                margin-bottom: 0.3rem;
                align-items: center;
            }

            .cc-cover {
                width: 84px;
                height: 84px;
                border-radius: 4px;
                object-fit: cover;
                display: block;
                background: var(--yj-bg-elevated, #343a40);
            }

            .cc-cover.placeholder {
                display: flex;
                align-items: center;
                justify-content: center;
                color: var(--yj-text-tertiary, #888);
                font-size: 0.68rem;
                text-align: center;
                padding: 0.2rem;
                box-sizing: border-box;
            }

            .cc-meta {
                display: flex;
                flex-direction: column;
                gap: 0.15rem;
                min-width: 0;
            }

            .cc-kicker {
                font-size: 0.68rem;
                text-transform: uppercase;
                letter-spacing: 0.4px;
                color: var(--yj-text-tertiary, #888);
            }

            .cc-title {
                font-size: 1rem;
                font-weight: 600;
                line-height: 1.2;
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .cc-artist {
                font-size: 0.82rem;
                color: var(--yj-text-secondary, #b3b3b3);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .cc-line {
                font-size: 0.74rem;
                color: var(--yj-text-tertiary, #888);
                display: flex;
                align-items: center;
                gap: 0.4rem;
                flex-wrap: wrap;
                margin-top: 0.1rem;
            }

            /* A row whose partner (same data-pair) is being hovered. */
            .track-row.pair-hi { background: rgba(120, 170, 255, 0.16); }

            /* Folder-side track with no candidate partner (extra), and
             * candidate-side track with no folder partner (missing) —
             * both are gaps, both amber. */
            .track-row.extra { background: rgba(255, 200, 90, 0.10); }
            .track-row.gap-spacer { visibility: hidden; }

            /* ── Candidate picker (ranked alternatives) ── */

            .cand-picker {
                display: flex;
                gap: 0.4rem;
                overflow-x: auto;
                padding: 0.1rem;
            }

            .cand-chip {
                flex: 0 0 auto;
                display: flex;
                align-items: center;
                gap: 0.4rem;
                padding: 0.3rem 0.55rem;
                border-radius: 6px;
                cursor: pointer;
                background: var(--yj-bg-surface, #222);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.1));
                font-size: 0.8rem;
                white-space: nowrap;
                max-width: 22rem;
            }

            .cand-chip:hover { background: var(--yj-bg-elevated, #343a40); }

            .cand-chip.selected {
                border-color: var(--yj-accent, #ffd43b);
                background: var(--yj-bg-elevated, #343a40);
            }

            .cand-chip .label {
                overflow: hidden;
                text-overflow: ellipsis;
                max-width: 15rem;
            }

            /* ── Versions dropdown (editions within the active album) ── */

            .versions-select {
                font: inherit;
                font-size: 0.8rem;
                background: var(--yj-bg-elevated, #343a40);
                color: var(--yj-text-primary, #fff);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.15));
                border-radius: 4px;
                padding: 0.15rem 0.3rem;
                max-width: 100%;
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

            /* ── Dialogs ──
               The frame, the backdrop, the focus trap and the Escape
               handling belong to wa-dialog; what is left here is the
               content these two put inside it. */

            wa-dialog::part(dialog) {
                background: var(--yj-bg-surface, #212529);
                color: var(--yj-text-primary, #fff);
            }

            wa-dialog p { margin: 0 0 1rem 0; font-size: 0.9rem; }

            .url-input {
                width: 100%;
                padding: 0.4rem;
                font: inherit;
                background: var(--yj-bg-elevated, #343a40);
                color: var(--yj-text-primary, #fff);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.15));
                border-radius: 4px;
                box-sizing: border-box;
            }

            .row {
                display: flex;
                gap: 0.5rem;
                justify-content: flex-end;
            }

            /* ── In-app search dialog ── */

            .search-dialog::part(dialog) { min-width: 480px; }

            .search-kind {
                display: flex;
                gap: 1rem;
                margin-bottom: 0.6rem;
                font-size: 0.85rem;
            }

            .search-kind label {
                display: flex;
                align-items: center;
                gap: 0.3rem;
                cursor: pointer;
            }

            .search-input {
                width: 100%;
                padding: 0.4rem;
                font: inherit;
                margin-bottom: 0.5rem;
                background: var(--yj-bg-elevated, #343a40);
                color: var(--yj-text-primary, #fff);
                border: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.15));
                border-radius: 4px;
                box-sizing: border-box;
            }

            .search-results {
                margin-top: 0.75rem;
                max-height: 320px;
                overflow: auto;
                border-top: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.08));
            }

            .search-result {
                display: block;
                width: 100%;
                text-align: left;
                background: transparent;
                color: inherit;
                font: inherit;
                padding: 0.45rem 0.5rem;
                cursor: pointer;
                border: 0;
                border-bottom: 1px solid var(--yj-bg-overlay, rgba(255, 255, 255, 0.05));
                border-radius: 4px;
            }

            .search-result:hover { background: var(--yj-bg-elevated, #343a40); }

            .sr-title {
                font-weight: 500;
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            .sr-sub {
                display: flex;
                gap: 0.5rem;
                font-size: 0.78rem;
                color: var(--yj-text-secondary, #b3b3b3);
            }

            .sr-detail { color: var(--yj-text-tertiary, #888); }

            .search-empty {
                margin-top: 0.75rem;
                font-size: 0.85rem;
                color: var(--yj-text-secondary, #b3b3b3);
            }
        `,
    ];

    @state() private folders: PendingItem[] = [];
    @state() private current: PendingItem | null = null;
    @state() private score: ScoreView | null = null;
    @state() private selectedCandidateIdx = 0;
    // Data-URI for artwork embedded in the current folder's local
    // files, fetched lazily per folder so the local comparison
    // column can show "what the folder already looks like".
    @state() private localCoverUrl = '';
    // loading tracks candidate scoring for the selected folder;
    // foldersLoading tracks the folder-list fetch.  They're separate
    // so the sidebar can paint as soon as the list arrives while the
    // main pane still shows a candidate skeleton.
    @state() private loading = false;
    @state() private foldersLoading = false;
    @state() private errorMessage = '';
    // Two of the four dialogs this view used to hand-roll were plain
    // confirmations and are `confirmAction()` calls now; the two that
    // remain carry input and so are `<wa-dialog>`s in the template.
    @state() private dialog: 'none' | 'paste' | 'search' = 'none';
    @state() private pasteURL = '';

    // In-app MusicBrainz search ("suggest a candidate") dialog state.
    @state() private searchKind: 'releasegroup' | 'recording' = 'releasegroup';
    @state() private searchQuery = '';
    @state() private searchArtist = '';
    @state() private searchResults: SearchHitView[] = [];
    @state() private searchLoading = false;
    /** Whether a search has actually been run, as opposed to the fields
     *  merely being seeded from the current folder. */
    @state() private searchRan = false;
    @state() private searchError = '';
    @state() private queueMenuOpen = false;
    // Collapsible sidebar sections.  Pending stays expanded so the
    // actionable queue is always visible; the non-actionable Skipped
    // and Completed sections start collapsed to keep the list short.
    @state() private collapsedSections = new Set<string>(['skipped', 'completed']);
    // Per-folder apply-job state, keyed by groupKey.  Updated from
    // AutotagApply{Started,Progress,Finished} events emitted by the
    // backend.  Drives the sidebar status icons (running ring,
    // green check, yellow warning).  Folders without an entry
    // render the default gray question-mark icon.
    @state() private applyJobs: Map<string, ApplyJobState> = new Map();

    private queueStarted = false;
    private unsubscribeLibraryStore?: () => void;
    private unsubscribeApplyEvents: Array<() => void> = [];
    private currentLibraryFilter: number | null = null;

    /** Everything here is torn down when the view leaves the screen, not
     *  when it is disconnected — which never happens, because the view
     *  is cached (see utils/view-lifecycle.ts). */
    protected override onViewActivate(): void {
        // This view owned one last document keydown listener, for Escape,
        // because its dialogs were hand-rolled and nothing else would
        // close them. They are `wa-dialog`s now and close themselves, so
        // the shortcut service is the only thing on this page deciding
        // what a key means — which is what Phase 1 was for.
        this.listenWhileActive(
            document,
            'mousedown',
            this.onDocumentClickForMenu as EventListener,
        );

        for (const [event, handler] of [
            ['shortcut:autotag-apply', () => void this.onApply()],
            ['shortcut:autotag-skip', () => void this.onSkip()],
            ['shortcut:autotag-leave', () => void this.onLeave()],
            ['shortcut:autotag-paste', () => { this.dialog = 'paste'; }],
            ['shortcut:autotag-search', () => this.openSearch()],
            ['shortcut:autotag-next', () => void this.navigateFolder(1)],
            ['shortcut:autotag-previous', () => void this.navigateFolder(-1)],
        ] as Array<[string, () => void]>) {
            this.listenWhileActive(document, event, () => {
                // A dialog owns the keyboard while it is open.
                if (this.dialog !== 'none') return;
                handler();
            });
        }

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
        // returns an unsubscribe; collected and called on deactivate.
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

        // Starting the queue resets the selection and refetches
        // candidates over the network, so it happens once.  Returning to
        // the page only needs the folder list, which is local and may
        // have moved while the page was away.
        if (this.queueStarted) {
            void this.loadFolders();
        } else {
            this.queueStarted = true;
            void this.startQueue();
        }
    }

    protected override onViewDeactivate(): void {
        this.unsubscribeLibraryStore?.();
        this.unsubscribeLibraryStore = undefined;
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
            void this.loadFolders().then(() => this.reconcileSelection());
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

        // Some files carry the new tags and some the old, and there is
        // no way to discover that later: Blocking, by the plan's rule
        // (errors.C3).
        if (succeeded > 0 && failed > 0) {
            this.updateApplyJob(groupKey, {
                state: 'failed',
                error: `${failed} of ${succeeded + failed} tracks failed`,
            });
            notificationStore.blocking({
                key: `autotag-partial:${groupKey}`,
                title: 'This folder was only partly retagged',
                text: `${succeeded} of ${succeeded + failed} tracks were written; ${failed} were not. The folder now holds a mix of old and new tags.`,
                detail: error || undefined,
            });
            await this.loadFolders();

            return;
        }

        if (allFailed) {
            this.updateApplyJob(groupKey, {
                state: 'failed',
                error: error || `${failed} of ${failed} tracks failed`,
            });
            // Nothing was written, so nothing is inconsistent: this is
            // something to retry, not something to interrupt for.
            notificationStore.persistent({
                key: 'autotag-apply',
                title: 'Tags not written',
                text: error
                    ? explainError(error, 'That folder could not be retagged.')
                    : `None of the ${failed} tracks in that folder could be written.`,
                detail: error || undefined,
            });
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
    /** Acknowledge the once-per-library "this rewrites files" warning,
     *  then apply. Returns without applying if the user backed out. */
    private async confirmWarningThenApply(): Promise<void> {
        const current = this.current;

        if (!current) return;

        const library = current.libraryName || `library #${current.libraryId}`;
        const ok = await confirmAction({
            title: 'Heads up: this rewrites audio files',
            message: `Applying autotag writes new metadata directly to every track in ${current.albumName}. `
                + 'The change lands on disk and is not automatically reversible. '
                + 'Files outside this library are not touched.',
            impact: `This warning shows once per library. Continue to acknowledge for ${library}.`,
            confirmLabel: 'Continue',
        });

        if (!ok) return;

        this.markWarningAcked(current.libraryId);

        // Unwrapped, a rejection here meant the lines after it never ran
        // and the dialog sat there forever (errors.m6). The dialog closes
        // itself now, but the apply still has to happen either way.
        try {
            await AckLibraryWarning(current.libraryId);
        } catch (err) {
            console.error('autotag: could not record the warning ack', err);
            this.errorMessage = describeError(
                err,
                'That acknowledgement could not be saved.',
            );
        }

        await this.executeApply();
    }

    /** Open the in-app MB search dialog, seeding the query fields from
     *  the current folder's album/artist so the common case is one
     *  keystroke away. */
    private openSearch(): void {
        this.searchKind = 'releasegroup';
        this.searchQuery = this.current?.albumName ?? '';
        this.searchArtist = this.current?.albumArtist ?? '';
        this.searchResults = [];
        this.searchError = '';
        // The query is seeded from the folder, so "no results" was true of
        // a search nobody had run yet — visible the moment the dialog
        // opens, under the fields still being filled in.
        this.searchRan = false;
        this.dialog = 'search';
    }

    private onSearchCancel = () => { this.dialog = 'none'; };

    private async runSearch(): Promise<void> {
        const query = this.searchQuery.trim();
        if (!query) return;
        this.searchLoading = true;
        this.searchError = '';
        this.searchRan = true;
        try {
            this.searchResults = await SearchCandidates(
                this.searchKind, query, this.searchArtist.trim(),
            );
        } catch (err) {
            console.error('autotag: candidate search failed', err);
            this.searchError = describeError(
                err,
                'The catalog search did not answer.',
            );
            this.searchResults = [];
        } finally {
            this.searchLoading = false;
        }
    }

    private async pickSearchResult(hit: SearchHitView): Promise<void> {
        if (!this.current) return;
        const groupKey = this.current.groupKey;
        this.dialog = 'none';
        this.loading = true;
        try {
            this.score = await SelectSearchCandidate(groupKey, hit.kind, hit.mbid);
            this.selectedCandidateIdx = 0;
        } catch (err) {
            console.error('autotag: could not load candidate', err);
            this.errorMessage = describeError(
                err,
                'That candidate could not be loaded.',
            );
        } finally {
            this.loading = false;
        }
    }

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
            await this.reconcileSelection();
        } catch (err) {
            console.error('autotag: clear completed failed', err);
            this.errorMessage = describeError(
                err,
                'The completed folders could not be cleared.',
            );
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

    // Incremented on every startQueue so an older in-flight run
    // (e.g. the initial load racing a library-filter change) can
    // detect it's stale and stop before clobbering the newer
    // run's selection.
    private queueGeneration = 0;

    /** First actionable folder, matching the sidebar's visual
     *  order: the Pending section renders first and Skipped /
     *  Completed start collapsed, so raw folders[0] (sorted by
     *  score across all statuses) may be hidden from view. */
    private firstPendingFolder(): PendingItem | undefined {
        return this.folders.find(
            (f) => f.status !== 'confirmed' && f.status !== 'skipped',
        );
    }

    private async startQueue(): Promise<void> {
        const generation = ++this.queueGeneration;
        this.foldersLoading = true;
        // Reset state so a library-filter change doesn't leave a
        // stale folder selected from the previous library.
        this.current = null;
        this.score = null;
        this.selectedCandidateIdx = 0;
        try {
            await StartAutotagQueue(this.libraryFilterID());
            if (generation !== this.queueGeneration) return;
            await this.loadFolders();
            if (generation !== this.queueGeneration) return;
            // Folder list is in — let the sidebar paint now, before we
            // block on scoring the first folder's candidates.
            this.foldersLoading = false;
            // Auto-select the first pending folder so the user lands
            // on something actionable (selectFolder drives its own
            // candidate-loading skeleton via this.loading).
            const first = this.firstPendingFolder();
            if (first) {
                await this.selectFolder(first.groupKey);
            }
        } finally {
            if (generation === this.queueGeneration) {
                this.foldersLoading = false;
            }
        }
    }

    private async loadFolders(): Promise<void> {
        try {
            const list = await ListPendingFolders(this.libraryFilterID());
            this.folders = list ?? [];
        } catch (e) {
            console.error('autotag: could not load pending folders', e);
            this.errorMessage = describeError(
                e,
                'The pending folders could not be loaded.',
            );
        }
    }

    /** Called after a background folder-list reload (prefetch
     *  refresh, clear-completed).  If the selected folder is no
     *  longer in the list, fall back to the first pending row so
     *  the main pane never shows a folder the sidebar doesn't. */
    private async reconcileSelection(): Promise<void> {
        const key = this.current?.groupKey;
        if (key !== undefined && this.folders.some((f) => f.groupKey === key)) {
            return;
        }

        const first = this.firstPendingFolder();
        if (first) {
            await this.selectFolder(first.groupKey);
        } else if (this.current) {
            this.current = null;
            this.score = null;
        }
    }

    private async selectFolder(groupKey: string): Promise<void> {
        const generation = this.queueGeneration;
        this.errorMessage = '';
        this.score = null;
        this.selectedCandidateIdx = 0;
        this.localCoverUrl = '';
        void this.loadLocalCover(groupKey);
        // Show what we already know about this folder while
        // candidates load — keeps the header from blanking.
        const inList = this.folders.find((f) => f.groupKey === groupKey);
        this.current = inList ?? null;
        try {
            if (!inList) {
                const fetched = await GetPendingFolder(groupKey);
                // A library-filter change restarted the queue while
                // this fetch was in flight — don't resurrect a
                // folder from the previous library.
                if (generation !== this.queueGeneration) return;
                this.current = fetched ?? null;
            }
            if (this.current) {
                await this.loadCandidates(this.current.groupKey);
            }
        } catch (e) {
            console.error('autotag: could not load folder', e);
            this.errorMessage = describeError(
                e,
                'That folder could not be loaded.',
            );
        }
    }

    /** Fetch the folder's embedded local artwork (best-effort).
     *  Guarded on groupKey so a fast folder switch doesn't paint the
     *  previous folder's cover against the new one. */
    private async loadLocalCover(groupKey: string): Promise<void> {
        try {
            const url = await GetLocalCoverArt(groupKey);
            if (this.current?.groupKey === groupKey) {
                this.localCoverUrl = url ?? '';
            }
        } catch {
            // No local art is a normal case; leave the placeholder.
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
            console.error('autotag: could not score candidates', e);
            this.errorMessage = describeError(
                e,
                'The candidates for this folder could not be scored.',
            );
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
            await this.confirmWarningThenApply();

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
                console.error('autotag: apply failed to start', e);
                this.updateApplyJob(groupKey, { state: 'failed', error: msg });
                notificationStore.persistent({
                    key: 'autotag-apply',
                    title: 'Tags not written',
                    text: explainError(e, 'That folder could not be retagged.'),
                    detail: msg,
                });
            }
        });

        // Advance to the next folder right away so the user can
        // keep reviewing while the background job runs.
        await this.refreshAfterAction();
    }

    private async onSkip(): Promise<void> {
        if (!this.current) return;

        try {
            await Skip(this.current.groupKey);
        } catch (err) {
            console.error('autotag: skip failed', err);
            this.errorMessage = describeError(
                err,
                'That folder could not be skipped.',
            );

            return;
        }

        await this.refreshAfterAction();
    }

    private async onLeave(): Promise<void> {
        if (!this.current) return;

        const groupKey = this.current.groupKey;

        if (this.topScore() < CONFIDENT_SCORE) {
            const top = Math.round(this.topScore() * 100);
            const ok = await confirmAction({
                title: 'Keep the current tags?',
                message: `No candidate scored high enough to trust automatically (top is ${top}%). `
                    + `“Leave as-is” marks the local tags on ${this.current.albumName} as correct `
                    + 'and removes this folder from the review queue.',
                impact: 'If the local tags are wrong, prefer Skip (S) instead — '
                    + 'that leaves the folder pending for later review.',
                confirmLabel: 'Mark as correct',
            });

            // The confirmation is modal, but it is still an await: the
            // folder we asked about has to be the folder we act on.
            if (!ok || this.current?.groupKey !== groupKey) return;
        }

        try {
            await LeaveAsIs(groupKey);
        } catch (err) {
            console.error('autotag: leave-as-is failed', err);
            this.errorMessage = describeError(
                err,
                'That folder could not be left as it is.',
            );

            return;
        }

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
            console.error('autotag: paste URL failed', e);
            this.errorMessage = describeError(
                e,
                'That release URL could not be used.',
            );
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

        // Cosmetic-only difference (case / punctuation / the spacing
        // punctuation induces): render it muted rather than alarming
        // red-green so the user can see the formatting change without
        // reading it as a real conflict.
        const cosmetic = isCosmeticDiff(local, candidate);
        const removeCls = cosmetic ? 'diff-cosmetic-old' : 'diff-old';
        const addCls = cosmetic ? 'diff-cosmetic-new' : 'diff-new';

        const segments = inlineDiff(local, candidate);
        return html`${segments.map((seg) => {
            if (seg.type === 'equal') return seg.text;
            if (seg.type === 'remove') return html`<span class=${removeCls}>${seg.text}</span>`;
            return html`<span class=${addCls}>${seg.text}</span>`;
        })}`;
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

    /** Toggle a sidebar section's collapsed state.  Reassigns a new
     *  Set so Lit sees the change (in-place mutation wouldn't). */
    private toggleSection(key: string): void {
        const next = new Set(this.collapsedSections);
        if (next.has(key)) {
            next.delete(key);
        } else {
            next.add(key);
        }
        this.collapsedSections = next;
    }

    /** Disclosure chevron for a collapsible section header. */
    private sectionChevron(key: string): TemplateResult {
        const collapsed = this.collapsedSections.has(key);
        return html`<wa-icon
            class="section-chevron"
            name=${collapsed ? 'chevron-right' : 'chevron-down'}></wa-icon>`;
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
                    <button class="section-toggle"
                            @click=${() => this.toggleSection('pending')}>
                        ${this.sectionChevron('pending')}
                        <span>Pending (${pending.length})</span>
                    </button>
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
                    ? (this.foldersLoading
                        ? this.renderFolderSkeleton()
                        : html`<div class="empty">Queue is empty.</div>`)
                    : html`
                        ${this.collapsedSections.has('pending')
                            ? nothing
                            : pending.map((f) => this.renderFolderRow(f))}
                        ${skipped.length > 0 ? html`
                            <button class="folders-section-header"
                                    @click=${() => this.toggleSection('skipped')}>
                                ${this.sectionChevron('skipped')}
                                <span>Skipped (${skipped.length})</span>
                            </button>
                            ${this.collapsedSections.has('skipped')
                                ? nothing
                                : skipped.map((f) => this.renderFolderRow(f))}
                        ` : nothing}
                        ${completed.length > 0 ? html`
                            <button class="folders-section-header"
                                    @click=${() => this.toggleSection('completed')}>
                                ${this.sectionChevron('completed')}
                                <span>Completed (${completed.length})</span>
                            </button>
                            ${this.collapsedSections.has('completed')
                                ? nothing
                                : completed.map((f) => this.renderFolderRow(f))}
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

    /**
     * Versions dropdown: the editions *within the active cluster*
     * (remaster / country / reissue of the same album), so the user
     * picks a release edition here without leaving the current album.
     * Picking a different album entirely is the candidate picker's job.
     * Hidden when the active cluster has only one edition.
     */
    private renderVersionsDropdown(cand: CandidateView, clusters: VersionCluster[]) {
        const cluster = clusters.find((c) => c.candidates.includes(cand));
        if (!cluster || cluster.candidates.length <= 1) return nothing;

        const editions = cluster.candidates
            .map((c) => ({ cand: c, idx: this.score?.candidates.indexOf(c) ?? -1 }))
            .filter((e) => e.idx >= 0);

        return html`
            <select class="versions-select"
                    aria-label="Release version"
                    @change=${(e: Event) => {
                        const idx = Number((e.target as HTMLSelectElement).value);
                        void this.selectCandidateByIdx(idx);
                    }}>
                ${editions.map((e) => html`
                    <option value=${e.idx} ?selected=${e.cand === cand}>
                        ${this.versionOptionLabel(e.cand)}
                    </option>
                `)}
            </select>
        `;
    }

    /** Compact one-line label for a release edition in the versions
     *  dropdown: year, country, status, falling back to the title. */
    private versionOptionLabel(c: CandidateView): string {
        const parts: string[] = [];
        const year = (c.date ?? '').slice(0, 4);
        if (year) parts.push(year);
        if (c.country) parts.push(c.country);
        if (c.status) parts.push(c.status);
        if (c.trackCount) parts.push(`${c.trackCount} trk`);
        return parts.join(' · ') || c.title || 'release';
    }

    /**
     * Compute the album-level "what would change" summary for the
     * given candidate.  This pairs with renderMatchDetails to give
     * the user a concrete answer to "why isn't this 100%?" — the
     * tracklist already shows per-track title/length/# diffs, but
     * subtle length drift (<5s) is suppressed there and album-
     * level metadata factors aren't visible at all.
     */
    private computeMatchSummary(cand: CandidateView) {
        let paired = 0;
        // Paired tracks where both sides carry a usable track number,
        // so "Track Numbers: N/M" only counts tracks it can judge.
        let numberedPaired = 0;
        let driftSum = 0;
        let driftCount = 0;

        // Title diffs split by significance: a cosmetic diff (case /
        // punctuation) has titleScore 1.0 — it does not move the score,
        // so it shouldn't read as a warning.  A significant diff has
        // titleScore < 1.  Each detail row keeps both sides so the
        // dropdown can render an inline diff of exactly what changed.
        const significantTitleDiffs: TitleDiffDetail[] = [];
        const cosmeticTitleDiffs: TitleDiffDetail[] = [];
        const trackNumDiffs: NumDiffDetail[] = [];
        const visibleLengthDiffs: LengthDiffDetail[] = [];
        const subtleLengthDiffs: LengthDiffDetail[] = [];
        const missingTitles: string[] = [];
        const extraTitles: string[] = [];

        // Mirrors the scorer's lengthExactMs grace band — anything
        // the score forgives, the UI mutes.
        const SUBTLE_LENGTH_MAX_MS = 5000;

        for (const a of cand.alignments) {
            if (a.status === 'matched' || a.status === 'mismatched') {
                paired++;
                const local = a.localIndex >= 0
                    ? this.score?.localTracks[a.localIndex] ?? null
                    : null;
                if (local) {
                    if (local.title !== a.candidateTitle) {
                        const detail: TitleDiffDetail = {
                            pos: a.candidatePosition,
                            local: local.title,
                            candidate: a.candidateTitle,
                        };
                        // Cosmetic when the backend already scored it a
                        // perfect title match, OR when the only
                        // difference is case/punctuation/spacing that the
                        // backend's punctuation-deletion left as a stray
                        // space (so titleScore dipped just under 1).
                        if (a.titleScore >= 1 || isCosmeticDiff(local.title, a.candidateTitle)) {
                            cosmeticTitleDiffs.push(detail);
                        } else {
                            significantTitleDiffs.push(detail);
                        }
                    }
                    if (a.candidatePosition > 0 && local.trackNumber > 0) {
                        numberedPaired++;
                        if (local.trackNumber !== a.candidatePosition) {
                            trackNumDiffs.push({
                                title: a.candidateTitle || local.title || '(untitled)',
                                local: local.trackNumber,
                                candidate: a.candidatePosition,
                            });
                        }
                    }
                }
                if (a.lengthDeltaMs > SUBTLE_LENGTH_MAX_MS) {
                    visibleLengthDiffs.push({
                        pos: a.candidatePosition,
                        title: a.candidateTitle || local?.title || '(untitled)',
                        localMs: a.localLengthMillis || local?.lengthMillis || 0,
                        candidateMs: a.candidateLength,
                        deltaMs: a.lengthDeltaMs,
                    });
                } else if (a.lengthDeltaMs > 0) {
                    subtleLengthDiffs.push({
                        pos: a.candidatePosition,
                        title: a.candidateTitle || local?.title || '(untitled)',
                        localMs: a.localLengthMillis || local?.lengthMillis || 0,
                        candidateMs: a.candidateLength,
                        deltaMs: a.lengthDeltaMs,
                    });
                    driftSum += a.lengthDeltaMs;
                    driftCount++;
                }
            } else if (a.status === 'missing') {
                missingTitles.push(a.candidateTitle || '(untitled)');
            } else if (a.status === 'unmatched') {
                const local = a.localIndex >= 0
                    ? this.score?.localTracks[a.localIndex] ?? null
                    : null;
                extraTitles.push(a.localTitle || local?.title || '(untitled)');
            }
        }

        const localAlbum = (this.current?.albumName ?? '').trim();
        const candAlbum = (cand.title ?? '').trim();
        const localArtist = (this.current?.albumArtist ?? '').trim();
        const candArtist = (cand.artistCredit ?? '').trim();

        return {
            paired,
            numberedPaired,
            missingFromFolder: missingTitles.length,
            unmatchedInFolder: extraTitles.length,
            missingTitles,
            extraTitles,
            significantTitleDiffs,
            cosmeticTitleDiffs,
            trackNumDiffs,
            visibleLengthDiffs,
            subtleLengthDiffs,
            avgDriftMs: driftCount > 0 ? driftSum / driftCount : 0,
            localAlbum,
            candAlbum,
            localArtist,
            candArtist,
            albumTitleMatches: localAlbum === '' || normalizeStrict(localAlbum) === normalizeStrict(candAlbum),
            albumArtistMatches: localArtist === '' || normalizeStrict(localArtist) === normalizeStrict(candArtist),
        };
    }

    /** One match-detail line.  When `body` is provided the line is a
     *  native <details> disclosure so the user can expand it to see
     *  exactly what differs; otherwise it's a plain, non-expandable
     *  row.  `cls` drives the leading marker (ok / warn / info). */
    private mdItem(
        cls: 'ok' | 'warn' | 'info',
        summary: TemplateResult | string,
        body: TemplateResult | null = null,
    ): TemplateResult {
        const mark = html`<span class="md-mark md-mark-${cls}"></span>`;
        if (!body) {
            return html`
                <div class="md-item ${cls}">
                    ${mark}<span class="md-text">${summary}</span>
                </div>
            `;
        }
        return html`
            <details class="md-item ${cls} expandable">
                <summary>
                    ${mark}<span class="md-text">${summary}</span>
                    <wa-icon class="md-chevron" name="chevron-right"></wa-icon>
                </summary>
                <div class="md-body">${body}</div>
            </details>
        `;
    }

    /** Inline before/after diff rows for a set of title changes. */
    private renderTitleDiffBody(details: TitleDiffDetail[]): TemplateResult {
        return html`${details.map((d) => html`
            <div class="md-diff-row">
                ${d.pos > 0 ? html`<span class="md-diff-pos">${d.pos}</span>` : nothing}
                <span class="md-diff-text">${this.diffText(d.local, d.candidate)}</span>
            </div>
        `)}`;
    }

    /** Track-number change rows: local # → candidate #. */
    private renderNumDiffBody(details: NumDiffDetail[]): TemplateResult {
        return html`${details.map((d) => html`
            <div class="md-diff-row">
                <span class="md-diff-text">${d.title || '(untitled)'}</span>
                <span class="md-diff-nums">
                    <span class="diff-old">#${d.local}</span>
                    <span class="md-arrow">→</span>
                    <span class="diff-new">#${d.candidate}</span>
                </span>
            </div>
        `)}`;
    }

    /** Length-drift rows: local m:ss → candidate m:ss (±Xs).
     *  When `subtle` (drift under 5s, treated as a successful match)
     *  the values render muted rather than red-strike/green so the
     *  row doesn't read as a real mismatch. */
    private renderLengthDiffBody(
        details: LengthDiffDetail[], subtle = false,
    ): TemplateResult {
        const oldCls = subtle ? 'diff-cosmetic-old' : 'diff-old';
        const newCls = subtle ? 'diff-cosmetic-new' : 'diff-new';
        return html`${details.map((d) => {
            const delta = (d.deltaMs / 1000).toFixed(1);
            return html`
                <div class="md-diff-row">
                    ${d.pos > 0 ? html`<span class="md-diff-pos">${d.pos}</span>` : nothing}
                    <span class="md-diff-text">${d.title || '(untitled)'}</span>
                    <span class="md-diff-nums">
                        <span class=${oldCls}>${this.formatLength(d.localMs) || '—'}</span>
                        <span class="md-arrow">→</span>
                        <span class=${newCls}>${this.formatLength(d.candidateMs) || '—'}</span>
                        <span class="md-delta">(±${delta}s)</span>
                    </span>
                </div>
            `;
        })}`;
    }

    /** Plain title list body for missing / extra tracks. */
    private renderTitleListBody(
        titles: string[], side: 'candidate' | 'local',
    ): TemplateResult {
        const cls = side === 'candidate' ? 'diff-new' : 'diff-old';
        return html`${titles.map((t) => html`
            <div class="md-diff-row">
                <span class="md-diff-text ${cls}">${t || '(untitled)'}</span>
            </div>
        `)}`;
    }

    /** A counted match category rendered as "Label: matched/total".
     *  When every item matches (matched >= total) the line is a plain
     *  green-check row with no dropdown; only a real conflict gets the
     *  expandable detail body. */
    private mdCategory(
        label: string,
        matched: number,
        total: number,
        body: TemplateResult | null = null,
    ): TemplateResult {
        const ok = matched >= total;
        const summary = html`${label}:
            <span class="md-count ${ok ? '' : 'bad'}">${matched}/${total}</span>`;
        return this.mdItem(ok ? 'ok' : 'warn', summary, ok ? null : body);
    }

    /** A single-count match line rendered as "Label: N".  `cls`
     *  drives the marker/colour; a body makes it an expandable
     *  disclosure (used for the missing/extra track lists). */
    private mdCount(
        cls: 'ok' | 'warn' | 'info',
        label: string,
        count: number,
        body: TemplateResult | null = null,
    ): TemplateResult {
        const summary = html`${label}:
            <span class="md-count ${cls === 'warn' ? 'bad' : ''}">${count}</span>`;
        return this.mdItem(cls, summary, body);
    }

    private renderMatchDetails(cand: CandidateView) {
        const s = this.computeMatchSummary(cand);
        const items: TemplateResult[] = [];

        // ── Track pairing — matched / missing / extra as separate
        // single-count lines, so there's no ambiguous "total".
        // Matched is always shown; missing/extra only when non-zero
        // (they're the conflicts, and carry the expandable lists).
        items.push(this.mdCount('ok', 'Matched Tracks', s.paired));
        if (s.missingFromFolder > 0) {
            items.push(this.mdCount(
                'warn',
                'Missing Tracks',
                s.missingFromFolder,
                this.renderTitleListBody(s.missingTitles, 'candidate'),
            ));
        }
        if (s.unmatchedInFolder > 0) {
            items.push(this.mdCount(
                'warn',
                'Extra Tracks',
                s.unmatchedInFolder,
                this.renderTitleListBody(s.extraTitles, 'local'),
            ));
        }

        // ── Track titles: cosmetic (case/punctuation) diffs count as
        // a match since they don't move the score.  Only significant
        // diffs are conflicts — but when there is one, the dropdown
        // shows every title change (significant + cosmetic) for context.
        if (s.paired > 0) {
            const allTitleDiffs = [...s.significantTitleDiffs, ...s.cosmeticTitleDiffs]
                .sort((a, b) => a.pos - b.pos);
            items.push(this.mdCategory(
                'Track Titles',
                s.paired - s.significantTitleDiffs.length,
                s.paired,
                this.renderTitleDiffBody(allTitleDiffs),
            ));
        }

        // ── Track lengths: drift under 5s counts as a match, so only
        // the >5s differences show up (and only they get a dropdown).
        if (s.paired > 0) {
            items.push(this.mdCategory(
                'Track Lengths',
                s.paired - s.visibleLengthDiffs.length,
                s.paired,
                this.renderLengthDiffBody(s.visibleLengthDiffs),
            ));
        }

        // ── Track numbers: only counts tracks that carry a number on
        // both sides, so an untagged folder doesn't read as all-wrong.
        if (s.numberedPaired > 0) {
            items.push(this.mdCategory(
                'Track Numbers',
                s.numberedPaired - s.trackNumDiffs.length,
                s.numberedPaired,
                this.renderNumDiffBody(s.trackNumDiffs),
            ));
        }

        // ── Album header — boolean fields, shown only when they'd
        // change (a conflict); the dropdown shows the exact diff.
        if (!s.albumTitleMatches) {
            items.push(this.mdItem(
                'warn',
                'Album name would change',
                html`<div class="md-diff-row">
                    <span class="md-diff-text">${this.diffText(s.localAlbum, s.candAlbum)}</span>
                </div>`,
            ));
        }
        if (!s.albumArtistMatches) {
            items.push(this.mdItem(
                'warn',
                'Album artist would change',
                html`<div class="md-diff-row">
                    <span class="md-diff-text">${this.diffText(s.localArtist, s.candArtist)}</span>
                </div>`,
            ));
        }

        // ── Release-info line: not a "diff" per se but a useful
        // signal since older / non-Official releases score lower
        // even when every track lines up.  No dropdown — informational.
        const releaseParts: string[] = [];
        const releaseYear = (cand.date ?? '').slice(0, 4);
        const originalYear = (cand.originalDate ?? '').slice(0, 4);
        if (originalYear && releaseYear && originalYear !== releaseYear) {
            releaseParts.push(`${originalYear} (${releaseYear} reissue)`);
        } else if (releaseYear) {
            releaseParts.push(releaseYear);
        }
        if (cand.primaryType) releaseParts.push(cand.primaryType);
        if (cand.country) releaseParts.push(cand.country);
        if (cand.status) releaseParts.push(cand.status);
        if (releaseParts.length) {
            items.push(this.mdItem('info', html`Release: ${releaseParts.join(' · ')}`));
        }

        const b = cand.breakdown;
        const pct = (v: number) => `${Math.round(v * 100)}%`;

        return html`
            <div class="match-details">
                <div class="md-header">
                    <span>Match details</span>
                </div>
                <div class="md-items">${items}</div>
                ${b ? html`
                    <div class="breakdown-line">
                        <span class="b-pair">Match <span class="b-val">${pct(cand.score)}</span></span>
                        <span class="b-pair">Title <span class="b-val">${pct(b.titleAvg)}</span></span>
                        <span class="b-pair">Length <span class="b-val">${pct(b.lengthAvg)}</span></span>
                        <span class="b-pair">Artist <span class="b-val">${pct(b.artistFit)}</span></span>
                        ${b.albumFit < 1
                            ? html`<span class="b-pair">Album <span class="b-val">${pct(b.albumFit)}</span></span>`
                            : nothing}
                        <span class="b-pair">Tracks <span class="b-val">${pct(b.trackCountFit)}</span></span>
                        <span class="b-pair">Release <span class="b-val">${pct(b.releaseMeta)}</span></span>
                        ${b.evidence < 1
                            ? html`<span class="b-pair">Evidence <span class="b-val">${pct(b.evidence)}</span></span>`
                            : nothing}
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
    /**
     * The two-column comparison: the folder's files on the left (in
     * folder order) and the candidate release on the right (in
     * candidate order).  Matched rows share a data-pair id so hovering
     * either side highlights its partner; folder tracks with no
     * candidate partner (extra) and candidate tracks with no folder
     * partner (missing) both surface as amber gaps, which is what makes
     * "do I have extra or missing tracks?" answerable at a glance.
     * Field-level title/length conflicts are summarised in
     * renderMatchDetails, so each column shows its own values plainly.
     */
    private renderComparison(cand: CandidateView, clusters: VersionCluster[] = []) {
        const locals = this.score?.localTracks ?? [];

        // localIndex -> its alignment, so a folder row knows whether it
        // paired and (if so) how confidently.
        const alignByLocal = new Map<number, AlignmentView>();
        for (const a of cand.alignments) {
            if (a.localIndex >= 0) alignByLocal.set(a.localIndex, a);
        }

        // Candidate side: every alignment that has a candidate track
        // (paired or missing-from-folder), in candidate order.
        const candRows = cand.alignments
            .filter((a) => a.status !== 'unmatched' && a.candidatePosition > 0)
            .slice()
            .sort((x, y) =>
                (x.candidateDiscNumber - y.candidateDiscNumber)
                || (x.candidatePosition - y.candidatePosition));

        const folderRows = locals.map((t, i) => {
            const a = alignByLocal.get(i);
            const paired = !!a && (a.status === 'matched' || a.status === 'mismatched');
            const cls = !paired ? 'extra' : a!.status === 'mismatched' ? 'mismatched' : 'matched';
            const pair = paired ? `p${i}` : '';
            return this.renderCompareRow(cls, pair, t.trackNumber, t.title, t.lengthMillis);
        });

        const candItems = candRows.map((a) => {
            const paired = a.status === 'matched' || a.status === 'mismatched';
            const cls = !paired ? 'extra' : a.status === 'mismatched' ? 'mismatched' : 'matched';
            const pair = paired ? `p${a.localIndex}` : '';
            return this.renderCompareRow(
                cls, pair, a.candidatePosition, a.candidateTitle, a.candidateLength,
            );
        });

        return html`
            <div class="compare">
                <div class="compare-col">
                    ${this.renderLocalAlbumHeader(folderRows.length)}
                    ${folderRows.length > 0
                        ? folderRows
                        : html`<div class="empty">No local tracks.</div>`}
                </div>
                <div class="compare-col">
                    ${this.renderCandidateAlbumHeader(cand, candItems.length, clusters)}
                    ${candItems.length > 0
                        ? candItems
                        : html`<div class="empty">No candidate tracks.</div>`}
                </div>
            </div>
        `;
    }

    /** Local column header: the folder's own artwork + album/artist,
     *  shown plainly (no diff coloring) so the user sees what the
     *  files currently look like. */
    private renderLocalAlbumHeader(trackCount: number) {
        const album = this.current?.albumName || '(no album)';
        const artist = this.current?.albumArtist || 'Unknown artist';
        return html`
            <div class="cc-album">
                ${this.localCoverUrl
                    ? html`<img class="cc-cover" src=${this.localCoverUrl} alt="local cover art">`
                    : html`<div class="cc-cover placeholder">No cover</div>`}
                <div class="cc-meta">
                    <div class="cc-kicker">Your folder</div>
                    <div class="cc-title" title=${album}>${album}</div>
                    <div class="cc-artist" title=${artist}>${artist}</div>
                    <div class="cc-line">${trackCount} ${trackCount === 1 ? 'track' : 'tracks'}</div>
                </div>
            </div>
        `;
    }

    /** Candidate column header: the fetched release's artwork +
     *  title/artist, its release-info line, the match percentage and
     *  — when the cluster has multiple editions — the versions picker.
     *  Shown plainly (no diff coloring). */
    private renderCandidateAlbumHeader(
        cand: CandidateView, trackCount: number, clusters: VersionCluster[],
    ) {
        const title = cand.title || '(untitled)';
        const artist = cand.artistCredit || 'Unknown artist';
        const releaseYear = (cand.date ?? '').slice(0, 4);
        const originalYear = (cand.originalDate ?? '').slice(0, 4);
        const subParts: string[] = [];
        if (originalYear && releaseYear && originalYear !== releaseYear) {
            subParts.push(`${originalYear} (${releaseYear} reissue)`);
        } else if (releaseYear) {
            subParts.push(releaseYear);
        }
        if (cand.country) subParts.push(cand.country);
        if (cand.status) subParts.push(cand.status);
        subParts.push(`${trackCount} ${trackCount === 1 ? 'track' : 'tracks'}`);

        const versions = this.renderVersionsDropdown(cand, clusters);

        return html`
            <div class="cc-album">
                ${cand.coverArtUrl
                    ? html`<img class="cc-cover" src=${cand.coverArtUrl} alt="candidate cover art">`
                    : html`<div class="cc-cover placeholder">No cover</div>`}
                <div class="cc-meta">
                    <div class="cc-kicker">Candidate</div>
                    <div class="cc-title" title=${title}>${title}</div>
                    <div class="cc-artist" title=${artist}>${artist}</div>
                    <div class="cc-line">
                        <span class="score-badge ${cand.score >= 0.85 ? 'high' : ''}">
                            ${(cand.score * 100).toFixed(0)}% match
                        </span>
                        <span>${subParts.join(' · ')}</span>
                    </div>
                    ${versions !== nothing
                        ? html`<div class="cc-line">${versions}</div>`
                        : nothing}
                </div>
            </div>
        `;
    }

    private renderCompareRow(
        cls: string, pair: string, pos: number, title: string, lengthMs: number,
    ): TemplateResult {
        const hover = pair
            ? {
                enter: () => this.highlightPair(pair, true),
                leave: () => this.highlightPair(pair, false),
            }
            : { enter: () => {}, leave: () => {} };
        return html`
            <div class="track-row ${cls}" data-pair=${pair || nothing}
                 @mouseenter=${hover.enter} @mouseleave=${hover.leave}>
                <span class="track-pos">${pos || '-'}</span>
                <span class="track-title">${title || '(untitled)'}</span>
                <span class="track-len">${this.formatLength(lengthMs)}</span>
            </div>
        `;
    }

    /** Toggle the partner-highlight class on every row that shares the
     *  given data-pair id (the local row and its candidate row). */
    private highlightPair(pair: string, on: boolean): void {
        const rows = this.renderRoot.querySelectorAll(`[data-pair="${pair}"]`);
        rows.forEach((r) => r.classList.toggle('pair-hi', on));
    }

    /**
     * Ranked candidate picker: one chip per version-cluster (its best
     * edition), sorted by score, so the user can jump to a lower-scored
     * alternative — the beets-style "here are the other matches" list.
     * Editions *within* the selected cluster are chosen via the
     * versions dropdown in the album card, not here.
     */
    private renderCandidatePicker(clusters: VersionCluster[], activeCand: CandidateView) {
        if (clusters.length <= 1) return nothing;

        return html`
            <div class="cand-picker">
                ${clusters.map((cluster) => {
                    const best = cluster.candidates[cluster.bestIdx]!;
                    const active = cluster.candidates.includes(activeCand);
                    const idx = this.score?.candidates.indexOf(best) ?? -1;
                    return html`
                        <div class="cand-chip ${active ? 'selected' : ''}"
                             title=${cluster.label}
                             @click=${() => { void this.selectCandidateByIdx(idx); }}>
                            <span class="label">${cluster.label}</span>
                            <span class="score-badge ${best.score >= 0.85 ? 'high' : ''}">
                                ${(best.score * 100).toFixed(0)}%
                            </span>
                        </div>
                    `;
                })}
            </div>
        `;
    }

    /** Shimmer placeholder rows for the folder sidebar while the
     *  pending-folder list is still loading. */
    private renderFolderSkeleton(): TemplateResult {
        const rows = 8;
        return html`
            ${Array.from({ length: rows }, () => html`
                <div class="sk-folder-row">
                    <div class="skeleton sk-icon"></div>
                    <div class="skeleton sk-line" style="width: 80%;"></div>
                    <div class="skeleton sk-line" style="width: 55%;"></div>
                </div>
            `)}
        `;
    }

    /** Shimmer placeholder for the main pane — mirrors the album card
     *  (cover + meta) and a few tracklist rows so the layout doesn't
     *  jump when the real candidate data arrives. */
    private renderMainSkeleton(): TemplateResult {
        return html`
            <div class="main">
                <div class="album-card">
                    <div class="skeleton sk-cover"></div>
                    <div class="album-meta">
                        <div class="skeleton sk-title"></div>
                        <div class="skeleton sk-artist"></div>
                        <div class="skeleton sk-line" style="width: 35%;"></div>
                        <div class="skeleton sk-pill"></div>
                    </div>
                </div>
                <div class="tracklist">
                    ${Array.from({ length: 6 }, () => html`
                        <div class="track-row">
                            <span class="skeleton sk-line" style="width: 1.2rem;"></span>
                            <span class="skeleton sk-line" style="width: 60%;"></span>
                            <span class="skeleton sk-line" style="width: 2.5rem;"></span>
                        </div>
                    `)}
                </div>
            </div>
        `;
    }

    private renderMain() {
        // A scoring error on the selected folder surfaces as an error,
        // not an endless skeleton.
        if (this.current && !this.score && this.errorMessage) {
            return html`
                <div class="main">
                    <div class="error">
                        <span>${this.errorMessage}</span>
                        <button @click=${this.onDismissError}>Dismiss</button>
                    </div>
                </div>
            `;
        }

        // Folder list still arriving, or the selected folder's
        // candidates are still being scored \u2192 skeleton, not text.
        if (this.foldersLoading || (this.current && (this.loading || !this.score))) {
            return this.renderMainSkeleton();
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
                        Hit <kbd>F</kbd> to search MusicBrainz or <kbd>U</kbd> to paste a URL.
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
                ${this.renderCandidatePicker(clusters, cand)}
                ${this.renderMatchDetails(cand)}
                ${this.renderComparison(cand, clusters)}
            </div>
        `;
    }

    /* ── Dialogs ── */

    private renderPasteDialog() {
        return html`
            <wa-dialog
                label="Paste MusicBrainz URL"
                data-testid="autotag-paste-dialog"
                ?open=${this.dialog === 'paste'}
                @wa-hide=${this.onPasteCancel}
            >
                <p>
                    Paste a MusicBrainz release or release-group URL.
                    Tracks are aligned automatically when it loads.
                </p>
                <input type="url"
                       class="url-input"
                       aria-label="MusicBrainz release URL"
                       placeholder="https://musicbrainz.org/release/..."
                       .value=${this.pasteURL}
                       @input=${this.onPasteInput}
                       @keydown=${this.onPasteKeydown}
                       autofocus>
                <div class="row" slot="footer">
                    <button class="secondary" @click=${this.onPasteCancel}>Cancel</button>
                    <button @click=${this.onPasteSubmit}>Load</button>
                </div>
            </wa-dialog>
        `;
    }

    private renderSearchDialog() {
        return html`
            <wa-dialog
                label="Search MusicBrainz"
                class="search-dialog"
                data-testid="autotag-search-dialog"
                ?open=${this.dialog === 'search'}
                @wa-hide=${this.onSearchCancel}
            >
                    <div class="search-kind">
                        <label>
                            <input type="radio" name="searchKind" value="releasegroup"
                                   .checked=${this.searchKind === 'releasegroup'}
                                   @change=${() => { this.searchKind = 'releasegroup'; }}>
                            Album
                        </label>
                        <label>
                            <input type="radio" name="searchKind" value="recording"
                                   .checked=${this.searchKind === 'recording'}
                                   @change=${() => { this.searchKind = 'recording'; }}>
                            Track (for singles)
                        </label>
                    </div>
                    <input type="text" class="search-input"
                           aria-label=${this.searchKind === 'recording' ? 'Track title' : 'Album name'}
                           placeholder=${this.searchKind === 'recording' ? 'Track title' : 'Album name'}
                           .value=${this.searchQuery}
                           @input=${(e: Event) => { this.searchQuery = (e.target as HTMLInputElement).value; }}
                           @keydown=${(e: KeyboardEvent) => { if (e.key === 'Enter') void this.runSearch(); }}
                           autofocus>
                    <input type="text" class="search-input"
                           aria-label="Artist (optional)"
                           placeholder="Artist (optional)"
                           .value=${this.searchArtist}
                           @input=${(e: Event) => { this.searchArtist = (e.target as HTMLInputElement).value; }}
                           @keydown=${(e: KeyboardEvent) => { if (e.key === 'Enter') void this.runSearch(); }}>
                    ${this.searchError
                        ? html`<div class="error" style="margin-top:0.75rem;">${this.searchError}</div>`
                        : nothing}
                    ${this.searchResults.length > 0 ? html`
                        <div class="search-results" role="list">
                            ${this.searchResults.map((hit) => html`
                                <button type="button" class="search-result" role="listitem"
                                     @click=${() => { void this.pickSearchResult(hit); }}>
                                    <div class="sr-title">${hit.title}</div>
                                    <div class="sr-sub">
                                        <span>${hit.artist || '—'}</span>
                                        ${hit.detail ? html`<span class="sr-detail">${hit.detail}</span>` : nothing}
                                    </div>
                                </button>
                            `)}
                        </div>
                    ` : this.searchRan && !this.searchLoading && !this.searchError
                        ? html`<div class="search-empty">No results — try dropping the artist or switching Album/Track.</div>`
                        : nothing}
                    <div class="row" slot="footer">
                        <button class="secondary" @click=${this.onSearchCancel}>Cancel</button>
                        <button @click=${() => { void this.runSearch(); }}
                                ?disabled=${this.searchLoading || this.searchQuery.trim() === ''}>
                            ${this.searchLoading ? 'Searching…' : 'Search'}
                        </button>
                    </div>
            </wa-dialog>
        `;
    }

    // Both dialogs render unconditionally so `wa-dialog` owns opening and
    // closing (and therefore the focus trap and the focus restore); the
    // `open` property is what says which — mounting one on demand would
    // put the element and its `showModal()` in the same update.
    private renderDialog() {
        return html`${this.renderPasteDialog()}${this.renderSearchDialog()}`;
    }

    override render() {
        // The layout chrome always renders immediately; each pane owns
        // its own skeleton while its data resolves, so the user never
        // sees a blank full-screen "Loading\u2026".
        return html`
            <div class="root">
                ${this.renderHeader()}
                ${this.renderFolderSidebar()}
                ${this.renderMain()}
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

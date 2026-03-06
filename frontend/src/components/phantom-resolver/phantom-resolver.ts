import { LitElement, html, css, nothing } from 'lit';
import {
    customElement,
    state,
    query,
} from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

import {
    FindPhantomMatches,
    GetPhantomCandidates,
    SearchLibrary,
    ResolvePhantomTracks,
    RemovePhantomTracks,
} from '@go/playlist/Service';
import type { playlist } from '@go/models';
import { formatMilliseconds } from '@utils/time';

const SEARCH_DEBOUNCE_MS = 400;

/**
 * A modal dialog for resolving phantom (unmatched) tracks
 * in imported playlists.
 */
@customElement('phantom-resolver')
export class PhantomResolver extends LitElement {
    @query('wa-dialog')
    private dialog!: HTMLElement & { open: boolean };

    // ─── State ──────────────────────────────────────
    @state() private loading = true;
    @state() private autoMatched: playlist.PhantomMatch[] =
        [];
    @state() private unmatched: string[] = [];
    @state() private autoMatchExpanded = false;
    @state() private selectedPhantom: string | null = null;
    @state() private candidates: playlist.CandidateTrack[] =
        [];
    @state() private candidatesLoading = false;
    @state() private searchQuery = '';
    @state() private searchResults: playlist.CandidateTrack[] =
        [];
    @state() private searching = false;

    private playlistId = 0;
    private phantomTracks: playlist.Track[] = [];

    /** User-confirmed matches: phantomPath -> resolvedFilePath. */
    private confirmedMatches = new Map<string, string>();

    /** Auto-match overrides: phantomPath -> null (removed). */
    private autoMatchOverrides = new Map<
        string,
        string | null
    >();

    private searchTimer: ReturnType<typeof setTimeout> | null =
        null;

    // ─── Public API ─────────────────────────────────

    show(
        playlistId: number,
        phantomTracks: playlist.Track[],
    ): void {
        this.playlistId = playlistId;
        this.phantomTracks = phantomTracks;
        this.loading = true;
        this.autoMatched = [];
        this.unmatched = [];
        this.autoMatchExpanded = false;
        this.selectedPhantom = null;
        this.candidates = [];
        this.candidatesLoading = false;
        this.searchQuery = '';
        this.searchResults = [];
        this.searching = false;
        this.confirmedMatches.clear();
        this.autoMatchOverrides.clear();

        this.updateComplete.then(() => {
            if (this.dialog) this.dialog.open = true;
            void this.runInitialSearch();
        });
    }

    close(): void {
        if (this.dialog) this.dialog.open = false;
    }

    // ─── Lifecycle ──────────────────────────────────

    override disconnectedCallback(): void {
        super.disconnectedCallback();

        if (this.searchTimer) {
            clearTimeout(this.searchTimer);
        }
    }

    // ─── Data fetching ──────────────────────────────

    private async runInitialSearch(): Promise<void> {
        this.loading = true;

        try {
            const paths = this.phantomTracks.map(
                (t) => t.FilePath,
            );
            const result = await FindPhantomMatches(
                this.playlistId,
                paths,
            );

            this.autoMatched =
                result.AutoMatched ?? [];
            this.unmatched = result.Unmatched ?? [];

            if (this.unmatched.length > 0) {
                this.selectedPhantom =
                    this.unmatched[0] ?? null;
                await this.loadCandidatesForSelected();
            }
        } catch (err) {
            console.error(
                'Failed to find phantom matches:',
                err,
            );
        } finally {
            this.loading = false;
        }
    }

    private async loadCandidatesForSelected(): Promise<void> {
        if (!this.selectedPhantom) {
            this.candidates = [];

            return;
        }

        this.candidatesLoading = true;

        try {
            this.candidates =
                await GetPhantomCandidates(
                    this.playlistId,
                    this.selectedPhantom,
                );
        } catch (err) {
            console.error(
                'Failed to load candidates:',
                err,
            );
            this.candidates = [];
        } finally {
            this.candidatesLoading = false;
        }
    }

    private async runLibrarySearch(): Promise<void> {
        const query = this.searchQuery.trim();

        if (!query) {
            this.searchResults = [];

            return;
        }

        this.searching = true;

        try {
            this.searchResults =
                await SearchLibrary(query);
        } catch (err) {
            console.error(
                'Library search failed:',
                err,
            );
            this.searchResults = [];
        } finally {
            this.searching = false;
        }
    }

    // ─── Event handlers ─────────────────────────────

    private handlePhantomClick(path: string): void {
        this.selectedPhantom = path;
        this.searchQuery = '';
        this.searchResults = [];
        void this.loadCandidatesForSelected();
    }

    private handleCandidateDblClick(
        candidate: playlist.CandidateTrack,
    ): void {
        if (!this.selectedPhantom) return;

        this.confirmedMatches.set(
            this.selectedPhantom,
            candidate.FilePath,
        );

        // Advance to next unmatched phantom.
        const remaining = this.unmatched.filter(
            (p) => !this.confirmedMatches.has(p),
        );

        if (remaining.length > 0) {
            this.selectedPhantom =
                remaining[0] ?? null;
            void this.loadCandidatesForSelected();
        } else {
            this.selectedPhantom = null;
            this.candidates = [];
        }

        this.requestUpdate();
    }

    private handleRemoveAutoMatch(
        phantomPath: string,
    ): void {
        this.autoMatchOverrides.set(phantomPath, null);
        this.unmatched = [
            ...this.unmatched,
            phantomPath,
        ];

        if (!this.selectedPhantom) {
            this.selectedPhantom = phantomPath;
            void this.loadCandidatesForSelected();
        }

        this.requestUpdate();
    }

    private handleSearchInput = (
        e: InputEvent,
    ): void => {
        const input = e.target as HTMLInputElement;
        this.searchQuery = input.value;

        if (this.searchTimer) {
            clearTimeout(this.searchTimer);
        }

        this.searchTimer = setTimeout(() => {
            void this.runLibrarySearch();
        }, SEARCH_DEBOUNCE_MS);
    };

    private handleSearchKeydown = (
        e: KeyboardEvent,
    ): void => {
        if (e.key === 'Enter') {
            e.preventDefault();

            if (this.searchTimer) {
                clearTimeout(this.searchTimer);
            }

            void this.runLibrarySearch();
        }

        e.stopPropagation();
    };

    private handleRemoveSelected = async (): Promise<void> => {
        // Remove all unmatched phantoms that don't have a
        // confirmed match.
        const toRemove = this.unmatched.filter(
            (p) => !this.confirmedMatches.has(p),
        );

        if (toRemove.length === 0) return;

        try {
            await RemovePhantomTracks(
                this.playlistId,
                toRemove,
            );
            this.unmatched = this.unmatched.filter(
                (p) => !toRemove.includes(p),
            );
            this.selectedPhantom = null;
            this.candidates = [];

            this.dispatchEvent(
                new CustomEvent(
                    'phantom-resolved',
                    { bubbles: true },
                ),
            );

            if (
                this.unmatched.length === 0 &&
                this.effectiveAutoMatched.length === 0 &&
                this.confirmedMatches.size === 0
            ) {
                this.close();
            }
        } catch (err) {
            console.error(
                'Failed to remove phantom tracks:',
                err,
            );
        }
    };

    private handleApplyAndClose = async (): Promise<void> => {
        // Collect all matches: auto-matched + confirmed.
        const allMatches: Record<string, string> = {};

        for (const match of this.effectiveAutoMatched) {
            allMatches[match.PhantomPath] =
                match.Candidate.FilePath;
        }

        for (const [
            phantom,
            resolved,
        ] of this.confirmedMatches) {
            allMatches[phantom] = resolved;
        }

        try {
            if (Object.keys(allMatches).length > 0) {
                await ResolvePhantomTracks(
                    this.playlistId,
                    allMatches,
                );
            }
        } catch (err) {
            console.error(
                'Failed to resolve phantom tracks:',
                err,
            );

            return;
        }

        this.dispatchEvent(
            new CustomEvent(
                'phantom-resolved',
                { bubbles: true },
            ),
        );
        this.close();
    };

    // ─── Computed ───────────────────────────────────

    private get effectiveAutoMatched(): playlist.PhantomMatch[] {
        return this.autoMatched.filter(
            (m) =>
                !this.autoMatchOverrides.has(
                    m.PhantomPath,
                ),
        );
    }

    private get hasChanges(): boolean {
        return (
            this.effectiveAutoMatched.length > 0 ||
            this.confirmedMatches.size > 0
        );
    }

    private get unresolvedCount(): number {
        return this.unmatched.filter(
            (p) => !this.confirmedMatches.has(p),
        ).length;
    }

    // ─── Formatting helpers ─────────────────────────

    private formatDuration(ms: string): string {
        return formatMilliseconds(ms);
    }

    private filenameFromPath(path: string): string {
        const parts = path.split('/');

        return parts[parts.length - 1] ?? path;
    }

    private scorePercent(score: number): string {
        return `${Math.round(score * 100)}%`;
    }

    // ─── Rendering ──────────────────────────────────

    static override styles = [
        css`
            wa-dialog {
                --width: 860px;
            }

            wa-dialog::part(dialog) {
                background: var(
                    --yj-bg-surface,
                    #212529
                );
                color: var(
                    --yj-text-primary,
                    #fff
                );
                border: 1px solid
                    var(--yj-border, #444);
                border-radius: 8px;
            }

            wa-dialog::part(title) {
                font-size: 16px;
                font-weight: 600;
                color: var(
                    --yj-text-primary,
                    #fff
                );
                padding: 16px 20px 8px;
            }

            wa-dialog::part(header-actions) {
                padding: 16px 20px 8px;
            }

            wa-dialog::part(close-button__base) {
                color: var(
                    --yj-text-tertiary,
                    #888
                );
            }

            wa-dialog::part(body) {
                padding: 0 20px 20px;
            }

            .loading {
                text-align: center;
                padding: 2em;
                color: var(
                    --yj-text-tertiary,
                    #888
                );
            }

            /* Auto-match section */
            .auto-match-header {
                display: flex;
                align-items: center;
                gap: 8px;
                padding: 8px 12px;
                background: color-mix(
                    in srgb,
                    var(--yj-success, #2f9e44)
                        15%,
                    var(
                        --yj-bg-elevated,
                        #343a40
                    )
                );
                border-radius: 4px;
                cursor: pointer;
                font-size: 13px;
                margin-bottom: 12px;
                user-select: none;
            }

            .auto-match-header:hover {
                background: color-mix(
                    in srgb,
                    var(--yj-success, #2f9e44)
                        25%,
                    var(
                        --yj-bg-elevated,
                        #343a40
                    )
                );
            }

            .auto-match-header wa-icon {
                color: var(
                    --yj-success,
                    #2f9e44
                );
                font-size: 12px;
                transition: transform 0.15s;
            }

            .auto-match-header
                wa-icon.expanded {
                transform: rotate(90deg);
            }

            .auto-match-count {
                color: var(
                    --yj-success,
                    #2f9e44
                );
                font-weight: 600;
            }

            .auto-match-list {
                margin-bottom: 12px;
            }

            .auto-match-pair {
                display: flex;
                align-items: center;
                gap: 8px;
                padding: 6px 12px;
                font-size: 12px;
                border-bottom: 1px solid
                    var(
                        --yj-border-subtle,
                        #333
                    );
            }

            .auto-match-pair
                .phantom-name {
                flex: 1;
                color: var(
                    --yj-text-secondary,
                    #adb5bd
                );
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
            }

            .auto-match-pair .arrow {
                color: var(
                    --yj-text-tertiary,
                    #888
                );
                flex-shrink: 0;
            }

            .auto-match-pair
                .match-name {
                flex: 1;
                color: var(
                    --yj-text-primary,
                    #fff
                );
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
            }

            .auto-match-pair .remove-btn {
                background: none;
                border: none;
                color: var(
                    --yj-text-tertiary,
                    #888
                );
                cursor: pointer;
                padding: 2px;
                font-size: 12px;
                flex-shrink: 0;
            }

            .auto-match-pair
                .remove-btn:hover {
                color: var(
                    --yj-error,
                    #e03131
                );
            }

            /* Two-panel layout */
            .panels {
                display: flex;
                gap: 1px;
                background: var(
                    --yj-border-subtle,
                    #333
                );
                border: 1px solid
                    var(
                        --yj-border-subtle,
                        #333
                    );
                border-radius: 4px;
                overflow: hidden;
                min-height: 300px;
                max-height: 400px;
            }

            .panel-left,
            .panel-right {
                flex: 1;
                background: var(
                    --yj-bg-elevated,
                    #343a40
                );
                overflow-y: auto;
                display: flex;
                flex-direction: column;
            }

            .panel-header {
                padding: 8px 12px;
                font-size: 11px;
                font-weight: 600;
                text-transform: uppercase;
                letter-spacing: 0.05em;
                color: var(
                    --yj-text-tertiary,
                    #888
                );
                border-bottom: 1px solid
                    var(
                        --yj-border-subtle,
                        #333
                    );
                flex-shrink: 0;
            }

            .panel-body {
                flex: 1;
                overflow-y: auto;
            }

            /* Phantom list items */
            .phantom-item {
                display: flex;
                align-items: center;
                gap: 8px;
                padding: 6px 12px;
                font-size: 12px;
                cursor: pointer;
                border-bottom: 1px solid
                    var(
                        --yj-border-subtle,
                        #2a2a2a
                    );
            }

            .phantom-item:hover {
                background: rgba(
                    255,
                    255,
                    255,
                    0.04
                );
            }

            .phantom-item.selected {
                background: rgba(
                    255,
                    212,
                    59,
                    0.1
                );
                border-left: 2px solid
                    var(--yj-accent, #ffd43b);
            }

            .phantom-item.matched {
                opacity: 0.5;
            }

            .phantom-item .check {
                color: var(
                    --yj-success,
                    #2f9e44
                );
                flex-shrink: 0;
                font-size: 12px;
            }

            .phantom-item .name {
                flex: 1;
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
                color: var(
                    --yj-text-secondary,
                    #adb5bd
                );
            }

            /* Candidate items */
            .candidate-item {
                display: flex;
                align-items: center;
                gap: 8px;
                padding: 8px 12px;
                font-size: 12px;
                cursor: pointer;
                border-bottom: 1px solid
                    var(
                        --yj-border-subtle,
                        #2a2a2a
                    );
            }

            .candidate-item:hover {
                background: rgba(
                    255,
                    255,
                    255,
                    0.06
                );
            }

            .candidate-info {
                flex: 1;
                overflow: hidden;
                min-width: 0;
            }

            .candidate-title {
                color: var(
                    --yj-text-primary,
                    #fff
                );
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
            }

            .candidate-meta {
                font-size: 11px;
                color: var(
                    --yj-text-tertiary,
                    #888
                );
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
                margin-top: 1px;
            }

            .candidate-score {
                flex-shrink: 0;
                font-size: 10px;
                padding: 1px 6px;
                border-radius: 3px;
                background: rgba(
                    255,
                    212,
                    59,
                    0.15
                );
                color: var(--yj-accent, #ffd43b);
            }

            .candidate-duration {
                flex-shrink: 0;
                font-size: 11px;
                color: var(
                    --yj-text-tertiary,
                    #888
                );
                font-variant-numeric: tabular-nums;
            }

            /* Search section */
            .search-section {
                border-top: 1px solid
                    var(
                        --yj-border-subtle,
                        #333
                    );
                padding: 8px 12px;
                flex-shrink: 0;
            }

            .search-label {
                font-size: 10px;
                text-transform: uppercase;
                letter-spacing: 0.05em;
                color: var(
                    --yj-text-tertiary,
                    #888
                );
                margin-bottom: 4px;
            }

            .search-input {
                width: 100%;
                box-sizing: border-box;
                padding: 6px 8px;
                background: var(
                    --yj-bg-surface,
                    #212529
                );
                border: 1px solid
                    var(
                        --yj-border-subtle,
                        #555
                    );
                border-radius: 4px;
                color: var(
                    --yj-text-primary,
                    #fff
                );
                font-size: 12px;
                font-family: inherit;
                outline: none;
            }

            .search-input:focus {
                border-color: var(
                    --yj-accent,
                    #ffd43b
                );
            }

            .search-input::placeholder {
                color: var(
                    --yj-text-tertiary,
                    #888
                );
            }

            .empty-message {
                text-align: center;
                padding: 2em 1em;
                color: var(
                    --yj-text-tertiary,
                    #888
                );
                font-size: 12px;
            }

            /* Footer buttons */
            .footer {
                display: flex;
                justify-content: flex-end;
                gap: 8px;
                margin-top: 16px;
            }

            .btn {
                background: none;
                border: 1px solid
                    var(
                        --yj-border-subtle,
                        #555
                    );
                border-radius: 4px;
                color: var(
                    --yj-text-primary,
                    #fff
                );
                padding: 6px 16px;
                font-size: 13px;
                cursor: pointer;
                font-family: inherit;
            }

            .btn:hover {
                border-color: var(
                    --yj-accent,
                    #ffd43b
                );
                color: var(--yj-accent, #ffd43b);
            }

            .btn-danger {
                color: var(
                    --yj-text-secondary,
                    #adb5bd
                );
            }

            .btn-danger:hover {
                border-color: var(
                    --yj-error,
                    #e03131
                );
                color: var(
                    --yj-error,
                    #e03131
                );
            }

            .btn-primary {
                background: var(
                    --yj-accent,
                    #ffd43b
                );
                color: #000;
                border-color: var(
                    --yj-accent,
                    #ffd43b
                );
                font-weight: 600;
            }

            .btn-primary:hover {
                background: color-mix(
                    in srgb,
                    var(--yj-accent, #ffd43b)
                        85%,
                    #000
                );
                color: #000;
            }

            .btn:disabled {
                opacity: 0.4;
                cursor: not-allowed;
            }

            .dbl-click-hint {
                font-size: 10px;
                color: var(
                    --yj-text-tertiary,
                    #666
                );
                text-align: center;
                padding: 4px;
            }
        `,
    ];

    override render() {
        return html`
            <wa-dialog
                label="Resolve Unmatched Tracks"
            >
                ${this.loading
                    ? html`<div class="loading">
                          Searching for
                          matches...
                      </div>`
                    : this.renderContent()}
            </wa-dialog>
        `;
    }

    private renderContent() {
        return html`
            ${this.renderAutoMatchSection()}
            ${this.unmatched.length > 0 ||
            this.confirmedMatches.size > 0
                ? this.renderPanels()
                : nothing}
            ${this.renderFooter()}
        `;
    }

    private renderAutoMatchSection() {
        const matches = this.effectiveAutoMatched;

        if (matches.length === 0) return nothing;

        return html`
            <div
                class="auto-match-header"
                @click=${() => {
                    this.autoMatchExpanded =
                        !this.autoMatchExpanded;
                }}
            >
                <wa-icon
                    name="chevron-right"
                    class=${this
                        .autoMatchExpanded
                        ? 'expanded'
                        : ''}
                ></wa-icon>
                <span class="auto-match-count">
                    ${matches.length}
                    track${matches.length !== 1
                        ? 's'
                        : ''}
                    auto-matched
                </span>
                <span
                    style="color: var(--yj-text-tertiary, #888); font-size: 11px;"
                >
                    (click to review)
                </span>
            </div>
            ${this.autoMatchExpanded
                ? html`<div
                      class="auto-match-list"
                  >
                      ${matches.map(
                          (m) => html`
                              <div
                                  class="auto-match-pair"
                              >
                                  <span
                                      class="phantom-name"
                                      title=${m.PhantomTitle ||
                                      this.filenameFromPath(
                                          m.PhantomPath,
                                      )}
                                  >
                                      ${m.PhantomTitle ||
                                      this.filenameFromPath(
                                          m.PhantomPath,
                                      )}
                                  </span>
                                  <span
                                      class="arrow"
                                      >&rarr;</span
                                  >
                                  <span
                                      class="match-name"
                                      title=${m
                                          .Candidate
                                          .Title ||
                                      m.Candidate
                                          .FilePath}
                                  >
                                      ${m.Candidate
                                          .Title ||
                                      this.filenameFromPath(
                                          m.Candidate
                                              .FilePath,
                                      )}
                                      ${m.Candidate
                                          .Artist
                                          ? html`<span
                                                style="color: var(--yj-text-tertiary, #888);"
                                            >
                                                &mdash;
                                                ${m
                                                    .Candidate
                                                    .Artist}
                                            </span>`
                                          : nothing}
                                  </span>
                                  <button
                                      class="remove-btn"
                                      title="Remove auto-match"
                                      @click=${(
                                          e: Event,
                                      ) => {
                                          e.stopPropagation();
                                          this.handleRemoveAutoMatch(
                                              m.PhantomPath,
                                          );
                                      }}
                                  >
                                      <wa-icon
                                          name="xmark"
                                      ></wa-icon>
                                  </button>
                              </div>
                          `,
                      )}
                  </div>`
                : nothing}
        `;
    }

    private renderPanels() {
        return html`
            <div class="panels">
                <div class="panel-left">
                    <div class="panel-header">
                        Unmatched
                        (${this.unresolvedCount})
                    </div>
                    <div class="panel-body">
                        ${this.unmatched.map(
                            (path) => {
                                const isSelected =
                                    this
                                        .selectedPhantom ===
                                    path;
                                const isMatched =
                                    this.confirmedMatches.has(
                                        path,
                                    );
                                const track =
                                    this.phantomTracks.find(
                                        (t) =>
                                            t.FilePath ===
                                            path,
                                    );
                                const label =
                                    track?.Title ||
                                    this.filenameFromPath(
                                        path,
                                    );

                                return html`
                                    <div
                                        class="phantom-item ${isSelected
                                            ? 'selected'
                                            : ''} ${isMatched
                                            ? 'matched'
                                            : ''}"
                                        @click=${() =>
                                            this.handlePhantomClick(
                                                path,
                                            )}
                                        title=${path}
                                    >
                                        ${isMatched
                                            ? html`<wa-icon
                                                  name="check"
                                                  class="check"
                                              ></wa-icon>`
                                            : nothing}
                                        <span
                                            class="name"
                                        >
                                            ${label}
                                        </span>
                                    </div>
                                `;
                            },
                        )}
                    </div>
                </div>
                <div class="panel-right">
                    ${this.selectedPhantom
                        ? this.renderRightPanel()
                        : html`<div
                              class="empty-message"
                          >
                              Select a phantom
                              track to see
                              candidates.
                          </div>`}
                </div>
            </div>
        `;
    }

    private renderRightPanel() {
        const label =
            this.phantomTracks.find(
                (t) =>
                    t.FilePath ===
                    this.selectedPhantom,
            )?.Title ||
            this.filenameFromPath(
                this.selectedPhantom ?? '',
            );

        return html`
            <div class="panel-header">
                Candidates for
                &ldquo;${label}&rdquo;
            </div>
            <div class="panel-body">
                ${this.candidatesLoading
                    ? html`<div
                          class="empty-message"
                      >
                          Searching...
                      </div>`
                    : this.candidates.length > 0
                      ? html`
                            <div
                                class="dbl-click-hint"
                            >
                                Double-click a
                                result to match
                            </div>
                            ${this.candidates.map(
                                (c) =>
                                    this.renderCandidateItem(
                                        c,
                                    ),
                            )}
                        `
                      : html`<div
                            class="empty-message"
                        >
                            No smart matches
                            found. Try
                            searching below.
                        </div>`}
                ${this.searchResults.length > 0
                    ? html`
                          <div
                              class="dbl-click-hint"
                              style="border-top: 1px solid var(--yj-border-subtle, #333); padding-top: 6px; margin-top: 4px;"
                          >
                              Library search
                              results
                          </div>
                          ${this.searchResults.map(
                              (c) =>
                                  this.renderCandidateItem(
                                      c,
                                  ),
                          )}
                      `
                    : nothing}
                ${this.searching
                    ? html`<div
                          class="empty-message"
                      >
                          Searching
                          library...
                      </div>`
                    : nothing}
            </div>
            <div class="search-section">
                <div class="search-label">
                    Search Library
                </div>
                <input
                    class="search-input"
                    type="text"
                    placeholder="Type to search..."
                    .value=${this.searchQuery}
                    @input=${this.handleSearchInput}
                    @keydown=${this
                        .handleSearchKeydown}
                />
            </div>
        `;
    }

    private renderCandidateItem(
        c: playlist.CandidateTrack,
    ) {
        const title =
            c.Title ||
            this.filenameFromPath(c.FilePath);
        const meta = [c.Artist, c.Album]
            .filter(Boolean)
            .join(' \u2014 ');

        return html`
            <div
                class="candidate-item"
                @dblclick=${() =>
                    this.handleCandidateDblClick(
                        c,
                    )}
                title=${c.FilePath}
            >
                <div class="candidate-info">
                    <div class="candidate-title">
                        ${title}
                    </div>
                    ${meta
                        ? html`<div
                              class="candidate-meta"
                          >
                              ${meta}
                          </div>`
                        : nothing}
                </div>
                ${c.Score > 0
                    ? html`<span
                          class="candidate-score"
                      >
                          ${this.scorePercent(
                              c.Score,
                          )}
                      </span>`
                    : nothing}
                <span class="candidate-duration">
                    ${this.formatDuration(
                        c.Duration,
                    )}
                </span>
            </div>
        `;
    }

    private renderFooter() {
        return html`
            <div class="footer">
                ${this.unresolvedCount > 0
                    ? html`<button
                          class="btn btn-danger"
                          @click=${this
                              .handleRemoveSelected}
                      >
                          Remove Unmatched
                          (${this.unresolvedCount})
                      </button>`
                    : nothing}
                <button
                    class="btn btn-primary"
                    ?disabled=${!this.hasChanges}
                    @click=${this
                        .handleApplyAndClose}
                >
                    Apply & Close
                </button>
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'phantom-resolver': PhantomResolver;
    }
}

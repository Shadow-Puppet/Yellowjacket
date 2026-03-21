import { LitElement, html, css, nothing } from 'lit';
import {
    customElement,
    property,
    state,
} from 'lit/decorators.js';
import { library } from '@go/models';
import {
    EvaluateSmartPlaylist,
    GetSmartPlaylistRules,
    UpdateSmartPlaylistRules,
} from '@go/playlist/Service';
import { EventsOn } from '@runtime/runtime';
import { Events } from '../../events';
import { queueStore } from '@store/queue-store';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/track-list/track-list.js';
import '@components/smart-playlist-editor/smart-playlist-editor.js';
import { designTokens } from '../../styles/tokens.css';


/**
 * Format total milliseconds as a human-readable duration.
 * e.g. 8_100_000 → "2h 15m", 180_000 → "3m 0s", 45_000 → "0m 45s"
 */
function formatTotalDuration(totalMs: number): string {
    if (totalMs <= 0) return '0m';

    const totalSeconds = Math.floor(totalMs / 1000);
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);

    if (hours > 0) {
        return `${hours}h ${minutes}m`;
    }

    const seconds = totalSeconds % 60;

    if (minutes > 0) {
        return `${minutes}m ${seconds}s`;
    }

    return `${seconds}s`;
}

@customElement('smart-playlist-details')
export class SmartPlaylistDetails extends LitElement {
    @property({ type: Number, attribute: 'playlist-id' })
    playlistId = 0;

    @property({ type: String, attribute: 'playlist-name' })
    playlistName = '';

    @property({ type: Boolean, attribute: 'auto-edit' })
    autoEdit = false;

    @state()
    private tracks: library.Track[] = [];

    @state()
    private loading = true;

    @state()
    private editing = false;

    @state()
    private currentRulesJSON = '';

    @state()
    private pendingRulesJSON = '';

    @state()
    private saving = false;

    private playlistDeletedCleanup: (() => void) | null = null;
    private playlistRenamedCleanup: (() => void) | null = null;

    // =================================================================
    // Styles
    // =================================================================

    static override styles = [designTokens, css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
            height: 100%;
        }

        /* ====================================
         * Header
         * ==================================== */

        .smart-playlist-header {
            display: flex;
            align-items: center;
            gap: 20px;
            padding: 16px 20px;
            flex-shrink: 0;
            border-bottom: 1px solid
                var(
                    --yj-border-subtle,
                    rgba(255, 255, 255, 0.06)
                );
        }

        .back-button {
            display: flex;
            align-items: center;
            justify-content: center;
            width: 32px;
            height: 32px;
            border: none;
            border-radius: 50%;
            background: var(
                --yj-bg-overlay,
                rgba(255, 255, 255, 0.06)
            );
            color: var(--yj-text-primary, #fff);
            cursor: pointer;
            flex-shrink: 0;
            transition: background-color 0.15s ease;
        }

        .back-button:hover {
            background: var(
                --yj-bg-hover,
                rgba(255, 255, 255, 0.12)
            );
        }

        .back-button wa-icon {
            font-size: 16px;
        }

        .playlist-avatar {
            width: 80px;
            height: 80px;
            border-radius: 8px;
            overflow: hidden;
            background: linear-gradient(
                135deg,
                var(--yj-bg-overlay, #404040) 0%,
                var(--yj-bg-surface, #282828) 100%
            );
            display: flex;
            align-items: center;
            justify-content: center;
            flex-shrink: 0;
        }

        .playlist-avatar wa-icon {
            font-size: 32px;
            color: var(
                --yj-text-secondary,
                #b3b3b3
            );
        }

        .playlist-info {
            display: flex;
            flex-direction: column;
            gap: 4px;
            min-width: 0;
            flex: 1;
        }

        .playlist-title {
            font-size: 24px;
            font-weight: 700;
            color: var(--yj-text-primary, #fff);
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            margin: 0;
            line-height: 1.2;
        }

        .track-count {
            font-size: var(--yj-text-md);
            color: var(
                --yj-text-secondary,
                #b3b3b3
            );
        }

        /* ====================================
         * Actions
         * ==================================== */

        .playlist-actions {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 12px 20px 8px;
            flex-shrink: 0;
        }

        .action-button {
            background: none;
            border: 1px solid var(--yj-border-subtle, #555);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 4px 10px;
            font-size: 12px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 5px;
            font-family: inherit;
            transition: border-color 0.15s ease, color 0.15s ease;
        }

        .action-button:hover {
            border-color: var(--yj-accent, #ffd43b);
            color: var(--yj-accent, #ffd43b);
        }

        .action-button:disabled {
            opacity: 0.4;
            cursor: not-allowed;
        }

        .action-button:disabled:hover {
            border-color: var(--yj-border-subtle, #555);
            color: var(--yj-text-primary, #fff);
        }

        .action-button wa-icon {
            font-size: 12px;
        }

        /* ====================================
         * Content
         * ==================================== */

        .content {
            flex: 1;
            overflow: hidden;
        }

        track-list {
            width: 100%;
            height: 100%;
        }

        .loading {
            display: flex;
            justify-content: center;
            align-items: center;
            padding: 32px;
            color: var(--yj-text-secondary, #b3b3b3);
        }

        .empty-state {
            padding: 32px 20px;
            color: var(--yj-text-tertiary, #666);
            font-size: 13px;
            text-align: center;
        }

        .editor-container {
            flex: 1;
            overflow: auto;
            padding: 0 20px 20px;
        }
    `];

    // =================================================================
    // Lifecycle
    // =================================================================

    override async connectedCallback() {
        super.connectedCallback();
        await this.loadTracks();

        if (this.autoEdit) {
            this.autoEdit = false;
            this.handleEditRules();
        }

        this.playlistDeletedCleanup = EventsOn(
            Events.PlaylistDeleted,
            (deletedId: number) => {
                if (deletedId === this.playlistId) {
                    this.navigateBack();
                }
            },
        );

        this.playlistRenamedCleanup = EventsOn(
            Events.PlaylistRenamed,
            (summary: { ID: number; Name: string }) => {
                if (summary.ID === this.playlistId) {
                    this.playlistName = summary.Name;
                }
            },
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();

        if (this.playlistDeletedCleanup) {
            this.playlistDeletedCleanup();
            this.playlistDeletedCleanup = null;
        }

        if (this.playlistRenamedCleanup) {
            this.playlistRenamedCleanup();
            this.playlistRenamedCleanup = null;
        }
    }

    // =================================================================
    // Data loading
    // =================================================================

    private async loadTracks() {
        if (!this.playlistId) return;

        this.loading = true;

        try {
            const result = await EvaluateSmartPlaylist(this.playlistId);

            this.tracks = result ?? [];
        } catch (error) {
            console.error(
                'Failed to evaluate smart playlist:',
                error,
            );
            this.tracks = [];
        } finally {
            this.loading = false;
        }
    }

    // =================================================================
    // Navigation
    // =================================================================

    private navigateBack() {
        this.dispatchEvent(
            new CustomEvent('navigate', {
                bubbles: true,
                composed: true,
                detail: { view: 'playlists' },
            }),
        );
    }

    // =================================================================
    // Actions
    // =================================================================

    private handlePlay() {
        const filePaths = this.tracks
            .filter((t) => t.FilePath)
            .map((t) => t.FilePath);

        if (filePaths.length === 0) return;

        queueStore.setQueue(filePaths, 0, false);
    }

    private handleShuffle() {
        const filePaths = this.tracks
            .filter((t) => t.FilePath)
            .map((t) => t.FilePath);

        if (filePaths.length === 0) return;

        queueStore.setQueue(filePaths, 0, true);
    }

    private handleRefresh() {
        void this.loadTracks();
    }

    private async handleEditRules() {
        try {
            const result = await GetSmartPlaylistRules(this.playlistId);

            this.currentRulesJSON = result;
            this.pendingRulesJSON = result;
            this.editing = true;
        } catch (error) {
            console.error('Failed to load smart playlist rules:', error);
        }
    }

    private async handleSaveRules() {
        this.saving = true;

        try {
            await UpdateSmartPlaylistRules(
                this.playlistId,
                this.pendingRulesJSON,
            );

            this.editing = false;
            this.loadTracks();
        } catch (error) {
            console.error('Failed to save smart playlist rules:', error);
        } finally {
            this.saving = false;
        }
    }

    private handleCancelEdit() {
        this.editing = false;
        this.pendingRulesJSON = '';
    }

    private handleRulesChanged(e: CustomEvent) {
        this.pendingRulesJSON = e.detail.json;
    }

    // =================================================================
    // Helpers
    // =================================================================

    private getTotalDuration(): string {
        const totalMs = this.tracks.reduce(
            (sum, t) => sum + Number(t.TrackLength || 0),
            0,
        );

        return formatTotalDuration(totalMs);
    }

    // =================================================================
    // Render
    // =================================================================

    override render() {
        const trackCount = this.tracks.length;
        const trackLabel = trackCount === 1 ? 'track' : 'tracks';
        const hasPlayableTracks = this.tracks.some((t) => t.FilePath);

        return html`
            <div class="smart-playlist-header">
                <button
                    class="back-button"
                    @click=${this.navigateBack}
                    title="Back to playlists"
                    aria-label="Back to playlists"
                >
                    <wa-icon name="arrow-left"></wa-icon>
                </button>
                <div class="playlist-avatar">
                    <wa-icon name="filter"></wa-icon>
                </div>
                <div class="playlist-info">
                    <h1
                        class="playlist-title"
                        title="${this.playlistName}"
                    >
                        ${this.playlistName}
                    </h1>
                    ${!this.loading
                        ? html`
                              <span class="track-count">
                                  ${trackCount}
                                  ${trackLabel}
                                  · ${this.getTotalDuration()}
                              </span>
                          `
                        : nothing}
                </div>
            </div>

            ${this.loading
                ? html`<div class="loading">
                      Evaluating smart playlist…
                  </div>`
                : html`
                      <div class="playlist-actions">
                          ${this.editing
                              ? html`
                                    <button
                                        class="action-button"
                                        @click=${this.handleSaveRules}
                                        ?disabled=${this.saving}
                                        title="Save rules"
                                    >
                                        <wa-icon name="floppy-disk"></wa-icon>
                                        ${this.saving ? 'Saving…' : 'Save Rules'}
                                    </button>
                                    <button
                                        class="action-button"
                                        @click=${this.handleCancelEdit}
                                        ?disabled=${this.saving}
                                        title="Cancel editing"
                                    >
                                        <wa-icon name="xmark"></wa-icon>
                                        Cancel
                                    </button>
                                `
                              : html`
                                    <button
                                        class="action-button"
                                        @click=${this.handlePlay}
                                        ?disabled=${!hasPlayableTracks}
                                        title="Play all tracks"
                                    >
                                        <wa-icon name="play"></wa-icon>
                                        Play
                                    </button>
                                    <button
                                        class="action-button"
                                        @click=${this.handleShuffle}
                                        ?disabled=${!hasPlayableTracks}
                                        title="Shuffle all tracks"
                                    >
                                        <wa-icon name="shuffle"></wa-icon>
                                        Shuffle
                                    </button>
                                    <button
                                        class="action-button"
                                        @click=${this.handleRefresh}
                                        title="Re-evaluate smart playlist rules"
                                    >
                                        <wa-icon name="arrow-rotate-right"></wa-icon>
                                        Refresh
                                    </button>
                                    <button
                                        class="action-button"
                                        @click=${this.handleEditRules}
                                        title="Edit smart playlist rules"
                                    >
                                        <wa-icon name="pen-to-square"></wa-icon>
                                        Edit Rules
                                    </button>
                                `}
                      </div>
                      ${this.editing
                          ? html`
                                <div class="editor-container">
                                    <smart-playlist-editor
                                        .rules=${this.currentRulesJSON}
                                        @rules-changed=${this.handleRulesChanged}
                                    ></smart-playlist-editor>
                                </div>
                            `
                          : trackCount > 0
                            ? html`
                                  <div class="content">
                                      <track-list
                                          .externalTracks=${this.tracks}
                                      ></track-list>
                                  </div>
                              `
                            : html`
                                  <div class="empty-state">
                                      No tracks match the current rules.
                                      Configure rules and click Refresh.
                                  </div>
                              `}
                  `}
        `;
    }
}

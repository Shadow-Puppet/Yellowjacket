import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/switch/switch.js';
import { AddTracksToPlaylist } from '@go/playlist/Service';
import { formatMilliseconds } from '@utils/time';
import { nameDialogsIn } from '@utils/name-dialog';

interface DuplicateTrack {
    FilePath: string;
    Title: string;
    Artist: string;
    Album: string;
    Duration: string;
}

/**
 * Modal dialog for resolving duplicate tracks when adding
 * to a playlist. Steps through each duplicate one at a time,
 * allowing the user to Add or Skip with an "apply to all"
 * toggle for batch operations.
 *
 * @fires playlist-action-complete - When all tracks have been processed.
 */
@customElement('duplicate-tracks-dialog')
export class DuplicateTracksDialog extends LitElement {
    @query('wa-dialog')
    private dialog!: HTMLElement & { open: boolean };

    @state() private duplicates: DuplicateTrack[] = [];
    @state() private currentIndex = 0;
    @state() private applyToAll = false;

    private playlistId = 0;
    private uniquePaths: string[] = [];
    private tracksToAdd: string[] = [];

    /** Opens the dialog. Called by playlist-picker when duplicates are found. */
    show(
        playlistId: number,
        duplicates: DuplicateTrack[],
        uniquePaths: string[],
    ): void {
        this.playlistId = playlistId;
        this.duplicates = duplicates;
        this.uniquePaths = uniquePaths;
        this.currentIndex = 0;
        this.applyToAll = false;
        this.tracksToAdd = [];

        this.updateComplete.then(() => {
            if (this.dialog) this.dialog.open = true;
        });
    }

    close(): void {
        if (this.dialog) this.dialog.open = false;
    }

    // =================================================================
    // STYLES
    // =================================================================

    static override styles = css`
        wa-dialog {
            --width: 480px;
        }

        wa-dialog::part(dialog) {
            background: var(--yj-bg-surface, #212529);
            color: var(--yj-text-primary, #fff);
            border: 1px solid var(--yj-border, #444);
            border-radius: 8px;
        }

        wa-dialog::part(title) {
            font-size: 16px;
            font-weight: 600;
            color: var(--yj-text-primary, #fff);
            padding: 16px 20px 8px;
        }

        wa-dialog::part(header-actions) {
            padding: 16px 20px 8px;
        }

        wa-dialog::part(close-button__base) {
            color: var(--yj-text-tertiary, #888);
        }

        wa-dialog::part(body) {
            padding: 0 20px 20px;
        }

        .summary {
            font-size: 13px;
            color: var(--yj-text-secondary, #b3b3b3);
            margin-bottom: 16px;
        }

        .summary strong {
            color: var(--yj-text-primary, #fff);
        }

        .progress {
            font-size: 12px;
            color: var(--yj-text-tertiary, #888);
            margin-bottom: 12px;
        }

        .track-card {
            background: var(--yj-bg-elevated, #343a40);
            border-radius: 6px;
            padding: 16px;
            margin-bottom: 16px;
        }

        .track-title {
            font-size: 15px;
            font-weight: 600;
            color: var(--yj-text-primary, #fff);
            margin-bottom: 4px;
            word-break: break-word;
        }

        .track-artist {
            font-size: 13px;
            color: var(--yj-text-secondary, #b3b3b3);
            margin-bottom: 2px;
        }

        .track-album {
            font-size: 13px;
            color: var(--yj-text-tertiary, #888);
            margin-bottom: 2px;
        }

        .track-duration {
            font-size: 12px;
            color: var(--yj-text-tertiary, #888);
            font-variant-numeric: tabular-nums;
        }

        .toggle-row {
            display: flex;
            align-items: center;
            gap: 8px;
            margin-bottom: 16px;
            font-size: 13px;
            color: var(--yj-text-secondary, #b3b3b3);
        }

        .actions {
            display: flex;
            justify-content: flex-end;
            gap: 8px;
        }

        .btn {
            padding: 6px 16px;
            border-radius: 4px;
            border: 1px solid var(--yj-border, #444);
            background: var(--yj-bg-elevated, #343a40);
            color: var(--yj-text-primary, #fff);
            font-size: 13px;
            cursor: pointer;
            font-family: inherit;
            transition: background-color 0.15s ease;
        }

        .btn:hover {
            background: var(--yj-bg-overlay, #495057);
        }

        .btn-primary {
            background: var(--yj-accent, #ffd43b);
            color: var(--yj-accent-fg, #000);
            border-color: var(--yj-accent, #ffd43b);
        }

        .btn-primary:hover {
            background: var(--yj-accent-hover, #ffe066);
            border-color: var(--yj-accent-hover, #ffe066);
        }
    `;

    // =================================================================
    // HANDLERS
    // =================================================================

    private handleAdd = () => {
        const current = this.duplicates[this.currentIndex];

        if (!current) return;

        this.tracksToAdd.push(current.FilePath);

        if (this.applyToAll) {
            // Add all remaining duplicates.
            for (
                let i = this.currentIndex + 1;
                i < this.duplicates.length;
                i++
            ) {
                this.tracksToAdd.push(
                    this.duplicates[i]!.FilePath,
                );
            }

            void this.finalize();

            return;
        }

        this.currentIndex++;

        if (this.currentIndex >= this.duplicates.length) {
            void this.finalize();
        } else {
            this.requestUpdate();
        }
    };

    private handleSkip = () => {
        if (this.applyToAll) {
            // Skip all remaining — finalize immediately.
            void this.finalize();

            return;
        }

        this.currentIndex++;

        if (this.currentIndex >= this.duplicates.length) {
            void this.finalize();
        } else {
            this.requestUpdate();
        }
    };

    private async finalize(): Promise<void> {
        const combined = [
            ...this.uniquePaths,
            ...this.tracksToAdd,
        ];

        if (combined.length > 0) {
            try {
                await AddTracksToPlaylist(
                    this.playlistId,
                    combined,
                );
            } catch (err) {
                console.error(
                    'Failed to add tracks to playlist:',
                    err,
                );
            }
        }

        this.dispatchEvent(
            new CustomEvent('playlist-action-complete', {
                bubbles: true,
                composed: true,
            }),
        );
        this.close();
    }

    // =================================================================
    // RENDER
    // =================================================================

    /**
     * Web Awesome renders `label` into a heading it never points the
     * `<dialog>` at, so the dialog has no accessible name until
     * something sets one. See `utils/name-dialog.ts`.
     */
    override updated() {
        nameDialogsIn(this.shadowRoot);
    }

    override render() {
        const current = this.duplicates[this.currentIndex];

        return html`
            <wa-dialog label="Duplicate Tracks Found">
                ${current ? this.renderContent(current) : nothing}
            </wa-dialog>
        `;
    }

    private renderContent(current: DuplicateTrack) {
        const total = this.duplicates.length;
        const num = this.currentIndex + 1;

        return html`
            <div class="summary">
                <strong>${total} duplicate track${total !== 1 ? 's' : ''}</strong>
                already exist in this playlist.
            </div>
            <div class="progress">
                Track ${num} of ${total}
            </div>
            <div class="track-card">
                <div class="track-title">
                    ${current.Title || current.FilePath}
                </div>
                ${current.Artist
                    ? html`<div class="track-artist">
                          ${current.Artist}
                      </div>`
                    : nothing}
                ${current.Album
                    ? html`<div class="track-album">
                          ${current.Album}
                      </div>`
                    : nothing}
                <div class="track-duration">
                    ${formatMilliseconds(current.Duration)}
                </div>
            </div>
            <div class="toggle-row">
                <wa-switch
                    size="small"
                    ?checked=${this.applyToAll}
                    @change=${(e: Event) => {
                        this.applyToAll = (
                            e.target as HTMLInputElement
                        ).checked;
                    }}
                >
                    Apply to all remaining
                </wa-switch>
            </div>
            <div class="actions">
                <button class="btn" @click=${this.handleSkip}>
                    Skip
                </button>
                <button
                    class="btn btn-primary"
                    @click=${this.handleAdd}
                >
                    Add
                </button>
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'duplicate-tracks-dialog': DuplicateTracksDialog;
    }
}

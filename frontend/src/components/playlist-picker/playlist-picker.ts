import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state, query } from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';

import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';

import {
    GetAllPlaylists,
    AddTracksToPlaylist,
    CreatePlaylistWithTracks,
    FindDuplicateTracksInPlaylist,
} from '@go/playlist/service.js';
import { Events } from '../../events';
import type * as playlist from '@go/playlist/models.js';
import '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';
import { notificationStore } from '@store/notification-store';
import { describeError } from '@utils/describe-error';
import type { DuplicateTracksDialog } from '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';
import { list } from '@utils/binding';

/**
 * A reusable playlist picker that displays existing playlists
 * and allows creating new ones. Accepts file paths and handles
 * adding tracks to the selected/created playlist.
 *
 * @fires playlist-action-complete - When tracks have been added successfully.
 */
@customElement('playlist-picker')
export class PlaylistPicker extends LitElement {
    /** File paths to add when a playlist is selected or created. */
    @property({ type: Array }) filePaths: string[] = [];
    private cancelScanComplete?: () => void;

    @query('duplicate-tracks-dialog')
    private duplicateDialog!: DuplicateTracksDialog;

    @state() private mode: 'list' | 'create' = 'list';
    @state() private playlists: playlist.Summary[] = [];
    @state() private newPlaylistName = '';
    @state() private loading = false;

    static override styles = css`
        :host {
            display: block;
        }

        .picker-panel {
            background-color: var(--yj-bg-elevated, #343a40);
            border: 1px solid var(--yj-border, #444);
            border-radius: 6px;
            padding: 4px 0;
            box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
            min-width: 180px;
            max-height: 300px;
            overflow-y: auto;
        }

        .picker-panel wa-dropdown-item {
            cursor: pointer;
            --wa-color-text-normal: var(--yj-text-primary, #fff);
            font-size: 13px;
        }

        .picker-panel wa-dropdown-item:hover {
            background-color: var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
        }

        .separator {
            height: 1px;
            background: var(--yj-border-subtle, #555);
            margin: 4px 0;
        }

        .create-form {
            padding: 8px 12px;
            display: flex;
            flex-direction: column;
            gap: 8px;
        }

        .create-form input {
            background: var(--yj-bg-surface, #2b3035);
            border: 1px solid var(--yj-border-subtle, #555);
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 6px 8px;
            font-size: 13px;
            outline: none;
            font-family: inherit;
        }

        .create-form input:focus {
            border-color: var(--yj-accent, #ffd43b);
        }

        .create-form input::placeholder {
            color: var(--yj-text-tertiary, #888);
        }

        .button-row {
            display: flex;
            gap: 6px;
            justify-content: flex-end;
        }

        .button-row button {
            background: var(--yj-bg-overlay, #495057);
            border: none;
            border-radius: 4px;
            color: var(--yj-text-primary, #fff);
            padding: 4px 10px;
            font-size: 12px;
            cursor: pointer;
            font-family: inherit;
        }

        .button-row button:hover {
            background: #5a6268;
        }

        .button-row button.primary {
            background: var(--yj-accent, #ffd43b);
            color: var(--yj-accent-fg, #000);
        }

        .button-row button.primary:hover {
            background: var(--yj-accent-hover, #ffe066);
        }

        .button-row button.primary:disabled {
            background: var(--yj-accent-muted, #665a1e);
            color: var(--yj-text-tertiary, #888);
            cursor: not-allowed;
        }

        .empty-message {
            padding: 8px 12px;
            color: var(--yj-text-tertiary, #888);
            font-size: 13px;
        }
    `;

    override connectedCallback() {
        super.connectedCallback();
        this.loadPlaylists();
        this.cancelScanComplete = EventsOn(
            Events.LibraryScanComplete,
            () => this.loadPlaylists(),
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        this.cancelScanComplete?.();
    }

    private async loadPlaylists() {
        try {
            this.playlists = await list(GetAllPlaylists());
        } catch (err) {
            console.error('Failed to load playlists:', err);
            this.playlists = [];
        }
    }

    private handleSelectPlaylist = async (playlistId: number) => {
        if (this.loading || this.filePaths.length === 0) return;

        this.loading = true;

        try {
            const result = await FindDuplicateTracksInPlaylist(
                playlistId,
                this.filePaths,
            );
            const duplicates = result.Duplicates ?? [];
            const unique = result.Unique ?? [];

            if (duplicates.length > 0) {
                // Show dialog — it handles adding tracks and dispatching completion.
                this.loading = false;
                await this.updateComplete;
                this.duplicateDialog.show(playlistId, duplicates, unique);

                return;
            }

            // No duplicates — add all directly.
            await AddTracksToPlaylist(playlistId, this.filePaths);
            this.dispatchComplete();
        } catch (err) {
            console.error('Failed to add tracks to playlist:', err);
            // Transient: the picker closes either way, and the tracks
            // simply are not there — nothing to undo, only to retry
            // (errors.m7).
            notificationStore.transient({
                key: 'playlist-add',
                text: `Could not add ${this.filePaths.length === 1 ? 'that track' : `those ${this.filePaths.length} tracks`} to the playlist. ${describeError(err)}`,
                detail: String(err),
            });
        } finally {
            this.loading = false;
        }
    };

    private handleShowCreate = () => {
        this.mode = 'create';
        this.newPlaylistName = '';

        void this.updateComplete.then(() => {
            const input =
                this.shadowRoot?.querySelector<HTMLInputElement>(
                    '.create-form input',
                );

            input?.focus();
        });
    };

    private handleCancelCreate = () => {
        this.mode = 'list';
        this.newPlaylistName = '';
    };

    private handleCreatePlaylist = async () => {
        const name = this.newPlaylistName.trim();
        if (!name || this.loading) return;

        this.loading = true;

        try {
            await CreatePlaylistWithTracks(name, this.filePaths);
            this.dispatchComplete();
        } catch (err) {
            console.error('Failed to create playlist:', err);
            notificationStore.transient({
                key: 'playlist-create',
                text: `Could not create “${name}”. ${describeError(err)}`,
                detail: String(err),
            });
        } finally {
            this.loading = false;
        }
    };

    private handleInputChange = (e: Event) => {
        const input = e.target as HTMLInputElement;
        this.newPlaylistName = input.value;
    };

    private handleInputKeydown = (e: KeyboardEvent) => {
        if (e.key === 'Enter') {
            void this.handleCreatePlaylist();
        } else if (e.key === 'Escape') {
            this.handleCancelCreate();
        }

        // Stop propagation so parent context menu handlers don't interfere.
        e.stopPropagation();
    };

    private dispatchComplete() {
        this.dispatchEvent(
            new CustomEvent('playlist-action-complete', {
                bubbles: true,
                composed: true,
            }),
        );
    }

    /** Resets the picker to its initial list state. */
    reset() {
        this.mode = 'list';
        this.newPlaylistName = '';
        this.loading = false;
        this.loadPlaylists();
    }

    override render() {
        return html`
            ${this.mode === 'create'
                ? this.renderCreateForm()
                : this.renderPlaylistList()}
            <duplicate-tracks-dialog
                @playlist-action-complete=${this.dispatchComplete}
            ></duplicate-tracks-dialog>
        `;
    }

    private renderPlaylistList() {
        return html`
            <div class="picker-panel">
                ${this.playlists.length > 0
                    ? html`
                          ${this.playlists.map(
                              (p) => html`
                                  <wa-dropdown-item
                                      @click=${() =>
                                          this.handleSelectPlaylist(p.ID)}
                                  >
                                      ${p.Name}
                                  </wa-dropdown-item>
                              `,
                          )}
                          <div class="separator"></div>
                      `
                    : nothing}
                <wa-dropdown-item @click=${this.handleShowCreate}>
                    <wa-icon slot="icon" name="plus"></wa-icon>
                    New Playlist
                </wa-dropdown-item>
            </div>
        `;
    }

    private renderCreateForm() {
        const canCreate = this.newPlaylistName.trim().length > 0;

        return html`
            <div class="picker-panel">
                <div class="create-form">
                    <input
                        type="text"
                        placeholder="Playlist name"
                        .value=${this.newPlaylistName}
                        @input=${this.handleInputChange}
                        @keydown=${this.handleInputKeydown}
                        @click=${(e: Event) => e.stopPropagation()}
                    />
                    <div class="button-row">
                        <button @click=${this.handleCancelCreate}>
                            Cancel
                        </button>
                        <button
                            class="primary"
                            ?disabled=${!canCreate || this.loading}
                            @click=${this.handleCreatePlaylist}
                        >
                            Create
                        </button>
                    </div>
                </div>
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'playlist-picker': PlaylistPicker;
    }
}

import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { EventsOn } from '@runtime/runtime';

import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';

import {
    GetAllPlaylists,
    AddTracksToPlaylist,
    CreatePlaylistWithTracks,
} from '@go/playlist/Service';
import { Events } from '../../events';
import type { playlist } from '@go/models';

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

    @state() private mode: 'list' | 'create' = 'list';
    @state() private playlists: playlist.Summary[] = [];
    @state() private newPlaylistName = '';
    @state() private loading = false;

    static override styles = css`
        :host {
            display: block;
        }

        .picker-panel {
            background-color: #343a40;
            border: 1px solid #444;
            border-radius: 6px;
            padding: 4px 0;
            box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
            min-width: 180px;
            max-height: 300px;
            overflow-y: auto;
        }

        .picker-panel wa-dropdown-item {
            cursor: pointer;
            --wa-color-text-normal: #fff;
            font-size: 13px;
        }

        .picker-panel wa-dropdown-item:hover {
            background-color: rgba(255, 255, 255, 0.1);
        }

        .separator {
            height: 1px;
            background: #555;
            margin: 4px 0;
        }

        .create-form {
            padding: 8px 12px;
            display: flex;
            flex-direction: column;
            gap: 8px;
        }

        .create-form input {
            background: #2a2d30;
            border: 1px solid #555;
            border-radius: 4px;
            color: #fff;
            padding: 6px 8px;
            font-size: 13px;
            outline: none;
            font-family: inherit;
        }

        .create-form input:focus {
            border-color: #ffd43b;
        }

        .create-form input::placeholder {
            color: #888;
        }

        .button-row {
            display: flex;
            gap: 6px;
            justify-content: flex-end;
        }

        .button-row button {
            background: #495057;
            border: none;
            border-radius: 4px;
            color: #fff;
            padding: 4px 10px;
            font-size: 12px;
            cursor: pointer;
            font-family: inherit;
        }

        .button-row button:hover {
            background: #5a6268;
        }

        .button-row button.primary {
            background: #ffd43b;
            color: #000;
        }

        .button-row button.primary:hover {
            background: #ffe066;
        }

        .button-row button.primary:disabled {
            background: #665a1e;
            color: #888;
            cursor: not-allowed;
        }

        .empty-message {
            padding: 8px 12px;
            color: #888;
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
            this.playlists = await GetAllPlaylists();
        } catch (err) {
            console.error('Failed to load playlists:', err);
            this.playlists = [];
        }
    }

    private handleSelectPlaylist = async (playlistId: number) => {
        if (this.loading || this.filePaths.length === 0) return;

        this.loading = true;

        try {
            await AddTracksToPlaylist(playlistId, this.filePaths);
            this.dispatchComplete();
        } catch (err) {
            console.error('Failed to add tracks to playlist:', err);
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
        if (this.mode === 'create') {
            return this.renderCreateForm();
        }

        return this.renderPlaylistList();
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

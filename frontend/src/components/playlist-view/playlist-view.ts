import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';

import '@awesome.me/webawesome/dist/components/icon/icon.js';

import {
    GetAllPlaylists,
    CreatePlaylist,
} from '@go/playlist/Service';
import type { playlist } from '@go/models';

@customElement('playlist-view')
export class PlaylistView extends LitElement {
    @state() private playlists: playlist.Summary[] = [];
    @state() private loading = true;
    @state() private creating = false;
    @state() private newPlaylistName = '';

    static override styles = css`
        :host {
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }

        .header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 16px;
            flex-shrink: 0;
            border-bottom: 1px solid #333;
        }

        .header h2 {
            margin: 0;
            font-size: 18px;
            font-weight: 600;
            color: #fff;
        }

        .new-playlist-button {
            background: none;
            border: 1px solid #555;
            border-radius: 4px;
            color: #fff;
            padding: 6px 12px;
            font-size: 13px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 6px;
            font-family: inherit;
        }

        .new-playlist-button:hover {
            border-color: #ffd43b;
            color: #ffd43b;
        }

        .create-form {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 12px 16px;
            border-bottom: 1px solid #333;
            flex-shrink: 0;
        }

        .create-form input {
            flex: 1;
            background: #2a2d30;
            border: 1px solid #555;
            border-radius: 4px;
            color: #fff;
            padding: 6px 10px;
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

        .create-form button {
            background: #495057;
            border: none;
            border-radius: 4px;
            color: #fff;
            padding: 6px 12px;
            font-size: 13px;
            cursor: pointer;
            font-family: inherit;
        }

        .create-form button:hover {
            background: #5a6268;
        }

        .create-form button.primary {
            background: #ffd43b;
            color: #000;
        }

        .create-form button.primary:hover {
            background: #ffe066;
        }

        .create-form button.primary:disabled {
            background: #665a1e;
            color: #888;
            cursor: not-allowed;
        }

        .playlist-list {
            flex: 1;
            overflow-y: auto;
            padding: 0;
            margin: 0;
            list-style: none;
        }

        .playlist-item {
            display: flex;
            align-items: center;
            padding: 12px 16px;
            gap: 12px;
            border-bottom: 1px solid rgba(255, 255, 255, 0.05);
        }

        .playlist-item:hover {
            background-color: rgba(255, 255, 255, 0.05);
        }

        .playlist-icon {
            font-size: 18px;
            color: #888;
            flex-shrink: 0;
        }

        .playlist-name {
            font-size: 14px;
            color: #fff;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .loading {
            display: flex;
            justify-content: center;
            align-items: center;
            padding: 32px;
            color: #b3b3b3;
        }

        .empty-state {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            padding: 48px 20px;
            color: #b3b3b3;
            text-align: center;
            gap: 8px;
        }

        .empty-state wa-icon {
            font-size: 32px;
        }

        .empty-state p {
            margin: 4px 0;
        }
    `;

    override connectedCallback() {
        super.connectedCallback();
        this.loadPlaylists();
    }

    private async loadPlaylists() {
        try {
            this.loading = true;
            const result = await GetAllPlaylists();
            this.playlists = result ?? [];
        } catch (err) {
            console.error('Failed to load playlists:', err);
            this.playlists = [];
        } finally {
            this.loading = false;
        }
    }

    private handleNewPlaylistClick = () => {
        this.creating = true;
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
        this.creating = false;
        this.newPlaylistName = '';
    };

    private handleCreatePlaylist = async () => {
        const name = this.newPlaylistName.trim();
        if (!name) return;

        try {
            await CreatePlaylist(name);
            this.creating = false;
            this.newPlaylistName = '';
            await this.loadPlaylists();
        } catch (err) {
            console.error('Failed to create playlist:', err);
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
    };

    override render() {
        return html`
            <div class="header">
                <h2>Playlists</h2>
                <button
                    class="new-playlist-button"
                    @click=${this.handleNewPlaylistClick}
                >
                    <wa-icon name="plus"></wa-icon>
                    New Playlist
                </button>
            </div>

            ${this.creating ? this.renderCreateForm() : ''}
            ${this.loading
                ? html`<div class="loading">Loading playlists...</div>`
                : this.renderPlaylistList()}
        `;
    }

    private renderCreateForm() {
        const canCreate = this.newPlaylistName.trim().length > 0;

        return html`
            <div class="create-form">
                <input
                    type="text"
                    placeholder="Playlist name"
                    .value=${this.newPlaylistName}
                    @input=${this.handleInputChange}
                    @keydown=${this.handleInputKeydown}
                />
                <button @click=${this.handleCancelCreate}>Cancel</button>
                <button
                    class="primary"
                    ?disabled=${!canCreate}
                    @click=${this.handleCreatePlaylist}
                >
                    Create
                </button>
            </div>
        `;
    }

    private renderPlaylistList() {
        if (this.playlists.length === 0) {
            return html`
                <div class="empty-state">
                    <wa-icon name="list"></wa-icon>
                    <p>No playlists yet</p>
                    <p style="font-size: 12px;">
                        Create a playlist to get started.
                    </p>
                </div>
            `;
        }

        return html`
            <ul class="playlist-list">
                ${this.playlists.map(
                    (p) => html`
                        <li class="playlist-item">
                            <wa-icon
                                class="playlist-icon"
                                name="list"
                            ></wa-icon>
                            <span class="playlist-name">${p.Name}</span>
                        </li>
                    `,
                )}
            </ul>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'playlist-view': PlaylistView;
    }
}

import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';

import '@awesome.me/webawesome/dist/components/icon/icon.js';

import { CreatePlaylist } from '@go/playlist/Service';
import type { playlist } from '@go/models';
import { QueueController } from '@store/controllers/queue-controller';
import { PlaylistController } from '@store/controllers/playlist-controller';
import '@components/track-info/track-info';

const SCROLL_DEBOUNCE_MS = 100;

interface PlaylistEntry {
    summary: playlist.Summary;
    expanded: boolean;
    tracks: playlist.Track[];
}

@customElement('playlist-view')
export class PlaylistView extends LitElement {
    private queue = new QueueController(this);
    private playlistCtrl = new PlaylistController(this);
    private scrollDebounceTimer: ReturnType<typeof setTimeout> | null =
        null;

    @state() private entries: PlaylistEntry[] = [];
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
            border-bottom: 1px solid rgba(255, 255, 255, 0.05);
        }

        .playlist-header {
            display: flex;
            align-items: center;
            padding: 12px 16px;
            gap: 10px;
            cursor: pointer;
            user-select: none;
        }

        .playlist-header:hover {
            background-color: rgba(255, 255, 255, 0.05);
        }

        .chevron {
            font-size: 14px;
            color: #888;
            flex-shrink: 0;
            transition: transform 0.15s ease;
        }

        .chevron.expanded {
            transform: rotate(90deg);
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
            flex: 1;
        }

        .track-count {
            font-size: 11px;
            color: #666;
            flex-shrink: 0;
        }

        .playlist-body {
            padding: 0 16px 12px 42px;
        }

        .playlist-actions {
            display: flex;
            align-items: center;
            gap: 8px;
            padding-bottom: 8px;
        }

        .play-all-button {
            background: none;
            border: 1px solid #555;
            border-radius: 4px;
            color: #fff;
            padding: 4px 10px;
            font-size: 12px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 5px;
            font-family: inherit;
        }

        .play-all-button:hover {
            border-color: #ffd43b;
            color: #ffd43b;
        }

        .track-item {
            padding: 6px 0;
            border-bottom: 1px solid rgba(255, 255, 255, 0.03);
        }

        .track-item:last-child {
            border-bottom: none;
        }

        .tracks-empty {
            padding: 12px 0;
            color: #666;
            font-size: 12px;
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

    override disconnectedCallback() {
        super.disconnectedCallback();

        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
            this.scrollDebounceTimer = null;
        }
    }

    private get scrollContainer(): HTMLElement | null {
        return (
            this.shadowRoot?.querySelector(
                '.playlist-list',
            ) ?? null
        );
    }

    private restoreScrollPosition() {
        const saved =
            this.playlistCtrl.getScrollPosition();

        if (saved > 0 && this.scrollContainer) {
            this.scrollContainer.scrollTop = saved;
        }
    }

    private onScroll = () => {
        if (this.scrollDebounceTimer !== null) {
            clearTimeout(this.scrollDebounceTimer);
        }

        this.scrollDebounceTimer = setTimeout(() => {
            if (this.scrollContainer) {
                this.playlistCtrl.setScrollPosition(
                    this.scrollContainer.scrollTop,
                );
            }
        }, SCROLL_DEBOUNCE_MS);
    };

    private async loadPlaylists() {
        try {
            this.loading = true;

            const playlists =
                await this.playlistCtrl.getPlaylists();

            this.entries = playlists.map((p) => ({
                summary: p.Summary,
                expanded: false,
                tracks: p.Tracks ?? [],
            }));
        } catch (err) {
            console.error('Failed to load playlists:', err);
            this.entries = [];
        } finally {
            this.loading = false;
        }

        await this.updateComplete;
        this.restoreScrollPosition();
    }

    private handleToggle = (index: number) => {
        const entry = this.entries[index];

        if (!entry) return;

        this.entries = this.entries.map((e, i) =>
            i === index
                ? { ...e, expanded: !e.expanded }
                : e,
        );
    };

    private handlePlayAll = (index: number) => {
        const entry = this.entries[index];

        if (!entry || entry.tracks.length === 0) return;

        const filePaths = entry.tracks.map((t) => t.FilePath);
        this.queue.setQueue(filePaths, 0);
    };

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
            this.playlistCtrl.invalidate();
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

            ${this.creating ? this.renderCreateForm() : nothing}
            ${this.loading
                ? html`<div class="loading">
                      Loading playlists...
                  </div>`
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
                <button @click=${this.handleCancelCreate}>
                    Cancel
                </button>
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
        if (this.entries.length === 0) {
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
            <ul class="playlist-list" @scroll=${this.onScroll}>
                ${this.entries.map((entry, i) =>
                    this.renderPlaylistItem(entry, i),
                )}
            </ul>
        `;
    }

    private renderPlaylistItem(entry: PlaylistEntry, index: number) {
        const trackCount = entry.tracks.length;
        const countLabel =
            `${trackCount} track${trackCount !== 1 ? 's' : ''}`;

        return html`
            <li class="playlist-item">
                <div
                    class="playlist-header"
                    @click=${() => this.handleToggle(index)}
                >
                    <wa-icon
                        class="chevron ${entry.expanded
                            ? 'expanded'
                            : ''}"
                        name="chevron-right"
                    ></wa-icon>
                    <wa-icon
                        class="playlist-icon"
                        name="list"
                    ></wa-icon>
                    <span class="playlist-name">
                        ${entry.summary.Name}
                    </span>
                    <span class="track-count">
                        ${countLabel}
                    </span>
                </div>
                ${entry.expanded
                    ? this.renderPlaylistBody(entry, index)
                    : nothing}
            </li>
        `;
    }

    private renderPlaylistBody(
        entry: PlaylistEntry,
        index: number,
    ) {
        if (entry.tracks.length === 0) {
            return html`
                <div class="playlist-body">
                    <div class="tracks-empty">
                        This playlist is empty.
                    </div>
                </div>
            `;
        }

        return html`
            <div class="playlist-body">
                <div class="playlist-actions">
                    <button
                        class="play-all-button"
                        @click=${(e: Event) => {
                            e.stopPropagation();
                            this.handlePlayAll(index);
                        }}
                    >
                        <wa-icon name="play"></wa-icon>
                        Play All
                    </button>
                </div>
                ${entry.tracks.map(
                    (track) => html`
                        <div class="track-item">
                            <track-info
                                .trackTitle=${track.Title}
                                .artist=${track.Artist}
                                .duration=${track.Duration}
                                .filePath=${track.FilePath}
                            ></track-info>
                        </div>
                    `,
                )}
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'playlist-view': PlaylistView;
    }
}

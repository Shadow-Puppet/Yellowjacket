import { GetAllTracks } from '@go/library/Library';
import { library } from '@go/models';
import { LogPrint } from '@runtime/runtime';
import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import { formatMilliseconds } from '@utils/time';
import { PlayerController } from '@store/controllers/player-controller';
import { QueueController } from '@store/controllers/queue-controller';
import '@lit-labs/virtualizer';
import { flow } from '@lit-labs/virtualizer/layouts/flow.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';

@customElement('track-list')
export class TrackList extends LitElement {
  private player = new PlayerController(this);
  private queue = new QueueController(this);

  @state()
  private tracks: library.Track[] = [];

  @state()
  private contextMenuOpen = false;

  @state()
  private contextMenuTrack: library.Track | null = null;

  @state()
  private playlistSubmenuOpen = false;

  @query('#context-menu')
  private contextMenuPopup!: HTMLElement;

  @query('#playlist-submenu')
  private playlistSubmenuPopup!: HTMLElement;

  private closeHandler = () => this.closeContextMenu();

  static override styles = css`
    :host {
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }

    .header-row {
      display: grid;
      grid-template-columns: 1fr 1fr 80px;
      padding: 8px;
      font-weight: bold;
      color: #fff;
      border-bottom: 1px solid #666;
      flex-shrink: 0;
    }

    lit-virtualizer {
      flex: 1;
      overflow-y: auto;
    }

    .track-row {
      display: grid;
      grid-template-columns: 1fr 1fr 80px;
      padding: 8px;
      border-bottom: 1px solid #333;
      align-items: center;
      width: 100%;
    }

    .track-row > * {
      min-width: 0;
    }

    .track-row:hover {
      background-color: rgba(255, 255, 255, 0.05);
    }

    .track-row.active {
      background-color: rgba(255, 212, 59, 0.1);
    }

    .track-row.active .track-name-button {
      color: #ffd43b;
    }

    .track-name-button {
      background: none;
      border: none;
      color: inherit;
      text-align: left;
      padding: 0;
      cursor: pointer;
      width: 100%;
      font: inherit;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .track-name-button:hover {
      text-decoration: underline;
    }

    .artist-name,
    .track-length {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    #context-menu {
      z-index: 200;
    }

    .context-menu-panel {
      background-color: #343a40;
      border: 1px solid #444;
      border-radius: 6px;
      padding: 4px 0;
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
      min-width: 160px;
    }

    .context-menu-panel wa-dropdown-item {
      cursor: pointer;
    }

    .context-menu-panel wa-dropdown-item {
      --wa-color-text-normal: #fff;
      font-size: 13px;
    }

    .context-menu-panel wa-dropdown-item:hover {
      background-color: rgba(255, 255, 255, 0.1);
    }

    .submenu-item {
      position: relative;
    }

    .submenu-arrow {
      font-size: 10px;
      margin-left: auto;
      padding-left: 12px;
    }

    #playlist-submenu {
      z-index: 210;
    }
  `;

  override connectedCallback() {
    super.connectedCallback();
    this.loadTracks();
    document.addEventListener('click', this.closeHandler);
    document.addEventListener('contextmenu', this.closeHandler);
  }

  override disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('click', this.closeHandler);
    document.removeEventListener('contextmenu', this.closeHandler);
  }

  async loadTracks() {
    try {
      const tracks = await GetAllTracks();
      this.tracks = tracks;

      if (tracks[0]) {
        LogPrint(tracks[0].TrackName);
      }
    } catch (error) {
      console.error('Error loading tracks:', error);
    }
  }

  private onTrackClick(track: library.Track) {
    this.queue.setQueue([track.FilePath], 0);
  }

  private onTrackContextMenu(e: MouseEvent, track: library.Track) {
    e.preventDefault();
    e.stopPropagation();

    this.contextMenuTrack = track;
    this.contextMenuOpen = true;

    // Position the popup at the mouse cursor using a virtual anchor.
    this.updateComplete.then(() => {
      const popup = this.contextMenuPopup;

      if (popup) {
        (popup as any).anchor = {
          getBoundingClientRect() {
            return {
              width: 0,
              height: 0,
              x: e.clientX,
              y: e.clientY,
              top: e.clientY,
              left: e.clientX,
              right: e.clientX,
              bottom: e.clientY,
            };
          },
        };
        (popup as any).active = true;
      }
    });
  }

  private onContextMenuAction(action: string) {
    if (!this.contextMenuTrack) return;

    const filePath = this.contextMenuTrack.FilePath;

    switch (action) {
      case 'play':
        this.queue.setQueue([filePath], 0);
        break;
      case 'add-to-queue':
        this.queue.addToQueue(filePath);
        break;
      case 'play-next':
        this.queue.playNext(filePath);
        break;
    }

    this.closeContextMenu();
  }

  private closeContextMenu() {
    if (!this.contextMenuOpen) return;

    this.closePlaylistSubmenu();
    this.contextMenuOpen = false;
    this.contextMenuTrack = null;

    const popup = this.contextMenuPopup;

    if (popup) {
      (popup as any).active = false;
    }
  }

  private async showPlaylistSubmenu() {
    if (this.playlistSubmenuOpen) return;

    this.playlistSubmenuOpen = true;

    await this.updateComplete;

    const submenu = this.playlistSubmenuPopup;
    const trigger = this.shadowRoot?.querySelector('.submenu-item');

    if (submenu && trigger) {
      (submenu as any).anchor = trigger;
      (submenu as any).active = true;
    }

    const picker = this.shadowRoot?.querySelector(
      'playlist-picker',
    ) as PlaylistPicker | null;

    picker?.reset();
  }

  private closePlaylistSubmenu() {
    if (!this.playlistSubmenuOpen) return;

    this.playlistSubmenuOpen = false;

    const submenu = this.playlistSubmenuPopup;

    if (submenu) {
      (submenu as any).active = false;
    }
  }

  private onPlaylistActionComplete = () => {
    this.closeContextMenu();
  };

  private isActiveTrack(track: library.Track): boolean {
    const currentTrack = this.player.currentTrack;

    if (!currentTrack) return false;

    return currentTrack.filePath === track.FilePath;
  }

  private renderTrackRow = (track: library.Track): unknown => {
    const active = this.isActiveTrack(track);

    return html`
      <div
        class="track-row ${active ? 'active' : ''}"
        @contextmenu=${(e: MouseEvent) => this.onTrackContextMenu(e, track)}
      >
        <div>
          <button
            class="track-name-button"
            @click=${() => this.onTrackClick(track)}
          >
            ${track.TrackName}
          </button>
        </div>
        <div class="artist-name">${track.ArtistName}</div>
        <div class="track-length">${formatMilliseconds(track.TrackLength)}</div>
      </div>
    `;
  };

  override render() {
    return html`
      ${this.tracks.length === 0
        ? html`<p>Loading tracks...</p>`
        : html`
            <div class="header-row">
              <span>Track Name</span>
              <span>Artist</span>
              <span>Track Length</span>
            </div>
            <lit-virtualizer
              scroller
              .items=${this.tracks}
              .renderItem=${this.renderTrackRow}
              .layout=${flow()}
            ></lit-virtualizer>
          `}

      <wa-popup
        id="context-menu"
        placement="bottom-start"
        .active=${this.contextMenuOpen}
      >
        ${this.contextMenuOpen
          ? html`
              <div class="context-menu-panel">
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('play')}
                >
                  <wa-icon slot="icon" name="play"></wa-icon>
                  Play
                </wa-dropdown-item>
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('add-to-queue')}
                >
                  <wa-icon slot="icon" name="plus"></wa-icon>
                  Add to Queue
                </wa-dropdown-item>
                <wa-dropdown-item
                  @click=${() => this.onContextMenuAction('play-next')}
                >
                  <wa-icon slot="icon" name="forward-step"></wa-icon>
                  Play Next
                </wa-dropdown-item>
                <wa-dropdown-item
                  class="submenu-item"
                  @mouseenter=${() => this.showPlaylistSubmenu()}
                  @click=${(e: Event) => {
                    e.stopPropagation();
                    void this.showPlaylistSubmenu();
                  }}
                >
                  <wa-icon slot="icon" name="plus"></wa-icon>
                  Add to Playlist
                  <span class="submenu-arrow">&#9654;</span>
                </wa-dropdown-item>
              </div>
            `
          : nothing}
      </wa-popup>

      <wa-popup
        id="playlist-submenu"
        placement="right-start"
        .active=${this.playlistSubmenuOpen}
      >
        ${this.playlistSubmenuOpen && this.contextMenuTrack
          ? html`
              <playlist-picker
                .filePaths=${[this.contextMenuTrack.FilePath]}
                @playlist-action-complete=${this.onPlaylistActionComplete}
                @click=${(e: Event) => e.stopPropagation()}
              ></playlist-picker>
            `
          : nothing}
      </wa-popup>
    `;
  }
}

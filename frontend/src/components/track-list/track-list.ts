import { GetAllTracks } from '@go/library/Library';
import { library } from '@go/models';
import { LogPrint } from '@runtime/runtime';
import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import { formatMilliseconds } from '@utils/time';
import { PlayerController } from '@store/controllers/player-controller';
import { QueueController } from '@store/controllers/queue-controller';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

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

  @query('#context-menu')
  private contextMenuPopup!: HTMLElement;

  private closeHandler = () => this.closeContextMenu();

  static override styles = css`
    table {
      width: 100%;
      border-collapse: collapse;
    }

    th {
      padding: 8px;
      text-align: left;
      font-weight: bold;
      color: #fff;
    }

    thead tr {
      border-bottom: 1px solid #666;
    }

    tbody tr {
      border-bottom: 1px solid #333;
    }

    tbody tr:hover {
      background-color: rgba(255, 255, 255, 0.05);
    }

    tbody tr.active {
      background-color: rgba(255, 212, 59, 0.1);
    }

    tbody tr.active .track-name-button {
      color: #ffd43b;
    }

    td {
      padding: 8px;
    }

    .track-name-button {
      background: none;
      border: none;
      color: inherit;
      text-align: left;
      padding: 0;
      cursor: pointer;
      width: 100%;
    }

    .track-name-button:hover {
      text-decoration: underline;
    }

    #context-menu {
      z-index: 200;
    }

    .context-menu-panel {
      background-color: #2a2a3e;
      border: 1px solid #444;
      border-radius: 6px;
      padding: 4px 0;
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
      min-width: 160px;
    }

    .context-menu-panel wa-dropdown-item {
      cursor: pointer;
    }

    .context-menu-panel wa-dropdown-item::part(base) {
      color: #e0e0e0;
      font-size: 13px;
    }

    .context-menu-panel wa-dropdown-item::part(base):hover {
      background-color: rgba(255, 255, 255, 0.1);
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

    this.contextMenuOpen = false;
    this.contextMenuTrack = null;

    const popup = this.contextMenuPopup;

    if (popup) {
      (popup as any).active = false;
    }
  }

  private isActiveTrack(track: library.Track): boolean {
    const currentTrack = this.player.currentTrack;

    if (!currentTrack) return false;

    return currentTrack.filePath === track.FilePath;
  }

  override render() {
    return html`
      <div>
        ${this.tracks.length === 0
          ? html`<p>Loading tracks...</p>`
          : html`
              <table>
                <thead>
                  <tr>
                    <th>Track Name</th>
                    <th>Artist</th>
                    <th>Track Length</th>
                  </tr>
                </thead>
                <tbody>
                  ${this.tracks.map(
                    (track) => html`
                      <tr
                        class=${this.isActiveTrack(track) ? 'active' : ''}
                        @contextmenu=${(e: MouseEvent) =>
                          this.onTrackContextMenu(e, track)}
                      >
                        <td>
                          <button
                            class="track-name-button"
                            @click=${() => this.onTrackClick(track)}
                          >
                            ${track.TrackName}
                          </button>
                        </td>
                        <td>${track.ArtistName}</td>
                        <td>${formatMilliseconds(track.TrackLength)}</td>
                      </tr>
                    `
                  )}
                </tbody>
              </table>
            `}
      </div>

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
              </div>
            `
          : nothing}
      </wa-popup>
    `;
  }
}

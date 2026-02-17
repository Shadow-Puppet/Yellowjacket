import { LitElement, html, css, nothing, unsafeCSS } from 'lit';
import { customElement, property, state, query } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import { QueueController } from '@store/controllers/queue-controller';
import '@components/playlist-picker/playlist-picker.js';
import type { PlaylistPicker } from '@components/playlist-picker/playlist-picker.js';

const MIN_WIDTH = 200;
const MAX_WIDTH = 500;
const DEFAULT_WIDTH = 320;

@customElement('queue-panel')
export class QueuePanel extends LitElement {
  private queue = new QueueController(this);

  @property({ type: Boolean, reflect: true })
  open = false;

  @state()
  private isDragging = false;

  @state()
  private playlistPickerOpen = false;

  @query('#add-to-playlist-popup')
  private addToPlaylistPopup!: HTMLElement;

  private closePickerHandler = (e: MouseEvent) => {
    const path = e.composedPath();
    const popup = this.addToPlaylistPopup;
    const btn = this.shadowRoot?.querySelector('.add-to-playlist-button');

    if (popup && !path.includes(popup) && (!btn || !path.includes(btn))) {
      this.closePlaylistPicker();
    }
  };

  private panelWidth = DEFAULT_WIDTH;

  static override styles = css`
    :host {
      flex-shrink: 0;
      width: 0;
      overflow: hidden;
      background-color: #212529;
      display: flex;
      flex-direction: row;
    }

    :host([open]) {
      width: var(--queue-width, ${unsafeCSS(DEFAULT_WIDTH)}px);
      border-left: 1px solid #333;
    }

    .resize-handle {
      position: absolute;
      top: 0;
      left: 0;
      width: 4px;
      height: 100%;
      cursor: col-resize;
      background-color: transparent;
      transition: background-color 0.15s ease;
      z-index: 10;
    }

    .resize-handle:hover,
    .resize-handle.dragging {
      background-color: #6c757d;
    }

    .panel-content {
      position: relative;
      display: flex;
      flex-direction: column;
      min-width: ${unsafeCSS(MIN_WIDTH)}px;
      flex: 1;
    }

    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 12px 16px;
      border-bottom: 1px solid #333;
      flex-shrink: 0;
    }

    .header h3 {
      margin: 0;
      font-size: 14px;
      font-weight: 600;
    }

    .add-to-playlist-button {
      background: none;
      border: none;
      color: inherit;
      cursor: pointer;
      padding: 4px;
      display: flex;
      align-items: center;
    }

    .add-to-playlist-button:hover {
      color: #ffd43b;
    }

    .add-to-playlist-button:disabled {
      color: #555;
      cursor: not-allowed;
    }

    #add-to-playlist-popup {
      z-index: 210;
    }

    .track-list {
      flex: 1;
      overflow-y: auto;
      padding: 0;
      margin: 0;
      list-style: none;
    }

    .track-item {
      display: flex;
      align-items: center;
      padding: 8px 16px;
      gap: 12px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.05);
      cursor: pointer;
    }

    .track-item:hover {
      background-color: rgba(255, 255, 255, 0.05);
    }

    .track-item.active {
      background-color: rgba(255, 212, 59, 0.1);
    }

    .track-position {
      font-size: 12px;
      color: #888;
      min-width: 20px;
      text-align: right;
    }

    .track-item.active .track-position {
      color: #ffd43b;
    }

    .track-details {
      flex: 1;
      min-width: 0;
      display: flex;
      flex-direction: column;
      gap: 2px;
    }

    .track-title {
      font-size: 13px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .track-item.active .track-title {
      color: #ffd43b;
    }

    .track-artist {
      font-size: 11px;
      color: #b3b3b3;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .remove-button {
      background: none;
      border: none;
      color: #888;
      cursor: pointer;
      padding: 4px;
      display: flex;
      align-items: center;
      opacity: 0;
      transition: opacity 0.15s;
    }

    .track-item:hover .remove-button {
      opacity: 1;
    }

    .remove-button:hover {
      color: #ff6b6b;
    }

    .empty-state {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: 40px 20px;
      color: #b3b3b3;
      text-align: center;
      gap: 8px;
    }

    .empty-state wa-icon {
      font-size: 32px;
    }
  `;

  override connectedCallback() {
    super.connectedCallback();
    this.style.setProperty('--queue-width', `${this.panelWidth}px`);
    document.addEventListener('mousemove', this.handleMouseMove);
    document.addEventListener('mouseup', this.handleMouseUp);
    document.addEventListener('click', this.closePickerHandler);
  }

  override disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('mousemove', this.handleMouseMove);
    document.removeEventListener('mouseup', this.handleMouseUp);
    document.removeEventListener('click', this.closePickerHandler);
  }

  private async handleAddToPlaylist() {
    if (this.queue.tracks.length === 0) return;

    this.playlistPickerOpen = !this.playlistPickerOpen;

    await this.updateComplete;

    const popup = this.addToPlaylistPopup;
    const btn = this.shadowRoot?.querySelector('.add-to-playlist-button');

    if (popup && btn) {
      (popup as any).anchor = btn;
      (popup as any).active = this.playlistPickerOpen;
    }

    if (this.playlistPickerOpen) {
      const picker = this.shadowRoot?.querySelector(
        'playlist-picker',
      ) as PlaylistPicker | null;

      picker?.reset();
    }
  }

  private closePlaylistPicker() {
    if (!this.playlistPickerOpen) return;

    this.playlistPickerOpen = false;

    const popup = this.addToPlaylistPopup;

    if (popup) {
      (popup as any).active = false;
    }
  }

  private onPlaylistActionComplete = () => {
    this.closePlaylistPicker();
  };

  private handleRemoveTrack(e: Event, position: number) {
    e.stopPropagation();
    this.queue.removeFromQueue(position);
  }

  private handleTrackClick(index: number) {
    this.queue.playAtIndex(index);
  }

  private getDisplayTitle(
    track: { title: string; filePath: string },
  ): string {
    if (track.title) return track.title;

    // Fall back to filename without extension.
    const parts = track.filePath.split(/[\\/]/);
    const filename = parts[parts.length - 1] ?? track.filePath;

    return filename.replace(/\.[^.]+$/, '');
  }

  private handleMouseDown = (e: MouseEvent) => {
    e.preventDefault();
    this.isDragging = true;
  };

  private handleMouseMove = (e: MouseEvent) => {
    if (!this.isDragging) return;

    const rect = this.getBoundingClientRect();
    const newWidth = rect.right - e.clientX;
    const clampedWidth = Math.min(
      Math.max(newWidth, MIN_WIDTH),
      MAX_WIDTH,
    );

    this.panelWidth = clampedWidth;
    this.style.setProperty('--queue-width', `${clampedWidth}px`);
  };

  private handleMouseUp = () => {
    if (!this.isDragging) return;

    this.isDragging = false;
  };

  override render() {
    const tracks = this.queue.tracks;
    const currentIndex = this.queue.currentIndex;

    return html`
      <div class="panel-content">
        <div
          class="resize-handle ${this.isDragging ? 'dragging' : ''}"
          @mousedown=${this.handleMouseDown}
        ></div>
        <div class="header">
          <h3>Queue</h3>
          <button
            class="add-to-playlist-button"
            @click=${this.handleAddToPlaylist}
            ?disabled=${tracks.length === 0}
            title="Add queue to playlist"
          >
            <wa-icon name="plus"></wa-icon>
          </button>
        </div>

        <wa-popup
          id="add-to-playlist-popup"
          placement="bottom-end"
          .active=${this.playlistPickerOpen}
        >
          ${this.playlistPickerOpen
            ? html`
                <playlist-picker
                  .filePaths=${tracks.map((t) => t.filePath)}
                  @playlist-action-complete=${this.onPlaylistActionComplete}
                  @click=${(e: Event) => e.stopPropagation()}
                ></playlist-picker>
              `
            : nothing}
        </wa-popup>

        ${tracks.length === 0
          ? html`
              <div class="empty-state">
                <wa-icon name="list"></wa-icon>
                <p>Queue is empty</p>
                <p style="font-size: 12px;">
                  Click a track to start playing
                </p>
              </div>
            `
          : html`
              <ul class="track-list">
                ${tracks.map(
                  (track, index) => html`
                    <li
                      class="track-item ${index === currentIndex
                        ? 'active'
                        : ''}"
                      @click=${() => this.handleTrackClick(index)}
                    >
                      <span class="track-position">${index + 1}</span>
                      <div class="track-details">
                        <span class="track-title">
                          ${this.getDisplayTitle(track)}
                        </span>
                        <span class="track-artist">
                          ${track.artist || 'Unknown Artist'}
                        </span>
                      </div>
                      <button
                        class="remove-button"
                        @click=${(e: Event) =>
                          this.handleRemoveTrack(e, index)}
                        title="Remove from queue"
                      >
                        <wa-icon name="xmark"></wa-icon>
                      </button>
                    </li>
                  `,
                )}
              </ul>
            `}
      </div>
    `;
  }
}

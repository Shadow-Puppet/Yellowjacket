import { LitElement, html, css } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { QueueController } from '@store/controllers/queue-controller';

@customElement('queue-panel')
export class QueuePanel extends LitElement {
  private queue = new QueueController(this);

  @property({ type: Boolean, reflect: true })
  open = false;

  static override styles = css`
    :host {
      display: block;
      position: fixed;
      top: 4em; /* below header */
      right: 0;
      bottom: 4em; /* above footer */
      width: 320px;
      background-color: #1a1a2e;
      border-left: 1px solid #333;
      transform: translateX(100%);
      transition: transform 0.25s ease-in-out;
      z-index: 100;
      overflow: hidden;
      display: flex;
      flex-direction: column;
    }

    :host([open]) {
      transform: translateX(0);
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

    .close-button {
      background: none;
      border: none;
      color: inherit;
      cursor: pointer;
      padding: 4px;
      display: flex;
      align-items: center;
    }

    .close-button:hover {
      color: #ffd43b;
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
      color: #666;
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
      color: #888;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .remove-button {
      background: none;
      border: none;
      color: #666;
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
      color: #666;
      text-align: center;
      gap: 8px;
    }

    .empty-state wa-icon {
      font-size: 32px;
    }
  `;

  private handleClose() {
    this.open = false;
    this.dispatchEvent(new CustomEvent('queue-panel-close', { bubbles: true, composed: true }));
  }

  private handleRemoveTrack(e: Event, position: number) {
    e.stopPropagation();
    this.queue.removeFromQueue(position);
  }

  private handleTrackClick(index: number) {
    this.queue.playAtIndex(index);
  }

  private getDisplayTitle(track: { title: string; filePath: string }): string {
    if (track.title) return track.title;

    // Fall back to filename without extension.
    const parts = track.filePath.split(/[\\/]/);
    const filename = parts[parts.length - 1] ?? track.filePath;

    return filename.replace(/\.[^.]+$/, '');
  }

  override render() {
    const tracks = this.queue.tracks;
    const currentIndex = this.queue.currentIndex;

    return html`
      <div class="header">
        <h3>Queue</h3>
        <button class="close-button" @click=${this.handleClose}>
          <wa-icon name="xmark"></wa-icon>
        </button>
      </div>

      ${tracks.length === 0
        ? html`
            <div class="empty-state">
              <wa-icon name="list"></wa-icon>
              <p>Queue is empty</p>
              <p style="font-size: 12px;">Click a track to start playing</p>
            </div>
          `
        : html`
            <ul class="track-list">
              ${tracks.map(
                (track, index) => html`
                  <li class="track-item ${index === currentIndex ? 'active' : ''}"
                      @click=${() => this.handleTrackClick(index)}>
                    <span class="track-position">${index + 1}</span>
                    <div class="track-details">
                      <span class="track-title">${this.getDisplayTitle(track)}</span>
                      <span class="track-artist">${track.artist || 'Unknown Artist'}</span>
                    </div>
                    <button
                      class="remove-button"
                      @click=${(e: Event) => this.handleRemoveTrack(e, index)}
                      title="Remove from queue"
                    >
                      <wa-icon name="xmark"></wa-icon>
                    </button>
                  </li>
                `
              )}
            </ul>
          `}
    `;
  }
}

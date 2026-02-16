import { LitElement, html, css, unsafeCSS } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { QueueController } from '@store/controllers/queue-controller';

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

  private panelWidth = DEFAULT_WIDTH;

  static override styles = css`
    :host {
      flex-shrink: 0;
      width: 0;
      overflow: hidden;
      background-color: #1a1a2e;
      transition: width 0.25s ease-in-out;
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

  override connectedCallback() {
    super.connectedCallback();
    this.style.setProperty('--queue-width', `${this.panelWidth}px`);
    document.addEventListener('mousemove', this.handleMouseMove);
    document.addEventListener('mouseup', this.handleMouseUp);
  }

  override disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('mousemove', this.handleMouseMove);
    document.removeEventListener('mouseup', this.handleMouseUp);
  }

  private handleClose() {
    this.open = false;
    this.dispatchEvent(
      new CustomEvent('queue-panel-close', {
        bubbles: true,
        composed: true,
      }),
    );
  }

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

    // Disable transition during drag for instant feedback.
    this.style.transition = 'none';
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

    // Re-enable transition after drag ends.
    this.style.removeProperty('transition');
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
          <button class="close-button" @click=${this.handleClose}>
            <wa-icon name="xmark"></wa-icon>
          </button>
        </div>

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

import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import { PlayerController } from '@store/controllers/player-controller';

const MIN_WIDTH = 120;
const MAX_WIDTH = 350;
const DEFAULT_WIDTH = 200;

@customElement('now-playing')
export class NowPlaying extends LitElement {
    private player = new PlayerController(this);

    @state()
    private isDragging = false;

    @state()
    private showCoverPreview = false;

    static override styles = css`
    :host {
      display: block;
      position: relative;
      height: 100%;
      overflow: hidden;
    }

    .now-playing {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 8px;
      height: 100%;
      box-sizing: border-box;
    }

    .cover-art {
      width: 48px;
      height: 48px;
      flex-shrink: 0;
      border-radius: 4px;
      overflow: hidden;
    }

    .cover-art img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }

    .cover-placeholder {
      width: 100%;
      height: 100%;
      background-color: var(--yj-bg-base, #000);
      display: flex;
      align-items: center;
      justify-content: center;
    }

    .cover-placeholder wa-icon {
      color: var(--yj-text-primary, #fff);
      font-size: 24px;
    }

    .cover-art-wrapper {
      position: relative;
    }

    .cover-preview-panel {
      width: 500px;
      height: 500px;
      border-radius: 8px;
      overflow: hidden;
      box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
      pointer-events: none;
    }

    .cover-preview-panel img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }

    .track-info {
      display: flex;
      flex-direction: column;
      gap: 2px;
      min-width: 0;
    }

    .track-title {
      font-size: 14px;
      font-weight: 500;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .track-artist {
      font-size: 12px;
      color: var(--yj-text-tertiary, #666);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .resize-handle {
      position: absolute;
      top: 0;
      right: 0;
      width: 4px;
      height: 100%;
      cursor: col-resize;
      background-color: transparent;
      transition: background-color 0.15s ease;
      z-index: 10;
    }

    .resize-handle:hover,
    .resize-handle.dragging {
      background-color: var(--yj-text-tertiary, #6c757d);
    }
  `;

    override connectedCallback() {
        super.connectedCallback();
        this.updateWidth(DEFAULT_WIDTH);
        document.addEventListener('mousemove', this.handleMouseMove);
        document.addEventListener('mouseup', this.handleMouseUp);
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        document.removeEventListener('mousemove', this.handleMouseMove);
        document.removeEventListener('mouseup', this.handleMouseUp);
    }

    override render() {
        const track = this.player.currentTrack;

        if (!track) {
            return html`
        <div class="now-playing">
          <div class="cover-art">
            <div class="cover-placeholder"><wa-icon name="music"></wa-icon></div>
          </div>
        </div>
        <div
          class="resize-handle ${this.isDragging ? 'dragging' : ''}"
          @mousedown=${this.handleMouseDown}
        ></div>
      `;
        }

        return html`
      <div class="now-playing">
        <div class="cover-art-wrapper">
          <div
            class="cover-art"
            @mouseenter=${this.handleCoverMouseEnter}
            @mouseleave=${this.handleCoverMouseLeave}
          >
            ${track.coverArt
              ? html`<img
                  src="${track.coverArtSmall || track.coverArt}"
                  alt="Album cover"
                  @error=${(e: Event) => {
                      const img = e.target as HTMLImageElement;
                      if (
                          track.coverArt &&
                          img.src !== track.coverArt
                      ) {
                          img.src = track.coverArt;
                      }
                  }}
                />`
              : html`<div class="cover-placeholder">
                  <wa-icon name="music"></wa-icon>
                </div>`}
          </div>
          <wa-popup
            id="cover-preview"
            placement="top-start"
            flip
            shift
            .active=${this.showCoverPreview}
          >
            ${this.showCoverPreview && track.coverArt
              ? html`
                  <div class="cover-preview-panel">
                    <img
                      src="${track.coverArt}"
                      alt="Album cover full size"
                    />
                  </div>
                `
              : nothing}
          </wa-popup>
        </div>
        <div class="track-info">
          <span class="track-title">${track.title}</span>
          <span class="track-artist">
            ${track.artist || 'Unknown Artist'}
          </span>
        </div>
      </div>
      <div
        class="resize-handle ${this.isDragging ? 'dragging' : ''}"
        @mousedown=${this.handleMouseDown}
      ></div>
    `;
    }

    private handleCoverMouseEnter = () => {
        const track = this.player.currentTrack;

        if (!track?.coverArt) return;

        this.showCoverPreview = true;

        this.updateComplete.then(() => {
            const popup = this.shadowRoot?.querySelector(
                '#cover-preview',
            );
            const anchor = this.shadowRoot?.querySelector(
                '.cover-art',
            );

            if (popup && anchor) {
                (popup as any).anchor = anchor;
            }
        });
    };

    private handleCoverMouseLeave = () => {
        this.showCoverPreview = false;
    };

    private handleMouseDown = (e: MouseEvent) => {
        e.preventDefault();
        this.isDragging = true;
    };

    private handleMouseMove = (e: MouseEvent) => {
        if (!this.isDragging) return;

        const rect = this.getBoundingClientRect();
        const newWidth = e.clientX - rect.left;
        const clampedWidth = Math.min(Math.max(newWidth, MIN_WIDTH), MAX_WIDTH);

        this.updateWidth(clampedWidth);
    };

    private handleMouseUp = () => {
        this.isDragging = false;
    };

    private updateWidth(width: number) {
        const bottomBar = this.closest('.bottom-bar');

        if (bottomBar) {
            (bottomBar as HTMLElement).style.setProperty(
                '--now-playing-width',
                `${width}px`,
            );
        }
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'now-playing': NowPlaying;
    }
}

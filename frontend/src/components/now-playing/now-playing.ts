import { LitElement, html, css } from 'lit';
import { customElement } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { PlayerController } from '@store/controllers/player-controller';

@customElement('now-playing')
export class NowPlaying extends LitElement {
    private player = new PlayerController(this);

    static override styles = css`
    .now-playing {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 8px;
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
      background-color: #000;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    .cover-placeholder wa-icon {
      color: #fff;
      font-size: 24px;
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
      color: #666;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

  `;

    override render() {
        const track = this.player.currentTrack;

        if (!track) {
            return html`
      <div class="now-playing">
        <div class="cover-art">
          <div class="cover-placeholder"><wa-icon name="music"></wa-icon></div>
        </div>
      </div>
    `;
        }

        return html`
      <div class="now-playing">
        <div class="cover-art">
          ${track.coverArt
            ? html`<img src="${track.coverArt}" alt="Album cover" />`
            : html`<div class="cover-placeholder"><wa-icon name="music"></wa-icon></div>`}
        </div>
        <div class="track-info">
          <span class="track-title">${track.title}</span>
          <span class="track-artist">${track.artist || 'Unknown Artist'}</span>
        </div>
      </div>
    `;
    }
}

import { LitElement, html, css } from 'lit';
import { customElement } from 'lit/decorators.js';
import './controls/player-controls';
import './seekbar/seek-bar';
import './volume-control/volume-control';

@customElement('audio-player')
export class AudioPlayer extends LitElement {
  static override styles = css`
    .audio-player-container {
      display: flex;
      align-items: center;
      gap: 0.5em;
    }

    .player-main {
      flex: 1;
    }
  `;

  override render() {
    return html`
      <div class="audio-player-container">
        <div class="player-main">
          <player-controls></player-controls>
          <div>
            <seek-bar></seek-bar>
          </div>
        </div>
        <volume-control></volume-control>
      </div>
    `;
  }
}

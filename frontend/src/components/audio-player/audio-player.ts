import { LitElement, html, css } from 'lit';
import { customElement } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import './controls/player-controls';
import './seekbar/seek-bar';
import './volume-control/volume-control';
import '../notifications/inline-notice';
import { PlayerRegion } from '@store/player-store';
import { designTokens } from '../../styles/tokens.css';

/**
 * The bottom bar. The transport lives here, and so does the one place
 * the player admits it could not do what it was told — an
 * `<inline-notice>` for the `player` region, floated above the bar
 * because `.bottom-bar` is a fixed 4em grid row with no room in it.
 */
@customElement('audio-player')
export class AudioPlayer extends LitElement {
  static override styles = [designTokens, css`
    :host {
      display: block;
      position: relative;
    }

    .audio-player-container {
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .player-main {
      flex: 1;
    }

  `];

  override render() {
    return html`
      <inline-notice
        region=${PlayerRegion}
        testid="player-message"
        floating
      ></inline-notice>
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

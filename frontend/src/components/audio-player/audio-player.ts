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

    /* The phone transport (plan 016 B2): the buttons, and nothing
       else.  A media query inside a shadow root is answered by the
       viewport, not by the host, so this is the component saying what
       it drops at phone width rather than the shell reaching in.

       Volume goes because the hardware keys own it on a phone --
       Android routes them to the media stream, which is also why
       mediacontrols' Android handler implements no volume callback.
       The seek bar goes because a 4px-tall target dragged with a thumb
       is not a seek control; seeking belongs to the full-screen
       now-playing view, which is the next phase. */
    @media (max-width: 599px) {
      volume-control,
      seek-bar {
        display: none;
      }
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

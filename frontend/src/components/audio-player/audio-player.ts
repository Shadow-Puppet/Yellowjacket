import { LitElement, html } from 'lit';
import { customElement } from 'lit/decorators.js';
import './controls/player-controls';
import './seekbar/seek-bar';

const audioPlayer = () => html`
<div>
  <player-controls></player-controls>
  <seek-bar></seek-bar>
</div>
`;

@customElement('audio-player')
export class AudioPlayer extends LitElement {

  override render() {
    return audioPlayer();
  }

}

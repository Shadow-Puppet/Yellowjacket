import { LitElement, html } from 'lit';
import { customElement } from 'lit/decorators.js';
import './controls/player-controls';
import './seekbar/seek-bar';
import '@go/player/Player';
import '@shoelace-style/shoelace/dist/components/icon/icon.js';


const audioPlayer = () => html`
<div>
  <player-controls></player-controls>
  <div style="width: 50%">
    <seek-bar></seek-bar>
  </div>
</div>
`;

@customElement('audio-player')
export class AudioPlayer extends LitElement {

  override render() {
    return audioPlayer();
  }

}

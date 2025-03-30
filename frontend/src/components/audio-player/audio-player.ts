import { LitElement, html } from 'lit';
import { customElement } from 'lit/decorators.js';
import { Play } from '@go/player/Player';
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

  render() {
    return audioPlayer();
  }

}

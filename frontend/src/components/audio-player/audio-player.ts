import { LitElement, html } from 'lit';
import { customElement } from 'lit/decorators.js';
import { Play } from '@go/player/Player';

const audioPlayer = (text: string, disabled: boolean) => html`
    <p>Welcome to the Lit tutorial!</p>
    <h
`;


@customElement('audio-player')
export class AudioPlayer extends LitElement {

  render() {
    return audioPlayer;
  }

  onPlayPauseButtonClick = function() {
    let playPauseButtonIcon = document.getElementById("playPauseButtonIcon");
    try {
      Play().then((result) => {
        console.log(result)
        playPauseButtonIcon.setAttribute("src", "/src/assets/images/icons/music/pause-solid.svg")
      }).catch((err) => {
        console.log("There is an error with playing")
        console.error(err);
      });
    }
    catch (err) {
      console.error(err);
    }
  }
}

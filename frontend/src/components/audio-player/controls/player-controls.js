import { LitElement, html } from 'lit';
import { Play } from '/wailsjs/go/player/Player';

export class Player extends LitElement {
  static properties = {
    version: {},
  };

  constructor() {
    super();
    this.version = 'STARTING';
  }

  render() {
    return html`
    <p>Welcome to the Lit tutorial!</p>
    <p>This is the ${this.version} code.</p>
    `;
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
customElements.define('player-controls', Player);



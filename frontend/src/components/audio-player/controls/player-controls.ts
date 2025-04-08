import { LitElement, html } from 'lit';
import { customElement } from 'lit/decorators.js';
import { Play } from '@go/player/Player';

@customElement('player-controls')
export class PlayerControls extends LitElement {

  override render() {
    return html`
<div>
  <button>
    <img src="/src/assets/images/icons/music/shuffle.svg"></img>
  </button>
  <button>
    <img src="/src/assets/images/icons/music/skip-prev-solid.svg"></img>
  </button>
  <button @click="${this.onPlayPauseClick}">
    <img src="/src/assets/images/icons/music/play-solid.svg"></img>
  </button>
  <button>
    <img src="/src/assets/images/icons/music/skip-next-solid.svg"></img>
  </button>
  <button>
    <img src="/src/assets/images/icons/music/repeat.svg"></img>
  </button>
</div>
`;
  }

  onPlayPauseClick(event: { target: HTMLButtonElement; }) {
    try {
      Play().then((result) => {
        console.log(result)
        event.target.setAttribute("src", "/src/assets/images/icons/music/pause-solid.svg")
      }).catch((err) => {
        console.error("there is an error with playing " + err);
      });
    }
    catch (err) {
      console.error(err);
    }
  }
}

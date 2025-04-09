import { LitElement, html } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import { Play, Pause } from '@go/player/Player';

@customElement('player-controls')
export class PlayerControls extends LitElement {

  @property({ type: Boolean })
  isPlaying = false

  override render() {
    var imagePath = this.isPlaying ? 
      "/src/assets/images/icons/music/pause-solid.svg" : "/src/assets/images/icons/music/play-solid.svg"
    return html`
    <div>
      <button>
        <img src="/src/assets/images/icons/music/shuffle.svg"></img>
      </button>
      <button>
        <img src="/src/assets/images/icons/music/skip-prev-solid.svg"></img>
      </button>
      <button @click="${this.onPlayPauseClick}">
        <img src="${imagePath}"></img>
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

  onPlayPauseClick() {
    if (this.isPlaying) {
      Play().then(() => {
      }).catch((err) => {
        console.error("there is an error with playing " + err);
      });
    } else {
      Pause().then(() => {
      }).catch((err) => {
        console.error("there is an error with pausing " + err);
      });

    }
    this.isPlaying = !this.isPlaying
  }
}

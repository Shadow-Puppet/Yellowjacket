import { LitElement, html } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import { Play, Pause } from '@go/player/Player';

// assets
import pauseSVG from "@assets/images/icons/music/pause-solid.svg"
import playSVG from "@assets/images/icons/music/play-solid.svg"
import shuffleSVG from "@assets/images/icons/music/shuffle.svg"
import skipPrevSVG from "@assets/images/icons/music/skip-prev-solid.svg"
import skipNextSVG from "@assets/images/icons/music/skip-next-solid.svg"
import repeatSVG from "@assets/images/icons/music/repeat.svg"

@customElement('player-controls')
export class PlayerControls extends LitElement {

  @property({ type: Boolean })
  isPlaying = false

  override render() {
    var imagePath = this.isPlaying ? pauseSVG : playSVG
    return html`
    <div>
      <button>
        <img src="${shuffleSVG}"></img>
      </button>
      <button>
        <img src="${skipPrevSVG}"></img>
      </button>
      <button @click="${this.onPlayPauseClick}">
        <img src="${imagePath}"></img>
      </button>
      <button>
        <img src="${skipNextSVG}"></img>
      </button>
      <button>
        <img src="${repeatSVG}"></img>
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

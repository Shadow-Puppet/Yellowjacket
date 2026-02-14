import { LitElement, html, css } from 'lit';
import { customElement } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { PlayerController } from '@store/controllers/player-controller';
import { QueueController } from '@store/controllers/queue-controller';

@customElement('player-controls')
export class PlayerControls extends LitElement {
  private player = new PlayerController(this);
  private queue = new QueueController(this);

  static override styles = css`
    #player-control-buttons {
      display: flex;
      justify-content: center;
      align-items: center;
      gap: 4px;
    }

    button {
      background: none;
      border: none;
      color: inherit;
      cursor: pointer;
      padding: 4px 8px;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    button:hover {
      color: #ffd43b;
    }

    .active {
      color: #ffd43b;
    }

    .repeat-one {
      position: relative;
    }

    .repeat-one::after {
      content: '1';
      font-size: 8px;
      font-weight: bold;
      position: absolute;
      bottom: 2px;
      right: 2px;
    }
  `;

  private handlePlayClick = () => {
    this.player.play();
  };

  private handlePauseClick = () => {
    this.player.pause();
  };

  private handleNextClick = () => {
    this.queue.next();
  };

  private handlePreviousClick = () => {
    this.queue.previous();
  };

  private handleShuffleClick = () => {
    this.queue.toggleShuffle();
  };

  private handleRepeatClick = () => {
    this.queue.cycleRepeat();
  };

  override render() {
    const playOrPauseIcon = this.player.isPlaying ? 'pause' : 'play';
    const playOrPauseHandler = this.player.isPlaying
      ? this.handlePauseClick
      : this.handlePlayClick;

    const shuffleClass = this.queue.shuffleMode ? 'active' : '';
    const repeatMode = this.queue.repeatMode;
    const repeatClasses = [
      repeatMode !== 'off' ? 'active' : '',
      repeatMode === 'one' ? 'repeat-one' : '',
    ].filter(Boolean).join(' ');

    return html`
      <div id="player-control-buttons">
        <button class=${shuffleClass} @click=${this.handleShuffleClick}>
          <wa-icon name="shuffle"></wa-icon>
        </button>
        <button @click=${this.handlePreviousClick}>
          <wa-icon name="backward-step"></wa-icon>
        </button>
        <button @click="${playOrPauseHandler}">
          <wa-icon name=${playOrPauseIcon}></wa-icon>
        </button>
        <button @click=${this.handleNextClick}>
          <wa-icon name="forward-step"></wa-icon>
        </button>
        <button class=${repeatClasses} @click=${this.handleRepeatClick}>
          <wa-icon name="repeat"></wa-icon>
        </button>
      </div>
    `;
  }
}

import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { PlayerController } from '@store/controllers/player-controller';
import { queueStore } from '@store/queue-store';
import type { RepeatMode } from '@store/queue-store';
import { designTokens } from '../../../styles/tokens.css';

@customElement('player-controls')
export class PlayerControls extends LitElement {
  private player = new PlayerController(this);
  private unsubscribeQueue?: () => void;

  @state() private shuffleMode = false;
  @state() private repeatMode: RepeatMode = 'off';

  override connectedCallback(): void {
    super.connectedCallback();

    const s = queueStore.getState();
    this.shuffleMode = s.shuffleMode;
    this.repeatMode = s.repeatMode;

    this.unsubscribeQueue = queueStore.subscribe(() => {
      const qs = queueStore.getState();

      if (
        qs.shuffleMode !== this.shuffleMode ||
        qs.repeatMode !== this.repeatMode
      ) {
        this.shuffleMode = qs.shuffleMode;
        this.repeatMode = qs.repeatMode;
      }
    });
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.unsubscribeQueue?.();
  }

  static override styles = [designTokens, css`
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
      color: var(--yj-accent, #ffd43b);
    }

    .active {
      color: var(--yj-accent, #ffd43b);
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
  `];

  private handlePlayClick = () => {
    queueStore.play();
  };

  private handlePauseClick = () => {
    this.player.pause();
  };

  private handleNextClick = () => {
    queueStore.next();
  };

  private handlePreviousClick = () => {
    queueStore.previous();
  };

  private handleShuffleClick = () => {
    queueStore.toggleShuffle();
  };

  private handleRepeatClick = () => {
    queueStore.cycleRepeat();
  };

  override render() {
    const playOrPauseIcon = this.player.isPlaying ? 'pause' : 'play';
    const playOrPauseHandler = this.player.isPlaying
      ? this.handlePauseClick
      : this.handlePlayClick;

    const shuffleClass = this.shuffleMode ? 'active' : '';
    const repeatMode = this.repeatMode;
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

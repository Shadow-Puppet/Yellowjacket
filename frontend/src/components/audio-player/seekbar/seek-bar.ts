import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { ref, createRef } from 'lit/directives/ref.js';
import WaSlider from '@awesome.me/webawesome/dist/components/slider/slider.js';
import { formatSeconds } from '@utils/time';
import { PlayerController } from '@store/controllers/player-controller';

const ProgressIntervalMillis = 1000;

@customElement('seek-bar')
export class SeekBar extends LitElement {
  private player = new PlayerController(this);
  private rangeRef = createRef<WaSlider>();
  private timerID: number = -1;
  private previousTrackChangeId: number = -1;

  @state()
  private seekValue: number = 0;

  static override styles = css`
    wa-slider {
      --track-size: 6px;
      flex: 1;
      margin: 0 1em;
      --wa-tooltip-background-color: var(--yj-bg-elevated, #343a40);
      --wa-tooltip-content-color: var(--yj-text-primary, white);
      --wa-tooltip-border-color: var(--yj-bg-elevated, #343a40);
      --wa-tooltip-border-radius: 4px;
      --wa-tooltip-font-size: 0.875em;
    }

    wa-slider::part(track) {
      background: var(--yj-text-primary, white);
    }

    wa-slider::part(indicator) {
      background: var(--yj-accent, yellow);
    }

    wa-slider::part(thumb) {
      background: var(--yj-bg-base, black);
    }

    #seek-bar-container {
      display: flex;
      justify-content: space-between;
      align-items: center;
    }
  `;

  // ===================================================================
  // DERIVED STATE
  // ===================================================================

  private get hasTrack(): boolean {
    return this.player.currentTrack !== null;
  }

  private get trackLength(): number {
    return this.player.currentTrack?.trackLength ?? 0;
  }

  private get isPlaying(): boolean {
    return this.player.isPlaying;
  }

  // ===================================================================
  // LIFECYCLE
  // ===================================================================

  override disconnectedCallback() {
    super.disconnectedCallback();
    this.stopProgress();
  }

  override updated() {
    // Detect track change and reset seek position.
    // Uses trackChangeId instead of filePath so the seek bar resets
    // even when the same file plays consecutively in the queue.
    const currentChangeId = this.player.currentTrack?.trackChangeId ?? -1;

    if (currentChangeId !== this.previousTrackChangeId) {
      this.previousTrackChangeId = currentChangeId;
      this.seekValue = this.player.currentTrack?.seekPosition ?? 0;
      this.stopProgress();
    }

    // Start/stop progress interval based on playback state
    if (this.isPlaying && this.hasTrack) {
      this.startProgress();
    } else {
      this.stopProgress();
    }
  }

  // ===================================================================
  // PROGRESS INTERVAL
  // ===================================================================

  private stopProgress() {
    if (this.timerID !== -1) {
      clearInterval(this.timerID);
      this.timerID = -1;
    }
  }

  private startProgress() {
    // Don't start multiple intervals
    if (this.timerID !== -1) {
      return;
    }

    this.timerID = window.setInterval(() => {
      if (this.seekValue < this.trackLength) {
        this.seekValue += 1;
      }
    }, ProgressIntervalMillis);
  }

  // ===================================================================
  // EVENT HANDLERS
  // ===================================================================

  private handleChange(e: Event) {
    const newSeekVal = (e.target as WaSlider).value;
    this.setSeekValue(newSeekVal);
    this.player.seek(newSeekVal);

    if (this.isPlaying) {
      this.startProgress();
    }
  }

  // Stops progress while user is dragging the thumb
  private handleInput() {
    this.stopProgress();
  }

  private setSeekValue(val: number) {
    if (val < 0) val = 0;
    if (val > this.trackLength) val = this.trackLength;
    this.seekValue = val;
  }

  // ===================================================================
  // RENDER
  // ===================================================================

  override render() {
    const elapsedTime = this.hasTrack ? formatSeconds(this.seekValue) : '--:--';
    const remainingTime = this.hasTrack
      ? formatSeconds(this.trackLength - this.seekValue)
      : '--:--';

    return html`
      <div id="seek-bar-container">
        <small>${elapsedTime}</small>
        <wa-slider
          .value="${this.seekValue}"
          max="${this.trackLength}"
          ?with-tooltip="${this.hasTrack}"
          .valueFormatter="${this.hasTrack ? formatSeconds : null}"
          ${ref(this.rangeRef)}
          @change="${this.handleChange}"
          @input="${this.handleInput}"
        ></wa-slider>
        <small>${remainingTime}</small>
      </div>
    `;
  }
}

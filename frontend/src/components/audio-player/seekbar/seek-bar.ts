import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { ref, createRef } from 'lit/directives/ref.js';
import WaSlider from '@awesome.me/webawesome/dist/components/slider/slider.js';
import { formatSeconds } from '@utils/time';
import { PlayerController } from '@store/controllers/player-controller';
import { designTokens } from '../../../styles/tokens.css';

const ProgressIntervalMillis = 1000;

@customElement('seek-bar')
export class SeekBar extends LitElement {
  private player = new PlayerController(this);
  private rangeRef = createRef<WaSlider>();
  private timerID: number = -1;
  private previousTrackChangeId: number = -1;

  /** The sequence number of the last backend report applied. */
  private previousPositionSeq: number = -1;

  @state()
  private seekValue: number = 0;

  /** Whether the right-hand clock shows time remaining or total. */
  @state()
  private showRemaining: boolean = true;

  static override styles = [designTokens, css`
    wa-slider {
      --track-size: 6px;
      flex: 1;
      margin: 0 16px;
      --wa-tooltip-background-color: var(--yj-bg-elevated, #343a40);
      --wa-tooltip-content-color: var(--yj-text-primary, white);
      --wa-tooltip-border-color: var(--yj-bg-elevated, #343a40);
      --wa-tooltip-border-radius: 4px;
      --wa-tooltip-font-size: var(--yj-text-lg);
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

    .time-toggle {
      background: none;
      border: none;
      padding: 0;
      color: inherit;
      font: inherit;
      font-size: var(--wa-font-size-s, 0.875rem);
      cursor: pointer;
    }

    .time-toggle:hover,
    .time-toggle:focus-visible {
      text-decoration: underline;
    }
  `];

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

    // The backend's own position wins over anything counted here.
    // Every report resets the interpolation, so the bar can be at most
    // one tick wrong and can never accumulate — which is what made a
    // keyboard seek desync it by 30 s (H-3).
    // A report for a track that is no longer loaded is stale by
    // definition: the change id is the only thing that distinguishes
    // it, since the same file can play twice in a row.
    const position = this.player.position;
    const forThisTrack =
      position !== null && position.trackChangeId === currentChangeId;

    if (position && forThisTrack && position.seq !== this.previousPositionSeq) {
      this.previousPositionSeq = position.seq;
      this.seekValue = position.positionSeconds;
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

  /**
   * Interpolate between backend reports.
   *
   * This is not the clock — it exists only so the display moves
   * smoothly in the second between two ticks.  It is stopped and
   * restarted by every report, so its error is bounded by one second
   * and is discarded rather than carried.
   */
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

  /** H-16: the right-hand clock never said which number it was. */
  private toggleRemaining() {
    this.showRemaining = !this.showRemaining;
  }

  override render() {
    const elapsedTime = this.hasTrack ? formatSeconds(this.seekValue) : '--:--';
    const rightLabel = this.showRemaining
      ? `-${formatSeconds(Math.max(0, this.trackLength - this.seekValue))}`
      : formatSeconds(this.trackLength);
    const rightTime = this.hasTrack ? rightLabel : '--:--';

    return html`
      <div id="seek-bar-container">
        <small data-testid="elapsed-time">${elapsedTime}</small>
        <wa-slider
          aria-label="Seek"
          .value="${this.seekValue}"
          max="${this.trackLength}"
          ?with-tooltip="${this.hasTrack}"
          .valueFormatter="${this.hasTrack ? formatSeconds : null}"
          ${ref(this.rangeRef)}
          @change="${this.handleChange}"
          @input="${this.handleInput}"
        ></wa-slider>
        <button
          class="time-toggle"
          type="button"
          data-testid="remaining-time"
          title="${this.showRemaining
            ? 'Time remaining — click for total duration'
            : 'Total duration — click for time remaining'}"
          aria-label="${this.showRemaining
            ? `Time remaining ${formatSeconds(
                Math.max(0, this.trackLength - this.seekValue),
              )}. Show total duration.`
            : `Total duration ${formatSeconds(
                this.trackLength,
              )}. Show time remaining.`}"
          @click="${this.toggleRemaining}"
        >${rightTime}</button>
      </div>
    `;
  }
}

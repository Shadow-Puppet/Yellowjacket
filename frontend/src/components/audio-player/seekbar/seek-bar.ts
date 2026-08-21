import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { ref, createRef } from 'lit/directives/ref.js';
import WaSlider from '@awesome.me/webawesome/dist/components/slider/slider.js';
import { formatSeconds } from '@utils/time';
import { PlayerController } from '@store/controllers/player-controller';
import { designTokens } from '../../../styles/tokens.css';
import { waSliderLabel } from '../../../styles/wa-slider-label.css';

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

  /**
   * Whether the user is dragging the thumb right now.
   *
   * It is `@state` rather than a plain field because `updated()` owns
   * the interval and only reactive state brings `updated()` round.  A
   * bare `stopProgress()` in the input handler mutated nothing, so
   * nothing re-rendered, so the tail of `updated()` that restarts the
   * interval never ran — and the only things that could restart it
   * were a `change` event or the next backend report.  Any `input`
   * without a committed `change` therefore froze the interpolation:
   * a drag cancelled outside the element, a pointer taken by a scroll,
   * or a touch on the track treated as a scrub, which on a phone are
   * ordinary gestures.  While playing, the 1 Hz report papered over it
   * within a second; with reports not arriving it was permanent.
   */
  @state()
  private dragging: boolean = false;

  /** Whether the right-hand clock shows time remaining or total. */
  @state()
  private showRemaining: boolean = true;

  static override styles = [designTokens, waSliderLabel, css`
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

    /* The phone's seek bar, and this block is last on purpose.

       A media query adds no specificity, so this lived above the plain
       "wa-slider" rule and lost to it at every width: the 12px track it
       asks for had never once applied, and the bar measured 261x6 on
       the device while the source said 12. That is index.css's rule
       ("the phone section is last on purpose") met inside a component's
       own stylesheet, and nothing renders differently in any tier here
       to say so.

       The bottom bar's seek bar is display:none below this width (016
       B2 phase 1), so the only instance a viewport media query can
       reach is the full-screen now-playing view's -- which is exactly
       the one a thumb uses. The desktop bar keeps its 6px, where a
       mouse is precise and the thickness is right.

       The painted track and the thing you can hit are allowed to
       differ, and a slider is the clearest case where they should: 12px
       is a progress bar you can see, and 44px is the app's touch floor
       (#56). A 44px-*thick* bar would be wrong-looking and would cost
       the album art the vertical space #51 spent an issue recovering.

       Two things about how the target is built.

       The padding goes on ::part(slider) rather than on the host,
       because that inner div is what carries the gesture -- it has the
       listener and the touch-action: none, and it is exactly the host's
       size, so padding the host would grow a box that does not take the
       press.

       The padding is asymmetric and the margins cancel it, so the row
       does not grow by the difference. Both halves are measured: the
       seek row is 19px (its clocks, not the track, decide that) and the
       play button's top edge is 8px below it, so the target takes the
       space *above*, where .art is a non-interactive div. Growing the
       row instead cost the art 25px of 143. Verified on the device at
       424x439: hit area 44px, painted track 12px, row still 19px, art
       still 143px, 8px of clearance left under the play button, a press
       26px above the track seeks, and a hit test on the play button's
       top edge still reaches the play button. */
    @media (max-width: 599px) {
      wa-slider {
        --track-size: 12px;
      }

      wa-slider::part(slider) {
        padding-block: 28px 4px;
        margin-block: -28px -4px;
      }
    }

    #seek-bar-container {
      display: flex;
      justify-content: space-between;
      align-items: center;
    }

    /* The clocks must not resize as they count.

       Two things move them, and they need different answers. Digits in
       a proportional font are different widths, so 1:11 is narrower
       than 4:08 and the bar breathed once a second -- that is what
       tabular figures fix. The character *count* changes too, at the
       hundredth minute and whenever the right-hand clock is toggled to
       remaining and grows a minus sign, and a figure width cannot fix
       that -- so each clock also reserves the widest string this track
       can put in it. The budget is per track rather than a constant
       because reserving six characters on every track would push the
       slider in by a character at each end for nothing. */
    #seek-bar-container small,
    .time-toggle {
      font-variant-numeric: tabular-nums;
      flex: 0 0 auto;
      min-width: calc(var(--yj-clock-chars, 5) * 1ch);
    }

    #seek-bar-container small {
      text-align: left;
    }

    .time-toggle {
      background: none;
      border: none;
      padding: 0;
      color: inherit;
      font: inherit;
      font-size: var(--wa-font-size-s, 0.875rem);
      cursor: pointer;
      /* One more for the minus sign the remaining form carries. */
      min-width: calc((var(--yj-clock-chars, 5) + 1) * 1ch);
      text-align: right;
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
    this.endDrag();
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
    //
    // A report arriving mid-drag is deliberately *not* applied: the
    // thumb belongs to the finger on it, and adopting a report once a
    // second pulls it back out from under them.  The seq is left
    // unrecorded too, so the first report after the drag still counts
    // as fresh.
    const position = this.player.position;
    const forThisTrack =
      position !== null && position.trackChangeId === currentChangeId;

    if (
      position &&
      forThisTrack &&
      !this.dragging &&
      position.seq !== this.previousPositionSeq
    ) {
      this.previousPositionSeq = position.seq;
      this.seekValue = position.positionSeconds;
      this.stopProgress();
    }

    // One owner for the interval, and this is it.  Every other place
    // that wants it started or stopped says so by changing state that
    // brings us back here, so the timer cannot be left running by a
    // path that forgot to stop it or stopped by a path that forgot to
    // start it again.
    if (this.isPlaying && this.hasTrack && !this.dragging) {
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
    this.endDrag();
    this.setSeekValue(newSeekVal);
    this.player.seek(newSeekVal);
  }

  /**
   * The user is moving the thumb.
   *
   * This only records that fact; `updated()` decides what it means for
   * the interval.  `seekValue` follows the slider so the clocks track
   * the thumb during the drag rather than jumping when it is released.
   */
  private handleInput(e: Event) {
    this.setSeekValue((e.target as WaSlider).value);

    if (this.dragging) {
      return;
    }

    this.dragging = true;

    // A drag that never commits must not strand the flag, or this fix
    // turns a stall of up to one second into a permanent one -- which
    // is the failure it exists to remove.  `change` is the ordinary
    // end; these are the ones that are not, and they are on the
    // document because the pointer is routinely released outside the
    // element it started in.  A drag's listeners belong to the drag,
    // so they go on with it and come off with it.
    document.addEventListener('pointerup', this.endDrag);
    document.addEventListener('pointercancel', this.endDrag);
    document.addEventListener('touchend', this.endDrag);
    document.addEventListener('touchcancel', this.endDrag);
  }

  private endDrag = () => {
    document.removeEventListener('pointerup', this.endDrag);
    document.removeEventListener('pointercancel', this.endDrag);
    document.removeEventListener('touchend', this.endDrag);
    document.removeEventListener('touchcancel', this.endDrag);

    this.dragging = false;
  };

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

    // The widest string either clock can hold for *this* track. The
    // duration is the longest elapsed value there can be, so its length
    // is the budget; `--:--` is five, which is also the floor.
    const clockChars = Math.max(
      5,
      this.hasTrack ? formatSeconds(this.trackLength).length : 0,
    );

    return html`
      <div
        id="seek-bar-container"
        style="--yj-clock-chars: ${clockChars}"
      >
        <small data-testid="elapsed-time">${elapsedTime}</small>
        <wa-slider
          label="Seek"
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

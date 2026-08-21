import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/slider/slider.js';
import type WaSlider from '@awesome.me/webawesome/dist/components/slider/slider.js';
import { PlayerController } from '@store/controllers/player-controller';
import { volumeStyleStore } from '@store/volume-style-store';
import { designTokens } from '../../../styles/tokens.css';
import { waSliderLabel } from '../../../styles/wa-slider-label.css';

/** Volume change (0-100) applied per scroll-wheel tick. */
const WHEEL_STEP = 5;

/** Delay before a live volume change is pushed to the backend. */
const VOLUME_DEBOUNCE_MS = 60;

@customElement('volume-control')
export class VolumeControl extends LitElement {
  private player = new PlayerController(this);
  private boundHandleOutsideClick = this.handleOutsideClick.bind(this);
  private volumeDebounceTimer?: ReturnType<typeof setTimeout>;

  @state()
  private showSlider = false;

  /** Whether this is the click-to-open popup rather than a slider. */
  @state()
  private popup = volumeStyleStore.popup;

  /**
   * Whether there is a volume of ours to control at all (#64).
   *
   * The decision is made here rather than at either mount point,
   * because there are two -- the bottom bar's copy lives in
   * `index.html`, which has no module scope to make it conditional --
   * and one of them is a control the shell cannot un-render. So the
   * control answers for itself, and the bar and the phone's
   * full-screen transport get the same answer without either knowing
   * the question exists.
   *
   * It renders `nothing` *and* hides the host: an empty shadow root is
   * what stops a positional or role query finding a button that cannot
   * act, and `:host([hidden])` is what stops the element occupying a
   * flex item's worth of the transport -- the `:host` display above
   * outranks the UA's `[hidden]` rule, so it has to be said.
   */
  @state()
  private available = volumeStyleStore.available;

  private unsubscribeStyle?: () => void;

  // Locally-tracked volume while the user is actively dragging or scrolling.
  // The store's volume only updates once the backend echoes VolumeChanged
  // (which we debounce), so we track intent here for responsive UI and to let
  // rapid events accumulate. Cleared once the store catches up.
  @state()
  private pendingVolume: number | null = null;

  static override styles = [designTokens, waSliderLabel, css`
    :host {
      position: relative;
      display: inline-flex;
      align-items: center;
    }

    /* See the available field. A gap is only drawn between boxes,
       so a hidden host costs its parent nothing -- which is where the
       29px this gives back to Now Playing comes from (#172). */
    :host([hidden]) {
      display: none;
    }

    button {
      background: none;
      border: none;
      cursor: pointer;
      color: inherit;
      padding: 4px;
      display: flex;
      align-items: center;
    }

    /* Muted is a state the volume number cannot express, so it gets a
       colour of its own on top of the crossed-out icon. */
    button.muted {
      color: var(--yj-text-tertiary, #888);
    }

    .volume-popup.muted wa-slider::part(indicator) {
      background: var(--yj-text-tertiary, #888);
    }

    .mute-toggle {
      margin-top: 10px;
      font-size: 11px;
      color: var(--yj-text-secondary, #b3b3b3);
      white-space: nowrap;
    }

    .volume-popup {
      position: absolute;
      bottom: 100%;
      left: 50%;
      transform: translateX(-50%);
      background: var(--yj-bg-surface, #1a1a1a);
      border: 1px solid var(--yj-border-subtle, #333);
      border-radius: 8px;
      padding: 16px 8px;
      margin-bottom: 8px;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      z-index: 100;
    }

    wa-slider {
      --track-size: 6px;
      --thumb-width: 16px;
      --thumb-height: 16px;
    }

    .volume-popup wa-slider::part(track) {
      background: var(--yj-text-primary, white);
      height: 120px;
    }

    /* The inline slider (#42). It is the default now, so the width is
       a real layout decision rather than a detail: 5em is wide enough
       to aim at and narrow enough that the bottom bar's *outer*
       columns stay equal without squeezing the transport — which is
       the arrangement #23 depends on.

       flex-shrink: 0 for the reason the top bar's children have it
       (#143): a control that quietly gets narrower under pressure
       hides the fact that the bar has run out of room. This one stands
       down at phone width instead, in index.css, where the shell can
       see the viewport. */
    .inline-slider {
      width: 5em;
      flex-shrink: 0;
    }

    .inline-slider::part(track) {
      background: var(--yj-text-primary, white);
    }

    wa-slider::part(indicator) {
      background: var(--yj-accent, yellow);
    }

    wa-slider::part(thumb) {
      background: var(--yj-bg-base, black);
    }
  `];

  // ===================================================================
  // DERIVED STATE
  // ===================================================================

  private get currentVolume(): number {
    return this.pendingVolume ?? this.player.volume;
  }

  private get volumeIcon(): string {
    const vol = this.currentVolume;

    if (this.player.muted) return 'volume-xmark';
    if (vol === 0) return 'volume-off';
    if (vol <= 50) return 'volume-low';

    return 'volume-high';
  }

  // ===================================================================
  // LIFECYCLE
  // ===================================================================

  override connectedCallback() {
    super.connectedCallback();

    this.unsubscribeStyle = volumeStyleStore.subscribe(() => {
      this.popup = volumeStyleStore.popup;
      this.setAvailable(volumeStyleStore.available);

      // Switching to the slider while the popup is open would leave the
      // document listener installed for a popup that no longer renders.
      if (!this.popup) this.closeSlider();
    });

    this.setAvailable(volumeStyleStore.available);

    void volumeStyleStore.init();
  }

  override disconnectedCallback() {
    super.disconnectedCallback();
    this.unsubscribeStyle?.();
    document.removeEventListener('click', this.boundHandleOutsideClick);
    clearTimeout(this.volumeDebounceTimer);
  }

  override willUpdate() {
    // Once the backend has echoed our pending change back through the store,
    // drop the local override so external volume changes are reflected again.
    if (this.pendingVolume !== null && this.player.volume === this.pendingVolume) {
      this.pendingVolume = null;
    }
  }

  // ===================================================================
  // EVENT HANDLERS
  // ===================================================================

  private toggleSlider(e: Event) {
    e.stopPropagation();
    this.showSlider = !this.showSlider;

    if (this.showSlider) {
      document.addEventListener('click', this.boundHandleOutsideClick);
    } else {
      document.removeEventListener('click', this.boundHandleOutsideClick);
    }
  }

  private handleOutsideClick(e: Event) {
    const path = e.composedPath();

    if (!path.includes(this)) this.closeSlider();
  }

  private closeSlider() {
    this.showSlider = false;
    document.removeEventListener('click', this.boundHandleOutsideClick);
  }

  private handleInput(e: Event) {
    this.changeVolume((e.target as WaSlider).value);
  }

  private handleWheel(e: WheelEvent) {
    e.preventDefault();
    const direction = e.deltaY < 0 ? 1 : -1;
    this.changeVolume(this.currentVolume + direction * WHEEL_STEP);
  }

  private handlePopupClick(e: Event) {
    e.stopPropagation();
  }

  /** Update the UI immediately and push to the backend on a short debounce. */
  private changeVolume(value: number) {
    const clamped = Math.max(0, Math.min(100, Math.round(value)));
    this.pendingVolume = clamped;

    clearTimeout(this.volumeDebounceTimer);
    this.volumeDebounceTimer = setTimeout(() => {
      this.player.setVolume(clamped);
    }, VOLUME_DEBOUNCE_MS);
  }

  // ===================================================================
  // RENDER
  // ===================================================================

  /**
   * `hidden` is set imperatively rather than reflected from the state,
   * because it has to be on the *host* and a `@state` does not reflect.
   * It is the right attribute besides: it takes the element out of the
   * accessibility tree as well as out of the layout.
   */
  private setAvailable(available: boolean) {
    this.available = available;
    this.hidden = !available;

    // A popup left open when the control goes away would keep its
    // document click listener installed for markup that no longer
    // renders.
    if (!available) this.closeSlider();
  }

  override render() {
    if (!this.available) return nothing;

    const muted = this.player.muted;

    // Inline, the icon is the mute toggle rather than a disclosure:
    // there is nothing left to disclose, and a button that opens a
    // popup containing the slider already beside it would be a control
    // whose only effect is to duplicate its neighbour.
    const iconAction = this.popup
      ? this.toggleSlider
      : () => this.player.toggleMute();
    const iconLabel = this.popup
      ? muted
        ? 'Muted'
        : `Volume ${this.currentVolume}%`
      : muted
        ? 'Unmute'
        : 'Mute';

    return html`
      <button
        class=${muted ? 'muted' : ''}
        title=${muted ? 'Muted — click for volume' : 'Volume'}
        aria-label=${iconLabel}
        data-muted=${muted ? 'true' : 'false'}
        @click="${iconAction}"
        @wheel="${this.handleWheel}"
      >
        <wa-icon name=${this.volumeIcon}></wa-icon>
      </button>
      ${!this.popup
        ? html`
            <wa-slider
              class="inline-slider ${muted ? 'muted' : ''}"
              label="Volume"
              min="0"
              max="100"
              .value="${this.currentVolume}"
              @input="${this.handleInput}"
              @wheel="${this.handleWheel}"
            ></wa-slider>
          `
        : ''}
      ${this.popup && this.showSlider
        ? html`
            <div
              class="volume-popup ${muted ? 'muted' : ''}"
              @click="${this.handlePopupClick}"
            >
              <wa-slider
                label="Volume"
                orientation="vertical"
                min="0"
                max="100"
                .value="${this.currentVolume}"
                @input="${this.handleInput}"
              ></wa-slider>
              <button
                class="mute-toggle"
                @click=${() => this.player.toggleMute()}
              >
                ${muted ? 'Unmute' : 'Mute'}
              </button>
            </div>
          `
        : ''}
    `;
  }
}

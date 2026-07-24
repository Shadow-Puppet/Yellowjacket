import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/slider/slider.js';
import type WaSlider from '@awesome.me/webawesome/dist/components/slider/slider.js';
import { PlayerController } from '@store/controllers/player-controller';
import { designTokens } from '../../../styles/tokens.css';

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

  // Locally-tracked volume while the user is actively dragging or scrolling.
  // The store's volume only updates once the backend echoes VolumeChanged
  // (which we debounce), so we track intent here for responsive UI and to let
  // rapid events accumulate. Cleared once the store catches up.
  @state()
  private pendingVolume: number | null = null;

  static override styles = [designTokens, css`
    :host {
      position: relative;
      display: inline-flex;
      align-items: center;
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
      justify-content: center;
      z-index: 100;
    }

    wa-slider {
      --track-size: 6px;
      --thumb-width: 16px;
      --thumb-height: 16px;
    }

    wa-slider::part(track) {
      background: var(--yj-text-primary, white);
      height: 120px;
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

    if (vol === 0) return 'volume-xmark';
    if (vol <= 50) return 'volume-low';

    return 'volume-high';
  }

  // ===================================================================
  // LIFECYCLE
  // ===================================================================

  override disconnectedCallback() {
    super.disconnectedCallback();
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

    if (!path.includes(this)) {
      this.showSlider = false;
      document.removeEventListener('click', this.boundHandleOutsideClick);
    }
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

  override render() {
    return html`
      <button @click="${this.toggleSlider}" @wheel="${this.handleWheel}">
        <wa-icon name=${this.volumeIcon}></wa-icon>
      </button>
      ${this.showSlider
        ? html`
            <div class="volume-popup" @click="${this.handlePopupClick}">
              <wa-slider
                orientation="vertical"
                min="0"
                max="100"
                .value="${this.currentVolume}"
                @input="${this.handleInput}"
              ></wa-slider>
            </div>
          `
        : ''}
    `;
  }
}

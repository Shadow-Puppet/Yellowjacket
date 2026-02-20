import { LitElement, html, css } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/slider/slider.js';
import type WaSlider from '@awesome.me/webawesome/dist/components/slider/slider.js';
import { PlayerController } from '@store/controllers/player-controller';

@customElement('volume-control')
export class VolumeControl extends LitElement {
  private player = new PlayerController(this);
  private boundHandleOutsideClick = this.handleOutsideClick.bind(this);

  @state()
  private showSlider = false;

  static override styles = css`
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
      padding: 0.25em;
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
      padding: 1em 0.5em;
      margin-bottom: 0.5em;
      display: flex;
      justify-content: center;
      z-index: 100;
    }

    wa-slider {
      --track-size: 6px;
      --thumb-width: 1em;
      --thumb-height: 1em;
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
  `;

  // ===================================================================
  // DERIVED STATE
  // ===================================================================

  private get volumeIcon(): string {
    const vol = this.player.volume;

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
    const value = (e.target as WaSlider).value;
    this.player.setVolume(value);
  }

  private handlePopupClick(e: Event) {
    e.stopPropagation();
  }

  // ===================================================================
  // RENDER
  // ===================================================================

  override render() {
    return html`
      <button @click="${this.toggleSlider}">
        <wa-icon name=${this.volumeIcon}></wa-icon>
      </button>
      ${this.showSlider
        ? html`
            <div class="volume-popup" @click="${this.handlePopupClick}">
              <wa-slider
                orientation="vertical"
                min="0"
                max="100"
                .value="${this.player.volume}"
                @change="${this.handleInput}"
              ></wa-slider>
            </div>
          `
        : ''}
    `;
  }
}

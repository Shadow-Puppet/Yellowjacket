import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import type WaPopup from '@awesome.me/webawesome/dist/components/popup/popup.js';
import { PlayerController } from '@store/controllers/player-controller';
import { FavoritesController } from '@store/controllers/favorites-controller';
import { designTokens } from '../../styles/tokens.css';

const MIN_WIDTH = 120;
const MAX_WIDTH = 350;
const DEFAULT_WIDTH = 200;

const SCROLL_STORAGE_KEY = 'yj-now-playing-scroll-mode';
const SCROLL_CHANGE_EVENT = 'yj-scroll-mode-changed';

/** Pixels per second the text scrolls at. */
const SCROLL_SPEED = 30;
const MIN_DURATION = 3;
const MAX_DURATION = 15;

type ScrollMode = 'hover' | 'always' | 'never';

@customElement('now-playing')
export class NowPlaying extends LitElement {
    private player = new PlayerController(this);
    private favCtrl = new FavoritesController(this);

    @state()
    private isDragging = false;

    @state()
    private showCoverPreview = false;

    @state()
    private scrollMode: ScrollMode = 'hover';

    @state()
    private titleOverflows = false;

    @state()
    private artistOverflows = false;

    @state()
    private titleHovered = false;

    @state()
    private artistHovered = false;

    /** Whether each field is actively mid-scroll (class toggle). */
    @state()
    private titleScrolling = false;

    @state()
    private artistScrolling = false;

    private scrollTimers: Record<string, ReturnType<typeof setTimeout> | null> = {
        title: null,
        artist: null,
    };

    private resizeObserver?: ResizeObserver;

    static override styles = [designTokens, css`
    :host {
      display: block;
      position: relative;
      height: 100%;
      overflow: hidden;
    }

    .now-playing {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 8px;
      height: 100%;
      box-sizing: border-box;
    }

    .cover-art {
      width: 48px;
      height: 48px;
      flex-shrink: 0;
      border-radius: 4px;
      overflow: hidden;
    }

    .cover-art img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }

    .cover-placeholder {
      width: 100%;
      height: 100%;
      background-color: var(--yj-bg-base, #000);
      display: flex;
      align-items: center;
      justify-content: center;
    }

    .cover-placeholder wa-icon {
      color: var(--yj-text-primary, #fff);
      font-size: var(--yj-icon-lg);
    }

    .cover-art-wrapper {
      position: relative;
    }

    .cover-preview-panel {
      width: 500px;
      height: 500px;
      border-radius: 8px;
      overflow: hidden;
      box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
      pointer-events: none;
    }

    .cover-preview-panel img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }

    .track-info-wrapper {
      display: flex;
      align-items: center;
      gap: 8px;
      min-width: 0;
      flex: 1;
    }

    .track-info {
      display: flex;
      flex-direction: column;
      gap: 2px;
      min-width: 0;
    }

    .fav-btn {
      display: flex;
      align-items: center;
      justify-content: center;
      flex-shrink: 0;
      cursor: pointer;
      color: var(--yj-text-tertiary, #666);
      font-size: var(--yj-icon-sm);
      transition: color 0.1s ease;
      background: none;
      border: none;
      padding: 0;
    }

    .fav-btn:hover {
      color: var(--yj-text-primary, #fff);
    }

    .fav-btn.favorited {
      color: var(--yj-accent, #ffd43b);
    }

    .fav-btn.favorited:hover {
      color: var(--yj-accent, #ffd43b);
      opacity: 0.8;
    }

    /* --- Scrollable text containers --- */

    .track-title,
    .track-artist {
      position: relative;
      white-space: nowrap;
      overflow: hidden;
    }

    .track-title {
      font-size: var(--yj-text-lg);
      font-weight: 500;
    }

    .track-artist {
      font-size: var(--yj-text-sm);
      color: var(--yj-text-tertiary, #666);
    }

    /* Static ellipsis when not scrolling */
    .track-title:not(.will-scroll),
    .track-artist:not(.will-scroll) {
      text-overflow: ellipsis;
    }

    .scroll-content {
      display: inline-block;
      white-space: nowrap;
    }

    /* Scrolling text: use a single transition instead of an infinite
       CSS animation.  The infinite animation + mask-image was repainting
       every frame in software rendering mode (no DMABuf).  A transition
       only repaints during the active scroll, and pauses are free. */
    .will-scroll .scroll-content {
      transition: transform var(--scroll-duration, 5s) linear;
      padding-right: 2em;
    }

    .will-scroll.scrolling .scroll-content {
      transform: translateX(var(--scroll-distance, -100%));
    }

    .resize-handle {
      position: absolute;
      top: 0;
      right: 0;
      width: 4px;
      height: 100%;
      cursor: col-resize;
      background-color: transparent;
      transition: background-color 0.15s ease;
      z-index: 10;
    }

    .resize-handle:hover,
    .resize-handle.dragging {
      background-color: var(--yj-text-tertiary, #6c757d);
    }
  `];

    override connectedCallback() {
        super.connectedCallback();
        this.loadScrollMode();
        this.updateWidth(DEFAULT_WIDTH);
        document.addEventListener('mousemove', this.handleMouseMove);
        document.addEventListener('mouseup', this.handleMouseUp);
        window.addEventListener(SCROLL_CHANGE_EVENT, this.handleScrollModeEvent);

        this.resizeObserver = new ResizeObserver(() => {
            this.checkOverflows();
        });
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        document.removeEventListener('mousemove', this.handleMouseMove);
        document.removeEventListener('mouseup', this.handleMouseUp);
        window.removeEventListener(SCROLL_CHANGE_EVENT, this.handleScrollModeEvent);
        this.resizeObserver?.disconnect();
        this.stopScrollCycle('title');
        this.stopScrollCycle('artist');
    }

    protected override updated(): void {
        this.checkOverflows();
        this.observeTextContainers();
        this.applyScrollDistances();
        this.syncScrollCycles();
    }

    override render() {
        const track = this.player.currentTrack;

        if (!track) {
            return html`
        <div class="now-playing">
          <div class="cover-art">
            <div class="cover-placeholder"><wa-icon name="music"></wa-icon></div>
          </div>
        </div>
        <div
          class="resize-handle ${this.isDragging ? 'dragging' : ''}"
          @mousedown=${this.handleMouseDown}
        ></div>
      `;
        }

        const isFav = track.filePath
            ? this.favCtrl.isFavorited(track.filePath)
            : false;
        const favVariant = isFav ? 'solid' : 'regular';

        const titleScrolling = this.shouldScroll('title');
        const artistScrolling = this.shouldScroll('artist');

        return html`
      <div class="now-playing">
        <div class="cover-art-wrapper">
          <div
            class="cover-art"
            @mouseenter=${this.handleCoverMouseEnter}
            @mouseleave=${this.handleCoverMouseLeave}
          >
            ${track.coverArt
              ? html`<img
                  src="${track.coverArtSmall || track.coverArt}"
                  alt="Album cover"
                  decoding="async"
                  @error=${(e: Event) => {
                      const img = e.target as HTMLImageElement;
                      if (
                          track.coverArt &&
                          img.src !== track.coverArt
                      ) {
                          img.src = track.coverArt;
                      }
                  }}
                />`
              : html`<div class="cover-placeholder">
                  <wa-icon name="music"></wa-icon>
                </div>`}
          </div>
          <wa-popup
            id="cover-preview"
            placement="top-start"
            flip
            shift
            .active=${this.showCoverPreview}
          >
            ${this.showCoverPreview && track.coverArt
              ? html`
                  <div class="cover-preview-panel">
                    <img
                      src="${track.coverArt}"
                      alt="Album cover full size"
                      decoding="async"
                    />
                  </div>
                `
              : nothing}
          </wa-popup>
        </div>
        <div class="track-info-wrapper">
          <div class="track-info">
            <span
              class="track-title ${titleScrolling ? 'will-scroll' : ''} ${this.titleScrolling ? 'scrolling' : ''}"
              @mouseenter=${this.handleTitleMouseEnter}
              @mouseleave=${this.handleTitleMouseLeave}
              @transitionend=${() => this.onScrollCycleEnd('title')}
            >
              <span class="scroll-content">${track.title}</span>
            </span>
            <span
              class="track-artist ${artistScrolling ? 'will-scroll' : ''} ${this.artistScrolling ? 'scrolling' : ''}"
              @mouseenter=${this.handleArtistMouseEnter}
              @mouseleave=${this.handleArtistMouseLeave}
              @transitionend=${() => this.onScrollCycleEnd('artist')}
            >
              <span class="scroll-content">${track.artist || 'Unknown Artist'}</span>
            </span>
          </div>
          ${track.filePath
              ? html`
                <button
                  class="fav-btn ${isFav ? 'favorited' : ''}"
                  title="${isFav ? `Remove from ${this.favCtrl.playlistName}` : `Add to ${this.favCtrl.playlistName}`}"
                  @click=${() =>
                      void this.favCtrl.toggleFavorite(
                          track.filePath,
                      )}
                >
                  <wa-icon
                    name=${this.favCtrl.iconName}
                    variant=${favVariant}
                  ></wa-icon>
                </button>
              `
              : nothing}
        </div>
      </div>
      <div
        class="resize-handle ${this.isDragging ? 'dragging' : ''}"
        @mousedown=${this.handleMouseDown}
      ></div>
    `;
    }

    // ===================================================================
    // SCROLL LOGIC
    // ===================================================================

    private loadScrollMode(): void {
        const stored = localStorage.getItem(SCROLL_STORAGE_KEY);

        if (stored === 'hover' || stored === 'always' || stored === 'never') {
            this.scrollMode = stored;
        }
    }

    private handleScrollModeEvent = (): void => {
        this.loadScrollMode();
    };

    private shouldScroll(field: 'title' | 'artist'): boolean {
        const overflows = field === 'title' ? this.titleOverflows : this.artistOverflows;

        if (!overflows || this.scrollMode === 'never') return false;
        if (this.scrollMode === 'always') return true;

        // hover mode
        return field === 'title' ? this.titleHovered : this.artistHovered;
    }

    private checkOverflows(): void {
        const titleEl = this.shadowRoot?.querySelector<HTMLElement>('.track-title');
        const artistEl = this.shadowRoot?.querySelector<HTMLElement>('.track-artist');

        const titleNow = titleEl ? titleEl.scrollWidth > titleEl.clientWidth : false;
        const artistNow = artistEl ? artistEl.scrollWidth > artistEl.clientWidth : false;

        // Only update state when changed to avoid infinite loops
        if (titleNow !== this.titleOverflows) {
            this.titleOverflows = titleNow;
        }

        if (artistNow !== this.artistOverflows) {
            this.artistOverflows = artistNow;
        }
    }

    private observeTextContainers(): void {
        if (!this.resizeObserver) return;

        const titleEl = this.shadowRoot?.querySelector('.track-title');
        const artistEl = this.shadowRoot?.querySelector('.track-artist');

        // ResizeObserver automatically deduplicates observed elements
        if (titleEl) this.resizeObserver.observe(titleEl);
        if (artistEl) this.resizeObserver.observe(artistEl);
    }

    /**
     * Set CSS custom properties on each scroll-content span so the
     * animation knows how far to travel and how long to take.
     */
    private applyScrollDistances(): void {
        const pairs: Array<{ container: string; overflows: boolean }> = [
            { container: '.track-title', overflows: this.titleOverflows },
            { container: '.track-artist', overflows: this.artistOverflows },
        ];

        for (const { container, overflows } of pairs) {
            if (!overflows) continue;

            const el = this.shadowRoot?.querySelector<HTMLElement>(container);
            const content = el?.querySelector<HTMLElement>('.scroll-content');

            if (!el || !content) continue;

            const overflow = content.scrollWidth - el.clientWidth;

            if (overflow > 0) {
                const duration = Math.min(MAX_DURATION, Math.max(MIN_DURATION, overflow / SCROLL_SPEED));
                el.style.setProperty('--scroll-distance', `-${overflow}px`);
                el.style.setProperty('--scroll-duration', `${duration.toFixed(1)}s`);
            }
        }
    }

    // ===================================================================
    // TRANSITION-BASED SCROLL CYCLE
    // ===================================================================

    /**
     * Start a scroll cycle for a field.  Adds the `scrolling` class which
     * triggers a CSS transition.  When the transition ends, we pause then
     * snap back and repeat.  This replaces the old infinite CSS animation
     * which repainted every frame even during the pause phases.
     */
    private startScrollCycle(field: 'title' | 'artist'): void {
        if (!this.shouldScroll(field)) return;

        // Small delay before starting the scroll
        this.scrollTimers[field] = setTimeout(() => {
            if (field === 'title') this.titleScrolling = true;
            else this.artistScrolling = true;
        }, 1500);
    }

    private stopScrollCycle(field: 'title' | 'artist'): void {
        if (this.scrollTimers[field] !== null) {
            clearTimeout(this.scrollTimers[field]!);
            this.scrollTimers[field] = null;
        }

        if (field === 'title') this.titleScrolling = false;
        else this.artistScrolling = false;
    }

    private onScrollCycleEnd(field: 'title' | 'artist'): void {
        // Transition finished → snap back after a pause, then repeat
        if (field === 'title') this.titleScrolling = false;
        else this.artistScrolling = false;

        // Restart after a pause (2s at the scrolled-to position)
        this.scrollTimers[field] = setTimeout(() => {
            this.startScrollCycle(field);
        }, 2000);
    }

    /** Called from updated() when shouldScroll state changes. */
    private syncScrollCycles(): void {
        for (const field of ['title', 'artist'] as const) {
            const should = this.shouldScroll(field);
            const active = this.scrollTimers[field] !== null ||
                (field === 'title' ? this.titleScrolling : this.artistScrolling);

            if (should && !active) {
                this.startScrollCycle(field);
            } else if (!should && active) {
                this.stopScrollCycle(field);
            }
        }
    }

    // ===================================================================
    // HOVER HANDLERS
    // ===================================================================

    private handleTitleMouseEnter = (): void => {
        this.titleHovered = true;
    };

    private handleTitleMouseLeave = (): void => {
        this.titleHovered = false;
    };

    private handleArtistMouseEnter = (): void => {
        this.artistHovered = true;
    };

    private handleArtistMouseLeave = (): void => {
        this.artistHovered = false;
    };

    // ===================================================================
    // COVER PREVIEW
    // ===================================================================

    private handleCoverMouseEnter = () => {
        const track = this.player.currentTrack;

        if (!track?.coverArt) return;

        this.showCoverPreview = true;

        this.updateComplete.then(() => {
            const popup =
                this.shadowRoot?.querySelector<WaPopup>(
                    '#cover-preview',
                );
            const anchor = this.shadowRoot?.querySelector(
                '.cover-art',
            );

            if (popup && anchor) {
                popup.anchor = anchor;
            }
        });
    };

    private handleCoverMouseLeave = () => {
        this.showCoverPreview = false;
    };

    // ===================================================================
    // RESIZE
    // ===================================================================

    private handleMouseDown = (e: MouseEvent) => {
        e.preventDefault();
        this.isDragging = true;
    };

    private handleMouseMove = (e: MouseEvent) => {
        if (!this.isDragging) return;

        const rect = this.getBoundingClientRect();
        const newWidth = e.clientX - rect.left;
        const clampedWidth = Math.min(Math.max(newWidth, MIN_WIDTH), MAX_WIDTH);

        this.updateWidth(clampedWidth);
    };

    private handleMouseUp = () => {
        this.isDragging = false;
    };

    private updateWidth(width: number) {
        const bottomBar = this.closest('.bottom-bar');

        if (bottomBar) {
            (bottomBar as HTMLElement).style.setProperty(
                '--now-playing-width',
                `${width}px`,
            );
        }
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'now-playing': NowPlaying;
    }
}

import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { PlayerController } from '@store/controllers/player-controller';
import { queueStore } from '@store/queue-store';
import type { RepeatMode } from '@store/queue-store';
import { designTokens } from '../../../styles/tokens.css';
import { PHONE_QUERY } from '../../../utils/breakpoints';

/**
 * The transport, in the two places it appears.
 *
 * **The context is a property and cannot be a media query**, which is
 * the whole reason this exists (#56). Everywhere else in this app a
 * component states what it drops at phone width itself, because a media
 * query inside a shadow root is answered by the viewport and that is
 * the honest signal. Here the two hosts want *different* answers at the
 * *same* viewport: on a phone the bottom bar wants three controls sized
 * for a thumb, and `now-playing-view` wants five, larger still. So the
 * host says which context and the viewport says which size band, and
 * neither one alone can express it.
 *
 * Measured at the reference device's 424x439 before this: every button
 * here was **33x21px**, in both places, which is what #56 reports as
 * "the most important thing in the mobile app and they are tiny".
 */
export type ControlsContext = 'bar' | 'full';

@customElement('player-controls')
export class PlayerControls extends LitElement {
  private player = new PlayerController(this);
  private unsubscribeQueue?: () => void;

  /**
   * Where these controls are drawn. `bar` is the bottom bar in both
   * bands; `full` is the full-screen transport.
   *
   * Reflected so a spec can read it and so the stylesheet keys off one
   * fact rather than a class the host has to remember to set.
   */
  @property({ type: String, reflect: true })
  context: ControlsContext = 'bar';

  @state() private shuffleMode = false;
  @state() private repeatMode: RepeatMode = 'off';

  /**
   * Phone width, from `matchMedia` rather than from a media query,
   * because what it decides is whether shuffle and repeat *exist* here
   * — and a stylesheet can only decide whether they are painted.
   * `job-band` and `search-trigger` are the same pattern for the same
   * reason.
   */
  @state() private phone = false;

  private media?: MediaQueryList;

  private onMedia = (e: MediaQueryListEvent) => {
    this.phone = e.matches;
  };

  /** Whether this is the phone's bottom bar, which carries three
   *  controls rather than five. */
  private get slim(): boolean {
    return this.context === 'bar' && this.phone;
  }

  override connectedCallback(): void {
    super.connectedCallback();

    this.media = window.matchMedia(PHONE_QUERY);
    this.phone = this.media.matches;
    this.media.addEventListener('change', this.onMedia);

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
    this.media?.removeEventListener('change', this.onMedia);
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

    /* ---------------------------------------------------------------
       Sizes (#56).

       44px is the floor everything here is sized to, and play/pause
       alone goes above it -- "large play/pause, adequate prev/next" is
       the Direction, and it is the one control the report calls "front
       and centre".

       They are stated as custom properties rather than on each button
       so a context sets two numbers instead of five rules, and so the
       icon scales with its target: a 44px box around a 16px glyph is a
       big hit area that still looks tiny, which is half of what the
       report is about.

       **The desktop bar sets none of them and must not change at all.**
       #56 is an Android issue; the desktop's buttons are 33x21 before
       this and are 33x21 after it.

       That is why the box rules take a zero fallback and the *font-size*
       rules are scoped to the two contexts instead of sharing them. A
       button does not inherit its font from its parent -- the UA
       stylesheet gives it one -- so a generic font-size: inherit is
       not the no-op it reads as: it moved the desktop's buttons from
       33x21 to 36x24, silently, by taking them from the UA's 13.3px to
       the shell's 16px. Measured before and after by stashing this
       file, which is the only way that particular 3px shows up.
       --------------------------------------------------------------- */
    button {
      min-width: var(--yj-control-target, 0);
      min-height: var(--yj-control-target, 0);
    }

    button.play {
      min-width: var(--yj-control-play-target, 0);
      min-height: var(--yj-control-play-target, 0);
    }

    /* The phone's bottom bar: three controls, sized for a thumb.
       Shuffle and repeat are not here -- see the render method, which
       does not draw them rather than hiding them, because a control
       that is display:none is still a thing the component claims to
       have. They are on the full-screen view, which is one tap away
       through the mini player's art (#59). */
    @media (max-width: 599px) {
      :host([context='bar']) {
        --yj-control-target: 44px;
        --yj-control-icon: 18px;
        --yj-control-play-target: 56px;
        --yj-control-play-icon: 24px;
      }

      :host([context='bar']) button {
        font-size: var(--yj-control-icon);
      }

      :host([context='bar']) button.play {
        font-size: var(--yj-control-play-icon);
      }
    }

    /* The full-screen transport, at every width: this view *is* the
       player, so the controls are the page rather than a strip of it. */
    :host([context='full']) {
      --yj-control-target: 44px;
      --yj-control-icon: 20px;
      --yj-control-play-target: 64px;
      --yj-control-play-icon: 28px;
    }

    :host([context='full']) button {
      font-size: var(--yj-control-icon);
    }

    :host([context='full']) button.play {
      font-size: var(--yj-control-play-icon);
    }

    :host([context='full']) #player-control-buttons {
      gap: 12px;
    }

    /* Secondary controls sit below the primary row rather than beside
       it, which is the Direction's shape and is why this is a second
       group in the DOM instead of a CSS order property: visual order
       and focus order have to agree. */
    .secondary {
      display: flex;
      justify-content: center;
      align-items: center;
      gap: 24px;
      margin-top: 8px;
    }

    button:hover {
      color: var(--yj-accent-text, #ffd43b);
    }

    .active {
      color: var(--yj-accent-text, #ffd43b);
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

  /** Shuffle. Secondary: it changes how the queue behaves rather than
   *  what is playing now. */
  private renderShuffle() {
    return html`
      <button
        class=${this.shuffleMode ? 'active' : ''}
        aria-label="Shuffle"
        aria-pressed=${this.shuffleMode}
        @click=${this.handleShuffleClick}
      >
        <wa-icon name="shuffle"></wa-icon>
      </button>
    `;
  }

  /** Repeat, whose label spells the mode out because one icon covers
   *  three states. */
  private renderRepeat() {
    const repeatMode = this.repeatMode;
    const repeatClasses = [
      repeatMode !== 'off' ? 'active' : '',
      repeatMode === 'one' ? 'repeat-one' : '',
    ].filter(Boolean).join(' ');

    return html`
      <button
        class=${repeatClasses}
        aria-label=${`Repeat: ${repeatMode}`}
        aria-pressed=${repeatMode !== 'off'}
        @click=${this.handleRepeatClick}
      >
        <wa-icon name="repeat"></wa-icon>
      </button>
    `;
  }

  /** Previous, play/pause, next — the three that are always drawn, in
   *  every context and at every width. Only play/pause takes the large
   *  size: the Direction asks for "large play/pause, adequate
   *  prev/next", and a row of identical squares says every action here
   *  is equally likely, which is not true of play. */
  private renderPrimary() {
    const playOrPauseIcon = this.player.isPlaying ? 'pause' : 'play';
    const playOrPauseHandler = this.player.isPlaying
      ? this.handlePauseClick
      : this.handlePlayClick;

    return html`
      <button
        aria-label="Previous track"
        @click=${this.handlePreviousClick}
      >
        <wa-icon name="backward-step"></wa-icon>
      </button>
      <button
        class="play"
        aria-label=${this.player.isPlaying ? 'Pause' : 'Play'}
        @click="${playOrPauseHandler}"
      >
        <wa-icon name=${playOrPauseIcon}></wa-icon>
      </button>
      <button
        aria-label="Next track"
        @click=${this.handleNextClick}
      >
        <wa-icon name="forward-step"></wa-icon>
      </button>
    `;
  }

  /**
   * Two arrangements, not two components.
   *
   * `bar` keeps the order it has always had — shuffle, prev, play,
   * next, repeat, one row — so nothing about the desktop bar moves.
   * `full` puts the primary three on their own row with the secondary
   * pair beneath, which the Direction asks for.
   *
   * **The phone's bar draws three buttons rather than hiding two.** A
   * `display: none` control is still in the component's shadow root,
   * still in the accessibility tree's markup, and still something a
   * `shadowAll('button')[4]` finds — so "the phone has three controls"
   * would be true of the pixels and false of the element. They are
   * reachable on the full-screen view, which the mini player's art
   * opens, and through the global shortcuts.
   */
  override render() {
    if (this.context === 'full') {
      return html`
        <div id="player-control-buttons">${this.renderPrimary()}</div>
        <div class="secondary">
          ${this.renderShuffle()}${this.renderRepeat()}
        </div>
      `;
    }

    return html`
      <div id="player-control-buttons">
        ${this.slim ? nothing : this.renderShuffle()}
        ${this.renderPrimary()}
        ${this.slim ? nothing : this.renderRepeat()}
      </div>
    `;
  }
}

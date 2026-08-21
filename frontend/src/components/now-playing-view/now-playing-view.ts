import { LitElement, html, css, nothing } from 'lit';
import { customElement } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '../audio-player/controls/player-controls';
import '../audio-player/seekbar/seek-bar';
import '../audio-player/volume-control/volume-control';
import {
    creditLink,
    albumLink,
    exploreLinkStyles,
} from '@utils/explore-link';
import { PlayerController } from '@store/controllers/player-controller';
import { creditStore } from '@store/credit-store';
import { FavoritesController } from '@store/controllers/favorites-controller';
import { designTokens } from '../../styles/tokens.css';
import { srOnly } from '../../styles/sr-only.css';
import { ICON_QUEUE } from '@utils/icon-language';
import { openQueue as showQueue } from '@utils/open-queue';

/**
 * What is playing, at the size a phone has room for (plan 016 B2,
 * phase 2).
 *
 * Phase 1 took the seek bar and the volume out of the bottom bar,
 * because 4px of height is not a thumb target and a phone's volume
 * belongs to its hardware keys. This is where they went: the same
 * `<seek-bar>`, `<player-controls>` and `<volume-control>` elements the
 * desktop transport uses, given room. **Not copies of them** — a phone
 * layout that reimplements the transport is a second transport to fix
 * every bug in, and the seek bar in particular carries the
 * interpolation rules that took a plan of their own to get right.
 *
 * It is a *detail* view rather than a primary one: it is somewhere you
 * go and come back from, so `index.ts` pushes the current view onto the
 * nav stack and Back pops it. That is also why it is not in the tab
 * bar — a tab you cannot leave by pressing the same tab again is not a
 * tab.
 */
@customElement('now-playing-view')
export class NowPlayingView extends LitElement {
    private player = new PlayerController(this);

    /** Unsubscribes the credit-arrival repaint. */
    private creditsUnsub?: () => void;

    override connectedCallback(): void {
        super.connectedCallback();
        // Credits arrive after the track does, so the name this view is
        // already showing has to be re-rendered when they land.
        this.creditsUnsub = creditStore.subscribe(() => this.requestUpdate());
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.creditsUnsub?.();
        this.creditsUnsub = undefined;
    }
    private favCtrl = new FavoritesController(this);

    static override styles = [designTokens, srOnly, exploreLinkStyles, css`
        :host {
            display: flex;
            flex-direction: column;
            height: 100%;
            box-sizing: border-box;
            padding: 0.75em 1em 1.25em;
            gap: 0.75em;
            background-color: var(--yj-bg-surface, #212529);
            overflow-y: auto;
        }

        header {
            display: flex;
            align-items: center;
            gap: 0.5em;
            flex: 0 0 auto;
        }

        .context {
            flex: 1 1 auto;
        }

        .back {
            background: none;
            border: none;
            color: var(--yj-text-primary, #f8f9fa);
            /* 48px is the touch-target floor, and this is the control
               that gets a user out of a full-screen view. */
            min-width: 48px;
            min-height: 48px;
            font-size: 1.1rem;
            cursor: pointer;
            border-radius: 6px;
        }

        .back:focus-visible {
            outline: 2px solid var(--yj-accent, #ffd43b);
            outline-offset: -2px;
        }

        .context {
            font-size: var(--yj-font-size-xs, 0.75rem);
            letter-spacing: 0.08em;
            text-transform: uppercase;
            color: var(--yj-text-secondary, #adb5bd);
        }

        /* The art and the names are one block, so that a short
           screen can lay them out side by side without either of them
           knowing about the other's box. Vertically it is exactly what
           the host used to do -- same gap, art flexible, names fixed --
           so the tall layout is unchanged. */
        .stack {
            display: flex;
            flex-direction: column;
            gap: 0.75em;
            flex: 1 1 auto;
            min-height: 0;
        }

        .art {
            flex: 1 1 auto;
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 0;
        }

        .art img {
            /* Square, and never larger than the room left over --
               where "square" is a property of what is painted and not
               just of what was asked for.

               The previous rule asked for a square and did not get
               one. width: min(100%, 60vh) makes the width definite,
               aspect-ratio: 1 derives the height from it, and
               max-height: 100% then clamps that height **without
               re-deriving the width** -- which is how the
               aspect-ratio property is specified to behave, unlike
               an intrinsic ratio. So whenever the room left over was
               shorter than the box was wide, the art was drawn as a
               letterbox strip and object-fit: cover cropped the
               cover to it. Measured on the reference device at
               424x439: **264x53**, a 5:1 band of a square image.

               That is not only the phone. The leftover exceeds the
               width only above ~843px of viewport, so every height
               from ~500 to ~843 -- most phones, and any small window
               -- drew a cropped strip too.

               Both maxes with auto sizes is the fix, and it is the
               replaced-element path rather than the aspect-ratio
               one: the used size preserves the ratio under *both*
               bounds (CSS2.1 10.4), so the art is square at every
               height. Checked against Chrome 113 itself -- the
               device's engine -- at column heights of 288, 300, 451,
               600 and 800: square at all five, where the old rule
               cropped at four.

               A corollary worth knowing: auto will not upscale past
               the image's natural size, and the largest tier
               saveCoverArt keeps is 400px. Drawing it larger was
               upscaling, so nothing is lost. */
            max-width: 100%;
            max-height: 100%;
            width: auto;
            height: auto;
            aspect-ratio: 1;
            object-fit: cover;
            border-radius: 12px;
            background-color: var(--yj-bg-elevated, #343a40);
        }

        /* The placeholder is not a replaced element, so it cannot use
           the rule above: with no intrinsic size, auto/auto collapses
           it to its icon -- measured at 13x58 in Chrome 113, which is
           neither square nor the art's size.

           So it is sized from the height, and then bounded by the
           width in the one way a box like this can be. A non-replaced
           element cannot express "the largest square that fits" in a
           single rule: aspect-ratio derives the second axis from the
           first, and whichever max clamps it does not re-derive the
           other, which is the same trap the image rule above is about.
           Driving it from the height alone is right until the column
           is taller than it is wide -- ~843px of viewport, which is a
           tall phone and #51's other named device -- and there it went
           380x484.

           max-height in viewport units is what closes it, and it is
           sound here for the reason 60vh was not: this view is a
           phone-width detail view, so its content box really is the
           viewport less the host's own 1rem gutters. It is a *max*, so
           the failure mode if that ever stopped being true is a square
           bounded slightly early rather than a crop. rem and not em --
           this box sets font-size: 3rem for the icon, so 2em here
           would be 96px. */
        .art .placeholder {
            height: 100%;
            width: auto;
            max-width: 100%;
            max-height: calc(100vw - 2rem);
            /* A flex item's automatic minimum is its content, so
               without this the icon's own width becomes a floor and
               the box goes wider than it is tall the moment the row is
               shorter than the icon -- which is exactly the state a
               job band puts this screen in. */
            min-width: 0;
            aspect-ratio: 1;
            border-radius: 12px;
            background-color: var(--yj-bg-elevated, #343a40);
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 3rem;
            color: var(--yj-text-tertiary, #868e96);
        }

        .meta {
            flex: 0 0 auto;
            display: flex;
            align-items: center;
            gap: 0.75em;
            min-width: 0;
        }

        .names {
            flex: 1 1 auto;
            min-width: 0;
        }

        .title {
            font-size: 1.15rem;
            font-weight: 600;
            margin: 0;
            /* Two lines, then an ellipsis. A marquee is the bottom
               bar's answer to a 320px box; here there is room to wrap,
               and wrapping does not move. */
            display: -webkit-box;
            -webkit-line-clamp: 2;
            -webkit-box-orient: vertical;
            overflow: hidden;
        }

        .artist,
        .album {
            margin: 0;
            font-size: 0.9rem;
            color: var(--yj-text-secondary, #adb5bd);
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }

        .favorite {
            background: none;
            border: none;
            color: var(--yj-text-secondary, #adb5bd);
            min-width: 48px;
            min-height: 48px;
            font-size: 1.25rem;
            cursor: pointer;
            border-radius: 6px;
        }

        .favorite.on {
            color: var(--yj-accent, #ffd43b);
        }

        .favorite:focus-visible {
            outline: 2px solid var(--yj-accent, #ffd43b);
            outline-offset: -2px;
        }

        .transport {
            flex: 0 0 auto;
            display: flex;
            flex-direction: column;
            gap: 0.5em;
        }

        /* The seek bar is the reason this view exists. Its own
           stylesheet thickens the track below the phone breakpoint --
           the track size is set on the wa-slider inside its shadow
           root, so a custom property set from here would not reach
           it. */
        seek-bar {
            display: block;
        }

        .empty {
            flex: 1 1 auto;
            display: flex;
            align-items: center;
            justify-content: center;
            color: var(--yj-text-secondary, #adb5bd);
            text-align: center;
        }

        /* Below 500px of viewport the art and the names sit side by
           side, and that is the whole of this screen's answer to a
           short phone (#51).

           The stacked layout cannot be rescued by sizing alone. Its
           budget is fixed -- 48px of header, 143px of transport since
           #64, 78px of names, 68px of padding and gaps -- so the art
           gets height - 386, which on the reference device's 424x439
           is **53px**. #172 measured 39px before #64 and named the
           two options: give the art a floor and let the block scroll,
           or reflow. A floor scrolls the transport off the bottom,
           and "controls never scroll off" is #51's own Direction and
           plan 018's promise -- so it is the reflow.

           Sideways the art is bounded by the row's height rather than
           by the column's leftover, which is the whole gain: the same
           439px screen goes from a 53px sliver to **143px**, measured
           on the device, with nothing scrolling and the transport
           untouched.

           500 is where the two layouts cross rather than a round
           number. In a row the art is height - 296 and the names get
           what is left of 392px, so the names hold 176px at exactly
           500 and less above it; stacked, the art is height - 386,
           which passes 176px at 562. Below 500 the row is the bigger
           art *and* the readable one -- above it the column is, which
           is why a tall phone (a Pixel 7's ~869) keeps the layout it
           has. Unverified on that device: none was attached.

           It is keyed on height alone, not on the phone's width,
           because it is an answer to vertical room -- a 900x450 window
           has the same problem and the same fix. */
        @media (max-height: 500px) {
            .stack {
                flex-direction: row;
                align-items: center;
            }

            /* A square of the row's height. The box has to carry the
               ratio here rather than the image, because in a row the
               art's width is what the ratio has to produce -- and the
               image's own rule then fits it to a box that is already
               square. */
            .art {
                flex: 0 1 auto;
                height: 100%;
                aspect-ratio: 1;
            }

            .meta {
                flex: 1 1 auto;
            }
        }
    `];

    private back() {
        this.dispatchEvent(new CustomEvent('navigate-back', {
            bubbles: true,
            composed: true,
        }));
    }

    /**
     * Open the queue.
     *
     * This view hides the bottom bar (index.css), and the bar is where
     * the queue button lives -- so without this, going full-screen
     * would take the queue away. It goes through the same helper
     * `index.ts` does, because the panel's state is an attribute on one
     * element and a second mechanism for it is a second thing to keep
     * in step -- which is exactly what this button was: it set `open`
     * directly, so on a phone it produced a queue with no history entry
     * behind it and back moved the page underneath instead (#55).
     */
    private openQueue() {
        showQueue();
    }

    private toggleFavorite() {
        const path = this.player.currentTrack?.filePath;

        if (path) void this.favCtrl.toggleFavorite(path);
    }

    override render() {
        const track = this.player.currentTrack;

        if (!track) {
            return html`
                ${this.renderHeader()}
                <p class="empty" data-testid="npv-empty">
                    Nothing is playing.
                </p>
            `;
        }

        const favorited = this.favCtrl.isFavorited(track.filePath);
        // The largest kept tier, which is what `saveCoverArt` records as
        // the path -- there is no full-resolution original to reach for.
        const art = track.coverArtLarge || track.coverArt;

        return html`
            ${this.renderHeader()}

            <div class="stack">
                <div class="art">
                    ${art
                        ? html`<img
                              src=${art}
                              alt=""
                              decoding="async"
                              data-testid="npv-art"
                          />`
                        : html`<div class="placeholder" aria-hidden="true">
                              <wa-icon name="compact-disc"></wa-icon>
                          </div>`}
                </div>

                <div class="meta">
                    <div class="names">
                        <h2 class="title" data-testid="npv-title">
                            ${track.title || track.fileName}
                        </h2>
                        <p class="artist">
                            ${creditLink(
                                creditStore.credits(track.recordingMbid),
                                track.artist,
                                track.artistMbid,
                            )}
                        </p>
                        ${track.album
                            ? html`<p class="album">
                                  ${albumLink(
                                      track.album,
                                      track.releaseGroupMbid,
                                      undefined,
                                      track.artist,
                                  )}
                              </p>`
                            : nothing}
                    </div>

                    <button
                        type="button"
                        class="favorite ${favorited ? 'on' : ''}"
                        data-testid="npv-favorite"
                        aria-pressed=${favorited ? 'true' : 'false'}
                        aria-label=${favorited
                            ? `Remove ${track.title} from ${this.favCtrl.playlistName}`
                            : `Add ${track.title} to ${this.favCtrl.playlistName}`}
                        @click=${this.toggleFavorite}
                    >
                        <wa-icon name=${this.favCtrl.iconFor(favorited)}></wa-icon>
                    </button>
                </div>
            </div>

            <div class="transport">
                <seek-bar></seek-bar>
                <!-- context="full": this view *is* the player, so the
                     transport is the page rather than a strip of it --
                     primary controls large, shuffle and repeat beneath
                     at normal size (#56). It is a property rather than
                     a media query because the bottom bar wants a
                     different answer at this same viewport. -->
                <player-controls context="full"></player-controls>
                <!-- Rendered unconditionally and absent on its own
                     terms where the device owns the volume (#64): the
                     control asks the player, not this view and not the
                     viewport. A hidden host draws no gap, so that is
                     29px of a 439px screen back to the album art
                     (#172). -->
                <volume-control></volume-control>
            </div>
        `;
    }

    private renderHeader() {
        return html`
            <header>
                <button
                    type="button"
                    class="back"
                    data-testid="npv-back"
                    aria-label="Back"
                    @click=${this.back}
                >
                    <wa-icon name="chevron-down"></wa-icon>
                </button>
                <span class="context">Now playing</span>
                <button
                    type="button"
                    class="back"
                    data-testid="npv-queue"
                    aria-label="Show the queue"
                    @click=${this.openQueue}
                >
                    <wa-icon name=${ICON_QUEUE}></wa-icon>
                </button>
            </header>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'now-playing-view': NowPlayingView;
    }
}

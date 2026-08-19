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

        .art {
            flex: 1 1 auto;
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 0;
        }

        .art img,
        .art .placeholder {
            /* Square, and never taller than the room left over: the
               art is the one thing here that would happily push the
               transport off the bottom of a short phone. */
            width: min(100%, 60vh);
            aspect-ratio: 1;
            object-fit: cover;
            border-radius: 12px;
            background-color: var(--yj-bg-elevated, #343a40);
        }

        .art .placeholder {
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
     * would take the queue away. It toggles the same `open` attribute
     * `index.ts` does, because the panel's state is an attribute on one
     * element and a second mechanism for it is a second thing to keep
     * in step.
     */
    private openQueue() {
        document.getElementById('queue-panel')?.setAttribute('open', '');
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

            <div class="transport">
                <seek-bar></seek-bar>
                <player-controls></player-controls>
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

import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';

import { PlayerController } from '@store/controllers/player-controller';
import { designTokens } from '../../../styles/tokens.css';
import { PHONE_QUERY } from '../../../utils/breakpoints';

/**
 * How far through the song we are, on the border between the mini
 * player and the tab bar (#58).
 *
 * The phone's bottom bar carries three controls and no seek bar — #59
 * took it out, because 4px of height is not a thumb target and the
 * full-screen `now-playing-view` is where seeking belongs. What went
 * with it is the one thing a mini player is expected to say without
 * being opened: how far through the song it is. This is that, and
 * only that.
 *
 * Four things about it are load-bearing.
 *
 * **It is the shell's element, not either bar's.** The mini player and
 * `<bottom-nav>` are separate components stacked in the shell's grid,
 * so a line on the border between them is a row of the grid — either
 * one drawing it means reaching into the other's box for two pixels.
 *
 * **It never counts.** The position is pushed at 1 Hz by the backend
 * (`PlaybackPositionChanged`), and the interval here interpolates
 * *between* those reports and is stopped and restarted by every one of
 * them — the seek bar's rule, for the reason the seek bar has it: a
 * local clock drifted 30 s away from the backend across four keyboard
 * seeks. The `trackChangeId` and `seq` guards come along for the same
 * reason: the store is a singleton, so a report about the previous
 * track must not be adopted, and the same second reported twice still
 * has to reset the interpolation.
 *
 * **It is not a control and cannot become one.** `aria-hidden` on the
 * host and `pointer-events: none` throughout: the real progress is
 * announced by the seek bar on Now Playing, and a 2px strip on the top
 * edge of the tab bar that sometimes seeks is worse than one that
 * never does. It is also where a thumb aiming at a tab lands.
 *
 * **It renders nothing above 600px**, from `matchMedia` rather than a
 * media query, because that decides whether the element *exists* — and
 * with it whether a 1 Hz interval runs for the life of every desktop
 * session about a line nobody can see. `job-band`, `search-trigger`
 * and `player-controls` are the same pattern for the same reason.
 */

/**
 * The reporting cadence, matched. This is not the clock: it exists
 * only so the line moves in the second between two reports, and its
 * error is discarded by the next one rather than carried.
 */
const InterpolationIntervalMillis = 1000;

@customElement('player-progress-line')
export class PlayerProgressLine extends LitElement {
    private player = new PlayerController(this);

    /** Phone width. See the class comment: existence, not paint. */
    @state() private phone = false;

    /** Seconds into the track, from the last report plus interpolation. */
    @state() private elapsed = 0;

    private previousTrackChangeId = -1;

    /** The sequence number of the last backend report applied. */
    private previousPositionSeq = -1;

    private timerID = -1;

    private media?: MediaQueryList;

    private onMedia = (e: MediaQueryListEvent) => {
        this.phone = e.matches;
    };

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
                /* Not a target, at any depth. */
                pointer-events: none;
            }

            .track {
                height: 2px;
                background-color: var(--yj-bg-surface, #212529);
            }

            .fill {
                height: 100%;
                background-color: var(--yj-accent, #ffd43b);
                /* scaleX off a full-width box rather than a width in
                   percent, so the moving thing is a transform and the
                   line costs no layout once a second. */
                transform-origin: left center;
            }
        `,
    ];

    private get trackLength(): number {
        return this.player.currentTrack?.trackLength ?? 0;
    }

    override connectedCallback(): void {
        super.connectedCallback();

        // Decorative in full: the seek bar on Now Playing is what
        // announces the position, and this says the same thing without
        // a name, a value or a way to act on it.
        this.setAttribute('aria-hidden', 'true');

        this.media = window.matchMedia(PHONE_QUERY);
        this.phone = this.media.matches;
        this.media.addEventListener('change', this.onMedia);
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.stopInterpolating();
        this.media?.removeEventListener('change', this.onMedia);
    }

    override updated(): void {
        // A track change resets the line, and `trackChangeId` is what
        // reveals one when the same file plays twice in a row.
        const currentChangeId = this.player.currentTrack?.trackChangeId ?? -1;

        if (currentChangeId !== this.previousTrackChangeId) {
            this.previousTrackChangeId = currentChangeId;
            this.elapsed = this.player.currentTrack?.seekPosition ?? 0;
            this.stopInterpolating();
        }

        // The backend's own position wins over anything counted here,
        // and a report for a track that is no longer loaded is stale by
        // definition.
        const position = this.player.position;

        if (
            position &&
            position.trackChangeId === currentChangeId &&
            position.seq !== this.previousPositionSeq
        ) {
            this.previousPositionSeq = position.seq;
            this.elapsed = position.positionSeconds;
            this.stopInterpolating();
        }

        // One owner for the interval, as in `seek-bar`: everything that
        // wants it started or stopped says so by changing state that
        // brings us back here.
        if (this.phone && this.player.isPlaying && currentChangeId !== -1) {
            this.startInterpolating();
        } else {
            this.stopInterpolating();
        }
    }

    private stopInterpolating(): void {
        if (this.timerID !== -1) {
            clearInterval(this.timerID);
            this.timerID = -1;
        }
    }

    private startInterpolating(): void {
        if (this.timerID !== -1) {
            return;
        }

        this.timerID = window.setInterval(() => {
            if (this.elapsed < this.trackLength) {
                this.elapsed += 1;
            }
        }, InterpolationIntervalMillis);
    }

    override render() {
        // Nothing playing is nothing to say, and the grid row is `auto`
        // so an empty render costs no height at all -- `job-band`'s
        // rule one row down.
        if (!this.phone || this.player.currentTrack === null) return nothing;

        const length = this.trackLength;
        const fraction =
            length > 0 ? Math.min(1, Math.max(0, this.elapsed / length)) : 0;

        return html`
            <div class="track" data-testid="progress-line">
                <div class="fill" style="transform: scaleX(${fraction})"></div>
            </div>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'player-progress-line': PlayerProgressLine;
    }
}

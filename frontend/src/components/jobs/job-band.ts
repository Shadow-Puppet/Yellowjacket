/**
 * The phone's view of background work (#62).
 *
 * The header `job-indicator` is a *popover*, anchored to a bar 3.25em
 * tall on a screen 439 CSS px tall, and it was reported as unreadable
 * behind other UI. Two things are wrong with it there regardless of
 * that symptom: a popover is a **disclosure**, and background work is
 * the one thing a phone should not make you open something to see; and
 * #57 deletes the bar it is anchored to, and is blocked on this issue
 * precisely because the indicator needs somewhere else to live first.
 *
 * This is that somewhere. Below 600px the indicator stands down
 * (`index.css`) and its work appears here instead.
 *
 * Four things about it are load-bearing.
 *
 * **It is the existing `job-panel`, not a second job UI.** Pause,
 * cancel, Details and the log all come along — and, more to the point,
 * so does `applyJobControl`, which is what carries the "you will
 * discard hours of downloading" confirmation for an index build. A
 * host drawing its own buttons drops that silently, which is the trap
 * #27 already named.
 *
 * **It is in the layout, not over it**, and that was measured rather
 * than assumed. The first version of this put the panel in
 * `notification-host`'s fixed band, which reads fine in a screenshot
 * and is unusable: at 424x439 a compact panel is ~200px of a 439px
 * screen, and it *covers* what is under it. Four e2e specs failed —
 * two phone-shell journeys and the header's action menu — because the
 * panel was intercepting the taps. A band that hides the app to tell
 * you the app is busy is worse than the popover it replaced. In flow
 * it pushes instead, so nothing is covered and nothing is unreachable,
 * which is #24's one sentence across all three bands.
 *
 * **It shows active work only.** A finished row that lingers is a
 * banner that stays after the work is done, which is the opposite of
 * what #62 asks for ("dismissed automatically on completion") and, in
 * flow, is furniture that keeps the content pushed down. Finished jobs
 * are still shown where the work was started, which is #27's rule and
 * unaffected.
 *
 * **It renders nothing at all above 600px**, from `matchMedia` rather
 * than a media query, because this decides whether the element
 * *exists*. `bottom-nav` learned that the expensive way: rendering its
 * duplicate `<app-sidebar>` unconditionally put a second copy of every
 * `nav-*` testid in the DOM and broke 30 specs on a viewport where it
 * was not even visible. Settings already holds four `job-panel`s, so a
 * fifth answering for *every* kind is the same trap.
 */
import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';

import { jobStore } from '@store/job-store';
import { isTerminal } from '@store/job-store';
import { designTokens } from '../../styles/tokens.css';
import { PHONE_QUERY } from '../../utils/breakpoints';
import './job-panel';

@customElement('job-band')
export class JobBand extends LitElement {
    @state() private phone = false;

    @state() private active = 0;

    private media?: MediaQueryList;

    private unsubscribe?: () => void;

    static override styles = [
        designTokens,
        css`
            :host {
                display: block;
                min-width: 0;
            }

            /* The panel's own margin is for a settings section; here the
               band owns the spacing. */
            job-panel {
                margin-top: 0;
                padding: 0 0.5em 0.5em;
            }
        `,
    ];

    private onMedia = (e: MediaQueryListEvent | MediaQueryList) => {
        this.phone = e.matches;
    };

    private onJobs = () => {
        this.active = jobStore.jobs.filter((job) => !isTerminal(job)).length;
    };

    override connectedCallback(): void {
        super.connectedCallback();

        this.media = window.matchMedia(PHONE_QUERY);
        this.phone = this.media.matches;
        this.media.addEventListener('change', this.onMedia);

        // The band decides whether to render *at all*, and a panel that
        // hides itself cannot tell its host that.
        this.unsubscribe = jobStore.subscribe(this.onJobs);
        this.onJobs();
        void jobStore.init();
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.unsubscribe?.();
        this.media?.removeEventListener('change', this.onMedia);
    }

    override render() {
        // `hidden` rather than an empty render, so the grid row this
        // sits in costs nothing at all while there is no work -- the
        // rule `job-panel` already follows one layer down.
        this.hidden = !(this.phone && this.active > 0);

        if (this.hidden) return nothing;

        return html`
            <job-panel kinds="*" density="compact" active-only></job-panel>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'job-band': JobBand;
    }
}

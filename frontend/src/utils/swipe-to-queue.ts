import { css, html, nothing } from 'lit';
import type { ReactiveController, ReactiveControllerHost } from 'lit';
import { classMap } from 'lit/directives/class-map.js';

import { queueStore } from '@store/queue-store';
import { ICON_QUEUE } from '@utils/icon-language';
import type { SwipeEvent } from '@utils/touch-gestures';

/**
 * Swipe a row right to add it to the queue (plan 019, #63).
 *
 * This is the *affordance* and the arithmetic, written once, because
 * three lists want it: `track-list` and both playlist detail views.
 * It was `track-list`'s own for one phase and is here rather than
 * copied twice, on the rule the rest of this app is built on — three
 * copies of "how far is far enough" is three chances for them to
 * disagree, which is what `utils/library-status.ts` and
 * `utils/ownership.ts` each exist to have stopped happening.
 *
 * The host keeps three things: what a row *is*, what a swipe on it
 * would queue, and what to call it afterwards. Everything else —
 * the threshold, the reveal, the settle, the announcement, the
 * repaint — is here.
 *
 * **The queue panel deliberately does not use it.** A right swipe means
 * *add to the queue* everywhere it exists, and a queue row is already
 * in the queue; the only thing it could sensibly mean there is
 * *remove*, which is the same gesture with the opposite effect one
 * screen away. Removing a queue row is on its own row (the ×), on its
 * bottom sheet since #60, and on the selection bar #63 gave it.
 *
 * Five things about it are load-bearing.
 *
 * **Both halves of the device fix are here or next door.**
 * `swipeRevealStyles` carries `touch-action: pan-y` on `[data-swipe]`,
 * and `utils/touch-gestures.ts` carries the non-passive
 * `preventDefault`. Chrome 113's WebView cancels the pointer stream
 * ~16px into any drag whatever `touch-action` says, and with the
 * `preventDefault` alone but `touch-action` at `auto` the gesture dies
 * after one move. Neither works without the other and **both are
 * correct in Chromium either way**, which is why the component tier
 * asserts the stylesheet rather than the rendering.
 *
 * **The row does not move; its cells do.** A row here is
 * `contain: strict` with `overflow: hidden`, so translating the row
 * and counter-translating a pane inside it puts that pane at a
 * negative offset inside a clipping box, where it is simply not
 * painted. Sliding the children instead leaves the pane where it was
 * drawn, clips the cells off the right edge, and needs no wrapper
 * element in a row that is already a grid.
 *
 * **The travel is written to the row's own style, never rendered.**
 * One render when the gesture starts, one when it crosses the
 * threshold, one when it ends — a virtualizer re-rendering every
 * visible row per frame of one finger's travel is exactly what audit
 * `perf.m1` is about.
 *
 * **The threshold is a fraction of the row**, with a floor. The row is
 * 424x52 on the reference device, so a threshold in bare pixels is a
 * fraction of a row height on one screen and a third of the width on
 * the next.
 *
 * **It is not only a colour** (WCAG 1.4.1, the rule the playing-row
 * marker exists for). The pane carries the queue icon and words, the
 * words change at the threshold, and the outcome goes to a live
 * region — one glyph throughout, because a tick is `ICON_IN_LIBRARY`
 * and means *you own this*.
 */

/** How far along the row a swipe has to reach to mean it. */
export const SWIPE_COMMIT_FRACTION = 0.3;

/** … and a floor, for a narrow list embedded in a detail page. */
export const SWIPE_COMMIT_MIN_PX = 72;

/** How long the reveal holds its confirmation before snapping back. */
const CONFIRM_MS = 550;

/** The snap itself. `swipeRevealStyles` states the same number. */
const SETTLE_MS = 180;

/** What a swipe on one row would do, as the host understands it. */
export interface SwipeTarget {
    /** Which row draws the reveal. */
    index: number;

    /** The file paths a commit queues, in the order they are shown. */
    filePaths: string[];

    /** What to call a single track when saying it was added. */
    label: string;
}

export interface SwipeToQueueOptions {
    /**
     * The row the gesture is on, or null for anything that is not a
     * swipeable row — a header, a gap, a track with no file.
     */
    resolve(e: SwipeEvent): SwipeTarget | null;

    /**
     * Repaint the rows. A `<lit-virtualizer>` renders through the
     * `virtualize` directive and reacts to its *own* properties, so a
     * host update alone leaves the rows exactly as they were.
     */
    repaint(): void;
}

export class SwipeToQueue implements ReactiveController {
    private host: ReactiveControllerHost;
    private opts: SwipeToQueueOptions;

    /** Which row is being swiped, and therefore draws a reveal. */
    private index: number | null = null;

    /** Past the commit threshold: the reveal says so, in words. */
    private armed = false;

    /** Committed, and holding its confirmation. */
    private done = false;

    private row: HTMLElement | null = null;
    private keys: string[] = [];
    private commitPx = 0;
    private settleTimer = 0;

    /** What the gesture did, for anyone not watching the row. */
    announcement = '';

    constructor(host: ReactiveControllerHost, opts: SwipeToQueueOptions) {
        this.host = host;
        this.opts = opts;
        host.addController(this);
    }

    hostConnected(): void {
        // No-op; state is component-local.
    }

    hostDisconnected(): void {
        window.clearTimeout(this.settleTimer);
        this.forget();
    }

    /** Whether this row is the one under the finger. */
    isSwiping(index: number): boolean {
        return this.index === index;
    }

    onSwipeStart = (e: SwipeEvent): void => {
        // Rightward only. Nothing is bound to a leftward swipe, and
        // claiming one would take a gesture away to do nothing with it.
        if (e.detail.dx <= 0) return;

        const target = this.opts.resolve(e);

        if (!target || target.filePaths.length === 0) return;

        const row = (e.target as HTMLElement).closest(
            '[data-swipe]',
        ) as HTMLElement | null;

        if (!row) return;

        e.preventDefault();

        this.row = row;
        this.keys = target.filePaths;
        this.trackLabel = target.label;
        this.commitPx = Math.max(
            SWIPE_COMMIT_MIN_PX,
            row.getBoundingClientRect().width * SWIPE_COMMIT_FRACTION,
        );
        this.armed = false;
        this.done = false;
        this.index = target.index;
        this.host.requestUpdate();
        this.opts.repaint();
        this.offset(0);
    };

    onSwipeMove = (e: SwipeEvent): void => {
        if (this.index === null) return;

        const dx = Math.min(Math.max(e.detail.dx, 0), this.commitPx * 2);
        const armed = dx >= this.commitPx;

        if (armed !== this.armed) {
            this.armed = armed;
            this.host.requestUpdate();
            this.opts.repaint();
        }

        this.offset(dx);
    };

    onSwipeEnd = (e: SwipeEvent): void => {
        if (this.index === null) return;

        if (e.detail.canceled || e.detail.dx < this.commitPx) {
            this.settle(0);

            return;
        }

        queueStore.addTracksToQueue(this.keys);

        const count = this.keys.length;

        // The reveal is the only thing on screen that says this
        // happened -- the queue panel may well be closed -- so it holds
        // its confirmation for a moment rather than vanishing the
        // instant the finger lifts.
        this.done = true;
        this.announcement =
            count === 1
                ? `Added ${this.label()} to the queue.`
                : `Added ${count} tracks to the queue.`;
        this.host.requestUpdate();
        this.opts.repaint();
        this.settle(CONFIRM_MS);
    };

    /** What is revealed behind the row, in three states. */
    renderReveal(index: number) {
        if (this.index !== index) return nothing;

        const count = this.keys.length;
        const what = count === 1 ? 'to queue' : `${count} tracks to queue`;
        const words = this.done
            ? 'Added'
            : this.armed
                ? 'Release to add'
                : `Add ${what}`;

        return html`
            <div
                class=${classMap({ 'swipe-reveal': true, armed: this.armed })}
                aria-hidden="true"
                data-testid="swipe-reveal"
            >
                <wa-icon name=${ICON_QUEUE}></wa-icon>
                <span>${words}</span>
            </div>
        `;
    }

    /**
     * What to call a single track, taken when the gesture starts.
     *
     * Held rather than looked up at the end, because a swipe outlives
     * a refetch: the store replaces its array when a play count
     * changes, which is once a song.
     */
    private trackLabel = '';

    private label(): string {
        return this.trackLabel === '' ? 'the track' : this.trackLabel;
    }

    /** Write the travel to the row itself, with no render. */
    private offset(dx: number): void {
        this.row?.style.setProperty('--yj-swipe-dx', `${dx}px`);
    }

    /**
     * Put the row back, after `delay`, and forget the swipe.
     *
     * The row element is held rather than looked up again: a
     * virtualizer recycles its rows, and by the time this runs the
     * element may be drawing a different track. Clearing the property
     * off whatever it holds now is right either way, since `index` is
     * what decides who draws the reveal.
     */
    private settle(delay: number): void {
        const row = this.row;

        window.clearTimeout(this.settleTimer);

        this.settleTimer = window.setTimeout(() => {
            row?.classList.add('settling');
            this.offset(0);

            this.settleTimer = window.setTimeout(() => {
                row?.classList.remove('settling');
                row?.style.removeProperty('--yj-swipe-dx');
                this.forget();
                this.host.requestUpdate();
                this.opts.repaint();
            }, SETTLE_MS);
        }, delay);
    }

    private forget(): void {
        this.row = null;
        this.index = null;
        this.armed = false;
        this.done = false;
    }
}

/**
 * The reveal, and the `touch-action` half of what makes the gesture
 * reach us on the device.
 *
 * Keyed on `[data-swipe]` rather than on a class name, so one
 * stylesheet serves three lists whose rows are called three different
 * things.
 */
export const swipeRevealStyles = css`
    /* Half of what makes the gesture reach us on Chrome 113's WebView:
       auto lets it commit to a horizontal pan on the first move past
       slop, and the pointer stream is cancelled before any threshold
       can be crossed. The other half is the non-passive preventDefault
       in utils/touch-gestures.ts, and neither works alone -- both were
       measured three ways on the phone. Never none: that takes the
       list's own vertical scrolling with it. */
    [data-swipe] {
        touch-action: pan-y;
    }

    .swipe-reveal {
        position: absolute;
        left: 0;
        top: 0;
        bottom: 0;
        width: var(--yj-swipe-dx, 0px);
        box-sizing: border-box;
        display: flex;
        align-items: center;
        gap: 0.4em;
        padding-left: 8px;
        overflow: hidden;
        white-space: nowrap;
        pointer-events: none;
        font-size: var(--yj-text-xs);
        background-color: var(--yj-bg-elevated, #343a40);
        color: var(--yj-text-secondary, #b3b3b3);
    }

    .swipe-reveal.armed {
        background-color: var(--yj-success, #2f9e44);
        color: var(--yj-success-fg, #fff);
    }

    /* The children move, not the row -- see the header. */
    [data-swipe].swiping > :not(.swipe-reveal) {
        transform: translateX(var(--yj-swipe-dx, 0px));
    }

    [data-swipe].settling > * {
        transition:
            transform 160ms ease-out,
            width 160ms ease-out;
    }

    @media (prefers-reduced-motion: reduce) {
        [data-swipe].settling > * {
            transition: none;
        }
    }
`;

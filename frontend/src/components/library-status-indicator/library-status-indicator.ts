import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { toggleRequest } from '@utils/library-status';
import { notificationStore } from '@store/notification-store';
import { describeError } from '@utils/describe-error';

/**
 * Library status for an entity (artist, album, or track).
 *
 *  - `in-library`: the entity is already in the user's local library.
 *  - `partial`: some of it is here and the rest is known to be missing
 *    — an album whose files declare twelve tracks where nine are held.
 *    Only ever correct when that total is *known*; see `owned` /
 *    `expected` below.
 *  - `queued`: the entity has been handed off to a download client but
 *    hasn't arrived yet.  Reserved for future download-client plumbing.
 *  - `not-in-library` (default): the entity is not owned and has not
 *    been requested.
 */
export type LibraryStatus =
    | 'in-library'
    | 'partial'
    | 'queued'
    | 'not-in-library';

/**
 * Tri-state library status indicator: a small circular badge embedded
 * in track rows, album cards, and artist cards.
 *
 * **It is a button only where it can act, and a badge everywhere
 * else.**  It used to be a `<button>` whose click handler was a
 * `stopPropagation()` and a comment saying to wire up the download
 * client later, so an Explore results page offered 20 keyboard stops
 * (of 66) that promised an action and performed none.  007 made it
 * `role="img"` for that reason and wrote down what would change the
 * answer: a `<button>` again *with* a handler, never a handler bolted
 * onto something already shaped like one.
 *
 * A call site opts in by passing `request-mbid`.  Where it does, this
 * is a `<button>` that toggles a durable **request** — and the copy
 * says so, because clicking still adds nothing to the library.  Where
 * it does not (`explore-album-details`'s header, which has "Want this"
 * in words directly below it) it stays exactly what it was.
 *
 * An `in-library` badge is never a button under either: there is
 * nothing left to ask for.
 *
 * Colours and glyphs:
 *  - in-library    → green circle, check mark
 *  - queued        → amber circle, hourglass
 *  - not-in-library → grey circle, plus sign
 *
 * Usage:
 *
 *   <library-status-indicator
 *       status="in-library"
 *       entity-type="album"
 *       label="Abbey Road"
 *   ></library-status-indicator>
 */
@customElement('library-status-indicator')
export class LibraryStatusIndicator extends LitElement {
    /** Current status. */
    @property({ type: String })
    status: LibraryStatus = 'not-in-library';

    /**
     * Entity kind for tooltip/aria-label phrasing.  Purely cosmetic
     * right now but required so the label text makes sense regardless
     * of where the indicator is rendered.
     */
    @property({ type: String, attribute: 'entity-type' })
    entityType: 'artist' | 'album' | 'track' = 'track';

    /** Optional label of the entity — used for the tooltip text. */
    @property({ type: String })
    label = '';

    /** Render size in CSS pixels. Default is 20. */
    @property({ type: Number })
    size = 20;

    /**
     * How many of `expected` are held, for `status="partial"`.
     *
     * These are only meaningful when the caller *knows* the total. A
     * tag that never declared one is a third state, and a caller with
     * no total must pass `in-library`, not a ring at 0% — most of an
     * untagged library would otherwise wear an incompleteness mark
     * nothing in the data supports.
     */
    @property({ type: Number })
    owned = 0;

    /** The declared track total behind `owned`. */
    @property({ type: Number })
    expected = 0;

    /**
     * MBID to request when this is clicked.  Supplying it is what makes
     * this a control; omitting it leaves a badge.  Only `album` and
     * `track` are requestable — see `utils/library-status.ts`.
     */
    @property({ type: String, attribute: 'request-mbid' })
    requestMbid = '';

    /** Display-cache artist for the request list. Matching is by MBID. */
    @property({ type: String, attribute: 'request-artist' })
    requestArtist = '';

    @state()
    private busy = false;

    /**
     * True when this can act: a call site opted in, and there is
     * something left to ask for.
     *
     * `partial` is deliberately actionable — an album you hold nine of
     * twelve tracks of has three left to request, which is exactly the
     * case worth asking about.  Only `in-library` is complete enough to
     * have nothing to ask for.
     */
    private get actionable(): boolean {
        return (
            this.requestMbid !== '' &&
            this.status !== 'in-library' &&
            this.entityType !== 'artist'
        );
    }

    static override styles = css`
        :host {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            --indicator-size: 20px;
            --indicator-bg: transparent;
            --indicator-fg: #fff;
            --indicator-border: transparent;
        }

        :host([status='in-library']) {
            --indicator-bg: #1db954;
            --indicator-fg: #000;
        }

        :host([status='queued']) {
            --indicator-bg: #f5a623;
            --indicator-fg: #000;
        }

        /* The ring draws its own arc, so the badge behind it stays
         * empty rather than taking a fill that would show through. */
        :host([status='partial']) {
            --indicator-bg: transparent;
            --indicator-fg: #f5a623;
        }

        svg {
            width: 100%;
            height: 100%;
            /* Start the arc at twelve o'clock; SVG angles start east. */
            transform: rotate(-90deg);
        }

        circle {
            fill: none;
            stroke-width: 3;
        }

        .ring-track {
            stroke: rgba(255, 255, 255, 0.18);
        }

        .ring-fill {
            stroke: #f5a623;
            stroke-linecap: round;
        }

        :host([status='not-in-library']) {
            --indicator-bg: rgba(255, 255, 255, 0.08);
            --indicator-fg: rgba(255, 255, 255, 0.65);
            --indicator-border: rgba(255, 255, 255, 0.2);
        }

        .badge {
            /* A <button> gets box-sizing: border-box from the UA
             * stylesheet and a <span> does not, so dropping the button
             * grew the badge by its 1px border on each side — 36px to
             * 38px, caught by the stored screenshot. Set explicitly so
             * the two branches of render() are the same size. */
            box-sizing: border-box;
            width: var(--indicator-size);
            height: var(--indicator-size);
            min-width: var(--indicator-size);
            min-height: var(--indicator-size);
            border-radius: 50%;
            background: var(--indicator-bg);
            color: var(--indicator-fg);
            border: 1px solid var(--indicator-border);
            padding: 0;
            display: inline-flex;
            align-items: center;
            justify-content: center;
            -webkit-tap-highlight-color: transparent;
        }

        wa-icon {
            font-size: calc(var(--indicator-size) * 0.55);
            line-height: 1;
            pointer-events: none;
        }

        button.badge {
            cursor: pointer;
            font: inherit;
        }

        button.badge:hover:not(:disabled) {
            filter: brightness(1.25);
        }

        button.badge:disabled {
            cursor: default;
            opacity: 0.6;
        }

        /* The card underneath draws its own focus ring, and this sits
         * inside it — so the badge needs one of its own or a keyboard
         * user cannot tell which of the two has focus. */
        button.badge:focus-visible {
            outline: 2px solid var(--yj-accent, #ffd43b);
            outline-offset: 2px;
        }

        /* Prevent the button from intercepting drag gestures on album
         * cards — the parent typically owns the drag behaviour. */
        :host {
            user-select: none;
        }
    `;

    private iconName(): string {
        switch (this.status) {
            case 'in-library':
                return 'check';
            case 'queued':
                return 'hourglass-half';
            default:
                return 'plus';
        }
    }

    /** The held fraction, clamped — extras do not overfill the ring. */
    private fraction(): number {
        if (this.expected <= 0) return 0;

        return Math.min(1, Math.max(0, this.owned / this.expected));
    }

    private tooltip(): string {
        const kind =
            this.entityType === 'album'
                ? 'album'
                : this.entityType === 'artist'
                  ? 'artist'
                  : 'track';
        const name = this.label ? ` "${this.label}"` : '';

        // A control is named after what activating it does; a badge is
        // named after what it is. Both are still deliberately about the
        // *request list* rather than the library — clicking this adds a
        // row to one and nothing to the other, and "Add … to library"
        // was the old button's promise written into the copy.
        if (this.actionable) {
            return this.status === 'queued'
                ? `Cancel the request for ${kind}${name}`
                : `Want ${kind}${name}`;
        }

        switch (this.status) {
            case 'in-library':
                return `${capitalize(kind)}${name} is in your library`;
            case 'partial':
                // The count is the whole point — a ring alone says
                // "some" to a sighted user and nothing to anyone else.
                return `${this.owned} of ${this.expected} tracks of ${kind}${name} are in your library`;
            case 'queued':
                return `${capitalize(kind)}${name} is queued for download`;
            default:
                return `${capitalize(kind)}${name} is not in your library`;
        }
    }

    /**
     * Toggle the request.
     *
     * The click is swallowed, which it was before too — but for the
     * opposite reason. 007 removed a `stopPropagation()` that guarded
     * nothing, on the rule that with no action of its own the badge is
     * part of its card and a click on it should mean what the card
     * means. Now it has one, so it does not.
     */
    private async onActivate(event: Event) {
        event.stopPropagation();
        event.preventDefault();

        if (this.busy || !this.actionable) return;

        this.busy = true;

        try {
            await toggleRequest({
                mbid: this.requestMbid,
                entity: this.entityType === 'album' ? 'album' : 'track',
                title: this.label,
                artist: this.requestArtist,
            });
        } catch (err) {
            console.error('could not update the request list', err);

            // Transient: the badge visibly stayed where it was, so
            // there is nothing for the user to do about it that they
            // are not already doing.
            notificationStore.transient({
                text: describeError(err, 'That request could not be updated.'),
                tone: 'error',
            });
        } finally {
            this.busy = false;
        }
    }

    /**
     * Keep Enter and Space from reaching the card underneath.
     *
     * A `<button>` fires `click` on both by itself, so this only has to
     * stop the keydown propagating — every card holding one of these is
     * a `role="button"` or `role="option"` with its own Enter/Space
     * handler, and without this a keyboard activation would both file
     * the request and open the page.
     */
    private onKeydown(event: KeyboardEvent) {
        if (event.key === 'Enter' || event.key === ' ') {
            event.stopPropagation();
        }
    }

    override render() {
        // Sync the host CSS variable with the configured size.
        if (this.size && this.size !== 20) {
            this.style.setProperty('--indicator-size', `${this.size}px`);
        }

        const title = this.tooltip();
        const icon = this.iconName()
            ? html`<wa-icon name=${this.iconName()} aria-hidden="true"></wa-icon>`
            : nothing;

        if (this.actionable) {
            return html`
                <button
                    class="badge"
                    type="button"
                    title=${title}
                    aria-label=${title}
                    ?disabled=${this.busy}
                    @click=${this.onActivate}
                    @keydown=${this.onKeydown}
                >
                    ${icon}
                </button>
            `;
        }

        // The ring stands in for the icon wherever the icon would go —
        // including inside the button, because a partly-held album is
        // actionable (it has tracks left to request) and must still
        // show how much of it is here.
        const glyph = this.status === 'partial'
            ? this.renderRing()
            : this.iconName()
              ? html`<wa-icon name=${this.iconName()} aria-hidden="true"></wa-icon>`
              : nothing;

        if (this.actionable) {
            return html`
                <button
                    class="badge"
                    type="button"
                    title=${title}
                    aria-label=${title}
                    ?disabled=${this.busy}
                    @click=${this.onActivate}
                    @keydown=${this.onKeydown}
                >
                    ${glyph}
                </button>
            `;
        }

        return html`
            <span class="badge" role="img" title=${title} aria-label=${title}>
                ${glyph}
            </span>
        `;
    }

    /**
     * The progress arc.  Drawn as a stroked circle rather than a conic
     * gradient so the ring keeps a constant width at every `size` and
     * the arc's ends stay round.
     */
    private renderRing() {
        const radius = 8;
        const circumference = 2 * Math.PI * radius;
        const offset = circumference * (1 - this.fraction());

        return html`
            <svg viewBox="0 0 20 20" aria-hidden="true">
                <circle class="ring-track" cx="10" cy="10" r=${radius}></circle>
                <circle
                    class="ring-fill"
                    cx="10"
                    cy="10"
                    r=${radius}
                    stroke-dasharray=${circumference}
                    stroke-dashoffset=${offset}
                ></circle>
            </svg>
        `;
    }
}

function capitalize(s: string): string {
    return s.length > 0 ? (s[0] ?? '').toUpperCase() + s.slice(1) : s;
}

declare global {
    interface HTMLElementTagNameMap {
        'library-status-indicator': LibraryStatusIndicator;
    }
}

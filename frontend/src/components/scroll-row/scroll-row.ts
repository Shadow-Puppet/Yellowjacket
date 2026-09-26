import { LitElement, css, html } from 'lit';
import { customElement, query, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

/** How far one press moves the row — most of a screenful, not all of
 *  it, so the card that was at the edge stays as an anchor. */
const SCROLL_FRACTION = 0.8;

/**
 * A horizontally scrolling row with arrow buttons.
 *
 * The shelves, the search results and (now) the artist page's
 * discography and similar-artists rows are all "more than fits, scroll
 * sideways". Until this existed the only way to see the rest was a
 * mousewheel or a trackpad gesture, which is not an affordance — a
 * mouse with no horizontal wheel simply could not reach the cards past
 * the fold.
 *
 * It is a component rather than a rule on `.horizontal-row` for two
 * reasons. The arrows are *state* — which way the row can still move —
 * and that state has to be recomputed when the viewport resizes or a
 * card arrives with its cover art; a stylesheet cannot do that. And
 * every caller then gets the same arrows, the same reveal and the same
 * keyboard labels without writing them again.
 *
 * **The arrows are `hidden`, not merely transparent, at the end they
 * cannot move from** — a control that cannot act is worse than none,
 * and an invisible one still holds a hit area and a tab stop. On a
 * pointer device the pair fades in with the row's hover; where there is
 * no hover they are always visible, because there is no other route to
 * them there (a swipe is not an affordance a mouse-less keyboard user
 * has either).
 *
 * The cards are light DOM children and stay in the *host's* shadow
 * root, so the host's own `.album-card` / `.artist-card` styles apply
 * unchanged — this component only owns the box they scroll inside.
 */
@customElement('scroll-row')
export class ScrollRow extends LitElement {
    @query('.viewport') private viewport?: HTMLElement;

    @state() private atStart = true;

    @state() private atEnd = true;

    @state() private overflowing = false;

    private observer?: ResizeObserver;

    static override styles = css`
        :host {
            display: block;
            position: relative;
        }

        .viewport {
            overflow-x: auto;
            overflow-y: hidden;
            scrollbar-width: none;
            /* A swipe that reaches the row's end should not drag the
               whole page sideways with it. */
            overscroll-behavior-x: contain;
        }

        .viewport::-webkit-scrollbar {
            display: none;
        }

        .track {
            display: flex;
            gap: 12px;
        }

        .arrow {
            position: absolute;
            top: 50%;
            transform: translateY(-50%);
            z-index: 2;
            display: flex;
            align-items: center;
            justify-content: center;
            width: 36px;
            height: 36px;
            padding: 0;
            border-radius: 50%;
            border: 1px solid var(--yj-border-subtle, rgba(255, 255, 255, 0.1));
            background: var(--yj-bg-elevated, #343a40);
            color: var(--yj-text-primary, #fff);
            cursor: pointer;
            opacity: 0;
            transition: opacity 0.15s ease;
        }

        .arrow[hidden] {
            display: none;
        }

        .arrow.prev {
            left: 4px;
        }

        .arrow.next {
            right: 4px;
        }

        .arrow:hover {
            background: var(--yj-bg-overlay, rgba(255, 255, 255, 0.12));
        }

        .arrow:focus-visible {
            outline: 2px solid var(--yj-accent, #ffd43b);
            outline-offset: 2px;
        }

        @media (hover: hover) and (pointer: fine) {
            :host(:hover) .arrow,
            .arrow:focus-visible {
                opacity: 1;
            }
        }

        @media not all and (hover: hover) {
            .arrow {
                opacity: 1;
            }
        }
    `;

    override firstUpdated(): void {
        const viewport = this.viewport;

        if (!viewport) return;

        this.observer = new ResizeObserver(() => this.measure());

        this.observer.observe(viewport);

        // The track's own size is what changes when a card arrives with
        // its cover art, and a ResizeObserver on the viewport alone
        // never fires for that.
        const track = viewport.firstElementChild;

        if (track) this.observer.observe(track);

        this.measure();
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.observer?.disconnect();
        this.observer = undefined;
    }

    private measure(): void {
        const viewport = this.viewport;

        if (!viewport) return;

        this.overflowing = viewport.scrollWidth > viewport.clientWidth + 1;
        this.atStart = viewport.scrollLeft <= 1;
        this.atEnd =
            viewport.scrollLeft + viewport.clientWidth >=
            viewport.scrollWidth - 1;
    }

    private onScroll = (): void => this.measure();

    private scrollStep(direction: -1 | 1): void {
        const viewport = this.viewport;

        if (!viewport) return;

        viewport.scrollBy({
            left: direction * viewport.clientWidth * SCROLL_FRACTION,
            behavior: 'smooth',
        });
    }

    override render() {
        const showPrev = this.overflowing && !this.atStart;
        const showNext = this.overflowing && !this.atEnd;

        return html`
            <button
                class="arrow prev"
                type="button"
                aria-label="Scroll left"
                ?hidden=${!showPrev}
                @click=${() => this.scrollStep(-1)}
            >
                <wa-icon name="chevron-left"></wa-icon>
            </button>
            <div class="viewport" @scroll=${this.onScroll}>
                <div class="track"><slot></slot></div>
            </div>
            <button
                class="arrow next"
                type="button"
                aria-label="Scroll right"
                ?hidden=${!showNext}
                @click=${() => this.scrollStep(1)}
            >
                <wa-icon name="chevron-right"></wa-icon>
            </button>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'scroll-row': ScrollRow;
    }
}

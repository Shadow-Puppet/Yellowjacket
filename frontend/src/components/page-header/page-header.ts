import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, query, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/dropdown-item/dropdown-item.js';
import type { MenuSurface } from '../menu-surface/menu-surface';
import '../menu-surface/menu-surface';

import { designTokens } from '../../styles/tokens.css';
import {
    MenuKeyboard,
    contextMenuStyles,
} from '../../utils/context-menu-controller';
import { ICON_MORE_ACTIONS } from '../../utils/icon-language';
import '../search-dialog/search-trigger';

/**
 * The one arrangement every primary view uses to say what it is.
 *
 * Before this, four views had a heading and four did not, two had sort
 * controls and none showed a count (`hands-on.md`, H-19) — so the page
 * shifted its shape as you moved through it, and the answer to "how
 * many albums do I have" was to count them. Each view had also written
 * its own arrangement, which is why they disagreed: the sort toolbar in
 * `track-list` and the one in `cover-grid` are the same twenty lines
 * twice, and the ninth view would have made a ninth.
 *
 * Title, count, sort, actions — in that order, in one component, so a
 * new view gets the shape by using it rather than by copying whichever
 * neighbour it happened to read.
 *
 * **Actions are data, and `<slot name="actions">` is the exception.**
 * Playlists' three buttons totalled 390px inside a header that gets
 * 700px at 900×600 and clipped "New Smart Playlist" to 114 of its 162
 * (#69) — a live defect at a size the app promises, against plan 018's
 * *no action is ever unreachable at any supported size*. The header
 * cannot fix that for slotted markup: it cannot move another
 * component's light-DOM children into a dropdown and keep their
 * behaviour, and arbitrary markup offers nothing generic to render as
 * a menu item. So a host declares `PageAction[]` and the header picks
 * the rendering. The slot survives for markup a data list genuinely
 * cannot express, at the stated cost that **a slotted action does not
 * collapse** and must therefore fit at 800×600.
 */

export interface SortOption {
    id: string;
    label: string;
}

export type SortDirection = 'asc' | 'desc';

/**
 * An action that only makes sense while it is a button.
 *
 * A drop target is the case: you cannot drag a track onto a closed
 * menu, so the affordance is absent from the overflow rather than
 * approximated there. The header wires these onto the button it
 * renders and owns none of them — the same division the sort control
 * already lives by.
 */
export interface PageActionDrop {
    /** True while an acceptable payload is over the button. */
    active?: boolean;
    onDragOver: (e: DragEvent) => void;
    onDragLeave: (e: DragEvent) => void;
    onDrop: (e: DragEvent) => void;
}

/**
 * One thing a view can do, as data rather than as markup.
 *
 * `<slot name="actions">` cannot be collapsed, and that is a fact about
 * the API rather than an effort estimate (#69): a component cannot move
 * another component's light-DOM children into a dropdown and keep their
 * behaviour, and there is nothing generic in arbitrary markup to render
 * as a menu item. Declaring an action instead is what lets the header
 * choose between the two renderings.
 */
export interface PageAction {
    id: string;
    label: string;
    /** From `utils/icon-language`, never a literal. */
    icon: string;
    onSelect: () => void;
    /**
     * Higher survives longer. The lowest collapses first, ties broken
     * by declaration order from the right, so a host that says nothing
     * gets "the last one written goes first".
     */
    priority?: number;
    disabled?: boolean;
    title?: string;
    drop?: PageActionDrop;
}

@customElement('page-header')
export class PageHeader extends LitElement {
    /**
     * The page's name. Rendered as the view's only `h1`.
     *
     * Empty is a real mode, not a missing value: `cover-grid` and
     * `track-list` are also *embedded* in the artist and genre pages,
     * which already have a heading of their own. There they keep the
     * count and the sort and drop the title, rather than growing a
     * second arrangement for the same three controls.
     */
    @property({ type: String })
    heading = '';

    /**
     * How many things are on the page. `null` means "not applicable"
     * (Jobs, Settings) rather than zero, and renders nothing — an
     * empty page says so in its empty state, which has room for a
     * sentence.
     */
    @property({ type: Number })
    count: number | null = null;

    /** Singular noun for the count; pluralised with a trailing `s`. */
    @property({ type: String, attribute: 'count-noun' })
    countNoun = 'item';

    /** Irregular plural, where a trailing `s` will not do. */
    @property({ type: String, attribute: 'count-plural' })
    countPlural = '';

    /** Sort choices. Empty (the default) renders no sort control. */
    @property({ attribute: false })
    sortOptions: SortOption[] = [];

    @property({ type: String, attribute: 'sort-field' })
    sortField = '';

    @property({ type: String, attribute: 'sort-direction' })
    sortDirection: SortDirection = 'asc';

    /**
     * The search term the page is filtered by, if any. The header says
     * so, because the *scope* of the header search box is the view —
     * so a page showing three of forty albums has to admit why.
     */
    @property({ type: String, attribute: 'search-term' })
    searchTerm = '';

    /** Shown while the view is refetching, next to the heading. */
    @property({ type: Boolean })
    busy = false;

    /**
     * What this view can do, in the order it wants them shown.
     *
     * The header decides what *fits*; the host decides what *happens*.
     * That is the rule the sort control already lives by — it asks for
     * a sort rather than performing one — and actions follow it, which
     * is why an action carries a handler rather than the header
     * carrying a verb it would have to interpret.
     */
    @property({ attribute: false })
    actions: PageAction[] = [];

    /** Action ids currently in the overflow menu. Derived, never set by a host. */
    @state()
    private collapsed: ReadonlySet<string> = new Set();

    /**
     * Whether the count has been given up. Derived, like `collapsed`.
     *
     * It is the last thing to yield and the only thing here that is
     * neither an identity nor an action — see `measureFit`.
     */
    @state()
    private countCollapsed = false;

    @state()
    private menuOpen = false;

    @query('.page-header')
    private headerEl?: HTMLElement;

    @query('.more-button')
    private moreButton?: HTMLButtonElement;

    @query('#page-header-overflow')
    private menuPanel?: HTMLElement;

    @query('menu-surface')
    private popup?: MenuSurface;

    private menuKeyboard = new MenuKeyboard(() => this.closeMenu());

    private resizeObserver?: ResizeObserver;

    /**
     * Whether the outside-click listener is attached.
     *
     * A `removeEventListener` with no matching `add` is not harmless
     * here: `view-lifecycle.test.ts` counts document listeners across a
     * view's life and an unconditional detach on disconnect shows up as
     * `held: -1`, which is the same accounting that would hide a real
     * leak in the other direction.
     */
    private outsideCloseAttached = false;

    /**
     * What the last fit was measured against.
     *
     * `updated()` runs on every pass, so it has to say what it depends
     * on or it re-measures — and a measurement here forces synchronous
     * layout. Width changes arrive through the ResizeObserver; this key
     * covers everything *else* in the flex row that can change how much
     * of it the actions are left.

     */
    private lastFitKey = '';

    override connectedCallback(): void {
        super.connectedCallback();

        this.resizeObserver = new ResizeObserver(() => this.measureFit());
        this.resizeObserver.observe(this);
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();

        this.resizeObserver?.disconnect();
        this.resizeObserver = undefined;
        this.detachOutsideClose();
    }

    static override styles = [
        designTokens,
        contextMenuStyles,
        css`
            :host {
                display: block;
                flex-shrink: 0;
            }

            .page-header {
                display: flex;
                align-items: center;
                gap: 12px;
                padding: 12px 16px 10px;
                border-bottom: 1px solid var(--yj-border-subtle, #333);
            }

            /* The title gives way before an action does.

               Everything in this row was flex-shrink: 0, so whatever
               came last lost — and the actions come last, which is how
               the "More actions" button ended up 76px off the right
               edge of a 320px viewport with every action already
               collapsed into it. The title is the one thing here the
               navigation also says (the sidebar item is selected, the
               bottom-nav tab is current), so it is the cheapest thing
               to truncate; the count, the sort and the actions are each
               the only place they are said. */
            h1 {
                margin: 0;
                font-size: var(--yj-text-xl, 18px);
                font-weight: 600;
                color: var(--yj-text-primary, #fff);
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
                min-width: 0;
            }

            .count {
                font-size: var(--yj-text-sm, 12px);
                color: var(--yj-text-tertiary, #888);
                white-space: nowrap;
            }

            .scope {
                font-size: var(--yj-text-sm, 12px);
                color: var(--yj-text-secondary, #b3b3b3);
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
                min-width: 0;
            }

            /* Actions sit at the right; everything before them is the
               page's identity and stays left. */
            .spacer {
                flex: 1 1 auto;
                min-width: 0;
            }

            .sort {
                display: inline-flex;
                align-items: center;
                gap: 6px;
                font-size: var(--yj-text-sm, 12px);
                color: var(--yj-text-secondary, #b3b3b3);
                flex-shrink: 0;
            }

            /* Every control in this header meets the app's 44px touch
               floor -- the number #56 set for the transport and the
               queue header already keeps (#186).

               It is min-size rather than padding with a negative
               margin, which is what the seek bar needed (#187), and
               the difference is worth stating because it decides
               whether targets can collide. There the painted track had
               to stay thin, so the target was grown past its own box
               and had to be checked against its neighbours. Here the
               control *is* the target: the boxes are flex items, so
               the gap keeps them apart and no two can overlap by
               construction.

               There is no phone branch. With the target being the box,
               a 44px control on a desktop is merely large, and a
               second declaration of what a phone shows is a second
               thing to keep in step -- which is the reason this
               component has never had one. It also avoids a media
               query that no tier here renders, which is exactly how
               the seek bar's phone rule came to be dead for months.

               Only the *width* of this reaches the overflow fit below:
               that pass measures inline size, so the height costs it
               nothing, and the two square controls grow the header's
               content by 22px in total. */
            .sort select {
                font: inherit;
                color: inherit;
                background: var(--yj-bg-surface, #212529);
                border: 1px solid var(--yj-border, #444);
                border-radius: 4px;
                padding: 3px 6px;
                cursor: pointer;
                min-block-size: 44px;
            }

            .sort-dir {
                display: inline-flex;
                align-items: center;
                justify-content: center;
                background: transparent;
                border: 1px solid transparent;
                border-radius: 4px;
                color: inherit;
                cursor: pointer;
                padding: 3px 5px;
                /* 28x21 before this, the smallest control in the
                   header and the only one that failed the floor in
                   both directions. */
                min-inline-size: 44px;
                min-block-size: 44px;
            }

            .sort-dir:hover {
                background: var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
            }

            .sort-dir:focus-visible,
            .sort select:focus-visible {
                outline: 2px solid var(--yj-accent, #ffd43b);
                outline-offset: -1px;
            }

            .spinner {
                width: 12px;
                height: 12px;
                border: 2px solid var(--yj-border, #444);
                border-top-color: var(--yj-accent, #ffd43b);
                border-radius: 50%;
                animation: spin 0.8s linear infinite;
            }

            @keyframes spin {
                to {
                    transform: rotate(360deg);
                }
            }

            @media (prefers-reduced-motion: reduce) {
                .spinner {
                    animation-duration: 3s;
                }
            }

            ::slotted(*) {
                flex-shrink: 0;
            }

            .actions {
                display: flex;
                align-items: center;
                gap: 8px;
                flex-shrink: 0;
            }

            .action,
            .more-button {
                background: none;
                border: 1px solid var(--yj-border-subtle, #555);
                border-radius: 4px;
                color: var(--yj-text-primary, #fff);
                padding: 6px 12px;
                font-size: var(--yj-text-md, 13px);
                font-family: inherit;
                cursor: pointer;
                display: flex;
                align-items: center;
                gap: 6px;
                white-space: nowrap;
                flex-shrink: 0;
                justify-content: center;
                min-block-size: 44px;
            }

            .more-button {
                padding: 6px 10px;
                /* 38x27, and it is the route to every collapsed
                   action, so it is the last control that should be
                   hard to hit. */
                min-inline-size: 44px;
            }

            /* The display: flex above outranks the UA stylesheet's
               rule for [hidden], and hiding is how an action
               collapses. (No backticks in here: one ends the css
               literal, and what you get is "css(...) is not a
               function" a long way from the cause.) */
            .action[hidden],
            .more-button[hidden] {
                display: none;
            }

            .action:hover,
            .more-button:hover,
            .action.drag-over {
                border-color: var(--yj-accent, #ffd43b);
                color: var(--yj-accent-text, #ffd43b);
            }

            .action.drag-over {
                background-color: var(
                    --yj-accent-bg-strong,
                    rgba(255, 212, 59, 0.15)
                );
            }

            .action:disabled {
                opacity: 0.5;
                cursor: default;
            }

            .action:focus-visible,
            .more-button:focus-visible {
                outline: 2px solid var(--yj-accent, #ffd43b);
                outline-offset: -1px;
            }

            menu-surface {
                z-index: 200;
            }

            /* A component states what it drops at phone width itself,
               in its own stylesheet, because a media query inside a
               shadow root is answered by the viewport and the shell
               cannot reach in. Here that is one word: the sort control
               is 172px of a 320px header, and "Sort:" is ~40px of it
               for a label the adjacent direction arrow already implies.
               It stays in the accessibility tree — it is the select's
               accessible name, so hiding it outright would rename the
               control to nothing — which is config-field's bug, one
               component over. clip-path rather than display: none for
               the reason styles/sr-only.css.ts gives. */
            @media (max-width: 599px) {
                .sort-label {
                    position: absolute;
                    width: 1px;
                    height: 1px;
                    margin: -1px;
                    padding: 0;
                    overflow: hidden;
                    clip-path: inset(50%);
                    white-space: nowrap;
                    border: 0;
                }
            }
        `,
    ];

    override render() {
        return html`
            <header class="page-header" part="header">
                ${this.heading === ''
                    ? nothing
                    : html`<h1 data-testid="page-heading">
                          ${this.heading}
                      </h1>`}
                ${this.busy
                    ? html`<span
                          class="spinner"
                          role="status"
                          aria-label="Refreshing"
                      ></span>`
                    : nothing}
                ${this.renderCount()}
                <div class="spacer"></div>
                ${this.renderScope()} ${this.renderSort()}
                <!-- #57. Below 600px the top bar is out of the layout,
                     so the search box has to be reachable from here.
                     It renders nothing at every other width and on
                     every view search-store says has nothing to
                     search, which is why no host declares it: the map
                     of searchable views already exists and this is one
                     more reader of it, not a second copy.

                     Before the actions, and never one of them: an
                     action can collapse into the overflow menu, and on
                     a phone that menu is already where the page's own
                     actions live -- search behind an ellipsis is the
                     top bar's problem moved rather than fixed. -->
                <search-trigger></search-trigger>
                ${this.renderActions()}
                <slot name="actions"></slot>
            </header>
        `;
    }

    protected override updated(): void {
        const key = [
            this.heading,
            this.count,
            this.countNoun,
            this.countPlural,
            this.searchTerm,
            this.sortOptions.length,
            this.sortField,
            this.sortDirection,
            this.busy,
            this.actions.map((a) => `${a.id}:${a.label}:${a.disabled ?? false}`).join(','),
        ].join('|');

        if (key === this.lastFitKey) return;

        this.lastFitKey = key;
        this.measureFit();
    }

    // =================================================================
    // What fits
    // =================================================================

    /**
     * Decide which actions are buttons and which are menu items.
     *
     * Two things about the shape of this are load-bearing.
     *
     * **Every pass starts from all-visible**, so the collapsed set is a
     * pure function of the current width rather than of the order the
     * widths arrived in. A rule that only ever *added* to the set would
     * never give an action back when the window grew, and one that
     * adjusted by a step would need a hysteresis band to stop it
     * oscillating on the pixel where a button exactly fits.
     *
     * **It flips `hidden` on the rendered nodes rather than re-rendering
     * between steps.** Reading `scrollWidth` forces layout, which is the
     * point; awaiting a Lit update between steps instead would let the
     * intermediate all-visible state paint, so the fix would flash the
     * overflow it exists to prevent. The reactive state is set once, at
     * the end, and the next render agrees with what was measured.
     *
     * The budget is the *header's* overflow and not the actions row's,
     * because the count and the sort control are `flex-shrink: 0` and
     * are therefore competing for the same width — only `.scope` gives
     * way, which is what it has an ellipsis for.
     */
    private measureFit(): void {
        const header = this.headerEl;

        if (!header) return;

        const buttons = new Map<string, HTMLElement>();

        for (const el of this.renderRoot.querySelectorAll<HTMLElement>(
            '[data-action-id]',
        )) {
            const id = el.dataset['actionId'];

            if (id !== undefined) buttons.set(id, el);
        }

        const more = this.moreButton;
        const title = this.renderRoot.querySelector('h1');
        const count = this.renderRoot.querySelector<HTMLElement>('.count');

        /**
         * Nothing is clipped — which is not the same as the header not
         * overflowing, and the difference is a trap worth naming.
         *
         * Once the title can ellipsis, it absorbs the pressure and
         * `scrollWidth` reports a header that fits perfectly while the
         * heading reads "Playlis…". That is this issue's own failure
         * mode moved from the button to the title, and it is invisible
         * to exactly the same measurement that missed it the first time.
         * So the title's own truncation counts as not fitting, and
         * collapsing an action is tried before the title gives way.
         */
        const fits = () =>
            header.scrollWidth <= header.clientWidth &&
            (title === null || title.scrollWidth <= title.clientWidth + 1);

        for (const el of buttons.values()) el.hidden = false;

        if (more) more.hidden = true;

        if (count) count.hidden = false;

        const collapsed = new Set<string>();

        if (!fits()) {
            if (more) more.hidden = false;

            for (const action of this.collapseOrder()) {
                collapsed.add(action.id);

                const el = buttons.get(action.id);

                if (el) el.hidden = true;

                if (fits()) break;
            }
        }

        this.commitCollapsed(collapsed, this.collapseCount(count, fits));
    }

    /**
     * The last thing to give way, after every action is in the menu and
     * the title has already run out.
     *
     * There are four things competing for this row and three of them
     * cannot go. The **title** yields first and is allowed to ellipsis
     * away entirely at 320px, because the navigation also says which
     * page you are on. The **sort** control and the **actions** are
     * each the only place they are said, so an action collapses into
     * the menu rather than disappearing and the sort control stays.
     * That leaves the **count**, which is the one purely informational
     * item on the row — an empty page says so in its empty state, and a
     * full one is being looked at.
     *
     * It became reachable rather than theoretical with #57: below 600px
     * the header also carries the phone's search button, and on
     * Playlists at 320px that is 43px more than the row has. Measured
     * there: title 0, count 50, sort 143, search 40, "More actions" 38,
     * five 12px gaps and 32px of gutters — 363 in 320, with the More
     * button ending 27px past the edge. Something has to go, and this
     * is the only candidate that is not an action.
     *
     * @returns whether the count was given up.
     */
    private collapseCount(
        count: HTMLElement | null,
        fits: () => boolean,
    ): boolean {
        if (count === null || fits()) return false;

        count.hidden = true;

        return true;
    }

    /** Lowest priority first; ties broken from the right. */
    private collapseOrder(): PageAction[] {
        return this.actions
            .map((action, index) => ({ action, index }))
            .sort(
                (a, b) =>
                    (a.action.priority ?? 0) - (b.action.priority ?? 0) ||
                    b.index - a.index,
            )
            .map(({ action }) => action);
    }

    private commitCollapsed(next: Set<string>, countHidden: boolean): void {
        this.countCollapsed = countHidden;

        const same =
            next.size === this.collapsed.size &&
            [...next].every((id) => this.collapsed.has(id));

        if (same) return;

        this.collapsed = next;

        // Nothing left to show in it. Closing rather than leaving an
        // empty menu open is the same rule the shelves follow.
        if (next.size === 0 && this.menuOpen) this.closeMenu();
    }

    // =================================================================
    // Rendering
    // =================================================================

    private renderActions() {
        if (this.actions.length === 0) return nothing;

        const overflowed = this.actions.filter((a) => this.collapsed.has(a.id));

        return html`
            <div class="actions">
                ${this.actions.map((a) => this.renderActionButton(a))}
                <!-- A sheet below 600px, like every other menu in the
                     app (#60). The clipping that issue is about does
                     not bite here — this one opens downward from the
                     top of a full-height view, so it has somewhere to
                     go even without top-layer promotion — but the touch
                     targets do: on a phone *every* action of a page
                     that overflows lives in here, at wa-dropdown-item
                     defaults. One surface, so there is no second
                     answer to what a menu looks like. -->
                <menu-surface
                    placement="bottom-end"
                    .active=${this.menuOpen}
                >
                    <button
                        slot="anchor"
                        class="more-button"
                        type="button"
                        data-testid="page-actions-more"
                        aria-label="More actions"
                        aria-haspopup="menu"
                        aria-expanded=${this.menuOpen ? 'true' : 'false'}
                        aria-controls="page-header-overflow"
                        ?hidden=${overflowed.length === 0}
                        @click=${this.onMoreClick}
                    >
                        <wa-icon name=${ICON_MORE_ACTIONS}></wa-icon>
                    </button>
                    <div
                        id="page-header-overflow"
                        class="context-menu-panel"
                        role="menu"
                        aria-label="More actions"
                    >
                        ${overflowed.map(
                            (a) => html`
                                <wa-dropdown-item
                                    ?disabled=${a.disabled ?? false}
                                    @click=${() => this.onActionSelect(a)}
                                >
                                    <wa-icon
                                        slot="icon"
                                        name=${a.icon}
                                    ></wa-icon>
                                    ${a.label}
                                </wa-dropdown-item>
                            `,
                        )}
                    </div>
                </menu-surface>
            </div>
        `;
    }

    private renderActionButton(a: PageAction) {
        const drop = a.drop;

        return html`
            <button
                class="action ${drop?.active === true ? 'drag-over' : ''}"
                type="button"
                data-action-id=${a.id}
                data-testid=${`page-action-${a.id}`}
                title=${a.title ?? nothing}
                ?disabled=${a.disabled ?? false}
                ?hidden=${this.collapsed.has(a.id)}
                @click=${() => a.onSelect()}
                @dragover=${(e: DragEvent) => drop?.onDragOver(e)}
                @dragleave=${(e: DragEvent) => drop?.onDragLeave(e)}
                @drop=${(e: DragEvent) => drop?.onDrop(e)}
            >
                <wa-icon name=${a.icon}></wa-icon>
                ${a.label}
            </button>
        `;
    }

    // =================================================================
    // The overflow menu
    // =================================================================

    private onActionSelect(a: PageAction): void {
        if (a.disabled === true) return;

        this.closeMenu();
        a.onSelect();
    }

    private onMoreClick = (): void => {
        if (this.menuOpen) {
            this.closeMenu();

            return;
        }

        this.menuOpen = true;

        void this.updateComplete.then(() => {
            if (!this.menuOpen) return;

            this.popup?.reposition();
            this.menuKeyboard.open(this.menuPanel ?? null, this.moreButton);
            this.attachOutsideClose();
        });
    };

    private closeMenu(): void {
        if (!this.menuOpen) return;

        this.detachOutsideClose();
        this.menuKeyboard.close();
        this.menuOpen = false;
    }

    /**
     * A click anywhere else closes it. `composedPath` rather than
     * `contains`, because the trigger and the panel are both inside
     * this shadow root and a click retargets at the host.
     */
    private onOutsideDown = (e: Event): void => {
        if (e.composedPath().includes(this.menuPanel as EventTarget)) return;
        if (e.composedPath().includes(this.moreButton as EventTarget)) return;

        this.closeMenu();
    };

    private attachOutsideClose(): void {
        if (this.outsideCloseAttached) return;

        this.outsideCloseAttached = true;
        document.addEventListener('mousedown', this.onOutsideDown, true);
    }

    private detachOutsideClose(): void {
        if (!this.outsideCloseAttached) return;

        this.outsideCloseAttached = false;
        document.removeEventListener('mousedown', this.onOutsideDown, true);
    }

    private renderCount() {
        if (this.count === null) return nothing;

        const plural =
            this.countPlural !== ''
                ? this.countPlural
                : `${this.countNoun}s`;

        const noun = this.count === 1 ? this.countNoun : plural;

        // Rendered whether or not it fits, and hidden with an
        // attribute -- the same shape the action buttons use, and for
        // the same reason: `measureFit` starts every pass from
        // all-visible, so it needs a node to un-hide. Returning
        // `nothing` here would take the count away for the rest of the
        // session the first time a 320px window appeared.
        return html`<span
            class="count"
            data-testid="page-count"
            ?hidden=${this.countCollapsed}
            >${this.count.toLocaleString()} ${noun}</span
        >`;
    }

    private renderScope() {
        if (this.searchTerm === '') return nothing;

        const what =
            this.heading === ''
                ? 'results'
                : this.heading.toLowerCase();

        return html`<span class="scope" data-testid="page-search-scope"
            >Showing ${what} matching &ldquo;${this.searchTerm}&rdquo;</span
        >`;
    }

    private renderSort() {
        if (this.sortOptions.length === 0) return nothing;

        const ascending = this.sortDirection === 'asc';

        // One option is not a choice: Artists can only be sorted by
        // name, because `library.Artist` carries no counts to sort by.
        // A select with a single option is a control that does
        // nothing, so it says what the order is and lets the direction
        // button do the work.
        if (this.sortOptions.length === 1) {
            return html`
                <div class="sort">
                    <span
                        ><span class="sort-label">Sort: </span
                        >${this.sortOptions[0]?.label}</span
                    >
                    ${this.renderDirectionButton(ascending)}
                </div>
            `;
        }

        return html`
            <div class="sort">
                <label>
                    <span class="sort-label">Sort:</span>
                    <select
                        data-testid="page-sort"
                        .value=${this.sortField}
                        @change=${this.onSortFieldChange}
                    >
                        ${this.sortOptions.map(
                            (o) => html`
                                <option
                                    value=${o.id}
                                    ?selected=${o.id === this.sortField}
                                >
                                    ${o.label}
                                </option>
                            `,
                        )}
                    </select>
                </label>
                ${this.renderDirectionButton(ascending)}
            </div>
        `;
    }

    private renderDirectionButton(ascending: boolean) {
        return html`
            <button
                class="sort-dir"
                type="button"
                data-testid="page-sort-direction"
                aria-label=${ascending ? 'Sort ascending' : 'Sort descending'}
                aria-pressed=${ascending ? 'false' : 'true'}
                title=${ascending ? 'Ascending' : 'Descending'}
                @click=${this.onDirectionClick}
            >
                <wa-icon
                    name=${ascending
                        ? 'arrow-up-short-wide'
                        : 'arrow-down-wide-short'}
                ></wa-icon>
            </button>
        `;
    }

    private onSortFieldChange = (e: Event) => {
        const field = (e.target as HTMLSelectElement).value;

        this.emitSort(field, this.sortDirection);
    };

    private onDirectionClick = () => {
        this.emitSort(
            this.sortField,
            this.sortDirection === 'asc' ? 'desc' : 'asc',
        );
    };

    /** The host owns the sort state and persists it; this only asks. */
    private emitSort(field: string, direction: SortDirection) {
        this.dispatchEvent(
            new CustomEvent('sort-change', {
                detail: { field, direction },
                bubbles: true,
                composed: true,
            }),
        );
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'page-header': PageHeader;
    }
}

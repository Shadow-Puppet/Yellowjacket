import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/drawer/drawer.js';
import type WaDrawer from '@awesome.me/webawesome/dist/components/drawer/drawer.js';
import { designTokens } from '../../styles/tokens.css';
import { sheetScrollFade } from '../../styles/sheet-scroll.css';
import '../sidebar/app-sidebar.js';
import { nameDialog } from '@utils/name-dialog';
import { ICON_PLAYLIST } from '@utils/icon-language';
import { ActiveViewController } from '@store/controllers/active-view-controller';
import { ViewVisibilityController } from '@store/controllers/view-visibility-controller';

type View = 'home' | 'albums' | 'tracks' | 'playlists';

interface Tab {
    id: View;
    label: string;
    icon: string;
}

/**
 * The phone's primary navigation: a bottom tab bar, shown only below
 * the phone breakpoint (index.css owns that; this element is
 * `display: none` above it).
 *
 * **Four destinations and a way to everything else.** A tab bar is
 * three to five items before the targets stop being thumb-sized —
 * 360 px over eleven sidebar entries is 32 px each — so the four here
 * are the ones plan 016's subset says a phone is *for*, and "More"
 * opens the existing `<app-sidebar>` in a sheet. That is deliberately
 * a reuse rather than a second nav: two lists of destinations is two
 * places to add the next view to, and the sidebar already carries the
 * drag-to-navigate behaviour, the active state and the labels.
 *
 * **"More" rises from the bottom, and it is the same sheet a context
 * menu is** (#71). It was a `wa-drawer` sliding in from the side: a
 * 200px column of a 424px screen, opening away from the thumb that
 * asked for it, with three nested scrollers in it — the dialog, its
 * body, and the sidebar's own `overflow-y: auto` host — which is the
 * "only part of the screen scrolls under my finger" in the report.
 *
 * Three things about the replacement are load-bearing.
 *
 * **It is the same element with another `placement`, not a new
 * surface.** `wa-drawer` renders a native `<dialog>` and opens it with
 * `showModal()`, which is exactly what `menu-surface`'s sheet relies
 * on — Chrome 37, the real top layer — so #60's containment finding
 * carries over with nothing new to prove, and the focus trap, Escape,
 * tap-outside and `wa-after-hide` all come along unchanged.
 *
 * **The body is the only scroller**, with `overscroll-behavior:
 * contain`, and the sidebar is told to stop being one. Nesting them is
 * what makes a drag scroll the wrong box.
 *
 * **The sidebar is still mounted rather than re-listed as data**,
 * which the issue offers as an alternative. Its `data-testid` per
 * destination is the reason: the shell's own sidebar is `display:
 * none` below 600px rather than removed, so a second list drawing
 * `nav-*` handles is the duplication this component already renders
 * conditionally to avoid — and it would be a second place to add the
 * next view to, with its own copy of #25's visibility filter.
 *
 * It emits the same bubbling, composed `navigate` event the sidebar
 * does, so `index.ts` needs no knowledge of it, and it listens for that
 * event globally for the same reason the sidebar does: a navigation it
 * did not send (a card click, a detail view, the sheet) still has to
 * move the highlight.
 */
@customElement('bottom-nav')
export class BottomNav extends LitElement {
    static override styles = [designTokens, css`
        :host {
            display: block;
            background-color: var(--yj-bg-elevated, #343a40);
            border-top: 1px solid var(--yj-border, #495057);
            /* The home indicator on a gesture-navigation phone sits
               under the last few pixels of the viewport, so the bar
               pads itself out of the way where the browser reports
               one and by nothing where it does not. */
            padding-bottom: env(safe-area-inset-bottom, 0);
        }

        nav ul {
            display: grid;
            grid-auto-flow: column;
            grid-auto-columns: 1fr;
            margin: 0;
            padding: 0;
            list-style: none;
        }

        button {
            width: 100%;
            /* 48px is the smallest target this should ever be; the
               label sits under the icon rather than beside it, which
               is what keeps five of them legible at 360px. */
            min-height: 48px;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            gap: 2px;
            padding: 4px 0;
            background: none;
            border: none;
            color: var(--yj-text-secondary, #adb5bd);
            cursor: pointer;
            font-family: inherit;
            font-size: var(--yj-font-size-xs, 0.7rem);
        }

        button wa-icon {
            font-size: 1.15rem;
        }

        button.active {
            color: var(--yj-accent, #ffd43b);
        }

        /* The press state (#54). This bar is the phone's primary
           navigation and had no feedback of its own at all -- what a
           tap produced was the web view's tap highlight, a grey box
           over the whole 48px cell, which index.css has now taken
           away. The .active rule above is which tab you are *on*; this
           is the tab being pressed, so they are a colour and a
           background rather than two colours. */
        button:active {
            background-color: var(--yj-press-overlay, rgba(255, 255, 255, 0.12));
        }

        button:focus-visible {
            outline: 2px solid var(--yj-accent, #ffd43b);
            outline-offset: -2px;
        }

        .label {
            /* A tab label is an aid, not the name: the button's own
               accessible name comes from its text, and truncating it
               visually does not change that. */
            max-width: 100%;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }

        /* The sheet. --size is the drawer's own API for the axis its
           placement uses, so auto is what makes it hug its content
           instead of being a fixed 25rem band; the rest is the shape
           the menu-surface context sheet already has, so a phone meets
           one sheet rather than two. 85vh for its reason too: a surface
           covering the whole screen is a page, not a sheet. */
        wa-drawer {
            --size: auto;
        }

        wa-drawer::part(dialog) {
            max-height: 85vh;
            border-radius: 12px 12px 0 0;
            /* The sidebar paints its own surface, so the sheet takes
               that colour rather than the menus' elevated one: two
               greys in one sheet is a seam across the middle of it. */
            background-color: var(--yj-bg-surface, #212529);
            /* One scroller, and it is the body below. The dialog's own
               overflow: auto is what let the sheet scroll as well as
               its content, and it is also what would square off the
               corners this rule just rounded. */
            overflow: hidden;
        }

        /* And this list does not fit (#210): measured at 424x439 with
           the seed's eight destinations, the body is scrollHeight 412
           against clientHeight 373, and eleven items at 48px would be
           528 -- the count is the user's since #25. So the sheet says
           where the fold is, with styles/sheet-scroll.css's two layers
           rather than a second answer to the question #207 settled for
           the context sheet. The colour is the local half: the sidebar
           paints --yj-bg-surface, so the cover does too, or the fade
           draws the menus' grey across the bottom of this one. */
        wa-drawer::part(body) {
            padding: 0;
            /* A scroll that reaches the end of this list must not
               become a scroll of the page underneath it. */
            overscroll-behavior: contain;
            /* The sheet sits on the bottom edge, so the last
               destination would otherwise be under the home indicator
               on a gesture-navigation phone -- the same allowance the
               bar itself makes above. */
            padding-bottom: env(safe-area-inset-bottom, 0);
            --yj-sheet-surface: var(--yj-bg-surface, #212529);
            ${sheetScrollFade}
        }

        /* And the sheet paints that surface once. The sidebar's host
           paints the same grey -- which in the shell is the sidebar's
           own background and here is a second, opaque copy of the
           sheet's, drawn *over* the body's layers. So the fade was
           painted and then covered: measured at 424x439 before this
           rule, the last 32px read a flat 52,58,64 with 39px still
           below. menu-surface meets the same requirement from the
           other side, where .context-menu-panel[data-sheet] is
           background-color: transparent; nothing changes visually
           here, because the colour underneath is the one being
           removed. */
        app-sidebar {
            background-color: transparent;
        }

        /* A sheet is dragged at with a thumb, so it says where its top
           edge is. Decorative: the destinations are below it. */
        .grip {
            width: 36px;
            height: 4px;
            margin: 8px auto 4px;
            border-radius: 2px;
            background: var(--yj-text-tertiary, #888);
        }
    `];

    /**
     * Which tab is lit, read from the shell rather than tracked here.
     *
     * This was a `@state()` field set from the `navigate` event, which
     * only the outbound path dispatches -- so backing out of a detail
     * view left the highlight wherever it had been (#72). It had no
     * equivalent of `app-sidebar`'s `navItems.some(...)` guard either,
     * so a detail view set it to a name matching no tab and *nothing*
     * was lit; that asymmetry is why one nav looked broken and the
     * other looked fine. The store answers both: a detail view leaves
     * the tab it was opened from lit, in both components.
     */
    private activeCtrl = new ActiveViewController(this);

    /**
     * The tab bar honours the sidebar's toggles (#25), and the reason is
     * inside this component rather than a general rule about phones.
     * `PHONE_COLUMN_IDS` is the precedent for "what a phone shows is a
     * different question", and it would apply here too -- except that
     * "More" opens the *same* `<app-sidebar>`, which filters. An
     * unfiltered bar would therefore contradict its own drawer, one tap
     * apart, and a destination the user switched off is off wherever it
     * is offered.
     *
     * Which four tabs remains plan 016's committed subset; this only
     * removes from it. Hiding all four leaves "More", which is always
     * present and reaches everything.
     */
    private visibilityCtrl = new ViewVisibilityController(this);

    /**
     * Whether the sheet has been asked for.
     *
     * The sidebar inside it is rendered only while this is true, and
     * that is not an optimisation. `app-sidebar` carries a
     * `data-testid` per destination, so a second copy standing by in
     * the DOM makes every `nav-*` testid ambiguous **for the whole
     * app** -- 30 existing specs failed with "strict mode violation:
     * resolved to 2 elements" on a desktop viewport where this element
     * is not even visible. A duplicate of a shared component is a
     * duplicate of its handles.
     */
    @state()
    private drawerOpen = false;

    @query('wa-drawer')
    private drawer?: WaDrawer;

    private static readonly TABS: Tab[] = [
        { id: 'home', label: 'Home', icon: 'house' },
        { id: 'albums', label: 'Albums', icon: 'compact-disc' },
        { id: 'tracks', label: 'Tracks', icon: 'music' },
        { id: 'playlists', label: 'Playlists', icon: ICON_PLAYLIST },
    ];

    override connectedCallback() {
        super.connectedCallback();
        document.addEventListener(
            'navigate',
            this.onGlobalNavigate as EventListener,
        );
    }

    override disconnectedCallback() {
        super.disconnectedCallback();
        document.removeEventListener(
            'navigate',
            this.onGlobalNavigate as EventListener,
        );
    }

    override updated() {
        // Web Awesome renders its heading into its own shadow root and
        // never points aria-labelledby at it, so the sheet would
        // otherwise be announced unnamed -- the same fix, and the same
        // reason, as every wa-dialog in the app. A drawer's shadow root
        // has the same shape, so the helper needs no change; under
        // `without-header` there is no heading to point at, which is
        // that helper's documented `aria-label` path.
        nameDialog(this.drawer);
    }

    private onGlobalNavigate = () => {
        // A navigation from inside the sheet is the sheet's job done.
        // The highlight is not this listener's business any more.
        this.drawerOpen = false;
    };

    private openDrawer = () => {
        this.drawerOpen = true;
    };

    /**
     * Web Awesome closes itself on Escape and on a click outside, and
     * tells us afterwards rather than asking -- so the flag follows the
     * element, or the next `open` would be a no-op against a drawer
     * that thinks it is already open.
     */
    private onDrawerHide = () => {
        this.drawerOpen = false;
    };

    private navigate(view: View) {
        this.dispatchEvent(new CustomEvent('navigate', {
            detail: { view },
            bubbles: true,
            composed: true,
        }));
    }

    override render() {
        return html`
            <nav aria-label="Primary">
                <ul>
                    ${BottomNav.TABS
                        .filter((tab) => this.visibilityCtrl.visible(tab.id))
                        .map((tab) => html`
                        <li>
                            <button
                                type="button"
                                class=${this.activeCtrl.isActive(tab.id)
                                    ? 'active'
                                    : ''}
                                data-testid="tab-${tab.id}"
                                aria-current=${this.activeCtrl.isActive(tab.id)
                                    ? 'page'
                                    : 'false'}
                                @click=${() => this.navigate(tab.id)}
                            >
                                <wa-icon name=${tab.icon}></wa-icon>
                                <span class="label">${tab.label}</span>
                            </button>
                        </li>
                    `)}
                    <li>
                        <button
                            type="button"
                            data-testid="tab-more"
                            aria-haspopup="dialog"
                            @click=${this.openDrawer}
                        >
                            <wa-icon name="bars"></wa-icon>
                            <span class="label">More</span>
                        </button>
                    </li>
                </ul>
            </nav>

            <wa-drawer
                placement="bottom"
                without-header
                label="All views"
                data-testid="nav-drawer"
                ?open=${this.drawerOpen}
                @wa-after-hide=${this.onDrawerHide}
            >
                <div class="grip"></div>
                ${this.drawerOpen
                    ? html`<app-sidebar expanded></app-sidebar>`
                    : nothing}
            </wa-drawer>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'bottom-nav': BottomNav;
    }
}

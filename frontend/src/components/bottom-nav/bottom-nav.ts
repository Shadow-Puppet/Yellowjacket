import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/drawer/drawer.js';
import type WaDrawer from '@awesome.me/webawesome/dist/components/drawer/drawer.js';
import { designTokens } from '../../styles/tokens.css';
import '../sidebar/app-sidebar.js';
import { nameDialog } from '@utils/name-dialog';
import { ICON_PLAYLIST } from '@utils/icon-language';
import { ActiveViewController } from '@store/controllers/active-view-controller';

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
 * opens the existing `<app-sidebar>` in a drawer. That is deliberately
 * a reuse rather than a second nav: two lists of destinations is two
 * places to add the next view to, and the sidebar already carries the
 * drag-to-navigate behaviour, the active state and the labels.
 *
 * It emits the same bubbling, composed `navigate` event the sidebar
 * does, so `index.ts` needs no knowledge of it, and it listens for that
 * event globally for the same reason the sidebar does: a navigation it
 * did not send (a card click, a detail view, the drawer) still has to
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

        wa-drawer::part(body) {
            padding: 0;
        }

        app-sidebar {
            /* The sidebar sizes itself inline and collapses to icons
               below 900px, which is every phone.  In the drawer there
               is room for the labels, so it is told not to. */
            height: 100%;
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
     * Whether the drawer has been asked for.
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
        // never points aria-labelledby at it, so the drawer would
        // otherwise be announced unnamed -- the same fix, and the same
        // reason, as every wa-dialog in the app. A drawer's shadow root
        // has the same shape, so the helper needs no change.
        nameDialog(this.drawer);
    }

    private onGlobalNavigate = () => {
        // A navigation from inside the drawer is the drawer's job done.
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
                    ${BottomNav.TABS.map((tab) => html`
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
                placement="start"
                label="All views"
                data-testid="nav-drawer"
                ?open=${this.drawerOpen}
                @wa-after-hide=${this.onDrawerHide}
            >
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

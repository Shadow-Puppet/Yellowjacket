/**
 * Where a context menu is drawn: a popup on a desktop, a bottom sheet
 * on a phone (#60).
 *
 * Every context menu in this app is a `.context-menu-panel` inside a
 * `<wa-popup>` anchored to the touch point, driven by
 * `ContextMenuController`. On the reference device that is structurally
 * broken, and the failure was measured on the hardware rather than
 * inferred:
 *
 * - Chrome 113 has **no Popover API** (`popover` is Chrome 114), so
 *   `wa-popup` takes its own documented fallback and positions with
 *   `strategy: "fixed"` instead of the top layer. Measured on the
 *   device: `HTMLElement.prototype.hasOwnProperty('popover')` is false
 *   and the popup's computed `position` is `fixed`.
 * - `index.css` puts `contain: layout style paint` on `.main-panel`,
 *   the ancestor of every view. Paint containment **clips** fixed
 *   descendants. Measured: `.main-panel` computes `contain: content`
 *   and spans 0-318 of a 439px viewport, while the open menu spans
 *   191-401 — so 83px of it, three of its seven items, is cut off.
 *
 * A `<dialog>` fixes it by construction rather than by styling, because
 * `showModal()` is Chrome 37 and uses the real top layer. **That was
 * measured too, and it needed to be**: every other dialog in this app
 * is mounted in `index.html`, *outside* `.main-panel`, so "dialogs are
 * fine" was not evidence about a dialog opened from inside a view. A
 * probe dialog appended to `track-list`'s shadow root paints to y=439,
 * over the mini player and the tab bar, with the contained ancestor
 * still there.
 *
 * Four things about this component are load-bearing.
 *
 * **It is one element with two presentations, not two components.**
 * The host keeps rendering exactly the panel it rendered before and
 * slots it into whichever surface is up, so the twelve call sites
 * changed one tag name each and nothing else — no second item model, no
 * second keyboard model, and `ContextMenuController` still drives
 * `.active` and `.anchor` as if it were talking to a `wa-popup`.
 *
 * **Which surface exists is `matchMedia`, not a media query.** The
 * decision is whether a `<dialog>` is in the tree at all, which is
 * `job-band` and `player-controls`' rule: a `display: none` surface is
 * still in the shadow root and still something a positional or by-role
 * query finds.
 *
 * **The sheet has to un-do the UA stylesheet to be full-bleed.**
 * A native `<dialog>` carries `max-width: calc(100% - 6px - 2em)` and
 * `margin: auto`, which on the device produced a 354px panel floating
 * in the middle of a 424px screen. `max-width: none` and explicit
 * margins are what make it a sheet rather than a small centred box.
 * The *positioning* needs no such care: a top-layer dialog's containing
 * block is the viewport even with a paint-contained ancestor, which is
 * why `bottom: 0` reaches y=439 and not the main panel's 318.
 *
 * **Dismissal has to travel back.** `wa-dialog` closes itself on
 * Escape, which would otherwise leave the controller's
 * `contextMenuOpen` true and the menu unopenable until something else
 * cleared it. `menu-dismiss` is that signal, and the controller listens
 * for it on the document beside the click and contextmenu listeners it
 * already has.
 */
import { LitElement, css, html } from 'lit';
import { customElement, property, query, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/popup/popup.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
import type WaPopup from '@awesome.me/webawesome/dist/components/popup/popup.js';

import { sheetScrollFade } from '../../styles/sheet-scroll.css';
import { PHONE_QUERY } from '@utils/breakpoints';
import { nameDialogsIn } from '@utils/name-dialog';

/** The event a surface dispatches when it closed itself. */
export const MENU_DISMISS_EVENT = 'menu-dismiss';

/**
 * The event a surface dispatches once it has finished showing.
 *
 * Only the sheet sends it, and only because `wa-dialog` moves focus to
 * itself on the frame after `showModal()` -- see `MenuKeyboard.refocus`
 * for why waiting longer is not the fix.
 */
export const MENU_SHOWN_EVENT = 'menu-shown';

/**
 * A `wa-popup` anchor: a real element or a virtual one.
 *
 * `undefined` rather than `null` for "not set yet", because that is
 * what `wa-popup`'s own property accepts — this surface hands the value
 * straight through and must not widen it.
 */
type MenuAnchor = WaPopup['anchor'] | undefined;

/** A `wa-dialog`, as much of it as this file needs. */
type DialogEl = HTMLElement & { open: boolean };

@customElement('menu-surface')
export class MenuSurface extends LitElement {
    /** Whether the menu is showing. Set by `ContextMenuController`. */
    @property({ type: Boolean }) active = false;

    /**
     * Where the popup hangs from. Ignored in sheet mode, which is
     * anchored to the bottom of the screen rather than to the touch
     * point — that is the whole point of a sheet.
     */
    @property({ attribute: false }) anchor: MenuAnchor = undefined;

    /**
     * `wa-popup`'s placement, defaulted because all twelve call sites
     * passed the same one. Kept as a property so a future menu that
     * wants another does not have to reach past this component.
     */
    @property() placement = 'bottom-start';

    /**
     * What to call the sheet, for a surface whose content is not a
     * `.context-menu-panel` with an `aria-label` of its own -- the
     * playlist submenu, whose content is a `playlist-picker`.
     */
    @property() label = '';

    @state() private sheet = false;

    @query('wa-popup') private popup?: WaPopup;

    @query('wa-dialog') private dialog?: DialogEl;

    private phoneQuery?: MediaQueryList;

    static override styles = css`
        :host {
            display: contents;
        }

        wa-popup {
            z-index: 200;
        }

        /* The sheet. A native dialog's UA stylesheet centres it and
           caps its width, which on the device drew a 354px box in the
           middle of a 424px screen — so all four of these are undoing
           that rather than decorating. */
        wa-dialog::part(dialog) {
            margin: auto auto 0 auto;
            max-width: none;
            max-height: 85vh;
            width: 100%;
            border-radius: 12px 12px 0 0;
            background: var(--yj-bg-elevated, #343a40);
            padding: 0;
        }

        /* **A long menu scrolls; it does not hang off the bottom.**
           Measured on the device at 80vh: seven 48px rows plus the grip
           came to 364px against a 351px dialog, so the last row's
           bottom was at y=452 on a 439px screen -- the one row a
           destructive action is most likely to be. The cap has to stay
           (a sheet covering the whole screen is a page, not a sheet),
           so the body is what gives.

           **And a body that scrolls says so** (#207). Scrolling was the
           whole of the fix above, which left the last item reachable
           and nothing on screen admitting it was there -- measured at
           424x439, eight items ending at y=470 with the fold at 439,
           and worse when the cut lands on a row boundary, where the
           sheet ends in a clean edge that reads as the end of the list.

           The two layers that say it live in styles/sheet-scroll.css
           (#210), because the phone has a second sheet -- bottom-nav's
           "More" -- which overflows for the same reason and must not
           arrive at its own answer for what a fold looks like. What is
           local to this sheet is the colour the cover is painted in:
           the menus' elevated grey, handed over as --yj-sheet-surface
           on the same box. */
        wa-dialog::part(body) {
            padding: 0;
            --yj-sheet-surface: var(--yj-bg-elevated, #343a40);
            ${sheetScrollFade}
        }

        /* A sheet is dragged at with a thumb, so it says where its top
           edge is. Decorative: the panel below it carries the actions. */
        .grip {
            width: 36px;
            height: 4px;
            margin: 8px auto 4px;
            border-radius: 2px;
            background: var(--yj-text-tertiary, #888);
        }
    `;

    override connectedCallback(): void {
        super.connectedCallback();

        // Looked up here rather than at module load, so a test can
        // install its own matchMedia before the element is created.
        this.phoneQuery = window.matchMedia?.(PHONE_QUERY);
        this.sheet = this.phoneQuery?.matches ?? false;
        this.phoneQuery?.addEventListener('change', this.onPhoneChange);
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.phoneQuery?.removeEventListener('change', this.onPhoneChange);
    }

    private onPhoneChange = (e: MediaQueryListEvent): void => {
        this.sheet = e.matches;
    };

    /**
     * Re-run the popup's positioning.
     *
     * Forwarded rather than dropped because `page-header` calls it when
     * it opens the overflow menu: the popup is rendered before the
     * button it anchors to has settled. A sheet has nothing to
     * reposition -- it is anchored to the bottom of the screen -- so
     * there it is deliberately a no-op rather than an error.
     */
    reposition(): void {
        this.popup?.reposition();
    }

    /**
     * The panel the host slotted in. It is light DOM here and stays in
     * the host's shadow root, which is what keeps the host's own
     * `contextMenuStyles` applying to it in both presentations.
     */
    private get panel(): HTMLElement | null {
        return this.querySelector('.context-menu-panel');
    }

    override updated(): void {
        const panel = this.panel;

        // The sheet's rows are bigger, and that rule lives in the one
        // stylesheet every call site already includes rather than in
        // twelve places. The attribute is how it knows.
        if (panel) panel.toggleAttribute('data-sheet', this.sheet);

        if (this.sheet) {
            this.syncSheet(panel);

            return;
        }

        if (this.popup) {
            if (this.anchor) this.popup.anchor = this.anchor;

            this.popup.active = this.active;
        }
    }

    private syncSheet(panel: HTMLElement | null): void {
        const dialog = this.dialog;

        if (!dialog) return;

        // The dialog is named after the menu it contains, so no call
        // site has to say the same thing twice: the panel already
        // carries `role="menu"` and an `aria-label` naming what it acts
        // on. `without-header` renders no heading, which is
        // `name-dialog`'s documented `aria-label` path.
        const label = panel?.getAttribute('aria-label') || this.label;

        if (label) dialog.setAttribute('label', label);

        nameDialogsIn(this.shadowRoot);

        if (dialog.open !== this.active) dialog.open = this.active;
    }

    /**
     * `wa-dialog` closed itself — Escape, or its own close button.
     * The controller owns `contextMenuOpen`, so it has to hear about
     * it or the menu is left open in state and shut on screen.
     */
    private onDialogShown = (): void => {
        if (!this.active) return;

        this.dispatchEvent(
            new CustomEvent(MENU_SHOWN_EVENT, {
                bubbles: true,
                composed: true,
            }),
        );
    };

    private onDialogHide = (): void => {
        if (!this.active) return;

        this.dispatchEvent(
            new CustomEvent(MENU_DISMISS_EVENT, {
                bubbles: true,
                composed: true,
            }),
        );
    };

    override render() {
        if (this.sheet) {
            // **The anchor stays out of the sheet.** One call site --
            // `page-header`'s overflow menu -- slots its own trigger
            // button as the thing the popup hangs from, and a sheet
            // hangs from the bottom of the screen instead. Rendering
            // that slot outside the dialog is what keeps the button on
            // the page rather than inside the surface it opens.
            return html`
                <slot name="anchor"></slot>
                <wa-dialog
                    without-header
                    data-testid="menu-sheet"
                    @wa-after-show=${this.onDialogShown}
                    @wa-hide=${this.onDialogHide}
                >
                    <div class="grip"></div>
                    <slot></slot>
                </wa-dialog>
            `;
        }

        return html`
            <wa-popup placement=${this.placement} flip shift>
                <slot name="anchor" slot="anchor"></slot>
                <slot></slot>
            </wa-popup>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'menu-surface': MenuSurface;
    }
}

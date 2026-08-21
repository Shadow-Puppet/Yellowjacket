import { css } from 'lit';
import type {
    ReactiveController,
    ReactiveControllerHost,
} from 'lit';
import type WaPopup from '@awesome.me/webawesome/dist/components/popup/popup.js';

/**
 * What this controller needs of a surface: something it can switch on
 * and point at. Both `wa-popup` and `menu-surface` satisfy it.
 */
export type MenuTarget = HTMLElement & {
    active: boolean;
    anchor?: WaPopup['anchor'];
};

import { registerViewAware } from './view-lifecycle';
import {
    MENU_DISMISS_EVENT,
    MENU_SHOWN_EVENT,
} from '../components/menu-surface/menu-surface';

/**
 * Host interface for components using the ContextMenuController.
 * The host must provide access to the popup elements (typically
 * via @query decorators) and optionally a callback for cleanup
 * when the context menu closes.
 */
export interface ContextMenuHost
    extends ReactiveControllerHost {
    updateComplete: Promise<boolean>;
    shadowRoot: ShadowRoot | null;
    /**
     * Return the main context-menu surface.
     *
     * `MenuSurface` since #60, which is a `wa-popup` above 600px and a
     * bottom sheet below it. The type is the narrow shape this
     * controller drives rather than either element, so a host that
     * still renders a bare `wa-popup` — the playlist submenu does —
     * satisfies it unchanged.
     */
    getContextMenuPopup(): MenuTarget | undefined;
    /** Return the playlist submenu surface. */
    getPlaylistSubmenuPopup(): MenuTarget | undefined;
    /**
     * Called when the context menu is closed by an
     * outside click/contextmenu/mousedown.  Components
     * use this to clear domain-specific state (e.g.
     * contextMenuAlbumId, contextMenuGenreName).
     */
    onContextMenuClose?(): void;
}

/** Submenu close delay in milliseconds. */
const SUBMENU_CLOSE_DELAY = 150;

/**
 * How long to keep trying to put focus on a menu's first item.
 *
 * Long enough to outlast `wa-dialog`'s show animation, which ends by
 * focusing the dialog; short enough that a menu which genuinely has no
 * items stops rather than spinning for the life of the page.
 */
const FOCUS_RETRY_BUDGET_MS = 500;

/** A menu item, focusable and clickable. Web Awesome sets `role` itself. */
type MenuItem = HTMLElement & { active?: boolean; disabled?: boolean };

/**
 * Whether a keypress is the conventional "open the context menu" one.
 * Shift+F10 is the long-standing binding; `ContextMenu` is the dedicated
 * key on keyboards that have one.
 */
export function isContextMenuKey(e: KeyboardEvent): boolean {
    return e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10');
}

/**
 * The keyboard model for an open menu panel: focus the first item,
 * Arrow/Home/End to move, Enter/Space to activate, Escape/Tab to close,
 * and focus back where it came from.
 *
 * It is a standalone class rather than part of `ContextMenuController`
 * because `playlist-view` renders a menu without using that controller,
 * and the one thing worse than a menu with no keyboard model is two
 * menus with two different ones.
 */
export class MenuKeyboard {
    private panel: HTMLElement | null = null;

    private restoreFocusTo: HTMLElement | null = null;

    constructor(private readonly onClose: () => void) {}

    /** Bind to a freshly-opened panel and focus its first item. */
    open(panel: HTMLElement | null, opener?: HTMLElement | null): void {
        if (!panel || this.panel === panel) return;

        this.detach();
        this.panel = panel;
        this.restoreFocusTo = opener ?? deepActiveElement();
        panel.addEventListener('keydown', this.onKeydown);
        void this.focusFirstItem(panel);
    }

    /**
     * Take focus back, for a surface that finished showing after we
     * had already placed it.
     *
     * `wa-dialog` focuses `[autofocus]` or *itself* on the animation
     * frame after `showModal()`, and it cannot see our first menu item
     * to prefer it: the panel is slotted through `menu-surface`, so the
     * dialog's own `querySelector` stops at the `<slot>`. Retrying on a
     * longer budget does not fix this either -- the first attempt
     * *succeeds*, and the steal happens afterwards. Measured on the
     * device: the sheet opened with focus on the `<dialog>` and every
     * arrow key went nowhere.
     *
     * So the surface says when it has settled and this re-asserts. It
     * is a no-op for a menu that is closed or that already has focus.
     */
    refocus(): void {
        const panel = this.panel;

        if (!panel || panel.contains(deepActiveElement())) return;

        void this.focusFirstItem(panel);
    }

    /**
     * Focus the first item, once the items are items.
     *
     * The host's `updateComplete` resolves before the `wa-dropdown-item`s
     * inside the panel have run their own first update — and `role` is
     * one of the things they set there. Querying by role at that moment
     * finds nothing, which reads exactly like a menu that opened and
     * refused to take focus.
     */
    private async focusFirstItem(panel: HTMLElement): Promise<void> {
        const candidates = [
            ...panel.querySelectorAll<MenuItem & { updateComplete?: Promise<boolean> }>(
                'wa-dropdown-item, [role^="menuitem"]',
            ),
        ];

        await Promise.all(candidates.map((el) => el.updateComplete ?? null));

        // …and once the surface has shown itself. `wa-popup` places the
        // panel on an animation frame, and `focus()` on a not-yet-shown
        // element is a silent no-op — which looks identical to a menu
        // that opened and refused to take focus.
        //
        // **The budget is time, not frames, because #60 gave this a
        // second kind of surface.** Three frames was enough for a
        // popup; a `wa-dialog` runs a show *animation* and moves focus
        // to the dialog itself when it finishes, which lands after
        // those frames and takes the focus back. Measured on the
        // device: the sheet opened with `document.activeElement` on the
        // `<dialog>`, so every arrow key went nowhere. Retrying to a
        // deadline is `roving-grid`'s rule for the same reason — the
        // thing being waited for is another component's animation, not
        // a fixed number of paints.
        const deadline = Date.now() + FOCUS_RETRY_BUDGET_MS;

        while (Date.now() < deadline) {
            // Bail if the menu closed while we waited.
            if (this.panel !== panel) return;

            const first = this.items()[0];

            this.focusItem(first);

            if (first && panel.contains(deepActiveElement())) return;

            await new Promise((resolve) => requestAnimationFrame(resolve));
        }
    }

    /**
     * Unbind, and give focus back if the menu had it. A click elsewhere
     * closes the menu too, and yanking focus back to the row the user
     * right-clicked a moment ago is worse than leaving it alone.
     */
    close(): void {
        const restoreTo = this.restoreFocusTo;
        const hadFocus = this.panel?.contains(deepActiveElement()) ?? false;

        this.detach();

        if (hadFocus && restoreTo?.isConnected) restoreTo.focus();
    }

    private detach(): void {
        this.panel?.removeEventListener('keydown', this.onKeydown);
        this.panel = null;
        this.restoreFocusTo = null;
    }

    /** The enabled items, in DOM order. */
    private items(): MenuItem[] {
        if (!this.panel) return [];

        return [
            ...this.panel.querySelectorAll<MenuItem>(
                'wa-dropdown-item, [role^="menuitem"]',
            ),
        ].filter(
            (item) =>
                !item.disabled && item.getAttribute('aria-disabled') !== 'true',
        );
    }

    private focusItem(item: MenuItem | undefined): void {
        if (!item) return;

        // `active` is what Web Awesome keys an item's tabindex and its
        // highlight off, so moving focus without it leaves the highlight
        // on whichever item the mouse last touched.
        for (const other of this.items()) other.active = other === item;

        item.tabIndex = 0;
        item.focus();
    }

    private onKeydown = (e: KeyboardEvent): void => {
        const items = this.items();

        if (items.length === 0) return;

        const current = items.findIndex(
            (item) => item === e.target || item.contains(e.target as Node),
        );
        const move = (next: number): void => {
            e.preventDefault();
            e.stopPropagation();
            this.focusItem(items[(next + items.length) % items.length]);
        };

        switch (e.key) {
        case 'ArrowDown':
            move(current + 1);

            break;
        case 'ArrowUp':
            move(current - 1);

            break;
        case 'Home':
            move(0);

            break;
        case 'End':
            move(items.length - 1);

            break;
        case 'Escape':
        case 'Tab':
            // Tab closes rather than moving through the menu: the panel
            // is a bare popup in the host's shadow root, so tabbing out
            // of it lands in the page behind with the menu still open.
            e.preventDefault();
            e.stopPropagation();
            this.onClose();

            break;
        case 'Enter':
        case ' ':
            // These items are in a `wa-popup`, not a `wa-dropdown`, so
            // nothing upstream turns a keypress into an activation.
            e.preventDefault();
            e.stopPropagation();
            items[current]?.click();

            break;
        default:
            break;
        }
    };
}

/** The focused element, resolved through shadow roots. */
function deepActiveElement(): HTMLElement | null {
    let el = document.activeElement as HTMLElement | null;

    while (el?.shadowRoot?.activeElement) {
        el = el.shadowRoot.activeElement as HTMLElement;
    }

    return el;
}

/**
 * Reusable context menu controller that manages the open/close
 * state of a wa-popup context menu with an optional playlist
 * submenu.
 *
 * Handles:
 * - Opening the context menu at a given screen position
 * - Closing on outside click / contextmenu / mousedown
 * - Playlist submenu open/close with hover delay
 * - Document-level event listener lifecycle
 *
 * Does NOT handle:
 * - Rendering the context menu template (component-specific)
 * - Dispatching menu actions (component-specific)
 * - File path resolution for the playlist picker
 */
export class ContextMenuController
    implements ReactiveController
{
    private host: ContextMenuHost;

    /** Whether the main context menu popup is open. */
    contextMenuOpen = false;

    /** Whether the playlist submenu popup is open. */
    playlistSubmenuOpen = false;

    /** File paths to pass to the playlist picker. */
    playlistFilePaths: string[] = [];

    private submenuCloseTimer: ReturnType<
        typeof setTimeout
    > | null = null;

    /** Bound close handler for document events. */
    private closeHandler = () => this.close();

    /**
     * A surface finished showing; see `MenuKeyboard.refocus`.
     *
     * **Not while the submenu is up.** Both surfaces send this, and the
     * submenu's sheet opens *over* the main one -- so re-asserting
     * focus on the main panel's first item would snatch it straight
     * back out of the playlist picker the user just opened.
     */
    private shownHandler = () => {
        if (this.contextMenuOpen && !this.playlistSubmenuOpen) {
            this.keyboard.refocus();
        }
    };

    /** Bound mousedown handler for outside-click detection. */
    private mousedownCloseHandler = (
        e: MouseEvent,
    ) => {
        const path = e.composedPath();
        const popup =
            this.host.getContextMenuPopup();
        const submenu =
            this.host.getPlaylistSubmenuPopup();

        if (popup && path.includes(popup)) return;

        if (submenu && path.includes(submenu)) {
            return;
        }

        this.close();
    };

    constructor(host: ContextMenuHost) {
        this.host = host;
        host.addController(this);
    }

    // =================================================================
    // LIFECYCLE
    // =================================================================

    /** Whether the document listeners are currently installed. */
    private listening = false;

    hostConnected(): void {
        // On a cached view, connection is not the right signal: it never
        // un-happens, so these listeners would stay on the document for
        // the life of the session and close a menu belonging to a page
        // the user left.  A lifecycle host drives attach/detach instead.
        const managed = registerViewAware(this.host, {
            onHostActivate: () => this.attach(),
            onHostDeactivate: () => this.detach(),
        });

        if (!managed) this.attach();
    }

    hostDisconnected(): void {
        this.detach();
        this.keyboard.close();
        this.clearSubmenuCloseTimer();
    }

    private attach(): void {
        if (this.listening) return;

        this.listening = true;
        document.addEventListener(
            'click',
            this.closeHandler,
        );
        document.addEventListener(
            'contextmenu',
            this.closeHandler,
        );
        document.addEventListener(
            'mousedown',
            this.mousedownCloseHandler,
        );
        document.addEventListener(
            MENU_DISMISS_EVENT,
            this.closeHandler,
        );
        document.addEventListener(
            MENU_SHOWN_EVENT,
            this.shownHandler,
        );
    }

    private detach(): void {
        if (!this.listening) return;

        this.listening = false;
        document.removeEventListener(
            MENU_DISMISS_EVENT,
            this.closeHandler,
        );
        document.removeEventListener(
            MENU_SHOWN_EVENT,
            this.shownHandler,
        );
        document.removeEventListener(
            'click',
            this.closeHandler,
        );
        document.removeEventListener(
            'contextmenu',
            this.closeHandler,
        );
        document.removeEventListener(
            'mousedown',
            this.mousedownCloseHandler,
        );
    }

    // =================================================================
    // MAIN CONTEXT MENU
    // =================================================================

    /**
     * Open the context menu at the given screen
     * coordinates using a virtual anchor.
     *
     * `opener` is where focus goes back to on close. It defaults to
     * whatever was focused when the menu opened, which is right for a
     * right-click (usually nothing) and for a keyboard open (the row).
     */
    openAt(clientX: number, clientY: number, opener?: HTMLElement | null): void {
        this.pendingOpener = opener ?? deepActiveElement();
        this.contextMenuOpen = true;
        this.host.requestUpdate();

        void this.host.updateComplete.then(() => {
            const popup =
                this.host.getContextMenuPopup();

            if (!popup) return;

            popup.anchor = {
                getBoundingClientRect() {
                    return new DOMRect(
                        clientX,
                        clientY,
                        0,
                        0,
                    );
                },
            };
            popup.active = true;

            this.bindKeyboard();
        });
    }

    /**
     * Open the menu from an element rather than from a pointer — the
     * Shift+F10 / ContextMenu-key path. Anchors to the element's own box
     * so the menu appears where the thing it acts on is, and restores
     * focus there on close.
     */
    openFrom(el: HTMLElement): void {
        const rect = el.getBoundingClientRect();

        this.openAt(rect.left + 16, rect.top + rect.height / 2, el);
    }

    // =================================================================
    // KEYBOARD
    // =================================================================

    /**
     * Close the context menu and playlist submenu.
     * Notifies the host via `onContextMenuClose()` so
     * it can clear domain-specific state.
     */
    close(): void {
        if (!this.contextMenuOpen) return;

        this.keyboard.close();
        this.closePlaylistSubmenu();
        this.contextMenuOpen = false;
        this.playlistFilePaths = [];

        const popup = this.host.getContextMenuPopup();

        if (popup) {
            popup.active = false;
        }

        this.host.onContextMenuClose?.();
        this.host.requestUpdate();
    }

    /** The menu's keyboard model, shared with the one host that renders
     *  a context menu without this controller. */
    private keyboard = new MenuKeyboard(() => this.close());

    /** The element focus returns to, captured at open and handed to the
     *  keyboard model once the panel exists. */
    private pendingOpener: HTMLElement | null = null;

    private get panel(): HTMLElement | null {
        const popup = this.host.getContextMenuPopup();

        return popup?.querySelector('.context-menu-panel') ?? null;
    }

    private bindKeyboard(): void {
        this.keyboard.open(this.panel, this.pendingOpener);
        this.pendingOpener = null;
    }

    // =================================================================
    // PLAYLIST SUBMENU
    // =================================================================

    /**
     * Open the playlist submenu, positioning it
     * relative to the `.submenu-item` trigger element.
     *
     * @param filePaths - File paths to pass to the
     *   playlist picker.  The caller resolves these
     *   before calling (sync or async).
     */
    async showPlaylistSubmenu(
        filePaths: string[],
    ): Promise<void> {
        this.clearSubmenuCloseTimer();

        if (this.playlistSubmenuOpen) return;

        if (filePaths.length === 0) return;

        this.playlistFilePaths = filePaths;
        this.playlistSubmenuOpen = true;
        this.host.requestUpdate();

        await this.host.updateComplete;

        const submenu =
            this.host.getPlaylistSubmenuPopup();
        const trigger =
            this.host.shadowRoot?.querySelector(
                '.submenu-item',
            );

        if (submenu && trigger) {
            submenu.anchor = trigger;
            submenu.active = true;
        }

        const picker =
            this.host.shadowRoot?.querySelector(
                'playlist-picker',
            ) as
                | (HTMLElement & { reset(): void })
                | null;

        picker?.reset();
    }

    /** Close the playlist submenu. */
    closePlaylistSubmenu(): void {
        this.clearSubmenuCloseTimer();

        if (!this.playlistSubmenuOpen) return;

        this.playlistSubmenuOpen = false;

        const submenu =
            this.host.getPlaylistSubmenuPopup();

        if (submenu) {
            submenu.active = false;
        }

        this.host.requestUpdate();
    }

    /** Clear any pending submenu close timer. */
    clearSubmenuCloseTimer(): void {
        if (this.submenuCloseTimer !== null) {
            clearTimeout(this.submenuCloseTimer);
            this.submenuCloseTimer = null;
        }
    }

    /**
     * Schedule the submenu to close after a short
     * delay.  Used on mouseleave to allow the user to
     * move between the trigger and the submenu popup.
     */
    scheduleSubmenuClose = (): void => {
        this.clearSubmenuCloseTimer();
        this.submenuCloseTimer = setTimeout(() => {
            this.submenuCloseTimer = null;
            this.closePlaylistSubmenu();
        }, SUBMENU_CLOSE_DELAY);
    };

    /**
     * Convenience callback for the playlist-picker's
     * `playlist-action-complete` event.  Closes the
     * entire context menu.
     */
    onPlaylistActionComplete = (): void => {
        this.close();
    };
}

/**
 * Shared CSS styles for context menu and playlist submenu
 * popups.  Components include these via the static styles
 * array: `static override styles = [myStyles, contextMenuStyles]`.
 */
export const contextMenuStyles = css`
    #context-menu {
        z-index: 200;
    }

    /* ---------------------------------------------------------------
       The sheet (#60).

       menu-surface puts data-sheet on the panel when it is drawn
       as a bottom sheet, and these rules are here rather than in that
       component because the panel is the *host's* light DOM: it lives
       in the host's shadow root, so only the host's stylesheet can
       reach it. This file is the one every call site already includes,
       which is what makes twelve menus grow thumb-sized rows from one
       edit.

       Measured on the device before the change: rows were 29px, against
       the 44px floor plan 018 promises and the 48px this issue asks
       for. --------------------------------------------------------- */
    .context-menu-panel[data-sheet] {
        border: none;
        border-radius: 0;
        box-shadow: none;
        min-width: 0;
        padding: 4px 0 8px;
        background-color: transparent;
    }

    .context-menu-panel[data-sheet] wa-dropdown-item {
        font-size: var(--yj-text-md, 0.9375rem);
        min-height: 48px;
        align-items: center;
    }

    .context-menu-panel[data-sheet] wa-dropdown-item::part(base) {
        min-height: 48px;
        align-items: center;
    }

    /* A submenu arrow means "a flyout opens to the right", which is not
       what happens on a phone and is not a thing a thumb can aim at.
       The row still works — it is the tap handler that opens the
       playlist picker — so what goes is the arrow, not the item. */
    .context-menu-panel[data-sheet] .submenu-arrow {
        display: none;
    }

    .context-menu-panel {
        background-color: var(
            --yj-bg-elevated,
            #343a40
        );
        border: 1px solid var(--yj-border, #444);
        border-radius: 6px;
        padding: 4px 0;
        box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
        min-width: 160px;
    }

    .context-menu-panel wa-dropdown-item {
        cursor: pointer;
        --wa-color-text-normal: var(
            --yj-text-primary,
            #fff
        );
        font-size: 13px;
    }

    .context-menu-panel wa-dropdown-item:hover {
        background-color: var(
            --yj-hover-overlay,
            rgba(255, 255, 255, 0.1)
        );
    }

    .submenu-item {
        position: relative;
    }

    .submenu-arrow {
        font-size: 10px;
        margin-left: auto;
        padding-left: 12px;
    }

    #playlist-submenu {
        z-index: 210;
    }
`;

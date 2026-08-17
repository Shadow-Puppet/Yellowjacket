/**
 * Long-press as the touch equivalent of a right-click (plan 016 B2,
 * phase 3).
 *
 * Every context menu in the app opens from a `contextmenu` event —
 * `track-list` and `queue-panel` delegate one on their virtualizer,
 * the card grids and both playlist detail views bind one per row, and
 * `explore-artist-details` binds three. A phone has no right-click, so
 * a phone reached none of them.
 *
 * **This is one document listener, not six components' worth of touch
 * handling.** A press that stays still for `LONG_PRESS_MS` dispatches a
 * synthetic `contextmenu` at the touch point on the element the touch
 * actually landed on, and every existing handler — delegated or
 * per-row, in any shadow root — runs unchanged. Six implementations of
 * a gesture is exactly the fault `ContextMenuController` exists to
 * prevent, and a seam that needs no component to opt in cannot be
 * forgotten by the next component.
 *
 * Three things about it are load-bearing.
 *
 * **The target comes from `composedPath()[0]`, not from
 * `elementFromPoint`**, which stops at the outermost shadow host: every
 * menu in this app is bound inside one, so a synthetic event dispatched
 * on the host reaches a delegated listener and no per-row one.
 *
 * **A browser that already does this must win.** Chromium fires a
 * `contextmenu` on long-press itself; WebKitGTK and the Android WebView
 * vary. So one arriving during the press cancels ours, and one arriving
 * just after ours is swallowed at document capture — where nothing else
 * has seen it yet. The two are told apart by **identity** (a `WeakSet`
 * of the events this module made) rather than by `isTrusted`, so the
 * suppressor cannot eat the event it exists to deliver, the rule holds
 * for anything else in the app that synthesises one, and a test can
 * stand in for a browser that fires its own.
 *
 * **The click that ends the gesture is swallowed.** A row's click
 * selects, and a card's plays; without this, opening a menu also
 * activates the thing under it. It is keyed on the gesture (cleared by
 * the next `pointerdown`) rather than on a time window, so a quick tap
 * on the menu that just opened is not eaten too.
 */

/** How long a press must hold still to mean "menu". */
export const LONG_PRESS_MS = 500;

/**
 * How far a press may drift and still count. Below a finger's own
 * jitter is a gesture nobody can perform; above ~12px it starts
 * stealing the first frames of a scroll.
 */
export const MOVE_TOLERANCE_PX = 10;

/** The active installation, so a second call is a no-op rather than a
 *  second listener set. */
let uninstall: (() => void) | null = null;

/** The events this module dispatched. Identity, not `isTrusted`: see
 *  the note above. */
const ours = new WeakSet<Event>();

/**
 * Install the gesture. Idempotent; returns the uninstaller (which the
 * tests use — the app installs once and never removes it).
 */
export function installLongPressContextMenu(): () => void {
    if (uninstall) return uninstall;

    let timer: ReturnType<typeof setTimeout> | null = null;
    let originX = 0;
    let originY = 0;
    let target: EventTarget | null = null;

    /** A trusted `contextmenu` arrived for this press: the browser has
     *  it covered. */
    let nativeSeen = false;

    /** We opened a menu, and the click ending that gesture is not a
     *  click on anything. */
    let swallowClick = false;

    /** We dispatched one, so a trusted one arriving now is a duplicate. */
    let justFired = false;

    const cancel = (): void => {
        if (timer !== null) clearTimeout(timer);

        timer = null;
        target = null;
    };

    const fire = (): void => {
        timer = null;

        const el = target;

        target = null;

        if (nativeSeen || !el) return;

        justFired = true;
        swallowClick = true;

        const menu = new MouseEvent('contextmenu', {
            bubbles: true,
            cancelable: true,
            // Or it stops at the shadow root the row lives in, and the
            // delegated listeners never see it.
            composed: true,
            clientX: originX,
            clientY: originY,
            button: 2,
        });

        ours.add(menu);
        el.dispatchEvent(menu);
    };

    const onPointerDown = (e: PointerEvent): void => {
        // A new gesture: whatever the last one left behind is stale.
        swallowClick = false;
        justFired = false;
        nativeSeen = false;
        cancel();

        if (e.pointerType !== 'touch' || !e.isPrimary) return;

        originX = e.clientX;
        originY = e.clientY;
        target = e.composedPath()[0] ?? e.target;
        timer = setTimeout(fire, LONG_PRESS_MS);
    };

    const onPointerMove = (e: PointerEvent): void => {
        if (timer === null) return;

        const drifted =
            Math.abs(e.clientX - originX) > MOVE_TOLERANCE_PX ||
            Math.abs(e.clientY - originY) > MOVE_TOLERANCE_PX;

        if (drifted) cancel();
    };

    const onContextMenu = (e: Event): void => {
        // Ours. Everything below is about somebody else's.
        if (ours.has(e)) return;

        if (timer !== null) {
            // The browser got there first, so stand down rather than
            // opening the same menu twice.
            nativeSeen = true;
            cancel();

            return;
        }

        if (justFired) {
            justFired = false;
            e.preventDefault();
            e.stopImmediatePropagation();
        }
    };

    const onClick = (e: Event): void => {
        if (!swallowClick) return;

        swallowClick = false;
        e.preventDefault();
        e.stopImmediatePropagation();
    };

    // Capture throughout: a component handler that stops propagation
    // (every context-menu handler in the app does) must not be able to
    // hide the gesture from this, and the suppressors have to run
    // before anything that would act on the event.
    const opts = { capture: true } as const;

    document.addEventListener('pointerdown', onPointerDown, opts);
    document.addEventListener('pointermove', onPointerMove, opts);
    document.addEventListener('pointerup', cancel, opts);
    document.addEventListener('pointercancel', cancel, opts);
    document.addEventListener('contextmenu', onContextMenu, opts);
    document.addEventListener('click', onClick, opts);
    // A scroll started by something other than the finger (momentum, a
    // programmatic reveal) still means the press was not a press.
    document.addEventListener('scroll', cancel, { capture: true, passive: true });

    uninstall = () => {
        cancel();
        document.removeEventListener('pointerdown', onPointerDown, opts);
        document.removeEventListener('pointermove', onPointerMove, opts);
        document.removeEventListener('pointerup', cancel, opts);
        document.removeEventListener('pointercancel', cancel, opts);
        document.removeEventListener('contextmenu', onContextMenu, opts);
        document.removeEventListener('click', onClick, opts);
        document.removeEventListener('scroll', cancel, opts);
        uninstall = null;
    };

    return uninstall;
}

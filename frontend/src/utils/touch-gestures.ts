/**
 * The touch gestures, as one document listener (plan 019, #63).
 *
 * This replaces `utils/long-press.ts` rather than sitting beside it,
 * and that is the point: two document listeners both claiming the
 * 500ms hold is exactly the fault that file's own header warns about.
 * What it did — one capture listener, the target from
 * `composedPath()[0]`, the browser's own gesture winning, the trailing
 * click swallowed — is kept whole. What changes is what the gesture
 * *means*.
 *
 * **A gesture is announced, not acted on.** Two composed, cancelable
 * events are dispatched on the element the finger actually landed on:
 *
 *   `yj-tap`         a short press that did not drift
 *   `yj-long-press`  a press that held still for LONG_PRESS_MS
 *
 * A component that wants the gesture handles it and calls
 * `preventDefault()`. Nothing else changes. That shape is what lets
 * this reassign the hold without touching a single one of the fourteen
 * context menus downstream of it: **an unclaimed `yj-long-press` still
 * becomes a synthetic `contextmenu`**, so a card grid, an Explore
 * result or a playlist row behaves exactly as it did, and only the
 * lists that opt in get selection mode.
 *
 * The same rule keeps taps honest. An unclaimed `yj-tap` does nothing
 * at all and the browser's click follows normally, so every button,
 * link and checkbox in the app is untouched by this file. Only a
 * claimed tap has its click swallowed — otherwise playing a track
 * would also select it.
 *
 * Five things are load-bearing.
 *
 * **The predicate is the pointer, not the platform** (plan 019,
 * decision 1). `pointerType === 'touch'`, per event — so an Android
 * tablet over 600px, a touchscreen laptop with a mouse also plugged
 * in, and a narrow desktop window are all right for free, and there is
 * no second declaration of what a phone does. Keyed on a viewport
 * width, the first of those three gets desktop semantics on a
 * touchscreen, which is the inversion #63 exists to fix, on the
 * platform it exists for.
 *
 * **There is no double-tap**, and it is not an omission — see plan
 * 019, decision 2. Measured on the reference device, the play command
 * to `TrackChanged` is ~100ms; a double-tap discriminator has to hold
 * every tap for the app's own `DOUBLE_CLICK_GRACE_MS` of 250 before it
 * can act, which is 3.5x the primary interaction in the app to reach a
 * menu that long-press already reaches.
 *
 * **The target comes from `composedPath()[0]`**, not
 * `elementFromPoint`, which stops at the outermost shadow host: every
 * list in this app delegates inside one, so an event dispatched on the
 * host reaches a delegated listener and no per-row one.
 *
 * **A browser that fires its own `contextmenu` is a trigger, not a
 * competitor**, and that is a change from `long-press.ts` rather than
 * an inherited rule. It used to stand down when a trusted
 * `contextmenu` arrived, because both paths ended in the same place: a
 * context menu. They no longer do — ours may end in selection mode —
 * so standing down means the gesture silently does the *old* thing.
 *
 * Measured on the reference device, which is the only tier that can
 * see this: Chrome 113's WebView fires its own `contextmenu` on a long
 * press, so a hold on a track row opened the context menu and
 * `yj-long-press` was never announced at all. Every test in the
 * component tier passed, because dispatched pointer events do not make
 * a browser synthesise one.
 *
 * So a trusted `contextmenu` arriving mid-press *becomes* the long
 * press: `yj-long-press` is announced from it, and only if a component
 * claims it is the native event suppressed. Unclaimed, it propagates
 * untouched and opens the menu it always did — which is the same
 * "browser wins" outcome, now reached by asking rather than assuming.
 *
 * Ours and the browser's are still told apart by identity rather than
 * `isTrusted` — a `WeakSet` of the events this module made — so the
 * suppressor cannot eat the event it exists to deliver, and a test can
 * stand in for a browser that fires one.
 *
 * **The click swallow is keyed on the gesture**, cleared by the next
 * `pointerdown` rather than by a time window, so the first tap on a
 * sheet that just opened is not eaten too.
 */

/** How long a press must hold still to mean "long press". */
export const LONG_PRESS_MS = 500;

/**
 * How far a press may drift and still count. Below a finger's own
 * jitter is a gesture nobody can perform; above ~12px it starts
 * stealing the first frames of a scroll.
 */
export const MOVE_TOLERANCE_PX = 10;

/** Detail carried by both gesture events. */
export interface GestureDetail {
    /** Where the finger was, in client coordinates — a menu opens here. */
    x: number;
    y: number;
}

export type GestureEvent = CustomEvent<GestureDetail>;

declare global {
    interface HTMLElementEventMap {
        'yj-tap': GestureEvent;
        'yj-long-press': GestureEvent;
    }
}

/** The active installation, so a second call is a no-op rather than a
 *  second listener set. */
let uninstall: (() => void) | null = null;

/** The events this module dispatched. Identity, not `isTrusted`. */
const ours = new WeakSet<Event>();

/**
 * Install the gestures. Idempotent; returns the uninstaller (which the
 * tests use — the app installs once and never removes it).
 */
export function installTouchGestures(): () => void {
    if (uninstall) return uninstall;

    let timer: ReturnType<typeof setTimeout> | null = null;
    let originX = 0;
    let originY = 0;
    let target: EventTarget | null = null;

    /** The press is still a candidate for a tap: it has neither
     *  drifted nor become a long press. */
    let tapCandidate = false;

    /** A trusted `contextmenu` arrived for this press: the browser has
     *  it covered. */
    let nativeSeen = false;

    /** A gesture was claimed, and the click ending it is not a click on
     *  anything. */
    let swallowClick = false;

    /** We dispatched a `contextmenu`, so a trusted one arriving now is
     *  a duplicate. */
    let justFired = false;

    const cancel = (): void => {
        if (timer !== null) clearTimeout(timer);

        timer = null;
        target = null;
        tapCandidate = false;
    };

    /**
     * Announce a gesture on the element the finger landed on.
     * Returns whether a component claimed it.
     */
    const announce = (name: 'yj-tap' | 'yj-long-press', el: EventTarget): boolean => {
        const event: GestureEvent = new CustomEvent<GestureDetail>(name, {
            bubbles: true,
            cancelable: true,
            // Or it stops at the shadow root the row lives in, and the
            // delegated listeners never see it.
            composed: true,
            detail: { x: originX, y: originY },
        });

        ours.add(event);
        el.dispatchEvent(event);

        return event.defaultPrevented;
    };

    const fireContextMenu = (el: EventTarget): void => {
        justFired = true;

        const menu = new MouseEvent('contextmenu', {
            bubbles: true,
            cancelable: true,
            composed: true,
            clientX: originX,
            clientY: originY,
            button: 2,
        });

        ours.add(menu);
        el.dispatchEvent(menu);
    };

    const onLongPress = (): void => {
        timer = null;
        tapCandidate = false;

        const el = target;

        target = null;

        if (nativeSeen || !el) return;

        // The gesture happened either way, so the click that ends it is
        // never a click on anything -- whether a list claimed it for
        // selection mode or a card grid let it fall through to a menu.
        swallowClick = true;

        // An unclaimed long press is what it has always been. This is
        // the whole reason the fourteen context menus need no change.
        if (!announce('yj-long-press', el)) fireContextMenu(el);
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
        tapCandidate = true;
        timer = setTimeout(onLongPress, LONG_PRESS_MS);
    };

    const onPointerMove = (e: PointerEvent): void => {
        if (timer === null) return;

        const drifted =
            Math.abs(e.clientX - originX) > MOVE_TOLERANCE_PX ||
            Math.abs(e.clientY - originY) > MOVE_TOLERANCE_PX;

        // A drifted press is neither gesture -- it is a scroll, and the
        // virtualizer's, not ours.
        if (drifted) cancel();
    };

    const onPointerUp = (): void => {
        const el = target;
        const wasTap = tapCandidate && timer !== null;

        // Clears the long-press timer, so a tap cannot also become one.
        cancel();

        if (!wasTap || !el) return;

        // Only a *claimed* tap swallows its click. An unclaimed one has
        // to fall through untouched, or every button in the app stops
        // working.
        if (announce('yj-tap', el)) swallowClick = true;
    };

    const onContextMenu = (e: Event): void => {
        // Ours. Everything below is about somebody else's.
        if (ours.has(e)) return;

        if (timer !== null) {
            // The browser recognised the same hold this was timing.
            // Use its event as the trigger rather than racing it --
            // and rather than standing down, which is what the old
            // rule did and which now silently means "do the thing this
            // gesture used to do".
            const el = e.composedPath()[0] ?? e.target;

            nativeSeen = true;
            cancel();

            if (!el) return;

            swallowClick = true;

            // Claimed: the component wants selection mode, so the
            // browser's menu must not also open. Unclaimed: let it
            // through exactly as before.
            if (announce('yj-long-press', el)) {
                e.preventDefault();
                e.stopImmediatePropagation();
            }

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
    document.addEventListener('pointerup', onPointerUp, opts);
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
        document.removeEventListener('pointerup', onPointerUp, opts);
        document.removeEventListener('pointercancel', cancel, opts);
        document.removeEventListener('contextmenu', onContextMenu, opts);
        document.removeEventListener('click', onClick, opts);
        document.removeEventListener('scroll', cancel, opts);
        uninstall = null;
    };

    return uninstall;
}

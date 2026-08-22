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
 *   `yj-swipe-start` a press that has travelled decisively sideways
 *
 * A claimed swipe is then followed by `yj-swipe-move` and one
 * `yj-swipe-end`, which is guaranteed: a swipe that the browser or a
 * second finger takes away still ends, with `canceled` set, so the
 * affordance a component put on screen always has something to snap
 * back from.
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
 *
 * ## The swipe runs on touch events, and that is not a style choice
 *
 * Everything above is Pointer Events. The swipe is not, and the reason
 * is measured on the reference device rather than reasoned about:
 * **Chrome 113's Android WebView cancels the pointer stream ~16px into
 * any drag, whatever `touch-action` says.** Three values were tried on
 * a track row, driving a real finger with `adb shell input swipe`:
 *
 * ```
 * touch-action: auto   pointerdown, 1 move,  pointercancel
 * touch-action: pan-y  pointerdown, 2 moves, pointercancel
 * touch-action: none   pointerdown, 2 moves, pointercancel
 * ```
 *
 * `touchmove` kept firing throughout all three. So a swipe recognised
 * from `pointermove` is a swipe that dies 16px in — plan 019 predicted
 * the class of failure ("works in Chromium and not on the phone") and
 * named `touch-action: pan-y` as the fix; it is half of it.
 *
 * The other half is that **a non-passive `touchmove` that calls
 * `preventDefault()` is what keeps the gesture ours**. With it, the
 * same swipe ran to 12 moves and a `pointerup` at full travel.
 *
 * Both halves are required, and that was measured too: with the
 * `preventDefault` in place but `touch-action` back at `auto`, the
 * gesture died after **one** move. The reading is that `auto` lets the
 * browser commit to a horizontal pan on the first move past slop —
 * before any threshold of ours can have been crossed — while `pan-y`
 * leaves it undecided long enough for the second move to claim it.
 *
 * So a surface that wants a horizontal swipe declares
 * `touch-action: pan-y` (`track-list`'s `.track-row` does) *and* gets
 * this module's `preventDefault`. Neither alone works on the device,
 * and **both work in Chromium either way**, which is exactly why this
 * paragraph exists rather than a test.
 *
 * `touch-action: none` is the one value to avoid: it also takes the
 * list's vertical scrolling away, which was measured as a list that
 * would not move.
 *
 * Two consequences of the touch listener worth knowing.
 *
 * **It is non-passive, which costs the compositor's scroll fast path**
 * for the first touchmoves of every scroll, until the browser starts
 * scrolling and stops waiting on us. That is the standard price of a
 * horizontal gesture in a scroller and it is paid once per gesture,
 * not per frame; a vertical drag on the device still scrolls the
 * virtualizer 81px on the same measurement that the horizontal one
 * survives.
 *
 * **The tie breaks toward scrolling**, deliberately and in that order:
 * vertical drift past the tolerance vetoes the swipe outright, and a
 * gesture that is not *strictly* more horizontal than vertical is the
 * scroller's. A list that will not scroll is unusable; a swipe that
 * needs a second try is not.
 */

/** How long a press must hold still to mean "long press". */
export const LONG_PRESS_MS = 500;

/**
 * How far a press may drift and still count. Below a finger's own
 * jitter is a gesture nobody can perform; above ~12px it starts
 * stealing the first frames of a scroll.
 */
export const MOVE_TOLERANCE_PX = 10;

/**
 * How far a press must travel sideways before it is a swipe.
 *
 * It has a ceiling the other constants do not: the browser's own
 * decision is made a little past this, so a threshold much higher is a
 * gesture the device never delivers. Measured, the second `touchmove`
 * of an `adb input swipe` lands at ~19px and the pointer stream dies
 * just after it, so 12 is inside that window with room for a slower
 * finger.
 */
export const SWIPE_START_PX = 12;

/** Detail carried by both gesture events. */
export interface GestureDetail {
    /** Where the finger was, in client coordinates — a menu opens here. */
    x: number;
    y: number;
}

/** Detail carried by the three swipe events. */
export interface SwipeDetail {
    /** Travel from where the finger landed. Signed: right is positive. */
    dx: number;
    dy: number;
    /**
     * The gesture was taken away rather than finished — a second
     * finger, a `touchcancel`, a scroll underneath. Only ever true on
     * `yj-swipe-end`, and it is the difference between "do the thing"
     * and "put the row back".
     */
    canceled: boolean;
}

export type GestureEvent = CustomEvent<GestureDetail>;
export type SwipeEvent = CustomEvent<SwipeDetail>;

declare global {
    interface HTMLElementEventMap {
        'yj-tap': GestureEvent;
        'yj-long-press': GestureEvent;
        'yj-swipe-start': SwipeEvent;
        'yj-swipe-move': SwipeEvent;
        'yj-swipe-end': SwipeEvent;
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

    /**
     * This press has already produced its outcome, so a trusted
     * `contextmenu` arriving now is a duplicate of it.
     *
     * It covers **both** outcomes, and that is a fix rather than a
     * tidy-up. `nativeSeen` handles the browser's menu arriving
     * *during* the hold; the reverse order was never handled, and it
     * happens: measured on the reference device over four holds, two
     * of them fired our 500ms timer and then delivered a trusted
     * `contextmenu` 50-70ms later, which nothing suppressed — so the
     * context menu opened on top of the selection bar, intermittently,
     * on exactly the surface #63 exists to have changed. Neither the
     * component tier nor the e2e tier can see it: no browser they run
     * in synthesises a `contextmenu` from a dispatched press at all.
     */
    let justFired = false;

    // --- the swipe, which runs on touch events; see the header ------

    /** Where the finger landed, and what it landed on. */
    let swipeTarget: EventTarget | null = null;
    let swipeOriginX = 0;
    let swipeOriginY = 0;

    /** The last travel, kept so a `touchcancel` — which carries no
     *  coordinates for a touch that is already gone — can still say how
     *  far the row had moved. */
    let lastDx = 0;
    let lastDy = 0;

    /** A component claimed the swipe: it is ours until the finger
     *  lifts, and every `touchmove` is prevented. */
    let swiping = false;

    /** This press can no longer become a swipe — it went vertical, a
     *  second finger arrived, or nobody claimed it. */
    let swipeVetoed = false;

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

        // The press is answered, so a trusted `contextmenu` for it is
        // late rather than new. `fireContextMenu` sets this too; it is
        // set here as well so the *claimed* branch is covered, which
        // is the branch that was showing a menu over the bar.
        justFired = true;

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

    /**
     * Announce a swipe on the element the finger landed on.
     * Returns whether a component claimed it (only `start` asks).
     */
    const announceSwipe = (
        name: 'yj-swipe-start' | 'yj-swipe-move' | 'yj-swipe-end',
        el: EventTarget,
        canceled = false,
    ): boolean => {
        const event: SwipeEvent = new CustomEvent<SwipeDetail>(name, {
            bubbles: true,
            cancelable: name === 'yj-swipe-start',
            composed: true,
            detail: { dx: lastDx, dy: lastDy, canceled },
        });

        ours.add(event);
        el.dispatchEvent(event);

        return event.defaultPrevented;
    };

    /**
     * End a claimed swipe, once.
     *
     * Every exit from a swipe comes through here so that `yj-swipe-end`
     * is guaranteed: a component that has put a reveal on screen and a
     * row half off its own left edge has no other way to learn the
     * gesture is over.
     */
    const endSwipe = (canceled: boolean): void => {
        const el = swipeTarget;

        swipeTarget = null;

        if (!swiping) return;

        swiping = false;

        if (!el) return;

        // The gesture happened, so the click that ends it is not a
        // click on the row it ended over.
        swallowClick = true;
        announceSwipe('yj-swipe-end', el, canceled);
    };

    const onTouchStart = (e: TouchEvent): void => {
        endSwipe(true);

        lastDx = 0;
        lastDy = 0;

        // A second finger is a pinch or a scroll, never one of ours.
        swipeVetoed = e.touches.length !== 1;

        if (swipeVetoed) return;

        const touch = e.touches[0];

        if (!touch) return;

        swipeOriginX = touch.clientX;
        swipeOriginY = touch.clientY;
        // `composedPath()[0]` for the reason the press path uses it: a
        // list delegates inside its own shadow root.
        swipeTarget = e.composedPath()[0] ?? e.target;
    };

    const onTouchMove = (e: TouchEvent): void => {
        if (swipeVetoed || !swipeTarget) return;

        if (e.touches.length !== 1) {
            endSwipe(true);
            swipeVetoed = true;

            return;
        }

        const touch = e.touches[0];

        if (!touch) return;

        lastDx = touch.clientX - swipeOriginX;
        lastDy = touch.clientY - swipeOriginY;

        if (swiping) {
            // This is what keeps the stream alive on the device. It is
            // only ever reached for a *claimed* swipe, so nothing that
            // scrolls is ever prevented.
            e.preventDefault();
            announceSwipe('yj-swipe-move', swipeTarget);

            return;
        }

        // Vertical first: past the tolerance the list has it, and a
        // gesture that is exactly diagonal is the list's too.
        if (
            Math.abs(lastDy) > MOVE_TOLERANCE_PX &&
            Math.abs(lastDy) >= Math.abs(lastDx)
        ) {
            swipeVetoed = true;
            swipeTarget = null;

            return;
        }

        if (
            Math.abs(lastDx) < SWIPE_START_PX ||
            Math.abs(lastDx) <= Math.abs(lastDy)
        ) {
            return;
        }

        if (!announceSwipe('yj-swipe-start', swipeTarget)) {
            // Nobody wants it. Leave the gesture to the browser rather
            // than holding it open for the rest of the press.
            swipeVetoed = true;
            swipeTarget = null;

            return;
        }

        swiping = true;

        // It is not a tap and it is not a hold.
        cancel();
        e.preventDefault();
    };

    const onTouchEnd = (): void => {
        endSwipe(false);
    };

    const onTouchCancel = (): void => {
        endSwipe(true);
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

    // Non-passive, because `onTouchMove` has to be able to prevent the
    // default for a claimed swipe -- see the header. The other three
    // are passive: they only read.
    const blocking = { capture: true, passive: false } as const;
    const listening = { capture: true, passive: true } as const;

    /** A surface moved under the finger: neither gesture survives it. */
    const abort = (): void => {
        endSwipe(true);
        cancel();
    };

    document.addEventListener('pointerdown', onPointerDown, opts);
    document.addEventListener('pointermove', onPointerMove, opts);
    document.addEventListener('pointerup', onPointerUp, opts);
    document.addEventListener('pointercancel', cancel, opts);
    document.addEventListener('contextmenu', onContextMenu, opts);
    document.addEventListener('click', onClick, opts);
    document.addEventListener('touchstart', onTouchStart, listening);
    document.addEventListener('touchmove', onTouchMove, blocking);
    document.addEventListener('touchend', onTouchEnd, listening);
    document.addEventListener('touchcancel', onTouchCancel, listening);
    // A scroll started by something other than the finger (momentum, a
    // programmatic reveal) still means the press was not a press.
    document.addEventListener('scroll', abort, listening);

    uninstall = () => {
        abort();
        document.removeEventListener('pointerdown', onPointerDown, opts);
        document.removeEventListener('pointermove', onPointerMove, opts);
        document.removeEventListener('pointerup', onPointerUp, opts);
        document.removeEventListener('pointercancel', cancel, opts);
        document.removeEventListener('contextmenu', onContextMenu, opts);
        document.removeEventListener('click', onClick, opts);
        document.removeEventListener('touchstart', onTouchStart, opts);
        document.removeEventListener('touchmove', onTouchMove, opts);
        document.removeEventListener('touchend', onTouchEnd, opts);
        document.removeEventListener('touchcancel', onTouchCancel, opts);
        document.removeEventListener('scroll', abort, opts);
        uninstall = null;
    };

    return uninstall;
}

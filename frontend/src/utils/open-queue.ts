/**
 * Opening the queue, from the two buttons that do it.
 *
 * **The queue is a place while it is covering the content, and a
 * control while it sits beside it** (#55). Those are not two components
 * and not two mount points — they are the two presentations #24 already
 * computes, and this is the one line that turns that measurement into a
 * navigation decision.
 *
 * A column is a thing the user docked: back must not undock it, and
 * navigating to Albums must not take it away. An overlay is a screen —
 * at the reference device's 424x439 it is 424x318, which is
 * `.main-panel`'s rect exactly — so it needs the two things a screen
 * has and this one did not: an entry in the back stack, and a way out
 * that answers the platform's own gesture. Measured before this existed:
 * opening the queue on Artists and pressing back moved the page
 * *underneath* to Albums and left the queue up.
 *
 * The mode is read off the panel rather than from a viewport width, for
 * the reason `queue-panel.overlay` is computed at all: the panel is
 * drag-resizable between 200 and 500px and persisted, so a breakpoint
 * is wrong by up to 180px in the direction that hurts.
 */
export function queuePanelElement(): HTMLElement | null {
    return document.getElementById('queue-panel');
}

/** Whether the queue is currently a screen rather than a column. */
export function queueIsAScreen(): boolean {
    return queuePanelElement()?.hasAttribute('overlay') ?? false;
}

/**
 * Show the queue: a navigation where it is a screen, an attribute where
 * it is a column.
 *
 * Both routes end at the same `open` attribute on the same element —
 * `index.ts` handles `navigate {view: 'queue'}` by setting it — because
 * the panel's state is one fact and a second mechanism for it is a
 * second thing to keep in step.
 */
export function openQueue(): void {
    if (queueIsAScreen()) {
        document.dispatchEvent(new CustomEvent('navigate', {
            bubbles: true,
            composed: true,
            detail: { view: 'queue' },
        }));

        return;
    }

    queuePanelElement()?.setAttribute('open', '');
}

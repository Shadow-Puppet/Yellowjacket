/**
 * How far the session can go back and forward.
 *
 * The History API exposes `length` and nothing useful: it counts
 * entries the app did not push, does not say where in the list the
 * current entry is, and `popstate` fires *identically* whether the
 * user went back or forward. So a control that wants to grey itself
 * out has to be told, and the shell is the only thing in a position to
 * know (#6).
 *
 * Two rules follow from how the shell counts, and both are the reason
 * this is a pair of booleans rather than one depth:
 *
 * **Forward is not "back, negated".** `pushedEntries` -- the counter
 * this replaces -- decremented on every `popstate`, which made a
 * forward navigation look like a second back. The shell keeps an index
 * per entry and a high-water mark instead, and publishes the two
 * answers rather than the arithmetic.
 *
 * **Back stops at the app's own floor.** The launch entry is
 * *replaced*, not pushed, so that one back press from the root exits
 * the app on Android; `canBack` is false there, which is what stops
 * the header's own button being the thing that quits.
 */

type Subscriber = () => void;

export interface HistoryDepth {
    canBack: boolean;
    canForward: boolean;
}

class HistoryStore {
    private depth: HistoryDepth = { canBack: false, canForward: false };

    private subscribers = new Set<Subscriber>();

    get(): HistoryDepth {
        return this.depth;
    }

    /** Called by the shell whenever an entry is pushed or restored. */
    setDepth(canBack: boolean, canForward: boolean): void {
        if (
            canBack === this.depth.canBack &&
            canForward === this.depth.canForward
        ) {
            return;
        }

        this.depth = { canBack, canForward };
        this.notify();
    }

    subscribe(fn: Subscriber): () => void {
        this.subscribers.add(fn);

        return () => this.subscribers.delete(fn);
    }

    private notify(): void {
        this.subscribers.forEach((fn) => fn());
    }
}

export const historyStore = new HistoryStore();

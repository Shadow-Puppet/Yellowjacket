/**
 * The app's one notification surface.
 *
 * Before this, 84 `catch` blocks ended at `console.error` and two
 * components had grown private, mutually-unaware toasts. The audit's
 * ~30 "the failure is invisible" findings are one problem wearing
 * thirty hats: there was nowhere to put a message.
 *
 * Four levels, and the **caller** picks, using one rule: *a failure is
 * only worth interrupting for if the user can do something about it
 * that they are not already doing.*
 *
 * | Level        | Behaviour                          | For |
 * |--------------|------------------------------------|-----|
 * | `blocking`   | modal, must be acknowledged        | data at risk; must be known before continuing |
 * | `persistent` | stays until dismissed, with an action | something asked for did not happen and retrying is meaningful |
 * | `transient`  | toast, auto-dismisses              | small action failed and the state visibly reverted anyway |
 * | `inline`     | rendered in the region that failed | the failure belongs to one panel; a global message would be noise |
 *
 * Coalescing lives here, not in the call sites: a queue of 200
 * unplayable files is one message with a count, and that holds for
 * every future caller without anyone remembering it.
 */

export type NotificationLevel =
    | 'blocking'
    | 'persistent'
    | 'transient'
    | 'inline';

export type NotificationTone = 'error' | 'warning' | 'info' | 'success';

export interface NotificationAction {
    label: string;
    run: () => void | Promise<void>;
}

export interface NotifyInput {
    level: NotificationLevel;
    /** The sentence. Written for a person; see `utils/describe-error`. */
    text: string;
    /**
     * Coalescing identity within a level (and, for `inline`, a region).
     * Defaults to the text, which is right for anything that does not
     * name a specific file or item.
     */
    key?: string;
    /** Which region renders this. `inline` only; ignored otherwise. */
    region?: string;
    /** A heading, for the levels that have room for one. */
    title?: string;
    tone?: NotificationTone;
    /** Offered to the user; dismisses the notification when it runs. */
    action?: NotificationAction;
    /** Raw error text. Never rendered as the sentence; available for a
     *  details disclosure and always worth keeping. */
    detail?: string;
    /** The sentence to use once this has happened more than once. */
    coalescedText?: (count: number) => string;
}

export interface Notification extends NotifyInput {
    id: number;
    key: string;
    tone: NotificationTone;
    /** How many occurrences this message stands for. */
    count: number;
    createdAt: number;
}

type Subscriber = () => void;

/** A toast is gone before the user can read it twice. */
const TransientTimeoutMillis = 6000;

/** Inline messages are about the panel the user is looking at. */
const InlineTimeoutMillis = 8000;

/** Occurrences within this window are one message with a count. */
const CoalesceWindowMillis = 10_000;

/** Beyond this the stack is noise; the oldest dismissible one goes. */
const MaxVisible = 5;

function coalesceKey(input: NotifyInput, key: string): string {
    return `${input.level}\u0000${input.region ?? ''}\u0000${key}`;
}

class NotificationStore {
    private items: Notification[] = [];
    private subscribers = new Set<Subscriber>();
    private notifyScheduled = false;
    private timers = new Map<number, number>();
    private lastSeen = new Map<string, { id: number; at: number }>();
    private seq = 0;

    // ===================================================================
    // RAISING
    // ===================================================================

    /** Raise a notification, or fold it into the one it repeats. */
    notify(input: NotifyInput): number {
        const now = Date.now();
        const key = input.key ?? input.text;
        const ck = coalesceKey(input, key);
        const previous = this.lastSeen.get(ck);
        const existing =
            previous && now - previous.at < CoalesceWindowMillis
                ? this.items.find((n) => n.id === previous.id)
                : undefined;

        if (existing) {
            const count = existing.count + 1;

            this.replace({
                ...existing,
                ...input,
                key,
                tone: input.tone ?? existing.tone,
                count,
                text: input.coalescedText?.(count) ?? input.text,
            });
            this.lastSeen.set(ck, { id: existing.id, at: now });
            this.arm(existing.id, input.level);

            return existing.id;
        }

        this.seq += 1;

        const notification: Notification = {
            ...input,
            id: this.seq,
            key,
            tone: input.tone ?? 'error',
            count: 1,
            createdAt: now,
        };

        this.items = [...this.items, notification];
        this.lastSeen.set(ck, { id: notification.id, at: now });
        this.trim();
        this.arm(notification.id, input.level);
        this.emit();

        return notification.id;
    }

    /** Modal. Rare by construction — argue for a third caller. */
    blocking(input: Omit<NotifyInput, 'level'>): number {
        return this.notify({ ...input, level: 'blocking' });
    }

    /** Stays until dismissed. Give it an action worth taking. */
    persistent(input: Omit<NotifyInput, 'level'>): number {
        return this.notify({ ...input, level: 'persistent' });
    }

    /** A toast. The state has already reverted; this only says so. */
    transient(input: Omit<NotifyInput, 'level'>): number {
        return this.notify({ ...input, level: 'transient' });
    }

    /** Rendered by `<inline-notice region="…">`, never as a toast. */
    inline(region: string, input: Omit<NotifyInput, 'level' | 'region'>): number {
        return this.notify({ ...input, level: 'inline', region });
    }

    // ===================================================================
    // DISMISSING
    // ===================================================================

    dismiss(id: number): void {
        const before = this.items.length;

        this.items = this.items.filter((n) => n.id !== id);
        this.clearTimer(id);

        if (this.items.length !== before) this.emit();
    }

    /** Everything one region is showing, e.g. on navigating away. */
    dismissRegion(region: string): void {
        const remaining = this.items.filter((n) => n.region !== region);

        if (remaining.length === this.items.length) return;

        for (const n of this.items) {
            if (n.region === region) this.clearTimer(n.id);
        }

        this.items = remaining;
        this.emit();
    }

    /** Run a notification's action and dismiss it. */
    runAction(id: number): void {
        const item = this.items.find((n) => n.id === id);

        if (!item?.action) return;

        this.dismiss(id);
        void item.action.run();
    }

    clear(): void {
        for (const id of [...this.timers.keys()]) this.clearTimer(id);
        this.lastSeen.clear();

        if (this.items.length === 0) return;

        this.items = [];
        this.emit();
    }

    // ===================================================================
    // READING
    // ===================================================================

    getAll(): readonly Notification[] {
        return this.items;
    }

    byLevel(level: NotificationLevel): Notification[] {
        return this.items.filter((n) => n.level === level);
    }

    /** The one modal to show, if any. Blocking is one at a time. */
    currentBlocking(): Notification | null {
        return this.items.find((n) => n.level === 'blocking') ?? null;
    }

    forRegion(region: string): Notification[] {
        return this.items.filter(
            (n) => n.level === 'inline' && n.region === region,
        );
    }

    // ===================================================================
    // SUBSCRIPTION
    // ===================================================================

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    // ===================================================================
    // INTERNALS
    // ===================================================================

    private replace(next: Notification): void {
        this.items = this.items.map((n) => (n.id === next.id ? next : n));
        this.emit();
    }

    /** Start (or restart) the self-dismissal timer for a level that has
     *  one. Blocking and persistent wait for the user. */
    private arm(id: number, level: NotificationLevel): void {
        this.clearTimer(id);

        const timeout =
            level === 'transient'
                ? TransientTimeoutMillis
                : level === 'inline'
                  ? InlineTimeoutMillis
                  : 0;

        if (timeout === 0) return;

        this.timers.set(
            id,
            window.setTimeout(() => {
                this.timers.delete(id);
                this.dismiss(id);
            }, timeout),
        );
    }

    private clearTimer(id: number): void {
        const timer = this.timers.get(id);

        if (timer !== undefined) {
            clearTimeout(timer);
            this.timers.delete(id);
        }
    }

    /** Keep the stack readable: drop the oldest thing the user has not
     *  been asked to acknowledge. */
    private trim(): void {
        while (this.items.length > MaxVisible) {
            const victim = this.items.find((n) => n.level !== 'blocking');

            if (!victim) return;

            this.clearTimer(victim.id);
            this.items = this.items.filter((n) => n.id !== victim.id);
        }
    }

    private emit(): void {
        if (this.notifyScheduled) return;
        this.notifyScheduled = true;
        queueMicrotask(() => {
            this.notifyScheduled = false;
            for (const sub of this.subscribers) sub();
        });
    }
}

/** Singleton: one surface, or it is not a surface. */
export const notificationStore = new NotificationStore();

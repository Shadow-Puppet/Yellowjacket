import { EventsOn } from '@runtime/runtime';
import { GetPopupVolume } from '@go/config/config.js';
import { SystemOwnsVolume } from '@go/player/player.js';
import { Events } from '../events';

type Subscriber = () => void;

/**
 * Whether the volume control is a click-to-open popup (#42).
 *
 * The popup was the only option, and "click open, drag, click closed"
 * is three gestures for a control a bottom bar has room to just show.
 * So an inline slider is the default and the popup is a setting.
 *
 * **The stored flag names the popup, not the slider**, which is the
 * polarity rule `backend/config` states for every option it has: the
 * zero value has to be the intended answer. An `InlineVolume bool`
 * would default to false, hand the popup to every existing install, and
 * need a migration to say what the default already says.
 *
 * It is a store rather than a field on the component because two
 * components render `<volume-control>` — the bottom bar and the phone's
 * full-screen now-playing view — and a setting that only reached
 * whichever one happened to mount after it changed is the fault
 * `active-view-store` exists to prevent, one surface over.
 *
 * The initial value is the *default* rather than a pending answer, so
 * the first paint is the inline slider and not an empty gap that
 * becomes one. An install that has chosen the popup sees it swap once
 * on load, which is the cheaper of the two wrong first frames: the
 * inline slider occupies the space the popup's button would have.
 *
 * **`available` is the question one step earlier — whether there is a
 * volume of ours to draw at all (#64).** On Android the hardware keys
 * are the volume control and the backend pins its own level at
 * maximum, so a slider here would move nothing.
 *
 * It is asked of the *player* rather than of the viewport, and that is
 * the whole design decision. Every other stand-down rule in this app
 * is a width, because a width is what a browser can answer and what
 * every tier can test — but this one is a property of the build. Keyed
 * on width instead, an Android tablet at 600px or more would draw the
 * bottom bar's slider over a pinned level: a control that cannot act,
 * which `library-status-indicator` settled is worse than none.
 *
 * It lives beside `popup` because both answer "what presentation does
 * the volume control get", both readers are the same two components,
 * and "none" is a presentation. A second store would be a second
 * subscription in the same `connectedCallback` saying the same thing.
 *
 * The initial value is `true` on the same first-frame rule: there is a
 * volume on every platform but one, and the platform that pins it sees
 * the control once at boot and never again in the session — the answer
 * cannot change while the app runs, so by the time the lazily-mounted
 * now-playing view exists it has long been settled by the bar's own
 * copy.
 */
class VolumeStyleStore {
    private value = false;

    private hasVolume = true;

    private loaded = false;

    private subscribers = new Set<Subscriber>();

    constructor() {
        EventsOn(Events.GeneralConfigChanged, () => {
            void this.refresh();
        });
    }

    /** Whether to draw the popup. Safe to read before `init()`. */
    get popup(): boolean {
        return this.value;
    }

    /**
     * Whether this app has a volume of its own to control. False where
     * the device owns it; see the class comment.
     */
    get available(): boolean {
        return this.hasVolume;
    }

    /** Reads the setting once. Safe to call from every mount. */
    async init(): Promise<void> {
        if (this.loaded) return;

        this.loaded = true;

        await Promise.all([this.refreshAvailability(), this.refresh()]);
    }

    subscribe(fn: Subscriber): () => void {
        this.subscribers.add(fn);

        return () => this.subscribers.delete(fn);
    }

    private async refresh(): Promise<void> {
        try {
            const popup = await GetPopupVolume();

            if (popup === this.value) return;

            this.value = popup;
            this.notify();
        } catch (err) {
            // Nothing to tell the user: the control renders in its
            // default presentation, which is a working volume control.
            console.error('failed to read the volume control setting', err);
        }
    }

    /**
     * Asked once, not on `GeneralConfigChanged`: this is a property of
     * the platform the binary was built for and cannot change while
     * the app is running.
     */
    private async refreshAvailability(): Promise<void> {
        try {
            const owned = await SystemOwnsVolume();

            if (owned === !this.hasVolume) return;

            this.hasVolume = !owned;
            this.notify();
        } catch (err) {
            // The control renders, which is the answer on every
            // platform but one and is the recoverable way to be wrong:
            // a working control nobody needs, rather than a missing one
            // somebody does.
            console.error('failed to ask who owns the volume', err);
        }
    }

    private notify(): void {
        for (const fn of this.subscribers) fn();
    }
}

export const volumeStyleStore = new VolumeStyleStore();

import { EventsOn } from '@runtime/runtime';
import { GetPopupVolume } from '@go/config/config.js';
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
 */
class VolumeStyleStore {
    private value = false;

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

    /** Reads the setting once. Safe to call from every mount. */
    async init(): Promise<void> {
        if (this.loaded) return;

        this.loaded = true;

        await this.refresh();
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

    private notify(): void {
        for (const fn of this.subscribers) fn();
    }
}

export const volumeStyleStore = new VolumeStyleStore();

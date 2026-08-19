import { EventsOn } from '@runtime/runtime';
import { GetViewVisibility, SetViewVisible } from '@go/config/config.js';
import { dictByName } from '@utils/binding';
import { downloadStore } from './download-store';
import { Events } from '../events';

type Subscriber = () => void;

/**
 * Which primary destinations the navigation offers.
 *
 * Eleven sidebar entries is more than most libraries need, so #25 makes
 * them individually toggleable. Three rules about this are load-bearing.
 *
 * **Hidden is not unreachable.** This decides what the *nav* draws and
 * nothing else: `navigate` still resolves a hidden view, which is not a
 * nicety — detail views navigate into these, and the shell's launch
 * page is one of them. Nothing here needs a special case for the
 * highlight either, because #72 moved that onto `active-view-store`:
 * `app-sidebar` asks `isActive(id)` per *rendered* item, so a hidden
 * view lights nothing exactly as a detail view does.
 *
 * **The defaults live in Go**, in `backend/config.Views`, and this asks
 * for the *resolved* answer rather than the stored map. A config that
 * says nothing about a view means "that view's own default", so a copy
 * of the defaults here would be a second thing to keep in step — and
 * the one that shipped in the artifact, not the one being edited.
 *
 * **Downloads is a second question**, answered by the download client
 * rather than by the config: a destination for a feature that cannot
 * work is worse than an absent one. It is gated at `visible()` and not
 * in the config, so switching it on in Settings still means what it
 * says once a client exists. `available` is false until the providers
 * have loaded, which makes the tab *appear* on a fresh launch rather
 * than appearing and then vanishing — the less jarring half of a race
 * that resolves in one query.
 */
class ViewVisibilityStore {
    /** The backend's resolved answer, empty until the first load. */
    private configured: Record<string, boolean> = {};

    private loaded = false;

    private subscribers = new Set<Subscriber>();

    constructor() {
        EventsOn(Events.GeneralConfigChanged, () => {
            void this.refresh();
        });

        // A client configured later has to add the destination without a
        // restart -- #37's rule, one surface over.
        downloadStore.subscribe(() => this.notify());
    }

    /** Loads the visibility map once. Safe to call from every mount. */
    async init(): Promise<void> {
        if (this.loaded) return;

        this.loaded = true;

        await Promise.all([
            this.refresh(),
            downloadStore.ensureProviders(),
        ]);
    }

    /**
     * Whether the navigation should offer this destination.
     *
     * An unknown id is visible: the caller is drawing it from its own
     * list, and a view this store has not heard of (or has not loaded
     * yet) is better shown than silently dropped.
     */
    visible(view: string): boolean {
        if (view === 'downloads' && !downloadStore.available) return false;

        return this.configured[view] ?? true;
    }

    /**
     * What the *config* says, ignoring the download-client gate — which
     * is what Settings' own checkbox has to show, or a user with no
     * client would see Downloads switched off and be unable to switch
     * it on.
     */
    enabled(view: string): boolean {
        return this.configured[view] ?? true;
    }

    async setVisible(view: string, visible: boolean): Promise<void> {
        await SetViewVisible(view, visible);

        // The backend emits GeneralConfigChanged, but the caller is
        // owed the new state by the time this resolves.
        await this.refresh();
    }

    subscribe(fn: Subscriber): () => void {
        this.subscribers.add(fn);

        return () => this.subscribers.delete(fn);
    }

    private async refresh(): Promise<void> {
        try {
            this.configured = await dictByName(GetViewVisibility());
            this.notify();
        } catch (err) {
            console.error('Failed to load view visibility:', err);
        }
    }

    private notify(): void {
        this.subscribers.forEach((fn) => fn());
    }
}

export const viewVisibilityStore = new ViewVisibilityStore();

import type {
    ReactiveController,
    ReactiveControllerHost,
} from 'lit';
import { viewVisibilityStore } from '../view-visibility-store';

/**
 * ViewVisibilityController connects a Lit component to the
 * ViewVisibilityStore.
 *
 * It reads through to the store rather than copying the map into a
 * `@state()` field, for the reason `ActiveViewController` does: there
 * are two live `<app-sidebar>` instances the moment `bottom-nav`'s
 * "More" drawer opens, and two components holding their own idea of
 * which destinations exist is how they come to disagree.
 */
export class ViewVisibilityController implements ReactiveController {
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    hostConnected(): void {
        this.unsubscribe = viewVisibilityStore.subscribe(() => {
            this.host.requestUpdate();
        });

        void viewVisibilityStore.init();
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    /** Whether the navigation should offer this destination. */
    visible(view: string): boolean {
        return viewVisibilityStore.visible(view);
    }

    /**
     * What the config says, ignoring the download-client gate — the
     * state Settings' own checkbox shows.
     */
    enabled(view: string): boolean {
        return viewVisibilityStore.enabled(view);
    }

    setVisible(view: string, visible: boolean): Promise<void> {
        return viewVisibilityStore.setVisible(view, visible);
    }
}

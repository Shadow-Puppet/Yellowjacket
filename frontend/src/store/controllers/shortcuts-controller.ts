import type { ReactiveController, ReactiveControllerHost } from 'lit';
import type { ShortcutsState } from '../shortcuts-store';
import { shortcutsStore } from '../shortcuts-store';

/**
 * ShortcutsController connects a Lit component to the ShortcutsStore.
 *
 * Use this controller in components that need to read or change
 * shortcut bindings (e.g. the shortcut settings UI in config-page).
 */
export class ShortcutsController implements ReactiveController {
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    // ===================================================================
    // LIFECYCLE HOOKS
    // ===================================================================

    hostConnected(): void {
        this.unsubscribe = shortcutsStore.subscribe(() => {
            this.host.requestUpdate();
        });
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    // ===================================================================
    // STATE ACCESSORS
    // ===================================================================

    get state(): Readonly<ShortcutsState> {
        return shortcutsStore.getState();
    }

    get bindings(): Map<string, string> {
        return shortcutsStore.getBindings();
    }

    // ===================================================================
    // ACTIONS
    // ===================================================================

    async updateBinding(
        action: string,
        key: string,
    ): Promise<void> {
        await shortcutsStore.updateBinding(action, key);
    }

    async resetAll(): Promise<void> {
        await shortcutsStore.resetAll();
    }
}

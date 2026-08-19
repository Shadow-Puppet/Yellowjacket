import type {
    ReactiveController,
    ReactiveControllerHost,
} from 'lit';
import { activeViewStore } from '../active-view-store';

/**
 * ActiveViewController connects a Lit component to the
 * ActiveViewStore.
 *
 * Usage in a component:
 *
 *   private activeCtrl = new ActiveViewController(this);
 *
 *   render() {
 *     const lit = this.activeCtrl.isActive('albums');
 *   }
 *
 * It reads through to the store rather than copying the value into a
 * `@state()` field, which is the point of #72: two components holding
 * their own idea of the active view is what let them disagree with the
 * shell and with each other.
 */
export class ActiveViewController implements ReactiveController {
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    // ===============================================================
    // LIFECYCLE HOOKS
    // ===============================================================

    hostConnected(): void {
        this.unsubscribe = activeViewStore.subscribe(() => {
            this.host.requestUpdate();
        });
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    // ===============================================================
    // DATA ACCESS
    // ===============================================================

    /** The active primary view, e.g. `albums`. */
    get current(): string {
        return activeViewStore.get();
    }

    isActive(view: string): boolean {
        return activeViewStore.isActive(view);
    }
}

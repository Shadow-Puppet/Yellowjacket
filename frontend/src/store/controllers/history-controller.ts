import type {
    ReactiveController,
    ReactiveControllerHost,
} from 'lit';
import { historyStore, type HistoryDepth } from '../history-store';

/**
 * HistoryController connects a Lit component to the HistoryStore.
 *
 * Usage in a component:
 *
 *   private historyCtrl = new HistoryController(this);
 *
 *   render() {
 *     const { canBack } = this.historyCtrl.depth;
 *   }
 */
export class HistoryController implements ReactiveController {
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    hostConnected(): void {
        this.unsubscribe = historyStore.subscribe(() => {
            this.host.requestUpdate();
        });
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    get depth(): HistoryDepth {
        return historyStore.get();
    }
}

import type {
    ReactiveController,
    ReactiveControllerHost,
} from 'lit';
import type { TrackListState } from '../tracklist-store';
import { trackListStore } from '../tracklist-store';

/**
 * TrackListController connects a Lit component to the
 * TrackListStore so it re-renders when the column layout changes.
 */
export class TrackListController
    implements ReactiveController
{
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
        this.unsubscribe = trackListStore.subscribe(
            () => {
                this.host.requestUpdate();
            },
        );
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    // ===============================================================
    // STATE ACCESSORS
    // ===============================================================

    get state(): Readonly<TrackListState> {
        return trackListStore.getState();
    }

    get columnIds(): readonly string[] {
        return this.state.columnIds;
    }

    // ===============================================================
    // ACTIONS
    // ===============================================================

    async setColumns(
        columnIds: string[],
    ): Promise<void> {
        await trackListStore.setColumns(columnIds);
    }
}

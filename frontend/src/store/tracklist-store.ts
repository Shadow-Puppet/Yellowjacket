import { EventsOn } from '@runtime/runtime';
import {
    GetTrackListColumns,
    SetTrackListColumns,
} from '@go/config/Config';
import { tracklist } from '@go/models';
import { Events } from '../events';
import { DEFAULT_COLUMN_IDS } from '@components/track-list/columns';

export interface TrackListState {
    /** Ordered list of visible column IDs. */
    columnIds: string[];
}

type Subscriber = () => void;

class TrackListStore {
    private state: TrackListState = {
        columnIds: [...DEFAULT_COLUMN_IDS],
    };

    private subscribers = new Set<Subscriber>();

    constructor() {
        this.initializeEventListeners();
        this.loadFromBackend();
    }

    // ===============================================================
    // WAILS EVENT BRIDGE
    // ===============================================================

    private initializeEventListeners(): void {
        EventsOn(
            Events.TrackListConfigChanged,
            (data: {
                columns: Array<{ id: string }>;
            }) => {
                this.update({
                    columnIds: data.columns.map(
                        (c) => c.id,
                    ),
                });
            },
        );
    }

    private async loadFromBackend(): Promise<void> {
        try {
            const columns = await GetTrackListColumns();

            this.update({
                columnIds: columns.map(
                    (c: tracklist.Column) => c.id,
                ),
            });
        } catch {
            // Use defaults on failure.
        }
    }

    // ===============================================================
    // STATE ACCESS
    // ===============================================================

    getState(): Readonly<TrackListState> {
        return this.state;
    }

    // ===============================================================
    // ACTIONS
    // ===============================================================

    async setColumns(columnIds: string[]): Promise<void> {
        const columns = columnIds.map((id) => {
            const col = new tracklist.Column();
            col.id = id;

            return col;
        });

        await SetTrackListColumns(columns);
    }

    // ===============================================================
    // SUBSCRIPTION SYSTEM
    // ===============================================================

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    private update(
        partial: Partial<TrackListState>,
    ): void {
        this.state = { ...this.state, ...partial };
        this.notify();
    }

    private notify(): void {
        this.subscribers.forEach((cb) => cb());
    }
}

// Singleton instance.
export const trackListStore = new TrackListStore();

import { EventsOn } from '@runtime/runtime';
import {
    GetTrackListColumns,
    SetTrackListColumns,
} from '@go/config/config.js';
import * as tracklist from '@go/tracklist/models.js';
import { Events } from '../events';
import { DEFAULT_COLUMN_IDS } from '@components/track-list/columns';
import { list } from '@utils/binding';

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
            const columns = await list(GetTrackListColumns());

            // An empty answer keeps the defaults rather than emptying
            // the list. `GetTrackListColumns` substitutes
            // `tracklist.DefaultColumns` only when the whole config
            // section is missing — a section that exists with no
            // columns in it returns nothing, and a track list with no
            // columns is not what that means. Until v3 this was
            // accidental: the binding's `[]Column` was typed `Column[]`
            // and an absent answer arrived as `undefined`, so `.map`
            // threw into the catch below.
            if (columns.length === 0) return;

            this.update({
                columnIds: columns.map((c) => c.id),
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
        const columns: tracklist.Column[] = columnIds.map((id) => ({
            id: id as tracklist.ColumnID,
        }));

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

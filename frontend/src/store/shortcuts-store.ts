import { EventsOn } from '@runtime/runtime';
import {
    GetShortcuts,
    SetShortcut,
    SetShortcuts,
    ResetShortcuts,
} from '@go/config/Config';
import { Events } from '../events';

export interface ShortcutsState {
    bindings: Map<string, string>; // action → key combo
    loaded: boolean;
}

type Subscriber = () => void;

/** Panel scope prefixes for scope-aware lookups. */
const PANEL_PREFIXES = ['tracklist.'] as const;

/**
 * ShortcutsStore holds the current keyboard shortcut bindings
 * and syncs them with the Go backend via Wails bindings.
 */
class ShortcutsStore {
    private state: ShortcutsState = {
        bindings: new Map(),
        loaded: false,
    };

    private subscribers = new Set<Subscriber>();
    private notifyQueued = false;

    constructor() {
        this.initializeEventListeners();
        this.loadFromBackend();
    }

    // ===================================================================
    // WAILS EVENT BRIDGE
    // ===================================================================

    private initializeEventListeners(): void {
        EventsOn(
            Events.ShortcutsConfigChanged,
            (data: Record<string, string>) => {
                this.state = {
                    bindings: new Map(Object.entries(data)),
                    loaded: true,
                };
                this.notify();
            },
        );
    }

    private async loadFromBackend(): Promise<void> {
        try {
            const raw = await GetShortcuts();
            this.state = {
                bindings: new Map(Object.entries(raw)),
                loaded: true,
            };
            this.notify();
        } catch {
            // Use empty bindings on failure; they'll be populated
            // when the backend pushes a ShortcutsConfigChanged event.
            this.state.loaded = true;
            this.notify();
        }
    }

    // ===================================================================
    // STATE ACCESS
    // ===================================================================

    getState(): Readonly<ShortcutsState> {
        return this.state;
    }

    /** Returns the full action→key bindings map. */
    getBindings(): Map<string, string> {
        return this.state.bindings;
    }

    /** Look up the key combo for a given action. */
    getKeyForAction(action: string): string | undefined {
        return this.state.bindings.get(action);
    }

    /**
     * Reverse lookup: find the action bound to a given key combo.
     * If a panel scope is provided, panel-specific bindings are checked
     * first, then global bindings.
     */
    getActionForKey(
        key: string,
        scope?: string,
    ): string | undefined {
        // If we have a panel scope, check panel-specific bindings first.
        if (scope && scope.startsWith('panel:')) {
            const panelPrefix = scope.replace('panel:', '') + '.';

            for (const [action, boundKey] of this.state.bindings) {
                if (
                    action.startsWith(panelPrefix) &&
                    boundKey === key
                ) {
                    return action;
                }
            }
        }

        // Fall back to global (non-panel) bindings.
        for (const [action, boundKey] of this.state.bindings) {
            if (boundKey !== key) continue;

            const isPanel = PANEL_PREFIXES.some((p) =>
                action.startsWith(p),
            );

            if (!isPanel) return action;
        }

        return undefined;
    }

    /**
     * Check for binding conflicts. Returns the conflicting binding
     * or null if no conflict.
     */
    findConflict(
        key: string,
        _scope: string,
        excludeAction: string,
    ): { action: string; key: string } | null {
        for (const [action, boundKey] of this.state.bindings) {
            if (action === excludeAction) continue;
            if (boundKey === key) return { action, key: boundKey };
        }

        return null;
    }

    // ===================================================================
    // ACTIONS
    // ===================================================================

    /** Update a single shortcut binding. */
    async updateBinding(
        action: string,
        key: string,
    ): Promise<void> {
        await SetShortcut(action, key);
    }

    /** Replace all bindings at once. */
    async setAll(
        bindings: Record<string, string>,
    ): Promise<void> {
        await SetShortcuts(bindings);
    }

    /** Reset all shortcuts to defaults. */
    async resetAll(): Promise<void> {
        await ResetShortcuts();
    }

    // ===================================================================
    // SUBSCRIPTION SYSTEM
    // ===================================================================

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    private notify(): void {
        if (this.notifyQueued) return;

        this.notifyQueued = true;
        queueMicrotask(() => {
            this.notifyQueued = false;
            this.subscribers.forEach((cb) => cb());
        });
    }
}

// Singleton instance.
export const shortcutsStore = new ShortcutsStore();

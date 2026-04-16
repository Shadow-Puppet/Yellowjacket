/**
 * ExploreSettingsStore — global settings for the explore feature.
 * Persists to localStorage so the toggle state survives restarts.
 */

type Listener = () => void;

class ExploreSettingsStore {
    private _libraryOnly: boolean;
    private listeners = new Set<Listener>();

    constructor() {
        this._libraryOnly = localStorage.getItem('explore:libraryOnly') === 'true';
    }

    get libraryOnly(): boolean {
        return this._libraryOnly;
    }

    setLibraryOnly(value: boolean) {
        if (this._libraryOnly === value) return;
        this._libraryOnly = value;
        localStorage.setItem('explore:libraryOnly', String(value));
        this.notify();
    }

    toggle() {
        this.setLibraryOnly(!this._libraryOnly);
    }

    subscribe(fn: Listener): () => void {
        this.listeners.add(fn);
        return () => this.listeners.delete(fn);
    }

    private notify() {
        for (const fn of this.listeners) fn();
    }
}

export const exploreSettings = new ExploreSettingsStore();

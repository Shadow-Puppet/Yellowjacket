/** Searchable views that respond to the global search term. */
const SEARCHABLE_VIEWS = new Set(['tracks', 'albums', 'playlists']);

type Subscriber = () => void;

class SearchStore {
    private term = '';
    private currentView = 'tracks';
    private subscribers = new Set<Subscriber>();

    // ===================================================================
    // SEARCH TERM
    // ===================================================================

    getTerm(): string {
        return this.term;
    }

    setTerm(term: string): void {
        if (term === this.term) return;

        this.term = term;
        this.notify();
    }

    // ===================================================================
    // CURRENT VIEW
    // ===================================================================

    getCurrentView(): string {
        return this.currentView;
    }

    setCurrentView(view: string): void {
        if (view === this.currentView) return;

        this.currentView = view;
        this.notify();
    }

    isSearchableView(): boolean {
        return SEARCHABLE_VIEWS.has(this.currentView);
    }

    // ===================================================================
    // SUBSCRIPTION SYSTEM
    // ===================================================================

    subscribe(callback: Subscriber): () => void {
        this.subscribers.add(callback);

        return () => this.subscribers.delete(callback);
    }

    private notify(): void {
        this.subscribers.forEach((callback) => callback());
    }
}

// Singleton instance.
export const searchStore = new SearchStore();

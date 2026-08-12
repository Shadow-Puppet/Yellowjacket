/** Searchable views that respond to the global search term. */
const SEARCHABLE_VIEWS = new Set(['tracks', 'albums', 'playlists', 'playlist-details', 'artists', 'genres']);

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

    /**
     * Deliberately *not* microtask-coalesced, unlike every other store
     * here (perf.p3 asks for it, and it is wrong about this one).
     *
     * Two reasons. Deferring makes an unsubscribe that happens
     * synchronously after a set drop the notification entirely, which
     * is a semantic change, not an optimisation — `view-stores.test.ts`
     * pins both halves. And this store is on the keystroke path, where
     * the batching the audit says would hide the cost is Lit's, not
     * ours: the `requestUpdate()`s are already coalesced one layer
     * down, so the microtask buys nothing and costs a frame of term
     * staleness in the one place a frame is visible.
     */
    private notify(): void {
        this.subscribers.forEach((callback) => callback());
    }
}

// Singleton instance.
export const searchStore = new SearchStore();

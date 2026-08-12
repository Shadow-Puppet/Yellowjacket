/**
 * What the header search box searches, per view.
 *
 * The box is **view-scoped by decision** (plan 007, Decisions 2), and
 * H-10 is that it never said so: it sits in the global header,
 * placeheld "Search…", and typing `tide` on Playlists answered "No
 * playlists match your search" with three *Tideline* tracks in the
 * library. The scope was always real; the copy is what was missing.
 *
 * A view absent from this map is one with nothing of its own to
 * search. The box keeps its slot there and is disabled, rather than
 * being removed — its appearing and disappearing is what made the
 * whole header change shape between pages.
 */
const SEARCH_SCOPES: Record<string, string> = {
    tracks: 'tracks',
    albums: 'albums',
    artists: 'artists',
    genres: 'genres',
    playlists: 'playlists',
    'playlist-details': 'tracks in this playlist',
    // Its sibling was in this map and it was not, so the box went
    // disabled and unlabelled on a view that filters on the term all
    // the same — typing narrowed the list under a placeholder saying
    // there was nothing to search. Checked before adding: it reads
    // `searchCtrl.term` in `getVisibleTracks` and says so in the page.
    'smart-playlist-details': 'tracks in this smart playlist',
};

/**
 * Views with a search of their own in the page. The header box points
 * at it rather than pretending to be it.
 */
const OWN_SEARCH_VIEWS: Record<string, string> = {
    explore: 'Search the catalog in the page',
};

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
        return this.currentView in SEARCH_SCOPES;
    }

    /** What this view searches, e.g. `albums`. Empty if it does not. */
    scopeLabel(): string {
        return SEARCH_SCOPES[this.currentView] ?? '';
    }

    /**
     * What the box should say when it cannot be used here: either that
     * the page has its own search, or that there is nothing to search.
     */
    disabledReason(): string {
        if (this.isSearchableView()) return '';

        return (
            OWN_SEARCH_VIEWS[this.currentView] ??
            'Nothing to search on this page'
        );
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

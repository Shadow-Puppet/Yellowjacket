import type { ReactiveController, ReactiveControllerHost } from 'lit';
import { searchStore } from '../search-store';

/**
 * SearchController connects a Lit component to the SearchStore.
 *
 * Usage in a component:
 *
 *   private search = new SearchController(this);
 *
 *   render() {
 *     const term = this.search.term;
 *     ...
 *   }
 */
export class SearchController implements ReactiveController {
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    // ===================================================================
    // LIFECYCLE HOOKS
    // ===================================================================

    hostConnected(): void {
        this.unsubscribe = searchStore.subscribe(() => {
            this.host.requestUpdate();
        });
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    // ===================================================================
    // DATA ACCESS
    // ===================================================================

    get term(): string {
        return searchStore.getTerm();
    }

    set term(value: string) {
        searchStore.setTerm(value);
    }

    get isSearchableView(): boolean {
        return searchStore.isSearchableView();
    }

    /** What this view searches ('albums'), or '' if it searches nothing. */
    get scopeLabel(): string {
        return searchStore.scopeLabel();
    }

    /** Why the box is disabled here, or '' if it is not. */
    get disabledReason(): string {
        return searchStore.disabledReason();
    }
}

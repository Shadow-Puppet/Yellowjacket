/**
 * Which primary view the app is showing.
 *
 * The shell has always known this -- `handleNavigate()` sets
 * `#main-content`'s `data-active-view` on every path, `_isBack`
 * included -- and never told anyone. The nav components learned it
 * from the `navigate` CustomEvent instead, which only the *outbound*
 * path dispatches: the `popstate` listener calls `handleNavigate()`
 * directly. So both navs kept highlighting the view you had just left
 * (#72).
 *
 * The fix cannot be a re-dispatch of `navigate`. `index.ts` is itself a
 * document listener for it, so emitting one from inside
 * `handleNavigate` is an infinite loop -- and the two statements are
 * different anyway: `navigate` means *please go to X*, and 28 call
 * sites across 18 files say it. This says *the active view is now X*,
 * which only the shell is in a position to say and only once per
 * navigation.
 *
 * Three things about it are load-bearing.
 *
 * **It is a store rather than an event**, because a component that
 * mounts *after* a navigation still has to know. `bottom-nav`'s "More"
 * drawer creates its `<app-sidebar>` on open, and that copy had heard
 * no `navigate` at all: standing on Albums, the drawer highlighted
 * Home -- its `activeView` default, which existed to match the landing
 * view and matched nothing else ever after. An event has no answer for
 * a listener that was not there; a value does.
 *
 * **A detail view is not a view here.** Opening one leaves the primary
 * view it was opened from lit, which is what #72 asks for and what
 * `app-sidebar` used to do by accident -- it guarded on
 * `navItems.some(...)`, so a name matching no item left its highlight
 * alone. `bottom-nav` had no such guard and so lit nothing on a detail
 * view. Neither was correct; the sidebar was stale-but-lucky, and
 * stating the rule once is what makes the two agree.
 *
 * **Whether a view is primary is the shell's fact, not this store's.**
 * `VIEW_TAGS` in `index.ts` is the list, and a copy of it here is a
 * second list to forget -- so the caller passes the answer it already
 * has rather than this file re-deriving it.
 */

type Subscriber = () => void;

class ActiveViewStore {
    /** Empty until the shell's first navigation, which happens at
     *  startup from `GetDefaultPage()`. Nothing is highlighted for that
     *  moment, which is honest: the alternative is a written-down
     *  default that is right only when the default page agrees with it. */
    private activeView = '';

    private subscribers = new Set<Subscriber>();

    /** The active primary view, e.g. `albums`. */
    get(): string {
        return this.activeView;
    }

    isActive(view: string): boolean {
        return this.activeView !== '' && this.activeView === view;
    }

    /**
     * Called by the shell on every navigation, `popstate` included.
     *
     * `isPrimary` is `view in VIEW_TAGS` at the call site: a detail
     * view reports itself and deliberately changes nothing, so the view
     * it was opened from stays lit until the user picks another one.
     */
    setView(view: string, isPrimary: boolean): void {
        if (!isPrimary) return;
        if (view === this.activeView) return;

        this.activeView = view;
        this.notify();
    }

    subscribe(fn: Subscriber): () => void {
        this.subscribers.add(fn);

        return () => this.subscribers.delete(fn);
    }

    private notify(): void {
        this.subscribers.forEach((fn) => fn());
    }
}

export const activeViewStore = new ActiveViewStore();

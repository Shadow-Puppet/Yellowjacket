/**
 * View lifecycle for the cached primary views.
 *
 * `index.ts` keeps every primary view alive in the DOM and toggles a
 * `.view-hidden` class, because that is what preserves `scrollTop`
 * across navigation.  The cost is that `disconnectedCallback` never
 * fires, so a view that is off-screen keeps its document listeners, its
 * intervals and its backend subscriptions — which is how a keypress on
 * Settings ended up skipping albums out of the Autotag queue
 * (`.planning/audits/2026-08-11-ui/hands-on.md`, H-1).
 *
 * The fix is not to unmount; it is to give the cache the half of the
 * lifecycle it never had.  A view is *activated* when it is the view on
 * screen and *deactivated* when it is not, and everything that listens
 * to the world hangs off that pair instead of off connection.
 *
 * Anything registered through `listenWhileActive`, `intervalWhileActive`
 * or `whileActive` is torn down on deactivation, so the common case
 * needs no `onViewDeactivate` at all.
 */
import type { LitElement } from 'lit';

import { claimShortcutScope } from '../services/shortcut-scope';

/** The half of the lifecycle `index.ts` drives. */
export interface ViewLifecycle {
    /** Called when this view becomes the one on screen. */
    viewActivated(): void;
    /** Called when it stops being. */
    viewDeactivated(): void;
    /** Whether it is on screen right now. */
    readonly viewActive: boolean;
}

/**
 * A reactive controller that wants the view lifecycle rather than the
 * connection lifecycle.
 *
 * A shared controller cannot know whether its host is a cached view, and
 * `hostDisconnected` never fires for one — so a controller that binds
 * document listeners (the context menu does) leaks them exactly the way
 * the views themselves did.  Registering here moves it onto the same
 * pair of calls; a host that is not a cached view never calls them, so
 * the controller keeps its connection-based behaviour there.
 */
export interface ViewAware {
    onHostActivate(): void;
    onHostDeactivate(): void;
}

interface ViewAwareRegistrar {
    addViewAware(aware: ViewAware): void;
}

/**
 * Register `aware` with `host` if the host takes part in the lifecycle.
 * Returns whether it did, which is also the answer to "should I wait to
 * be activated rather than attaching now?".
 */
export function registerViewAware(
    host: unknown,
    aware: ViewAware,
): boolean {
    const registrar = host as Partial<ViewAwareRegistrar>;

    if (typeof registrar.addViewAware !== 'function') return false;

    registrar.addViewAware(aware);

    return true;
}

/** Whether an element participates in the lifecycle. */
export function isViewLifecycle(
    el: Element | null,
): el is Element & ViewLifecycle {
    return (
        !!el &&
        typeof (el as Partial<ViewLifecycle>).viewActivated === 'function' &&
        typeof (el as Partial<ViewLifecycle>).viewDeactivated === 'function'
    );
}

/** Activate an element if it takes part in the lifecycle. */
export function activateView(el: Element | null): void {
    if (isViewLifecycle(el)) el.viewActivated();
}

/** Deactivate an element if it takes part in the lifecycle. */
export function deactivateView(el: Element | null): void {
    if (isViewLifecycle(el)) el.viewDeactivated();
}

type Constructor<T> = new (...args: any[]) => T;

/**
 * Mixin implementing {@link ViewLifecycle}.
 *
 * Subclasses override `onViewActivate`/`onViewDeactivate` rather than
 * `connectedCallback`/`disconnectedCallback` for anything outside their
 * own subtree.
 *
 * Activation is driven by `index.ts`, with one exception: a view that is
 * connected without being hidden was put on screen by whoever created it
 * (every ephemeral detail view, and every nested use of a view element),
 * so it activates itself.  `index.ts` creates cached views hidden, which
 * is what keeps that rule from firing for them.
 */
export function ViewLifecycleMixin<T extends Constructor<LitElement>>(
    Base: T,
) {
    abstract class ViewLifecycleElement extends Base implements ViewLifecycle {
        /** Panel scope this view claims while it is on screen, if any —
         *  see `services/shortcut-scope.ts`.  Subclasses set it. */
        protected shortcutScope: string | null = null;

        #active = false;
        #disposers: Array<() => void> = [];
        #missedUpdate = false;
        #aware = new Set<ViewAware>();

        get viewActive(): boolean {
            return this.#active;
        }

        override connectedCallback(): void {
            super.connectedCallback();

            if (!this.classList.contains('view-hidden')) {
                this.viewActivated();
            }
        }

        override disconnectedCallback(): void {
            this.viewDeactivated();
            super.disconnectedCallback();
        }

        viewActivated(): void {
            if (this.#active || !this.isConnected) return;

            this.#active = true;

            if (this.shortcutScope) {
                this.dataset['shortcutScope'] = this.shortcutScope;
                this.whileActive(claimShortcutScope(this.shortcutScope));
            }

            for (const aware of this.#aware) aware.onHostActivate();

            this.onViewActivate();

            if (this.#missedUpdate) {
                this.#missedUpdate = false;
                this.requestUpdate();
            }
        }

        viewDeactivated(): void {
            if (!this.#active) return;

            this.#active = false;

            const disposers = this.#disposers;

            this.#disposers = [];

            for (const dispose of disposers) dispose();

            if (this.shortcutScope) delete this.dataset['shortcutScope'];

            for (const aware of this.#aware) aware.onHostDeactivate();

            this.onViewDeactivate();
        }

        /** Called by {@link registerViewAware}. */
        addViewAware(aware: ViewAware): void {
            this.#aware.add(aware);

            if (this.#active) aware.onHostActivate();
        }

        /** Register a teardown to run on deactivation. */
        protected whileActive(dispose: () => void): void {
            if (this.#active) {
                this.#disposers.push(dispose);
            } else {
                // Registered by something that ran after deactivation
                // (an in-flight promise, say) — it has no owner, so run
                // it now rather than leak it until the next activation.
                dispose();
            }
        }

        /** `addEventListener`, removed again on deactivation. */
        protected listenWhileActive<E extends Event = Event>(
            target: EventTarget,
            type: string,
            handler: (event: E) => void,
            options?: boolean | AddEventListenerOptions,
        ): void {
            const listener = handler as EventListener;

            target.addEventListener(type, listener, options);
            this.whileActive(() =>
                target.removeEventListener(type, listener, options),
            );
        }

        /** `setInterval`, cleared again on deactivation. */
        protected intervalWhileActive(
            handler: () => void,
            ms: number,
        ): void {
            const id = setInterval(handler, ms);

            this.whileActive(() => clearInterval(id));
        }

        /**
         * An off-screen view does not render.
         *
         * Store subscriptions held by shared reactive controllers keep
         * calling `requestUpdate()` on every cached view — so a keystroke
         * in the search box re-rendered eleven pages, ten of which are
         * not on screen.  The update is remembered and replayed on
         * activation, so returning to a view still shows current state.
         */
        protected override shouldUpdate(
            changed: Map<PropertyKey, unknown>,
        ): boolean {
            if (!this.#active && this.hasUpdated) {
                this.#missedUpdate = true;

                return false;
            }

            return super.shouldUpdate(changed);
        }

        /** Called when the view goes on screen. */
        protected onViewActivate(): void {}

        /** Called when it leaves.  Registered teardowns have already
         *  run; override only for state that is not a disposer. */
        protected onViewDeactivate(): void {}
    }

    return ViewLifecycleElement;
}

/**
 * A fake of the two globals the Wails runtime installs: `window.runtime`
 * and `window.go`.
 *
 * Everything in `frontend/wailsjs/` is a pure passthrough — every binding
 * is `window['go'][svc][Type][Method](args)` and every runtime call is
 * `window.runtime.X(...)`. So faking the globals means tests exercise the
 * *real* generated bindings and the *real* store code, and there is no
 * second description of the Wails layer free to drift from the first.
 *
 * The event dispatcher mirrors wails v2's
 * `internal/frontend/runtime/desktop/events.js` exactly, including
 * `maxCallbacks` expiry and the fact that `EventsEmit` notifies local JS
 * listeners *before* it notifies Go.
 */

// ===================================================================
// EVENT DISPATCH  (mirrors desktop/events.js)
// ===================================================================

type Callback = (...data: unknown[]) => void;

class Listener {
  private remaining: number;

  constructor(
    readonly eventName: string,
    private readonly callback: Callback,
    maxCallbacks: number,
  ) {
    this.remaining = maxCallbacks || -1;
  }

  /** Invokes the callback; returns true if this listener is spent. */
  fire(data: unknown[]): boolean {
    this.callback(...data);

    if (this.remaining === -1) return false;

    this.remaining -= 1;

    return this.remaining === 0;
  }
}

/** Records one bound-method invocation. */
export interface BindingCall {
  /** Dotted path, e.g. `queue.Queue.SetQueue`. */
  path: string;
  args: unknown[];
}

type StubValue = unknown | ((...args: unknown[]) => unknown);

class WailsFake {
  private listeners = new Map<string, Listener[]>();
  private stubs = new Map<string, StubValue>();

  /** Every bound-method call made since the last `reset()`. */
  readonly calls: BindingCall[] = [];

  /** Every runtime (non-binding) call, e.g. `WindowSetTitle`. */
  readonly runtimeCalls: BindingCall[] = [];

  // -- listener registry --

  on(eventName: string, callback: Callback, maxCallbacks: number): () => void {
    const listener = new Listener(eventName, callback, maxCallbacks);
    const existing = this.listeners.get(eventName);

    if (existing) {
      existing.push(listener);
    } else {
      this.listeners.set(eventName, [listener]);
    }

    return () => this.off(eventName, listener);
  }

  private off(eventName: string, listener: Listener): void {
    const list = this.listeners.get(eventName);

    if (!list) return;

    const idx = list.indexOf(listener);

    if (idx >= 0) list.splice(idx, 1);
    if (list.length === 0) this.listeners.delete(eventName);
  }

  offNamed(eventName: string, ...more: string[]): void {
    for (const name of [eventName, ...more]) {
      this.listeners.delete(name);
    }
  }

  offAll(): void {
    this.listeners.clear();
  }

  /**
   * Deliver an event exactly as the backend push does. Iterates in
   * reverse and drops spent listeners, like `notifyListeners`.
   */
  notify(eventName: string, data: unknown[]): void {
    const list = this.listeners.get(eventName);

    if (!list || list.length === 0) return;

    const snapshot = list.slice();

    for (let i = snapshot.length - 1; i >= 0; i -= 1) {
      const listener = snapshot[i];

      if (!listener) continue;

      if (listener.fire(data)) snapshot.splice(i, 1);
    }

    if (snapshot.length === 0) {
      this.listeners.delete(eventName);
    } else {
      this.listeners.set(eventName, snapshot);
    }
  }

  /** Names with at least one live listener — useful for assertions. */
  listenerNames(): string[] {
    return [...this.listeners.keys()].sort();
  }

  // -- binding stubs --

  stub(path: string, value: StubValue): void {
    this.stubs.set(path, value);
  }

  invoke(path: string, args: unknown[]): Promise<unknown> {
    this.calls.push({ path, args });

    const stub = this.stubs.get(path);

    if (typeof stub === 'function') {
      // A throwing stub becomes a rejected promise, matching the real
      // bridge: a Go method returning an error rejects, it does not
      // throw synchronously into the caller.
      try {
        return Promise.resolve(
          (stub as (...a: unknown[]) => unknown)(...args),
        );
      } catch (err) {
        return Promise.reject(err instanceof Error ? err : new Error(String(err)));
      }
    }

    return Promise.resolve(stub);
  }

  recordRuntime(path: string, args: unknown[]): void {
    this.runtimeCalls.push({ path, args });
  }

  /** Clears recorded calls and stubs. Listeners survive — the store
   *  singletons that registered them are never re-imported. */
  reset(): void {
    this.calls.length = 0;
    this.runtimeCalls.length = 0;
    this.stubs.clear();
  }
}

// ===================================================================
// GLOBAL INSTALLATION
// ===================================================================

export const wails = new WailsFake();

/** A `window.go` that materialises `svc.Type.Method` lazily. */
function makeGoProxy(): unknown {
  const level = (prefix: string): unknown =>
    new Proxy(function () {} as unknown as Record<string, unknown>, {
      get(_target, prop: string | symbol) {
        if (typeof prop !== 'string') return undefined;

        return level(prefix ? `${prefix}.${prop}` : prop);
      },
      apply(_target, _thisArg, args: unknown[]) {
        return wails.invoke(prefix, args);
      },
    });

  return level('');
}

/** A `window.runtime` with real event plumbing and recorded no-ops
 *  for everything else (window, clipboard, browser, log). */
function makeRuntimeProxy(): unknown {
  const real: Record<string, unknown> = {
    EventsOnMultiple: (name: string, cb: Callback, max: number) =>
      wails.on(name, cb, max),
    EventsOn: (name: string, cb: Callback) => wails.on(name, cb, -1),
    EventsOnce: (name: string, cb: Callback) => wails.on(name, cb, 1),
    EventsOff: (name: string, ...more: string[]) =>
      wails.offNamed(name, ...more),
    EventsOffAll: () => wails.offAll(),
    // The real runtime notifies local JS listeners first, then Go.
    EventsEmit: (name: string, ...data: unknown[]) => {
      wails.recordRuntime(`EventsEmit:${name}`, data);
      wails.notify(name, data);
    },
  };

  return new Proxy(real, {
    get(target, prop: string | symbol) {
      if (typeof prop !== 'string') return undefined;
      if (prop in target) return target[prop];

      return (...args: unknown[]) => {
        wails.recordRuntime(prop, args);

        return undefined;
      };
    },
  });
}

declare global {
  interface Window {
    go: unknown;
    runtime: unknown;
  }
}

/**
 * Installs the fake. Must run before any module that imports a store,
 * because the store singletons call `EventsOn` in their constructors at
 * import time. `setupFiles` runs before test modules, which is exactly
 * the window we need.
 */
export function installWailsFake(): void {
  window.go = makeGoProxy();
  window.runtime = makeRuntimeProxy();
}

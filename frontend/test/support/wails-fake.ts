/**
 * A fake of the one thing v3 routes every runtime call through: the
 * IPC transport.
 *
 * v2 installed two globals, `window.go` and `window.runtime`, and the
 * fake replaced both. v3 has neither — the runtime is an npm module and
 * the generated bindings call `$Call.ByID(<id>, …)` into it. What it
 * does have is better: `setTransport()` is a public, documented seam
 * for replacing the transport wholesale, and *every* runtime call goes
 * through it — bindings, event emits, window, dialogs, clipboard. So
 * this fake is smaller than v2's and covers strictly more, and tests
 * still exercise the real generated bindings and the real store code.
 *
 * The event *dispatcher* is no longer mirrored here at all. v2's fake
 * reimplemented `desktop/events.js` — the listener list, `maxCallbacks`
 * expiry, the reverse iteration — because there was no way to reach the
 * real one. v3 exposes `window._wails.dispatchWailsEvent`, which is the
 * exact entry point the backend's own push uses, so delivery, expiry
 * and the post-dispatch filter are the runtime's real code. What is
 * mirrored instead is one line of Go: how `EventManager.Emit` packs its
 * variadic data into an event's single `data` field.
 *
 * One thing v2 did that v3 does not: a frontend `Events.Emit` no longer
 * notifies in-page listeners before it notifies Go. It calls the
 * backend, and `EventProcessor.Emit` sends the event back out to every
 * window — so the emitting page does see it, one round trip later
 * rather than synchronously. That is reproduced below.
 */

import { Events, objectNames, setTransport } from '@wailsio/runtime';
// Not public API: `listener.js` has no entry in the package's exports
// map, and this import only resolves because vitest.config.mts aliases
// it. It is the *only* thing here that is not public, and it buys one
// thing — `listenerNames()`, which is what lets a test assert that
// importing a store subscribes it. Registration, dispatch and
// unregistration all go through the public API above. If Wails moves
// the file the import throws at setup, which is loud rather than
// silent.
import { eventListeners } from '@wailsio/listener';

// ===================================================================
// METHOD IDS
// ===================================================================

/**
 * FNV-1a, 32-bit — `internal/hash.Fnv`, which is what the binding
 * generator hashes a method's fully-qualified name with to produce the
 * `$Call.ByID` argument.
 */
function fnv1a(value: string): number {
  let hash = 0x811c9dc5;

  for (const byte of new TextEncoder().encode(value)) {
    hash ^= byte;
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }

  return hash >>> 0;
}

/**
 * Maps a method ID back to the dotted path tests name a binding by
 * (`queue.Queue.GetState`).
 *
 * The map has to be complete rather than built as paths are mentioned,
 * because 21 assertions read `calls()` with no argument and compare the
 * whole list of paths — including methods no test ever stubbed.
 *
 * It is derived from the generated tree rather than written down: each
 * package's `index.ts` re-exports its service module under the Go type's
 * real name (`export { Library }`), which is the one place that casing
 * survives — the file is `library/library.ts`, and `frontendutil.ts`
 * cannot tell you it is `FrontendUtil`. The directory path under
 * `bindings/` *is* the Go import path, so the FQN needs nothing
 * hardcoded.
 */
function buildMethodIDs(): Map<number, string> {
  const modules = import.meta.glob(
    '../../bindings/yellowjacket/**/index.ts',
    { eager: true },
  ) as Record<string, Record<string, unknown>>;
  const byID = new Map<number, string>();

  for (const [file, mod] of Object.entries(modules)) {
    // '../../bindings/yellowjacket/backend/queue/index.ts'
    //   -> importPath 'yellowjacket/backend/queue', pkg 'queue'
    const importPath = file
      .replace(/^.*\/bindings\//, '')
      .replace(/\/index\.ts$/, '');
    const pkg = importPath.split('/').pop() ?? importPath;

    for (const [typeName, namespace] of Object.entries(mod)) {
      if (typeName === 'default' || typeName !== typeName.replace(/\W/g, '')) {
        continue;
      }

      if (typeof namespace !== 'object' || namespace === null) continue;

      for (const [method, value] of Object.entries(namespace)) {
        // An enum is also an object of exports; only a function is a
        // bound method.
        if (typeof value !== 'function') continue;

        byID.set(
          fnv1a(`${importPath}.${typeName}.${method}`),
          `${pkg}.${typeName}.${method}`,
        );
      }
    }
  }

  return byID;
}

// ===================================================================
// THE FAKE
// ===================================================================

/** Records one bound-method invocation. */
export interface BindingCall {
  /** Dotted path, e.g. `queue.Queue.SetQueue`. */
  path: string;
  args: unknown[];
}

type StubValue = unknown | ((...args: unknown[]) => unknown);

type Callback = (...data: unknown[]) => void;

class WailsFake {
  private readonly stubs = new Map<string, StubValue>();
  private methodIDs = new Map<number, string>();

  /** Every bound-method call made since the last `reset()`. */
  readonly calls: BindingCall[] = [];

  /** Every runtime (non-binding) call, e.g. `Window.SetTitle`. */
  readonly runtimeCalls: BindingCall[] = [];

  install(): void {
    this.methodIDs = buildMethodIDs();
    setTransport({ call: (object, method, _window, args) =>
      this.route(object, method, args) });
  }

  // -- listeners --

  /**
   * Registers a listener through the real runtime, so a test's own
   * listener expires and unregisters exactly as a store's does. The
   * only translation is the callback shape: the runtime hands over a
   * WailsEvent, and this tier's callers want the payload.
   */
  on(eventName: string, callback: Callback, maxCallbacks: number): () => void {
    return Events.OnMultiple(
      eventName,
      (event) => { callback(event.data); },
      maxCallbacks,
    );
  }

  offNamed(eventName: string, ...more: string[]): void {
    Events.Off(eventName, ...more);
  }

  offAll(): void {
    Events.OffAll();
  }

  /** Names with at least one live listener — useful for assertions. */
  listenerNames(): string[] {
    return [...eventListeners.keys()].sort();
  }

  /**
   * Deliver an event exactly as the backend push does, through the
   * runtime's own dispatcher.
   *
   * The packing mirrors `application.EventManager.Emit`: no data at all
   * is null, one value is that value, and more than one is the slice.
   * Getting this wrong is invisible in the fake and shows up as a store
   * reading `undefined` off its payload.
   */
  notify(eventName: string, data: unknown[]): void {
    const wails = (window as unknown as {
      _wails?: { dispatchWailsEvent?: (e: unknown) => void };
    })._wails;

    let payload: unknown = null;

    if (data.length === 1) payload = data[0];
    else if (data.length > 1) payload = data;

    wails?.dispatchWailsEvent?.({ name: eventName, data: payload });
  }

  // -- binding stubs --

  stub(path: string, value: StubValue): void {
    this.stubs.set(path, value);
  }

  /**
   * Routes one transport call.
   *
   * `objectNames.Call` is a bound method; `objectNames.Events` is a
   * frontend emit; everything else — window, dialogs, clipboard,
   * screens — is recorded and answers undefined, which is what v2's
   * `window.runtime` proxy did for the same surface.
   */
  private route(object: number, method: number, args: unknown): Promise<unknown> {
    if (object === objectNames.Call) {
      const { methodID, args: callArgs } =
        (args ?? {}) as { methodID?: number; args?: unknown[] };

      return this.invokeID(methodID, callArgs ?? []);
    }

    if (object === objectNames.Events) {
      const event = (args ?? {}) as { name?: string; data?: unknown };

      this.runtimeCalls.push({
        path: `EventsEmit:${event.name}`,
        args: event.data === undefined ? [] : [event.data],
      });

      // The backend re-broadcasts a custom event to every window,
      // including the one that emitted it — so the page does see its
      // own emit, a round trip later rather than synchronously. The
      // microtask is that round trip.
      queueMicrotask(() => {
        this.notify(event.name ?? '', event.data === undefined ? [] : [event.data]);
      });

      return Promise.resolve(undefined);
    }

    const name = OBJECT_LABELS.get(object) ?? `object${object}`;

    this.runtimeCalls.push({ path: `${name}.${method}`, args: [args] });

    return Promise.resolve(undefined);
  }

  private invokeID(methodID: number | undefined, args: unknown[]): Promise<unknown> {
    // An unmapped ID means the derivation above missed a method, not
    // that the call did not happen. Recording it as `#<id>` fails the
    // assertion that names it, which is the right kind of loud.
    const path = (methodID !== undefined && this.methodIDs.get(methodID))
      || `#${methodID}`;

    this.calls.push({ path, args });

    const stub = this.stubs.get(path);

    if (typeof stub === 'function') {
      // A throwing stub becomes a rejected promise, matching the real
      // bridge: a Go method returning an error rejects, it does not
      // throw synchronously into the caller.
      try {
        return Promise.resolve((stub as (...a: unknown[]) => unknown)(...args));
      } catch (err) {
        return Promise.reject(
          err instanceof Error ? err : new Error(String(err)),
        );
      }
    }

    return Promise.resolve(stub);
  }

  /** Clears recorded calls and stubs. Listeners survive — the store
   *  singletons that registered them are never re-imported. */
  reset(): void {
    this.calls.length = 0;
    this.runtimeCalls.length = 0;
    this.stubs.clear();
  }
}

/** Reverse of `objectNames`, for labelling a recorded runtime call. */
const OBJECT_LABELS = new Map<number, string>(
  Object.entries(objectNames).map(([name, id]) => [id as number, name]),
);

// ===================================================================
// INSTALLATION
// ===================================================================

export const wails = new WailsFake();

/**
 * Installs the fake. Must run before any module that imports a store,
 * because the store singletons call `EventsOn` and load from the
 * backend in their constructors at import time. `setupFiles` runs
 * before test modules, which is exactly the window we need.
 */
export function installWailsFake(): void {
  wails.install();
}

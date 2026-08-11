/**
 * Helpers on top of the Wails fake: pushing backend events, stubbing
 * bound methods, and inspecting what the frontend called back.
 */
import { wails, type BindingCall } from './wails-fake';

export { wails } from './wails-fake';

/**
 * Push a backend event into the page, exactly as `runtime.EventsEmit`
 * on the Go side would. Extra arguments become the event's data array.
 */
export function emit(name: string, ...data: unknown[]): void {
  wails.notify(name, data);
}

/**
 * Register the return value of a bound method. The path is the one the
 * generated bindings use — `service.Type.Method`, e.g.
 * `config.Config.GetShortcuts`.
 *
 * A function value is called with the invocation's arguments, so a stub
 * can vary by input.
 */
export function stub(path: string, value: unknown): void {
  wails.stub(path, value);
}

/**
 * Make a bound method fail, as a Go method returning an error does:
 * the promise rejects, it does not throw into the caller.
 */
export function stubFailure(path: string, message = 'backend error'): void {
  wails.stub(path, () => {
    throw new Error(message);
  });
}

/** Every call made to a bound method, in order. */
export function calls(path?: string): BindingCall[] {
  if (path === undefined) return wails.calls.slice();

  return wails.calls.filter((c) => c.path === path);
}

/** The most recent call to `path`, or undefined. */
export function lastCall(path: string): BindingCall | undefined {
  return calls(path).at(-1);
}

/** The argument list of the most recent call to `path`. */
export function lastArgs(path: string): unknown[] | undefined {
  return lastCall(path)?.args;
}

/**
 * Flush pending microtasks. Stores coalesce subscriber notification
 * through `queueMicrotask`, so state is observable immediately but
 * subscribers are not — anything asserting on a subscriber must await
 * this first.
 */
export async function flush(): Promise<void> {
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 0);
  });
}

/** Clears recorded calls and stubs between tests. */
export function resetHarness(): void {
  wails.reset();
}

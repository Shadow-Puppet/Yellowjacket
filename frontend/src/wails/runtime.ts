/**
 * The `@runtime/runtime` seam.
 *
 * 22 files import `EventsOn` from here and nothing else, so rather than
 * rewriting 22 imports to v3's `Events.On` the alias points at this
 * module — which is also what gives the Vitest fake exactly one seam to
 * intercept instead of a `window.runtime` global that v3 does not have.
 * (Plan 009, D4.)
 *
 * The one real difference between the two runtimes is the callback
 * shape: v2 spread an event's data across the callback's arguments,
 * v3 hands over a single `WailsEvent` object.  Unwrapping `.data` here
 * reproduces v2's shape for the single-value case, which is every emit
 * in this tree — `backend/events` has no call site passing more than
 * one data argument, and v3's `EventManager.Emit` only packs arguments
 * into a slice when there is more than one, so there is nothing to
 * un-spread.
 */

import { Events } from '@wailsio/runtime';

/**
 * EventsOn registers a listener for a backend event and returns the
 * function that unregisters it.
 */
export function EventsOn(
    eventName: string,
    callback: (...data: any[]) => void,
): () => void {
    return Events.On(eventName, (ev) => {
        callback(ev.data);
    });
}

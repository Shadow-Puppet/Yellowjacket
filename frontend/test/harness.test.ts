/**
 * Self-tests for the component tier, in the spirit of e2e/specs/
 * harness.spec.ts: prove the rig is what it claims before trusting a
 * single assertion built on it.
 */
import { describe, expect, it } from 'vitest';

import { Events } from '../src/events';
import { emit, calls, wails, flush } from '@test/support/harness';
// Importing a store must be enough to make it start listening.
import { queueStore } from '@store/queue-store';

describe('component-tier harness', () => {
  it('runs in a real browser with a real shadow DOM', () => {
    const host = document.createElement('div');

    host.attachShadow({ mode: 'open' }).innerHTML = '<b>x</b>';

    expect(host.shadowRoot?.querySelector('b')?.textContent).toBe('x');
  });

  it('routes generated bindings through the fake, not a module mock', async () => {
    // The import path under test is the real generated stub, which does
    // window['go']['queue']['Queue']['GetState']().
    const Queue = await import('@go/queue/queue.js');

    wails.stub('queue.Queue.GetState', { currentIndex: 4 });

    await expect(Queue.GetState()).resolves.toEqual({ currentIndex: 4 });
    expect(calls('queue.Queue.GetState')).toHaveLength(1);
  });

  it('resolves an unstubbed binding instead of hanging', async () => {
    const Queue = await import('@go/queue/queue.js');

    // The real trap this pays for is the reverse: a *real* binding
    // called with wrong argument types never settles. Here, silence is
    // an immediate undefined so a test fails on the assertion rather
    // than on a timeout.
    await expect(Queue.Play()).resolves.toBeUndefined();
  });

  it('registers listeners merely by importing a store', () => {
    expect(wails.listenerNames()).toContain(Events.QueueChanged);
  });

  it('delivers events to store listeners with their payload', () => {
    emit(Events.QueueIndexChanged, { currentIndex: 11 });

    expect(queueStore.getState().currentIndex).toBe(11);
  });

  it('expires a once-listener after a single delivery', () => {
    let fired = 0;

    wails.on('SyntheticEvent', () => {
      fired += 1;
    }, 1);

    wails.notify('SyntheticEvent', []);
    wails.notify('SyntheticEvent', []);

    expect(fired).toBe(1);
  });

  it('notifies local listeners on a frontend-side EventsEmit', async () => {
    // Wails' own runtime notifies JS listeners before it notifies Go
    // (desktop/events.js), so a frontend emit is observable in-page.
    const { EventsEmit } = await import('@runtime/runtime');
    let seen: unknown;

    wails.on('SyntheticEmit', (data) => {
      seen = data;
    }, -1);

    EventsEmit('SyntheticEmit', 42);

    expect(seen).toBe(42);
  });

  it('flushes the microtask queue stores notify on', async () => {
    let notified = false;

    const off = queueStore.subscribe(() => {
      notified = true;
    });

    emit(Events.QueueIndexChanged, { currentIndex: 1 });
    const beforeFlush = notified;

    await flush();
    off();

    expect([beforeFlush, notified]).toEqual([false, true]);
  });
});

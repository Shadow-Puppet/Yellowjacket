/**
 * The app's one notification surface.
 *
 * Two behaviours here are the reason it exists as a store rather than
 * as a component: the caller picks a *level* and nothing else, and
 * coalescing happens once, here, so a queue of 200 unplayable files
 * produces one message rather than 200 and no future caller has to
 * remember that.
 */
import { describe, expect, it, beforeEach, vi, afterEach } from 'vitest';

import { notificationStore } from '@store/notification-store';

describe('notification store', () => {
  beforeEach(() => {
    notificationStore.clear();
  });

  it('keeps a notification per level', () => {
    notificationStore.blocking({ text: 'Half the folder was retagged.' });
    notificationStore.persistent({ text: 'The scan did not start.' });
    notificationStore.transient({ text: 'That favourite was undone.' });
    notificationStore.inline('player', { text: 'Could not seek.' });

    expect(notificationStore.getAll().map((n) => n.level)).toEqual([
      'blocking',
      'persistent',
      'transient',
      'inline',
    ]);
  });

  it('routes an inline message to its region and nowhere else', () => {
    notificationStore.inline('player', { text: 'Could not seek.' });

    expect([
      notificationStore.forRegion('player').length,
      notificationStore.forRegion('explore').length,
      notificationStore.byLevel('transient').length,
    ]).toEqual([1, 0, 0]);
  });

  it('folds a repeat into one message with a count', () => {
    for (const title of ['One', 'Two', 'Three']) {
      notificationStore.inline('player', {
        key: 'playback-failed',
        text: `Could not play “${title}”.`,
        coalescedText: (count) => `Skipped ${count} tracks.`,
      });
    }

    const [only] = notificationStore.forRegion('player');

    expect([notificationStore.getAll().length, only?.count, only?.text]).toEqual(
      [1, 3, 'Skipped 3 tracks.'],
    );
  });

  it('does not fold two different failures together', () => {
    notificationStore.transient({ key: 'a', text: 'One thing failed.' });
    notificationStore.transient({ key: 'b', text: 'Another thing failed.' });

    expect(notificationStore.getAll()).toHaveLength(2);
  });

  it('does not fold the same key across levels', () => {
    notificationStore.transient({ key: 'scan', text: 'Scan failed.' });
    notificationStore.persistent({ key: 'scan', text: 'Scan failed.' });

    expect(notificationStore.getAll()).toHaveLength(2);
  });

  it('runs an action and takes the message away with it', () => {
    const retry = vi.fn();
    const id = notificationStore.persistent({
      text: 'The scan did not start.',
      action: { label: 'Try again', run: retry },
    });

    notificationStore.runAction(id);

    expect([retry.mock.calls.length, notificationStore.getAll().length]).toEqual(
      [1, 0],
    );
  });

  it('notifies subscribers once per batch', async () => {
    let notifications = 0;
    const off = notificationStore.subscribe(() => {
      notifications += 1;
    });

    notificationStore.transient({ text: 'One.' });
    notificationStore.transient({ text: 'Two.' });
    await new Promise((resolve) => setTimeout(resolve, 0));
    off();

    expect(notifications).toBe(1);
  });

  it('keeps the stack readable, and never drops a modal to do it', () => {
    notificationStore.blocking({ text: 'Half the folder was retagged.' });

    for (let i = 0; i < 8; i += 1) {
      notificationStore.persistent({ key: `k${i}`, text: `Failure ${i}.` });
    }

    const levels = notificationStore.getAll().map((n) => n.level);

    expect([levels.length, levels.includes('blocking')]).toEqual([5, true]);
  });
});

describe('notification store: self-dismissal', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    notificationStore.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('takes a toast away by itself', () => {
    notificationStore.transient({ text: 'That favourite was undone.' });
    vi.advanceTimersByTime(6000);

    expect(notificationStore.getAll()).toHaveLength(0);
  });

  it('leaves the levels that are waiting for an answer', () => {
    notificationStore.persistent({ text: 'The scan did not start.' });
    notificationStore.blocking({ text: 'Half the folder was retagged.' });
    vi.advanceTimersByTime(60_000);

    expect(notificationStore.getAll()).toHaveLength(2);
  });

  it('restarts the clock when a message repeats', () => {
    notificationStore.transient({ key: 'fav', text: 'Undone.' });
    vi.advanceTimersByTime(4000);
    notificationStore.transient({ key: 'fav', text: 'Undone.' });
    vi.advanceTimersByTime(4000);

    // Still there: the second occurrence bought it another window.
    expect(notificationStore.getAll()).toHaveLength(1);
  });

  it('starts a new message once the coalescing window has passed', () => {
    notificationStore.inline('player', { key: 'seek', text: 'Could not seek.' });
    vi.advanceTimersByTime(20_000);
    notificationStore.inline('player', { key: 'seek', text: 'Could not seek.' });

    const [only] = notificationStore.forRegion('player');

    expect(only?.count).toBe(1);
  });
});

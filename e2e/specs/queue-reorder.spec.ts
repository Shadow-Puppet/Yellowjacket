import {
  test,
  expect,
  callBinding,
  NO_QUEUE_SOURCE,
} from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * `a11y.11` — the queue's order can be changed without a mouse.
 *
 * The component tier pins the arithmetic against a faked binding. This
 * one is here because the arithmetic is only half of it: `toIndex` is
 * interpreted by `Queue.MoveQueueTracks`, whose contiguous-block guard
 * turns the plausible-looking `i + 1` into a silent no-op. Nothing but
 * the real backend can say whether the order actually moved.
 *
 * Reproduced first: with a row focused, Alt/Ctrl/Shift/Meta + arrows all
 * left the order untouched.
 */

/** The queue's order, asked of the backend rather than of the DOM. */
async function order(app: Page): Promise<string[]> {
  const state = await callBinding<{ tracks: { title: string }[] }>(
    app,
    'queue.Queue.GetState',
  );

  return state.tracks.map((t) => t.title);
}

async function queueFourAndOpen(app: Page): Promise<string[]> {
  const paths: string[] = await app.evaluate(async () => {
    const tracks = await window.__yjEvents.call(
      'library.Library.GetAllTracks',
      [],
      10_000,
    );

    return (tracks as { FilePath: string }[]).slice(0, 4).map((t) => t.FilePath);
  });

  await callBinding(app, 'queue.Queue.SetQueue', [paths, 0, false, NO_QUEUE_SOURCE]);

  // A closed panel renders no list at all, so there is no row to focus.
  await app.locator('#queue-button').click();
  await expect(app.locator('queue-panel .track-item').first()).toBeVisible();

  return order(app);
}

test.describe('reordering the queue from the keyboard', () => {
  // The 36 specs share one backend process in file order, and these
  // leave two things behind that outlive the page: a reordered queue
  // and an open panel. Both are put back, because a spec that spends
  // state fails the *next* one, in a list that reads like a regression
  // in whatever you are holding.
  test.afterEach(async ({ app }) => {
    await callBinding(app, 'queue.Queue.Clear').catch(() => {
      /* nothing queued is the state we wanted anyway */
    });

    const open = await app.locator('queue-panel[open]').count();

    if (open > 0) await app.locator('#queue-button').click();
  });

  test('Alt+Arrow moves the focused row, and puts it back', async ({ app }) => {
    const start = await queueFourAndOpen(app);

    expect(start.length).toBe(4);

    await app.locator('queue-panel .track-item').nth(1).focus();
    await app.keyboard.press('Alt+ArrowUp');
    await expect.poll(() => order(app)).toEqual([start[1], start[0], ...start.slice(2)]);

    // Down is the direction the obvious index arithmetic gets wrong: it
    // has to ask for i + 2, because i + 1 is a no-op once the row's own
    // removal is accounted for. A spec that only moved up would pass
    // against a build where down does nothing.
    await app.keyboard.press('Alt+ArrowDown');
    await expect.poll(() => order(app)).toEqual(start);
  });

  test('says where the row went', async ({ app }) => {
    await queueFourAndOpen(app);

    await app.locator('queue-panel .track-item').nth(1).focus();
    await app.keyboard.press('Alt+ArrowUp');

    await expect(
      app.locator('queue-panel [role="status"]'),
    ).toHaveText(/Moved to position 1 of 4/);
  });

  test('refuses at the ends without reordering anything', async ({ app }) => {
    const start = await queueFourAndOpen(app);

    await app.locator('queue-panel .track-item').first().focus();
    await app.keyboard.press('Alt+ArrowUp');

    await expect(
      app.locator('queue-panel [role="status"]'),
    ).toHaveText(/Already first/);
    expect(await order(app)).toEqual(start);
  });

  // The plain arrows belong to the roving tab stop, and must not reach
  // the global volume binding from a focused row.
  test('leaves the unmodified arrows roving', async ({ app }) => {
    const start = await queueFourAndOpen(app);

    await app.locator('queue-panel .track-item').first().focus();
    await app.keyboard.press('ArrowDown');

    const focused = await app.evaluate(
      () =>
        document
          .querySelector('queue-panel')
          ?.shadowRoot?.activeElement?.getAttribute('data-index') ?? null,
    );

    expect([focused, await order(app)]).toEqual(['1', start]);
  });
});

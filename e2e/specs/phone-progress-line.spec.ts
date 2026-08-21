import {
  test,
  expect,
  callBinding,
  resetEvents,
  waitForEvent,
  LONG_TRACK,
  NO_QUEUE_SOURCE,
} from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * The phone's progress line (#58).
 *
 * The component tier already pins what the line *says* — that it
 * renders the backend's reported position and never a count of its own.
 * What only a real shell can answer is **where it is**: the issue asks
 * for a line on the border between the mini player and the tab bar, and
 * "on the border" is two adjacencies in a grid that no component-level
 * render has around it.
 *
 * It also asserts the line is not there on a desktop, which is the
 * other half of the same fact: above 600px there is no tab bar for it
 * to sit on the border of, and the bar carries a real seek bar.
 */
type Rect = { x: number; y: number; width: number; height: number };

/** The reference device's real viewport. */
const DEVICE = { width: 424, height: 439 };
const DESKTOP = { width: 1280, height: 800 };

async function rectOf(app: Page, selector: string): Promise<Rect | null> {
  return app.evaluate((sel) => {
    const el = document.querySelector(sel);

    if (!el) return null;

    const r = el.getBoundingClientRect();

    return { x: r.x, y: r.y, width: r.width, height: r.height };
  }, selector);
}

/**
 * Put the 90-second fixture on and wait for the first position report.
 *
 * The long track rather than any track: every other fixture is 2-6
 * seconds, which is shorter than the time this spec takes to measure
 * three rectangles.
 */
async function play(app: Page): Promise<void> {
  const tracks = await callBinding<{ FilePath: string; TrackName: string }[]>(
    app,
    'library.Library.GetTracks',
    [0],
  );

  // `TrackName`, not `Title`: that is what the library model calls it.
  const long = tracks.find((t) => t.TrackName === LONG_TRACK);

  expect(long, `no fixture track named ${LONG_TRACK}`).toBeTruthy();

  await callBinding(app, 'queue.Queue.Clear');
  await resetEvents(app);
  await callBinding(app, 'queue.Queue.SetQueue', [
    [long!.FilePath],
    0,
    false,
    NO_QUEUE_SOURCE,
  ]);
  await waitForEvent(app, 'QueueChanged');
  await callBinding(app, 'queue.Queue.Play');
  await waitForEvent(app, 'PlaybackPositionChanged', { timeoutMs: 15_000 });
}

test.describe('the progress line sits on the border between the bars', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await play(app);
  });

  test('spans the width, between the mini player and the tab bar', async ({
    app,
  }) => {
    const line = await rectOf(app, 'player-progress-line');
    const bar = await rectOf(app, '.bottom-bar');
    const nav = await rectOf(app, 'bottom-nav');

    expect(line, 'no progress line on the phone').not.toBeNull();
    expect(bar).not.toBeNull();
    expect(nav).not.toBeNull();

    // A border, not a band: 2px, the full width, and touching both.
    expect(line!.height).toBeCloseTo(2, 0);
    expect(line!.width).toBeCloseTo(bar!.width, 0);
    expect(line!.y).toBeCloseTo(bar!.y + bar!.height, 0);
    expect(nav!.y).toBeCloseTo(line!.y + line!.height, 0);
  });

  /**
   * It is 2px on the top edge of the tab bar, which is exactly where a
   * thumb aiming at a tab lands. A line that sometimes seeks is worse
   * than one that never does, so it must take no part in hit testing
   * at all.
   */
  test('takes no taps', async ({ app }) => {
    const line = await rectOf(app, 'player-progress-line');

    const hit = await app.evaluate(
      ({ x, y }) => document.elementFromPoint(x, y)?.tagName ?? '',
      { x: line!.x + line!.width / 2, y: line!.y + 1 },
    );

    expect(hit).not.toBe('PLAYER-PROGRESS-LINE');
  });

  test('is not there on a desktop', async ({ app }) => {
    await app.setViewportSize(DESKTOP);

    await expect(app.locator('player-progress-line')).toBeHidden();
  });
});

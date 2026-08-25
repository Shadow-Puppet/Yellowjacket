import {
  test,
  expect,
  callBinding,
  openTheQueue,
  NO_QUEUE_SOURCE,
} from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * #67 — a name is not a link on a phone, and the menu is where it went.
 *
 * The queue panel is the surface this is visible on: its rows draw a
 * track title and an artist credit as `explore-link`s at every width,
 * unlike `track-list`, whose phone column set stacks title over artist
 * as plain text already.
 *
 * **The pair is what makes either assertion mean anything.** A link
 * that is gone and a menu item that never arrived is not a smaller
 * affordance — it is a destination the phone cannot reach, which is
 * what plan 018's "no action is unreachable at any supported size"
 * refuses. So each test asserts the phone and the desktop in the same
 * breath: text *and* an item here, a link *and* no item there.
 *
 * The desktop half is also the regression guard for the change: menus
 * above the breakpoint must be exactly what they were, because the name
 * beside them is still a link and a menu that repeats the row is
 * furniture.
 */

/** The reference device's real viewport, not a resized desktop. */
const DEVICE = { width: 424, height: 439 };

/** Wide enough that the queue is a column beside the content. */
const DESKTOP = { width: 1280, height: 800 };

const row = (app: Page, index: number) =>
  app.locator(`queue-panel .track-item[data-index="${index}"]`);

/** The queue panel's own context menu, as a list of item labels. */
async function menuLabels(app: Page): Promise<string[]> {
  return app.evaluate(() =>
    [
      ...document
        .querySelector('queue-panel')!
        .shadowRoot!.querySelectorAll('wa-dropdown-item'),
    ].map((item) => item.textContent?.replace(/\s+/g, ' ').trim() ?? ''),
  );
}

/**
 * Queue three tracks that have an album, for the reason
 * `queue-selection.spec.ts` states at length: `explore-link` routes a
 * title to its *album's* page and renders plain text where it cannot
 * route, so a track with no album answers this file's question with
 * the wrong "no link".
 */
async function queueThree(app: Page): Promise<void> {
  const paths = await app.evaluate(async () => {
    const tracks = (await window.__yjEvents.call(
      'library.Library.GetTracks',
      [0],
      10_000,
    )) as { FilePath: string; Album: string; ArtistName: string }[];

    return tracks
      .filter((t) => t.Album !== '' && t.ArtistName !== '')
      .slice(0, 3)
      .map((t) => t.FilePath);
  });

  await callBinding(app, 'queue.Queue.SetQueue', [
    paths,
    0,
    false,
    NO_QUEUE_SOURCE,
  ]);
}

/** Open the row's context menu and read the items back. */
async function openRowMenu(app: Page, index: number): Promise<string[]> {
  await row(app, index).click({ button: 'right' });
  await expect
    .poll(async () => (await menuLabels(app)).length)
    .toBeGreaterThan(0);

  return menuLabels(app);
}

test.describe('an inline name and the menu that replaces it', () => {
  test.afterEach(async ({ app }) => {
    await app.keyboard.press('Escape');
    await callBinding(app, 'queue.Queue.Clear').catch(() => {
      /* an empty queue is the state we were asking for */
    });
    await app.setViewportSize(DESKTOP);
  });

  test('a queue row is plain text on a phone and carries the destination', async ({
    app,
  }) => {
    await app.setViewportSize(DEVICE);
    await queueThree(app);
    await openTheQueue(app);
    await expect(row(app, 0)).toBeVisible();

    // The name is text: nothing in the row is a link at all.
    await expect(app.locator('queue-panel .track-item .explore-link')).toHaveCount(
      0,
    );

    const labels = await openRowMenu(app, 0);

    expect(labels).toContain('Go to Artist');
    expect(labels).toContain('Go to Album');
  });

  test('the same row on a desktop is a link, and its menu is untouched', async ({
    app,
  }) => {
    await app.setViewportSize(DESKTOP);
    await queueThree(app);
    await openTheQueue(app);
    await expect(row(app, 0)).toBeVisible();

    await expect(
      row(app, 0).locator('.track-title .explore-link'),
    ).toHaveCount(1);

    const labels = await openRowMenu(app, 0);

    expect(labels).not.toContain('Go to Artist');
    expect(labels).not.toContain('Go to Album');
  });
});

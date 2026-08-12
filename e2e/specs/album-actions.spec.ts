import { test, expect, callBinding } from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * Plan 007 phase 5: `H-13` — the album page can be played from.
 *
 * The page is `explore-album-details`; there is no library-side album
 * detail page at all, so this catalog page is where a Play button has
 * to live, and what it can honestly claim depends on how much of the
 * album the user owns.
 *
 * What this tier adds over the component tests is that the paths
 * resolve to something the queue accepts. The first version of this
 * feature keyed the lookup on recording MBIDs — which is how the
 * backend decides a track is `inLibrary` — and a library-only album has
 * none, so Play was wired, labelled correctly, clicked cleanly and
 * queued **nothing**. Every component test still passed.
 */
test.describe('playing an album from its page', () => {
  test.beforeEach(async ({ app }) => {
    await openFirstAlbum(app);
  });

  test.afterEach(async ({ app }) => {
    // The suite shares one backend process in file order, and a queue
    // left full is state the next spec did not ask for.
    await callBinding(app, 'queue.Queue.Clear', []);
    await app.getByTestId('nav-tracks').click();
  });

  test('Play queues what the user owns of it', async ({ app }) => {
    const play = app
      .locator('explore-album-details')
      .locator('[data-testid="album-play"]');

    // The fixture library is untagged, so this album is wholly local
    // and the button carries no count.
    await expect(play).toContainText('Play');

    await play.click();

    await expect.poll(() => queueLength(app)).toBeGreaterThan(0);
  });

  test('Add to queue appends rather than replacing', async ({ app }) => {
    const details = app.locator('explore-album-details');

    await details.locator('[data-testid="album-play"]').click();
    await expect.poll(() => queueLength(app)).toBeGreaterThan(0);

    const before = await queueLength(app);

    await details.locator('[data-testid="album-queue"]').click();

    await expect.poll(() => queueLength(app)).toBe(before * 2);
  });

  test('the ticks against the tracks have a legend', async ({ app }) => {
    // `H-13` calls them unexplained. They were never *unlabelled* — the
    // indicator has carried a title and an aria-label reading
    // "Track “X” is in your library" all along — but a sighted user
    // scanning the page got a column of green circles and no key.
    await expect(
      app.locator('explore-album-details').locator('.tracklist-legend'),
    ).toContainText('in your library');
  });

  test('the ticks are badges, not keyboard stops', async ({ app }) => {
    // Every one of them was a <button> whose click handler was a
    // stopPropagation() and a comment saying to wire up the download
    // client later: on an Explore results page, 20 of 66 tab stops
    // promised an action and performed none. Counted here rather than
    // reasoned about, because the count is the finding.
    const stops = await app.evaluate(() => {
      const badges = [
        ...(document
          .querySelector('explore-album-details')
          ?.shadowRoot?.querySelectorAll('library-status-indicator') ?? []),
      ];

      return {
        badges: badges.length,
        focusable: badges.filter((b) =>
          b.shadowRoot?.querySelector('button, [tabindex]:not([tabindex="-1"])'),
        ).length,
        labelled: badges.filter((b) =>
          b.shadowRoot?.querySelector('[role="img"][aria-label]'),
        ).length,
      };
    });

    expect(stops.badges).toBeGreaterThan(0);
    expect(stops.focusable).toBe(0);
    expect(stops.labelled).toBe(stops.badges);
  });
});

/** Albums → click the second card, which navigates to the album page. */
async function openFirstAlbum(app: Page): Promise<void> {
  await app.getByTestId('nav-albums').click();
  await expect(app.getByTestId('main-content')).toHaveAttribute(
    'data-active-view',
    'albums',
  );

  // The cards come from a virtualizer, so they are not there when the
  // view is: a click dispatched into an empty grid hits nothing and
  // silently leaves the app on Albums, which reads as a broken
  // navigation rather than a race.
  await expect.poll(() => cardCount(app)).toBeGreaterThan(1);

  // A plain click on a card navigates here; Enter expands the dropdown
  // instead. Dispatched rather than clicked because the card lives in a
  // virtualizer inside a shadow root.
  await app.evaluate(() => {
    document
      .querySelector('cover-grid')
      ?.shadowRoot?.querySelectorAll('.album-card')[1]
      ?.dispatchEvent(
        new MouseEvent('click', { bubbles: true, composed: true }),
      );
  });

  await expect(app.getByTestId('main-content')).toHaveAttribute(
    'data-active-view',
    'explore-album-details',
  );
  await expect(
    app.locator('explore-album-details').locator('[data-testid="album-play"]'),
  ).toBeVisible();
}

async function cardCount(app: Page): Promise<number> {
  return app.evaluate(
    () =>
      document
        .querySelector('cover-grid')
        ?.shadowRoot?.querySelectorAll('.album-card').length ?? 0,
  );
}

async function queueLength(app: Page): Promise<number> {
  const state = (await callBinding(app, 'queue.Queue.GetState', [])) as {
    tracks?: unknown[];
  };

  return state?.tracks?.length ?? 0;
}

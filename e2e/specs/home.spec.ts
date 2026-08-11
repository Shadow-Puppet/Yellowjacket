import { test, expect, waitForEvent, resetEvents } from '../support/fixtures.js';

/**
 * The home page, which is the one view whose content is a *judgement*
 * rather than a listing: `backend/home` decides which shelves exist for
 * this library and why, and the page is only correct if that reasoning
 * survives to the screen.
 *
 * The fixture library has never been played, so the shelves that need
 * history are legitimately absent — asserting on which ones appear is
 * asserting that the page does not invent them.
 */
test.describe('home', () => {
  test.beforeEach(async ({ app }) => {
    await app.getByTestId('nav-home').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'home',
    );
  });

  test('offers shelves, each with the reason it is there', async ({ app }) => {
    const shelves = app.locator('home-view .shelf');

    await expect(shelves.first()).toBeVisible();

    const count = await shelves.count();
    expect(count).toBeGreaterThan(0);

    // Every shelf says why it exists.  A row of covers with no reason
    // is the albums grid, which the user already has.
    for (let i = 0; i < count; i += 1) {
      await expect(shelves.nth(i).locator('.shelf-sub')).not.toBeEmpty();
    }
  });

  test('never renders a shelf with nothing on it', async ({ app }) => {
    // The reason shelves are built server-side: a row that promises
    // "what you play the most" and then shows nothing is worse than no
    // row, so a shelf with no albums must not reach the page at all.
    const shelves = app.locator('home-view .shelf');

    await expect(shelves.first()).toBeVisible();

    const count = await shelves.count();

    for (let i = 0; i < count; i += 1) {
      await expect(shelves.nth(i).locator('.card').first()).toBeVisible();
    }
  });

  test('a cover opens that album', async ({ app }) => {
    const card = app.locator('home-view .card').first();
    const name = await card.locator('.name').innerText();

    await card.click();

    await expect(app.locator('explore-album-details')).toBeVisible();
    await expect(app.locator('explore-album-details .album-title')).toContainText(
      name,
    );
  });

  test('the play button plays the album instead of opening it', async ({
    app,
  }) => {
    await resetEvents(app);

    const card = app.locator('home-view .card').first();

    await card.hover();
    await card.locator('.play').click();

    await waitForEvent(app, 'TrackChanged');

    // Playing must not also navigate: the two actions live on the same
    // card and the inner one has to win outright.
    await expect(app.locator('home-view')).toBeVisible();
    await expect(app.locator('explore-album-details')).toHaveCount(0);
  });
});

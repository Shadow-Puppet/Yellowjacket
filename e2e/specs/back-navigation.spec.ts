import { test, expect } from '../support/fixtures.js';

/**
 * Back is the platform's, and the app has to have somewhere for it to
 * go (reported from a device: "the Android back button does not
 * navigate back in the app").
 *
 * The scaffold's `MainActivity.onBackPressed` asks `webView.canGoBack()`
 * and finishes the activity otherwise. This app never touched
 * `history`, so that was always false and back quit from any depth. A
 * navigation is a history entry now, which is why this is assertable
 * here at all: `page.goBack()` is the same `popstate` the phone's
 * gesture produces, so the browser tier can answer a question that
 * otherwise needs a device.
 *
 * What it cannot answer is whether Android's *gesture* reaches the
 * WebView, which is between the OS and the scaffold.
 */
type Page = import('@playwright/test').Page;

const activeView = (page: Page) =>
  page.getByTestId('main-content');

/**
 * Open an artist's detail view, which is the deepest ordinary route.
 *
 * A library artist opens `explore-artist-details` -- the catalog panel
 * standing in for a library one, as `explore-link.ts` describes -- and
 * the view name follows the component, not the source of the click.
 */
async function openAnArtist(app: Page): Promise<void> {
  await app.getByTestId('nav-artists').click();
  await expect(activeView(app)).toHaveAttribute('data-active-view', 'artists');

  // A card, by the name on it: the grid is virtualized and positioned
  // by transform, so a click at coordinates is a click at whatever
  // happens to be there.
  await app.locator('artists-view').getByText('Aurora Fields').first().click();
  await expect(activeView(app)).toHaveAttribute(
    'data-active-view',
    'explore-artist-details',
  );
}

test.describe('the back gesture', () => {
  test('leaves a detail view for the view it was opened from', async ({
    app,
  }) => {
    await openAnArtist(app);

    await app.goBack();

    await expect(activeView(app)).toHaveAttribute('data-active-view', 'artists');
  });

  test('walks back through primary views, one press per navigation', async ({
    app,
  }) => {
    await app.getByTestId('nav-tracks').click();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'tracks');

    await app.getByTestId('nav-albums').click();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');

    await app.goBack();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'tracks');

    // Forward is free once back works, and it is what proves the entry
    // was restored rather than the view merely re-rendered.
    await app.goForward();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');
  });

  test('an in-app back button consumes exactly one entry', async ({ app }) => {
    await app.getByTestId('nav-tracks').click();
    await openAnArtist(app);

    // The detail view's own back button and the phone's gesture are the
    // same press: if each popped its own stack, this would land two
    // navigations back instead of one.
    await app
      .locator('explore-artist-details')
      .getByRole('button', { name: 'Back to explore' })
      .click();

    await expect(activeView(app)).toHaveAttribute('data-active-view', 'artists');

    await app.goBack();

    await expect(activeView(app)).toHaveAttribute('data-active-view', 'tracks');
  });
});

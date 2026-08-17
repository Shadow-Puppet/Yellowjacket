import { test, expect } from '../support/fixtures.js';

/**
 * The phone shell (plan 016 B2, phase 1).
 *
 * This is the tier that can actually answer the question. Wails v3's
 * server mode serves the real frontend, so a Chromium at 390×844 is the
 * same document an Android WebView renders — the only thing a device
 * adds here is the WebView's own quirks, and CI runs the WebKit half
 * for exactly that reason.
 *
 * The assertions are the three things B2 is *for*: the eleven-item
 * sidebar is gone, the four destinations plan 016 committed to are
 * reachable with a thumb, and nothing scrolls sideways. The last one is
 * the one that hides: `overflow-x: auto` on `body` means a shell that
 * does not fit produces a scrollbar rather than a broken layout, which
 * looks survivable in a screenshot and is not.
 */

/** A common small phone. Narrower than any device this is likely to meet. */
const PHONE = { width: 390, height: 844 };

/** The narrowest thing still sold, near enough. */
const SMALL_PHONE = { width: 360, height: 780 };

const horizontalOverflow = (page: import('@playwright/test').Page) =>
  page.evaluate(() => ({
    scrollWidth: document.body.scrollWidth,
    clientWidth: document.body.clientWidth,
  }));

test.describe('the shell on a phone', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(PHONE);
  });

  test('replaces the sidebar with a bottom tab bar', async ({ app }) => {
    await expect(app.locator('div.sidebar')).toBeHidden();

    const nav = app.locator('bottom-nav');

    await expect(nav).toBeVisible();

    // Four tabs and a way to everything else, which is the shape the
    // plan argues for: a tab bar is 3-5 items before the targets stop
    // being thumb-sized.
    for (const id of ['home', 'albums', 'tracks', 'playlists', 'more']) {
      await expect(app.getByTestId(`tab-${id}`)).toBeVisible();
    }
  });

  test('navigates from a tab', async ({ app }) => {
    await app.getByTestId('tab-albums').click();

    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'albums');

    await app.getByTestId('tab-home').click();

    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'home');
  });

  test('reaches the views with no tab through the drawer', async ({ app }) => {
    await app.getByTestId('tab-more').click();

    // Scoped to the drawer: the desktop sidebar is still in the DOM
    // (hidden by the media query, not removed), so an unscoped testid
    // matches two elements and Playwright's strict mode refuses --
    // which is the right complaint, since the two really are different
    // buttons.
    //
    // The drawer holds the *same* sidebar the desktop uses, so Settings
    // -- which a phone still needs occasionally -- is reachable without
    // a second list of destinations to keep in step.
    const settings = app
      .getByTestId('nav-drawer')
      .getByTestId('nav-settings');

    await expect(settings).toBeVisible();
    await settings.click();

    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'settings');

    // And the drawer gets out of the way once it has done its job.
    await expect(app.getByTestId('nav-drawer')).toBeHidden();
  });

  test('has a named drawer', async ({ app }) => {
    await app.getByTestId('tab-more').click();

    // The a11y snapshot never prints a dialog's name, so this asks for
    // the role and the name together -- which is the check that caught
    // eleven unnamed dialogs.
    await expect(
      app.getByRole('dialog', { name: 'All views' }),
    ).toBeVisible();
  });

  for (const vp of [PHONE, SMALL_PHONE]) {
    test(`does not scroll sideways at ${vp.width}×${vp.height}`, async ({ app }) => {
      await app.setViewportSize(vp);
      await app.getByTestId('tab-tracks').click();
      await expect(app.getByTestId('main-content'))
        .toHaveAttribute('data-active-view', 'tracks');

      const { scrollWidth, clientWidth } = await horizontalOverflow(app);

      expect(scrollWidth, `body overflows by ${scrollWidth - clientWidth}px`)
        .toBeLessThanOrEqual(clientWidth);
    });
  }

  test('opens the full-screen now playing, and comes back', async ({ app }) => {
    // Something has to be playing for the mini player to be a way in.
    await app.getByTestId('tab-tracks').click();
    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'tracks');

    await app.locator('track-list .track-row').first().dblclick();
    await expect(app.getByTestId('now-playing-title')).not.toBeEmpty();

    await app.getByTestId('open-now-playing').click();

    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'now-playing');

    // The seek bar and volume that phase 1 took out of the bottom bar
    // are here, and they are the *same* components -- this view
    // composes the transport rather than reimplementing it.
    await expect(app.locator('now-playing-view seek-bar')).toBeVisible();
    await expect(app.locator('now-playing-view volume-control')).toBeVisible();

    // Back goes where the user came from, through the nav stack.
    await app.getByTestId('npv-back').click();
    await expect(app.getByTestId('main-content'))
      .toHaveAttribute('data-active-view', 'tracks');
  });

  test('offers no way in on a desktop, where the bar is whole', async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });

    // The button exists in the markup at every size; CSS decides. If
    // this becomes visible on a desktop it is a 48px hit target over
    // the cover art, swallowing the clicks that open the preview.
    await expect(app.getByTestId('open-now-playing')).toBeHidden();
  });

  test('keeps the transport, minus what a thumb cannot use', async ({ app }) => {
    // The player bar stays: this is a music player, and what is playing
    // has to be visible and pausable from every view.
    await expect(app.locator('audio-player')).toBeVisible();
    await expect(app.locator('now-playing')).toBeVisible();

    // Volume is the hardware keys' job on a phone, and a 4px seek bar
    // is not a thumb target -- both belong to a later phase's
    // full-screen now-playing view.
    await expect(app.locator('audio-player volume-control')).toBeHidden();
  });
});

test.describe('the desktop shell is unchanged', () => {
  test('keeps the sidebar and hides the tab bar', async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });

    await expect(app.locator('div.sidebar')).toBeVisible();
    await expect(app.locator('bottom-nav')).toBeHidden();
  });
});

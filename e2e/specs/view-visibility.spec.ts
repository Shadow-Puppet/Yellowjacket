import { test, expect, navigateTo } from '../support/fixtures.js';

/**
 * Which destinations the navigation offers (#25).
 *
 * Eleven sidebar entries is more than most libraries need, so they are
 * individually toggleable from Settings, Autotag is off until asked for
 * and Downloads is absent until there is a client to download with.
 *
 * **The assertions are about the navigation, not about the setting.**
 * "The config was saved" is the plumbing, and the two most recent bugs
 * in this area — #69 and #72 — both shipped green under specs that
 * measured exactly that. What a person sees is whether the item is in
 * the accessibility tree, and whether the view is still reachable when
 * it is not.
 *
 * This runs against the seeded app, whose config is defaults and whose
 * download client list is empty, so the initial state below is what a
 * fresh install looks like.
 */
type Page = import('@playwright/test').Page;

const navItem = (page: Page, label: string) =>
  page.getByRole('button', { name: label, exact: true });

/** The Navigation section's checkbox for a destination. */
const viewToggle = (page: Page, label: string) =>
  page.getByRole('checkbox', { name: `Show ${label} in the navigation` });

async function openNavigationSettings(page: Page): Promise<void> {
  await page.getByTestId('nav-settings').click();

  const section = page.locator(
    'config-page config-section[heading="Navigation"] .header',
  );

  await expect(section).toBeVisible();

  if ((await section.getAttribute('aria-expanded')) === 'false') {
    await section.click();
  }

  await expect(section).toHaveAttribute('aria-expanded', 'true');
}

test.describe('configurable destinations', () => {
  test('Autotag is off by default and Downloads needs a client', async ({
    app,
  }) => {
    await expect(app.getByTestId('nav-home')).toBeVisible();

    await expect(app.getByTestId('nav-autotag')).toHaveCount(0);
    await expect(app.getByTestId('nav-downloads')).toHaveCount(0);
  });

  /**
   * Hiding takes the item away and nothing else. Detail views navigate
   * into these and the launch page is one of them, so a destination
   * with no nav item still has to open.
   */
  test('a hidden destination is still reachable', async ({ app }) => {
    await navigateTo(app, 'autotag');

    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'autotag',
    );

    // And nothing is falsely lit while standing on it -- the same rule
    // a detail view follows, with no special case for either.
    await expect(navItem(app, 'Home')).toHaveAttribute('aria-current', 'false');
  });

  test('switching Autotag on adds it to the sidebar', async ({ app }) => {
    await openNavigationSettings(app);

    await viewToggle(app, 'Autotag').check();

    await expect(app.getByTestId('nav-autotag')).toBeVisible();

    // Clicking it is the point of having it.
    await app.getByTestId('nav-autotag').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'autotag',
    );

    // Put it back, or the next spec against this app sees a library
    // this one changed.
    await openNavigationSettings(app);
    await viewToggle(app, 'Autotag').uncheck();
    await expect(app.getByTestId('nav-autotag')).toHaveCount(0);
  });

  /**
   * Settings has no toggle at all, rather than a toggle that refuses:
   * a user who hides it cannot get back to unhide it. The backend
   * refuses it too, because `config.toml` is hand-editable.
   */
  test('Settings cannot be switched off', async ({ app }) => {
    await openNavigationSettings(app);

    await expect(viewToggle(app, 'Settings')).toBeDisabled();
    await expect(app.getByTestId('nav-settings')).toBeVisible();
  });

  /**
   * The launch page is refused while it is the launch page, which is a
   * state the user can leave by changing the launch page above it.
   */
  test('the launch page cannot be switched off', async ({ app }) => {
    await openNavigationSettings(app);

    await expect(viewToggle(app, 'Home')).toBeDisabled();
  });
});

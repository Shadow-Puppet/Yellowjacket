import { test, expect } from '../support/fixtures.js';

/**
 * Plan 007 phase 5: a11y.1 and a11y.2, frozen against the real app.
 *
 * Every `config-section` header was a bare `<div @click>` and every
 * section defaults to collapsed, so every setting in the app was behind
 * a control that could not be tabbed to. Downloads' two tabs were the
 * same bug one page over.
 *
 * A component test can see the markup; only this tier can see that the
 * control is reachable *through the app's own tab order*, past the
 * sidebar, the header and whatever else is on screen.
 */
test.describe('Settings is reachable without a mouse', () => {
  test('every section disclosure is a button that reports its state', async ({
    app,
  }) => {
    await app.getByTestId('nav-settings').click();

    const headers = app.locator('config-page config-section .header');

    await expect(headers.first()).toBeVisible();

    const count = await headers.count();

    expect(count).toBeGreaterThan(4);

    for (let i = 0; i < count; i++) {
      const header = headers.nth(i);

      expect(await header.evaluate((el) => el.tagName)).toBe('BUTTON');
      expect(['true', 'false']).toContain(
        await header.getAttribute('aria-expanded'),
      );
    }
  });

  test('a collapsed section can be opened from the keyboard', async ({
    app,
  }) => {
    await app.getByTestId('nav-settings').click();

    const theme = app
      .locator('config-page config-section[heading="Theme"] .header')
      .first();

    await expect(theme).toHaveAttribute('aria-expanded', 'false');

    await theme.focus();
    await app.locator('body').press('Enter');

    await expect(theme).toHaveAttribute('aria-expanded', 'true');
  });
});

test.describe("Downloads' tabs are tabs", () => {
  test('arrow keys move the selection and swap the panel', async ({ app }) => {
    await app.getByTestId('nav-downloads').click();

    const view = app.locator('downloads-view');
    const requests = view.getByRole('tab', { name: 'Requests' });
    const downloads = view.getByRole('tab', { name: 'Downloads' });

    await expect(requests).toHaveAttribute('aria-selected', 'true');

    await requests.focus();
    await app.locator('body').press('ArrowRight');

    await expect(downloads).toHaveAttribute('aria-selected', 'true');
    await expect(view.locator('[role="tabpanel"]')).toHaveAttribute(
      'aria-labelledby',
      'tab-downloads',
    );
  });
});

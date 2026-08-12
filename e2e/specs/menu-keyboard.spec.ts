import { test, expect } from '../support/fixtures.js';

/**
 * The context menu is reachable, navigable and escapable without a
 * mouse.
 *
 * `a11y.3`: the menu is the only route to Play, Add to Queue, Play Next,
 * Add to Playlist, Favourite and Track Details, and it opened on
 * `contextmenu` alone. The panel had no `role="menu"`, so the six
 * `role="menuitem"`s Web Awesome sets were orphaned; nothing moved focus
 * into it, nothing handled Arrow or Escape, and nothing restored focus.
 *
 * Frozen here because two of the three things that made it work are
 * invisible to a component test running against hand-built markup: the
 * real `wa-dropdown-item`s do not have their `role` yet when the host
 * finishes updating, and the real `wa-popup` has not positioned itself,
 * so `focus()` on an item is a silent no-op. Both produced a menu that
 * opened and refused to take focus, which is exactly the bug this spec
 * exists to catch.
 */
test.describe('the context menu without a mouse', () => {
  /** The deep-focused element, described the way an AT would see it. */
  const focused = (page: import('@playwright/test').Page) =>
    page.evaluate(() => {
      let el = document.activeElement;

      while (el?.shadowRoot?.activeElement) el = el.shadowRoot.activeElement;

      return {
        role: el?.getAttribute('role') ?? '',
        text: el?.textContent?.trim().slice(0, 40) ?? '',
      };
    });

  /** The menu panel's own semantics, or null when it is not rendered. */
  const panel = (page: import('@playwright/test').Page) =>
    page.evaluate(() => {
      const el = document
        .querySelector('track-list')
        ?.shadowRoot?.querySelector('.context-menu-panel');

      if (!el) return null;

      return {
        role: el.getAttribute('role'),
        label: el.getAttribute('aria-label'),
        items: el.querySelectorAll('[role="menuitem"]').length,
      };
    });

  /** Tab to the track list's single roving stop. */
  async function focusARow(app: import('@playwright/test').Page) {
    for (let i = 0; i < 25; i += 1) {
      await app.keyboard.press('Tab');

      if ((await focused(app)).role === 'row') return;
    }

    throw new Error('never reached a track row');
  }

  test.beforeEach(async ({ app }) => {
    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
  });

  test('Shift+F10 on a row opens a menu and focuses its first item', async ({
    app,
  }) => {
    await focusARow(app);
    await app.keyboard.press('Shift+F10');

    await expect.poll(() => panel(app)).toMatchObject({
      role: 'menu',
      label: 'Track actions',
    });

    // The row is where the key came from; the menu is where focus went.
    await expect.poll(async () => (await focused(app)).role).toBe('menuitem');
  });

  test('the arrows move through the items and wrap', async ({ app }) => {
    await focusARow(app);
    await app.keyboard.press('Shift+F10');
    await expect.poll(async () => (await focused(app)).role).toBe('menuitem');

    const first = (await focused(app)).text;

    await app.keyboard.press('ArrowDown');

    const second = (await focused(app)).text;

    expect(second).not.toBe(first);

    await app.keyboard.press('ArrowUp');
    expect((await focused(app)).text).toBe(first);

    // Up from the first item wraps to the last, which is what a menu
    // does and what a list does not.
    await app.keyboard.press('ArrowUp');
    expect((await focused(app)).text).not.toBe(first);

    await app.keyboard.press('Escape');
  });

  test('Escape closes the menu and gives the row its focus back', async ({
    app,
  }) => {
    await focusARow(app);

    const row = (await focused(app)).text;

    await app.keyboard.press('Shift+F10');
    await expect.poll(async () => (await focused(app)).role).toBe('menuitem');

    await app.keyboard.press('Escape');

    await expect.poll(() => panel(app)).toBeNull();
    await expect.poll(async () => (await focused(app)).role).toBe('row');
    expect((await focused(app)).text).toBe(row);
  });
});

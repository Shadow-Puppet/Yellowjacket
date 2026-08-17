import { test, expect } from '../support/fixtures.js';

/**
 * Long-press is the touch route to a context menu (plan 016 B2 phase 3).
 *
 * The component tier proves the gesture in isolation, against markup it
 * built itself. What it cannot prove is the half that made this one
 * listener instead of six: that the synthetic event reaches the handler
 * a *real* component bound — `track-list` delegates its `contextmenu`
 * on the `lit-virtualizer` rather than binding one per row — and that
 * the real `wa-popup` menu opens from it, which is a path with its own
 * history of opening and then refusing to work (see
 * `menu-keyboard.spec.ts`).
 *
 * The pointer events are dispatched rather than performed: this project
 * runs Desktop Chrome and Desktop Safari, neither of which has touch,
 * and a device tier does not exist. So this is honest about what it
 * checks — the app's own listeners, on the app's own DOM, from the
 * events a touch would produce — and not about a real finger.
 */

/** A common small phone, as in `phone-shell.spec.ts`. */
const PHONE = { width: 390, height: 844 };

/** Comfortably past the module's 500ms hold. */
const HELD = 900;

type Page = import('@playwright/test').Page;

/** The track list's menu panel, or null while it is not rendered. */
const panel = (page: Page) =>
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

/**
 * Press the first track row, optionally dragging partway through — the
 * shape of a scroll that begins on a row, which must not open a menu.
 */
async function pressFirstRow(
  page: Page,
  opts: { driftY?: number } = {},
): Promise<void> {
  await page.evaluate((drift) => {
    // `.track-row`, not `[role="row"]`: the column header is a row too,
    // and it is the *first* one -- a press on it is correctly ignored,
    // which reads exactly like the gesture not working.
    const row = document
      .querySelector('track-list')
      ?.shadowRoot?.querySelector('.track-row');

    if (!row) throw new Error('no track row to press');

    const box = row.getBoundingClientRect();
    const x = Math.round(box.left + box.width / 2);
    const y = Math.round(box.top + box.height / 2);
    const send = (type: string, dy = 0) =>
      row.dispatchEvent(
        new PointerEvent(type, {
          bubbles: true,
          composed: true,
          cancelable: true,
          pointerType: 'touch',
          isPrimary: true,
          clientX: x,
          clientY: y + dy,
        }),
      );

    send('pointerdown');

    if (drift) send('pointermove', drift);
  }, opts.driftY ?? 0);
}

test.describe('long-press opens the track menu', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(PHONE);
    await app.getByTestId('tab-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
  });

  test.afterEach(async ({ app }) => {
    // Every other spec file runs against a desktop, and the viewport
    // belongs to the shared context rather than to this file.
    await app.setViewportSize({ width: 1440, height: 900 });
  });

  test('reaches the delegated handler and opens the real menu', async ({
    app,
  }) => {
    await expect.poll(() => panel(app)).toBeNull();

    await pressFirstRow(app);

    await expect
      .poll(() => panel(app), { timeout: HELD + 2000 })
      .toMatchObject({ role: 'menu', label: 'Track actions' });

    // The same panel Shift+F10 opens, items and all -- not an empty
    // popup that happened to become visible.
    expect((await panel(app))?.items).toBeGreaterThan(0);
  });

  test('does not open one for a press that turns into a scroll', async ({
    app,
  }) => {
    await pressFirstRow(app, { driftY: 40 });

    await app.waitForTimeout(HELD);

    expect(await panel(app)).toBeNull();
  });
});

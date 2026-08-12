import { test, expect } from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * Plan 007 phase 5: every `wa-dialog` in this app was an unnamed dialog.
 *
 * `a11y.md` lists all of them under "what is already correct" and notes
 * that every call site passes a `label`. Both true, and the label never
 * reached the accessibility tree: Web Awesome renders it into an
 * `<h2 id="title">` in the same shadow root as the native `<dialog>` and
 * never points `aria-labelledby` at it.
 *
 * This spec is the reason the fix is believable, and it lives here
 * rather than in the component tier for one reason: **only Playwright
 * computes an accessible name.** The Vitest tier queries shadow roots
 * directly, so it can assert the IDREF is wired and resolves to a
 * heading carrying the label — it cannot assert that anything would
 * announce it. `getByRole('dialog', { name })` can, and before the fix
 * it matched nothing anywhere in this app.
 *
 * Nor can the a11y *snapshot*: `playwright-cli snapshot` renders this
 * dialog as a bare `- dialog [ref=…]` whether it is named by
 * `aria-labelledby`, named by `aria-label`, or not named at all —
 * checked all three ways against the running app. Verifying the fix by
 * reading a snapshot would have reported failure on a working build,
 * which is this plan's most-repeated trap wearing an accessibility hat:
 * a probe that cannot move is not evidence.
 */
test.describe('a dialog says what it is', () => {
  test('the shortcuts overlay is announced by its title', async ({ app }) => {
    await app.keyboard.press('?');

    await expect(
      app.locator('shortcuts-overlay').getByRole('dialog', {
        name: 'Keyboard Shortcuts',
      }),
    ).toBeVisible();

    await app.keyboard.press('Escape');
    await expect(
      app.locator('shortcuts-overlay').getByRole('dialog'),
    ).toHaveCount(0);
  });

  test('a dialog with a computed label is announced by it', async ({ app }) => {
    // `track-details` builds its label at render time ("Track Details"
    // or "Batch Edit"), which is why it is the one checked here: the
    // fix is an IDREF to the heading Web Awesome re-renders, so a label
    // that changes stays announced without anything resyncing it.
    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );

    await openRowMenu(app);
    await app.getByRole('menuitem', { name: 'Track Details' }).click();

    await expect(
      app.getByRole('dialog', { name: 'Track Details' }),
    ).toBeVisible();

    // Leave the app as this suite found it — the specs share one
    // backend process in file order.
    await app.keyboard.press('Escape');
    await expect(
      app.getByRole('dialog', { name: 'Track Details' }),
    ).toHaveCount(0);
  });
});

/** Right-click the third track row, which is where the menu is anchored. */
async function openRowMenu(app: Page): Promise<void> {
  await app.evaluate(() => {
    const row = document
      .querySelector('track-list')
      ?.shadowRoot?.querySelectorAll('[role="row"]')[2];

    row?.dispatchEvent(
      new MouseEvent('contextmenu', {
        bubbles: true,
        composed: true,
        clientX: 200,
        clientY: 300,
      }),
    );
  });

  await expect(app.getByRole('menu', { name: 'Track actions' })).toBeVisible();
}

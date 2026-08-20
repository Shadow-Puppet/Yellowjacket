import { test, expect, waitForEvent } from '../support/fixtures.js';

/**
 * The Jobs tab folded into the places the work is started (#27).
 *
 * The assertion worth making is not that the tab is gone — that is one
 * line of a table — but that **nothing became unreachable when it
 * went**. Scanning is the case that mattered: the per-library controls
 * lived only on that page, and the tab's own comment says they had been
 * moved there out of Settings in the first place.
 *
 * `#24` wrote down one sentence covering all three size bands: *no
 * action is ever unreachable at any supported size*. Deleting a
 * destination is exactly the change that can quietly break it.
 */
type Page = import('@playwright/test').Page;

const section = (page: Page, heading: string) =>
  page.locator(`config-page config-section[heading="${heading}"]`);

async function openSettings(page: Page, heading: string): Promise<void> {
  await page.getByTestId('nav-settings').click();

  // The section's own disclosure, by role rather than by `.header`:
  // an open Libraries section also contains `job-details-drawer`,
  // whose own header matches that class and makes it ambiguous.
  const header = section(page, heading)
    .getByRole('button', { name: heading })
    .first();

  await expect(header).toBeVisible();

  if ((await header.getAttribute('aria-expanded')) === 'false') {
    await header.click();
  }

  await expect(header).toHaveAttribute('aria-expanded', 'true');
}

test.describe('background jobs live where the work is started', () => {
  test('the Jobs destination is gone', async ({ app }) => {
    await expect(app.getByTestId('nav-jobs')).toHaveCount(0);

    // And it is not offered as a launch page either, which is the copy
    // of the destination list that is easiest to forget.
    await openSettings(app, 'General');

    const options = await section(app, 'General')
      .locator('select')
      .first()
      .locator('option')
      .allTextContents();

    expect(options).not.toContain('Jobs');
  });

  /**
   * Scanning is startable from Settings → Libraries, and the job that
   * results is visible there with its controls. One assertion covers
   * both halves, because a Scan All that started nothing would leave
   * the panel empty and read exactly like a panel that does not work.
   */
  test('a scan is started and watched in Settings', async ({ app }) => {
    await openSettings(app, 'Libraries');

    const libraries = section(app, 'Libraries');

    await libraries.getByRole('button', { name: 'Scan All' }).click();

    await waitForEvent(app, 'LibraryScanComplete', { timeoutMs: 60_000 });

    const panel = libraries.locator('job-panel');

    await expect(panel.locator('job-row')).toHaveCount(1, { timeout: 10_000 });

    // The generic affordances are the point of the panel: the tier
    // list and the progress rings the other surfaces already had
    // cannot open a log.
    await expect(
      panel.getByRole('button', { name: /^Details/ }),
    ).toBeVisible();
  });

  /** A finished job dismisses from where it is shown. */
  test('a finished scan can be dismissed in place', async ({ app }) => {
    await openSettings(app, 'Libraries');

    const panel = section(app, 'Libraries').locator('job-panel');
    const dismiss = panel.getByRole('button', { name: /^Dismiss/ }).first();

    await expect(dismiss).toBeVisible({ timeout: 10_000 });
    await dismiss.click();

    await expect(panel.locator('job-row')).toHaveCount(0);
  });

  /**
   * Full rescan is destructive and asks first. It is asserted at the
   * dialog rather than through it — running one against the seeded app
   * would delete the library the rest of the suite reads.
   */
  test('Full Rescan asks before it does anything', async ({ app }) => {
    await openSettings(app, 'Libraries');

    await section(app, 'Libraries')
      .getByRole('button', { name: 'Full Rescan' })
      .click();

    const dialog = app.getByRole('dialog', { name: 'Full rescan' });

    await expect(dialog).toBeVisible();

    // The message is read off the *host*, not the dialog: a wa-dialog
    // keeps its slotted content in the host's shadow root, so
    // `toContainText` on the dialog itself sees only Web Awesome's
    // chrome.
    await expect(app.locator('confirm-dialog')).toContainText(
      'deletes all library data',
    );

    await app.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toBeHidden();
  });
});

import { test, expect } from '../support/fixtures.js';

/**
 * Plan 007 phase 3: a failure the user caused has to reach the user.
 *
 * The failure is induced through `/__test/sql` rather than staged in the
 * UI: a second library called "Decoy" makes `RenameLibrary` reject with
 * its duplicate-name error, which is a real binding rejection on a path
 * the audit found ends at `console.error` (errors.m5).
 */
test.describe('a failed binding says so', () => {
  const DECOY = 'Decoy';

  test.afterEach(async ({ testctl }) => {
    await testctl.sql('DELETE FROM libraries WHERE name = ?', [DECOY]);
  });

  test('a rejected rename reaches the user, not the console', async ({
    app,
    testctl,
  }) => {
    // The library that was seeded by running the app, i.e. the one row
    // that is not the decoy.
    const health = await testctl.health();
    const seeded = health.libraries[0].name as string;

    await testctl.sql(
      'INSERT INTO libraries (name, path) VALUES (?, ?)',
      [DECOY, '/tmp/yj-decoy-library'],
    );

    await app.getByTestId('nav-settings').click();

    const page = app.locator('config-page');

    // Libraries is the one section that starts expanded (H-22), so ask
    // the disclosure what state it is in rather than assuming one — a
    // blind click used to expand it and now collapses it.
    //
    // By role and name, not by `.header`: since #27 the section also
    // contains a `job-panel`, and an open `job-details-drawer` inside
    // it carries the same class. That only bites once a job exists,
    // which is why it showed up on the *second* engine of a CI run and
    // not the first.
    const disclosure = page
      .locator('config-section[heading="Libraries"]')
      .getByRole('button', { name: 'Libraries' })
      .first();

    if ((await disclosure.getAttribute('aria-expanded')) === 'false') {
      await disclosure.click();
    }

    // Renaming the decoy to its own name is a no-op the backend
    // accepts, so the rename has to happen on the other row.
    const row = page.locator('.library-row').filter({ hasText: seeded });

    // Through the overflow menu, not by clicking the name: the name's
    // own click bubbles to config-page's document handler, which closes
    // the editor it just opened.
    await expect(row).toBeVisible();
    await row.locator('.overflow-btn').click();
    await row.getByText('Rename', { exact: true }).click();

    const input = row.locator('.edit-input');

    await input.fill(DECOY);
    await input.press('Enter');

    // The message is the assertion: a name it can act on, and none of
    // the Go error's wrapping.
    const notice = app.getByTestId('notification').first();

    await expect(notice).toBeVisible();
    await expect(notice).toContainText(DECOY);
    await expect(notice).not.toContainText('could not rename library:');
  });
});

import { test, expect } from '../support/fixtures.js';

/**
 * The library views, against the generated fixture library
 * (`make testdata`): 31 tracks chosen to cover the cases the app has
 * code for — unicode and RTL titles, missing tags, a deliberately
 * absurd artist name for truncation, duplicates.
 */
test.describe('library views', () => {
  test('lands in the app, not the first-run wizard', async ({ app }) => {
    // A fresh YJ_HOME puts <first-run-wizard> over everything and it
    // intercepts every pointer event, so "the click did nothing" is the
    // symptom of an unseeded sandbox rather than a broken control.
    //
    // Asserted by clicking rather than by inspecting the wizard element:
    // the element is always in the DOM and merely renders nothing once a
    // library exists, so its presence proves nothing.  Playwright's own
    // actionability check fails a covered click with "intercepts pointer
    // events", which is exactly the condition worth catching.
    await expect(app.getByTestId('track-row').first()).toBeVisible();
    await app.getByTestId('nav-artists').click({ timeout: 5_000 });

    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'artists',
    );
  });

  test('renders every fixture track, unicode included', async ({
    app,
    testctl,
  }) => {
    const health = await testctl.health();
    const rows = app.getByTestId('track-row');

    await expect(rows).toHaveCount(health.counts.tracks);

    // Non-Latin scripts survive the tag reader, the database and the
    // renderer.  These titles exist in the fixtures for this reason.
    await expect(app.getByText('Привет мир')).toBeVisible();
    await expect(app.getByText('さくら')).toBeVisible();
    await expect(app.getByText('مرحبا بالعالم')).toBeVisible();
  });

  test('the sidebar navigates between primary views', async ({ app }) => {
    const main = app.getByTestId('main-content');

    for (const view of ['artists', 'genres', 'albums', 'playlists', 'tracks']) {
      await app.getByTestId(`nav-${view}`).click();
      await expect(main).toHaveAttribute('data-active-view', view);
      await expect(app.getByTestId(`nav-${view}`)).toHaveAttribute(
        'aria-current',
        'page',
      );
    }
  });

  test('the artists view shows the fixture artists', async ({ app }) => {
    await app.getByTestId('nav-artists').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'artists',
    );

    await expect(app.getByText('Aurora Fields').first()).toBeVisible();
    await expect(app.getByText('Pale Circuit').first()).toBeVisible();
  });
});

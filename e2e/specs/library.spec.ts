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
    await app.getByTestId('nav-tracks').click({ timeout: 5_000 });
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
    await app.getByTestId('nav-tracks').click();

    const health = await testctl.health();
    const rows = app.getByTestId('track-row');

    await expect(rows).toHaveCount(health.counts.tracks);

    // Non-Latin scripts survive the tag reader, the database and the
    // renderer.  These titles exist in the fixtures for this reason.
    await expect(app.getByText('Привет мир')).toBeVisible();
    await expect(app.getByText('さくら')).toBeVisible();
    await expect(app.getByText('مرحبا بالعالم')).toBeVisible();
  });

  test('opens on Home, which is the page built to answer what to play', async ({
    app,
  }) => {
    // H-8: the app opened on Tracks — an alphabetical list of
    // everything, the one entry point that is identical every time and
    // therefore gives the user nothing to start from. Home is listed
    // first in the nav and was never what anybody saw.
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'home',
    );

    // …and the sidebar agrees. It does not hear a `navigate` it did not
    // send, so this is a separate claim from the one above and was
    // briefly false.
    await expect(app.getByTestId('nav-home')).toHaveAttribute(
      'aria-current',
      'page',
    );
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

    // Scoped to the view: every primary view stays in the DOM, and
    // Home's shelves name the same artists — so an unscoped `.first()`
    // matches a card on a page that is `.view-hidden`, and asserting it
    // is visible fails for a reason that has nothing to do with Artists.
    const artists = app.locator('artists-view');

    await expect(artists.getByText('Aurora Fields').first()).toBeVisible();
    await expect(artists.getByText('Pale Circuit').first()).toBeVisible();
  });

  test('a library can be renamed from its own name', async ({ app }) => {
    // Found while closing Phase 3: clicking the name opened the rename
    // editor and closed it in the same click, because the click bubbled
    // to config-page's own document handler — which exists to close it.
    // The overflow menu's Rename always worked; it stops propagation.
    //
    // The editor is opened and abandoned, never committed: these specs
    // share one backend process, and a renamed library would fail the
    // ones after this on fixture content.
    await app.getByTestId('nav-settings').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'settings',
    );

    const page = app.locator('config-page');

    await expect
      .poll(() =>
        page.evaluate((el) => {
          const sections = [
            ...(el.shadowRoot?.querySelectorAll('config-section') ?? []),
          ];
          const libraries = sections.at(-1);
          const header =
            libraries?.shadowRoot?.querySelector<HTMLElement>('.header');

          header?.click();

          return !!el.shadowRoot?.querySelector('.library-name');
        }),
      )
      .toBe(true);

    const opened = await page.evaluate(async (el) => {
      el.shadowRoot
        ?.querySelector<HTMLElement>('.library-name')
        ?.click();

      // Lit renders on a microtask: a synchronous read here reports
      // "not editing" on a build where it works, which is how this bug
      // was first "reproduced" against a fix that already worked.
      await new Promise((r) => setTimeout(r, 100));

      return !!el.shadowRoot?.querySelector('.edit-input');
    });

    expect(opened).toBe(true);

    // Escape the editor without renaming, and leave the app on Tracks.
    await app.keyboard.press('Escape');
    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
  });
});

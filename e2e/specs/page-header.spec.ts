import { test, expect } from '../support/fixtures.js';

/**
 * H-19: Playlists, Downloads, Jobs, Settings and Home had a page
 * title; Artists, Genres, Albums and Tracks had none, none of the nine
 * showed a count, and two of them had a sort control that the other
 * seven had written out (or not) for themselves.
 *
 * This is the spec that stops the tenth view inventing a tenth
 * arrangement: every primary view is asked for its heading, and the
 * ones that show a list are asked for a count as well.
 */

/** view id -> [heading, has a count] */
const VIEWS: [string, string, boolean][] = [
  ['home', 'Home', false],
  ['playlists', 'Playlists', true],
  ['artists', 'Artists', true],
  ['genres', 'Genres', true],
  ['albums', 'Albums', true],
  ['tracks', 'Tracks', true],
  ['explore', 'Explore', false],
  ['downloads', 'Downloads', false],
  ['jobs', 'Background jobs', false],
];

/** The header lives in the view's shadow root, inside its own. */
const header = (page: import('@playwright/test').Page, view: string) =>
  page.evaluate((v) => {
    const el = document.querySelector(`[data-testid="main-content"] ${v}`);
    const ph = el?.shadowRoot?.querySelector('page-header');
    const root = ph?.shadowRoot;

    return {
      heading:
        root
          ?.querySelector('[data-testid="page-heading"]')
          ?.textContent?.trim() ?? null,
      count:
        root
          ?.querySelector('[data-testid="page-count"]')
          ?.textContent?.trim() ?? null,
    };
  }, view);

/** Which element a view id renders as. */
const TAGS: Record<string, string> = {
  home: 'home-view',
  playlists: 'playlist-view',
  artists: 'artists-view',
  genres: 'genres-view',
  albums: 'cover-grid',
  tracks: 'track-list',
  explore: 'explore-view',
  downloads: 'downloads-view',
  jobs: 'jobs-view',
};

test.describe('every primary view says what it is', () => {
  test('each one has the shared header, with a heading', async ({ app }) => {
    for (const [view, heading, hasCount] of VIEWS) {
      await app.getByTestId(`nav-${view}`).click();
      await expect(app.getByTestId('main-content')).toHaveAttribute(
        'data-active-view',
        view,
      );

      // Poll: a view is a chunk, so the first visit awaits an import.
      await expect
        .poll(async () => (await header(app, TAGS[view]!)).heading)
        .toBe(heading);

      if (hasCount) {
        // Polled, not read: the count is deliberately absent until the
        // view has an answer — "0 albums" while loading is a lie that
        // corrects itself, which is worse than saying nothing.
        await expect
          .poll(async () => (await header(app, TAGS[view]!)).count)
          .toMatch(/^[\d,]+ \w+$/);
      } else {
        expect((await header(app, TAGS[view]!)).count).toBeNull();
      }
    }

    // Leave the app where the next spec expects it.
    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
  });

  test('the count is of what is on screen, not of the library', async ({
    app,
  }) => {
    await app.getByTestId('nav-artists').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'artists',
    );

    // Polled, and cleared first: the term survives navigation (by
    // decision), and the count is absent until the view has an answer
    // — so reading either one straight after a click can capture null
    // and make every assertion after it meaningless.
    await app.getByTestId('search-input').fill('');
    await expect
      .poll(async () => (await header(app, 'artists-view')).count)
      .toMatch(/^[\d,]+ \w+$/);

    const all = (await header(app, 'artists-view')).count;

    // The header search is view-scoped by decision (Decisions, 2), and
    // the header is where that scope is finally admitted to.
    await app.getByTestId('search-input').fill('aurora');

    await expect
      .poll(async () => (await header(app, 'artists-view')).count)
      .not.toBe(all);

    const scope = await app.evaluate(() =>
      document
        .querySelector('[data-testid="main-content"] artists-view')
        ?.shadowRoot?.querySelector('page-header')
        ?.shadowRoot?.querySelector('[data-testid="page-search-scope"]')
        ?.textContent?.trim(),
    );

    expect(scope).toContain('artists matching');

    await app.getByTestId('search-input').fill('');
    await expect
      .poll(async () => (await header(app, 'artists-view')).count)
      .toBe(all);

    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
  });
});

import { test, expect } from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * Plan 007 phase 6 (`H-23`): Explore starts the conversation.
 *
 * It was a search box over a 1.1 M-row local catalog and a sentence
 * telling the user to type into it — the only view in the app that
 * answers "what exists" rather than "what have I got", and it would not
 * begin.
 *
 * Two worlds, and both are asserted rather than one assertion loose
 * enough to pass in either. A developer machine downloads the real
 * catalog artifact on launch and gets a million rows; **CI points
 * `YJ_CORE_INDEX_URL` at a dead address**, so the app there has an
 * empty index — which is also every user's first run.
 *
 * The empty world is the interesting one and it is where the specs used
 * to *skip*, which is no signal at all. So this suite stages its own
 * catalog through `/__test/sql` when there is none, the way the perf
 * harness builds the playlists the bulk seed does not have. That works
 * only because the page asks the database whether a catalog exists
 * rather than consulting a flag set at startup — the first two versions
 * of that gate were cached, and rows staged afterwards were invisible
 * to both.
 */
test.describe('Explore before anyone has typed', () => {
  test.beforeEach(async ({ app }) => {
    // Idempotent, so running it per test costs one count query when a
    // catalog is already there — which is every developer machine.
    await stageCatalogIfEmpty(app);

    await app.getByTestId('nav-explore').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'explore',
    );

    // The view being on screen is not the shelves being on it: they
    // are fetched when it activates. Reading them a moment too early
    // gives an empty list, which is also what a broken page gives.
    await expect.poll(() => shelfHeadings(app)).not.toHaveLength(0);
  });

  test('says something without being typed into', async ({ app }) => {
    const view = app.locator('explore-view');

    // Shelves, on `backend/home`'s terms: a reason per row, and the
    // sentence that says so beside it. That there are any at all is
    // asserted in beforeEach.
    const reasons = await app.evaluate(
      () =>
        [
          ...(document
            .querySelector('explore-view')
            ?.shadowRoot?.querySelectorAll('.section-reason') ?? []),
        ].length,
    );

    expect(reasons).toBeGreaterThan(0);

    // And the page it replaced is gone.
    await expect(view).not.toContainText('Search to discover');
  });

  test('a shelf card opens the page it is about', async ({ app }) => {
    // The cards route through what already existed — no new detail
    // page was invented for this.
    await clickCard(app, '.album-card');
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'explore-album-details',
    );

    await app.getByTestId('nav-explore').click();

    await clickCard(app, '.artist-card');
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'explore-artist-details',
    );
  });

  test('clearing a search comes back to the shelves', async ({ app }) => {
    const before = await shelfHeadings(app);

    // What the search finds is not this spec's business and differs
    // between the two worlds — a real catalog answers `Artists /
    // Albums / Tracks`, a staged one answers nothing at all. What has
    // to be true in both is that the shelves get out of the way…
    await type(app, 'nirvana');
    await expect.poll(() => shelfHeadings(app)).not.toEqual(before);

    // …and that they are what the page comes back to when the query is
    // cleared, rather than a mode you have to leave.
    await type(app, '');
    await expect.poll(() => shelfHeadings(app)).toEqual(before);
  });

  test('two shelves are not the same shelf twice', async ({ app }) => {
    // Ordered by raw listen count, the catalog's top albums are one act
    // and its members and the artists row underneath was the same
    // people — a duplication no id comparison can see, because the two
    // rows hold different entity types.
    const names = await app.evaluate(() => {
      const root = document.querySelector('explore-view')?.shadowRoot;
      const text = (sel: string) =>
        [...(root?.querySelectorAll(sel) ?? [])].map((e) =>
          (e.textContent ?? '').trim(),
        );

      return {
        albumArtists: text('.album-card .album-artist'),
        artists: text('.artist-card .artist-name'),
      };
    });

    // One album per artist, and no artist in both rows.
    expect(new Set(names.albumArtists).size).toBe(names.albumArtists.length);

    for (const artist of names.artists) {
      expect(names.albumArtists).not.toContain(artist);
    }
  });
});

/**
 * Give the app a catalog if it has none, so the empty-index environment
 * still exercises the shelves rather than skipping them.
 *
 * Deliberately shaped: two artists with albums (one of them with
 * three), and a third with none. The three albums are what "one album
 * per artist" can be false about; the third artist is what the artists
 * shelf is made of, because the other two are spent by the albums row
 * above it and correctly skipped — the first version of this fixture
 * had only two, and the artists shelf was rightly omitted, which read
 * as a broken page.
 */
async function stageCatalogIfEmpty(app: Page): Promise<void> {
  if ((await catalogRows(app)) > 0) return;

  const rows = [
    ['artist', 'e2e-ar-a', 'Staged Alpha', 'Staged Alpha', 'e2e-ar-a', 9000],
    ['artist', 'e2e-ar-b', 'Staged Beta', 'Staged Beta', 'e2e-ar-b', 500],
    ['artist', 'e2e-ar-c', 'Staged Gamma', 'Staged Gamma', 'e2e-ar-c', 300],
    ['release_group', 'e2e-rg-a1', 'Alpha One', 'Staged Alpha', 'e2e-ar-a', 8000],
    ['release_group', 'e2e-rg-a2', 'Alpha Two', 'Staged Alpha', 'e2e-ar-a', 7000],
    ['release_group', 'e2e-rg-a3', 'Alpha Three', 'Staged Alpha', 'e2e-ar-a', 6000],
    ['release_group', 'e2e-rg-b1', 'Beta One', 'Staged Beta', 'e2e-ar-b', 400],
  ];

  for (const row of rows) {
    const result = await app.evaluate(async (args) => {
      const res = await fetch('/__test/sql', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          sql: `INSERT OR IGNORE INTO explore_index
                  (entity_type, mbid, title, artist_name, artist_mbid,
                   popularity, listener_count, primary_type)
                VALUES (?, ?, ?, ?, ?, ?, ?, 'Album')`,
          args: [...args, 10],
        }),
      });

      return { status: res.status, body: await res.text() };
    }, row);

    // The first version of this passed six values to seven
    // placeholders and never read the response, so every insert failed
    // and the staging step looked exactly like a staging step. A setup
    // whose failure is not checked is not setup.
    expect(result.status, `staging failed: ${result.body}`).toBe(200);
  }

  expect(await catalogRows(app)).toBeGreaterThan(0);
}

/** How many catalog rows this environment has. CI has none. */
async function catalogRows(app: Page): Promise<number> {
  const health = await app.evaluate(async () => {
    const res = await fetch('/__test/health');

    return (await res.json()) as { counts?: { exploreIndex?: number } };
  });

  return health.counts?.exploreIndex ?? 0;
}

async function shelfHeadings(app: Page): Promise<string[]> {
  return app.evaluate(() =>
    [
      ...(document
        .querySelector('explore-view')
        ?.shadowRoot?.querySelectorAll('.section-header') ?? []),
    ].map((h) => (h.textContent ?? '').trim()),
  );
}

async function clickCard(app: Page, selector: string): Promise<void> {
  await expect
    .poll(() =>
      app.evaluate(
        (sel) =>
          document
            .querySelector('explore-view')
            ?.shadowRoot?.querySelectorAll(sel).length ?? 0,
        selector,
      ),
    )
    .toBeGreaterThan(0);

  await app.evaluate((sel) => {
    document
      .querySelector('explore-view')
      ?.shadowRoot?.querySelector<HTMLElement>(sel)
      ?.click();
  }, selector);
}

/** Type into Explore's own search box, and wait out its debounce. */
async function type(app: Page, query: string): Promise<void> {
  await app.evaluate((q) => {
    const input = document
      .querySelector('explore-view')
      ?.shadowRoot?.querySelector<HTMLInputElement>('.search-container input');

    if (!input) return;

    input.value = q;
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }, query);
}

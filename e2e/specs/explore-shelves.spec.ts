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
    await stageCatalog(app);

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
 * Give the app the catalog these shelves are written against.
 *
 * Deliberately shaped: two artists with albums (one of them with
 * three), and a third with none. The three albums are what "one album
 * per artist" can be false about; the third artist is what the artists
 * shelf is made of, because the other two are spent by the albums row
 * above it and correctly skipped — the first version of this fixture
 * had only two, and the artists shelf was rightly omitted, which read
 * as a broken page.
 *
 * **Unconditional, and it used to ask whether the catalog was empty.**
 * "Any rows at all" is the wrong question: the backend is one shared
 * process with one database, so a *single* row left by another spec
 * file — `requested-badge` stages one album — satisfies that gate and
 * this suite then draws a shelf page with no artist card on it and
 * times out looking for one. It survives a suite run, so it is the
 * second local `make e2e` that fails and the first that passes, which
 * is the least useful order. CI never sees it: every run there is a
 * fresh YJ_HOME.
 *
 * The inserts are `INSERT OR IGNORE` keyed on the MBID, so running
 * this per test is idempotent, and adding seven low-popularity rows to
 * a developer machine's real million-row catalog changes nothing the
 * shelves show.
 */
async function stageCatalog(app: Page): Promise<void> {

  // The catalog stores an MBID as its 16 raw bytes and an entity type
  // as a small integer, so a staged row has to be spelled the way the
  // app spells one: a real UUID, converted at the boundary, and a code
  // rather than the word.  `'e2e-ar-a'` is 8 characters and fails
  // `CHECK(length(mbid) = 16)` -- which `INSERT OR IGNORE` then
  // swallows, so the staging step looked exactly like a staging step
  // and the page had nothing to draw.  That is the same fault this
  // helper's own comment below describes, one layer down.
  const ARTIST = 1;
  const RELEASE_GROUP = 2;

  const rows = [
    [ARTIST, uuid('ar-a'), 'Staged Alpha', 'Staged Alpha', uuid('ar-a'), 9000],
    [ARTIST, uuid('ar-b'), 'Staged Beta', 'Staged Beta', uuid('ar-b'), 500],
    [ARTIST, uuid('ar-c'), 'Staged Gamma', 'Staged Gamma', uuid('ar-c'), 300],
    [RELEASE_GROUP, uuid('rg-a1'), 'Alpha One', 'Staged Alpha', uuid('ar-a'), 8000],
    [RELEASE_GROUP, uuid('rg-a2'), 'Alpha Two', 'Staged Alpha', uuid('ar-a'), 7000],
    [RELEASE_GROUP, uuid('rg-a3'), 'Alpha Three', 'Staged Alpha', uuid('ar-a'), 6000],
    [RELEASE_GROUP, uuid('rg-b1'), 'Beta One', 'Staged Beta', uuid('ar-b'), 400],
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
                VALUES (?, unhex(replace(?, '-', '')), ?, ?,
                        unhex(replace(?, '-', '')), ?, ?, 'Album')`,
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

    // …and `OR IGNORE` means a 200 is not a write. A CHECK the row
    // violates is *ignored*, not reported, so the check below is the
    // only thing that can tell staging from silence.
    //
    // It is `0 or 1`, not `1`, because this helper is now
    // unconditional: the second call of a run legitimately writes
    // nothing. What must hold either way is that the rows are *there*,
    // which is what the assertion after the loop says — a stronger
    // statement than "this insert wrote something", and the one that
    // actually protects the fixture.
    expect(
      (JSON.parse(result.body) as { rowsAffected?: number }).rowsAffected,
      `staging error: ${result.body}`,
    ).toBeLessThanOrEqual(1);
  }

  // Every staged row is present, whoever put it there. An MBID that
  // fails `CHECK(length(mbid) = 16)` is silently dropped by OR IGNORE,
  // and this is where that shows up.
  expect(await stagedRowCount(app), 'the staged catalog is incomplete')
    .toBe(rows.length);
}

/** How many of the staged fixture rows are in the catalog. */
async function stagedRowCount(app: Page): Promise<number> {
  const result = await app.evaluate(async () => {
    const res = await fetch('/__test/sql', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        sql: `SELECT COUNT(*) AS n FROM explore_index
              WHERE artist_name IN ('Staged Alpha', 'Staged Beta',
                                    'Staged Gamma')`,
      }),
    });

    return { status: res.status, body: await res.text() };
  });

  expect(result.status, `count failed: ${result.body}`).toBe(200);

  const parsed = JSON.parse(result.body) as {
    rows?: { n?: number }[];
  };

  return parsed.rows?.[0]?.n ?? 0;
}

/**
 * A stable, valid MBID from a short label.
 *
 * The column is `CHECK(length(mbid) = 16)` after `unhex`, so a fixture
 * id has to be a real UUID rather than a readable string -- the same
 * trade the Go fixtures make with `testMBID()`, and for the same
 * reason: a readable id that cannot be stored is not readable, it is
 * absent.
 */
function uuid(label: string): string {
  const hex = [...label]
    .map((c) => c.charCodeAt(0).toString(16).padStart(2, '0'))
    .join('')
    .padEnd(32, '0')
    .slice(0, 32);

  return [
    hex.slice(0, 8),
    hex.slice(8, 12),
    hex.slice(12, 16),
    hex.slice(16, 20),
    hex.slice(20),
  ].join('-');
}

/**
 * Whether this environment has a catalog at all. CI has none.
 *
 * `exploreIndex` is deliberately 0-or-1 rather than a row count: a real
 * catalog is ~1.1M rows, and a cold `COUNT(*)` over it took 65 seconds
 * on the first call after a seed was extracted -- which timed out
 * whichever spec ran first and looked like flake.
 */
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

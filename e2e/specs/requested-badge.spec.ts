import { test, expect, callBinding } from '../support/fixtures.js';

/**
 * The badge says an album is requested, and the button beside it agrees.
 *
 * `library-status-indicator` has had three states since it was written
 * and produced two: every one of the eight call sites was a two-way
 * ternary, so an album already on the request list showed a plus and
 * said "is not in your library" — on the same page, forty pixels from a
 * filled button reading "Wanted".
 *
 * This spec exists at this tier rather than only in the component one
 * because of what it drags in with it: reaching the requested state is
 * also the only way to render the requested *icon*, and the requested
 * icon was `bookmark-check`, a Font Awesome **Pro** name that has never
 * been bundled. `offline-icons.spec.ts` asserts `__yjIconMisses` is
 * empty and passed anyway, because no spec had ever put the app in this
 * state. A name computed from state is only checkable from the state.
 *
 * It gives back what it spends: the request is removed in `afterAll`,
 * and the staged catalog rows are `INSERT OR IGNORE`d so a second run
 * against the same backend is a no-op.
 */

/** A release group that exists whether or not this environment has a
 *  catalog — CI's `YJ_CORE_INDEX_URL` is deliberately dead.
 *
 *  It is a real UUID because the catalog stores an MBID as its 16 raw
 *  bytes under `CHECK(length(mbid) = 16)`. A readable id fails that
 *  check, `INSERT OR IGNORE` swallows the failure, and the staging step
 *  below reports 200 having written nothing. */
const MBID = 'e2ebad9e-0001-4000-8000-000000000001';

/** The staged album's artist, for the same reason. */
const ARTIST_MBID = 'e2ebad9e-0002-4000-8000-000000000002';
const TITLE = 'Requested Album';
const ARTIST = 'Badge Artist';

let requestId = 0;

test.describe('the requested badge', () => {
  test.beforeAll(async ({ browser, baseURL }) => {
    const page = await browser.newPage();

    await page.goto(baseURL!);

    const res = await page.evaluate(
      async (row) => {
        const r = await fetch('/__test/sql', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            sql: `INSERT OR IGNORE INTO explore_index
                    (entity_type, mbid, title, artist_name, artist_mbid,
                     popularity, listener_count, primary_type)
                  VALUES (2 /* release_group */,
                          unhex(replace(?, '-', '')), ?, ?,
                          unhex(replace(?, '-', '')),
                          10, 10, 'Album')`,
            args: [row.mbid, row.title, row.artist, row.artistMbid],
          }),
        });

        return { status: r.status, body: await r.text() };
      },
      { mbid: MBID, title: TITLE, artist: ARTIST, artistMbid: ARTIST_MBID },
    );

    // A setup step whose failure is not checked is not setup — and with
    // `OR IGNORE` a 200 is not a write either: a CHECK the row violates
    // is ignored rather than reported. `rowsAffected` is 0 on a second
    // run against the same backend, so what is asserted is that the
    // statement did not error.
    expect(res.status, `staging failed: ${res.body}`).toBe(200);
    expect(
      (JSON.parse(res.body) as { error?: string }).error,
      `staging failed: ${res.body}`,
    ).toBeUndefined();

    // A previous run that died between adding and removing would leave
    // this album requested, and the first assertion here is that it is
    // not — so start from a known state rather than from the last run's
    // luck. The 90 specs share one backend in file order.
    await clearRequest(page);

    await page.close();
  });

  test.afterAll(async ({ browser, baseURL }) => {
    const page = await browser.newPage();

    await page.goto(baseURL!);
    await clearRequest(page);
    await page.close();
    requestId = 0;
  });

  test('a requested album is queued, not absent', async ({ app }) => {
    const libraryId = 1;

    // Before: the badge for an unrequested album.
    await app.getByTestId('nav-explore').click();
    await search(app, TITLE);

    expect(await badgeStatus(app, TITLE)).toBe('not-in-library');

    requestId = await addRequest(app, libraryId);

    expect(requestId).toBeGreaterThan(0);

    // The badge is told by the store, which is told by the backend —
    // no navigation, no reload.
    await expect
      .poll(() => badgeStatus(app, TITLE), { timeout: 10_000 })
      .toBe('queued');

    // And it says so where it counts. The name is the whole point —
    // the plus used to be accompanied by "is not in your library" —
    // and since phase 3 it is a control, so the name is the action it
    // performs rather than the state it is in.
    expect(await badgeLabel(app, TITLE)).toBe(
      `Cancel the request for album "${TITLE}"`,
    );
  });

  test('clicking the badge wants the album and does not open it', async ({
    app,
  }) => {
    // Only this tier can say this. The badge sits inside a card whose
    // own click navigates, so the assertion is that a real gesture on
    // the badge files a request *and* leaves the page where it was —
    // and a synthetic MouseEvent is not evidence of either.
    await clearRequest(app);
    await app.getByTestId('nav-explore').click();
    await search(app, TITLE);

    // A locator rather than measured coordinates, and the difference
    // is not style. The first version read a bounding box the moment
    // the search settled and clicked it — but cover art is still
    // arriving then, and a card that grows moves the badge, so the
    // click landed on the card and opened the album. A locator
    // re-resolves and waits for the element to stop moving.
    const button = app
      .locator(`library-status-indicator[label="${TITLE}"] button`)
      .first();

    // A badge that is not a button makes every assertion below vacuous.
    await expect(button, 'the badge is not a button').toBeVisible();

    await button.click();

    await expect
      .poll(() => badgeStatus(app, TITLE), { timeout: 10_000 })
      .toBe('queued');

    expect(
      await app.evaluate(() => !!document.querySelector('explore-album-details')),
      'the click reached the card underneath',
    ).toBe(false);

    requestId = 1; // so afterAll cleans up regardless of order
  });

  test('the requested state renders a real icon', async ({ app }) => {
    // `bookmark-check` is Pro, so the button rendered the fallback
    // glyph for as long as anything had been requested. The sweep in
    // offline-icons.spec.ts could not see it: it never reached here.
    // Runs after the test above in file order, which is where the
    // request comes from — but a spec that only passes as part of a
    // sequence is a spec that lies when it is run alone.
    if (!requestId) requestId = await addRequest(app, 1);

    await app.getByTestId('nav-explore').click();
    await search(app, TITLE);
    await openFirstAlbum(app);

    await expect
      .poll(
        () =>
          app.evaluate(() => {
            const ds = document.querySelector('explore-album-details')
              ?.shadowRoot;
            const btn = [...(ds?.querySelectorAll('wa-button') ?? [])].find(
              (b) => /Wanted/.test(b.textContent ?? ''),
            );

            return btn?.querySelector('wa-icon')?.getAttribute('name') ?? '';
          }),
        { timeout: 10_000 },
      )
      .not.toBe('');

    const misses = await app.evaluate(
      () =>
        (window as unknown as { __yjIconMisses?: string[] }).__yjIconMisses ??
        [],
    );

    expect(misses, 'an icon name that is not bundled').toEqual([]);
  });
});

function addRequest(
  app: import('@playwright/test').Page,
  libraryId: number,
): Promise<number> {
  return callBinding<number>(app, 'download.Service.AddRequest', [
    {
      mbid: MBID,
      entity: 'release-group',
      libraryId,
      artist: ARTIST,
      title: TITLE,
      scope: 'future',
      secondary: false,
    },
  ]);
}

/**
 * Drop any request for this album, over the runtime endpoint.
 *
 * Deliberately not `callBinding`: that goes through
 * `window.__yjEvents`, which only exists on a page the `app` fixture
 * created — a bare `browser.newPage()` has no init script, so the
 * bridge is undefined and the cleanup throws where nobody is looking.
 * The first version of this did exactly that and left the request
 * behind, which failed the *next* run of this same spec.
 *
 * v2's answer was `window.go`, which every page had.  v3 has no such
 * global, and the version of this that kept reading it did not throw —
 * it returned early on `if (!svc)`, which is the same silent cleanup
 * with a different cause.  A POST to `/wails/runtime` needs neither:
 * it is the same request the bundle makes, and any page can make it.
 */
async function clearRequest(page: import('@playwright/test').Page) {
  await page.evaluate(async (mbid) => {
    const call = async (method: string, args: unknown[]) => {
      const res = await fetch('/wails/runtime', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          object: 0,
          method: 0,
          args: {
            'call-id': `clear-${method}`,
            methodName: `yellowjacket/backend/${method}`,
            args,
          },
        }),
      });

      if (!res.ok) throw new Error(`${method}: ${await res.text()}`);

      return res.json();
    };

    const rows: { id: number; mbid: string }[] =
      (await call('download.Service.ListRequests', [])) ?? [];

    for (const row of rows) {
      if (row.mbid?.toLowerCase() === mbid.toLowerCase()) {
        await call('download.Service.RemoveRequest', [row.id]);
      }
    }
  }, MBID);
}

/** Type into Explore's own search box and wait for it to settle. */
async function search(app: import('@playwright/test').Page, term: string) {
  // A view is a chunk, and `document.createElement` on a tag that has
  // not loaded yet yields an inert element rather than throwing — so
  // the box being missing reads exactly like a selector bug.
  await expect
    .poll(
      () =>
        app.evaluate(
          () =>
            !!document
              .querySelector('explore-view')
              ?.shadowRoot?.querySelector('input'),
        ),
      { timeout: 15_000 },
    )
    .toBe(true);

  await app.evaluate((t) => {
    const sr = document.querySelector('explore-view')?.shadowRoot;
    const input = sr?.querySelector('input');

    if (!input) throw new Error('explore search box not found');

    input.value = t;
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }, term);

  // 60 s, and it is not paranoia. A freshly launched app spends its
  // first ~40 s merging the core catalog artifact, and until that lands
  // Explore's search returns nothing at all — including for rows staged
  // directly into `explore_index` a moment ago. A shorter budget fails
  // here for a reason that has nothing to do with what is being tested,
  // which is how this spec first "failed" on a build that was fine.
  await expect
    .poll(() => cardTitles(app), { timeout: 60_000 })
    .toContain(term);
}

function cardTitles(app: import('@playwright/test').Page): Promise<string[]> {
  return app.evaluate(() =>
    [
      ...(document
        .querySelector('explore-view')
        ?.shadowRoot?.querySelectorAll('library-status-indicator') ?? []),
    ].map((b) => b.getAttribute('label') ?? ''),
  );
}

function badgeStatus(
  app: import('@playwright/test').Page,
  label: string,
): Promise<string> {
  return app.evaluate((wanted) => {
    const badge = [
      ...(document
        .querySelector('explore-view')
        ?.shadowRoot?.querySelectorAll('library-status-indicator') ?? []),
    ].find((b) => b.getAttribute('label') === wanted);

    return badge?.getAttribute('status') ?? '(no badge)';
  }, label);
}

function badgeLabel(
  app: import('@playwright/test').Page,
  label: string,
): Promise<string> {
  return app.evaluate((wanted) => {
    const badge = [
      ...(document
        .querySelector('explore-view')
        ?.shadowRoot?.querySelectorAll('library-status-indicator') ?? []),
    ].find((b) => b.getAttribute('label') === wanted);

    return (
      badge?.shadowRoot?.querySelector('.badge')?.getAttribute('aria-label') ??
      '(no badge)'
    );
  }, label);
}

async function openFirstAlbum(app: import('@playwright/test').Page) {
  await app.evaluate((wanted) => {
    const badge = [
      ...(document
        .querySelector('explore-view')
        ?.shadowRoot?.querySelectorAll('library-status-indicator') ?? []),
    ].find((b) => b.getAttribute('label') === wanted);

    (badge?.closest('.album-card') as HTMLElement | null)?.click();
  }, TITLE);

  await expect
    .poll(
      () => app.evaluate(() => !!document.querySelector('explore-album-details')),
      { timeout: 15_000 },
    )
    .toBe(true);
}

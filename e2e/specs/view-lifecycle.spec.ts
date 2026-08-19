import {
  test,
  expect,
  eventNames,
  resetEvents,
  waitForEvent,
  navigateTo,
} from '../support/fixtures.js';

/**
 * Primary views are cached, not unmounted, so that `scrollTop` survives
 * navigation (`frontend/index.ts`).  The cost of that decision is that
 * `disconnectedCallback` never fires, so anything a view registered on
 * `document` keeps running from every other page.
 *
 * H-1 in `.planning/audits/2026-08-11-ui/hands-on.md` is the worst case:
 * `autotag-view`'s document keydown handler binds `s` to "skip this
 * album", so pressing `s` on Settings — where `s` is the global shuffle
 * shortcut — silently removed albums from the autotag queue.  The same
 * handler binds `a` to Apply, which rewrites tags on disk.
 *
 * The invariant this asserts is the whole of Phase 1: a view that is not
 * on screen is not listening.
 */
test.describe('view lifecycle', () => {
  /** The autotag sidebar's "Pending (N)" header.
   *
   *  Read with `textContent`, not Playwright's text matchers: once the
   *  view is off-screen it is `.view-hidden`, so every visibility-aware
   *  API reports it as empty — which would make this spec pass for the
   *  wrong reason. */
  const pendingCount = (page: import('@playwright/test').Page) =>
    page.evaluate(() => {
      const view = document.querySelector('autotag-view');
      const header = view?.shadowRoot?.querySelector('.folders-header');

      return header?.textContent?.trim() ?? '';
    });

  test('a keypress on Settings does not reach the Autotag queue', async ({
    app,
  }) => {
    // By event, not by nav item: Autotag is hidden by default (#25)
    // and a hidden view is still reachable.
    await navigateTo(app, 'autotag');
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'autotag',
    );
    await expect
      .poll(() => pendingCount(app))
      .toMatch(/^Pending \(\d+\)$/);

    const before = await pendingCount(app);

    await app.getByTestId('nav-settings').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'settings',
    );

    await resetEvents(app);
    await app.keyboard.press('s');

    // The global binding is shuffle, and it must be the *only* thing
    // that happened.
    await expect
      .poll(() => eventNames(app).then((n) => n.QueueModeChanged ?? 0))
      .toBe(1);

    expect(await pendingCount(app)).toBe(before);

    // Toggle it back. Shuffle is backend state that outlives the page,
    // so leaving it on fails `playback.spec`'s shuffle assertion on the
    // *next* run against the same app — the specs share one process,
    // and this one was quietly spending state it never returned.
    await app.keyboard.press('s');
    await expect
      .poll(() => eventNames(app).then((n) => n.QueueModeChanged ?? 0))
      .toBe(2);
  });

  test('on Autotag, the same key skips and does not also shuffle', async ({
    app,
  }) => {
    // The other half of the same bug (H-2): two document keydown handlers
    // with no arbitration meant `s` on this page skipped the album *and*
    // toggled shuffle.  As a panel binding it can only mean one thing.
    // By event, not by nav item: Autotag is hidden by default (#25)
    // and a hidden view is still reachable.
    await navigateTo(app, 'autotag');
    await expect
      .poll(() => pendingCount(app))
      .toMatch(/^Pending \(\d+\)$/);

    const before = Number(/\((\d+)\)/.exec(await pendingCount(app))![1]);

    await resetEvents(app);
    await app.keyboard.press('s');

    await expect.poll(() => pendingCount(app)).toBe(`Pending (${before - 1})`);
    expect((await eventNames(app)).QueueModeChanged ?? 0).toBe(0);
  });
});

/**
 * H-5: tabbing through the app produced fourteen stops and not one of
 * them was navigation or content. These are the two that matter most —
 * getting *into* the app, and doing the app's primary action once there.
 */
test.describe('keyboard reach', () => {
  /** The deepest focused element, resolved through shadow roots the way
   *  the shortcut service does — `document.activeElement` stops at the
   *  host and would report every stop as the same element. */
  const focused = (page: import('@playwright/test').Page) =>
    page.evaluate(() => {
      let el: Element | null = document.activeElement;

      while (el?.shadowRoot?.activeElement) el = el.shadowRoot.activeElement;

      return {
        tag: el?.tagName ?? '',
        testid: (el as HTMLElement | null)?.dataset?.['testid'] ?? '',
        role: el?.getAttribute('role') ?? '',
      };
    });

  test('tabs out of the header straight into the sidebar', async ({
    app,
  }) => {
    // On Tracks, because the header search box is disabled on Home —
    // it keeps its slot everywhere and says why, but a disabled input
    // cannot hold focus, and this spec is about what follows it.
    await app.getByTestId('nav-tracks').click();

    // Started from the search box rather than from the top of the page:
    // the header's leading controls come and go (the index-status button
    // is only there while the index builds), so counting stops from the
    // start makes the assertion about the header, not about the nav.
    await app.evaluate(() => {
      document
        .querySelector('search-bar')
        ?.shadowRoot?.querySelector('input')
        ?.focus();
    });

    await app.keyboard.press('Tab');

    expect(await focused(app)).toMatchObject({ testid: 'nav-home' });
  });

  test('a track row can be reached and played without a mouse', async ({
    app,
  }) => {
    await app.getByTestId('nav-tracks').click();

    // Tab until the list's single stop — the roving tabindex means there
    // is exactly one, however many thousand rows there are.
    for (let i = 0; i < 25; i += 1) {
      await app.keyboard.press('Tab');

      if ((await focused(app)).role === 'row') break;
    }

    expect(await focused(app)).toMatchObject({ role: 'row' });

    await app.keyboard.press('ArrowDown');
    await resetEvents(app);
    await app.keyboard.press('Enter');

    await waitForEvent(app, 'TrackChanged');
  });
});

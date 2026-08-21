import { test, expect } from '../support/fixtures.js';

/**
 * #57. Below 600px the top bar is not in the layout, and search is a
 * button that opens a modal on the pages where searching means
 * anything.
 *
 * **This is the tier that can answer it, with one honest exception.**
 * The shell's breakpoints are media queries, which the component tier
 * cannot set — so whether the bar is a grid row, and whether a header
 * grows a search button, is a question for a real viewport. What this
 * tier *cannot* answer is the reason the surface is a `wa-dialog`:
 * #60 read out of the Web Awesome source that `wa-popup` falls back to
 * `position: fixed` where there is no Popover API (Chrome 113, the
 * reference device) and that `.main-panel`'s `contain: paint` clips a
 * fixed descendant. Chromium and WebKit here both have the Popover API,
 * so a popup is top-layered and correct, and **an assertion that the
 * modal is not clipped would pass on the broken build.** The mechanism
 * is asserted in `frontend/test/components/search-dialog.test.ts`
 * instead, where "is there a native <dialog>" is a question a browser
 * can answer without lying.
 *
 * **And it is measured per element.** `layout-overflow.spec.ts` asks
 * whether the *shell* needs sideways scrolling and was green throughout
 * the defect it is named for; the win this issue is for is vertical and
 * belongs to one element, so it is that element's box that is read.
 */
type Page = import('@playwright/test').Page;

/** The reference device's own viewport, and a common small phone. */
const DEVICE = { width: 424, height: 439 };
const PHONE = { width: 390, height: 780 };

/**
 * Where the top bar is, and how much of the screen it costs.
 *
 * `contentTop` is measured against the *jobs band* rather than against
 * the window, because that band is a real grid row whenever work is in
 * flight (#62) and the app under these specs is long-lived — a job
 * staged by another file is still in the store. Measuring against zero
 * makes this assertion say "and no background job is running", which is
 * not what it is for and is not something it can arrange.
 */
const barBox = (page: Page) =>
  page.evaluate(() => {
    const bar = document.querySelector<HTMLElement>('header.top-bar')!;
    const main = document.querySelector<HTMLElement>('.main-panel')!;
    const band = document.querySelector<HTMLElement>('job-band');
    const cs = getComputedStyle(bar);

    return {
      position: cs.position,
      height: Math.round(bar.getBoundingClientRect().height),
      /** Where the content starts, and where the row above it ends. */
      contentTop: Math.round(main.getBoundingClientRect().top),
      aboveBottom: Math.round(band?.getBoundingClientRect().bottom ?? 0),
    };
  });

test.describe('the phone has no top bar', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DEVICE);
  });

  test.afterEach(async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });
  });

  /**
   * The vertical win, measured rather than asserted by the absence of
   * an element: `display: none` on the header would satisfy "the bar is
   * hidden" while leaving a 3.25em grid row exactly where it was.
   */
  test('gives the row back to the content', async ({ app }) => {
    const box = await barBox(app);

    // Out of flow, so it takes no row — and 1px rather than 0, because
    // it still carries the document's h1.
    expect(box.position).toBe('absolute');
    expect(box.height).toBeLessThanOrEqual(1);

    // The content starts where the row above it ends, and there is no
    // row above it but the jobs band. On `main` at the time of writing
    // the content started 52px down from that point.
    expect(box.contentTop).toBe(box.aboveBottom);
  });

  /**
   * The wordmark yields its width and not its existence, which is the
   * rule `top-bar-fit.ts` already lives by one band up: with the bar
   * gone, `display: none` would take this document from one top-level
   * heading to none on every page whose own header has no h1 —
   * Settings has no `page-header` at all.
   */
  test('still has a top-level heading', async ({ app }) => {
    await expect(
      app.getByRole('heading', { name: 'YellowJacket', level: 1 }),
    ).toHaveCount(1);
  });

  /**
   * And its four controls are gone from the tab order, not merely from
   * sight. A visually-hidden container is still focusable, and tabbing
   * into a search box nobody can see is worse than not having one.
   */
  test('leaves nothing in the bar to tab into', async ({ app }) => {
    for (const tag of [
      'nav-history',
      'library-filter',
      'search-bar',
      'job-indicator',
    ]) {
      await expect(app.locator(`header.top-bar ${tag}`)).toBeHidden();
    }

    const focusable = await app.evaluate(
      () =>
        document
          .querySelector('header.top-bar')!
          .querySelectorAll('input, select, button, a[href]').length,
    );

    // Nothing in the bar is *rendered*, so nothing in it can be
    // focused; the controls are display:none, which takes their own
    // shadow content with them.
    expect(focusable).toBe(0);
  });
});

test.describe('search on a phone', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(PHONE);
  });

  test.afterEach(async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });
  });

  test('is a button in the view that can be searched', async ({ app }) => {
    await app.getByTestId('tab-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );

    // Scoped to the view: every cached primary view holds a
    // `page-header`, and an unscoped testid is `bottom-nav`'s
    // "resolved to 2 elements" trap again.
    const trigger = app.locator('track-list page-header search-trigger button');

    await expect(trigger).toBeVisible();
    await expect(trigger).toHaveAttribute('aria-label', 'Search tracks');
  });

  /**
   * The whole journey, which is the thing the issue asks for: a button,
   * a modal, and the results on the page behind it saying what they are
   * showing.
   */
  test('opens a modal, filters the page, and says so', async ({ app }) => {
    await app.getByTestId('tab-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );

    await app.locator('track-list page-header search-trigger button').click();

    const dialog = app.getByTestId('search-dialog');

    // Attached, not visible: `wa-dialog`'s host is `display: contents`,
    // so the element carrying the testid always reports hidden — what
    // is visible is the native `<dialog>` inside it. That awkwardness
    // is written down in CLAUDE.md and is why the assertion that this
    // is really up is the role query below.
    await expect(dialog).toBeAttached();

    // Named, which `getByRole` can answer and the a11y snapshot cannot
    // — the snapshot never prints a dialog's name, named or not. This
    // is also the assertion that the dialog is genuinely showing.
    await expect(
      app.getByRole('dialog', { name: 'Search tracks' }),
    ).toBeVisible();

    // Scoped: the header's own box is still in the document, hidden.
    // This is the one moment there are two `search-input`s.
    await dialog.getByTestId('search-input').fill('aurora');

    // Enter hands the screen back, because the results are the page.
    await app.keyboard.press('Enter');
    await expect(dialog).not.toBeAttached();

    // Polled: the box debounces by 150ms, so reading the page once
    // straight after closing the dialog can capture the state before
    // the term ever reached the store.
    await expect
      .poll(() =>
        app.evaluate(
          () =>
            document
              .querySelector('[data-testid="main-content"] track-list')
              ?.shadowRoot?.querySelector('page-header')
              ?.shadowRoot?.querySelector('[data-testid="page-search-scope"]')
              ?.textContent?.trim() ?? '',
        ),
      )
      .toMatch(/matching.*aurora/);

    // And the button says the search is on, in its name rather than
    // only in its colour.
    await expect(
      app.locator('track-list page-header search-trigger button'),
    ).toHaveAttribute('aria-label', /aurora/);

    // Leave the app as the next spec expects to find it.
    await app.locator('track-list page-header search-trigger button').click();
    await app.getByTestId('search-dialog').getByTestId('search-input').fill('');
    await app.keyboard.press('Escape');
  });

  /**
   * Two of the seven searchable views have no `page-header` — they are
   * detail views that filter on the term and say so in their own
   * headers. A trigger placed only in `page-header` would leave them
   * with a search they can show and no way to set it, which is #24's
   * sentence broken in the band it was written for.
   */
  test('reaches the playlist detail view too', async ({ app }) => {
    await app.getByTestId('tab-playlists').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'playlists',
    );

    // `.playlist-item`, which is what the list renders. Asserted to
    // exist rather than skipped on: the seed has a playlist, and a
    // spec that quietly skips when its selector stops matching is a
    // spec that reports success for a renamed class.
    const first = app.locator('playlist-view .playlist-item').first();

    await expect(first).toBeVisible();
    await first.dblclick();

    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'playlist-details',
    );

    await expect(
      app.locator('playlist-details search-trigger button'),
    ).toBeVisible();
  });

  /**
   * A button that cannot do anything is worse than none — the rule
   * `library-status-indicator` was rewritten on. Home has nothing of
   * its own to search and is not in the store's map.
   */
  test('offers no button where there is nothing to search', async ({ app }) => {
    await app.getByTestId('tab-home').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'home',
    );

    await expect(
      app.locator('home-view page-header search-trigger button'),
    ).toHaveCount(0);
  });

  test('offers no button on a desktop, where the header has a box', async ({
    app,
  }) => {
    await app.setViewportSize({ width: 1440, height: 900 });
    await app.getByTestId('nav-tracks').click();

    await expect(
      app.locator('track-list page-header search-trigger button'),
    ).toHaveCount(0);
    await expect(app.locator('header.top-bar search-bar')).toBeVisible();
  });
});

/**
 * #148, which #57 inherits: `library-filter` is the only control in the
 * app that calls `setSelectedLibrary`, and the bar it lived in is gone
 * on a phone. #143 refused to hide it as a fit step for exactly this
 * reason, so dropping it here would have been the same trade.
 */
test.describe('the library filter has a home that is not the bar', () => {
  test.afterEach(async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });
  });

  test('is in Settings, and is reachable from a phone', async ({ app }) => {
    await app.setViewportSize(PHONE);

    await app.getByTestId('tab-more').click();
    await app.getByTestId('nav-drawer').getByTestId('nav-settings').click();

    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'settings',
    );

    const filter = app.getByTestId('settings-library-filter');

    await expect(filter).toBeVisible();
    await expect(filter.locator('select')).toBeVisible();
  });

  test('and it is the same control at every width', async ({ app }) => {
    // Not a phone-only copy: "where do I change which library I am
    // browsing" having two answers by viewport is the fault, not the
    // fix.
    await app.setViewportSize({ width: 1440, height: 900 });
    await app.getByTestId('nav-settings').click();

    await expect(app.getByTestId('settings-library-filter')).toBeVisible();
    await expect(app.locator('header.top-bar library-filter')).toBeVisible();
  });
});

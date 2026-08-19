import { test, expect } from '../support/fixtures.js';

/**
 * #69: the Playlists header's buttons could not be reached.
 *
 * Three text buttons — Import (91px), New Playlist (122px), New Smart
 * Playlist (162px), 390px in total — inside a header that gets 700px at
 * 900×600. "New Smart Playlist" rendered **114 of its 162px**, and at
 * phone width the Android report was the plain version of it: you
 * cannot scroll to reach them, and scrolling is not how page controls
 * should be exposed anyway.
 *
 * **`layout-overflow.spec.ts` passes on the broken build**, which is why
 * this file exists rather than a case being added there. That spec
 * asserts the *shell* needs no sideways scrolling; clipping *inside* a
 * component is invisible to it. So the measurement here is per-button
 * and per-header, against the widths the app promises.
 *
 * Plan 018's size matrix is the promise being kept: **no action is ever
 * unreachable at any supported size.** These are its three bands.
 */

const VIEWPORTS = [
  // Desktop's worst case, and not the enforced minimum: the sidebar
  // collapses to icons *below* 900, so the content area is 843px at 899
  // and 700px at 900. Testing "the minimum" and stopping misses it.
  { name: '900×600 (widest sidebar, narrowest content)', width: 900, height: 600 },
  { name: '800×600 (the enforced minimum)', width: 800, height: 600 },
  { name: '390×780 (phone)', width: 390, height: 780 },
  // WCAG 1.4.10's reflow target, which plan 018 promises the app fits.
  { name: '320×600 (400% zoom)', width: 320, height: 600 },
];

/** Every action the Playlists header can offer, in declared order. */
const ACTIONS = ['Import', 'New Playlist', 'New Smart Playlist'];

/**
 * What the header is actually rendering, measured rather than inferred.
 *
 * A shadow query is the wrong tool for *asserting* — that is what
 * `getByRole` below is for — but it is the right one for a measurement,
 * because the number this issue is about (a button 48px wider than the
 * box holding it) is not in the accessibility tree at all.
 */
const headerFit = (page: import('@playwright/test').Page) =>
  page.evaluate(() => {
    const root = document
      .querySelector('[data-testid="main-content"] playlist-view')
      ?.shadowRoot?.querySelector('page-header')?.shadowRoot;

    if (!root) return null;

    const header = root.querySelector<HTMLElement>('.page-header')!;
    const box = header.getBoundingClientRect();
    const title = root.querySelector<HTMLElement>('h1')!;

    const clipped = [
      ...root.querySelectorAll<HTMLElement>('.action, .more-button'),
    ]
      .filter((b) => !b.hidden)
      .filter((b) => {
        const r = b.getBoundingClientRect();

        return r.right > box.right + 1 || r.left < box.left - 1;
      })
      .map((b) => b.dataset['actionId'] ?? 'more');

    return {
      overflow: header.scrollWidth - header.clientWidth,
      clipped,
      titleTruncated: title.scrollWidth > title.clientWidth + 1,
      buttons: [...root.querySelectorAll<HTMLElement>('.action')]
        .filter((b) => !b.hidden)
        .map((b) => b.textContent?.trim() ?? ''),
      menu: [
        ...root.querySelectorAll('#page-header-overflow wa-dropdown-item'),
      ].map((i) => i.textContent?.trim() ?? ''),
    };
  });

test.describe('the page header never clips an action', () => {
  test.beforeEach(async ({ app }) => {
    await app.getByTestId('nav-playlists').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'playlists',
    );
  });

  test.afterEach(async ({ app }) => {
    await app.setViewportSize({ width: 1280, height: 800 });
  });

  for (const vp of VIEWPORTS) {
    test(`every action is reachable at ${vp.name}`, async ({ app }) => {
      await app.setViewportSize({ width: vp.width, height: vp.height });

      // Polled: the fit is decided by a ResizeObserver, so it settles a
      // frame after the resize rather than with it.
      await expect
        .poll(async () => (await headerFit(app))?.clipped)
        .toEqual([]);

      const fit = (await headerFit(app))!;

      expect(fit.overflow).toBeLessThanOrEqual(0);

      // Between them, buttons and menu account for all three. This is
      // the assertion the issue asks for: not "it fits" but "nothing
      // was dropped to make it fit".
      expect([...fit.buttons, ...fit.menu].sort()).toEqual([...ACTIONS].sort());
    });
  }

  /**
   * The title gives way before an action does.
   *
   * Once the heading can ellipsis it absorbs the pressure, and
   * `scrollWidth` then reports a header that fits perfectly while the
   * heading reads "Playlis…" — this issue's own failure mode moved from
   * the button to the title, and invisible to exactly the measurement
   * that missed it the first time. At the desktop sizes there is always
   * an action to collapse instead.
   */
  test('does not truncate the heading to keep a button', async ({ app }) => {
    for (const vp of VIEWPORTS.slice(0, 2)) {
      await app.setViewportSize({ width: vp.width, height: vp.height });

      await expect
        .poll(async () => (await headerFit(app))?.titleTruncated)
        .toBe(false);
    }
  });

  /**
   * Asserted through the accessibility tree, never a shadow query. An
   * overflow menu is exactly the shape that grows a nameless control,
   * and this repo has shipped one four times — most recently the
   * queue's own close button.
   */
  test('the overflow is a named control that opens a named menu', async ({
    app,
  }) => {
    await app.setViewportSize({ width: 900, height: 600 });

    const more = app.getByRole('button', { name: 'More actions' });

    await expect(more).toBeVisible();
    await expect(more).toHaveAttribute('aria-expanded', 'false');

    await more.click();

    await expect(more).toHaveAttribute('aria-expanded', 'true');

    const menu = app.getByRole('menu', { name: 'More actions' });

    await expect(menu).toBeVisible();

    // Collapsed at 900×600: Import (lowest priority) and New Smart
    // Playlist. New Playlist stays a button because it is the drop
    // target, and a closed menu cannot be one.
    await expect(
      menu.getByRole('menuitem', { name: 'Import' }),
    ).toBeVisible();
    await expect(
      app.getByRole('button', { name: 'New Playlist', exact: true }),
    ).toBeVisible();
  });

  /**
   * The phone case is the original report. Every action is in the menu
   * at 390px, and the menu is reachable by name — which is the whole of
   * "these need to be reachable in a sensible way".
   */
  test('offers every action from the menu on a phone', async ({ app }) => {
    await app.setViewportSize({ width: 390, height: 780 });

    const more = app.getByRole('button', { name: 'More actions' });

    await expect(more).toBeVisible();
    await more.click();

    const menu = app.getByRole('menu', { name: 'More actions' });

    for (const label of ACTIONS) {
      await expect(menu.getByRole('menuitem', { name: label })).toBeVisible();
    }
  });

  /**
   * Escape closes it and focus goes back to the trigger — `MenuKeyboard`
   * is shared with every other menu in the app precisely so this is not
   * a second keyboard model, and this is what proves it was wired up
   * rather than merely imported.
   */
  test('takes the keyboard, and gives it back', async ({ app }) => {
    await app.setViewportSize({ width: 900, height: 600 });

    const more = app.getByRole('button', { name: 'More actions' });

    await more.click();

    const menu = app.getByRole('menu', { name: 'More actions' });

    await expect(menu).toBeVisible();

    // The first item takes focus on open. `wa-dropdown-item` sets its
    // own role in its own first update, so this is polled rather than
    // read: a query at the host's updateComplete finds nothing, which
    // reads exactly like a menu that refused to take focus.
    await expect
      .poll(async () =>
        app.evaluate(() => {
          // Stops where `MenuKeyboard`'s own `deepActiveElement` stops:
          // on the *host* whose shadow root has no active element.
          // Descending unconditionally lands inside the focused
          // `wa-dropdown-item`'s own shadow root, where nothing is
          // focused — which reads exactly like a menu that refused the
          // keyboard, on a build where it did not.
          let el = document.activeElement;

          while (el?.shadowRoot?.activeElement) el = el.shadowRoot.activeElement;

          return el?.textContent?.trim() ?? null;
        }),
      )
      .toBe('Import');

    await app.keyboard.press('Escape');

    await expect(more).toHaveAttribute('aria-expanded', 'false');
    await expect(more).toBeFocused();
  });

  /**
   * New Playlist is a drop target, and declaring it as data must not
   * take that away — which is why a `PageAction` carries the drop
   * handlers rather than the header owning a notion of dropping.
   *
   * Nothing covered this before, in either tier, and it is the one
   * behaviour the migration could plausibly have destroyed silently:
   * dragging still *looks* fine against a button that no longer
   * accepts anything.
   */
  test('New Playlist still accepts a dropped track', async ({ app }) => {
    await app.setViewportSize({ width: 1280, height: 800 });

    const button = app.getByRole('button', {
      name: 'New Playlist',
      exact: true,
    });

    await expect(button).toBeVisible();

    const result = await app.evaluate(async () => {
      const view = document.querySelector(
        '[data-testid="main-content"] playlist-view',
      )!;
      const target = view.shadowRoot!
        .querySelector('page-header')!
        .shadowRoot!.querySelector('[data-testid="page-action-new-playlist"]')!;

      const data = new DataTransfer();

      data.setData(
        'application/x-yj-tracks',
        JSON.stringify({ filePaths: ['/tmp/dropped.mp3'] }),
      );

      const fire = (type: string) =>
        target.dispatchEvent(
          new DragEvent(type, {
            bubbles: true,
            cancelable: true,
            dataTransfer: data,
          }),
        );

      fire('dragover');
      await new Promise((r) => setTimeout(r, 50));

      // The affordance is the host's state reaching the header's
      // button, which is the half a plain handler call would not prove.
      const highlighted = target.classList.contains('drag-over');

      fire('drop');
      await new Promise((r) => setTimeout(r, 200));

      return {
        highlighted,
        opened: view.shadowRoot!.querySelector('.create-form') !== null,
      };
    });

    expect(result).toEqual({ highlighted: true, opened: true });

    // Leave the view as it was found.
    await app.keyboard.press('Escape');
  });

  /**
   * An action given back when the window widens again. The collapsed
   * set is a function of the current width and not of how it got there
   * — a rule that only ever *added* to it would never widen.
   */
  test('gives the buttons back when the window grows', async ({ app }) => {
    await app.setViewportSize({ width: 390, height: 780 });

    await expect.poll(async () => (await headerFit(app))?.buttons).toEqual([]);

    await app.setViewportSize({ width: 1440, height: 900 });

    await expect
      .poll(async () => (await headerFit(app))?.buttons)
      .toEqual(ACTIONS);
    await expect.poll(async () => (await headerFit(app))?.menu).toEqual([]);
  });
});

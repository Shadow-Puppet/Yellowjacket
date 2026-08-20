import { test, expect } from '../support/fixtures.js';

/**
 * H-7 and H-11, which are one finding seen from two distances: the app
 * did not fit in its own window.
 *
 * `computeDefaultWidths` shared out the track list's whole
 * `clientWidth` and never subtracted the 24 px favourite column or the
 * 2×8 px row padding that `colBoundaryPositions` already knew about, so
 * every row was 40 px wider than the box holding it (`scrollWidth 1280`
 * against `clientWidth 1240`) and Duration was clipped at every size.
 *
 * And the enforced minimum window was 512×384, which the layout had
 * never supported: at 700×480 the eleven sidebar items needed 406 px of
 * a 352 px pane, `overflow: hidden` cut the last two off, nothing
 * scrolled, and **Settings and Jobs could not be reached at all**.
 *
 * The three viewports are the two common ones and the new enforced
 * minimum (`backend/config/window.go`). The minimum is the one that
 * matters: it is the only size the app is *promising* to work at.
 */

/** Keep in step with `MinWidth`/`MinHeight` in backend/config/window.go. */
const MIN_VIEWPORT = { width: 800, height: 600 };

const VIEWPORTS = [
  { name: '1440×900', width: 1440, height: 900 },
  { name: '1024×768', width: 1024, height: 768 },
  // Not the minimum, and that is the point (#24). The sidebar collapses
  // to icons *below* 900, so the main panel is 843px at 899 and 700px
  // at 900 — the narrowest content area any desktop width produces is
  // here, not at the enforced floor. A list that stopped at the minimum
  // was missing its own worst case.
  { name: '900×600 (the widest sidebar, so the narrowest content)', width: 900, height: 600 },
  { name: `the minimum (${MIN_VIEWPORT.width}×${MIN_VIEWPORT.height})`, ...MIN_VIEWPORT },
];

/**
 * Rows report their own overflow, which is the symptom a user sees: a
 * column rendered past the edge of the row it belongs to.
 */
const rowOverflow = (page: import('@playwright/test').Page) =>
  page.evaluate(() => {
    const list = document.querySelector('track-list');
    const root = list?.shadowRoot;

    if (!root) return { rows: 0, overflowing: [] as string[] };

    const boxes = [
      ...root.querySelectorAll<HTMLElement>('.header-row, .track-row'),
    ];

    return {
      rows: boxes.length,
      overflowing: boxes
        .filter((b) => b.scrollWidth > b.clientWidth)
        .map((b) => `${b.className}: ${b.scrollWidth} > ${b.clientWidth}`),
    };
  });

test.describe('the app fits in its own window', () => {
  for (const vp of VIEWPORTS) {
    test(`no track row overflows at ${vp.name}`, async ({ app }) => {
      await app.setViewportSize({ width: vp.width, height: vp.height });
      await app.getByTestId('nav-tracks').click();
      await expect(app.getByTestId('main-content')).toHaveAttribute(
        'data-active-view',
        'tracks',
      );

      // The columns are recomputed by a ResizeObserver, so poll rather
      // than read once: a single read races the resize and passes on
      // the widths from the previous viewport.
      await expect.poll(async () => (await rowOverflow(app)).rows).toBeGreaterThan(1);
      await expect
        .poll(async () => (await rowOverflow(app)).overflowing)
        .toEqual([]);
    });
  }

  test('every destination stays reachable at the minimum size', async ({
    app,
  }) => {
    await app.setViewportSize(MIN_VIEWPORT);

    // Below the breakpoint the sidebar collapses to icons; the labels
    // go, the destinations do not.
    const sidebar = app.locator('app-sidebar');

    await expect
      .poll(() =>
        sidebar.evaluate((el) => el.classList.contains('collapsed')),
      )
      .toBe(true);

    // Settings is the one that was unreachable: it is last in the nav,
    // and the pane used to clip rather than scroll. (Jobs was the other
    // half of this until #27 folded it into Settings.)
    for (const view of ['explore', 'settings'] as const) {
      const item = app.getByTestId(`nav-${view}`);

      await item.scrollIntoViewIfNeeded();
      await item.click();
      await expect(app.getByTestId('main-content')).toHaveAttribute(
        'data-active-view',
        view,
      );
    }

    // Leave the app where the next spec expects to find it.
    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
  });

  test('the sidebar scrolls rather than hiding what does not fit', async ({
    app,
  }) => {
    await app.setViewportSize({ width: 700, height: 480 });

    // Smaller than the enforced minimum on purpose: the window cannot
    // be dragged here, but a scaled display or a large system font can
    // still land the layout in it, and clipping the nav with no scroll
    // is the failure that made Settings unreachable.
    const reachable = await app.locator('app-sidebar').evaluate((el) => {
      const settings = el.shadowRoot?.querySelector<HTMLElement>(
        '[data-testid="nav-settings"]',
      );

      if (!settings) return null;

      el.scrollTop = el.scrollHeight;

      const item = settings.getBoundingClientRect();
      const pane = el.getBoundingClientRect();

      return item.bottom <= Math.ceil(pane.bottom) && item.top >= Math.floor(pane.top);
    });

    expect(reachable).toBe(true);
  });
});

test.describe('the title block fits its bar', () => {
  test('the hgroup stays inside the 4em top bar', async ({ app }) => {
    // The state a11y.29 landed in. The pair is flex-centred and a UA
    // gives an `h1` a 0.67em top margin, so the block measured 67px
    // inside 64 — pre-existing, and invisible until dropping the h3's
    // bottom margin shortened the block and shifted it down into the
    // clip. The descenders of "meant to bee." were cut.
    const fits = await app.locator('hgroup').evaluate((el) => {
      const bar = el.closest('.top-bar')!.getBoundingClientRect();
      const group = el.getBoundingClientRect();

      return {
        top: group.top >= Math.floor(bar.top),
        bottom: group.bottom <= Math.ceil(bar.bottom),
        height: Math.round(group.height),
      };
    });

    expect(fits.top).toBe(true);
    expect(fits.bottom).toBe(true);
  });
});

/**
 * `a11y.21` (WCAG 1.4.10 Reflow), measured rather than taken as filed.
 *
 * The finding's mechanism is vertical — "at high zoom the 4em bars grow
 * while the viewport does not, and anything that no longer fits is
 * clipped with no scrollbar" — and that is not what happens. The middle
 * row is `1fr` and absorbs the bars exactly. What is real is the axis
 * the finding does not mention.
 */
test.describe('the shell reflows rather than hiding what does not fit', () => {
  test('larger text shrinks the panel instead of clipping the shell', async ({
    app,
  }) => {
    await app.setViewportSize({ width: 800, height: 600 });
    await app.evaluate(() => {
      document.documentElement.style.fontSize = '32px';
    });

    const shell = await app.evaluate(() => {
      const foot = document.querySelector('.bottom-bar')!.getBoundingClientRect();
      const main = document.querySelector('#main-content')!.getBoundingClientRect();

      return {
        footBottom: Math.round(foot.bottom),
        viewport: document.documentElement.clientHeight,
        mainHeight: Math.round(main.height),
      };
    });

    // 200% text takes the bars from 64px to 128px each and the panel
    // from 472px to 344px, and the footer still lands exactly on the
    // bottom of the viewport. Nothing is clipped vertically.
    expect(shell.footBottom).toBe(shell.viewport);
    expect(shell.mainHeight).toBeGreaterThan(300);

    await app.evaluate(() => {
      document.documentElement.style.fontSize = '';
    });
  });

  test('nothing needs scrolling to at 320px, because it all fits', async ({ app }) => {
    // 320 CSS px is 400% page zoom of a 1280px viewport, which is the
    // size 1.4.10 names.
    //
    // **This assertion is the inverse of the one it replaces, and that
    // is the fix landing rather than the test being weakened.** The
    // shell used to be 784px wide here, so 464px of the app — the job
    // indicator and the queue button among it — sat behind
    // `overflow: hidden` with no way to reach it; making the axis
    // scrollable was the remedy available at the time. 016 B2's phone
    // layout reflows instead: below 600px the sidebar becomes a bottom
    // tab bar, the header's controls shrink, and the shell measures
    // exactly 320px in a 320px viewport. Reflow is what 1.4.10 asks
    // for; being able to scroll to the overflow was the concession.
    await app.setViewportSize({ width: 320, height: 256 });

    const fit = await app.evaluate(() => {
      const se = document.scrollingElement!;

      return {
        scrollWidth: se.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
        scrollHeight: se.scrollHeight,
        clientHeight: document.documentElement.clientHeight,
      };
    });

    expect(fit.scrollWidth).toBeLessThanOrEqual(fit.clientWidth);

    // And the vertical axis stays fixed, which is what keeps the
    // transport where a player's transport belongs.
    expect(fit.scrollHeight).toBeLessThanOrEqual(fit.clientHeight);

    await app.setViewportSize({ width: 1440, height: 900 });
  });

  for (const vp of VIEWPORTS) {
    test(`no scrollbar appears at ${vp.name}`, async ({ app }) => {
      await app.setViewportSize({ width: vp.width, height: vp.height });

      const excess = await app.evaluate(() => {
        const de = document.documentElement;

        return de.scrollWidth - de.clientWidth;
      });

      // The other half: at every size this app promises, the fix costs
      // nothing. A scrollbar that is always there is a worse answer
      // than the clipping it replaced.
      expect(excess).toBe(0);
    });
  }
});

import { test, expect } from '../support/fixtures.js';

/**
 * #62. On a phone, background work is shown in the notification band
 * and the header indicator stands down.
 *
 * The report was that the indicator's popover "is obscured by other UI,
 * so it cannot be read while jobs run". Worth saying plainly: **that
 * symptom did not reproduce in this tier.** Measured at the device's
 * own 424x439 viewport, the popover was neither clipped nor covered —
 * `elementFromPoint` at its centre returned the indicator at every
 * width tried. So this is not a fix for a stacking bug, and a spec
 * asserting one would be a spec asserting something that was never
 * true here.
 *
 * What is true regardless, and is what these assert:
 *
 * - a popover is a **disclosure**, and it is anchored to a bar 3.25em
 *   tall on a screen 439px tall. Background work is the one thing a
 *   phone should not make you open something to see.
 * - #57 deletes that bar and is *blocked on this issue*, because the
 *   indicator needs somewhere else to live first. Somewhere else is
 *   the band, and the test that matters for #57 is that the bar no
 *   longer holds the indicator at all.
 *
 * This is the media-query tier by necessity: a query inside a shadow
 * root is answered by the viewport, and `notification-host` decides
 * whether the panel *exists* from `matchMedia`. The component tier
 * cannot set either.
 */

type Page = import('@playwright/test').Page;

const JOBS = [
  {
    id: 'phone:scan',
    kind: 'library-scan',
    state: 'running',
    title: 'Scanning Music',
    current: 40,
    total: 100,
    caps: { pausable: true, cancellable: true },
  },
  {
    id: 'phone:idx',
    kind: 'index-build',
    state: 'running',
    title: 'Building the search index',
    current: 2,
    total: 9,
    caps: { pausable: true, cancellable: true },
  },
];

/** The panel the band renders. Playwright's CSS engine pierces open
 *  shadow roots, which is what keeps this one line. */
const bandPanel = (page: Page) => page.locator('job-band').locator('job-panel');

const PHONE = { width: 424, height: 439 };
const DESKTOP = { width: 1100, height: 800 };

test.describe('background jobs on a phone', () => {
  test('are shown in the band, without opening anything', async ({
    app,
    testctl,
  }) => {
    await app.setViewportSize(PHONE);
    await testctl.emit('JobsChanged', JOBS);

    await expect(bandPanel(app)).toBeVisible();

    // Both jobs, drawn by real `job-row`s -- asking the rows what they
    // hold rather than reading the panel's text, which would pass
    // whether or not a row rendered. Playwright's CSS engine pierces
    // open shadow roots, which is what makes this one line;
    // `querySelectorAll` does not, and stops at `job-panel`.
    await expect(bandPanel(app).locator('job-row')).toHaveCount(2);

    await expect(
      bandPanel(app).locator('job-row').first(),
    ).toContainText('Scanning Music');
  });

  /**
   * The #57 assertion. Not "the indicator is invisible" — that could be
   * true because the bar overflowed — but that the shell's own rule
   * puts it away at this width.
   */
  test('leave the top bar, which is what #57 is waiting for', async ({
    app,
    testctl,
  }) => {
    await app.setViewportSize(PHONE);
    await testctl.emit('JobsChanged', JOBS);
    await expect(bandPanel(app)).toBeVisible();

    await expect(app.locator('job-indicator')).toBeHidden();
  });

  /**
   * The property the first attempt at this got wrong, so it is the one
   * worth pinning: the band is **in the layout**, not over it.
   *
   * A fixed band reads fine in a screenshot and is unusable -- at
   * 424x439 a compact panel is ~200px of a 439px screen and it covers
   * what is under it. Four specs failed on that version, two
   * phone-shell journeys and the header's action menu, because the
   * panel was intercepting the taps. So: nothing of the app is
   * underneath it, and the main panel starts below it.
   */
  test('push the content down rather than covering it', async ({
    app,
    testctl,
  }) => {
    await app.setViewportSize(PHONE);

    const before = await app
      .getByTestId('main-content')
      .evaluate((el) => el.getBoundingClientRect().top);

    await testctl.emit('JobsChanged', JOBS);
    await expect(bandPanel(app)).toBeVisible();

    const after = await app.evaluate(() => {
      const band = document.querySelector('job-band') as HTMLElement;
      const main = document.querySelector(
        '[data-testid="main-content"]',
      ) as HTMLElement;
      const b = band.getBoundingClientRect();
      const m = main.getBoundingClientRect();

      // What the browser reports at the band's own centre. If this is
      // anything but the band, the band is sitting on top of it.
      const hit = document.elementFromPoint(
        Math.round(b.x + b.width / 2),
        Math.round(b.y + b.height / 2),
      );

      return {
        mainTop: m.top,
        bandBottom: b.bottom,
        withinViewport: b.bottom <= window.innerHeight + 0.5,
        hit: hit?.tagName.toLowerCase() ?? null,
      };
    });

    expect({
      pushed: after.mainTop > before,
      mainClearsBand: after.mainTop >= after.bandBottom - 0.5,
      withinViewport: after.withinViewport,
      hit: after.hit,
    }).toEqual({
      pushed: true,
      mainClearsBand: true,
      withinViewport: true,
      hit: 'job-band',
    });
  });

  /**
   * A running job repaints several times a second. The stack it sits
   * beside is `role="status" aria-live="polite"`, and a progress bar
   * inside a live region is a screen reader reading a number out over
   * and over — so the two are siblings in the band rather than one
   * list, and this is what says so.
   */
  test('are not inside the live region they sit beside', async ({
    app,
    testctl,
  }) => {
    await app.setViewportSize(PHONE);
    await testctl.emit('JobsChanged', JOBS);
    await expect(bandPanel(app)).toBeVisible();

    const insideLiveRegion = await app.evaluate(() => {
      const band = document.querySelector('job-band');

      // Neither the band itself nor anything it is nested in may be a
      // live region -- `closest` answers both at once.
      return !!band?.closest('[aria-live]') || band?.hasAttribute('aria-live');
    });

    expect(insideLiveRegion).toBe(false);
  });

  /**
   * `bottom-nav` rendering its duplicate `<app-sidebar>` unconditionally
   * broke 30 specs with "resolved to 2 elements" on a viewport where it
   * was not even visible. Settings already holds four `job-panel`s, so
   * a fifth that answers for *every* kind is the same trap — which is
   * why the band decides from `matchMedia` whether the element exists
   * rather than hiding it with CSS.
   */
  test('do not leave a second panel behind on a desktop', async ({
    app,
    testctl,
  }) => {
    await app.setViewportSize(DESKTOP);
    await testctl.emit('JobsChanged', JOBS);

    await expect(app.locator('job-indicator')).toBeVisible();
    await expect(bandPanel(app)).toHaveCount(0);
  });
});

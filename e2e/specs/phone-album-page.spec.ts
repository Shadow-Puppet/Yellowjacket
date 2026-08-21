import { test, expect } from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * The album page on a phone (#66).
 *
 * Two faults, and neither was visible to `layout-overflow.spec.ts`:
 * that spec asserts the *shell* needs no sideways scrolling, and the
 * shell was correct throughout — `body.scrollWidth === clientWidth`
 * while `explore-album-details` itself measured 443 inside a 424px box
 * and clipped two of the album's three primary actions with its own
 * `overflow: hidden`. So the measurement here is **per control against
 * the component's box**, which is the same shape `top-bar-fit.spec.ts`
 * needed for the same reason.
 *
 * The other half is the scroll: the page was a fixed header over a
 * scrolling tracklist, so at the reference device's 424x439 the header
 * owned 253 of the panel's 318px and the list scrolled in the 64 that
 * were left. It is one scroll container below 600px, which is a
 * property of the *host* rather than of `.content`.
 *
 * The engine is the caveat this tier cannot close: the reference device
 * renders in Chrome 113 and this is Chromium/WebKit. A flex direction
 * and a scroll container are nowhere near that engine's documented gaps
 * (relaxed nesting, the Popover API, `light-dark()`), but "it renders
 * at that size in Chromium" is not evidence about the phone.
 */

/** The phone this was measured on, in CSS pixels. */
const DEVICE = { width: 424, height: 439 };

const details = (page: Page) => page.locator('explore-album-details');

/** The page's own boxes, read from inside its shadow root. */
const geometry = (page: Page) =>
  page.evaluate(() => {
    const host = document.querySelector('explore-album-details');
    const sr = host?.shadowRoot;

    if (!host || !sr) return null;

    const box = (sel: string) => {
      const el = sr.querySelector(sel);

      if (!el) return null;

      const r = el.getBoundingClientRect();

      return { width: Math.round(r.width), right: Math.round(r.right) };
    };

    const content = sr.querySelector('.content');

    return {
      hostWidth: host.clientWidth,
      hostScrollWidth: host.scrollWidth,
      // The host is the scroller below 600px, so the page is taller
      // than its box rather than the tracklist being a window inside it.
      hostScrolls: host.scrollHeight > host.clientHeight,
      contentScrolls: content
        ? content.scrollHeight > content.clientHeight
        : null,
      header: box('.album-header'),
      play: box('[data-testid="album-play"]'),
      shuffle: box('[data-testid="album-shuffle"]'),
      queue: box('[data-testid="album-queue"]'),
      title: (() => {
        const el = sr.querySelector('.album-title-text');

        return el ? el.scrollWidth <= el.clientWidth + 1 : null;
      })(),
    };
  });

test.describe('the album page on a phone', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await openFirstAlbum(app);
  });

  test.afterEach(async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });
    await app.getByTestId('nav-tracks').click();
  });

  test('keeps every action inside its own box', async ({ app }) => {
    const geo = await geometry(app);

    expect(geo).not.toBeNull();
    // "Shuffle album" ended at x=443 in a 424px component and could not
    // be reached by any gesture; "Add to queue" at 440.
    for (const action of ['play', 'shuffle', 'queue'] as const) {
      expect(
        geo?.[action],
        `${action} is rendered`,
      ).not.toBeNull();
      expect(
        geo?.[action]?.right ?? 0,
        `${action} ends inside the page`,
      ).toBeLessThanOrEqual(geo?.hostWidth ?? 0);
    }

    expect(geo?.hostScrollWidth).toBe(geo?.hostWidth);
    expect(geo?.header?.width).toBe(geo?.hostWidth);
  });

  test('gives the title the row rather than one glyph of it', async ({
    app,
  }) => {
    // `.album-info` was squeezed to 112px beside the art, so an album
    // called *Glass Harbour* drew as `G…`. It carries `min-width: 0`
    // and was shrinking as asked — the row had to stack.
    expect(await geometry(app).then((g) => g?.title)).toBe(true);
  });

  test('scrolls as one page, with the header scrolling away', async ({
    app,
  }) => {
    const before = await geometry(app);

    expect(before?.hostScrolls).toBe(true);
    expect(before?.contentScrolls).toBe(false);

    const headerTop = () =>
      app.evaluate(
        () =>
          document
            .querySelector('explore-album-details')
            ?.shadowRoot?.querySelector('.album-header')
            ?.getBoundingClientRect().top ?? 0,
      );

    expect(await headerTop()).toBeGreaterThanOrEqual(0);

    // A wheel gesture, not `scrollTop`: `overflow: hidden` still permits
    // programmatic scrolling, so a probe that assigns it passes on the
    // build this exists to fail.
    await details(app).hover();
    await app.mouse.wheel(0, 250);

    await expect.poll(headerTop).toBeLessThan(-100);
  });

  test('is the desktop arrangement again above the breakpoint', async ({
    app,
  }) => {
    await app.setViewportSize({ width: 1024, height: 800 });

    // The same element, re-laid-out: one component with two
    // arrangements, not a phone-only copy.
    await expect
      .poll(async () => (await geometry(app))?.hostScrolls)
      .toBe(false);

    const arrangement = await app.evaluate(() => {
      const sr = document.querySelector('explore-album-details')?.shadowRoot;
      const header = sr?.querySelector('.album-header');
      const content = sr?.querySelector('.content');

      return {
        direction: header ? getComputedStyle(header).flexDirection : null,
        contentOverflow: content ? getComputedStyle(content).overflowY : null,
      };
    });

    expect(arrangement.direction).toBe('row');
    expect(arrangement.contentOverflow).toBe('auto');
  });
});

/** Albums → the second card, which navigates to the album page. */
async function openFirstAlbum(app: Page): Promise<void> {
  // Below 600px the sidebar is gone; the tab bar is the navigation.
  await app.getByTestId('tab-albums').click();
  await expect(app.getByTestId('main-content')).toHaveAttribute(
    'data-active-view',
    'albums',
  );

  await expect.poll(() => cardCount(app)).toBeGreaterThan(1);

  // Dispatched rather than clicked: the card lives in a virtualizer
  // inside a shadow root, and Enter expands the dropdown instead.
  await app.evaluate(() => {
    document
      .querySelector('cover-grid')
      ?.shadowRoot?.querySelectorAll('.album-card')[1]
      ?.dispatchEvent(
        new MouseEvent('click', { bubbles: true, composed: true }),
      );
  });

  await expect(app.getByTestId('main-content')).toHaveAttribute(
    'data-active-view',
    'explore-album-details',
  );
  await expect(
    details(app).locator('[data-testid="album-play"]'),
  ).toBeVisible();
}

async function cardCount(app: Page): Promise<number> {
  return app.evaluate(
    () =>
      document
        .querySelector('cover-grid')
        ?.shadowRoot?.querySelectorAll('.album-card').length ?? 0,
  );
}

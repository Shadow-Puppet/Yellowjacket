import { test, expect } from '../support/fixtures.js';

/**
 * The track list on a phone (plan 016 B2 phase 4).
 *
 * The component tier pins the arrangement; this pins it in the real
 * shell, at the viewport of the device the work was measured on — 424 x
 * 439, a Light Phone III — because the fault it fixes was invisible to
 * every assertion the app had. The columns *fit*: `--grid-cols` summed
 * to exactly the host width, nothing overflowed, and every column was
 * still unreadable. Only a measurement of what a cell can hold, or a
 * screenshot, shows that.
 */
type Page = import('@playwright/test').Page;

/** The phone this was built against, in CSS pixels. */
const DEVICE = { width: 424, height: 439 };

/** A common small phone, as the shell specs use. */
const PHONE = { width: 390, height: 844 };

const list = (page: Page) => page.locator('track-list');

/** The row's grid tracks and the widest text a cell can show. */
const rowGeometry = (page: Page) =>
  page.evaluate(() => {
    const sr = document.querySelector('track-list')?.shadowRoot;
    const row = sr?.querySelector('.track-row');

    if (!row) return null;

    const title = row.querySelector('.stacked-title');
    const sub = row.querySelector('.stacked-sub');

    return {
      tracks: getComputedStyle(row)
        .gridTemplateColumns.split(/\s+/)
        .filter(Boolean).length,
      rowHeight: Math.round(row.getBoundingClientRect().height),
      headerRow: !!sr?.querySelector('.header-row'),
      handles: sr?.querySelectorAll('.col-resize-handle').length ?? 0,
      titleWidth: title ? Math.round(title.getBoundingClientRect().width) : 0,
      // A truncated cell is the fault; a cell wider than its text is fine.
      titleTruncated: title ? title.scrollWidth > title.clientWidth + 1 : null,
      subText: sub?.textContent?.trim() ?? null,
    };
  });

test.describe('the track list on a phone', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await app.getByTestId('tab-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
    await expect(list(app).first()).toBeVisible();
  });

  test.afterEach(async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });
  });

  test('stacks the title over the artist and drops the pointer affordances', async ({
    app,
  }) => {
    const geo = await rowGeometry(app);

    expect(geo).not.toBeNull();
    // Favourite + one stacked column + duration.
    expect(geo?.tracks).toBe(3);
    expect(geo?.headerRow).toBe(false);
    expect(geo?.handles).toBe(0);
    expect(geo?.subText).toBeTruthy();

    // The row height has to match the virtualizer's item size, or rows
    // overlap; 52 is that number.
    expect(geo?.rowHeight).toBe(52);
  });

  test('gives the title most of the row instead of a quarter of it', async ({
    app,
  }) => {
    const geo = await rowGeometry(app);

    // Four columns at this width gave a title ~102px. The measurement
    // that matters is the share of the row, not the pixel count.
    expect(geo?.titleWidth ?? 0).toBeGreaterThan(DEVICE.width * 0.55);
  });

  test('needs no sideways scrolling, and neither does the shell', async ({
    app,
  }) => {
    const overflow = await app.evaluate(() => ({
      body: [document.body.scrollWidth, document.body.clientWidth],
      list: (() => {
        const sr = document.querySelector('track-list')?.shadowRoot;
        const row = sr?.querySelector('.track-row');

        return row ? [row.scrollWidth, row.clientWidth] : null;
      })(),
    }));

    expect(overflow.body[0]).toBe(overflow.body[1]);
    expect(overflow.list?.[0]).toBe(overflow.list?.[1]);
  });

  test('keeps the sorts a phone has no headers to reach', async ({ app }) => {
    // With no column headers, the page header's sort control is the only
    // route to sort-by-artist — so it must still offer the columns the
    // phone does not draw.
    const ids = await app.evaluate(() => {
      const header = document
        .querySelector('track-list')
        ?.shadowRoot?.querySelector('page-header') as
        | (Element & { sortOptions?: { id: string }[] })
        | null;

      return (header?.sortOptions ?? []).map((o) => o.id);
    });

    expect(ids).toContain('artistName');
    expect(ids).toContain('album');
  });

  test('is the desktop list again above the breakpoint', async ({ app }) => {
    await app.setViewportSize(PHONE);
    await expect.poll(async () => (await rowGeometry(app))?.tracks).toBe(3);

    await app.setViewportSize({ width: 1024, height: 800 });

    // The same element, re-laid-out: this is one component with two
    // column sets, not two components.
    await expect.poll(async () => (await rowGeometry(app))?.headerRow).toBe(true);
    await expect.poll(async () => (await rowGeometry(app))?.tracks).toBe(5);
  });
});

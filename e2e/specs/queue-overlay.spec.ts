import { test, expect, openTheQueue } from '../support/fixtures.js';

/**
 * #24 — the queue panel does not take the page's width away from it.
 *
 * The panel is `flex-shrink: 0` in the flow of `.content-area`, so an
 * open queue used to be paid for by the main panel. Measured on
 * Playlists before the fix:
 *
 * | viewport | main panel |
 * |---|---|
 * | 900×600 | 379px — all three header actions clipped |
 * | 390×780 | 69px |
 * | 320×600 | **0px** |
 *
 * **900×600 is the worst desktop case, not the 800×600 minimum**, and
 * that is the trap this file exists to keep closed: the sidebar
 * collapses to icons *below* 900, so the main panel is 843px at 899 and
 * 700px at 900. A spec that checks "the minimum" and stops has not
 * checked the worst case — which is what every viewport list in this
 * suite did before this.
 *
 * These assert the *content's* width rather than the panel's mode
 * wherever they can, because the mode is the mechanism and the width is
 * the complaint.
 */

/** The bands from plan 018's size matrix, plus the pixel above the collapse. */
const BANDS = [
  { name: 'a wide desktop (1280×800)', width: 1280, height: 800, inline: true },
  { name: 'the default window (1100×720)', width: 1100, height: 720, inline: true },
  { name: 'a laptop (1024×768)', width: 1024, height: 768, inline: true },
  { name: 'the worst desktop width (900×600)', width: 900, height: 600, inline: false },
  { name: 'the enforced minimum (800×600)', width: 800, height: 600, inline: false },
  { name: 'a phone (390×780)', width: 390, height: 780, inline: false },
  { name: '400% zoom (320×600)', width: 320, height: 600, inline: false },
];

/**
 * How much room the content has, and whether the shell needs scrolling
 * to reach any of itself.
 */
const shellGeometry = (page: import('@playwright/test').Page) =>
  page.evaluate(() => {
    const main = document.querySelector('#main-content')!.getBoundingClientRect();
    const panel = document.querySelector('#queue-panel')!;

    return {
      mainWidth: Math.round(main.width),
      overlay: panel.hasAttribute('overlay'),
      open: panel.hasAttribute('open'),
      bodyScrollWidth: document.body.scrollWidth,
      bodyClientWidth: document.body.clientWidth,
    };
  });

/**
 * Opening the queue is `openTheQueue`, which takes the route this
 * viewport offers. It used to be a local helper that clicked
 * `#queue-button` unconditionally, and #59 hid that button below
 * 600px -- so the two phone bands here failed on a build where the
 * queue was working perfectly, having been asserting *how* it opens as
 * much as what it does.
 */
const openQueue = openTheQueue;

test.describe('an open queue leaves the content its width', () => {
  for (const band of BANDS) {
    test(`at ${band.name}`, async ({ app }) => {
      await app.setViewportSize({ width: band.width, height: band.height });
      await openQueue(app);

      // The mode is settled by a ResizeObserver, so poll rather than
      // read once: a single read races the resize and reports the
      // previous viewport's answer.
      await expect
        .poll(async () => (await shellGeometry(app)).overlay)
        .toBe(!band.inline);

      const geo = await shellGeometry(app);

      // The floor is the point of the whole issue. Inline, the queue is
      // affordable and the content keeps the rest; as an overlay the
      // content keeps *everything*, which is what makes 0px at 320
      // impossible rather than merely unlikely.
      expect(geo.mainWidth).toBeGreaterThanOrEqual(320);

      if (!band.inline) {
        expect(geo.mainWidth).toBeGreaterThanOrEqual(
          Math.min(band.width, 320),
        );
      }

      // And opening the queue must not make the shell overflow.
      expect(geo.bodyScrollWidth).toBeLessThanOrEqual(geo.bodyClientWidth);
    });
  }
});

test.describe('an overlaid queue says it is over the content', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize({ width: 900, height: 600 });
  });

  test('draws a scrim and closes when it is clicked', async ({ app }) => {
    await openQueue(app);

    const panel = app.locator('#queue-panel');

    await expect(panel).toHaveAttribute('overlay', '');

    // The scrim is `aria-hidden` on purpose — it is a dismissal target,
    // and the named routes out are the close button and Escape — so it
    // is located structurally rather than by role.
    await panel.evaluate((el) =>
      el.shadowRoot!.querySelector<HTMLElement>('.scrim')!.click(),
    );

    await expect(app.locator('#queue-button')).toHaveAttribute(
      'aria-expanded',
      'false',
    );
  });

  /**
   * `getByRole`, not a shadow-root query: this repo has shipped a
   * nameless control three times, and a drawer with a scrim is exactly
   * the shape that grows a fourth.
   */
  test('offers a named close button', async ({ app }) => {
    await openQueue(app);

    const close = app.getByRole('button', { name: 'Close queue' });

    await expect(close).toBeVisible();
    await close.click();

    await expect(app.locator('#queue-button')).toHaveAttribute(
      'aria-expanded',
      'false',
    );
  });

  test('closes on Escape and gives focus back to the toggle', async ({
    app,
  }) => {
    const toggle = app.locator('#queue-button');

    await toggle.focus();
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');

    await app.keyboard.press('Escape');

    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(toggle).toBeFocused();
  });
});

/**
 * The inline panel is the mode that already worked, and the one every
 * other queue spec is written against. It keeps its resize handle and
 * gains none of the overlay's chrome.
 */
test.describe('a wide window keeps the queue beside the content', () => {
  test('no scrim, no close button, and the content is narrower', async ({
    app,
  }) => {
    await app.setViewportSize({ width: 1280, height: 800 });

    const widthWithoutQueue = (await shellGeometry(app)).mainWidth;

    await openQueue(app);

    await expect(app.locator('#queue-panel')).not.toHaveAttribute(
      'overlay',
      '',
    );

    const geo = await shellGeometry(app);

    expect(geo.mainWidth).toBeLessThan(widthWithoutQueue);
    await expect(
      app.getByRole('button', { name: 'Close queue' }),
    ).toHaveCount(0);
  });
});

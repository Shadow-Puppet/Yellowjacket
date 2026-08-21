import { test, expect } from '../support/fixtures.js';

/**
 * Now Playing on a short screen (#51).
 *
 * #51 asks for a layout that "survives" ~424x439 with the controls
 * never scrolling off. #172 measured why it did not — the stacked
 * layout's budget is fixed, so the art gets whatever is left, and that
 * was 39px before #64 and 53px after it.
 *
 * **Two separate claims are asserted here, and only one of them is
 * about the phone.**
 *
 * The first is that the art is *square*. It was not: `aspect-ratio` is
 * specified not to re-derive the width when `max-height` clamps the
 * height, so the art was drawn as a letterbox band and `object-fit:
 * cover` cropped the cover to it — 264x53 on the reference device. The
 * leftover only exceeds the width above ~843px of viewport, so this
 * was every height from ~500 to ~843 as well: most phones, and any
 * short window. That is ordinary CSS rather than a Chrome 113 quirk,
 * so this tier can see it, and the heights below are chosen to cover
 * the range rather than the one device.
 *
 * The second is the reflow: below 500px the art and the names sit side
 * by side, which is what takes the art from 53px to 143px. That is
 * asserted as a *relation between boxes* — the art beside the names,
 * not above them — because the pixel count is a consequence of the
 * arrangement and would pin this file to one device's chrome.
 *
 * **What this tier cannot see** is the device's engine: CI's Chromium
 * and WebKit are current, and #60's clipping showed what that costs.
 * Nothing here depends on Chrome 113 behaviour — the sizing rules were
 * checked against the device itself, at column heights of 288, 300,
 * 451, 600 and 800, and the numbers are on #51.
 */
type Page = import('@playwright/test').Page;

/** The reference device's real viewport. */
const DEVICE = { width: 424, height: 439 };

/**
 * A tall phone, above the reflow's 500px. Roughly a Pixel 7, which is
 * #51's other named device and was not attached — so what is checked
 * here is the layout it *should* get, not that device.
 */
const TALL_PHONE = { width: 412, height: 869 };

/** Inside the crop's old range and above the reflow: a short window. */
const SHORT_WINDOW = { width: 390, height: 700 };

/**
 * The height the layout reflows at. Written down once here because the
 * specs have to know which arrangement to *wait* for, not only which
 * to assert.
 */
const REFLOW_AT = 500;

/** Put a track in the player, so the view has art and names to lay out. */
async function stageATrack(page: Page): Promise<void> {
  await page.evaluate(async () => {
    const tracks = (await window.__yjEvents.call(
      'library.Library.GetTracks',
      [0],
      10_000,
    )) as { FilePath: string }[];

    await window.__yjEvents.call(
      'queue.Queue.SetQueue',
      [tracks.slice(0, 4).map((t) => t.FilePath), 0, false, { type: '', id: 0, label: '' }],
      10_000,
    );
  });
}

/** Open the full-screen view and wait for the shell to say so. */
async function openNowPlaying(page: Page): Promise<void> {
  await page.evaluate(() => {
    document.dispatchEvent(
      new CustomEvent('navigate', {
        detail: { view: 'now-playing' },
        bubbles: true,
      }),
    );
  });

  await expect(page.getByTestId('main-content')).toHaveAttribute(
    'data-active-view',
    'now-playing',
  );

  // The attribute is the shell's bookkeeping and lands before the view
  // has a track, so measuring on it alone races the first layout --
  // which showed up as a 60x5 art on the first spec of a cold run.
  //
  // Waiting for a non-zero box is not enough on its own either: a
  // previous test leaves the *other* arrangement on screen, and a
  // stale column satisfies "has a size" perfectly. So the wait is for
  // the arrangement this viewport should have, which is the thing
  // every assertion below depends on. Found by this file passing one
  // test at a time and failing in file order.
  const wantRow = (page.viewportSize()?.height ?? 0) <= REFLOW_AT;

  await page.waitForFunction(
    (row: boolean) => {
      const v = document.querySelector('now-playing-view');
      const stack = v?.shadowRoot?.querySelector('.stack');
      const el = v?.shadowRoot?.querySelector('.art img, .art .placeholder');
      const t = v?.shadowRoot?.querySelector('.transport');

      if (!el || !t) return false;

      // A build with no `.stack` at all is the one before this change,
      // and the squareness assertions are still meaningful against it
      // -- so this waits for the arrangement only where there is one to
      // wait for. Otherwise reverting the component to check that these
      // tests bite produces eight timeouts instead of the measurements
      // that make the case.
      if (stack) {
        const dir = getComputedStyle(stack).flexDirection;

        if (dir !== (row ? 'row' : 'column')) return false;
      }

      const r = el.getBoundingClientRect();

      return r.width > 0 && r.height > 0 && t.getBoundingClientRect().height > 0;
    },
    wantRow,
  );
}

/**
 * The boxes this file reasons about, read in one evaluate.
 *
 * It reaches into the view's shadow root rather than using locators
 * because the question is geometric — where these boxes are *relative
 * to each other* — and a testid per edge would be four locators and
 * four round trips to say one thing.
 */
async function boxes(page: Page) {
  return page.evaluate(() => {
    const v = document.querySelector('now-playing-view');

    if (!v || !v.shadowRoot) return null;

    const rect = (sel: string) => {
      const el = v.shadowRoot!.querySelector(sel);

      if (!el) return null;

      const r = el.getBoundingClientRect();

      return {
        left: r.left, right: r.right, top: r.top, bottom: r.bottom,
        width: r.width, height: r.height,
      };
    };

    return {
      // Whichever of the two the track has; both carry the sizing.
      art: rect('.art img') ?? rect('.art .placeholder'),
      artBox: rect('.art'),
      stack: rect('.stack'),
      meta: rect('.meta'),
      transport: rect('.transport'),
      scrollHeight: v.scrollHeight,
      clientHeight: v.clientHeight,
    };
  });
}

test.describe('Now Playing survives a short screen', () => {
  test.beforeEach(async ({ app }) => {
    await stageATrack(app);
  });

  /**
   * The crop, at four heights spanning the range it covered. This is
   * the assertion that fails on the build before this change: at
   * 424x439 the art measured 264x53.
   */
  for (const vp of [DEVICE, SHORT_WINDOW, TALL_PHONE, { width: 900, height: 500 }]) {
    test(`draws the art square at ${vp.width}x${vp.height}`, async ({ app }) => {
      await app.setViewportSize(vp);
      await openNowPlaying(app);

      const b = await boxes(app);

      expect(b, 'now-playing-view did not mount').not.toBeNull();
      expect(b!.art, 'neither art nor placeholder rendered').not.toBeNull();

      const { width, height } = b!.art!;

      expect(width, 'the art has no width').toBeGreaterThan(0);
      // One pixel of slack for sub-pixel layout, and no more: the
      // defect this guards was a 5:1 band.
      expect(
        Math.abs(width - height),
        `art is ${Math.round(width)}x${Math.round(height)}, not square`,
      ).toBeLessThanOrEqual(1);
    });
  }

  /**
   * The promise #51 states and plan 018's matrix repeats. A floor on
   * the art with the block scrolling was the other option on #172 and
   * this is why it was not taken.
   */
  test('never scrolls the transport off the bottom', async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await openNowPlaying(app);

    const b = await boxes(app);

    expect(b!.transport!.bottom).toBeLessThanOrEqual(DEVICE.height);
    expect(
      b!.scrollHeight,
      'the view scrolls, so the transport can be moved off screen',
    ).toBeLessThanOrEqual(b!.clientHeight + 1);
  });

  /**
   * The reflow itself, as a relation rather than a measurement: below
   * 500px the names are *beside* the art, above it they are below.
   */
  test('puts the names beside the art below 500px', async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await openNowPlaying(app);

    const b = await boxes(app);

    expect(
      b!.meta!.left,
      'the names are not to the right of the art',
    ).toBeGreaterThanOrEqual(b!.artBox!.right - 1);
  });

  test('keeps the names below the art on a tall phone', async ({ app }) => {
    await app.setViewportSize(TALL_PHONE);
    await openNowPlaying(app);

    const b = await boxes(app);

    expect(
      b!.meta!.top,
      'the names are not below the art',
    ).toBeGreaterThanOrEqual(b!.artBox!.bottom - 1);
  });

  /**
   * What the reflow actually does, stated as a mechanism rather than
   * as a number: in a row the art is bounded by the row's *height*,
   * so it fills it — where in a column it is the leftover after the
   * names, which is what made it 53px.
   *
   * **The pixel count is deliberately not asserted here.** Two drafts
   * tried. The first compared the art against the column's leftover
   * computed from the boxes on screen and passed on the broken build,
   * because the subtraction goes negative when the names are taller
   * than the art — precisely the defect. The second put a floor of
   * 100px on it, passed locally at 114 and **failed in CI at 64**: this
   * app is long-lived, so a job staged by an earlier spec is still on
   * screen, and the volume control renders here where it does not on
   * Android. Both are chrome above and below this view, and both move
   * the leftover. A test that asserts how much room CI happened to
   * have is a test about the runner.
   *
   * The device numbers — 53px to 143px — are on #51, measured there,
   * which is the only tier that can honestly produce them.
   */
  test('fills the row with the art rather than the leftover', async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await openNowPlaying(app);

    const b = await boxes(app);

    expect(b!.stack, 'there is no row to fill').not.toBeNull();
    expect(
      Math.abs(b!.artBox!.height - b!.stack!.height),
      'the art does not fill the row, so it is still a leftover',
    ).toBeLessThanOrEqual(1);
  });
});

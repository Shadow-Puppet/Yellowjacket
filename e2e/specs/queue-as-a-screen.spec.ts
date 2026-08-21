import { test, expect } from '../support/fixtures.js';

/**
 * #55 — the queue is a *place* while it covers the content, and a
 * *control* while it sits beside it.
 *
 * #24 already made the pixels right: measured at the reference device's
 * 424×439, the overlaid panel is 424×318, which is `.main-panel`'s rect
 * exactly. What was missing was the navigation model, and the defect was
 * measurable in one line — opening the queue on Artists and pressing
 * back moved the page *underneath* to Albums and left the queue up. A
 * back press that changes something the user cannot see, and costs them
 * their place, is the whole of "it does not flow".
 *
 * **These assert the entry, not the attribute.** The temptation is to
 * check `#queue-button[aria-expanded]` and stop, which is the shell's
 * own bookkeeping and was right throughout the bug: what has to be true
 * is that *one* back press closes the queue and the *next* one
 * navigates. Asserting only the first would pass on a build that
 * orphans the entry, which is the defect moved one press later — the
 * same trap `back-navigation.spec.ts` documents about `data-active-view`
 * and `layout-overflow.spec.ts` set for #69.
 *
 * **Three of these nine fail on the build before #55**, and the other
 * six cannot, which is worth knowing before trusting them: "the entry
 * is not orphaned" and "the column is not in the stack" are both
 * vacuously true of a build that pushes no entry at all, and the
 * containment assertion pins the mount that was *not* taken. They guard
 * the next change rather than reproducing this one — the three that
 * reproduce it are the two back-press tests and the touch target.
 */
type Page = import('@playwright/test').Page;

/** The reference device's real viewport, not a resized desktop. */
const DEVICE = { width: 424, height: 439 };

/** Wide enough that the queue is a column: 1280 − 200 − 320 ≥ 480. */
const DESKTOP = { width: 1280, height: 800 };

const activeView = (page: Page) => page.getByTestId('main-content');
const queue = (page: Page) => page.locator('#queue-panel');
const toggle = (page: Page) => page.locator('#queue-button');

async function expectQueue(page: Page, open: boolean): Promise<void> {
  await expect(toggle(page)).toHaveAttribute(
    'aria-expanded',
    String(open),
  );
}

test.describe('the queue is a screen where it covers the content', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await app.getByTestId('tab-albums').click();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');
  });

  test('back closes the queue and leaves the page where it was', async ({
    app,
  }) => {
    await expect(queue(app)).toHaveAttribute('overlay', '');

    await toggle(app).click();
    await expectQueue(app, true);

    await app.goBack();

    await expectQueue(app, false);
    // The page underneath is untouched. Before #55 this was the
    // *previous* view, because the queue was not in the stack at all
    // and back spent an entry navigating something nobody could see.
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');
  });

  test('costs exactly one entry, so the next press navigates', async ({
    app,
  }) => {
    await toggle(app).click();
    await expectQueue(app, true);

    await app.goBack();
    await expectQueue(app, false);

    await app.goBack();

    // Whatever the launch page is, it is not Albums — the point is that
    // this press moved the app rather than being swallowed by a queue
    // that had already closed.
    await expect(activeView(app)).not.toHaveAttribute(
      'data-active-view',
      'albums',
    );
  });

  /**
   * Every route out unwinds the entry, and they do it through the
   * panel's own `open` attribute rather than each knowing about
   * history — which is why a fourth route added later gets this free.
   *
   * The failure this pins is silent: close by button, and if the entry
   * is orphaned the app looks correct until the next back press does
   * nothing at all. It is a guard rather than a reproduction — a build
   * with no entry to orphan passes it — and it is paired with the two
   * above, which do reproduce.
   */
  for (const [name, dismiss] of [
    [
      'the close button',
      async (app: Page) => {
        await app.getByRole('button', { name: 'Close queue' }).click();
      },
    ],
    [
      'Escape',
      async (app: Page) => {
        await app.keyboard.press('Escape');
      },
    ],
    [
      'the toggle it was opened from',
      async (app: Page) => {
        await toggle(app).click();
      },
    ],
  ] as Array<[string, (app: Page) => Promise<void>]>) {
    test(`${name} leaves no entry behind`, async ({ app }) => {
      await toggle(app).click();
      await expectQueue(app, true);

      await dismiss(app);
      await expectQueue(app, false);

      await app.goBack();

      await expect(activeView(app)).not.toHaveAttribute(
        'data-active-view',
        'albums',
      );
    });
  }

  /**
   * A detail view leaves the destination it was opened from lit
   * (`active-view-store`, #72), and the queue inherits that — it is
   * published with `isPrimary: false`, so `isActive('albums')` is still
   * true underneath it.
   *
   * `aria-current` rather than a class, for the reason
   * `back-navigation.spec.ts` gives: the class was right throughout the
   * bug that rule exists for.
   */
  test('leaves the tab it was opened from highlighted', async ({ app }) => {
    await expect(
      app.getByRole('button', { name: 'Albums', exact: true }),
    ).toHaveAttribute('aria-current', 'page');

    await toggle(app).click();
    await expectQueue(app, true);

    await expect(
      app.getByRole('button', { name: 'Albums', exact: true }),
    ).toHaveAttribute('aria-current', 'page');
  });

  /**
   * With the panel spanning the whole width the scrim has no uncovered
   * pixels, so the close button is the only pointer route out of a
   * full-screen surface. Measured at 424×439 before #55: **25×21px**.
   */
  test('offers a way out a thumb can hit', async ({ app }) => {
    await toggle(app).click();

    const box = await app
      .getByRole('button', { name: 'Close queue' })
      .boundingBox();

    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.height).toBeGreaterThanOrEqual(44);
  });
});

/**
 * **The mechanism, because no tier here can see the consequence.**
 *
 * #55's Direction asked for a `DETAIL_LOADERS` mount, which would put
 * the panel inside `.main-panel > *`. That box is paint-contained under
 * a `.main-panel` that is too, and `contain: paint` makes an element a
 * containing block for fixed descendants *and clips them* — which is
 * what a `wa-popup` falls back to on the reference device's Chrome 113,
 * where the Popover API does not exist (#60, `.planning/NOTES.md`).
 * `queue-panel` has a context menu, so that mount would have broken a
 * working menu on the one device this issue is about.
 *
 * CI's Chromium and WebKit both *have* the Popover API, so the menu is
 * top-layered and correct here either way: a spec asserting "the menu is
 * not clipped" is green on the broken build. What a browser can answer
 * honestly is where the element is, so that is what this asks.
 */
test('the panel stays out of the paint-contained region', async ({ app }) => {
  await app.setViewportSize(DEVICE);

  // Open, because that is the only state in which a menu can be opened
  // from it — and because the host drops `paint` from its own
  // containment deliberately in overlay mode, so a closed panel answers
  // a different question.
  await toggle(app).click();
  await expectQueue(app, true);

  const ancestry = await app.evaluate(() => {
    const chain: Array<{ tag: string; contain: string }> = [];

    for (
      let el = document.getElementById('queue-panel');
      el && el !== document.documentElement;
      el = el.parentElement
    ) {
      chain.push({
        tag: el.tagName.toLowerCase(),
        contain: getComputedStyle(el).contain,
      });
    }

    return chain;
  });

  expect(ancestry.length).toBeGreaterThan(1);
  expect(ancestry.some((a) => a.tag === 'main')).toBe(false);

  for (const { tag, contain } of ancestry) {
    expect(
      `${tag}: ${contain}`,
      'a paint-contained ancestor clips a fixed-positioned popup on Chrome 113',
    ).not.toMatch(/paint|content|strict/);
  }
});

/**
 * The column is not a place. Somebody docked it; back must not undock
 * it, and navigating to another view must not take it away.
 *
 * This is the half a viewport breakpoint would get wrong: the mode is
 * computed from the panel's own drag-resizable width, so the queue
 * becomes a screen exactly when it stops being affordable as a column.
 */
test.describe('a docked queue is not in the back stack', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DESKTOP);
  });

  test('survives a navigation, and back navigates the page', async ({
    app,
  }) => {
    await app.getByTestId('nav-albums').click();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');

    await toggle(app).click();
    await expectQueue(app, true);
    await expect(queue(app)).not.toHaveAttribute('overlay', '');

    await app.getByTestId('nav-artists').click();
    await expect(activeView(app)).toHaveAttribute('data-active-view', 'artists');
    await expectQueue(app, true);

    await app.goBack();

    await expect(activeView(app)).toHaveAttribute('data-active-view', 'albums');
    await expectQueue(app, true);
  });
});

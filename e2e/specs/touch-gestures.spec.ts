import { test, expect } from '../support/fixtures.js';

/**
 * The touch gestures against the real app (plan 019, #63; long-press
 * from plan 016 B2).
 *
 * The component tier proves the gestures in isolation, against markup
 * it built itself. What it cannot prove is the half that made this one
 * document listener instead of six: that the announced gesture reaches
 * the handler a *real* component bound — `track-list` delegates on the
 * `lit-virtualizer` rather than binding per row — and that the real
 * menu opens from it, a path with its own history of opening and then
 * refusing to work (see `menu-keyboard.spec.ts`).
 *
 * **Both halves of the reassignment are here, and the second is the
 * one that matters.** #63 makes a hold on a *track row* mean selection
 * mode; every other surface in the app keeps the context menu it has
 * had, because an unclaimed `yj-long-press` still becomes a
 * `contextmenu`. A spec that only checked the row would pass on a
 * build that had silently broken the other thirteen menus.
 *
 * The pointer events are dispatched rather than performed: this
 * project runs Desktop Chrome and Desktop Safari, neither of which has
 * touch. So this is honest about what it checks — the app's own
 * listeners, on the app's own DOM, from the events a touch would
 * produce — and not about a real finger. The finger is the Android
 * tier, and it found something this cannot see: Chrome 113's WebView
 * fires its own `contextmenu` on a long press, which is why the module
 * announces the gesture from a native event rather than standing down.
 */

/** A common small phone, as in `phone-shell.spec.ts`. */
const PHONE = { width: 390, height: 844 };

/** Comfortably past the module's 500ms hold. */
const HELD = 900;

type Page = import('@playwright/test').Page;

/** A component's menu panel, or null while it is not rendered. */
const panel = (page: Page, host: string) =>
  page.evaluate((tag) => {
    const el = document
      .querySelector(tag)
      ?.shadowRoot?.querySelector('.context-menu-panel');

    if (!el) return null;

    return {
      role: el.getAttribute('role'),
      label: el.getAttribute('aria-label'),
      items: el.querySelectorAll('[role="menuitem"]').length,
    };
  }, host);

/** How many tracks the selection bar says are selected, or null. */
const selectionCount = (page: Page) =>
  page.evaluate(() => {
    const bar = document
      .querySelector('track-list')
      ?.shadowRoot?.querySelector('selection-bar');

    return bar ? (bar as unknown as { count: number }).count : null;
  });

/**
 * Press an element, optionally dragging partway through — the shape of
 * a scroll that begins on a row, which must be neither gesture — and
 * optionally lifting, which is what makes it a tap rather than a hold.
 */
async function press(
  page: Page,
  selector: { host: string; inner: string },
  opts: { driftY?: number; lift?: boolean } = {},
): Promise<void> {
  await page.evaluate(
    ({ host, inner, drift, lift }) => {
      const el = document
        .querySelector(host)
        ?.shadowRoot?.querySelector(inner);

      if (!el) throw new Error(`no ${inner} in ${host} to press`);

      const box = el.getBoundingClientRect();
      const x = Math.round(box.left + box.width / 2);
      const y = Math.round(box.top + box.height / 2);
      const send = (type: string, dy = 0) =>
        el.dispatchEvent(
          new PointerEvent(type, {
            bubbles: true,
            composed: true,
            cancelable: true,
            pointerType: 'touch',
            isPrimary: true,
            clientX: x,
            clientY: y + dy,
          }),
        );

      send('pointerdown');

      if (drift) send('pointermove', drift);
      if (lift) send('pointerup');
    },
    {
      host: selector.host,
      inner: selector.inner,
      drift: opts.driftY ?? 0,
      lift: opts.lift ?? false,
    },
  );
}

// `.track-row`, not `[role="row"]`: the column header is a row too, and
// it is the *first* one — a press on it is correctly ignored, which
// reads exactly like the gesture not working.
const TRACK_ROW = { host: 'track-list', inner: '.track-row' };

test.describe('a hold on a track row selects it', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(PHONE);
    await app.getByTestId('tab-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
  });

  test.afterEach(async ({ app }) => {
    // Every other spec file runs against a desktop, and the viewport
    // belongs to the shared context rather than to this file.
    await app.setViewportSize({ width: 1440, height: 900 });
  });

  test('raises the selection bar rather than the context menu', async ({
    app,
  }) => {
    await expect.poll(() => selectionCount(app)).toBeNull();

    await press(app, TRACK_ROW);

    await expect
      .poll(() => selectionCount(app), { timeout: HELD + 2000 })
      .toBe(1);

    // The gesture is claimed, so the menu this hold used to open must
    // not also be up -- on a phone that would be a sheet over the bar.
    expect(await panel(app, 'track-list')).toBeNull();
  });

  test('is neither gesture when the press turns into a scroll', async ({
    app,
  }) => {
    await press(app, TRACK_ROW, { driftY: 40 });
    await app.waitForTimeout(HELD);

    expect(await selectionCount(app)).toBeNull();
    expect(await panel(app, 'track-list')).toBeNull();
  });
});

test.describe('a hold anywhere else still opens the menu', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(PHONE);
    await app.getByTestId('tab-albums').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'albums',
    );
  });

  test.afterEach(async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });
  });

  test('reaches the delegated handler and opens the real menu', async ({
    app,
  }) => {
    // The property that let #63 reassign the hold without touching one
    // of the fourteen context menus: unclaimed, it is what it was.
    // Without this half, breaking all of them passes the suite.
    await expect.poll(() => panel(app, 'cover-grid')).toBeNull();

    await press(app, { host: 'cover-grid', inner: '[role="option"]' });

    await expect
      .poll(() => panel(app, 'cover-grid'), { timeout: HELD + 2000 })
      .toMatchObject({ role: 'menu' });

    // The same panel Shift+F10 opens, items and all -- not an empty
    // popup that happened to become visible.
    expect((await panel(app, 'cover-grid'))?.items).toBeGreaterThan(0);
  });
});

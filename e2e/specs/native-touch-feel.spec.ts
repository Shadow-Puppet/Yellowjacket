import { test, expect } from '../support/fixtures.js';

/**
 * The web view's own tap highlight, and what replaced it (#54).
 *
 * Two halves, and each is here because no other tier can see it.
 *
 * **The highlight is killed by one declaration on `html`**, which
 * reaches the app's shadow roots because `-webkit-tap-highlight-color`
 * is inherited and inheritance crosses a shadow boundary. That is a
 * property of `index.css`, and `index.css` is loaded by the real app
 * and by nothing else — the component tier mounts a component with no
 * page stylesheet at all, which is the same reason the theme's ramps
 * are invisible to it.
 *
 * **The press state is measured rather than read.** The component tier
 * asserts the shape of the stylesheet (which rule is inside which
 * query, and that the press selector carries a state class), because
 * `:active` cannot be forced there. Here there is a real pointer: hold
 * the button down on a real row of the real list and read what the row
 * became. That is the assertion that would fail if the rule were
 * hoisted, renamed, or lost to `.selected`.
 *
 * What neither half is, is the device. Chrome 113's WebView is where
 * the grey box was reported and where a finger is; the numbers from it
 * are on the PR.
 */
type Page = import('@playwright/test').Page;

/** The phone this work was measured against, in CSS pixels. */
const DEVICE = { width: 424, height: 439 };

/** The computed tap-highlight colour of a node inside a shadow root. */
const tapHighlight = (page: Page, host: string, inner: string) =>
  page.evaluate(
    ([hostSel, innerSel]) => {
      const el = document
        .querySelector(hostSel!)
        ?.shadowRoot?.querySelector(innerSel!);

      if (!el) return null;

      return getComputedStyle(el).getPropertyValue(
        '-webkit-tap-highlight-color',
      );
    },
    [host, inner],
  );

test.describe('the tap highlight', () => {
  test('is transparent inside a shadow root, from one rule on html', async ({
    app,
    browserName,
  }) => {
    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );

    const row = await tapHighlight(app, 'track-list', '.track-row');

    expect(row).not.toBeNull();

    // The property is a WebKit extension that only iOS honours, so an
    // engine is free not to report one at all. Chromium always does —
    // measured at rgba(0, 0, 0, 0.18) with the rule removed, which is
    // the grey box the report describes — so the assertion is not
    // skippable there, and nothing this app can do makes the property
    // disappear on an engine that has it.
    test.skip(
      row === '',
      `${browserName} reports no -webkit-tap-highlight-color to read`,
    );

    expect(row).toBe('rgba(0, 0, 0, 0)');
  });
});

test.describe('the press state that replaced it', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await app.getByTestId('tab-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
    await expect(app.locator('track-list').first()).toBeVisible();
  });

  test.afterEach(async ({ app }) => {
    await app.mouse.up();
    await app.setViewportSize({ width: 1440, height: 900 });
  });

  test('shows on the row being pressed, and on that row only', async ({
    app,
  }) => {
    const rows = await app.evaluate(() => {
      const found = document
        .querySelector('track-list')
        ?.shadowRoot?.querySelectorAll('.track-row');

      if (!found || found.length < 2) return null;

      const rect = found[1]!.getBoundingClientRect();

      return {
        x: Math.round(rect.x + rect.width / 2),
        y: Math.round(rect.y + rect.height / 2),
      };
    });

    expect(rows).not.toBeNull();

    const backgrounds = () =>
      app.evaluate(() => {
        const found = document
          .querySelector('track-list')!
          .shadowRoot!.querySelectorAll('.track-row');

        return {
          pressed: getComputedStyle(found[1]!).backgroundColor,
          neighbour: getComputedStyle(found[2]!).backgroundColor,
        };
      });

    await app.mouse.move(rows!.x, rows!.y);
    await app.mouse.down();

    const held = await backgrounds();

    // The press overlay, from the theme rather than from a literal in
    // a component: rgba(255, 255, 255, 0.12) on both dark ramps.
    expect(held.pressed).toBe('rgba(255, 255, 255, 0.12)');
    expect(held.neighbour).not.toBe(held.pressed);

    await app.mouse.up();
  });
});

import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

import { test, expect } from '../support/fixtures.js';

/**
 * The app must work with no network.
 *
 * Every `<wa-icon>` used to resolve to ka-f.fontawesome.com at runtime
 * (audit H-4 / perf.M9), so a desktop music player offline, on a
 * captive portal or behind a firewall rendered no icons at all — while
 * playing files sitting on the local disk.  `src/icons/` bundles them
 * and overrides Web Awesome's `default` library.
 *
 * The reproduction is the point. Asserting that a `<wa-icon>` *exists*
 * would have passed before the fix too: the element is always in the
 * DOM and only its contents came from the network.  So this asserts on
 * the `<svg>` inside each icon's shadow root, with everything that is
 * not the app's own origin blocked — which is what a closed network
 * actually looks like to a local server.
 */

const here = dirname(fileURLToPath(import.meta.url));
const INIT_SCRIPT = resolve(here, '../../.playwright/init-events.js');

/** Icons on the first screen. Fewer than this means nothing rendered. */
const EXPECTED_MIN_ICONS = 5;

const VIEWS = [
  'home', 'tracks', 'albums', 'artists', 'genres', 'playlists',
  'explore', 'downloads', 'autotag', 'settings',
];

type IconState = { name: string; hasSvg: boolean };

const collectIcons = (): IconState[] => {
  const icons: IconState[] = [];

  const walk = (root: Document | ShadowRoot): void => {
    for (const el of Array.from(root.querySelectorAll('*'))) {
      if (el.tagName === 'WA-ICON') {
        icons.push({
          name: el.getAttribute('name') ?? '(unnamed)',
          hasSvg: !!el.shadowRoot?.querySelector('svg'),
        });
      }

      if (el.shadowRoot) walk(el.shadowRoot);
    }
  };

  walk(document);

  return icons;
};

test.describe('offline', () => {
  test('icons render with every external request blocked', async ({
    page,
    baseURL,
  }) => {
    const blocked: string[] = [];

    // Deliberately not `context.setOffline(true)`: the app *is* a local
    // server, so taking the whole stack down would break the bindings
    // rather than the icons.  A closed network is precisely "the app's
    // own origin still answers, nothing else does".
    await page.route('**/*', (route) => {
      const url = route.request().url();

      if (url.startsWith(baseURL!) || url.startsWith('data:')) {
        return route.continue();
      }

      blocked.push(url);

      return route.abort();
    });

    await page.addInitScript({ path: INIT_SCRIPT });
    await page.goto(baseURL!);
    await page.evaluate(() => window.__yjEvents.ready(20_000));
    await page.waitForTimeout(2000);

    const drawn = await page.evaluate(collectIcons);

    expect(
      drawn.length,
      'no <wa-icon> on screen at all — the app did not render',
    ).toBeGreaterThanOrEqual(EXPECTED_MIN_ICONS);

    expect(
      drawn.filter((i) => !i.hasSvg).map((i) => i.name),
      'icons that did not draw with the network closed',
    ).toEqual([]);

    // The complementary half: nothing should have *wanted* the network.
    // An icon drawing from a warm module cache would satisfy the
    // assertion above on a machine that happens to be online.
    expect(
      blocked.filter((u) => u.includes('fontawesome')),
      'still reaching for the icon CDN',
    ).toEqual([]);
  });

  test('every icon the app asks for is bundled', async ({ app }) => {
    for (const view of VIEWS) {
      await app.evaluate((v) => document.dispatchEvent(
        new CustomEvent('navigate', { detail: { view: v } }),
      ), view);
      await app.waitForTimeout(600);
    }

    const drawn = await app.evaluate(collectIcons);

    expect(drawn.length).toBeGreaterThan(EXPECTED_MIN_ICONS);

    // Covers the icons that only exist on views past the first screen,
    // which the offline test above never reaches.
    expect(
      drawn.filter((i) => !i.hasSvg).map((i) => i.name),
      'icons that did not draw on some view',
    ).toEqual([]);

    // The resolver records what it could not find rather than failing
    // silently, because twenty call sites compute their icon name from
    // state and no static check can enumerate them.
    const misses = await app.evaluate(
      () => (window as unknown as { __yjIconMisses?: string[] })
        .__yjIconMisses ?? [],
    );

    expect(
      misses,
      'add these to frontend/src/icons/names.txt, then run: ' +
      'node frontend/scripts/fetch-icons.mjs',
    ).toEqual([]);
  });
});

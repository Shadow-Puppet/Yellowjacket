import path from 'path';
import { playwright } from '@vitest/browser-playwright';
import { defineConfig, mergeConfig } from 'vitest/config';

import viteConfig from './vite.config.mts';

// Visual regression is opt-in. `toMatchScreenshot` baselines are
// sensitive to font hinting and GPU compositing, so a baseline taken on
// one machine fails on another for reasons that have nothing to do with
// the component. Behavioural assertions run everywhere; screenshots run
// where the baselines were taken (`make ui-visual`, or CI's container).
const screenshotsEnabled = process.env['YJ_VISUAL'] === '1';

export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      // The Playwright *runner* lives in e2e/, a separate package, so its
      // specs can never be picked up here and the two Playwright
      // versions cannot collide.
      include: ['test/**/*.test.ts'],
      setupFiles: ['./test/setup.ts'],
      globals: false,
      browser: {
        enabled: true,
        provider: playwright(),
        headless: true,
        // WebKit's Linux build wants Ubuntu libraries Arch does not
        // have, so it is a CI-only browser. See .planning/NOTES.md.
        instances: [{ browser: 'chromium' }],
        viewport: { width: 1280, height: 800 },
        screenshotFailures: false,
        expect: {
          toMatchScreenshot: {
            comparatorName: 'pixelmatch',
            comparatorOptions: { allowedMismatchedPixelRatio: 0.02 },
          },
        },
      },
      expect: { requireAssertions: true },
      env: { YJ_VISUAL: screenshotsEnabled ? '1' : '' },
    },
    resolve: {
      alias: {
        '@test': path.resolve(__dirname, 'test'),
      },
    },
    // Pre-bundle what the components pull in, or Vite discovers it
    // mid-run and reloads the page underneath a running test.
    optimizeDeps: {
      include: [
        'lit',
        'lit/decorators.js',
        'lit/directives/repeat.js',
        '@lit-labs/virtualizer',
        // Web Awesome components are deep imports, one per element;
        // the glob pre-bundles them all rather than discovering each
        // one the first time a component under test imports it.
        '@awesome.me/webawesome/dist/components/*/*.js',
        '@awesome.me/webawesome/dist/webawesome.js',
      ],
    },
  }),
);

import { defineConfig, devices } from '@playwright/test';

/**
 * These specs drive the *real* application: the Wails dev server on
 * :34115 serves the real frontend with real bindings on `window.go`,
 * bridged to the same Go backend a desktop window would use.  Nothing
 * here is mocked.
 *
 * The app is not started by Playwright.  `make dev-headless` daemonises
 * (it writes .dev/app.pid and returns), which is the opposite of what
 * `webServer` expects to supervise, and starting it per-run would also
 * mean rebuilding the frontend per-run.  globalSetup checks it is up
 * and says exactly what to run if it is not.
 *
 * WebKit is CI-only: Playwright's Linux WebKit build links Ubuntu 24.04
 * libraries that Arch does not provide, so it cannot start on a local
 * dev machine.  It is the closest available approximation of the
 * WebKit2GTK renderer we actually ship, so CI runs it and local runs
 * do not.
 */
const PORT = Number(process.env.YJ_E2E_PORT ?? 34115);

export default defineConfig({
  testDir: './specs',
  globalSetup: './support/global-setup.ts',
  // The backend is a single shared process with one SQLite database, so
  // parallel workers would fight over the same state.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list']],
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: `http://localhost:${PORT}`,
    testIdAttribute: 'data-testid',
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: process.env.CI ? 'retain-on-failure' : 'off',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    ...(process.env.YJ_E2E_WEBKIT
      ? [{ name: 'webkit', use: { ...devices['Desktop Safari'] } }]
      : []),
  ],
});

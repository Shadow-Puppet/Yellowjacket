import { test, expect } from '../support/fixtures.js';

/**
 * Plan 008 phase 3: the two sliders the audit filed as exemplary have
 * no accessible name.
 *
 * `a11y.md` lists `seek-bar` and `volume-control` under **what is
 * already correct** — "`wa-slider` with `aria-label` and a
 * `valueFormatter`, so the seek position is announced as `3:42` rather
 * than `222`". The formatter is real. The name was not: `wa-slider`
 * puts `role="slider"` on a `<div id="slider" aria-labelledby="label">`
 * inside its own shadow root, and that IDREF — pointing at an empty
 * internal `<label>` — outranks whatever `aria-label` the host carries.
 * Measured with `Accessibility.getFullAXTree` on all eleven views:
 * name `""`, every time. `volume-control` had no `aria-label` at all.
 *
 * It is here rather than only in the component tier for the reason
 * `dialog-names.spec.ts` gives: **only Playwright computes an
 * accessible name.** The Vitest tier can assert the internal label
 * carries the text and the IDREF still resolves to it; it cannot say
 * whether anything would announce it. `getByRole('slider', { name })`
 * matched nothing in this app before the fix.
 */
test.describe('a control says what it controls', () => {
  test('the seek bar is announced as Seek', async ({ app }) => {
    await expect(
      app.getByRole('slider', { name: 'Seek' }),
    ).toBeVisible();
  });

  test('the volume slider is announced as Volume', async ({ app }) => {
    // The popup renders no slider at all while closed, the same way the
    // queue panel renders no list — so this has to open it first.
    await app.getByRole('button', { name: /volume/i }).click();

    await expect(
      app.getByRole('slider', { name: 'Volume' }),
    ).toBeVisible();

    // Leave the transport as it was found: the specs share one page in
    // file order, and an open popup covers the buttons beneath it.
    await app.keyboard.press('Escape');
    await app.locator('body').click({ position: { x: 5, y: 5 } });
  });

  test('naming the slider did not move the transport', async ({ app }) => {
    // `#slider` takes an 8px margin-block-start the moment a label
    // exists, so the fix that gives it a name also grows it from 6px to
    // 14px unless the margin is put back. Nothing else in this app
    // would fail if it did — the bar would simply sit lower.
    const height = await app
      .getByRole('slider', { name: 'Seek' })
      .evaluate((el) => el.getBoundingClientRect().height);

    expect(height).toBeLessThan(10);
  });
});

import { test, expect } from '../support/fixtures.js';

/**
 * Plan 008 phase 3: `a11y.30` and `a11y.29`, the two findings that are
 * about the document itself rather than about a component.
 *
 * `<main id="main-content">` existed and nothing linked to it, so a
 * keyboard user walked the library filter, the search box, the job
 * indicator and eleven nav items before reaching content — on every
 * navigation. And `<h1>` was followed immediately by `<h3>`, using a
 * heading level for type size.
 *
 * Two things in the fix are only checkable here, because the Vitest
 * tier has no `index.html` at all:
 *
 * - **The link is out of flow in both states.** `body` is a grid with
 *   named areas, so an in-flow extra child is auto-placed into one of
 *   them and silently takes a row from the shell.
 * - **`<main>` carries `tabindex="-1"`.** A fragment link to an element
 *   that cannot hold focus moves the *scroll* and leaves the tab
 *   sequence exactly where it was, which is the whole thing the link
 *   exists to change — and it looks like it worked.
 */
test.describe('skipping to the content', () => {
  test('is the first thing Tab reaches', async ({ app }) => {
    // From the very top of the document, not from a control part-way
    // in: "first" is the claim.
    await app.evaluate(() => {
      (document.activeElement as HTMLElement | null)?.blur();
      document.body.focus();
    });
    await app.keyboard.press('Tab');

    const first = await app.evaluate(() => ({
      tag: document.activeElement?.tagName ?? '',
      text: document.activeElement?.textContent?.trim() ?? '',
    }));

    expect(first).toEqual({ tag: 'A', text: 'Skip to content' });
  });

  test('is off screen until focused, and never takes a grid cell', async ({
    app,
  }) => {
    const offscreen = await app
      .locator('.skip-link')
      .evaluate((el) => el.getBoundingClientRect().left);

    expect(offscreen).toBeLessThan(-100);

    await app.locator('.skip-link').focus();

    const box = await app
      .locator('.skip-link')
      .evaluate((el) => {
        const r = el.getBoundingClientRect();

        return { left: r.left, position: getComputedStyle(el).position };
      });

    expect(box.left).toBeGreaterThanOrEqual(0);
    expect(box.position).toBe('absolute');
  });

  test('moves focus into the main panel, not just the scroll', async ({
    app,
  }) => {
    await app.locator('.skip-link').focus();
    await app.keyboard.press('Enter');

    const landed = await app.evaluate(
      () => document.activeElement?.id ?? '',
    );

    expect(landed).toBe('main-content');

    // Leave the page as it was found: the specs share one page in file
    // order, and focus inside `main` changes which shortcut scope the
    // service resolves.
    await app.evaluate(() => {
      (document.activeElement as HTMLElement | null)?.blur();
    });
  });

  test('does not use a heading level for type size', async ({ app }) => {
    // `hgroup` takes one heading plus paragraphs, so this is also what
    // the element was supposed to contain.
    await expect(app.locator('hgroup h3')).toHaveCount(0);
    await expect(app.locator('hgroup p.subtitle')).toHaveText(
      'Music how it was meant to bee.',
    );
  });
});

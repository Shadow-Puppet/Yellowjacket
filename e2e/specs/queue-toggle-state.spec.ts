import { test, expect } from '../support/fixtures.js';

/**
 * The queue button says whether the queue is open.
 *
 * It used to look identical in both states, so the only way to tell
 * what pressing it would do was to look at the other side of the window
 * and infer it — and for anyone not looking at all there was nothing to
 * infer from: no `aria-expanded`, no `aria-controls`, no pressed state.
 *
 * The state is reflected *from the panel*, not kept beside the click,
 * because the button is not the only thing that opens the queue —
 * `now-playing-view` sets the same attribute, since it hides the bar
 * this button lives in. A flag maintained by the click handler would be
 * right until something else opened the panel and then quietly wrong,
 * which is the second test here.
 */
test.describe('the queue toggle', () => {
  test('reports open and closed, and names what it controls', async ({
    app,
  }) => {
    const toggle = app.locator('#queue-button');

    await expect(toggle).toHaveAttribute('aria-controls', 'queue-panel');
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');

    // The state is not only in the accessibility tree: a control that
    // announces a state it does not draw is half a fix.
    //
    // Background rather than colour, because the pointer is still on
    // the button after the click and `:hover` paints it the same accent
    // the open state does -- so a colour comparison here passes on the
    // broken build and proves nothing.
    const [open, closed] = await toggle.evaluate((el) => {
      const now = getComputedStyle(el).backgroundColor;

      el.setAttribute('aria-expanded', 'false');
      const shut = getComputedStyle(el).backgroundColor;

      el.setAttribute('aria-expanded', 'true');

      return [now, shut];
    });

    expect(open).not.toBe(closed);

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  });

  test('follows the panel when something else opens it', async ({ app }) => {
    const toggle = app.locator('#queue-button');

    await expect(toggle).toHaveAttribute('aria-expanded', 'false');

    // Exactly what `now-playing-view`'s queue button does.
    await app.evaluate(() =>
      document.getElementById('queue-panel')?.setAttribute('open', ''),
    );

    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  });
});

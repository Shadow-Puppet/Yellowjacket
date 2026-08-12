import {
  test,
  expect,
  callBinding,
  resetEvents,
  waitForEvent,
  LONG_TRACK,
} from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * Plan 007 phase 5: the key story, told once.
 *
 * Decision 1 keeps the unmodified single-key bindings, and until now
 * Settings was the only place they were written down — three of the
 * four categories of them, so the autotag keys were written down
 * nowhere. `?` opens the overlay from anywhere the app owns the
 * keyboard.
 *
 * The other half is the same explanation from the other side: Phase 1
 * gave the arrow keys to the grid, correctly, but all six of them —
 * and no list in this app moves horizontally, so seeking stopped
 * working from a focused row and nothing gained the keys. Reproduced
 * in the running app before the fix: two ArrowRights on a focused
 * track row produced zero `Player.Seek` calls, against one per press
 * with focus on the body.
 */
test.describe('the shortcuts overlay', () => {
  // Two things about locating a `wa-dialog`, both found here:
  //
  //   - the host is `display: contents`, so the element carrying the
  //     testid always reports hidden; what is visible is the native
  //     `<dialog>` inside its shadow root, and
  //   - that dialog has **no accessible name**. Web Awesome renders the
  //     `label` into an `<h2 id="title">` in the same shadow root and
  //     never points `aria-labelledby` at it, so every dialog in this
  //     app is an unnamed dialog to a screen reader. Not fixed here:
  //     it is eight call sites and a helper, and it is worth doing on
  //     purpose rather than as a side effect of a spec.
  const overlay = (app: Page) =>
    app.locator('shortcuts-overlay').getByRole('dialog');

  test('? opens it, and it names the keys', async ({ app }) => {
    await app.keyboard.press('?');

    await expect(overlay(app)).toBeVisible();
    // The rows are slotted, so they are in the overlay's own shadow
    // root rather than inside the native `<dialog>`.
    const content = app.locator('shortcuts-overlay');

    await expect(content).toContainText('Play / Pause');
    // Autotag's bindings are in the table Settings renders three
    // quarters of.
    await expect(content).toContainText('Apply Match');
  });

  test('Escape closes it', async ({ app }) => {
    await app.keyboard.press('?');
    await expect(overlay(app)).toBeVisible();

    // Not a toggle: a dialog owns every unmodified key while it is up,
    // so a second `?` never reaches the shortcut service. Escape is
    // what closes a dialog here, and `wa-dialog` brings it.
    await app.keyboard.press('Escape');
    await expect(overlay(app)).toBeHidden();
  });
});

test.describe('seeking from a focused track row', () => {
  test('Left and Right seek; Up and Down still move the row', async ({
    app,
  }) => {
    await app.getByTestId('nav-tracks').click();
    await callBinding(app, 'queue.Queue.Clear');
    await resetEvents(app);

    await app
      .getByTestId('track-row')
      .filter({ hasText: LONG_TRACK })
      .first()
      .dblclick();
    await waitForEvent(app, 'TrackChanged');

    // Focus the roving row, which is what took the arrows.
    await app.evaluate(() => {
      const list = document.querySelector('track-list');
      const row = list?.shadowRoot?.querySelector<HTMLElement>(
        '[role="row"][tabindex="0"]',
      );

      row?.focus();
    });

    const position = async (): Promise<number> =>
      (await callBinding(app, 'player.Player.CurrentPositionSeconds')) as number;

    const before = await position();

    for (let i = 0; i < 3; i++) await app.keyboard.press('ArrowRight');

    // Three 5 s steps, against a clock that advances 1 s per second:
    // the assertion is a jump the passage of time cannot account for.
    await expect.poll(position).toBeGreaterThan(before + 8);

    // …and the list still moves vertically, which is what the arrows
    // were given to the grid for.
    const focusedBefore = await app.evaluate(
      () =>
        (
          document.querySelector('track-list') as unknown as {
            focusedIndex: number;
          }
        ).focusedIndex,
    );

    await app.keyboard.press('ArrowDown');

    await expect
      .poll(() =>
        app.evaluate(
          () =>
            (
              document.querySelector('track-list') as unknown as {
                focusedIndex: number;
              }
            ).focusedIndex,
        ),
      )
      .toBe(focusedBefore + 1);
  });
});

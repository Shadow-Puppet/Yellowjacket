import { test, expect, callBinding, NO_QUEUE_SOURCE } from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * The bottom bar's two promises (#23, #42): the transport is centred in
 * the window, and the volume is a slider rather than a popup.
 *
 * **"Centred" is measured against the window, not against the space
 * left over**, which is the whole of #23. The bar was
 * `320px 1fr auto`, so the transport sat in the middle of what the
 * metadata and the queue button did not use — its centre was ~140px
 * right of the window's at every size, which reads as an alignment
 * mistake rather than as a layout choice.
 *
 * The mechanism is that the outer two columns are the same width, so
 * this asserts the *outcome* (centre lines up) rather than the CSS. A
 * spec that checked `grid-template-columns` would pass on any build
 * that kept the declaration and broke the result.
 */

/** Where the transport sits, against where the window's centre is. */
const geometry = (app: Page) =>
  app.evaluate(() => {
    const bar = document.querySelector<HTMLElement>('.bottom-bar')!;
    const player = document.querySelector<HTMLElement>('audio-player')!;
    const b = bar.getBoundingClientRect();
    const p = player.getBoundingClientRect();

    const seek = player.shadowRoot
      ?.querySelector('seek-bar')
      ?.shadowRoot?.querySelector('wa-slider');

    return {
      offset: Math.round(p.left + p.width / 2 - (b.left + b.width / 2)),
      barHeight: Math.round(b.height),
      seekWidth: seek ? Math.round(seek.getBoundingClientRect().width) : -1,
    };
  });

/** Something has to be playing before the transport draws a seek bar. */
async function play(app: Page): Promise<void> {
  const paths = await app.evaluate(async () => {
    const tracks = (await window.__yjEvents.call(
      'library.Library.GetTracks',
      [0],
      10_000,
    )) as { FilePath: string }[];

    return tracks.slice(0, 3).map((t) => t.FilePath);
  });

  await callBinding(app, 'queue.Queue.SetQueue', [
    paths,
    0,
    false,
    NO_QUEUE_SOURCE,
  ]);
  await callBinding(app, 'queue.Queue.Play');
  await expect(app.getByTestId('now-playing-title')).not.toBeEmpty();
}

test.describe('the bottom bar', () => {
  test.afterEach(async ({ app }) => {
    await callBinding(app, 'queue.Queue.Clear').catch(() => {
      /* already empty */
    });
    await app.setViewportSize({ width: 1440, height: 900 });
  });

  /**
   * Four widths, because a centring bug is a function of width: the old
   * layout was off by half the difference between the two outer
   * columns, so it was wrong by a different amount at each one and
   * exactly right at none.
   */
  for (const width of [800, 900, 1100, 1440]) {
    test(`centres the transport in the window at ${width}px`, async ({
      app,
    }) => {
      await app.setViewportSize({ width, height: 700 });
      await play(app);

      await expect.poll(() => geometry(app).then((g) => g.offset)).toBe(0);
    });
  }

  /**
   * The seek bar is what the centring is *paid for* with, so it is
   * asserted rather than assumed.
   *
   * Reserving the metadata's full width on both sides centres the
   * transport perfectly and squeezes the control you drag: measured
   * during this work at **61px of track at 800px**, against 257 before
   * the change. The side columns are capped at a quarter of the bar for
   * that reason, and this is the number that says so — 246 at 800px,
   * which is parity with the uncentred layout.
   */
  test('does not pay for the centring with the seek bar', async ({ app }) => {
    await app.setViewportSize({ width: 800, height: 700 });
    await play(app);

    await expect
      .poll(() => geometry(app).then((g) => g.seekWidth))
      .toBeGreaterThan(200);
  });

  /**
   * #42: the slider is simply there. Three gestures — click open, drag,
   * click closed — is what a bottom bar has room not to ask for.
   */
  test('shows the volume slider without a click', async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });

    const volume = app.locator('.bottom-bar volume-control');

    await expect(volume).toBeVisible();
    await expect(volume.locator('wa-slider')).toBeVisible();
  });

  /**
   * And the inline icon is the mute toggle, because with the slider
   * beside it there is nothing left to disclose. The name follows the
   * action rather than the state for the same reason.
   */
  test('names the inline icon after what it does', async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });

    await expect(
      app.locator('.bottom-bar volume-control').getByRole('button', {
        name: 'Mute',
      }),
    ).toBeVisible();
  });

  /**
   * The bar is a fixed 4em row and the transport sits in it. A slider
   * with a label grows `#slider` by 8px unless `wa-slider-label.css`
   * suppresses it, which moved the whole bar the last time — so the
   * height is pinned here rather than left to a screenshot.
   */
  test('stays 4em tall', async ({ app }) => {
    await app.setViewportSize({ width: 1440, height: 900 });
    await play(app);

    await expect.poll(() => geometry(app).then((g) => g.barHeight)).toBe(64);
  });
});

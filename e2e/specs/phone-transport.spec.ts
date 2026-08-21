import { test, expect } from '../support/fixtures.js';

/**
 * The phone's transport (#59, #56).
 *
 * #56 reports that "the playback controls are the most important thing
 * in the mobile app and they are tiny". Measured at the reference
 * device's 424x439 before this, every one of them was **33x21px**, and
 * the favourite beside them — which #59 keeps on the bar — was
 * **18x14px**, the smallest control in the app.
 *
 * #59 is what makes the sizes affordable: five controls plus a queue
 * button at 44px does not fit 424 CSS px, so the bar carries three and
 * the rest are on the full-screen view.
 *
 * **The assertion that matters is not the pixel count.** Plan 018's
 * matrix promises that *no action is ever unreachable at any supported
 * size*, and #59 removes three controls from the phone's bar — so the
 * first thing this file checks is that all three are still reachable,
 * by walking the route a user would. A spec that only measured the
 * survivors would be green on a build that had made shuffle
 * unreachable, which is the failure mode this pair of issues is one
 * mistake away from.
 */
type Page = import('@playwright/test').Page;

/** The reference device's real viewport. */
const DEVICE = { width: 424, height: 439 };
const PHONE = { width: 390, height: 780 };
const DESKTOP = { width: 1280, height: 800 };

/**
 * The touch-target floor. 44px is what #56's Findings name and what
 * #55's queue header was sized to, so the app has one number.
 */
const TARGET = 44;

/** The play button is named for its action, not its identity. */
const PLAY_PAUSE = /^(Play|Pause)$/;

const barControls = (page: Page) =>
  page.locator('audio-player player-controls');

/**
 * `name` may be a regex, and for play/pause it must be: that button is
 * named for the *action*, so it is "Pause" while a track runs and
 * "Play" when it stops. An exact 'Play' made these tests wait out a
 * fixture track (11.1s each, passing by luck) and would have failed
 * outright against `LONG_TRACK`. A test about a control's size does not
 * care what the transport is doing.
 */
async function sizeOf(
  page: Page,
  name: string | RegExp,
): Promise<[number, number]> {
  const box = await page
    .getByRole('button', { name, exact: typeof name === 'string' })
    .boundingBox();

  expect(box, `no button named ${name}`).not.toBeNull();

  return [box!.width, box!.height];
}

/** Put something in the queue, so the transport has a track to act on. */
async function stageATrack(page: Page): Promise<void> {
  await page.evaluate(async () => {
    const tracks = (await window.__yjEvents.call(
      'library.Library.GetTracks',
      [0],
      10_000,
    )) as { FilePath: string }[];

    await window.__yjEvents.call(
      'queue.Queue.SetQueue',
      [tracks.slice(0, 4).map((t) => t.FilePath), 0, false, { type: '', id: 0, label: '' }],
      10_000,
    );
  });
}

test.describe('the phone bar carries three controls', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await stageATrack(app);
  });

  test('drops shuffle, repeat and the queue from the bar', async ({ app }) => {
    const bar = barControls(app);

    await expect(bar.getByRole('button', { name: 'Previous track' })).toBeVisible();
    await expect(bar.getByRole('button', { name: 'Next track' })).toBeVisible();

    // Not in the bar's own subtree. Asserted against the bar rather
    // than the page, because the whole point is that they moved rather
    // than went away -- a page-wide `not.toBeVisible()` would fail the
    // moment Now Playing is open and would be asserting the wrong
    // thing besides.
    await expect(bar.getByRole('button', { name: 'Shuffle' })).toHaveCount(0);
    await expect(bar.getByRole('button', { name: /^Repeat/ })).toHaveCount(0);
    await expect(app.locator('#queue-button')).toBeHidden();
  });

  /**
   * The promise, walked. Every control #59 takes off the bar is
   * reachable from the mini player's art in one tap.
   */
  test('leaves every removed control reachable from Now Playing', async ({
    app,
  }) => {
    await app.getByTestId('open-now-playing').click();

    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'now-playing',
    );

    await expect(app.getByRole('button', { name: 'Shuffle' })).toBeVisible();
    await expect(app.getByRole('button', { name: /^Repeat/ })).toBeVisible();
    await expect(app.getByRole('button', { name: 'Show the queue' })).toBeVisible();
  });

  test('sizes what is left for a thumb', async ({ app }) => {
    for (const name of ['Previous track', 'Next track']) {
      const [w, h] = await sizeOf(app, name);

      expect(w, `${name} width`).toBeGreaterThanOrEqual(TARGET);
      expect(h, `${name} height`).toBeGreaterThanOrEqual(TARGET);
    }

    // Play is deliberately bigger than its neighbours: a row of
    // identical squares says every action is equally likely, which is
    // not true of play.
    const [pw, ph] = await sizeOf(app, PLAY_PAUSE);
    const [nw] = await sizeOf(app, 'Next track');

    expect(ph).toBeGreaterThanOrEqual(TARGET);
    expect(pw).toBeGreaterThan(nw);
  });

  /**
   * The favourite was 18x14 and is one of the three controls #59
   * keeps, so it is part of this issue rather than a nicety.
   */
  test('sizes the favourite, which was the smallest control in the app', async ({
    app,
  }) => {
    const fav = app
      .locator('now-playing')
      .getByRole('button', { name: /Favorites$/ });

    const box = await fav.boundingBox();

    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThanOrEqual(TARGET);
    expect(box!.height).toBeGreaterThanOrEqual(TARGET);
  });

  /**
   * **The route to the queue must not depend on what is playing.**
   *
   * `now-playing` renders two branches, and the no-track one had no
   * `.expand` button on its placeholder — so with nothing loaded there
   * was no way to Now Playing, and once #59 takes the queue button off
   * the bar that makes the *queue* unreachable. The queue is persisted
   * across restarts, so "tracks queued, nothing playing" is a state the
   * app launches into.
   *
   * This is asserted with the queue explicitly emptied rather than by
   * relying on the app not having played anything: `make e2e` runs one
   * long-lived app across every spec file (#168), so "no track loaded"
   * is otherwise whatever the file before this one left behind — which
   * is how the underlying fault first showed up as a flake in a spec
   * about something else.
   */
  test('reaches the queue with nothing playing', async ({ app }) => {
    await app.evaluate(async () => {
      await window.__yjEvents.call('queue.Queue.Clear', [], 10_000);
    });

    await expect(app.getByTestId('open-now-playing')).toBeVisible();

    await app.getByTestId('open-now-playing').click();
    await app.getByTestId('npv-queue').click();

    await expect(app.locator('#queue-panel')).toHaveAttribute('open', '');
  });

  test('still fits, with nothing to scroll sideways to', async ({ app }) => {
    const fit = await app.evaluate(() => ({
      scroll: document.body.scrollWidth,
      client: document.body.clientWidth,
    }));

    expect(fit.scroll).toBe(fit.client);
  });
});

test.describe('the full-screen transport is the page', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DEVICE);
    await stageATrack(app);
    await app.getByTestId('open-now-playing').click();
  });

  test('draws all five, larger than the bar draws any', async ({ app }) => {
    const [pw, ph] = await sizeOf(app, PLAY_PAUSE);

    expect(pw).toBeGreaterThanOrEqual(56);
    expect(ph).toBeGreaterThanOrEqual(56);

    for (const name of ['Shuffle', 'Previous track', 'Next track']) {
      const [w, h] = await sizeOf(app, name);

      expect(w, `${name} width`).toBeGreaterThanOrEqual(TARGET);
      expect(h, `${name} height`).toBeGreaterThanOrEqual(TARGET);
    }
  });

  test('fits at both phone widths', async ({ app }) => {
    for (const size of [DEVICE, PHONE]) {
      await app.setViewportSize(size);

      const fit = await app.evaluate(() => ({
        scroll: document.body.scrollWidth,
        client: document.body.clientWidth,
      }));

      expect(fit.scroll, `${size.width}px`).toBe(fit.client);
    }
  });
});

/**
 * **The desktop bar is not what either issue is about, and must not
 * move.** Both are `Platform/Android`; this is the guard that says so
 * in a way a build can check.
 *
 * It caught a real regression while it was being written: a generic
 * `font-size` on the buttons took them from the UA stylesheet's 13.3px
 * to the shell's 16px and grew every one from 33x21 to 36x24 — a
 * change nobody asked for, invisible to every other assertion here.
 */
test.describe('the desktop bar is untouched', () => {
  test.beforeEach(async ({ app }) => {
    await app.setViewportSize(DESKTOP);
    await stageATrack(app);
  });

  test('keeps all five controls and the queue button', async ({ app }) => {
    const bar = barControls(app);

    for (const name of ['Shuffle', 'Previous track', 'Next track']) {
      await expect(bar.getByRole('button', { name })).toBeVisible();
    }

    await expect(bar.getByRole('button', { name: /^Repeat/ })).toBeVisible();
    await expect(app.locator('#queue-button')).toBeVisible();
  });

  test('keeps them exactly the size they were', async ({ app }) => {
    const sizes = await barControls(app).evaluate((el) =>
      [...el.shadowRoot!.querySelectorAll('button')].map((b) => {
        const r = b.getBoundingClientRect();

        return `${Math.round(r.width)}x${Math.round(r.height)}`;
      }),
    );

    // Measured on `main` before this change, at 1280x800 and at 424x439
    // alike. Written down as a literal rather than as "not bigger",
    // because the regression was three pixels and a range would have
    // swallowed it.
    expect(sizes).toEqual(['33x21', '33x21', '33x21', '33x21', '33x21']);
  });
});

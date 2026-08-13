import { test, expect, callBinding, waitForEvent } from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * `a11y.15` — WCAG 2.2.2.  The bottom bar's title and artist scroll
 * continuously while a track plays, re-armed in a loop by
 * `transitionend`, with no pause mechanism and no reduced-motion guard.
 *
 * The component test for this fakes `window.matchMedia`, which is a
 * stub of the thing being tested.  This spec sets the real context
 * option, so the real media query answers.
 *
 * Both directions are here on purpose.  A guard that suppressed
 * everything would pass the reduce case for free, and so would a bar
 * whose text simply does not overflow at this viewport — which is what
 * the component test failed on first.
 */

/** The fixture track whose title is long enough to overflow the bar. */
const LONG_TITLE = 'An Exhaustively Overlong Track Title';

/**
 * Read the title line's classes from inside `now-playing`'s shadow root.
 *
 * `will-scroll` is the class that carries both the transition and the
 * `padding-right` the scroll distance is measured against, so its
 * absence is the whole fix: suppressing only the animation leaves the
 * text translated off its own box with nothing to bring it back.
 */
async function titleClasses(app: Page): Promise<string> {
  return app.evaluate(() => {
    const np = document.querySelector('now-playing');
    const title = np?.shadowRoot?.querySelector('.track-title');

    return title?.className ?? '';
  });
}

async function playTheLongOne(app: Page): Promise<void> {
  await app.getByTestId('nav-tracks').click();

  // The scroll mode defaults to `hover`, and a fresh context has no
  // persisted setting — so without this the positive case never
  // scrolls and reports the same thing a broken build would. Set it
  // rather than hovering, because `always` is also the mode the
  // finding is about: continuous motion for as long as the track
  // plays, with nothing the user has to do to provoke it.
  await app.evaluate(() => {
    localStorage.setItem('yj-now-playing-scroll-mode', 'always');
    window.dispatchEvent(new CustomEvent('yj-scroll-mode-changed'));
  });

  const paths: string[] = await app.evaluate(async (needle) => {
    const tracks = await window.__yjEvents.call(
      'library.Library.GetAllTracks',
      [],
      10_000,
    );

    return (tracks as { TrackName: string; FilePath: string }[])
      .filter((t) => t.TrackName.startsWith(needle))
      .map((t) => t.FilePath);
  }, LONG_TITLE);

  expect(paths.length).toBeGreaterThan(0);

  await callBinding(app, 'queue.Queue.SetQueue', [paths, 0, false]);
  await waitForEvent(app, 'TrackChanged');

  // The scroll cycle is armed 1500 ms after the geometry is measured,
  // and the geometry is measured after the render that puts the title
  // on screen. Reading before that reports "not scrolling" on a build
  // that scrolls — the same shape as every probe read too early in
  // plan 007.
  await expect
    .poll(() => titleClasses(app), { timeout: 10_000 })
    .toContain('track-title');
}

test.describe('the marquee under prefers-reduced-motion', () => {
  test.use({ contextOptions: { reducedMotion: 'reduce' } });

  test('does not scroll the now-playing text at all', async ({ app }) => {
    await playTheLongOne(app);

    // Give the cycle longer than the 1500 ms arming delay to prove it
    // never arms, rather than catching it before it would have.
    await app.waitForTimeout(2500);

    expect(await titleClasses(app)).not.toContain('will-scroll');
  });
});

test.describe('the marquee without a motion preference', () => {
  test.use({ contextOptions: { reducedMotion: 'no-preference' } });

  test('still scrolls an overflowing title', async ({ app }) => {
    await playTheLongOne(app);

    await expect
      .poll(() => titleClasses(app), { timeout: 10_000 })
      .toContain('will-scroll');
  });
});

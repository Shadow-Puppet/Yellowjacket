import {
  test,
  expect,
  callBinding,
  navigateTo,
  LONG_TRACK,
  NO_QUEUE_SOURCE,
} from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * The queue panel's mouse model (#43): single click selects, ctrl and
 * shift extend, double click plays from that row.
 *
 * **All four already worked, and nothing pinned any of them** — which is
 * the whole reason the report could be made and could not be settled.
 * `queue-reorder.spec.ts` covers the keyboard, `queue-overlay.spec.ts`
 * covers the panel's mode, and the component tier has the reorder
 * arithmetic; the pointer path had no coverage in either tier, so
 * "selection is broken here" and "selection is fine here" were equally
 * consistent with a green suite.
 *
 * Two things this spec is deliberately shaped around.
 *
 * **The clicks are real.** A `dispatchEvent(new MouseEvent('click'))`
 * on a row exercises the delegated handler and *not* the question being
 * asked, which is what the pointer lands on: the rows carry
 * `explore-link` names that take their own clicks, and a synthetic
 * event aimed at the row reports a selection the mouse would never have
 * produced. Every click here goes through Playwright.
 *
 * **The playing assertions use the 90-second fixture.** Every other
 * track is 2–6 seconds, so "double click plays row 3" read against a
 * 2-second track reports whatever auto-advance moved on to — measured
 * during this work as row 3 double-clicked and row 4 playing, which
 * reads exactly like an off-by-one in `PlayIndex` and is not one.
 */

/**
 * How long a click may take to show up as a highlight.
 *
 * **A poll with the default 5s timeout cannot see this defect**, and
 * that is the point of naming it. `queue-panel` repaints its rows two
 * ways — `onSelectionChanged()` calls `virtualizer.requestUpdate()`,
 * and `.keyFunction` is a per-render arrow, which is itself a changed
 * property the virtualizer reacts to. With **both** removed the
 * highlight still arrives, on whatever unrelated render happens next:
 * measured at 134ms, 3,866ms and 5,816ms for three clicks, against
 * 5ms, 16ms and 17ms on a healthy build.
 *
 * A user cannot tell "four seconds late" from "broken", which is very
 * close to what this issue reports. So the assertion is that the
 * highlight is *prompt*, with a bound ~30x the measured healthy case
 * and an order of magnitude under the degraded one.
 */
const HIGHLIGHT_MS = 500;

/** The queue's own answer, never the DOM's. */
async function playing(app: Page): Promise<{ index: number; title: string }> {
  const state = await callBinding<{
    currentIndex: number;
    tracks: { title: string }[];
  }>(app, 'queue.Queue.GetState');

  return {
    index: state.currentIndex,
    title: state.tracks[state.currentIndex]?.title ?? '',
  };
}

/** Which rows are selected, as the accessibility tree sees it. */
const selected = (app: Page) =>
  app.evaluate(() =>
    [
      ...document
        .querySelector('queue-panel')!
        .shadowRoot!.querySelectorAll('[data-index]'),
    ]
      .filter((row) => row.getAttribute('aria-selected') === 'true')
      .map((row) => Number((row as HTMLElement).dataset['index'])),
  );

/**
 * Six tracks with the long one in the middle, so a "play from here"
 * assertion has something to land on that will still be playing when it
 * is read back.
 */
async function queueSixAndOpen(app: Page): Promise<void> {
  const paths = await app.evaluate(async (longTitle) => {
    // `TrackName`, not `Title`: the library model names it after the
    // tag, and the *queue* is what calls it `title`.
    const tracks = (await window.__yjEvents.call(
      'library.Library.GetTracks',
      [0],
      10_000,
    )) as { FilePath: string; TrackName: string; Album: string }[];

    const long = tracks.find((t) => t.TrackName === longTitle);

    /**
     * **Tracks that have an album**, which is a requirement of one of
     * the tests and was previously left to luck (#156).
     *
     * `explore-link` routes a track name to its *album's* page, so a
     * track with no album renders a name that navigates nowhere — and
     * the fixture library deliberately contains two (`01 Tone A`,
     * `02 Tone B`). Which tracks arrive first is `audio_files.id`
     * order, i.e. the order the **scan** inserted them, which depends
     * on concurrency and directory traversal: locally the first eight
     * all had albums and the spec passed twice over, and CI rebuilds
     * its seed with a real scan and got a different eight.
     *
     * Asking for what the test needs is the fix. It is not a
     * narrowing: every assertion here wants an ordinary track, and
     * "the first five rows" was never a way to ask for one in a
     * library whose whole purpose is edge cases.
     */
    const rest = tracks
      .filter((t) => t.TrackName !== longTitle && t.Album !== '')
      .slice(0, 5);

    // Index 3 is the long one: far enough down that a shift-extend has
    // room either side of it.
    return [
      ...rest.slice(0, 3).map((t) => t.FilePath),
      long!.FilePath,
      ...rest.slice(3).map((t) => t.FilePath),
    ];
  }, LONG_TRACK);

  await callBinding(app, 'queue.Queue.SetQueue', [
    paths,
    0,
    false,
    NO_QUEUE_SOURCE,
  ]);

  // A closed panel renders no list at all.
  await app.locator('#queue-button').click();
  await expect(app.locator('queue-panel .track-item').first()).toBeVisible();
  await expect(app.locator('queue-panel .track-item')).toHaveCount(6);
}

/** The row at a data-index, not the nth child: see the note in the file. */
const row = (app: Page, index: number) =>
  app.locator(`queue-panel .track-item[data-index="${index}"]`);

test.describe('selecting in the queue with a mouse', () => {
  // The suite shares one backend in file order, and a queue and an open
  // panel both outlive the page. `queue-reorder.spec.ts` sets the
  // precedent and the reason: a spec that spends state fails the next
  // one, in a list that reads like a regression in whatever you hold.
  test.afterEach(async ({ app }) => {
    await callBinding(app, 'queue.Queue.Clear').catch(() => {
      /* an empty queue is the state we were asking for */
    });

    const open = await app.locator('queue-panel[open]').count();

    if (open > 0) await app.locator('#queue-button').click();
  });

  test('a single click selects that row and only that row', async ({ app }) => {
    await queueSixAndOpen(app);

    await row(app, 1).click();
    await expect
      .poll(() => selected(app), { timeout: HIGHLIGHT_MS })
      .toEqual([1]);

    // And it *replaces* rather than accumulating, which is the half a
    // test of one click cannot see.
    await row(app, 4).click();
    await expect
      .poll(() => selected(app), { timeout: HIGHLIGHT_MS })
      .toEqual([4]);
  });

  test('ctrl adds a row and shift extends a range', async ({ app }) => {
    await queueSixAndOpen(app);

    await row(app, 1).click();
    await row(app, 3).click({ modifiers: ['Control'] });
    await expect
      .poll(() => selected(app), { timeout: HIGHLIGHT_MS })
      .toEqual([1, 3]);

    // From the last row touched, so 3→5, keeping the ctrl-picked 1.
    await row(app, 5).click({ modifiers: ['Shift'] });
    await expect
      .poll(() => selected(app), { timeout: HIGHLIGHT_MS })
      .toEqual([1, 3, 4, 5]);

    // A plain click collapses the whole thing back to one.
    await row(app, 2).click();
    await expect
      .poll(() => selected(app), { timeout: HIGHLIGHT_MS })
      .toEqual([2]);
  });

  test('a double click plays from that row', async ({ app }) => {
    await queueSixAndOpen(app);

    // Row 3 is the 90-second track. Asked of the backend, because the
    // panel's own highlight is a different claim.
    await row(app, 3).dblclick();

    await expect.poll(() => playing(app)).toEqual({
      index: 3,
      title: LONG_TRACK,
    });

    // Playing is not selecting: the double click clears the selection
    // it made on the way through, or every play leaves a row looking
    // picked out for an action the user did not ask for.
    await expect.poll(() => selected(app)).toEqual([]);
  });

  /**
   * The one collision the report is actually about.
   *
   * Every track, album and artist name in the app navigates
   * (`utils/explore-link.ts`), and it does that by **stopping the
   * click's propagation** — in its own words, "the row must not also
   * treat it as a selection". So a click that lands on the name text
   * navigates and selects nothing, in the queue panel and in the track
   * list alike.
   *
   * That is deliberate and it is pinned here rather than argued with,
   * because the measurement says the queue is not the surface where it
   * hurts: a horizontal hit-scan of a row at three heights makes the
   * queue row **12%** link and the track list's row **21%** — the panel
   * the report calls broken is *less* covered by links than the list it
   * calls correct. What is left is one deliberate exception, and a
   * change to it should have to fail a test.
   */
  test('a click on a name navigates instead, and that is the exception', async ({
    app,
  }) => {
    await queueSixAndOpen(app);

    await row(app, 1).click();
    await expect
      .poll(() => selected(app), { timeout: HIGHLIGHT_MS })
      .toEqual([1]);

    // `.track-title .explore-link`, not `.explore-link` first(): a row
    // has two, and which one `first()` finds depends on whether the
    // *title* is a link at all. It is not, for a track with no album —
    // `explore-link` renders plain text where it cannot route — so the
    // loose locator silently clicked the **artist** instead and the
    // assertion below was about a different destination than the one
    // being exercised (#156).
    await row(app, 2).locator('.track-title .explore-link').click();

    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'explore-album-details',
    );

    // Row 2 did not join the selection — the link took the click.
    await expect.poll(() => selected(app)).toEqual([1]);

    await navigateTo(app, 'tracks');
  });

  /**
   * And the other half of that bargain: the link holds its navigation
   * for one double-click interval and drops it if a second click
   * arrives, so double-clicking a *name* still plays the row rather
   * than navigating away from it. That is what makes the exception
   * above survivable, and it is the part most likely to break silently
   * if the grace interval is ever removed.
   */
  test('a double click on a name plays rather than navigating', async ({
    app,
  }) => {
    await queueSixAndOpen(app);

    // Read rather than assumed: which view the app lands on is the
    // user's `DefaultPage`, so naming one here would be asserting on a
    // config value in a test about a double click.
    const before = await app
      .getByTestId('main-content')
      .getAttribute('data-active-view');

    await row(app, 3).locator('.explore-link').first().dblclick();

    await expect.poll(() => playing(app)).toEqual({
      index: 3,
      title: LONG_TRACK,
    });

    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      before!,
    );
  });
});

import type { Page } from '@playwright/test';

import {
  test,
  expect,
  resetEvents,
  callBinding,
  bindingCalls,
  NO_QUEUE_SOURCE,
} from '../support/fixtures.js';

/**
 * Finishing a track is cheap, and does not disturb the user.
 *
 * `recordPlay` used to emit `TrackMetadataChanged` — the event that
 * means "the tags on disk were rewritten" — so the library store threw
 * away its whole cache and refetched tracks, albums, artists and genres
 * once per song. Measured on a 50 000-track library that is ~37 MB
 * across the IPC and ~0.8 s of blocked main thread *per track*
 * (audit perf.C1), and `track-list` answered the new array by clearing
 * the selection, which made selecting forty tracks to drag into a
 * playlist impossible while music played (perf.C2).
 *
 * Both halves are asserted here, and both assertions are negative:
 * what makes this a fix is the work that no longer happens.
 */

/** Long enough for a fixture track (2–6 s) to finish by itself. */
const FINISH_TIMEOUT = 60_000;

/**
 * "Was anything refetched" is a fact, not an inference.
 *
 * This used to wrap every method on `window.go.library.Library` in
 * place.  v3 has no such object, and does not need one: the harness
 * bridge records every binding call off the single POST they all go
 * through, so the question is answered by reading that log rather than
 * by instrumenting a target first.  `resetEvents` clears it.
 */
const libraryCalls = async (app: Page): Promise<string[]> =>
  (await bindingCalls(app)).filter((c) => c.startsWith('library.Library.'));

/**
 * Select rows by dispatching on the row rather than clicking it.
 *
 * A real click in the middle of a row lands on the track *title*, and
 * a title navigates — that is `utils/explore-link.ts` working as
 * designed, not a bug. Which pixel selects a row is not what this spec
 * is about; that the selection survives a track change is.
 */
const selectRows = (indices: number[]): void => {
  const list = document.querySelector('track-list');
  const rows = Array.from(
    list?.shadowRoot?.querySelectorAll('[data-testid="track-row"]') ?? [],
  );

  indices.forEach((i, n) => {
    rows[i]?.dispatchEvent(new MouseEvent('click', {
      bubbles: true, composed: true, ctrlKey: n > 0,
    }));
  });
};

/** The file paths of every row currently showing as selected. */
const selectedPaths = (): string[] => Array.from(
  document.querySelectorAll('track-list'),
).flatMap((l) => Array.from(
  l.shadowRoot?.querySelectorAll('[aria-selected="true"]') ?? [],
)).map((r) => r.getAttribute('data-file-path') ?? '');

/** The first n file paths in the list. */
const firstPaths = (n: number): string[] => Array.from(
  document.querySelector('track-list')
    ?.shadowRoot?.querySelectorAll('[data-file-path]') ?? [],
).map((r) => r.getAttribute('data-file-path') ?? '').slice(0, n);

test.describe('a finished track', () => {
  test.beforeEach(async ({ app }) => {
    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('track-row').first()).toBeVisible();
  });

  test('reports a play count, not a metadata change', async ({ app }) => {
    const paths = await app.evaluate(firstPaths, 2);

    expect(paths.length).toBeGreaterThanOrEqual(2);

    await callBinding(app, 'queue.Queue.SetQueue', [paths, 0, false, NO_QUEUE_SOURCE]);
    await resetEvents(app);

    await callBinding(app, 'queue.Queue.PlayIndex', [0]);

    const event = await app.evaluate(
      (ms) => window.__yjEvents.wait('TrackPlayCountChanged', {
        timeoutMs: ms,
      }),
      FINISH_TIMEOUT,
    );

    // Enough for a refetch, if one were going to happen, to be recorded.
    await app.waitForTimeout(2500);

    const payload = (event.data as Array<Record<string, unknown>>)[0]!;

    expect(payload['filePath']).toBe(paths[0]);
    expect(typeof payload['playCount']).toBe('number');
    expect(payload['playCount']).toBeGreaterThan(0);

    const names = await app.evaluate(() => window.__yjEvents.names());

    expect(
      names['TrackMetadataChanged'] ?? 0,
      'a play emitted the retag event, which invalidates every cache',
    ).toBe(0);

    const refetched = await libraryCalls(app);

    expect(
      refetched.filter((c) => c.startsWith('library.Library.GetAll')),
      'a play refetched a collection',
    ).toEqual([]);

    await callBinding(app, 'player.Player.Pause', []);
  });

  test('leaves the track-list selection alone', async ({ app }) => {
    // Rows away from the top, so the tracks selected are not the ones
    // playing and the assertion is about the selection rather than
    // about what happens to be on screen.
    await app.evaluate(selectRows, [3, 4, 5]);
    await app.waitForTimeout(300);

    const selectedBefore = await app.evaluate(selectedPaths);

    expect(selectedBefore).toHaveLength(3);

    const paths = await app.evaluate(firstPaths, 2);

    await callBinding(app, 'queue.Queue.SetQueue', [paths, 0, false, NO_QUEUE_SOURCE]);
    await resetEvents(app);
    await callBinding(app, 'queue.Queue.PlayIndex', [0]);

    // `PlaybackFinished`, deliberately, for two reasons.  Starting
    // playback emits `TrackChanged` immediately, so waiting for that
    // would assert about a click rather than about a track *finishing*
    // — the only transition that used to clear the selection.  And
    // unlike `TrackPlayCountChanged` it exists on both sides of this
    // fix, so reverting the fix makes this spec fail by reporting a
    // cleared selection rather than by timing out on an event that was
    // never introduced.
    await app.evaluate(
      (ms) => window.__yjEvents.wait('PlaybackFinished', { timeoutMs: ms }),
      FINISH_TIMEOUT,
    );
    await app.waitForTimeout(2000);

    const selectedAfter = await app.evaluate(selectedPaths);

    expect(
      selectedAfter,
      'the selection was cleared by a track finishing',
    ).toEqual(selectedBefore);

    await callBinding(app, 'player.Player.Pause', []);
  });
});

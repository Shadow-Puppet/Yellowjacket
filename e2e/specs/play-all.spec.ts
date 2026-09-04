import { test, expect, callBinding, resetEvents, waitForEvent } from '../support/fixtures.js';

type Page = import('@playwright/test').Page;

/**
 * Play-all/Shuffle-all, asserted on what the backend queued rather than
 * on playback pixels.
 *
 * `SetQueue` reports the queue through `QueueChanged`, and `GetState`
 * says exactly what it holds: the tracks in order, whether shuffle is
 * on, and the `Source` the "Playing from" link is built from.  That is
 * the honest contract here — the buttons are only as good as the queue
 * they build, and the queue is only as good as the source it names.
 */

interface QueueState {
  tracks: { filePath: string; title: string }[];
  currentIndex: number;
  shuffleMode: boolean;
  source: { type: string; id: number; label: string };
}

const TRACKS_SOURCE = { type: 'tracks', id: 0, label: 'All Tracks' };

const getQueue = (app: Page) =>
  callBinding<QueueState>(app, 'queue.Queue.GetState');

/** The track paths a rendered track list shows, in row order. */
function displayedPaths(app: Page, scope: string): Promise<string[]> {
  return app
    .locator(`${scope} [data-testid="track-row"]`)
    .evaluateAll((els) =>
      els.map((el) => el.getAttribute('data-file-path') ?? ''),
    );
}

/** Leave shuffle in a known state. The mode persists across specs in
 *  one backend process, so a test that asserts on it has to set it. */
async function setShuffleMode(app: Page, on: boolean): Promise<void> {
  const state = await getQueue(app);

  if (state.shuffleMode !== on) {
    await resetEvents(app);
    await callBinding(app, 'queue.Queue.ToggleShuffle');
    await waitForEvent(app, 'QueueModeChanged');
  }
}

test.describe('play-all/shuffle-all on the track list', () => {
  test.beforeEach(async ({ app }) => {
    await callBinding(app, 'queue.Queue.Clear').catch(() => {
      /* the queue is clearable on every build these specs run against */
    });
    await setShuffleMode(app, false);
  });

  test('Tracks Play all queues the displayed list with an honest source', async ({
    app,
  }) => {
    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'tracks',
    );
    await expect(
      app.locator('track-list [data-testid="track-row"]').first(),
    ).toBeVisible();

    const paths = await displayedPaths(app, 'track-list');

    await resetEvents(app);
    await app.getByTestId('page-action-play-all').click();
    await waitForEvent(app, 'QueueChanged');

    const state = await getQueue(app);

    expect(state.tracks.map((t) => t.filePath)).toEqual(paths);
    expect(state.currentIndex).toBe(0);
    expect(state.shuffleMode).toBe(false);
    expect(state.source).toEqual(TRACKS_SOURCE);
  });

  test('Tracks Shuffle all turns shuffle on and keeps the source', async ({
    app,
  }) => {
    await app.getByTestId('nav-tracks').click();
    await expect(
      app.locator('track-list [data-testid="track-row"]').first(),
    ).toBeVisible();

    const paths = await displayedPaths(app, 'track-list');

    await resetEvents(app);
    await app.getByTestId('page-action-shuffle-all').click();
    await waitForEvent(app, 'QueueChanged');

    const state = await getQueue(app);

    expect(state.tracks.map((t) => t.filePath)).toEqual(paths);
    expect(state.shuffleMode).toBe(true);
    expect(state.source).toEqual(TRACKS_SOURCE);
  });
});

test.describe('play-all on an embedded track list', () => {
  test.beforeEach(async ({ app }) => {
    await callBinding(app, 'queue.Queue.Clear').catch(() => {});
    await setShuffleMode(app, false);
  });

  test('a genre page queues the genre with its name as the source', async ({
    app,
  }) => {
    await app.getByTestId('nav-genres').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'genres',
    );

    const first = app.locator('genres-view .genre-card').first();

    await expect(first).toBeVisible();
    await first.click();

    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'genre-details',
    );
    await expect(
      app.locator('genre-details [data-testid="track-row"]').first(),
    ).toBeVisible();

    const genreName = (await app
      .locator('genre-details .genre-title')
      .textContent())?.trim();
    const paths = await displayedPaths(app, 'genre-details');

    await resetEvents(app);
    await app
      .locator('genre-details [data-testid="page-action-play-all"]')
      .click();
    await waitForEvent(app, 'QueueChanged');

    const state = await getQueue(app);

    expect(state.tracks.map((t) => t.filePath)).toEqual(paths);
    expect(state.currentIndex).toBe(0);
    expect(state.source).toEqual({ type: 'genre', id: 0, label: genreName });
  });
});

test.describe('play-all on the library artist page', () => {
  test.beforeEach(async ({ app }) => {
    await callBinding(app, 'queue.Queue.Clear').catch(() => {});
    await setShuffleMode(app, false);
  });

  test('an artist page queues album paths in album order with the artist source', async ({
    app,
  }) => {
    const artists = await callBinding<{ ID: number; Name: string }[]>(
      app,
      'library.Library.GetArtists',
      [0],
    );
    const first = artists[0]!;

    await app.evaluate(
      ([id, name]) => {
        document.dispatchEvent(
          new CustomEvent('navigate', {
            detail: {
              view: 'artist-details',
              artistId: id,
              artistName: name,
            },
            bubbles: true,
            composed: true,
          }),
        );
      },
      [first.ID, first.Name] as const,
    );

    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'artist-details',
    );
    await expect(app.getByTestId('artist-play-all')).toBeEnabled();

    const albums = await callBinding<{ ID: number }[]>(
      app,
      'library.Library.GetAlbumsByArtist',
      [first.Name, 0],
    );
    const byAlbum = await callBinding<Record<string, string[]>>(
      app,
      'library.Library.GetFilePathsByAlbums',
      [albums.map((a) => a.ID), 0],
    );
    const expected: string[] = [];

    for (const album of albums) {
      expected.push(...(byAlbum[String(album.ID)] ?? []));
    }

    await resetEvents(app);
    await app.getByTestId('artist-play-all').click();
    await waitForEvent(app, 'QueueChanged');

    const state = await getQueue(app);

    expect(state.tracks.map((t) => t.filePath)).toEqual(expected);
    expect(state.source).toEqual({
      type: 'artist',
      id: first.ID,
      label: first.Name,
    });
  });
});

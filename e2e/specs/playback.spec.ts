import {
  test,
  expect,
  callBinding,
  resetEvents,
  waitForEvent,
  LONG_TRACK,
} from '../support/fixtures.js';

/** The one fixture long enough to still be playing on the next line. */
const longRow = (app: import('@playwright/test').Page) =>
  app.getByTestId('track-row').filter({ hasText: LONG_TRACK }).first();

/**
 * Playback and the queue, driven through the UI and asserted on the
 * events the backend actually emits.
 *
 * Audio really is initialised here: under `dbus-run-session` + Xvfb the
 * PulseAudio socket in /run/user is untouched, so InitSpeaker succeeds
 * and these tracks genuinely play.  A CI container without /run/user
 * needs a null sink; everything except the audio itself still works
 * without one.
 */
test.describe('playback', () => {
  test.beforeEach(async ({ app }) => {
    await callBinding(app, 'queue.Queue.Clear').catch(() => {
      /* older builds may not expose Clear; the specs below do not need it */
    });
    await resetEvents(app);
  });

  test('double-clicking a track plays it', async ({ app }) => {
    await longRow(app).dblclick();

    const changed = await waitForEvent(app, 'TrackChanged');

    expect(changed.data[0]).toBeTruthy();

    // The transport flips to Pause, which is the only place the UI
    // states "we are playing" in a way a user can see.  `exact` is not
    // optional: "Add queue to playlist" also matches /play/i.
    await expect(
      app.getByRole('button', { name: 'Pause', exact: true }),
    ).toBeVisible();

    await expect(app.getByTestId('now-playing-title')).toContainText(
      LONG_TRACK,
    );
  });

  test('the elapsed time advances', async ({ app }) => {
    await longRow(app).dblclick();
    await waitForEvent(app, 'TrackChanged');

    // Not a fixed sleep on a fixed value: assert the observable
    // outcome, which is that the clock is no longer at zero.
    await expect(app.getByTestId('elapsed-time')).not.toHaveText('--:--');
    await expect(app.getByTestId('elapsed-time')).not.toHaveText('00:00', {
      timeout: 15_000,
    });
  });

  test('pause and play round-trip through the backend', async ({ app }) => {
    await longRow(app).dblclick();
    await waitForEvent(app, 'TrackChanged');

    await resetEvents(app);
    await app.getByRole('button', { name: 'Pause', exact: true }).click();
    await waitForEvent(app, 'PlaybackStateChanged');

    await expect(
      app.getByRole('button', { name: 'Play', exact: true }),
    ).toBeVisible();
  });

  test('volume changes are pushed back from Go', async ({ app }) => {
    await resetEvents(app);
    await callBinding(app, 'player.Player.SetVolume', [55]);

    const ev = await waitForEvent(app, 'VolumeChanged');

    expect(ev.data).toEqual([55]);
  });
});

test.describe('queue', () => {
  test('playing a track populates the queue panel', async ({ app }) => {
    await resetEvents(app);
    await longRow(app).dblclick();
    await waitForEvent(app, 'QueueChanged');

    // The panel has to be open to have rows. A closed one is `width: 0`
    // and now renders no list at all (perf.m7) — before that it kept a
    // virtualizer measuring its window on every queue change, and this
    // assertion passed against a panel nobody could see.
    const queueToggle = app.getByRole('button', { name: 'Toggle queue' });

    await queueToggle.click();

    await expect(app.getByTestId('queue-row')).toHaveCount(1);

    const state = await callBinding<{ tracks: unknown[] }>(
      app,
      'queue.Queue.GetState',
    );

    expect(state.tracks).toHaveLength(1);

    // Shut it again, and wait until it really is shut. These specs
    // share one backend process in file order, the panel's width is
    // animated, and the transport slides while it closes — a click
    // issued during that lands on whichever button has moved under the
    // pointer, which for the very next test was Repeat rather than
    // Shuffle. Both emit QueueModeChanged, so it failed on the
    // assertion rather than on the wait, one run in two.
    //
    // Waiting on the row count rather than a timeout is also the m7
    // assertion: a closed panel renders no list at all.
    await queueToggle.click();
    await expect(app.getByTestId('queue-row')).toHaveCount(0);
  });

  test('shuffle and repeat toggles report their state', async ({ app }) => {
    const shuffle = app.getByRole('button', { name: 'Shuffle' });

    await resetEvents(app);
    await shuffle.click();
    await waitForEvent(app, 'QueueModeChanged');

    await expect(shuffle).toHaveAttribute('aria-pressed', 'true');

    await shuffle.click();
    await expect(shuffle).toHaveAttribute('aria-pressed', 'false');
  });
});

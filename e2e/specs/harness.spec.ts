import {
  test,
  expect,
  callBinding,
  resetEvents,
  waitForEvent,
} from '../support/fixtures.js';

/**
 * The harness testing itself.
 *
 * If these fail, every other spec's failure is uninterpretable — a
 * missing event could mean a broken feature or a broken recorder, and
 * telling those apart afterwards is expensive.
 */
test.describe('harness', () => {
  test('the app is the real app, not a mock', async ({ app }) => {
    // All 11 bound services land on window.go through the dev server.
    const services = await app.evaluate(() => Object.keys(window.go));

    expect(services).toEqual(
      expect.arrayContaining(['queue', 'player', 'library', 'explore']),
    );

    const state = await callBinding<{ tracks: unknown[] }>(
      app,
      'queue.Queue.GetState',
    );

    expect(state).toHaveProperty('tracks');
  });

  test('backend events are recorded, in order, with payloads', async ({
    app,
  }) => {
    await resetEvents(app);
    await callBinding(app, 'player.Player.SetVolume', [37]);

    const ev = await waitForEvent(app, 'VolumeChanged');

    expect(ev.data).toEqual([37]);
    expect(ev.dir).toBe('in');
  });

  test('exactly one recorder is installed', async ({ app }) => {
    // Listeners registered by one evaluate survive into the next, so a
    // recorder that re-registers counts every event twice.  This is the
    // regression test for that.
    await resetEvents(app);
    await callBinding(app, 'player.Player.SetVolume', [41]);
    await waitForEvent(app, 'VolumeChanged');

    const count = await app.evaluate(() =>
      window.__yjEvents.count('VolumeChanged'),
    );

    expect(count).toBe(1);
  });

  test('a binding called with wrong types fails fast', async ({ app }) => {
    // player.UserVolume is an int.  Passing a float makes the backend
    // log "error parsing arguments" and never fire the callback; without
    // a timeout the promise never settles and the spec hangs until the
    // suite gives up.
    const failure = await app.evaluate(async () => {
      try {
        await window.__yjEvents.call(
          'player.Player.SetVolume',
          [0.42],
          2_000,
        );

        return 'settled';
      } catch (err) {
        return (err as Error).message;
      }
    });

    expect(failure).toContain('did not settle');
  });

  test('the control surface is mounted and seeded', async ({ testctl }) => {
    const health = await testctl.health();

    expect(health.ok).toBe(true);
    expect(health.libraries.length).toBeGreaterThan(0);
    expect(health.counts.tracks).toBeGreaterThan(0);
  });
});

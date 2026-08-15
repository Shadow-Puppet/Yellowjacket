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
    // v2 landed every bound service on `window.go`, so "is this real"
    // could be asked of an object.  v3 has no such global — the
    // bindings are ordinary bundled modules — so the question is asked
    // of the runtime instead, which is a better question anyway: the
    // real runtime is loaded, and it answers for real methods and
    // refuses invented ones.
    const runtime = await app.evaluate(() => ({
      dispatch: typeof window._wails?.dispatchWailsEvent,
      client: typeof window._wails?.clientId,
    }));

    expect(runtime).toEqual({ dispatch: 'function', client: 'string' });

    const state = await callBinding<{ tracks: unknown[] }>(
      app,
      'queue.Queue.GetState',
    );

    expect(state).toHaveProperty('tracks');

    const unknown = await app
      .evaluate(() => window.__yjEvents.call('queue.Queue.Nope', [], 2_000))
      .catch((err: Error) => err.message);

    expect(unknown).toContain('unknown bound method');
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
    // player.UserVolume is an int.  Under v2 a float made the backend
    // log "error parsing arguments" and never fire the callback, so
    // this asserted that the harness's own timeout fired — the failure
    // was visible only because the harness invented a deadline.  v3
    // answers 422 with a TypeError naming the argument, so the
    // assertion is now on the backend's own words.
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

    expect(failure).toContain('TypeError');
    expect(failure).toContain('player.UserVolume');
  });

  test('the control surface is mounted and seeded', async ({ testctl }) => {
    const health = await testctl.health();

    expect(health.ok).toBe(true);
    expect(health.libraries.length).toBeGreaterThan(0);
    expect(health.counts.tracks).toBeGreaterThan(0);
  });
});

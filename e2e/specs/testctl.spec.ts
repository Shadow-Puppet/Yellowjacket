import { test, expect, resetEvents, waitForEvent } from '../support/fixtures.js';

/**
 * The dev-only control surface (backend/testctl), which exists for the
 * things a browser genuinely cannot do.
 */
test.describe('control surface', () => {
  test('database snapshot and restore round-trip', async ({ testctl }) => {
    // VACUUM INTO copies the whole file and the restore copies every
    // row back; on a database carrying an explore catalog that is tens
    // of seconds, not the default 30s budget for a whole test.
    test.setTimeout(180_000);

    const before = (await testctl.health()).counts.tracks;

    await testctl.snapshot('e2e-pristine');
    await testctl.sql('DELETE FROM audio_files');

    expect((await testctl.health()).counts.tracks).toBe(0);

    // Restore copies rows rather than files, because the app holds the
    // database open across two connection pools and cannot be made to
    // reopen it from here.
    await testctl.restore('e2e-pristine');

    expect((await testctl.health()).counts.tracks).toBe(before);
  });

  test('a forced backend event reaches the browser', async ({
    app,
    testctl,
  }) => {
    // LibraryScanProgress normally only arrives during a real scan.
    // Emitting it directly is how a push-driven view gets exercised
    // without staging the work that would produce it.
    await resetEvents(app);
    await testctl.emit('LibraryScanProgress', {
      current: 7,
      total: 31,
      currentFile: 'probe.mp3',
    });

    const ev = await waitForEvent(app, 'LibraryScanProgress');

    expect(ev.data[0]).toMatchObject({ current: 7, total: 31 });
  });

  test('sql reads return rows, writes return a count', async ({ testctl }) => {
    const read = await testctl.sql(
      'SELECT COUNT(*) AS n FROM audio_files',
    );

    expect(read.rows[0].n).toBeGreaterThan(0);

    const write = await testctl.sql(
      'UPDATE player_state SET volume = volume',
    );

    expect(write).toHaveProperty('rowsAffected');
  });

  test('bad input is rejected with a reason, not a bare status', async ({
    testctl,
  }) => {
    await expect(testctl.snapshot('../escape')).rejects.toThrow(/name must/);
    await expect(testctl.restore('nope')).rejects.toThrow(/no such snapshot/);
  });
});

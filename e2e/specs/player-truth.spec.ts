import { rename } from 'node:fs/promises';

import {
  test,
  expect,
  callBinding,
  resetEvents,
  waitForEvent,
  LONG_TRACK,
  NO_QUEUE_SOURCE,
} from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * Phase 2 of plan 007: the player tells the truth.
 *
 * Both specs here are reproductions of measured findings, written
 * before the fix and failing against the code that shipped Phase 1:
 *
 * - `H-3`: the seek bar is a local `setInterval` counter that only
 *   reconciles with the backend on a track change.  Measured 3 s behind
 *   during steady playback and **29 s** behind after four keyboard
 *   seeks, because the seek shortcut moves the backend and never tells
 *   the bar.
 * - `errors.C1`: a track whose file has moved is a silent no-op.
 *   Auto-advance onto it reverts `currentIndex`, stopping the queue
 *   dead with nothing emitted, so playback never reaches the track
 *   after it.
 */

const longRow = (app: Page) =>
  app.getByTestId('track-row').filter({ hasText: LONG_TRACK }).first();

/**
 * Read the displayed clock and the backend position in one round trip.
 *
 * Two sequential calls would measure a moving target: at 1 Hz, the
 * skew between reading the DOM and awaiting a binding is enough to
 * turn a correct player into a one-second failure.
 */
async function readClocks(
  app: Page,
): Promise<{ ui: number; backend: number }> {
  return app.evaluate(async () => {
    const deep = (root: Document | ShadowRoot): Element | null => {
      const hit = root.querySelector('[data-testid="elapsed-time"]');

      if (hit) return hit;

      for (const el of root.querySelectorAll('*')) {
        if (el.shadowRoot) {
          const nested = deep(el.shadowRoot);

          if (nested) return nested;
        }
      }

      return null;
    };

    const text = deep(document)?.textContent?.trim() ?? '';
    const [mins, secs] = text.split(':').map(Number);

    return {
      ui: Number.isFinite(mins) && Number.isFinite(secs)
        ? mins * 60 + secs
        : Number.NaN,
      backend: (await window.__yjEvents.call(
        'player.Player.CurrentPositionSeconds',
        [],
        5000,
      )) as number,
    };
  });
}

/**
 * Move focus off the track row.
 *
 * Phase 1 made rows a real grid with roving tabindex, and global
 * single-key bindings yield to a focused control that owns the key.
 * A row owns the *vertical* arrows only now (Phase 5), so Left/Right
 * seek from a focused row too and this is no longer load-bearing — it
 * stays because this spec is about the clock, not about scope
 * resolution, and it should keep measuring the same thing if that
 * rule changes again.
 */
async function blurDeepActive(app: Page): Promise<void> {
  await app.evaluate(() => {
    const deepActive = (root: Document | ShadowRoot): Element | null => {
      const active = root.activeElement;

      return active?.shadowRoot ? deepActive(active.shadowRoot) : active;
    };

    (deepActive(document) as HTMLElement | null)?.blur?.();
  });
}

test.describe('the player reports its real position', () => {
  test.beforeEach(async ({ app }) => {
    // The app lands on Home now (H-8); these specs start from a row.
    await app.getByTestId('nav-tracks').click();
    await callBinding(app, 'queue.Queue.Clear');
    await resetEvents(app);
  });

  test('the elapsed clock tracks the backend during steady playback', async ({
    app,
  }) => {
    await longRow(app).dblclick();
    await waitForEvent(app, 'TrackChanged');

    await expect
      .poll(async () => (await readClocks(app)).backend, {
        timeout: 15_000,
      })
      .toBeGreaterThan(4);

    const { ui, backend } = await readClocks(app);

    expect(Math.abs(ui - backend)).toBeLessThanOrEqual(1);
  });

  test('the elapsed clock survives four keyboard seeks', async ({ app }) => {
    await longRow(app).dblclick();
    await waitForEvent(app, 'TrackChanged');

    await blurDeepActive(app);

    for (let i = 0; i < 4; i++) {
      await app.keyboard.press('ArrowRight');
    }

    // The seek is asynchronous through the backend; give the tick that
    // reports it a chance to arrive before comparing.
    await expect
      .poll(async () => {
        const { ui, backend } = await readClocks(app);

        return Math.abs(ui - backend);
      })
      .toBeLessThanOrEqual(1);
  });
});

test.describe('a finished queue keeps its context', () => {
  test('the bar still shows what just played, at 0:00', async ({
    app,
    testctl,
  }) => {
    const { rows } = (await testctl.sql(
      "SELECT file_path FROM audio_files WHERE file_path LIKE '%Salt Air%' " +
        'LIMIT 1',
    )) as { rows: { file_path: string }[] };

    expect(rows).toHaveLength(1);

    await callBinding(app, 'queue.Queue.Clear');
    await resetEvents(app);
    await callBinding(app, 'queue.Queue.SetQueue', [
      [rows[0].file_path],
      0,
      false,
      NO_QUEUE_SOURCE,
    ]);
    await waitForEvent(app, 'QueueChanged');
    await callBinding(app, 'queue.Queue.Play');

    await waitForEvent(app, 'PlaybackFinished', { timeoutMs: 30_000 });

    // H-18: the bar used to blank completely while the queue panel
    // still listed the track that had just played.
    await expect(app.getByTestId('now-playing-title')).toContainText(
      'Salt Air',
    );
    await expect(app.getByTestId('elapsed-time')).toHaveText('00:00');
  });
});

test.describe('a track that will not play says so', () => {
  test('auto-advance skips a missing file and reaches the next track', async ({
    app,
    testctl,
  }) => {
    const { rows } = (await testctl.sql(
      "SELECT file_path FROM audio_files WHERE file_path LIKE '%Glass Harbour%' " +
        'ORDER BY file_path LIMIT 3',
    )) as { rows: { file_path: string }[] };

    expect(rows).toHaveLength(3);

    const paths = rows.map((r) => r.file_path);
    const missing = paths[1];
    const hidden = `${missing}.e2e-hidden`;

    test.setTimeout(90_000);

    await rename(missing, hidden);

    try {
      await callBinding(app, 'queue.Queue.Clear');
      await resetEvents(app);
      await callBinding(app, 'queue.Queue.SetQueue', [paths, 0, false, NO_QUEUE_SOURCE]);
      await waitForEvent(app, 'QueueChanged');
      await callBinding(app, 'queue.Queue.Play');

      // The first fixture is ~6 s long; the whole hop through the bad
      // file and onto the third track has to happen inside that plus
      // the third track's own length.
      const failed = await waitForEvent(app, 'PlaybackFailed', {
        timeoutMs: 30_000,
      });

      expect(failed.data[0]).toMatchObject({ filePath: missing });

      await expect
        .poll(
          async () =>
            (
              await callBinding<{ currentIndex: number }>(
                app,
                'queue.Queue.GetState',
              )
            ).currentIndex,
          { timeout: 30_000 },
        )
        .toBe(2);

      // Not just skipped — said so.  A failure the user cannot see is
      // the finding, not the fix.
      await expect(app.getByTestId('player-message')).toContainText(
        /could not (be )?play/i,
      );
    } finally {
      await rename(hidden, missing);
    }
  });
});

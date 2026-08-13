import { existsSync } from 'node:fs';

import { test, expect, resetEvents, callBinding, eventNames } from '../support/fixtures.js';

/**
 * "Remove from library" removes the row and leaves the file.
 *
 * Two assertions carry this spec and neither is about the row count.
 * The first is that the **file is still on disk** — that is the promise
 * the confirmation copy makes, and the only thing standing between this
 * feature and a user's music. The second is that a **real scan does not
 * bring the row back**: without the exclusion the operation undoes
 * itself on the next scan, which is worse than not having it.
 *
 * The suite shares one backend process in file order, so this restores
 * the database it spent.
 */
const SNAPSHOT = 'e2e-pre-remove';

/** The file paths of the first n rows, in list order. */
const firstPaths = (n: number): string[] => Array.from(
  document.querySelector('track-list')
    ?.shadowRoot?.querySelectorAll('[data-file-path]') ?? [],
).map((r) => r.getAttribute('data-file-path') ?? '').slice(0, n);

test.describe('remove from library', () => {
  test.beforeAll(async ({ baseURL }) => {
    // VACUUM INTO copies the whole file and the restore copies every row
    // back, which is well over the 30 s a hook gets by default once
    // earlier specs have staged an explore catalog.
    test.setTimeout(180_000);

    const res = await fetch(`${baseURL}/__test/db/snapshot?name=${SNAPSHOT}`, {
      method: 'POST',
      signal: AbortSignal.timeout(120_000),
    });

    expect(res.ok, 'could not snapshot the database before spending it').toBe(true);
  });

  test.afterAll(async ({ baseURL }) => {
    test.setTimeout(180_000);

    const res = await fetch(`${baseURL}/__test/db/restore?name=${SNAPSHOT}`, {
      method: 'POST',
      signal: AbortSignal.timeout(120_000),
    });

    expect(res.ok, 'could not restore the database this spec spent').toBe(true);
  });

  test.beforeEach(async ({ app }) => {
    await app.getByTestId('nav-tracks').click();
    await expect(app.getByTestId('track-row').first()).toBeVisible();
  });

  /**
   * Delete is bound to *opening* the confirmation and to nothing else.
   * A key that asks is defensible one row from the user's music; a key
   * that acts is not.
   */
  test('Delete asks, and cancelling is a true no-op', async ({ app }) => {
    const before = await app.getByTestId('track-row').count();

    await resetEvents(app);
    await app.evaluate(() => {
      const rows = document.querySelector('track-list')
        ?.shadowRoot?.querySelectorAll('[data-testid="track-row"]');

      rows?.[2]?.dispatchEvent(new MouseEvent('click', {
        bubbles: true, composed: true,
      }));
    });

    await app.keyboard.press('Delete');

    const dialog = app.getByRole('dialog', { name: /from the library\?/ });

    await expect(dialog).toBeVisible();
    // The copy is the user's only protection, so it is asserted rather
    // than assumed: it has to say the file is not deleted.
    await expect(app.getByTestId('confirm-dialog')).toContainText(
      /not deleted/,
    );

    await app.getByTestId('confirm-cancel').click();
    await expect(dialog).toBeHidden();

    expect(await app.getByTestId('track-row').count()).toBe(before);
    expect((await eventNames(app))['TracksRemovedFromLibrary'] ?? 0).toBe(0);
  });

  test('confirming removes the row, keeps the file, and survives a scan', async ({
    app,
  }) => {
    test.setTimeout(120_000);

    const [target, control] = await app.evaluate(firstPaths, 2);

    expect(target, 'no tracks in the library to remove').toBeTruthy();
    expect(existsSync(target!), 'fixture file missing before the test').toBe(true);

    const before = await app.getByTestId('track-row').count();

    await resetEvents(app);
    await app.getByTestId('track-row').first().click({ button: 'right' });
    await app.getByRole('menuitem', { name: 'Remove from Library' }).click();
    await app.getByTestId('confirm-accept').click();

    const removed = await app.evaluate(
      () => window.__yjEvents.wait('TracksRemovedFromLibrary', {
        timeoutMs: 15_000,
      }),
    );

    expect((removed.data as Array<Record<string, unknown>>)[0]).toMatchObject({
      filePaths: [target],
      count: 1,
    });

    await expect(app.getByTestId('track-row')).toHaveCount(before - 1);

    // The promise the copy makes.
    expect(existsSync(target!), 'the file was deleted from disk').toBe(true);

    // And the half that makes the rest true: a real scan of the real
    // directory must not import it again.
    await resetEvents(app);
    await callBinding(app, 'library.Library.ScanAllLibraries', []);
    await app.evaluate(
      () => window.__yjEvents.wait('LibraryScanComplete', { timeoutMs: 90_000 }),
    );
    await app.waitForTimeout(1000);

    const paths = await app.evaluate(
      () => Array.from(
        document.querySelector('track-list')
          ?.shadowRoot?.querySelectorAll('[data-file-path]') ?? [],
      ).map((r) => r.getAttribute('data-file-path') ?? ''),
    );

    expect(paths, 'the excluded path came back on the next scan')
      .not.toContain(target);
    // The positive half: a guard that excluded everything would pass
    // the assertion above for free.
    expect(paths, 'the scan lost a path nobody excluded').toContain(control);
    expect(existsSync(target!), 'the file was deleted from disk').toBe(true);
  });
});

import { test, expect } from '../support/fixtures.js';
import type { Page } from '@playwright/test';

/**
 * Plan 007 phase 5: expanding an album shows its tracks.
 *
 * `perf.p2` files `cover-grid`'s `renderSplitGrid` as dead code carried
 * in the bundle. It was a **missing feature** whose data path already
 * worked: Enter on an album card fetched the album's tracks over the
 * IPC and ran the whole split state machine, and then `render()` drew
 * the single grid regardless because it never consulted `splitMode`.
 *
 * This spec is here rather than only in the component tier because two
 * of the three things that had to be true are about the real app: that
 * the route from a card to `track-details` exists at all (a plain click
 * navigates to the catalog page instead, so the dropdown is the only
 * one), and that the grid keeps its scroll position when the dropdown
 * opens — which it did not until the scroll container was given an
 * overflow, having never scrolled in its life.
 */
test.describe('the album dropdown', () => {
  test.beforeEach(async ({ app }) => {
    await app.getByTestId('nav-albums').click();
    await expect(app.getByTestId('main-content')).toHaveAttribute(
      'data-active-view',
      'albums',
    );
  });

  test.afterEach(async ({ app }) => {
    // The suite shares one backend process in file order, and an open
    // dropdown changes what the next spec's selectors match.
    await closeDropdown(app);
    await app.getByTestId('nav-tracks').click();
  });

  test('Enter on a card draws that album’s tracks', async ({ app }) => {
    await expandCard(app, 1);

    await expect
      .poll(() => dropdownState(app))
      .toMatchObject({ present: true, split: true });

    const state = await dropdownState(app);

    expect(state.rows).toBeGreaterThan(0);
    expect(state.rows).toBe(state.tracks);
  });

  test('a track in it reaches Track Details', async ({ app }) => {
    // The only route from the albums grid to a track. A plain click on
    // a card navigates to `explore-album-details` instead.
    await expandCard(app, 1);
    await expect.poll(() => dropdownState(app)).toMatchObject({
      present: true,
    });

    await app.evaluate(() => {
      document
        .querySelector('cover-grid')
        ?.shadowRoot?.querySelector('album-dropdown')
        ?.shadowRoot?.querySelector('.track-row')
        ?.dispatchEvent(
          new MouseEvent('contextmenu', {
            bubbles: true,
            composed: true,
            clientX: 300,
            clientY: 400,
          }),
        );
    });

    // The panel is shared with the album menu and used to be labelled
    // "Album actions" unconditionally — which nothing could observe
    // while the only menu that could open on a track was unreachable.
    await expect(app.getByRole('menu', { name: 'Track actions' }))
      .toBeVisible();

    await app.getByRole('menuitem', { name: 'Track Details' }).click();

    await expect(
      app.getByRole('dialog', { name: 'Track Details' }),
    ).toBeVisible();

    await app.keyboard.press('Escape');
    await expect(
      app.getByRole('dialog', { name: 'Track Details' }),
    ).toHaveCount(0);
  });

  test('the grid it opens in can be scrolled', async ({ app }) => {
    // `cover-grid` carried the same `.grid-scroll-container` markup as
    // `artists-view` with no rule for the class, so it never scrolled:
    // the container grew to its full content height inside an
    // `overflow: hidden` host and everything past the first screenful
    // was unreachable. Invisible on eight albums, fatal on a real
    // library — measured at 5 000 albums, 186 984 px of content in a
    // 772 px box.
    //
    // The fixture does not scroll at the default viewport, so this
    // shrinks the window until it does. Without that, every form of
    // this assertion passes against a scrollTop that is 0 both times
    // and could not have moved.
    await app.setViewportSize({ width: 900, height: 600 });

    try {
      await expect.poll(() => scrollRange(app)).toMatchObject({
        scrollable: true,
        overflowY: 'auto',
      });

      await app.evaluate(() => {
        const sc = document
          .querySelector('cover-grid')
          ?.shadowRoot?.querySelector('.grid-scroll-container');

        if (sc) sc.scrollTop = 80;
      });

      expect(await scrollTop(app)).toBe(80);

      // And the dropdown it opens is on screen, wherever the manager
      // decides that leaves the scroll. It is *not* "the position is
      // preserved": `scrollToShowDropdown` deliberately moves it to
      // reveal the dropdown, which on a library this small is most of
      // the way back to the top (80 → 4, with the content *taller*
      // than before, so it is not clamping). At 5 000 albums, with the
      // expanded card mid-viewport, the same code preserved 2891
      // exactly.
      await expandCard(app, 1);
      await expect.poll(() => dropdownState(app)).toMatchObject({
        present: true,
      });

      await expect.poll(() => dropdownOnScreen(app)).toBe(true);
    } finally {
      await app.setViewportSize({ width: 1440, height: 900 });
    }
  });

  test('the arrow keys still move by a row across the split', async ({
    app,
  }) => {
    // The dropdown draws two virtualizers where there was one, and the
    // roving tab stop indexes the whole album list rather than either
    // half. Home and End have to cross the dropdown, and ArrowDown has
    // to move by a row rather than to the end — which it did not, in
    // any of these grids, because `offsetTop` inside a virtualizer is
    // always 0 and every rendered card counted as one row.
    await app.setViewportSize({ width: 700, height: 700 });

    try {
      await expandCard(app, 1);
      await expect.poll(() => dropdownState(app)).toMatchObject({
        present: true,
        split: true,
      });

      const moves = await app.evaluate(async () => {
        const grid = document.querySelector('cover-grid');
        const root = grid?.shadowRoot;
        const container = root?.querySelector('.grid-scroll-container');
        const cards = () =>
          [...(root?.querySelectorAll<HTMLElement>('.album-card') ?? [])];
        const at = () => {
          const active = root?.activeElement as HTMLElement | null;

          return active ? Number(active.dataset['index']) : null;
        };
        const press = async (key: string) => {
          container?.dispatchEvent(
            new KeyboardEvent('keydown', {
              key,
              bubbles: true,
              composed: true,
            }),
          );
          await new Promise((r) => setTimeout(r, 400));

          return at();
        };

        cards()[0]?.focus();

        const columns = cards().filter(
          (c) =>
            Math.round(c.getBoundingClientRect().top) ===
            Math.round(cards()[0]!.getBoundingClientRect().top),
        ).length;
        const down = await press('ArrowDown');
        const end = await press('End');
        const home = await press('Home');

        return { columns, down, end, home, last: cards().length - 1 };
      });

      expect(moves.columns).toBeGreaterThan(1);
      expect(moves.down).toBe(moves.columns);
      expect(moves.end).toBe(moves.last);
      expect(moves.home).toBe(0);
    } finally {
      await app.setViewportSize({ width: 1440, height: 900 });
    }
  });
});

/** Focus a card and press Enter, which is the only thing that expands one. */
async function expandCard(app: Page, index: number): Promise<void> {
  // The cards come from a virtualizer, so they are not there when the
  // view is: a keydown dispatched into an empty grid hits nothing, and
  // what fails is the *poll* several lines later, which reads as the
  // dropdown being broken rather than as a race. This spec flaked on
  // roughly one run in two on main without this wait.
  await expect.poll(() => cardCount(app)).toBeGreaterThan(index);

  await app.evaluate((i) => {
    const card = document
      .querySelector('cover-grid')
      ?.shadowRoot?.querySelectorAll<HTMLElement>('.album-card')[i];

    card?.focus();
    card?.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Enter',
        bubbles: true,
        composed: true,
      }),
    );
  }, index);
}

async function cardCount(app: Page): Promise<number> {
  return app.evaluate(
    () =>
      document
        .querySelector('cover-grid')
        ?.shadowRoot?.querySelectorAll('.album-card').length ?? 0,
  );
}

async function closeDropdown(app: Page): Promise<void> {
  await app.evaluate(() => {
    const grid = document.querySelector('cover-grid') as
      | (Element & { expandedAlbumId: number | null })
      | null;

    if (grid) grid.expandedAlbumId = null;
  });
}

/** Whether the grid can scroll at all, which decides if a probe can move. */
async function scrollRange(app: Page) {
  return app.evaluate(() => {
    const sc = document
      .querySelector('cover-grid')
      ?.shadowRoot?.querySelector('.grid-scroll-container');

    return {
      scrollable: !!sc && sc.scrollHeight > sc.clientHeight + 40,
      overflowY: sc ? getComputedStyle(sc).overflowY : '',
    };
  });
}

async function scrollTop(app: Page): Promise<number> {
  return app.evaluate(
    () =>
      document
        .querySelector('cover-grid')
        ?.shadowRoot?.querySelector('.grid-scroll-container')?.scrollTop ?? -1,
  );
}

/** Whether the open dropdown is inside the scroll container's viewport. */
async function dropdownOnScreen(app: Page): Promise<boolean> {
  return app.evaluate(() => {
    const grid = document.querySelector('cover-grid');
    const sc = grid?.shadowRoot?.querySelector('.grid-scroll-container');
    const dd = grid?.shadowRoot?.querySelector('album-dropdown');

    if (!sc || !dd) return false;

    const box = sc.getBoundingClientRect();
    const it = dd.getBoundingClientRect();

    return it.bottom > box.top && it.top < box.bottom;
  });
}

async function dropdownState(app: Page) {
  return app.evaluate(() => {
    const grid = document.querySelector('cover-grid') as
      | (Element & { splitMode: boolean; expandedTracks: unknown[] })
      | null;
    const dropdown = grid?.shadowRoot?.querySelector('album-dropdown');

    return {
      present: !!dropdown,
      split: grid?.splitMode ?? false,
      tracks: grid?.expandedTracks?.length ?? 0,
      rows: dropdown?.shadowRoot?.querySelectorAll('.track-row').length ?? 0,
    };
  });
}

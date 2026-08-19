/**
 * The icon vocabulary is one table, and nothing writes around it.
 *
 * A wrong-but-real icon name renders perfectly: no error, no fallback,
 * no failing assertion anywhere. That is how `plus` came to mean "add
 * to the queue", "add to a playlist", "make a new playlist" and "you do
 * not own this" — the first two adjacent in the same context menu —
 * while `list` meant the queue, the Playlists destination *and* adding
 * to the queue.
 *
 * `src/icons/index.ts` catches a name that is not *bundled*. Nothing
 * catches a name that is bundled and means something else, so this
 * sweeps the source for the governed ones. It is the same shape as
 * `TestNoDirectRuntimeEmits` and `TestNoWritesOnTheReadPool` in the
 * backend, and exists for the same reason: the rule is about every call
 * site, so checking one is checking nothing.
 */
import { describe, expect, it } from 'vitest';

import { bundledIconNames } from '../../src/icons';
import * as icons from '@utils/icon-language';

/** Every component source, as text. */
const SOURCES = import.meta.glob<string>('../../src/**/*.ts', {
  eager: true,
  query: '?raw',
  import: 'default',
});

/**
 * The names that carry a meaning the table owns.
 *
 * Deliberately not every bundled name. `check` is `ICON_IN_LIBRARY`
 * here and also the "Copied" confirmation in `job-log-view`, which is
 * a different, perfectly good meaning — governing it would force a
 * false rename. What belongs on this list is a name that was actually
 * overloaded.
 */
const GOVERNED = [
  'plus',
  'list',
  'bookmark',
  'solid/bookmark',
  'regular/bookmark',
  'bars-staggered',
  'tag',
  'filter',
  'ellipsis',
];

/** The one file allowed to say them, plus its own test. */
const DEFINITION = /icon-language\.(ts|test\.ts)$/;

describe('the icon vocabulary', () => {
  /**
   * A sweep over nothing passes. This is the assertion that makes the
   * rest of the file mean something, and it is the first thing that
   * breaks if the glob pattern stops matching after a move.
   */
  it('actually reads the source', () => {
    const paths = Object.keys(SOURCES);

    expect(paths.length).toBeGreaterThan(100);
    expect(paths.some((p) => p.endsWith('/track-list.ts'))).toBe(true);
    expect(SOURCES[paths[0]!]).toContain('import');
  });

  it.each(GOVERNED)('is not written around for %s', (name) => {
    const offenders: string[] = [];

    for (const [path, source] of Object.entries(SOURCES)) {
      if (DEFINITION.test(path)) continue;

      // Both spellings: an icon in a template, and an icon name in a
      // data table (which is how the sidebar and bottom-nav carry
      // theirs).
      const literal = new RegExp(
        `(name="${name}"|icon: '${name}'|name=\\$\\{[^}]*'${name}')`,
      );

      if (literal.test(source)) offenders.push(path);
    }

    expect(offenders).toEqual([]);
  });

  /**
   * A meaning with no icon behind it is the state the badge's `queued`
   * spent a year in — declared, styled, and produced by nothing.
   */
  it('gives every meaning a name', () => {
    const values = Object.entries(icons).filter(([k]) => k.startsWith('ICON_'));

    expect(values.length).toBeGreaterThan(0);

    for (const [key, value] of values) {
      expect(`${key}=${value}`).toMatch(/^ICON_[A-Z_]+=[a-z]+[a-z/-]*$/);
    }
  });

  /**
   * Every name in the table is a name the app actually ships.
   *
   * This is the loop the vocabulary closes. A name that is not bundled
   * renders a circled question mark and reports itself to
   * `__yjIconMisses` — at *runtime*, from a state something has to
   * reach first. `bookmark-check` is Font Awesome **Pro**, and it was
   * on `explore-artist-details`'s Follow button, drawn for every
   * followed artist, invisible to `offline-icons.spec.ts` because no
   * spec had ever followed one. Reaching the state is no longer how
   * this is found.
   */
  it('names only icons that are bundled', () => {
    const bundled = new Set(bundledIconNames());
    const missing = Object.entries(icons)
      .filter(([k]) => k.startsWith('ICON_'))
      .filter(([, v]) => !bundled.has(v as string))
      .map(([k, v]) => `${k} (${v})`);

    expect(missing).toEqual([]);
  });

  /**
   * The two states of the request toggle have to be the same glyph in
   * two weights, or they do not read as each other's opposite — which
   * is what a plus against a bookmark was.
   */
  it('makes the request toggle an outline/solid pair', () => {
    expect(icons.ICON_CAN_REQUEST).toBe(`regular/${icons.ICON_REQUESTED.replace('solid/', '')}`);
  });

  /**
   * The queue and the Playlists destination wore the same icon, and
   * "add to queue" and "add to playlist" sat next to each other wearing
   * a third same one. Whatever the table says, these three have to
   * differ from each other.
   */
  it('keeps the queue, playlists and creating something apart', () => {
    const three = [icons.ICON_QUEUE, icons.ICON_PLAYLIST, icons.ICON_NEW];

    expect(new Set(three).size).toBe(3);
  });
});

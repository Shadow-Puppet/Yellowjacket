/**
 * Settings offers the columns the backend will accept, and no others.
 *
 * The list is built from `COLUMN_DEFS`, which is the *drawing* table:
 * every definition the track list knows how to render, including
 * `titleArtist` — the phone's stacked column, chosen by width in
 * `PHONE_COLUMN_IDS` and never by a person. `tracklist.AllColumnIDs` in
 * Go does not list that id, so the configurator offered a nineteenth
 * row that could not be ticked:
 *
 * ```
 * validate = unknown track-list column ID: "titleArtist"
 * titleArtist valid = false
 * ```
 *
 * What a user saw was **two rows both called "Track Name"** (#197), one
 * of which did nothing — and a screen reader heard "Show the Track Name
 * column" twice with nothing to tell them apart, which is `a11y.32`'s
 * complaint inside the list that was fixed for exactly that.
 *
 * It is worse than an inert control, which is why the duplicate name
 * was not the thing to fix. `SetTrackListColumns` assigns before it
 * validates, so a rejected list stays in memory and `Save()` validates
 * the whole config:
 *
 * ```
 * later, unrelated SetThemeAccentColor = could not save config: invalid
 *   config: ... unknown track-list column ID: "titleArtist"
 * ```
 *
 * — one tick and no setting saves for the rest of the session. That
 * half is filed separately; this file keeps the row from being offered.
 *
 * The last test is the one that would have caught it when the column
 * was added: the two lists are in different languages, so nothing but a
 * sweep can hold them together.
 */
import { beforeEach, describe, expect, it } from 'vitest';

import '@components/config-page/config-page';

import {
  COLUMN_DEFS,
  CONFIGURABLE_COLUMN_IDS,
} from '@components/track-list/columns';
import { flush, stub } from '@test/support/harness';
import { fixture, shadowAll } from '@test/support/render';

/** Go's own list of column ids, as text. */
const GO_CONFIG = Object.values(
  import.meta.glob<string>('../../../backend/tracklist/config.go', {
    eager: true,
    query: '?raw',
    import: 'default',
  }),
)[0];

/**
 * The ids `tracklist.AllColumnIDs` actually contains.
 *
 * Read out of the source rather than written down here, because a
 * third copy of this list is a third thing to forget — which is the
 * defect, one copy earlier.
 */
function goColumnIDs(source: string): string[] {
  const constants = new Map<string, string>();
  const constBlock = /const \(([\s\S]*?)\n\)/.exec(source)?.[1] ?? '';

  for (const [, name, id] of constBlock.matchAll(
    /(\w+)\s+ColumnID\s*=\s*"([^"]+)"/g,
  )) {
    constants.set(name!, id!);
  }

  const listBlock =
    /var AllColumnIDs = \[\]ColumnID\{([\s\S]*?)\n\}/.exec(source)?.[1] ?? '';

  return [...listBlock.matchAll(/(\w+),/g)]
    .map(([, name]) => constants.get(name!))
    .filter((id): id is string => id !== undefined);
}

/**
 * The column rows, and only those.
 *
 * Settings’ view-visibility list (#25) is drawn with the same two
 * classes, so a bare `.column-label` sweeps 29 rows across two
 * sections — and "Albums" the destination sitting beside "Album" the
 * column is not the fault this file is about. The `for`/`id` prefix is
 * what tells them apart.
 */
const COLUMN_ROW_LABEL = 'label.column-label[for^="column-"]';
const COLUMN_ROW_BOX = 'input.column-toggle[id^="column-"]';

/** The rows the configurator draws, by their visible name. */
async function columnRowNames(): Promise<string[]> {
  const page = await fixture('config-page');

  await flush();
  await page.updateComplete;

  // Every section renders collapsed, and a collapsed body is `hidden`.
  for (const section of shadowAll<HTMLElement>(page, 'config-section')) {
    section.shadowRoot
      ?.querySelector<HTMLButtonElement>('button[aria-expanded="false"]')
      ?.click();
  }

  await flush();
  await page.updateComplete;

  return shadowAll<HTMLElement>(page, COLUMN_ROW_LABEL).map(
    (label) => label.textContent?.trim() ?? '',
  );
}

describe('the Settings column list', () => {
  beforeEach(() => {
    for (const path of [
      'library.Library.GetAllLibrariesWithTrackCounts',
      'jobs.Service.GetJobs',
      'download.Service.ListProviders',
      'download.Service.ProviderKinds',
    ]) {
      stub(path, []);
    }

    stub('config.Config.GetShortcuts', {});
    stub('config.Config.GetDownloadPreferences', {});
    stub('config.Config.GetThemeAccentColor', '#ffd43b');
    stub('config.Config.GetThemeBackgroundShade', 'dark');
  });

  it('names each row once', async () => {
    const names = await columnRowNames();

    // A sweep over nothing passes.
    expect(names.length, 'the page draws column rows').toBeGreaterThan(5);

    const seen = new Set<string>();
    const duplicated = names.filter((name) => !seen.add(name));

    expect(duplicated).toEqual([]);
    expect(names.filter((n) => n === 'Track Name')).toHaveLength(1);
  });

  it('gives each checkbox a name that identifies it', async () => {
    // The visible half above is what was reported; this is the half a
    // screen reader gets, and it is the one `config-page` computes
    // from the same string.
    const page = await fixture('config-page');

    await flush();
    await page.updateComplete;

    const labels = shadowAll<HTMLInputElement>(page, COLUMN_ROW_BOX).map(
      (box) => box.getAttribute('aria-label') ?? '',
    );

    expect(labels.length, 'the page draws column checkboxes').toBeGreaterThan(5);
    expect(new Set(labels).size).toBe(labels.length);
  });
});

describe('the column table', () => {
  it('offers no column the backend would reject', async () => {
    const accepted = goColumnIDs(GO_CONFIG ?? '');

    // Two non-vacuity guards: a glob that stopped matching, and a
    // parse that stopped finding the list it names.
    expect(GO_CONFIG, 'backend/tracklist/config.go is readable').toBeTruthy();
    expect(accepted.length, 'AllColumnIDs was parsed').toBeGreaterThan(10);

    expect(
      CONFIGURABLE_COLUMN_IDS.filter((id) => !accepted.includes(id)),
    ).toEqual([]);
  });

  it('still knows how to draw every column it offers', async () => {
    // The filter must not have taken a column *out* of the drawing
    // table: `configurable` says what Settings may list, not what the
    // list may render.
    expect(
      CONFIGURABLE_COLUMN_IDS.filter((id) => COLUMN_DEFS[id] === undefined),
    ).toEqual([]);
    expect(CONFIGURABLE_COLUMN_IDS).not.toContain('titleArtist');
    expect(COLUMN_DEFS['titleArtist'], 'the phone still has its column')
      .toBeTruthy();
  });
});

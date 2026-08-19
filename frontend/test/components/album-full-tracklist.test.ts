/**
 * Asking to see the whole album.
 *
 * The page could already draw the full release with the missing rows
 * dimmed, and did so automatically once the tags said the album was
 * incomplete. What it could not do was be *asked*: the rule depends on
 * the files declaring a per-disc total, so where they declare none —
 * which is a great deal of any library — a partly-owned album showed
 * only the tracks on disk and nothing said the rest existed.
 *
 * The switch is the explicit route. Its rules are all one rule: a
 * control that cannot change what is on screen is worse than no
 * control, which is the same test the version dropdown answers.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import { page } from 'vitest/browser';
import type { LitElement } from 'lit';

import '@components/explore-album-details/explore-album-details';
import { stub, flush, resetHarness } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';

const MBID = 'rg-0001';

function track(n: number, owned = false) {
  return {
    position: n,
    discNumber: 1,
    title: `Track ${n}`,
    length: 200000,
    mbid: `rec-${n}`,
    inLibrary: owned,
  };
}

function release(mbid: string, date: string, trackCount: number, owned = 0) {
  return {
    mbid,
    title: 'Glass Harbour',
    date,
    status: 'Official',
    tracks: Array.from({ length: trackCount }, (_, i) =>
      track(i + 1, i < owned),
    ),
  };
}

/** Local files with no recording MBIDs — an untagged rip, which is the
 * case the automatic rule cannot see. */
function localTracks(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    TrackName: `Track ${i + 1}`,
    TrackNumber: i + 1,
    DiscNumber: 1,
    TrackLength: '210000',
    RecordingMBID: '',
  }));
}

const UNKNOWN = { owned: 0, expected: 0, known: false, complete: false };

async function albumWith(
  releases: unknown[],
  completeness: Record<string, unknown>,
  local: unknown[] = [],
): Promise<LitElement> {
  stub('explore.Service.BrowseReleases', releases);
  stub('library.Library.GetAlbumCompleteness', completeness);
  stub('library.Library.GetAlbumTracks', local);

  const el = await fixture<LitElement>('explore-album-details', {
    releaseGroupMBID: MBID,
    localAlbumId: 7,
    albumName: 'Glass Harbour',
  });

  await flush();
  await el.updateComplete;

  return el;
}

const scopeSwitch = (el: LitElement) => shadow(el, '.tracklist-scope wa-switch');

async function toggle(el: LitElement) {
  const sw = scopeSwitch(el) as HTMLInputElement | null;
  if (!sw) throw new Error('no tracklist scope switch on the page');

  sw.checked = !sw.checked;
  sw.dispatchEvent(new Event('change'));

  await flush();
  await el.updateComplete;
}

describe('the "show the whole album" switch', () => {
  beforeEach(() => {
    resetHarness();
    stub('explore.Service.LookupReleaseGroup', {
      mbid: MBID,
      title: 'Glass Harbour',
      artistCredit: 'Tideline',
    });
    stub('explore.Service.GetThumbnail', '');
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    stub('library.Library.GetFilePathsByRecordingMBIDs', {});
  });

  /**
   * The report, exactly: two tracks on disk, twelve on the release,
   * and nothing to say so because the tags declared no total.
   */
  it('reveals the rest of the release when the total is unknown', async () => {
    const el = await albumWith(
      [release('rel-1', '2019-04-01', 12, 2)],
      UNKNOWN,
      localTracks(2),
    );

    expect(shadowAll(el, '.track-row')).toHaveLength(2);

    await toggle(el);

    const rows = shadowAll(el, '.track-row');
    expect(rows).toHaveLength(12);
    // Nothing resolves to a file, so every row is marked unowned —
    // the dimming is the signal, and it is not this switch's job to
    // invent ownership it cannot prove.
    expect(rows.filter((r) => r.classList.contains('unowned'))).toHaveLength(12);
  });

  /** And back again — a switch that only goes one way is a button. */
  it('goes back to the files on disk', async () => {
    const el = await albumWith(
      [release('rel-1', '2019-04-01', 12, 2)],
      UNKNOWN,
      localTracks(2),
    );

    await toggle(el);
    expect(shadowAll(el, '.track-row')).toHaveLength(12);

    await toggle(el);
    expect(shadowAll(el, '.track-row')).toHaveLength(2);
  });

  /**
   * The automatic rule still fires, and the control has to agree with
   * the page it is sitting on rather than starting out contradicting
   * it. This is what the tri-state is for.
   */
  it('starts checked when the tags already said the album is short', async () => {
    stub(
      'library.Library.GetFilePathsByRecordingMBIDs',
      Object.fromEntries(
        Array.from({ length: 9 }, (_, i) => [
          `rec-${i + 1}`,
          [`/music/0${i + 1}.mp3`],
        ]),
      ),
    );

    const el = await albumWith(
      [release('rel-1', '2019-04-01', 12, 9)],
      { owned: 9, expected: 12, known: true, complete: false },
      localTracks(9),
    );

    expect(shadowAll(el, '.track-row')).toHaveLength(12);
    expect((scopeSwitch(el) as HTMLInputElement).checked).toBe(true);
  });

  /** And the user outranks it: turning it off asks for the files. */
  it('lets the automatic answer be overridden', async () => {
    const el = await albumWith(
      [release('rel-1', '2019-04-01', 12, 9)],
      { owned: 9, expected: 12, known: true, complete: false },
      localTracks(9),
    );

    await toggle(el);

    expect(shadowAll(el, '.track-row')).toHaveLength(9);
  });

  /**
   * A control the accessibility tree cannot name is not a control, and
   * this app has shipped that fault twice — `wa-slider` pointed
   * `aria-labelledby` at an empty internal label, and `config-field`
   * rendered a `<label>` as a sibling with no `for`.
   *
   * `wa-switch` gets it right for a *different* reason than either:
   * its `<input role="switch">` sits inside a native `<label>` that also
   * holds the `<slot>`, so the name is computed across the flattened
   * tree from light-DOM text. That is worth an assertion rather than an
   * assumption — and it has to be the browser's own answer, since
   * querying shadow roots cannot compute a name.
   */
  it('is named for anyone not looking at it', async () => {
    await albumWith(
      [release('rel-1', '2019-04-01', 12, 2)],
      UNKNOWN,
      localTracks(2),
    );

    await expect
      .element(page.getByRole('switch', { name: 'Show the whole album' }))
      .toBeInTheDocument();
  });

  describe('is absent where it could not change anything', () => {
    it('when the album is entirely owned', async () => {
      // Ten files, a ten-track release: the switch would redraw the
      // same list, which reads as broken.
      const el = await albumWith(
        [release('rel-1', '2019-04-01', 10, 10)],
        { owned: 10, expected: 10, known: true, complete: true },
        localTracks(10),
      );

      expect(scopeSwitch(el)).toBeNull();
    });

    it('when there is no catalog release to switch to', async () => {
      const el = await albumWith([], UNKNOWN, localTracks(4));

      expect(scopeSwitch(el)).toBeNull();
    });

    it('when the album is not in the library at all', async () => {
      // Every entry here is already a catalog tracklist; there is no
      // "only my tracks" to go back to.
      const el = await albumWith([release('rel-1', '2019-04-01', 12)], UNKNOWN);

      expect(scopeSwitch(el)).toBeNull();
    });
  });
});

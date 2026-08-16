/**
 * When the version dropdown is a choice, and when it is furniture.
 *
 * A release group routinely has several releases — reissues, regional
 * pressings, a remaster — whose tracklists are word for word identical,
 * and the synthetic "Your Library" entry is often a third name for the
 * same one. Counting *entries* offered a control whose every option
 * showed the same rows. The test is distinct tracklists.
 *
 * The second rule here is about an album you own part of: the page
 * draws the *release*, with the tracks you are missing dimmed in place,
 * because the missing ones are the information and a tracklist trimmed
 * to what is on disk cannot show them at all.
 */
import { describe, expect, it, beforeEach } from 'vitest';
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

async function albumWith(
  releases: unknown[],
  completeness: Record<string, unknown>,
  localTracks: unknown[] = [],
): Promise<LitElement> {
  stub('explore.Service.BrowseReleases', releases);
  stub('library.Library.GetAlbumCompleteness', completeness);
  stub('library.Library.GetAlbumTracks', localTracks);

  const el = await fixture<LitElement>('explore-album-details', {
    releaseGroupMBID: MBID,
    localAlbumId: 7,
    albumName: 'Glass Harbour',
  });

  await flush();
  await el.updateComplete;

  return el;
}

const UNKNOWN = { owned: 0, expected: 0, known: false, complete: false };

describe('the version dropdown', () => {
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

  it('stays hidden when every release has the same tracklist', async () => {
    const el = await albumWith(
      [
        release('rel-1', '2019-04-01', 10),
        release('rel-2', '2020-09-01', 10),
        release('rel-3', '2021-01-01', 10),
      ],
      UNKNOWN,
    );

    expect(shadow(el, '#version-select')).toBeNull();
  });

  it('appears when a release actually differs', async () => {
    const el = await albumWith(
      [release('rel-1', '2019-04-01', 10), release('rel-2', '2020-09-01', 14)],
      UNKNOWN,
    );

    expect(shadow(el, '#version-select')).not.toBeNull();
  });

  it('stays hidden for a single release', async () => {
    const el = await albumWith([release('rel-1', '2019-04-01', 10)], UNKNOWN);

    expect(shadow(el, '#version-select')).toBeNull();
  });

  /**
   * An untagged library copy against the catalog's copy of the very
   * same album. This is the one that reached the running app: keys
   * were `mbid || title` *per track*, which only helps when both sides
   * lack ids — so the local ten (no MBIDs) and the catalog's identical
   * ten (with MBIDs) never compared equal, and every owned album grew
   * a dropdown the moment its catalog data landed.
   */
  it('counts an untagged copy and its catalog twin as one tracklist', async () => {
    resetHarness();
    stub('explore.Service.LookupReleaseGroup', {
      mbid: MBID,
      title: 'Glass Harbour',
      artistCredit: 'Tideline',
    });
    stub('explore.Service.GetThumbnail', '');
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    stub('library.Library.GetFilePathsByRecordingMBIDs', {});
    stub('library.Library.GetAlbumCompleteness', UNKNOWN);
    stub(
      'library.Library.GetAlbumTracks',
      Array.from({ length: 10 }, (_, i) => ({
        TrackName: `Track ${i + 1}`,
        TrackNumber: i + 1,
        DiscNumber: 1,
        TrackLength: '210000',
        RecordingMBID: '',
      })),
    );
    stub('explore.Service.BrowseReleases', [release('rel-1', '2019-04-01', 10)]);

    const el = await fixture<LitElement>('explore-album-details', {
      releaseGroupMBID: MBID,
      localAlbumId: 7,
      albumName: 'Glass Harbour',
    });

    await flush();
    await el.updateComplete;

    expect(shadow(el, '#version-select')).toBeNull();
  });

  /**
   * The case that prompted the rule, reported from the running app: a
   * local album with no release-group MBID at all. `hydrateLocalOnly`
   * synthesises a release from the files, so the entries come out as
   * "Your Library" *and* the cluster built from the very same tracks —
   * two entries, one tracklist, and under the old length test a
   * dropdown whose both options were the same ten songs.
   */
  it('stays hidden for a local album with no MBID', async () => {
    resetHarness();
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    stub('library.Library.GetFilePathsByRecordingMBIDs', {});
    stub('library.Library.GetAlbumCompleteness', {
      owned: 10,
      expected: 10,
      known: true,
      complete: true,
    });
    // No RecordingMBID on any of them, which is what an untagged rip
    // looks like and why the fingerprint fallback matters.
    stub(
      'library.Library.GetAlbumTracks',
      Array.from({ length: 10 }, (_, i) => ({
        TrackName: `Track ${i + 1}`,
        TrackNumber: i + 1,
        DiscNumber: 1,
        TrackLength: '3:30',
        RecordingMBID: '',
      })),
    );

    const el = await fixture<LitElement>('explore-album-details', {
      localAlbumId: 7,
      albumName: 'Melophobia',
    });

    await flush();
    await el.updateComplete;

    expect(shadow(el, '#version-select')).toBeNull();
    // The tracklist is still there — this hides a control, not content.
    expect(shadowAll(el, '.track-row')).toHaveLength(10);
  });
});

describe('an album the library holds part of', () => {
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

  it('draws the whole release, with the missing tracks dimmed', async () => {
    // Ownership is a *file*, not the catalog row's `inLibrary` flag —
    // nine of the twelve recordings resolve to a path, so three rows
    // dim.  Stating it as flags is what let the page claim an album it
    // could not play a note of.
    stub(
      'library.Library.GetFilePathsByRecordingMBIDs',
      Object.fromEntries(
        Array.from({ length: 9 }, (_, i) => [`rec-${i + 1}`, [`/music/0${i + 1}.mp3`]]),
      ),
    );

    const el = await albumWith(
      [release('rel-1', '2019-04-01', 12, 9)],
      { owned: 9, expected: 12, known: true, complete: false },
      [
        { TrackName: 'Track 1', TrackNumber: 1, DiscNumber: 1, TrackLength: '3:20' },
      ],
    );

    const rows = shadowAll(el, '.track-row');

    // Twelve rows, not the nine on disk.
    expect(rows).toHaveLength(12);
    expect(rows.filter((r) => r.classList.contains('unowned'))).toHaveLength(3);
  });

  it('does not swap in a catalog tracklist when the total is unknown', async () => {
    // Without a declared total there is no evidence the local copy is
    // short, and preferring the catalog here would quietly replace
    // every untagged album's tracklist with a guess.
    const el = await albumWith(
      [release('rel-1', '2019-04-01', 12, 2)],
      UNKNOWN,
      [
        { TrackName: 'Track 1', TrackNumber: 1, DiscNumber: 1, TrackLength: '3:20' },
        { TrackName: 'Track 2', TrackNumber: 2, DiscNumber: 1, TrackLength: '4:10' },
      ],
    );

    expect(shadowAll(el, '.track-row')).toHaveLength(2);
  });
});

/**
 * Which version you own, by name.
 *
 * There used to be a synthetic "Your Library" entry standing in for the
 * matching release, which hid the thing worth knowing: you could see
 * that you owned *a* version but not *which*, while the real release —
 * with its date, country and release count — sat underneath under a
 * different name. The release is marked instead.
 */
describe('the version you own', () => {
  const OWNED_TRACKS = Array.from({ length: 10 }, (_, i) => ({
    TrackName: `Track ${i + 1}`,
    TrackNumber: i + 1,
    DiscNumber: 1,
    TrackLength: '210000',
    RecordingMBID: `rec-${i + 1}`,
  }));

  /** A deluxe edition: a genuinely different track *set*, so it stays
   * its own version rather than being folded as a near-duplicate. */
  const DELUXE = {
    mbid: 'rel-deluxe',
    title: 'Glass Harbour (Deluxe)',
    date: '2014-05-01',
    status: 'Official',
    tracks: Array.from({ length: 13 }, (_, i) => ({
      position: i + 1,
      discNumber: 1,
      title: `Track ${i + 1}`,
      length: 200000,
      mbid: `rec-${i + 1}`,
      inLibrary: i < 10,
    })),
  };

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

  async function twoVersions(): Promise<LitElement> {
    return albumWith(
      [release('rel-2013', '2013-10-08', 10), DELUXE],
      UNKNOWN,
      OWNED_TRACKS,
    );
  }

  const optionTexts = (el: LitElement) =>
    shadowAll(el, '#version-select option').map((o) =>
      (o.textContent ?? '').trim().replace(/\s+/g, ' '),
    );

  it('names the release rather than calling it "Your Library"', async () => {
    const options = optionTexts(await twoVersions());

    expect(options).toHaveLength(2);
    expect(options.some((o) => o.startsWith('Your Library'))).toBe(false);
    expect(options.some((o) => o.includes('2013-10-08'))).toBe(true);
  });

  it('marks the owned one, in words as well as a glyph', async () => {
    const owned = optionTexts(await twoVersions()).filter((o) =>
      o.includes('in your library'),
    );

    expect(owned).toHaveLength(1);
    expect(owned[0]).toContain('2013-10-08');
    expect(owned[0]).toContain('\u2605');
  });

  it('selects the owned one by default', async () => {
    const el = await twoVersions();
    const select = shadow<HTMLSelectElement>(el, '#version-select');

    expect(select?.value).toBe('cluster:rel-2013');
    // Ten rows, not the deluxe's thirteen.
    expect(shadowAll(el, '.track-row')).toHaveLength(10);
  });

  it('says which one it is under the dropdown', async () => {
    const el = await twoVersions();

    expect(shadow(el, '.version-meta')?.textContent).toContain(
      'the version in your library',
    );
  });

  it('still falls back to a synthetic when nothing matches', async () => {
    // Local files that are not any known release: there is no version
    // name to mark, so the stand-in is still the honest answer.
    const el = await albumWith(
      [release('rel-2013', '2013-10-08', 10), DELUXE],
      UNKNOWN,
      OWNED_TRACKS.slice(0, 4),
    );

    expect(
      optionTexts(el).some((o) => o.startsWith('Your Library')),
    ).toBe(true);
  });
});

/**
 * Which pressing a merged cluster shows.
 *
 * Near-duplicates are folded by track *set*, so a resequenced pressing
 * — same songs, different running order — merges correctly. But the
 * survivor used to be whichever release came first in the browse
 * response, which is meaningless ordering: on the album that prompted
 * this, one 2021 pressing arrived ahead of eleven 2013 ones and the
 * cluster wore the 2021 running order. The user's own files then
 * matched no cluster fingerprint, so the page called their copy
 * unlinked to MusicBrainz *and* offered a second version whose only
 * difference was an ordering almost nothing was pressed in.
 */
describe('a merged cluster', () => {
  const resequenced = {
    mbid: 'rel-2021',
    title: 'Glass Harbour',
    date: '2021',
    status: 'Official',
    tracks: [10, 2, 3, 4, 5, 6, 7, 8, 9, 1].map((n, i) => ({
      position: i + 1,
      discNumber: 1,
      title: `Track ${n}`,
      length: 200000,
      mbid: `rec-${n}`,
      inLibrary: true,
    })),
  };

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

  it('shows the order the most releases agree on, not the first seen', async () => {
    const el = await albumWith(
      // The outlier first, exactly as the real browse returned it.
      [
        resequenced,
        ...Array.from({ length: 11 }, (_, i) =>
          release(`rel-2013-${i}`, '2013-10-08', 10),
        ),
      ],
      UNKNOWN,
      Array.from({ length: 10 }, (_, i) => ({
        TrackName: `Track ${i + 1}`,
        TrackNumber: i + 1,
        DiscNumber: 1,
        TrackLength: '210000',
        RecordingMBID: `rec-${i + 1}`,
      })),
    );

    // The consensus order, so the library copy is recognised as it...
    const titles = shadowAll(el, '.track-row .track-title').map((t) =>
      t.textContent?.trim(),
    );
    expect(titles[0]).toBe('Track 1');

    // ...and there is one version, so no dropdown at all.
    expect(shadow(el, '#version-select')).toBeNull();
  });
});

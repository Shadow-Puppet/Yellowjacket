/**
 * An album page you can play from.
 *
 * `H-13`: no Play, no Shuffle, no Add to queue on the album header, and
 * green ticks with no legend. The reason it is not simply "add three
 * buttons" is that this is a **catalog** page — the album on it may be
 * entirely the user's, partly theirs, or not theirs at all — and a Play
 * button that plays 7 of a release's 40 tracks under a label saying
 * "Play" is the page lying about what is owned.
 *
 * The partial case is the interesting one and it is **only reachable
 * here**: it needs a catalog release whose tracklist is partly matched
 * against the library, which the fixture library (untagged, no MBIDs,
 * no network) cannot produce. The whole-album case was driven by hand
 * in the running app.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-album-details/explore-album-details';
import { stub, flush, resetHarness, calls } from '@test/support/harness';
import { fixture, shadow, text } from '@test/support/render';

type Version = {
  key: string;
  label: string;
  sublabel: string;
  tracks: Array<{
    position: number;
    discNumber: number;
    title: string;
    length: number;
    mbid: string;
    inLibrary: boolean;
  }>;
};

function track(n: number, owned: boolean) {
  return {
    position: n,
    discNumber: 1,
    title: `Track ${n}`,
    length: 200000,
    mbid: `mbid-${n}`,
    inLibrary: owned,
  };
}

/**
 * Put a release on the page without the network.
 *
 * The component builds its versions from fetched releases; this reaches
 * past that and sets the state the header actually reads, which is the
 * only part under test here.
 */
async function withVersion(
  owned: number,
  total: number,
): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-album-details', {
    albumName: 'Glass Harbour',
  });

  const version: Version = {
    key: 'v1',
    label: '2019',
    sublabel: `${total} tracks`,
    tracks: Array.from({ length: total }, (_, i) => track(i + 1, i < owned)),
  };

  Object.assign(el, {
    versionEntries: [version],
    selectedVersionKey: 'v1',
    loadingReleases: false,
    loadingInfo: false,
  });
  el.requestUpdate();
  await flush();
  await el.updateComplete;

  return el;
}

const playLabel = (el: LitElement) =>
  text(el, '[data-testid="album-play"]');

describe('the album header’s primary action', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetFilePathsByRecordingMBIDs', {});
    stub('library.Library.GetFilePathsByAlbums', {});
    stub('library.Library.GetAlbumTracks', []);
    // The download actions resolve a target library on mount; without
    // this the store awaits an undefined binding result and the whole
    // file dies in an unhandled rejection rather than a failed test.
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
  });

  it('says “Play” when the whole release is owned', async () => {
    const el = await withVersion(6, 6);

    expect(playLabel(el)).toBe('Play');
    expect(shadow(el, '[data-testid="album-shuffle"]')).toBeTruthy();
    expect(shadow(el, '[data-testid="album-queue"]')).toBeTruthy();
    // No count sentence: there is nothing to qualify.
    expect(shadow(el, '.album-owned-note')).toBeNull();
  });

  it('counts itself when only some of it is owned', async () => {
    const el = await withVersion(7, 12);

    expect(playLabel(el)).toBe('Play 7 of 12');
    expect(text(el, '.album-owned-note')).toBe(
      'You have 7 of these 12 tracks.',
    );
  });

  it('offers no play button at all when none of it is owned', async () => {
    // A Play button that plays nothing is worse than no Play button;
    // the download and want actions are the whole answer here.
    const el = await withVersion(0, 12);

    expect(shadow(el, '[data-testid="album-play"]')).toBeNull();
    expect(shadow(el, '[data-testid="album-shuffle"]')).toBeNull();
    expect(shadow(el, '[data-testid="album-queue"]')).toBeNull();
  });

  it('asks for the owned tracks’ paths once, by the key it owns them by', async () => {
    // `perf.m2`'s rule: ask for what the caller uses, once. The caller
    // here uses file paths and knows its tracks only as recording
    // MBIDs — `MBTrack.localId` is declared and never written by
    // anything in the backend.
    const el = await withVersion(7, 12);

    shadow<HTMLElement>(el, '[data-testid="album-play"]')!.click();
    await flush();

    const asked = calls('library.Library.GetFilePathsByRecordingMBIDs');

    expect(asked).toHaveLength(1);
    // Only the owned ones, and no empty MBID — an empty string matches
    // every untagged recording in the library.
    expect(asked[0]!.args[0]).toHaveLength(7);
    expect(asked[0]!.args[0]).not.toContain('');
  });
});

describe('the ticks have a legend', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetAlbumTracks', []);
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
  });

  it('names the symbol when at least one track carries it', async () => {
    const el = await withVersion(3, 12);

    expect(text(el, '.tracklist-legend')).toContain('in your library');
  });

  it('does not explain a symbol that is not on screen', async () => {
    const el = await withVersion(0, 12);

    expect(shadow(el, '.tracklist-legend')).toBeNull();
  });
});

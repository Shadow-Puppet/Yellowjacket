/**
 * An album page you can play from.
 *
 * `H-13`: no Play, no Shuffle, no Add to queue on the album header, and
 * green ticks with no explanation. The reason it is not simply "add three
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
import { stub, flush, resetHarness, calls, lastArgs } from '@test/support/harness';
import { fixture, shadow, shadowAll, text } from '@test/support/render';

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
 *
 * Owning a track means the library has a *file* for it — the page
 * resolves the displayed tracklist's paths once and every action, badge
 * and dimmed row reads that one answer. So the fixture says which
 * tracks have files rather than setting an `inLibrary` flag, which is
 * what used to be able to claim ownership of something unplayable.
 */
async function withVersion(
  owned: number,
  total: number,
): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-album-details', {
    albumName: 'Glass Harbour',
  });

  const tracks = Array.from({ length: total }, (_, i) => track(i + 1, i < owned));

  const paths: Record<string, string[]> = {};
  for (const t of tracks.filter((t) => t.inLibrary)) {
    paths[t.mbid] = [`/music/${t.mbid}.mp3`];
  }

  stub('library.Library.GetFilePathsByRecordingMBIDs', paths);

  const version: Version = {
    key: 'v1',
    label: '2019',
    sublabel: `${total} tracks`,
    tracks,
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

  it('asks for the tracklist’s paths once, on load, and not on click', async () => {
    // `perf.m2`'s rule — ask for what the caller uses, once — and the
    // ownership rule with it. The page asks about the *whole* displayed
    // tracklist when it settles, because whether a track is owned is
    // that query's answer and not something to be inferred first. Every
    // action, badge and dimmed row then reads the one result, so a
    // click asks nothing and cannot fail.
    const el = await withVersion(7, 12);

    const onLoad = calls('library.Library.GetFilePathsByRecordingMBIDs');

    expect(onLoad).toHaveLength(1);
    expect(onLoad[0]!.args[0]).toHaveLength(12);
    // No empty MBID — an empty string matches every untagged recording
    // in the library.
    expect(onLoad[0]!.args[0]).not.toContain('');

    shadow<HTMLElement>(el, '[data-testid="album-play"]')!.click();
    await flush();

    expect(calls('library.Library.GetFilePathsByRecordingMBIDs')).toHaveLength(1);
    expect(lastArgs('queue.Queue.SetQueue')?.[0]).toHaveLength(7);
  });
});

/**
 * How a track that is not in the library reads.
 *
 * It used to be a green tick against the ones that were, plus a legend
 * explaining the tick — a positive mark on the common case, which put a
 * column of circles down an album you own outright. The comparison that
 * settled it is a streaming service dimming what it cannot play: the
 * *absence* is the exception, so the absence is what gets marked.
 *
 * Dimming is a colour, though, so it cannot be the only signal.
 * `aria-disabled` is what carries it to anyone not seeing the page.
 */
describe('a track the library does not have', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetAlbumTracks', []);
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
  });

  it('is dimmed, and the owned ones are not', async () => {
    const el = await withVersion(3, 12);
    const rows = shadowAll(el, '.track-row');

    expect(rows).toHaveLength(12);
    expect(rows.filter((r) => r.classList.contains('unowned'))).toHaveLength(9);
    expect(rows.filter((r) => r.classList.contains('owned'))).toHaveLength(3);
  });

  it('says so without relying on the colour', async () => {
    const el = await withVersion(3, 12);
    const rows = shadowAll(el, '.track-row');

    expect(rows[0]?.getAttribute('aria-disabled')).toBe('false');
    expect(rows[11]?.getAttribute('aria-disabled')).toBe('true');
    expect(rows[11]?.getAttribute('aria-label')).toContain('not in your library');
  });

  /**
   * The badge is only on rows that can act on it. An owned track has
   * nothing to request, so it carries no mark at all — the undimmed row
   * already says it is yours, which is what retired the green tick.
   * An unowned one keeps the badge, because it is now a request
   * control rather than a decoration, revealed on hover or focus so a
   * mostly-owned album is not a column of plus signs.
   */
  it('marks only the rows with something left to ask for', async () => {
    const el = await withVersion(3, 12);
    const rows = shadowAll(el, '.track-row');

    const badgeIn = (row: Element) =>
      row.querySelector('library-status-indicator');

    expect(badgeIn(rows[0]!)).toBeNull();
    expect(badgeIn(rows[11]!)).not.toBeNull();
    expect(
      shadowAll(el, '.track-row library-status-indicator'),
    ).toHaveLength(9);
    expect(shadow(el, '.tracklist-legend')).toBeNull();
  });
});

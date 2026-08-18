/**
 * The request badge on an unowned row is there without being hovered.
 *
 * It used to be transparent until the row was hovered or focused, on
 * the reasoning that a column of plus signs down a mostly-owned album
 * is clutter. That reasoning came from the green ticks it replaced and
 * does not survive the rule those were removed for: a tick marked the
 * **common** case, while this marks the rows that are *not* here. A
 * mark on the exception is the information on this page, and one that
 * exists only under the pointer cannot be seen, counted, or reached by
 * anyone driving the app with a finger.
 *
 * That the badge *repaints* when clicked is the other half of #33 and
 * is covered by `album-track-request.test.ts`.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-album-details/explore-album-details';
import { stub, flush, resetHarness } from '@test/support/harness';
import { fixture, shadowAll } from '@test/support/render';

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

/** An album with one owned track and one that is not here. */
async function albumWithAnUnownedTrack(): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-album-details', {
    albumName: 'Glass Harbour',
    releaseGroupMBID: 'rg-1',
  });

  stub('library.Library.GetFilePathsByRecordingMBIDs', {
    'mbid-1': ['/music/mbid-1.mp3'],
  });

  Object.assign(el, {
    versionEntries: [
      {
        key: 'v1',
        label: '2019',
        sublabel: '2 tracks',
        tracks: [track(1, true), track(2, false)],
      },
    ],
    selectedVersionKey: 'v1',
    loadingReleases: false,
    loadingInfo: false,
  });
  el.requestUpdate();
  await flush();
  await el.updateComplete;

  return el;
}

const badges = (el: LitElement) =>
  shadowAll(el, 'library-status-indicator.track-request');

describe('the tracklist’s request badge', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetFilePathsByRecordingMBIDs', {});
    stub('library.Library.GetFilePathsByAlbums', {});
    stub('library.Library.GetAlbumTracks', []);
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    stub('download.Service.ProviderKinds', []);
    stub('download.Service.ListProviders', []);
    stub('download.Service.ListDownloads', []);
    stub('download.Service.ListRequests', []);
  });

  it('is visible without a pointer anywhere near it', async () => {
    const el = await albumWithAnUnownedTrack();
    const [badge] = badges(el);

    expect(badge).toBeTruthy();
    // Computed opacity rather than the absence of a rule, because the
    // rule could come back under a different selector.
    expect(getComputedStyle(badge!).opacity).toBe('1');
  });

  it('is still only on the rows with something to request', async () => {
    // Always-visible is not the same as everywhere: an owned track has
    // nothing left to ask for, and a badge on it would be the column of
    // green ticks this page deliberately stopped drawing.
    expect(badges(await albumWithAnUnownedTrack())).toHaveLength(1);
  });
});

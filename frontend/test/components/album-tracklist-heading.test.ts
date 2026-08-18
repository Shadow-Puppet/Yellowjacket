/**
 * The tracklist's own heading.
 *
 * A list of numbered titles with durations, under the album's cover, is
 * the one thing on this page that did not need a word above it saying
 * what it was — "TRACKLIST" labelled the only thing already obvious.
 *
 * What goes is the *ink*, not the element. The section is a landmark
 * and the page's heading structure runs through it, so a reader moving
 * by heading still has to be able to find it, and it is hidden the way
 * `sr-only` hides things: `clip-path`, never `display: none`, which
 * would take it out of the accessibility tree along with the layout.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-album-details/explore-album-details';
import { stub, flush, resetHarness } from '@test/support/harness';
import { fixture, shadowAll } from '@test/support/render';

function track(n: number) {
  return {
    position: n,
    discNumber: 1,
    title: `Track ${n}`,
    length: 200000,
    mbid: `mbid-${n}`,
    inLibrary: true,
  };
}

async function albumPage(): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-album-details', {
    albumName: 'Glass Harbour',
    releaseGroupMBID: 'rg-1',
  });

  Object.assign(el, {
    versionEntries: [
      {
        key: 'v1',
        label: '2019',
        sublabel: '2 tracks',
        tracks: [track(1), track(2)],
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

const tracklistHeading = (el: LitElement) =>
  shadowAll(el, 'h3').find((h) => h.textContent?.trim() === 'Tracklist');

describe('the album tracklist heading', () => {
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

  it('is still in the tree', async () => {
    expect(tracklistHeading(await albumPage())).toBeTruthy();
  });

  it('takes up no room on the page', async () => {
    const heading = tracklistHeading(await albumPage())!;
    const box = heading.getBoundingClientRect();

    expect(box.width).toBeLessThanOrEqual(1);
    expect(box.height).toBeLessThanOrEqual(1);
    // Hidden by clipping, not by removal: display:none and
    // visibility:hidden both take it out of the accessibility tree.
    expect(getComputedStyle(heading).display).not.toBe('none');
    expect(getComputedStyle(heading).visibility).not.toBe('hidden');
  });
});

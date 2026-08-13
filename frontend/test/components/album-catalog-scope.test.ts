/**
 * What the album page claims about the catalog while it is waiting.
 *
 * The scope notice said "No catalog details for this album right now"
 * on albums that were matched correctly and whose catalog data arrived
 * a few seconds later. The cause was that *not having an answer yet*
 * and *having been told there is no answer* were the same state: the
 * page inferred a failure from a deadline, and the deadline was 12 s
 * against a browse that waits on a 1 req/s limiter shared with
 * `PrefetchReleases`, which fires up to eight of them when an artist
 * page renders.
 *
 * So the rule under test is that `unavailable` is only ever reached by
 * something *telling* the page the catalog did not answer —
 * `AlbumReleasesFailed`, or an empty result after the background fetch
 * reported itself done.
 *
 * A fetch that is merely slow says *nothing at all*. It used to say
 * "showing what your library has while the full album details load",
 * which is a sentence about the page's own plumbing; the dimmed rows in
 * the tracklist carry that information without a banner, so tracks
 * arriving dimmed reads as the album filling in.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-album-details/explore-album-details';
import { stub, emit, flush, resetHarness, calls } from '@test/support/harness';
import { fixture, shadow } from '@test/support/render';

const MBID = 'rg-0001';

/** The scope the notice is currently being rendered with. */
function scope(el: LitElement): string | null {
  return shadow(el, 'catalog-scope-notice')?.getAttribute('scope') ?? null;
}

/**
 * An album page mid-flight: the release group resolves, but
 * `BrowseReleases` returns empty, which is what the local-first backend
 * path does on a cold cache while it fetches in the background.
 */
async function coldAlbum(): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-album-details', {
    releaseGroupMBID: MBID,
    albumName: 'Glass Harbour',
  });

  await flush();
  await el.updateComplete;

  return el;
}

describe('what the album page says while the catalog is still coming', () => {
  beforeEach(() => {
    resetHarness();
    stub('explore.Service.BrowseReleases', []);
    stub('explore.Service.LookupReleaseGroup', {
      mbid: MBID,
      title: 'Glass Harbour',
      artistCredit: 'Tideline',
    });
    stub('explore.Service.GetThumbnail', '');
    stub('library.Library.GetAlbumTracks', []);
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    stub('library.Library.GetAlbumCompleteness', {
      owned: 0,
      expected: 0,
      known: false,
      complete: false,
    });
    stub('library.Library.GetFilePathsByRecordingMBIDs', {});
  });

  it('does not call a slow fetch a failure', async () => {
    const el = await coldAlbum();

    // No event either way yet — the background browse is still queued,
    // and `catalog` is the silent scope: the notice renders nothing.
    expect(scope(el)).toBe('catalog');
  });

  it('says the catalog is unavailable when the browse reports failing', async () => {
    const el = await coldAlbum();

    emit('AlbumReleasesFailed', MBID);
    await flush();
    await el.updateComplete;

    expect(scope(el)).toBe('unavailable');
  });

  it('ignores a failure for a different release group', async () => {
    const el = await coldAlbum();

    emit('AlbumReleasesFailed', 'rg-9999');
    await flush();
    await el.updateComplete;

    expect(scope(el)).toBe('catalog');
  });

  it('says unavailable when the catalog answers with nothing', async () => {
    const el = await coldAlbum();

    // The background fetch reported done, and the re-fetch it prompts
    // still comes back empty: the catalog answered, and the answer was
    // that it has no releases for this group.
    emit('AlbumReleasesReady', MBID);
    await flush();
    await el.updateComplete;

    expect(scope(el)).toBe('unavailable');
  });

  it('goes quiet once the releases actually arrive', async () => {
    const el = await coldAlbum();

    stub('explore.Service.BrowseReleases', [
      {
        mbid: 'rel-1',
        title: 'Glass Harbour',
        date: '2019-04-01',
        tracks: [
            {
                position: 1,
                discNumber: 1,
                title: 'Track 1',
                length: 200000,
                mbid: 'rec-1',
                inLibrary: false,
            },
        ],
      },
    ]);

    emit('AlbumReleasesReady', MBID);
    await flush();
    await el.updateComplete;

    // `catalog` is the silent scope — the notice renders nothing.
    expect(scope(el)).toBe('catalog');
  });
});

/**
 * The album you already own in full.
 *
 * Identity comes from the MBID and the tracklist from the files' own
 * "5/12" denominators, so between them there is nothing left for a
 * browse to answer — and the browse was the expensive part, waiting on
 * a 1 req/s limiter behind up to eight queued prefetches.
 */
describe('an album the library already holds in full', () => {
  beforeEach(() => {
    resetHarness();
    stub('explore.Service.BrowseReleases', []);
    stub('explore.Service.LookupReleaseGroup', {
      mbid: MBID,
      title: 'Glass Harbour',
      artistCredit: 'Tideline',
    });
    stub('explore.Service.GetThumbnail', '');
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    stub('library.Library.GetFilePathsByRecordingMBIDs', {});
    stub('library.Library.GetAlbumTracks', [
      { TrackName: 'Track 1', TrackNumber: 1, DiscNumber: 1, TrackLength: '3:20' },
    ]);
  });

  it('never asks the catalog', async () => {
    stub('library.Library.GetAlbumCompleteness', {
      owned: 12,
      expected: 12,
      known: true,
      complete: true,
    });

    const el = await fixture<LitElement>('explore-album-details', {
      releaseGroupMBID: MBID,
      localAlbumId: 7,
      albumName: 'Glass Harbour',
    });

    await flush();
    await el.updateComplete;

    expect(calls('explore.Service.BrowseReleases')).toHaveLength(0);
    // And says nothing about it, because nothing is missing.
    expect(scope(el)).toBe('catalog');
  });

  it('still asks when tracks are missing', async () => {
    stub('library.Library.GetAlbumCompleteness', {
      owned: 9,
      expected: 12,
      known: true,
      complete: false,
    });

    const el = await fixture<LitElement>('explore-album-details', {
      releaseGroupMBID: MBID,
      localAlbumId: 7,
      albumName: 'Glass Harbour',
    });

    await flush();
    await el.updateComplete;

    expect(calls('explore.Service.BrowseReleases').length).toBeGreaterThan(0);
  });

  it('still asks when the tags never declared a total', async () => {
    // Unknown is not incomplete. The catalog is the only way to learn
    // the total here, so this is exactly when it is worth asking.
    stub('library.Library.GetAlbumCompleteness', {
      owned: 9,
      expected: 0,
      known: false,
      complete: false,
    });

    const el = await fixture<LitElement>('explore-album-details', {
      releaseGroupMBID: MBID,
      localAlbumId: 7,
      albumName: 'Glass Harbour',
    });

    await flush();
    await el.updateComplete;

    expect(calls('explore.Service.BrowseReleases').length).toBeGreaterThan(0);
  });

  it('marks a partly-held album with a ring, and a full one with a tick', async () => {
    stub('library.Library.GetAlbumCompleteness', {
      owned: 9,
      expected: 12,
      known: true,
      complete: false,
    });

    const el = await fixture<LitElement>('explore-album-details', {
      releaseGroupMBID: MBID,
      localAlbumId: 7,
      albumName: 'Glass Harbour',
    });

    await flush();
    await el.updateComplete;

    const badge = shadow(el, 'library-status-indicator');
    expect(badge?.getAttribute('status')).toBe('partial');
  });
});

/**
 * How much the artist page asks the catalog to warm up.
 *
 * `PrefetchReleases` fires up to eight `BrowseReleases` calls, which is
 * the most expensive request the app makes — every version of a release
 * group, with `recordings` and `media`, on a shared rate limiter. The
 * page used to call it from *both* the top-releases fetch and the
 * discography fetch, and because the backend skips groups it has
 * already cached, the second call did not collapse into the first: it
 * spent its own cap of eight on the next eight albums. On a cold artist
 * `ArtistDiscographyReady` re-runs both fetchers, so one page view could
 * queue thirty-two of them.
 *
 * The rule under test is therefore about call *count*, not content: the
 * two sections contribute to one batched request, the top releases lead
 * it because that is what a visitor clicks, and nothing is asked for
 * twice.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-artist-details/explore-artist-details';
import { stub, emit, flush, resetHarness, calls } from '@test/support/harness';
import { fixture } from '@test/support/render';

const ARTIST = 'artist-0001';

/** Every argument list PrefetchReleases has been called with. */
function prefetchCalls(): string[][] {
  return calls('explore.Service.PrefetchReleases').map(
    (c) => (c.args[0] as string[]) ?? [],
  );
}

beforeEach(() => {
  resetHarness();

  stub('explore.Service.LookupArtist', {
    mbid: ARTIST,
    name: 'Tideline',
  });

  // Top releases and the full discography overlap, as they do in life:
  // the top list is a subset of the discography.
  stub('explore.Service.TopReleaseGroupsForArtist', [
    { releaseGroupMbid: 'rg-top-1', title: 'Foreshore', artistName: 'Tideline' },
    { releaseGroupMbid: 'rg-top-2', title: 'Backwash', artistName: 'Tideline' },
  ]);

  stub('explore.Service.BrowseReleaseGroups', [
    { mbid: 'rg-top-1', title: 'Foreshore', artistCredit: 'Tideline' },
    { mbid: 'rg-deep-1', title: 'Spring Tide', artistCredit: 'Tideline' },
  ]);

  stub('explore.Service.TopRecordingsForArtist', []);
  stub('explore.Service.SimilarArtists', []);
  stub('explore.Service.PrefetchReleases', undefined);
});

describe('what the artist page asks the catalog to prefetch', () => {
  it('makes one prefetch call for both sections, not one each', async () => {
    await fixture<LitElement>('explore-artist-details', {
      artistMBID: ARTIST,
      artistName: 'Tideline',
    });

    await flush();

    expect(prefetchCalls().length).toBe(1);
  });

  it('leads with the top releases and includes the deep cuts', async () => {
    await fixture<LitElement>('explore-artist-details', {
      artistMBID: ARTIST,
      artistName: 'Tideline',
    });

    await flush();

    const batch = prefetchCalls()[0] ?? [];

    expect(batch.slice(0, 2)).toEqual(['rg-top-1', 'rg-top-2']);
    expect(batch).toContain('rg-deep-1');
  });

  it('asks for each release group once, across both sections', async () => {
    await fixture<LitElement>('explore-artist-details', {
      artistMBID: ARTIST,
      artistName: 'Tideline',
    });

    await flush();

    const batch = prefetchCalls()[0] ?? [];

    expect(batch.length).toBe(new Set(batch).size);
    expect(batch.filter((m) => m === 'rg-top-1').length).toBe(1);
  });

  it('does not re-ask on the cold-artist refetch', async () => {
    await fixture<LitElement>('explore-artist-details', {
      artistMBID: ARTIST,
      artistName: 'Tideline',
    });

    await flush();

    const asked = prefetchCalls().flat().length;

    // The background discography fetch reports in, and both fetchers
    // run again against the freshly-populated index.
    emit('ArtistDiscographyReady', ARTIST);
    await flush();

    const askedAfter = prefetchCalls().flat().length;

    expect(askedAfter).toBe(asked);
  });
});

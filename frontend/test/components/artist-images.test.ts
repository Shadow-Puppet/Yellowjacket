/**
 * Which call an artist portrait comes from.
 *
 * `GetArtistImageURL` is the *resolving* entry point: on a miss it does
 * MusicBrainz artist-rels → Wikidata → Wikipedia → a Wikimedia
 * download. `GetArtistImagesCachedPaths` is a disk existence check.
 * Explore used to seed only from the library store — owned artists,
 * which on a catalog search is nearly none of the results — and send
 * everything else to the resolver, one `await` at a time.
 *
 * So the rule under test is that a portrait already on disk costs no
 * network call at all, and that the ones that do need resolving are not
 * serialised behind each other.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-view/explore-view';
import { stub, flush, resetHarness, calls } from '@test/support/harness';
import { fixture } from '@test/support/render';

const CACHED = 'artist-cached';
const UNCACHED = 'artist-uncached';

function searchResult() {
  return {
    artists: [
      { mbid: CACHED, name: 'Tideline', popularity: 10 },
      { mbid: UNCACHED, name: 'Shorebreak', popularity: 5 },
    ],
    releaseGroups: [],
    recordings: [],
    topResults: [],
  };
}

beforeEach(() => {
  resetHarness();

  stub('explore.Service.SearchLocal', searchResult());
  stub('explore.Service.GetThumbnails', {});
  stub('explore.Service.GetExploreShelves', { state: 'ready', shelves: [] });
  stub('explore.Service.GetArtistImagesCachedPaths', {
    [CACHED]: '/artist-images/ar/artist-cached/primary_md.jpg',
  });
  stub('explore.Service.GetArtistImageURL', '');
});

async function search(): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-view', {});

  await (
    el as unknown as {
      executeIndexSearch: (v: number, q: string) => Promise<void>;
    }
  ).executeIndexSearch(0, 'tide');

  await flush();
  await el.updateComplete;

  return el;
}

describe('where Explore gets its artist portraits', () => {
  it('asks the disk about every unresolved artist in one call', async () => {
    await search();

    const cachedCalls = calls('explore.Service.GetArtistImagesCachedPaths');

    // Exactly one: the point is that N artists cost one disk lookup,
    // and a zero here would mean the search never ran.
    expect(cachedCalls.length).toBe(1);

    const asked = cachedCalls[0]?.args[0];

    expect(asked).toContain(CACHED);
    expect(asked).toContain(UNCACHED);
  });

  it('never sends a disk-cached artist to the resolver', async () => {
    await search();

    const resolved = calls('explore.Service.GetArtistImageURL').map(
      (c) => c.args[0],
    );

    expect(resolved).not.toContain(CACHED);
  });
});

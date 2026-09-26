/**
 * The artist page's header and its top tracks.
 *
 * Two cleanups, asserted together because they are one screen:
 *
 *  - the Play/Shuffle pair became one split button ("Play" with the
 *    words on its title, Shuffle behind the caret), the Follow button
 *    moved onto the same line, and the name and listen count went up a
 *    size;
 *  - a top track's play/request affordance moved onto its artwork,
 *    where a hover reveals it, instead of a badge at the end of the
 *    row beside a row that already plays on a double-click.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-artist-details/explore-artist-details';
import { stub, flush, emit, resetHarness } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadow, shadowAll } from '@test/support/render';

const ARTIST = 'artist-0001';

const track = (name: string, localId = 0) => ({
  recordingMbid: `rec-${name}`,
  artistName: 'Tideline',
  trackName: name,
  totalListenCount: 100,
  caaReleaseMbid: '',
  releaseName: 'Foreshore',
  releaseGroupMbid: 'rg-owned',
  length: 200000,
  inLibrary: localId > 0,
  localId,
});

beforeEach(() => {
  resetHarness();

  stub('explore.Service.LookupArtist', {
    mbid: ARTIST,
    name: 'Tideline',
    popularity: 1200,
    type: 'Group',
    country: 'GB',
  });
  stub('explore.Service.TopReleaseGroupsForArtist', []);
  stub('explore.Service.TopRecordingsForArtist', [
    track('Owned Song', 7),
    track('Absent Song'),
  ]);
  stub('explore.Service.SimilarArtists', []);
  stub('explore.Service.PrefetchReleases', undefined);
  stub('explore.Service.BrowseReleaseGroups', [
    {
      mbid: 'rg-owned',
      title: 'Foreshore',
      artistCredit: 'Tideline',
      primaryType: 'Album',
      inLibrary: true,
      localId: 7,
    },
  ]);
  stub('library.Library.GetAlbumsCompleteness', {});
  stub('download.Service.ListRequests', []);
});

async function mount(): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-artist-details', {
    artistMBID: ARTIST,
    artistName: 'Tideline',
  });

  await flush();

  return el;
}

describe('the artist header', () => {
  it('offers Play, with Shuffle behind its caret', async () => {
    const el = await mount();
    const play = shadow<HTMLElement>(el, '[data-testid="artist-play-library"]')!;

    // The words moved to the title, which is where "Play library
    // tracks" can still be read without taking the width of a button.
    expect(play.textContent?.trim()).toBe('Play');
    expect(play.getAttribute('title')).toBe('Play library tracks');

    const menuButton = shadow(el, '[data-testid="artist-play-menu"]');

    expect(menuButton).not.toBeNull();

    const menu = shadow(el, '#artist-play-menu');

    expect(menu?.textContent).toContain('Shuffle');
  });

  it('puts Follow on the same line as Play', async () => {
    const el = await mount();
    const actions = shadow(el, '.artist-actions')!;

    expect(actions.querySelector('[data-testid="artist-play-library"]')).not.toBeNull();

    const follow = actions.querySelector('[data-testid="artist-follow"]') as HTMLElement;

    expect(follow).not.toBeNull();
    expect(follow.textContent?.trim()).toBe('Follow');
  });

  it('says Following once the artist is on the request list', async () => {
    const el = await mount();

    // The store is a singleton and caches its list, so the change is
    // announced the way the backend announces one.
    stub('download.Service.ListRequests', [
      { id: 3, mbid: ARTIST, state: 'queued' },
    ]);
    emit(Events.RequestsChanged);
    await flush();
    await el.updateComplete;

    const follow = shadow<HTMLElement>(el, '[data-testid="artist-follow"]')!;

    expect(follow.textContent?.trim()).toBe('Following');
  });

  it('sizes the name and the listen count above the metadata line', async () => {
    const el = await mount();

    const title = shadow<HTMLElement>(el, '.artist-title')!;
    const listens = shadow<HTMLElement>(el, '.artist-listens')!;
    const meta = shadow<HTMLElement>(el, '.artist-meta')!;

    expect(listens.textContent).toContain('plays on ListenBrainz');

    const titleSize = parseFloat(getComputedStyle(title).fontSize);
    const listensSize = parseFloat(getComputedStyle(listens).fontSize);
    const metaSize = parseFloat(getComputedStyle(meta).fontSize);

    expect(titleSize).toBeGreaterThan(24);
    expect(listensSize).toBeGreaterThan(metaSize);
  });
});

describe('a top track’s affordance', () => {
  it('plays from the artwork when it is owned', async () => {
    const el = await mount();
    const rows = shadowAll<HTMLElement>(el, '.track-item');

    const owned = rows.find((r) => r.textContent?.includes('Owned Song'))!;

    expect(owned.querySelector('.track-art-overlay .track-art-play')).not.toBeNull();
    // Nothing beside the row any more.
    expect(owned.querySelector(':scope > library-status-indicator')).toBeNull();
  });

  it('requests from the artwork when it is not', async () => {
    const el = await mount();
    const rows = shadowAll<HTMLElement>(el, '.track-item');

    const absent = rows.find((r) => r.textContent?.includes('Absent Song'))!;

    expect(
      absent.querySelector('.track-art-overlay library-status-indicator'),
    ).not.toBeNull();
    expect(absent.querySelector('.track-art-overlay .track-art-play')).toBeNull();
  });
});

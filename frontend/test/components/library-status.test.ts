/**
 * Plan 009 phase 1: the badge tells the truth.
 *
 * `library-status-indicator` has had three states since it was written
 * — a tick, an hourglass and a plus — and the hourglass was produced by
 * nothing. All eight call sites were a two-way ternary, so an album the
 * user had already asked for through "Want this" showed a plus and said
 * it was not in their library, on the same page as a filled button
 * reading "Wanted".
 *
 * Two tiers of assertion here, and the second is the one that would
 * have failed:
 *
 *  - the rule itself, which is now written once, and
 *  - a rendered Explore result whose release group is requested,
 *    because a helper nobody calls is a rule nobody follows.
 */
import { beforeEach, describe, expect, it } from 'vitest';

import '@components/explore-view/explore-view';
import type { Request } from '@store/download-store';
import { libraryStatusFor } from '@utils/library-status';
import { Events } from '../../src/events';
import { emit, flush, stub } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';

const SEARCH = 'explore.Service.SearchLocal';

function request(overrides: Partial<Request>): Request {
  return {
    id: 1,
    mbid: 'rg-wanted',
    entity: 'release-group',
    libraryId: 1,
    artist: 'An Artist',
    title: 'Wanted Album',
    scope: 'future',
    secondary: false,
    state: 'wanted',
    attempts: 0,
    ...overrides,
  } as Request;
}

/** Put a request list into the store the way the backend does. */
async function withRequests(rows: Request[]): Promise<void> {
  stub('download.Service.ListRequests', rows);
  emit(Events.RequestsChanged);
  await flush();
}

const releaseGroup = (mbid: string, title: string) => ({
  mbid,
  title,
  artistCredit: 'An Artist',
  artistMbid: 'ar-1',
  primaryType: 'Album',
  firstReleaseDate: '1994-05-01',
  popularity: 100,
  listenerCount: 10,
  inLibrary: false,
  secondaryTypes: [],
});

describe('libraryStatusFor', () => {
  beforeEach(async () => {
    await withRequests([]);
  });

  it('says nothing about an entity with no MBID to ask about', () => {
    expect(libraryStatusFor(false, '')).toBe('not-in-library');
    expect(libraryStatusFor(false, undefined)).toBe('not-in-library');
  });

  it('reports a request as queued', async () => {
    await withRequests([request({ mbid: 'rg-wanted' })]);

    expect(libraryStatusFor(false, 'rg-wanted')).toBe('queued');
  });

  it('lets owning outrank wanting', async () => {
    // Both are true of an album that has arrived but whose request has
    // not been retired yet. What the user has is not news; what they
    // have is.
    await withRequests([request({ mbid: 'rg-wanted' })]);

    expect(libraryStatusFor(true, 'rg-wanted')).toBe('in-library');
  });

  it('does not call a satisfied request queued', async () => {
    // Nothing is coming: the request is history. An unowned entity with
    // a satisfied request is a stale row, not a download in flight.
    await withRequests([request({ mbid: 'rg-done', state: 'satisfied' })]);

    expect(libraryStatusFor(false, 'rg-done')).toBe('not-in-library');
  });

  it('does count a paused request, which the user did ask for', async () => {
    await withRequests([request({ mbid: 'rg-paused', state: 'paused' })]);

    expect(libraryStatusFor(false, 'rg-paused')).toBe('queued');
  });

  it('answers about the entity it is on, not the one containing it', () => {
    // A request is by MBID. A track inside a requested album is not
    // itself requested, and saying otherwise promises that clicking it
    // would find that recording.
    expect(libraryStatusFor(false, 'recording-inside-rg-wanted')).toBe(
      'not-in-library',
    );
  });
});

describe('<explore-view> badges', () => {
  beforeEach(async () => {
    stub('explore.Service.GetThumbnails', []);
    stub('explore.Service.GetThumbnail', '');
    stub('explore.Service.GetArtistImageURL', '');
    stub('explore.Service.GetExploreShelves', { shelves: [], state: 'ready' });
    stub('library.Library.GetAllAlbums', []);
    stub('library.Library.GetAllTracks', []);
    await withRequests([]);
  });

  async function searchFor(rows: ReturnType<typeof releaseGroup>[]) {
    stub(SEARCH, {
      artists: [],
      releaseGroups: rows,
      recordings: [],
      topResults: [],
    });

    const el = await fixture('explore-view');
    (el as unknown as { viewActivated(): void }).viewActivated();
    await flush();

    const input = shadow<HTMLInputElement>(el, 'input');
    if (input) {
      input.value = 'anything';
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }

    // The search box debounces, so waiting a frame measures the input
    // echoing its own character.
    await new Promise((resolve) => setTimeout(resolve, 300));
    await flush();
    await el.updateComplete;

    return el;
  }

  it('shows a requested album as queued rather than as absent', async () => {
    await withRequests([request({ mbid: 'rg-wanted' })]);

    const el = await searchFor([
      releaseGroup('rg-wanted', 'Wanted Album'),
      releaseGroup('rg-other', 'Some Other Album'),
    ]);

    const badges = shadowAll(el, 'library-status-indicator');

    expect(badges.length).toBeGreaterThanOrEqual(2);
    expect(badges.map((b) => b.getAttribute('status'))).toEqual([
      'queued',
      'not-in-library',
    ]);
  });

  it('re-renders when the request list changes underneath it', async () => {
    // A background reconcile pass expands an artist or retires a want
    // without this page doing anything, so the badge has to be told.
    const el = await searchFor([releaseGroup('rg-wanted', 'Wanted Album')]);

    expect(shadow(el, 'library-status-indicator')?.getAttribute('status')).toBe(
      'not-in-library',
    );

    await withRequests([request({ mbid: 'rg-wanted' })]);
    await el.updateComplete;

    expect(shadow(el, 'library-status-indicator')?.getAttribute('status')).toBe(
      'queued',
    );
  });
});

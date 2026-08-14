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
import { notificationStore } from '@store/notification-store';
import {
  calls,
  emit,
  flush,
  lastArgs,
  stub,
  stubFailure,
} from '@test/support/harness';
import { fixture, shadow, shadowAll, update } from '@test/support/render';

const SEARCH = 'explore.Service.SearchLocal';

// v3's bindings type `state` and `entity` as real enums where v2 typed
// them as strings; the fixture widens both back to their value unions.
type RequestOverrides = Partial<Omit<Request, 'state' | 'entity'>> & {
  state?: `${Request['state']}`;
  entity?: `${Request['entity']}`;
};

function request(overrides: RequestOverrides): Request {
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

/**
 * Plan 009 phase 3: the badge becomes a button where it can act.
 *
 * 007 made it `role="img"` because a control that cannot act is worse
 * than none, and wrote down what would change the answer: a `<button>`
 * *with* a handler. Both halves of that are asserted here — the badge
 * branch is still a badge (the tests in `chrome.test.ts` pin it, and
 * they pass unchanged because a call site has to opt in), and the
 * button branch actually files a request.
 */
describe('<library-status-indicator> as a control', () => {
  beforeEach(async () => {
    stub('download.Service.ListRequests', []);
    stub('download.Service.AddRequest', 7);
    stub('download.Service.RemoveRequest', null);
    stub('library.Library.GetAllLibrariesWithTrackCounts', [
      { id: 3, name: 'Music' },
    ]);
    notificationStore.clear();
    await withRequests([]);
  });

  const badge = (props: Record<string, unknown> = {}) =>
    fixture('library-status-indicator', {
      entityType: 'album',
      label: 'Abbey Road',
      ...props,
    });

  it('is a button only where a call site opted in', async () => {
    const inert = await badge();
    const control = await badge({ requestMbid: 'rg-1' });

    expect(shadow(inert, 'button')).toBeNull();
    expect(shadow(inert, '.badge')?.getAttribute('role')).toBe('img');
    expect(shadow(control, 'button')).not.toBeNull();
  });

  it('is never a button for something already owned', async () => {
    // There is nothing left to ask for, so the tab stop would cost the
    // keyboard user exactly what 007 gave back.
    const el = await badge({ requestMbid: 'rg-1', status: 'in-library' });

    expect(shadow(el, 'button')).toBeNull();
  });

  it('is named after what activating it does', async () => {
    const el = await badge({ requestMbid: 'rg-1' });

    expect(shadow(el, '.badge')?.getAttribute('aria-label')).toBe(
      'Want album "Abbey Road"',
    );

    await update(el, { status: 'queued' });

    expect(shadow(el, '.badge')?.getAttribute('aria-label')).toBe(
      'Cancel the request for album "Abbey Road"',
    );
  });

  it('still describes rather than offers where it cannot act', async () => {
    const el = await badge();

    expect(shadow(el, '.badge')?.getAttribute('aria-label')).toBe(
      'Album "Abbey Road" is not in your library',
    );
  });

  it('files a request for the entity it is on', async () => {
    const el = await badge({ requestMbid: 'rg-1', requestArtist: 'The Beatles' });

    shadow<HTMLElement>(el, 'button')?.click();
    await flush();

    expect(lastArgs('download.Service.AddRequest')?.[0]).toMatchObject({
      mbid: 'rg-1',
      entity: 'release-group',
      title: 'Abbey Road',
      artist: 'The Beatles',
      libraryId: 3,
    });
  });

  it('asks for a recording when it is on a track', async () => {
    const el = await badge({
      entityType: 'track',
      label: 'Come Together',
      requestMbid: 'rec-1',
    });

    shadow<HTMLElement>(el, 'button')?.click();
    await flush();

    expect(lastArgs('download.Service.AddRequest')?.[0]).toMatchObject({
      entity: 'recording',
    });
  });

  it('cancels a request it already made', async () => {
    await withRequests([request({ id: 42, mbid: 'rg-1' })]);

    const el = await badge({ requestMbid: 'rg-1', status: 'queued' });

    shadow<HTMLElement>(el, 'button')?.click();
    await flush();

    expect(lastArgs('download.Service.RemoveRequest')).toEqual([42]);
    expect(calls('download.Service.AddRequest')).toEqual([]);
  });

  it('keeps its click off the card it sits on', async () => {
    // The inverse of the badge branch, and for the opposite reason:
    // with an action of its own, a click on it no longer means what
    // the card means.
    const el = await badge({ requestMbid: 'rg-1' });
    const button = shadow<HTMLElement>(el, 'button');
    let bubbled = 0;

    el.addEventListener('click', () => {
      bubbled += 1;
    });

    button?.click();
    await flush();

    // Both halves, because "nothing bubbled" is free on a build with
    // no button to click: `?.click()` on null is a silent no-op and
    // this passed on the neutered build until it also asserted that
    // the click did the thing it was swallowed for.
    expect([button !== null, bubbled, calls('download.Service.AddRequest')
      .length]).toEqual([true, 0, 1]);
  });

  it('keeps Enter and Space off it too', async () => {
    // Every card holding one of these is a role=button or role=option
    // with its own Enter/Space handler, so without this a keyboard
    // activation would file the request *and* open the page.
    const el = await badge({ requestMbid: 'rg-1' });
    const seen: string[] = [];

    el.addEventListener('keydown', (e) => seen.push((e as KeyboardEvent).key));

    for (const key of ['Enter', ' ', 'ArrowDown']) {
      shadow<HTMLElement>(el, 'button')?.dispatchEvent(
        new KeyboardEvent('keydown', { key, bubbles: true, composed: true }),
      );
    }

    // ArrowDown is not ours: the grid still moves by it.
    expect(seen).toEqual(['ArrowDown']);
  });

  it('says so when the request could not be filed', async () => {
    stubFailure('download.Service.AddRequest', 'nope');

    const el = await badge({ requestMbid: 'rg-1' });

    shadow<HTMLElement>(el, 'button')?.click();
    await flush();

    expect(notificationStore.getAll().map((n) => n.level)).toEqual([
      'transient',
    ]);
    // The badge is where it was, which is why the toast is transient.
    expect(shadow(el, 'button')).not.toBeNull();
  });
});

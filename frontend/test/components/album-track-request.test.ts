/**
 * Issue #33: "Want track" filed the request and left the badge alone.
 *
 * The tracklist's badges read `libraryStatusFor(false, track.mbid)` at
 * render time, which is a dependency on `downloadStore` that Lit cannot
 * see. `explore-album-details` did subscribe to that store, but its
 * callback only assigned `canDownload` and `isRequested` — neither of
 * which a *track* request changes — so nothing in the component's
 * reactive state moved and the page never re-rendered. The request was
 * real, the plus stayed a plus, and clicking again cancelled it.
 *
 * The other three hosts rendering these badges (`explore-artist-
 * details`, `explore-view`, `top-results-row`) have always asked for
 * the repaint in the same place, which is what made this one look
 * correct on inspection.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-album-details/explore-album-details';
import type { Request } from '@store/download-store';
import { Events } from '../../src/events';
import { stub, flush, resetHarness, emit } from '@test/support/harness';
import { fixture, shadowAll } from '@test/support/render';

type RequestOverrides = Partial<Omit<Request, 'state' | 'entity'>> & {
  state?: `${Request['state']}`;
  entity?: `${Request['entity']}`;
};

function request(overrides: RequestOverrides): Request {
  return {
    id: 1,
    mbid: 'mbid-1',
    entity: 'recording',
    libraryId: 1,
    artist: 'An Artist',
    title: 'Track 1',
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

function track(n: number) {
  return {
    position: n,
    discNumber: 1,
    title: `Track ${n}`,
    length: 200000,
    mbid: `mbid-${n}`,
    inLibrary: false,
  };
}

/** An unowned catalog tracklist, which is the only case with badges:
 *  an owned row renders none, there being nothing left to ask for. */
async function withTracklist(count: number): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-album-details', {
    albumName: 'Glass Harbour',
  });

  Object.assign(el, {
    versionEntries: [
      {
        key: 'v1',
        label: '2019',
        sublabel: `${count} tracks`,
        tracks: Array.from({ length: count }, (_, i) => track(i + 1)),
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

/** The status of each track badge, in tracklist order. */
function badgeStatuses(el: LitElement): string[] {
  return shadowAll(el, 'library-status-indicator.track-request').map(
    (b) => b.getAttribute('status') ?? '',
  );
}

describe('the album tracklist’s request badges', () => {
  beforeEach(async () => {
    resetHarness();
    stub('library.Library.GetFilePathsByRecordingMBIDs', {});
    stub('library.Library.GetFilePathsByAlbums', {});
    stub('library.Library.GetAlbumTracks', []);
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    await withRequests([]);
  });

  it('starts as a plus on every unowned row', async () => {
    const el = await withTracklist(3);

    expect(badgeStatuses(el)).toEqual([
      'not-in-library',
      'not-in-library',
      'not-in-library',
    ]);
  });

  it('repaints the row whose track has been requested', async () => {
    const el = await withTracklist(3);

    await withRequests([request({ mbid: 'mbid-2' })]);
    await el.updateComplete;

    // Only the requested row moves. A request is by MBID, so the two
    // rows either side of it are still a plus.
    expect(badgeStatuses(el)).toEqual([
      'not-in-library',
      'queued',
      'not-in-library',
    ]);
  });

  it('repaints again when the request is cancelled', async () => {
    const el = await withTracklist(3);

    await withRequests([request({ mbid: 'mbid-2' })]);
    await el.updateComplete;

    await withRequests([]);
    await el.updateComplete;

    expect(badgeStatuses(el)).toEqual([
      'not-in-library',
      'not-in-library',
      'not-in-library',
    ]);
  });
});

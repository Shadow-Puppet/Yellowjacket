/**
 * "Track Details" and "Add to Playlist" on Explore's owned tracks.
 *
 * The library-side lists have had this item for as long as the dialog
 * has existed; Explore's tracklists — the album page's and the artist
 * page's top tracks — had Play, Add to Queue and Play Next and stopped
 * there. The reason it is worth a test rather than being one more
 * `<wa-dropdown-item>` is that the two sides hold different things: a
 * library row *is* a `library.Track`, and an Explore row is the
 * catalog's, which can name a file only through its recording MBID.
 *
 * So what this pins is the join. Both items appear only for a track the
 * user owns (an unowned one has no file, and both are about a file):
 * Track Details resolves MBID → path → the library's own track and
 * hands *that* to the dialog, and Add to Playlist resolves the same
 * path and hands it to the shared picker.
 */
import type { LitElement } from 'lit';
import { beforeEach, describe, expect, it } from 'vitest';

import '@components/explore-album-details/explore-album-details';
import { emit, flush, stub } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadow, shadowAll } from '@test/support/render';
import { showTrackDetailsForPath } from '@utils/track-details-opener';
import type { TrackDetails } from '@components/track-details/track-details';

const ALBUM_TRACKS = 'library.Library.GetAlbumTracks';
const COMPLETENESS = 'library.Library.GetAlbumCompleteness';
const FILE_PATHS = 'library.Library.GetFilePathsByRecordingMBIDs';
const ALL_TRACKS = 'library.Library.GetTracks';
const LOOKUP_RG = 'explore.Service.LookupReleaseGroup';
const BROWSE_RELEASES = 'explore.Service.BrowseReleases';

const PATH = '/music/an-album/01.flac';
const MBID = 'rec-1';

/** One row as `GetAlbumTracks` returns it. */
const albumTrack = {
  ID: 1,
  FilePath: PATH,
  TrackName: 'A Song',
  TrackNumber: 1,
  DiscNumber: 1,
  TrackLength: '3:00',
  RecordingMBID: MBID,
};

/** The same track as the library's own model, which is what the dialog wants. */
const libraryTrack = {
  ID: 1,
  FilePath: PATH,
  Title: 'A Song',
  Artist: 'An Artist',
  Album: 'An Album',
  CoverArtPath: '/covers/a.jpg',
  CoverArtSmall: '/covers/a-64.jpg',
  CoverArtMedium: '/covers/a-256.jpg',
  CoverArtLarge: '/covers/a-512.jpg',
};

/**
 * Mount the album page as a library-only album, which is the cheapest
 * route to a rendered tracklist: no MBID means it hydrates entirely
 * from `GetAlbumTracks` and asks the catalog nothing.
 */
async function albumPage() {
  return fixture('explore-album-details', { localAlbumId: 7 });
}

/** Open the context menu on the first track row and return its items. */
async function openTrackMenu(el: LitElement) {
  const row = shadow(el, '.track-row');

  expect(row, 'a track row is rendered').not.toBeNull();

  row!.dispatchEvent(
    new MouseEvent('contextmenu', { bubbles: true, cancelable: true }),
  );
  await flush();
  await el.updateComplete;

  return shadowAll(el, '.context-menu-panel wa-dropdown-item');
}

/** The track the page's `<track-details>` was opened on, once it has one. */
async function dialogTrack(
  el: LitElement,
  attempts = 100,
): Promise<{ FilePath: string } | null> {
  for (let i = 0; i < attempts; i += 1) {
    const dialog = shadow<TrackDetails>(el, 'track-details') as unknown as {
      track?: { FilePath: string } | null;
    } | null;

    if (dialog?.track) return dialog.track;

    await flush();
  }

  return null;
}

/** The file paths handed to the playlist picker, once it is mounted. */
async function pickerPaths(
  el: LitElement,
  attempts = 100,
): Promise<string[] | null> {
  for (let i = 0; i < attempts; i += 1) {
    const picker = shadow(el, 'playlist-picker') as unknown as {
      filePaths?: string[];
    } | null;

    if (picker?.filePaths?.length) return picker.filePaths;

    await flush();
  }

  return null;
}

const labels = (items: Element[]) =>
  items.map((i) => (i.textContent ?? '').trim());

describe('Explore track details', () => {
  beforeEach(() => {
    stub(ALBUM_TRACKS, [albumTrack]);
    stub(COMPLETENESS, { known: true, complete: true, owned: 1, expected: 1 });
    stub(FILE_PATHS, { [MBID]: [PATH] });
    stub(ALL_TRACKS, [libraryTrack]);
  });

  /**
   * `libraryStore` fetches at import and caches the empty list the
   * shared setup stubs, for the life of the browser session — so a test
   * that wants tracks in it has to say so. A scan-complete event is how
   * the app itself invalidates that cache.
   */
  async function primeLibrary() {
    emit(Events.LibraryScanComplete);
    await flush();
  }

  it('offers Track Details on an owned track', async () => {
    const el = await albumPage();
    const items = await openTrackMenu(el);

    expect(labels(items)).toContain('Track Details');
  });

  it('does not offer it on a track the library does not have', async () => {
    // A catalog album the user owns nothing of: the tracklist renders
    // from the browse, and every row is unowned.
    stub(ALBUM_TRACKS, []);
    stub(LOOKUP_RG, {
      mbid: 'rg-1',
      title: 'An Album',
      artistCredit: 'An Artist',
      firstReleaseDate: '1994',
      primaryType: 'Album',
    });
    stub(BROWSE_RELEASES, [
      {
        mbid: 'rel-1',
        title: 'An Album',
        date: '1994',
        country: 'GB',
        tracks: [
          {
            mbid: 'rec-2',
            title: 'Another Song',
            position: 1,
            length: 180000,
            discNumber: 1,
            inLibrary: false,
          },
        ],
      },
    ]);

    const el = await fixture<LitElement>('explore-album-details', {
      releaseGroupMBID: 'rg-1',
    });
    const items = await openTrackMenu(el);

    expect(labels(items)).not.toContain('Track Details');
    expect(
      labels(items).some((l) => l.startsWith('Add to Playlist')),
      'nothing to add to a playlist when there is no file',
    ).toBe(false);
    // The one item a track nobody owns still has, which is what makes
    // the assertion above about the gate rather than about an empty
    // menu that never opened.
    expect(labels(items)).toContain('View on MusicBrainz');
  });

  it('opens the dialog on the library track behind the row', async () => {
    await primeLibrary();

    const el = await albumPage();
    const items = await openTrackMenu(el);
    const details = labels(items).indexOf('Track Details');

    items[details]!.dispatchEvent(new MouseEvent('click', { bubbles: true }));

    // Polled rather than counted: the opener is three awaits deep — the
    // path lookup, the store's tracks, and the dynamic `import()` of
    // the dialog chunk — and a chunk fetch is the one of the three
    // whose cost depends on what else the suite is doing.
    const shown = await dialogTrack(el);

    expect(shown?.FilePath).toBe(PATH);
  });

  it('opens the playlist submenu on the row’s own file', async () => {
    const el = await albumPage();
    const items = await openTrackMenu(el);
    // Its label carries the submenu arrow, so match the prefix.
    const add = labels(items).findIndex((l) => l.startsWith('Add to Playlist'));

    expect(add, 'the submenu item is in the menu').toBeGreaterThan(-1);

    items[add]!.dispatchEvent(new MouseEvent('click', { bubbles: true }));

    // Resolved by MBID at the moment the submenu opens, so the picker
    // is not there on the first tick the way a library list's is.
    const picker = await pickerPaths(el);

    expect(picker).toEqual([PATH]);
  });

  it('reports a path with no library track rather than opening empty', async () => {
    stub(ALL_TRACKS, []);
    await primeLibrary();

    const outcome = await showTrackDetailsForPath(
      () => undefined,
      PATH,
      () => undefined,
    );

    expect(outcome).toBe('not-in-library');
  });
});

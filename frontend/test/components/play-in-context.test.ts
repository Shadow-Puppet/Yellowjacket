/**
 * Playing a track plays the list it is in.
 *
 * Double-clicking a row — or picking Play from its context menu — used
 * to call `SetQueue([thatOnePath], 0)` on the album page, in the track
 * list and in the two playlist views' menus: the queue became one
 * track, the rest of the album was discarded, and playback stopped at
 * the end of it. What a player means by activating a row is "start
 * here", and the here is a position in the list on screen.
 *
 * So the queue is the *displayed* list and `startIndex` is the row.
 * Two rules ride along and are what these tests are mostly for:
 *
 * - The album page's list is the tracks it has files for, so the index
 *   is into that and not into the tracklist that includes the dimmed
 *   rows — off-by-however-many-you-do-not-own is silent, it just plays
 *   the wrong song.
 * - A menu asks how much is selected. One row means "from here"; an
 *   explicit multi-row selection means play exactly those, which is
 *   the one case where a queue of the selection is what was asked for.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-album-details/explore-album-details';
import '@components/track-list/track-list';
import '@components/playlist-details/playlist-details';
import { stub, flush, resetHarness, lastArgs } from '@test/support/harness';
import { fixture, shadowAll } from '@test/support/render';

/** What `Queue.SetQueue` was last asked to play, and from where. */
function queued(): { paths: string[]; startIndex: number } {
  const args = lastArgs('queue.Queue.SetQueue');

  if (!args) throw new Error('nothing was queued');

  return {
    paths: args[0] as string[],
    startIndex: args[1] as number,
  };
}

function dblclick(el: Element): void {
  el.dispatchEvent(
    new MouseEvent('dblclick', { bubbles: true, composed: true }),
  );
}

// =====================================================================
// The album page
// =====================================================================

function albumTrack(n: number, owned: boolean) {
  return {
    position: n,
    discNumber: 1,
    title: `Track ${n}`,
    length: 200000,
    mbid: `mbid-${n}`,
    inLibrary: owned,
  };
}

/**
 * A release on the page without the network, owned every `nth` track.
 * The fixture says which tracks have *files*, because that is the one
 * question the page asks about ownership.
 */
async function album(total: number, ownedMbids: string[]): Promise<LitElement> {
  const el = await fixture<LitElement>('explore-album-details', {
    albumName: 'Glass Harbour',
  });

  const tracks = Array.from({ length: total }, (_, i) =>
    albumTrack(i + 1, ownedMbids.includes(`mbid-${i + 1}`)),
  );

  const paths: Record<string, string[]> = {};
  for (const mbid of ownedMbids) paths[mbid] = [`/music/${mbid}.mp3`];

  stub('library.Library.GetFilePathsByRecordingMBIDs', paths);

  Object.assign(el, {
    versionEntries: [
      { key: 'v1', label: '2019', sublabel: `${total} tracks`, tracks },
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

describe('double-clicking a track on the album page', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetFilePathsByAlbums', {});
    stub('library.Library.GetAlbumTracks', []);
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
  });

  it('queues the album and starts on that track', async () => {
    const el = await album(6, ['mbid-1', 'mbid-2', 'mbid-3', 'mbid-4', 'mbid-5', 'mbid-6']);

    dblclick(shadowAll(el, '.track-row')[2]!);
    await flush();

    expect(queued()).toEqual({
      paths: [1, 2, 3, 4, 5, 6].map((n) => `/music/mbid-${n}.mp3`),
      startIndex: 2,
    });
  });

  it('indexes into what is owned, not into the rows on screen', async () => {
    // Tracks 2, 5 and 6 have files; the other three rows are dimmed and
    // are not in the queue at all. Row 5 is therefore the *second*
    // thing that will play, and an index taken from the row would start
    // this album past its end.
    const el = await album(6, ['mbid-2', 'mbid-5', 'mbid-6']);

    dblclick(shadowAll(el, '.track-row')[4]!);
    await flush();

    expect(queued()).toEqual({
      paths: ['/music/mbid-2.mp3', '/music/mbid-5.mp3', '/music/mbid-6.mp3'],
      startIndex: 1,
    });
  });

  it('does nothing at all on a row with no file behind it', async () => {
    const el = await album(6, ['mbid-2']);

    dblclick(shadowAll(el, '.track-row')[0]!);
    await flush();

    expect(lastArgs('queue.Queue.SetQueue')).toBeUndefined();
  });
});

// =====================================================================
// The track list
// =====================================================================

const LIST = Array.from({ length: 12 }, (_, i) => ({
  FilePath: `/music/track-${i}.mp3`,
  TrackName: `Track ${i}`,
  ArtistName: 'An Artist',
  Album: 'An Album',
  Duration: 180,
})) as never[];

describe('double-clicking a row in the track list', () => {
  let el: LitElement;

  beforeEach(async () => {
    resetHarness();
    localStorage.removeItem('track-list-column-widths');

    el = await fixture<LitElement>('track-list', { externalTracks: LIST });

    // Say which order is being asserted rather than inheriting one.
    // `restoreSortPreferences()` runs in `connectedCallback`, so the
    // list opens in whatever sort was last persisted -- and
    // `track-11` sorts before `track-3` by title, which is the shape
    // of #138. The row below is the fixture's third track only while
    // nothing is sorting the list.
    (el as unknown as { sortField: string | null }).sortField = null;

    el.style.display = 'block';
    el.style.height = '600px';
    await flush();
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));
  });

  it('queues the list as displayed and starts on that row', async () => {
    const rows = shadowAll(el, '.track-row');
    const row = rows.find((r) => r.getAttribute('data-index') === '3');

    // Stated first, so a list that is not in the order this asserts
    // fails by saying so rather than as an off-by-eight file path.
    expect(row?.getAttribute('data-file-path')).toBe('/music/track-3.mp3');

    dblclick(row!);
    await flush();

    const { paths, startIndex } = queued();

    expect([paths.length, paths[startIndex]]).toEqual([
      12,
      '/music/track-3.mp3',
    ]);
  });
});

// =====================================================================
// A playlist's context menu
// =====================================================================

function playlistTracks(n: number) {
  return Array.from({ length: n }, (_, i) => ({
    ID: i + 1,
    FilePath: `/music/track-${i}.mp3`,
    Title: `Track ${i}`,
    Artist: 'An Artist',
    Album: 'An Album',
    Duration: 180000,
    Phantom: false,
  }));
}

describe('Play from a playlist row’s context menu', () => {
  let el: LitElement;

  beforeEach(async () => {
    resetHarness();
    stub('playlist.Service.GetPlaylistTracks', playlistTracks(8));
    stub('playlist.Service.GetAllPlaylists', []);

    el = await fixture<LitElement>('playlist-details', {
      playlistId: 1,
      playlistName: 'A playlist',
    });
    el.style.display = 'block';
    el.style.height = '600px';
    await flush();
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));
  });

  /** Right-click a row, then click the menu's Play item. */
  async function playFromMenu(index: number): Promise<void> {
    const row = shadowAll(el, '.track-item').find(
      (r) => r.getAttribute('data-index') === String(index),
    );

    row!.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, composed: true }),
    );
    await el.updateComplete;

    const items = shadowAll<HTMLElement>(el, 'wa-dropdown-item');
    const play = items.find((i) => i.textContent?.trim().startsWith('Play') &&
      !i.textContent.includes('Next'));

    play!.click();
    await flush();
  }

  it('starts the playlist from the row that was clicked', async () => {
    await playFromMenu(5);

    const { paths, startIndex } = queued();

    expect([paths.length, startIndex]).toEqual([8, 5]);
  });

  it('plays only the selection when several rows are selected', async () => {
    // The one case where a queue of the selection is what was asked
    // for: the user said which tracks, not where to start.
    const rows = shadowAll(el, '.track-item');
    const click = (i: number, modifiers: MouseEventInit) =>
      rows
        .find((r) => r.getAttribute('data-index') === String(i))!
        .dispatchEvent(
          new MouseEvent('click', { bubbles: true, composed: true, ...modifiers }),
        );

    click(1, {});
    click(4, { ctrlKey: true });
    await el.updateComplete;

    await playFromMenu(4);

    expect(queued().paths).toEqual([
      '/music/track-1.mp3',
      '/music/track-4.mp3',
    ]);
  });
});

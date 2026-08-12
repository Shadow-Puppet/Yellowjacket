/**
 * The playlist store caches one list and invalidates it on six
 * different events. Two distinctions are worth testing. `invalidate` vs
 * `refetch`: one drops the cache (consumers render empty until the
 * fetch lands), the other holds the stale list until the new one
 * arrives — using the wrong one shows up as a flash of empty list.
 *
 * And `invalidate` vs *patch*: `GetAllPlaylistsWithTracks` returns every
 * row of every playlist with full track metadata, which is the wrong
 * answer to "one track was added to playlist 2" and was measured at
 * 2.61 MB for one heart toggle (`perf.C5`). The event carries the id.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { playlistStore } from '@store/playlist-store';
import { Events } from '../../src/events';
import { emit, calls, stub, flush, resetHarness } from '@test/support/harness';

/** The real shape: `WithTracks` is `{ Summary, Tracks }`. */
const PLAYLISTS = [
  {
    Summary: { ID: 1, Name: 'Morning', UpdatedAt: '2026-01-01T00:00:00Z' },
    Tracks: [{ FilePath: '/a.mp3', Title: 'One' }],
  },
  {
    Summary: { ID: 2, Name: 'Evening', UpdatedAt: '2026-01-01T00:00:00Z' },
    Tracks: [{ FilePath: '/b.mp3', Title: 'Two' }],
  },
];

const SUMMARIES = PLAYLISTS.map((p) => p.Summary);

/**
 * The store only refetches eagerly when something is subscribed —
 * before `playlist-view` has ever been opened there is no reader and
 * nothing to refresh. Tests that assert on a refetch therefore need a
 * subscriber, exactly as the running app does.
 */
let unsubscribe: (() => void) | null = null;

function stubReads(): void {
  stub('playlist.Service.GetAllPlaylistsWithTracks', PLAYLISTS);
  stub('playlist.Service.GetAllPlaylists', SUMMARIES);
  stub('playlist.Service.GetPlaylistTracks', []);
}

async function reload(): Promise<void> {
  unsubscribe?.();
  unsubscribe = playlistStore.subscribe(() => {});
  stubReads();
  playlistStore.invalidate();
  await flush();
  resetHarness();
  stubReads();
}

describe('playlist store: caching', () => {
  beforeEach(async () => {
    await reload();
  });

  it('serves a second read from cache', async () => {
    await playlistStore.getPlaylists();

    expect(calls()).toEqual([]);
  });

  it('deduplicates concurrent first reads', async () => {
    playlistStore.invalidate();
    await Promise.all([
      playlistStore.getPlaylists(),
      playlistStore.getPlaylists(),
    ]);

    expect(calls('playlist.Service.GetAllPlaylistsWithTracks')).toHaveLength(1);
  });

  it('exposes the cache synchronously for render', () => {
    expect(playlistStore.getCachedPlaylists()).toEqual(PLAYLISTS);
  });

  it('normalises a null list to an empty one', async () => {
    stub('playlist.Service.GetAllPlaylistsWithTracks', null);
    playlistStore.invalidate();
    await flush();

    expect(playlistStore.getCachedPlaylists()).toEqual([]);
  });

  it('holds the stale list across a refetch, so the view does not flash empty', async () => {
    const pending = playlistStore.refetch();
    const during = playlistStore.getCachedPlaylists();

    await pending;

    expect(during).toEqual(PLAYLISTS);
  });

  it('drops the cache on invalidate, which is the difference from refetch', () => {
    playlistStore.invalidate();

    expect(playlistStore.getCachedPlaylists()).toBeNull();
  });

  it('resets the scroll position when the list is invalidated', async () => {
    playlistStore.setScrollPosition(900);
    playlistStore.invalidate();
    await flush();

    expect(playlistStore.getScrollPosition()).toBe(0);
  });
});

describe('playlist store: invalidating events', () => {
  beforeEach(async () => {
    await reload();
  });

  it('refetches everything for every event that can restructure the list', async () => {
    const events = [
      Events.PlaylistCreated,
      Events.PlaylistDeleted,
      Events.PlaylistRenamed,
      Events.PlaylistsRestored,
      Events.LibraryScanComplete,
    ];

    for (const name of events) {
      emit(name, 1);
      await flush();
    }

    expect(
      calls('playlist.Service.GetAllPlaylistsWithTracks'),
    ).toHaveLength(events.length);
  });

  it('does not refetch when nothing is subscribed', async () => {
    unsubscribe?.();
    unsubscribe = null;

    emit(Events.PlaylistCreated, 1);
    await flush();

    expect(calls('playlist.Service.GetAllPlaylistsWithTracks')).toEqual([]);
    // Still dropped, so the next reader fetches rather than serving a
    // list the backend has moved on from.
    expect(playlistStore.getCachedPlaylists()).toBeNull();
  });
});

describe('playlist store: patching one playlist (perf.C5)', () => {
  beforeEach(async () => {
    await reload();
  });

  it('refetches only the playlist the event names', async () => {
    stub('playlist.Service.GetPlaylistTracks', [
      { FilePath: '/b.mp3', Title: 'Two' },
      { FilePath: '/c.mp3', Title: 'Three' },
    ]);

    emit(Events.PlaylistTracksChanged, 2);
    await flush();

    expect(calls('playlist.Service.GetAllPlaylistsWithTracks')).toEqual([]);
    expect(calls('playlist.Service.GetPlaylistTracks')).toHaveLength(1);

    const cached = playlistStore.getCachedPlaylists() ?? [];
    expect(cached).toHaveLength(2);
    expect(cached[1]?.Tracks).toHaveLength(2);
  });

  it('shares the tracks of every playlist that did not change', async () => {
    const before = playlistStore.getCachedPlaylists() ?? [];

    emit(Events.PlaylistTracksChanged, 2);
    await flush();

    const after = playlistStore.getCachedPlaylists() ?? [];

    // A new array identity, because `playlist-view` keys its reload off
    // it — but the untouched playlist's tracks are the same objects.
    // Asserted non-empty first, or two `undefined`s would pass this.
    expect(before[0]?.Tracks).toBeDefined();
    expect(after).not.toBe(before);
    expect(after[0]?.Tracks).toBe(before[0]?.Tracks);
  });

  it('refreshes summaries, which carry the sort key', async () => {
    stub('playlist.Service.GetAllPlaylists', [
      SUMMARIES[0],
      { ...SUMMARIES[1], UpdatedAt: '2026-06-01T00:00:00Z' },
    ]);

    emit(Events.PlaylistTracksChanged, 2);
    await flush();

    expect(
      (playlistStore.getCachedPlaylists() ?? [])[1]?.Summary.UpdatedAt,
    ).toBe('2026-06-01T00:00:00Z');
  });

  it('falls back to a full refetch when the event carries no id', async () => {
    // The bulk restore and reorder paths emit a nil id, which says
    // "something changed" without saying what.
    emit(Events.PlaylistTracksChanged, null);
    await flush();

    expect(
      calls('playlist.Service.GetAllPlaylistsWithTracks'),
    ).toHaveLength(1);
  });

  it('falls back to a full refetch for a playlist it has never seen', async () => {
    emit(Events.PlaylistTracksChanged, 99);
    await flush();

    expect(
      calls('playlist.Service.GetAllPlaylistsWithTracks'),
    ).toHaveLength(1);
  });

  it('does not patch a cold cache, or one with a fetch already in flight', async () => {
    // Both cases arrive as "there is nothing here to patch, and a full
    // fetch either is happening or is about to" — patching would race
    // that fetch and be overwritten by it.
    playlistStore.invalidate();

    emit(Events.PlaylistTracksChanged, 2);
    await flush();

    expect(calls('playlist.Service.GetPlaylistTracks')).toEqual([]);
    expect(playlistStore.getCachedPlaylists()).toEqual(PLAYLISTS);
  });
});

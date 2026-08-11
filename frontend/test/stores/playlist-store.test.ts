/**
 * The playlist store caches one list and invalidates it on six
 * different events. The distinction worth testing is `invalidate` vs
 * `refetch`: one drops the cache (consumers render empty until the
 * fetch lands), the other holds the stale list until the new one
 * arrives. Using the wrong one shows up as a flash of empty list.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { playlistStore } from '@store/playlist-store';
import { Events } from '../../src/events';
import { emit, calls, stub, flush, resetHarness } from '@test/support/harness';

const PLAYLISTS = [
  { ID: 1, Name: 'Morning', Tracks: [] },
  { ID: 2, Name: 'Evening', Tracks: [] },
];

async function reload(): Promise<void> {
  stub('playlist.Service.GetAllPlaylistsWithTracks', PLAYLISTS);
  playlistStore.invalidate();
  await flush();
  resetHarness();
  stub('playlist.Service.GetAllPlaylistsWithTracks', PLAYLISTS);
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

  it('refetches for every event that can change a playlist', async () => {
    const events = [
      Events.PlaylistCreated,
      Events.PlaylistDeleted,
      Events.PlaylistRenamed,
      Events.PlaylistTracksChanged,
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
});

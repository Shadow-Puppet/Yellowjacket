/**
 * Favourites is the one store that updates optimistically: the heart
 * fills before Go has agreed. So the interesting cases are the reverts,
 * and the set of playlist events that force a reload — a default
 * playlist edited elsewhere has to show up here.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { favoritesStore } from '@store/favorites-store';
import { Events } from '../../src/events';
import {
  emit,
  calls,
  stub,
  stubFailure,
  flush,
  lastArgs,
  resetHarness,
} from '@test/support/harness';

const PATHS = ['/music/a.mp3', '/music/b.mp3'];

function stubReads(paths: string[] = PATHS): void {
  stub('playlist.Service.GetDefaultPlaylistTrackPaths', paths);
  stub('playlist.Service.GetDefaultPlaylistInfo', { Name: 'Loved' });
  stub('config.Config.GetFavoritesPlaylistID', 3);
  stub('config.Config.GetFavoritesIconStyle', 'star');
  stub('config.Config.GetPinDefaultPlaylist', false);
}

/** Push a config change and let the reloads it triggers settle. */
async function reload(paths: string[] = PATHS): Promise<void> {
  stubReads(paths);
  emit(Events.FavoritesConfigChanged, {
    PlaylistID: 3,
    IconStyle: 'star',
    PinDefault: false,
  });
  await flush();
  resetHarness();
  stubReads(paths);
}

describe('favorites store: cached membership', () => {
  beforeEach(async () => {
    await reload();
  });

  it('reports membership by file path', () => {
    expect([
      favoritesStore.isFavorited('/music/a.mp3'),
      favoritesStore.isFavorited('/music/z.mp3'),
    ]).toEqual([true, false]);
  });

  it('requires every path for a multi-selection to count as favourited', () => {
    expect([
      favoritesStore.allFavorited(PATHS),
      favoritesStore.allFavorited([...PATHS, '/music/z.mp3']),
    ]).toEqual([true, false]);
  });

  it('treats an empty selection as not favourited, so the button is not lit for nothing', () => {
    expect(favoritesStore.allFavorited([])).toBe(false);
  });

  it('adopts the config the backend pushed', () => {
    expect([
      favoritesStore.getPlaylistId(),
      favoritesStore.getIconStyle(),
      favoritesStore.getPinDefault(),
    ]).toEqual([3, 'star', false]);
  });

  it('resolves the playlist name from the backend', () => {
    expect(favoritesStore.getPlaylistName()).toBe('Loved');
  });
});

describe('favorites store: optimistic writes', () => {
  beforeEach(async () => {
    await reload();
  });

  it('fills the heart before the backend answers', () => {
    void favoritesStore.toggleFavorite('/music/z.mp3');

    expect(favoritesStore.isFavorited('/music/z.mp3')).toBe(true);
  });

  it('reverts an add the backend rejected', async () => {
    stubFailure('playlist.Service.ToggleDefaultPlaylistTrack');

    await favoritesStore.toggleFavorite('/music/z.mp3');

    expect(favoritesStore.isFavorited('/music/z.mp3')).toBe(false);
  });

  it('reverts a removal the backend rejected', async () => {
    stubFailure('playlist.Service.ToggleDefaultPlaylistTrack');

    await favoritesStore.toggleFavorite('/music/a.mp3');

    expect(favoritesStore.isFavorited('/music/a.mp3')).toBe(true);
  });

  it('adds a batch optimistically and forwards the whole list', async () => {
    await favoritesStore.addToFavorites(['/music/y.mp3', '/music/z.mp3']);

    expect([
      favoritesStore.allFavorited(['/music/y.mp3', '/music/z.mp3']),
      lastArgs('playlist.Service.AddToDefaultPlaylist'),
    ]).toEqual([true, [['/music/y.mp3', '/music/z.mp3']]]);
  });

  it('removes a batch optimistically', async () => {
    await favoritesStore.removeFromFavorites(['/music/a.mp3']);

    expect(favoritesStore.isFavorited('/music/a.mp3')).toBe(false);
  });

  it('resyncs from the backend when a batch write fails, rather than guessing', async () => {
    stubFailure('playlist.Service.AddToDefaultPlaylist');

    await favoritesStore.addToFavorites(['/music/z.mp3']);
    await flush();

    expect(
      calls('playlist.Service.GetDefaultPlaylistTrackPaths'),
    ).toHaveLength(1);
  });
});

describe('favorites store: reacting to playlist changes', () => {
  beforeEach(async () => {
    await reload();
  });

  it('reloads when the default playlist itself changed', async () => {
    emit(Events.PlaylistTracksChanged, 3);
    await flush();

    expect(
      calls('playlist.Service.GetDefaultPlaylistTrackPaths'),
    ).toHaveLength(1);
  });

  it('ignores changes to some other playlist', async () => {
    emit(Events.PlaylistTracksChanged, 99);
    await flush();

    expect(
      calls('playlist.Service.GetDefaultPlaylistTrackPaths'),
    ).toHaveLength(0);
  });

  it('reloads after a restore, which rewrites every playlist', async () => {
    emit(Events.PlaylistsRestored);
    await flush();

    expect(
      calls('playlist.Service.GetDefaultPlaylistTrackPaths'),
    ).toHaveLength(1);
  });

  it('re-reads the name when a playlist is renamed', async () => {
    stub('playlist.Service.GetDefaultPlaylistInfo', { Name: 'Renamed' });
    emit(Events.PlaylistRenamed, 3);
    await flush();

    expect(favoritesStore.getPlaylistName()).toBe('Renamed');
  });

  it('falls back to "Favorites" when no default playlist is configured', async () => {
    await favoritesStore.setDefaultPlaylist(0);

    expect(favoritesStore.getPlaylistName()).toBe('Favorites');
  });

  it('persists a changed icon style', async () => {
    await favoritesStore.setIconStyle('heart');

    expect([
      favoritesStore.getIconStyle(),
      lastArgs('config.Config.SetFavoritesIconStyle'),
    ]).toEqual(['heart', ['heart']]);
  });

  it('persists the pin setting', async () => {
    await favoritesStore.setPinDefault(true);

    expect(lastArgs('config.Config.SetPinDefaultPlaylist')).toEqual([true]);
  });
});

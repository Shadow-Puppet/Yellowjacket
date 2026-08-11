/**
 * The player store is a pure projection of backend push events. Its
 * whole job is to be a truthful cache, so the tests are about what it
 * derives (`isPlaying` from a state string) and what it refuses to
 * invent (it never predicts the result of an action).
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { playerStore, type TrackInfo } from '@store/player-store';
import { Events } from '../../src/events';
import { emit, calls, lastArgs, flush } from '@test/support/harness';

const TRACK: TrackInfo = {
  fileName: 'one.mp3',
  filePath: '/music/one.mp3',
  trackLength: 180,
  seekPosition: 0,
  state: 'playing',
  title: 'One',
  artist: 'Artist',
  album: 'Album',
  coverArt: '',
  coverArtSmall: '',
  coverArtMedium: '',
  coverArtLarge: '',
  trackChangeId: 1,
  artistMbid: '',
  releaseGroupMbid: '',
  recordingMbid: '',
};

describe('player store: playback state', () => {
  beforeEach(() => {
    emit(Events.PlaybackStateChanged, { state: 'stopped' });
    emit(Events.TrackChanged, null);
  });

  it('is playing only for the literal "playing" state', () => {
    const seen: boolean[] = [];

    for (const state of ['playing', 'paused', 'stopped', 'buffering']) {
      emit(Events.PlaybackStateChanged, { state });
      seen.push(playerStore.getState().isPlaying);
    }

    expect(seen).toEqual([true, false, false, false]);
  });

  it('stops playing when the track finishes', () => {
    emit(Events.PlaybackStateChanged, { state: 'playing' });
    emit(Events.PlaybackFinished);

    expect(playerStore.getState().isPlaying).toBe(false);
  });

  it('caches the current track', () => {
    emit(Events.TrackChanged, TRACK);

    expect(playerStore.getState().currentTrack).toEqual(TRACK);
  });

  it('normalises an absent track to null rather than undefined', () => {
    emit(Events.TrackChanged, TRACK);
    emit(Events.TrackChanged, undefined);

    expect(playerStore.getState().currentTrack).toBeNull();
  });

  it('keeps the cached track across a pause', () => {
    emit(Events.TrackChanged, TRACK);
    emit(Events.PlaybackStateChanged, { state: 'paused' });

    expect(playerStore.getState().currentTrack).toEqual(TRACK);
  });

  it('tracks volume pushed back from Go', () => {
    emit(Events.VolumeChanged, 42);

    expect(playerStore.getState().volume).toBe(42);
  });

  it('tracks mute separately from the volume level', () => {
    // Mute leaves the volume number alone, which is exactly why it
    // needs an event of its own: the indicator had nothing to react to.
    emit(Events.VolumeChanged, 42);
    emit(Events.MuteChanged, true);

    expect(playerStore.getState()).toMatchObject({ volume: 42, muted: true });

    emit(Events.MuteChanged, false);

    expect(playerStore.getState().muted).toBe(false);
  });

  it('replaces state rather than mutating it, so a saved reference is stable', () => {
    emit(Events.VolumeChanged, 10);
    const before = playerStore.getState();

    emit(Events.VolumeChanged, 20);

    expect(before.volume).toBe(10);
  });

  it('coalesces a burst into a single notification', async () => {
    let notifications = 0;
    const off = playerStore.subscribe(() => {
      notifications += 1;
    });

    emit(Events.VolumeChanged, 1);
    emit(Events.VolumeChanged, 2);
    emit(Events.PlaybackStateChanged, { state: 'playing' });
    await flush();
    off();

    expect(notifications).toBe(1);
  });
});

describe('player store: actions', () => {
  it('forwards each action to its bound method', () => {
    playerStore.pause();
    playerStore.loadTrack('/music/one.mp3');
    playerStore.seek(30);
    playerStore.setVolume(60);
    playerStore.toggleMute();

    expect(calls().map((c) => c.path)).toEqual([
      'player.Player.Pause',
      'player.Player.LoadFile',
      'player.Player.Seek',
      'player.Player.SetVolume',
      'player.Player.MuteToggle',
    ]);
  });

  it('sends the volume as an integer percentage, not a fraction', () => {
    // player.UserVolume is an int in Go; a float never settles its
    // callback. See .planning/NOTES.md.
    playerStore.setVolume(42);

    expect(lastArgs('player.Player.SetVolume')).toEqual([42]);
  });

  it('does not optimistically change cached volume', () => {
    emit(Events.VolumeChanged, 50);
    playerStore.setVolume(80);

    expect(playerStore.getState().volume).toBe(50);
  });
});

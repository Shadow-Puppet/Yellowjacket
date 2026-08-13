/**
 * The queue store's delta reducer is the most intricate pure logic in
 * the frontend: four mutation actions arriving as events, applied to a
 * cached array that must stay identical to the Go queue's own. `move` in
 * particular adjusts its insertion index for elements removed before it,
 * and gets that wrong silently.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { queueStore, type QueueTrack } from '@store/queue-store';
import { Events } from '../../src/events';
import { emit, flush, lastArgs, calls } from '@test/support/harness';

function track(n: number): QueueTrack {
  return {
    id: n,
    audioFileId: n,
    filePath: `/music/${n}.mp3`,
    position: n,
    title: `Track ${n}`,
    artist: 'Artist',
    album: 'Album',
    coverArtPath: '',
    artistMbid: '',
    releaseGroupMbid: '',
    recordingMbid: '',
  };
}

/** Titles of the cached queue, the cheapest readable assertion. */
function titles(): string[] {
  return queueStore.getState().tracks.map((t) => t.title);
}

/** Push an authoritative full-state sync, as the backend does on
 *  startup and after SetQueue. */
function sync(tracks: QueueTrack[], currentIndex = 0): void {
  emit(Events.QueueChanged, {
    tracks,
    currentIndex,
    shuffleMode: false,
    repeatMode: 'off',
    source: { type: '', id: 0, label: '' },
  });
}

describe('queue store: full-state sync', () => {
  beforeEach(() => {
    sync([]);
  });

  it('replaces cached state wholesale', () => {
    sync([track(1), track(2)], 1);

    expect(queueStore.getState()).toEqual({
      tracks: [track(1), track(2)],
      currentIndex: 1,
      shuffleMode: false,
      repeatMode: 'off',
      source: { type: '', id: 0, label: '' },
    });
  });

  it('carries a non-empty source through from the backend', () => {
    emit(Events.QueueChanged, {
      tracks: [track(1)],
      currentIndex: 0,
      shuffleMode: false,
      repeatMode: 'off',
      source: { type: 'album', id: 7, label: 'Abbey Road' },
    });

    expect(queueStore.getState().source).toEqual({
      type: 'album',
      id: 7,
      label: 'Abbey Road',
    });
  });

  it('tolerates a null track list, which Go sends for an empty queue', () => {
    emit(Events.QueueChanged, {
      tracks: null,
      currentIndex: -1,
      shuffleMode: false,
      repeatMode: 'off',
      source: { type: '', id: 0, label: '' },
    });

    expect(queueStore.getState().tracks).toEqual([]);
  });
});

describe('queue store: track deltas', () => {
  beforeEach(() => {
    sync([track(1), track(2), track(3)], 0);
  });

  it('appends on add', () => {
    emit(Events.QueueTracksModified, {
      action: 'add',
      tracks: [track(4)],
      index: 0,
      currentIndex: 0,
    });

    expect(titles()).toEqual(['Track 1', 'Track 2', 'Track 3', 'Track 4']);
  });

  it('splices at the index on insert', () => {
    emit(Events.QueueTracksModified, {
      action: 'insert',
      tracks: [track(9)],
      index: 1,
      currentIndex: 0,
    });

    expect(titles()).toEqual(['Track 1', 'Track 9', 'Track 2', 'Track 3']);
  });

  it('removes every listed position at once, not one at a time', () => {
    // Removing 0 then 2 sequentially would take the wrong second track;
    // the reducer must treat the positions as indices into the original.
    emit(Events.QueueTracksModified, {
      action: 'remove',
      positions: [0, 2],
      index: 0,
      currentIndex: 0,
    });

    expect(titles()).toEqual(['Track 2']);
  });

  it('adopts the backend current index from every delta', () => {
    emit(Events.QueueTracksModified, {
      action: 'remove',
      positions: [0],
      index: 0,
      currentIndex: 1,
    });

    expect(queueStore.getState().currentIndex).toBe(1);
  });
});

describe('queue store: move', () => {
  beforeEach(() => {
    sync([track(1), track(2), track(3), track(4)], 0);
  });

  it('moves a track forward, compensating for its own removal', () => {
    // Move index 0 to index 2. After removing it, the target shifts
    // down by one, so track 1 lands between 2 and 3 — not after 3.
    emit(Events.QueueTracksModified, {
      action: 'move',
      tracks: [track(1)],
      positions: [0],
      index: 2,
      currentIndex: 1,
    });

    expect(titles()).toEqual(['Track 2', 'Track 1', 'Track 3', 'Track 4']);
  });

  it('moves a track backward without compensating', () => {
    emit(Events.QueueTracksModified, {
      action: 'move',
      tracks: [track(4)],
      positions: [3],
      index: 1,
      currentIndex: 0,
    });

    expect(titles()).toEqual(['Track 1', 'Track 4', 'Track 2', 'Track 3']);
  });

  it('moves a multi-selection, compensating once per element before the target', () => {
    emit(Events.QueueTracksModified, {
      action: 'move',
      tracks: [track(1), track(2)],
      positions: [0, 1],
      index: 3,
      currentIndex: 0,
    });

    expect(titles()).toEqual(['Track 3', 'Track 1', 'Track 2', 'Track 4']);
  });

  it('clamps a target past the end of the shortened list', () => {
    emit(Events.QueueTracksModified, {
      action: 'move',
      tracks: [track(1)],
      positions: [0],
      index: 99,
      currentIndex: 3,
    });

    expect(titles()).toEqual(['Track 2', 'Track 3', 'Track 4', 'Track 1']);
  });
});

describe('queue store: mode deltas', () => {
  beforeEach(() => {
    sync([track(1)], 0);
  });

  it('applies shuffle and repeat together', () => {
    emit(Events.QueueModeChanged, { shuffleMode: true, repeatMode: 'one' });

    const state = queueStore.getState();

    expect([state.shuffleMode, state.repeatMode]).toEqual([true, 'one']);
  });

  it('leaves the track list untouched', () => {
    emit(Events.QueueModeChanged, { shuffleMode: true, repeatMode: 'all' });

    expect(titles()).toEqual(['Track 1']);
  });

  it('applies an index-only delta', () => {
    emit(Events.QueueIndexChanged, { currentIndex: 7 });

    expect(queueStore.getState().currentIndex).toBe(7);
  });
});

describe('queue store: subscriber notification', () => {
  beforeEach(() => {
    sync([]);
  });

  it('coalesces a burst of events into one notification', async () => {
    let notifications = 0;
    const unsubscribe = queueStore.subscribe(() => {
      notifications += 1;
    });

    emit(Events.QueueIndexChanged, { currentIndex: 1 });
    emit(Events.QueueIndexChanged, { currentIndex: 2 });
    emit(Events.QueueIndexChanged, { currentIndex: 3 });
    await flush();

    unsubscribe();

    expect(notifications).toBe(1);
  });

  it('stops notifying after unsubscribe', async () => {
    let notifications = 0;
    const unsubscribe = queueStore.subscribe(() => {
      notifications += 1;
    });

    unsubscribe();
    emit(Events.QueueIndexChanged, { currentIndex: 1 });
    await flush();

    expect(notifications).toBe(0);
  });
});

describe('queue store: actions reach the backend', () => {
  it('forwards setQueue with its default shuffleStart and no source', () => {
    queueStore.setQueue(['/a.mp3', '/b.mp3'], 1);

    expect(lastArgs('queue.Queue.SetQueue')).toEqual([
      ['/a.mp3', '/b.mp3'],
      1,
      false,
      { type: '', id: 0, label: '' },
    ]);
  });

  it('forwards setQueue with the given source', () => {
    queueStore.setQueue(['/a.mp3'], 0, true, {
      type: 'playlist',
      id: 7,
      label: 'Road Trip',
    });

    expect(lastArgs('queue.Queue.SetQueue')).toEqual([
      ['/a.mp3'],
      0,
      true,
      { type: 'playlist', id: 7, label: 'Road Trip' },
    ]);
  });

  it('maps each mutation onto its own bound method', () => {
    queueStore.addToQueue('/a.mp3');
    queueStore.playNext('/b.mp3');
    queueStore.removeFromQueue(2);
    queueStore.moveTracksInQueue([0, 1], 3);
    queueStore.toggleShuffle();
    queueStore.cycleRepeat();
    queueStore.clearQueue();

    expect(calls().map((c) => c.path)).toEqual([
      'queue.Queue.AddTrack',
      'queue.Queue.InsertNext',
      'queue.Queue.RemoveTrack',
      'queue.Queue.MoveQueueTracks',
      'queue.Queue.ToggleShuffle',
      'queue.Queue.CycleRepeat',
      'queue.Queue.Clear',
    ]);
  });

  it('does not optimistically mutate cached state', () => {
    sync([track(1)], 0);
    queueStore.clearQueue();

    // The backend is the only writer; the cache waits for the event.
    expect(titles()).toEqual(['Track 1']);
  });
});

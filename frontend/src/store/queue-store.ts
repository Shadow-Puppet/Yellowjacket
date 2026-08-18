import { EventsOn } from '@runtime/runtime';
import { Events } from '../events';
import * as Queue from '@go/queue/queue.js';

// Types
export interface QueueTrack {
  id: number;
  audioFileId: number;
  filePath: string;
  position: number;
  title: string;
  artist: string;
  album: string;
  coverArtPath: string;
  artistMbid: string;
  releaseGroupMbid: string;
  recordingMbid: string;
}

export type RepeatMode = 'off' | 'all' | 'one';

/**
 * Describes the collection a queue was built from — an album, a
 * playlist, a genre, an artist — so the UI can offer to navigate back
 * to it.  An empty `type` means the queue has no single source (the
 * whole library, or one ad-hoc track).
 */
export interface QueueSource {
  type: string;
  id: number;
  label: string;
}

export const EMPTY_QUEUE_SOURCE: QueueSource = { type: '', id: 0, label: '' };

export interface QueueState {
  tracks: QueueTrack[];
  currentIndex: number;
  shuffleMode: boolean;
  repeatMode: RepeatMode;
  source: QueueSource;
}

// Delta event payloads (mirror Go structs in backend/queue/queue.go).
interface IndexChanged {
  currentIndex: number;
}

interface ModeChanged {
  shuffleMode: boolean;
  repeatMode: RepeatMode;
}

interface TracksModified {
  action: string;
  tracks?: QueueTrack[];
  index: number;
  positions?: number[];
  currentIndex: number;
  /** The queue's source *after* the mutation. An append clears it
   *  backend-side — a queue built from one album is not that album
   *  once a track from elsewhere joins it — and this delta is the
   *  only event those paths emit, so the label would otherwise keep
   *  pointing at a collection the queue no longer holds. */
  source?: QueueSource;
}

type Subscriber = () => void;

/**
 * Queue bindings are fire-and-forget by design: what happened is
 * reported by events (QueueChanged, PlaybackFailed), not by a return
 * value.  A rejected bridge call still needs somewhere to land other
 * than an unhandled rejection (errors.m1).
 */
function reportBindingFailure(name: string): (err: unknown) => void {
  return (err: unknown) => console.error(`${name} failed`, err);
}

class QueueStore {
  private state: QueueState = {
    tracks: [],
    currentIndex: -1,
    shuffleMode: false,
    repeatMode: 'off',
    source: EMPTY_QUEUE_SOURCE,
  };

  private subscribers = new Set<Subscriber>();
  private notifyScheduled = false;

  constructor() {
    this.initializeEventListeners();
  }

  // ===================================================================
  // WAILS EVENT BRIDGE
  // Subscribe to backend events and update cached state
  // ===================================================================

  private initializeEventListeners(): void {
    // Full-state sync (startup, SetQueue).
    EventsOn(Events.QueueChanged, (queueState: QueueState) => {
      this.state = {
        tracks: queueState.tracks ?? [],
        currentIndex: queueState.currentIndex,
        shuffleMode: queueState.shuffleMode,
        repeatMode: queueState.repeatMode,
        source: queueState.source ?? EMPTY_QUEUE_SOURCE,
      };
      this.notify();
    });

    // Delta: index-only change (Next, Previous, PlayIndex, etc.).
    EventsOn(
      Events.QueueIndexChanged,
      (payload: IndexChanged) => {
        this.state.currentIndex = payload.currentIndex;
        this.notify();
      },
    );

    // Delta: mode-only change (ToggleShuffle, CycleRepeat).
    EventsOn(
      Events.QueueModeChanged,
      (payload: ModeChanged) => {
        this.state.shuffleMode = payload.shuffleMode;
        this.state.repeatMode = payload.repeatMode;
        this.notify();
      },
    );

    // Delta: track list mutation (Add, Insert, Remove).
    EventsOn(
      Events.QueueTracksModified,
      (payload: TracksModified) => {
        this.applyTracksDelta(payload);
        this.notify();
      },
    );
  }

  private applyTracksDelta(delta: TracksModified): void {
    const tracks = this.state.tracks;

    switch (delta.action) {
      case 'add':
        if (delta.tracks) {
          this.state.tracks = [...tracks, ...delta.tracks];
        }

        break;

      case 'insert':
        if (delta.tracks) {
          const before = tracks.slice(0, delta.index);
          const after = tracks.slice(delta.index);
          this.state.tracks = [...before, ...delta.tracks, ...after];
        }

        break;

      case 'remove':
        if (delta.positions) {
          const removeSet = new Set(delta.positions);
          this.state.tracks = tracks.filter(
            (_, i) => !removeSet.has(i),
          );
        }

        break;

      case 'move':
        if (delta.positions && delta.tracks) {
          const removeSet = new Set(delta.positions);
          const remaining = tracks.filter(
            (_, i) => !removeSet.has(i),
          );

          // Adjust insertion index for removed elements.
          let adjustedIdx = delta.index;

          for (const pos of delta.positions) {
            if (pos < delta.index) {
              adjustedIdx--;
            }
          }

          adjustedIdx = Math.max(
            0,
            Math.min(adjustedIdx, remaining.length),
          );

          const before = remaining.slice(0, adjustedIdx);
          const after = remaining.slice(adjustedIdx);
          this.state.tracks = [
            ...before,
            ...delta.tracks,
            ...after,
          ];
        }

        break;
    }

    this.state.currentIndex = delta.currentIndex;
    this.state.source = delta.source ?? EMPTY_QUEUE_SOURCE;
  }

  // ===================================================================
  // STATE ACCESS
  // ===================================================================

  getState(): Readonly<QueueState> {
    return this.state;
  }

  // ===================================================================
  // ACTIONS
  // These delegate to the backend via Wails bindings
  // ===================================================================

  play(): void {
    void Queue.Play().catch(
      reportBindingFailure('Queue.Play'),
    );
  }

  next(): void {
    void Queue.Next().catch(
      reportBindingFailure('Queue.Next'),
    );
  }

  previous(): void {
    void Queue.Previous().catch(
      reportBindingFailure('Queue.Previous'),
    );
  }

  setQueue(
    filePaths: string[],
    startIndex: number,
    shuffleStart = false,
    source: QueueSource = EMPTY_QUEUE_SOURCE,
  ): void {
    void Queue.SetQueue(filePaths, startIndex, shuffleStart, source).catch(
      reportBindingFailure('Queue.SetQueue'),
    );
  }

  addToQueue(filePath: string): void {
    void Queue.AddTrack(filePath).catch(
      reportBindingFailure('Queue.AddTrack'),
    );
  }

  playNext(filePath: string): void {
    void Queue.InsertNext(filePath).catch(
      reportBindingFailure('Queue.InsertNext'),
    );
  }

  removeFromQueue(position: number): void {
    void Queue.RemoveTrack(position).catch(
      reportBindingFailure('Queue.RemoveTrack'),
    );
  }

  removeTracksFromQueue(positions: number[]): void {
    void Queue.RemoveTracks(positions).catch(
      reportBindingFailure('Queue.RemoveTracks'),
    );
  }

  addTracksToQueue(filePaths: string[]): void {
    void Queue.AddTracks(filePaths).catch(
      reportBindingFailure('Queue.AddTracks'),
    );
  }

  playTracksNext(filePaths: string[]): void {
    void Queue.InsertNextTracks(filePaths).catch(
      reportBindingFailure('Queue.InsertNextTracks'),
    );
  }

  toggleShuffle(): void {
    void Queue.ToggleShuffle().catch(
      reportBindingFailure('Queue.ToggleShuffle'),
    );
  }

  cycleRepeat(): void {
    void Queue.CycleRepeat().catch(
      reportBindingFailure('Queue.CycleRepeat'),
    );
  }

  playAtIndex(index: number): void {
    void Queue.PlayIndex(index).catch(
      reportBindingFailure('Queue.PlayIndex'),
    );
  }

  insertTracksAtIndex(
    filePaths: string[],
    index: number,
  ): void {
    void Queue.InsertTracksAt(filePaths, index).catch(
      reportBindingFailure('Queue.InsertTracksAt'),
    );
  }

  moveTracksInQueue(
    fromIndices: number[],
    toIndex: number,
  ): void {
    void Queue.MoveQueueTracks(fromIndices, toIndex).catch(
      reportBindingFailure('Queue.MoveQueueTracks'),
    );
  }

  clearQueue(): void {
    void Queue.Clear().catch(
      reportBindingFailure('Queue.Clear'),
    );
  }

  // ===================================================================
  // SUBSCRIPTION SYSTEM
  // ===================================================================

  subscribe(callback: Subscriber): () => void {
    this.subscribers.add(callback);

    return () => this.subscribers.delete(callback);
  }

  private notify(): void {
    if (this.notifyScheduled) return;
    this.notifyScheduled = true;
    queueMicrotask(() => {
      this.notifyScheduled = false;
      for (const sub of this.subscribers) {
        sub();
      }
    });
  }
}

// Singleton instance
export const queueStore = new QueueStore();

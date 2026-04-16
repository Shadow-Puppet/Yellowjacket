import { EventsOn } from '@runtime/runtime';
import { Events } from '../events';
import * as Queue from '@go/queue/Queue';

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

export interface QueueState {
  tracks: QueueTrack[];
  currentIndex: number;
  shuffleMode: boolean;
  repeatMode: RepeatMode;
  sourcePlaylistId: number;
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
}

type Subscriber = () => void;

class QueueStore {
  private state: QueueState = {
    tracks: [],
    currentIndex: -1,
    shuffleMode: false,
    repeatMode: 'off',
    sourcePlaylistId: 0,
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
        sourcePlaylistId: queueState.sourcePlaylistId,
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
    Queue.Play();
  }

  next(): void {
    Queue.Next();
  }

  previous(): void {
    Queue.Previous();
  }

  setQueue(
    filePaths: string[],
    startIndex: number,
    shuffleStart = false,
  ): void {
    Queue.SetQueue(filePaths, startIndex, shuffleStart);
  }

  addToQueue(filePath: string): void {
    Queue.AddTrack(filePath);
  }

  playNext(filePath: string): void {
    Queue.InsertNext(filePath);
  }

  removeFromQueue(position: number): void {
    Queue.RemoveTrack(position);
  }

  removeTracksFromQueue(positions: number[]): void {
    Queue.RemoveTracks(positions);
  }

  addTracksToQueue(filePaths: string[]): void {
    Queue.AddTracks(filePaths);
  }

  playTracksNext(filePaths: string[]): void {
    Queue.InsertNextTracks(filePaths);
  }

  toggleShuffle(): void {
    Queue.ToggleShuffle();
  }

  cycleRepeat(): void {
    Queue.CycleRepeat();
  }

  playAtIndex(index: number): void {
    Queue.PlayIndex(index);
  }

  insertTracksAtIndex(
    filePaths: string[],
    index: number,
  ): void {
    Queue.InsertTracksAt(filePaths, index);
  }

  moveTracksInQueue(
    fromIndices: number[],
    toIndex: number,
  ): void {
    Queue.MoveQueueTracks(fromIndices, toIndex);
  }

  clearQueue(): void {
    Queue.Clear();
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

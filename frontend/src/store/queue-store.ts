import { EventsOn, EventsEmit } from '@runtime/runtime';
import { Events } from '../events';

// Types
export interface QueueTrack {
  id: number;
  audioFileId: number;
  filePath: string;
  position: number;
  title: string;
  artist: string;
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
  // These delegate to the backend via Wails events
  // ===================================================================

  next(): void {
    EventsEmit(Events.RequestNext);
  }

  previous(): void {
    EventsEmit(Events.RequestPrevious);
  }

  setQueue(filePaths: string[], startIndex: number): void {
    EventsEmit(Events.RequestSetQueue, filePaths, startIndex);
  }

  addToQueue(filePath: string): void {
    EventsEmit(Events.RequestAddToQueue, filePath);
  }

  playNext(filePath: string): void {
    EventsEmit(Events.RequestPlayNext, filePath);
  }

  removeFromQueue(position: number): void {
    EventsEmit(Events.RequestRemoveFromQueue, position);
  }

  removeTracksFromQueue(positions: number[]): void {
    EventsEmit(Events.RequestRemoveTracksFromQueue, positions);
  }

  addTracksToQueue(filePaths: string[]): void {
    EventsEmit(Events.RequestAddTracksToQueue, filePaths);
  }

  playTracksNext(filePaths: string[]): void {
    EventsEmit(Events.RequestPlayTracksNext, filePaths);
  }

  toggleShuffle(): void {
    EventsEmit(Events.RequestToggleShuffle);
  }

  cycleRepeat(): void {
    EventsEmit(Events.RequestCycleRepeat);
  }

  playAtIndex(index: number): void {
    EventsEmit(Events.RequestPlayQueueIndex, index);
  }

  insertTracksAtIndex(filePaths: string[], index: number): void {
    EventsEmit(
      Events.RequestInsertTracksAtIndex,
      filePaths,
      index,
    );
  }

  moveTracksInQueue(
    fromIndices: number[],
    toIndex: number,
  ): void {
    EventsEmit(
      Events.RequestMoveQueueTracks,
      fromIndices,
      toIndex,
    );
  }

  clearQueue(): void {
    EventsEmit(Events.RequestClearQueue);
  }

  // ===================================================================
  // SUBSCRIPTION SYSTEM
  // ===================================================================

  subscribe(callback: Subscriber): () => void {
    this.subscribers.add(callback);

    return () => this.subscribers.delete(callback);
  }

  private notify(): void {
    this.subscribers.forEach((callback) => callback());
  }
}

// Singleton instance
export const queueStore = new QueueStore();

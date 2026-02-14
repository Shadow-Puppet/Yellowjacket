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

type Subscriber = () => void;

class QueueStore {
  private state: QueueState = {
    tracks: [],
    currentIndex: 0,
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

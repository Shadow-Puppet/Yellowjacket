import { EventsOn, EventsEmit } from '@runtime/runtime';
import { Events } from '../events';

// Types
export interface TrackInfo {
  fileName: string;
  filePath: string;
  trackLength: number; // in seconds
  seekPosition: number; // current playback position in seconds
  state: string; // playback state from backend
  title: string; // track title (falls back to fileName)
  artist: string; // artist name
  album: string; // album name
  coverArt: string; // URL path to cover art (e.g., "/covers/abc.jpg") or empty string
}

export interface PlayerState {
  // Cached from backend
  isPlaying: boolean;
  currentTrack: TrackInfo | null;
  volume: number; // 0-100

  // Frontend-only state (for future use)
  // selectedTrackIds: Set<number>;
  // isQueuePanelOpen: boolean;
}

type Subscriber = () => void;

class PlayerStore {
  private state: PlayerState = {
    isPlaying: false,
    currentTrack: null,
    volume: 50,
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
    EventsOn(Events.PlaybackStateChanged, (data: { state: string }) => {
      this.update({ isPlaying: data.state === 'playing' });
    });

    EventsOn(Events.TrackChanged, (trackInfo: TrackInfo) => {
      this.update({ currentTrack: trackInfo });
    });

    EventsOn(Events.PlaybackFinished, () => {
      this.update({ isPlaying: false });
      // Queue auto-advance is handled by the backend queue package.
    });

    EventsOn(Events.VolumeChanged, (volume: number) => {
      this.update({ volume });
    });
  }

  // ===================================================================
  // STATE ACCESS
  // ===================================================================

  getState(): Readonly<PlayerState> {
    return this.state;
  }

  // ===================================================================
  // ACTIONS
  // These delegate to the backend via Wails events
  // ===================================================================

  play(): void {
    EventsEmit(Events.RequestPlay);
  }

  pause(): void {
    EventsEmit(Events.RequestPause);
  }

  loadTrack(filePath: string): void {
    EventsEmit(Events.RequestLoadFile, filePath);
  }

  seek(seconds: number): void {
    EventsEmit(Events.Seek, seconds);
  }

  setVolume(level: number): void {
    EventsEmit(Events.RequestSetVolume, level);
  }

  // ===================================================================
  // SUBSCRIPTION SYSTEM
  // ===================================================================

  subscribe(callback: Subscriber): () => void {
    this.subscribers.add(callback);

    return () => this.subscribers.delete(callback);
  }

  private update(partial: Partial<PlayerState>): void {
    this.state = { ...this.state, ...partial };
    this.notify();
  }

  private notify(): void {
    this.subscribers.forEach((callback) => callback());
  }
}

// Singleton instance
export const playerStore = new PlayerStore();

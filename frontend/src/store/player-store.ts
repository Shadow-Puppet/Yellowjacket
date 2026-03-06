import { EventsOn } from '@runtime/runtime';
import { Events } from '../events';
import * as Player from '@go/player/Player';

// TrackInfo mirrors the player.TrackInfo struct in the Go backend.
// Fields are serialized as camelCase JSON via struct tags.
export interface TrackInfo {
  fileName: string;
  filePath: string;
  trackLength: number; // in seconds
  seekPosition: number; // current playback position in seconds
  state: string; // playback state from backend
  title: string; // track title (falls back to fileName)
  artist: string; // artist name
  album: string; // album name
  coverArt: string; // URL path to full-size cover art or empty string
  coverArtSmall: string; // URL path to small variant (100px max) or empty string
  coverArtMedium: string; // URL path to medium variant (200px max) or empty string
  coverArtLarge: string; // URL path to large variant (400px max) or empty string
  trackChangeId: number; // monotonic counter to detect track changes even when the same file plays consecutively
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

    EventsOn(Events.TrackChanged, (trackInfo: TrackInfo | null) => {
      this.update({ currentTrack: trackInfo ?? null });
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
  // These delegate to the backend via Wails bindings
  // ===================================================================

  pause(): void {
    Player.Pause();
  }

  loadTrack(filePath: string): void {
    Player.LoadFile(filePath);
  }

  seek(seconds: number): void {
    Player.Seek(seconds);
  }

  setVolume(level: number): void {
    Player.SetVolume(level);
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

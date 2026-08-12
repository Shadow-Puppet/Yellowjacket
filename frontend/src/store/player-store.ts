import { EventsOn } from '@runtime/runtime';
import { Events } from '../events';
import * as Player from '@go/player/Player';
import { notificationStore } from './notification-store';

/** The region `<inline-notice>` renders these in: the player bar. */
export const PlayerRegion = 'player';

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
  artistMbid: string; // MusicBrainz artist ID or empty string
  releaseGroupMbid: string; // MusicBrainz release group ID or empty string
  recordingMbid: string; // MusicBrainz recording ID or empty string
}

// PositionInfo mirrors player.PositionInfo in the Go backend: the
// player's own answer to "where are we", pushed once a second while
// playing and immediately after any seek, pause, resume or track
// change.
export interface PositionInfo {
  positionSeconds: number;
  trackLength: number;
  trackChangeId: number;
  seq: number; // increments per report, so an unchanged second is still a fresh reading
  playing: boolean;
}

// PlaybackFailure mirrors queue.PlaybackFailure.
export interface PlaybackFailure {
  filePath: string;
  title: string;
  artist: string;
  reason: string;
}

export interface PlayerState {
  // Cached from backend
  isPlaying: boolean;
  currentTrack: TrackInfo | null;
  volume: number; // 0-100
  muted: boolean; // silenced independently of the volume level

  // The backend's position report.  The seek bar renders this and
  // interpolates only between reports; it does not count.
  position: PositionInfo | null;
}

type Subscriber = () => void;

/**
 * Bindings are fire-and-forget by design — the backend reports what
 * happened through events, not return values — but a rejected bridge
 * call still has to land somewhere other than an unhandled rejection
 * (errors.m1).
 */
function reportBindingFailure(name: string): (err: unknown) => void {
  return (err: unknown) => console.error(`${name} failed`, err);
}

class PlayerStore {
  private state: PlayerState = {
    isPlaying: false,
    currentTrack: null,
    volume: 50,
    muted: false,
    position: null,
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
    EventsOn(Events.PlaybackStateChanged, (data: { state: string }) => {
      this.update({ isPlaying: data.state === 'playing' });
    });

    EventsOn(Events.TrackChanged, (trackInfo: TrackInfo | null) => {
      this.update({ currentTrack: trackInfo ?? null });
    });

    EventsOn(Events.PlaybackPositionChanged, (position: PositionInfo) => {
      this.update({ position });
    });

    EventsOn(Events.PlaybackFailed, (failure: PlaybackFailure) => {
      // The raw Go reason is a debugging tool, not a sentence; it stays
      // in the console and rides along as `detail`.
      console.error(`playback failed: ${failure.filePath}: ${failure.reason}`);

      const name = failure.title || failure.filePath;

      // Inline by the plan's own rule: the useful response to a track
      // that will not play is to keep playing, which the backend is
      // already doing by skipping it.
      notificationStore.inline(PlayerRegion, {
        key: 'playback-failed',
        tone: 'warning',
        text: `Could not play “${name}” — the file may have moved.`,
        coalescedText: (count) =>
          `Skipped ${count} tracks that could not be played.`,
        detail: failure.reason,
      });
    });

    EventsOn(Events.SeekFailed, () => {
      // The backend re-reports its real position alongside this, so the
      // optimistic move the seek bar made is already being taken back;
      // all that is missing is saying why.
      notificationStore.inline(PlayerRegion, {
        key: 'seek-failed',
        tone: 'warning',
        text: 'Could not seek in this track.',
      });
    });

    EventsOn(Events.PlaybackFinished, () => {
      this.update({ isPlaying: false });
      // Queue auto-advance is handled by the backend queue package.
    });

    EventsOn(Events.VolumeChanged, (volume: number) => {
      this.update({ volume });
    });

    EventsOn(Events.MuteChanged, (muted: boolean) => {
      this.update({ muted });
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
    void Player.Pause().catch(reportBindingFailure('Player.Pause'));
  }

  loadTrack(filePath: string): void {
    void Player.LoadFile(filePath).catch(
      reportBindingFailure('Player.LoadFile'),
    );
  }

  seek(seconds: number): void {
    void Player.Seek(seconds).catch(reportBindingFailure('Player.Seek'));
  }

  setVolume(level: number): void {
    void Player.SetVolume(level).catch(
      reportBindingFailure('Player.SetVolume'),
    );
  }

  toggleMute(): void {
    void Player.MuteToggle().catch(reportBindingFailure('Player.MuteToggle'));
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
export const playerStore = new PlayerStore();

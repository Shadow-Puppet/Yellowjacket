import type { ReactiveController, ReactiveControllerHost } from 'lit';
import type {
  PlayerState,
  PositionInfo,
  TrackInfo,
} from '../player-store';
import { playerStore } from '../player-store';

/**
 * PlayerController connects a Lit component to the PlayerStore.
 *
 * Usage in a component:
 *
 *   private player = new PlayerController(this);
 *
 *   render() {
 *     return html`
 *       <span>${this.player.currentTrack?.fileName}</span>
 *       <button @click=${() => this.player.pause()}>Pause</button>
 *     `;
 *   }
 */
export class PlayerController implements ReactiveController {
  private host: ReactiveControllerHost;
  private unsubscribe?: () => void;

  constructor(host: ReactiveControllerHost) {
    this.host = host;
    host.addController(this);
  }

  // ===================================================================
  // LIFECYCLE HOOKS
  // ===================================================================

  hostConnected(): void {
    // Subscribe to store when component mounts
    this.unsubscribe = playerStore.subscribe(() => {
      this.host.requestUpdate();
    });
  }

  hostDisconnected(): void {
    // Unsubscribe when component unmounts (prevents memory leaks)
    this.unsubscribe?.();
  }

  // ===================================================================
  // STATE ACCESSORS
  // Convenience getters so components don't need to call getState()
  // ===================================================================

  get state(): Readonly<PlayerState> {
    return playerStore.getState();
  }

  get isPlaying(): boolean {
    return this.state.isPlaying;
  }

  get currentTrack(): TrackInfo | null {
    return this.state.currentTrack;
  }

  get volume(): number {
    return this.state.volume;
  }

  get muted(): boolean {
    return this.state.muted;
  }

  /** The backend's last position report, or null before the first. */
  get position(): PositionInfo | null {
    return this.state.position;
  }

  // ===================================================================
  // ACTIONS
  // Delegate to store (which delegates to backend)
  // ===================================================================

  pause(): void {
    playerStore.pause();
  }

  loadTrack(filePath: string): void {
    playerStore.loadTrack(filePath);
  }

  seek(seconds: number): void {
    playerStore.seek(seconds);
  }

  setVolume(level: number): void {
    playerStore.setVolume(level);
  }

  toggleMute(): void {
    playerStore.toggleMute();
  }
}

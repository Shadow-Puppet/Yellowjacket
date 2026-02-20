import type { ReactiveController, ReactiveControllerHost } from 'lit';
import type { QueueState, QueueTrack, RepeatMode } from '../queue-store';
import { queueStore } from '../queue-store';

/**
 * QueueController connects a Lit component to the QueueStore.
 *
 * Usage in a component:
 *
 *   private queue = new QueueController(this);
 *
 *   render() {
 *     return html`
 *       <span>Tracks: ${this.queue.tracks.length}</span>
 *       <button @click=${() => this.queue.next()}>Next</button>
 *     `;
 *   }
 */
export class QueueController implements ReactiveController {
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
    this.unsubscribe = queueStore.subscribe(() => {
      this.host.requestUpdate();
    });
  }

  hostDisconnected(): void {
    this.unsubscribe?.();
  }

  // ===================================================================
  // STATE ACCESSORS
  // ===================================================================

  get state(): Readonly<QueueState> {
    return queueStore.getState();
  }

  get tracks(): QueueTrack[] {
    return this.state.tracks;
  }

  get currentIndex(): number {
    return this.state.currentIndex;
  }

  get currentTrack(): QueueTrack | undefined {
    return this.state.tracks[this.state.currentIndex];
  }

  get shuffleMode(): boolean {
    return this.state.shuffleMode;
  }

  get repeatMode(): RepeatMode {
    return this.state.repeatMode;
  }

  // ===================================================================
  // ACTIONS
  // ===================================================================

  next(): void {
    queueStore.next();
  }

  previous(): void {
    queueStore.previous();
  }

  setQueue(filePaths: string[], startIndex: number): void {
    queueStore.setQueue(filePaths, startIndex);
  }

  addToQueue(filePath: string): void {
    queueStore.addToQueue(filePath);
  }

  playNext(filePath: string): void {
    queueStore.playNext(filePath);
  }

  removeFromQueue(position: number): void {
    queueStore.removeFromQueue(position);
  }

  removeTracksFromQueue(positions: number[]): void {
    queueStore.removeTracksFromQueue(positions);
  }

  addTracksToQueue(filePaths: string[]): void {
    queueStore.addTracksToQueue(filePaths);
  }

  playTracksNext(filePaths: string[]): void {
    queueStore.playTracksNext(filePaths);
  }

  toggleShuffle(): void {
    queueStore.toggleShuffle();
  }

  cycleRepeat(): void {
    queueStore.cycleRepeat();
  }

  playAtIndex(index: number): void {
    queueStore.playAtIndex(index);
  }

  insertTracksAtIndex(
    filePaths: string[],
    index: number,
  ): void {
    queueStore.insertTracksAtIndex(filePaths, index);
  }

  moveTracksInQueue(
    fromIndices: number[],
    toIndex: number,
  ): void {
    queueStore.moveTracksInQueue(fromIndices, toIndex);
  }

  clearQueue(): void {
    queueStore.clearQueue();
  }
}

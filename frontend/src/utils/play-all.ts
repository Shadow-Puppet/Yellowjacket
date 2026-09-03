import { queueStore } from '@store/queue-store';
import type { QueueSource } from '@store/queue-store';

/**
 * Queue a list and start it, optionally shuffled.
 *
 * This is the one place that owns what "shuffle this collection" means.
 * `SetQueue`'s `shuffleStart` only picks a random first track when
 * shuffle mode is *already* on — it does not turn it on — so the mode
 * has to be set before the queue, not after.  The album page used to
 * carry that rule privately; the play-all/shuffle-all pair on every
 * track list now shares it.
 */
export function playAll(
    paths: string[],
    source: QueueSource | undefined,
    shuffle: boolean,
): void {
    if (paths.length === 0) return;

    if (shuffle && !queueStore.getState().shuffleMode) {
        queueStore.toggleShuffle();
    }

    queueStore.setQueue(paths, 0, shuffle, source);
}

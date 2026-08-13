/**
 * `a11y.11` — the queue's order can be changed without a mouse.
 *
 * Reproduced in the running app first: with a queue row focused, five
 * plausible combinations (Alt/Ctrl/Shift/Meta + arrows) all left the
 * order untouched, because reordering existed only as a drag whose drop
 * index is computed from the cursor's Y position.
 *
 * The arithmetic is what these pin. `MoveQueueTracks`'s `toIndex` is an
 * index into the array *before* the move, so up-by-one and down-by-one
 * are not symmetric: up asks for `i - 1` and down has to ask for
 * `i + 2`, because `i + 1` is where the row already is once its own
 * removal is accounted for — and the backend's contiguous-block guard
 * correctly treats that as a no-op. A fix written to look symmetric
 * silently does nothing in one direction.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/queue-panel/queue-panel';
import type { QueuePanel } from '@components/queue-panel/queue-panel';
import { Events } from '../../src/events';
import { emit, calls, flush, lastArgs, resetHarness } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';
import type { QueueTrack } from '@store/queue-store';

function queueTrack(n: number, title: string): QueueTrack {
  return {
    id: n,
    audioFileId: n,
    filePath: `/music/${n}.mp3`,
    position: n,
    title,
    artist: 'Artist',
    album: 'Album',
    coverArtPath: '',
    artistMbid: '',
    releaseGroupMbid: '',
    recordingMbid: '',
  };
}

const TRACKS = ['First', 'Second', 'Third', 'Fourth'].map((t, i) =>
  queueTrack(i + 1, t),
);

type Panel = QueuePanel;

async function panelWithQueue(): Promise<Panel> {
  const el = await fixture<Panel>('queue-panel', { open: true });

  emit(Events.QueueChanged, {
    tracks: TRACKS,
    currentIndex: 0,
    shuffleMode: false,
    repeatMode: 'off',
    sourcePlaylistId: 0,
  });
  await flush();
  await el.updateComplete;
  await new Promise((r) => {
    requestAnimationFrame(() => r(null));
  });

  return el;
}

/**
 * Press a key *from a row*, the way the delegated handler receives it.
 *
 * The index comes off the event's own row rather than from the
 * component's `focusedIndex`, which is the fix for a pre-existing bug:
 * only the arrow keys used to move that field, so a row reached by a
 * click or by Tab left it saying 0 and every key acted on the wrong row.
 */
function pressFrom(el: Panel, index: number, key: string, alt: boolean) {
  const row = shadowAll(el, `.track-item[data-index="${index}"]`)[0];

  row?.dispatchEvent(
    new KeyboardEvent('keydown', { key, altKey: alt, bubbles: true }),
  );
}

const live = (el: Panel) =>
  shadow(el, '[role="status"]')?.textContent?.trim() ?? '';

describe('<queue-panel> keyboard reorder', () => {
  beforeEach(() => {
    emit(Events.QueueChanged, {
      tracks: [],
      currentIndex: -1,
      shuffleMode: false,
      repeatMode: 'off',
      sourcePlaylistId: 0,
    });
  });

  it('moves a row up by one', async () => {
    const el = await panelWithQueue();

    pressFrom(el, 2, 'ArrowUp', true);
    await el.updateComplete;

    expect(lastArgs('queue.Queue.MoveQueueTracks')).toEqual([[2], 1]);
  });

  // The asymmetry, pinned. `[[1], 2]` would be the symmetric-looking
  // version and is precisely the no-op the backend guards against.
  it('moves a row down by one, past its own removal', async () => {
    const el = await panelWithQueue();

    pressFrom(el, 1, 'ArrowDown', true);
    await el.updateComplete;

    expect(lastArgs('queue.Queue.MoveQueueTracks')).toEqual([[1], 3]);
  });

  it('acts on the row the key came from, not the last one arrowed to', async () => {
    const el = await panelWithQueue();

    pressFrom(el, 3, 'ArrowUp', true);
    await el.updateComplete;

    expect(lastArgs('queue.Queue.MoveQueueTracks')).toEqual([[3], 2]);
  });

  it('says where the row went', async () => {
    const el = await panelWithQueue();

    pressFrom(el, 2, 'ArrowUp', true);
    await el.updateComplete;

    expect(live(el)).toBe('Moved to position 2 of 4');
  });

  it('refuses at the ends, and says so rather than silently doing nothing', async () => {
    const el = await panelWithQueue();

    pressFrom(el, 0, 'ArrowUp', true);
    await el.updateComplete;
    const top = live(el);

    pressFrom(el, 3, 'ArrowDown', true);
    await el.updateComplete;

    expect([top, live(el), calls().some((c) => c.path.includes('Move'))]).toEqual(
      ['Already first in the queue', 'Already last in the queue', false],
    );
  });

  // The live region has to be in the DOM before it has anything to say:
  // most screen readers announce a change to a region they are already
  // watching and ignore one that appears with its content already in it.
  it('has the live region mounted and empty before any move', async () => {
    const el = await panelWithQueue();

    expect([shadow(el, '[role="status"]') !== null, live(el)]).toEqual([
      true,
      '',
    ]);
  });

  // Without the modifier the same keys must still rove, and must not
  // reach the global volume binding.
  it('leaves the plain arrows as a roving move', async () => {
    const el = await panelWithQueue();

    pressFrom(el, 0, 'ArrowDown', false);
    await el.updateComplete;

    expect(calls().some((c) => c.path.includes('Move'))).toBe(false);
  });
});

describe('a queue row says which track its controls act on', () => {
  beforeEach(() => {
    resetHarness();
  });

  it('names each remove button after its own track', async () => {
    const el = await panelWithQueue();

    const labels = shadowAll(el, '.remove-button').map((b) =>
      b.getAttribute('aria-label'),
    );

    // `a11y.32`: `title="Remove from queue"` on every row is a name
    // that never identifies which track — four identical buttons in a
    // list whose whole purpose is the order.
    expect(labels.slice(0, 4)).toEqual([
      'Remove First from queue',
      'Remove Second from queue',
      'Remove Third from queue',
      'Remove Fourth from queue',
    ]);
  });

  it('gives the title and artist a tooltip, since the panel is resizable', async () => {
    const el = await panelWithQueue();

    // `a11y.24` calls this one acute: MIN_WIDTH is narrow enough that
    // both lines clip routinely, and nothing else can show the value.
    expect(shadow(el, '.track-title')?.getAttribute('title')).toBe('First');
    expect(shadow(el, '.track-artist')?.getAttribute('title')).toBe('Artist');
  });
});

/**
 * The other three selecting surfaces (plan 019 phase 3, #63).
 *
 * `track-list` got the gestures in phases 1 and 2; the queue panel and
 * both playlist detail views are the rest, and phase 3 was "mostly
 * wiring" only in the sense that they already share
 * `SelectionController`. Two of them are not symmetric with the track
 * list at all, and those two asymmetries are what this file is for:
 *
 * **A tap on a queue row plays that position in the queue.** Copying
 * `track-list`'s tap — which sets the queue to the list the row is in —
 * would rebuild the queue from the queue, discarding its source, its
 * shuffle order and everything inserted by hand along the way. It is
 * not the no-op it reads as.
 *
 * **The queue panel has no swipe, deliberately.** A right swipe means
 * *add to the queue* everywhere it exists, and a queue row is already
 * in the queue; the only thing it could mean there is *remove*, which
 * is the same gesture with the opposite effect one screen away. So the
 * assertion is that the rows do not opt in — a swipe there must not
 * silently become a second meaning for the app's one horizontal
 * gesture.
 *
 * The playlist views are the symmetric half, and they are here because
 * they bind their gestures **per template** rather than through the
 * `firstUpdated` delegation the two virtualized lists use, so "the
 * handler is attached at all" is a different question in each.
 */
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import '@components/queue-panel/queue-panel';
import '@components/playlist-details/playlist-details';

import { Events } from '../../src/events';
import { calls, emit, flush, resetHarness, stub } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';
import { installTouchGestures, LONG_PRESS_MS } from '@utils/touch-gestures';
import type { QueueTrack } from '@store/queue-store';

const HELD = LONG_PRESS_MS + 120;
const BRIEF = Math.round(LONG_PRESS_MS / 4);
const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

let uninstall: (() => void) | null = null;

afterEach(() => {
  // The layer is one document listener set, so a suite that leaves it
  // installed makes the next file's gestures fire twice.
  uninstall?.();
  uninstall = null;
});

function queueTrack(n: number): QueueTrack {
  return {
    id: n,
    audioFileId: n,
    filePath: `/music/${n}.mp3`,
    position: n,
    title: `Track ${n}`,
    artist: 'Artist',
    album: 'Album',
    coverArtPath: '',
    artistMbid: '',
    releaseGroupMbid: '',
    recordingMbid: '',
  };
}

const QUEUE = [1, 2, 3, 4].map(queueTrack);

function playlistTrack(n: number) {
  return {
    ID: n,
    FilePath: `/music/${n}.mp3`,
    Title: `Track ${n}`,
    Artist: 'Artist',
    Album: 'Album',
    Duration: 100 + n,
    Position: n,
    Phantom: false,
  };
}

const PLAYLIST_TRACKS = [1, 2, 3, 4].map(playlistTrack);

function press(el: EventTarget, type: string, init: PointerEventInit = {}) {
  el.dispatchEvent(
    new PointerEvent(type, {
      bubbles: true,
      composed: true,
      cancelable: true,
      pointerType: 'touch',
      isPrimary: true,
      clientX: 40,
      clientY: 60,
      ...init,
    }),
  );
}

async function tap(el: EventTarget) {
  press(el, 'pointerdown');
  await wait(BRIEF);
  press(el, 'pointerup');
  await wait(0);
}

async function hold(el: EventTarget) {
  press(el, 'pointerdown');
  await wait(HELD);
  press(el, 'pointerup');
  await wait(0);
}

/** Drag a row sideways by `dx` and lift, as one finger. */
async function swipe(el: EventTarget, dx: number) {
  const at = (x: number) =>
    new Touch({
      identifier: 1,
      target: el as Element,
      clientX: x,
      clientY: 100,
    });
  const send = (type: string, points: Touch[]) =>
    el.dispatchEvent(
      new TouchEvent(type, {
        bubbles: true,
        composed: true,
        cancelable: true,
        touches: points,
        changedTouches: points.length > 0 ? points : [at(0)],
      }),
    );

  send('touchstart', [at(0)]);

  for (const step of [0.25, 0.5, 0.75, 1]) {
    send('touchmove', [at(dx * step)]);
    await Promise.resolve();
  }

  send('touchend', []);
  await wait(0);
}

/** Past any commit threshold the row could compute. */
const FAR = 400;

describe('a finger on a queue row', () => {
  beforeEach(() => {
    resetHarness();
    stub('config.Config.GetShortcuts', {});
    stub('queue.Queue.PlayIndex', null);
    stub('queue.Queue.SetQueue', null);
    stub('queue.Queue.AddTracks', null);
    uninstall = installTouchGestures();
  });

  async function panel() {
    const el = await fixture('queue-panel', { open: true });

    emit(Events.QueueChanged, {
      tracks: QUEUE,
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

  const rows = (el: HTMLElement) => shadowAll<HTMLElement>(el, '.track-item');

  it('plays that position rather than rebuilding the queue', async () => {
    const el = await panel();

    await tap(rows(el)[2]!);
    await flush();

    expect(calls('queue.Queue.PlayIndex')[0]?.args[0]).toBe(2);

    // The asymmetry with `track-list`: setting the queue here would
    // discard its source, its shuffle order and anything inserted by
    // hand, which is not the no-op it reads as.
    expect(calls('queue.Queue.SetQueue').length).toBe(0);
  });

  it('enters selection mode on a hold, with that row selected', async () => {
    const el = await panel();

    await hold(rows(el)[1]!);
    await el.updateComplete;

    const bar = shadow(el, 'selection-bar');

    expect(bar, 'the action bar appears').toBeTruthy();
    expect((bar as unknown as { count: number }).count).toBe(1);
    expect(calls('queue.Queue.PlayIndex').length, 'and nothing played').toBe(0);
  });

  it('toggles rows while the mode is on, instead of playing them', async () => {
    const el = await panel();

    await hold(rows(el)[0]!);
    await el.updateComplete;
    await tap(rows(el)[2]!);
    await el.updateComplete;

    expect(calls('queue.Queue.PlayIndex').length).toBe(0);
    expect(
      (shadow(el, 'selection-bar') as unknown as { count: number }).count,
    ).toBe(2);
  });

  it('has no swipe, which is a decision and not an omission', async () => {
    const el = await panel();

    expect(
      rows(el)[1]?.hasAttribute('data-swipe'),
      'the row does not opt into the shared rule',
    ).toBe(false);

    await swipe(rows(el)[1]!, FAR);
    await flush();

    // A right swipe means "add to the queue" everywhere it exists.
    // The only thing it could mean on a queue row is "remove", which
    // is the same gesture with the opposite effect one screen away.
    expect(calls('queue.Queue.AddTracks').length).toBe(0);
    expect(calls('queue.Queue.RemoveTracks').length).toBe(0);
  });
});

describe('a finger on a playlist row', () => {
  beforeEach(() => {
    resetHarness();
    stub('config.Config.GetShortcuts', {});
    stub('playlist.Service.GetPlaylist', {
      ID: 1,
      Name: 'A Playlist',
      TrackCount: PLAYLIST_TRACKS.length,
    });
    stub('playlist.Service.GetPlaylistTracks', PLAYLIST_TRACKS);
    stub('queue.Queue.SetQueue', null);
    stub('queue.Queue.AddTracks', null);
    uninstall = installTouchGestures();
  });

  async function details() {
    const el = await fixture('playlist-details', {
      playlistId: 1,
      playlistName: 'A Playlist',
    });

    await flush();
    await el.updateComplete;
    await wait(60);
    await el.updateComplete;

    return el;
  }

  const rows = (el: HTMLElement) => shadowAll<HTMLElement>(el, '.track-item');

  it('plays the playlist from the row it taps', async () => {
    const el = await details();

    expect(rows(el).length, 'the list rendered rows').toBeGreaterThan(2);

    await tap(rows(el)[2]!);
    await flush();

    const queued = calls('queue.Queue.SetQueue');

    // The app's rule: activating one row plays the list that row is
    // in, from that row -- not a queue of one that stops when the song
    // ends.
    expect(queued.length).toBe(1);
    expect(queued[0]?.args[1]).toBe(2);
    expect((queued[0]?.args[0] as string[]).length).toBe(
      PLAYLIST_TRACKS.length,
    );
  });

  it('enters selection mode on a hold', async () => {
    const el = await details();

    await hold(rows(el)[1]!);
    await el.updateComplete;

    expect(
      (shadow(el, 'selection-bar') as unknown as { count: number } | null)
        ?.count,
    ).toBe(1);
    expect(calls('queue.Queue.SetQueue').length, 'nothing played').toBe(0);
  });

  it('queues the row a swipe crosses', async () => {
    const el = await details();

    await swipe(rows(el)[1]!, FAR);
    await flush();

    expect(calls('queue.Queue.AddTracks')[0]?.args[0]).toEqual([
      '/music/2.mp3',
    ]);
  });

  it('opts its rows into the shared touch-action rule', async () => {
    // Half of what makes the gesture reach us on the device, and
    // invisible in this browser either way -- the other half is the
    // non-passive preventDefault in `utils/touch-gestures.ts`.
    const el = await details();

    expect(rows(el)[0]?.hasAttribute('data-swipe')).toBe(true);

    const css = (
      customElements.get('playlist-details') as unknown as {
        styles: { cssText: string }[];
      }
    ).styles
      .map((s) => s.cssText)
      .join('\n');

    expect(css).toContain('touch-action: pan-y');
  });
});

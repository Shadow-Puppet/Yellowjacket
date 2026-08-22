/**
 * Swipe right on a track row to queue it (plan 019 phase 2, #63).
 *
 * `touch-gestures.test.ts` holds the recogniser — the axis rule, the
 * claim, the guaranteed end. What is here is what the *list* does with
 * it, and the two things that are only true of a list:
 *
 * **A swipe is not a selection.** It acts on the row it was made on,
 * unless that row is one of several the user has explicitly chosen, in
 * which case it acts on all of them — the same rule the context menu
 * answers with, because a bar saying "40 selected" and a gesture that
 * quietly queues one of them is two answers to the same question.
 *
 * **A short swipe is a no-op**, and that is the only thing standing
 * between "add to queue" and a scroll that drifted sideways. The
 * threshold is a fraction of the row, so it is measured from the row
 * here rather than written down twice.
 *
 * What this tier cannot see is the device, and the reason is in the
 * module's own header: Chrome 113's WebView cancels the pointer stream
 * ~16px into any drag whatever `touch-action` says, so the gesture
 * runs on touch events and needs `touch-action: pan-y` *and* a
 * non-passive `preventDefault`. Both are correct in Chromium either
 * way. The stylesheet half is asserted below for the same reason
 * `hover-affordance.test.ts` reads a parsed stylesheet: the regression
 * is someone tidying the declaration away, and nothing here renders
 * differently when they do.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import '@components/track-list/track-list';

import { calls, flush, resetHarness, stub } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';
import { installTouchGestures } from '@utils/touch-gestures';

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

let uninstall: (() => void) | null = null;

function track(n: number) {
  return {
    FilePath: `/music/track-${n}.mp3`,
    TrackName: `Track ${n}`,
    ArtistName: 'An Artist',
    Album: 'An Album',
    Duration: 100 + n,
    ID: n,
  };
}

const TRACKS = [track(1), track(2), track(3), track(4)];

async function mountList() {
  const el = await fixture('track-list');

  // A definite width, because the commit threshold is a fraction of
  // the row and a list that has not been given one is not a list.
  el.style.display = 'block';
  el.style.width = '400px';

  (el as unknown as { tracks: unknown[] }).tracks = TRACKS;
  await flush();
  await el.updateComplete;
  await wait(60);
  await el.updateComplete;

  return el;
}

function rows(el: HTMLElement): HTMLElement[] {
  return shadowAll<HTMLElement>(el, '.track-row');
}

/**
 * Drag a row sideways by `dx` and lift, as one finger.
 *
 * `onStep` runs after each move and is awaited, which is how the
 * reveal is observed: the component writes the travel straight to the
 * row's style but renders the pane through Lit, so it exists a frame
 * after the move that asked for it, not during it.
 */
async function swipe(
  el: EventTarget,
  dx: number,
  dy = 0,
  onStep?: () => Promise<void> | void,
): Promise<void> {
  const at = (x: number, y: number) =>
    new Touch({
      identifier: 1,
      target: el as Element,
      clientX: x,
      clientY: y,
    });
  const send = (type: string, points: Touch[]) =>
    el.dispatchEvent(
      new TouchEvent(type, {
        bubbles: true,
        composed: true,
        cancelable: true,
        touches: points,
        changedTouches: points.length > 0 ? points : [at(0, 0)],
      }),
    );

  send('touchstart', [at(0, 100)]);

  // Several steps, because the recogniser claims the gesture on the
  // move that crosses its threshold and the component reads every one
  // after it.
  for (const step of [0.25, 0.5, 0.75, 1]) {
    send('touchmove', [at(dx * step, 100 + dy * step)]);
    if (onStep) await onStep();
  }

  send('touchend', []);
}

/** Where the commit threshold falls for the row as rendered. */
function threshold(row: HTMLElement): number {
  return Math.max(72, row.getBoundingClientRect().width * 0.3);
}

describe('a finger swiped right across a track row', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetTracks', TRACKS);
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    stub('config.Config.GetShortcuts', {});
    stub('queue.Queue.SetQueue', null);
    stub('queue.Queue.AddTracks', null);
    uninstall = installTouchGestures();
  });

  afterEach(() => {
    uninstall?.();
    uninstall = null;
    vi.restoreAllMocks();
  });

  it('adds that row to the queue', async () => {
    const el = await mountList();
    const row = rows(el)[1];

    expect(row, 'the list rendered rows').toBeTruthy();

    await swipe(row!, threshold(row!) + 40);
    await flush();

    const queued = calls('queue.Queue.AddTracks');

    expect(queued.length, 'one swipe, one call').toBe(1);
    expect(queued[0]?.args[0]).toEqual(['/music/track-2.mp3']);

    // It queues; it does not play. The row that was playing keeps
    // playing, which is the difference from a tap.
    expect(calls('queue.Queue.SetQueue').length).toBe(0);
  });

  it('does nothing when the finger did not get far enough', async () => {
    const el = await mountList();
    const row = rows(el)[1];

    await swipe(row!, Math.round(threshold(row!)) - 10);
    await flush();

    expect(calls('queue.Queue.AddTracks').length).toBe(0);
  });

  it('leaves a scroll that began on a row to the list', async () => {
    // The same shape as `touch-selection.test.ts`'s "does not play a
    // row the finger scrolled from", one gesture over: the failure
    // this guards against makes the list unusable rather than wrong.
    const el = await mountList();
    const row = rows(el)[1];

    await swipe(row!, 30, 200);
    await flush();

    expect(calls('queue.Queue.AddTracks').length).toBe(0);
  });

  it('reveals what it will do, in words, while the finger is down', async () => {
    const el = await mountList();
    const row = rows(el)[1];
    const reveals: (string | undefined)[] = [];

    await swipe(row!, threshold(row!) + 40, 0, async () => {
      await el.updateComplete;
      reveals.push(
        shadow(el, '[data-testid="swipe-reveal"]')?.textContent?.trim(),
      );
    });

    // Not only a colour (WCAG 1.4.1): the pane says what it is for,
    // and says something different once the gesture would commit.
    expect(reveals.some((t) => t?.includes('Add to queue'))).toBe(true);
    expect(reveals.some((t) => t?.includes('Release to add'))).toBe(true);
  });

  it('says what it did, for anyone not watching the row', async () => {
    const el = await mountList();
    const row = rows(el)[1];

    await swipe(row!, threshold(row!) + 40);
    await flush();
    await el.updateComplete;

    const said = shadowAll(el, '[role="status"]')
      .map((r) => r.textContent?.trim())
      .join(' ');

    expect(said).toContain('Track 2');
    expect(said).toContain('queue');
  });

  it('queues the whole selection when the row is part of one', async () => {
    // One row is a position; several rows are an explicit choice. A
    // gesture that quietly queued the one row touched would contradict
    // the bar above it saying how many are selected.
    const el = await mountList();

    rows(el)[1]?.dispatchEvent(
      new MouseEvent('click', { bubbles: true, composed: true }),
    );
    rows(el)[3]?.dispatchEvent(
      new MouseEvent('click', {
        bubbles: true,
        composed: true,
        ctrlKey: true,
      }),
    );
    await el.updateComplete;

    const row = rows(el)[1];

    await swipe(row!, threshold(row!) + 40);
    await flush();

    expect(calls('queue.Queue.AddTracks')[0]?.args[0]).toEqual([
      '/music/track-2.mp3',
      '/music/track-4.mp3',
    ]);
  });

  it('queues only the row it touched when that row is outside the selection', async () => {
    const el = await mountList();

    rows(el)[3]?.dispatchEvent(
      new MouseEvent('click', { bubbles: true, composed: true }),
    );
    await el.updateComplete;

    const row = rows(el)[0];

    await swipe(row!, threshold(row!) + 40);
    await flush();

    expect(calls('queue.Queue.AddTracks')[0]?.args[0]).toEqual([
      '/music/track-1.mp3',
    ]);

    // And it did not become a way of selecting anything.
    expect(rows(el)[3]?.getAttribute('aria-selected')).toBe('true');
    expect(rows(el)[0]?.getAttribute('aria-selected')).toBe('false');
  });

  it('declares pan-y on the row, which is half of what makes it work', async () => {
    // The other half is the gesture module's non-passive
    // `preventDefault`. Neither works alone on Chrome 113 and both are
    // irrelevant here, so this reads the stylesheet and the attribute
    // rather than the rendering — the regression is someone tidying
    // one of them away, and nothing in this browser looks different
    // when they do.
    const el = await mountList();

    expect(
      rows(el)[0]?.hasAttribute('data-swipe'),
      'the row opts into the shared rule',
    ).toBe(true);

    const sheets = (
      customElements.get('track-list') as unknown as {
        styles: { cssText: string }[];
      }
    ).styles;
    const css = sheets.map((s) => s.cssText).join('\n');
    const rule = css
      .split('}')
      .find((block) => /\[data-swipe\]\s*\{/.test(block));

    expect(rule, 'the shared rule is in this component').toBeTruthy();
    expect(rule).toContain('touch-action: pan-y');
    expect(css, 'never none: it takes the scrolling too').not.toContain(
      'touch-action: none',
    );
  });
});

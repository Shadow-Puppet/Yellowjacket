/**
 * What a finger does to a track list (plan 019, #63).
 *
 * The desktop semantics being diverged from are real and stay: a click
 * selects, a double-click plays. A finger has no second button and no
 * modifier keys, so the primary action has to be the primary gesture —
 * and the inversion is decided **per event**, off `pointerType`, not
 * off a viewport width or a platform flag (plan 019, decision 1).
 *
 * That is what these assert: the same row, in the same component, at
 * the same width, answering a mouse one way and a finger the other.
 *
 * There is deliberately no double-tap. Measured on the reference
 * device, playing a track is ~100ms end to end, and a double-tap
 * discriminator has to hold every tap for the app's own
 * `DOUBLE_CLICK_GRACE_MS` of 250 before it can act — 3.5x the primary
 * interaction, to reach a menu a long press already reaches.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import '@components/track-list/track-list';
import '@components/selection-bar/selection-bar';

import { calls, flush, resetHarness, stub } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';
import { installTouchGestures, LONG_PRESS_MS } from '@utils/touch-gestures';

const HELD = LONG_PRESS_MS + 120;
const BRIEF = Math.round(LONG_PRESS_MS / 4);
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

/** Dispatch a pointer event as a finger would produce it. */
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

/** A whole finger tap: down, a moment, up. */
async function tap(el: EventTarget) {
  press(el, 'pointerdown');
  await wait(BRIEF);
  press(el, 'pointerup');
  await wait(0);
}

/** A finger held still until the gesture resolves. */
async function hold(el: EventTarget) {
  press(el, 'pointerdown');
  await wait(HELD);
  press(el, 'pointerup');
  await wait(0);
}

async function mountList() {
  const el = await fixture('track-list');

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

describe('a finger on a track row', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetTracks', TRACKS);
    stub('library.Library.GetAllLibrariesWithTrackCounts', []);
    stub('config.Config.GetShortcuts', {});
    stub('queue.Queue.SetQueue', null);
    stub('queue.Queue.AddTracksToQueue', null);
    uninstall = installTouchGestures();
  });

  afterEach(() => {
    uninstall?.();
    uninstall = null;
    vi.restoreAllMocks();
  });

  it('plays the row it taps, rather than selecting it', async () => {
    const el = await mountList();
    const row = rows(el)[1];

    expect(row, 'the list rendered rows').toBeTruthy();

    await tap(row!);
    await flush();

    const queued = calls('queue.Queue.SetQueue');

    expect(queued.length, 'a tap plays').toBe(1);

    // From that row, in the list as displayed -- not a queue of one
    // that stops when the song ends.
    expect(queued[0]?.args[1]).toBe(1);
    expect((queued[0]?.args[0] as string[]).length).toBe(TRACKS.length);
  });

  it('leaves the same row to a mouse, which still selects', async () => {
    // The inversion is per event, so one component answers both
    // pointers at the same width. A viewport rule cannot say this.
    const el = await mountList();
    const row = rows(el)[1];

    row!.dispatchEvent(
      new MouseEvent('click', { bubbles: true, composed: true }),
    );
    await el.updateComplete;

    expect(calls('queue.Queue.SetQueue').length, 'a click does not play').toBe(0);
    expect(rows(el)[1]?.getAttribute('aria-selected')).toBe('true');
  });

  it('enters selection mode on a long press, with that row selected', async () => {
    const el = await mountList();

    await hold(rows(el)[2]!);
    await el.updateComplete;

    const bar = shadow(el, 'selection-bar');

    expect(bar, 'the action bar appears').toBeTruthy();
    expect((bar as unknown as { count: number }).count).toBe(1);
    expect(rows(el)[2]?.getAttribute('aria-selected')).toBe('true');
  });

  it('does not open a context menu when it enters the mode', async () => {
    // The gesture is claimed, so the layer must not fall through to
    // the synthetic `contextmenu` that every other surface still gets.
    const el = await mountList();

    await hold(rows(el)[0]!);
    await el.updateComplete;

    const menu = shadow(el, 'menu-surface');

    expect((menu as unknown as { active?: boolean } | null)?.active ?? false).toBe(
      false,
    );
  });

  it('toggles rows while the mode is on, instead of playing them', async () => {
    const el = await mountList();

    await hold(rows(el)[0]!);
    await el.updateComplete;
    await tap(rows(el)[2]!);
    await el.updateComplete;

    expect(calls('queue.Queue.SetQueue').length, 'no track was played').toBe(0);
    expect(
      (shadow(el, 'selection-bar') as unknown as { count: number }).count,
    ).toBe(2);
  });

  it('leaves the mode when the last row is deselected', async () => {
    // Android's own lists do this, and here it matters more than
    // convention: the mode changes what a tap means, so a mode holding
    // nothing is a list where tapping does nothing and the bar that
    // would explain it is showing a count of zero.
    const el = await mountList();

    await hold(rows(el)[0]!);
    await el.updateComplete;
    await tap(rows(el)[0]!);
    await el.updateComplete;

    expect(shadow(el, 'selection-bar')).toBeFalsy();
  });

  it('keeps the favourite icon a favourite icon', async () => {
    // It is inside a row whose tap now plays, and it has been a 44px
    // target since #56 -- so without the "a control inside the row
    // owns its own tap" rule, that target silently becomes a second
    // play button.
    const el = await mountList();
    const fav = rows(el)[1]?.querySelector('.fav-icon');

    expect(fav, 'a row renders a favourite control').toBeTruthy();

    await tap(fav!);
    await flush();

    expect(calls('queue.Queue.SetQueue').length, 'tapping it does not play').toBe(
      0,
    );
  });

  it('does not play a row the finger scrolled from', async () => {
    // The failure this exists for makes the list unusable rather than
    // merely wrong: every flick to scroll would start a track.
    const el = await mountList();
    const row = rows(el)[1];

    press(row!, 'pointerdown');
    press(row!, 'pointermove', { clientX: 40, clientY: 200 });
    press(row!, 'pointerup');
    await flush();

    expect(calls('queue.Queue.SetQueue').length).toBe(0);
  });
});

describe('<selection-bar>', () => {
  it('renders nothing with nothing selected', async () => {
    const el = await fixture('selection-bar', { count: 0 });

    expect(shadow(el, '.bar')).toBeFalsy();
  });

  it('announces the count, which changes under the finger', async () => {
    const el = await fixture('selection-bar', { count: 3, actions: [] });
    const live = shadow(el, '[aria-live="polite"]');

    expect(live?.textContent?.trim()).toContain('3 tracks selected');
  });

  it('names one track in the singular', async () => {
    const el = await fixture('selection-bar', { count: 1, actions: [] });

    expect(shadow(el, '[aria-live="polite"]')?.textContent?.trim()).toContain(
      '1 track selected',
    );
  });

  it('keeps every control at the touch floor', async () => {
    // #56 and #186. A bar a thumb uses, in the one mode that only a
    // thumb can enter.
    const el = await fixture('selection-bar', {
      count: 2,
      actions: [{ id: 'play', label: 'Play', icon: 'play' }],
    });

    const buttons = shadowAll<HTMLElement>(el, 'button');

    expect(buttons.length).toBeGreaterThan(0);

    for (const button of buttons) {
      const box = button.getBoundingClientRect();

      expect(
        Math.min(Math.round(box.width), Math.round(box.height)),
        button.getAttribute('aria-label') ?? '',
      ).toBeGreaterThanOrEqual(44);
    }
  });
});

/**
 * The touch gestures (plan 019, #63; long-press from plan 016 B2).
 *
 * These run in a real browser with real event dispatch, which is the
 * only place the two things that make this hard are true: the synthetic
 * event has to cross a shadow boundary to reach the listener a
 * component actually bound, and the suppressors have to tell a trusted
 * event from ours at document capture without eating the one they exist
 * to deliver.
 *
 * The timings are real rather than faked, because the thing under test
 * *is* a timing, and 600 ms twice is cheaper than a fake-timer harness
 * that would also have to fake the pointer events.
 */
import { describe, expect, it, afterEach, beforeEach } from 'vitest';

import {
  installTouchGestures,
  LONG_PRESS_MS,
  MOVE_TOLERANCE_PX,
} from '@utils/touch-gestures';

/** A press that has certainly resolved, either way. */
const HELD = LONG_PRESS_MS + 120;

/** A press that has certainly not. */
const BRIEF = Math.round(LONG_PRESS_MS / 4);

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

let uninstall: (() => void) | null = null;
let host: HTMLElement;
let inner: HTMLElement;

/** A row inside a shadow root, which is where every menu in this app
 *  is bound — an element in the light DOM would pass a weaker test. */
function mountRow(): { host: HTMLElement; inner: HTMLElement } {
  const el = document.createElement('div');
  const root = el.attachShadow({ mode: 'open' });
  const row = document.createElement('div');

  row.textContent = 'a track';
  root.append(row);
  document.body.append(el);

  return { host: el, inner: row };
}

function press(
  el: EventTarget,
  type: string,
  init: PointerEventInit = {},
): void {
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

/**
 * Record every `contextmenu` that reaches the listener, *as the
 * listener sees it*.
 *
 * `target` is retargeted for the scope reading it, so an assertion made
 * after dispatch has finished reports the shadow host however the event
 * was dispatched - which is the same answer a broken implementation
 * gives. It has to be read from inside the handler, where the component
 * reads it.
 */
function recordMenus(el: EventTarget): { event: MouseEvent; target: EventTarget | null }[] {
  const seen: { event: MouseEvent; target: EventTarget | null }[] = [];

  el.addEventListener('contextmenu', (e) => {
    e.preventDefault();
    // Every real handler does this; the gesture must work anyway.
    e.stopPropagation();
    seen.push({ event: e as MouseEvent, target: e.target });
  });

  return seen;
}

describe('an unclaimed long press is still a context menu', () => {
  beforeEach(() => {
    uninstall = installTouchGestures();
    ({ host, inner } = mountRow());
  });

  afterEach(() => {
    uninstall?.();
    uninstall = null;
    host.remove();
  });

  it('dispatches one at the touch point, on the element touched', async () => {
    const seen = recordMenus(inner);

    press(inner, 'pointerdown');
    await wait(HELD);

    expect(seen).toHaveLength(1);
    expect(seen[0]?.event.clientX).toBe(40);
    expect(seen[0]?.event.clientY).toBe(60);
    // Dispatched on the row itself, not on its shadow host - which is
    // the difference between a per-row handler firing and only a
    // delegated one firing.
    expect(seen[0]?.target).toBe(inner);
  });

  it('is cancelled by a press that moves', async () => {
    const seen = recordMenus(inner);

    press(inner, 'pointerdown');
    press(inner, 'pointermove', {
      clientX: 40 + MOVE_TOLERANCE_PX + 5,
      clientY: 60,
    });
    await wait(HELD);

    expect(seen).toHaveLength(0);
  });

  it('tolerates the jitter a finger cannot help', async () => {
    const seen = recordMenus(inner);

    press(inner, 'pointerdown');
    press(inner, 'pointermove', { clientX: 43, clientY: 62 });
    await wait(HELD);

    expect(seen).toHaveLength(1);
  });

  it('is cancelled by lifting early, and by a scroll', async () => {
    const seen = recordMenus(inner);

    press(inner, 'pointerdown');
    await wait(BRIEF);
    press(inner, 'pointerup');
    await wait(HELD);

    expect(seen).toHaveLength(0);

    press(inner, 'pointerdown');
    press(inner, 'pointercancel');
    await wait(HELD);

    expect(seen).toHaveLength(0);
  });

  it('ignores a mouse, which has a right button of its own', async () => {
    const seen = recordMenus(inner);

    press(inner, 'pointerdown', { pointerType: 'mouse' });
    await wait(HELD);

    expect(seen).toHaveLength(0);
  });

  it('swallows the click that ends the gesture, and only that one', async () => {
    let clicks = 0;

    inner.addEventListener('click', () => {
      clicks += 1;
    });

    press(inner, 'pointerdown');
    await wait(HELD);
    press(inner, 'pointerup');
    inner.click();

    expect(clicks).toBe(0);

    // The next tap is a tap: on a phone that is the user choosing an
    // item in the menu that just opened, so eating it would make the
    // gesture useless.
    press(inner, 'pointerdown');
    press(inner, 'pointerup');
    inner.click();

    expect(clicks).toBe(1);
  });

  it('stands down where the browser fires its own', async () => {
    const seen = recordMenus(inner);

    press(inner, 'pointerdown');
    await wait(BRIEF);
    // Chromium does this itself on touch; WebKit and the Android
    // WebView vary, which is the whole reason both halves exist. A
    // test cannot dispatch a *trusted* event, which is why the module
    // tells its own apart by identity rather than by `isTrusted`.
    inner.dispatchEvent(
      new MouseEvent('contextmenu', {
        bubbles: true,
        composed: true,
        cancelable: true,
      }),
    );
    await wait(HELD);

    // One menu: the browser's. Not two.
    expect(seen).toHaveLength(1);
  });
});

/**
 * What plan 019 adds on top, and the one property that protects
 * everything downstream: a long press nobody claims is unchanged.
 */
describe('a gesture is announced before it is acted on', () => {
  beforeEach(() => {
    uninstall = installTouchGestures();
    ({ host, inner } = mountRow());
  });

  afterEach(() => {
    uninstall?.();
    uninstall = null;
    host.remove();
  });

  it('does not synthesise a menu when the long press is claimed', async () => {
    // This is the whole reason #63 could reassign the hold without
    // touching one of the fourteen context menus: the lists that want
    // selection mode claim it, and nothing else changes.
    const menus = recordMenus(inner);
    const presses: unknown[] = [];

    inner.addEventListener('yj-long-press', (e) => {
      presses.push(e);
      e.preventDefault();
    });

    press(inner, 'pointerdown');
    await wait(HELD);

    expect(presses).toHaveLength(1);
    expect(menus).toHaveLength(0);
  });

  it('announces a tap on the element touched, at the touch point', async () => {
    const taps: { target: EventTarget | null; x: number; y: number }[] = [];

    inner.addEventListener('yj-tap', (e) => {
      taps.push({ target: e.target, x: e.detail.x, y: e.detail.y });
    });

    press(inner, 'pointerdown');
    await wait(BRIEF);
    press(inner, 'pointerup');

    expect(taps).toHaveLength(1);
    // The row, not its shadow host -- the difference between a
    // delegated handler firing and a per-row one never firing.
    expect(taps[0]?.target).toBe(inner);
    expect([taps[0]?.x, taps[0]?.y]).toEqual([40, 60]);
  });

  it('lets an unclaimed tap through as an ordinary click', async () => {
    // Every button, link and checkbox in the app depends on this. Only
    // a *claimed* tap has its click swallowed.
    let clicks = 0;

    inner.addEventListener('yj-tap', () => {
      /* seen, not claimed */
    });
    inner.addEventListener('click', () => {
      clicks += 1;
    });

    press(inner, 'pointerdown');
    await wait(BRIEF);
    press(inner, 'pointerup');
    inner.click();

    expect(clicks).toBe(1);
  });

  it('swallows the click behind a claimed tap', async () => {
    // Or playing a track would also select it, and the row would end
    // up in both states at once.
    let clicks = 0;

    inner.addEventListener('yj-tap', (e) => e.preventDefault());
    inner.addEventListener('click', () => {
      clicks += 1;
    });

    press(inner, 'pointerdown');
    await wait(BRIEF);
    press(inner, 'pointerup');
    inner.click();

    expect(clicks).toBe(0);
  });

  it('does not announce a tap for a press that became a long press', async () => {
    // A hold is one gesture, not a hold and then a tap on release.
    const taps: unknown[] = [];

    inner.addEventListener('yj-tap', (e) => taps.push(e));

    press(inner, 'pointerdown');
    await wait(HELD);
    press(inner, 'pointerup');

    expect(taps).toHaveLength(0);
  });

  it('does not announce a tap for a press that drifted', async () => {
    // A drifted press is a scroll, and the virtualizer's -- not a tap
    // that happened to move. This is the one that would make a list
    // unscrollable if it were wrong.
    const taps: unknown[] = [];

    inner.addEventListener('yj-tap', (e) => taps.push(e));

    press(inner, 'pointerdown');
    press(inner, 'pointermove', {
      clientX: 40,
      clientY: 60 + MOVE_TOLERANCE_PX + 20,
    });
    press(inner, 'pointerup');

    expect(taps).toHaveLength(0);
  });

  it('ignores a mouse entirely, for both gestures', async () => {
    // plan 019 decision 1: the predicate is the pointer, per event. A
    // mouse on a touchscreen keeps click-selects / double-click-plays
    // on the very same row, which no viewport width can express.
    const seen: unknown[] = [];

    inner.addEventListener('yj-tap', (e) => seen.push(e));
    inner.addEventListener('yj-long-press', (e) => seen.push(e));

    press(inner, 'pointerdown', { pointerType: 'mouse' });
    await wait(BRIEF);
    press(inner, 'pointerup', { pointerType: 'mouse' });
    press(inner, 'pointerdown', { pointerType: 'mouse' });
    await wait(HELD);

    expect(seen).toHaveLength(0);
  });
});

/**
 * The browser's own long press is a trigger, not a competitor.
 *
 * This is a device-only defect made checkable here. `long-press.ts`
 * stood down when a trusted `contextmenu` arrived mid-press, which was
 * right while both paths ended in a context menu. Once a hold can mean
 * *selection mode*, standing down means the gesture silently does the
 * old thing — and Chrome 113's Android WebView does fire its own, so
 * on the reference device `yj-long-press` was never announced at all
 * while every test in this tier passed.
 *
 * A test cannot dispatch a *trusted* event, which is exactly why the
 * module tells its own apart by identity rather than by `isTrusted`:
 * an untrusted one dispatched from here takes the same path the
 * browser's does.
 */
describe("the browser's own long press", () => {
  beforeEach(() => {
    uninstall = installTouchGestures();
    ({ host, inner } = mountRow());
  });

  afterEach(() => {
    uninstall?.();
    uninstall = null;
    host.remove();
  });

  /** Stand in for the browser recognising the hold itself. */
  function browserContextMenu(el: EventTarget): MouseEvent {
    const e = new MouseEvent('contextmenu', {
      bubbles: true,
      composed: true,
      cancelable: true,
    });

    el.dispatchEvent(e);

    return e;
  }

  it('announces the gesture rather than standing down', async () => {
    const presses: unknown[] = [];

    inner.addEventListener('yj-long-press', (e) => {
      presses.push(e);
      e.preventDefault();
    });

    press(inner, 'pointerdown');
    await wait(BRIEF);
    browserContextMenu(inner);
    await wait(HELD);

    expect(presses, 'the hold reached the component').toHaveLength(1);
  });

  it('suppresses its menu when a component claims the gesture', async () => {
    const menus = recordMenus(inner);

    inner.addEventListener('yj-long-press', (e) => e.preventDefault());

    press(inner, 'pointerdown');
    await wait(BRIEF);
    browserContextMenu(inner);
    await wait(HELD);

    // The component wants selection mode, so the menu must not also
    // open -- otherwise the device shows both at once.
    expect(menus).toHaveLength(0);
  });

  it('still opens exactly one menu when nobody claims it', async () => {
    // The old behaviour, reached by asking instead of assuming. This
    // is what leaves the card grids, Explore and the playlist rows
    // untouched by #63.
    const menus = recordMenus(inner);

    press(inner, 'pointerdown');
    await wait(BRIEF);
    browserContextMenu(inner);
    await wait(HELD);

    expect(menus).toHaveLength(1);
  });
});

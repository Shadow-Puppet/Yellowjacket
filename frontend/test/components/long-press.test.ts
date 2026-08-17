/**
 * Long-press as the touch route to a context menu (plan 016 B2 phase 3).
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
  installLongPressContextMenu,
  LONG_PRESS_MS,
  MOVE_TOLERANCE_PX,
} from '@utils/long-press';

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

describe('long-press opens a context menu', () => {
  beforeEach(() => {
    uninstall = installLongPressContextMenu();
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

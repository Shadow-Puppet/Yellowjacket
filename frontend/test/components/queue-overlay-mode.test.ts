/**
 * #24 — the queue stops being a column when it cannot afford to be one.
 *
 * In flow the panel is `flex-shrink: 0`, so it takes its width *from
 * the main panel* rather than covering it. Measured against the running
 * app on the Playlists page, that left 379px of content at 900×600 —
 * with all three of the page header's actions clipped — 69px at 390px
 * wide, and **0px** at 320px, where the content was not degraded but
 * gone.
 *
 * The rule is `available - panelWidth >= MAIN_PANEL_FLOOR`, and the
 * reason it is a computed property rather than a `@media` block is the
 * third test here: the panel's width is user state, drag-resizable
 * between 200 and 500px and persisted, so a breakpoint on the viewport
 * alone is wrong by up to 180px for a user who has widened it — in the
 * direction that hurts, since a wider queue is exactly when the content
 * can least afford it.
 *
 * The parent is `.content-area`, i.e. the viewport minus the sidebar,
 * which is why these mount into a sized wrapper rather than into
 * `document.body`: the width that decides this is the *parent's*, and
 * `fixture()` would hand the panel the whole test window.
 */
import { describe, expect, it, afterEach } from 'vitest';

import '@components/queue-panel/queue-panel';
import type { QueuePanel } from '@components/queue-panel/queue-panel';
import { shadow } from '@test/support/render';

const wrappers: HTMLElement[] = [];

let restoreMedia: (() => void) | null = null;

/**
 * Answer the shell's phone query with `phone` until restored.
 *
 * Stubbed rather than emulated, for the reason `search-dialog.test.ts`
 * gives: the runner's viewport is fixed at 1280x800, and the panel
 * reads `matchMedia` in `connectedCallback` precisely so a test can
 * answer it first.
 */
/**
 * Answering the phone query is not enough on its own: what decides
 * whether the scrim exists is a `change` listener, and a stub whose
 * `addEventListener` is a no-op leaves that listener untested — the
 * whole suite stays green with it deleted. So the stub records the
 * listeners and hands back a way to fire them.
 */
function stubPhone(phone: boolean): (next: boolean) => void {
  const real = window.matchMedia.bind(window);
  const listeners = new Set<(e: MediaQueryListEvent) => void>();
  let matches = phone;

  window.matchMedia = ((q: string) =>
    q.includes('max-width: 599px')
      ? {
          get matches() {
            return matches;
          },
          media: q,
          addEventListener(_: string, fn: (e: MediaQueryListEvent) => void) {
            listeners.add(fn);
          },
          removeEventListener(_: string, fn: (e: MediaQueryListEvent) => void) {
            listeners.delete(fn);
          },
        }
      : real(q)) as typeof window.matchMedia;

  restoreMedia = () => {
    window.matchMedia = real;
  };

  return (next: boolean) => {
    matches = next;

    for (const fn of listeners) {
      fn({ matches: next } as MediaQueryListEvent);
    }
  };
}

afterEach(() => {
  for (const w of wrappers.splice(0)) w.remove();
  restoreMedia?.();
  restoreMedia = null;
});

/**
 * Mount a panel inside a parent of a stated width.
 *
 * The wrapper is `position: relative` and `display: flex` because that
 * is what `.content-area` is; the mode is measured from
 * `parentElement.clientWidth`, so a wrapper that collapses to its
 * content would measure the panel rather than the space around it.
 */
async function panelIn(parentWidth: number): Promise<QueuePanel> {
  const wrapper = document.createElement('div');

  wrapper.style.cssText = `position: relative; display: flex; width: ${parentWidth}px;`;
  document.body.append(wrapper);
  wrappers.push(wrapper);

  const el = document.createElement('queue-panel') as QueuePanel;

  el.open = true;
  wrapper.append(el);

  await el.updateComplete;
  await settle(el);

  return el;
}

/**
 * A ResizeObserver delivers on a frame, not a microtask, so the mode
 * lands a frame after the width that decides it.
 */
async function settle(el: QueuePanel): Promise<void> {
  for (let frame = 0; frame < 4; frame += 1) {
    await new Promise((r) => {
      requestAnimationFrame(() => r(null));
    });
    await el.updateComplete;
  }
}

/** Drag the resize handle by `dx`, the way a user widens the panel. */
async function dragHandleBy(el: QueuePanel, dx: number): Promise<void> {
  const handle = shadow<HTMLElement>(el, '.resize-handle');
  const startX = el.getBoundingClientRect().left;

  if (!handle) throw new Error('no resize handle to drag');

  handle.dispatchEvent(
    new MouseEvent('mousedown', { clientX: startX, bubbles: true }),
  );
  document.dispatchEvent(
    new MouseEvent('mousemove', { clientX: startX - dx, bubbles: true }),
  );
  document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));

  await settle(el);
}

describe('the queue panel decides whether it can be a column', () => {
  it('stays inline while the content can spare the width', async () => {
    const el = await panelIn(1080);

    expect(el.overlay).toBe(false);
    expect(el.hasAttribute('overlay')).toBe(false);
  });

  it('becomes an overlay when it cannot', async () => {
    const el = await panelIn(700);

    expect(el.overlay).toBe(true);
    expect(el.hasAttribute('overlay')).toBe(true);
  });

  /**
   * The test the media query could not have passed. The parent does not
   * move; only the user's own panel width does.
   */
  it('flips to overlay when the user widens the panel, at a fixed width', async () => {
    const el = await panelIn(880);

    expect(el.overlay).toBe(false);

    await dragHandleBy(el, 180);

    expect(el.overlay).toBe(true);
  });

  it('gives an overlay a scrim and a named way out, and an inline panel neither', async () => {
    const overlaid = await panelIn(700);

    expect(shadow(overlaid, '.scrim')).toBeTruthy();

    const close = shadow(overlaid, '[data-testid="queue-close"]');

    expect(close?.getAttribute('aria-label')).toBe('Close queue');

    const inline = await panelIn(1080);

    expect(inline.shadowRoot?.querySelector('.scrim')).toBeNull();
    expect(
      inline.shadowRoot?.querySelector('[data-testid="queue-close"]'),
    ).toBeNull();
  });

  it('closes on the scrim, on the close button and on Escape', async () => {
    for (const close of [
      (el: QueuePanel) => shadow<HTMLElement>(el, '.scrim')?.click(),
      (el: QueuePanel) =>
        shadow<HTMLElement>(el, '[data-testid="queue-close"]')?.click(),
      () =>
        document.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
        ),
    ]) {
      const el = await panelIn(700);

      expect(el.open).toBe(true);

      close(el);
      await el.updateComplete;

      expect(el.open).toBe(false);
    }
  });

  /**
   * #171 — the scrim is a dismissal target, so it exists only where it
   * has pixels to be tapped.
   *
   * Below 600px `.panel-content` is `width: 100%`, so the scrim is
   * entirely underneath an opaque panel: measured at 424x439, host,
   * panel and scrim all 424x318. Drawing it there is a `cursor:
   * pointer` click target nobody can reach, and the queue is a screen
   * at that width anyway (#55) — back and the close button are its ways
   * out. Existence rather than `display: none`, because a hidden scrim
   * is still an element carrying the handler.
   */
  it('draws no scrim at phone width, where it would have no reachable pixels', async () => {
    stubPhone(true);

    const el = await panelIn(424);

    expect(el.overlay).toBe(true);
    expect(el.shadowRoot?.querySelector('.scrim')).toBeNull();

    // The way out a thumb can hit is still there.
    expect(
      shadow(el, '[data-testid="queue-close"]')?.getAttribute('aria-label'),
    ).toBe('Close queue');
  });

  /**
   * The 600–899 band is where the panel is a 320px column of a wider
   * content area, so the scrim has uncovered pixels and #24's
   * tap-outside-to-close is real. Same width as the overlay tests
   * above, with the phone query explicitly answered `false`, so this
   * fails if the scrim is ever dropped for every overlay.
   */
  it('keeps the scrim above phone width, where it can be tapped', async () => {
    stubPhone(false);

    const el = await panelIn(700);

    expect(el.overlay).toBe(true);

    shadow<HTMLElement>(el, '.scrim')?.click();
    await el.updateComplete;

    expect(el.open).toBe(false);
  });

  /**
   * The scrim's existence comes from `matchMedia` rather than a
   * stylesheet, which only holds up if the query is *listened* to — a
   * panel opened on a desktop and carried across the breakpoint (a
   * resized window, an unfolded phone) has to lose its scrim without
   * being reopened. Nothing else in this file fires `change`, so
   * deleting the listener leaves the whole suite green.
   */
  it('drops the scrim when the viewport crosses the breakpoint', async () => {
    const setPhone = stubPhone(false);

    const el = await panelIn(700);

    expect(el.shadowRoot?.querySelector('.scrim')).not.toBeNull();

    setPhone(true);
    await el.updateComplete;

    expect(el.shadowRoot?.querySelector('.scrim')).toBeNull();

    setPhone(false);
    await el.updateComplete;

    expect(el.shadowRoot?.querySelector('.scrim')).not.toBeNull();
  });

  /**
   * Escape belongs to the overlay, not to the queue. An inline panel is
   * beside the content rather than over it, so there is nothing to
   * dismiss and the key has to reach whatever else wants it.
   */
  it('leaves Escape alone while inline', async () => {
    const el = await panelIn(1080);

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );
    await el.updateComplete;

    expect(el.open).toBe(true);
  });
});

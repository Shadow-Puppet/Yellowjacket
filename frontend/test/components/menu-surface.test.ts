/**
 * Where a context menu is drawn (#60).
 *
 * **This file asserts the mechanism, not the symptom, and that is the
 * whole point of it.** The defect is that on the reference device's
 * Chrome 113 a `wa-popup` has no Popover API to promote it to the top
 * layer, so it falls back to `position: fixed` and is then *clipped* by
 * `.main-panel`'s `contain: paint`. No tier here can reproduce that:
 * this runner's Chromium and CI's WebKit both have the Popover API, so
 * the popup is top-layered and looks perfectly correct. A test that
 * asserted "the menu is not clipped" would pass on the broken build.
 *
 * What is checkable everywhere is *which surface exists*. A native
 * `<dialog>` uses the real top layer, which Chrome 37 has, so "it is a
 * dialog at phone width" is the property that makes the device
 * behaviour follow. The measurements that needed the hardware are on
 * the PR and in `.planning/NOTES.md`.
 */
import { describe, expect, it, beforeEach, afterEach } from 'vitest';

import '@components/menu-surface/menu-surface';
import { MENU_DISMISS_EVENT } from '@components/menu-surface/menu-surface';
import { fixture } from '@test/support/render';

/** Every source file, as text. */
const SOURCES = import.meta.glob<string>('../../src/**/*.ts', {
  eager: true,
  query: '?raw',
  import: 'default',
});

/**
 * The two files allowed to render a raw `wa-popup`.
 *
 * `menu-surface` *is* the popup, in its desktop presentation.
 * `job-indicator` is the documented exception and the contrast that
 * proved the diagnosis: it lives in `.top-bar`, no ancestor of which
 * has containment, so even the fixed fallback lands correctly on the
 * device -- measured on #62, unclipped at every width.
 *
 * `now-playing`'s cover preview is the third, and it is a different
 * reason: it is not a menu. It opens on `mouseenter` over the album
 * art, so a touch device never sees it at all, and a bottom sheet for
 * a hover preview would be absurd. It is also in the bottom bar rather
 * than the main panel, so nothing clips it either.
 *
 * **Both were found by this sweep, not by the conversion**, which is
 * the argument for having it: twelve call sites were converted by hand
 * and two more existed.
 */
const MAY_USE_POPUP = [
  'menu-surface/menu-surface.ts',
  'jobs/job-indicator.ts',
  'now-playing/now-playing.ts',
];

/**
 * Answer `matchMedia` for the phone query, on `transport-context`'s
 * pattern: what is under test is the component's reaction to the
 * answer, not whether this runner's window can get below 600px.
 */
const realMatchMedia = window.matchMedia;

function pretendPhone(phone: boolean): void {
  window.matchMedia = ((query: string) => ({
    matches: phone && query.includes('599'),
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia;
}

/** A surface with the panel a real call site slots into it. */
async function surfaceWithPanel(): Promise<HTMLElement> {
  const el = await fixture('menu-surface');

  el.innerHTML =
    '<div class="context-menu-panel" role="menu" aria-label="Track actions">' +
    '<wa-dropdown-item>Play</wa-dropdown-item>' +
    '</div>';

  const surface = el as unknown as HTMLElement & {
    active: boolean;
    updateComplete: Promise<unknown>;
  };

  surface.active = true;
  await surface.updateComplete;

  return el;
}

/**
 * The sweep, in the spirit of `icon-language.test.ts` and
 * `TestNoDirectRuntimeEmits`: the rule is about *every* call site, and
 * checking one checks nothing.
 *
 * Twelve menus were converted by hand. A thirteenth written as a bare
 * `<wa-popup>` would work perfectly in every tier here and be clipped
 * on the device, which is exactly the failure this whole change is
 * about and exactly the one no runtime assertion can see.
 */
describe('every menu goes through the one surface', () => {
  it('reads the sources at all', () => {
    // A sweep over an empty glob passes, so this is asserted first.
    expect(Object.keys(SOURCES).length).toBeGreaterThan(100);
  });

  it('leaves no raw wa-popup outside the two files allowed one', () => {
    const offenders = Object.entries(SOURCES)
      .filter(([path]) => !MAY_USE_POPUP.some((ok) => path.endsWith(ok)))
      .filter(([, src]) => src.includes('<wa-popup'))
      .map(([path]) => path.replace(/^.*\/src\//, 'src/'));

    expect(
      offenders,
      'these render a popup directly; use <menu-surface> so the phone gets a sheet',
    ).toEqual([]);
  });
});

describe('menu-surface', () => {
  afterEach(() => {
    window.matchMedia = realMatchMedia;
  });

  describe('above the phone breakpoint', () => {
    beforeEach(() => pretendPhone(false));

    it('draws a popup, which is what the desktop has always had', async () => {
      const el = await surfaceWithPanel();

      expect(el.shadowRoot?.querySelector('wa-popup')).not.toBeNull();
      expect(el.shadowRoot?.querySelector('wa-dialog')).toBeNull();
    });

    it('does not mark the panel as a sheet', async () => {
      const el = await surfaceWithPanel();

      expect(
        el.querySelector('.context-menu-panel')?.hasAttribute('data-sheet'),
      ).toBe(false);
    });
  });

  describe('at phone width', () => {
    beforeEach(() => pretendPhone(true));

    /**
     * The load-bearing one. `wa-dialog` renders a *native* `<dialog>`,
     * and it is the native element -- not the wrapper -- that gets the
     * top layer and therefore escapes the paint containment that clips
     * the popup on the device.
     */
    it('draws a native dialog, which is what escapes the clip', async () => {
      const el = await surfaceWithPanel();

      const wrapper = el.shadowRoot?.querySelector('wa-dialog');

      expect(wrapper, 'no wa-dialog at phone width').not.toBeNull();
      expect(el.shadowRoot?.querySelector('wa-popup')).toBeNull();

      await (wrapper as HTMLElement & { updateComplete: Promise<unknown> })
        .updateComplete;

      expect(
        wrapper?.shadowRoot?.querySelector('dialog'),
        'the wrapper is not backed by a native dialog',
      ).not.toBeNull();
    });

    /**
     * The rows are sized by `contextMenuStyles`, which lives in the
     * *host's* shadow root — so the only thing this component can do is
     * say which mode it is in. That attribute is the contract between
     * the two, and it is what twelve call sites get their thumb-sized
     * rows from.
     */
    it('marks the panel as a sheet, which is what sizes the rows', async () => {
      const el = await surfaceWithPanel();

      expect(
        el.querySelector('.context-menu-panel')?.hasAttribute('data-sheet'),
      ).toBe(true);
    });

    /**
     * `wa-dialog` closes itself on Escape. Without this the controller
     * would still believe the menu was open, and the *next* long-press
     * would do nothing — which is the failure mode that looks like the
     * gesture breaking rather than the dialog.
     */
    it('reports a dismissal it did not initiate', async () => {
      const el = await surfaceWithPanel();

      let dismissed = 0;

      document.addEventListener(MENU_DISMISS_EVENT, () => {
        dismissed += 1;
      });

      el.shadowRoot
        ?.querySelector('wa-dialog')
        ?.dispatchEvent(new CustomEvent('wa-hide', { bubbles: false }));

      expect(dismissed, 'no menu-dismiss reached the document').toBe(1);
    });

    /**
     * The scroll affordance (#207), and this is the mechanism again
     * rather than the symptom.
     *
     * The sheet's body has scrolled since #60 and said nothing about
     * it: measured at 424x439, eight items ended at y=470 with the
     * fold at 439, and where the cut lands on a row boundary the sheet
     * ends in a clean edge that reads as the end of the list.
     *
     * What makes the fade *conditional* — absent on a menu that fits,
     * present the moment one does not, gone again at the end of the
     * list — is `background-attachment`, not a scroll listener: a cover
     * of the sheet's own colour is painted at the end of the content
     * and attached `local`, over a shadow pinned to the box and
     * attached `scroll`. So the pair of attachments *is* the feature,
     * and it is what this asserts. The rendered result was measured in
     * the harness (dark ramp 52,58,64 flat before; 52,57,63 at the last
     * label and 22,24,27 at the bottom edge with more below; flat again
     * at the end of the list) and is on the PR.
     */
    it('paints the fade only while there is more below', async () => {
      const el = await surfaceWithPanel();

      const wrapper = el.shadowRoot?.querySelector('wa-dialog');

      await (wrapper as HTMLElement & { updateComplete: Promise<unknown> })
        .updateComplete;

      const body = wrapper?.shadowRoot?.querySelector('[part~="body"]');

      expect(body, 'no body part to scroll').not.toBeNull();

      const style = getComputedStyle(body as Element);

      expect(style.overflowY, 'the body is what gives, not the cap').toBe(
        'auto',
      );

      // The cover scrolls with the content; the shadow does not. Either
      // one alone is a fade that is always there or never there.
      expect(
        style.backgroundAttachment,
        'the cover must be local and the shadow must not',
      ).toBe('local, scroll');

      // Both sit at the bottom, or the cover hides nothing.
      expect(style.backgroundPosition).toBe('50% 100%, 50% 100%');
      expect(style.backgroundSize).toBe('100% 32px, 100% 32px');

      // The layers are shared with `bottom-nav`'s sheet since #210, and
      // the colour is what each host still says for itself: this one
      // paints the menus' `--yj-bg-elevated` (#343a40). A shared rule
      // that hard-coded one grey would draw a seam across the other
      // sheet, which is why the fragment reads a custom property.
      expect(style.backgroundImage).toMatch(
        /^linear-gradient\(rgb\(52, 58, 64\), rgb\(52, 58, 64\)\)/,
      );
    });

    /**
     * A dialog with no accessible name is what `utils/name-dialog.ts`
     * exists for; here the name is already written on the panel, so no
     * call site says it twice.
     */
    it('names the sheet after the menu it contains', async () => {
      const el = await surfaceWithPanel();

      const wrapper = el.shadowRoot?.querySelector('wa-dialog');

      expect(wrapper?.getAttribute('label')).toBe('Track actions');
    });
  });
});

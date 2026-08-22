/**
 * The phone's search surface (#57).
 *
 * Two things are asserted here that the e2e tier cannot reach, and one
 * that it deliberately must not be trusted with.
 *
 * **Which views show the trigger is `search-store`'s answer**, so this
 * walks the map rather than sampling a view: the fault the issue guards
 * against is a second list of searchable views, and a spec that checks
 * Albums checks nothing about Playlists.
 *
 * **The dialog is a `<dialog>`, not a popup.** #60 established from the
 * Web Awesome source that `wa-popup` falls back to `position: fixed`
 * without the Popover API — Chrome 113, the reference device — and that
 * `.main-panel`'s `contain: paint` clips a fixed descendant. Every tier
 * available here has the Popover API, so a popup renders perfectly in
 * CI and is clipped on the device: **an assertion that the surface is
 * not clipped passes on the broken build.** So the assertion is the
 * *mechanism* — a real `<dialog>` in the tree — which is the one form
 * of this that a browser here can answer honestly.
 *
 * The breakpoint is stubbed rather than emulated, for the reason
 * `now-playing-phone.test.ts` gives: the runner's viewport is fixed at
 * 1280x800, and the component reads `matchMedia` in `connectedCallback`
 * precisely so a test can answer it first.
 */
import { describe, expect, it, beforeEach, afterEach } from 'vitest';

import '@components/search-dialog/search-dialog';
import '@components/search-dialog/search-trigger';
import { searchStore } from '@store/search-store';
import { fixture, shadow, deepShadow } from '@test/support/render';
import { flush } from '@test/support/harness';

/** Views the store says can be searched, and what they search. */
const SEARCHABLE: [string, string][] = [
  ['tracks', 'tracks'],
  ['albums', 'albums'],
  ['artists', 'artists'],
  ['genres', 'genres'],
  ['playlists', 'playlists'],
  ['playlist-details', 'tracks in this playlist'],
  ['smart-playlist-details', 'tracks in this smart playlist'],
];

/** Views with nothing of their own to search, or a search of their own. */
const UNSEARCHABLE = ['home', 'explore', 'settings', 'downloads', 'autotag'];

let restoreMedia: (() => void) | null = null;

/** Answer the shell's phone query with `phone` until restored. */
function stubPhone(phone: boolean): void {
  const real = window.matchMedia.bind(window);

  window.matchMedia = ((q: string) =>
    q.includes('max-width: 599px')
      ? {
          matches: phone,
          media: q,
          addEventListener() {},
          removeEventListener() {},
        }
      : real(q)) as typeof window.matchMedia;

  restoreMedia = () => {
    window.matchMedia = real;
  };
}

beforeEach(() => {
  searchStore.setTerm('');
  searchStore.setCurrentView('tracks');
});

afterEach(() => {
  restoreMedia?.();
  restoreMedia = null;
  searchStore.setTerm('');
  searchStore.setCurrentView('tracks');
});

describe('<search-trigger>', () => {
  it('is offered on every view the store says can be searched', async () => {
    stubPhone(true);

    // One element, walked across the views: the trigger reads the store
    // on every render, so remounting per view would test mounting
    // rather than the condition.
    const el = await fixture('search-trigger');

    for (const [view] of SEARCHABLE) {
      searchStore.setCurrentView(view);
      await el.updateComplete;

      expect(
        shadow(el, '[data-testid="search-trigger"]'),
        `no trigger on ${view}`,
      ).not.toBeNull();
    }
  });

  it('meets the touch floor it was shipped four pixels under', async () => {
    stubPhone(true);

    // #57 created this as the phone's replacement for the header search
    // box, so it exists *only* where there is a thumb -- and it shipped
    // at 40x40 under a comment calling that "the smallest a touch
    // target should be", which was the app's own 44px floor (#56)
    // restated short rather than a second opinion about it. #186.
    const el = await fixture('search-trigger');
    const button = shadow<HTMLButtonElement>(el, '[data-testid="search-trigger"]');

    expect(button).not.toBeNull();

    const box = button!.getBoundingClientRect();

    expect(Math.round(box.width)).toBeGreaterThanOrEqual(44);
    expect(Math.round(box.height)).toBeGreaterThanOrEqual(44);
  });

  it('names what the button will search', async () => {
    stubPhone(true);

    const el = await fixture('search-trigger');

    for (const [view, scope] of SEARCHABLE) {
      searchStore.setCurrentView(view);
      await el.updateComplete;

      expect(
        shadow(el, '[data-testid="search-trigger"]')?.getAttribute(
          'aria-label',
        ),
      ).toBe(`Search ${scope}`);
    }
  });

  it('is absent where there is nothing to search', async () => {
    stubPhone(true);

    const el = await fixture('search-trigger');

    for (const view of UNSEARCHABLE) {
      searchStore.setCurrentView(view);
      await el.updateComplete;

      expect(
        shadow(el, '[data-testid="search-trigger"]'),
        `a trigger appeared on ${view}`,
      ).toBeNull();
    }
  });

  it('is absent above the phone breakpoint, where the header has a box', async () => {
    stubPhone(false);

    const el = await fixture('search-trigger');

    expect(shadow(el, '[data-testid="search-trigger"]')).toBeNull();
  });

  /**
   * A colour is not a signal on its own. The button is the only thing
   * on screen that reopens a filtered search, so the state it is in has
   * to reach someone who cannot see the accent border.
   */
  it('says in its name that a search is applied', async () => {
    stubPhone(true);

    const el = await fixture('search-trigger');

    searchStore.setTerm('aurora');
    await el.updateComplete;

    const button = shadow(el, '[data-testid="search-trigger"]');

    expect(button?.getAttribute('aria-label')).toContain('aurora');
    expect(button?.className).toContain('filtering');
  });
});

describe('<search-dialog>', () => {
  it('opens on the event the trigger dispatches, as a real dialog', async () => {
    stubPhone(true);

    const el = await fixture('search-dialog');
    const trigger = await fixture('search-trigger');

    shadow<HTMLElement>(trigger, '[data-testid="search-trigger"]')?.click();
    await flush();
    await el.updateComplete;

    expect(shadow(el, '[data-testid="search-dialog"]')).not.toBeNull();

    // The mechanism, not the appearance: a native <dialog> is what
    // reaches the top layer on Chrome 113, and a wa-popup would look
    // identical in this browser while being clipped on the device.
    expect(deepShadow(el, 'dialog')).not.toBeNull();
  });

  /**
   * It carries the real box rather than a second input, which is what
   * keeps one debounce, one clear button and one view-scoped
   * placeholder — and what keeps `search-store` the only statement of
   * what a view searches.
   */
  it('carries the header search box itself', async () => {
    const el = await fixture('search-dialog');

    document.dispatchEvent(new CustomEvent('open-search'));
    await flush();
    await el.updateComplete;

    expect(shadow(el, 'search-bar')).not.toBeNull();
  });

  /**
   * The one place the shortcut route and the button could disagree.
   * Ctrl+F on a view with nothing to search dispatches the same event
   * the button would, and the button is not there to be pressed.
   */
  it('declines to open where there is nothing to search', async () => {
    const el = await fixture('search-dialog');

    searchStore.setCurrentView('home');
    document.dispatchEvent(new CustomEvent('open-search'));
    await flush();
    await el.updateComplete;

    expect(shadow(el, '[data-testid="search-dialog"]')).toBeNull();
  });

  /**
   * Escape closes and **keeps the term**.
   *
   * `search-bar`'s own input treats Escape as "clear the search", which
   * is right in a header where the box stays on screen either way. Here
   * it would mean dismissing the surface silently discarded the search,
   * and the page behind would refill without being asked to.
   */
  it('keeps the search when it is dismissed', async () => {
    const el = await fixture('search-dialog');

    document.dispatchEvent(new CustomEvent('open-search'));
    await flush();
    await el.updateComplete;

    searchStore.setTerm('aurora');

    const input = deepShadow<HTMLInputElement>(el, 'input');

    expect(input).not.toBeNull();

    input!.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        composed: true,
      }),
    );
    await flush();
    await el.updateComplete;

    expect(searchStore.getTerm()).toBe('aurora');
    expect(shadow(el, '[data-testid="search-dialog"]')).toBeNull();
  });
});

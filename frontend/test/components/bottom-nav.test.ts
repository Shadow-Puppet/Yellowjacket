/**
 * The phone's primary navigation (plan 016 B2).
 *
 * Three of these are about the thing that makes a second nav dangerous:
 * it has to agree with the first one. `bottom-nav` emits the same
 * bubbling, composed `navigate` event `app-sidebar` does, and reads
 * which tab is lit from `activeViewStore` — the shell's one statement
 * of where the user is — so it follows a navigation from anywhere: a
 * card, a detail view, the drawer's own sidebar, or the back gesture.
 *
 * That last one is why the source is the store and not the `navigate`
 * event these tests used to dispatch. `popstate` dispatches no
 * `navigate` (index.ts calls `handleNavigate` directly), so a tab bar
 * listening for the event looked right until the user pressed back —
 * #72.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/bottom-nav/bottom-nav';
import { activeViewStore } from '@store/active-view-store';
import type { BottomNav } from '@components/bottom-nav/bottom-nav';
import { fixture, shadow, shadowAll, update } from '@test/support/render';
import { resetHarness } from '@test/support/harness';

type Nav = BottomNav;

const tabs = (el: HTMLElement) =>
  shadowAll<HTMLButtonElement>(el, 'nav button');

/** The testids of whatever the bar says is the current page. */
const current = (el: HTMLElement) =>
  tabs(el)
    .filter((b) => b.getAttribute('aria-current') === 'page')
    .map((b) => b.dataset.testid);

/** Resolve on one occurrence of an event, or reject loudly on time. */
const once = (el: Element, name: string, timeoutMs = 2000) =>
  new Promise<void>((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error(`${name} never fired`)),
      timeoutMs,
    );

    el.addEventListener(name, () => {
      clearTimeout(timer);
      resolve();
    }, { once: true });
  });

describe('bottom-nav', () => {
  beforeEach(() => {
    resetHarness();
  });

  it('offers the four phone destinations and a way to the rest', async () => {
    const el = await fixture<Nav>('bottom-nav');

    expect(tabs(el).map((b) => b.dataset.testid)).toEqual([
      'tab-home',
      'tab-albums',
      'tab-tracks',
      'tab-playlists',
      'tab-more',
    ]);
  });

  it('emits a navigate event that escapes its shadow root', async () => {
    const el = await fixture<Nav>('bottom-nav');
    const seen: string[] = [];

    document.addEventListener('navigate', (e) => {
      seen.push((e as CustomEvent<{ view: string }>).detail.view);
    });

    shadow<HTMLButtonElement>(el, '[data-testid="tab-albums"]')?.click();

    // Composed and bubbling, or index.ts's document-level listener --
    // the only thing that actually changes the view -- never hears it.
    expect(seen).toEqual(['albums']);
  });

  it('follows a navigation it did not send', async () => {
    const el = await fixture<Nav>('bottom-nav');

    activeViewStore.setView('tracks', true);
    await update(el, {});

    expect(current(el)).toEqual(['tab-tracks']);
  });

  it('marks exactly one tab current, and none for a view it has no tab for', async () => {
    const el = await fixture<Nav>('bottom-nav');

    activeViewStore.setView('settings', true);
    await update(el, {});

    // Settings lives in the drawer, so nothing in the bar is current.
    // Leaving Home highlighted would be a tab bar lying about where
    // the user is.
    expect(current(el)).toEqual([]);
  });

  it('keeps the parent tab lit while a detail view is open', async () => {
    const el = await fixture<Nav>('bottom-nav');

    activeViewStore.setView('albums', true);
    // A detail view reports itself and is not primary, so it changes
    // nothing. This is the first half of #72: the bar used to take the
    // name, match it against no tab, and light nothing at all — while
    // `app-sidebar`, which guarded on its own item list, kept the
    // highlight. Neither was deliberate and the two disagreed.
    activeViewStore.setView('explore-album-details', false);
    await update(el, {});

    expect(current(el)).toEqual(['tab-albums']);
  });

  it('follows the back path, which dispatches no navigate event', async () => {
    const el = await fixture<Nav>('bottom-nav');

    activeViewStore.setView('albums', true);
    activeViewStore.setView('tracks', true);
    await update(el, {});
    expect(current(el)).toEqual(['tab-tracks']);

    // What `popstate` does: the shell replays the entry through
    // `handleNavigate` without dispatching `navigate`. A bar listening
    // for the event stayed on Tracks — the view just left, confidently
    // wrong rather than merely blank.
    activeViewStore.setView('albums', true);
    await update(el, {});

    expect(current(el)).toEqual(['tab-albums']);
  });

  it('closes the drawer when a navigation happens', async () => {
    const el = await fixture<Nav>('bottom-nav');
    const drawer = shadow<HTMLElement & { open: boolean }>(el, 'wa-drawer');

    if (!drawer) throw new Error('no drawer');

    // The drawer animates, so the assertion is its own event rather
    // than the `open` property: setting `open = false` starts a hide
    // that has not finished on the next microtask, and a test that
    // reads the property in between sees the state it is leaving.
    const shown = once(drawer, 'wa-after-show');

    shadow<HTMLButtonElement>(el, '[data-testid="tab-more"]')?.click();
    await shown;

    const hidden = once(drawer, 'wa-after-hide');

    document.dispatchEvent(new CustomEvent('navigate', {
      detail: { view: 'settings' },
      bubbles: true,
      composed: true,
    }));

    await hidden;
    expect(drawer.open).toBe(false);
  });

  it('gives every tab a name and a target big enough to hit', async () => {
    const el = await fixture<Nav>('bottom-nav');

    for (const button of tabs(el)) {
      expect(button.textContent?.trim()).not.toBe('');
      // 48px is the floor for a touch target; the bar is the one
      // surface in this app that has no pointer to fall back on.
      expect(button.getBoundingClientRect().height).toBeGreaterThanOrEqual(48);
    }
  });

  it('holds no second sidebar until the drawer is asked for', async () => {
    const el = await fixture<Nav>('bottom-nav');

    // `app-sidebar` carries a data-testid per destination, so a spare
    // copy standing by makes every `nav-*` testid ambiguous for the
    // *whole app*: rendering it unconditionally failed 30 existing
    // specs with "strict mode violation: resolved to 2 elements", on a
    // desktop viewport where this element is not even visible.
    expect(shadow(el, 'app-sidebar')).toBeNull();
  });

  it('keeps the drawer sidebar expanded, where there is room for labels', async () => {
    const el = await fixture<Nav>('bottom-nav');

    shadow<HTMLButtonElement>(el, '[data-testid="tab-more"]')?.click();
    await update(el, {});

    // Without this the sidebar's own auto-collapse (a response to a
    // narrow *shell*) would render icons in a full-width drawer.
    expect(shadow<HTMLElement>(el, 'app-sidebar')?.hasAttribute('expanded'))
      .toBe(true);
  });
});

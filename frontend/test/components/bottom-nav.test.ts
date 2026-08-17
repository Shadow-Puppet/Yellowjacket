/**
 * The phone's primary navigation (plan 016 B2).
 *
 * Three of these are about the thing that makes a second nav dangerous:
 * it has to agree with the first one. `bottom-nav` emits the same
 * bubbling, composed `navigate` event `app-sidebar` does and listens
 * for that event globally, so a navigation from anywhere — a card, a
 * detail view, the drawer's own sidebar — moves its highlight too. A
 * tab bar that only tracks its own clicks looks right until the moment
 * the user arrives somewhere by another route.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/bottom-nav/bottom-nav';
import type { BottomNav } from '@components/bottom-nav/bottom-nav';
import { fixture, shadow, shadowAll, update } from '@test/support/render';
import { resetHarness } from '@test/support/harness';

type Nav = BottomNav;

const tabs = (el: HTMLElement) =>
  shadowAll<HTMLButtonElement>(el, 'nav button');

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

    document.dispatchEvent(new CustomEvent('navigate', {
      detail: { view: 'tracks' },
      bubbles: true,
      composed: true,
    }));
    await update(el, {});

    const current = tabs(el)
      .filter((b) => b.getAttribute('aria-current') === 'page')
      .map((b) => b.dataset.testid);

    expect(current).toEqual(['tab-tracks']);
  });

  it('marks exactly one tab current, and none for a view it has no tab for', async () => {
    const el = await fixture<Nav>('bottom-nav');

    document.dispatchEvent(new CustomEvent('navigate', {
      detail: { view: 'settings' },
      bubbles: true,
      composed: true,
    }));
    await update(el, {});

    // Settings lives in the drawer, so nothing in the bar is current.
    // Leaving Home highlighted would be a tab bar lying about where
    // the user is.
    expect(
      tabs(el).filter((b) => b.getAttribute('aria-current') === 'page'),
    ).toHaveLength(0);
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

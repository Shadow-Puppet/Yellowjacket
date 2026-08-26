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

  it('draws "More" as a sheet rising from the bottom', async () => {
    const el = await fixture<Nav>('bottom-nav');
    const drawer = shadow<HTMLElement>(el, 'wa-drawer');

    // #71. A side drawer is a desktop shape: it opened away from the
    // thumb that asked for it and drew a 200px column of a 424px
    // screen. `placement` is the whole of the change to *where* it
    // comes from, and `without-header` is what makes it the same sheet
    // `menu-surface` draws rather than a second pattern with a title
    // bar and a close button.
    expect(drawer?.getAttribute('placement')).toBe('bottom');
    expect(drawer?.hasAttribute('without-header')).toBe(true);

    // Named all the same: `nameDialog`'s documented aria-label path,
    // since without-header renders no heading to point at.
    expect(drawer?.getAttribute('label')).toBe('All views');
  });

  it('leaves exactly one scroll container, and it is the sheet body', async () => {
    const el = await fixture<Nav>('bottom-nav');
    const drawer = shadow<HTMLElement & { open: boolean }>(el, 'wa-drawer');

    if (!drawer) throw new Error('no drawer');

    const shown = once(drawer, 'wa-after-show');

    shadow<HTMLButtonElement>(el, '[data-testid="tab-more"]')?.click();
    await shown;
    await update(el, {});

    // The report is "only part of the screen scrolls under my finger",
    // and the cause is three boxes that each scroll: the dialog, its
    // body, and the sidebar's own overflow-y host. Which one a drag
    // moves depends on where the finger landed.
    const dialog = drawer.shadowRoot?.querySelector('[part~="dialog"]');
    const body = drawer.shadowRoot?.querySelector('[part~="body"]');
    const sidebar = shadow<HTMLElement>(el, 'app-sidebar');

    if (!dialog || !body || !sidebar) throw new Error('no sheet');

    expect(getComputedStyle(dialog).overflowY).toBe('hidden');
    expect(getComputedStyle(body).overflowY).toBe('auto');
    expect(getComputedStyle(sidebar).overflowY).toBe('visible');

    // And the one that does scroll keeps it to itself, or reaching the
    // end of the destinations scrolls the page behind the sheet.
    expect(getComputedStyle(body).overscrollBehaviorY).toBe('contain');
  });

  it('says where the fold is, in the sheet\'s own colour', async () => {
    const el = await fixture<Nav>('bottom-nav');
    const drawer = shadow<HTMLElement & { open: boolean }>(el, 'wa-drawer');

    if (!drawer) throw new Error('no drawer');

    const shown = once(drawer, 'wa-after-show');

    shadow<HTMLButtonElement>(el, '[data-testid="tab-more"]')?.click();
    await shown;

    const body = drawer.shadowRoot?.querySelector('[part~="body"]');

    if (!body) throw new Error('no body part to scroll');

    const style = getComputedStyle(body);

    // #210. This list does not fit the phone — measured at 424x439,
    // `scrollHeight` 412 against `clientHeight` 373 with the seed's
    // eight destinations — and said nothing about it, which where the
    // cut lands on a row boundary reads as the end of the list.
    //
    // The mechanism is #207's and is asserted the same way: the pair of
    // attachments *is* the feature. A cover of the sheet's own colour
    // painted at the end of the content (`local`) over a shadow pinned
    // to the box (`scroll`), so the fade is absent on a sheet that
    // fits, present the moment one does not, and gone again at the end.
    expect(
      style.backgroundAttachment,
      'the cover must be local and the shadow must not',
    ).toBe('local, scroll');
    expect(style.backgroundPosition).toBe('50% 100%, 50% 100%');
    expect(style.backgroundSize).toBe('100% 32px, 100% 32px');

    // And the colour is the local half of a shared rule: this sheet
    // paints the sidebar's `--yj-bg-surface` (#212529) rather than the
    // menus' elevated grey, or the fade draws the *other* sheet's
    // colour across the bottom of this one — which is the seam a
    // shared fragment would otherwise reintroduce.
    expect(style.backgroundImage).toMatch(
      /^linear-gradient\(rgb\(33, 37, 41\), rgb\(33, 37, 41\)\)/,
    );

    // And nothing paints over it. The sidebar's host carries the same
    // grey, which inside the sheet is a second opaque copy of the
    // surface drawn on top of these layers -- measured at 424x439 with
    // the rule removed, the last 32px read a flat 52,58,64 with 39px
    // still below, so the fade was painted and covered. That is
    // `.context-menu-panel[data-sheet]`'s transparency, one sheet over.
    const sidebar = shadow<HTMLElement>(el, 'app-sidebar');

    if (!sidebar) throw new Error('no sidebar');

    expect(getComputedStyle(sidebar).backgroundColor).toBe('rgba(0, 0, 0, 0)');
  });

  it('gives the sheet the whole width, which the sidebar does not take', async () => {
    const el = await fixture<Nav>('bottom-nav');

    shadow<HTMLButtonElement>(el, '[data-testid="tab-more"]')?.click();
    await update(el, {});

    const sidebar = shadow<HTMLElement>(el, 'app-sidebar');

    if (!sidebar) throw new Error('no sidebar');

    // `app-sidebar` writes an *inline* width and caps itself at 400px,
    // which beats any rule this host could write — so "the host owns
    // the box" has to be part of what `expanded` means, or the sheet
    // draws the old 200px column inside a full-width surface.
    expect(sidebar.style.width).toBe('100%');
    expect(getComputedStyle(sidebar).maxWidth).toBe('none');
  });

  it('sizes the sheet rows for a thumb, below the phone breakpoint', async () => {
    const el = await fixture<Nav>('bottom-nav');

    shadow<HTMLButtonElement>(el, '[data-testid="tab-more"]')?.click();
    await update(el, {});

    const sidebar = shadow<HTMLElement>(el, 'app-sidebar');
    const sheets = sidebar?.shadowRoot?.adoptedStyleSheets ?? [];
    const phoneRules: string[] = [];

    for (const sheet of sheets) {
      for (const rule of Array.from(sheet.cssRules)) {
        if (!(rule instanceof CSSMediaRule)) continue;

        if (!/max-width:\s*599px/.test(rule.conditionText)) continue;

        for (const inner of Array.from(rule.cssRules)) {
          phoneRules.push(inner.cssText);
        }
      }
    }

    // Asserted against the parsed stylesheet, like
    // `hover-affordance.test.ts` and for the same reason: this tier's
    // iframe is not 599px wide, so the rule cannot be *rendered* here —
    // but the regression worth catching is someone moving it out of the
    // query, which nothing on a desktop draws differently.
    expect(phoneRules.length).toBeGreaterThan(0);
    expect(phoneRules.some((r) => /min-height:\s*48px/.test(r))).toBe(true);
  });

  it('takes the resize handle out of the sheet', async () => {
    const el = await fixture<Nav>('bottom-nav');

    shadow<HTMLButtonElement>(el, '[data-testid="tab-more"]')?.click();
    await update(el, {});

    const handle = shadow<HTMLElement>(el, 'app-sidebar')
      ?.shadowRoot?.querySelector('.resize-handle');

    if (!handle) throw new Error('no resize handle');

    // A col-resize strip on the right edge of a touch surface: the
    // compatibility mouse events a tap synthesises reach its
    // `mousedown`, so it can start a resize nobody asked for.
    expect(getComputedStyle(handle).display).toBe('none');
  });
});

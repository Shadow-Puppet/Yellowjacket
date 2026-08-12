/**
 * Settings is reachable, and Downloads' tabs are tabs.
 *
 * `a11y.1` is the last Critical in the accessibility audit: every
 * `config-section` header was a bare `<div @click>` with no tabindex,
 * no role and no `aria-expanded`, and every section defaults to
 * collapsed — so every setting in the app sat behind a control that
 * could not be tabbed to. `a11y.2` is the same bug one page over, in
 * Downloads' two `<div class="tab">`s.
 *
 * Reproduced in the running app before either was fixed: seven
 * sections, seven `DIV`s, `tabindex` and `role` null on all of them.
 */
import { describe, expect, it } from 'vitest';

import '@components/config-page/config-section';
import '@components/downloads-view/downloads-view';
import { fixture, shadow, shadowAll, update } from '@test/support/render';

describe('<config-section> disclosure', () => {
  it('is a button that reports its state', async () => {
    const el = await fixture('config-section', { heading: 'Libraries' });

    const header = shadow(el, '.header');

    expect(header?.tagName).toBe('BUTTON');
    expect(header?.getAttribute('aria-expanded')).toBe('false');
  });

  it('points aria-controls at a body that exists while collapsed', async () => {
    const el = await fixture('config-section', { heading: 'Theme' });

    const id = shadow(el, '.header')?.getAttribute('aria-controls');
    const body = shadow(el, `#${id}`);

    // The body renders unconditionally and is toggled with `hidden`:
    // aria-controls has to name an element that is in the DOM, and the
    // slot's light-DOM children exist either way.
    expect(body).toBeTruthy();
    expect((body as HTMLElement).hidden).toBe(true);
  });

  it('expands on activation and says so', async () => {
    const el = await fixture('config-section', { heading: 'Theme' });

    shadow<HTMLButtonElement>(el, '.header')?.click();
    await update(el, {});

    expect(shadow(el, '.header')?.getAttribute('aria-expanded')).toBe('true');
    expect((shadow(el, '.body') as HTMLElement).hidden).toBe(false);
  });

  it('starts expanded when the host asks it to', async () => {
    const el = await fixture('config-section', { heading: 'Libraries', open: true });

    expect(shadow(el, '.header')?.getAttribute('aria-expanded')).toBe('true');
  });
});

describe('<downloads-view> tabs', () => {
  it('is a tablist of tabs owning a panel', async () => {
    const el = await fixture('downloads-view');

    const tabs = shadowAll<HTMLButtonElement>(el, '[role="tab"]');
    const panel = shadow(el, '[role="tabpanel"]');

    expect(shadow(el, '[role="tablist"]')).toBeTruthy();
    expect(tabs).toHaveLength(2);
    expect(tabs.map((t) => t.getAttribute('aria-selected'))).toEqual([
      'true',
      'false',
    ]);
    expect(tabs[0]!.getAttribute('aria-controls')).toBe(panel?.id);
  });

  it('carries a roving tab stop, not two', async () => {
    const el = await fixture('downloads-view');

    const tabs = shadowAll<HTMLButtonElement>(el, '[role="tab"]');

    expect(tabs.map((t) => t.tabIndex)).toEqual([0, -1]);
  });

  it('moves and activates on ArrowRight, wrapping', async () => {
    const el = await fixture('downloads-view');

    const tablist = shadow<HTMLElement>(el, '[role="tablist"]')!;
    tablist.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }),
    );
    await update(el, {});

    expect(
      shadowAll(el, '[role="tab"]').map((t) => t.getAttribute('aria-selected')),
    ).toEqual(['false', 'true']);
    expect(shadow(el, '[role="tabpanel"]')?.id).toBe('panel-downloads');

    tablist.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }),
    );
    await update(el, {});

    expect(shadow(el, '[role="tabpanel"]')?.id).toBe('panel-requests');
  });
});

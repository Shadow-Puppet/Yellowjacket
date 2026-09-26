/**
 * A horizontally scrolling row can be moved without a wheel.
 *
 * Until this existed the only way to see the cards past the fold on the
 * shelves, the search results and the artist page's discography was a
 * mousewheel or a trackpad gesture — which is not an affordance. A
 * mouse with no horizontal wheel simply could not reach them.
 *
 * What is asserted here is the state that makes the arrows honest: an
 * arrow is `hidden` at the end it cannot move from, because a control
 * that cannot act is worse than none, and an invisible one still holds
 * a hit area and a tab stop.
 */
import { beforeEach, describe, expect, it } from 'vitest';
import type { LitElement } from 'lit';

import '@components/scroll-row/scroll-row';
import { fixture } from '@test/support/render';

/** Six 100px cards in a 320px row — comfortably overflowing. */
function content(el: Element): void {
  for (let i = 0; i < 6; i += 1) {
    const card = document.createElement('div');

    card.style.cssText = 'flex: 0 0 100px; height: 40px';
    card.textContent = String(i);
    el.append(card);
  }
}

function arrows(el: LitElement): { prev: HTMLButtonElement; next: HTMLButtonElement } {
  const root = el.shadowRoot!;

  return {
    prev: root.querySelector('.arrow.prev') as HTMLButtonElement,
    next: root.querySelector('.arrow.next') as HTMLButtonElement,
  };
}

function viewport(el: LitElement): HTMLElement {
  return el.shadowRoot!.querySelector('.viewport') as HTMLElement;
}

async function row(): Promise<LitElement> {
  const el = await fixture<LitElement>('scroll-row');

  el.style.display = 'block';
  el.style.width = '320px';
  content(el);
  await el.updateComplete;
  // The observer reports on a later frame than a microtask drain.
  await new Promise((r) => setTimeout(r, 60));
  await el.updateComplete;

  return el;
}

describe('<scroll-row>', () => {
  beforeEach(() => {
    document.body.style.margin = '0';
  });

  it('draws an arrow for each direction it can still move', async () => {
    const el = await row();
    const { prev, next } = arrows(el);

    expect(prev).not.toBeNull();
    expect(next).not.toBeNull();

    // At the start there is nothing behind, so only the forward arrow is
    // offered.
    expect(prev.hasAttribute('hidden')).toBe(true);
    expect(next.hasAttribute('hidden')).toBe(false);
  });

  it('offers the way back once the row has moved', async () => {
    const el = await row();
    const vp = viewport(el);

    vp.scrollLeft = 120;
    vp.dispatchEvent(new Event('scroll'));
    await el.updateComplete;

    expect(arrows(el).prev.hasAttribute('hidden')).toBe(false);
  });

  it('stands the forward arrow down at the end', async () => {
    const el = await row();
    const vp = viewport(el);

    vp.scrollLeft = vp.scrollWidth;
    vp.dispatchEvent(new Event('scroll'));
    await el.updateComplete;

    expect(arrows(el).next.hasAttribute('hidden')).toBe(true);
    expect(arrows(el).prev.hasAttribute('hidden')).toBe(false);
  });

  it('moves the row when the arrow is pressed', async () => {
    const el = await row();
    const vp = viewport(el);

    expect(vp.scrollLeft).toBe(0);

    arrows(el).next.click();

    await expect.poll(() => vp.scrollLeft).toBeGreaterThan(0);
  });

  it('shows nothing to scroll when the content fits', async () => {
    const el = await fixture<LitElement>('scroll-row');

    el.style.cssText = 'display: block; width: 320px';

    const only = document.createElement('div');

    only.style.cssText = 'flex: 0 0 100px; height: 40px';
    only.textContent = 'one';
    el.append(only);
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));
    await el.updateComplete;
    await new Promise((r) => requestAnimationFrame(() => r(null)));
    await el.updateComplete;

    const { prev, next } = arrows(el);

    expect(prev.hasAttribute('hidden')).toBe(true);
    expect(next.hasAttribute('hidden')).toBe(true);
  });
});

/**
 * The global back/forward control (#6).
 *
 * The interesting half of this component is what it does when it
 * *cannot* act. The app's rule is that a control which cannot do
 * anything should not be a button at all — `library-status-indicator`
 * spent a release as a `<button>` whose handler was a comment — and
 * this is the documented exception: back and forward are a pair whose
 * positions the user learns, so the unavailable one greys out rather
 * than disappearing and moving the other one under the cursor.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/nav-history/nav-history';
import { fixture, shadow, update } from '@test/support/render';
import { historyStore } from '@store/history-store';

const back = (el: HTMLElement) =>
  shadow<HTMLButtonElement>(el, '[data-testid="history-back"]');

const forward = (el: HTMLElement) =>
  shadow<HTMLButtonElement>(el, '[data-testid="history-forward"]');

describe('nav-history', () => {
  beforeEach(() => {
    historyStore.setDepth(false, false);
  });

  it('offers both directions, named', async () => {
    const el = await fixture('nav-history');

    // The name is the whole control: two arrows side by side are
    // indistinguishable to anything not looking at them.
    expect(back(el)?.getAttribute('aria-label')).toBe('Back');
    expect(forward(el)?.getAttribute('aria-label')).toBe('Forward');
  });

  it('disables what cannot be done, in both directions independently', async () => {
    const el = await fixture('nav-history');

    expect(back(el)?.disabled).toBe(true);
    expect(forward(el)?.disabled).toBe(true);

    historyStore.setDepth(true, false);
    await update(el, {});

    expect(back(el)?.disabled).toBe(false);
    expect(forward(el)?.disabled).toBe(true);

    // Standing in the middle of the list, which is what a back press
    // followed by a look at the toolbar produces.
    historyStore.setDepth(true, true);
    await update(el, {});

    expect(back(el)?.disabled).toBe(false);
    expect(forward(el)?.disabled).toBe(false);
  });

  it('asks the shell rather than reaching for history itself', async () => {
    const el = await fixture('nav-history');
    const seen: string[] = [];

    for (const name of ['navigate-back', 'navigate-forward']) {
      document.addEventListener(name, () => seen.push(name));
    }

    historyStore.setDepth(true, true);
    await update(el, {});

    back(el)?.click();
    forward(el)?.click();

    // Composed and bubbling, or index.ts's document listener — which
    // owns the guard that stops a press at the root leaving the app —
    // never hears them. A second caller reaching for `history`
    // directly is how the old `navStack` came to disagree with the
    // platform.
    expect(seen).toEqual(['navigate-back', 'navigate-forward']);
  });

  it('says nothing when it cannot act', async () => {
    const el = await fixture('nav-history');
    const seen: string[] = [];

    document.addEventListener('navigate-back', () => seen.push('back'));

    back(el)?.click();

    expect(seen).toEqual([]);
  });
});

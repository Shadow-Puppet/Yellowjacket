/**
 * `a11y.14` — the combobox announces the option it has moved to.
 *
 * Reproduced in the running app first, on the smart-playlist rule
 * editor, and confirmed against the browser's own computation rather
 * than a snapshot: `Accessibility.getFullAXTree` reported no
 * `activedescendant` and no `controls` on any of the five comboboxes on
 * the page, so arrowing through nineteen options moved a visual
 * highlight and told a screen reader nothing.
 *
 * The IDREF cases assert the *link*, not the attribute: an
 * `aria-activedescendant` naming an id that no element carries is
 * exactly as silent as no attribute at all, and reads as fixed.
 */
import { describe, expect, it } from 'vitest';

import '@components/combobox/combobox';
import type { YjCombobox } from '@components/combobox/combobox';
import { fixture, shadow, shadowAll } from '@test/support/render';

const OPTIONS = ['Artist', 'Album', 'Genre', 'Year'];

type Combobox = YjCombobox;

async function open(value = ''): Promise<Combobox> {
  const el = await fixture<Combobox>('yj-combobox', {
    options: OPTIONS,
    value,
  });

  shadow<HTMLInputElement>(el, 'input')?.focus();
  await el.updateComplete;

  return el;
}

async function arrowDown(el: Combobox, times = 1): Promise<void> {
  for (let i = 0; i < times; i++) {
    shadow<HTMLInputElement>(el, 'input')?.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }),
    );
    await el.updateComplete;
  }
}

describe('<yj-combobox> ARIA', () => {
  it('points aria-controls at the listbox it opens', async () => {
    const el = await open();

    const controls = shadow(el, 'input')?.getAttribute('aria-controls');

    expect(shadow(el, `ul#${controls}`)).not.toBeNull();
  });

  it('names the highlighted option, and that option exists', async () => {
    const el = await open();

    await arrowDown(el, 2);

    const active = shadow(el, 'input')?.getAttribute('aria-activedescendant');
    const target = active ? shadow(el, `#${active}`) : null;

    expect([target?.getAttribute('role'), target?.textContent?.trim()]).toEqual(
      ['option', 'Album'],
    );
  });

  it('moves the pointer as the highlight moves', async () => {
    const el = await open();

    await arrowDown(el);
    const first = shadow(el, 'input')?.getAttribute('aria-activedescendant');

    await arrowDown(el);
    const second = shadow(el, 'input')?.getAttribute('aria-activedescendant');

    expect(first).not.toBe(second);
  });

  it('carries no pointer while nothing is highlighted', async () => {
    const el = await open();

    expect(shadow(el, 'input')?.hasAttribute('aria-activedescendant')).toBe(
      false,
    );
  });

  // aria-selected means *chosen*. It used to mean "highlighted", which
  // is the one thing it does not mean — so a user arrowing past an
  // option heard it announced as selected when it was not, and the
  // value they had actually chosen was announced as unselected.
  it('marks the chosen value as selected, not the highlighted one', async () => {
    const el = await open('Genre');

    await arrowDown(el);

    const selected = shadowAll(el, 'li')
      .filter((li) => li.getAttribute('aria-selected') === 'true')
      .map((li) => li.textContent?.trim());

    expect(selected).toEqual(['Genre']);
  });
});

/**
 * Every form control in Settings had a visible label and no name.
 *
 * Not in the audit, and `a11y.6` says why in its own text: it scanned
 * every `<button>`, and a `<select>` is not one. Measured with
 * `Accessibility.getFullAXTree` against the running app with all seven
 * sections expanded — **24 of 93 controls unnamed**: every
 * `config-field` select and toggle, and all eighteen track-list column
 * checkboxes. In each case a `<label>` sat right beside the control
 * with nothing associating the two.
 *
 * Three of the fixes here are about a name that *exists* and does not
 * identify anything, which is `a11y.32`'s complaint one page over:
 * three shortcut buttons announced themselves as "S", thirty-six column
 * arrows as "Move up" or "Move down", and — `a11y.26`, the audit's own
 * finding — Explore's search box by a placeholder that disappears the
 * moment anyone types into it. That last one is why the finding
 * survived four phases: a placeholder is an accname fallback, so the
 * AX sweep reported the view clean.
 */
import { describe, expect, it } from 'vitest';

import '@components/config-page/config-field';
import '@components/config-page/shortcut-capture';
import '@components/explore-view/explore-view';
import { fixture, shadow } from '@test/support/render';

/** What the `<label>` in this shadow root actually points at. */
function labelledControl(host: Element): Element | null {
  const forId = shadow(host, 'label[for]')?.getAttribute('for');

  return forId ? shadow(host, `#${CSS.escape(forId)}`) : null;
}

describe('a config field names its control', () => {
  it('associates the label with a select', async () => {
    const el = await fixture('config-field', {
      schema: {
        key: 'theme.shade',
        label: 'Shade',
        type: 'select',
        options: [{ value: 'dark', label: 'Dark' }],
      },
      value: 'dark',
    });

    expect(labelledControl(el)?.tagName).toBe('SELECT');
  });

  it('associates the label with a toggle, which is a second label deep', async () => {
    const el = await fixture('config-field', {
      schema: { key: 'x', label: 'Scan on startup', type: 'toggle' },
      value: true,
    });

    // The checkbox is wrapped in a *styling* label carrying only a
    // span, which contributes no text and so named nothing.
    const control = labelledControl(el) as HTMLInputElement | null;

    expect(control?.type).toBe('checkbox');
  });

  it.each(['text', 'number', 'color', 'directory'] as const)(
    'associates the label with a %s field',
    async (type) => {
      const el = await fixture('config-field', {
        schema: { key: 'k', label: 'Music folder', type },
        value: '',
      });

      expect(labelledControl(el)).toBeTruthy();
    },
  );

  it('says which field a Browse button browses for', async () => {
    const el = await fixture('config-field', {
      schema: { key: 'k', label: 'Music folder', type: 'directory' },
      value: '',
    });

    expect(shadow(el, 'button')?.getAttribute('aria-label'))
      .toBe('Browse for Music folder');
  });
});

describe('a shortcut button says what it binds', () => {
  it('names the action, and keeps the key as the value', async () => {
    const el = await fixture('shortcut-capture', {
      action: 'player.playPause',
      label: 'Play / Pause',
      currentKey: 'Space',
    });

    expect(shadow(el, 'button')?.getAttribute('aria-label'))
      .toBe('Play / Pause shortcut: Space');
    expect(shadow(el, 'button')?.textContent?.trim()).toBe('Space');
  });

  it('says so when there is no key rather than announcing nothing', async () => {
    const el = await fixture('shortcut-capture', {
      action: 'tracklist.delete',
      label: 'Delete Track',
      currentKey: '',
    });

    expect(shadow(el, 'button')?.getAttribute('aria-label'))
      .toBe('Delete Track shortcut: not set');
  });

  it('names the action in the reset button too', async () => {
    const el = await fixture('shortcut-capture', {
      action: 'player.next',
      label: 'Next Track',
      currentKey: 'X',
      defaultKey: 'N',
    });

    expect(shadow(el, '.reset-btn')?.getAttribute('aria-label'))
      .toBe('Reset Next Track to N');
  });
});

describe('a search box is labelled by more than its placeholder', () => {
  it('names the catalog search, which loses its placeholder on typing', async () => {
    const el = await fixture('explore-view');

    await el.updateComplete;

    // `a11y.26`. A placeholder *is* an accname fallback, so this box
    // was never unnamed and the AX sweep reported the view clean —
    // which is why the finding survived four phases. It is a weak name:
    // it disappears the moment the user types, and it is the only
    // thing distinguishing catalog search from lyric search.
    const input = shadow(el, '.search-container input');

    expect(input?.getAttribute('aria-label')).toBe('Search the catalog');
  });
});

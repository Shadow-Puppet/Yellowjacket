/**
 * The theme store is the only store that writes to the document: it
 * derives a whole custom-property ramp from two settings and applies it
 * to :root, where every shadow root inherits it. Asserting on the
 * computed values of the real document is the point of running in a
 * browser at all.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import { themeStore } from '@store/theme-store';
import { Events } from '../../src/events';
import { emit, lastArgs } from '@test/support/harness';

/** Push a theme, as the backend's config change event does. */
function applyTheme(AccentColor: string, BackgroundShade: string): void {
  emit(Events.ThemeConfigChanged, { AccentColor, BackgroundShade });
}

function cssVar(name: string): string {
  return document.documentElement.style.getPropertyValue(name).trim();
}

describe('theme store: derived variables', () => {
  beforeEach(() => {
    applyTheme('#ffd43b', 'dark');
  });

  it('caches the pushed theme', () => {
    expect(themeStore.getState()).toEqual({
      accentColor: '#ffd43b',
      backgroundShade: 'dark',
    });
  });

  it('sets the accent verbatim', () => {
    expect(cssVar('--yj-accent')).toBe('#ffd43b');
  });

  it('derives a lighter hover accent and a darker muted one', () => {
    expect([cssVar('--yj-accent-hover'), cssVar('--yj-accent-muted')]).toEqual([
      '#ffda58',
      '#806a1e',
    ]);
  });

  it('derives translucent accent backgrounds as rgba, for layering', () => {
    expect(cssVar('--yj-accent-bg')).toBe('rgba(255, 212, 59, 0.1)');
  });

  it('expands a three-digit hex before deriving from it', () => {
    applyTheme('#fff', 'dark');

    expect(cssVar('--yj-accent-hover')).toBe('#ffffff');
  });

  it('swaps the whole background ramp with the shade', () => {
    applyTheme('#ffd43b', 'darker');
    const darker = cssVar('--yj-bg-surface');

    applyTheme('#ffd43b', 'light');

    expect([darker, cssVar('--yj-bg-surface')]).toEqual(['#121212', '#f8f9fa']);
  });

  it('keeps semantic colours fixed across shades', () => {
    const dark = cssVar('--yj-error');

    applyTheme('#ffd43b', 'light');

    expect([dark, cssVar('--yj-error')]).toEqual(['#e03131', '#e03131']);
  });
});

describe('theme store: document integration', () => {
  it('flags dark shades to Web Awesome, which otherwise renders white surfaces', () => {
    applyTheme('#ffd43b', 'dark');
    const darkFlagged = document.documentElement.classList.contains('wa-dark');

    applyTheme('#ffd43b', 'light');

    expect([
      darkFlagged,
      document.documentElement.classList.contains('wa-dark'),
    ]).toEqual([true, false]);
  });

  it('sets color-scheme so native controls and scrollbars match', () => {
    applyTheme('#ffd43b', 'light');
    const light = document.documentElement.style.colorScheme;

    applyTheme('#ffd43b', 'darker');

    expect([light, document.documentElement.style.colorScheme]).toEqual([
      'light',
      'dark',
    ]);
  });

  it('bridges the surface ramp onto Web Awesome custom properties', () => {
    applyTheme('#ffd43b', 'darker');

    expect([
      cssVar('--wa-color-surface-default'),
      cssVar('--wa-color-surface-raised'),
      cssVar('--wa-color-surface-lowered'),
    ]).toEqual(['#000000', '#121212', '#1e1e1e']);
  });

  it('is inherited through a shadow root', () => {
    applyTheme('#ff0000', 'dark');

    const host = document.createElement('div');
    const root = host.attachShadow({ mode: 'open' });
    const inner = document.createElement('span');

    inner.style.color = 'var(--yj-accent)';
    root.append(inner);
    document.body.append(host);

    const colour = getComputedStyle(inner).color;

    host.remove();

    expect(colour).toBe('rgb(255, 0, 0)');
  });
});

describe('theme store: writes', () => {
  it('sends a new accent to the backend rather than applying it locally', async () => {
    applyTheme('#ffd43b', 'dark');
    await themeStore.setAccentColor('#00ff00');

    // The backend is the writer; the store waits for ThemeConfigChanged.
    expect([
      lastArgs('config.Config.SetThemeAccentColor'),
      themeStore.getState().accentColor,
    ]).toEqual([['#00ff00'], '#ffd43b']);
  });

  it('sends a new shade to the backend', async () => {
    await themeStore.setBackgroundShade('light');

    expect(lastArgs('config.Config.SetThemeBackgroundShade')).toEqual([
      'light',
    ]);
  });
});

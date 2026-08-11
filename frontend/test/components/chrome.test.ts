/**
 * The app chrome: sidebar navigation, the library filter dropdown and
 * the tri-state library status indicator. All three are small, all
 * three are on screen constantly, and the sidebar's testids and
 * aria-current are what the e2e tier navigates by.
 */
import { describe, expect, it, beforeEach, vi, afterEach } from 'vitest';

import '@components/sidebar/app-sidebar';
import '@components/library-filter/library-filter';
import '@components/library-status-indicator/library-status-indicator';
import { Events } from '../../src/events';
import { emit, stub, flush, calls, lastArgs } from '@test/support/harness';
import {
  fixture,
  shadow,
  shadowAll,
  texts,
  update,
  visual,
} from '@test/support/render';

describe('<app-sidebar>', () => {
  it('renders a testid per destination, which is how e2e navigates', async () => {
    const el = await fixture('app-sidebar');

    expect(
      shadowAll(el, 'li').map((li) => li.getAttribute('data-testid')),
    ).toEqual([
      'nav-home',
      'nav-playlists',
      'nav-artists',
      'nav-genres',
      'nav-albums',
      'nav-tracks',
      'nav-explore',
      'nav-downloads',
      'nav-autotag',
      'nav-jobs',
      'nav-settings',
    ]);
  });

  it('marks exactly one item as the current page', async () => {
    const el = await fixture('app-sidebar');

    const current = shadowAll(el, 'li').filter(
      (li) => li.getAttribute('aria-current') === 'page',
    );

    expect(current).toHaveLength(1);
  });

  it('announces a navigation as a composed event, so the shell hears it through the shadow root', async () => {
    const el = await fixture('app-sidebar');
    const seen: string[] = [];

    document.addEventListener('navigate', (e) => {
      seen.push((e as CustomEvent<{ view: string }>).detail.view);
    }, { once: true });

    shadow<HTMLElement>(el, '[data-testid="nav-artists"]')?.click();
    await el.updateComplete;

    expect(seen).toEqual(['artists']);
  });

  it('moves aria-current to the clicked destination', async () => {
    const el = await fixture('app-sidebar');

    shadow<HTMLElement>(el, '[data-testid="nav-genres"]')?.click();
    await el.updateComplete;

    expect(
      shadow(el, '[data-testid="nav-genres"]')?.getAttribute('aria-current'),
    ).toBe('page');
  });

  it('looks the way it did last time', async () => {
    const el = await fixture('app-sidebar');

    await visual(el, 'app-sidebar');
    expect(shadowAll(el, 'li').length).toBeGreaterThan(0);
  });
});

describe('<library-filter>', () => {
  beforeEach(() => {
    stub('library.Library.GetAllLibrariesWithTrackCounts', [
      { id: 7, name: 'Music' },
      { id: 8, name: 'Field Recordings' },
    ]);
  });

  it('offers every library plus the merged view', async () => {
    const el = await fixture('library-filter');

    await flush();
    await el.updateComplete;

    expect(texts(el, 'option')).toEqual([
      'All Libraries',
      'Music',
      'Field Recordings',
    ]);
  });

  it('carries an accessible name — it is a bare select otherwise', async () => {
    const el = await fixture('library-filter');

    expect(shadow(el, 'select')?.getAttribute('aria-label')).toBe(
      'Library filter',
    );
  });

  it('selects a library by id, and the merged view by empty string', async () => {
    const el = await fixture('library-filter');

    await flush();
    await el.updateComplete;

    const select = shadow<HTMLSelectElement>(el, 'select');

    if (select) select.value = '8';

    select?.dispatchEvent(new Event('change'));
    await flush();

    expect(lastArgs('library.Library.GetAllTracksByLibrary')).toEqual([8]);
  });

  it('picks up a library added while it was on screen', async () => {
    const el = await fixture('library-filter');

    await flush();
    await el.updateComplete;

    stub('library.Library.GetAllLibrariesWithTrackCounts', [
      { id: 7, name: 'Music' },
      { id: 8, name: 'Field Recordings' },
      { id: 9, name: 'Podcasts' },
    ]);
    emit(Events.LibraryAdded, { id: 9 });
    await flush();
    await el.updateComplete;

    expect(texts(el, 'option')).toContain('Podcasts');
  });
});

describe('<library-status-indicator>', () => {
  it('defaults to "not in library"', async () => {
    const el = await fixture('library-status-indicator');

    expect(shadow(el, 'wa-icon')?.getAttribute('name')).toBe('plus');
  });

  it('uses a distinct glyph per state', async () => {
    const glyphs: (string | null | undefined)[] = [];

    for (const status of ['in-library', 'queued', 'not-in-library']) {
      const el = await fixture('library-status-indicator', { status });

      glyphs.push(shadow(el, 'wa-icon')?.getAttribute('name'));
    }

    expect(glyphs).toEqual(['check', 'hourglass-half', 'plus']);
  });

  it('phrases its label around the entity it describes', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'in-library',
      entityType: 'album',
      label: 'Abbey Road',
    });

    expect(shadow(el, 'button')?.getAttribute('aria-label')).toBe(
      'Album "Abbey Road" is in your library',
    );
  });

  it('phrases an unowned entity as an invitation', async () => {
    const el = await fixture('library-status-indicator', {
      entityType: 'artist',
      label: 'Eno',
    });

    expect(shadow(el, 'button')?.getAttribute('aria-label')).toBe(
      'Add artist "Eno" to library',
    );
  });

  it('drops the quoted name when it has none', async () => {
    const el = await fixture('library-status-indicator', { status: 'queued' });

    expect(shadow(el, 'button')?.getAttribute('aria-label')).toBe(
      'Track is queued for download',
    );
  });

  it('mirrors the label into the tooltip', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'in-library',
    });

    const button = shadow(el, 'button');

    expect(button?.getAttribute('title')).toBe(
      button?.getAttribute('aria-label'),
    );
  });

  it('swallows the click, so it does not navigate the card it sits on', async () => {
    const el = await fixture('library-status-indicator');
    let bubbled = 0;

    el.addEventListener('click', () => {
      bubbled += 1;
    });

    shadow<HTMLElement>(el, 'button')?.click();

    expect([bubbled, calls()]).toEqual([0, []]);
  });

  it('swallows Enter and Space for the same reason', async () => {
    const el = await fixture('library-status-indicator');
    let bubbled = 0;

    el.addEventListener('keydown', () => {
      bubbled += 1;
    });

    for (const key of ['Enter', ' ', 'Tab']) {
      shadow(el, 'button')?.dispatchEvent(
        new KeyboardEvent('keydown', { key, bubbles: true, composed: true }),
      );
    }

    // Tab still gets through: it is navigation, not activation.
    expect(bubbled).toBe(1);
  });

  it('honours a non-default size', async () => {
    const el = await fixture('library-status-indicator', { size: 32 });

    expect(el.style.getPropertyValue('--indicator-size')).toBe('32px');
  });

  it('looks the way it did last time', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'in-library',
    });

    await update(el, { size: 40 });
    await visual(el, 'library-status-indicator-in-library');
    expect(shadow(el, 'button')).not.toBeNull();
  });
});

describe('<library-filter> resilience', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the merged view even when the library list cannot be loaded', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    stub('library.Library.GetAllLibrariesWithTrackCounts', () => {
      throw new Error('database locked');
    });
    // The store caches the library list; drop it so the failing stub is
    // actually reached.
    emit(Events.LibraryRemoved, { id: 7 });

    const el = await fixture('library-filter');

    await flush();
    await el.updateComplete;

    expect(texts(el, 'option')).toEqual(['All Libraries']);
  });
});

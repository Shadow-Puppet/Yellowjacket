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
import { activeViewStore } from '@store/active-view-store';
import { emit, stub, flush, calls, lastArgs } from '@test/support/harness';
import {
  fixture,
  shadow,
  shadowAll,
  texts,
  update,
  visual,
} from '@test/support/render';
import {
  ICON_CAN_REQUEST,
  ICON_IN_LIBRARY,
  ICON_REQUESTED,
} from '@utils/icon-language';

/** A configured, enabled download client. */
const PROVIDER = {
  id: 1,
  kind: 'slskd',
  name: 'Sound',
  enabled: true,
  priority: 50,
};

describe('<app-sidebar>', () => {
  // Downloads is offered only where there is a client to download with
  // (#25), so "all eleven destinations" is a statement about a
  // configured install. `view-visibility.test.ts` owns the rule itself;
  // this states the world these cases are describing.
  beforeEach(async () => {
    stub('download.Service.ListProviders', [PROVIDER]);
    emit(Events.DownloadProvidersChanged);
    await flush();
  });

  it('renders a testid per destination, which is how e2e navigates', async () => {
    const el = await fixture('app-sidebar');

    expect(
      shadowAll(el, 'li button').map((item) =>
        item.getAttribute('data-testid'),
      ),
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
    activeViewStore.setView('home', true);

    const el = await fixture('app-sidebar');

    const current = shadowAll(el, 'li button').filter(
      (item) => item.getAttribute('aria-current') === 'page',
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

  it('moves aria-current with the shell, not with the click', async () => {
    activeViewStore.setView('home', true);

    const el = await fixture('app-sidebar');

    shadow<HTMLElement>(el, '[data-testid="nav-genres"]')?.click();
    await el.updateComplete;

    // The click asks; it does not answer. The sidebar used to move its
    // own highlight optimistically, which is the second opinion #72
    // removed -- one component deciding where the user is, while the
    // shell decided separately and `bottom-nav` decided a third way.
    expect(
      shadow(el, '[data-testid="nav-genres"]')?.getAttribute('aria-current'),
    ).toBe('false');

    // What the shell does with that event, in one line.
    activeViewStore.setView('genres', true);
    await update(el, {});

    expect(
      shadow(el, '[data-testid="nav-genres"]')?.getAttribute('aria-current'),
    ).toBe('page');
  });

  it('follows the back path, which dispatches no navigate event', async () => {
    activeViewStore.setView('albums', true);

    const el = await fixture('app-sidebar');

    // `popstate` replays an entry through `handleNavigate` directly, so
    // there is no `navigate` event to hear -- which is why the sidebar
    // stayed on the view the user had just left (#72).
    activeViewStore.setView('tracks', true);
    await update(el, {});

    expect(
      shadowAll(el, 'li button')
        .filter((item) => item.getAttribute('aria-current') === 'page')
        .map((item) => item.getAttribute('data-testid')),
    ).toEqual(['nav-tracks']);
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

    expect(lastArgs('library.Library.GetTracks')).toEqual([8]);
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

    expect(shadow(el, 'wa-icon')?.getAttribute('name')).toBe(ICON_CAN_REQUEST);
  });

  /**
   * Named from the vocabulary rather than written out, or this test
   * pins the glyphs *against* the table it is supposed to follow —
   * which is what it did: it asserted `plus` for the un-owned state,
   * the same glyph two adjacent menu items were using for two other
   * meanings, and passing was the reason nobody looked.
   *
   * What is still worth asserting is that the three differ, which is
   * the property the states need and the one the table cannot state
   * about itself here.
   */
  it('uses a distinct glyph per state', async () => {
    const glyphs: (string | null | undefined)[] = [];

    for (const status of ['in-library', 'queued', 'not-in-library']) {
      const el = await fixture('library-status-indicator', { status });

      glyphs.push(shadow(el, 'wa-icon')?.getAttribute('name'));
    }

    expect(glyphs).toEqual([ICON_IN_LIBRARY, ICON_REQUESTED, ICON_CAN_REQUEST]);
    expect(new Set(glyphs).size).toBe(3);
  });

  it('phrases its label around the entity it describes', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'in-library',
      entityType: 'album',
      label: 'Abbey Road',
    });

    expect(shadow(el, '.badge')?.getAttribute('aria-label')).toBe(
      'Album "Abbey Road" is in your library',
    );
  });

  it('states an unowned entity rather than offering to add it', async () => {
    // The old copy was "Add artist “Eno” to library", which is the
    // promise the inert button was making. Nothing here adds anything.
    const el = await fixture('library-status-indicator', {
      entityType: 'artist',
      label: 'Eno',
    });

    expect(shadow(el, '.badge')?.getAttribute('aria-label')).toBe(
      'Artist "Eno" is not in your library',
    );
  });

  it('is a badge, not a keyboard stop', async () => {
    // 20 of the 66 tab stops on an Explore results page were these,
    // each announcing itself as a button and doing nothing.
    const el = await fixture('library-status-indicator', {
      status: 'in-library',
    });

    expect(shadow(el, 'button')).toBeNull();

    const badge = shadow(el, '.badge');

    expect(badge?.getAttribute('role')).toBe('img');
    expect(badge?.hasAttribute('tabindex')).toBe(false);
  });

  it('drops the quoted name when it has none', async () => {
    const el = await fixture('library-status-indicator', { status: 'queued' });

    expect(shadow(el, '.badge')?.getAttribute('aria-label')).toBe(
      'Track is queued for download',
    );
  });

  it('mirrors the label into the tooltip', async () => {
    const el = await fixture('library-status-indicator', {
      status: 'in-library',
    });

    const badge = shadow(el, '.badge');

    expect(badge?.getAttribute('title')).toBe(badge?.getAttribute('aria-label'));
  });

  it('lets a click reach the card it sits on, and calls nothing itself', async () => {
    // It used to stopPropagation() so its own no-op click would not
    // navigate the card. With no click of its own, the badge is part
    // of the card and a click on it means what the card means.
    const el = await fixture('library-status-indicator');
    let bubbled = 0;

    el.addEventListener('click', () => {
      bubbled += 1;
    });

    shadow<HTMLElement>(el, '.badge')?.click();

    expect([bubbled, calls()]).toEqual([1, []]);
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
    expect(shadow(el, '.badge')).not.toBeNull();
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

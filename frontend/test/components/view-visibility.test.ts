/**
 * Which destinations the navigation offers (#25).
 *
 * Eleven sidebar entries is more than most libraries need, so they are
 * individually toggleable. The assertions here are about the **nav**
 * and not about the setting being saved: "the config was written" is
 * the plumbing, and a spec that measures the plumbing is how #69 and
 * #72 both shipped green on a broken build.
 *
 * Two singletons make ordering matter, and both are driven the way the
 * app drives them rather than reset: `GeneralConfigChanged` is what the
 * backend emits when a toggle is saved, and `DownloadProvidersChanged`
 * is what it emits when a client is configured. So each case states the
 * world it wants and is independent of which one ran first.
 */
import { describe, expect, it, beforeEach } from 'vitest';

import '@components/sidebar/app-sidebar';
import '@components/bottom-nav/bottom-nav';
import { activeViewStore } from '@store/active-view-store';
import { stub, emit, flush, resetHarness } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadowAll } from '@test/support/render';
import type { LitElement } from 'lit';

const PROVIDER = {
  id: 1,
  kind: 'slskd',
  name: 'Sound',
  enabled: true,
  priority: 50,
};

/** Every view id the sidebar is currently drawing, in order. */
const navIDs = (el: HTMLElement) =>
  shadowAll<HTMLButtonElement>(el, 'nav button')
    .map((b) => b.dataset.testid?.replace(/^nav-/, ''))
    .filter((id): id is string => id !== undefined);

const tabIDs = (el: HTMLElement) =>
  shadowAll<HTMLButtonElement>(el, 'nav button')
    .map((b) => b.dataset.testid?.replace(/^tab-/, ''))
    .filter((id): id is string => id !== undefined);

/**
 * State the backend's resolved answer and push the event that says it
 * changed. The map is *resolved* — every known view, defaults already
 * applied — because that is what the binding returns and the whole
 * reason the frontend holds no copy of the defaults.
 */
async function setViews(views: Record<string, boolean>): Promise<void> {
  stub('config.Config.GetViewVisibility', views);
  emit(Events.GeneralConfigChanged, {});
  await flush();
  await flush();
}

async function setClientConfigured(configured: boolean): Promise<void> {
  stub('download.Service.ListProviders', configured ? [PROVIDER] : []);
  emit(Events.DownloadProvidersChanged);
  await flush();
  await flush();
}

const ALL_VISIBLE = {
  home: true,
  playlists: true,
  artists: true,
  genres: true,
  albums: true,
  tracks: true,
  explore: true,
  downloads: true,
  autotag: true,
  jobs: true,
  settings: true,
};

describe('view visibility', () => {
  beforeEach(async () => {
    resetHarness();
    await setViews(ALL_VISIBLE);
    await setClientConfigured(true);
  });

  it('draws every destination the config keeps', async () => {
    const el = await fixture<LitElement>('app-sidebar');

    expect(navIDs(el)).toEqual([
      'home',
      'playlists',
      'artists',
      'genres',
      'albums',
      'tracks',
      'explore',
      'downloads',
      'autotag',
      'jobs',
      'settings',
    ]);
  });

  it('drops the ones the user switched off', async () => {
    const el = await fixture<LitElement>('app-sidebar');

    await setViews({ ...ALL_VISIBLE, autotag: false, jobs: false });
    await el.updateComplete;

    expect(navIDs(el)).not.toContain('autotag');
    expect(navIDs(el)).not.toContain('jobs');
    expect(navIDs(el)).toContain('settings');
  });

  /**
   * Hiding is about the nav item, not about the view. Detail views
   * navigate into these and the launch page is one of them, so the
   * shell's own statement of where the user is has to survive a
   * destination that draws no item — and it does so with no special
   * case here, because #72 moved the highlight onto `active-view-store`
   * and this only filters what is rendered.
   */
  it('lights nothing when the active view is a hidden one', async () => {
    const el = await fixture<LitElement>('app-sidebar');

    await setViews({ ...ALL_VISIBLE, autotag: false });
    activeViewStore.setView('autotag', true);
    await el.updateComplete;

    const lit = shadowAll<HTMLButtonElement>(el, 'nav button')
      .filter((b) => b.getAttribute('aria-current') === 'page');

    expect(lit).toHaveLength(0);
    expect(navIDs(el)).not.toContain('autotag');

    activeViewStore.setView('albums', true);
  });

  /**
   * A destination for a feature that cannot work is worse than an
   * absent one, so Downloads asks the download client rather than the
   * config — and it appears when one is configured, without a restart
   * (#37's rule, one surface over).
   */
  it('hides Downloads until a client is configured', async () => {
    const el = await fixture<LitElement>('app-sidebar');

    await setClientConfigured(false);
    await el.updateComplete;

    expect(navIDs(el)).not.toContain('downloads');

    await setClientConfigured(true);
    await el.updateComplete;

    expect(navIDs(el)).toContain('downloads');
  });

  /**
   * The tab bar honours the toggles too, and the reason is local: its
   * "More" drawer opens the same `<app-sidebar>`, which filters. An
   * unfiltered bar would contradict its own drawer one tap away.
   */
  it('drops a hidden destination from the phone tab bar', async () => {
    const el = await fixture<LitElement>('bottom-nav');

    expect(tabIDs(el)).toEqual(['home', 'albums', 'tracks', 'playlists', 'more']);

    await setViews({ ...ALL_VISIBLE, albums: false });
    await el.updateComplete;

    expect(tabIDs(el)).toEqual(['home', 'tracks', 'playlists', 'more']);
  });

  /** "More" is not a destination and is never filtered away: it is how
   *  everything else is still reachable. */
  it('keeps More when every tab is hidden', async () => {
    const el = await fixture<LitElement>('bottom-nav');

    await setViews({
      ...ALL_VISIBLE,
      home: false,
      albums: false,
      tracks: false,
      playlists: false,
    });
    await el.updateComplete;

    expect(tabIDs(el)).toEqual(['more']);
  });
});

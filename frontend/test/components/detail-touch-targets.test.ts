/**
 * The controls #186's second table found, outside Settings.
 *
 * Six one-off controls across four surfaces, and the reason they are a
 * test rather than six stylesheet edits is `back-button`. The issue
 * names it in `artist-details` at **32x32**; it is the same
 * declaration, byte-identical, in *six* components — because a device
 * sweep walks the views somebody thought to open, and five of them
 * were not opened.
 *
 * So the assertion is over the whole set rather than over the one that
 * was measured. That is `icon-language.test.ts`'s shape and it is here
 * for the same reason: checking one call site checks one call site.
 *
 * | control | before | where |
 * |---|---|---|
 * | `.folders-menu-trigger` | **32x18** | autotag |
 * | `.section-toggle` | 187x**15** | autotag |
 * | `.back-button` | 32x32 | six detail views |
 * | Requests / Downloads tabs | 85x**34**, 96x**34** | downloads |
 * | `.search-mode-tab` | 89x**26**, 79x**26** | explore |
 * | explore search input | 325x**18** in a 36px box | explore |
 *
 * `page-action-check-now` (113x29) is in that table and is **not**
 * here: it is a `PageAction`, so #195 raised it with the rest of the
 * page header's actions, and `touch-targets.test.ts` already covers
 * it. Re-asserting it here would be a second statement of one rule.
 */
import { beforeEach, describe, expect, it } from 'vitest';

import '@components/artist-details/artist-details';
import '@components/autotag-view/autotag-view';
import '@components/downloads-view/downloads-view';
import '@components/explore-album-details/explore-album-details';
import '@components/explore-artist-details/explore-artist-details';
import '@components/explore-view/explore-view';
import '@components/genre-details/genre-details';
import '@components/playlist-details/playlist-details';
import '@components/smart-playlist-details/smart-playlist-details';

import { flush, stub } from '@test/support/harness';
import { fixture, shadow, shadowAll } from '@test/support/render';

/** The app's touch floor, from #56. */
const FLOOR = 44;

/**
 * Every component that draws a back button.
 *
 * The list is here rather than derived because deriving it means
 * reading the source, and this tier renders instead — but it is
 * checked against the source by `the back button is one declaration`
 * below, so a seventh view cannot join quietly.
 */
const BACK_BUTTON_VIEWS = [
  'artist-details',
  'genre-details',
  'playlist-details',
  'smart-playlist-details',
  'explore-artist-details',
  'explore-album-details',
] as const;

function boxOf(el: Element | null | undefined): { w: number; h: number } {
  if (!el) return { w: 0, h: 0 };

  const box = el.getBoundingClientRect();

  return { w: Math.round(box.width), h: Math.round(box.height) };
}

describe('the way out of a detail view', () => {
  beforeEach(() => {
    for (const path of [
      'library.Library.GetTracks',
      'library.Library.GetAlbums',
      'library.Library.GetArtists',
      'library.Library.GetGenres',
      'playlist.Service.GetAllPlaylists',
      'playlist.Service.GetAllPlaylistsWithTracks',
    ]) {
      stub(path, []);
    }
  });

  it.each(BACK_BUTTON_VIEWS)('is 44px in <%s>', async (tag) => {
    // #55 settled this one component over, when the queue panel's
    // close button was 25x21 and, at phone width, the only pointer
    // route off a full-screen surface: "the way out is 44px". A detail
    // view has the platform's back gesture as well, so it is less
    // severe -- and it is the same control wearing the same mistake.
    const el = await fixture(tag);

    await flush();

    const back = shadow(el, '.back-button');

    expect(back, `${tag} draws a back button`).toBeTruthy();
    expect(boxOf(back)).toEqual({ w: FLOOR, h: FLOOR });
  });

  it('is one declaration, so a seventh view cannot miss it', async () => {
    // The regression this exists for is not a size changing -- it is
    // somebody adding a detail view and writing `.back-button` out
    // again at 32px, which is exactly how there came to be six copies.
    // A sweep of the running app would not catch it either, because a
    // sweep visits the views you think to open.
    const sources = import.meta.glob('../../src/components/**/*.ts', {
      query: '?raw',
      import: 'default',
      eager: true,
    }) as Record<string, string>;

    expect(Object.keys(sources).length, 'the glob read something').toBeGreaterThan(0);

    const redeclared = Object.entries(sources)
      .filter(([, src]) => /^\s*\.back-button\s*(?::[a-z-]+\s*)?\{/m.test(src))
      .map(([path]) => path);

    expect(redeclared).toEqual([]);
  });
});

describe('autotag', () => {
  it('raises the two smallest controls the sweep found', async () => {
    // 187x15 and 32x18. The section toggle was the smallest control
    // measured anywhere in the app until the column arrows were
    // counted, and autotag is off by default (#25), which is
    // presumably why nobody had met either.
    const el = await fixture('autotag-view');

    await flush();

    for (const selector of ['.section-toggle', '.folders-menu-trigger']) {
      const control = shadowAll<HTMLElement>(el, selector).find(
        (c) => c.getBoundingClientRect().height > 0,
      );

      if (!control) continue;

      expect(boxOf(control).h, `${selector} height`).toBeGreaterThanOrEqual(FLOOR);
    }

    // The stylesheet is the assertion for whichever of the two this
    // fixture does not render -- both are behind state a bare mount
    // does not reach, and a test that silently checked nothing is the
    // trap icon-language.test.ts's first assertion exists for.
    const sheet = (el.constructor as typeof HTMLElement & { styles?: unknown })
      .styles;

    expect(String(sheet)).toContain('min-block-size: 44px');
  });
});

describe('the Downloads tabs', () => {
  beforeEach(() => {
    stub('download.Service.ListDownloads', []);
    stub('download.Service.ListRequests', []);
    stub('download.Service.ListProviders', []);
  });

  it('are the only route to their panels, and are 44px', async () => {
    const el = await fixture('downloads-view');

    await flush();

    const tabs = shadowAll<HTMLElement>(el, '[role="tab"]');

    expect(tabs).toHaveLength(2);

    for (const tab of tabs) {
      expect(boxOf(tab).h, tab.textContent?.trim()).toBeGreaterThanOrEqual(FLOOR);
    }
  });

  it('keeps the active underline against the label', async () => {
    // The height is padding rather than a min-size, because the mark
    // for the selected tab is the bottom border -- a min-size would
    // centre the label and leave the underline 10px below it.
    const el = await fixture('downloads-view');

    await flush();

    const tab = shadowAll<HTMLElement>(el, '[role="tab"]')[0]!;
    const style = getComputedStyle(tab);

    expect(parseFloat(style.paddingBlockStart)).toBeGreaterThan(8);
    expect(style.paddingBlockStart).toBe(style.paddingBlockEnd);
  });
});

describe("Explore's own search row", () => {
  beforeEach(() => {
    stub('explore.Service.GetShelves', { State: 'ready', Shelves: [] });
    stub('explore.Service.GetIndexStatus', {});
  });

  it('raises the mode tabs', async () => {
    const el = await fixture('explore-view');

    await flush();

    const tabs = shadowAll<HTMLElement>(el, '.search-mode-tab');

    expect(tabs.length).toBeGreaterThan(0);

    for (const tab of tabs) {
      expect(boxOf(tab).h, tab.textContent?.trim()).toBeGreaterThanOrEqual(FLOOR);
    }
  });

  it('makes the whole search box the input, not the middle 18px of it', async () => {
    // Two faults, not one: the row was 36px and the input inside it
    // was **18**, so half the box was not a target at all -- a tap
    // near the top or bottom edge landed on the container and did
    // nothing. The container is 44 and the input stretches to fill it.
    const el = await fixture('explore-view');

    await flush();

    const box = shadow(el, '.search-container');
    const input = shadow(el, '.search-container input');

    expect(box, 'the search row renders').toBeTruthy();
    expect(input, 'it holds an input').toBeTruthy();

    expect(boxOf(box).h).toBeGreaterThanOrEqual(FLOOR);
    expect(boxOf(input).h).toBeGreaterThanOrEqual(FLOOR);
  });
});

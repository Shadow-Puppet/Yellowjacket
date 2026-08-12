/**
 * Plan 007 phase 6 (`H-23`): Explore starts the conversation.
 *
 * The page was a search box over a 1.1 M-row local catalog and a
 * sentence telling the user to type into it. It now opens with shelves
 * — on `backend/home`'s terms, where a shelf is a *reason* rather than
 * a filter and carries the sentence that says so.
 *
 * What this tier pins is the half that is a rendering decision rather
 * than a query: that a reason is drawn next to its row, that a shelf of
 * artists is not a shelf of albums, and — the part that matters most —
 * that a page with no shelves says which of the three reasons it has no
 * shelves for. Home can omit an empty shelf and be honest, because a
 * library with no history really has less to say. Explore's data is a
 * *downloaded artifact*, so an empty page there might just be a page
 * that does not know yet, and rendering nothing is the blank panel this
 * whole feature exists to remove.
 */
import { beforeEach, describe, expect, it } from 'vitest';

import '@components/explore-view/explore-view';
import { flush, stub } from '@test/support/harness';
import { fixture, shadow, shadowAll, texts } from '@test/support/render';

const SHELVES = 'explore.Service.GetExploreShelves';

type Shelves = {
  shelves: {
    id: string;
    kind: string;
    title: string;
    subtitle: string;
    albums?: unknown[];
    artists?: unknown[];
  }[];
  state: string;
};

const album = (title: string, artist = 'An Artist') => ({
  mbid: `rg-${title}`,
  title,
  artistCredit: artist,
  artistMbid: 'ar-1',
  primaryType: 'Album',
  firstReleaseDate: '1994-05-01',
  popularity: 100,
  listenerCount: 10,
  inLibrary: false,
  secondaryTypes: [],
});

const artist = (name: string) => ({
  mbid: `ar-${name}`,
  name,
  sortName: name,
  type: 'Group',
  country: 'GB',
  disambiguation: '',
  score: 0,
  popularity: 100,
  listenerCount: 10,
  inLibrary: false,
});

/** Mount Explore and activate it, which is what fetches the shelves. */
async function explore(page: Shelves) {
  stub(SHELVES, page);

  const el = await fixture('explore-view');

  (el as unknown as { viewActivated(): void }).viewActivated();
  await flush();
  await el.updateComplete;

  return el;
}

describe('<explore-view> shelves', () => {
  beforeEach(() => {
    stub('explore.Service.GetThumbnails', []);
  });

  it('opens with shelves instead of telling the user to type', async () => {
    const el = await explore({
      state: 'ready',
      shelves: [
        {
          id: 'popular-albums',
          kind: 'popular-albums',
          title: 'Popular right now',
          subtitle: "The most listened-to albums you don't already own",
          albums: [album('One'), album('Two')],
        },
      ],
    });

    expect(texts(el, '.section-header')).toEqual(['Popular right now']);
    expect(shadowAll(el, '.album-card')).toHaveLength(2);
    expect(el.shadowRoot?.textContent).not.toContain('Search to discover');
  });

  it('draws the reason next to the row, not just the title', async () => {
    // Without it a shelf is indistinguishable from a random grid —
    // which is the whole difference between a shelf and a filter.
    const el = await explore({
      state: 'ready',
      shelves: [
        {
          id: 'more-from-owned',
          kind: 'more-from-owned',
          title: 'More from Solo',
          subtitle: 'The rest of what they made',
          albums: [album('Second')],
        },
      ],
    });

    expect(texts(el, '.section-reason')).toEqual([
      'The rest of what they made',
    ]);
  });

  it('renders an artist shelf as artists, not as albums', async () => {
    const el = await explore({
      state: 'ready',
      shelves: [
        {
          id: 'popular-artists',
          kind: 'popular-artists',
          title: 'Artists worth knowing',
          subtitle: 'Widely listened to, and not yet in your library',
          artists: [artist('One'), artist('Two')],
        },
      ],
    });

    expect(shadowAll(el, '.artist-card')).toHaveLength(2);
    expect(shadowAll(el, '.album-card')).toHaveLength(0);
  });

  it('says the catalog is missing rather than rendering nothing', async () => {
    const el = await explore({ state: 'no-index', shelves: [] });

    const empty = shadow(el, '.shelves-empty');

    expect(empty).not.toBeNull();
    expect(empty?.textContent).toContain('has not been downloaded');
    // …and points at the one thing the user can do about it.
    expect(empty?.textContent).toContain('Settings');
  });

  it('distinguishes a catalog that is still arriving from one that is absent', async () => {
    // The same empty page for both would tell a first-run user their
    // catalog is missing while it is downloading in the background.
    const el = await explore({ state: 'building', shelves: [] });

    const empty = shadow(el, '.shelves-empty');

    expect(empty?.textContent).toContain('still downloading');
    expect(empty?.textContent).not.toContain('has not been downloaded');
  });

  it('admits a partial page while the catalog is still arriving', async () => {
    const el = await explore({
      state: 'building',
      shelves: [
        {
          id: 'popular-albums',
          kind: 'popular-albums',
          title: 'Popular right now',
          subtitle: 'The most listened-to albums',
          albums: [album('One')],
        },
      ],
    });

    expect(
      shadow(el, '.shelves-note')?.textContent?.replace(/\s+/g, ' '),
    ).toContain('there is more to come');
  });

  it('does not fetch shelves until the view is on screen', async () => {
    // Every primary view is created and warmed at startup, so a fetch
    // on connect is three catalog queries paid by users who never open
    // Explore.
    stub(SHELVES, { state: 'ready', shelves: [] });

    // Mounted the way `index.ts` creates a cached view: hidden, which
    // is exactly what stops the mixin activating it on connect.
    const el = document.createElement('explore-view');

    el.classList.add('view-hidden');
    document.body.append(el);
    await (el as unknown as { updateComplete: Promise<unknown> })
      .updateComplete;
    await flush();

    const { calls } = await import('@test/support/harness');

    expect(calls(SHELVES)).toHaveLength(0);

    (el as unknown as { viewActivated(): void }).viewActivated();
    await flush();

    expect(calls(SHELVES)).toHaveLength(1);

    el.remove();
  });
});

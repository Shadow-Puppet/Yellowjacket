/**
 * Every album card is the same size, and its artwork is a square.
 *
 * The size came apart because `explore-view` clamped its cards to a
 * 130–150px range, so two cards in one row could be different widths —
 * and since the artwork is square, different *heights* as well. A row
 * of covers with ragged bottoms is what that looks like.
 *
 * What makes the fix hold is that the lines below the art each reserve
 * their own space (`album-card.css.ts`), so an album with no year, no
 * release type or a one-character title is not shorter than its
 * neighbour. This measures that rather than trusting it, because the
 * next component to format a card is the way it comes back.
 *
 * The artwork half is the other change: the container was already
 * square but the image was `object-fit: cover`, so a non-square cover
 * was cropped to it. It is `contain` now, and the container has no
 * background of its own, so a tall cover is inset with the page
 * showing through beside it.
 */
import { beforeEach, describe, expect, it } from 'vitest';
import type { LitElement } from 'lit';

import '@components/explore-view/explore-view';
import { flush, stub, resetHarness } from '@test/support/harness';
import { fixture, shadow, shadowAll, update } from '@test/support/render';
import { completenessStore } from '@store/completeness-store';

const SEARCH = 'explore.Service.SearchLocal';
const SHELVES = 'explore.Service.GetExploreShelves';

/** A 1x1 transparent gif, so the `<img>` branch renders. */
const TINY_IMAGE =
  'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';

/** Release groups chosen so every optional line is present on one and
 *  absent on another — that is what a size regression hides behind. */
const ALBUMS = [
  {
    mbid: 'rg-1',
    title: 'A',
    artistCredit: '',
    artistMbid: 'ar-1',
    primaryType: '',
    firstReleaseDate: '',
    popularity: 1,
    listenerCount: 1,
    secondaryTypes: [],
    inLibrary: false,
    localId: 0,
  },
  {
    mbid: 'rg-2',
    title: 'A Very Long Album Name That Will Certainly Be Truncated By The Card',
    artistCredit: 'An Artist With A Long Name',
    artistMbid: 'ar-2',
    primaryType: 'Album',
    firstReleaseDate: '1994-05-01',
    popularity: 1,
    listenerCount: 1,
    secondaryTypes: [],
    inLibrary: false,
    localId: 0,
  },
  {
    mbid: 'rg-3',
    title: 'Three',
    artistCredit: 'Another',
    artistMbid: 'ar-3',
    primaryType: 'EP',
    firstReleaseDate: '2001-01-01',
    popularity: 1,
    listenerCount: 1,
    secondaryTypes: [],
    inLibrary: false,
    localId: 0,
  },
];

async function exploreWithAlbums(): Promise<LitElement> {
  stub(SHELVES, { shelves: [], state: 'ready' });
  stub(SEARCH, {
    artists: [],
    releaseGroups: ALBUMS,
    recordings: [],
  });
  stub('explore.Service.GetThumbnails', Object.fromEntries(
    ALBUMS.map((a) => [a.mbid, TINY_IMAGE]),
  ));
  stub('explore.Service.GetThumbnail', TINY_IMAGE);

  const el = await fixture<LitElement>('explore-view');

  (el as unknown as { onViewActivate: () => void }).onViewActivate?.();
  await update(el, {
    results: { artists: [], releaseGroups: ALBUMS, recordings: [] },
  });
  await flush();
  await el.updateComplete;

  return el;
}

beforeEach(() => {
  resetHarness();
  stub('library.Library.GetAlbumsCompleteness', {});
  completenessStore.invalidate();
});

describe('the album card size', () => {
  it('is the same width and height for every card in a row', async () => {
    const el = await exploreWithAlbums();
    const cards = shadowAll(el, '.album-card');

    expect(cards.length).toBe(ALBUMS.length);

    const boxes = cards.map((c) => c.getBoundingClientRect());

    // The first card is the reference; every other one must match it.
    for (const box of boxes) {
      expect(box.width).toBe(boxes[0]!.width);
      expect(box.height).toBe(boxes[0]!.height);
    }

    // …and the reference is a real box, or the loop above is vacuous.
    expect(boxes[0]!.width).toBeGreaterThan(0);
    expect(boxes[0]!.height).toBeGreaterThan(0);
  });

  it('keeps the artwork square', async () => {
    const el = await exploreWithAlbums();

    for (const art of shadowAll(el, '.album-art-container')) {
      const box = art.getBoundingClientRect();

      expect(Math.round(box.width)).toBe(Math.round(box.height));
    }
  });

  it('insets a non-square cover rather than cropping it', async () => {
    const el = await exploreWithAlbums();

    // Read from the parsed stylesheet rather than from a rendered
    // `<img>`: the search path is what calls `loadThumbnails`, and
    // setting `results` directly skips it, so there is no image to
    // measure. The regression worth catching is the rule going back to
    // `cover`, which is a stylesheet fact.
    const rules = (el.shadowRoot?.adoptedStyleSheets ?? []).flatMap((sheet) =>
      Array.from(sheet.cssRules).map((rule) => rule.cssText),
    );
    const art = rules.find(
      (text) =>
        text.startsWith('.album-art-container img') &&
        text.includes('object-fit'),
    );

    expect(art, 'no object-fit rule for the cover image').toBeDefined();
    expect(art).toContain('object-fit: contain');
  });

  it('draws the badge over the artwork, and not in the metadata line', async () => {
    const el = await exploreWithAlbums();
    const card = shadow(el, '.album-card')!;

    const badge = card.querySelector('.album-art-container .album-card-badge');

    expect(badge).not.toBeNull();
    // The badge is positioned inside the art box, so its parent is the
    // square rather than the row underneath it.
    expect(badge?.parentElement?.classList.contains('album-art-container')).toBe(
      true,
    );
  });
});

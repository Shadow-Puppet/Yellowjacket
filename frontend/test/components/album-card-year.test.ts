/**
 * The year on an album card survives a long album name.
 *
 * The year used to be part of the same run of text as the title, inside
 * one `text-overflow: ellipsis` box — so it was the first thing the
 * ellipsis ate. A card wide enough for a long name never showed its
 * year at all, which means sorting the grid *by year* showed years only
 * for the albums with short names: the sort said one thing and the
 * cards showed another.
 *
 * The fix is a flex row in which only the title truncates, rather than
 * a second line, because the card's height is what the virtualizer
 * measures rows by.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/cover-grid/cover-grid';
import { emit, stub, flush, resetHarness } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadowAll } from '@test/support/render';

const LONG =
  'The Rise and Fall of a Midwest Princess in the Key of Everything';

/** Long names throughout: the fault only shows on a card under
 *  pressure, and a grid of "Album 3" proves nothing. */
const ALBUMS = Array.from({ length: 12 }, (_, i) => ({
  ID: i + 1,
  Name: `${LONG} ${i + 1}`,
  ArtistName: 'Aurora Fields',
  Year: 2019 + (i % 5),
}));

/** Give the virtualizer a viewport; a zero-height host renders nothing. */
function sized(el: HTMLElement): void {
  el.style.display = 'block';
  el.style.height = '600px';
  el.style.width = '900px';
}

async function settle(el: LitElement): Promise<void> {
  await flush();
  await el.updateComplete;
  await new Promise((r) => setTimeout(r, 80));
}

describe('the album card’s year', () => {
  beforeEach(() => {
    resetHarness();
    stub('library.Library.GetAlbums', ALBUMS);
    stub('library.Library.GetTracks', []);
    emit(Events.LibraryScanComplete);
  });

  it('is rendered on every card, however long the name', async () => {
    const el = await fixture<LitElement>('cover-grid');

    sized(el);
    await settle(el);

    const cards = shadowAll(el, '.album-card');
    const years = shadowAll(el, '.album-year');

    expect(cards.length).toBeGreaterThan(0);
    expect(years).toHaveLength(cards.length);
    expect(years.every((y) => /^\(\d{4}\)$/.test(y.textContent!.trim()))).toBe(
      true,
    );
  });

  it('is not what the ellipsis eats', async () => {
    const el = await fixture<LitElement>('cover-grid');

    sized(el);
    await settle(el);

    const year = shadowAll(el, '.album-year')[0]!;
    const title = shadowAll(el, '.album-title')[0]!;

    // The title is the box that gives way...
    expect(title.scrollWidth).toBeGreaterThan(title.clientWidth);
    // ...and the year keeps every pixel it asked for.
    expect(year.clientWidth).toBeGreaterThan(0);
    expect(year.scrollWidth).toBeLessThanOrEqual(year.clientWidth + 1);
  });
});

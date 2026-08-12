/**
 * The ARIA tail of `a11y.md`, pinned.
 *
 * Every assertion here is a finding that was reproduced in the running
 * app first. Three of them are the kind that no other tier can see: a
 * grid that sorts and never says so, a list whose loading/empty/failed
 * states are text nobody is watching, and `aria-selected` on
 * `role="button"`, which is not merely useless but *invalid* — the
 * attribute is dropped, so the state the whole ctrl/shift interaction
 * exists to produce was invisible.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/track-list/track-list';
import '@components/artists-view/artists-view';
import '@components/genres-view/genres-view';
import { emit, stub, flush, resetHarness } from '@test/support/harness';
import { Events } from '../../src/events';
import { fixture, shadow, shadowAll } from '@test/support/render';
import { searchStore } from '@store/search-store';

/**
 * The searchable columns' accessors read these fields and call
 * `.toLowerCase()` on the result, so a sparse fixture throws inside the
 * ranker rather than failing an assertion. Real tracks always carry
 * them; a fixture has to as well.
 */
const TRACKS = [
  {
    FilePath: '/m/a.mp3',
    TrackName: 'Departure',
    ArtistName: 'Aurora Fields',
    Album: 'Glass Harbour',
    AlbumArtist: 'Aurora Fields',
    Composer: '',
    Genre: ['Ambient'],
    TrackLength: 4000,
  },
  {
    FilePath: '/m/b.mp3',
    TrackName: 'Tideline',
    ArtistName: 'Aurora Fields',
    Album: 'Glass Harbour',
    AlbumArtist: 'Aurora Fields',
    Composer: '',
    Genre: ['Ambient'],
    TrackLength: 6000,
  },
];

const ARTISTS = [
  { ID: 1, Name: 'Alpha', AlbumCount: 2, TrackCount: 9 },
  { ID: 2, Name: 'Beta', AlbumCount: 1, TrackCount: 4 },
];

const GENRES = [
  { name: 'Ambient', count: 12 },
  { name: 'Doom', count: 3 },
];

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

describe('the track list says how it is sorted', () => {
  beforeEach(async () => {
    resetHarness();
    searchStore.setTerm('');
    stub('library.Library.GetAllTracks', TRACKS);
    stub('library.Library.GetAllAlbums', []);
    emit(Events.LibraryScanComplete);
  });

  it('gives every column an aria-sort, defaulting to none', async () => {
    const el = await fixture<LitElement>('track-list');

    sized(el);
    await settle(el);

    const sorts = shadowAll(el, '.header-cell[role="columnheader"]').map((h) =>
      h.getAttribute('aria-sort'),
    );

    expect(sorts.length, 'no sortable column headers').toBeGreaterThan(0);
    // The list opens in file order — `sortField` is null — so no column
    // claims to be the sort until one is chosen.
    expect(sorts.every((s) => s === 'none')).toBe(true);
  });

  it('changes aria-sort when a column is activated from the keyboard', async () => {
    const el = await fixture<LitElement>('track-list');

    sized(el);
    await settle(el);

    const title = shadowAll(el, '.header-cell[role="columnheader"]').find((h) =>
      /track name/i.test(h.textContent ?? ''),
    );

    expect(title, 'no Track Name column header').toBeTruthy();

    // Reading the DOM synchronously after this would report the state
    // *before* Lit rendered, which is how a fix for nothing gets shipped.
    title!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, composed: true }),
    );
    await settle(el);

    const after = shadowAll(el, '.header-cell[role="columnheader"]').find((h) =>
      /track name/i.test(h.textContent ?? ''),
    );

    expect(after?.getAttribute('aria-sort')).toBe('ascending');
  });
});

describe('the track list has a voice for its own state', () => {
  beforeEach(() => {
    resetHarness();
    searchStore.setTerm('');
    stub('library.Library.GetAllAlbums', []);
  });

  it('announces the result of a search that matches nothing', async () => {
    stub('library.Library.GetAllTracks', TRACKS);
    emit(Events.LibraryScanComplete);

    const el = await fixture<LitElement>('track-list');

    sized(el);
    await settle(el);

    const live = shadow(el, '[role="status"][aria-live="polite"]');

    // The region exists *before* it has anything to say — a region that
    // appears with its text already in it is not announced by most
    // screen readers.
    expect(live, 'no live region on the track list').toBeTruthy();

    searchStore.setCurrentView('tracks');
    searchStore.setTerm('nothing matches this');
    await settle(el);

    expect(shadow(el, '[role="status"]')?.textContent?.trim()).toMatch(
      /No tracks match/i,
    );
  });
});

describe('a selectable grid is a listbox, not a row of buttons', () => {
  beforeEach(() => {
    resetHarness();
    searchStore.setTerm('');
    stub('library.Library.GetAllArtists', ARTISTS);
    stub('library.Library.GetAllGenresWithCounts', GENRES);
    stub('library.Library.GetAllTracks', []);
    stub('library.Library.GetAllAlbums', []);
    emit(Events.LibraryScanComplete);
  });

  it.each([
    ['artists-view', '.artist-card'],
    ['genres-view', '.genre-card'],
  ])('%s cards are options carrying aria-selected', async (tag, cardSelector) => {
    const el = await fixture<LitElement>(tag);

    sized(el);
    await settle(el);

    const card = shadowAll(el, cardSelector)[0];

    expect(card, `no cards rendered in ${tag}`).toBeTruthy();

    // role=button + aria-selected is invalid: the attribute is dropped,
    // and the selection is invisible to anything but a sighted user.
    expect(card!.getAttribute('role')).toBe('option');
    expect(card!.hasAttribute('aria-selected')).toBe(true);

    const list = shadow(el, '[role="listbox"]');

    expect(list, `${tag} has options with no listbox`).toBeTruthy();
    expect(list!.getAttribute('aria-multiselectable')).toBe('true');
  });
});

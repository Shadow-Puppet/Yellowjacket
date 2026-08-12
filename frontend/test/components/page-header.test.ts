/**
 * H-19: four views had a heading and four did not, two had sort
 * controls, and none showed a count — so the app changed shape as you
 * moved through it, and "how many albums have I got" could only be
 * answered by counting them.
 *
 * `<page-header>` is the one arrangement they all use now. The
 * behavioural assertions are here; the baselines below are what catch
 * the thing no assertion can — the header looking wrong.
 */
import { describe, expect, it } from 'vitest';
import type { PageHeader } from '@components/page-header/page-header';

import '@components/page-header/page-header';
import { fixture, shadow, shadowAll, update, visual } from '@test/support/render';

const SORTS = [
  { id: 'name', label: 'Name' },
  { id: 'tracks', label: 'Tracks' },
];

describe('<page-header>', () => {
  it('renders the heading as the page\u2019s only h1', async () => {
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Albums',
    });

    expect(shadowAll(el, 'h1').map((h) => h.textContent?.trim())).toEqual([
      'Albums',
    ]);
  });

  it('counts in the plural, and in the singular when there is one', async () => {
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      count: 4,
      countNoun: 'playlist',
    });

    expect(shadow(el, '[data-testid="page-count"]')?.textContent?.trim()).toBe(
      '4 playlists',
    );

    await update(el, { count: 1 });

    expect(shadow(el, '[data-testid="page-count"]')?.textContent?.trim()).toBe(
      '1 playlist',
    );
  });

  it('says nothing about a count it has not been given', async () => {
    // `null` is "not applicable" (Jobs, Settings), not zero — a page
    // with nothing on it says so in its empty state, which has room
    // for a sentence.
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Background jobs',
    });

    expect(shadow(el, '[data-testid="page-count"]')).toBeNull();
  });

  it('renders zero, which is a real answer', async () => {
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Albums',
      count: 0,
      countNoun: 'album',
    });

    expect(shadow(el, '[data-testid="page-count"]')?.textContent?.trim()).toBe(
      '0 albums',
    );
  });

  it('asks for a sort rather than performing one', async () => {
    // The host owns the sort state and its persistence; the header is
    // a control, not a source of truth.
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Genres',
      sortOptions: SORTS,
      sortField: 'name',
      sortDirection: 'asc',
    });

    const seen: unknown[] = [];

    el.addEventListener('sort-change', (e) =>
      seen.push((e as CustomEvent).detail),
    );

    const select = shadow<HTMLSelectElement>(el, '[data-testid="page-sort"]');

    select!.value = 'tracks';
    select!.dispatchEvent(new Event('change'));

    shadow<HTMLElement>(el, '[data-testid="page-sort-direction"]')?.click();

    expect(seen).toEqual([
      { field: 'tracks', direction: 'asc' },
      { field: 'name', direction: 'desc' },
    ]);
    // Unchanged: the host had not applied either.
    expect(el.sortField).toBe('name');
  });

  it('offers no sort control when there is nothing to sort by', async () => {
    const el = await fixture<PageHeader>('page-header', { heading: 'Home' });

    expect(shadow(el, '.sort')).toBeNull();
  });

  it('shows one sort key as a label, not a select with one option', async () => {
    // Artists: `library.Artist` carries nothing countable, so the only
    // key is the name and the direction button does the work.
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Artists',
      sortOptions: [{ id: 'name', label: 'Name' }],
      sortField: 'name',
    });

    expect(shadow(el, '[data-testid="page-sort"]')).toBeNull();
    expect(shadow(el, '[data-testid="page-sort-direction"]')).not.toBeNull();
    expect(shadow(el, '.sort')?.textContent).toContain('Name');
  });

  it('names the scope of the search that is filtering it', async () => {
    // The header search is view-scoped by decision, so a page showing
    // three of forty has to say why — "No playlists match your search"
    // arriving after the fact was the whole of H-10.
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      count: 3,
      countNoun: 'playlist',
      searchTerm: 'tide',
    });

    expect(
      shadow(el, '[data-testid="page-search-scope"]')?.textContent,
    ).toContain('playlists matching');
  });

  it('drops the title where it is embedded in a page that has one', async () => {
    // `cover-grid` inside the artist page, `track-list` inside the
    // genre page: the count and the sort still apply, a second h1 does
    // not.
    const el = await fixture<PageHeader>('page-header', {
      heading: '',
      count: 12,
      countNoun: 'album',
    });

    expect(shadow(el, 'h1')).toBeNull();
    expect(shadow(el, '[data-testid="page-count"]')).not.toBeNull();
  });
});

describe('<page-header> as each view wears it', () => {
  // One baseline per arrangement rather than per view: the point is
  // that eight views produce four shapes, not eight.
  const cases: [string, Record<string, unknown>][] = [
    ['title-only', { heading: 'Home' }],
    [
      'title-and-count',
      { heading: 'Downloads', count: 12, countNoun: 'request' },
    ],
    [
      'title-count-and-sort',
      {
        heading: 'Genres',
        count: 5,
        countNoun: 'genre',
        sortOptions: SORTS,
        sortField: 'tracks',
        sortDirection: 'desc',
      },
    ],
    [
      'filtered-by-search',
      {
        heading: 'Tracks',
        count: 3,
        countNoun: 'track',
        sortOptions: SORTS,
        sortField: 'name',
        searchTerm: 'tide',
      },
    ],
  ];

  for (const [name, props] of cases) {
    it(`looks right: ${name}`, async () => {
      const el = await fixture<PageHeader>('page-header', props);

      el.style.width = '900px';
      await el.updateComplete;

      await visual(el, `page-header-${name}`);
      // The baseline is opt-in (`make ui-visual`); this keeps the
      // default run asserting something rather than nothing.
      expect(shadow(el, '.page-header')).not.toBeNull();
    });
  }
});

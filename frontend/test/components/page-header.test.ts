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
import type { PageAction, PageHeader } from '@components/page-header/page-header';

import '@components/page-header/page-header';
import { fixture, shadow, shadowAll, update, visual } from '@test/support/render';

const SORTS = [
  { id: 'name', label: 'Name' },
  { id: 'tracks', label: 'Tracks' },
];

/**
 * Three actions of the shape that broke: Playlists' own, whose widths
 * (91 + 122 + 162 = 390px) are what a 700px header could not hold.
 */
function playlistActions(seen: string[]): PageAction[] {
  return [
    {
      id: 'import',
      label: 'Import',
      icon: 'file-import',
      priority: 0,
      onSelect: () => seen.push('import'),
    },
    {
      id: 'new-playlist',
      label: 'New Playlist',
      icon: 'plus',
      priority: 2,
      onSelect: () => seen.push('new-playlist'),
    },
    {
      id: 'new-smart-playlist',
      label: 'New Smart Playlist',
      icon: 'filter',
      priority: 1,
      onSelect: () => seen.push('new-smart-playlist'),
    },
  ];
}

/**
 * Resize and let the fit settle.
 *
 * The rule is driven by a ResizeObserver, which delivers before paint
 * and therefore after the microtask queue an `updateComplete` drains —
 * so this waits on frames rather than on promises, and then on the
 * render the measurement asks for.
 */
async function widthOf(el: PageHeader, px: number): Promise<void> {
  el.style.width = `${px}px`;

  for (let frame = 0; frame < 3; frame += 1) {
    await new Promise((r) => requestAnimationFrame(r));
    await el.updateComplete;
  }
}

/** The labels currently rendered as buttons, in order. */
function buttons(el: PageHeader): string[] {
  return shadowAll<HTMLButtonElement>(el, '.action')
    .filter((b) => !b.hidden)
    .map((b) => b.textContent?.trim() ?? '');
}

/** The labels currently in the overflow menu, in order. */
function menu(el: PageHeader): string[] {
  return shadowAll(el, '#page-header-overflow wa-dropdown-item').map(
    (i) => i.textContent?.trim() ?? '',
  );
}

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

/**
 * #69: Playlists slotted three buttons totalling 390px into a header
 * that gets 700px at 900×600, and "New Smart Playlist" rendered 114 of
 * its 162. It survived a spec named `layout-overflow` because that one
 * asserts the *shell* needs no sideways scrolling — clipping inside a
 * component is invisible to it.
 *
 * The header can only fix that for actions it renders itself, which is
 * why they are data now. These are the assertions about the rule; the
 * e2e spec is what checks it against the real widths.
 */
describe('<page-header> actions', () => {
  it('renders a declared action, and asks the host to perform it', async () => {
    // Same division the sort control already lives by: the header
    // decides what fits, the host decides what happens.
    const seen: string[] = [];
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      actions: playlistActions(seen),
    });

    await widthOf(el, 1200);

    expect(buttons(el)).toEqual([
      'Import',
      'New Playlist',
      'New Smart Playlist',
    ]);

    shadow<HTMLButtonElement>(el, '[data-testid="page-action-import"]')!.click();

    expect(seen).toEqual(['import']);
  });

  it('hides the overflow trigger while everything fits', async () => {
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      actions: playlistActions([]),
    });

    await widthOf(el, 1200);

    expect(shadow<HTMLButtonElement>(el, '.more-button')!.hidden).toBe(true);
    expect(menu(el)).toEqual([]);
  });

  it('collapses the lowest priority first', async () => {
    // Import is lowest because it is rarest; New Playlist is highest
    // because it is the drop target, and a closed menu cannot be one.
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      count: 4,
      countNoun: 'playlist',
      sortOptions: SORTS,
      sortField: 'name',
      actions: playlistActions([]),
    });

    // Asserted as the *order* rather than at two chosen widths: which
    // pixel drops which button depends on the font and on the shell
    // this tier does not have, and pinning those numbers here would be
    // a test of the fixture. What the host declares is a sequence.
    const states: string[][] = [];

    for (let width = 1200; width >= 300; width -= 40) {
      await widthOf(el, width);

      const now = menu(el);
      const last = states[states.length - 1];

      if (last === undefined || last.join() !== now.join()) states.push(now);
    }

    expect(states).toEqual([
      [],
      ['Import'],
      ['Import', 'New Smart Playlist'],
      ['Import', 'New Playlist', 'New Smart Playlist'],
    ]);

    // The menu lists them in the host's declared order, not in the
    // order they happened to collapse — a menu that reshuffles itself
    // as the window narrows is a menu nobody can learn.
    expect(buttons(el)).toEqual([]);
  });

  it('gives an action back when the width returns', async () => {
    // Every pass starts from all-visible, so the collapsed set is a
    // function of the current width and not of how it got there. A rule
    // that only ever added to the set would never widen again.
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      count: 4,
      countNoun: 'playlist',
      sortOptions: SORTS,
      sortField: 'name',
      actions: playlistActions([]),
    });

    await widthOf(el, 420);

    expect(buttons(el)).toEqual([]);

    await widthOf(el, 1200);

    expect(menu(el)).toEqual([]);
    expect(buttons(el)).toEqual([
      'Import',
      'New Playlist',
      'New Smart Playlist',
    ]);
  });

  it('collapses an action before it truncates the title', async () => {
    // The title can ellipsis, which means `scrollWidth` reports a
    // header that fits perfectly while the heading reads "Playlis…" —
    // this issue's failure mode moved from the button to the title, and
    // invisible to the same measurement that missed it the first time.
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      count: 4,
      countNoun: 'playlist',
      sortOptions: SORTS,
      sortField: 'name',
      actions: playlistActions([]),
    });

    await widthOf(el, 700);

    const h1 = shadow<HTMLElement>(el, 'h1')!;

    expect(h1.scrollWidth).toBeLessThanOrEqual(h1.clientWidth + 1);
    expect(menu(el).length).toBeGreaterThan(0);
  });

  it('names the overflow trigger and says what it controls', async () => {
    // An overflow menu is exactly the shape that grows a nameless
    // control, and `aria-controls` cannot name an element that is not
    // in the DOM — which is why the panel renders unconditionally and
    // `wa-popup` hides it, the same rule `config-section` follows.
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      count: 4,
      countNoun: 'playlist',
      sortOptions: SORTS,
      sortField: 'name',
      actions: playlistActions([]),
    });

    await widthOf(el, 480);

    const more = shadow<HTMLButtonElement>(el, '.more-button')!;

    expect(more.hidden).toBe(false);
    expect(more.getAttribute('aria-label')).toBe('More actions');
    expect(more.getAttribute('aria-expanded')).toBe('false');
    expect(more.getAttribute('aria-haspopup')).toBe('menu');

    const panel = shadow<HTMLElement>(el, '#page-header-overflow')!;

    expect(more.getAttribute('aria-controls')).toBe(panel.id);
    expect(panel.getAttribute('role')).toBe('menu');

    more.click();
    await el.updateComplete;

    expect(
      shadow<HTMLButtonElement>(el, '.more-button')!.getAttribute(
        'aria-expanded',
      ),
    ).toBe('true');
  });

  it('runs a collapsed action from the menu, and closes it', async () => {
    const seen: string[] = [];
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      count: 4,
      countNoun: 'playlist',
      sortOptions: SORTS,
      sortField: 'name',
      actions: playlistActions(seen),
    });

    await widthOf(el, 700);
    shadow<HTMLButtonElement>(el, '.more-button')!.click();
    await el.updateComplete;

    shadowAll<HTMLElement>(el, '#page-header-overflow wa-dropdown-item')[0]!.click();
    await el.updateComplete;

    expect(seen).toEqual(['import']);
    expect(
      shadow<HTMLButtonElement>(el, '.more-button')!.getAttribute(
        'aria-expanded',
      ),
    ).toBe('false');
  });

  it('keeps a drop target a drop target, and does not fake one in the menu', async () => {
    // You cannot drag a track onto a closed menu, so the affordance is
    // absent from the overflow rather than approximated there. The
    // header wires the handlers onto the button and owns none of them.
    const dropped: string[] = [];
    const actions: PageAction[] = [
      {
        id: 'new-playlist',
        label: 'New Playlist',
        icon: 'plus',
        onSelect: () => undefined,
        drop: {
          active: true,
          onDragOver: () => dropped.push('over'),
          onDragLeave: () => dropped.push('leave'),
          onDrop: () => dropped.push('drop'),
        },
      },
    ];
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      actions,
    });

    await widthOf(el, 1200);

    const button = shadow<HTMLElement>(
      el,
      '[data-testid="page-action-new-playlist"]',
    )!;

    expect(button.classList.contains('drag-over')).toBe(true);

    button.dispatchEvent(new DragEvent('dragover', { bubbles: true }));
    button.dispatchEvent(new DragEvent('drop', { bubbles: true }));

    expect(dropped).toEqual(['over', 'drop']);

    // …and collapsed, it is a menu item with no drop wiring at all.
    await widthOf(el, 120);

    expect(menu(el)).toEqual(['New Playlist']);
    expect(
      shadow<HTMLElement>(el, '[data-testid="page-action-new-playlist"]')
        ?.hidden,
    ).toBe(true);
  });

  it('renders nothing at all for a view with no actions', async () => {
    // Two of the three hosts have one action and one has none while its
    // other tab is up; an empty actions row is not a mode.
    const el = await fixture<PageHeader>('page-header', { heading: 'Albums' });

    await widthOf(el, 900);

    expect(shadow(el, '.actions')).toBeNull();
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

/**
 * Every control a finger meets is at least 44px (#186).
 *
 * #56 sized the playback transport for a thumb and named 44px; the
 * queue header keeps it; nothing else was resized. So the controls a
 * user meets on *every* screen — the sort control, its direction
 * button, the page actions, the overflow trigger and the phone's search
 * button — sat between a third and two thirds of the app's own floor.
 * Measured on the reference device (TLP301, 424x439): `page-sort` 99x23,
 * `page-sort-direction` **28x21**, `page-actions-more` 38x27,
 * `search-trigger` 40x40.
 *
 * Unlike the seek bar's target (#187), this one can be measured here
 * rather than inferred from the stylesheet. There the painted track had
 * to stay thin, so the hit area was grown past its own box and only a
 * phone-width layout of a third-party slider could show it. Here the
 * control *is* the target, so a real Chromium rendering a real
 * `page-header` gives the actual answer — and because it is a `min-size`
 * rather than a media query, the answer is the same at every width,
 * which is what makes it checkable in this tier at all.
 *
 * That is also why there is no phone branch to test: a 44px control on
 * a desktop is merely large, and a second declaration of what a phone
 * shows is a second thing to keep in step.
 */
import { describe, expect, it } from 'vitest';
import type { PageAction, PageHeader } from '@components/page-header/page-header';

import '@components/page-header/page-header';
import { fixture, shadowAll } from '@test/support/render';

/** The app's touch floor, from #56. */
const FLOOR = 44;

const SORTS = [
  { id: 'name', label: 'Name' },
  { id: 'tracks', label: 'Tracks' },
];

function actions(): PageAction[] {
  return [
    { id: 'import', label: 'Import', icon: 'file-import', priority: 0, onSelect: () => {} },
    { id: 'new', label: 'New Playlist', icon: 'plus', priority: 2, onSelect: () => {} },
  ];
}

/** Every visible control in the header's own shadow root. */
function controlsOf(el: PageHeader): { name: string; el: HTMLElement }[] {
  return shadowAll<HTMLElement>(el, 'button, select')
    .filter((c) => !(c as HTMLButtonElement).hidden)
    .map((c) => ({
      name: c.dataset.testid ?? (c.className || c.tagName.toLowerCase()),
      el: c,
    }));
}

function tooSmall(controls: { name: string; el: HTMLElement }[]): string[] {
  return controls
    .map(({ name, el }) => {
      const b = el.getBoundingClientRect();

      return { name, w: Math.round(b.width), h: Math.round(b.height) };
    })
    .filter((c) => c.w < FLOOR || c.h < FLOOR)
    .map((c) => `${c.name} ${c.w}x${c.h}`);
}

describe("the page header's controls", () => {
  it('all meet the touch floor', async () => {
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      count: 50,
      countNoun: 'playlist',
      sortOptions: SORTS,
      sortField: 'name',
      sortDirection: 'asc',
      actions: actions(),
    });

    const controls = controlsOf(el);

    // A sweep that found no controls passes vacuously — the same first
    // assertion icon-language.test.ts makes, for the same reason.
    expect(controls.length).toBeGreaterThan(0);

    // The two that were smallest, named so a regression says which.
    expect(controls.map((c) => c.name)).toContain('page-sort-direction');
    expect(controls.map((c) => c.name)).toContain('page-sort');

    expect(tooSmall(controls)).toEqual([]);
  });

  it('includes the overflow trigger, which is the route to the rest', async () => {
    // At 320px the fit pass collapses actions into the menu, so the
    // trigger is rendered — and it is then the only way to reach them,
    // which makes it the last control that should be hard to hit.
    const el = await fixture<PageHeader>('page-header', {
      heading: 'Playlists',
      sortOptions: SORTS,
      sortField: 'name',
      actions: actions(),
    });

    el.style.width = '320px';

    for (let frame = 0; frame < 3; frame += 1) {
      await new Promise((r) => requestAnimationFrame(r));
      await el.updateComplete;
    }

    const more = shadowAll<HTMLButtonElement>(el, '.more-button').filter(
      (b) => !b.hidden,
    );

    expect(more.length).toBe(1);

    const box = more[0]!.getBoundingClientRect();

    expect(Math.round(box.width)).toBeGreaterThanOrEqual(FLOOR);
    expect(Math.round(box.height)).toBeGreaterThanOrEqual(FLOOR);
  });
});

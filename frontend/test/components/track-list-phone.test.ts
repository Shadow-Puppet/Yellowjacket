/**
 * The track list on a phone (plan 016 B2 phase 4).
 *
 * Measured on the device this was built for: at 424 CSS px the four
 * configured columns fit the row *exactly* — `--grid-cols` came out
 * `24px 102px 101px 101px 80px` — and not one of them fit its content.
 * "Duration" did not fit its own header. The columns were never too
 * wide; there were too many of them.
 *
 * So a phone draws one column of two lines plus the duration, and the
 * split is a *column set* rather than a second row template: everything
 * about a row that is not "how many columns" keeps working, which is
 * what these tests pin. The `matchMedia` stub is the same one
 * `now-playing.test.ts` uses — the component reads the breakpoint from
 * JS because its grid is computed in JS, so this is the seam.
 */
import { describe, expect, it, afterEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/track-list/track-list';
import { fixture, shadow, shadowAll } from '@test/support/render';

const TRACKS = Array.from({ length: 12 }, (_, i) => ({
  FilePath: `/music/track-${i}.mp3`,
  TrackName: `Track ${i}`,
  ArtistName: `Artist ${i}`,
  Album: 'An Album',
  Duration: 180 + i,
})) as never[];

const real = window.matchMedia.bind(window);

/** Force the shell's phone breakpoint on or off. */
function stubPhone(phone: boolean): void {
  window.matchMedia = ((q: string) =>
    q.includes('max-width: 599px')
      ? {
          matches: phone,
          media: q,
          addEventListener() {},
          removeEventListener() {},
        }
      : real(q)) as typeof window.matchMedia;
}

async function mount(phone: boolean): Promise<LitElement> {
  stubPhone(phone);

  // Narrow, so a desktop layout at this width would be the cramped one
  // the plan describes rather than a comfortable one.
  const el = await fixture<LitElement>('track-list', {
    externalTracks: TRACKS,
  });

  el.style.width = '424px';
  el.style.height = '400px';
  await el.updateComplete;

  return el;
}

afterEach(() => {
  window.matchMedia = real;
  localStorage.removeItem('track-list-column-widths');
});

describe('the track list at phone width', () => {
  it('draws the title with the artist under it, and the duration', async () => {
    const el = await mount(true);
    const row = shadow(el, '.track-row');

    expect(row).not.toBeNull();
    expect(shadow(el, '.stacked-title')?.textContent?.trim()).toBe('Track 0');
    expect(shadow(el, '.stacked-sub')?.textContent?.trim()).toBe('Artist 0');

    // Two drawn columns plus the favourite: three grid tracks, not five.
    const tracks = getComputedStyle(row as Element)
      .gridTemplateColumns.split(/\s+/)
      .filter(Boolean);

    expect(tracks).toHaveLength(3);
  });

  it('drops the column headers and the resize handles', async () => {
    const el = await mount(true);

    // Both are pointer affordances: a header is where a click sorts and
    // a handle is where a drag resizes, and a phone can do neither.
    expect(shadow(el, '.header-row')).toBeNull();
    expect(shadowAll(el, '.col-resize-handle')).toHaveLength(0);
  });

  it('keeps every sort the desktop offers', async () => {
    const el = await mount(true);
    const header = shadow(el, 'page-header') as
      | (Element & { sortOptions?: { id: string }[] })
      | null;

    // The regression this guards: building the sort list from the
    // *drawn* columns would leave a phone able to sort by title and
    // duration only, with no column headers to reach the rest by.
    const ids = (header?.sortOptions ?? []).map((o) => o.id);

    expect(ids).toContain('artistName');
    expect(ids).toContain('album');
  });

  it('still marks the row a screen reader has to understand', async () => {
    const el = await mount(true);
    const row = shadow(el, '.track-row');

    // The row is the same row: only the cells inside it changed, which
    // is the entire argument for doing this as a column set.
    expect(row?.getAttribute('role')).toBe('row');
    expect(row?.getAttribute('aria-selected')).toBe('false');
    expect(row?.getAttribute('data-testid')).toBe('track-row');
    expect(shadowAll(el, '[role="gridcell"]').length).toBeGreaterThan(0);
  });

  it('ignores widths saved for the desktop, and does not overwrite them', async () => {
    // The bug the device found, in the fixture that reproduces it.
    // `loadColumnWidths` is keyed by column *id* and fills a gap with
    // MIN_COLUMN_WIDTH, so the phone's stacked column -- which nothing
    // can ever have saved a width for -- came out at the minimum while
    // the duration column inherited a width dragged on a wide window:
    // measured `24px 148px 236px` on a 424px phone.
    const desktop = { trackName: 300, artistName: 200, album: 200, trackLength: 236 };

    localStorage.setItem('track-list-column-widths', JSON.stringify(desktop));

    const el = await mount(true);
    const tracks = getComputedStyle(shadow(el, '.track-row') as Element)
      .gridTemplateColumns.split(/\s+/)
      .filter(Boolean)
      .map((t) => Math.round(parseFloat(t)));

    // Favourite, then the stacked column, then the duration -- and the
    // stacked one is the widest thing in the row.
    expect(tracks[1]).toBeGreaterThan(tracks[2] ?? 0);

    // And the desktop's own widths survive being on a phone: writing the
    // computed phone widths back would silently replace the width the
    // user dragged for the same column id.
    expect(JSON.parse(localStorage.getItem('track-list-column-widths') ?? '{}'))
      .toMatchObject(desktop);
  });

  it('leaves the desktop alone', async () => {
    const el = await mount(false);
    const row = shadow(el, '.track-row');

    expect(shadow(el, '.header-row')).not.toBeNull();
    expect(shadow(el, '.stacked-title')).toBeNull();

    const tracks = getComputedStyle(row as Element)
      .gridTemplateColumns.split(/\s+/)
      .filter(Boolean);

    expect(tracks).toHaveLength(5);
  });
});

/**
 * `H-7`: `computeDefaultWidths` distributed the host's whole
 * `clientWidth` across the resizable columns, while every row spends
 * 24 px on the favourite column and 2×8 px on its own padding before
 * the first one starts. So the grid was always exactly 40 px wider
 * than the box it had to fit in, and the last column was clipped on
 * every row at every window size — measured in the running app at
 * `scrollWidth 1280` against `clientWidth 1240`.
 *
 * The e2e spec (`e2e/specs/layout-overflow.spec.ts`) asserts it in the
 * real shell at three viewports. This is the cheap guard on the
 * arithmetic itself, because the failure is silent: the row still
 * renders, nothing throws, and the only symptom is a column you cannot
 * read.
 */
import { describe, expect, it, beforeEach } from 'vitest';
import type { LitElement } from 'lit';

import '@components/track-list/track-list';
import { fixture, shadow, shadowAll } from '@test/support/render';

/** The chrome a row spends before the first resizable column. */
const FAV_COL = 24;
const ROW_PADDING_X = 8;

const TRACKS = Array.from({ length: 40 }, (_, i) => ({
  FilePath: `/music/track-${i}.mp3`,
  TrackName: `Track ${i}`,
  ArtistName: 'An Artist',
  Album: 'An Album',
  Duration: 180,
})) as never[];

/** Sum the px tracks of a `grid-template-columns` value. */
function trackWidths(el: Element | null): number[] {
  if (el === null) throw new Error('no element to measure');

  return getComputedStyle(el)
    .gridTemplateColumns.split(/\s+/)
    .filter(Boolean)
    .map((t) => parseFloat(t))
    .filter((n) => !Number.isNaN(n));
}

describe('<track-list> column arithmetic', () => {
  let el: LitElement;
  /** What the columns may spend: the host, minus the row's padding. */
  let budget: number;

  beforeEach(async () => {
    // A saved layout is normalised against the same budget, so the
    // default path is the one worth pinning.
    localStorage.removeItem('track-list-column-widths');

    el = await fixture<LitElement>('track-list', {
      externalTracks: TRACKS,
    });
    el.style.height = '600px';
    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 60));

    budget = el.clientWidth - ROW_PADDING_X * 2;
  });

  it('leaves room for the favourite column and the row padding', () => {
    const widths = trackWidths(shadow(el, '.header-row'));
    const total = widths.reduce((a, b) => a + b, 0);

    expect(total).toBeLessThanOrEqual(budget);
  });

  it('spends the whole budget and no more', () => {
    // Not merely "does not overflow": a fix that under-filled would
    // pass that and leave a gap down the right of every row.
    const widths = trackWidths(shadow(el, '.header-row'));
    const total = widths.reduce((a, b) => a + b, 0);

    expect(total).toBe(budget);
    expect(widths[0]).toBe(FAV_COL);
  });

  it('renders no row wider than the row itself', () => {
    const rows = shadowAll<HTMLElement>(el, '.track-row');

    expect(rows.length).toBeGreaterThan(0);
    expect(
      rows.filter((r) => r.scrollWidth > r.clientWidth),
    ).toHaveLength(0);
  });

  it('keeps the header aligned with the rows it heads', () => {
    // The regression this catches is the one a screenshot found twice
    // in this plan: columns that no longer line up with their header.
    expect(trackWidths(shadow(el, '.header-row'))).toEqual(
      trackWidths(shadow(el, '.track-row')),
    );
  });

  it('places the resize handles on the boundaries it just measured', () => {
    // `colBoundaryPositions` starts from the same two numbers
    // `computeDefaultWidths` subtracts. They were written out
    // separately, which is how they came to disagree.
    const widths = trackWidths(shadow(el, '.header-row'));
    const handles = shadowAll<HTMLElement>(el, '.col-resize-handle');

    expect(handles).toHaveLength(widths.length - 2);
    expect(parseFloat(handles[0]?.style.left ?? '0')).toBe(
      ROW_PADDING_X + FAV_COL + (widths[1] ?? 0),
    );
  });
});
